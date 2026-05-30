package monitoring

import (
	"database/sql"
	"fmt"
	"strings"
)

type Service struct {
	db      *sql.DB
	applier ManifestApplier
}

func NewService(db *sql.DB, applier ManifestApplier) *Service {
	return &Service{db: db, applier: applier}
}

func (s *Service) Status() (MonitoringStatus, error) {
	stack, err := s.Stack()
	stackFound := err == nil
	if err != nil && err != ErrStackNotFound {
		return MonitoringStatus{}, err
	}
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM monitored_clusters GROUP BY status`)
	if err != nil {
		return MonitoringStatus{}, err
	}
	defer rows.Close()

	status := MonitoringStatus{RecommendedMode: ClusterModeRemoteWrite}
	if stackFound {
		status.Stack = &stack
	}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return MonitoringStatus{}, err
		}
		status.ClusterCount += count
		switch strings.ToLower(state) {
		case "healthy", "synced":
			status.HealthyClusters += count
		case "configured", "planned":
			status.ConfiguredClusters += count
		default:
			status.NeedsAttention += count
		}
	}
	return status, rows.Err()
}

func (s *Service) Stack() (MonitoringStack, error) {
	row := s.db.QueryRow(`
		SELECT id, namespace, grafana_domain, storage_class, prometheus_storage_size,
		       grafana_storage_size, retention, expose_grafana, expose_prometheus,
		       expose_alertmanager, status, last_values_yaml, last_manifest,
		       last_apply_output, last_applied_at, created_at, updated_at
		FROM monitoring_stacks
		ORDER BY id DESC
		LIMIT 1`)
	return scanStack(row)
}

func (s *Service) UpsertStack(req UpsertStackRequest) (MonitoringStack, error) {
	req.Namespace = nonEmpty(req.Namespace, "monitoring")
	req.GrafanaDomain = nonEmpty(req.GrafanaDomain, "grafana.proxaops.com")
	req.PrometheusStorageSize = nonEmpty(req.PrometheusStorageSize, "50Gi")
	req.GrafanaStorageSize = nonEmpty(req.GrafanaStorageSize, "10Gi")
	req.Retention = nonEmpty(req.Retention, "15d")
	values := buildValuesYAML(req)
	manifest := buildStackManifest(req)

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM monitoring_stacks`).Scan(&count); err != nil {
		return MonitoringStack{}, err
	}
	if count == 0 {
		_, err := s.db.Exec(`
			INSERT INTO monitoring_stacks (
				namespace, grafana_domain, storage_class, prometheus_storage_size,
				grafana_storage_size, retention, expose_grafana, expose_prometheus,
				expose_alertmanager, status, last_values_yaml, last_manifest
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'planned',$10,$11)`,
			req.Namespace, req.GrafanaDomain, req.StorageClass, req.PrometheusStorageSize,
			req.GrafanaStorageSize, req.Retention, req.ExposeGrafana, req.ExposePrometheus,
			req.ExposeAlertmanager, values, manifest,
		)
		if err != nil {
			return MonitoringStack{}, err
		}
	} else {
		_, err := s.db.Exec(`
			UPDATE monitoring_stacks
			SET namespace=$1, grafana_domain=$2, storage_class=$3,
			    prometheus_storage_size=$4, grafana_storage_size=$5, retention=$6,
			    expose_grafana=$7, expose_prometheus=$8, expose_alertmanager=$9,
			    status='planned', last_values_yaml=$10, last_manifest=$11, updated_at=NOW()
			WHERE id = (SELECT id FROM monitoring_stacks ORDER BY id DESC LIMIT 1)`,
			req.Namespace, req.GrafanaDomain, req.StorageClass, req.PrometheusStorageSize,
			req.GrafanaStorageSize, req.Retention, req.ExposeGrafana, req.ExposePrometheus,
			req.ExposeAlertmanager, values, manifest,
		)
		if err != nil {
			return MonitoringStack{}, err
		}
	}
	return s.Stack()
}

func (s *Service) StackManifest() (ManifestResponse, error) {
	stack, err := s.Stack()
	if err != nil {
		return ManifestResponse{}, err
	}
	values := stack.LastValuesYAML
	if strings.TrimSpace(values) == "" {
		values = buildValuesYAML(stackRequestFromStack(stack))
	}
	manifest := stack.LastManifest
	if strings.TrimSpace(manifest) == "" {
		manifest = buildStackManifest(stackRequestFromStack(stack))
	}
	return ManifestResponse{
		ValuesYAML: values,
		Manifest:   manifest,
		Commands:   buildInstallCommands(stack.Namespace),
	}, nil
}

