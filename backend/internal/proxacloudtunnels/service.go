package proxacloudtunnels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"proxacloud/backend/internal/secrets"
)

type Service struct {
	db      *sql.DB
	secrets secrets.Store
	applier ManifestApplier
}

type pendingEvent struct {
	step    string
	level   string
	message string
	output  string
}

func NewService(db *sql.DB, secretStore secrets.Store, applier ManifestApplier) *Service {
	return &Service{db: db, secrets: secretStore, applier: applier}
}

func (s *Service) List() ([]ProxacloudTunnel, error) {
	rows, err := s.db.Query(
		`SELECT t.id, t.cluster_id, COALESCE(c.name, ''), t.name, t.namespace, t.deployment_name,
		        t.proxacloud_tunnel_id, t.token_secret_ref, t.gateway_url, t.target_url, t.replicas,
		        t.image, t.image_pull_secret, t.status, t.last_manifest, t.last_apply_output,
		        t.last_applied_at, t.created_at, t.updated_at
		   FROM proxacloud_tunnels t
		   LEFT JOIN clusters c ON c.id = t.cluster_id
		  ORDER BY t.created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list proxacloud tunnels: %w", err)
	}
	defer rows.Close()

	out := make([]ProxacloudTunnel, 0)
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
		out = append(out, rec.ProxacloudTunnel)
	}
	return out, rows.Err()
}

func (s *Service) Get(id int64) (ProxacloudTunnel, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return ProxacloudTunnel{}, err
	}
	routes, err := s.listRoutes(id)
	if err != nil {
		return ProxacloudTunnel{}, err
	}
	rec.Routes = routes
	return rec.ProxacloudTunnel, nil
}

func (s *Service) Create(req CreateTunnelRequest) (ProxacloudTunnel, error) {
	normalized, err := normalizeCreateRequest(req)
	if err != nil {
		return ProxacloudTunnel{}, err
	}
	if err := s.ensureDeploymentSlotAvailable(normalized.ClusterID, normalized.Namespace, normalized.DeploymentName); err != nil {
		return ProxacloudTunnel{}, err
	}
	if s.secrets == nil {
		return ProxacloudTunnel{}, errors.New("secret store is not configured")
	}

	token := strings.TrimSpace(normalized.TunnelToken)
	if token == "" {
		return ProxacloudTunnel{}, errors.New("tunnel_token is required")
	}
	if normalized.ProxacloudTunnelID == "" {
		return ProxacloudTunnel{}, errors.New("proxacloud_tunnel_id is required")
	}

	ctx := context.Background()
	tokenRef, err := s.secrets.Put(ctx, "proxacloud-tunnels", normalized.Name+"-tunnel-token", []byte(token))
	if err != nil {
		return ProxacloudTunnel{}, fmt.Errorf("store proxacloud tunnel token: %w", err)
	}

	redactedManifest := buildAgentManifest(normalized, "REDACTED")
	tunnelID, err := s.insertTunnel(normalized, tokenRef, redactedManifest)
	if err != nil {
		_ = s.secrets.Delete(ctx, tokenRef)
		return ProxacloudTunnel{}, err
	}
	_ = s.appendEvent(tunnelID, "validate", "info", "Proxacloud tunnel request validated", "")
	_ = s.appendEvent(tunnelID, "configure", "success", "Proxacloud agent manifest configured", "")

	if normalized.Apply {
		if _, deployErr := s.Deploy(tunnelID); deployErr != nil {
			_ = s.appendEvent(tunnelID, "deploy", "error", "Proxacloud agent deployment failed", deployErr.Error())
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
		return DeployTunnelResponse{}, errors.New("proxacloud tunnel is not attached to a cluster")
	}
	if s.secrets == nil {
		return DeployTunnelResponse{}, errors.New("secret store is not configured")
	}
	if s.applier == nil {
		return DeployTunnelResponse{}, errors.New("manifest applier is not configured")
	}
	tokenBytes, err := s.secrets.Get(context.Background(), rec.TokenSecretRef)
	if err != nil {
		return DeployTunnelResponse{}, fmt.Errorf("load proxacloud tunnel token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return DeployTunnelResponse{}, errors.New("stored proxacloud tunnel token is empty")
	}

	req := requestFromRecord(rec)
	manifest := buildAgentManifest(req, token)
	redacted := buildAgentManifest(req, "REDACTED")
	_ = s.appendEvent(id, "deploy", "info", "Applying Proxacloud agent manifest", "")
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
		fmt.Sprintf(`UPDATE proxacloud_tunnels
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
		return DeployTunnelResponse{}, fmt.Errorf("update proxacloud tunnel deploy status: %w", updateErr)
	}
	if applyErr != nil {
		_ = s.appendEvent(id, "deploy", "error", "Proxacloud agent manifest apply failed", errorMessage)
		return DeployTunnelResponse{TunnelID: id, Status: status, Output: output}, applyErr
	}
	_ = s.appendEvent(id, "deploy", "success", "Proxacloud agent manifest applied", output)
	return DeployTunnelResponse{TunnelID: id, Status: status, Output: output}, nil
}

