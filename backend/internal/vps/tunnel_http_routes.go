package vps

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"proxacloud/backend/internal/proxacloudtunnelapi"
)

const maxVPSHTTPRoutes = 20

var ErrTunnelHTTPRouteNotFound = errors.New("VPS tunnel HTTP route not found")

type TunnelHTTPRoute struct {
	ID                         int64     `json:"id"`
	TunnelAttachmentID         int64     `json:"tunnel_attachment_id"`
	HTTPRouteID                string    `json:"http_route_id"`
	Name                       string    `json:"name"`
	PublicHostname             string    `json:"public_hostname"`
	TargetURL                  string    `json:"target_url"`
	TLSMode                    string    `json:"tls_mode"`
	Enabled                    bool      `json:"enabled"`
	CloudflareDNSRecordID      string    `json:"cloudflare_dns_record_id,omitempty"`
	CloudflareDNSRecordType    string    `json:"cloudflare_dns_record_type,omitempty"`
	CloudflareDNSRecordContent string    `json:"cloudflare_dns_record_content,omitempty"`
	CloudflareDNSProxied       bool      `json:"cloudflare_dns_proxied"`
	CloudflareDNSStatus        string    `json:"cloudflare_dns_status,omitempty"`
	Status                     string    `json:"status"`
	LastError                  string    `json:"last_error"`
	IsPrimary                  bool      `json:"is_primary"`
	WebURL                     string    `json:"web_url"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type CreateTunnelHTTPRouteRequest struct {
	Name           string `json:"name"`
	PublicHostname string `json:"public_hostname"`
	TargetURL      string `json:"target_url"`
	TLSMode        string `json:"tls_mode"`
	Enabled        *bool  `json:"enabled,omitempty"`
}

type UpdateTunnelHTTPRouteRequest struct {
	Name           string `json:"name"`
	PublicHostname string `json:"public_hostname"`
	TargetURL      string `json:"target_url"`
	TLSMode        string `json:"tls_mode"`
	Enabled        bool   `json:"enabled"`
}

func (s *Service) ListTunnelHTTPRoutes(vpsID int64) ([]TunnelHTTPRoute, error) {
	rows, err := s.db.Query(`
		SELECT r.id, r.tunnel_attachment_id, r.http_route_id, r.name, r.public_hostname,
		       r.target_url, r.tls_mode, r.enabled, r.cloudflare_dns_record_id,
		       r.cloudflare_dns_record_type, r.cloudflare_dns_record_content,
		       r.cloudflare_dns_proxied, r.cloudflare_dns_status, r.status, r.last_error,
		       r.is_primary,
		       r.created_at, r.updated_at
		FROM vps_tunnel_http_routes r
		JOIN vps_tunnel_attachments a ON a.id = r.tunnel_attachment_id
		WHERE a.vps_id = $1
		ORDER BY r.created_at, r.id
	`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("list VPS tunnel HTTP routes: %w", err)
	}
	defer rows.Close()

	routes := make([]TunnelHTTPRoute, 0)
	for rows.Next() {
		route, scanErr := scanTunnelHTTPRoute(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list VPS tunnel HTTP routes: %w", err)
	}
	return routes, nil
}

func (s *Service) CreateTunnelHTTPRoute(vpsID int64, req CreateTunnelHTTPRouteRequest) (TunnelHTTPRoute, error) {
	name, hostname, targetURL, tlsMode, err := s.validateTunnelHTTPRoute(req.Name, req.PublicHostname, req.TargetURL, req.TLSMode)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	domainID, err := s.managedDomainIDForHostname(context.Background(), hostname)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	attachment, err := s.GetTunnelAttachment(vpsID)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	if !s.tunnelConfigured() || strings.TrimSpace(attachment.TunnelID) == "" {
		return TunnelHTTPRoute{}, errors.New("VPS tunnel is not configured")
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM vps_tunnel_http_routes WHERE tunnel_attachment_id = $1`, attachment.ID).Scan(&count); err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("count VPS tunnel HTTP routes: %w", err)
	}
	if count >= maxVPSHTTPRoutes {
		return TunnelHTTPRoute{}, fmt.Errorf("a VPS tunnel can have at most %d HTTP routes", maxVPSHTTPRoutes)
	}

	var route TunnelHTTPRoute
	err = s.db.QueryRow(`
		INSERT INTO vps_tunnel_http_routes (
			tunnel_attachment_id, name, public_hostname, target_url, tls_mode, enabled, status, managed_domain_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', NULLIF($7, 0))
		RETURNING id, tunnel_attachment_id, http_route_id, name, public_hostname, target_url,
		          tls_mode, enabled, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, FALSE, created_at, updated_at
	`, attachment.ID, name, hostname, targetURL, tlsMode, enabled, domainID).Scan(tunnelHTTPRouteScanDest(&route)...)
	if err != nil {
		if isDuplicateRouteHostnameError(err) {
			return TunnelHTTPRoute{}, fmt.Errorf("public hostname %s is already in use", hostname)
		}
		return TunnelHTTPRoute{}, fmt.Errorf("create VPS tunnel HTTP route: %w", err)
	}
	route.populate()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	route, err = s.provisionTunnelHTTPRoute(ctx, attachment, route)
	if err != nil {
		_ = s.markTunnelHTTPRouteFailed(ctx, route.ID, err)
		return TunnelHTTPRoute{}, err
	}
	_ = s.insertEvent(vpsID, "tunnel-http-route-create", "success", "HTTP route configured", route.WebURL+" -> "+route.TargetURL)
	return route, nil
}

