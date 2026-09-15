package vps

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"proxacloud/backend/internal/manageddomains"
	"proxacloud/backend/internal/proxacloudtunnelapi"
	"proxacloud/backend/internal/tunnelinfra"
)

var ErrTunnelAttachmentNotFound = errors.New("vps tunnel attachment not found")

type tunnelAPI interface {
	Enabled() bool
	CreateTunnel(context.Context, proxacloudtunnelapi.CreateTunnelRequest) (proxacloudtunnelapi.Tunnel, error)
	CreateHTTPRoute(context.Context, string, proxacloudtunnelapi.CreateHTTPRouteRequest) (proxacloudtunnelapi.Route, error)
	ListHTTPRoutes(context.Context, string) ([]proxacloudtunnelapi.Route, error)
	UpdateHTTPRoute(context.Context, string, proxacloudtunnelapi.UpdateHTTPRouteRequest) (proxacloudtunnelapi.Route, error)
	CreateTCPRoute(context.Context, string, proxacloudtunnelapi.CreateTCPRouteRequest) (proxacloudtunnelapi.TCPRoute, error)
	CreateToken(context.Context, string, proxacloudtunnelapi.CreateTokenRequest) (proxacloudtunnelapi.TokenResponse, error)
	DeleteTunnel(context.Context, string) error
	DeleteHTTPRoute(context.Context, string) error
	DeleteTCPRoute(context.Context, string) error
	DeleteToken(context.Context, string) error
}

type tunnelMailer interface {
	Send(to, subject, body string) error
	Enabled() bool
}

type TunnelConfig struct {
	Enabled         bool
	APIURL          string
	GatewayURL      string
	PublicHostname  string
	HostnameSuffix  string
	HTTPTargetURL   string
	SSHHost         string
	SSHPort         int
	PublicPortStart int
	PublicPortEnd   int
	AgentBinaryURL  string
	CloudflareDNS   CloudflareDNSConfig
}

type CloudflareDNSConfig struct {
	Enabled    bool
	APIToken   string
	ZoneID     string
	Domain     string
	Target     string
	RecordType string
	Proxied    bool
}

