package managedstorefront

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"proxacloud/backend/internal/proxacloudtunnelapi"
	"proxacloud/backend/internal/tunnelinfra"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Config struct {
	Enabled    bool
	APIToken   string
	BaseDomain string
	TunnelID   string
	TargetURL  string
}

type tunnelAPI interface {
	Enabled() bool
	CreateHTTPRoute(context.Context, string, proxacloudtunnelapi.CreateHTTPRouteRequest) (proxacloudtunnelapi.Route, error)
	ListHTTPRoutes(context.Context, string) ([]proxacloudtunnelapi.Route, error)
	UpdateHTTPRoute(context.Context, string, proxacloudtunnelapi.UpdateHTTPRouteRequest) (proxacloudtunnelapi.Route, error)
	DeleteHTTPRoute(context.Context, string) error
}

type dnsAPI interface {
	EnsureDNS(context.Context, string) (tunnelinfra.DNSResult, error)
	DeleteDNS(context.Context, string) error
}

type Service struct {
	db     *sql.DB
	cfg    Config
	tunnel tunnelAPI
	dns    dnsAPI
}

type Endpoint struct {
	ID               int64     `json:"id"`
	Provider         string    `json:"provider"`
	ExternalID       string    `json:"externalId"`
	Hostname         string    `json:"hostname"`
	TunnelID         string    `json:"tunnelId"`
	TargetURL        string    `json:"targetUrl"`
	HTTPRouteID      string    `json:"httpRouteId"`
	DNSRecordID      string    `json:"dnsRecordId"`
	DNSRecordType    string    `json:"dnsRecordType"`
	DNSRecordContent string    `json:"dnsRecordContent"`
	DNSProxied       bool      `json:"dnsProxied"`
	Status           string    `json:"status"`
	LastError        string    `json:"lastError"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func New(db *sql.DB, cfg Config, tunnel tunnelAPI, dns dnsAPI) *Service {
	cfg.APIToken = strings.TrimSpace(cfg.APIToken)
	cfg.BaseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(cfg.BaseDomain)), ".")
	cfg.TunnelID = strings.TrimSpace(cfg.TunnelID)
	cfg.TargetURL = strings.TrimSpace(cfg.TargetURL)
	return &Service{db: db, cfg: cfg, tunnel: tunnel, dns: dns}
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.db != nil && s.tunnel != nil &&
		s.tunnel.Enabled() && s.dns != nil && s.cfg.APIToken != "" &&
		s.cfg.BaseDomain != "" && s.cfg.TunnelID != "" && s.cfg.TargetURL != ""
}

func (s *Service) Authorize(token string) bool {
	if s == nil || s.cfg.APIToken == "" {
		return false
	}
	expected := sha256.Sum256([]byte(s.cfg.APIToken))
	actual := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}

func (s *Service) Ensure(ctx context.Context, externalID, hostname string) (Endpoint, error) {
	if !s.Enabled() {
		return Endpoint{}, errors.New("managed storefront integration is not configured")
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" || len(externalID) > 128 {
		return Endpoint{}, errors.New("publication id is required and cannot exceed 128 characters")
	}
	hostname, err := s.validateHostname(hostname)
	if err != nil {
		return Endpoint{}, err
	}
	if err := validateTargetURL(s.cfg.TargetURL); err != nil {
		return Endpoint{}, err
	}

	endpoint, err := s.reserve(ctx, externalID, hostname)
	if err != nil {
		return Endpoint{}, err
	}
	fail := func(cause error) (Endpoint, error) {
		_, _ = s.db.ExecContext(context.Background(), `
			UPDATE managed_storefront_endpoints
			SET status = 'failed', last_error = $2, updated_at = NOW()
			WHERE id = $1
		`, endpoint.ID, cause.Error())
		return Endpoint{}, cause
	}

	routes, err := s.tunnel.ListHTTPRoutes(ctx, s.cfg.TunnelID)
	if err != nil {
		return fail(fmt.Errorf("list tunnel routes: %w", err))
	}
	var route proxacloudtunnelapi.Route
	for _, candidate := range routes {
		if (endpoint.HTTPRouteID != "" && candidate.ID == endpoint.HTTPRouteID) ||
			strings.EqualFold(strings.TrimSuffix(candidate.Hostname, "."), hostname) {
			route = candidate
			break
		}
	}
	disabled := false
	if route.ID == "" {
		route, err = s.tunnel.CreateHTTPRoute(ctx, s.cfg.TunnelID, proxacloudtunnelapi.CreateHTTPRouteRequest{
			Hostname: hostname, TargetURL: s.cfg.TargetURL, TLSMode: "edge", Enabled: false,
		})
	} else {
		route, err = s.tunnel.UpdateHTTPRoute(ctx, route.ID, proxacloudtunnelapi.UpdateHTTPRouteRequest{
			Hostname: hostname, TargetURL: s.cfg.TargetURL, Enabled: &disabled,
		})
	}
	if err != nil {
		return fail(fmt.Errorf("reconcile tunnel route: %w", err))
	}

	dnsResult, err := s.dns.EnsureDNS(ctx, hostname)
	if err != nil {
		return fail(fmt.Errorf("reconcile DNS record: %w", err))
	}
	if dnsResult.Status != "active" || strings.TrimSpace(dnsResult.RecordID) == "" {
		return fail(errors.New("Cloudflare DNS provisioning did not return an active record"))
	}
	enabled := true
	if _, err := s.tunnel.UpdateHTTPRoute(ctx, route.ID, proxacloudtunnelapi.UpdateHTTPRouteRequest{Enabled: &enabled}); err != nil {
		return fail(fmt.Errorf("enable tunnel route: %w", err))
	}

	err = s.db.QueryRowContext(ctx, `
		UPDATE managed_storefront_endpoints
		SET http_route_id = $2, dns_record_id = $3, dns_record_type = $4,
		    dns_record_content = $5, dns_proxied = $6, status = 'active',
		    last_error = '', updated_at = NOW()
		WHERE id = $1
		RETURNING id, provider, external_id, hostname, tunnel_id, target_url, http_route_id,
		          dns_record_id, dns_record_type, dns_record_content, dns_proxied,
		          status, last_error, created_at, updated_at
	`, endpoint.ID, route.ID, dnsResult.RecordID, dnsResult.RecordType,
		dnsResult.Content, dnsResult.Proxied).Scan(endpointScanDest(&endpoint)...)
	if err != nil {
		return Endpoint{}, fmt.Errorf("activate managed storefront endpoint: %w", err)
	}
	return endpoint, nil
}

func (s *Service) Get(ctx context.Context, externalID string) (Endpoint, error) {
	var endpoint Endpoint
	err := s.db.QueryRowContext(ctx, `
		SELECT id, provider, external_id, hostname, tunnel_id, target_url, http_route_id,
		       dns_record_id, dns_record_type, dns_record_content, dns_proxied,
		       status, last_error, created_at, updated_at
		FROM managed_storefront_endpoints
		WHERE provider = 'bikri' AND external_id = $1
	`, strings.TrimSpace(externalID)).Scan(endpointScanDest(&endpoint)...)
	return endpoint, err
}

func (s *Service) List(ctx context.Context) ([]Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider, external_id, hostname, tunnel_id, target_url, http_route_id,
		       dns_record_id, dns_record_type, dns_record_content, dns_proxied,
		       status, last_error, created_at, updated_at
		FROM managed_storefront_endpoints
		ORDER BY updated_at DESC, id DESC
		LIMIT 500
	`)
	if err != nil {
		return nil, fmt.Errorf("list managed storefront endpoints: %w", err)
	}
	defer rows.Close()
	endpoints := make([]Endpoint, 0)
	for rows.Next() {
		var endpoint Endpoint
		if err := rows.Scan(endpointScanDest(&endpoint)...); err != nil {
			return nil, fmt.Errorf("scan managed storefront endpoint: %w", err)
		}
		endpoints = append(endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list managed storefront endpoints: %w", err)
	}
	return endpoints, nil
}

