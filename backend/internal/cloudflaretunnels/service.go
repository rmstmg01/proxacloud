package cloudflaretunnels

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"k8s-proxmox-manager/backend/internal/secrets"
)

const cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"

type Service struct {
	db         *sql.DB
	secrets    secrets.Store
	applier    ManifestApplier
	cfClient   *http.Client
	apiBaseURL string
}

type pendingEvent struct {
	step    string
	level   string
	message string
	output  string
}

func NewService(db *sql.DB, secretStore secrets.Store, applier ManifestApplier) *Service {
	return &Service{
		db:         db,
		secrets:    secretStore,
		applier:    applier,
		cfClient:   &http.Client{Timeout: 30 * time.Second},
		apiBaseURL: cloudflareAPIBaseURL,
	}
}

func (s *Service) List() ([]CloudflareTunnel, error) {
	rows, err := s.db.Query(
		`SELECT t.id, t.cluster_id, COALESCE(c.name, ''), t.name, t.namespace, t.deployment_name,
		        t.routing_mode, t.provisioning_mode, t.cloudflare_account_id, t.cloudflare_zone_id,
		        t.cloudflare_tunnel_id, t.cloudflare_tunnel_name, t.token_secret_ref, t.api_token_secret_ref,
		        t.replicas, t.image, t.ingress_namespace, t.ingress_service_name,
		        t.ingress_service_port, t.ingress_service_scheme, t.configure_dns, t.status,
		        t.last_manifest, t.last_apply_output, t.last_applied_at, t.created_at, t.updated_at
		   FROM cloudflare_tunnels t
		   LEFT JOIN clusters c ON c.id = t.cluster_id
		  ORDER BY t.created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloudflare tunnels: %w", err)
	}
	defer rows.Close()

	out := make([]CloudflareTunnel, 0)
	for rows.Next() {
		rec, scanErr := scanTunnel(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		routes, routeErr := s.listRoutes(rec.ID)
		if routeErr != nil {
			return nil, routeErr
		}
		rec.Routes = routes
		out = append(out, rec.CloudflareTunnel)
	}
	return out, rows.Err()
}

func (s *Service) Get(id int64) (CloudflareTunnel, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return CloudflareTunnel{}, err
	}
	routes, err := s.listRoutes(id)
	if err != nil {
		return CloudflareTunnel{}, err
	}
	rec.Routes = routes
	return rec.CloudflareTunnel, nil
}

func (s *Service) Create(req CreateTunnelRequest) (CloudflareTunnel, error) {
	normalized, err := normalizeCreateRequest(req)
	if err != nil {
		return CloudflareTunnel{}, err
	}
	if err := s.ensureDeploymentSlotAvailable(normalized.ClusterID, normalized.Namespace, normalized.DeploymentName); err != nil {
		return CloudflareTunnel{}, err
	}
	if s.secrets == nil {
		return CloudflareTunnel{}, errors.New("secret store is not configured")
	}

	ctx := context.Background()
	events := []pendingEvent{
		{step: "validate", level: "info", message: "Cloudflare tunnel request validated"},
	}

	tunnelToken := strings.TrimSpace(normalized.TunnelToken)
	apiToken := strings.TrimSpace(normalized.CloudflareAPIToken)
	if normalized.ProvisioningMode == ProvisioningModeCloudflareAPI {
		if apiToken == "" {
			return CloudflareTunnel{}, errors.New("cloudflare_api_token is required for Cloudflare API mode")
		}
		cfTunnelName := normalized.CloudflareTunnelName
		if cfTunnelName == "" {
			cfTunnelName = normalized.Name
		}
		if normalized.CloudflareTunnelID == "" {
			cfTunnel, createErr := s.createCloudflareTunnel(ctx, apiToken, normalized.CloudflareAccountID, cfTunnelName)
			if createErr != nil {
				return CloudflareTunnel{}, createErr
			}
			normalized.CloudflareTunnelID = cfTunnel.ID
			normalized.CloudflareTunnelName = cfTunnel.Name
			events = append(events, pendingEvent{step: "cloudflare-api", level: "success", message: "Cloudflare Tunnel created", output: cfTunnel.ID})
		}
		fetchedToken, tokenErr := s.getCloudflareTunnelToken(ctx, apiToken, normalized.CloudflareAccountID, normalized.CloudflareTunnelID)
		if tokenErr != nil {
			return CloudflareTunnel{}, tokenErr
		}
		tunnelToken = fetchedToken
		events = append(events, pendingEvent{step: "cloudflare-api", level: "success", message: "Cloudflare Tunnel token fetched"})

		if configErr := s.putCloudflareTunnelConfig(ctx, apiToken, normalized); configErr != nil {
			return CloudflareTunnel{}, configErr
		}
		events = append(events, pendingEvent{step: "cloudflare-api", level: "success", message: "Cloudflare ingress routes configured"})

		if normalized.ConfigureDNS {
			dnsOutput, dnsErr := s.upsertCloudflareDNSRecords(ctx, apiToken, normalized)
			if dnsErr != nil {
				events = append(events, pendingEvent{step: "cloudflare-dns", level: "warning", message: "DNS record sync failed", output: dnsErr.Error()})
			} else {
				events = append(events, pendingEvent{step: "cloudflare-dns", level: "success", message: "DNS records synced", output: dnsOutput})
			}
		}
	}
	if tunnelToken == "" {
		return CloudflareTunnel{}, errors.New("tunnel_token is required")
	}

	tokenRef, err := s.secrets.Put(ctx, "cloudflare-tunnels", normalized.Name+"-tunnel-token", []byte(tunnelToken))
	if err != nil {
		return CloudflareTunnel{}, fmt.Errorf("store cloudflare tunnel token: %w", err)
	}
	apiTokenRef := ""
	if apiToken != "" {
		apiTokenRef, err = s.secrets.Put(ctx, "cloudflare-tunnels", normalized.Name+"-api-token", []byte(apiToken))
		if err != nil {
			_ = s.secrets.Delete(ctx, tokenRef)
			return CloudflareTunnel{}, fmt.Errorf("store cloudflare api token: %w", err)
		}
	}

	redactedManifest := buildCloudflaredManifest(normalized.Namespace, normalized.DeploymentName, normalized.Image, normalized.Replicas, "REDACTED")
	tunnelID, err := s.insertTunnel(normalized, tokenRef, apiTokenRef, redactedManifest)
	if err != nil {
		_ = s.secrets.Delete(ctx, tokenRef)
		if apiTokenRef != "" {
			_ = s.secrets.Delete(ctx, apiTokenRef)
		}
		return CloudflareTunnel{}, err
	}
	for _, event := range events {
		_ = s.appendEvent(tunnelID, event.step, event.level, event.message, event.output)
	}

	if normalized.Apply {
		if _, deployErr := s.Deploy(tunnelID); deployErr != nil {
			_ = s.appendEvent(tunnelID, "deploy", "error", "Cloudflare tunnel deployment failed", deployErr.Error())
		}
	}

	return s.Get(tunnelID)
}

func (s *Service) Deploy(id int64) (DeployTunnelResponse, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return DeployTunnelResponse{}, err
	}
	if rec.ClusterID <= 0 {
		return DeployTunnelResponse{}, errors.New("cloudflare tunnel is not attached to a cluster")
	}
	if s.secrets == nil {
		return DeployTunnelResponse{}, errors.New("secret store is not configured")
	}
	if s.applier == nil {
		return DeployTunnelResponse{}, errors.New("manifest applier is not configured")
	}
	tokenBytes, err := s.secrets.Get(context.Background(), rec.TokenSecretRef)
	if err != nil {
		return DeployTunnelResponse{}, fmt.Errorf("load cloudflare tunnel token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return DeployTunnelResponse{}, errors.New("stored cloudflare tunnel token is empty")
	}

	manifest := buildCloudflaredManifest(rec.Namespace, rec.DeploymentName, rec.Image, rec.Replicas, token)
	redacted := buildCloudflaredManifest(rec.Namespace, rec.DeploymentName, rec.Image, rec.Replicas, "REDACTED")
	_ = s.appendEvent(id, "deploy", "info", "Applying cloudflared high availability manifest", "")
	output, applyErr := s.applier.ApplyRawManifest(rec.ClusterID, rec.Namespace, manifest)
	status := "deployed"
	errorMessage := ""
	appliedAtExpr := "NOW()"
	if applyErr != nil {
		status = "apply_failed"
		errorMessage = applyErr.Error()
		appliedAtExpr = "last_applied_at"
	}
	_, updateErr := s.db.Exec(
		fmt.Sprintf(`UPDATE cloudflare_tunnels
		                SET status = $2,
		                    last_manifest = $3,
		                    last_apply_output = $4,
		                    last_applied_at = %s,
		                    updated_at = NOW()
		              WHERE id = $1`, appliedAtExpr),
		id,
		status,
		redacted,
		output,
	)
	if updateErr != nil {
		return DeployTunnelResponse{}, fmt.Errorf("update cloudflare tunnel deploy status: %w", updateErr)
	}
	if applyErr != nil {
		_ = s.appendEvent(id, "deploy", "error", "cloudflared manifest apply failed", errorMessage)
		return DeployTunnelResponse{TunnelID: id, Status: status, Output: output}, applyErr
	}
	_ = s.appendEvent(id, "deploy", "success", "cloudflared manifest applied", output)
	return DeployTunnelResponse{TunnelID: id, Status: status, Output: output}, nil
}

func (s *Service) Manifest(id int64) (map[string]string, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return nil, err
	}
	manifest := strings.TrimSpace(rec.LastManifest)
	if manifest == "" {
		manifest = buildCloudflaredManifest(rec.Namespace, rec.DeploymentName, rec.Image, rec.Replicas, "REDACTED")
	}
	return map[string]string{"manifest": manifest}, nil
}

func (s *Service) Delete(id int64, confirmationName string) error {
	rec, err := s.getInternal(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(confirmationName) != rec.Name {
		return ErrTunnelDeleteConfirmation
	}
	res, err := s.db.Exec(`DELETE FROM cloudflare_tunnels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cloudflare tunnel: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrTunnelNotFound
	}
	if s.secrets != nil {
		if rec.TokenSecretRef != "" {
			_ = s.secrets.Delete(context.Background(), rec.TokenSecretRef)
		}
		if rec.APITokenSecretRef != "" {
			_ = s.secrets.Delete(context.Background(), rec.APITokenSecretRef)
		}
	}
	return nil
}