type TunnelAttachment struct {
	ID                         int64      `json:"id"`
	VPSID                      int64      `json:"vps_id"`
	CustomerAccountID          int64      `json:"customer_account_id,omitempty"`
	TunnelID                   string     `json:"tunnel_id"`
	HTTPRouteID                string     `json:"http_route_id"`
	TCPRouteID                 string     `json:"tcp_route_id"`
	TokenID                    string     `json:"token_id"`
	PublicHostname             string     `json:"public_hostname"`
	PublicPort                 int        `json:"public_port"`
	HTTPTargetURL              string     `json:"http_target_url"`
	TCPTargetHost              string     `json:"tcp_target_host"`
	TCPTargetPort              int        `json:"tcp_target_port"`
	SSHUser                    string     `json:"ssh_user"`
	InstallCommandRedacted     string     `json:"install_command_redacted,omitempty"`
	CloudflareDNSRecordID      string     `json:"cloudflare_dns_record_id,omitempty"`
	CloudflareDNSRecordType    string     `json:"cloudflare_dns_record_type,omitempty"`
	CloudflareDNSRecordContent string     `json:"cloudflare_dns_record_content,omitempty"`
	CloudflareDNSProxied       bool       `json:"cloudflare_dns_proxied"`
	CloudflareDNSStatus        string     `json:"cloudflare_dns_status,omitempty"`
	Status                     string     `json:"status"`
	LastError                  string     `json:"last_error"`
	EmailedAt                  *time.Time `json:"emailed_at,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
	SSHCommand                 string     `json:"ssh_command"`
	WebURL                     string     `json:"web_url"`
}

func (s *Service) SetTunnelIntegration(cfg TunnelConfig, client tunnelAPI, mailer tunnelMailer) {
	cfg.APIURL = strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/")
	cfg.GatewayURL = strings.TrimSpace(cfg.GatewayURL)
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = gatewayURLFromAPIURL(cfg.APIURL)
	}
	cfg.PublicHostname = strings.TrimSpace(cfg.PublicHostname)
	cfg.HostnameSuffix = strings.TrimSpace(cfg.HostnameSuffix)
	cfg.HTTPTargetURL = strings.TrimSpace(cfg.HTTPTargetURL)
	if cfg.HTTPTargetURL == "" {
		cfg.HTTPTargetURL = "http://127.0.0.1:80"
	}
	cfg.SSHHost = strings.TrimSpace(cfg.SSHHost)
	if cfg.SSHHost == "" {
		cfg.SSHHost = "127.0.0.1"
	}
	if cfg.SSHPort <= 0 || cfg.SSHPort > 65535 {
		cfg.SSHPort = 22
	}
	if cfg.PublicPortStart <= 0 {
		cfg.PublicPortStart = 22000
	}
	if cfg.PublicPortEnd < cfg.PublicPortStart || cfg.PublicPortEnd > 65535 {
		cfg.PublicPortEnd = 29999
	}
	cfg.AgentBinaryURL = strings.TrimSpace(cfg.AgentBinaryURL)
	cfg.CloudflareDNS.APIToken = strings.TrimSpace(cfg.CloudflareDNS.APIToken)
	cfg.CloudflareDNS.ZoneID = strings.TrimSpace(cfg.CloudflareDNS.ZoneID)
	cfg.CloudflareDNS.Domain = strings.Trim(strings.ToLower(strings.TrimSpace(cfg.CloudflareDNS.Domain)), ".")
	if cfg.CloudflareDNS.Domain == "" {
		cfg.CloudflareDNS.Domain = "proxaops.com"
	}
	cfg.CloudflareDNS.Target = strings.TrimSpace(cfg.CloudflareDNS.Target)
	cfg.CloudflareDNS.RecordType = strings.ToUpper(strings.TrimSpace(cfg.CloudflareDNS.RecordType))
	if cfg.CloudflareDNS.RecordType == "" {
		cfg.CloudflareDNS.RecordType = cloudflareRecordTypeForTarget(cfg.CloudflareDNS.Target)
	}

	s.tunnelConfig = cfg
	s.tunnelClient = client
	s.mailer = mailer
	s.tunnelInfra = tunnelinfra.New(tunnelinfra.Config{
		Enabled: cfg.Enabled, APIURL: cfg.APIURL, GatewayURL: cfg.GatewayURL,
		HostnameSuffix: cfg.HostnameSuffix, AgentBinaryURL: cfg.AgentBinaryURL,
		Cloudflare: tunnelinfra.CloudflareConfig{
			Enabled: cfg.CloudflareDNS.Enabled, APIToken: cfg.CloudflareDNS.APIToken,
			ZoneID: cfg.CloudflareDNS.ZoneID, Domain: cfg.CloudflareDNS.Domain,
			Target: cfg.CloudflareDNS.Target, RecordType: cfg.CloudflareDNS.RecordType,
			Proxied: cfg.CloudflareDNS.Proxied,
		},
	}, client)
}

func (s *Service) SetManagedDomains(service *manageddomains.Service) { s.managedDomains = service }

func (s *Service) TunnelInfrastructure() *tunnelinfra.Service { return s.tunnelInfra }

func (s *Service) configureTunnelForCreate(ctx context.Context, vpsID int64, req CreateRequest, sshUser string) (string, *TunnelAttachment, error) {
	if !s.tunnelConfigured() {
		return "", nil, errors.New("proxacloud tunnel integration is enabled but API URL/token are not configured")
	}
	attachment, err := s.reserveTunnelAttachment(ctx, vpsID, req, sshUser)
	if err != nil {
		return "", nil, err
	}
	installCommand, updated, err := s.provisionTunnelAttachment(ctx, attachment, req.Name)
	if err != nil {
		_ = s.markTunnelAttachmentFailed(ctx, attachment.ID, err)
		return "", nil, err
	}
	return installCommand, &updated, nil
}

func (s *Service) RetryTunnel(id int64) (TunnelAttachment, error) {
	rec, err := s.Get(id)
	if err != nil {
		return TunnelAttachment{}, err
	}
	if !s.tunnelConfigured() {
		return TunnelAttachment{}, errors.New("proxacloud tunnel integration is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	attachment, err := s.GetTunnelAttachment(id)
	if errors.Is(err, ErrTunnelAttachmentNotFound) {
		attachment, err = s.reserveTunnelAttachment(ctx, id, CreateRequest{
			Name:              rec.Name,
			CustomerAccountID: rec.CustomerAccountID,
		}, rec.SSHUser)
	}
	if err != nil {
		return TunnelAttachment{}, err
	}
	if (attachment.Status == "configured" || attachment.Status == "emailed") &&
		attachment.TunnelID != "" && attachment.HTTPRouteID != "" && attachment.TCPRouteID != "" {
		updated, syncErr := s.syncCloudflareDNSForAttachment(ctx, attachment)
		if syncErr != nil {
			_ = s.insertEvent(id, "cloudflare-dns", "error", "Cloudflare DNS sync failed", syncErr.Error())
			return TunnelAttachment{}, syncErr
		}
		if syncErr := s.syncAllTunnelHTTPRoutes(ctx, updated); syncErr != nil {
			_ = s.insertEvent(id, "tunnel-http-routes", "error", "HTTP route synchronization failed", syncErr.Error())
			return TunnelAttachment{}, syncErr
		}
		_ = s.insertEvent(id, "cloudflare-dns", "success", "Cloudflare DNS synced", updated.CloudflareDNSSummary())
		return updated, nil
	}
	_, updated, err := s.provisionTunnelAttachment(ctx, attachment, rec.Name)
	if err != nil {
		_ = s.markTunnelAttachmentFailed(ctx, attachment.ID, err)
		return TunnelAttachment{}, err
	}
	if err := s.syncAllTunnelHTTPRoutes(ctx, updated); err != nil {
		_ = s.insertEvent(id, "tunnel-http-routes", "error", "HTTP route synchronization failed", err.Error())
		return TunnelAttachment{}, err
	}
	_ = s.insertEvent(id, "tunnel-retry", "success", "Proxacloud tunnel routes configured", updated.AccessSummary())
	return updated, nil
}

func (s *Service) syncCloudflareDNSForAttachment(ctx context.Context, attachment TunnelAttachment) (TunnelAttachment, error) {
	dnsResult, err := s.ensureCloudflareDNSRecord(ctx, attachment)
	if err != nil {
		return TunnelAttachment{}, err
	}
	return s.updateTunnelDNSMetadata(ctx, attachment.ID, dnsResult)
}

func (s *Service) GetTunnelAttachment(vpsID int64) (TunnelAttachment, error) {
	row := s.db.QueryRow(`
		SELECT id, vps_id, COALESCE(customer_account_id, 0), tunnel_id, http_route_id, tcp_route_id, token_id,
		       public_hostname, public_port, http_target_url, tcp_target_host, tcp_target_port, ssh_user,
		       install_command_redacted, cloudflare_dns_record_id, cloudflare_dns_record_type,
		       cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		       status, last_error, emailed_at, created_at, updated_at
		FROM vps_tunnel_attachments
		WHERE vps_id = $1
	`, vpsID)
	return scanTunnelAttachment(row)
}

func (s *Service) tunnelConfigured() bool {
	return s.tunnelConfig.Enabled && s.tunnelClient != nil && s.tunnelClient.Enabled()
}

func (s *Service) reserveTunnelAttachment(ctx context.Context, vpsID int64, req CreateRequest, sshUser string) (TunnelAttachment, error) {
	hostname, domainID, hostnameLabel, err := s.hostnameForVPS(ctx, req)
	if err != nil {
		return TunnelAttachment{}, err
	}
	port, err := s.allocateTunnelPort(ctx)
	if err != nil {
		return TunnelAttachment{}, err
	}

	var out TunnelAttachment
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO vps_tunnel_attachments (
			vps_id, customer_account_id, public_hostname, public_port, http_target_url,
			tcp_target_host, tcp_target_port, ssh_user, status, managed_domain_id, hostname_label
		)
		VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8, 'pending', NULLIF($9, 0), $10)
		ON CONFLICT (vps_id) DO UPDATE SET
			customer_account_id = EXCLUDED.customer_account_id,
			public_hostname = EXCLUDED.public_hostname,
			public_port = EXCLUDED.public_port,
			http_target_url = EXCLUDED.http_target_url,
			tcp_target_host = EXCLUDED.tcp_target_host,
			tcp_target_port = EXCLUDED.tcp_target_port,
			ssh_user = EXCLUDED.ssh_user,
			managed_domain_id = EXCLUDED.managed_domain_id,
			hostname_label = EXCLUDED.hostname_label,
			status = 'pending',
			last_error = '',
			updated_at = NOW()
		RETURNING id, vps_id, COALESCE(customer_account_id, 0), tunnel_id, http_route_id, tcp_route_id, token_id,
		          public_hostname, public_port, http_target_url, tcp_target_host, tcp_target_port, ssh_user,
		          install_command_redacted, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, emailed_at, created_at, updated_at
	`, vpsID, req.CustomerAccountID, hostname, port, s.tunnelConfig.HTTPTargetURL, s.tunnelConfig.SSHHost, s.tunnelConfig.SSHPort, sshUser, domainID, hostnameLabel).Scan(
		&out.ID, &out.VPSID, &out.CustomerAccountID, &out.TunnelID, &out.HTTPRouteID, &out.TCPRouteID, &out.TokenID,
		&out.PublicHostname, &out.PublicPort, &out.HTTPTargetURL, &out.TCPTargetHost, &out.TCPTargetPort, &out.SSHUser,
		&out.InstallCommandRedacted, &out.CloudflareDNSRecordID, &out.CloudflareDNSRecordType,
		&out.CloudflareDNSRecordContent, &out.CloudflareDNSProxied, &out.CloudflareDNSStatus,
		&out.Status, &out.LastError, &out.EmailedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return TunnelAttachment{}, fmt.Errorf("reserve vps tunnel attachment: %w", err)
	}
	out.populateAccessFields()
	return out, nil
}

