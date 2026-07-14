package server

import (
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"proxacloud/backend/internal/ansible"
	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/backups"
	"proxacloud/backend/internal/billing"
	"proxacloud/backend/internal/cloudflaretunnels"
	"proxacloud/backend/internal/clusters"
	"proxacloud/backend/internal/config"
	"proxacloud/backend/internal/containers"
	"proxacloud/backend/internal/databaseclusters"
	"proxacloud/backend/internal/db"
	"proxacloud/backend/internal/firewalls"
	"proxacloud/backend/internal/mailer"
	"proxacloud/backend/internal/monitoring"
	"proxacloud/backend/internal/networks"
	"proxacloud/backend/internal/proxacloudtunnelapi"
	"proxacloud/backend/internal/proxacloudtunnels"
	"proxacloud/backend/internal/proxmox"
	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/regions"
	"proxacloud/backend/internal/secrets"
	"proxacloud/backend/internal/sshkeys"
	"proxacloud/backend/internal/sslcerts"
	"proxacloud/backend/internal/terraform"
	"proxacloud/backend/internal/topology"
	"proxacloud/backend/internal/users"
	"proxacloud/backend/internal/vps"
)

type App struct {
	Config config.Config
	Router http.Handler
	DB     *sql.DB
}

func NewApp() (*App, error) {
	cfg := config.Load()

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if cfg.AutoMigrate {
		migrationsDir := filepath.Join("migrations")
		if err := db.RunMigrations(database, migrationsDir); err != nil {
			_ = database.Close()
			return nil, err
		}
	}

	userService := users.NewService(database)
	if err := userService.SeedSuperAdmin(cfg.SuperAdminUsername, cfg.SuperAdminPassword); err != nil {
		_ = database.Close()
		return nil, err
	}

	networkService := networks.NewService(database)
	regionService := regions.NewService(database)
	secretStore, err := secrets.NewDBStore(database, cfg.SecretEncryptionKey)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	proxmoxService := proxmox.NewService(database, secretStore)
	sshKeyService := sshkeys.NewService(database, secretStore)
	sslCertService := sslcerts.NewService(database, secretStore)
	topologyService := topology.NewService(proxmoxService)
	terraformRunner := terraform.NewRunner(cfg.TerraformBinary, cfg.TerraformWorkDir, cfg.TerraformPluginDir)
	ansibleRunner := ansible.NewRunner(cfg.AnsibleBinary, cfg.AnsibleWorkDir)
	clusterService := clusters.NewService(
		database,
		networkService,
		topologyService,
		terraformRunner,
		ansibleRunner,
		proxmoxService,
		sshKeyService,
		sslCertService,
		secretStore,
		cfg.OperationsWorkDir,
		cfg.TerraformAutoApply,
		cfg.AnsibleAutoApply,
	)
	databaseClusterService := databaseclusters.NewService(
		database,
		networkService,
		proxmoxService,
		terraformRunner,
		proxmoxService,
		sshKeyService,
		secretStore,
		cfg.AnsibleBinary,
		cfg.AnsibleWorkDir,
		cfg.TerraformAutoApply,
		cfg.AnsibleAutoApply,
	)
	vpsService := vps.NewService(
		database,
		networkService,
		proxmoxService,
		proxmoxService,
		sshKeyService,
		secretStore,
		terraformRunner,
		regionService,
		cfg.TerraformAutoApply,
	)
	containerService := containers.NewService(
		database,
		networkService,
		proxmoxService,
		sshKeyService,
		secretStore,
		terraformRunner,
		cfg.TerraformAutoApply,
	)
	firewallService := firewalls.NewService(database, proxmoxService)
	backupService := backups.NewService(database, secretStore, proxmoxService)
	billingService := billing.NewService(database, billing.Config{
		PublicAppURL:        cfg.PublicAppURL,
		StripeSecretKey:     cfg.StripeSecretKey,
		StripeWebhookSecret: cfg.StripeWebhookSecret,
		PayPalClientID:      cfg.PayPalClientID,
		PayPalClientSecret:  cfg.PayPalClientSecret,
		PayPalEnvironment:   cfg.PayPalEnvironment,
	})
	cloudflareTunnelService := cloudflaretunnels.NewService(database, secretStore, clusterService)
	proxacloudTunnelService := proxacloudtunnels.NewService(database, secretStore, clusterService)
	monitoringService := monitoring.NewService(database, clusterService)

	if len(networkService.List()) == 0 {
		_, err = networkService.Create(networks.Profile{
			Name:      cfg.DefaultNetworkName,
			CIDR:      cfg.DefaultNetworkCIDR,
			Gateway:   cfg.DefaultNetworkGateway,
			DNS:       cfg.DefaultNetworkDNS,
			Bridge:    cfg.DefaultNetworkBridge,
			VLAN:      cfg.DefaultNetworkVLAN,
			IsDefault: true,
		})
		if err != nil {
			_ = database.Close()
			return nil, err
		}
	}

	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.JWTTTLMinutes)
	mailSender := mailer.NewSMTPMailer(mailer.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
	})
	tunnelAPIClient := proxacloudtunnelapi.New(proxacloudtunnelapi.Config{
		BaseURL:  cfg.ProxacloudTunnelAPIURL,
		APIToken: cfg.ProxacloudTunnelAPIToken,
	})
	vpsService.SetTunnelIntegration(vps.TunnelConfig{
		Enabled:         cfg.ProxacloudTunnelEnabled,
		APIURL:          cfg.ProxacloudTunnelAPIURL,
		GatewayURL:      cfg.ProxacloudTunnelGatewayURL,
		PublicHostname:  cfg.ProxacloudTunnelPublicHostname,
		HostnameSuffix:  cfg.ProxacloudTunnelHostnameSuffix,
		HTTPTargetURL:   cfg.ProxacloudTunnelHTTPSTargetURL,
		SSHHost:         cfg.ProxacloudTunnelSSHHost,
		SSHPort:         cfg.ProxacloudTunnelSSHPort,
		PublicPortStart: cfg.ProxacloudTunnelPublicPortStart,
		PublicPortEnd:   cfg.ProxacloudTunnelPublicPortEnd,
		AgentBinaryURL:  cfg.ProxacloudTunnelAgentBinaryURL,
		CloudflareDNS: vps.CloudflareDNSConfig{
			Enabled:    cfg.CloudflareDNSEnabled,
			APIToken:   cfg.CloudflareAPIToken,
			ZoneID:     cfg.CloudflareZoneID,
			Domain:     cfg.CloudflareDomain,
			Target:     cfg.CloudflareDNSTarget,
			RecordType: cfg.CloudflareDNSRecordType,
			Proxied:    cfg.CloudflareDNSProxied,
		},
	}, tunnelAPIClient, mailSender)
	clusterService.SetTunnelInfrastructure(vpsService.TunnelInfrastructure(), cfg.ProxacloudTunnelPublicPortStart, cfg.ProxacloudTunnelPublicPortEnd)
	userHandler := users.NewHandler(userService, tokens, mailSender, cfg.PublicAppURL)
	networkHandler := networks.NewHandler(networkService)
	regionHandler := regions.NewHandler(regionService)
	proxmoxHandler := proxmox.NewHandler(proxmoxService)
	sshKeyHandler := sshkeys.NewHandler(sshKeyService)
	sslCertHandler := sslcerts.NewHandler(sslCertService)
	topologyHandler := topology.NewHandler(topologyService)
	clusterHandler := clusters.NewHandler(clusterService, userService, billingService)
	databaseClusterHandler := databaseclusters.NewHandler(databaseClusterService, userService, billingService)
	vpsHandler := vps.NewHandler(vpsService, userService, billingService)
	containerHandler := containers.NewHandler(containerService, userService, billingService)
	firewallHandler := firewalls.NewHandler(firewallService)
	backupHandler := backups.NewHandler(backupService)
	billingHandler := billing.NewHandler(billingService, userService)
	cloudflareTunnelHandler := cloudflaretunnels.NewHandler(cloudflareTunnelService)
	proxacloudTunnelHandler := proxacloudtunnels.NewHandler(proxacloudTunnelService)
	monitoringHandler := monitoring.NewHandler(monitoringService)

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			successful, failed := proxmoxService.RefreshAllNodes()
			if successful+failed == 0 {
				continue
			}
			if failed > 0 {
				log.Printf("proxmox auto-discovery completed: %d successful, %d failed", successful, failed)
			}
		}
	}()

	// Initialize health check system with Proxmox-based checker
	healthChecker := clusters.NewProxmoxHealthChecker(proxmoxService)
	reconciliationWorker := clusters.NewReconciliationWorker(database, healthChecker, proxmoxService)
	reconciliationWorker.SetService(clusterService) // Wire in the service for recovery operations
	go reconciliationWorker.Start(30 * time.Second) // Run health checks every 30 seconds

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/auth/login", userHandler.Login)
		api.Post("/auth/login/2fa/verify", userHandler.Login2FAVerify)
		api.Post("/auth/login/2fa/send-email-code", userHandler.Login2FAResendEmailCode)
		api.Post("/auth/register", userHandler.Register)
		api.Get("/auth/verify-email", userHandler.VerifyEmail)
		api.Post("/auth/verify-email", userHandler.VerifyEmail)
		api.Post("/auth/password-reset/request", userHandler.RequestPasswordReset)
		api.Post("/auth/password-reset/confirm", userHandler.ResetPassword)
		api.Post("/billing/webhooks/stripe", billingHandler.StripeWebhook)

		api.Group(func(console chi.Router) {
			console.Use(auth.QueryTokenMiddleware(tokens, "token"))
			console.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/{id}/console/ws", vpsHandler.ConsoleWebSocket)
		})

		api.Group(func(protected chi.Router) {
			protected.Use(auth.AuthMiddleware(tokens))
			protected.Get("/auth/me", userHandler.Me)
			protected.Put("/profile", userHandler.UpdateProfile)
			protected.Post("/profile/password", userHandler.ChangePassword)
			protected.Get("/profile/2fa/status", userHandler.TwoFactorStatus)
			protected.Post("/profile/2fa/email/enable-request", userHandler.EnableTwoFactorEmailRequest)
			protected.Post("/profile/2fa/email/enable-confirm", userHandler.EnableTwoFactorEmailConfirm)
			protected.Post("/profile/2fa/totp/setup", userHandler.SetupTwoFactorTOTP)
			protected.Post("/profile/2fa/totp/confirm", userHandler.ConfirmTwoFactorTOTP)
			protected.Post("/profile/2fa/disable", userHandler.DisableTwoFactor)

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/users", userHandler.ListUsers)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/users", userHandler.CreateUser)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Put("/users/{id}", userHandler.UpdateUser)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Delete("/users/{id}", userHandler.DeleteUser)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/users/{id}/status", userHandler.SetUserStatus)

			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/billing/summary", billingHandler.Summary)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/billing/checkout", billingHandler.CreateCheckout)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/billing/paypal/{transactionID}/capture", billingHandler.CapturePayPal)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/billing/accounts", billingHandler.ListAccounts)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/billing/accounts/{accountID}/credits", billingHandler.AddCredit)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/billing/accounts/{accountID}/charge-current-month", billingHandler.ChargeCurrentMonth)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/billing/prices", billingHandler.ListPrices)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/billing/prices/{resourceType}", billingHandler.UpdatePrice)

			protected.Get("/vpcs", networkHandler.ListVPCs)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vpcs", networkHandler.CreateVPC)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/vpcs/{id}", networkHandler.UpdateVPC)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/vpcs/{id}", networkHandler.DeleteVPC)
			protected.Get("/networks", networkHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks", networkHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/networks/{id}", networkHandler.Update)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/networks/{id}", networkHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks/{id}/set-default", networkHandler.SetDefault)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/networks/{id}/ip-usage", networkHandler.GetIPUsage)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks/{id}/ip-usage/scan", networkHandler.ScanIPUsage)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/networks/{id}/reserved-ips", networkHandler.ListReservedIPs)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks/{id}/reserved-ips", networkHandler.CreateReservedIP)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/networks/{id}/reserved-ips/{reservedId}", networkHandler.DeleteReservedIP)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/networks/node-tags", networkHandler.ListNodeTags)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/networks/node-tags/{nodeName}", networkHandler.UpsertNodeTag)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/networks/node-tags/{nodeName}", networkHandler.DeleteNodeTag)

			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/regions", regionHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/regions", regionHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/regions/{id}", regionHandler.Update)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/regions/{id}", regionHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/regions/{id}/nodes", regionHandler.SetNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/regions/{id}/networks", regionHandler.SetNetworks)

			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/proxmox/credentials", proxmoxHandler.CreateCredential)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxmox/credentials", proxmoxHandler.ListCredentials)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Put("/proxmox/credentials/{id}", proxmoxHandler.UpdateCredential)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Delete("/proxmox/credentials/{id}", proxmoxHandler.DeleteCredential)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/proxmox/credentials/{id}/discover-nodes", proxmoxHandler.DiscoverNodes)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxmox/nodes", proxmoxHandler.ListNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Patch("/proxmox/nodes/{id}", proxmoxHandler.SetNodeEnabled)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/storage", proxmoxHandler.ListStorageInventory)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/backups/summary", backupHandler.Summary)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/backups/targets", backupHandler.ListTargets)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/backups/targets", backupHandler.CreateTarget)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/backups/targets/{id}", backupHandler.DeleteTarget)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/backups/jobs", backupHandler.ListJobs)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/backups/jobs", backupHandler.StartJob)

			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/ssh-keys", sshKeyHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/ssh-keys", sshKeyHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/ssh-keys/{id}", sshKeyHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/ssl-certificates", sslCertHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/ssl-certificates", sslCertHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/ssl-certificates/{id}", sslCertHandler.Delete)

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/topology/plan", topologyHandler.Plan)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters", databaseClusterHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/plan", databaseClusterHandler.Plan)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/database-clusters/provision", databaseClusterHandler.Provision)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}", databaseClusterHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/database-clusters/{id}", databaseClusterHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/runs", databaseClusterHandler.ListRuns)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/events", databaseClusterHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/terraform/apply", databaseClusterHandler.ApplyTerraform)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/bootstrap", databaseClusterHandler.Bootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/start", databaseClusterHandler.Start)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/restart", databaseClusterHandler.Restart)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/stop", databaseClusterHandler.Stop)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/k8s-manifests", databaseClusterHandler.KubernetesManifest)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps", vpsHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/vps", vpsHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/cloud-init-images", vpsHandler.ListImages)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/cloud-init-images", vpsHandler.CreateImage)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/cloud-init-images/discover", vpsHandler.DiscoverImages)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/{id}", vpsHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Patch("/vps/{id}", vpsHandler.Update)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Patch("/vps/{id}/hostname", vpsHandler.UpdateHostname)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/vps/{id}", vpsHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/{id}/events", vpsHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/{id}/tunnel", vpsHandler.GetTunnel)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/tunnel/retry", vpsHandler.RetryTunnel)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/vps/{id}/login-details", vpsHandler.LoginDetails)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/root-password/reset", vpsHandler.ResetRootPassword)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/deploy", vpsHandler.Deploy)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/redeploy", vpsHandler.Redeploy)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/start", vpsHandler.Start)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/restart", vpsHandler.Restart)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/shutdown", vpsHandler.Shutdown)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/console", vpsHandler.Console)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/vps/{id}/refresh-status", vpsHandler.RefreshStatus)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/containers", containerHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/containers", containerHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/containers/templates", containerHandler.ListTemplates)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/templates", containerHandler.CreateTemplate)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/containers/{id}", containerHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/containers/{id}", containerHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/containers/{id}/events", containerHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/deploy", containerHandler.Deploy)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/redeploy", containerHandler.Redeploy)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/start", containerHandler.Start)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/restart", containerHandler.Restart)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/shutdown", containerHandler.Shutdown)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/containers/{id}/refresh-status", containerHandler.RefreshStatus)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/firewalls", firewallHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/firewalls", firewallHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/firewalls/{id}", firewallHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/firewalls/{id}", firewallHandler.Update)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/firewalls/{id}", firewallHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/firewalls/{id}/rules", firewallHandler.CreateRule)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/firewalls/{id}/rules/{ruleId}", firewallHandler.UpdateRule)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/firewalls/{id}/rules/{ruleId}", firewallHandler.DeleteRule)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/firewalls/{id}/attachments", firewallHandler.CreateAttachment)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/firewalls/{id}/attachments/{attachmentId}", firewallHandler.DeleteAttachment)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/firewalls/{id}/apply", firewallHandler.Apply)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/firewalls/{id}/events", firewallHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels", cloudflareTunnelHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels", cloudflareTunnelHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}", cloudflareTunnelHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels/{id}/deploy", cloudflareTunnelHandler.Deploy)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels/{id}/attach-cluster", cloudflareTunnelHandler.AttachToCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels/{id}/token", cloudflareTunnelHandler.UpdateToken)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}/manifest", cloudflareTunnelHandler.Manifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/cloudflare-tunnels/{id}", cloudflareTunnelHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}/events", cloudflareTunnelHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxacloud-tunnels", proxacloudTunnelHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/proxacloud-tunnels", proxacloudTunnelHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxacloud-tunnels/{id}", proxacloudTunnelHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/proxacloud-tunnels/{id}/deploy", proxacloudTunnelHandler.Deploy)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/proxacloud-tunnels/{id}/attach-cluster", proxacloudTunnelHandler.AttachToCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/proxacloud-tunnels/{id}/token", proxacloudTunnelHandler.UpdateToken)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxacloud-tunnels/{id}/manifest", proxacloudTunnelHandler.Manifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/proxacloud-tunnels/{id}", proxacloudTunnelHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxacloud-tunnels/{id}/events", proxacloudTunnelHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/monitoring/status", monitoringHandler.Status)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/monitoring/stack", monitoringHandler.GetStack)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/monitoring/stack", monitoringHandler.UpsertStack)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/monitoring/stack/manifest", monitoringHandler.StackManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/monitoring/stack/apply", monitoringHandler.ApplyStack)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/monitoring/clusters", monitoringHandler.ListClusters)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/monitoring/clusters", monitoringHandler.CreateCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/monitoring/clusters/{id}/manifest", monitoringHandler.ClusterManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/monitoring/clusters/{id}/sync", monitoringHandler.SyncCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/monitoring/clusters/{id}", monitoringHandler.DeleteCluster)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters", clusterHandler.ListClusters)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/check-name", clusterHandler.CheckName)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}", clusterHandler.GetCluster)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/runs", clusterHandler.ListRuns)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/operations/latest", clusterHandler.GetLatestOperation)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/operations/events", clusterHandler.ListOperationEvents)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/nodes", clusterHandler.ListClusterNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/bootstrap-config", clusterHandler.GetBootstrapConfig)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/bootstrap-keys", clusterHandler.ListBootstrapSSHKeys)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}", clusterHandler.DeleteCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/start", clusterHandler.StartCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/restart", clusterHandler.RestartCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/shutdown", clusterHandler.ShutdownCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/redeploy", clusterHandler.RedeployCluster)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/clusters/provision", clusterHandler.Provision)
			protected.With(auth.RequireRoles(rbac.RoleNormalUser, rbac.RoleSuperUser, rbac.RoleSuperAdmin)).With(userService.RequireActiveCustomer()).Post("/clusters/provision-and-bootstrap", clusterHandler.ProvisionAndBootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/bootstrap", clusterHandler.Bootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/scale", clusterHandler.Scale)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/upgrade", clusterHandler.Upgrade)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/replace-node", clusterHandler.ReplaceNode)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/rotate-kubeconfig", clusterHandler.RotateKubeconfig)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/metallb/deploy", clusterHandler.DeployMetalLB)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/metallb", clusterHandler.GetMetalLB)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/ingress-controller/deploy", clusterHandler.DeployIngressController)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/longhorn/deploy", clusterHandler.DeployLonghorn)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/cert-manager/deploy", clusterHandler.DeployCertificateManager)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/secrets", clusterHandler.ListGlobalSecrets)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/secrets", clusterHandler.CreateGlobalSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/secrets/{secretID}", clusterHandler.UpdateGlobalSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/secrets/{secretID}", clusterHandler.DeleteGlobalSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/config-maps", clusterHandler.ListGlobalConfigMaps)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/config-maps", clusterHandler.CreateGlobalConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/config-maps/{configMapID}", clusterHandler.UpdateGlobalConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/config-maps/{configMapID}", clusterHandler.DeleteGlobalConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/image-pull-secrets", clusterHandler.ListGlobalImagePullSecrets)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/image-pull-secrets", clusterHandler.CreateGlobalImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/image-pull-secrets/{secretID}", clusterHandler.UpdateGlobalImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/image-pull-secrets/{secretID}", clusterHandler.DeleteGlobalImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/manifests", clusterHandler.ListGlobalSavedManifests)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/manifests", clusterHandler.CreateGlobalSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/manifests/{manifestID}", clusterHandler.UpdateGlobalSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/manifests/{manifestID}", clusterHandler.DeleteGlobalSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/secrets", clusterHandler.ListClusterSecrets)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/secrets", clusterHandler.CreateClusterSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/secrets/{secretID}", clusterHandler.UpdateClusterSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/secrets/{secretID}", clusterHandler.DeleteClusterSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/secrets/{secretID}/keys/{key}", clusterHandler.UpdateClusterSecretKey)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/secrets/{secretID}/keys/{key}", clusterHandler.DeleteClusterSecretKey)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/secrets/{secretID}/apply", clusterHandler.ApplyClusterSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/config-maps", clusterHandler.ListClusterConfigMaps)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/config-maps", clusterHandler.CreateClusterConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/config-maps/{configMapID}", clusterHandler.UpdateClusterConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/config-maps/{configMapID}", clusterHandler.DeleteClusterConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/config-maps/{configMapID}/keys/{key}", clusterHandler.UpdateClusterConfigMapKey)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/config-maps/{configMapID}/keys/{key}", clusterHandler.DeleteClusterConfigMapKey)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/config-maps/{configMapID}/apply", clusterHandler.ApplyClusterConfigMap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/image-pull-secrets", clusterHandler.ListClusterImagePullSecrets)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/image-pull-secrets", clusterHandler.CreateClusterImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/image-pull-secrets/{secretID}", clusterHandler.UpdateClusterImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/image-pull-secrets/{secretID}", clusterHandler.DeleteClusterImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/image-pull-secrets/{secretID}/apply", clusterHandler.ApplyClusterImagePullSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/manifests", clusterHandler.ListClusterSavedManifests)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/manifests", clusterHandler.CreateClusterSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/clusters/{id}/manifests/{manifestID}", clusterHandler.UpdateClusterSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}/manifests/{manifestID}", clusterHandler.DeleteClusterSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/manifests/{manifestID}/apply", clusterHandler.ApplyClusterSavedManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/manifests/{manifestID}/revisions", clusterHandler.ListClusterSavedManifestRevisions)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/manifests/{manifestID}/runs", clusterHandler.ListClusterSavedManifestApplyRuns)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/apps", clusterHandler.ListApplicationManifests)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/apps/deploy", clusterHandler.DeployApp)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/apps/{appID}/revisions", clusterHandler.ListApplicationManifestRevisions)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/apps/{appID}/revisions/{revisionID}", clusterHandler.GetApplicationManifestRevision)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/apps/{appID}/redeploy", clusterHandler.RedeployApplicationManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/kubectl-commands", clusterHandler.StartKubectlCommand)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/kubectl-commands/{runID}", clusterHandler.GetKubectlCommandRun)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/nodes/{nodeName}/commands", clusterHandler.StartNodeCommand)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/node-commands/{runID}", clusterHandler.GetNodeCommandRun)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/observability/deploy", clusterHandler.DeployObservability)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/observability", clusterHandler.GetObservability)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/etcd/snapshot", clusterHandler.CreateEtcdSnapshot)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/etcd/snapshots", clusterHandler.ListEtcdSnapshots)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/etcd/restore", clusterHandler.RestoreEtcdSnapshot)
			// Health and recovery endpoints
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/health", clusterHandler.GetClusterHealth)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/recovery/config", clusterHandler.SetRecoveryConfig)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/recovery/trigger", clusterHandler.TriggerNodeRecovery)
		})
	})

	return &App{Config: cfg, Router: r, DB: database}, nil
}
