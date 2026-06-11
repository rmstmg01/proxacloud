package cloudflaretunnels

import (
	"database/sql"
	"errors"
	"time"
)

const (
	RoutingModeTraefik = "traefik"
	RoutingModeDirect  = "direct"

	ProvisioningModeManualToken   = "manual_token"
	ProvisioningModeCloudflareAPI = "cloudflare_api"
)

var (
	ErrTunnelNotFound           = errors.New("cloudflare tunnel not found")
	ErrTunnelDeleteConfirmation = errors.New("cloudflare tunnel name confirmation does not match")
)

type ManifestApplier interface {
	ApplyRawManifest(clusterID int64, namespace, manifest string) (string, error)
}

type CloudflareTunnel struct {
	ID                   int64                   `json:"id"`
	ClusterID            int64                   `json:"cluster_id"`
	ClusterName          string                  `json:"cluster_name,omitempty"`
	IsGlobal             bool                    `json:"is_global"`
	Name                 string                  `json:"name"`
	Namespace            string                  `json:"namespace"`
	DeploymentName       string                  `json:"deployment_name"`
	RoutingMode          string                  `json:"routing_mode"`
	ProvisioningMode     string                  `json:"provisioning_mode"`
	CloudflareAccountID  string                  `json:"cloudflare_account_id,omitempty"`
	CloudflareZoneID     string                  `json:"cloudflare_zone_id,omitempty"`
	CloudflareTunnelID   string                  `json:"cloudflare_tunnel_id,omitempty"`
	CloudflareTunnelName string                  `json:"cloudflare_tunnel_name,omitempty"`
	HasTunnelToken       bool                    `json:"has_tunnel_token"`
	HasAPIToken          bool                    `json:"has_api_token"`
	Replicas             int                     `json:"replicas"`
	Image                string                  `json:"image"`
	IngressNamespace     string                  `json:"ingress_namespace"`
	IngressServiceName   string                  `json:"ingress_service_name"`
	IngressServicePort   int                     `json:"ingress_service_port"`
	IngressServiceScheme string                  `json:"ingress_service_scheme"`
	ConfigureDNS         bool                    `json:"configure_dns"`
	Status               string                  `json:"status"`
	LastManifest         string                  `json:"last_manifest,omitempty"`
	LastApplyOutput      string                  `json:"last_apply_output,omitempty"`
	LastAppliedAt        *time.Time              `json:"last_applied_at,omitempty"`
	Routes               []CloudflareTunnelRoute `json:"routes"`
	CreatedAt            time.Time               `json:"created_at"`
	UpdatedAt            time.Time               `json:"updated_at"`
}

type CloudflareTunnelRoute struct {
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

type CloudflareTunnelEvent struct {
	ID        int64     `json:"id"`
	TunnelID  int64     `json:"tunnel_id"`
	Step      string    `json:"step"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTunnelRequest struct {
	ClusterID            int64                        `json:"cluster_id"`
	IsGlobal             bool                         `json:"is_global"`
	Name                 string                       `json:"name"`
	Namespace            string                       `json:"namespace"`
	DeploymentName       string                       `json:"deployment_name"`
	RoutingMode          string                       `json:"routing_mode"`
	ProvisioningMode     string                       `json:"provisioning_mode"`
	TunnelToken          string                       `json:"tunnel_token"`
	CloudflareAPIToken   string                       `json:"cloudflare_api_token"`
	CloudflareAccountID  string                       `json:"cloudflare_account_id"`
	CloudflareZoneID     string                       `json:"cloudflare_zone_id"`
	CloudflareTunnelID   string                       `json:"cloudflare_tunnel_id"`
	CloudflareTunnelName string                       `json:"cloudflare_tunnel_name"`
	Replicas             int                          `json:"replicas"`
	Image                string                       `json:"image"`
	IngressNamespace     string                       `json:"ingress_namespace"`
	IngressServiceName   string                       `json:"ingress_service_name"`
	IngressServicePort   int                          `json:"ingress_service_port"`
	IngressServiceScheme string                       `json:"ingress_service_scheme"`
	ConfigureDNS         bool                         `json:"configure_dns"`
	Apply                bool                         `json:"apply"`
	Routes               []CloudflareTunnelRouteInput `json:"routes"`
}

type CloudflareTunnelRouteInput struct {
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

type DeleteTunnelRequest struct {
	ConfirmationName string `json:"confirmation_name"`
}

type tunnelRecord struct {
	CloudflareTunnel
	TokenSecretRef    string
	APITokenSecretRef string
	LastApplied       sql.NullTime
}