func (s *Service) provisionTunnelAttachment(ctx context.Context, attachment TunnelAttachment, vpsName string) (string, TunnelAttachment, error) {
	infra, err := s.tunnelInfrastructureForHostname(ctx, attachment.PublicHostname)
	if err != nil {
		return "", TunnelAttachment{}, err
	}
	if infra == nil {
		return "", TunnelAttachment{}, errors.New("shared tunnel infrastructure service is not configured")
	}
	ports, err := s.candidateTunnelPorts(ctx, attachment.PublicPort)
	if err != nil {
		return "", TunnelAttachment{}, err
	}
	provisioned, err := infra.Provision(ctx, tunnelinfra.ProvisionRequest{
		Name: fmt.Sprintf("VPS %s", vpsName), Slug: "vps-" + sanitizeName(vpsName),
		Description: fmt.Sprintf("Managed VPS tunnel for %s", vpsName),
		Hostname:    attachment.PublicHostname, PublicPorts: ports,
		TCPTargetHost: attachment.TCPTargetHost, TCPTargetPort: attachment.TCPTargetPort,
		CreateHTTPRoute: true, HTTPTargetURL: attachment.HTTPTargetURL,
		AgentTargetURL: attachment.HTTPTargetURL, TokenName: "VPS agent token",
	})
	if err != nil {
		return "", TunnelAttachment{}, err
	}
	attachment.PublicPort = provisioned.PublicPort
	installCommand := provisioned.InstallCommand
	redactedCommand := installCommand
	if marker := strings.Index(redactedCommand, "--token"); marker >= 0 {
		redactedCommand = redactedCommand[:marker] + "--token 'REDACTED'"
	}
	var out TunnelAttachment
	err = s.db.QueryRowContext(ctx, `
		UPDATE vps_tunnel_attachments
		SET tunnel_id = $2,
		    http_route_id = $3,
		    tcp_route_id = $4,
		    token_id = $5,
		    install_command_redacted = $6,
		    cloudflare_dns_record_id = $7,
		    cloudflare_dns_record_type = $8,
		    cloudflare_dns_record_content = $9,
		    cloudflare_dns_proxied = $10,
		    cloudflare_dns_status = $11,
		    public_port = $12,
		    status = 'configured',
		    last_error = '',
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, vps_id, COALESCE(customer_account_id, 0), tunnel_id, http_route_id, tcp_route_id, token_id,
		          public_hostname, public_port, http_target_url, tcp_target_host, tcp_target_port, ssh_user,
		          install_command_redacted, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, emailed_at, created_at, updated_at
	`, attachment.ID, provisioned.TunnelID, provisioned.HTTPRouteID, provisioned.TCPRouteID, provisioned.TokenID, redactedCommand,
		provisioned.DNS.RecordID, provisioned.DNS.RecordType, provisioned.DNS.Content, provisioned.DNS.Proxied, provisioned.DNS.Status, attachment.PublicPort).Scan(
		&out.ID, &out.VPSID, &out.CustomerAccountID, &out.TunnelID, &out.HTTPRouteID, &out.TCPRouteID, &out.TokenID,
		&out.PublicHostname, &out.PublicPort, &out.HTTPTargetURL, &out.TCPTargetHost, &out.TCPTargetPort, &out.SSHUser,
		&out.InstallCommandRedacted, &out.CloudflareDNSRecordID, &out.CloudflareDNSRecordType,
		&out.CloudflareDNSRecordContent, &out.CloudflareDNSProxied, &out.CloudflareDNSStatus,
		&out.Status, &out.LastError, &out.EmailedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return "", TunnelAttachment{}, fmt.Errorf("update vps tunnel attachment: %w", err)
	}
	out.populateAccessFields()
	if err := s.upsertPrimaryTunnelHTTPRoute(ctx, out); err != nil {
		return "", TunnelAttachment{}, err
	}
	return installCommand, out, nil
}

