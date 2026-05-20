package clusters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s-proxmox-manager/backend/internal/ansible"
	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/secrets"
	"k8s-proxmox-manager/backend/internal/sshkeys"
	"k8s-proxmox-manager/backend/internal/sslcerts"
	"k8s-proxmox-manager/backend/internal/terraform"
	"k8s-proxmox-manager/backend/internal/topology"
)

type TopologyPlanner interface {
	Plan(req topology.PlanRequest) (topology.PlanResponse, error)
}

type NetworkStore interface {
	GetByID(id int64) (networks.Profile, error)
	ListNodeTagMap() (map[string]int64, error)
	GetReservedIPsByNetworkProfileID(networkProfileID int64) ([]networks.ReservedIP, error)
}

// ProxmoxCredentialStore is satisfied by *proxmox.Service and is used to look
// up stored credentials when running Terraform.
type ProxmoxCredentialStore interface {
	GetCredentialWithSecrets(id int64) (proxmox.Credential, error)
	ControlVMByName(cred proxmox.Credential, proxmoxNodeName string, vmName string, action string) (string, error)
}

type SSHKeyPairStore interface {
	GetByID(id int64) (sshkeys.KeyPair, error)
}

type TLSCertificateStore interface {
	GetByID(id int64) (sslcerts.Certificate, error)
}

type Service struct {
	db              *sql.DB
	networks        NetworkStore
	planner         TopologyPlanner
	terraformRunner *terraform.Runner
	ansibleRunner   *ansible.Runner
	proxmoxSvc      ProxmoxCredentialStore
	sshKeys         SSHKeyPairStore
	tlsCertificates TLSCertificateStore
	secrets         secrets.Store
	operationsDir   string
	autoApply       bool
	autoBootstrap   bool
	kubectlRunsMu   sync.RWMutex
	kubectlRuns     map[string]*kubectlCommandRunState
}

var ErrClusterExists = errors.New("cluster already exists")
var ErrClusterNotFound = errors.New("cluster not found")
var ErrOperationNotFound = errors.New("cluster operation not found")
var ErrApplicationManifestNotFound = errors.New("application manifest not found")

type ProvisionRequest struct {
	Name                string `json:"name"`
	TopologyType        string `json:"topology_type"`
	ControlPlaneCount   int    `json:"control_plane_count"`
	WorkerCount         int    `json:"worker_count"`
	NetworkProfileID    int64  `json:"network_profile_id"`
	KubernetesVersion   string `json:"kubernetes_version"`
	UbuntuImage         string `json:"ubuntu_image"`
	SSHPublicKey        string `json:"ssh_public_key"`
	ControlPlaneCPU     int    `json:"control_plane_cpu"`
	ControlPlaneRAMMB   int    `json:"control_plane_ram_mb"`
	ControlPlaneDisk    int    `json:"control_plane_disk_gb"`
	WorkerCPU           int    `json:"worker_cpu"`
	WorkerRAMMB         int    `json:"worker_ram_mb"`
	WorkerDisk          int    `json:"worker_disk_gb"`
	ProxmoxCredentialID int64  `json:"proxmox_credential_id"`
	SSHKeyPairID        int64  `json:"ssh_key_pair_id"`
	// NodeCredentialIDs optionally overrides the Proxmox credential per node name.
	// Key: proxmox node name, Value: proxmox credential id.
	NodeCredentialIDs map[string]int64 `json:"node_credential_ids"`
	// NodeIPPoolStart is the first IP in the sequential pool to assign to VMs.
	// For example "192.168.8.100" assigns .100, .101, .102 … to each VM.
	// For per-node network profiles, set node_ip_pool_start_by_node overrides.
	NodeIPPoolStart string `json:"node_ip_pool_start"`
	// NodeNetworkProfileIDs optionally overrides network profile per Proxmox node.
	// Key: proxmox node name, Value: network profile id.
	NodeNetworkProfileIDs map[string]int64 `json:"node_network_profile_ids"`
	// NodeIPPoolStartByNode optionally overrides per-node IP pool start.
	// Key: proxmox node name, Value: first node IP in that node's CIDR.
	NodeIPPoolStartByNode map[string]string `json:"node_ip_pool_start_by_node"`
	Apply                 bool              `json:"apply"`
}

type ProvisionResponse struct {
	ClusterID        int64                 `json:"cluster_id"`
	ClusterName      string                `json:"cluster_name"`
	Status           string                `json:"status"`
	Topology         topology.PlanResponse `json:"topology"`
	TerraformWorkDir string                `json:"terraform_work_dir"`
	TerraformOutput  string                `json:"terraform_output,omitempty"`
	Warnings         []string              `json:"warnings"`
}

type ProvisionAndBootstrapRequest struct {
	Provision ProvisionRequest `json:"provision"`
	Bootstrap BootstrapRequest `json:"bootstrap"`
}

type ProvisionAndBootstrapResponse struct {
	ClusterID   int64    `json:"cluster_id"`
	ClusterName string   `json:"cluster_name"`
	Status      string   `json:"status"`
	Warnings    []string `json:"warnings"`
}