func (s *Service) AttachToCluster(id int64, req AttachTunnelRequest) (DeployTunnelResponse, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return DeployTunnelResponse{}, err
	}
	if req.ClusterID <= 0 {
		return DeployTunnelResponse{}, errors.New("cluster_id is required")
	}
	if err := s.ensureClusterExists(req.ClusterID); err != nil {
		return DeployTunnelResponse{}, err
	}
	if err := s.ensureDeploymentSlotAvailableForTunnel(id, req.ClusterID, rec.Namespace, rec.DeploymentName); err != nil {
		return DeployTunnelResponse{}, err
	}
	_, err = s.db.Exec(
		`UPDATE proxacloud_tunnels
		    SET cluster_id = $2,
		        status = CASE WHEN status = 'deployed' THEN 'attached' ELSE status END,
		        updated_at = NOW()
		  WHERE id = $1`,
		id,
		req.ClusterID,
	)
	if err != nil {
		return DeployTunnelResponse{}, fmt.Errorf("attach proxacloud tunnel to cluster: %w", err)
	}
	_ = s.appendEvent(id, "attach-cluster", "success", "Proxacloud tunnel attached to Kubernetes cluster", fmt.Sprintf("cluster_id=%d", req.ClusterID))

	if req.Apply {
		return s.Deploy(id)
	}
	return DeployTunnelResponse{TunnelID: id, Status: "attached", Warnings: []string{}}, nil
}

func (s *Service) UpdateToken(id int64, req UpdateTunnelTokenRequest) (DeployTunnelResponse, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return DeployTunnelResponse{}, err
	}
	if s.secrets == nil {
		return DeployTunnelResponse{}, errors.New("secret store is not configured")
	}
	token := strings.TrimSpace(req.TunnelToken)
	if token == "" {
		return DeployTunnelResponse{}, errors.New("tunnel_token is required")
	}

	ctx := context.Background()
	newRef, err := s.secrets.Put(ctx, "proxacloud-tunnels", rec.Name+"-tunnel-token", []byte(token))
	if err != nil {
		return DeployTunnelResponse{}, fmt.Errorf("store proxacloud tunnel token: %w", err)
	}

	_, err = s.db.Exec(
		`UPDATE proxacloud_tunnels
		    SET token_secret_ref = $2,
		        status = CASE WHEN status = 'deployed' THEN 'token_updated' ELSE status END,
		        updated_at = NOW()
		  WHERE id = $1`,
		id,
		newRef,
	)
	if err != nil {
		_ = s.secrets.Delete(ctx, newRef)
		return DeployTunnelResponse{}, fmt.Errorf("update proxacloud tunnel token ref: %w", err)
	}
	if strings.TrimSpace(rec.TokenSecretRef) != "" {
		_ = s.secrets.Delete(ctx, rec.TokenSecretRef)
	}
	_ = s.appendEvent(id, "update-token", "success", "Proxacloud tunnel token updated", "")

	if req.Apply {
		return s.Deploy(id)
	}
	return DeployTunnelResponse{TunnelID: id, Status: "token_updated", Warnings: []string{}}, nil
}

