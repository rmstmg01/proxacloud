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
	"k8s-proxmox-manager/backend/internal/clusters"
	"k8s-proxmox-manager/backend/internal/config"
	"k8s-proxmox-manager/backend/internal/db"
	"k8s-proxmox-manager/backend/internal/networks"
	"k8s-proxmox-manager/backend/internal/proxmox"
	"k8s-proxmox-manager/backend/internal/rbac"
	"k8s-proxmox-manager/backend/internal/sshkeys"
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
	proxmoxService := proxmox.NewService(database)
	sshKeyService := sshkeys.NewService(database)
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
		cfg.OperationsWorkDir,
		cfg.TerraformAutoApply,
		cfg.AnsibleAutoApply,
	)

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
	topologyHandler := topology.NewHandler(topologyService)
	clusterHandler := clusters.NewHandler(clusterService)

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

			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/topology/plan", topologyHandler.Plan)
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
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/provision", clusterHandler.Provision)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/provision-and-bootstrap", clusterHandler.ProvisionAndBootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/bootstrap", clusterHandler.Bootstrap)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/scale", clusterHandler.Scale)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/upgrade", clusterHandler.Upgrade)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/replace-node", clusterHandler.ReplaceNode)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/rotate-kubeconfig", clusterHandler.RotateKubeconfig)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/metallb/deploy", clusterHandler.DeployMetalLB)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/metallb", clusterHandler.GetMetalLB)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/observability/deploy", clusterHandler.DeployObservability)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/observability", clusterHandler.GetObservability)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/etcd/snapshot", clusterHandler.CreateEtcdSnapshot)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Get("/clusters/{id}/etcd/snapshots", clusterHandler.ListEtcdSnapshots)
			protected.With(auth.RequireRoles(rbac.RoleSuperUser, rbac.RoleSuperAdmin)).Post("/clusters/{id}/etcd/restore", clusterHandler.RestoreEtcdSnapshot)
		})
	})

	return &App{Config: cfg, Router: r, DB: database}, nil
}