func (s *Service) cleanupTunnelAttachment(ctx context.Context, attachment TunnelAttachment) error {
	if attachment.ID == 0 {
		return nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT cloudflare_dns_record_id, public_hostname
		FROM vps_tunnel_http_routes
		WHERE tunnel_attachment_id = $1 AND cloudflare_dns_record_id <> ''
	`, attachment.ID)
	if err != nil {
		return fmt.Errorf("list HTTP route DNS records: %w", err)
	}
	type dnsRecordRef struct{ id, hostname string }
	routeDNSRecords := make([]dnsRecordRef, 0)
	for rows.Next() {
		var record dnsRecordRef
		if err := rows.Scan(&record.id, &record.hostname); err != nil {
			rows.Close()
			return fmt.Errorf("scan HTTP route DNS record: %w", err)
		}
		routeDNSRecords = append(routeDNSRecords, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list HTTP route DNS records: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close HTTP route DNS records: %w", err)
	}
	for _, record := range routeDNSRecords {
		if record.id == attachment.CloudflareDNSRecordID {
			continue
		}
		if err := s.deleteCloudflareDNSRecord(ctx, record.id, record.hostname); err != nil {
			return fmt.Errorf("delete HTTP route Cloudflare DNS record: %w", err)
		}
	}

	if strings.TrimSpace(attachment.CloudflareDNSRecordID) != "" {
		if err := s.deleteCloudflareDNSRecord(ctx, attachment.CloudflareDNSRecordID, attachment.PublicHostname); err != nil {
			return fmt.Errorf("delete Cloudflare DNS record: %w", err)
		}
		_ = s.insertEvent(attachment.VPSID, "tunnel-cleanup", "success", "Cloudflare DNS record deleted", attachment.CloudflareDNSSummary())
	}

	hasTunnelResources := strings.TrimSpace(attachment.TokenID) != "" ||
		strings.TrimSpace(attachment.HTTPRouteID) != "" ||
		strings.TrimSpace(attachment.TCPRouteID) != "" ||
		strings.TrimSpace(attachment.TunnelID) != ""
	if !hasTunnelResources {
		return nil
	}
	if s.tunnelClient == nil || !s.tunnelClient.Enabled() {
		return errors.New("proxacloud tunnel integration is not configured")
	}

	if strings.TrimSpace(attachment.TunnelID) != "" {
		if err := s.tunnelClient.DeleteTunnel(ctx, attachment.TunnelID); err != nil {
			return fmt.Errorf("delete proxacloud tunnel: %w", err)
		}
		_ = s.insertEvent(attachment.VPSID, "tunnel-cleanup", "success", "Proxacloud tunnel deleted", attachment.TunnelID)
		return nil
	}

	if strings.TrimSpace(attachment.TokenID) != "" {
		if err := s.tunnelClient.DeleteToken(ctx, attachment.TokenID); err != nil {
			return fmt.Errorf("delete proxacloud tunnel token: %w", err)
		}
		_ = s.insertEvent(attachment.VPSID, "tunnel-cleanup", "success", "Proxacloud tunnel token deleted", attachment.TokenID)
	}
	if strings.TrimSpace(attachment.HTTPRouteID) != "" {
		if err := s.tunnelClient.DeleteHTTPRoute(ctx, attachment.HTTPRouteID); err != nil {
			return fmt.Errorf("delete proxacloud HTTP route: %w", err)
		}
		_ = s.insertEvent(attachment.VPSID, "tunnel-cleanup", "success", "Proxacloud HTTP route deleted", attachment.PublicHostname)
	}
	if strings.TrimSpace(attachment.TCPRouteID) != "" {
		if err := s.tunnelClient.DeleteTCPRoute(ctx, attachment.TCPRouteID); err != nil {
			return fmt.Errorf("delete proxacloud TCP route: %w", err)
		}
		_ = s.insertEvent(attachment.VPSID, "tunnel-cleanup", "success", "Proxacloud TCP route deleted", attachment.SSHCommand)
	}
	return nil
}

func (s *Service) EmailTunnelDetails(vpsID int64) error {
	if s.mailer == nil {
		return errors.New("email delivery is not configured")
	}
	attachment, err := s.GetTunnelAttachment(vpsID)
	if err != nil {
		if errors.Is(err, ErrTunnelAttachmentNotFound) {
			return nil
		}
		return err
	}
	if attachment.Status != "configured" && attachment.Status != "emailed" {
		return nil
	}
	email, name, err := s.ownerEmail(attachment.CustomerAccountID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(email) == "" {
		return nil
	}

	subject := "Your ProxaCloud VPS access details"

	body := fmt.Sprintf("Hello %s,\n\nYour ProxaCloud VPS tunnel access is configured.\n\nHostname: %s\nSSH Port: %d\n\nSign in to your dashboard to view access details. Passwords are not sent by email.\n",
		name,
		attachment.PublicHostname,
		attachment.PublicPort,
	)
	if err := s.mailer.Send(email, subject, body); err != nil {
		return fmt.Errorf("send vps tunnel access email: %w", err)
	}
	_ = s.insertEvent(vpsID, "tunnel-email", "info", "VPS access email queued", "")
	return nil
}

func (s *Service) ownerEmail(accountID int64) (string, string, error) {
	if accountID <= 0 {
		return "", "", nil
	}
	var email, firstName, lastName, username string
	err := s.db.QueryRow(`
		SELECT u.email, COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), u.username
		FROM customer_accounts ca
		JOIN users u ON u.id = ca.owner_user_id
		WHERE ca.id = $1
	`, accountID).Scan(&email, &firstName, &lastName, &username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load customer owner email: %w", err)
	}
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		name = username
	}
	return email, name, nil
}

func (s *Service) createTCPRouteWithPortRetry(ctx context.Context, tunnelID string, attachment TunnelAttachment) (proxacloudtunnelapi.TCPRoute, int, error) {
	ports, err := s.candidateTunnelPorts(ctx, attachment.PublicPort)
	if err != nil {
		return proxacloudtunnelapi.TCPRoute{}, 0, err
	}
	var lastDuplicate error
	for _, port := range ports {
		route, err := s.tunnelClient.CreateTCPRoute(ctx, tunnelID, proxacloudtunnelapi.CreateTCPRouteRequest{
			Name:           "SSH",
			PublicHostname: attachment.PublicHostname,
			PublicPort:     port,
			TargetHost:     attachment.TCPTargetHost,
			TargetPort:     attachment.TCPTargetPort,
			Enabled:        true,
		})
		if err == nil {
			return route, port, nil
		}
		if !isDuplicateTCPPublicPortError(err) {
			return proxacloudtunnelapi.TCPRoute{}, 0, err
		}
		lastDuplicate = err
	}
	if lastDuplicate != nil {
		return proxacloudtunnelapi.TCPRoute{}, 0, fmt.Errorf("no available proxacloud tunnel TCP ports between %d and %d: %w", s.tunnelConfig.PublicPortStart, s.tunnelConfig.PublicPortEnd, lastDuplicate)
	}
	return proxacloudtunnelapi.TCPRoute{}, 0, fmt.Errorf("no available proxacloud tunnel TCP ports between %d and %d", s.tunnelConfig.PublicPortStart, s.tunnelConfig.PublicPortEnd)
}

func isDuplicateTCPPublicPortError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tcp_routes_public_port_key") ||
		(strings.Contains(msg, "duplicate key value") && strings.Contains(msg, "public_port"))
}

func (s *Service) allocateTunnelPort(ctx context.Context) (int, error) {
	ports, err := s.candidateTunnelPorts(ctx, 0)
	if err != nil {
		return 0, err
	}
	if len(ports) == 0 {
		return 0, fmt.Errorf("no available proxacloud tunnel TCP ports between %d and %d", s.tunnelConfig.PublicPortStart, s.tunnelConfig.PublicPortEnd)
	}
	return ports[0], nil
}

func (s *Service) candidateTunnelPorts(ctx context.Context, preferred int) ([]int, error) {
	if s.tunnelConfig.PublicPortEnd < s.tunnelConfig.PublicPortStart {
		return nil, fmt.Errorf("invalid proxacloud tunnel TCP port range %d-%d", s.tunnelConfig.PublicPortStart, s.tunnelConfig.PublicPortEnd)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT public_port
		FROM vps_tunnel_attachments
		WHERE public_port BETWEEN $1 AND $2
	`, s.tunnelConfig.PublicPortStart, s.tunnelConfig.PublicPortEnd)
	if err != nil {
		return nil, fmt.Errorf("list allocated tunnel ports: %w", err)
	}
	defer rows.Close()

	used := map[int]struct{}{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return nil, err
		}
		used[port] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ports := make([]int, 0, s.tunnelConfig.PublicPortEnd-s.tunnelConfig.PublicPortStart+1)
	if preferred >= s.tunnelConfig.PublicPortStart && preferred <= s.tunnelConfig.PublicPortEnd {
		ports = append(ports, preferred)
	}
	for port := s.tunnelConfig.PublicPortStart; port <= s.tunnelConfig.PublicPortEnd; port++ {
		if port == preferred {
			continue
		}
		if _, exists := used[port]; !exists {
			ports = append(ports, port)
		}
	}
	return ports, nil
}