func (s *Service) UpdateTunnelHTTPRoute(vpsID, routeID int64, req UpdateTunnelHTTPRouteRequest) (TunnelHTTPRoute, error) {
	name, hostname, targetURL, tlsMode, err := s.validateTunnelHTTPRoute(req.Name, req.PublicHostname, req.TargetURL, req.TLSMode)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	domainID, err := s.managedDomainIDForHostname(context.Background(), hostname)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	attachment, route, err := s.getTunnelHTTPRoute(vpsID, routeID)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	if !s.tunnelConfigured() || strings.TrimSpace(attachment.TunnelID) == "" {
		return TunnelHTTPRoute{}, errors.New("VPS tunnel is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	oldHostname := route.PublicHostname
	if route.IsPrimary && hostname != oldHostname {
		return TunnelHTTPRoute{}, errors.New("the primary route hostname is also used by SSH and cannot be changed")
	}
	if route.TLSMode != tlsMode {
		return TunnelHTTPRoute{}, errors.New("TLS mode cannot be changed after a route is created")
	}
	if route.HTTPRouteID == "" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE vps_tunnel_http_routes
			SET name = $2, public_hostname = $3, target_url = $4, tls_mode = $5,
			    enabled = $6, managed_domain_id = NULLIF($7, 0), status = 'pending', last_error = '', updated_at = NOW()
			WHERE id = $1
		`, route.ID, name, hostname, targetURL, tlsMode, req.Enabled, domainID)
		if err != nil {
			if isDuplicateRouteHostnameError(err) {
				return TunnelHTTPRoute{}, fmt.Errorf("public hostname %s is already in use", hostname)
			}
			return TunnelHTTPRoute{}, fmt.Errorf("save pending VPS tunnel HTTP route: %w", err)
		}
		route.Name = name
		route.PublicHostname = hostname
		route.TargetURL = targetURL
		route.TLSMode = tlsMode
		route.Enabled = req.Enabled
		route.Status = "pending"
		route.LastError = ""
		route, err = s.provisionTunnelHTTPRoute(ctx, attachment, route)
		if err != nil {
			_ = s.markTunnelHTTPRouteFailed(ctx, route.ID, err)
			return TunnelHTTPRoute{}, err
		}
		_ = s.insertEvent(vpsID, "tunnel-http-route-update", "success", "HTTP route configured", route.WebURL+" -> "+route.TargetURL)
		return route, nil
	}

	if _, err := s.tunnelClient.UpdateHTTPRoute(ctx, route.HTTPRouteID, proxacloudtunnelapi.UpdateHTTPRouteRequest{
		Hostname: hostname, TargetURL: targetURL,
	}); err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("update proxacloud HTTP route: %w", err)
	}
	if route.Enabled != req.Enabled {
		enabled := req.Enabled
		if _, err := s.tunnelClient.UpdateHTTPRoute(ctx, route.HTTPRouteID, proxacloudtunnelapi.UpdateHTTPRouteRequest{Enabled: &enabled}); err != nil {
			return TunnelHTTPRoute{}, fmt.Errorf("update proxacloud HTTP route state: %w", err)
		}
	}

	route.Name = name
	route.PublicHostname = hostname
	route.TargetURL = targetURL
	route.TLSMode = tlsMode
	route.Enabled = req.Enabled
	route.Status = "pending"
	route.LastError = ""
	if oldHostname != route.PublicHostname && route.CloudflareDNSRecordID != "" {
		if err := s.deleteCloudflareDNSRecord(ctx, route.CloudflareDNSRecordID, oldHostname); err != nil {
			return TunnelHTTPRoute{}, fmt.Errorf("delete previous HTTP route DNS record: %w", err)
		}
		route.CloudflareDNSRecordID = ""
		route.CloudflareDNSRecordType = ""
		route.CloudflareDNSRecordContent = ""
		route.CloudflareDNSStatus = ""
	}
	route, err = s.syncTunnelHTTPRouteDNS(ctx, attachment, route)
	if err != nil {
		_ = s.markTunnelHTTPRouteFailed(ctx, route.ID, err)
		return TunnelHTTPRoute{}, err
	}

	err = s.db.QueryRowContext(ctx, `
		UPDATE vps_tunnel_http_routes
		SET name = $2, public_hostname = $3, target_url = $4, tls_mode = $5, enabled = $6,
		    cloudflare_dns_record_id = $7, cloudflare_dns_record_type = $8,
		    cloudflare_dns_record_content = $9, cloudflare_dns_proxied = $10,
		    cloudflare_dns_status = $11, managed_domain_id = NULLIF($12, 0), status = 'active', last_error = '', updated_at = NOW()
		WHERE id = $1
		RETURNING id, tunnel_attachment_id, http_route_id, name, public_hostname, target_url,
		          tls_mode, enabled, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, $13, created_at, updated_at
	`, route.ID, route.Name, route.PublicHostname, route.TargetURL, route.TLSMode, route.Enabled,
		route.CloudflareDNSRecordID, route.CloudflareDNSRecordType, route.CloudflareDNSRecordContent,
		route.CloudflareDNSProxied, route.CloudflareDNSStatus, domainID, route.IsPrimary).Scan(tunnelHTTPRouteScanDest(&route)...)
	if err != nil {
		if isDuplicateRouteHostnameError(err) {
			return TunnelHTTPRoute{}, fmt.Errorf("public hostname %s is already in use", hostname)
		}
		return TunnelHTTPRoute{}, fmt.Errorf("update VPS tunnel HTTP route: %w", err)
	}
	route.populate()
	if route.IsPrimary {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE vps_tunnel_attachments
			SET public_hostname = $2, http_target_url = $3,
			    cloudflare_dns_record_id = $4, cloudflare_dns_record_type = $5,
			    cloudflare_dns_record_content = $6, cloudflare_dns_proxied = $7,
			    cloudflare_dns_status = $8, updated_at = NOW()
			WHERE id = $1
		`, attachment.ID, route.PublicHostname, route.TargetURL, route.CloudflareDNSRecordID,
			route.CloudflareDNSRecordType, route.CloudflareDNSRecordContent,
			route.CloudflareDNSProxied, route.CloudflareDNSStatus)
	}
	_ = s.insertEvent(vpsID, "tunnel-http-route-update", "success", "HTTP route updated", oldHostname+" -> "+route.WebURL)
	return route, nil
}