func (s *Service) Disable(ctx context.Context, externalID string) error {
	endpoint, err := s.Get(ctx, externalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if endpoint.HTTPRouteID != "" {
		disabled := false
		if _, err := s.tunnel.UpdateHTTPRoute(ctx, endpoint.HTTPRouteID, proxacloudtunnelapi.UpdateHTTPRouteRequest{Enabled: &disabled}); err != nil {
			return fmt.Errorf("disable tunnel route: %w", err)
		}
		if err := s.tunnel.DeleteHTTPRoute(ctx, endpoint.HTTPRouteID); err != nil {
			return fmt.Errorf("delete tunnel route: %w", err)
		}
	}
	if endpoint.DNSRecordID != "" {
		if err := s.dns.DeleteDNS(ctx, endpoint.DNSRecordID); err != nil {
			return fmt.Errorf("delete DNS record: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE managed_storefront_endpoints
		SET status = 'disabled', http_route_id = '', dns_record_id = '',
		    dns_record_type = '', dns_record_content = '', last_error = '',
		    updated_at = NOW()
		WHERE id = $1
	`, endpoint.ID)
	return err
}

func (s *Service) reserve(ctx context.Context, externalID, hostname string) (Endpoint, error) {
	var endpoint Endpoint
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO managed_storefront_endpoints (
			provider, external_id, hostname, tunnel_id, target_url, status
		)
		VALUES ('bikri', $1, $2, $3, $4, 'pending')
		ON CONFLICT (provider, external_id) DO UPDATE
		SET hostname = EXCLUDED.hostname, tunnel_id = EXCLUDED.tunnel_id,
		    target_url = EXCLUDED.target_url, status = 'pending',
		    last_error = '', updated_at = NOW()
		RETURNING id, provider, external_id, hostname, tunnel_id, target_url, http_route_id,
		          dns_record_id, dns_record_type, dns_record_content, dns_proxied,
		          status, last_error, created_at, updated_at
	`, externalID, hostname, s.cfg.TunnelID, s.cfg.TargetURL).Scan(endpointScanDest(&endpoint)...)
	if err != nil {
		return Endpoint{}, fmt.Errorf("reserve managed storefront endpoint: %w", err)
	}
	return endpoint, nil
}

func (s *Service) validateHostname(hostname string) (string, error) {
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	suffix := "." + s.cfg.BaseDomain
	if !strings.HasSuffix(hostname, suffix) {
		return "", fmt.Errorf("hostname must be within %s", s.cfg.BaseDomain)
	}
	label := strings.TrimSuffix(hostname, suffix)
	if !labelPattern.MatchString(label) {
		return "", errors.New("hostname must contain exactly one valid label before the storefront base domain")
	}
	return hostname, nil
}

func validateTargetURL(raw string) error {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return errors.New("managed storefront target URL must be an absolute HTTP or HTTPS URL")
	}
	return nil
}

func TokenMiddleware(service *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			scheme, token, ok := strings.Cut(header, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || !service.Authorize(token) {
				WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid integration token"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func endpointScanDest(endpoint *Endpoint) []any {
	return []any{
		&endpoint.ID, &endpoint.Provider, &endpoint.ExternalID, &endpoint.Hostname, &endpoint.TunnelID,
		&endpoint.TargetURL, &endpoint.HTTPRouteID, &endpoint.DNSRecordID,
		&endpoint.DNSRecordType, &endpoint.DNSRecordContent, &endpoint.DNSProxied,
		&endpoint.Status, &endpoint.LastError, &endpoint.CreatedAt, &endpoint.UpdatedAt,
	}
}

func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