type BootstrapRequest struct {
	SSHUser           string                `json:"ssh_user"`
	SSHPrivateKey     string                `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPath string                `json:"ssh_private_key_path,omitempty"`
	SSHKeyPairID      int64                 `json:"ssh_key_pair_id,omitempty"`
	SSHKeyID          int64                 `json:"ssh_key_id,omitempty"`
	SSHKeyName        string                `json:"ssh_key_name,omitempty"`
	DeployKubeVIP     bool                  `json:"deploy_kube_vip,omitempty"`
	ControlPlaneVIP   string                `json:"control_plane_vip"`
	PodCIDR           string                `json:"pod_cidr"`
	ServiceCIDR       string                `json:"service_cidr"`
	CNI               string                `json:"cni"`
	NodeIPs           map[string]string     `json:"node_ips"`
	Apply             bool                  `json:"apply"`
	MetalLB           *DeployMetalLBRequest `json:"metallb,omitempty"`
}

type BootstrapResponse struct {
	ClusterID      int64    `json:"cluster_id"`
	Status         string   `json:"status"`
	AnsibleWorkDir string   `json:"ansible_work_dir"`
	AnsibleOutput  string   `json:"ansible_output,omitempty"`
	Warnings       []string `json:"warnings"`
}

type BootstrapConfigRecord struct {
	ClusterID    int64         `json:"cluster_id"`
	SSHUser      string        `json:"ssh_user"`
	LastSSHKeyID sql.NullInt64 `json:"last_ssh_key_id"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type BootstrapSSHKeyRecord struct {
	ID        int64     `json:"id"`
	ClusterID int64     `json:"cluster_id"`
	KeyName   string    `json:"key_name"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ClusterNodeRecord struct {
	ID                  int64  `json:"id"`
	Role                string `json:"role"`
	RoleIndex           int    `json:"role_index"`
	VMName              string `json:"vm_name"`
	IPAddress           string `json:"ip_address"`
	ProxmoxNodeName     string `json:"proxmox_node_name"`
	ProxmoxCredentialID int64  `json:"proxmox_credential_id"`
}

type ClusterSummary struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	TopologyType      string    `json:"topology_type"`
	KubernetesVersion string    `json:"kubernetes_version"`
	NetworkProfileID  int64     `json:"network_profile_id"`
	ControlPlaneCount int       `json:"control_plane_count"`
	WorkerCount       int       `json:"worker_count"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ClusterPowerActionResponse struct {
	ClusterID int64    `json:"cluster_id"`
	Action    string   `json:"action"`
	Status    string   `json:"status"`
	Nodes     []string `json:"nodes"`
	Warnings  []string `json:"warnings"`
}

type ClusterRedeployResponse struct {
	ClusterID int64    `json:"cluster_id"`
	Action    string   `json:"action"`
	Status    string   `json:"status"`
	WorkDirs  []string `json:"work_dirs"`
	Warnings  []string `json:"warnings"`
}

type clusterDeleteInfo struct {
	Name                string
	ProxmoxCredentialID sql.NullInt64
	TerraformWorkDir    string
}

type ProvisionRunRecord struct {
	ID         int64      `json:"id"`
	Action     string     `json:"action"`
	Status     string     `json:"status"`
	WorkDir    string     `json:"work_dir"`
	Output     string     `json:"output"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type ClusterOperation struct {
	ID              int64      `json:"id"`
	ClusterID       int64      `json:"cluster_id"`
	Action          string     `json:"action"`
	Status          string     `json:"status"`
	CurrentStep     string     `json:"current_step"`
	ProgressPercent int        `json:"progress_percent"`
	ErrorSummary    string     `json:"error_summary,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

type ClusterOperationEvent struct {
	ID          int64     `json:"id"`
	OperationID int64     `json:"operation_id"`
	Step        string    `json:"step"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"created_at"`
}

type ScaleRequest struct {
	TargetWorkerCount int `json:"target_worker_count"`
}

type ScaleResponse struct {
	ClusterID           int64    `json:"cluster_id"`
	PreviousWorkerCount int      `json:"previous_worker_count"`
	NewWorkerCount      int      `json:"new_worker_count"`
	AddedNodes          []string `json:"added_nodes"`
	RemovedNodes        []string `json:"removed_nodes"`
	Warnings            []string `json:"warnings"`
}

type UpgradeRequest struct {
	TargetKubernetesVersion string `json:"target_kubernetes_version"`
}

type UpgradeResponse struct {
	ClusterID       int64  `json:"cluster_id"`
	PreviousVersion string `json:"previous_version"`
	TargetVersion   string `json:"target_version"`
	Status          string `json:"status"`
	Warning         string `json:"warning,omitempty"`
}

type ReplaceNodeRequest struct {
	FailedVMName       string `json:"failed_vm_name"`
	NewProxmoxNodeName string `json:"new_proxmox_node_name"`
	NewIPAddress       string `json:"new_ip_address"`
}

type ReplaceNodeResponse struct {
	ClusterID int64  `json:"cluster_id"`
	OldVMName string `json:"old_vm_name"`
	NewVMName string `json:"new_vm_name"`
	Status    string `json:"status"`
	Warning   string `json:"warning,omitempty"`
}

type RotateKubeconfigRequest struct {
	TTLHours int `json:"ttl_hours"`
}

type RotateKubeconfigResponse struct {
	ClusterID int64     `json:"cluster_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
}

type DeployObservabilityRequest struct {
	GrafanaAdminUser     string `json:"grafana_admin_user"`
	GrafanaAdminPassword string `json:"grafana_admin_password"`
	RetentionDays        int    `json:"retention_days"`
	StorageClass         string `json:"storage_class"`
	Apply                bool   `json:"apply"`
}

type DeployIngressControllerRequest struct {
	Namespace string `json:"namespace"`
	Image     string `json:"image"`
	Apply     bool   `json:"apply"`
	Wait      bool   `json:"wait"`
}

type DeployIngressControllerResponse struct {
	ClusterID int64    `json:"cluster_id"`
	Status    string   `json:"status"`
	Namespace string   `json:"namespace"`
	Image     string   `json:"image"`
	Output    string   `json:"output,omitempty"`
	Warnings  []string `json:"warnings"`
}

type DeployCertificateManagerRequest struct {
	Namespace    string `json:"namespace"`
	Version      string `json:"version"`
	IssuerName   string `json:"issuer_name"`
	Email        string `json:"email"`
	ACMEServer   string `json:"acme_server"`
	IngressClass string `json:"ingress_class"`
	Apply        bool   `json:"apply"`
	Wait         bool   `json:"wait"`
}

type DeployCertificateManagerResponse struct {
	ClusterID  int64    `json:"cluster_id"`
	Status     string   `json:"status"`
	Namespace  string   `json:"namespace"`
	Version    string   `json:"version"`
	IssuerName string   `json:"issuer_name"`
	ACMEServer string   `json:"acme_server"`
	Output     string   `json:"output,omitempty"`
	Warnings   []string `json:"warnings"`
}

type DeployAppRequest struct {
	Name                 string  `json:"name"`
	Namespace            string  `json:"namespace"`
	Manifest             string  `json:"manifest"`
	Hostname             string  `json:"hostname"`
	IngressClass         string  `json:"ingress_class"`
	TLSSecretName        string  `json:"tls_secret_name"`
	TLSCertificateID     int64   `json:"tls_certificate_id"`
	TLSFullchainPEM      string  `json:"tls_fullchain_pem"`
	TLSPrivateKeyPEM     string  `json:"tls_private_key_pem"`
	SelectedSecretIDs    []int64 `json:"selected_secret_ids"`
	ApplySelectedSecrets bool    `json:"apply_selected_secrets"`
	SelectedConfigMapIDs []int64 `json:"selected_config_map_ids"`
	ApplyConfigMaps      bool    `json:"apply_config_maps"`
	Apply                bool    `json:"apply"`
}

type DeployAppResponse struct {
	ClusterID      int64    `json:"cluster_id"`
	Status         string   `json:"status"`
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	AppID          int64    `json:"app_id,omitempty"`
	RevisionID     int64    `json:"revision_id,omitempty"`
	Revision       int      `json:"revision,omitempty"`
	Hostname       string   `json:"hostname,omitempty"`
	IngressClass   string   `json:"ingress_class,omitempty"`
	TLSSecretName  string   `json:"tls_secret_name,omitempty"`
	SecretNames    []string `json:"secret_names,omitempty"`
	ConfigMapNames []string `json:"config_map_names,omitempty"`
	Output         string   `json:"output,omitempty"`
	Warnings       []string `json:"warnings"`
}

type ClusterSecretItem struct {
	Key string `json:"key"`
}

type ClusterSecretRecord struct {
	ID             int64                  `json:"id"`
	ClusterID      int64                  `json:"cluster_id"`
	Name           string                 `json:"name"`
	Namespace      string                 `json:"namespace"`
	Provider       string                 `json:"provider"`
	Type           string                 `json:"type"`
	Description    string                 `json:"description"`
	ExternalRef    string                 `json:"external_ref,omitempty"`
	ProviderConfig map[string]interface{} `json:"provider_config,omitempty"`
	Immutable      bool                   `json:"immutable"`
	IsGlobal       bool                   `json:"is_global"`
	Keys           []string               `json:"keys"`
	LastAppliedAt  *time.Time             `json:"last_applied_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type UpsertClusterSecretRequest struct {
	Name           string                 `json:"name"`
	Namespace      string                 `json:"namespace"`
	Provider       string                 `json:"provider"`
	Type           string                 `json:"type"`
	Description    string                 `json:"description"`
	ExternalRef    string                 `json:"external_ref"`
	ProviderConfig map[string]interface{} `json:"provider_config"`
	Immutable      bool                   `json:"immutable"`
	IsGlobal       bool                   `json:"is_global"`
	Data           map[string]string      `json:"data"`
}

type ApplyClusterSecretResponse struct {
	ClusterID int64    `json:"cluster_id"`
	SecretID  int64    `json:"secret_id"`
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Provider  string   `json:"provider"`
	Status    string   `json:"status"`
	Output    string   `json:"output,omitempty"`
	Warnings  []string `json:"warnings"`
}

type ClusterConfigMapRecord struct {
	ID            int64             `json:"id"`
	ClusterID     int64             `json:"cluster_id"`
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Description   string            `json:"description"`
	Data          map[string]string `json:"data"`
	Immutable     bool              `json:"immutable"`
	IsGlobal      bool              `json:"is_global"`
	LastAppliedAt *time.Time        `json:"last_applied_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type UpsertClusterConfigMapRequest struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Description string            `json:"description"`
	Data        map[string]string `json:"data"`
	Immutable   bool              `json:"immutable"`
	IsGlobal    bool              `json:"is_global"`
}

type UpdateSecretKeyRequest struct {
	Value string `json:"value"`
}

type UpdateConfigMapKeyRequest struct {
	Value string `json:"value"`
}

type ApplyClusterConfigMapResponse struct {
	ClusterID   int64    `json:"cluster_id"`
	ConfigMapID int64    `json:"config_map_id"`
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Status      string   `json:"status"`
	Output      string   `json:"output,omitempty"`
	Warnings    []string `json:"warnings"`
}

type KubectlCommandRequest struct {
	Command string `json:"command"`
}

type KubectlCommandRun struct {
	ID         string     `json:"id"`
	ClusterID  int64      `json:"cluster_id"`
	Command    string     `json:"command"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type kubectlCommandRunState struct {
	KubectlCommandRun
	outputMu sync.RWMutex
}

type ApplicationManifestRecord struct {
	ID               int64     `json:"id"`
	ClusterID        int64     `json:"cluster_id"`
	Name             string    `json:"name"`
	Namespace        string    `json:"namespace"`
	Hostname         string    `json:"hostname"`
	IngressClass     string    `json:"ingress_class"`
	TLSSecretName    string    `json:"tls_secret_name"`
	TLSCertificateID int64     `json:"tls_certificate_id,omitempty"`
	LatestRevisionID int64     `json:"latest_revision_id,omitempty"`
	LatestRevision   int       `json:"latest_revision,omitempty"`
	LatestStatus     string    `json:"latest_status,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ApplicationManifestRevisionRecord struct {
	ID                    int64     `json:"id"`
	ApplicationManifestID int64     `json:"application_manifest_id"`
	ClusterID             int64     `json:"cluster_id"`
	RevisionNumber        int       `json:"revision_number"`
	RawManifest           string    `json:"raw_manifest"`
	RenderedManifest      string    `json:"rendered_manifest"`
	KubectlOutput         string    `json:"kubectl_output"`
	Status                string    `json:"status"`
	ErrorMessage          string    `json:"error_message"`
	Apply                 bool      `json:"apply"`
	CreatedAt             time.Time `json:"created_at"`
}

type RedeployAppRequest struct {
	RevisionID int64 `json:"revision_id,omitempty"`
	Apply      bool  `json:"apply"`
}

type MetalLBAddressPool struct {
	Name          string   `json:"name"`
	Addresses     []string `json:"addresses"`
	AutoAssign    bool     `json:"auto_assign"`
	AvoidBuggyIPs bool     `json:"avoid_buggy_ips"`
}

type DeployMetalLBRequest struct {
	Namespace    string               `json:"namespace"`
	Version      string               `json:"version"`
	AddressPools []MetalLBAddressPool `json:"address_pools"`
	Interfaces   []string             `json:"interfaces"`
	Apply        bool                 `json:"apply"`
}

type MetalLBCommands struct {
	PowerShell string `json:"powershell"`
	Shell      string `json:"shell"`
}

type DeployMetalLBResponse struct {
	ClusterID   int64           `json:"cluster_id"`
	Status      string          `json:"status"`
	Namespace   string          `json:"namespace"`
	Version     string          `json:"version"`
	ManifestDir string          `json:"manifest_dir"`
	Commands    MetalLBCommands `json:"commands"`
	Warnings    []string        `json:"warnings"`
}

type MetalLBStatusResponse struct {
	ClusterID    int64                `json:"cluster_id"`
	Namespace    string               `json:"namespace"`
	Version      string               `json:"version"`
	AddressPools []MetalLBAddressPool `json:"address_pools"`
	Interfaces   []string             `json:"interfaces"`
	Status       string               `json:"status"`
	ManifestDir  string               `json:"manifest_dir"`
	Commands     MetalLBCommands      `json:"commands"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

type DeployObservabilityResponse struct {
	ClusterID    int64    `json:"cluster_id"`
	Status       string   `json:"status"`
	DashboardURL string   `json:"dashboard_url"`
	ManifestDir  string   `json:"manifest_dir"`
	Warnings     []string `json:"warnings"`
}

type ObservabilityStatusResponse struct {
	ClusterID        int64     `json:"cluster_id"`
	GrafanaAdminUser string    `json:"grafana_admin_user"`
	RetentionDays    int       `json:"retention_days"`
	StorageClass     string    `json:"storage_class"`
	Status           string    `json:"status"`
	DashboardURL     string    `json:"dashboard_url"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateEtcdSnapshotRequest struct {
	StorageTarget string `json:"storage_target"`
	Apply         bool   `json:"apply"`
}

type CreateEtcdSnapshotResponse struct {
	ClusterID    int64     `json:"cluster_id"`
	SnapshotID   int64     `json:"snapshot_id"`
	SnapshotName string    `json:"snapshot_name"`
	SnapshotPath string    `json:"snapshot_path"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	Warning      string    `json:"warning,omitempty"`
}

type EtcdSnapshotRecord struct {
	ID            int64     `json:"id"`
	SnapshotName  string    `json:"snapshot_name"`
	StorageTarget string    `json:"storage_target"`
	SnapshotPath  string    `json:"snapshot_path"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type RestoreEtcdSnapshotRequest struct {
	SnapshotID int64 `json:"snapshot_id"`
	Apply      bool  `json:"apply"`
}

type RestoreEtcdSnapshotResponse struct {
	ClusterID  int64  `json:"cluster_id"`
	SnapshotID int64  `json:"snapshot_id"`
	Status     string `json:"status"`
	Warning    string `json:"warning,omitempty"`
}

// Health check and recovery types
type ClusterNodeHealthRecord struct {
	ID                 int64     `json:"id"`
	ClusterID          int64     `json:"cluster_id"`
	ClusterNodeID      int64     `json:"cluster_node_id"`
	VMName             string    `json:"vm_name"`
	Role               string    `json:"role"`
	HealthStatus       string    `json:"health_status"` // healthy, unhealthy, missing, unknown
	LastCheckAt        time.Time `json:"last_check_at"`
	ConsecutiveFailure int       `json:"consecutive_failures"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ClusterRecoveryConfig struct {
	ClusterID             int64 `json:"cluster_id"`
	AutoRecoveryEnabled   bool  `json:"auto_recovery_enabled"`
	HealthCheckInterval   int   `json:"health_check_interval_seconds"`
	FailureThresholdCount int   `json:"failure_threshold_count"`
}

type ClusterRecoveryEvent struct {
	ID            int64         `json:"id"`
	ClusterID     int64         `json:"cluster_id"`
	ClusterNodeID sql.NullInt64 `json:"cluster_node_id"`
	EventType     string        `json:"event_type"` // health_check_started, health_check_failed, recovery_triggered, recovery_completed, recovery_failed
	VMName        string        `json:"vm_name"`
	Role          string        `json:"role"`
	Status        string        `json:"status"`
	Message       string        `json:"message"`
	CreatedAt     time.Time     `json:"created_at"`
}

type NodeHealthStatus struct {
	ClusterID           int64     `json:"cluster_id"`
	NodeID              int64     `json:"node_id"`
	VMName              string    `json:"vm_name"`
	Role                string    `json:"role"`
	HealthStatus        string    `json:"health_status"`
	LastCheckAt         time.Time `json:"last_check_at"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	RecoveryEligible    bool      `json:"recovery_eligible"`
	RecommendedAction   string    `json:"recommended_action"`
}

type ClusterHealthResponse struct {
	ClusterID      int64              `json:"cluster_id"`
	OverallStatus  string             `json:"overall_status"` // healthy, degraded, critical
	HealthyNodes   int                `json:"healthy_nodes"`
	UnhealthyNodes int                `json:"unhealthy_nodes"`
	MissingNodes   int                `json:"missing_nodes"`
	NodeStatuses   []NodeHealthStatus `json:"node_statuses"`
	RecoveryStatus string             `json:"recovery_status"` // idle, in-progress
	LastCheckAt    time.Time          `json:"last_check_at"`
}

type TriggerRecoveryRequest struct {
	VMName string `json:"vm_name"`
	Force  bool   `json:"force,omitempty"`
}

type TriggerRecoveryResponse struct {
	ClusterID int64  `json:"cluster_id"`
	OldVMName string `json:"old_vm_name"`
	NewVMName string `json:"new_vm_name"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

type provisionApplyWork struct {
	CredentialID int64
	Workspace    string
}

func NewService(
	db *sql.DB,
	networks NetworkStore,
	planner TopologyPlanner,
	terraformRunner *terraform.Runner,
	ansibleRunner *ansible.Runner,
	proxmoxSvc ProxmoxCredentialStore,
	sshKeys SSHKeyPairStore,
	tlsCertificates TLSCertificateStore,
	secretStore secrets.Store,
	operationsDir string,
	autoApply bool,
	autoBootstrap bool,
) *Service {
	operationsDir = strings.TrimSpace(operationsDir)
	if operationsDir == "" {
		operationsDir = filepath.Join("data", "operations", "runs")
	}

	return &Service{
		db:              db,
		networks:        networks,
		planner:         planner,
		terraformRunner: terraformRunner,
		ansibleRunner:   ansibleRunner,
		proxmoxSvc:      proxmoxSvc,
		sshKeys:         sshKeys,
		tlsCertificates: tlsCertificates,
		secrets:         secretStore,
		operationsDir:   operationsDir,
		autoApply:       autoApply,
		autoBootstrap:   autoBootstrap,
		kubectlRuns:     make(map[string]*kubectlCommandRunState),
	}
}

func (s *Service) Provision(req ProvisionRequest) (ProvisionResponse, error) {
	if err := s.resolveProvisionSSHKey(&req); err != nil {
		return ProvisionResponse{}, err
	}

	if err := validateProvisionRequest(req); err != nil {
		return ProvisionResponse{}, err
	}

	plan, err := s.planner.Plan(topology.PlanRequest{
		TopologyType:      req.TopologyType,
		ControlPlaneCount: req.ControlPlaneCount,
		WorkerCount:       req.WorkerCount,
	})
	if err != nil {
		return ProvisionResponse{}, err
	}

	nodeCredentialIDs, clusterCredentialID, err := resolveProvisionNodeCredentials(req, plan)
	if err != nil {
		return ProvisionResponse{}, err
	}
	req.ProxmoxCredentialID = clusterCredentialID

	nodeTagMap, err := s.networks.ListNodeTagMap()
	if err != nil {
		return ProvisionResponse{}, fmt.Errorf("load node network tags: %w", err)
	}
	nodeNetworkProfileIDs, err := resolveProvisionNodeNetworkProfiles(req, plan, nodeTagMap)
	if err != nil {
		return ProvisionResponse{}, err
	}

	primaryNetworkProfileID := req.NetworkProfileID
	if primaryNetworkProfileID <= 0 {
		for _, profileID := range nodeNetworkProfileIDs {
			if primaryNetworkProfileID <= 0 || profileID < primaryNetworkProfileID {
				primaryNetworkProfileID = profileID
			}
		}
	}
	if primaryNetworkProfileID <= 0 {
		return ProvisionResponse{}, errors.New("unable to resolve effective network profile")
	}
	req.NetworkProfileID = primaryNetworkProfileID

	networkProfile, err := s.networks.GetByID(primaryNetworkProfileID)
	if err != nil {
		return ProvisionResponse{}, fmt.Errorf("load network profile: %w", err)
	}

	networkProfilesByID := map[int64]networks.Profile{networkProfile.ID: networkProfile}
	for _, profileID := range nodeNetworkProfileIDs {
		if _, exists := networkProfilesByID[profileID]; exists {
			continue
		}
		p, loadErr := s.networks.GetByID(profileID)
		if loadErr != nil {
			return ProvisionResponse{}, fmt.Errorf("load node network profile %d: %w", profileID, loadErr)
		}
		networkProfilesByID[profileID] = p
	}

	clusterID, err := s.insertCluster(req, plan)
	if err != nil {
		return ProvisionResponse{}, err
	}

	vms := make([]terraform.VMNode, 0, len(plan.Placements))
	iteratorsByNode := make(map[string]*ipIterator)
	assignedIPs := make(map[string]struct{})
	poolWarnings := make([]string, 0)
	for profileID := range networkProfilesByID {
		reserved, reservedErr := s.networks.GetReservedIPsByNetworkProfileID(profileID)
		if reservedErr != nil {
			return ProvisionResponse{}, fmt.Errorf("load reserved ips for network profile %d: %w", profileID, reservedErr)
		}
		for _, item := range reserved {
			trimmed := strings.TrimSpace(item.IPAddress)
			if trimmed != "" {
				assignedIPs[trimmed] = struct{}{}
			}
		}
	}
	for _, placement := range plan.Placements {
		vmName := buildVMName(req.Name, placement.Role, placement.Index)
		nodeName := strings.TrimSpace(placement.ProxmoxNodeName)
		credentialID := nodeCredentialIDs[nodeName]
		networkProfileID := nodeNetworkProfileIDs[nodeName]
		networkCfg, exists := networkProfilesByID[networkProfileID]
		if !exists {
			return ProvisionResponse{}, fmt.Errorf("missing network profile %d for proxmox node %s", networkProfileID, nodeName)
		}

		iter := iteratorsByNode[nodeName]
		if iter == nil {
			poolStart := strings.TrimSpace(req.NodeIPPoolStartByNode[nodeName])
			if poolStart == "" {
				poolStart = req.NodeIPPoolStart
			}
			adjustedPoolStart, adjustErr := alignIPToCIDR(poolStart, networkCfg.CIDR)
			if adjustErr != nil {
				return ProvisionResponse{}, fmt.Errorf("invalid IP pool for proxmox node %s: %w", nodeName, adjustErr)
			}
			if adjustedPoolStart != strings.TrimSpace(poolStart) {
				poolWarnings = append(poolWarnings, fmt.Sprintf("Adjusted IP pool start for %s from %s to %s to match %s", nodeName, strings.TrimSpace(poolStart), adjustedPoolStart, networkCfg.CIDR))
			}
			it, iterErr := newIPIterator(adjustedPoolStart, networkCfg.CIDR, networkCfg.Gateway, assignedIPs)
			if iterErr != nil {
				return ProvisionResponse{}, fmt.Errorf("invalid IP pool for proxmox node %s: %w", nodeName, iterErr)
			}
			iteratorsByNode[nodeName] = it
			iter = it
		}

		assignedIP, err := iter.nextAvailable()
		if err != nil {
			return ProvisionResponse{}, fmt.Errorf("ip pool exhausted for %s: %w", vmName, err)
		}

		vms = append(vms, terraform.VMNode{
			VMName:          vmName,
			Role:            placement.Role,
			RoleIndex:       placement.Index,
			ProxmoxNodeName: placement.ProxmoxNodeName,
			CPU:             pickCPU(req, placement.Role),
			MemoryMB:        pickMemory(req, placement.Role),
			DiskGB:          pickDisk(req, placement.Role),
			IPAddress:       assignedIP,
			NetworkCIDR:     networkCfg.CIDR,
			Gateway:         networkCfg.Gateway,
			Bridge:          networkCfg.Bridge,
			VLAN:            networkCfg.VLAN,
		})

		if err := s.insertClusterNode(clusterID, placement, vmName, assignedIP, credentialID); err != nil {
			return ProvisionResponse{}, err
		}
	}

	status := "prepared"
	terraformOutput := ""
	warnings := append([]string{}, poolWarnings...)
	warnings = append(warnings, plan.Warnings...)
	workspace := ""

	vmGroups, err := groupVMsByCredential(vms, nodeCredentialIDs)
	if err != nil {
		return ProvisionResponse{}, err
	}
	effectiveCredentialIDs := sortedCredentialIDs(vmGroups)
	if len(effectiveCredentialIDs) == 0 {
		return ProvisionResponse{}, errors.New("unable to resolve credential groups for provisioning")
	}

	applyNow := req.Apply || s.autoApply
	if applyNow {
		operationID, opErr := s.startClusterOperation(clusterID, "provision", "prepare-workspaces", 5)
		if opErr != nil {
			return ProvisionResponse{}, opErr
		}
		_ = s.appendOperationEvent(operationID, "prepare-workspaces", "info", "Preparing Terraform workspaces")

		applyWork := make([]provisionApplyWork, 0, len(effectiveCredentialIDs))
		for _, credentialID := range effectiveCredentialIDs {
			groupNodes := vmGroups[credentialID]
			workspaceKey := req.Name
			if len(effectiveCredentialIDs) > 1 {
				workspaceKey = workspaceKeyForCredential(req.Name, credentialID)
			}

			groupWorkspace, prepErr := s.terraformRunner.PrepareWorkspace(terraform.WorkspaceSpec{
				WorkspaceKey:      workspaceKey,
				ClusterName:       req.Name,
				KubernetesVersion: req.KubernetesVersion,
				NetworkCIDR:       networkProfile.CIDR,
				Gateway:           networkProfile.Gateway,
				DNS:               networkProfile.DNS,
				Bridge:            networkProfile.Bridge,
				VLAN:              networkProfile.VLAN,
				UbuntuImage:       req.UbuntuImage,
				SSHPublicKey:      req.SSHPublicKey,
				Nodes:             groupNodes,
			})
			if prepErr != nil {
				_ = s.updateClusterOperation(operationID, "failed", "prepare-workspaces", 100, prepErr.Error())
				_ = s.appendOperationEvent(operationID, "prepare-workspaces", "error", prepErr.Error())
				return ProvisionResponse{}, prepErr
			}
			_ = s.appendOperationEvent(operationID, "prepare-workspaces", "info", fmt.Sprintf("Workspace prepared for credential_id=%d", credentialID))
			if workspace == "" {
				workspace = groupWorkspace
			}
			applyWork = append(applyWork, provisionApplyWork{CredentialID: credentialID, Workspace: groupWorkspace})
		}

		status = "provisioning"
		warnings = append(warnings, "terraform apply is running in background; check cluster run history for progress")
		if err := s.insertProvisionRun(clusterID, "provision", status, workspace, ""); err != nil {
			return ProvisionResponse{}, err
		}
		if err := s.updateClusterStatus(clusterID, status); err != nil {
			return ProvisionResponse{}, err
		}
		_ = s.updateClusterOperation(operationID, "running", "terraform-apply", 15, "")
		_ = s.appendOperationEvent(operationID, "terraform-apply", "info", "Terraform apply started")

		go s.runProvisionApplyAsync(clusterID, applyWork, workspace, operationID)

		return ProvisionResponse{
			ClusterID:        clusterID,
			ClusterName:      req.Name,
			Status:           status,
			Topology:         plan,
			TerraformWorkDir: workspace,
			TerraformOutput:  terraformOutput,
			Warnings:         warnings,
		}, nil
	} else {
		workspace, err = s.terraformRunner.PrepareWorkspace(terraform.WorkspaceSpec{
			ClusterName:       req.Name,
			KubernetesVersion: req.KubernetesVersion,
			NetworkCIDR:       networkProfile.CIDR,
			Gateway:           networkProfile.Gateway,
			DNS:               networkProfile.DNS,
			Bridge:            networkProfile.Bridge,
			VLAN:              networkProfile.VLAN,
			UbuntuImage:       req.UbuntuImage,
			SSHPublicKey:      req.SSHPublicKey,
			Nodes:             vms,
		})
		if err != nil {
			return ProvisionResponse{}, err
		}
	}

	if err := s.insertProvisionRun(clusterID, "provision", status, workspace, terraformOutput); err != nil {
		return ProvisionResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return ProvisionResponse{}, err
	}

	return ProvisionResponse{
		ClusterID:        clusterID,
		ClusterName:      req.Name,
		Status:           status,
		Topology:         plan,
		TerraformWorkDir: workspace,
		TerraformOutput:  terraformOutput,
		Warnings:         warnings,
	}, nil
}

func (s *Service) ProvisionAndBootstrap(req ProvisionAndBootstrapRequest) (ProvisionAndBootstrapResponse, error) {
	provisionReq := req.Provision
	provisionReq.Apply = true

	bootstrapReq := req.Bootstrap
	bootstrapReq.Apply = true
	if bootstrapReq.MetalLB != nil {
		metallbReq := *bootstrapReq.MetalLB
		metallbReq.Apply = true
		if _, _, _, _, err := normalizeMetalLBRequest(metallbReq); err != nil {
			return ProvisionAndBootstrapResponse{}, fmt.Errorf("bootstrap.metallb: %w", err)
		}
		bootstrapReq.MetalLB = &metallbReq
	}

	if strings.TrimSpace(bootstrapReq.SSHUser) == "" {
		return ProvisionAndBootstrapResponse{}, errors.New("bootstrap.ssh_user is required for provision-and-bootstrap")
	}
	if bootstrapReq.SSHKeyPairID <= 0 &&
		bootstrapReq.SSHKeyID <= 0 &&
		strings.TrimSpace(bootstrapReq.SSHPrivateKey) == "" &&
		strings.TrimSpace(bootstrapReq.SSHPrivateKeyPath) == "" &&
		strings.TrimSpace(bootstrapReq.SSHKeyName) == "" {
		return ProvisionAndBootstrapResponse{}, errors.New("bootstrap ssh key is required: set bootstrap.ssh_key_pair_id or bootstrap.ssh_private_key")
	}

	provisionResp, err := s.Provision(provisionReq)
	if err != nil {
		return ProvisionAndBootstrapResponse{}, err
	}

	go s.runBootstrapAfterProvision(provisionResp.ClusterID, bootstrapReq)

	warnings := append([]string{}, provisionResp.Warnings...)
	warnings = append(warnings, "bootstrap will start automatically after terraform apply succeeds")
	if bootstrapReq.MetalLB != nil {
		warnings = append(warnings, "metallb will deploy automatically after bootstrap succeeds and kubeconfig is exported")
	}

	return ProvisionAndBootstrapResponse{
		ClusterID:   provisionResp.ClusterID,
		ClusterName: provisionResp.ClusterName,
		Status:      "provisioning-then-bootstrap",
		Warnings:    warnings,
	}, nil
}

func (s *Service) runBootstrapAfterProvision(clusterID int64, req BootstrapRequest) {
	if clusterID <= 0 {
		return
	}

	_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "info", "Auto-bootstrap enabled; waiting for provision apply to complete before starting bootstrap")

	const maxWait = 30 * time.Minute
	const interval = 3 * time.Second
	deadline := time.Now().Add(maxWait)

	for {
		summary, err := s.getClusterSummary(clusterID)
		if err != nil {
			log.Printf("ERROR waiting for provision completion for cluster %d: %v", clusterID, err)
			_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "error", fmt.Sprintf("Error waiting for provision completion: %v", err))
			return
		}

		status := strings.TrimSpace(strings.ToLower(summary.Status))
		switch status {
		case "applied":
			_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "info", "Provision apply completed; starting bootstrap now")
			_, bootErr := s.Bootstrap(clusterID, req)
			if bootErr != nil {
				log.Printf("ERROR auto-bootstrap for cluster %d: %v", clusterID, bootErr)
				_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "error", fmt.Sprintf("Auto-bootstrap failed to start: %v", bootErr))
			}
			return
		case "failed", "destroy-failed":
			log.Printf("INFO skip auto-bootstrap for cluster %d due to status %s", clusterID, status)
			_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "warn", fmt.Sprintf("Skipping auto-bootstrap because provision status is %s", status))
			return
		}

		if time.Now().After(deadline) {
			log.Printf("ERROR timeout waiting for provision apply before bootstrap for cluster %d", clusterID)
			_ = s.appendLatestOperationEvent(clusterID, "auto-bootstrap", "error", "Timed out waiting for provision apply to complete before bootstrap")
			return
		}

		time.Sleep(interval)
	}
}

func (s *Service) appendLatestOperationEvent(clusterID int64, step, level, message string) error {
	op, err := s.GetLatestOperation(clusterID)
	if err != nil {
		return err
	}
	return s.appendOperationEvent(op.ID, step, level, message)
}

func (s *Service) runProvisionApplyAsync(clusterID int64, applyWork []provisionApplyWork, primaryWorkspace string, operationID int64) {
	outputs := make([]string, 0, len(applyWork))
	for index, work := range applyWork {
		progress := 15
		if len(applyWork) > 0 {
			progress = 15 + int(float64(index)/float64(len(applyWork))*70)
		}
		stepLabel := fmt.Sprintf("apply-credential-%d", work.CredentialID)
		_ = s.updateClusterOperation(operationID, "running", stepLabel, progress, "")
		_ = s.appendOperationEvent(operationID, stepLabel, "info", fmt.Sprintf("Applying Terraform for credential_id=%d", work.CredentialID))

		cred, credErr := s.proxmoxSvc.GetCredentialWithSecrets(work.CredentialID)
		if credErr != nil {
			failureOutput := strings.Join(append(outputs, fmt.Sprintf("load proxmox credential %d failed: %v", work.CredentialID, credErr)), "\n\n")
			_ = s.updateClusterOperation(operationID, "failed", stepLabel, progress, credErr.Error())
			_ = s.appendOperationEvent(operationID, stepLabel, "error", fmt.Sprintf("Credential load failed: %v", credErr))
			if dbErr := s.insertProvisionRun(clusterID, "apply", "failed", primaryWorkspace, failureOutput); dbErr != nil {
				log.Printf("ERROR saving failed apply run for cluster %d: %v", clusterID, dbErr)
			}
			if dbErr := s.updateClusterStatus(clusterID, "failed"); dbErr != nil {
				log.Printf("ERROR updating cluster %d status to failed: %v", clusterID, dbErr)
			}
			return
		}

		auth := terraform.ProxmoxAuth{
			Endpoint:       cred.Endpoint,
			Username:       cred.Username,
			Password:       cred.Password,
			APITokenID:     cred.APITokenID,
			APITokenSecret: cred.APITokenSecret,
			InsecureTLS:    cred.InsecureTLS,
			SSHUsername:    sshUserFrom(cred),
			SSHPassword:    cred.Password,
		}

		runResult, runErr := s.terraformRunner.Apply(work.Workspace, auth)
		outputs = append(outputs, fmt.Sprintf("### credential_id=%d workspace=%s\n%s", work.CredentialID, work.Workspace, strings.TrimSpace(runResult.Output)))
		if runErr != nil {
			failureOutput := strings.Join(outputs, "\n\n")
			_ = s.updateClusterOperation(operationID, "failed", stepLabel, progress, runErr.Error())
			_ = s.appendOperationEvent(operationID, stepLabel, "error", fmt.Sprintf("Terraform apply failed: %v", runErr))
			if dbErr := s.insertProvisionRun(clusterID, "apply", "failed", primaryWorkspace, failureOutput); dbErr != nil {
				log.Printf("ERROR saving failed apply run for cluster %d: %v", clusterID, dbErr)
			}
			if dbErr := s.updateClusterStatus(clusterID, "failed"); dbErr != nil {
				log.Printf("ERROR updating cluster %d status to failed: %v", clusterID, dbErr)
			}
			return
		}
		_ = s.appendOperationEvent(operationID, stepLabel, "info", fmt.Sprintf("Credential_id=%d apply completed", work.CredentialID))
	}

	finalOutput := strings.Join(outputs, "\n\n")
	_ = s.updateClusterOperation(operationID, "succeeded", "completed", 100, "")
	_ = s.appendOperationEvent(operationID, "completed", "info", "Provisioning apply completed successfully")
	if dbErr := s.insertProvisionRun(clusterID, "apply", "applied", primaryWorkspace, finalOutput); dbErr != nil {
		log.Printf("ERROR saving applied run for cluster %d: %v", clusterID, dbErr)
	}
	if dbErr := s.updateClusterStatus(clusterID, "applied"); dbErr != nil {
		log.Printf("ERROR updating cluster %d status to applied: %v", clusterID, dbErr)
	}
}

func (s *Service) Bootstrap(clusterID int64, req BootstrapRequest) (BootstrapResponse, error) {
	if s.ansibleRunner == nil {
		return BootstrapResponse{}, errors.New("ansible runner is not configured")
	}
	if clusterID <= 0 {
		return BootstrapResponse{}, errors.New("cluster id is required")
	}
	if err := s.hydrateBootstrapSSHUser(&req); err != nil {
		return BootstrapResponse{}, err
	}
	if err := validateBootstrapRequest(req); err != nil {
		return BootstrapResponse{}, err
	}

	sshPrivateKey, keyID, err := s.resolveBootstrapSSHKey(clusterID, req)
	if err != nil {
		return BootstrapResponse{}, err
	}

	if err := s.upsertBootstrapConfig(clusterID, req.SSHUser, keyID); err != nil {
		return BootstrapResponse{}, err
	}

	clusterSummary, err := s.getClusterSummary(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}
	clusterName := clusterSummary.Name
	kubernetesVersion := clusterSummary.KubernetesVersion

	vip := strings.TrimSpace(req.ControlPlaneVIP)
	if vip != "" && clusterSummary.NetworkProfileID > 0 {
		reserved, reservedErr := s.networks.GetReservedIPsByNetworkProfileID(clusterSummary.NetworkProfileID)
		if reservedErr != nil {
			return BootstrapResponse{}, fmt.Errorf("load reserved ips for control-plane vip validation: %w", reservedErr)
		}
		for _, item := range reserved {
			if strings.TrimSpace(item.IPAddress) == vip {
				reason := strings.TrimSpace(item.Description)
				if reason == "" {
					reason = "no description"
				}
				return BootstrapResponse{}, fmt.Errorf("control_plane_vip %s is reserved (hostname=%s, description=%s)", vip, item.Hostname, reason)
			}
		}
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return BootstrapResponse{}, err
	}
	if len(nodes) == 0 {
		return BootstrapResponse{}, errors.New("cluster has no nodes")
	}

	ansibleNodes := make([]ansible.Node, 0, len(nodes))
	warnings := make([]string, 0)
	for _, n := range nodes {
		ip := strings.TrimSpace(n.IPAddress)
		if override, ok := req.NodeIPs[n.VMName]; ok && strings.TrimSpace(override) != "" {
			ip = strings.TrimSpace(override)
			if err := s.updateClusterNodeIP(n.ID, ip); err != nil {
				return BootstrapResponse{}, err
			}
		}
		if net.ParseIP(ip) == nil {
			if n.Role == "control-plane" {
				return BootstrapResponse{}, fmt.Errorf("missing or invalid ip for control-plane node %s; provide node_ips map", n.VMName)
			}
			warnings = append(warnings, fmt.Sprintf("skipping worker %s due to missing/invalid ip", n.VMName))
			continue
		}
		ansibleNodes = append(ansibleNodes, ansible.Node{Name: n.VMName, IP: ip, Role: n.Role})
	}

	controlPlaneCount := 0
	for _, n := range ansibleNodes {
		if n.Role == "control-plane" {
			controlPlaneCount++
		}
	}
	if controlPlaneCount == 0 {
		return BootstrapResponse{}, errors.New("no control-plane nodes with valid IPs available for bootstrap")
	}
	if controlPlaneCount > 1 && req.DeployKubeVIP && strings.TrimSpace(req.ControlPlaneVIP) == "" {
		return BootstrapResponse{}, errors.New("control_plane_vip is required when deploy_kube_vip is enabled for multi-control-plane bootstrap")
	}

	if controlPlaneCount > 1 && strings.TrimSpace(req.ControlPlaneVIP) != "" && !req.DeployKubeVIP {
		warnings = append(warnings, "control_plane_vip was provided while deploy_kube_vip is disabled; bootstrap will use the first control-plane node directly. Enable deploy_kube_vip to advertise the VIP before kubeadm init")
	}
	if strings.TrimSpace(req.ControlPlaneVIP) == "" {
		warnings = append(warnings, "control_plane_vip is empty; kubeadm init will use primary control-plane endpoint")
	}
	if req.MetalLB != nil {
		warnings = append(warnings, "metallb will deploy automatically after bootstrap succeeds and kubeconfig is exported")
	}

	workDir, err := s.ansibleRunner.PrepareWorkspace(ansible.BootstrapSpec{
		ClusterName:       clusterName,
		KubernetesVersion: kubernetesVersion,
		SSHSUser:          req.SSHUser,
		SSHPrivateKey:     sshPrivateKey,
		ControlPlaneVIP:   req.ControlPlaneVIP,
		DeployKubeVIP:     req.DeployKubeVIP,
		PodCIDR:           req.PodCIDR,
		ServiceCIDR:       req.ServiceCIDR,
		CNI:               req.CNI,
		Nodes:             ansibleNodes,
	})
	if err != nil {
		return BootstrapResponse{}, err
	}

	status := "bootstrap-prepared"
	ansibleOutput := ""
	applyNow := req.Apply || s.autoBootstrap
	if applyNow {
		operationID, opErr := s.startClusterOperation(clusterID, "bootstrap", "prepare-ansible", 10)
		if opErr != nil {
			return BootstrapResponse{}, opErr
		}
		_ = s.appendOperationEvent(operationID, "prepare-ansible", "info", "Preparing bootstrap inventory and playbook")

		status = "bootstrapping"
		_ = s.insertProvisionRun(clusterID, "bootstrap", status, workDir, "")
		_ = s.updateClusterStatus(clusterID, status)
		_ = s.updateClusterOperation(operationID, "running", "ansible-apply", 35, "")
		_ = s.appendOperationEvent(operationID, "ansible-apply", "info", "Ansible bootstrap started")

		go func() {
			runResult, runErr := s.ansibleRunner.Apply(workDir)
			runStatus := runResult.Status
			runOutput := runResult.Output
			if runErr != nil {
				_ = s.updateClusterOperation(operationID, "failed", "ansible-apply", 100, runErr.Error())
				_ = s.appendOperationEvent(operationID, "ansible-apply", "error", fmt.Sprintf("Ansible bootstrap failed: %v", runErr))
				log.Printf("ERROR bootstrap cluster %d: %v", clusterID, runErr)
				if dbErr := s.insertProvisionRun(clusterID, "bootstrap", runStatus, workDir, runOutput); dbErr != nil {
					log.Printf("ERROR saving bootstrap run for cluster %d: %v", clusterID, dbErr)
				}
				if dbErr := s.updateClusterStatus(clusterID, "failed"); dbErr != nil {
					log.Printf("ERROR updating cluster %d status to failed: %v", clusterID, dbErr)
				}
				return
			}
			_ = s.appendOperationEvent(operationID, "completed", "info", "Bootstrap completed successfully")
			log.Printf("INFO bootstrap cluster %d completed with status %s", clusterID, runStatus)
			if dbErr := s.insertProvisionRun(clusterID, "bootstrap", runStatus, workDir, runOutput); dbErr != nil {
				log.Printf("ERROR saving bootstrap run for cluster %d: %v", clusterID, dbErr)
			}
			exportPath, exportErr := s.exportBootstrapKubeconfig(workDir)
			if exportErr != nil {
				log.Printf("WARN export kubeconfig for cluster %d: %v", clusterID, exportErr)
				_ = s.appendOperationEvent(operationID, "export-kubeconfig", "warning", fmt.Sprintf("Failed to export kubeconfig: %v", exportErr))
			} else {
				_ = s.appendOperationEvent(operationID, "export-kubeconfig", "info", fmt.Sprintf("Kubeconfig exported to %s", exportPath))
			}
			finalStatus := runStatus
			if req.MetalLB != nil {
				if exportErr != nil {
					_ = s.appendOperationEvent(operationID, "metallb-deploy", "error", "Skipping MetalLB deployment because kubeconfig export failed")
				} else {
					_ = s.updateClusterOperation(operationID, "running", "metallb-deploy", 90, "")
					_ = s.appendOperationEvent(operationID, "metallb-deploy", "info", "Deploying MetalLB after bootstrap")
					metallbReq := *req.MetalLB
					metallbReq.Apply = true
					if _, deployErr := s.DeployMetalLB(clusterID, metallbReq); deployErr != nil {
						log.Printf("ERROR post-bootstrap metallb deployment for cluster %d: %v", clusterID, deployErr)
						_ = s.appendOperationEvent(operationID, "metallb-deploy", "error", fmt.Sprintf("MetalLB deployment failed: %v", deployErr))
					} else {
						finalStatus = "metallb-deployed"
						_ = s.appendOperationEvent(operationID, "metallb-deploy", "info", "MetalLB deployed successfully")
					}
				}
			}
			_ = s.updateClusterOperation(operationID, "succeeded", "completed", 100, "")
			if dbErr := s.updateClusterStatus(clusterID, finalStatus); dbErr != nil {
				log.Printf("ERROR updating cluster %d status: %v", clusterID, dbErr)
			}
		}()

		return BootstrapResponse{
			ClusterID:      clusterID,
			Status:         status,
			AnsibleWorkDir: workDir,
			Warnings:       warnings,
		}, nil
	}

	if err := s.insertProvisionRun(clusterID, "bootstrap", status, workDir, ansibleOutput); err != nil {
		return BootstrapResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return BootstrapResponse{}, err
	}

	return BootstrapResponse{
		ClusterID:      clusterID,
		Status:         status,
		AnsibleWorkDir: workDir,
		AnsibleOutput:  ansibleOutput,
		Warnings:       warnings,
	}, nil
}

func (s *Service) ListClusters() ([]ClusterSummary, error) {
	rows, err := s.db.Query(
		`SELECT id, name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, status, created_at, updated_at
		 FROM clusters
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterSummary, 0)
	for rows.Next() {
		var c ClusterSummary
		if scanErr := rows.Scan(
			&c.ID,
			&c.Name,
			&c.TopologyType,
			&c.KubernetesVersion,
			&c.NetworkProfileID,
			&c.ControlPlaneCount,
			&c.WorkerCount,
			&c.Status,
			&c.CreatedAt,
			&c.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, c)
	}

	return out, nil
}

func (s *Service) GetCluster(clusterID int64) (ClusterSummary, error) {
	return s.getClusterSummary(clusterID)
}

func (s *Service) ListClusterNodes(clusterID int64) ([]ClusterNodeRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}
	return s.getClusterNodes(clusterID)
}

func (s *Service) GetBootstrapConfig(clusterID int64) (BootstrapConfigRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return BootstrapConfigRecord{}, err
	}

	return s.getBootstrapConfig(clusterID)
}

func (s *Service) ListBootstrapSSHKeys(clusterID int64) ([]BootstrapSSHKeyRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	return s.getBootstrapSSHKeys(clusterID)
}

func (s *Service) ListRuns(clusterID int64) ([]ProvisionRunRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		`SELECT id, action, status, work_dir, output, created_at, finished_at
		 FROM provision_runs
		 WHERE cluster_id = $1
		 ORDER BY created_at DESC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cluster runs: %w", err)
	}
	defer rows.Close()

	out := make([]ProvisionRunRecord, 0)
	for rows.Next() {
		var rec ProvisionRunRecord
		if scanErr := rows.Scan(&rec.ID, &rec.Action, &rec.Status, &rec.WorkDir, &rec.Output, &rec.CreatedAt, &rec.FinishedAt); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) GetLatestOperation(clusterID int64) (ClusterOperation, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ClusterOperation{}, err
	}

	var op ClusterOperation
	err := s.db.QueryRow(
		`SELECT id, cluster_id, action, status, current_step, progress_percent,
		        COALESCE(error_summary, ''), started_at, updated_at, finished_at
		 FROM cluster_operations
		 WHERE cluster_id = $1
		 ORDER BY started_at DESC
		 LIMIT 1`,
		clusterID,
	).Scan(
		&op.ID,
		&op.ClusterID,
		&op.Action,
		&op.Status,
		&op.CurrentStep,
		&op.ProgressPercent,
		&op.ErrorSummary,
		&op.StartedAt,
		&op.UpdatedAt,
		&op.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterOperation{}, ErrOperationNotFound
	}
	if err != nil {
		return ClusterOperation{}, fmt.Errorf("get latest operation: %w", err)
	}

	return op, nil
}

func (s *Service) ListOperationEvents(clusterID int64, operationID int64, sinceID int64, limit int) ([]ClusterOperationEvent, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	if operationID <= 0 {
		latest, err := s.GetLatestOperation(clusterID)
		if err != nil {
			if errors.Is(err, ErrOperationNotFound) {
				return []ClusterOperationEvent{}, nil
			}
			return nil, err
		}
		operationID = latest.ID
	}

	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	rows, err := s.db.Query(
		`SELECT e.id, e.operation_id, e.step, e.level, e.message, e.created_at
		 FROM cluster_operation_events e
		 JOIN cluster_operations o ON o.id = e.operation_id
		 WHERE o.cluster_id = $1
		   AND e.operation_id = $2
		   AND e.id > $3
		 ORDER BY e.id ASC
		 LIMIT $4`,
		clusterID,
		operationID,
		sinceID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list operation events: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterOperationEvent, 0)
	for rows.Next() {
		var event ClusterOperationEvent
		if scanErr := rows.Scan(&event.ID, &event.OperationID, &event.Step, &event.Level, &event.Message, &event.CreatedAt); scanErr != nil {
			continue
		}
		out = append(out, event)
	}

	return out, nil
}

func (s *Service) RedeployCluster(clusterID int64) (ClusterRedeployResponse, error) {
	summary, err := s.getClusterSummary(clusterID)
	if err != nil {
		return ClusterRedeployResponse{}, err
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return ClusterRedeployResponse{}, err
	}
	credentialIDs := distinctCredentialIDsFromClusterNodes(nodes)
	if len(credentialIDs) == 0 {
		return ClusterRedeployResponse{}, errors.New("cluster cannot be redeployed because proxmox credential metadata is missing")
	}

	latestWorkDir, err := s.latestProvisionWorkDir(clusterID)
	if err != nil {
		return ClusterRedeployResponse{}, err
	}

	multiCredential := len(credentialIDs) > 1
	applyWork := make([]provisionApplyWork, 0, len(credentialIDs))
	workDirs := make([]string, 0, len(credentialIDs))
	for _, credentialID := range credentialIDs {
		candidates := make([]string, 0, 2)
		if multiCredential {
			candidates = append(candidates, s.terraformRunner.WorkspaceDirForKey(workspaceKeyForCredential(summary.Name, credentialID)))
		} else {
			if latestWorkDir != "" {
				candidates = append(candidates, latestWorkDir)
			}
			candidates = append(candidates, s.terraformRunner.WorkspaceDir(summary.Name))
		}

		workDir, findErr := firstExistingTerraformWorkspace(candidates)
		if findErr != nil {
			return ClusterRedeployResponse{}, findErr
		}
		if workDir == "" {
			return ClusterRedeployResponse{}, fmt.Errorf("no terraform workspace found for cluster %s credential_id=%d", summary.Name, credentialID)
		}
		applyWork = append(applyWork, provisionApplyWork{CredentialID: credentialID, Workspace: workDir})
		workDirs = append(workDirs, workDir)
	}

	operationID, err := s.startClusterOperation(clusterID, "redeploy", "terraform-apply", 10)
	if err != nil {
		return ClusterRedeployResponse{}, err
	}
	_ = s.appendOperationEvent(operationID, "terraform-apply", "info", "Redeploy started using existing Terraform workspace configuration")

	primaryWorkspace := workDirs[0]
	if latestWorkDir != "" {
		primaryWorkspace = latestWorkDir
	}
	if err := s.insertProvisionRun(clusterID, "redeploy", "redeploying", primaryWorkspace, ""); err != nil {
		_ = s.updateClusterOperation(operationID, "failed", "save-run", 100, err.Error())
		return ClusterRedeployResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, "redeploying"); err != nil {
		_ = s.updateClusterOperation(operationID, "failed", "update-status", 100, err.Error())
		return ClusterRedeployResponse{}, err
	}

	go s.runProvisionApplyAsync(clusterID, applyWork, primaryWorkspace, operationID)

	return ClusterRedeployResponse{
		ClusterID: clusterID,
		Action:    "redeploy",
		Status:    "redeploying",
		WorkDirs:  workDirs,
		Warnings:  []string{"terraform apply is running in background; check cluster run history for progress"},
	}, nil
}

func (s *Service) DeleteCluster(clusterID int64) error {
	info, err := s.getClusterDeleteInfo(clusterID)
	if err != nil {
		return err
	}

	workDir := strings.TrimSpace(info.TerraformWorkDir)
	nodes, nodeErr := s.getClusterNodes(clusterID)
	if nodeErr != nil {
		return nodeErr
	}

	credentialIDs := distinctCredentialIDsFromClusterNodes(nodes)
	if len(credentialIDs) == 0 && info.ProxmoxCredentialID.Valid {
		credentialIDs = append(credentialIDs, info.ProxmoxCredentialID.Int64)
	}

	if len(credentialIDs) == 0 {
		if len(nodes) > 0 {
			return errors.New("cluster cannot be deleted safely because its proxmox credential metadata is missing; re-save the credential mapping or destroy the VMs manually first")
		}
	} else {
		multiCredential := len(credentialIDs) > 1
		for _, credentialID := range credentialIDs {
			cred, credErr := s.proxmoxSvc.GetCredentialWithSecrets(credentialID)
			if credErr != nil {
				return fmt.Errorf("load proxmox credential for delete: %w", credErr)
			}
			auth := terraform.ProxmoxAuth{
				Endpoint:       cred.Endpoint,
				Username:       cred.Username,
				Password:       cred.Password,
				APITokenID:     cred.APITokenID,
				APITokenSecret: cred.APITokenSecret,
				InsecureTLS:    cred.InsecureTLS,
				SSHUsername:    sshUserFrom(cred),
				SSHPassword:    cred.Password,
			}

			candidates := make([]string, 0, 2)
			if multiCredential {
				candidates = append(candidates, s.terraformRunner.WorkspaceDirForKey(workspaceKeyForCredential(info.Name, credentialID)))
			} else {
				if workDir != "" {
					candidates = append(candidates, workDir)
				}
				candidates = append(candidates, s.terraformRunner.WorkspaceDir(info.Name))
			}

			destroyed := false
			seen := make(map[string]struct{})
			for _, candidate := range candidates {
				trimmed := strings.TrimSpace(candidate)
				if trimmed == "" {
					continue
				}
				if _, ok := seen[trimmed]; ok {
					continue
				}
				seen[trimmed] = struct{}{}

				if _, statErr := os.Stat(trimmed); statErr == nil {
					if _, destroyErr := s.terraformRunner.Destroy(trimmed, auth); destroyErr != nil {
						if isIgnorableDestroyError(destroyErr) {
							log.Printf("warning: terraform destroy failed for cluster %d credential_id=%d workspace=%s; proceeding with metadata delete only: %v", clusterID, credentialID, trimmed, destroyErr)
							destroyed = true
							break
						}
						return fmt.Errorf("destroy cluster infrastructure: %w", destroyErr)
					}
					destroyed = true
					break
				} else if !os.IsNotExist(statErr) {
					return fmt.Errorf("inspect terraform workspace: %w", statErr)
				}
			}

			if !destroyed && len(nodes) > 0 {
				log.Printf("warning: terraform workspace for cluster %d credential_id=%d is missing; proceeding with metadata delete only", clusterID, credentialID)
			}
		}
	}

	res, err := s.db.Exec(`DELETE FROM clusters WHERE id = $1`, clusterID)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrClusterNotFound
	}
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
	_ = os.RemoveAll(s.terraformRunner.WorkspaceDir(info.Name))
	for _, credentialID := range distinctCredentialIDsFromClusterNodes(nodes) {
		_ = os.RemoveAll(s.terraformRunner.WorkspaceDirForKey(workspaceKeyForCredential(info.Name, credentialID)))
	}
	return nil
}

func (s *Service) PowerActionCluster(clusterID int64, action string) (ClusterPowerActionResponse, error) {
	action = strings.TrimSpace(strings.ToLower(action))
	proxmoxAction := action
	status := action
	switch action {
	case "start":
		status = "online"
	case "shutdown":
		status = "offline"
	case "restart":
		proxmoxAction = "reboot"
		status = "online"
	default:
		return ClusterPowerActionResponse{}, fmt.Errorf("unsupported cluster power action %q", action)
	}

	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ClusterPowerActionResponse{}, err
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return ClusterPowerActionResponse{}, err
	}
	if len(nodes) == 0 {
		return ClusterPowerActionResponse{}, errors.New("cluster has no nodes")
	}

	operationID, err := s.startClusterOperation(clusterID, action, "prepare", 5)
	if err != nil {
		return ClusterPowerActionResponse{}, err
	}
	_ = s.appendOperationEvent(operationID, "prepare", "info", fmt.Sprintf("Starting cluster %s for %d node(s)", action, len(nodes)))

	credentials := make(map[int64]proxmox.Credential)
	nodeCredentials := make(map[int64]proxmox.Credential, len(nodes))
	for _, node := range nodes {
		if node.ProxmoxCredentialID <= 0 {
			err := fmt.Errorf("missing proxmox credential metadata for node %s", node.VMName)
			_ = s.updateClusterOperation(operationID, "failed", "load-credential", 100, err.Error())
			_ = s.appendOperationEvent(operationID, "load-credential", "error", err.Error())
			_ = s.insertProvisionRun(clusterID, action, "failed", "", err.Error())
			return ClusterPowerActionResponse{}, err
		}

		cred, ok := credentials[node.ProxmoxCredentialID]
		if !ok {
			cred, err = s.proxmoxSvc.GetCredentialWithSecrets(node.ProxmoxCredentialID)
			if err != nil {
				wrapped := fmt.Errorf("load proxmox credential for node %s: %w", node.VMName, err)
				_ = s.updateClusterOperation(operationID, "failed", "load-credential", 100, wrapped.Error())
				_ = s.appendOperationEvent(operationID, "load-credential", "error", wrapped.Error())
				_ = s.insertProvisionRun(clusterID, action, "failed", "", wrapped.Error())
				return ClusterPowerActionResponse{}, wrapped
			}
			credentials[node.ProxmoxCredentialID] = cred
		}
		nodeCredentials[node.ID] = cred
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	changedNodes := make([]string, 0, len(nodes))
	outputs := make([]string, 0, len(nodes))
	failures := make([]string, 0)

	for index, node := range nodes {
		node := node
		index := index
		cred := nodeCredentials[node.ID]
		wg.Add(1)
		go func() {
			defer wg.Done()

			step := fmt.Sprintf("%s-%s", action, node.VMName)
			progress := 10 + int(float64(index)/float64(len(nodes))*80)
			_ = s.updateClusterOperation(operationID, "running", step, progress, "")
			_ = s.appendOperationEvent(operationID, step, "info", fmt.Sprintf("Sending %s command to VM %s on %s", action, node.VMName, node.ProxmoxNodeName))

			actualAction, err := s.proxmoxSvc.ControlVMByName(cred, node.ProxmoxNodeName, node.VMName, proxmoxAction)
			if err != nil {
				wrapped := fmt.Errorf("%s node %s: %w", action, node.VMName, err)
				_ = s.appendOperationEvent(operationID, step, "error", wrapped.Error())
				mu.Lock()
				failures = append(failures, wrapped.Error())
				mu.Unlock()
				return
			}

			mu.Lock()
			changedNodes = append(changedNodes, node.VMName)
			outputs = append(outputs, fmt.Sprintf("%s %s on %s", actualAction, node.VMName, node.ProxmoxNodeName))
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Strings(changedNodes)
	sort.Strings(outputs)
	sort.Strings(failures)

	if len(failures) > 0 {
		failureOutput := strings.Join(append(outputs, failures...), "\n")
		err := errors.New(strings.Join(failures, "; "))
		_ = s.updateClusterOperation(operationID, "failed", "completed-with-errors", 100, err.Error())
		_ = s.insertProvisionRun(clusterID, action, "failed", "", failureOutput)
		return ClusterPowerActionResponse{}, err
	}

	if err := s.insertProvisionRun(clusterID, action, status, "", strings.Join(outputs, "\n")); err != nil {
		_ = s.updateClusterOperation(operationID, "failed", "save-run", 100, err.Error())
		return ClusterPowerActionResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		_ = s.updateClusterOperation(operationID, "failed", "update-status", 100, err.Error())
		return ClusterPowerActionResponse{}, err
	}

	_ = s.updateClusterOperation(operationID, "succeeded", "completed", 100, "")
	_ = s.appendOperationEvent(operationID, "completed", "info", fmt.Sprintf("Cluster %s command sent to %d node(s)", action, len(changedNodes)))

	return ClusterPowerActionResponse{
		ClusterID: clusterID,
		Action:    action,
		Status:    status,
		Nodes:     changedNodes,
		Warnings:  []string{},
	}, nil
}

func (s *Service) Scale(clusterID int64, req ScaleRequest) (ScaleResponse, error) {
	if req.TargetWorkerCount < 1 {
		return ScaleResponse{}, errors.New("target_worker_count must be at least 1")
	}

	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return ScaleResponse{}, err
	}

	nodes, err := s.getClusterNodes(clusterID)
	if err != nil {
		return ScaleResponse{}, err
	}

	currentWorkers := make([]ClusterNodeRecord, 0)
	for _, n := range nodes {
		if n.Role == "worker" {
			currentWorkers = append(currentWorkers, n)
		}
	}

	response := ScaleResponse{
		ClusterID:           clusterID,
		PreviousWorkerCount: len(currentWorkers),
		NewWorkerCount:      req.TargetWorkerCount,
		AddedNodes:          []string{},
		RemovedNodes:        []string{},
		Warnings:            []string{},
	}

	if req.TargetWorkerCount == len(currentWorkers) {
		response.Warnings = append(response.Warnings, "target worker count equals current count; no changes applied")
		return response, nil
	}

	if req.TargetWorkerCount > len(currentWorkers) {
		defaultCredentialID, err := s.getClusterCredentialID(clusterID)
		if err != nil {
			return ScaleResponse{}, err
		}

		nextIndex := 1
		for _, n := range currentWorkers {
			if n.RoleIndex >= nextIndex {
				nextIndex = n.RoleIndex + 1
			}
		}

		nodePool, err := s.listDistinctProxmoxNodes(clusterID)
		if err != nil {
			return ScaleResponse{}, err
		}
		if len(nodePool) == 0 {
			return ScaleResponse{}, errors.New("no proxmox nodes associated with cluster")
		}

		toAdd := req.TargetWorkerCount - len(currentWorkers)
		for i := 0; i < toAdd; i++ {
			vmName := buildVMName(cluster.Name, "worker", nextIndex)
			selectedNode := nodePool[i%len(nodePool)]
			nodeCredentialID, credErr := s.resolveCredentialForNode(clusterID, selectedNode, defaultCredentialID)
			if credErr != nil {
				return ScaleResponse{}, credErr
			}
			_, execErr := s.db.Exec(
				`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name, proxmox_credential_id)
				 VALUES ($1, 'worker', $2, $3, $4, $5)`,
				clusterID,
				nextIndex,
				selectedNode,
				vmName,
				nodeCredentialID,
			)
			if execErr != nil {
				return ScaleResponse{}, fmt.Errorf("insert scaled worker node: %w", execErr)
			}
			response.AddedNodes = append(response.AddedNodes, vmName)
			nextIndex++
		}
	} else {
		toRemove := len(currentWorkers) - req.TargetWorkerCount
		for i := len(currentWorkers) - 1; i >= 0 && toRemove > 0; i-- {
			n := currentWorkers[i]
			_, execErr := s.db.Exec(`DELETE FROM cluster_nodes WHERE id = $1`, n.ID)
			if execErr != nil {
				return ScaleResponse{}, fmt.Errorf("delete worker node: %w", execErr)
			}
			response.RemovedNodes = append(response.RemovedNodes, n.VMName)
			toRemove--
		}
		response.Warnings = append(response.Warnings, "worker node records removed; VM deletion must be handled by provisioning workflow")
	}

	if _, err := s.db.Exec(
		`UPDATE clusters SET worker_count = $2, status = 'scaled', updated_at = NOW() WHERE id = $1`,
		clusterID,
		req.TargetWorkerCount,
	); err != nil {
		return ScaleResponse{}, fmt.Errorf("update cluster worker count: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "scale", "scaled", "", strings.Join(append(response.AddedNodes, response.RemovedNodes...), ",")); err != nil {
		return ScaleResponse{}, err
	}

	return response, nil
}

func (s *Service) Upgrade(clusterID int64, req UpgradeRequest) (UpgradeResponse, error) {
	target := strings.TrimSpace(req.TargetKubernetesVersion)
	if target == "" {
		return UpgradeResponse{}, errors.New("target_kubernetes_version is required")
	}

	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return UpgradeResponse{}, err
	}

	if _, err := s.db.Exec(
		`UPDATE clusters SET kubernetes_version = $2, status = 'upgrade-planned', updated_at = NOW() WHERE id = $1`,
		clusterID,
		target,
	); err != nil {
		return UpgradeResponse{}, fmt.Errorf("update cluster version: %w", err)
	}

	out := fmt.Sprintf("upgrade planned from %s to %s", cluster.KubernetesVersion, target)
	if err := s.insertProvisionRun(clusterID, "upgrade", "upgrade-planned", "", out); err != nil {
		return UpgradeResponse{}, err
	}

	return UpgradeResponse{
		ClusterID:       clusterID,
		PreviousVersion: cluster.KubernetesVersion,
		TargetVersion:   target,
		Status:          "upgrade-planned",
		Warning:         "version metadata updated; execute upgrade automation runbook before production cutover",
	}, nil
}

func (s *Service) ReplaceNode(clusterID int64, req ReplaceNodeRequest) (ReplaceNodeResponse, error) {
	failedVM := strings.TrimSpace(req.FailedVMName)
	newNode := strings.TrimSpace(req.NewProxmoxNodeName)
	newIP := strings.TrimSpace(req.NewIPAddress)
	if failedVM == "" {
		return ReplaceNodeResponse{}, errors.New("failed_vm_name is required")
	}
	if newIP != "" && net.ParseIP(newIP) == nil {
		return ReplaceNodeResponse{}, errors.New("new_ip_address is invalid")
	}

	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ReplaceNodeResponse{}, err
	}

	var existing ClusterNodeRecord
	err := s.db.QueryRow(
		`SELECT id, role, role_index, vm_name, COALESCE(ip_address, ''), proxmox_node_name, COALESCE(proxmox_credential_id, 0)
		 FROM cluster_nodes
		 WHERE cluster_id = $1 AND vm_name = $2`,
		clusterID,
		failedVM,
	).Scan(&existing.ID, &existing.Role, &existing.RoleIndex, &existing.VMName, &existing.IPAddress, &existing.ProxmoxNodeName, &existing.ProxmoxCredentialID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReplaceNodeResponse{}, errors.New("failed node not found")
		}
		return ReplaceNodeResponse{}, fmt.Errorf("query failed node: %w", err)
	}

	// Both worker and control-plane roles are supported
	if existing.Role != "worker" && existing.Role != "control-plane" {
		return ReplaceNodeResponse{}, fmt.Errorf("replace-node does not support role: %s", existing.Role)
	}

	// Auto-select a different Proxmox node if not specified
	if newNode == "" {
		selectedNode, err := selectAlternateProxmoxNode(s.db, clusterID, existing.ProxmoxNodeName)
		if err != nil {
			// If auto-selection fails, fall back to current node
			selectedNode = existing.ProxmoxNodeName
		}
		newNode = selectedNode
	}

	newVMName := existing.VMName + "-r"
	tx, err := s.db.Begin()
	if err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("begin replace node transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM cluster_nodes WHERE id = $1`, existing.ID); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("delete failed node record: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name, ip_address, proxmox_credential_id)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
		clusterID,
		existing.Role,
		existing.RoleIndex,
		newNode,
		newVMName,
		newIP,
		existing.ProxmoxCredentialID,
	); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("insert replacement node: %w", err)
	}

	if _, err := tx.Exec(`UPDATE clusters SET status = 'node-replaced', updated_at = NOW() WHERE id = $1`, clusterID); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("update cluster status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ReplaceNodeResponse{}, fmt.Errorf("commit replace node transaction: %w", err)
	}

	_ = s.insertProvisionRun(clusterID, "replace-node", "node-replaced", "", fmt.Sprintf("%s->%s", failedVM, newVMName))

	return ReplaceNodeResponse{
		ClusterID: clusterID,
		OldVMName: failedVM,
		NewVMName: newVMName,
		Status:    "node-replaced",
		Warning:   "replacement updates metadata; provision/decommission VM actions should be executed in infra workflow",
	}, nil
}

