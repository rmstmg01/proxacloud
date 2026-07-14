package tunnelinfra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"proxacloud/backend/internal/proxacloudtunnelapi"
)

const cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

type API interface {
	Enabled() bool
	CreateTunnel(context.Context, proxacloudtunnelapi.CreateTunnelRequest) (proxacloudtunnelapi.Tunnel, error)
	CreateHTTPRoute(context.Context, string, proxacloudtunnelapi.CreateHTTPRouteRequest) (proxacloudtunnelapi.Route, error)
	CreateTCPRoute(context.Context, string, proxacloudtunnelapi.CreateTCPRouteRequest) (proxacloudtunnelapi.TCPRoute, error)
	CreateToken(context.Context, string, proxacloudtunnelapi.CreateTokenRequest) (proxacloudtunnelapi.TokenResponse, error)
	DeleteTunnel(context.Context, string) error
	DeleteHTTPRoute(context.Context, string) error
	DeleteTCPRoute(context.Context, string) error
	DeleteToken(context.Context, string) error
}

type CloudflareConfig struct {
	Enabled    bool
	APIToken   string
	ZoneID     string
	Domain     string
	Target     string
	RecordType string
	Proxied    bool
}

type Config struct {
	Enabled        bool
	APIURL         string
	GatewayURL     string
	HostnameSuffix string
	AgentBinaryURL string
	Cloudflare     CloudflareConfig
}

type Service struct {
	cfg    Config
	client API
	http   *http.Client
}

type ProvisionRequest struct {
	Name            string
	Slug            string
	Description     string
	Hostname        string
	PublicPorts     []int
	TCPTargetHost   string
	TCPTargetPort   int
	CreateHTTPRoute bool
	HTTPTargetURL   string
	AgentTargetURL  string
	TokenName       string
	TCPRouteName    string
}

type DNSResult struct {
	RecordID   string
	RecordType string
	Content    string
	Proxied    bool
	Status     string
}

type Result struct {
	TunnelID       string
	HTTPRouteID    string
	TCPRouteID     string
	TokenID        string
	PublicHostname string
	PublicPort     int
	InstallCommand string
	DNS            DNSResult
}

func New(cfg Config, client API) *Service {
	cfg.APIURL = strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	cfg.GatewayURL = strings.TrimSpace(cfg.GatewayURL)
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = gatewayURLFromAPIURL(cfg.APIURL)
	}
	cfg.HostnameSuffix = strings.Trim(strings.ToLower(strings.TrimSpace(cfg.HostnameSuffix)), ".")
	if cfg.HostnameSuffix == "" {
		cfg.HostnameSuffix = "proxaops.com"
	}
	cfg.AgentBinaryURL = strings.TrimSpace(cfg.AgentBinaryURL)
	cfg.Cloudflare.APIToken = strings.TrimSpace(cfg.Cloudflare.APIToken)
	cfg.Cloudflare.ZoneID = strings.TrimSpace(cfg.Cloudflare.ZoneID)
	cfg.Cloudflare.Domain = strings.Trim(strings.ToLower(strings.TrimSpace(cfg.Cloudflare.Domain)), ".")
	if cfg.Cloudflare.Domain == "" {
		cfg.Cloudflare.Domain = cfg.HostnameSuffix
	}
	cfg.Cloudflare.Target = strings.TrimSpace(cfg.Cloudflare.Target)
	cfg.Cloudflare.RecordType = strings.ToUpper(strings.TrimSpace(cfg.Cloudflare.RecordType))
	return &Service{cfg: cfg, client: client, http: &http.Client{Timeout: 20 * time.Second}}
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.client != nil && s.client.Enabled()
}

func (s *Service) Hostname(base string) (string, error) {
	label := sanitizeLabel(base)
	if label == "" {
		return "", errors.New("hostname label is required")
	}
	return label + "." + s.cfg.HostnameSuffix, nil
}

