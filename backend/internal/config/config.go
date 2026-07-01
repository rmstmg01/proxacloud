package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv              string
	Address             string
	DatabaseURL         string
	AutoMigrate         bool
	TerraformBinary     string
	TerraformWorkDir    string
	TerraformPluginDir  string
	TerraformAutoApply  bool
	AnsibleBinary       string
	AnsibleWorkDir      string
	AnsibleAutoApply    bool
	OperationsWorkDir   string
	JWTSecret           string
	SecretEncryptionKey string
	JWTTTLMinutes       int
	SuperAdminUsername  string
	SuperAdminPassword  string
	PublicAppURL        string
	SMTPHost            string
	SMTPPort            int
	SMTPUsername        string
	SMTPPassword        string
	SMTPFrom            string
	StripeSecretKey     string
	StripeWebhookSecret string
	PayPalClientID      string
	PayPalClientSecret  string
	PayPalEnvironment   string

	ProxacloudTunnelEnabled         bool
	ProxacloudTunnelAPIURL          string
	ProxacloudTunnelAPIToken        string
	ProxacloudTunnelGatewayURL      string
	ProxacloudTunnelPublicHostname  string
	ProxacloudTunnelHostnameSuffix  string
	ProxacloudTunnelHTTPSTargetURL  string
	ProxacloudTunnelSSHHost         string
	ProxacloudTunnelSSHPort         int
	ProxacloudTunnelPublicPortStart int
	ProxacloudTunnelPublicPortEnd   int
	ProxacloudTunnelAgentBinaryURL  string

	DefaultNetworkName    string
	DefaultNetworkCIDR    string
	DefaultNetworkGateway string
	DefaultNetworkDNS     []string
	DefaultNetworkBridge  string
	DefaultNetworkVLAN    int
}

func Load() Config {
	return Config{
		AppEnv:                          getenv("APP_ENV", "dev"),
		Address:                         getenv("APP_ADDR", ":8080"),
		DatabaseURL:                     getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/k8s_proxmox?sslmode=disable"),
		AutoMigrate:                     getenvBool("AUTO_MIGRATE", true),
		TerraformBinary:                 getenv("TERRAFORM_BINARY", "terraform"),
		TerraformWorkDir:                getenv("TERRAFORM_WORKDIR", "infra/terraform/runs"),
		TerraformPluginDir:              getenv("TERRAFORM_PLUGIN_DIR", "/terraform-providers"),
		TerraformAutoApply:              getenvBool("TERRAFORM_AUTO_APPLY", false),
		AnsibleBinary:                   getenv("ANSIBLE_BINARY", "ansible-playbook"),
		AnsibleWorkDir:                  getenv("ANSIBLE_WORKDIR", "infra/ansible/runs"),
		AnsibleAutoApply:                getenvBool("ANSIBLE_AUTO_APPLY", false),
		OperationsWorkDir:               getenv("OPERATIONS_WORKDIR", filepath.Join("data", "operations", "runs")),
		JWTSecret:                       getenv("JWT_SECRET", "change-me-in-prod"),
		SecretEncryptionKey:             getenv("SECRET_ENCRYPTION_KEY", "dev-only-change-me-secret-key"),
		JWTTTLMinutes:                   getenvInt("JWT_TTL_MINUTES", 120),
		SuperAdminUsername:              getenv("SUPER_ADMIN_USERNAME", "k8s-admin"),
		SuperAdminPassword:              getenv("SUPER_ADMIN_PASSWORD", "ChangeMeNow!"),
		PublicAppURL:                    getenv("PUBLIC_APP_URL", "http://localhost:3000"),
		SMTPHost:                        getenv("SMTP_HOST", ""),
		SMTPPort:                        getenvInt("SMTP_PORT", 587),
		SMTPUsername:                    getenv("SMTP_USERNAME", ""),
		SMTPPassword:                    getenv("SMTP_PASSWORD", ""),
		SMTPFrom:                        getenv("SMTP_FROM", ""),
		StripeSecretKey:                 getenv("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:             getenv("STRIPE_WEBHOOK_SECRET", ""),
		PayPalClientID:                  getenv("PAYPAL_CLIENT_ID", ""),
		PayPalClientSecret:              getenv("PAYPAL_CLIENT_SECRET", ""),
		PayPalEnvironment:               getenv("PAYPAL_ENVIRONMENT", "sandbox"),
		ProxacloudTunnelEnabled:         getenvBool("PROXACLOUD_TUNNEL_ENABLED", false),
		ProxacloudTunnelAPIURL:          getenv("PROXACLOUD_TUNNEL_API_URL", ""),
		ProxacloudTunnelAPIToken:        getenv("PROXACLOUD_TUNNEL_API_TOKEN", ""),
		ProxacloudTunnelGatewayURL:      getenv("PROXACLOUD_TUNNEL_GATEWAY_URL", ""),
		ProxacloudTunnelPublicHostname:  getenv("PROXACLOUD_TUNNEL_PUBLIC_HOSTNAME", ""),
		ProxacloudTunnelHostnameSuffix:  getenv("PROXACLOUD_TUNNEL_HOSTNAME_SUFFIX", ".proxaops.com"),
		ProxacloudTunnelHTTPSTargetURL:  getenv("PROXACLOUD_TUNNEL_HTTP_TARGET_URL", "http://127.0.0.1:80"),
		ProxacloudTunnelSSHHost:         getenv("PROXACLOUD_TUNNEL_SSH_HOST", "127.0.0.1"),
		ProxacloudTunnelSSHPort:         getenvInt("PROXACLOUD_TUNNEL_SSH_PORT", 22),
		ProxacloudTunnelPublicPortStart: getenvInt("PROXACLOUD_TUNNEL_PUBLIC_PORT_START", 22000),
		ProxacloudTunnelPublicPortEnd:   getenvInt("PROXACLOUD_TUNNEL_PUBLIC_PORT_END", 29999),
		ProxacloudTunnelAgentBinaryURL:  getenv("PROXACLOUD_TUNNEL_AGENT_BINARY_URL", ""),
		DefaultNetworkName:              getenv("DEFAULT_NETWORK_NAME", "homelab-default"),
		DefaultNetworkCIDR:              getenv("DEFAULT_NETWORK_CIDR", "192.168.8.0/24"),
		DefaultNetworkGateway:           getenv("DEFAULT_NETWORK_GATEWAY", "192.168.8.1"),
		DefaultNetworkDNS:               splitCSV(getenv("DEFAULT_NETWORK_DNS", "1.1.1.1,8.8.8.8")),
		DefaultNetworkBridge:            getenv("DEFAULT_NETWORK_BRIDGE", "vmbr0"),
		DefaultNetworkVLAN:              getenvInt("DEFAULT_NETWORK_VLAN", 0),
	}
}

func getenv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func getenvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getenvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