func (s *Service) RotateKubeconfig(clusterID int64, req RotateKubeconfigRequest) (RotateKubeconfigResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return RotateKubeconfigResponse{}, err
	}

	ttl := req.TTLHours
	if ttl <= 0 {
		ttl = 24
	}
	if ttl > 24*30 {
		return RotateKubeconfigResponse{}, errors.New("ttl_hours too large; max 720")
	}

	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return RotateKubeconfigResponse{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().UTC().Add(time.Duration(ttl) * time.Hour)

	if _, err := s.db.Exec(
		`INSERT INTO kubeconfig_tokens (cluster_id, token, expires_at, revoked)
		 VALUES ($1, $2, $3, FALSE)`,
		clusterID,
		token,
		expiresAt,
	); err != nil {
		return RotateKubeconfigResponse{}, fmt.Errorf("insert kubeconfig token: %w", err)
	}

	_ = s.insertProvisionRun(clusterID, "rotate-kubeconfig", "token-rotated", "", fmt.Sprintf("expires_at=%s", expiresAt.Format(time.RFC3339)))

	return RotateKubeconfigResponse{
		ClusterID: clusterID,
		Token:     token,
		ExpiresAt: expiresAt,
		Status:    "token-rotated",
	}, nil
}

func (s *Service) DeployObservability(clusterID int64, req DeployObservabilityRequest) (DeployObservabilityResponse, error) {
	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return DeployObservabilityResponse{}, err
	}

	adminUser := strings.TrimSpace(req.GrafanaAdminUser)
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPassword := strings.TrimSpace(req.GrafanaAdminPassword)
	if adminPassword == "" {
		return DeployObservabilityResponse{}, errors.New("grafana_admin_password is required")
	}
	retention := req.RetentionDays
	if retention <= 0 {
		retention = 15
	}
	storageClass := strings.TrimSpace(req.StorageClass)
	if storageClass == "" {
		storageClass = "default"
	}

	manifestDir := s.operationsManifestDir(cluster.Name, "observability")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("create observability workspace: %w", err)
	}

	manifest := buildObservabilityManifest(adminUser, adminPassword, retention, storageClass)
	manifestPath := filepath.Join(manifestDir, "kube-prometheus-stack-values.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("write observability manifest: %w", err)
	}

	status := "observability-prepared"
	warnings := []string{}
	if req.Apply {
		status = "observability-apply-requested"
		warnings = append(warnings, "runtime helm deployment is not executed yet; apply using generated values file in your deployment pipeline")
	}

	dashboardURL := fmt.Sprintf("http://%s-grafana.local", sanitizePathToken(cluster.Name))
	_, err = s.db.Exec(
		`INSERT INTO observability_configs (cluster_id, grafana_admin_user, grafana_admin_password, retention_days, storage_class, status, dashboard_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (cluster_id)
		 DO UPDATE SET
		   grafana_admin_user = EXCLUDED.grafana_admin_user,
		   grafana_admin_password = EXCLUDED.grafana_admin_password,
		   retention_days = EXCLUDED.retention_days,
		   storage_class = EXCLUDED.storage_class,
		   status = EXCLUDED.status,
		   dashboard_url = EXCLUDED.dashboard_url,
		   updated_at = NOW()`,
		clusterID,
		adminUser,
		adminPassword,
		retention,
		storageClass,
		status,
		dashboardURL,
	)
	if err != nil {
		return DeployObservabilityResponse{}, fmt.Errorf("upsert observability config: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "observability-deploy", status, manifestDir, manifestPath); err != nil {
		return DeployObservabilityResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return DeployObservabilityResponse{}, err
	}

	return DeployObservabilityResponse{
		ClusterID:    clusterID,
		Status:       status,
		DashboardURL: dashboardURL,
		ManifestDir:  manifestDir,
		Warnings:     warnings,
	}, nil
}