func (s *Service) Provision(ctx context.Context, req ProvisionRequest) (Result, error) {
	if !s.Enabled() {
		return Result{}, errors.New("proxacloud tunnel integration is not configured")
	}
	hostname := strings.Trim(strings.ToLower(strings.TrimSpace(req.Hostname)), ".")
	if hostname == "" {
		return Result{}, errors.New("public hostname is required")
	}
	if len(req.PublicPorts) == 0 {
		return Result{}, errors.New("at least one public TCP port candidate is required")
	}
	if req.TCPTargetHost == "" {
		req.TCPTargetHost = "127.0.0.1"
	}
	if req.TCPTargetPort <= 0 {
		req.TCPTargetPort = 22
	}
	if req.TokenName == "" {
		req.TokenName = "ProxaCloud agent token"
	}
	if req.TCPRouteName == "" {
		req.TCPRouteName = "SSH"
	}

	tunnel, err := s.client.CreateTunnel(ctx, proxacloudtunnelapi.CreateTunnelRequest{Name: req.Name, Slug: req.Slug, Description: req.Description})
	if err != nil {
		return Result{}, fmt.Errorf("create proxacloud tunnel: %w", err)
	}
	result := Result{TunnelID: tunnel.ID, PublicHostname: hostname}
	cleanup := func() { _ = s.Cleanup(context.Background(), result) }

	if req.CreateHTTPRoute {
		route, routeErr := s.client.CreateHTTPRoute(ctx, tunnel.ID, proxacloudtunnelapi.CreateHTTPRouteRequest{Hostname: hostname, TargetURL: req.HTTPTargetURL, TLSMode: "edge", Enabled: true})
		if routeErr != nil {
			cleanup()
			return Result{}, fmt.Errorf("create proxacloud HTTP route: %w", routeErr)
		}
		result.HTTPRouteID = route.ID
	}

	var lastErr error
	for _, port := range req.PublicPorts {
		route, routeErr := s.client.CreateTCPRoute(ctx, tunnel.ID, proxacloudtunnelapi.CreateTCPRouteRequest{Name: req.TCPRouteName, PublicHostname: hostname, PublicPort: port, TargetHost: req.TCPTargetHost, TargetPort: req.TCPTargetPort, Enabled: true})
		if routeErr == nil {
			result.TCPRouteID = route.ID
			result.PublicPort = port
			lastErr = nil
			break
		}
		lastErr = routeErr
		if !isDuplicatePortError(routeErr) {
			break
		}
	}
	if lastErr != nil || result.TCPRouteID == "" {
		cleanup()
		return Result{}, fmt.Errorf("create proxacloud TCP route: %w", lastErr)
	}

	token, err := s.client.CreateToken(ctx, tunnel.ID, proxacloudtunnelapi.CreateTokenRequest{Name: req.TokenName, Scope: "all"})
	if err != nil {
		cleanup()
		return Result{}, fmt.Errorf("create proxacloud agent token: %w", err)
	}
	result.TokenID = token.Item.ID
	result.InstallCommand = s.AgentInstallCommand(tunnel.ID, token.Token, req.AgentTargetURL)

	dns, err := s.EnsureDNS(ctx, hostname)
	if err != nil {
		cleanup()
		return Result{}, err
	}
	result.DNS = dns
	return result, nil
}

func (s *Service) Cleanup(ctx context.Context, result Result) error {
	var cleanupErr error
	if result.DNS.RecordID != "" {
		if err := s.DeleteDNS(ctx, result.DNS.RecordID); err != nil {
			cleanupErr = err
		}
	}
	if result.TunnelID != "" {
		if err := s.client.DeleteTunnel(ctx, result.TunnelID); err != nil && cleanupErr == nil {
			cleanupErr = err
		}
		return cleanupErr
	}
	if result.TokenID != "" {
		_ = s.client.DeleteToken(ctx, result.TokenID)
	}
	if result.HTTPRouteID != "" {
		_ = s.client.DeleteHTTPRoute(ctx, result.HTTPRouteID)
	}
	if result.TCPRouteID != "" {
		_ = s.client.DeleteTCPRoute(ctx, result.TCPRouteID)
	}
	return cleanupErr
}

func (s *Service) AgentInstallCommand(tunnelID, token, targetURL string) string {
	binaryURL := s.cfg.AgentBinaryURL
	if binaryURL == "" && s.cfg.APIURL != "" {
		binaryURL = s.cfg.APIURL + "/api/v1/downloads/proxacloud-agent-linux-amd64"
	}
	args := []string{"curl", "-fsSL", s.cfg.APIURL + "/api/v1/install-agent.sh", "|", "sudo", "sh", "-s", "--", "--tunnel-id", tunnelID, "--token", token, "--gateway-url", s.cfg.GatewayURL}
	if strings.TrimSpace(targetURL) != "" {
		args = append(args, "--target-url", targetURL)
	}
	if binaryURL != "" {
		args = append(args, "--binary-url", binaryURL)
	}
	for i := range args {
		if args[i] != "|" {
			args[i] = shellQuote(args[i])
		}
	}
	return strings.Join(args, " ")
}