func (s *Service) ListEvents(id int64) ([]CloudflareTunnelEvent, error) {
	if _, err := s.getInternal(id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, tunnel_id, step, level, message, output, created_at
		   FROM cloudflare_tunnel_events
		  WHERE tunnel_id = $1
		  ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloudflare tunnel events: %w", err)
	}
	defer rows.Close()
	out := make([]CloudflareTunnelEvent, 0)
	for rows.Next() {
		var event CloudflareTunnelEvent
		if err := rows.Scan(&event.ID, &event.TunnelID, &event.Step, &event.Level, &event.Message, &event.Output, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan cloudflare tunnel event: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Service) insertTunnel(req CreateTunnelRequest, tokenRef, apiTokenRef, redactedManifest string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin cloudflare tunnel transaction: %w", err)
	}
	defer tx.Rollback()

	clusterID := sql.NullInt64{}
	if req.ClusterID > 0 {
		clusterID = sql.NullInt64{Int64: req.ClusterID, Valid: true}
	}

	var id int64
	err = tx.QueryRow(
		`INSERT INTO cloudflare_tunnels (
		     cluster_id, name, namespace, deployment_name, routing_mode, provisioning_mode,
		     cloudflare_account_id, cloudflare_zone_id, cloudflare_tunnel_id, cloudflare_tunnel_name,
		     token_secret_ref, api_token_secret_ref, replicas, image, ingress_namespace,
		     ingress_service_name, ingress_service_port, ingress_service_scheme, configure_dns,
		     status, last_manifest
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, 'configured', $20)
		 RETURNING id`,
		clusterID,
		req.Name,
		req.Namespace,
		req.DeploymentName,
		req.RoutingMode,
		req.ProvisioningMode,
		req.CloudflareAccountID,
		req.CloudflareZoneID,
		req.CloudflareTunnelID,
		req.CloudflareTunnelName,
		tokenRef,
		apiTokenRef,
		req.Replicas,
		req.Image,
		req.IngressNamespace,
		req.IngressServiceName,
		req.IngressServicePort,
		req.IngressServiceScheme,
		req.ConfigureDNS,
		redactedManifest,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert cloudflare tunnel: %w", err)
	}

	for _, route := range req.Routes {
		_, err = tx.Exec(
			`INSERT INTO cloudflare_tunnel_routes (
			     tunnel_id, hostname, path, service_namespace, service_name, service_port, service_scheme
			 )
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id,
			route.Hostname,
			route.Path,
			route.ServiceNamespace,
			route.ServiceName,
			route.ServicePort,
			route.ServiceScheme,
		)
		if err != nil {
			return 0, fmt.Errorf("insert cloudflare tunnel route: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit cloudflare tunnel transaction: %w", err)
	}
	return id, nil
}

func (s *Service) ensureDeploymentSlotAvailable(clusterID int64, namespace, deploymentName string) error {
	var exists bool
	var err error
	if clusterID > 0 {
		err = s.db.QueryRow(
			`SELECT EXISTS (
			     SELECT 1
			       FROM cloudflare_tunnels
			      WHERE cluster_id = $1
			        AND namespace = $2
			        AND deployment_name = $3
			 )`,
			clusterID,
			namespace,
			deploymentName,
		).Scan(&exists)
	} else {
		err = s.db.QueryRow(
			`SELECT EXISTS (
			     SELECT 1
			       FROM cloudflare_tunnels
			      WHERE cluster_id IS NULL
			        AND namespace = $1
			        AND deployment_name = $2
			 )`,
			namespace,
			deploymentName,
		).Scan(&exists)
	}
	if err != nil {
		return fmt.Errorf("check cloudflare tunnel deployment uniqueness: %w", err)
	}
	if exists {
		return errors.New("cloudflare tunnel deployment already exists for this cluster and namespace")
	}
	return nil
}

func (s *Service) getInternal(id int64) (tunnelRecord, error) {
	row := s.db.QueryRow(
		`SELECT t.id, t.cluster_id, COALESCE(c.name, ''), t.name, t.namespace, t.deployment_name,
		        t.routing_mode, t.provisioning_mode, t.cloudflare_account_id, t.cloudflare_zone_id,
		        t.cloudflare_tunnel_id, t.cloudflare_tunnel_name, t.token_secret_ref, t.api_token_secret_ref,
		        t.replicas, t.image, t.ingress_namespace, t.ingress_service_name,
		        t.ingress_service_port, t.ingress_service_scheme, t.configure_dns, t.status,
		        t.last_manifest, t.last_apply_output, t.last_applied_at, t.created_at, t.updated_at
		   FROM cloudflare_tunnels t
		   LEFT JOIN clusters c ON c.id = t.cluster_id
		  WHERE t.id = $1`,
		id,
	)
	rec, err := scanTunnel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return tunnelRecord{}, ErrTunnelNotFound
	}
	if err != nil {
		return tunnelRecord{}, err
	}
	routes, err := s.listRoutes(id)
	if err != nil {
		return tunnelRecord{}, err
	}
	rec.Routes = routes
	return rec, nil
}

func (s *Service) listRoutes(tunnelID int64) ([]CloudflareTunnelRoute, error) {
	rows, err := s.db.Query(
		`SELECT id, tunnel_id, hostname, path, service_namespace, service_name, service_port, service_scheme, created_at
		   FROM cloudflare_tunnel_routes
		  WHERE tunnel_id = $1
		  ORDER BY hostname ASC, path ASC`,
		tunnelID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloudflare tunnel routes: %w", err)
	}
	defer rows.Close()
	out := make([]CloudflareTunnelRoute, 0)
	for rows.Next() {
		var route CloudflareTunnelRoute
		if err := rows.Scan(
			&route.ID,
			&route.TunnelID,
			&route.Hostname,
			&route.Path,
			&route.ServiceNamespace,
			&route.ServiceName,
			&route.ServicePort,
			&route.ServiceScheme,
			&route.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan cloudflare tunnel route: %w", err)
		}
		out = append(out, route)
	}
	return out, rows.Err()
}

type tunnelScanner interface {
	Scan(dest ...any) error
}

func scanTunnel(row tunnelScanner) (tunnelRecord, error) {
	var rec tunnelRecord
	var clusterID sql.NullInt64
	err := row.Scan(
		&rec.ID,
		&clusterID,
		&rec.ClusterName,
		&rec.Name,
		&rec.Namespace,
		&rec.DeploymentName,
		&rec.RoutingMode,
		&rec.ProvisioningMode,
		&rec.CloudflareAccountID,
		&rec.CloudflareZoneID,
		&rec.CloudflareTunnelID,
		&rec.CloudflareTunnelName,
		&rec.TokenSecretRef,
		&rec.APITokenSecretRef,
		&rec.Replicas,
		&rec.Image,
		&rec.IngressNamespace,
		&rec.IngressServiceName,
		&rec.IngressServicePort,
		&rec.IngressServiceScheme,
		&rec.ConfigureDNS,
		&rec.Status,
		&rec.LastManifest,
		&rec.LastApplyOutput,
		&rec.LastApplied,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		return tunnelRecord{}, err
	}
	if clusterID.Valid {
		rec.ClusterID = clusterID.Int64
	} else {
		rec.IsGlobal = true
	}
	if rec.LastApplied.Valid {
		appliedAt := rec.LastApplied.Time
		rec.LastAppliedAt = &appliedAt
	}
	rec.HasTunnelToken = rec.TokenSecretRef != ""
	rec.HasAPIToken = rec.APITokenSecretRef != ""
	return rec, nil
}

func (s *Service) appendEvent(tunnelID int64, step, level, message, output string) error {
	_, err := s.db.Exec(
		`INSERT INTO cloudflare_tunnel_events (tunnel_id, step, level, message, output)
		 VALUES ($1, $2, $3, $4, $5)`,
		tunnelID,
		strings.TrimSpace(step),
		strings.TrimSpace(level),
		strings.TrimSpace(message),
		output,
	)
	if err != nil {
		return fmt.Errorf("append cloudflare tunnel event: %w", err)
	}
	return nil
}

func normalizeCreateRequest(req CreateTunnelRequest) (CreateTunnelRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultString(req.Namespace, "cloudflare-tunnel")
	req.DeploymentName = defaultString(req.DeploymentName, "cloudflared")
	if req.IsGlobal {
		req.ClusterID = 0
		req.Apply = false
	}
	req.RoutingMode = defaultString(req.RoutingMode, RoutingModeTraefik)
	req.ProvisioningMode = defaultString(req.ProvisioningMode, ProvisioningModeManualToken)
	req.CloudflareAccountID = strings.TrimSpace(req.CloudflareAccountID)
	req.CloudflareZoneID = strings.TrimSpace(req.CloudflareZoneID)
	req.CloudflareTunnelID = strings.TrimSpace(req.CloudflareTunnelID)
	req.CloudflareTunnelName = strings.TrimSpace(req.CloudflareTunnelName)
	req.Image = defaultString(req.Image, "cloudflare/cloudflared:latest")
	req.IngressNamespace = defaultString(req.IngressNamespace, "traefik")
	req.IngressServiceName = defaultString(req.IngressServiceName, "traefik")
	req.IngressServicePort = defaultInt(req.IngressServicePort, 80)
	req.IngressServiceScheme = defaultString(req.IngressServiceScheme, "http")
	req.Replicas = defaultInt(req.Replicas, 3)

	if req.Apply && req.ClusterID <= 0 {
		return CreateTunnelRequest{}, errors.New("cluster_id is required when apply is true")
	}
	if req.Name == "" {
		return CreateTunnelRequest{}, errors.New("name is required")
	}
	if !isValidKubernetesName(req.Namespace) {
		return CreateTunnelRequest{}, errors.New("namespace must be a valid Kubernetes name")
	}
	if !isValidKubernetesName(req.DeploymentName) {
		return CreateTunnelRequest{}, errors.New("deployment_name must be a valid Kubernetes name")
	}
	if req.RoutingMode != RoutingModeTraefik && req.RoutingMode != RoutingModeDirect {
		return CreateTunnelRequest{}, errors.New("routing_mode must be traefik or direct")
	}
	if req.ProvisioningMode != ProvisioningModeManualToken && req.ProvisioningMode != ProvisioningModeCloudflareAPI {
		return CreateTunnelRequest{}, errors.New("provisioning_mode must be manual_token or cloudflare_api")
	}
	if req.ProvisioningMode == ProvisioningModeCloudflareAPI && req.CloudflareAccountID == "" {
		return CreateTunnelRequest{}, errors.New("cloudflare_account_id is required for Cloudflare API mode")
	}
	if req.ConfigureDNS && req.CloudflareZoneID == "" {
		return CreateTunnelRequest{}, errors.New("cloudflare_zone_id is required when configure_dns is enabled")
	}
	if req.Replicas < 2 {
		return CreateTunnelRequest{}, errors.New("replicas must be at least 2 for high availability")
	}
	if req.IngressServicePort <= 0 || req.IngressServicePort > 65535 {
		return CreateTunnelRequest{}, errors.New("ingress_service_port must be between 1 and 65535")
	}
	if req.IngressServiceScheme != "http" && req.IngressServiceScheme != "https" {
		return CreateTunnelRequest{}, errors.New("ingress_service_scheme must be http or https")
	}

	seen := make(map[string]struct{})
	normalizedRoutes := make([]CloudflareTunnelRouteInput, 0, len(req.Routes))
	for _, route := range req.Routes {
		route.Hostname = strings.ToLower(strings.TrimSpace(route.Hostname))
		route.Path = normalizeRoutePath(route.Path)
		route.ServiceNamespace = defaultString(route.ServiceNamespace, "pscs")
		route.ServiceName = strings.TrimSpace(route.ServiceName)
		route.ServicePort = defaultInt(route.ServicePort, 80)
		route.ServiceScheme = defaultString(route.ServiceScheme, "http")
		if route.Hostname == "" || !isValidHostname(route.Hostname) {
			return CreateTunnelRequest{}, fmt.Errorf("invalid route hostname %q", route.Hostname)
		}
		if req.RoutingMode == RoutingModeTraefik && route.ServiceName == "" {
			route.ServiceNamespace = req.IngressNamespace
			route.ServiceName = req.IngressServiceName
			route.ServicePort = req.IngressServicePort
			route.ServiceScheme = req.IngressServiceScheme
		}
		if route.ServiceName == "" {
			return CreateTunnelRequest{}, fmt.Errorf("service_name is required for route %s", route.Hostname)
		}
		if !isValidKubernetesName(route.ServiceNamespace) || !isValidKubernetesName(route.ServiceName) {
			return CreateTunnelRequest{}, fmt.Errorf("route %s has an invalid service reference", route.Hostname)
		}
		if route.ServicePort <= 0 || route.ServicePort > 65535 {
			return CreateTunnelRequest{}, fmt.Errorf("route %s has invalid service_port", route.Hostname)
		}
		if route.ServiceScheme != "http" && route.ServiceScheme != "https" {
			return CreateTunnelRequest{}, fmt.Errorf("route %s service_scheme must be http or https", route.Hostname)
		}
		key := route.Hostname + "\x00" + route.Path
		if _, exists := seen[key]; exists {
			return CreateTunnelRequest{}, fmt.Errorf("duplicate route for %s%s", route.Hostname, route.Path)
		}
		seen[key] = struct{}{}
		normalizedRoutes = append(normalizedRoutes, route)
	}
	if len(normalizedRoutes) == 0 {
		return CreateTunnelRequest{}, errors.New("at least one hostname route is required")
	}
	sort.SliceStable(normalizedRoutes, func(i, j int) bool {
		if normalizedRoutes[i].Hostname == normalizedRoutes[j].Hostname {
			return normalizedRoutes[i].Path < normalizedRoutes[j].Path
		}
		return normalizedRoutes[i].Hostname < normalizedRoutes[j].Hostname
	})
	req.Routes = normalizedRoutes
	return req, nil
}

func buildCloudflaredManifest(namespace, deploymentName, image string, replicas int, token string) string {
	labels := fmt.Sprintf("app.kubernetes.io/name: cloudflared\n    app.kubernetes.io/instance: %s", deploymentName)
	selector := fmt.Sprintf("app.kubernetes.io/name: cloudflared\n      app.kubernetes.io/instance: %s", deploymentName)
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
apiVersion: v1
kind: Secret
metadata:
  name: %s-token
  namespace: %s
  labels:
    %s
type: Opaque
stringData:
  token: %q
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: %s
  namespace: %s
  labels:
    %s
spec:
  minAvailable: 1
  selector:
    matchLabels:
      %s
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    %s
spec:
  replicas: %d
  revisionHistoryLimit: 5
  selector:
    matchLabels:
      %s
  template:
    metadata:
      labels:
        app.kubernetes.io/name: cloudflared
        app.kubernetes.io/instance: %s
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app.kubernetes.io/name: cloudflared
                    app.kubernetes.io/instance: %s
                topologyKey: kubernetes.io/hostname
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: cloudflared
              app.kubernetes.io/instance: %s
      containers:
        - name: cloudflared
          image: %s
          imagePullPolicy: IfNotPresent
          args:
            - tunnel
            - --no-autoupdate
            - --metrics
            - 0.0.0.0:2000
            - run
            - --token
            - $(TUNNEL_TOKEN)
          env:
            - name: TUNNEL_TOKEN
              valueFrom:
                secretKeyRef:
                  name: %s-token
                  key: token
          ports:
            - name: metrics
              containerPort: 2000
          readinessProbe:
            httpGet:
              path: /ready
              port: metrics
            initialDelaySeconds: 10
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 6
          livenessProbe:
            httpGet:
              path: /ready
              port: metrics
            initialDelaySeconds: 30
            periodSeconds: 20
            timeoutSeconds: 5
            failureThreshold: 6
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              memory: 256Mi
          securityContext:
            allowPrivilegeEscalation: false
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop:
                - ALL
---
apiVersion: v1
kind: Service
metadata:
  name: %s-metrics
  namespace: %s
  labels:
    %s
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: cloudflared
    app.kubernetes.io/instance: %s
  ports:
    - name: metrics
      port: 2000
      targetPort: metrics
`, namespace, deploymentName, namespace, labels, token, deploymentName, namespace, labels, selector, deploymentName, namespace, labels, replicas, selector, deploymentName, deploymentName, deploymentName, image, deploymentName, deploymentName, namespace, labels, deploymentName)
}

func remoteConfigIngress(req CreateTunnelRequest) []map[string]string {
	ingress := make([]map[string]string, 0, len(req.Routes)+1)
	for _, route := range req.Routes {
		entry := map[string]string{
			"hostname": route.Hostname,
			"service":  routeTargetURL(req, route),
		}
		if route.Path != "" {
			entry["path"] = route.Path
		}
		ingress = append(ingress, entry)
	}
	ingress = append(ingress, map[string]string{"service": "http_status:404"})
	return ingress
}

func routeTargetURL(req CreateTunnelRequest, route CloudflareTunnelRouteInput) string {
	if req.RoutingMode == RoutingModeTraefik {
		return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", req.IngressServiceScheme, req.IngressServiceName, req.IngressNamespace, req.IngressServicePort)
	}
	return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", route.ServiceScheme, route.ServiceName, route.ServiceNamespace, route.ServicePort)
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func isValidKubernetesName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	for i, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			return false
		}
		if (i == 0 || i == len(name)-1) && r == '-' {
			return false
		}
	}
	return true
}