func (s *Service) hostnameForVPS(ctx context.Context, req CreateRequest) (string, int64, string, error) {
	base := sanitizeName(req.TunnelHostnameLabel)
	if base == "" {
		base = sanitizeName(req.Name)
	}
	if base == "" {
		base = "vps"
	}
	if s.managedDomains != nil {
		runtime, err := s.managedDomains.RuntimeByID(ctx, req.TunnelDomainID)
		if err != nil {
			return "", 0, "", err
		}
		hostname, err := manageddomains.BuildHostname(base, runtime)
		return hostname, runtime.ID, base, err
	}
	if s.tunnelInfra != nil {
		hostname, err := s.tunnelInfra.Hostname(base)
		return hostname, 0, base, err
	}
	if suffix := strings.TrimSpace(s.tunnelConfig.HostnameSuffix); suffix != "" {
		suffix = strings.TrimPrefix(suffix, ".")
		return base + "." + suffix, 0, base, nil
	}
	if host := strings.TrimSpace(s.tunnelConfig.PublicHostname); host != "" {
		return strings.ToLower(host), 0, base, nil
	}
	return "", 0, "", errors.New("select a managed tunnel domain or configure a legacy hostname suffix")
}

func (s *Service) tunnelInfrastructureForHostname(ctx context.Context, hostname string) (*tunnelinfra.Service, error) {
	if s.managedDomains == nil {
		return s.tunnelInfra, nil
	}
	runtime, err := s.managedDomains.RuntimeForHostname(ctx, hostname)
	if err != nil {
		return nil, err
	}
	cfg := tunnelinfra.Config{Enabled: true, APIURL: s.tunnelConfig.APIURL, GatewayURL: s.tunnelConfig.GatewayURL, HostnameSuffix: runtime.BaseDomain, AgentBinaryURL: s.tunnelConfig.AgentBinaryURL,
		Cloudflare: tunnelinfra.CloudflareConfig{Enabled: true, APIToken: runtime.CloudflareAPIToken, ZoneID: runtime.CloudflareZoneID, Domain: runtime.BaseDomain, Target: runtime.DNSTarget, RecordType: runtime.DNSRecordType, Proxied: runtime.DNSProxied}}
	return tunnelinfra.New(cfg, s.tunnelClient), nil
}

