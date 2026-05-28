package monitoring

import (
	"database/sql"
	"errors"
	"time"
)

const (
	ClusterModeInCluster   = "in_cluster"
	ClusterModeRemoteWrite = "remote_write"
	ClusterModeFederation  = "federation"
)

var (
	ErrStackNotFound       = errors.New("monitoring stack not configured")
	ErrClusterNotFound     = errors.New("monitored cluster not found")
	ErrDeleteConfirmation  = errors.New("monitored cluster name confirmation does not match")
	ErrClusterNeedsKubeRef = errors.New("monitored cluster is not linked to a managed Kubernetes cluster")
)

type ManifestApplier interface {
	ApplyRawManifest(clusterID int64, namespace, manifest string) (string, error)
}

type MonitoringStack struct {
	ID                    int64      `json:"id"`
	Namespace             string     `json:"namespace"`
	GrafanaDomain         string     `json:"grafana_domain"`
	StorageClass          string     `json:"storage_class"`
	PrometheusStorageSize string     `json:"prometheus_storage_size"`
	GrafanaStorageSize    string     `json:"grafana_storage_size"`
	Retention             string     `json:"retention"`
	ExposeGrafana         bool       `json:"expose_grafana"`
	ExposePrometheus      bool       `json:"expose_prometheus"`
	ExposeAlertmanager    bool       `json:"expose_alertmanager"`
	Status                string     `json:"status"`
	LastValuesYAML        string     `json:"last_values_yaml,omitempty"`
	LastManifest          string     `json:"last_manifest,omitempty"`
	LastApplyOutput       string     `json:"last_apply_output,omitempty"`
	LastAppliedAt         *time.Time `json:"last_applied_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type MonitoredCluster struct {
	ID              int64      `json:"id"`
	ClusterID       *int64     `json:"cluster_id,omitempty"`
	ClusterName     string     `json:"cluster_name,omitempty"`
	Name            string     `json:"name"`
	Environment     string     `json:"environment"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	PrometheusURL   string     `json:"prometheus_url"`
	GrafanaURL      string     `json:"grafana_url"`
	AgentNamespace  string     `json:"agent_namespace"`
	RemoteWriteURL  string     `json:"remote_write_url"`
	Notes           string     `json:"notes"`
	LastManifest    string     `json:"last_manifest,omitempty"`
	LastApplyOutput string     `json:"last_apply_output,omitempty"`
	LastSyncedAt    *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type MonitoringStatus struct {
	Stack              *MonitoringStack `json:"stack,omitempty"`
	ClusterCount       int              `json:"cluster_count"`
	HealthyClusters    int              `json:"healthy_clusters"`
	ConfiguredClusters int              `json:"configured_clusters"`
	NeedsAttention     int              `json:"needs_attention"`
	RecommendedMode    string           `json:"recommended_mode"`
}

type UpsertStackRequest struct {
	Namespace             string `json:"namespace"`
	GrafanaDomain         string `json:"grafana_domain"`
	StorageClass          string `json:"storage_class"`
	PrometheusStorageSize string `json:"prometheus_storage_size"`
	GrafanaStorageSize    string `json:"grafana_storage_size"`
	Retention             string `json:"retention"`
	ExposeGrafana         bool   `json:"expose_grafana"`
	ExposePrometheus      bool   `json:"expose_prometheus"`
	ExposeAlertmanager    bool   `json:"expose_alertmanager"`
}

type CreateClusterRequest struct {
	ClusterID      *int64 `json:"cluster_id"`
	Name           string `json:"name"`
	Environment    string `json:"environment"`
	Mode           string `json:"mode"`
	PrometheusURL  string `json:"prometheus_url"`
	GrafanaURL     string `json:"grafana_url"`
	AgentNamespace string `json:"agent_namespace"`
	RemoteWriteURL string `json:"remote_write_url"`
	Notes          string `json:"notes"`
}

type DeleteClusterRequest struct {
	ConfirmationName string `json:"confirmation_name"`
}

type ManifestResponse struct {
	ValuesYAML string `json:"values_yaml,omitempty"`
	Manifest   string `json:"manifest"`
	Commands   string `json:"commands"`
}

type SyncResponse struct {
	ClusterID int64  `json:"cluster_id"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	Manifest  string `json:"manifest"`
}

type stackRecord struct {
	MonitoringStack
	LastApplied sql.NullTime
}

type clusterRecord struct {
	MonitoredCluster
	ClusterIDValue sql.NullInt64
	LastSynced     sql.NullTime
}