func (s *Service) RetryTunnelHTTPRoute(vpsID, routeID int64) (TunnelHTTPRoute, error) {
	attachment, route, err := s.getTunnelHTTPRoute(vpsID, routeID)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	if !s.tunnelConfigured() || strings.TrimSpace(attachment.TunnelID) == "" {
		return TunnelHTTPRoute{}, errors.New("VPS tunnel is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	route, err = s.provisionTunnelHTTPRoute(ctx, attachment, route)
	if err != nil {
		_ = s.markTunnelHTTPRouteFailed(ctx, route.ID, err)
		return TunnelHTTPRoute{}, err
	}
	_ = s.insertEvent(vpsID, "tunnel-http-route-retry", "success", "HTTP route synchronized", route.WebURL+" -> "+route.TargetURL)
	return route, nil
}

func (s *Service) DeleteTunnelHTTPRoute(vpsID, routeID int64) error {
	_, route, err := s.getTunnelHTTPRoute(vpsID, routeID)
	if err != nil {
		return err
	}
	if route.IsPrimary {
		return errors.New("the primary HTTP route cannot be deleted; edit or disable it instead")
	}
	if !s.tunnelConfigured() {
		return errors.New("VPS tunnel is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if route.HTTPRouteID != "" {
		if err := s.tunnelClient.DeleteHTTPRoute(ctx, route.HTTPRouteID); err != nil {
			return fmt.Errorf("delete proxacloud HTTP route: %w", err)
		}
	}
	if route.CloudflareDNSRecordID != "" {
		if err := s.deleteCloudflareDNSRecord(ctx, route.CloudflareDNSRecordID, route.PublicHostname); err != nil {
			return fmt.Errorf("delete HTTP route DNS record: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM vps_tunnel_http_routes WHERE id = $1`, route.ID); err != nil {
		return fmt.Errorf("delete VPS tunnel HTTP route: %w", err)
	}
	_ = s.insertEvent(vpsID, "tunnel-http-route-delete", "success", "HTTP route deleted", route.WebURL)
	return nil
}

func (s *Service) syncAllTunnelHTTPRoutes(ctx context.Context, attachment TunnelAttachment) error {
	routes, err := s.ListTunnelHTTPRoutes(attachment.VPSID)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if _, err := s.provisionTunnelHTTPRoute(ctx, attachment, route); err != nil {
			_ = s.markTunnelHTTPRouteFailed(ctx, route.ID, err)
			return fmt.Errorf("synchronize %s: %w", route.PublicHostname, err)
		}
	}
	return nil
}

func (s *Service) provisionTunnelHTTPRoute(ctx context.Context, attachment TunnelAttachment, route TunnelHTTPRoute) (TunnelHTTPRoute, error) {
	remoteRoutes, err := s.tunnelClient.ListHTTPRoutes(ctx, attachment.TunnelID)
	if err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("list proxacloud HTTP routes: %w", err)
	}
	var remote proxacloudtunnelapi.Route
	for _, item := range remoteRoutes {
		if route.HTTPRouteID != "" && item.ID == route.HTTPRouteID {
			remote = item
			break
		}
		if strings.EqualFold(strings.TrimSuffix(item.Hostname, "."), route.PublicHostname) {
			remote = item
			break
		}
	}
	if remote.ID == "" {
		remote, err = s.tunnelClient.CreateHTTPRoute(ctx, attachment.TunnelID, proxacloudtunnelapi.CreateHTTPRouteRequest{
			Hostname: route.PublicHostname, TargetURL: route.TargetURL, TLSMode: route.TLSMode, Enabled: route.Enabled,
		})
		if err != nil {
			return TunnelHTTPRoute{}, fmt.Errorf("create proxacloud HTTP route: %w", err)
		}
	} else {
		if remote.TLSMode != "" && remote.TLSMode != route.TLSMode {
			return TunnelHTTPRoute{}, fmt.Errorf(
				"proxacloud HTTP route %s uses TLS mode %s instead of %s",
				remote.Hostname, remote.TLSMode, route.TLSMode,
			)
		}
		if remote.Hostname != route.PublicHostname || remote.TargetURL != route.TargetURL {
			remote, err = s.tunnelClient.UpdateHTTPRoute(ctx, remote.ID, proxacloudtunnelapi.UpdateHTTPRouteRequest{
				Hostname: route.PublicHostname, TargetURL: route.TargetURL,
			})
			if err != nil {
				return TunnelHTTPRoute{}, fmt.Errorf("reconcile proxacloud HTTP route: %w", err)
			}
		}
		if remote.Enabled != route.Enabled {
			enabled := route.Enabled
			remote, err = s.tunnelClient.UpdateHTTPRoute(ctx, remote.ID, proxacloudtunnelapi.UpdateHTTPRouteRequest{Enabled: &enabled})
			if err != nil {
				return TunnelHTTPRoute{}, fmt.Errorf("reconcile proxacloud HTTP route state: %w", err)
			}
		}
	}
	route.HTTPRouteID = remote.ID
	route, err = s.syncTunnelHTTPRouteDNS(ctx, attachment, route)
	if err != nil {
		return TunnelHTTPRoute{}, err
	}
	err = s.db.QueryRowContext(ctx, `
		UPDATE vps_tunnel_http_routes
		SET http_route_id = $2, cloudflare_dns_record_id = $3, cloudflare_dns_record_type = $4,
		    cloudflare_dns_record_content = $5, cloudflare_dns_proxied = $6,
		    cloudflare_dns_status = $7, status = 'active', last_error = '', updated_at = NOW()
		WHERE id = $1
		RETURNING id, tunnel_attachment_id, http_route_id, name, public_hostname, target_url,
		          tls_mode, enabled, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, $8, created_at, updated_at
	`, route.ID, route.HTTPRouteID, route.CloudflareDNSRecordID, route.CloudflareDNSRecordType,
		route.CloudflareDNSRecordContent, route.CloudflareDNSProxied, route.CloudflareDNSStatus,
		route.IsPrimary).Scan(tunnelHTTPRouteScanDest(&route)...)
	if err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("activate VPS tunnel HTTP route: %w", err)
	}
	route.populate()
	if route.IsPrimary {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE vps_tunnel_attachments
			SET http_route_id = $2, http_target_url = $3,
			    cloudflare_dns_record_id = $4, cloudflare_dns_record_type = $5,
			    cloudflare_dns_record_content = $6, cloudflare_dns_proxied = $7,
			    cloudflare_dns_status = $8, updated_at = NOW()
			WHERE id = $1
		`, attachment.ID, route.HTTPRouteID, route.TargetURL, route.CloudflareDNSRecordID,
			route.CloudflareDNSRecordType, route.CloudflareDNSRecordContent,
			route.CloudflareDNSProxied, route.CloudflareDNSStatus); err != nil {
			return TunnelHTTPRoute{}, fmt.Errorf("update primary VPS tunnel HTTP route: %w", err)
		}
	}
	return route, nil
}

func (s *Service) syncTunnelHTTPRouteDNS(ctx context.Context, attachment TunnelAttachment, route TunnelHTTPRoute) (TunnelHTTPRoute, error) {
	dnsAttachment := attachment
	dnsAttachment.PublicHostname = route.PublicHostname
	dnsAttachment.CloudflareDNSRecordID = route.CloudflareDNSRecordID
	dnsAttachment.CloudflareDNSRecordType = route.CloudflareDNSRecordType
	dnsAttachment.CloudflareDNSRecordContent = route.CloudflareDNSRecordContent
	dnsAttachment.CloudflareDNSProxied = route.CloudflareDNSProxied
	result, err := s.ensureCloudflareDNSRecord(ctx, dnsAttachment)
	if err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("sync HTTP route DNS: %w", err)
	}
	route.CloudflareDNSRecordID = result.RecordID
	route.CloudflareDNSRecordType = result.RecordType
	route.CloudflareDNSRecordContent = result.Content
	route.CloudflareDNSProxied = result.Proxied
	route.CloudflareDNSStatus = result.Status
	return route, nil
}

func (s *Service) getTunnelHTTPRoute(vpsID, routeID int64) (TunnelAttachment, TunnelHTTPRoute, error) {
	attachment, err := s.GetTunnelAttachment(vpsID)
	if err != nil {
		return TunnelAttachment{}, TunnelHTTPRoute{}, err
	}
	var route TunnelHTTPRoute
	err = s.db.QueryRow(`
		SELECT r.id, r.tunnel_attachment_id, r.http_route_id, r.name, r.public_hostname,
		       r.target_url, r.tls_mode, r.enabled, r.cloudflare_dns_record_id,
		       r.cloudflare_dns_record_type, r.cloudflare_dns_record_content,
		       r.cloudflare_dns_proxied, r.cloudflare_dns_status, r.status, r.last_error,
		       r.is_primary,
		       r.created_at, r.updated_at
		FROM vps_tunnel_http_routes r
		JOIN vps_tunnel_attachments a ON a.id = r.tunnel_attachment_id
		WHERE r.id = $1 AND a.vps_id = $2
	`, routeID, vpsID).Scan(tunnelHTTPRouteScanDest(&route)...)
	if errors.Is(err, sql.ErrNoRows) {
		return TunnelAttachment{}, TunnelHTTPRoute{}, ErrTunnelHTTPRouteNotFound
	}
	if err != nil {
		return TunnelAttachment{}, TunnelHTTPRoute{}, fmt.Errorf("get VPS tunnel HTTP route: %w", err)
	}
	route.populate()
	return attachment, route, nil
}

func (s *Service) validateTunnelHTTPRoute(name, hostname, targetURL, tlsMode string) (string, string, string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", "", errors.New("route name is required")
	}
	if len(name) > 100 {
		return "", "", "", "", errors.New("route name must be 100 characters or fewer")
	}
	hostname = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(hostname)), ".")
	if err := validateTunnelRouteHostname(hostname); err != nil {
		return "", "", "", "", err
	}
	if s.managedDomains != nil {
		if _, err := s.managedDomains.RuntimeForHostname(context.Background(), hostname); err != nil {
			return "", "", "", "", errors.New("public hostname is not within an active managed domain")
		}
	} else {
		suffix := strings.Trim(strings.ToLower(strings.TrimSpace(s.tunnelConfig.HostnameSuffix)), ".")
		if suffix == "" {
			suffix = strings.Trim(strings.ToLower(strings.TrimSpace(s.tunnelConfig.CloudflareDNS.Domain)), ".")
		}
		if suffix != "" && hostname != suffix && !strings.HasSuffix(hostname, "."+suffix) {
			return "", "", "", "", fmt.Errorf("public hostname must be within %s", suffix)
		}
	}
	target, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return "", "", "", "", errors.New("target URL must be a valid absolute URL")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return "", "", "", "", errors.New("target URL scheme must be http or https")
	}
	if target.User != nil {
		return "", "", "", "", errors.New("target URL must not include credentials")
	}
	if target.Fragment != "" {
		return "", "", "", "", errors.New("target URL must not include a fragment")
	}
	if port := target.Port(); port != "" {
		parsed, err := net.LookupPort("tcp", port)
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", "", "", "", errors.New("target URL port must be between 1 and 65535")
		}
	}
	tlsMode = strings.ToLower(strings.TrimSpace(tlsMode))
	if tlsMode == "" {
		tlsMode = "edge"
	}
	if tlsMode != "edge" && tlsMode != "passthrough" && tlsMode != "reencrypt" {
		return "", "", "", "", errors.New("TLS mode must be edge, passthrough, or reencrypt")
	}
	return name, hostname, target.String(), tlsMode, nil
}

func validateTunnelRouteHostname(hostname string) error {
	if hostname == "" || len(hostname) > 253 {
		return errors.New("a valid public hostname is required")
	}
	if net.ParseIP(hostname) != nil {
		return errors.New("public hostname must be a DNS name")
	}
	labels := strings.Split(hostname, ".")
	if len(labels) < 2 {
		return errors.New("public hostname must be fully qualified")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("public hostname is invalid")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return errors.New("public hostname is invalid")
			}
		}
	}
	return nil
}

func (s *Service) markTunnelHTTPRouteFailed(ctx context.Context, id int64, routeErr error) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE vps_tunnel_http_routes
		SET status = 'failed', last_error = $2, updated_at = NOW()
		WHERE id = $1
	`, id, routeErr.Error())
	return err
}

func (s *Service) upsertPrimaryTunnelHTTPRoute(ctx context.Context, attachment TunnelAttachment) error {
	if attachment.ID == 0 || strings.TrimSpace(attachment.PublicHostname) == "" {
		return nil
	}
	status := "pending"
	if attachment.HTTPRouteID != "" {
		status = "active"
	} else if attachment.Status == "failed" {
		status = "failed"
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO vps_tunnel_http_routes (
			tunnel_attachment_id, http_route_id, name, public_hostname, target_url, tls_mode,
			enabled, is_primary, cloudflare_dns_record_id, cloudflare_dns_record_type,
			cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
			status, last_error, managed_domain_id
		)
		VALUES ($1, $2, 'Primary web route', $3, $4, 'edge', TRUE, TRUE, $5, $6, $7, $8, $9, $10, $11, (SELECT managed_domain_id FROM vps_tunnel_attachments WHERE id=$1))
		ON CONFLICT (public_hostname) DO UPDATE SET
			http_route_id = EXCLUDED.http_route_id,
			target_url = EXCLUDED.target_url,
			cloudflare_dns_record_id = EXCLUDED.cloudflare_dns_record_id,
			cloudflare_dns_record_type = EXCLUDED.cloudflare_dns_record_type,
			cloudflare_dns_record_content = EXCLUDED.cloudflare_dns_record_content,
			cloudflare_dns_proxied = EXCLUDED.cloudflare_dns_proxied,
			cloudflare_dns_status = EXCLUDED.cloudflare_dns_status,
			status = EXCLUDED.status,
			last_error = EXCLUDED.last_error,
			managed_domain_id = EXCLUDED.managed_domain_id,
			updated_at = NOW()
		WHERE vps_tunnel_http_routes.tunnel_attachment_id = EXCLUDED.tunnel_attachment_id
	`, attachment.ID, attachment.HTTPRouteID, attachment.PublicHostname, attachment.HTTPTargetURL,
		attachment.CloudflareDNSRecordID, attachment.CloudflareDNSRecordType,
		attachment.CloudflareDNSRecordContent, attachment.CloudflareDNSProxied,
		attachment.CloudflareDNSStatus, status, attachment.LastError)
	if err != nil {
		return fmt.Errorf("save primary VPS tunnel HTTP route: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("public hostname %s is already assigned to another VPS tunnel", attachment.PublicHostname)
	}
	return nil
}

func (s *Service) managedDomainIDForHostname(ctx context.Context, hostname string) (int64, error) {
	if s.managedDomains == nil {
		return 0, nil
	}
	runtime, err := s.managedDomains.RuntimeForHostname(ctx, hostname)
	if err != nil {
		return 0, errors.New("public hostname is not within an active managed domain")
	}
	return runtime.ID, nil
}

func scanTunnelHTTPRoute(row interface{ Scan(...any) error }) (TunnelHTTPRoute, error) {
	var route TunnelHTTPRoute
	if err := row.Scan(tunnelHTTPRouteScanDest(&route)...); err != nil {
		return TunnelHTTPRoute{}, fmt.Errorf("scan VPS tunnel HTTP route: %w", err)
	}
	route.populate()
	return route, nil
}

func tunnelHTTPRouteScanDest(route *TunnelHTTPRoute) []any {
	return []any{
		&route.ID, &route.TunnelAttachmentID, &route.HTTPRouteID, &route.Name,
		&route.PublicHostname, &route.TargetURL, &route.TLSMode, &route.Enabled,
		&route.CloudflareDNSRecordID, &route.CloudflareDNSRecordType,
		&route.CloudflareDNSRecordContent, &route.CloudflareDNSProxied,
		&route.CloudflareDNSStatus, &route.Status, &route.LastError,
		&route.IsPrimary, &route.CreatedAt, &route.UpdatedAt,
	}
}

func (r *TunnelHTTPRoute) populate() {
	r.WebURL = "https://" + r.PublicHostname
}

func isDuplicateRouteHostnameError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "vps_tunnel_http_routes_public_hostname_key") ||
		strings.Contains(message, "tunnel_routes_hostname_key") ||
		(strings.Contains(message, "duplicate") && strings.Contains(message, "hostname"))
}