func (s *Service) agentInstallCommand(tunnelID, token string) string {
	apiURL := strings.TrimRight(s.tunnelConfig.APIURL, "/")
	binaryURL := strings.TrimSpace(s.tunnelConfig.AgentBinaryURL)
	if binaryURL == "" && apiURL != "" {
		binaryURL = apiURL + "/api/v1/downloads/proxacloud-agent-linux-amd64"
	}
	args := []string{
		"curl", "-fsSL", apiURL + "/api/v1/install-agent.sh",
		"|", "sudo", "sh", "-s", "--",
		"--tunnel-id", tunnelID,
		"--token", token,
		"--gateway-url", s.tunnelConfig.GatewayURL,
		"--target-url", s.tunnelConfig.HTTPTargetURL,
	}
	if binaryURL != "" {
		args = append(args, "--binary-url", binaryURL)
	}
	for i, arg := range args {
		if arg == "|" {
			continue
		}
		args[i] = shellQuote(arg)
	}
	return strings.Join(args, " ")
}

func (s *Service) markTunnelAttachmentFailed(ctx context.Context, id int64, err error) error {
	_, execErr := s.db.ExecContext(ctx, `
		UPDATE vps_tunnel_attachments
		SET status = 'failed', last_error = $2, updated_at = NOW()
		WHERE id = $1
	`, id, err.Error())
	return execErr
}

