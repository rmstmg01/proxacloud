package server

import (
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"k8s-proxmox-manager/backend/internal/ansible"
	"k8s-proxmox-manager/backend/internal/auth"
	"k8s-proxmox-manager/backend/internal/cloudflaretunnels"
	"k8s-proxmox-manager/backend/internal/clusters"
	"k8s-proxmox-manager/backend/internal/config"
	"k8s-proxmox-manager/backend/internal/databaseclusters"
	"k8s-proxmox-manager/backend/internal/db"
	"k8s-proxmox-manager/backend/internal/monitoring"
	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/rbac"
	"k8s-proxmox-manager/backend/internal/secrets"
	"k8s-proxmox-manager/backend/internal/sshkeys"
	"k8s-proxmox-manager/backend/internal/sslcerts"
	"k8s-proxmox-manager/backend/internal/terraform"
	"k8s-proxmox-manager/backend/internal/topology"
	"k8s-proxmox-manager/backend/internal/users"
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
	cloudflareTunnelService := cloudflaretunnels.NewService(database, secretStore, clusterService)
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
	userHandler := users.NewHandler(userService, tokens)
	networkHandler := networks.NewHandler(networkService)
	proxmoxHandler := proxmox.NewHandler(proxmoxService)
	sshKeyHandler := sshkeys.NewHandler(sshKeyService)
	sslCertHandler := sslcerts.NewHandler(sslCertService)
	topologyHandler := topology.NewHandler(topologyService)
	clusterHandler := clusters.NewHandler(clusterService)
	databaseClusterHandler := databaseclusters.NewHandler(databaseClusterService)
	cloudflareTunnelHandler := cloudflaretunnels.NewHandler(cloudflareTunnelService)
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

		api.Group(func(protected chi.Router) {
			protected.Use(auth.AuthMiddleware(tokens))
			protected.Get("/auth/me", userHandler.Me)

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/users", userHandler.ListUsers)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/users", userHandler.CreateUser)

			protected.Get("/networks", networkHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks", networkHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/networks/{id}", networkHandler.Update)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks/{id}/set-default", networkHandler.SetDefault)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/networks/{id}/reserved-ips", networkHandler.ListReservedIPs)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/networks/{id}/reserved-ips", networkHandler.CreateReservedIP)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/networks/{id}/reserved-ips/{reservedId}", networkHandler.DeleteReservedIP)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/networks/node-tags", networkHandler.ListNodeTags)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Put("/networks/node-tags/{nodeName}", networkHandler.UpsertNodeTag)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/networks/node-tags/{nodeName}", networkHandler.DeleteNodeTag)

			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/proxmox/credentials", proxmoxHandler.CreateCredential)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Get("/proxmox/credentials", proxmoxHandler.ListCredentials)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Put("/proxmox/credentials/{id}", proxmoxHandler.UpdateCredential)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Delete("/proxmox/credentials/{id}", proxmoxHandler.DeleteCredential)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Post("/proxmox/credentials/{id}/discover-nodes", proxmoxHandler.DiscoverNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/proxmox/nodes", proxmoxHandler.ListNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperAdmin)).Patch("/proxmox/nodes/{id}", proxmoxHandler.SetNodeEnabled)

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/ssh-keys", sshKeyHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/ssh-keys", sshKeyHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/ssh-keys/{id}", sshKeyHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/ssl-certificates", sslCertHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/ssl-certificates", sslCertHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/ssl-certificates/{id}", sslCertHandler.Delete)

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/topology/plan", topologyHandler.Plan)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters", databaseClusterHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/plan", databaseClusterHandler.Plan)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/provision", databaseClusterHandler.Provision)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}", databaseClusterHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/database-clusters/{id}", databaseClusterHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/runs", databaseClusterHandler.ListRuns)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/events", databaseClusterHandler.ListEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/terraform/apply", databaseClusterHandler.ApplyTerraform)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/database-clusters/{id}/bootstrap", databaseClusterHandler.Bootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/database-clusters/{id}/k8s-manifests", databaseClusterHandler.KubernetesManifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels", cloudflareTunnelHandler.List)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels", cloudflareTunnelHandler.Create)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}", cloudflareTunnelHandler.Get)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/cloudflare-tunnels/{id}/deploy", cloudflareTunnelHandler.Deploy)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}/manifest", cloudflareTunnelHandler.Manifest)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/cloudflare-tunnels/{id}", cloudflareTunnelHandler.Delete)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/cloudflare-tunnels/{id}/events", cloudflareTunnelHandler.ListEvents)
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
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters", clusterHandler.ListClusters)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/check-name", clusterHandler.CheckName)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}", clusterHandler.GetCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/runs", clusterHandler.ListRuns)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/operations/latest", clusterHandler.GetLatestOperation)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/operations/events", clusterHandler.ListOperationEvents)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/nodes", clusterHandler.ListClusterNodes)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/bootstrap-config", clusterHandler.GetBootstrapConfig)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/bootstrap-keys", clusterHandler.ListBootstrapSSHKeys)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Delete("/clusters/{id}", clusterHandler.DeleteCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/start", clusterHandler.StartCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/restart", clusterHandler.RestartCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/shutdown", clusterHandler.ShutdownCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/redeploy", clusterHandler.RedeployCluster)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/provision", clusterHandler.Provision)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/provision-and-bootstrap", clusterHandler.ProvisionAndBootstrap)
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
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/secrets", clusterHandler.CreateGlobalSecret)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/config-maps", clusterHandler.CreateGlobalConfigMap)
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
