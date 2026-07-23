package backups

import "time"

type TargetType string

const (
	TargetProxmoxStorage TargetType = "proxmox_storage"
	TargetPBS            TargetType = "pbs"
	TargetS3             TargetType = "s3"
	TargetHetzner        TargetType = "hetzner_storagebox"
)

type Target struct {
	ID                     int64          `json:"id"`
	Name                   string         `json:"name"`
	Type                   TargetType     `json:"type"`
	Config                 map[string]any `json:"config"`
	Enabled                bool           `json:"enabled"`
	HasAccessKey           bool           `json:"has_access_key"`
	HasSecretKey           bool           `json:"has_secret_key"`
	HasPassword            bool           `json:"has_password"`
	HasSSHPrivateKey       bool           `json:"has_ssh_private_key"`
	HasResticPassword      bool           `json:"has_restic_password"`
	AccessKeySecretRef     string         `json:"-"`
	SecretKeySecretRef     string         `json:"-"`
	PasswordSecretRef      string         `json:"-"`
	SSHPrivateKeySecretRef string         `json:"-"`
	ResticPasswordRef      string         `json:"-"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type TargetInput struct {
	Name           string         `json:"name"`
	Type           TargetType     `json:"type"`
	Config         map[string]any `json:"config"`
	AccessKey      string         `json:"access_key"`
	SecretKey      string         `json:"secret_key"`
	Password       string         `json:"password"`
	SSHPrivateKey  string         `json:"ssh_private_key"`
	ResticPassword string         `json:"restic_password"`
	Enabled        bool           `json:"enabled"`
	EnabledSet     bool           `json:"-"`
}

type Summary struct {
	Targets       []Target `json:"targets"`
	Jobs          []Job    `json:"jobs"`
	TargetCount   int      `json:"target_count"`
	EnabledCount  int      `json:"enabled_count"`
	ExternalCount int      `json:"external_count"`
}

type SourceType string

const (
	SourceVPS       SourceType = "vps"
	SourceContainer SourceType = "container"
	SourceCluster   SourceType = "cluster"
)

type Job struct {
	ID                int64      `json:"id"`
	TargetID          int64      `json:"target_id"`
	TargetName        string     `json:"target_name,omitempty"`
	SourceType        SourceType `json:"source_type"`
	SourceID          int64      `json:"source_id"`
	SourceName        string     `json:"source_name"`
	Mode              string     `json:"mode"`
	Status            string     `json:"status"`
	ProxmoxNodeName   string     `json:"proxmox_node_name"`
	ProxmoxTaskID     string     `json:"proxmox_task_id"`
	StagingStorageID  string     `json:"staging_storage_id"`
	ArtifactVolID     string     `json:"artifact_volid"`
	ArtifactPath      string     `json:"artifact_path"`
	UploadMethod      string     `json:"upload_method"`
	UploadStatus      string     `json:"upload_status"`
	RemotePath        string     `json:"remote_path"`
	UploadOutput      string     `json:"upload_output"`
	UploadError       string     `json:"upload_error"`
	Output            string     `json:"output"`
	Error             string     `json:"error"`
	PolicyID          int64      `json:"policy_id,omitempty"`
	CustomerAccountID int64      `json:"customer_account_id,omitempty"`
	TriggerType       string     `json:"trigger_type"`
	ScheduledFor      *time.Time `json:"scheduled_for,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type JobInput struct {
	TargetID          int64      `json:"target_id"`
	SourceType        SourceType `json:"source_type"`
	SourceID          int64      `json:"source_id"`
	Mode              string     `json:"mode"`
	StagingStorageID  string     `json:"staging_storage_id"`
	PolicyID          int64      `json:"-"`
	CustomerAccountID int64      `json:"-"`
	TriggerType       string     `json:"-"`
	ScheduledFor      *time.Time `json:"-"`
	IdempotencyKey    string     `json:"-"`
	RetentionCount    int        `json:"-"`
}

type Plan struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Frequency          string `json:"frequency"`
	BaseMonthlyCents   int    `json:"base_monthly_cents"`
	DiskGBMonthlyCents int    `json:"disk_gb_monthly_cents"`
	RetentionCount     int    `json:"retention_count"`
	Enabled            bool   `json:"enabled"`
}

type VPSBackupPolicy struct {
	ID                int64      `json:"id"`
	VPSID             int64      `json:"vps_id"`
	CustomerAccountID int64      `json:"customer_account_id,omitempty"`
	TargetID          int64      `json:"-"`
	TargetName        string     `json:"target_name"`
	PlanCode          string     `json:"plan_code"`
	Frequency         string     `json:"frequency"`
	Weekday           *int       `json:"weekday,omitempty"`
	MonthDay          *int       `json:"month_day,omitempty"`
	LocalTime         string     `json:"local_time"`
	Timezone          string     `json:"timezone"`
	Mode              string     `json:"mode"`
	RetentionCount    int        `json:"retention_count"`
	Enabled           bool       `json:"enabled"`
	MonthlyPriceCents int        `json:"monthly_price_cents"`
	NextRunAt         *time.Time `json:"next_run_at,omitempty"`
	LastRunAt         *time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	Version           int        `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type UpsertVPSBackupPolicyInput struct {
	PlanCode  string `json:"plan_code"`
	Weekday   *int   `json:"weekday,omitempty"`
	MonthDay  *int   `json:"month_day,omitempty"`
	LocalTime string `json:"local_time"`
	Timezone  string `json:"timezone"`
}

type VPSBackupOverview struct {
	Policy *VPSBackupPolicy `json:"policy,omitempty"`
	Plans  []Plan           `json:"plans"`
	Jobs   []Job            `json:"jobs"`
}