func (s *Service) updateTunnelDNSMetadata(ctx context.Context, id int64, dnsResult cloudflareDNSResult) (TunnelAttachment, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE vps_tunnel_attachments
		SET cloudflare_dns_record_id = $2,
		    cloudflare_dns_record_type = $3,
		    cloudflare_dns_record_content = $4,
		    cloudflare_dns_proxied = $5,
		    cloudflare_dns_status = $6,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, vps_id, COALESCE(customer_account_id, 0), tunnel_id, http_route_id, tcp_route_id, token_id,
		          public_hostname, public_port, http_target_url, tcp_target_host, tcp_target_port, ssh_user,
		          install_command_redacted, cloudflare_dns_record_id, cloudflare_dns_record_type,
		          cloudflare_dns_record_content, cloudflare_dns_proxied, cloudflare_dns_status,
		          status, last_error, emailed_at, created_at, updated_at
	`, id, dnsResult.RecordID, dnsResult.RecordType, dnsResult.Content, dnsResult.Proxied, dnsResult.Status)
	attachment, err := scanTunnelAttachment(row)
	if err != nil {
		return TunnelAttachment{}, err
	}
	if err := s.upsertPrimaryTunnelHTTPRoute(ctx, attachment); err != nil {
		return TunnelAttachment{}, err
	}
	return attachment, nil
}

func scanTunnelAttachment(row interface {
	Scan(dest ...any) error
}) (TunnelAttachment, error) {
	var out TunnelAttachment
	var emailedAt sql.NullTime
	err := row.Scan(
		&out.ID, &out.VPSID, &out.CustomerAccountID, &out.TunnelID, &out.HTTPRouteID, &out.TCPRouteID, &out.TokenID,
		&out.PublicHostname, &out.PublicPort, &out.HTTPTargetURL, &out.TCPTargetHost, &out.TCPTargetPort, &out.SSHUser,
		&out.InstallCommandRedacted, &out.CloudflareDNSRecordID, &out.CloudflareDNSRecordType,
		&out.CloudflareDNSRecordContent, &out.CloudflareDNSProxied, &out.CloudflareDNSStatus,
		&out.Status, &out.LastError, &emailedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TunnelAttachment{}, ErrTunnelAttachmentNotFound
	}
	if err != nil {
		return TunnelAttachment{}, fmt.Errorf("scan vps tunnel attachment: %w", err)
	}
	if emailedAt.Valid {
		out.EmailedAt = &emailedAt.Time
	}
	out.populateAccessFields()
	return out, nil
}

func (a *TunnelAttachment) populateAccessFields() {
	a.WebURL = "https://" + a.PublicHostname
	if a.SSHUser != "" && a.PublicHostname != "" && a.PublicPort > 0 {
		a.SSHCommand = fmt.Sprintf("ssh %s@%s -p %d", a.SSHUser, a.PublicHostname, a.PublicPort)
	}
}

func (a TunnelAttachment) AccessSummary() string {
	return fmt.Sprintf("Web URL: %s\nSSH: %s", a.WebURL, a.SSHCommand)
}

func (a TunnelAttachment) CloudflareDNSSummary() string {
	if a.CloudflareDNSStatus == "" || a.CloudflareDNSStatus == "disabled" {
		return "Cloudflare DNS disabled"
	}
	return fmt.Sprintf("%s %s -> %s (proxied=%t)", a.CloudflareDNSRecordType, a.PublicHostname, a.CloudflareDNSRecordContent, a.CloudflareDNSProxied)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func gatewayURLFromAPIURL(apiURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}
	parsed.Path = "/__agent/connect"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