func (s *Service) EnsureDNS(ctx context.Context, hostname string) (DNSResult, error) {
	cfg := s.cfg.Cloudflare
	if !cfg.Enabled {
		return DNSResult{Status: "disabled"}, nil
	}
	if cfg.APIToken == "" || cfg.ZoneID == "" || cfg.Target == "" {
		return DNSResult{}, errors.New("Cloudflare DNS token, zone ID, and target are required")
	}
	recordType := cfg.RecordType
	if recordType == "" {
		recordType = recordTypeForTarget(cfg.Target)
	}
	content := strings.TrimSuffix(cfg.Target, ".")
	existing, err := s.findDNS(ctx, hostname, recordType)
	if err != nil {
		return DNSResult{}, err
	}
	if existing != nil {
		if existing.Content == content && existing.Proxied == cfg.Proxied {
			return DNSResult{RecordID: existing.ID, RecordType: recordType, Content: content, Proxied: cfg.Proxied, Status: "active"}, nil
		}
		if err := s.DeleteDNS(ctx, existing.ID); err != nil {
			return DNSResult{}, err
		}
	}
	id, err := s.createDNS(ctx, hostname, recordType, content, cfg.Proxied)
	if err != nil {
		return DNSResult{}, err
	}
	return DNSResult{RecordID: id, RecordType: recordType, Content: content, Proxied: cfg.Proxied, Status: "active"}, nil
}

type dnsRecord struct {
	ID, Content string
	Proxied     bool
}
type cfRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}
type cfResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID, Name, Type, Content string
		Proxied                 bool
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}
type cfListResponse struct {
	Success bool `json:"success"`
	Result  []struct {
		ID, Name, Type, Content string
		Proxied                 bool
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (s *Service) createDNS(ctx context.Context, hostname, recordType, content string, proxied bool) (string, error) {
	payload, _ := json.Marshal(cfRequest{Type: recordType, Name: hostname, Content: content, TTL: 1, Proxied: proxied})
	req, err := s.cfRequest(ctx, http.MethodPost, "/dns_records", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	var out cfResponse
	if err := s.do(req, &out); err != nil {
		return "", err
	}
	if !out.Success {
		return "", cfError(out.Errors)
	}
	return out.Result.ID, nil
}

func (s *Service) findDNS(ctx context.Context, hostname, recordType string) (*dnsRecord, error) {
	query := url.Values{"type": {recordType}, "name": {strings.TrimSuffix(strings.ToLower(hostname), ".")}}
	req, err := s.cfRequest(ctx, http.MethodGet, "/dns_records?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out cfListResponse
	if err := s.do(req, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, cfError(out.Errors)
	}
	if len(out.Result) == 0 {
		return nil, nil
	}
	return &dnsRecord{ID: out.Result[0].ID, Content: out.Result[0].Content, Proxied: out.Result[0].Proxied}, nil
}

func (s *Service) DeleteDNS(ctx context.Context, recordID string) error {
	if recordID == "" || !s.cfg.Cloudflare.Enabled {
		return nil
	}
	req, err := s.cfRequest(ctx, http.MethodDelete, "/dns_records/"+recordID, nil)
	if err != nil {
		return err
	}
	var out cfResponse
	if err := s.do(req, &out); err != nil {
		return err
	}
	if !out.Success {
		err := cfError(out.Errors)
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "not found") || strings.Contains(message, "does not exist") {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) cfRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBaseURL+"/zones/"+s.cfg.Cloudflare.ZoneID+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Cloudflare.APIToken)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (s *Service) do(req *http.Request, out any) error {
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Cloudflare API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse Cloudflare response: %w", err)
	}
	return nil
}

func cfError(items []struct {
	Message string `json:"message"`
}) error {
	if len(items) > 0 && strings.TrimSpace(items[0].Message) != "" {
		return errors.New("Cloudflare API error: " + items[0].Message)
	}
	return errors.New("Cloudflare API error")
}

func recordTypeForTarget(target string) string {
	if net.ParseIP(strings.TrimSpace(target)) != nil {
		return "A"
	}
	return "CNAME"
}
func sanitizeLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
func isDuplicatePortError(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "already") || strings.Contains(m, "duplicate") || strings.Contains(m, "in use") || strings.Contains(m, "conflict")
}
func gatewayURLFromAPIURL(apiURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else if parsed.Scheme == "http" {
		parsed.Scheme = "ws"
	}
	parsed.Path = "/__agent/connect"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
