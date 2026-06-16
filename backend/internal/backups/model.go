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
	ID              int64      `json:"id"`
	TargetID        int64      `json:"target_id"`
	TargetName      string     `json:"target_name,omitempty"`
	SourceType      SourceType `json:"source_type"`
	SourceID        int64      `json:"source_id"`
	SourceName      string     `json:"source_name"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	ProxmoxNodeName string     `json:"proxmox_node_name"`
	ProxmoxTaskID   string     `json:"proxmox_task_id"`
	Output          string     `json:"output"`
	Error           string     `json:"error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type JobInput struct {
	TargetID   int64      `json:"target_id"`
	SourceType SourceType `json:"source_type"`
	SourceID   int64      `json:"source_id"`
	Mode       string     `json:"mode"`
}