func (s *Service) Manifest(id int64) (map[string]string, error) {
	rec, err := s.getInternal(id)
	if err != nil {
		return nil, err
	}
	manifest := strings.TrimSpace(rec.LastManifest)
	if manifest == "" {
		manifest = buildAgentManifest(requestFromRecord(rec), "REDACTED")
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
	res, err := s.db.Exec(`DELETE FROM proxacloud_tunnels WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete proxacloud tunnel: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrTunnelNotFound
	}
	if s.secrets != nil && rec.TokenSecretRef != "" {
		_ = s.secrets.Delete(context.Background(), rec.TokenSecretRef)
	}
	return nil
}

func (s *Service) ListEvents(id int64) ([]ProxacloudTunnelEvent, error) {
	if _, err := s.getInternal(id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, tunnel_id, step, level, message, output, created_at
		   FROM proxacloud_tunnel_events
		  WHERE tunnel_id = $1
		  ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list proxacloud tunnel events: %w", err)
	}
	defer rows.Close()
	out := make([]ProxacloudTunnelEvent, 0)
	for rows.Next() {
		var event ProxacloudTunnelEvent
		if err := rows.Scan(&event.ID, &event.TunnelID, &event.Step, &event.Level, &event.Message, &event.Output, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proxacloud tunnel event: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Service) insertTunnel(req CreateTunnelRequest, tokenRef, redactedManifest string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin proxacloud tunnel transaction: %w", err)
	}
	defer tx.Rollback()

	clusterID := sql.NullInt64{}
	if req.ClusterID > 0 {
		clusterID = sql.NullInt64{Int64: req.ClusterID, Valid: true}
	}

	var id int64
	err = tx.QueryRow(
		`INSERT INTO proxacloud_tunnels (
		     cluster_id, name, namespace, deployment_name, proxacloud_tunnel_id,
		     token_secret_ref, gateway_url, target_url, replicas, image, image_pull_secret,
		     status, last_manifest
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'configured', $12)
		 RETURNING id`,
		clusterID,
		req.Name,
		req.Namespace,
		req.DeploymentName,
		req.ProxacloudTunnelID,
		tokenRef,
		req.GatewayURL,
		req.TargetURL,
		req.Replicas,
		req.Image,
		req.ImagePullSecret,
		redactedManifest,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert proxacloud tunnel: %w", err)
	}

	for _, route := range req.Routes {
		_, err = tx.Exec(
			`INSERT INTO proxacloud_tunnel_routes (
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
			return 0, fmt.Errorf("insert proxacloud tunnel route: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit proxacloud tunnel transaction: %w", err)
	}
	return id, nil
}

func (s *Service) ensureDeploymentSlotAvailable(clusterID int64, namespace, deploymentName string) error {
	return s.ensureDeploymentSlotAvailableForTunnel(0, clusterID, namespace, deploymentName)
}

func (s *Service) ensureDeploymentSlotAvailableForTunnel(tunnelID int64, clusterID int64, namespace, deploymentName string) error {
	var exists bool
	var err error
	if clusterID > 0 {
		err = s.db.QueryRow(
			`SELECT EXISTS (
			     SELECT 1 FROM proxacloud_tunnels
			      WHERE cluster_id = $1 AND namespace = $2 AND deployment_name = $3 AND id <> $4
			 )`,
			clusterID, namespace, deploymentName, tunnelID,
		).Scan(&exists)
	} else {
		err = s.db.QueryRow(
			`SELECT EXISTS (
			     SELECT 1 FROM proxacloud_tunnels
			      WHERE cluster_id IS NULL AND namespace = $1 AND deployment_name = $2 AND id <> $3
			 )`,
			namespace, deploymentName, tunnelID,
		).Scan(&exists)
	}
	if err != nil {
		return fmt.Errorf("check proxacloud tunnel deployment uniqueness: %w", err)
	}
	if exists {
		return errors.New("proxacloud tunnel deployment already exists for this cluster and namespace")
	}
	return nil
}

func (s *Service) ensureClusterExists(clusterID int64) error {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM clusters WHERE id = $1)`, clusterID).Scan(&exists); err != nil {
		return fmt.Errorf("check cluster exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("cluster %d not found", clusterID)
	}
	return nil
}

func (s *Service) getInternal(id int64) (tunnelRecord, error) {
	row := s.db.QueryRow(
		`SELECT t.id, t.cluster_id, COALESCE(c.name, ''), t.name, t.namespace, t.deployment_name,
		        t.proxacloud_tunnel_id, t.token_secret_ref, t.gateway_url, t.target_url, t.replicas,
		        t.image, t.image_pull_secret, t.status, t.last_manifest, t.last_apply_output,
		        t.last_applied_at, t.created_at, t.updated_at
		   FROM proxacloud_tunnels t
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

func (s *Service) listRoutes(tunnelID int64) ([]ProxacloudTunnelRoute, error) {
	rows, err := s.db.Query(
		`SELECT id, tunnel_id, hostname, path, service_namespace, service_name, service_port, service_scheme, created_at
		   FROM proxacloud_tunnel_routes
		  WHERE tunnel_id = $1
		  ORDER BY hostname ASC, path ASC`,
		tunnelID,
	)
	if err != nil {
		return nil, fmt.Errorf("list proxacloud tunnel routes: %w", err)
	}
	defer rows.Close()
	out := make([]ProxacloudTunnelRoute, 0)
	for rows.Next() {
		var route ProxacloudTunnelRoute
		if err := rows.Scan(&route.ID, &route.TunnelID, &route.Hostname, &route.Path, &route.ServiceNamespace, &route.ServiceName, &route.ServicePort, &route.ServiceScheme, &route.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proxacloud tunnel route: %w", err)
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
		&rec.ID, &clusterID, &rec.ClusterName, &rec.Name, &rec.Namespace, &rec.DeploymentName,
		&rec.ProxacloudTunnelID, &rec.TokenSecretRef, &rec.GatewayURL, &rec.TargetURL, &rec.Replicas,
		&rec.Image, &rec.ImagePullSecret, &rec.Status, &rec.LastManifest, &rec.LastApplyOutput,
		&rec.LastApplied, &rec.CreatedAt, &rec.UpdatedAt,
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
	return rec, nil
}

func (s *Service) appendEvent(tunnelID int64, step, level, message, output string) error {
	_, err := s.db.Exec(
		`INSERT INTO proxacloud_tunnel_events (tunnel_id, step, level, message, output)
		 VALUES ($1, $2, $3, $4, $5)`,
		tunnelID,
		strings.TrimSpace(step),
		strings.TrimSpace(level),
		strings.TrimSpace(message),
		output,
	)
	if err != nil {
		return fmt.Errorf("append proxacloud tunnel event: %w", err)
	}
	return nil
}

func normalizeCreateRequest(req CreateTunnelRequest) (CreateTunnelRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Namespace = defaultString(req.Namespace, "proxacloud-agent")
	req.DeploymentName = defaultString(req.DeploymentName, "proxacloud-agent")
	if req.IsGlobal {
		req.ClusterID = 0
		req.Apply = false
	}
	req.ProxacloudTunnelID = strings.TrimSpace(req.ProxacloudTunnelID)
	req.GatewayURL = defaultString(req.GatewayURL, "wss://tunnel.example.com/__agent/connect")
	req.TargetURL = defaultString(req.TargetURL, "http://traefik.kube-system.svc.cluster.local")
	req.Image = strings.TrimSpace(req.Image)
	req.ImagePullSecret = strings.TrimSpace(req.ImagePullSecret)
	req.Replicas = defaultInt(req.Replicas, 1)

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
	if req.Image == "" || strings.HasSuffix(req.Image, ":latest") || !strings.Contains(req.Image, ":") {
		return CreateTunnelRequest{}, errors.New("image must include an immutable non-latest tag")
	}
	if req.Replicas < 1 {
		return CreateTunnelRequest{}, errors.New("replicas must be at least 1")
	}
	if req.ImagePullSecret != "" && !isValidKubernetesName(req.ImagePullSecret) {
		return CreateTunnelRequest{}, errors.New("image_pull_secret must be a valid Kubernetes name")
	}
	if !strings.HasPrefix(req.GatewayURL, "wss://") && !strings.HasPrefix(req.GatewayURL, "ws://") {
		return CreateTunnelRequest{}, errors.New("gateway_url must start with wss:// or ws://")
	}
	if !strings.HasPrefix(req.TargetURL, "http://") && !strings.HasPrefix(req.TargetURL, "https://") {
		return CreateTunnelRequest{}, errors.New("target_url must start with http:// or https://")
	}

	seen := make(map[string]struct{})
	normalizedRoutes := make([]ProxacloudTunnelRouteInput, 0, len(req.Routes))
	for _, route := range req.Routes {
		route.Hostname = strings.ToLower(strings.TrimSpace(route.Hostname))
		route.Path = normalizeRoutePath(route.Path)
		route.ServiceNamespace = defaultString(route.ServiceNamespace, "traefik")
		route.ServiceName = defaultString(route.ServiceName, "traefik")
		route.ServicePort = defaultInt(route.ServicePort, 80)
		route.ServiceScheme = defaultString(route.ServiceScheme, "http")
		if route.Hostname == "" || !isValidHostname(route.Hostname) {
			return CreateTunnelRequest{}, fmt.Errorf("invalid route hostname %q", route.Hostname)
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

func requestFromRecord(rec tunnelRecord) CreateTunnelRequest {
	routes := make([]ProxacloudTunnelRouteInput, 0, len(rec.Routes))
	for _, route := range rec.Routes {
		routes = append(routes, ProxacloudTunnelRouteInput{
			Hostname:         route.Hostname,
			Path:             route.Path,
			ServiceNamespace: route.ServiceNamespace,
			ServiceName:      route.ServiceName,
			ServicePort:      route.ServicePort,
			ServiceScheme:    route.ServiceScheme,
		})
	}
	return CreateTunnelRequest{
		ClusterID:          rec.ClusterID,
		IsGlobal:           rec.IsGlobal,
		Name:               rec.Name,
		Namespace:          rec.Namespace,
		DeploymentName:     rec.DeploymentName,
		ProxacloudTunnelID: rec.ProxacloudTunnelID,
		GatewayURL:         rec.GatewayURL,
		TargetURL:          rec.TargetURL,
		Replicas:           rec.Replicas,
		Image:              rec.Image,
		ImagePullSecret:    rec.ImagePullSecret,
		Routes:             routes,
	}
}

func buildAgentManifest(req CreateTunnelRequest, token string) string {
	labels := fmt.Sprintf("app.kubernetes.io/name: proxacloud-agent\n    app.kubernetes.io/instance: %s", req.DeploymentName)
	selector := fmt.Sprintf("app.kubernetes.io/name: proxacloud-agent\n      app.kubernetes.io/instance: %s", req.DeploymentName)
	imagePullSecrets := ""
	if req.ImagePullSecret != "" {
		imagePullSecrets = fmt.Sprintf(`      imagePullSecrets:
        - name: %s
`, req.ImagePullSecret)
	}
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
  TUNNEL_TOKEN: %q
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
        app.kubernetes.io/name: proxacloud-agent
        app.kubernetes.io/instance: %s
    spec:
%s      containers:
        - name: agent
          image: %s
          imagePullPolicy: IfNotPresent
          env:
            - name: TUNNEL_TOKEN
              valueFrom:
                secretKeyRef:
                  name: %s-token
                  key: TUNNEL_TOKEN
            - name: TUNNEL_ID
              value: %q
            - name: GATEWAY_URL
              value: %q
            - name: TARGET_URL
              value: %q
            - name: AGENT_METRICS_ADDR
              value: ":9091"
          ports:
            - name: metrics
              containerPort: 9091
          readinessProbe:
            httpGet:
              path: /healthz
              port: metrics
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 6
          resources:
            requests:
              cpu: 25m
              memory: 48Mi
            limits:
              memory: 192Mi
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
    app.kubernetes.io/name: proxacloud-agent
    app.kubernetes.io/instance: %s
  ports:
    - name: metrics
      port: 9091
      targetPort: metrics
`, req.Namespace, req.DeploymentName, req.Namespace, labels, token, req.DeploymentName, req.Namespace, labels, selector, req.DeploymentName, req.Namespace, labels, req.Replicas, selector, req.DeploymentName, imagePullSecrets, req.Image, req.DeploymentName, req.ProxacloudTunnelID, req.GatewayURL, req.TargetURL, req.DeploymentName, req.Namespace, labels, req.DeploymentName)
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