func (s *Service) DeployMetalLB(clusterID int64, req DeployMetalLBRequest) (DeployMetalLBResponse, error) {
	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return DeployMetalLBResponse{}, err
	}

	namespace, version, addressPools, interfaces, err := normalizeMetalLBRequest(req)
	if err != nil {
		return DeployMetalLBResponse{}, err
	}

	manifestDir := s.operationsManifestDir(cluster.Name, "metallb")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("create metallb workspace: %w", err)
	}

	configManifest := buildMetalLBConfigManifest(namespace, addressPools, interfaces)
	configPath := filepath.Join(manifestDir, "metallb-resources.yaml")
	if err := os.WriteFile(configPath, []byte(configManifest), 0o644); err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("write metallb resources manifest: %w", err)
	}

	readmePath := filepath.Join(manifestDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(buildMetalLBREADME(cluster.Name, namespace, version)), 0o644); err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("write metallb readme: %w", err)
	}

	psPath := filepath.Join(manifestDir, "apply-metallb.ps1")
	if err := os.WriteFile(psPath, []byte(buildMetalLBPowerShell(namespace, version)), 0o644); err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("write metallb powershell helper: %w", err)
	}

	shPath := filepath.Join(manifestDir, "apply-metallb.sh")
	if err := os.WriteFile(shPath, []byte(buildMetalLBShell(namespace, version)), 0o755); err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("write metallb shell helper: %w", err)
	}

	addressPoolsJSON, err := json.Marshal(addressPools)
	if err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("marshal metallb address pools: %w", err)
	}
	interfacesJSON, err := json.Marshal(interfaces)
	if err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("marshal metallb interfaces: %w", err)
	}

	status := "metallb-prepared"
	warnings := []string{}
	if req.Apply {
		// Execute kubectl commands to deploy MetalLB
		if err := s.executeMetalLBDeployment(namespace, version, manifestDir); err != nil {
			return DeployMetalLBResponse{}, fmt.Errorf("failed to deploy metallb: %w", err)
		}
		status = "metallb-deployed"
		warnings = append(warnings, "metallb deployed successfully to the cluster")
	} else {
		warnings = append(warnings, "runtime kubectl apply is not executed yet; use the generated helper script or README in the manifest directory")
	}

	_, err = s.db.Exec(
		`INSERT INTO metallb_configs (cluster_id, namespace, version, address_pools, interfaces, status, manifest_dir)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (cluster_id)
		 DO UPDATE SET
		   namespace = EXCLUDED.namespace,
		   version = EXCLUDED.version,
		   address_pools = EXCLUDED.address_pools,
		   interfaces = EXCLUDED.interfaces,
		   status = EXCLUDED.status,
		   manifest_dir = EXCLUDED.manifest_dir,
		   updated_at = NOW()`,
		clusterID,
		namespace,
		version,
		string(addressPoolsJSON),
		string(interfacesJSON),
		status,
		manifestDir,
	)
	if err != nil {
		return DeployMetalLBResponse{}, fmt.Errorf("upsert metallb config: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "metallb-deploy", status, manifestDir, configPath); err != nil {
		return DeployMetalLBResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return DeployMetalLBResponse{}, err
	}

	commands := buildMetalLBCommands(manifestDir)

	return DeployMetalLBResponse{
		ClusterID:   clusterID,
		Status:      status,
		Namespace:   namespace,
		Version:     version,
		ManifestDir: manifestDir,
		Commands:    commands,
		Warnings:    warnings,
	}, nil
}

func (s *Service) DeployIngressController(clusterID int64, req DeployIngressControllerRequest) (DeployIngressControllerResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return DeployIngressControllerResponse{}, err
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "traefik"
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		image = "traefik:v3.3"
	}

	status := "ingress-controller-prepared"
	output := ""
	warnings := []string{}
	if req.Apply {
		status = "ingress-controller-deployed"
		applyOutput, err := s.executeIngressControllerDeployment(namespace, image, req.Wait)
		output = applyOutput
		if err != nil {
			_ = s.insertProvisionRun(clusterID, "ingress-controller-deploy", "failed", namespace, output)
			return DeployIngressControllerResponse{}, fmt.Errorf("failed to deploy ingress controller: %w", err)
		}
		warnings = append(warnings, "traefik ingress controller deployed successfully")
	} else {
		warnings = append(warnings, "ingress controller was not applied; enable apply to run kubectl")
	}

	if err := s.insertProvisionRun(clusterID, "ingress-controller-deploy", status, namespace, output); err != nil {
		return DeployIngressControllerResponse{}, err
	}

	return DeployIngressControllerResponse{
		ClusterID: clusterID,
		Status:    status,
		Namespace: namespace,
		Image:     image,
		Output:    output,
		Warnings:  warnings,
	}, nil
}

func (s *Service) DeployCertificateManager(clusterID int64, req DeployCertificateManagerRequest) (DeployCertificateManagerResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return DeployCertificateManagerResponse{}, err
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "cert-manager"
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "v1.16.2"
	}
	issuerName := strings.TrimSpace(req.IssuerName)
	if issuerName == "" {
		issuerName = "letsencrypt-prod"
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return DeployCertificateManagerResponse{}, errors.New("email is required for Let's Encrypt registration")
	}
	acmeServer := strings.TrimSpace(req.ACMEServer)
	if acmeServer == "" {
		acmeServer = "https://acme-v02.api.letsencrypt.org/directory"
	}
	ingressClass := strings.TrimSpace(req.IngressClass)
	if ingressClass == "" {
		ingressClass = "traefik"
	}

	status := "certificate-manager-prepared"
	output := ""
	warnings := []string{}
	if req.Apply {
		status = "certificate-manager-deployed"
		applyOutput, err := s.executeCertificateManagerDeployment(namespace, version, issuerName, email, acmeServer, ingressClass, req.Wait)
		output = applyOutput
		if err != nil {
			_ = s.insertProvisionRun(clusterID, "certificate-manager-deploy", "failed", issuerName, output)
			return DeployCertificateManagerResponse{}, fmt.Errorf("failed to deploy certificate manager: %w", err)
		}
		warnings = append(warnings, "cert-manager and Let's Encrypt ClusterIssuer deployed successfully")
	} else {
		warnings = append(warnings, "cert-manager was not applied; enable apply to run kubectl")
	}

	if err := s.insertProvisionRun(clusterID, "certificate-manager-deploy", status, issuerName, output); err != nil {
		return DeployCertificateManagerResponse{}, err
	}

	return DeployCertificateManagerResponse{
		ClusterID:  clusterID,
		Status:     status,
		Namespace:  namespace,
		Version:    version,
		IssuerName: issuerName,
		ACMEServer: acmeServer,
		Output:     output,
		Warnings:   warnings,
	}, nil
}

func (s *Service) ListClusterSecrets(clusterID int64) ([]ClusterSecretRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, cluster_id, name, namespace, provider, type, description, external_ref,
		        provider_config, immutable, is_global, last_applied_at, created_at, updated_at
		 FROM cluster_secrets
		 WHERE cluster_id = $1 OR is_global = TRUE
		 ORDER BY is_global DESC, namespace ASC, name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cluster secrets: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterSecretRecord, 0)
	for rows.Next() {
		rec, scanErr := s.scanClusterSecret(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) CreateClusterSecret(clusterID int64, req UpsertClusterSecretRequest) (ClusterSecretRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ClusterSecretRecord{}, err
	}
	return s.createClusterSecret(clusterID, req)
}

func (s *Service) CreateGlobalSecret(req UpsertClusterSecretRequest) (ClusterSecretRecord, error) {
	req.IsGlobal = true
	return s.createClusterSecret(0, req)
}

func (s *Service) createClusterSecret(clusterID int64, req UpsertClusterSecretRequest) (ClusterSecretRecord, error) {
	req = normalizeClusterSecretRequest(req)
	if err := validateClusterSecretRequest(req, true); err != nil {
		return ClusterSecretRecord{}, err
	}

	configJSON, err := json.Marshal(req.ProviderConfig)
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("encode provider config: %w", err)
	}
	var id int64
	clusterIDValue := sql.NullInt64{Int64: clusterID, Valid: clusterID > 0}
	err = s.db.QueryRow(
		`INSERT INTO cluster_secrets
		 (cluster_id, name, namespace, provider, type, description, external_ref, provider_config, immutable, is_global)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id`,
		clusterIDValue, req.Name, req.Namespace, req.Provider, req.Type, req.Description, req.ExternalRef, configJSON, req.Immutable, req.IsGlobal,
	).Scan(&id)
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("create cluster secret: %w", err)
	}
	if err := s.replaceClusterSecretItems(clusterID, id, req.Name, req.Data); err != nil {
		return ClusterSecretRecord{}, err
	}
	return s.GetClusterSecret(clusterID, id)
}

func (s *Service) GetClusterSecret(clusterID, secretID int64) (ClusterSecretRecord, error) {
	if clusterID > 0 {
		if _, err := s.getClusterSummary(clusterID); err != nil {
			return ClusterSecretRecord{}, err
		}
	}
	row := s.db.QueryRow(
		`SELECT id, cluster_id, name, namespace, provider, type, description, external_ref,
		        provider_config, immutable, is_global, last_applied_at, created_at, updated_at
		 FROM cluster_secrets
		 WHERE id = $2 AND (($1 > 0 AND cluster_id = $1) OR is_global = TRUE)`,
		clusterID,
		secretID,
	)
	rec, err := s.scanClusterSecret(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClusterSecretRecord{}, ErrApplicationManifestNotFound
		}
		return ClusterSecretRecord{}, fmt.Errorf("get cluster secret: %w", err)
	}
	return rec, nil
}

func (s *Service) UpdateClusterSecret(clusterID, secretID int64, req UpsertClusterSecretRequest) (ClusterSecretRecord, error) {
	if _, err := s.GetClusterSecret(clusterID, secretID); err != nil {
		return ClusterSecretRecord{}, err
	}
	req = normalizeClusterSecretRequest(req)
	if err := validateClusterSecretRequest(req, false); err != nil {
		return ClusterSecretRecord{}, err
	}
	configJSON, err := json.Marshal(req.ProviderConfig)
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("encode provider config: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE cluster_secrets
		 SET name = $3, namespace = $4, provider = $5, type = $6, description = $7,
		     external_ref = $8, provider_config = $9, immutable = $10, is_global = $11, updated_at = NOW()
		 WHERE cluster_id = $1 AND id = $2`,
		clusterID, secretID, req.Name, req.Namespace, req.Provider, req.Type, req.Description, req.ExternalRef, configJSON, req.Immutable, req.IsGlobal,
	)
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("update cluster secret: %w", err)
	}
	if req.Data != nil {
		if err := s.replaceClusterSecretItems(clusterID, secretID, req.Name, req.Data); err != nil {
			return ClusterSecretRecord{}, err
		}
	}
	return s.GetClusterSecret(clusterID, secretID)
}

func (s *Service) DeleteClusterSecret(clusterID, secretID int64) error {
	res, err := s.db.Exec(`DELETE FROM cluster_secrets WHERE cluster_id = $1 AND id = $2`, clusterID, secretID)
	if err != nil {
		return fmt.Errorf("delete cluster secret: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrApplicationManifestNotFound
	}
	return nil
}

func (s *Service) UpdateClusterSecretKey(clusterID, secretID int64, key string, req UpdateSecretKeyRequest) (ClusterSecretRecord, error) {
	rec, err := s.GetClusterSecret(clusterID, secretID)
	if err != nil {
		return ClusterSecretRecord{}, err
	}
	if rec.ClusterID != clusterID {
		return ClusterSecretRecord{}, errors.New("global secret can only be edited from its owning cluster")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ClusterSecretRecord{}, errors.New("secret key is required")
	}
	if s.secrets == nil {
		return ClusterSecretRecord{}, errors.New("secret store is not configured")
	}
	ref, err := s.secrets.Put(context.Background(), fmt.Sprintf("clusters/%d/app-secrets/%d", rec.ClusterID, secretID), key, []byte(req.Value))
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("store cluster secret key %s: %w", key, err)
	}
	sum := sha256.Sum256([]byte(req.Value))
	_, err = s.db.Exec(
		`INSERT INTO cluster_secret_items (cluster_secret_id, key, secret_ref, value_sha256)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (cluster_secret_id, key)
		 DO UPDATE SET secret_ref = EXCLUDED.secret_ref, value_sha256 = EXCLUDED.value_sha256, updated_at = NOW()`,
		secretID,
		key,
		ref,
		hex.EncodeToString(sum[:]),
	)
	if err != nil {
		return ClusterSecretRecord{}, fmt.Errorf("update cluster secret key: %w", err)
	}
	_, _ = s.db.Exec(`UPDATE cluster_secrets SET updated_at = NOW() WHERE id = $1`, secretID)
	return s.GetClusterSecret(clusterID, secretID)
}

func (s *Service) ApplyClusterSecret(clusterID, secretID int64) (ApplyClusterSecretResponse, error) {
	rec, err := s.GetClusterSecret(clusterID, secretID)
	if err != nil {
		return ApplyClusterSecretResponse{}, err
	}
	output, err := s.applyClusterSecretRecord(rec)
	if err != nil {
		return ApplyClusterSecretResponse{}, err
	}
	_, _ = s.db.Exec(`UPDATE cluster_secrets SET last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, secretID)
	return ApplyClusterSecretResponse{
		ClusterID: clusterID,
		SecretID:  secretID,
		Name:      rec.Name,
		Namespace: rec.Namespace,
		Provider:  rec.Provider,
		Status:    "applied",
		Output:    output,
		Warnings:  providerWarnings(rec.Provider),
	}, nil
}

func (s *Service) ListClusterConfigMaps(clusterID int64) ([]ClusterConfigMapRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, cluster_id, name, namespace, description, data, immutable, is_global, last_applied_at, created_at, updated_at
		 FROM cluster_config_maps
		 WHERE cluster_id = $1 OR is_global = TRUE
		 ORDER BY is_global DESC, namespace ASC, name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cluster config maps: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterConfigMapRecord, 0)
	for rows.Next() {
		rec, scanErr := scanClusterConfigMap(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) CreateClusterConfigMap(clusterID int64, req UpsertClusterConfigMapRequest) (ClusterConfigMapRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ClusterConfigMapRecord{}, err
	}
	return s.createClusterConfigMap(clusterID, req)
}

func (s *Service) CreateGlobalConfigMap(req UpsertClusterConfigMapRequest) (ClusterConfigMapRecord, error) {
	req.IsGlobal = true
	return s.createClusterConfigMap(0, req)
}

func (s *Service) createClusterConfigMap(clusterID int64, req UpsertClusterConfigMapRequest) (ClusterConfigMapRecord, error) {
	req = normalizeClusterConfigMapRequest(req)
	if err := validateClusterConfigMapRequest(req); err != nil {
		return ClusterConfigMapRecord{}, err
	}
	dataJSON, err := json.Marshal(req.Data)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("encode config map data: %w", err)
	}
	var id int64
	clusterIDValue := sql.NullInt64{Int64: clusterID, Valid: clusterID > 0}
	err = s.db.QueryRow(
		`INSERT INTO cluster_config_maps (cluster_id, name, namespace, description, data, immutable, is_global)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		clusterIDValue, req.Name, req.Namespace, req.Description, dataJSON, req.Immutable, req.IsGlobal,
	).Scan(&id)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("create cluster config map: %w", err)
	}
	return s.GetClusterConfigMap(clusterID, id)
}

func (s *Service) GetClusterConfigMap(clusterID, configMapID int64) (ClusterConfigMapRecord, error) {
	if clusterID > 0 {
		if _, err := s.getClusterSummary(clusterID); err != nil {
			return ClusterConfigMapRecord{}, err
		}
	}
	row := s.db.QueryRow(
		`SELECT id, cluster_id, name, namespace, description, data, immutable, is_global, last_applied_at, created_at, updated_at
		 FROM cluster_config_maps
		 WHERE id = $2 AND (($1 > 0 AND cluster_id = $1) OR is_global = TRUE)`,
		clusterID,
		configMapID,
	)
	rec, err := scanClusterConfigMap(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClusterConfigMapRecord{}, ErrApplicationManifestNotFound
		}
		return ClusterConfigMapRecord{}, fmt.Errorf("get cluster config map: %w", err)
	}
	return rec, nil
}

func (s *Service) UpdateClusterConfigMap(clusterID, configMapID int64, req UpsertClusterConfigMapRequest) (ClusterConfigMapRecord, error) {
	if _, err := s.GetClusterConfigMap(clusterID, configMapID); err != nil {
		return ClusterConfigMapRecord{}, err
	}
	req = normalizeClusterConfigMapRequest(req)
	if err := validateClusterConfigMapRequest(req); err != nil {
		return ClusterConfigMapRecord{}, err
	}
	dataJSON, err := json.Marshal(req.Data)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("encode config map data: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE cluster_config_maps
		 SET name = $3, namespace = $4, description = $5, data = $6, immutable = $7, is_global = $8, updated_at = NOW()
		 WHERE cluster_id = $1 AND id = $2`,
		clusterID, configMapID, req.Name, req.Namespace, req.Description, dataJSON, req.Immutable, req.IsGlobal,
	)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("update cluster config map: %w", err)
	}
	return s.GetClusterConfigMap(clusterID, configMapID)
}