func isValidHostname(hostname string) bool {
	if strings.Contains(hostname, "://") || strings.ContainsAny(hostname, "/ \\") || len(hostname) > 253 {
		return false
	}
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

type cfResponse[T any] struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
	Result  T         `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfTunnel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

func (s *Service) createCloudflareTunnel(ctx context.Context, apiToken, accountID, name string) (cfTunnel, error) {
	body := map[string]string{
		"name":       name,
		"config_src": "cloudflare",
	}
	var resp cfResponse[cfTunnel]
	if err := s.doCloudflare(ctx, apiToken, http.MethodPost, fmt.Sprintf("/accounts/%s/cfd_tunnel", url.PathEscape(accountID)), body, &resp); err != nil {
		return cfTunnel{}, err
	}
	if resp.Result.ID == "" {
		return cfTunnel{}, errors.New("cloudflare create tunnel response did not include an id")
	}
	return resp.Result, nil
}

func (s *Service) getCloudflareTunnelToken(ctx context.Context, apiToken, accountID, tunnelID string) (string, error) {
	var resp cfResponse[string]
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", url.PathEscape(accountID), url.PathEscape(tunnelID))
	if err := s.doCloudflare(ctx, apiToken, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}
	token := strings.TrimSpace(resp.Result)
	if token == "" {
		return "", errors.New("cloudflare token response was empty")
	}
	return token, nil
}

func (s *Service) putCloudflareTunnelConfig(ctx context.Context, apiToken string, req CreateTunnelRequest) error {
	body := map[string]any{
		"config": map[string]any{
			"ingress": remoteConfigIngress(req),
		},
	}
	var resp cfResponse[json.RawMessage]
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/configurations", url.PathEscape(req.CloudflareAccountID), url.PathEscape(req.CloudflareTunnelID))
	return s.doCloudflare(ctx, apiToken, http.MethodPut, path, body, &resp)
}

func (s *Service) upsertCloudflareDNSRecords(ctx context.Context, apiToken string, req CreateTunnelRequest) (string, error) {
	target := req.CloudflareTunnelID + ".cfargotunnel.com"
	outputs := make([]string, 0, len(req.Routes))
	seen := make(map[string]struct{})
	for _, route := range req.Routes {
		if _, ok := seen[route.Hostname]; ok {
			continue
		}
		seen[route.Hostname] = struct{}{}

		existing, err := s.findCloudflareDNSRecord(ctx, apiToken, req.CloudflareZoneID, route.Hostname)
		if err != nil {
			return strings.Join(outputs, "\n"), err
		}
		body := map[string]any{
			"type":    "CNAME",
			"name":    route.Hostname,
			"content": target,
			"proxied": true,
		}
		if existing.ID == "" {
			var resp cfResponse[cfDNSRecord]
			if err := s.doCloudflare(ctx, apiToken, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(req.CloudflareZoneID)), body, &resp); err != nil {
				return strings.Join(outputs, "\n"), err
			}
			outputs = append(outputs, "created "+route.Hostname)
			continue
		}
		var resp cfResponse[cfDNSRecord]
		path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(req.CloudflareZoneID), url.PathEscape(existing.ID))
		if err := s.doCloudflare(ctx, apiToken, http.MethodPatch, path, body, &resp); err != nil {
			return strings.Join(outputs, "\n"), err
		}
		outputs = append(outputs, "updated "+route.Hostname)
	}
	return strings.Join(outputs, "\n"), nil
}

func (s *Service) findCloudflareDNSRecord(ctx context.Context, apiToken, zoneID, hostname string) (cfDNSRecord, error) {
	query := url.Values{}
	query.Set("type", "CNAME")
	query.Set("name", hostname)
	var resp cfResponse[[]cfDNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records?%s", url.PathEscape(zoneID), query.Encode())
	if err := s.doCloudflare(ctx, apiToken, http.MethodGet, path, nil, &resp); err != nil {
		return cfDNSRecord{}, err
	}
	if len(resp.Result) == 0 {
		return cfDNSRecord{}, nil
	}
	return resp.Result[0], nil
}

func (s *Service) doCloudflare(ctx context.Context, apiToken, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal cloudflare request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.apiBaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.cfClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare api request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read cloudflare response: %w", err)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode cloudflare response: %w\nResponse: %s", err, string(respBody))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare api returned %s: %s", resp.Status, summarizeCloudflareErrors(respBody))
	}
	if failed := cloudflareFailed(out); failed != "" {
		return errors.New(failed)
	}
	return nil
}

func cloudflareFailed(out any) string {
	raw, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	var base struct {
		Success bool      `json:"success"`
		Errors  []cfError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return ""
	}
	if base.Success {
		return ""
	}
	return "cloudflare api returned success=false: " + formatCloudflareErrors(base.Errors)
}

func summarizeCloudflareErrors(respBody []byte) string {
	var base struct {
		Errors []cfError `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &base); err != nil || len(base.Errors) == 0 {
		return string(respBody)
	}
	return formatCloudflareErrors(base.Errors)
}

func formatCloudflareErrors(errors []cfError) string {
	parts := make([]string, 0, len(errors))
	for _, cfErr := range errors {
		if cfErr.Code > 0 {
			parts = append(parts, fmt.Sprintf("%d: %s", cfErr.Code, cfErr.Message))
			continue
		}
		parts = append(parts, cfErr.Message)
	}
	if len(parts) == 0 {
		return "unknown cloudflare api error"
	}
	return strings.Join(parts, "; ")
}
