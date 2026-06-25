package proxacloudtunnels

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrTunnelNotFound           = errors.New("proxacloud tunnel not found")
	ErrTunnelDeleteConfirmation = errors.New("proxacloud tunnel name confirmation does not match")
)

type ManifestApplier interface {
	ApplyRawManifest(clusterID int64, namespace, manifest string) (string, error)
}

type ProxacloudTunnel struct {
	ID                 int64                   `json:"id"`
	ClusterID          int64                   `json:"cluster_id"`
	ClusterName        string                  `json:"cluster_name,omitempty"`
	IsGlobal           bool                    `json:"is_global"`
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	DeploymentName     string                  `json:"deployment_name"`
	ProxacloudTunnelID string                  `json:"proxacloud_tunnel_id"`
	HasTunnelToken     bool                    `json:"has_tunnel_token"`
	GatewayURL         string                  `json:"gateway_url"`
	TargetURL          string                  `json:"target_url"`
	Replicas           int                     `json:"replicas"`
	Image              string                  `json:"image"`
	ImagePullSecret    string                  `json:"image_pull_secret"`
	Status             string                  `json:"status"`
	LastManifest       string                  `json:"last_manifest,omitempty"`
	LastApplyOutput    string                  `json:"last_apply_output,omitempty"`
	LastAppliedAt      *time.Time              `json:"last_applied_at,omitempty"`
	Routes             []ProxacloudTunnelRoute `json:"routes"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

type ProxacloudTunnelRoute struct {
	ID               int64     `json:"id"`
	TunnelID         int64     `json:"tunnel_id"`
	Hostname         string    `json:"hostname"`
	Path             string    `json:"path"`
	ServiceNamespace string    `json:"service_namespace"`
	ServiceName      string    `json:"service_name"`
	ServicePort      int       `json:"service_port"`
	ServiceScheme    string    `json:"service_scheme"`
	CreatedAt        time.Time `json:"created_at"`
}

type ProxacloudTunnelEvent struct {
	ID        int64     `json:"id"`
	TunnelID  int64     `json:"tunnel_id"`
	Step      string    `json:"step"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTunnelRequest struct {
	ClusterID          int64                        `json:"cluster_id"`
	IsGlobal           bool                         `json:"is_global"`
	Name               string                       `json:"name"`
	Namespace          string                       `json:"namespace"`
	DeploymentName     string                       `json:"deployment_name"`
	ProxacloudTunnelID string                       `json:"proxacloud_tunnel_id"`
	TunnelToken        string                       `json:"tunnel_token"`
	GatewayURL         string                       `json:"gateway_url"`
	TargetURL          string                       `json:"target_url"`
	Replicas           int                          `json:"replicas"`
	Image              string                       `json:"image"`
	ImagePullSecret    string                       `json:"image_pull_secret"`
	Apply              bool                         `json:"apply"`
	Routes             []ProxacloudTunnelRouteInput `json:"routes"`
}

type ProxacloudTunnelRouteInput struct {
	Hostname         string `json:"hostname"`
	Path             string `json:"path"`
	ServiceNamespace string `json:"service_namespace"`
	ServiceName      string `json:"service_name"`
	ServicePort      int    `json:"service_port"`
	ServiceScheme    string `json:"service_scheme"`
}

type DeployTunnelResponse struct {
	TunnelID int64    `json:"tunnel_id"`
	Status   string   `json:"status"`
	Output   string   `json:"output,omitempty"`
	Warnings []string `json:"warnings"`
}

type AttachTunnelRequest struct {
	ClusterID int64 `json:"cluster_id"`
	Apply     bool  `json:"apply"`
}

type UpdateTunnelTokenRequest struct {
	TunnelToken string `json:"tunnel_token"`
	Apply       bool   `json:"apply"`
}

type DeleteTunnelRequest struct {
	ConfirmationName string `json:"confirmation_name"`
}

type tunnelRecord struct {
	ProxacloudTunnel
	TokenSecretRef string
	LastApplied    sql.NullTime
}