func (s *Service) DeleteClusterConfigMap(clusterID, configMapID int64) error {
	res, err := s.db.Exec(`DELETE FROM cluster_config_maps WHERE cluster_id = $1 AND id = $2`, clusterID, configMapID)
	if err != nil {
		return fmt.Errorf("delete cluster config map: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrApplicationManifestNotFound
	}
	return nil
}

func (s *Service) UpdateClusterConfigMapKey(clusterID, configMapID int64, key string, req UpdateConfigMapKeyRequest) (ClusterConfigMapRecord, error) {
	rec, err := s.GetClusterConfigMap(clusterID, configMapID)
	if err != nil {
		return ClusterConfigMapRecord{}, err
	}
	if rec.ClusterID != clusterID {
		return ClusterConfigMapRecord{}, errors.New("global config map can only be edited from its owning cluster")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ClusterConfigMapRecord{}, errors.New("config map key is required")
	}
	rec.Data[key] = req.Value
	dataJSON, err := json.Marshal(rec.Data)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("encode config map data: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE cluster_config_maps SET data = $3, updated_at = NOW() WHERE cluster_id = $1 AND id = $2`,
		clusterID,
		configMapID,
		dataJSON,
	)
	if err != nil {
		return ClusterConfigMapRecord{}, fmt.Errorf("update cluster config map key: %w", err)
	}
	return s.GetClusterConfigMap(clusterID, configMapID)
}

func (s *Service) ApplyClusterConfigMap(clusterID, configMapID int64) (ApplyClusterConfigMapResponse, error) {
	rec, err := s.GetClusterConfigMap(clusterID, configMapID)
	if err != nil {
		return ApplyClusterConfigMapResponse{}, err
	}
	output, err := s.applyClusterConfigMapRecord(rec)
	if err != nil {
		return ApplyClusterConfigMapResponse{}, err
	}
	_, _ = s.db.Exec(`UPDATE cluster_config_maps SET last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, configMapID)
	return ApplyClusterConfigMapResponse{
		ClusterID:   clusterID,
		ConfigMapID: configMapID,
		Name:        rec.Name,
		Namespace:   rec.Namespace,
		Status:      "applied",
		Output:      output,
	}, nil
}

func (s *Service) DeployApp(clusterID int64, req DeployAppRequest) (DeployAppResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return DeployAppResponse{}, err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "application"
	}
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	rawManifest := strings.TrimSpace(req.Manifest)
	if rawManifest == "" {
		return DeployAppResponse{}, errors.New("manifest is required")
	}
	hostname := strings.TrimSpace(req.Hostname)
	if hostname != "" && !isValidAppHostname(hostname) {
		return DeployAppResponse{}, fmt.Errorf("invalid hostname %q", hostname)
	}
	ingressClass := strings.TrimSpace(req.IngressClass)
	if ingressClass == "" {
		ingressClass = "traefik"
	}
	tlsSecretName := strings.TrimSpace(req.TLSSecretName)
	if hostname != "" && tlsSecretName == "" {
		tlsSecretName = sanitizeKubernetesName(name + "-tls")
	}
	tlsFullchainPEM := strings.TrimSpace(req.TLSFullchainPEM)
	tlsPrivateKeyPEM := strings.TrimSpace(req.TLSPrivateKeyPEM)
	if req.TLSCertificateID > 0 {
		if s.tlsCertificates == nil {
			return DeployAppResponse{}, errors.New("saved TLS certificates are not configured")
		}
		cert, err := s.tlsCertificates.GetByID(req.TLSCertificateID)
		if err != nil {
			return DeployAppResponse{}, fmt.Errorf("load saved TLS certificate: %w", err)
		}
		if tlsFullchainPEM != "" || tlsPrivateKeyPEM != "" {
			return DeployAppResponse{}, errors.New("select a saved TLS certificate or upload PEMs, not both")
		}
		tlsFullchainPEM = strings.TrimSpace(cert.FullchainPEM)
		tlsPrivateKeyPEM = strings.TrimSpace(cert.PrivateKeyPEM)
		if tlsSecretName == "" {
			tlsSecretName = sanitizeKubernetesName(cert.Name)
		}
	}
	hasTLSPEMs := tlsFullchainPEM != "" || tlsPrivateKeyPEM != ""
	if hasTLSPEMs {
		if tlsFullchainPEM == "" || tlsPrivateKeyPEM == "" {
			return DeployAppResponse{}, errors.New("both fullchain.pem and privkey.pem are required when uploading a TLS certificate")
		}
		if tlsSecretName == "" {
			tlsSecretName = sanitizeKubernetesName(name + "-tls")
		}
		if _, err := tls.X509KeyPair([]byte(tlsFullchainPEM), []byte(tlsPrivateKeyPEM)); err != nil {
			return DeployAppResponse{}, fmt.Errorf("invalid TLS certificate/key pair: %w", err)
		}
	}
	renderedManifest := rawManifest
	if hostname != "" {
		renderedManifest = renderedManifest + "\n---\n" + buildAppIngressManifest(name, hostname, ingressClass, tlsSecretName)
	}

	status := "app-prepared"
	output := ""
	warnings := []string{}
	if hostname != "" {
		warnings = append(warnings, fmt.Sprintf("application ingress will use hostname %s", hostname))
	}
	if hostname != "" && !hasTLSPEMs {
		warnings = append(warnings, fmt.Sprintf("no TLS PEMs were uploaded; ingress will reference existing secret %s", tlsSecretName))
	}
	selectedSecrets, err := s.resolveSelectedClusterSecrets(clusterID, namespace, req.SelectedSecretIDs)
	if err != nil {
		return DeployAppResponse{}, err
	}
	if len(selectedSecrets) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d predefined cluster secret(s) selected for this deployment", len(selectedSecrets)))
		warnings = append(warnings, fmt.Sprintf("selected secrets will be applied into namespace %s for this app deployment", namespace))
	}
	selectedConfigMaps, err := s.resolveSelectedClusterConfigMaps(clusterID, namespace, req.SelectedConfigMapIDs)
	if err != nil {
		return DeployAppResponse{}, err
	}
	if len(selectedConfigMaps) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d predefined cluster config map(s) selected for this deployment", len(selectedConfigMaps)))
		warnings = append(warnings, fmt.Sprintf("selected ConfigMaps will be applied into namespace %s for this app deployment", namespace))
	}
	appID, revisionID, revisionNumber, err := s.createApplicationManifestRevision(applicationManifestRevisionInput{
		ClusterID:        clusterID,
		Name:             name,
		Namespace:        namespace,
		Hostname:         hostname,
		IngressClass:     ingressClass,
		TLSSecretName:    tlsSecretName,
		TLSCertificateID: req.TLSCertificateID,
		RawManifest:      rawManifest,
		RenderedManifest: renderedManifest,
		Status:           status,
		Apply:            req.Apply,
	})
	if err != nil {
		return DeployAppResponse{}, err
	}
	if err := s.replaceApplicationManifestSecrets(appID, req.SelectedSecretIDs); err != nil {
		return DeployAppResponse{}, err
	}
	if err := s.replaceApplicationManifestConfigMaps(appID, req.SelectedConfigMapIDs); err != nil {
		return DeployAppResponse{}, err
	}
	if req.Apply {
		status = "app-deployed"
		applyOutput, err := s.executeAppManifestApply(namespace, renderedManifest, appTLSSecretSpec{
			Name:             tlsSecretName,
			FullchainPEM:     tlsFullchainPEM,
			PrivateKeyPEM:    tlsPrivateKeyPEM,
			CreateFromUpload: hasTLSPEMs,
		}, selectedSecrets, req.ApplySelectedSecrets, selectedConfigMaps, req.ApplyConfigMaps)
		output = applyOutput
		if err != nil {
			_ = s.updateApplicationManifestRevision(revisionID, "failed", output, err.Error())
			_ = s.insertProvisionRun(clusterID, "app-deploy", "failed", name, output)
			return DeployAppResponse{}, fmt.Errorf("failed to deploy app: %w", err)
		}
		warnings = append(warnings, "application manifest applied successfully")
	} else {
		warnings = append(warnings, "application manifest was not applied; enable apply to run kubectl")
	}
	if err := s.updateApplicationManifestRevision(revisionID, status, output, ""); err != nil {
		return DeployAppResponse{}, err
	}

	if err := s.insertProvisionRun(clusterID, "app-deploy", status, name, output); err != nil {
		return DeployAppResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return DeployAppResponse{}, err
	}

	return DeployAppResponse{
		ClusterID:      clusterID,
		Status:         status,
		Name:           name,
		Namespace:      namespace,
		AppID:          appID,
		RevisionID:     revisionID,
		Revision:       revisionNumber,
		Hostname:       hostname,
		IngressClass:   ingressClass,
		TLSSecretName:  tlsSecretName,
		SecretNames:    clusterSecretNames(selectedSecrets),
		ConfigMapNames: clusterConfigMapNames(selectedConfigMaps),
		Output:         output,
		Warnings:       warnings,
	}, nil
}

func (s *Service) ListApplicationManifests(clusterID int64) ([]ApplicationManifestRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT am.id, am.cluster_id, am.name, am.namespace, am.hostname, am.ingress_class,
		        am.tls_secret_name, COALESCE(am.tls_certificate_id, 0), COALESCE(am.latest_revision_id, 0),
		        COALESCE(r.revision_number, 0), COALESCE(r.status, ''), am.created_at, am.updated_at
		 FROM application_manifests am
		 LEFT JOIN application_manifest_revisions r ON r.id = am.latest_revision_id
		 WHERE am.cluster_id = $1
		 ORDER BY am.updated_at DESC, am.namespace ASC, am.name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list application manifests: %w", err)
	}
	defer rows.Close()

	out := make([]ApplicationManifestRecord, 0)
	for rows.Next() {
		var rec ApplicationManifestRecord
		if scanErr := rows.Scan(
			&rec.ID,
			&rec.ClusterID,
			&rec.Name,
			&rec.Namespace,
			&rec.Hostname,
			&rec.IngressClass,
			&rec.TLSSecretName,
			&rec.TLSCertificateID,
			&rec.LatestRevisionID,
			&rec.LatestRevision,
			&rec.LatestStatus,
			&rec.CreatedAt,
			&rec.UpdatedAt,
		); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) ListApplicationManifestRevisions(clusterID, appID int64) ([]ApplicationManifestRevisionRecord, error) {
	if err := s.ensureApplicationManifestBelongsToCluster(clusterID, appID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, application_manifest_id, cluster_id, revision_number, raw_manifest, rendered_manifest,
		        kubectl_output, status, error_message, apply, created_at
		 FROM application_manifest_revisions
		 WHERE cluster_id = $1 AND application_manifest_id = $2
		 ORDER BY revision_number DESC`,
		clusterID,
		appID,
	)
	if err != nil {
		return nil, fmt.Errorf("list application manifest revisions: %w", err)
	}
	defer rows.Close()

	out := make([]ApplicationManifestRevisionRecord, 0)
	for rows.Next() {
		rec, scanErr := scanApplicationManifestRevision(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) GetApplicationManifestRevision(clusterID, appID, revisionID int64) (ApplicationManifestRevisionRecord, error) {
	if err := s.ensureApplicationManifestBelongsToCluster(clusterID, appID); err != nil {
		return ApplicationManifestRevisionRecord{}, err
	}
	row := s.db.QueryRow(
		`SELECT id, application_manifest_id, cluster_id, revision_number, raw_manifest, rendered_manifest,
		        kubectl_output, status, error_message, apply, created_at
		 FROM application_manifest_revisions
		 WHERE cluster_id = $1 AND application_manifest_id = $2 AND id = $3`,
		clusterID,
		appID,
		revisionID,
	)
	rec, err := scanApplicationManifestRevision(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ApplicationManifestRevisionRecord{}, ErrApplicationManifestNotFound
		}
		return ApplicationManifestRevisionRecord{}, fmt.Errorf("get application manifest revision: %w", err)
	}
	return rec, nil
}

func (s *Service) RedeployApplicationManifest(clusterID, appID int64, req RedeployAppRequest) (DeployAppResponse, error) {
	app, err := s.getApplicationManifestRecord(clusterID, appID)
	if err != nil {
		return DeployAppResponse{}, err
	}
	revisionID := req.RevisionID
	if revisionID <= 0 {
		revisionID = app.LatestRevisionID
	}
	if revisionID <= 0 {
		return DeployAppResponse{}, errors.New("application has no revision to redeploy")
	}
	revision, err := s.GetApplicationManifestRevision(clusterID, appID, revisionID)
	if err != nil {
		return DeployAppResponse{}, err
	}

	tlsFullchainPEM := ""
	tlsPrivateKeyPEM := ""
	hasTLSPEMs := false
	if app.TLSCertificateID > 0 {
		if s.tlsCertificates == nil {
			return DeployAppResponse{}, errors.New("saved TLS certificates are not configured")
		}
		cert, loadErr := s.tlsCertificates.GetByID(app.TLSCertificateID)
		if loadErr != nil {
			return DeployAppResponse{}, fmt.Errorf("load saved TLS certificate: %w", loadErr)
		}
		tlsFullchainPEM = strings.TrimSpace(cert.FullchainPEM)
		tlsPrivateKeyPEM = strings.TrimSpace(cert.PrivateKeyPEM)
		hasTLSPEMs = tlsFullchainPEM != "" || tlsPrivateKeyPEM != ""
	}

	status := "app-prepared"
	output := ""
	newAppID, newRevisionID, newRevisionNumber, err := s.createApplicationManifestRevision(applicationManifestRevisionInput{
		ClusterID:        clusterID,
		Name:             app.Name,
		Namespace:        app.Namespace,
		Hostname:         app.Hostname,
		IngressClass:     app.IngressClass,
		TLSSecretName:    app.TLSSecretName,
		TLSCertificateID: app.TLSCertificateID,
		RawManifest:      revision.RawManifest,
		RenderedManifest: revision.RenderedManifest,
		Status:           status,
		Apply:            req.Apply,
	})
	if err != nil {
		return DeployAppResponse{}, err
	}

	warnings := []string{fmt.Sprintf("redeployed from revision %d", revision.RevisionNumber)}
	if req.Apply {
		status = "app-deployed"
		output, err = s.executeAppManifestApply(app.Namespace, revision.RenderedManifest, appTLSSecretSpec{
			Name:             app.TLSSecretName,
			FullchainPEM:     tlsFullchainPEM,
			PrivateKeyPEM:    tlsPrivateKeyPEM,
			CreateFromUpload: hasTLSPEMs,
		}, nil, false, nil, false)
		if err != nil {
			_ = s.updateApplicationManifestRevision(newRevisionID, "failed", output, err.Error())
			_ = s.insertProvisionRun(clusterID, "app-redeploy", "failed", app.Name, output)
			return DeployAppResponse{}, fmt.Errorf("failed to redeploy app: %w", err)
		}
		warnings = append(warnings, "application manifest redeployed successfully")
	} else {
		warnings = append(warnings, "application manifest redeploy was recorded but not applied; enable apply to run kubectl")
	}
	if err := s.updateApplicationManifestRevision(newRevisionID, status, output, ""); err != nil {
		return DeployAppResponse{}, err
	}
	if err := s.insertProvisionRun(clusterID, "app-redeploy", status, app.Name, output); err != nil {
		return DeployAppResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return DeployAppResponse{}, err
	}

	return DeployAppResponse{
		ClusterID:     clusterID,
		Status:        status,
		Name:          app.Name,
		Namespace:     app.Namespace,
		AppID:         newAppID,
		RevisionID:    newRevisionID,
		Revision:      newRevisionNumber,
		Hostname:      app.Hostname,
		IngressClass:  app.IngressClass,
		TLSSecretName: app.TLSSecretName,
		Output:        output,
		Warnings:      warnings,
	}, nil
}

func (s *Service) executeMetalLBDeployment(namespace, version, manifestDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// Use the kubeconfig from the mounted location or configured env.
	kubeconfigPath := s.kubeconfigPath()
	kubeconfigPath, err := s.resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return err
	}

	// Test kubectl connectivity
	testCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "cluster-info")
	if output, err := testCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl cluster connectivity test failed: %w\nOutput: %s", err, string(output))
	}

	// Step 1: Apply MetalLB controller manifest
	manifestURL := fmt.Sprintf("https://raw.githubusercontent.com/metallb/metallb/%s/config/manifests/metallb-native.yaml", version)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", manifestURL, "--validate=false")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to apply metallb controller manifest: %w\nOutput: %s", err, string(output))
	}

	// Step 2: Wait for MetalLB controller to be ready. Fresh clusters can spend
	// several minutes pulling images before the deployment becomes available.
	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "rollout", "status", "--namespace", namespace, "deployment/controller", "--timeout=600s")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to wait for metallb controller: %w\nOutput: %s\nDiagnostics:\n%s", err, string(output), s.metalLBDiagnostics(kubeconfigPath, namespace))
	}

	// Step 3: Apply cluster-specific MetalLB resources
	configPath := filepath.Join(manifestDir, "metallb-resources.yaml")
	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", configPath, "--validate=false")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to apply metallb resources: %w\nOutput: %s", err, string(output))
	}

	return nil
}

func (s *Service) executeIngressControllerDeployment(namespace, image string, wait bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return "", err
	}

	outputs := make([]string, 0, 3)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "cluster-info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("kubectl cluster connectivity test failed: %w\nOutput: %s", err, string(output))
	}
	outputs = append(outputs, string(output))

	manifest := buildTraefikIngressControllerManifest(namespace, image)
	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(manifest)
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("apply traefik ingress controller manifest: %w\nOutput: %s", err, string(output))
	}

	if wait {
		cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "rollout", "status", "--namespace", namespace, "deployment/traefik", "--timeout=600s")
		output, err = cmd.CombinedOutput()
		outputs = append(outputs, string(output))
		if err != nil {
			return strings.Join(outputs, "\n"), fmt.Errorf("wait for traefik rollout: %w\nOutput: %s\nDiagnostics:\n%s", err, string(output), s.kubeNamespaceDiagnostics(kubeconfigPath, namespace, "deployment/traefik"))
		}
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "svc", "--namespace", namespace, "-o", "wide")
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("get ingress services: %w\nOutput: %s", err, string(output))
	}

	return strings.Join(outputs, "\n"), nil
}

func (s *Service) executeCertificateManagerDeployment(namespace, version, issuerName, email, acmeServer, ingressClass string, wait bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return "", err
	}

	outputs := make([]string, 0, 5)
	manifestURL := fmt.Sprintf("https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml", version)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "cluster-info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("kubectl cluster connectivity test failed: %w\nOutput: %s", err, string(output))
	}
	outputs = append(outputs, string(output))

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", manifestURL, "--validate=false")
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("apply cert-manager manifest: %w\nOutput: %s", err, string(output))
	}

	if wait {
		for _, deployment := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
			cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "rollout", "status", "--namespace", namespace, "deployment/"+deployment, "--timeout=600s")
			output, err = cmd.CombinedOutput()
			outputs = append(outputs, string(output))
			if err != nil {
				return strings.Join(outputs, "\n"), fmt.Errorf("wait for %s rollout: %w\nOutput: %s\nDiagnostics:\n%s", deployment, err, string(output), s.kubeNamespaceDiagnostics(kubeconfigPath, namespace, "deployment/"+deployment))
			}
		}
	}

	issuerManifest := buildLetsEncryptClusterIssuerManifest(issuerName, email, acmeServer, ingressClass)
	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(issuerManifest)
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("apply Let's Encrypt ClusterIssuer: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "clusterissuer", issuerName, "-o", "wide")
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("get ClusterIssuer: %w\nOutput: %s", err, string(output))
	}

	return strings.Join(outputs, "\n"), nil
}

type appTLSSecretSpec struct {
	Name             string
	FullchainPEM     string
	PrivateKeyPEM    string
	CreateFromUpload bool
}

func (s *Service) executeAppManifestApply(namespace, manifest string, tlsSecret appTLSSecretSpec, selectedSecrets []ClusterSecretRecord, applySelectedSecrets bool, selectedConfigMaps []ClusterConfigMapRecord, applyConfigMaps bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return "", err
	}

	outputs := make([]string, 0, 3)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "namespace", namespace)
	if output, err := cmd.CombinedOutput(); err != nil {
		createCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "create", "namespace", namespace)
		createOutput, createErr := createCmd.CombinedOutput()
		outputs = append(outputs, string(output), string(createOutput))
		if createErr != nil {
			return strings.Join(outputs, "\n"), fmt.Errorf("ensure namespace %s: %w\nOutput: %s", namespace, createErr, string(createOutput))
		}
	} else {
		outputs = append(outputs, string(output))
	}

	if tlsSecret.CreateFromUpload {
		secretOutput, err := s.applyTLSSecret(ctx, kubeconfigPath, namespace, tlsSecret)
		outputs = append(outputs, secretOutput)
		if err != nil {
			return strings.Join(outputs, "\n"), err
		}
	}
	if applySelectedSecrets {
		for _, selected := range selectedSecrets {
			secretOutput, err := s.applyClusterSecretRecordWithContext(ctx, kubeconfigPath, selected)
			outputs = append(outputs, secretOutput)
			if err != nil {
				return strings.Join(outputs, "\n"), err
			}
			_, _ = s.db.Exec(`UPDATE cluster_secrets SET last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, selected.ID)
		}
	}
	if applyConfigMaps {
		for _, selected := range selectedConfigMaps {
			configMapOutput, err := s.applyClusterConfigMapRecordWithContext(ctx, kubeconfigPath, selected)
			outputs = append(outputs, configMapOutput)
			if err != nil {
				return strings.Join(outputs, "\n"), err
			}
			_, _ = s.db.Exec(`UPDATE cluster_config_maps SET last_applied_at = NOW(), updated_at = NOW() WHERE id = $1`, selected.ID)
		}
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "--namespace", namespace, "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(manifest)
	output, err := cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("apply application manifest: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "all,ingress", "--namespace", namespace)
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		return strings.Join(outputs, "\n"), fmt.Errorf("get application resources: %w\nOutput: %s", err, string(output))
	}

	cmd = exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "describe", "ingress", "--namespace", namespace)
	output, err = cmd.CombinedOutput()
	outputs = append(outputs, string(output))
	if err != nil {
		outputs = append(outputs, fmt.Sprintf("describe ingress warning: %v", err))
	}

	return strings.Join(outputs, "\n"), nil
}

func (s *Service) applyTLSSecret(ctx context.Context, kubeconfigPath, namespace string, secret appTLSSecretSpec) (string, error) {
	if strings.TrimSpace(secret.Name) == "" {
		return "", errors.New("tls secret name is required")
	}
	tmpDir, err := os.MkdirTemp("", "kpm-app-tls-*")
	if err != nil {
		return "", fmt.Errorf("create temporary tls workspace: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	certPath := filepath.Join(tmpDir, "tls.crt")
	keyPath := filepath.Join(tmpDir, "tls.key")
	if err := os.WriteFile(certPath, []byte(secret.FullchainPEM), 0o600); err != nil {
		return "", fmt.Errorf("write temporary fullchain.pem: %w", err)
	}
	if err := os.WriteFile(keyPath, []byte(secret.PrivateKeyPEM), 0o600); err != nil {
		return "", fmt.Errorf("write temporary privkey.pem: %w", err)
	}

	createCmd := exec.CommandContext(
		ctx,
		"kubectl",
		"--kubeconfig", kubeconfigPath,
		"--namespace", namespace,
		"create", "secret", "tls", secret.Name,
		"--cert", certPath,
		"--key", keyPath,
		"--dry-run=client",
		"-o", "yaml",
	)
	secretManifest, err := createCmd.CombinedOutput()
	if err != nil {
		return string(secretManifest), fmt.Errorf("render TLS secret %s: %w\nOutput: %s", secret.Name, err, string(secretManifest))
	}

	applyCmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "--namespace", namespace, "-f", "-", "--validate=false")
	applyCmd.Stdin = strings.NewReader(string(secretManifest))
	output, err := applyCmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("apply TLS secret %s: %w\nOutput: %s", secret.Name, err, string(output))
	}

	return string(output), nil
}

type clusterSecretScanner interface {
	Scan(dest ...interface{}) error
}

type clusterConfigMapScanner interface {
	Scan(dest ...interface{}) error
}

func scanClusterConfigMap(row clusterConfigMapScanner) (ClusterConfigMapRecord, error) {
	var rec ClusterConfigMapRecord
	var clusterID sql.NullInt64
	var dataRaw []byte
	var lastApplied sql.NullTime
	err := row.Scan(
		&rec.ID,
		&clusterID,
		&rec.Name,
		&rec.Namespace,
		&rec.Description,
		&dataRaw,
		&rec.Immutable,
		&rec.IsGlobal,
		&lastApplied,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		return ClusterConfigMapRecord{}, err
	}
	if clusterID.Valid {
		rec.ClusterID = clusterID.Int64
	}
	if len(dataRaw) > 0 {
		_ = json.Unmarshal(dataRaw, &rec.Data)
	}
	if rec.Data == nil {
		rec.Data = map[string]string{}
	}
	if lastApplied.Valid {
		rec.LastAppliedAt = &lastApplied.Time
	}
	return rec, nil
}

func (s *Service) scanClusterSecret(row clusterSecretScanner) (ClusterSecretRecord, error) {
	var rec ClusterSecretRecord
	var clusterID sql.NullInt64
	var configRaw []byte
	var lastApplied sql.NullTime
	err := row.Scan(
		&rec.ID,
		&clusterID,
		&rec.Name,
		&rec.Namespace,
		&rec.Provider,
		&rec.Type,
		&rec.Description,
		&rec.ExternalRef,
		&configRaw,
		&rec.Immutable,
		&rec.IsGlobal,
		&lastApplied,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		return ClusterSecretRecord{}, err
	}
	if clusterID.Valid {
		rec.ClusterID = clusterID.Int64
	}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &rec.ProviderConfig)
	}
	if rec.ProviderConfig == nil {
		rec.ProviderConfig = map[string]interface{}{}
	}
	if lastApplied.Valid {
		rec.LastAppliedAt = &lastApplied.Time
	}
	keys, err := s.clusterSecretKeys(rec.ID)
	if err != nil {
		return ClusterSecretRecord{}, err
	}
	rec.Keys = keys
	return rec, nil
}

func (s *Service) clusterSecretKeys(secretID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT key FROM cluster_secret_items WHERE cluster_secret_id = $1 ORDER BY key ASC`, secretID)
	if err != nil {
		return nil, fmt.Errorf("list cluster secret keys: %w", err)
	}
	defer rows.Close()
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err == nil {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Service) replaceClusterSecretItems(clusterID, secretID int64, secretName string, data map[string]string) error {
	if data == nil {
		return nil
	}
	if s.secrets == nil {
		return errors.New("secret store is not configured")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin cluster secret item update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cluster_secret_items WHERE cluster_secret_id = $1`, secretID); err != nil {
		return fmt.Errorf("clear cluster secret items: %w", err)
	}
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("secret data keys must be non-empty")
		}
		ref := ""
		sum := sha256.Sum256([]byte(value))
		if value != "" {
			var err error
			ref, err = s.secrets.Put(context.Background(), fmt.Sprintf("clusters/%d/app-secrets/%d", clusterID, secretID), key, []byte(value))
			if err != nil {
				return fmt.Errorf("store cluster secret item %s: %w", key, err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO cluster_secret_items (cluster_secret_id, key, secret_ref, value_sha256)
			 VALUES ($1, $2, $3, $4)`,
			secretID, key, ref, hex.EncodeToString(sum[:]),
		); err != nil {
			return fmt.Errorf("insert cluster secret item %s: %w", key, err)
		}
	}
	return tx.Commit()
}

func (s *Service) resolveSelectedClusterSecrets(clusterID int64, appNamespace string, ids []int64) ([]ClusterSecretRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]ClusterSecretRecord, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		rec, err := s.GetClusterSecret(clusterID, id)
		if err != nil {
			return nil, err
		}
		rec.Namespace = appNamespace
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) resolveSelectedClusterConfigMaps(clusterID int64, appNamespace string, ids []int64) ([]ClusterConfigMapRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]ClusterConfigMapRecord, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		rec, err := s.GetClusterConfigMap(clusterID, id)
		if err != nil {
			return nil, err
		}
		rec.Namespace = appNamespace
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) replaceApplicationManifestSecrets(appID int64, secretIDs []int64) error {
	_, err := s.db.Exec(`DELETE FROM application_manifest_secrets WHERE application_manifest_id = $1`, appID)
	if err != nil {
		return fmt.Errorf("clear application secret selections: %w", err)
	}
	for _, secretID := range secretIDs {
		if secretID <= 0 {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO application_manifest_secrets (application_manifest_id, cluster_secret_id)
			 VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			appID,
			secretID,
		); err != nil {
			return fmt.Errorf("record application secret selection: %w", err)
		}
	}
	return nil
}