func (s *Service) ApplyStack() (StackApplyResponse, error) {
	stack, err := s.Stack()
	if err != nil {
		return StackApplyResponse{}, err
	}
	values := stack.LastValuesYAML
	if strings.TrimSpace(values) == "" {
		values = buildValuesYAML(stackRequestFromStack(stack))
	}
	manifest := stack.LastManifest
	if strings.TrimSpace(manifest) == "" {
		manifest = buildStackManifest(stackRequestFromStack(stack))
	}

	output, applyErr := s.applier.ApplyMonitoringStack(stack.Namespace, values, manifest)
	status := "installed"
	if applyErr != nil {
		status = "install_failed"
		output = output + "\n" + applyErr.Error()
	}
	_, err = s.db.Exec(`
		UPDATE monitoring_stacks
		SET status=$1, last_values_yaml=$2, last_manifest=$3,
		    last_apply_output=$4, last_applied_at=NOW(), updated_at=NOW()
		WHERE id=$5`, status, values, manifest, output, stack.ID)
	if err != nil {
		return StackApplyResponse{}, err
	}
	if applyErr != nil {
		return StackApplyResponse{}, applyErr
	}
	return StackApplyResponse{StackID: stack.ID, Status: status, Output: output}, nil
}

func (s *Service) ListClusters() ([]MonitoredCluster, error) {
	rows, err := s.db.Query(`
		SELECT mc.id, mc.cluster_id, COALESCE(c.name, ''), mc.name, mc.environment,
		       mc.mode, mc.status, mc.prometheus_url, mc.grafana_url,
		       mc.agent_namespace, mc.remote_write_url, mc.notes, mc.last_manifest,
		       mc.last_apply_output, mc.last_synced_at, mc.created_at, mc.updated_at
		FROM monitored_clusters mc
		LEFT JOIN clusters c ON c.id = mc.cluster_id
		ORDER BY mc.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []MonitoredCluster
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func (s *Service) CreateCluster(req CreateClusterRequest) (MonitoredCluster, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return MonitoredCluster{}, fmt.Errorf("cluster name is required")
	}
	req.Environment = nonEmpty(req.Environment, "production")
	req.Mode = nonEmpty(req.Mode, ClusterModeInCluster)
	req.AgentNamespace = nonEmpty(req.AgentNamespace, "monitoring")
	if req.Mode != ClusterModeInCluster && req.Mode != ClusterModeRemoteWrite && req.Mode != ClusterModeFederation {
		return MonitoredCluster{}, fmt.Errorf("unsupported monitoring mode %q", req.Mode)
	}
	manifest := buildClusterManifest(req)

	row := s.db.QueryRow(`
		INSERT INTO monitored_clusters (
			cluster_id, name, environment, mode, status, prometheus_url,
			grafana_url, agent_namespace, remote_write_url, notes, last_manifest
		) VALUES ($1,$2,$3,$4,'configured',$5,$6,$7,$8,$9,$10)
		RETURNING id`,
		req.ClusterID, req.Name, req.Environment, req.Mode, req.PrometheusURL,
		req.GrafanaURL, req.AgentNamespace, req.RemoteWriteURL, req.Notes, manifest,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		return MonitoredCluster{}, err
	}
	return s.GetCluster(id)
}

func (s *Service) GetCluster(id int64) (MonitoredCluster, error) {
	row := s.db.QueryRow(`
		SELECT mc.id, mc.cluster_id, COALESCE(c.name, ''), mc.name, mc.environment,
		       mc.mode, mc.status, mc.prometheus_url, mc.grafana_url,
		       mc.agent_namespace, mc.remote_write_url, mc.notes, mc.last_manifest,
		       mc.last_apply_output, mc.last_synced_at, mc.created_at, mc.updated_at
		FROM monitored_clusters mc
		LEFT JOIN clusters c ON c.id = mc.cluster_id
		WHERE mc.id=$1`, id)
	return scanCluster(row)
}

func (s *Service) ClusterManifest(id int64) (ManifestResponse, error) {
	cluster, err := s.GetCluster(id)
	if err != nil {
		return ManifestResponse{}, err
	}
	manifest := cluster.LastManifest
	if strings.TrimSpace(manifest) == "" {
		req := CreateClusterRequest{
			ClusterID: cluster.ClusterID, Name: cluster.Name, Environment: cluster.Environment,
			Mode: cluster.Mode, PrometheusURL: cluster.PrometheusURL, GrafanaURL: cluster.GrafanaURL,
			AgentNamespace: cluster.AgentNamespace, RemoteWriteURL: cluster.RemoteWriteURL, Notes: cluster.Notes,
		}
		manifest = buildClusterManifest(req)
	}
	return ManifestResponse{Manifest: manifest, Commands: buildClusterCommands(cluster)}, nil
}

func (s *Service) SyncCluster(id int64) (SyncResponse, error) {
	cluster, err := s.GetCluster(id)
	if err != nil {
		return SyncResponse{}, err
	}
	if cluster.ClusterID == nil {
		return SyncResponse{}, ErrClusterNeedsKubeRef
	}
	manifestResp, err := s.ClusterManifest(id)
	if err != nil {
		return SyncResponse{}, err
	}
	output, applyErr := s.applier.ApplyRawManifest(*cluster.ClusterID, cluster.AgentNamespace, manifestResp.Manifest)
	status := "synced"
	if applyErr != nil {
		status = "sync_failed"
		output = output + "\n" + applyErr.Error()
	}
	_, err = s.db.Exec(`
		UPDATE monitored_clusters
		SET status=$1, last_manifest=$2, last_apply_output=$3, last_synced_at=NOW(), updated_at=NOW()
		WHERE id=$4`, status, manifestResp.Manifest, output, id)
	if err != nil {
		return SyncResponse{}, err
	}
	if applyErr != nil {
		return SyncResponse{}, applyErr
	}
	return SyncResponse{ClusterID: id, Status: status, Output: output, Manifest: manifestResp.Manifest}, nil
}

func (s *Service) DeleteCluster(id int64, confirmationName string) error {
	cluster, err := s.GetCluster(id)
	if err != nil {
		return err
	}
	if confirmationName != cluster.Name {
		return ErrDeleteConfirmation
	}
	_, err = s.db.Exec(`DELETE FROM monitored_clusters WHERE id=$1`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanStack(row scanner) (MonitoringStack, error) {
	var record stackRecord
	err := row.Scan(
		&record.ID, &record.Namespace, &record.GrafanaDomain, &record.StorageClass,
		&record.PrometheusStorageSize, &record.GrafanaStorageSize, &record.Retention,
		&record.ExposeGrafana, &record.ExposePrometheus, &record.ExposeAlertmanager,
		&record.Status, &record.LastValuesYAML, &record.LastManifest,
		&record.LastApplyOutput, &record.LastApplied, &record.CreatedAt, &record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return MonitoringStack{}, ErrStackNotFound
	}
	if err != nil {
		return MonitoringStack{}, err
	}
	if record.LastApplied.Valid {
		record.LastAppliedAt = &record.LastApplied.Time
	}
	return record.MonitoringStack, nil
}

func scanCluster(row scanner) (MonitoredCluster, error) {
	var record clusterRecord
	err := row.Scan(
		&record.ID, &record.ClusterIDValue, &record.ClusterName, &record.Name,
		&record.Environment, &record.Mode, &record.Status, &record.PrometheusURL,
		&record.GrafanaURL, &record.AgentNamespace, &record.RemoteWriteURL,
		&record.Notes, &record.LastManifest, &record.LastApplyOutput,
		&record.LastSynced, &record.CreatedAt, &record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return MonitoredCluster{}, ErrClusterNotFound
	}
	if err != nil {
		return MonitoredCluster{}, err
	}
	if record.ClusterIDValue.Valid {
		record.ClusterID = &record.ClusterIDValue.Int64
	}
	if record.LastSynced.Valid {
		record.LastSyncedAt = &record.LastSynced.Time
	}
	return record.MonitoredCluster, nil
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func stackRequestFromStack(stack MonitoringStack) UpsertStackRequest {
	return UpsertStackRequest{
		Namespace: stack.Namespace, GrafanaDomain: stack.GrafanaDomain, StorageClass: stack.StorageClass,
		PrometheusStorageSize: stack.PrometheusStorageSize, GrafanaStorageSize: stack.GrafanaStorageSize,
		Retention: stack.Retention, ExposeGrafana: stack.ExposeGrafana,
		ExposePrometheus: stack.ExposePrometheus, ExposeAlertmanager: stack.ExposeAlertmanager,
	}
}

func yamlStorageClassLine(storageClass string, indent string) string {
	if strings.TrimSpace(storageClass) == "" {
		return ""
	}
	return fmt.Sprintf("%sstorageClassName: %s\n", indent, strings.TrimSpace(storageClass))
}

func buildValuesYAML(req UpsertStackRequest) string {
	return fmt.Sprintf(`prometheus:
  prometheusSpec:
    retention: %s
    externalLabels:
      cluster: management
      environment: production
    storageSpec:
      volumeClaimTemplate:
        spec:
%s
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: %s

grafana:
  enabled: true
  adminPassword: CHANGE_ME_BEFORE_INSTALL
  persistence:
    enabled: true
    type: pvc
%s
    accessModes:
      - ReadWriteOnce
    size: %s

alertmanager:
  enabled: true

kube-state-metrics:
  enabled: true

prometheus-node-exporter:
  enabled: true
`, req.Retention, yamlStorageClassLine(req.StorageClass, "          "), req.PrometheusStorageSize, yamlStorageClassLine(req.StorageClass, "    "), req.GrafanaStorageSize)
}

func buildStackManifest(req UpsertStackRequest) string {
	parts := []string{fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, req.Namespace)}
	if req.ExposeGrafana {
		parts = append(parts, fmt.Sprintf(`---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: grafana
  namespace: %s
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  ingressClassName: traefik
  rules:
    - host: %s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kube-prometheus-stack-grafana
                port:
                  number: 80
`, req.Namespace, req.GrafanaDomain))
	}
	if req.ExposePrometheus {
		parts = append(parts, fmt.Sprintf(`---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: prometheus
  namespace: %s
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  ingressClassName: traefik
  rules:
    - host: prometheus.%s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kube-prometheus-stack-prometheus
                port:
                  number: 9090
`, req.Namespace, strings.TrimPrefix(req.GrafanaDomain, "grafana.")))
	}
	if req.ExposeAlertmanager {
		parts = append(parts, fmt.Sprintf(`---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: alertmanager
  namespace: %s
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web
spec:
  ingressClassName: traefik
  rules:
    - host: alertmanager.%s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: kube-prometheus-stack-alertmanager
                port:
                  number: 9093
`, req.Namespace, strings.TrimPrefix(req.GrafanaDomain, "grafana.")))
	}
	return strings.Join(parts, "")
}

func buildClusterManifest(req CreateClusterRequest) string {
	clusterLabel := strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
	if req.Mode == ClusterModeRemoteWrite {
		return fmt.Sprintf(`# Remote cluster agent values for %s
cluster:
  name: %s
  environment: %s

alloy:
  controller:
    type: daemonset
  clustering:
    enabled: true
  configMap:
    content: |
      prometheus.remote_write "central" {
        endpoint {
          url = "%s"
        }
      }
      prometheus.exporter.unix "node" {}
`, req.Name, clusterLabel, req.Environment, req.RemoteWriteURL)
	}
	return fmt.Sprintf(`apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: pscs-backend
  namespace: %s
  labels:
    release: kube-prometheus-stack
    cluster: %s
    environment: %s
spec:
  namespaceSelector:
    matchNames:
      - pscs
  selector:
    matchLabels:
      app: backend
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: pscs-gateway
  namespace: %s
  labels:
    release: kube-prometheus-stack
    cluster: %s
    environment: %s
spec:
  namespaceSelector:
    matchNames:
      - pscs
  selector:
    matchLabels:
      app: gateway
  endpoints:
    - port: gateway
      path: /metrics
      interval: 30s
`, req.AgentNamespace, clusterLabel, req.Environment, req.AgentNamespace, clusterLabel, req.Environment)
}

func buildInstallCommands(namespace string) string {
	return fmt.Sprintf(`helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack --namespace %s --values infra/k8s/monitoring/values.yaml
kubectl apply -f infra/k8s/monitoring/grafana-ingress.yaml`, namespace, namespace)
}

func buildClusterCommands(cluster MonitoredCluster) string {
	if cluster.Mode == ClusterModeRemoteWrite {
		return "Install Grafana Alloy or Prometheus Agent in the remote cluster with the generated values, then verify remote_write samples in the central backend."
	}
	return fmt.Sprintf("kubectl apply -n %s -f infra/k8s/monitoring/pscs-servicemonitors.yaml", cluster.AgentNamespace)
}