func (s *Service) replaceApplicationManifestConfigMaps(appID int64, configMapIDs []int64) error {
	_, err := s.db.Exec(`DELETE FROM application_manifest_config_maps WHERE application_manifest_id = $1`, appID)
	if err != nil {
		return fmt.Errorf("clear application config map selections: %w", err)
	}
	for _, configMapID := range configMapIDs {
		if configMapID <= 0 {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO application_manifest_config_maps (application_manifest_id, cluster_config_map_id)
			 VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			appID,
			configMapID,
		); err != nil {
			return fmt.Errorf("record application config map selection: %w", err)
		}
	}
	return nil
}

func (s *Service) applyClusterConfigMapRecord(rec ClusterConfigMapRecord) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return "", err
	}
	return s.applyClusterConfigMapRecordWithContext(ctx, kubeconfigPath, rec)
}

func (s *Service) applyClusterConfigMapRecordWithContext(ctx context.Context, kubeconfigPath string, rec ClusterConfigMapRecord) (string, error) {
	manifest := buildNativeKubernetesConfigMapManifest(rec)
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "--namespace", rec.Namespace, "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(manifest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("apply cluster config map %s/%s: %w\nOutput: %s", rec.Namespace, rec.Name, err, string(output))
	}
	return string(output), nil
}

func (s *Service) applyClusterSecretRecord(rec ClusterSecretRecord) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return "", err
	}
	return s.applyClusterSecretRecordWithContext(ctx, kubeconfigPath, rec)
}

func (s *Service) applyClusterSecretRecordWithContext(ctx context.Context, kubeconfigPath string, rec ClusterSecretRecord) (string, error) {
	manifest, err := s.buildClusterSecretApplyManifest(ctx, rec)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "--namespace", rec.Namespace, "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(manifest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("apply cluster secret %s/%s: %w\nOutput: %s", rec.Namespace, rec.Name, err, string(output))
	}
	return string(output), nil
}

func (s *Service) buildClusterSecretApplyManifest(ctx context.Context, rec ClusterSecretRecord) (string, error) {
	switch rec.Provider {
	case "native":
		values, err := s.loadClusterSecretValues(ctx, rec.ID)
		if err != nil {
			return "", err
		}
		return buildNativeKubernetesSecretManifest(rec, values), nil
	case "external-secrets-operator", "hashicorp-vault", "aws-secrets-manager", "azure-key-vault", "google-secret-manager":
		return buildExternalSecretManifest(rec), nil
	case "bitnami-sealed-secrets":
		if strings.TrimSpace(rec.ExternalRef) == "" {
			return "", errors.New("sealed secrets provider requires external_ref to contain a SealedSecret manifest")
		}
		return rec.ExternalRef, nil
	default:
		return "", fmt.Errorf("unsupported secret provider %q", rec.Provider)
	}
}

func (s *Service) loadClusterSecretValues(ctx context.Context, secretID int64) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, COALESCE(secret_ref, '') FROM cluster_secret_items WHERE cluster_secret_id = $1 ORDER BY key ASC`, secretID)
	if err != nil {
		return nil, fmt.Errorf("load cluster secret items: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, ref string
		if err := rows.Scan(&key, &ref); err != nil {
			return nil, err
		}
		if ref == "" {
			continue
		}
		value, err := s.secrets.Get(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("load cluster secret item %s: %w", key, err)
		}
		out[key] = string(value)
	}
	return out, nil
}

func normalizeClusterSecretRequest(req UpsertClusterSecretRequest) UpsertClusterSecretRequest {
	req.Name = sanitizeKubernetesName(req.Name)
	req.Namespace = sanitizeKubernetesName(req.Namespace)
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	req.Provider = strings.TrimSpace(req.Provider)
	if req.Provider == "" {
		req.Provider = "native"
	}
	req.Type = strings.TrimSpace(req.Type)
	if req.Type == "" {
		req.Type = "Opaque"
	}
	req.Description = strings.TrimSpace(req.Description)
	req.ExternalRef = strings.TrimSpace(req.ExternalRef)
	if req.ProviderConfig == nil {
		req.ProviderConfig = map[string]interface{}{}
	}
	return req
}

func normalizeClusterConfigMapRequest(req UpsertClusterConfigMapRequest) UpsertClusterConfigMapRequest {
	req.Name = sanitizeKubernetesName(req.Name)
	req.Namespace = sanitizeKubernetesName(req.Namespace)
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	req.Description = strings.TrimSpace(req.Description)
	if req.Data == nil {
		req.Data = map[string]string{}
	}
	return req
}

func validateClusterConfigMapRequest(req UpsertClusterConfigMapRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.Namespace == "" {
		return errors.New("namespace is required")
	}
	if len(req.Data) == 0 {
		return errors.New("config map requires at least one data key")
	}
	for key := range req.Data {
		if strings.TrimSpace(key) == "" {
			return errors.New("config map data keys must be non-empty")
		}
	}
	return nil
}

func validateClusterSecretRequest(req UpsertClusterSecretRequest, requireData bool) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.Namespace == "" {
		return errors.New("namespace is required")
	}
	switch req.Provider {
	case "native":
		if requireData && len(req.Data) == 0 {
			return errors.New("native secrets require at least one data key")
		}
	case "external-secrets-operator", "hashicorp-vault", "aws-secrets-manager", "azure-key-vault", "google-secret-manager":
		if req.ExternalRef == "" {
			return errors.New("external_ref is required for external secret providers")
		}
		if requireData && len(req.Data) == 0 {
			return errors.New("external secret providers require key names in data")
		}
	case "bitnami-sealed-secrets":
		if req.ExternalRef == "" {
			return errors.New("bitnami sealed secrets provider requires external_ref to contain a SealedSecret manifest")
		}
	default:
		return fmt.Errorf("unsupported secret provider %q", req.Provider)
	}
	return nil
}

func buildNativeKubernetesSecretManifest(rec ClusterSecretRecord, values map[string]string) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n  namespace: %s\ntype: %s\n", rec.Name, rec.Namespace, rec.Type))
	if rec.Immutable {
		b.WriteString("immutable: true\n")
	}
	b.WriteString("data:\n")
	for _, key := range sortedMapKeys(values) {
		b.WriteString(fmt.Sprintf("  %s: %s\n", key, base64.StdEncoding.EncodeToString([]byte(values[key]))))
	}
	return b.String()
}

func buildNativeKubernetesConfigMapManifest(rec ClusterConfigMapRecord) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n  namespace: %s\n", rec.Name, rec.Namespace))
	if rec.Immutable {
		b.WriteString("immutable: true\n")
	}
	b.WriteString("data:\n")
	for _, key := range sortedMapKeys(rec.Data) {
		b.WriteString(fmt.Sprintf("  %s: %s\n", key, quoteYAMLString(rec.Data[key])))
	}
	return b.String()
}

func buildExternalSecretManifest(rec ClusterSecretRecord) string {
	storeName := stringConfig(rec.ProviderConfig, "secret_store_name", "cluster-secret-store")
	storeKind := stringConfig(rec.ProviderConfig, "secret_store_kind", "ClusterSecretStore")
	refresh := stringConfig(rec.ProviderConfig, "refresh_interval", "1h")
	var b strings.Builder
	b.WriteString("apiVersion: external-secrets.io/v1beta1\nkind: ExternalSecret\nmetadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n  namespace: %s\nspec:\n", rec.Name, rec.Namespace))
	b.WriteString(fmt.Sprintf("  refreshInterval: %s\n", refresh))
	b.WriteString("  secretStoreRef:\n")
	b.WriteString(fmt.Sprintf("    name: %s\n    kind: %s\n", storeName, storeKind))
	b.WriteString("  target:\n")
	b.WriteString(fmt.Sprintf("    name: %s\n    creationPolicy: Owner\n", rec.Name))
	b.WriteString("  data:\n")
	for _, key := range rec.Keys {
		b.WriteString(fmt.Sprintf("    - secretKey: %s\n      remoteRef:\n        key: %s\n        property: %s\n", key, rec.ExternalRef, key))
	}
	return b.String()
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringConfig(config map[string]interface{}, key, fallback string) string {
	if raw, ok := config[key]; ok {
		if val := strings.TrimSpace(fmt.Sprint(raw)); val != "" {
			return val
		}
	}
	return fallback
}

func clusterSecretNames(records []ClusterSecretRecord) []string {
	out := make([]string, 0, len(records))
	for _, rec := range records {
		out = append(out, fmt.Sprintf("%s/%s", rec.Namespace, rec.Name))
	}
	return out
}

func clusterConfigMapNames(records []ClusterConfigMapRecord) []string {
	out := make([]string, 0, len(records))
	for _, rec := range records {
		out = append(out, fmt.Sprintf("%s/%s", rec.Namespace, rec.Name))
	}
	return out
}

func quoteYAMLString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func providerWarnings(provider string) []string {
	switch provider {
	case "native":
		return nil
	case "external-secrets-operator", "hashicorp-vault", "aws-secrets-manager", "azure-key-vault", "google-secret-manager":
		return []string{"External Secrets Operator and the referenced SecretStore/ClusterSecretStore must already be installed in the cluster."}
	case "bitnami-sealed-secrets":
		return []string{"Bitnami Sealed Secrets requires the sealed-secrets controller and a pre-encrypted SealedSecret manifest."}
	default:
		return nil
	}
}

func (s *Service) metalLBDiagnostics(kubeconfigPath, namespace string) string {
	return s.kubeNamespaceDiagnostics(kubeconfigPath, namespace, "deployment/controller")
}

func (s *Service) kubeNamespaceDiagnostics(kubeconfigPath, namespace, workload string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	commands := [][]string{
		{"get", workload, "--namespace", namespace, "-o", "wide"},
		{"describe", workload, "--namespace", namespace},
		{"get", "pods", "--namespace", namespace, "-o", "wide"},
		{"get", "events", "--namespace", namespace, "--sort-by=.lastTimestamp"},
	}
	sections := make([]string, 0, len(commands))
	for _, args := range commands {
		fullArgs := append([]string{"--kubeconfig", kubeconfigPath}, args...)
		cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)
		output, err := cmd.CombinedOutput()
		title := "kubectl " + strings.Join(args, " ")
		if err != nil {
			sections = append(sections, fmt.Sprintf("### %s\nerror: %v\n%s", title, err, string(output)))
			continue
		}
		sections = append(sections, fmt.Sprintf("### %s\n%s", title, string(output)))
	}
	return strings.Join(sections, "\n")
}

func (s *Service) exportBootstrapKubeconfig(workDir string) (string, error) {
	localKubeconfig := filepath.Join(workDir, "admin.conf")
	data, err := os.ReadFile(localKubeconfig)
	if err != nil {
		return "", fmt.Errorf("read fetched kubeconfig %s: %w", localKubeconfig, err)
	}

	destPath := s.kubeconfigPath()
	if info, err := os.Stat(destPath); err == nil && info.IsDir() {
		destPath = filepath.Join(destPath, "config")
	}
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure kubeconfig mount directory %s: %w", destDir, err)
	}

	if err := os.WriteFile(destPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write kubeconfig to %s: %w", destPath, err)
	}

	return destPath, nil
}

func (s *Service) kubeconfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("KUBECONFIG")); envPath != "" {
		return envPath
	}
	return "/app/backend/.kube/config"
}

func (s *Service) resolveKubeconfigPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("kubeconfig file not found at %s: %w", path, err)
		}
		return "", fmt.Errorf("failed to stat kubeconfig path %s: %w", path, err)
	}
	if info.IsDir() {
		candidate := filepath.Join(path, "config")
		candidateInfo, candidateErr := os.Stat(candidate)
		if candidateErr == nil && !candidateInfo.IsDir() {
			return candidate, nil
		}
		return "", fmt.Errorf("kubeconfig path %s is a directory; mount the kubeconfig file, not a directory", path)
	}
	return path, nil
}

func (s *Service) StartKubectlCommand(clusterID int64, req KubectlCommandRequest) (KubectlCommandRun, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return KubectlCommandRun{}, err
	}

	rawCommand := strings.TrimSpace(req.Command)
	commands, err := parseKubectlCommands(rawCommand)
	if err != nil {
		return KubectlCommandRun{}, err
	}

	kubeconfigPath, err := s.resolveKubeconfigPath(s.kubeconfigPath())
	if err != nil {
		return KubectlCommandRun{}, err
	}

	runID, err := newKubectlRunID()
	if err != nil {
		return KubectlCommandRun{}, err
	}

	run := &kubectlCommandRunState{
		KubectlCommandRun: KubectlCommandRun{
			ID:        runID,
			ClusterID: clusterID,
			Command:   formatKubectlCommands(commands),
			Status:    "running",
			StartedAt: time.Now().UTC(),
		},
	}

	s.kubectlRunsMu.Lock()
	s.kubectlRuns[runID] = run
	s.kubectlRunsMu.Unlock()

	go s.executeKubectlCommands(run, kubeconfigPath, commands)

	return run.snapshot(), nil
}

func (s *Service) GetKubectlCommandRun(clusterID int64, runID string) (KubectlCommandRun, error) {
	s.kubectlRunsMu.RLock()
	run := s.kubectlRuns[runID]
	s.kubectlRunsMu.RUnlock()
	if run == nil || run.ClusterID != clusterID {
		return KubectlCommandRun{}, errors.New("kubectl command run not found")
	}
	return run.snapshot(), nil
}

func (s *Service) executeKubectlCommands(run *kubectlCommandRunState, kubeconfigPath string, commands [][]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for index, args := range commands {
		if ctx.Err() != nil {
			break
		}
		if len(commands) > 1 {
			run.appendOutput(fmt.Sprintf("### Command %d/%d: kubectl %s\n", index+1, len(commands), strings.Join(args, " ")))
		}
		if err := runKubectlCommand(ctx, run, kubeconfigPath, args); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				run.finish("failed", "kubectl command sequence timed out after 10 minutes")
				return
			}
			run.finish("failed", err.Error())
			return
		}
		if len(commands) > 1 {
			run.appendOutput("\n")
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		run.finish("failed", "kubectl command sequence timed out after 10 minutes")
		return
	}
	run.finish("succeeded", "")
}

func runKubectlCommand(ctx context.Context, run *kubectlCommandRunState, kubeconfigPath string, args []string) error {
	fullArgs := append([]string{"--kubeconfig", kubeconfigPath}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", fullArgs...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start kubectl: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go copyKubectlOutput(run, stdout, &wg)
	go copyKubectlOutput(run, stderr, &wg)

	err = cmd.Wait()
	wg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func copyKubectlOutput(run *kubectlCommandRunState, reader io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			run.appendOutput(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

func (r *kubectlCommandRunState) appendOutput(chunk string) {
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	r.Output += chunk
}

func (r *kubectlCommandRunState) finish(status, errText string) {
	now := time.Now().UTC()
	r.outputMu.Lock()
	defer r.outputMu.Unlock()
	r.Status = status
	r.Error = errText
	r.FinishedAt = &now
}

func (r *kubectlCommandRunState) snapshot() KubectlCommandRun {
	r.outputMu.RLock()
	defer r.outputMu.RUnlock()
	return KubectlCommandRun{
		ID:         r.ID,
		ClusterID:  r.ClusterID,
		Command:    r.Command,
		Status:     r.Status,
		Output:     r.Output,
		Error:      r.Error,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
	}
}

func newKubectlRunID() (string, error) {
	tokenBytes := make([]byte, 12)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate kubectl run id: %w", err)
	}
	return hex.EncodeToString(tokenBytes), nil
}

func parseKubectlCommands(command string) ([][]string, error) {
	if command == "" {
		return nil, errors.New("kubectl command is required")
	}
	lines := strings.Split(command, "\n")
	commands := make([][]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "$ ")
		line = strings.TrimPrefix(line, "> ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		args, err := parseKubectlCommand(line)
		if err != nil {
			return nil, err
		}
		commands = append(commands, args)
	}
	if len(commands) == 0 {
		return nil, errors.New("kubectl command is required")
	}
	return commands, nil
}

func parseKubectlCommand(command string) ([]string, error) {
	parts, err := splitCommandFields(command)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, errors.New("kubectl command is required")
	}
	if parts[0] == "kubectl" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return nil, errors.New("kubectl arguments are required")
	}
	for _, part := range parts {
		if part == "--kubeconfig" || strings.HasPrefix(part, "--kubeconfig=") {
			return nil, errors.New("do not include --kubeconfig; the cluster kubeconfig is selected automatically")
		}
	}
	return parts, nil
}

func formatKubectlCommands(commands [][]string) string {
	lines := make([]string, 0, len(commands))
	for _, args := range commands {
		lines = append(lines, "kubectl "+strings.Join(args, " "))
	}
	return strings.Join(lines, "\n")
}

func splitCommandFields(command string) ([]string, error) {
	fields := []string{}
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in kubectl command")
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields, nil
}

func (s *Service) GetMetalLB(clusterID int64) (MetalLBStatusResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return MetalLBStatusResponse{}, err
	}

	var out MetalLBStatusResponse
	var addressPoolsJSON []byte
	var interfacesJSON []byte
	err := s.db.QueryRow(
		`SELECT cluster_id, namespace, version, address_pools, interfaces, status, manifest_dir, updated_at
		 FROM metallb_configs
		 WHERE cluster_id = $1`,
		clusterID,
	).Scan(
		&out.ClusterID,
		&out.Namespace,
		&out.Version,
		&addressPoolsJSON,
		&interfacesJSON,
		&out.Status,
		&out.ManifestDir,
		&out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MetalLBStatusResponse{}, errors.New("metallb not configured for this cluster")
		}
		return MetalLBStatusResponse{}, fmt.Errorf("get metallb config: %w", err)
	}

	if err := json.Unmarshal(addressPoolsJSON, &out.AddressPools); err != nil {
		return MetalLBStatusResponse{}, fmt.Errorf("decode metallb address pools: %w", err)
	}
	if err := json.Unmarshal(interfacesJSON, &out.Interfaces); err != nil {
		return MetalLBStatusResponse{}, fmt.Errorf("decode metallb interfaces: %w", err)
	}

	out.Commands = buildMetalLBCommands(out.ManifestDir)

	return out, nil
}

func (s *Service) GetObservability(clusterID int64) (ObservabilityStatusResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return ObservabilityStatusResponse{}, err
	}

	var out ObservabilityStatusResponse
	err := s.db.QueryRow(
		`SELECT cluster_id, grafana_admin_user, retention_days, storage_class, status, COALESCE(dashboard_url, ''), updated_at
		 FROM observability_configs
		 WHERE cluster_id = $1`,
		clusterID,
	).Scan(&out.ClusterID, &out.GrafanaAdminUser, &out.RetentionDays, &out.StorageClass, &out.Status, &out.DashboardURL, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ObservabilityStatusResponse{}, errors.New("observability not configured for this cluster")
		}
		return ObservabilityStatusResponse{}, fmt.Errorf("get observability config: %w", err)
	}

	return out, nil
}

func (s *Service) CreateEtcdSnapshot(clusterID int64, req CreateEtcdSnapshotRequest) (CreateEtcdSnapshotResponse, error) {
	cluster, err := s.getClusterSummary(clusterID)
	if err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}

	storageTarget := strings.TrimSpace(req.StorageTarget)
	if storageTarget == "" {
		storageTarget = filepath.Join("backups", "etcd")
	}

	snapshotName := fmt.Sprintf("%s-etcd-%s.db", sanitizePathToken(cluster.Name), time.Now().UTC().Format("20060102-150405"))
	snapshotPath := filepath.Join(storageTarget, snapshotName)
	status := "snapshot-planned"
	warning := "snapshot record prepared; execute etcdctl snapshot save from control-plane node to materialize backup"
	if req.Apply {
		status = "snapshot-requested"
	}

	var snapshotID int64
	var createdAt time.Time
	err = s.db.QueryRow(
		`INSERT INTO etcd_snapshots (cluster_id, snapshot_name, storage_target, snapshot_path, status)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		clusterID,
		snapshotName,
		storageTarget,
		snapshotPath,
		status,
	).Scan(&snapshotID, &createdAt)
	if err != nil {
		return CreateEtcdSnapshotResponse{}, fmt.Errorf("insert etcd snapshot record: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "etcd-snapshot", status, storageTarget, snapshotPath); err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return CreateEtcdSnapshotResponse{}, err
	}

	return CreateEtcdSnapshotResponse{
		ClusterID:    clusterID,
		SnapshotID:   snapshotID,
		SnapshotName: snapshotName,
		SnapshotPath: snapshotPath,
		Status:       status,
		CreatedAt:    createdAt,
		Warning:      warning,
	}, nil
}

func (s *Service) ListEtcdSnapshots(clusterID int64) ([]EtcdSnapshotRecord, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		`SELECT id, snapshot_name, storage_target, snapshot_path, status, created_at
		 FROM etcd_snapshots
		 WHERE cluster_id = $1
		 ORDER BY created_at DESC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list etcd snapshots: %w", err)
	}
	defer rows.Close()

	out := make([]EtcdSnapshotRecord, 0)
	for rows.Next() {
		var r EtcdSnapshotRecord
		if scanErr := rows.Scan(&r.ID, &r.SnapshotName, &r.StorageTarget, &r.SnapshotPath, &r.Status, &r.CreatedAt); scanErr != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) RestoreEtcdSnapshot(clusterID int64, req RestoreEtcdSnapshotRequest) (RestoreEtcdSnapshotResponse, error) {
	if _, err := s.getClusterSummary(clusterID); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}
	if req.SnapshotID <= 0 {
		return RestoreEtcdSnapshotResponse{}, errors.New("snapshot_id is required")
	}

	var snapshotPath string
	err := s.db.QueryRow(
		`SELECT snapshot_path FROM etcd_snapshots WHERE id = $1 AND cluster_id = $2`,
		req.SnapshotID,
		clusterID,
	).Scan(&snapshotPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RestoreEtcdSnapshotResponse{}, errors.New("snapshot not found")
		}
		return RestoreEtcdSnapshotResponse{}, fmt.Errorf("query snapshot: %w", err)
	}

	status := "restore-planned"
	warning := "restore plan recorded; execute etcd restore runbook with maintenance window"
	if req.Apply {
		status = "restore-requested"
	}

	if _, err := s.db.Exec(
		`INSERT INTO etcd_restore_runs (cluster_id, snapshot_id, status, details)
		 VALUES ($1, $2, $3, $4)`,
		clusterID,
		req.SnapshotID,
		status,
		snapshotPath,
	); err != nil {
		return RestoreEtcdSnapshotResponse{}, fmt.Errorf("insert restore run: %w", err)
	}

	if err := s.insertProvisionRun(clusterID, "etcd-restore", status, "", snapshotPath); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}
	if err := s.updateClusterStatus(clusterID, status); err != nil {
		return RestoreEtcdSnapshotResponse{}, err
	}

	return RestoreEtcdSnapshotResponse{
		ClusterID:  clusterID,
		SnapshotID: req.SnapshotID,
		Status:     status,
		Warning:    warning,
	}, nil
}

// sshUserFrom derives the SSH system username from a Proxmox credential.
// API usernames are like "root@pam" or "user@realm"; for SSH we just want "root" / "user".
// For API-token-only credentials the token ID format is "user@realm!token-name".
func sshUserFrom(cred proxmox.Credential) string {
	if cred.Username != "" {
		return strings.SplitN(cred.Username, "@", 2)[0]
	}
	if cred.APITokenID != "" {
		return strings.SplitN(cred.APITokenID, "@", 2)[0]
	}
	return ""
}

func validateProvisionRequest(req ProvisionRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.KubernetesVersion = strings.TrimSpace(req.KubernetesVersion)
	req.UbuntuImage = strings.TrimSpace(req.UbuntuImage)
	req.SSHPublicKey = strings.TrimSpace(req.SSHPublicKey)

	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.KubernetesVersion == "" {
		return errors.New("kubernetes_version is required")
	}
	if req.UbuntuImage == "" {
		return errors.New("ubuntu_image is required")
	}
	if req.SSHPublicKey == "" {
		return errors.New("ssh_public_key is required")
	}
	if req.ProxmoxCredentialID <= 0 && len(req.NodeCredentialIDs) == 0 {
		return errors.New("proxmox_credential_id is required when node_credential_ids is empty")
	}
	for nodeName, credentialID := range req.NodeCredentialIDs {
		if strings.TrimSpace(nodeName) == "" {
			return errors.New("node_credential_ids keys must be non-empty proxmox node names")
		}
		if credentialID <= 0 {
			return fmt.Errorf("node_credential_ids[%s] must be a valid credential id", nodeName)
		}
	}
	for nodeName, profileID := range req.NodeNetworkProfileIDs {
		if strings.TrimSpace(nodeName) == "" {
			return errors.New("node_network_profile_ids keys must be non-empty proxmox node names")
		}
		if profileID <= 0 {
			return fmt.Errorf("node_network_profile_ids[%s] must be a valid network profile id", nodeName)
		}
	}
	if strings.TrimSpace(req.NodeIPPoolStart) == "" {
		return errors.New("node_ip_pool_start is required (e.g. 192.168.8.100)")
	}
	if net.ParseIP(strings.TrimSpace(req.NodeIPPoolStart)) == nil {
		return errors.New("node_ip_pool_start is not a valid IP address")
	}
	for nodeName, poolStart := range req.NodeIPPoolStartByNode {
		if strings.TrimSpace(nodeName) == "" {
			return errors.New("node_ip_pool_start_by_node keys must be non-empty proxmox node names")
		}
		if net.ParseIP(strings.TrimSpace(poolStart)) == nil {
			return fmt.Errorf("node_ip_pool_start_by_node[%s] is not a valid IP address", nodeName)
		}
	}
	if req.ControlPlaneRAMMB > 0 && req.ControlPlaneRAMMB < 2048 {
		return errors.New("control_plane_ram_mb must be at least 2048")
	}
	if req.WorkerRAMMB > 0 && req.WorkerRAMMB < 2048 {
		return errors.New("worker_ram_mb must be at least 2048")
	}

	return nil
}

func resolveProvisionNodeCredentials(req ProvisionRequest, plan topology.PlanResponse) (map[string]int64, int64, error) {
	nodeCredentialIDs := make(map[string]int64, len(plan.SelectedProxmoxNode)+len(req.NodeCredentialIDs))
	for nodeName, credentialID := range req.NodeCredentialIDs {
		trimmed := strings.TrimSpace(nodeName)
		if trimmed == "" || credentialID <= 0 {
			continue
		}
		nodeCredentialIDs[trimmed] = credentialID
	}

	defaultCredentialID := req.ProxmoxCredentialID
	for _, placement := range plan.Placements {
		nodeName := strings.TrimSpace(placement.ProxmoxNodeName)
		credentialID := nodeCredentialIDs[nodeName]
		if credentialID <= 0 {
			credentialID = defaultCredentialID
		}
		if credentialID <= 0 {
			return nil, 0, fmt.Errorf("missing credential for proxmox node %q; provide node_credential_ids or proxmox_credential_id", nodeName)
		}
		nodeCredentialIDs[nodeName] = credentialID
		if defaultCredentialID <= 0 {
			defaultCredentialID = credentialID
		}
	}

	if defaultCredentialID <= 0 {
		return nil, 0, errors.New("no valid proxmox credential mapping resolved for planned nodes")
	}

	return nodeCredentialIDs, defaultCredentialID, nil
}

func resolveProvisionNodeNetworkProfiles(req ProvisionRequest, plan topology.PlanResponse, nodeTagMap map[string]int64) (map[string]int64, error) {
	resolved := make(map[string]int64, len(plan.SelectedProxmoxNode)+len(req.NodeNetworkProfileIDs))
	for nodeName, profileID := range req.NodeNetworkProfileIDs {
		trimmed := strings.TrimSpace(nodeName)
		if trimmed == "" || profileID <= 0 {
			continue
		}
		resolved[trimmed] = profileID
	}

	for _, placement := range plan.Placements {
		nodeName := strings.TrimSpace(placement.ProxmoxNodeName)
		profileID := resolved[nodeName]
		if profileID <= 0 {
			profileID = nodeTagMap[nodeName]
		}
		if profileID <= 0 {
			profileID = req.NetworkProfileID
		}
		if profileID <= 0 {
			return nil, fmt.Errorf("missing network profile for proxmox node %q; provide node_network_profile_ids, saved node tags, or network_profile_id", nodeName)
		}
		resolved[nodeName] = profileID
	}

	return resolved, nil
}

func distinctPlacementCredentialIDs(plan topology.PlanResponse, nodeCredentialIDs map[string]int64) []int64 {
	seen := make(map[int64]struct{})
	out := make([]int64, 0)
	for _, placement := range plan.Placements {
		credentialID := nodeCredentialIDs[strings.TrimSpace(placement.ProxmoxNodeName)]
		if credentialID <= 0 {
			continue
		}
		if _, exists := seen[credentialID]; exists {
			continue
		}
		seen[credentialID] = struct{}{}
		out = append(out, credentialID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func groupVMsByCredential(vms []terraform.VMNode, nodeCredentialIDs map[string]int64) (map[int64][]terraform.VMNode, error) {
	out := make(map[int64][]terraform.VMNode)
	for _, vm := range vms {
		nodeName := strings.TrimSpace(vm.ProxmoxNodeName)
		credentialID := nodeCredentialIDs[nodeName]
		if credentialID <= 0 {
			return nil, fmt.Errorf("missing proxmox credential mapping for node %q", nodeName)
		}
		out[credentialID] = append(out[credentialID], vm)
	}
	return out, nil
}

func sortedCredentialIDs(vmGroups map[int64][]terraform.VMNode) []int64 {
	out := make([]int64, 0, len(vmGroups))
	for credentialID := range vmGroups {
		if credentialID > 0 {
			out = append(out, credentialID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func workspaceKeyForCredential(clusterName string, credentialID int64) string {
	return fmt.Sprintf("%s-cred-%d", strings.TrimSpace(clusterName), credentialID)
}

func firstExistingTerraformWorkspace(candidates []string) (string, error) {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		workDir := strings.TrimSpace(candidate)
		if workDir == "" {
			continue
		}
		if _, exists := seen[workDir]; exists {
			continue
		}
		seen[workDir] = struct{}{}

		if _, err := os.Stat(filepath.Join(workDir, "terraform.tfvars.json")); err == nil {
			return workDir, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect terraform workspace: %w", err)
		}
	}
	return "", nil
}

func distinctCredentialIDsFromClusterNodes(nodes []ClusterNodeRecord) []int64 {
	seen := make(map[int64]struct{})
	out := make([]int64, 0)
	for _, node := range nodes {
		if node.ProxmoxCredentialID <= 0 {
			continue
		}
		if _, exists := seen[node.ProxmoxCredentialID]; exists {
			continue
		}
		seen[node.ProxmoxCredentialID] = struct{}{}
		out = append(out, node.ProxmoxCredentialID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ipIterator hands out sequential IPs starting from a base address,
// skipping any that are already responding on the network.
type ipIterator struct {
	current    net.IP
	cidr       *net.IPNet
	gateway    string
	assignedIP map[string]struct{}
}

func newIPIterator(start string, cidr string, gateway string, assigned map[string]struct{}) (*ipIterator, error) {
	start = strings.TrimSpace(start)
	ip := net.ParseIP(start).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid node_ip_pool_start %q", start)
	}
	_, cidrNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid cidr %q", cidr)
	}
	if !cidrNet.Contains(ip) {
		return nil, fmt.Errorf("ip %s is outside cidr %s", ip.String(), cidrNet.String())
	}

	clone := make(net.IP, 4)
	copy(clone, ip)
	if assigned == nil {
		assigned = make(map[string]struct{})
	}

	return &ipIterator{current: clone, cidr: cidrNet, gateway: strings.TrimSpace(gateway), assignedIP: assigned}, nil
}

func alignIPToCIDR(start string, cidr string) (string, error) {
	start = strings.TrimSpace(start)
	ip := net.ParseIP(start).To4()
	if ip == nil {
		return "", fmt.Errorf("invalid node_ip_pool_start %q", start)
	}
	_, cidrNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return "", fmt.Errorf("invalid cidr %q", cidr)
	}
	if cidrNet.Contains(ip) {
		return ip.String(), nil
	}

	mask := binary.BigEndian.Uint32(cidrNet.Mask)
	networkBase := binary.BigEndian.Uint32(cidrNet.IP.To4()) & mask
	hostBits := binary.BigEndian.Uint32(ip) & ^mask
	candidateRaw := networkBase | hostBits

	candidate := make(net.IP, 4)
	binary.BigEndian.PutUint32(candidate, candidateRaw)
	if !cidrNet.Contains(candidate) {
		return "", fmt.Errorf("ip %s is outside cidr %s", ip.String(), cidrNet.String())
	}

	for tries := 0; tries < 8; tries++ {
		if !isNetworkOrBroadcast(cidrNet, candidate) {
			return candidate.String(), nil
		}
		candidateRaw++
		binary.BigEndian.PutUint32(candidate, candidateRaw)
		if !cidrNet.Contains(candidate) {
			break
		}
	}

	return "", fmt.Errorf("ip %s cannot be aligned to usable host in cidr %s", ip.String(), cidrNet.String())
}

// nextAvailable advances until it finds an IP that does not respond to ping,
// then returns that IP. It tries up to 64 addresses before giving up.
func (it *ipIterator) nextAvailable() (string, error) {
	const maxSkip = 512
	for i := 0; i < maxSkip; i++ {
		ip := it.advance()
		parsed := net.ParseIP(ip).To4()
		if parsed == nil {
			continue
		}
		if !it.cidr.Contains(parsed) {
			return "", fmt.Errorf("no free IP found in cidr %s", it.cidr.String())
		}
		if ip == it.gateway || isNetworkOrBroadcast(it.cidr, parsed) {
			continue
		}
		if _, exists := it.assignedIP[ip]; exists {
			continue
		}
		if !isIPInUse(ip) {
			it.assignedIP[ip] = struct{}{}
			return ip, nil
		}
	}
	return "", errors.New("no free IP found in the next 512 addresses of the pool")
}

// advance returns the current IP and increments to the next one.
func (it *ipIterator) advance() string {
	addr := make(net.IP, 4)
	copy(addr, it.current)
	result := addr.String()
	for i := 3; i >= 0; i-- {
		it.current[i]++
		if it.current[i] != 0 {
			break
		}
	}
	return result
}

func isNetworkOrBroadcast(cidr *net.IPNet, ip net.IP) bool {
	networkIP := cidr.IP.To4()
	ipv4 := ip.To4()
	if networkIP == nil || ipv4 == nil {
		return false
	}
	if networkIP.Equal(ipv4) {
		return true
	}
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		broadcast[i] = networkIP[i] | ^cidr.Mask[i]
	}
	return broadcast.Equal(ipv4)
}

// isIPInUse probes the given IP address with a single ICMP ping (1 s timeout).
// Returns true if the host is alive (IP already taken).
func isIPInUse(ip string) bool {
	// Try ICMP ping first — works on Linux hosts/containers with NET_RAW or
	// when run as root (as is typical in Docker containers).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", ip)
	if err := cmd.Run(); err == nil {
		return true // got a reply — IP is in use
	}

	// Fallback: try a short TCP dial to port 22 (SSH).
	// "connection refused" means the host is up; timeout means it is down.
	conn, err := net.DialTimeout("tcp", ip+":22", 500*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}
	// If the error is not a timeout, the host likely sent a TCP RST → it is up.
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return false
	}
	// "connection refused" or similar → host is up
	if strings.Contains(err.Error(), "refused") {
		return true
	}
	return false
}

// isIgnorableDestroyError returns true for terraform destroy errors that are
// caused by environment/connectivity issues (for example stale Proxmox host
// names) where metadata cleanup should still be allowed.
func isIgnorableDestroyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "hostname lookup") ||
		strings.Contains(msg, "name or service not known") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection refused") ||
		strings.Contains(msg, "server misbehaving") ||
		strings.Contains(msg, "i/o timeout")
}

func validateBootstrapRequest(req BootstrapRequest) error {
	if strings.TrimSpace(req.SSHUser) == "" {
		return errors.New("ssh_user is required")
	}

	if strings.TrimSpace(req.PodCIDR) != "" {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(req.PodCIDR)); err != nil {
			return errors.New("pod_cidr is invalid")
		}
	}
	if strings.TrimSpace(req.ServiceCIDR) != "" {
		if _, _, err := net.ParseCIDR(strings.TrimSpace(req.ServiceCIDR)); err != nil {
			return errors.New("service_cidr is invalid")
		}
	}
	if req.MetalLB != nil {
		metallbReq := *req.MetalLB
		metallbReq.Apply = true
		if _, _, _, _, err := normalizeMetalLBRequest(metallbReq); err != nil {
			return fmt.Errorf("metallb: %w", err)
		}
	}

	return nil
}

func normalizeMetalLBRequest(req DeployMetalLBRequest) (string, string, []MetalLBAddressPool, []string, error) {
	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		namespace = "metallb-system"
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "v0.14.9"
	}

	pools := make([]MetalLBAddressPool, 0, len(req.AddressPools))
	for index, pool := range req.AddressPools {
		name := strings.TrimSpace(pool.Name)
		if name == "" {
			name = fmt.Sprintf("pool-%d", index+1)
		}

		addresses := make([]string, 0, len(pool.Addresses))
		for _, address := range pool.Addresses {
			trimmed := strings.TrimSpace(address)
			if trimmed == "" {
				continue
			}
			if err := validateMetalLBAddress(trimmed); err != nil {
				return "", "", nil, nil, fmt.Errorf("address_pools[%d]: %w", index, err)
			}
			addresses = append(addresses, trimmed)
		}
		if len(addresses) == 0 {
			return "", "", nil, nil, fmt.Errorf("address_pools[%d] must include at least one address range or CIDR", index)
		}

		pools = append(pools, MetalLBAddressPool{
			Name:          name,
			Addresses:     addresses,
			AutoAssign:    pool.AutoAssign,
			AvoidBuggyIPs: pool.AvoidBuggyIPs,
		})
	}
	if len(pools) == 0 {
		return "", "", nil, nil, errors.New("at least one address pool is required")
	}

	interfaces := make([]string, 0, len(req.Interfaces))
	for _, iface := range req.Interfaces {
		trimmed := strings.TrimSpace(iface)
		if trimmed == "" {
			continue
		}
		interfaces = append(interfaces, trimmed)
	}

	return namespace, version, pools, interfaces, nil
}

func validateMetalLBAddress(value string) error {
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid address range %q", value)
		}
		start := strings.TrimSpace(parts[0])
		end := strings.TrimSpace(parts[1])
		if net.ParseIP(start) == nil || net.ParseIP(end) == nil {
			return fmt.Errorf("invalid address range %q", value)
		}
		return nil
	}

	if _, _, err := net.ParseCIDR(value); err == nil {
		return nil
	}
	if net.ParseIP(value) != nil {
		return nil
	}

	return fmt.Errorf("invalid address or CIDR %q", value)
}

func (s *Service) insertCluster(req ProvisionRequest, plan topology.PlanResponse) (int64, error) {
	var clusterID int64
	err := s.db.QueryRow(
		`INSERT INTO clusters (name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, proxmox_credential_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, 'planned')
		 RETURNING id`,
		req.Name,
		plan.TopologyType,
		req.KubernetesVersion,
		req.NetworkProfileID,
		plan.ControlPlaneCount,
		plan.WorkerCount,
		req.ProxmoxCredentialID,
	).Scan(&clusterID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return 0, ErrClusterExists
		}
		return 0, fmt.Errorf("insert cluster: %w", err)
	}

	return clusterID, nil
}

func (s *Service) insertClusterNode(clusterID int64, placement topology.Placement, vmName string, ipAddress string, proxmoxCredentialID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO cluster_nodes (cluster_id, role, role_index, proxmox_node_name, vm_name, ip_address, proxmox_credential_id)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)`,
		clusterID,
		placement.Role,
		placement.Index,
		placement.ProxmoxNodeName,
		vmName,
		strings.TrimSpace(ipAddress),
		proxmoxCredentialID,
	)
	if err != nil {
		return fmt.Errorf("insert cluster node: %w", err)
	}
	return nil
}

func (s *Service) insertProvisionRun(clusterID int64, action, status, workDir, output string) error {
	_, err := s.db.Exec(
		`INSERT INTO provision_runs (cluster_id, action, status, work_dir, output, finished_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		clusterID,
		action,
		status,
		workDir,
		output,
	)
	if err != nil {
		return fmt.Errorf("insert provision run: %w", err)
	}
	return nil
}

type applicationManifestRevisionInput struct {
	ClusterID        int64
	Name             string
	Namespace        string
	Hostname         string
	IngressClass     string
	TLSSecretName    string
	TLSCertificateID int64
	RawManifest      string
	RenderedManifest string
	Status           string
	Apply            bool
}

func (s *Service) createApplicationManifestRevision(in applicationManifestRevisionInput) (int64, int64, int, error) {
	tlsCertificateID := sql.NullInt64{Int64: in.TLSCertificateID, Valid: in.TLSCertificateID > 0}
	var appID int64
	err := s.db.QueryRow(
		`INSERT INTO application_manifests (cluster_id, name, namespace, hostname, ingress_class, tls_secret_name, tls_certificate_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (cluster_id, namespace, name)
		 DO UPDATE SET
		   hostname = EXCLUDED.hostname,
		   ingress_class = EXCLUDED.ingress_class,
		   tls_secret_name = EXCLUDED.tls_secret_name,
		   tls_certificate_id = EXCLUDED.tls_certificate_id,
		   updated_at = NOW()
		 RETURNING id`,
		in.ClusterID,
		in.Name,
		in.Namespace,
		in.Hostname,
		in.IngressClass,
		in.TLSSecretName,
		tlsCertificateID,
	).Scan(&appID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("upsert application manifest: %w", err)
	}

	revisionNumber := 1
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(revision_number), 0) + 1
		 FROM application_manifest_revisions
		 WHERE application_manifest_id = $1`,
		appID,
	).Scan(&revisionNumber); err != nil {
		return 0, 0, 0, fmt.Errorf("calculate application manifest revision: %w", err)
	}

	var revisionID int64
	err = s.db.QueryRow(
		`INSERT INTO application_manifest_revisions
		   (application_manifest_id, cluster_id, revision_number, raw_manifest, rendered_manifest, status, apply)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		appID,
		in.ClusterID,
		revisionNumber,
		in.RawManifest,
		in.RenderedManifest,
		in.Status,
		in.Apply,
	).Scan(&revisionID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("insert application manifest revision: %w", err)
	}

	if _, err := s.db.Exec(
		`UPDATE application_manifests
		 SET latest_revision_id = $2, updated_at = NOW()
		 WHERE id = $1`,
		appID,
		revisionID,
	); err != nil {
		return 0, 0, 0, fmt.Errorf("update latest application manifest revision: %w", err)
	}

	return appID, revisionID, revisionNumber, nil
}

func (s *Service) updateApplicationManifestRevision(revisionID int64, status, output, errorMessage string) error {
	if revisionID <= 0 {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE application_manifest_revisions
		 SET status = $2,
		     kubectl_output = $3,
		     error_message = $4
		 WHERE id = $1`,
		revisionID,
		status,
		output,
		errorMessage,
	)
	if err != nil {
		return fmt.Errorf("update application manifest revision: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApplicationManifestRevision(row scanner) (ApplicationManifestRevisionRecord, error) {
	var rec ApplicationManifestRevisionRecord
	err := row.Scan(
		&rec.ID,
		&rec.ApplicationManifestID,
		&rec.ClusterID,
		&rec.RevisionNumber,
		&rec.RawManifest,
		&rec.RenderedManifest,
		&rec.KubectlOutput,
		&rec.Status,
		&rec.ErrorMessage,
		&rec.Apply,
		&rec.CreatedAt,
	)
	return rec, err
}

func (s *Service) ensureApplicationManifestBelongsToCluster(clusterID, appID int64) error {
	var exists bool
	if err := s.db.QueryRow(
		`SELECT EXISTS (
		   SELECT 1 FROM application_manifests
		   WHERE id = $1 AND cluster_id = $2
		 )`,
		appID,
		clusterID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check application manifest: %w", err)
	}
	if !exists {
		return ErrApplicationManifestNotFound
	}
	return nil
}

func (s *Service) getApplicationManifestRecord(clusterID, appID int64) (ApplicationManifestRecord, error) {
	var rec ApplicationManifestRecord
	err := s.db.QueryRow(
		`SELECT am.id, am.cluster_id, am.name, am.namespace, am.hostname, am.ingress_class,
		        am.tls_secret_name, COALESCE(am.tls_certificate_id, 0), COALESCE(am.latest_revision_id, 0),
		        COALESCE(r.revision_number, 0), COALESCE(r.status, ''), am.created_at, am.updated_at
		 FROM application_manifests am
		 LEFT JOIN application_manifest_revisions r ON r.id = am.latest_revision_id
		 WHERE am.cluster_id = $1 AND am.id = $2`,
		clusterID,
		appID,
	).Scan(
		&rec.ID,
		&rec.ClusterID,
		&rec.Name,
		&rec.Namespace,
		&rec.Hostname,
		&rec.IngressClass,
		&rec.TLSSecretName,
		&rec.TLSCertificateID,
		&rec.LatestRevisionID,
		&rec.LatestRevision,
		&rec.LatestStatus,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ApplicationManifestRecord{}, ErrApplicationManifestNotFound
		}
		return ApplicationManifestRecord{}, fmt.Errorf("get application manifest: %w", err)
	}
	return rec, nil
}

func (s *Service) startClusterOperation(clusterID int64, action, step string, progressPercent int) (int64, error) {
	var operationID int64
	err := s.db.QueryRow(
		`INSERT INTO cluster_operations (cluster_id, action, status, current_step, progress_percent, started_at, updated_at)
		 VALUES ($1, $2, 'running', $3, $4, NOW(), NOW())
		 RETURNING id`,
		clusterID,
		action,
		strings.TrimSpace(step),
		progressPercent,
	).Scan(&operationID)
	if err != nil {
		return 0, fmt.Errorf("start cluster operation: %w", err)
	}
	return operationID, nil
}

func (s *Service) updateClusterOperation(operationID int64, status string, step string, progressPercent int, errorSummary string) error {
	if operationID <= 0 {
		return nil
	}
	status = strings.TrimSpace(status)
	step = strings.TrimSpace(step)
	errorSummary = strings.TrimSpace(errorSummary)
	if status == "" {
		status = "running"
	}

	_, err := s.db.Exec(
		`UPDATE cluster_operations
		 SET status = $2,
		     current_step = $3,
		     progress_percent = $4,
		     error_summary = NULLIF($5, ''),
		     finished_at = CASE WHEN $2 IN ('succeeded', 'failed') THEN NOW() ELSE finished_at END,
		     updated_at = NOW()
		 WHERE id = $1`,
		operationID,
		status,
		step,
		progressPercent,
		errorSummary,
	)
	if err != nil {
		return fmt.Errorf("update cluster operation: %w", err)
	}

	return nil
}

func (s *Service) appendOperationEvent(operationID int64, step, level, message string) error {
	if operationID <= 0 {
		return nil
	}

	step = strings.TrimSpace(step)
	level = strings.TrimSpace(strings.ToLower(level))
	message = strings.TrimSpace(message)
	if step == "" {
		step = "general"
	}
	if level == "" {
		level = "info"
	}
	if message == "" {
		return nil
	}

	_, err := s.db.Exec(
		`INSERT INTO cluster_operation_events (operation_id, step, level, message, created_at)
		 VALUES ($1, $2, $3, $4, NOW())`,
		operationID,
		step,
		level,
		message,
	)
	if err != nil {
		return fmt.Errorf("insert operation event: %w", err)
	}

	return nil
}

func (s *Service) updateClusterStatus(clusterID int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE clusters SET status = $2, updated_at = NOW() WHERE id = $1`,
		clusterID,
		status,
	)
	if err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}
	return nil
}

func (s *Service) getClusterBase(clusterID int64) (string, string, error) {
	var name string
	var kubernetesVersion string
	err := s.db.QueryRow(
		`SELECT name, kubernetes_version FROM clusters WHERE id = $1`,
		clusterID,
	).Scan(&name, &kubernetesVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrClusterNotFound
		}
		return "", "", fmt.Errorf("query cluster: %w", err)
	}
	return name, kubernetesVersion, nil
}

func (s *Service) getClusterDeleteInfo(clusterID int64) (clusterDeleteInfo, error) {
	var info clusterDeleteInfo
	err := s.db.QueryRow(
		`SELECT c.name, c.proxmox_credential_id,
		        COALESCE((
		          SELECT pr.work_dir
		          FROM provision_runs pr
		          WHERE pr.cluster_id = c.id AND pr.work_dir <> '' AND pr.action = 'provision'
		          ORDER BY pr.created_at DESC
		          LIMIT 1
		        ), '')
		 FROM clusters c
		 WHERE c.id = $1`,
		clusterID,
	).Scan(&info.Name, &info.ProxmoxCredentialID, &info.TerraformWorkDir)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return clusterDeleteInfo{}, ErrClusterNotFound
		}
		return clusterDeleteInfo{}, fmt.Errorf("query cluster delete info: %w", err)
	}
	return info, nil
}

func (s *Service) latestProvisionWorkDir(clusterID int64) (string, error) {
	var workDir string
	err := s.db.QueryRow(
		`SELECT COALESCE((
		  SELECT pr.work_dir
		  FROM provision_runs pr
		  WHERE pr.cluster_id = $1 AND pr.work_dir <> '' AND pr.action IN ('provision', 'redeploy')
		  ORDER BY pr.created_at DESC
		  LIMIT 1
		), '')`,
		clusterID,
	).Scan(&workDir)
	if err != nil {
		return "", fmt.Errorf("query latest provision workspace: %w", err)
	}
	return strings.TrimSpace(workDir), nil
}

func (s *Service) getClusterNodes(clusterID int64) ([]ClusterNodeRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, role, role_index, vm_name, COALESCE(ip_address, ''), proxmox_node_name, COALESCE(proxmox_credential_id, 0)
		 FROM cluster_nodes
		 WHERE cluster_id = $1
		 ORDER BY role, role_index`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("query cluster nodes: %w", err)
	}
	defer rows.Close()

	out := make([]ClusterNodeRecord, 0)
	for rows.Next() {
		var n ClusterNodeRecord
		if scanErr := rows.Scan(&n.ID, &n.Role, &n.RoleIndex, &n.VMName, &n.IPAddress, &n.ProxmoxNodeName, &n.ProxmoxCredentialID); scanErr != nil {
			continue
		}
		out = append(out, n)
	}

	return out, nil
}

func (s *Service) getClusterSummary(clusterID int64) (ClusterSummary, error) {
	var c ClusterSummary
	err := s.db.QueryRow(
		`SELECT id, name, topology_type, kubernetes_version, network_profile_id, control_plane_count, worker_count, status, created_at, updated_at
		 FROM clusters WHERE id = $1`,
		clusterID,
	).Scan(
		&c.ID,
		&c.Name,
		&c.TopologyType,
		&c.KubernetesVersion,
		&c.NetworkProfileID,
		&c.ControlPlaneCount,
		&c.WorkerCount,
		&c.Status,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClusterSummary{}, ErrClusterNotFound
		}
		return ClusterSummary{}, fmt.Errorf("get cluster summary: %w", err)
	}
	return c, nil
}

func (s *Service) getBootstrapConfig(clusterID int64) (BootstrapConfigRecord, error) {
	var out BootstrapConfigRecord
	err := s.db.QueryRow(
		`SELECT cluster_id, ssh_user, last_ssh_key_id, updated_at
		 FROM cluster_bootstrap_configs
		 WHERE cluster_id = $1`,
		clusterID,
	).Scan(&out.ClusterID, &out.SSHUser, &out.LastSSHKeyID, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BootstrapConfigRecord{}, errors.New("bootstrap ssh config not found")
		}
		return BootstrapConfigRecord{}, fmt.Errorf("query bootstrap ssh config: %w", err)
	}

	return out, nil
}

func (s *Service) getBootstrapSSHKeys(clusterID int64) ([]BootstrapSSHKeyRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, cluster_id, key_name, updated_at
		 FROM cluster_bootstrap_ssh_keys
		 WHERE cluster_id = $1
		 ORDER BY key_name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list bootstrap ssh keys: %w", err)
	}
	defer rows.Close()

	out := make([]BootstrapSSHKeyRecord, 0)
	for rows.Next() {
		var rec BootstrapSSHKeyRecord
		if scanErr := rows.Scan(&rec.ID, &rec.ClusterID, &rec.KeyName, &rec.UpdatedAt); scanErr != nil {
			continue
		}
		out = append(out, rec)
	}

	return out, nil
}

func (s *Service) upsertBootstrapConfig(clusterID int64, sshUser string, lastSSHKeyID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO cluster_bootstrap_configs (cluster_id, ssh_user, ssh_private_key_path, last_ssh_key_id)
		 VALUES ($1, $2, '', $3)
		 ON CONFLICT (cluster_id)
		 DO UPDATE SET
		   ssh_user = EXCLUDED.ssh_user,
		   ssh_private_key_path = EXCLUDED.ssh_private_key_path,
		   last_ssh_key_id = EXCLUDED.last_ssh_key_id,
		   updated_at = NOW()`,
		clusterID,
		strings.TrimSpace(sshUser),
		lastSSHKeyID,
	)
	if err != nil {
		return fmt.Errorf("upsert bootstrap ssh config: %w", err)
	}

	return nil
}

func (s *Service) resolveBootstrapSSHKey(clusterID int64, req BootstrapRequest) (string, int64, error) {
	if key := strings.TrimSpace(req.SSHPrivateKey); key != "" {
		name := strings.TrimSpace(req.SSHKeyName)
		if name == "" {
			name = "default"
		}
		keyID, err := s.upsertBootstrapSSHKey(clusterID, name, key)
		if err != nil {
			return "", 0, err
		}
		return key, keyID, nil
	}

	if req.SSHKeyPairID > 0 {
		if s.sshKeys == nil {
			return "", 0, errors.New("ssh key pair service is not configured")
		}
		pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
		if err != nil {
			if errors.Is(err, sshkeys.ErrKeyPairNotFound) {
				return "", 0, errors.New("selected ssh key pair not found")
			}
			return "", 0, fmt.Errorf("load ssh key pair: %w", err)
		}
		privateKey := strings.TrimSpace(pair.PrivateKey)
		if privateKey == "" {
			return "", 0, errors.New("selected ssh key pair has empty private key")
		}
		keyID, saveErr := s.upsertBootstrapSSHKey(clusterID, strings.TrimSpace(pair.Name), privateKey)
		if saveErr != nil {
			return "", 0, saveErr
		}
		return privateKey, keyID, nil
	}

	if req.SSHKeyID > 0 {
		key, err := s.getBootstrapSSHKeyByID(clusterID, req.SSHKeyID)
		if err != nil {
			return "", 0, err
		}
		return key, req.SSHKeyID, nil
	}

	cfg, err := s.getBootstrapConfig(clusterID)
	if err == nil && cfg.LastSSHKeyID.Valid {
		key, keyErr := s.getBootstrapSSHKeyByID(clusterID, cfg.LastSSHKeyID.Int64)
		if keyErr != nil {
			return "", 0, keyErr
		}
		return key, cfg.LastSSHKeyID.Int64, nil
	}

	if path := strings.TrimSpace(req.SSHPrivateKeyPath); path != "" {
		bytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", 0, fmt.Errorf("read ssh private key path: %w", readErr)
		}
		key := strings.TrimSpace(string(bytes))
		if key == "" {
			return "", 0, errors.New("ssh private key file is empty")
		}
		keyID, err := s.upsertBootstrapSSHKey(clusterID, "legacy-path-import", key)
		if err != nil {
			return "", 0, err
		}
		return key, keyID, nil
	}

	return "", 0, errors.New("provide ssh_private_key, select ssh_key_pair_id, or select an existing ssh_key_id")
}

func (s *Service) resolveProvisionSSHKey(req *ProvisionRequest) error {
	req.SSHPublicKey = strings.TrimSpace(req.SSHPublicKey)
	if req.SSHPublicKey != "" {
		return nil
	}
	if req.SSHKeyPairID <= 0 {
		return nil
	}
	if s.sshKeys == nil {
		return errors.New("ssh key pair service is not configured")
	}

	pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
	if err != nil {
		if errors.Is(err, sshkeys.ErrKeyPairNotFound) {
			return errors.New("selected ssh key pair not found")
		}
		return fmt.Errorf("load ssh key pair: %w", err)
	}
	req.SSHPublicKey = strings.TrimSpace(pair.PublicKey)
	if req.SSHPublicKey == "" {
		return errors.New("selected ssh key pair has empty public key")
	}
	return nil
}

func (s *Service) hydrateBootstrapSSHUser(req *BootstrapRequest) error {
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.SSHUser != "" || req.SSHKeyPairID <= 0 {
		return nil
	}
	if s.sshKeys == nil {
		return errors.New("ssh key pair service is not configured")
	}

	pair, err := s.sshKeys.GetByID(req.SSHKeyPairID)
	if err != nil {
		if errors.Is(err, sshkeys.ErrKeyPairNotFound) {
			return errors.New("selected ssh key pair not found")
		}
		return fmt.Errorf("load ssh key pair: %w", err)
	}
	req.SSHUser = strings.TrimSpace(pair.SSHUser)
	if req.SSHUser == "" {
		req.SSHUser = "ubuntu"
	}
	return nil
}

func (s *Service) upsertBootstrapSSHKey(clusterID int64, keyName, privateKey string) (int64, error) {
	keyName = strings.TrimSpace(keyName)
	privateKey = strings.TrimSpace(privateKey)
	if keyName == "" {
		keyName = "default"
	}
	if privateKey == "" {
		return 0, errors.New("bootstrap ssh private key is required")
	}
	if s.secrets == nil {
		return 0, errors.New("secret store is not configured")
	}
	privateKeyRef, err := s.secrets.Put(context.Background(), fmt.Sprintf("clusters/%d/bootstrap-ssh", clusterID), keyName, []byte(privateKey))
	if err != nil {
		return 0, fmt.Errorf("store bootstrap ssh private key: %w", err)
	}

	var keyID int64
	err = s.db.QueryRow(
		`INSERT INTO cluster_bootstrap_ssh_keys (cluster_id, key_name, private_key, private_key_secret_ref)
		 VALUES ($1, $2, NULL, $3)
		 ON CONFLICT (cluster_id, key_name)
		 DO UPDATE SET
		   private_key = NULL,
		   private_key_secret_ref = EXCLUDED.private_key_secret_ref,
		   updated_at = NOW()
		 RETURNING id`,
		clusterID,
		keyName,
		privateKeyRef,
	).Scan(&keyID)
	if err != nil {
		return 0, fmt.Errorf("upsert bootstrap ssh key: %w", err)
	}

	return keyID, nil
}

func (s *Service) getBootstrapSSHKeyByID(clusterID, keyID int64) (string, error) {
	var privateKey, privateKeyRef string
	err := s.db.QueryRow(
		`SELECT COALESCE(private_key, ''), COALESCE(private_key_secret_ref, '')
		 FROM cluster_bootstrap_ssh_keys
		 WHERE id = $1 AND cluster_id = $2`,
		keyID,
		clusterID,
	).Scan(&privateKey, &privateKeyRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("selected bootstrap ssh key not found")
		}
		return "", fmt.Errorf("query bootstrap ssh key: %w", err)
	}

	if strings.TrimSpace(privateKeyRef) != "" {
		if s.secrets == nil {
			return "", errors.New("secret store is not configured")
		}
		value, err := s.secrets.Get(context.Background(), privateKeyRef)
		if err != nil {
			return "", fmt.Errorf("load bootstrap ssh private key secret: %w", err)
		}
		privateKey = string(value)
	}
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return "", errors.New("selected bootstrap ssh key is empty")
	}

	return privateKey, nil
}

func (s *Service) ClusterNameExists(name string) (bool, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, errors.New("name is required")
	}

	var exists bool
	err := s.db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM clusters WHERE LOWER(name) = LOWER($1))`,
		trimmed,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check cluster name: %w", err)
	}

	return exists, nil
}

func (s *Service) listDistinctProxmoxNodes(clusterID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT proxmox_node_name
		 FROM cluster_nodes
		 WHERE cluster_id = $1
		 ORDER BY proxmox_node_name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("list distinct proxmox nodes: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var n string
		if scanErr := rows.Scan(&n); scanErr != nil {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) getClusterCredentialID(clusterID int64) (int64, error) {
	var credentialID sql.NullInt64
	err := s.db.QueryRow(`SELECT proxmox_credential_id FROM clusters WHERE id = $1`, clusterID).Scan(&credentialID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrClusterNotFound
		}
		return 0, fmt.Errorf("query cluster credential id: %w", err)
	}
	if !credentialID.Valid || credentialID.Int64 <= 0 {
		return 0, errors.New("cluster has no default proxmox credential configured")
	}
	return credentialID.Int64, nil
}

func (s *Service) resolveCredentialForNode(clusterID int64, proxmoxNodeName string, fallbackCredentialID int64) (int64, error) {
	var credentialID sql.NullInt64
	err := s.db.QueryRow(
		`SELECT proxmox_credential_id
		 FROM cluster_nodes
		 WHERE cluster_id = $1 AND proxmox_node_name = $2 AND proxmox_credential_id IS NOT NULL
		 ORDER BY id ASC
		 LIMIT 1`,
		clusterID,
		strings.TrimSpace(proxmoxNodeName),
	).Scan(&credentialID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("resolve node credential id: %w", err)
	}
	if credentialID.Valid && credentialID.Int64 > 0 {
		return credentialID.Int64, nil
	}
	if fallbackCredentialID > 0 {
		return fallbackCredentialID, nil
	}
	return 0, fmt.Errorf("no proxmox credential mapped for node %q", proxmoxNodeName)
}

func (s *Service) updateClusterNodeIP(nodeID int64, ip string) error {
	_, err := s.db.Exec(`UPDATE cluster_nodes SET ip_address = $2 WHERE id = $1`, nodeID, ip)
	if err != nil {
		return fmt.Errorf("update cluster node ip: %w", err)
	}
	return nil
}

func pickCPU(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneCPU > 0 {
			return req.ControlPlaneCPU
		}
		return 4
	}
	if req.WorkerCPU > 0 {
		return req.WorkerCPU
	}
	return 4
}

func pickMemory(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneRAMMB > 0 {
			return req.ControlPlaneRAMMB
		}
		return 8192
	}
	if req.WorkerRAMMB > 0 {
		return req.WorkerRAMMB
	}
	return 8192
}

func pickDisk(req ProvisionRequest, role string) int {
	if role == "control-plane" {
		if req.ControlPlaneDisk > 0 {
			return req.ControlPlaneDisk
		}
		return 80
	}
	if req.WorkerDisk > 0 {
		return req.WorkerDisk
	}
	return 120
}

func buildVMName(clusterName, role string, index int) string {
	roleToken := "wk"
	if role == "control-plane" {
		roleToken = "cp"
	}
	name := strings.ToLower(strings.TrimSpace(clusterName))
	name = strings.ReplaceAll(name, " ", "-")
	return fmt.Sprintf("%s-%s-%d", name, roleToken, index)
}

func sanitizePathToken(name string) string {
	token := strings.ToLower(strings.TrimSpace(name))
	token = strings.ReplaceAll(token, " ", "-")
	token = strings.ReplaceAll(token, "/", "-")
	token = strings.ReplaceAll(token, "\\", "-")
	token = strings.ReplaceAll(token, ":", "-")
	if token == "" {
		return "cluster"
	}
	return token
}

func sanitizeKubernetesName(name string) string {
	token := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	previousDash := false
	for _, r := range token {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			previousDash = false
			continue
		}
		if !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	token = strings.Trim(builder.String(), "-")
	if token == "" {
		return "application"
	}
	if len(token) > 63 {
		token = strings.Trim(token[:63], "-")
	}
	if token == "" {
		return "application"
	}
	return token
}

func isValidAppHostname(hostname string) bool {
	if strings.Contains(hostname, "://") || strings.ContainsAny(hostname, "/ \\") {
		return false
	}
	if len(hostname) > 253 {
		return false
	}
	trimmed := strings.TrimSuffix(hostname, ".")
	if trimmed == "" {
		return false
	}
	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
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

func (s *Service) operationsManifestDir(clusterName, component string) string {
	return filepath.Join(s.operationsDir, sanitizePathToken(clusterName), component)
}

func buildObservabilityManifest(adminUser, adminPassword string, retentionDays int, storageClass string) string {
	return fmt.Sprintf(`grafana:
	adminUser: %s
	adminPassword: %s
	persistence:
		enabled: true
		storageClassName: %s
		size: 10Gi

prometheus:
	prometheusSpec:
		retention: %dd
		storageSpec:
			volumeClaimTemplate:
				spec:
					storageClassName: %s
					accessModes: ["ReadWriteOnce"]
					resources:
						requests:
							storage: 40Gi
`, adminUser, adminPassword, storageClass, retentionDays, storageClass)
}

func buildMetalLBConfigManifest(namespace string, addressPools []MetalLBAddressPool, interfaces []string) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("apiVersion: metallb.io/v1beta1\n")
	builder.WriteString("kind: IPAddressPool\n")
	for index, pool := range addressPools {
		if index > 0 {
			builder.WriteString("---\n")
			builder.WriteString("apiVersion: metallb.io/v1beta1\n")
			builder.WriteString("kind: IPAddressPool\n")
		}
		builder.WriteString(fmt.Sprintf("metadata:\n  name: %s\n  namespace: %s\n", pool.Name, namespace))
		builder.WriteString("spec:\n")
		builder.WriteString(fmt.Sprintf("  autoAssign: %t\n", pool.AutoAssign))
		builder.WriteString(fmt.Sprintf("  avoidBuggyIPs: %t\n", pool.AvoidBuggyIPs))
		builder.WriteString("  addresses:\n")
		for _, address := range pool.Addresses {
			builder.WriteString(fmt.Sprintf("    - %s\n", address))
		}
		builder.WriteString("---\n")
		builder.WriteString("apiVersion: metallb.io/v1beta1\n")
		builder.WriteString("kind: L2Advertisement\n")
		builder.WriteString(fmt.Sprintf("metadata:\n  name: %s-l2\n  namespace: %s\n", pool.Name, namespace))
		builder.WriteString("spec:\n")
		builder.WriteString("  ipAddressPools:\n")
		builder.WriteString(fmt.Sprintf("    - %s\n", pool.Name))
		if len(interfaces) > 0 {
			builder.WriteString("  interfaces:\n")
			for _, iface := range interfaces {
				builder.WriteString(fmt.Sprintf("    - %s\n", iface))
			}
		}
	}
	return builder.String()
}

func buildMetalLBREADME(clusterName, namespace, version string) string {
	return fmt.Sprintf(`# MetalLB Deployment

Cluster: %s
Namespace: %s
Version: %s

Files in this directory:

- metallb-resources.yaml: IPAddressPool and L2Advertisement resources for this cluster.
- apply-metallb.ps1: PowerShell helper for Windows operators.
- apply-metallb.sh: Shell helper for Linux operators.

Manual install flow:

1. Install MetalLB controllers from the official upstream manifest.
2. Wait for the controller deployment to become available.
3. Apply metallb-resources.yaml.

Official manifest URL:

https://raw.githubusercontent.com/metallb/metallb/%s/config/manifests/metallb-native.yaml
`, clusterName, namespace, version, version)
}

func buildMetalLBPowerShell(namespace, version string) string {
	return fmt.Sprintf(`$ErrorActionPreference = "Stop"

$manifest = "https://raw.githubusercontent.com/metallb/metallb/%s/config/manifests/metallb-native.yaml"

kubectl apply -f $manifest --validate=false
kubectl rollout status --namespace %s deployment/controller --timeout=600s
kubectl apply -f ./metallb-resources.yaml --validate=false
`, version, namespace)
}

func buildMetalLBShell(namespace, version string) string {
	return fmt.Sprintf(`#!/usr/bin/env sh
set -eu

manifest="https://raw.githubusercontent.com/metallb/metallb/%s/config/manifests/metallb-native.yaml"

kubectl apply -f "$manifest" --validate=false
kubectl rollout status --namespace %s deployment/controller --timeout=600s
kubectl apply -f ./metallb-resources.yaml --validate=false
`, version, namespace)
}

func buildMetalLBCommands(manifestDir string) MetalLBCommands {
	quotedDir := fmt.Sprintf("%q", manifestDir)
	return MetalLBCommands{
		PowerShell: fmt.Sprintf("Set-Location %s\n.\\apply-metallb.ps1", quotedDir),
		Shell:      fmt.Sprintf("cd %s\nsh ./apply-metallb.sh", quotedDir),
	}
}

func buildTraefikIngressControllerManifest(namespace, image string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: traefik
  namespace: %s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: traefik-ingress-controller
rules:
  - apiGroups: [""]
    resources: ["services", "endpoints", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["extensions", "networking.k8s.io"]
    resources: ["ingresses", "ingressclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["extensions", "networking.k8s.io"]
    resources: ["ingresses/status"]
    verbs: ["update"]
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: traefik-ingress-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: traefik-ingress-controller
subjects:
  - kind: ServiceAccount
    name: traefik
    namespace: %s
---
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: traefik
  annotations:
    ingressclass.kubernetes.io/is-default-class: "true"
spec:
  controller: traefik.io/ingress-controller
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: traefik
  namespace: %s
  labels:
    app.kubernetes.io/name: traefik
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: traefik
  template:
    metadata:
      labels:
        app.kubernetes.io/name: traefik
    spec:
      serviceAccountName: traefik
      containers:
        - name: traefik
          image: %s
          args:
            - --entrypoints.web.address=:80
            - --entrypoints.websecure.address=:443
            - --providers.kubernetesingress=true
            - --providers.kubernetesingress.ingressclass=traefik
            - --providers.kubernetesingress.ingressendpoint.publishedservice=%s/traefik
            - --api.dashboard=true
            - --ping=true
          ports:
            - name: web
              containerPort: 80
            - name: websecure
              containerPort: 443
            - name: traefik
              containerPort: 8080
          readinessProbe:
            httpGet:
              path: /ping
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /ping
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
---
apiVersion: v1
kind: Service
metadata:
  name: traefik
  namespace: %s
  labels:
    app.kubernetes.io/name: traefik
spec:
  type: LoadBalancer
  selector:
    app.kubernetes.io/name: traefik
  ports:
    - name: web
      port: 80
      targetPort: web
    - name: websecure
      port: 443
      targetPort: websecure
`, namespace, namespace, namespace, namespace, image, namespace, namespace)
}

func buildLetsEncryptClusterIssuerManifest(issuerName, email, acmeServer, ingressClass string) string {
	return fmt.Sprintf(`apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: %s
spec:
  acme:
    email: %s
    server: %s
    privateKeySecretRef:
      name: %s-account-key
    solvers:
      - http01:
          ingress:
          class: %s
`, issuerName, email, acmeServer, issuerName, ingressClass)
}

func buildAppIngressManifest(appName, hostname, ingressClass, tlsSecretName string) string {
	name := sanitizeKubernetesName(appName)
	if tlsSecretName == "" {
		tlsSecretName = sanitizeKubernetesName(appName + "-tls")
	}
	return fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web,websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
spec:
  ingressClassName: %s
  tls:
    - hosts:
        - %s
      secretName: %s
  rules:
    - host: %s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: %s
                port:
                  number: 80
`, name, ingressClass, hostname, tlsSecretName, hostname, name)
}
