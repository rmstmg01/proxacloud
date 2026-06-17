# ProxaCloud

ProxaCloud is a private cloud control plane for building and operating Kubernetes-focused infrastructure, virtual servers, databases, networking, monitoring, and access workflows from one portal. The current provider implementation targets Proxmox, combining a React admin UI, a Go API, PostgreSQL persistence, Proxmox inventory discovery, Terraform VM provisioning, Ansible kubeadm bootstrap, Kubernetes application operations, database-cluster automation, Cloudflare tunnels, and reusable manifest/config catalogs.

The project is intended for cloud-service-provider, private-cloud, homelab, and small platform-team environments where infrastructure and supporting services are provisioned from owned capacity and then operated from a single UI.

## Current Capabilities

- JWT login with role-based access control.
- User management with seeded super-admin bootstrap.
- Proxmox credential storage, discovery, inventory refresh, node enablement, and resource tracking.
- Network profiles, reserved IPs, Proxmox node network tags, bridges, VLANs, gateways, and DNS settings.
- SSH key-pair and SSL certificate catalogs.
- Topology planning for single, dual, and tri-node Proxmox layouts.
- Kubernetes cluster provisioning through generated Terraform workspaces.
- Optional Terraform apply from the backend.
- Kubernetes bootstrap through generated Ansible/kubeadm workspaces.
- Optional Ansible execution from the backend.
- Provision-and-bootstrap workflow with live operation progress and event logs.
- Cluster lifecycle operations: start, shutdown, restart, redeploy, scale, upgrade plan, node replacement, kubeconfig rotation, and delete.
- Cluster health checks, node health reconciliation, recovery configuration, and manual recovery trigger.
- Cluster node command and kubectl command execution with persisted output.
- MetalLB, Traefik ingress controller, Longhorn, cert-manager, application manifest, observability, etcd snapshot, and etcd restore workflows.
- Cluster-scoped and global Secrets, ConfigMaps, image pull secrets, and saved manifests.
- Application deployment history, manifest revisions, redeploy, and kubectl apply logs.
- MariaDB/Galera database-cluster planning, provisioning, Terraform apply, bootstrap, event log, and Kubernetes manifest generation.
- Cloudflare Tunnel records, Kubernetes manifests, deployment, and event logs.
- Central monitoring stack configuration, stack manifest generation/apply, remote cluster sync, and remote-cluster agent manifests.
- Built-in Kubernetes troubleshooting and command reference pages.

## Project Layout

```text
.
|-- backend/
|   |-- cmd/api-server/              Go API entrypoint
|   |-- internal/
|   |   |-- ansible/                 Ansible runner
|   |   |-- auth/                    JWT and auth middleware
|   |   |-- cloudflaretunnels/       Cloudflare Tunnel model, handler, manifest service
|   |   |-- clusters/                Cluster provisioning, operations, health, commands, apps
|   |   |-- config/                  Environment configuration
|   |   |-- databaseclusters/        MariaDB/Galera cluster automation
|   |   |-- db/                      PostgreSQL connection and migration runner
|   |   |-- monitoring/              Central monitoring stack and remote agents
|   |   |-- networks/                Network profiles, reserved IPs, node tags
|   |   |-- proxmox/                 Proxmox API client, credentials, discovery
|   |   |-- rbac/                    Role names
|   |   |-- secrets/                 Encrypted secret storage helper
|   |   |-- server/                  Router composition
|   |   |-- sshkeys/                 SSH key-pair catalog
|   |   |-- sslcerts/                SSL certificate catalog
|   |   |-- terraform/               Terraform runner
|   |   |-- topology/                Proxmox placement planner
|   |   `-- users/                   Users, login, seed admin
|   `-- migrations/                  PostgreSQL schema migrations
|-- frontend/
|   |-- src/api/                     Axios API clients
|   |-- src/components/              Layout, sidebar, protected routes, UI primitives
|   |-- src/context/                 Auth context
|   |-- src/pages/                   Main application pages
|   `-- Dockerfile                   Production nginx image
|-- infra/
|   |-- ansible/                     Generated/bootstrap Ansible assets
|   |-- k8s/                         Deployment and monitoring Kubernetes manifests
|   `-- terraform/                   Generated/provisioning Terraform assets
|-- docs/                            API notes and operational documentation
|-- docker-compose.yml               Local/container deployment
|-- .env.example                     Example environment configuration
`-- README.md
```

## Architecture

```text
Browser
  |
  | HTTPS/HTTP
  v
Frontend container
  - nginx
  - built React/Vite application
  |
  | /api/v1 requests
  v
Backend container
  - Go API server
  - JWT auth and RBAC
  - PostgreSQL persistence
  - Proxmox API client
  - Terraform runner
  - Ansible runner
  - kubectl/operation helpers
  - background discovery and health loops
  |
  | SQL
  v
PostgreSQL

Backend also talks to:
  - Proxmox API for credentials, nodes, inventory, and VM operations
  - Terraform for VM provisioning workspaces
  - Ansible for kubeadm bootstrap workspaces
  - kubectl through KUBECONFIG for Kubernetes manifests and commands
  - Cloudflare Tunnel manifests generated for Kubernetes deployment
```

### Frontend

The frontend is a React 18 application using Vite, TypeScript, Tailwind CSS, React Router, TanStack Query, Axios, and lucide-react icons.

Main routes:

| Route | Purpose |
| --- | --- |
| `/login` | Sign in |
| `/dashboard` | Platform overview |
| `/clusters` | Kubernetes cluster list |
| `/clusters/new` | Provision a Kubernetes cluster |
| `/clusters/:id` | Cluster detail, lifecycle, apps, health, command, backup, and add-on operations |
| `/database-clusters` | MariaDB/Galera database clusters |
| `/monitoring` | Central monitoring stack and remote cluster agents |
| `/cloudflare-tunnels` | Cloudflare Tunnel records and manifests |
| `/commands` | Command reference and command center |
| `/networks` | Network profiles, reserved IPs, and Proxmox node tags |
| `/proxmox` | Proxmox credentials and discovered nodes |
| `/topology` | Placement/topology planner |
| `/users` | User administration |
| `/ssh-keys` | SSH key catalog |
| `/ssl-certificates` | TLS certificate catalog |
| `/secrets` | Global/unattached Secret catalog |
| `/configmaps` | Global/unattached ConfigMap catalog |
| `/manifests` | Global saved manifest catalog |
| `/documents` | Kubernetes/database troubleshooting documents |

### Backend

The backend is a Go API using:

- `go-chi/chi` routing.
- JWT authentication.
- Role-based middleware.
- PostgreSQL through `database/sql` and `lib/pq`.
- Startup migrations.
- Encrypted secret storage for sensitive values.
- Background Proxmox auto-discovery.
- Background cluster health reconciliation.
- Terraform, Ansible, and Kubernetes operation runners.

The backend entrypoint is:

```text
backend/cmd/api-server/main.go
```

Router wiring lives in:

```text
backend/internal/server/router.go
```

### Database

PostgreSQL stores users, credentials, inventory, cluster records, operation history, generated workspace metadata, health status, catalogs, database-cluster records, monitoring configuration, and Cloudflare Tunnel data.

Migrations live in:

```text
backend/migrations/
```

Migration coverage currently includes:

- users and roles
- network profiles
- Proxmox credentials and inventory
- Kubernetes cluster provisioning
- cluster maintenance operations
- observability and backup state
- bootstrap configuration and SSH keys
- MetalLB configuration
- cluster operations and event logs
- Proxmox node resource metrics
- reserved IPs and node network tags
- cluster node health and recovery events
- SSL certificates
- application manifest history
- managed secrets
- application secrets and ConfigMaps
- global secret/config catalog items
- saved manifests
- MariaDB/Galera database clusters and events
- Cloudflare tunnels
- monitoring stack and remote cluster records
- cluster image pull secrets
- aggregate cluster health status

## Roles And Access

The application uses three roles:

| Role | Description |
| --- | --- |
| `normal_user` | Authenticated user. Suitable for limited read/workflow access where routes allow it. |
| `super_user` | Operational user. Can manage clusters, networks, topology, SSH keys, add-ons, database clusters, monitoring, tunnels, and catalogs. |
| `super_admin` | Full administrator. Also manages users, Proxmox credentials, and Proxmox node enablement. |

The backend seeds a super admin on startup if no matching user exists.

Default development credentials:

```text
Username: k8s-admin
Password: ChangeMeNow!
```

Change these before any non-local deployment.

## Runtime Configuration

Copy `.env.example` to `.env` and update values for your environment.

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `dev` | Application environment label |
| `APP_ADDR` | `:8080` | Backend listen address |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/k8s_proxmox?sslmode=disable` | PostgreSQL connection string |
| `AUTO_MIGRATE` | `true` | Run migrations on backend startup |
| `TERRAFORM_BINARY` | `terraform` | Terraform executable |
| `TERRAFORM_WORKDIR` | `infra/terraform/runs` | Terraform run/workspace directory |
| `TERRAFORM_PLUGIN_DIR` | `/terraform-providers` | Terraform provider plugin directory |
| `TERRAFORM_AUTO_APPLY` | `false` | Allow backend-triggered Terraform apply |
| `ANSIBLE_BINARY` | `ansible-playbook` | Ansible executable |
| `ANSIBLE_WORKDIR` | `infra/ansible/runs` | Ansible run/workspace directory |
| `ANSIBLE_AUTO_APPLY` | `false` | Allow backend-triggered Ansible execution |
| `OPERATIONS_WORKDIR` | `data/operations/runs` | Generated Kubernetes operation manifests and scripts |
| `KUBECONFIG_DIR` | `/root/.kube` | Host kubeconfig directory mounted into backend and kubectl-runner |
| `JWT_SECRET` | `change-me-in-prod` | JWT signing secret |
| `SECRET_ENCRYPTION_KEY` | `dev-only-change-me-secret-key` | Encryption key for stored secrets |
| `JWT_TTL_MINUTES` | `120` | JWT lifetime |
| `SUPER_ADMIN_USERNAME` | `k8s-admin` | Seeded admin username |
| `SUPER_ADMIN_PASSWORD` | `ChangeMeNow!` | Seeded admin password |
| `DEFAULT_NETWORK_NAME` | `homelab-default` | Seeded network profile name |
| `DEFAULT_NETWORK_CIDR` | `192.168.8.0/24` | Seeded network CIDR |
| `DEFAULT_NETWORK_GATEWAY` | `192.168.8.1` | Seeded gateway |
| `DEFAULT_NETWORK_DNS` | `1.1.1.1,8.8.8.8` | Seeded DNS servers |
| `DEFAULT_NETWORK_BRIDGE` | `vmbr0` | Default Proxmox bridge |
| `DEFAULT_NETWORK_VLAN` | `0` | Default VLAN tag |
| `HTTP_PORT` | `80` | Frontend HTTP port |
| `HTTPS_PORT` | `443` | Frontend HTTPS port |
| `TLS_CERTS_DIR` | `./certs` | Host TLS certificate directory mounted into nginx |

## Docker Compose Deployment

Build and start the stack:

```powershell
docker compose up -d --build
```

Check backend logs:

```powershell
docker compose logs -f backend
```

Stop the stack:

```powershell
docker compose down
```

Default service endpoints:

- Backend API: `http://localhost:8080`
- PostgreSQL: `localhost:5432`
- Frontend portal: `https://localhost` or the configured domain/port

Compose services:

| Service | Description |
| --- | --- |
| `postgres` | PostgreSQL 16 database |
| `backend` | Go API server with generated data volumes |
| `frontend` | nginx serving the built React app |
| `kubectl-runner` | Optional ops-profile container for running kubectl against generated manifests |

Start the optional kubectl runner:

```powershell
docker compose --profile ops up -d kubectl-runner
```

## Local Development

Run backend locally:

```powershell
cd backend
go mod tidy
go run ./cmd/api-server
```

Run backend tests:

```powershell
cd backend
go test ./...
```

Run frontend locally:

```powershell
cd frontend
npm install
npm run dev
```

Build frontend:

```powershell
cd frontend
npm run build
```

## Core Kubernetes Cluster Workflow

### 1. Prepare Access And Inventory

1. Start the stack.
2. Log in as the seeded super admin.
3. Add Proxmox credentials.
4. Discover Proxmox nodes.
5. Enable the Proxmox nodes that can receive Kubernetes VMs.
6. Review CPU, memory, disk, node status, and placement eligibility.
7. Create or update network profiles.
8. Reserve IPs that should not be assigned to generated VMs.
9. Configure node network tags for Proxmox-node-specific bridges, VLANs, or network intent.
10. Create SSH key pairs if the bootstrap workflow should use stored keys.

### 2. Plan Topology

Supported topology types:

| Topology | Requirement | Notes |
| --- | --- | --- |
| `single` | At least one enabled Proxmox node | Not highly available |
| `dual` | At least two enabled Proxmox nodes | Better spread, weaker quorum than tri-node |
| `tri` | At least three enabled Proxmox nodes | Preferred HA placement |

The topology planner evaluates enabled Proxmox nodes, available memory, and placement rules. It returns placements for control-plane and worker VMs plus warnings when resources or topology are weak.

### 3. Provision VMs

Cluster provisioning collects:

- cluster name
- topology type
- Kubernetes version
- Ubuntu cloud image
- network profile
- optional per-node network profile overrides
- optional per-node IP pool starts
- Proxmox credential or per-node credential overrides
- SSH public key or stored SSH key pair
- control-plane CPU, memory, and disk sizes
- worker CPU, memory, and disk sizes
- optional Terraform apply flag
- optional auto-bootstrap settings
- optional MetalLB settings for post-bootstrap deployment

The backend creates:

- a cluster record
- node records
- VM names and IP assignments
- Terraform workspace files
- provision run records
- live operation records and timeline events

If `TERRAFORM_AUTO_APPLY=false`, the backend generates the workspace and returns metadata without applying it. If apply is allowed and requested, Terraform runs in the configured work directory.

### 4. Bootstrap Kubernetes

Bootstrap settings include:

- SSH user
- private key, stored key pair, or existing bootstrap key reference
- kube-vip control-plane VIP
- pod CIDR
- service CIDR
- CNI selection
- node IP mapping
- optional apply flag

The backend writes Ansible inventory/playbook material and stores bootstrap run history. If `ANSIBLE_AUTO_APPLY=false`, the workspace is generated but not executed. If apply is allowed and requested, `ansible-playbook` runs from the backend.

When bootstrap succeeds, the backend can export the generated `admin.conf` into the mounted kubeconfig path:

```text
/app/backend/.kube/config
```

In Docker Compose that path maps to:

```text
${KUBECONFIG_DIR:-/root/.kube}/config
```

### 5. Track Live Operation Status

Long-running operations create:

- a latest operation record
- current step
- status
- progress percentage
- started/updated/finished timestamps
- error summary when failed
- event timeline entries

The cluster detail Runs tab polls the latest operation and its events. This is used for provisioning, bootstrap, auto-bootstrap, apply, and other cluster operations.

## Cluster Maintenance

Cluster detail pages expose operational areas for:

- cluster details and run history
- live operation timeline
- node inventory and health
- start, restart, shutdown, and redeploy
- worker scaling
- Kubernetes upgrade planning
- unhealthy node replacement
- kubeconfig token rotation
- MetalLB deployment/status
- Traefik ingress controller deployment
- Longhorn deployment
- cert-manager and Let's Encrypt issuer deployment
- application manifest deployment
- saved manifests and revision history
- cluster Secrets, ConfigMaps, and image pull secrets
- kubectl command execution
- node-level command execution
- observability deployment/status
- etcd snapshots and restore planning
- health and recovery configuration

The Cluster Maintenance tab is the final tab on the cluster detail page, after etcd Backup. It groups controlled lifecycle tasks such as scaling workers, upgrading Kubernetes, replacing unhealthy nodes, and rotating kubeconfig tokens.

Some workflows directly apply manifests using the configured kubeconfig. Others generate manifests, scripts, plans, or helper commands for review before manual execution.

## Application And Catalog Workflows

### Secrets

Cluster Secrets can be:

- native Kubernetes Secrets with values stored through the backend encrypted secret store
- provider-backed references for external secret systems
- marked global for reuse across clusters
- created from a cluster page or as unattached global catalog entries
- applied to a cluster namespace before application deployment

Supported provider-style references include Vault, External Secrets Operator, AWS Secrets Manager, Azure Key Vault, Google Secret Manager, and Bitnami Sealed Secrets.

### ConfigMaps

Cluster ConfigMaps store non-sensitive application configuration. They can be:

- cluster-scoped
- marked global
- created as unattached global catalog items
- copied into an application namespace during deployment
- updated key-by-key

Do not put credentials, tokens, private keys, kubeconfigs, or database passwords in ConfigMaps.

### Image Pull Secrets

Image pull secrets can be created globally or per cluster and applied into target namespaces before deploying workloads that reference private registries.

### Saved Manifests

Saved manifests provide a reusable YAML catalog. They support:

- global saved manifests
- cluster-scoped saved manifests
- updates and deletes
- revision history
- apply runs
- kubectl output capture

### Application Deployments

Application deployments can select predefined Secrets, ConfigMaps, image pull secrets, and YAML manifests. The backend applies selected supporting resources first, then applies the application manifest and records revision and output history.

## Add-On Workflows

### MetalLB

MetalLB deployment collects namespace, version, address pool, address ranges, and optional L2 interfaces. When applied automatically after bootstrap, the backend waits for controller rollout and records diagnostics when rollout does not become ready.

Generated MetalLB artifacts are written under the configured operations work directory.

### Traefik Ingress

The ingress controller workflow creates Traefik resources, RBAC, an ingress class, deployment, and a `LoadBalancer` service so MetalLB can allocate an external IP.

### cert-manager

The cert-manager workflow can install cert-manager resources and create a Let's Encrypt issuer for Traefik HTTP-01 validation. Public DNS and ports 80/443 must be reachable for HTTP-01 certificates. Private-only addresses need public forwarding or a DNS-01 style workflow.

### Longhorn

The Longhorn workflow prepares and applies Kubernetes storage resources for clusters where persistent storage should be managed through Longhorn.

### Observability

Cluster observability deployment stores dashboard and manifest metadata. The separate Monitoring page manages the central monitoring stack and remote monitored cluster agents.

### etcd Backup And Restore

The etcd workflows track snapshot and restore operations. Restore operations should be treated as high-risk maintenance actions and reviewed carefully before applying in production-like environments.

## Database Cluster Workflow

The database-cluster module focuses on MariaDB/Galera clusters on Proxmox VMs with Kubernetes integration manifests.

Typical workflow:

1. Create a database cluster plan.
2. Review topology, node placement, network settings, and credentials.
3. Provision database VMs through generated Terraform.
4. Optionally run Terraform apply.
5. Bootstrap MariaDB/Galera through generated Ansible.
6. Review event logs and run history.
7. Generate Kubernetes manifests for applications that consume the external database service.

Database-cluster APIs live under:

```text
/api/v1/database-clusters
```

More detail is available in:

```text
docs/mariadb-galera-cluster.md
```

## Cloudflare Tunnel Workflow

Cloudflare Tunnel records provide a way to define tunnel-backed ingress manifests for Kubernetes workloads.

Typical workflow:

1. Create a tunnel record.
2. Review tunnel configuration.
3. Generate the Kubernetes manifest.
4. Deploy the tunnel resources to the target cluster.
5. Review deployment events.

Cloudflare Tunnel APIs live under:

```text
/api/v1/cloudflare-tunnels
```

## Monitoring Workflow

The monitoring module manages a central observability stack plus remote cluster registrations.

Central stack workflow:

1. View monitoring status.
2. Create or update stack configuration.
3. Generate stack manifests.
4. Apply the stack.

Remote cluster workflow:

1. Register a remote cluster.
2. Generate the remote cluster agent manifest.
3. Apply or sync the remote cluster.
4. Review status and remove stale registrations when needed.

Monitoring APIs live under:

```text
/api/v1/monitoring
```

Additional Kubernetes monitoring assets are in:

```text
infra/k8s/monitoring/
```

## API Overview

Base API path:

```text
/api/v1
```

Public endpoints:

- `GET /health`
- `POST /api/v1/auth/login`

Authenticated endpoint groups:

| Group | Examples |
| --- | --- |
| Auth | `GET /auth/me` |
| Users | `GET /users`, `POST /users` |
| Networks | `GET /networks`, `POST /networks`, reserved IPs, node tags |
| Proxmox | credentials, discovery, node listing, node enablement |
| SSH keys | list, create, delete |
| SSL certificates | list, create, delete |
| Topology | `POST /topology/plan` |
| Database clusters | list, plan, provision, runs, events, Terraform apply, bootstrap, manifests |
| Cloudflare tunnels | list, create, get, deploy, manifest, delete, events |
| Monitoring | status, stack config, stack manifest/apply, remote clusters |
| Kubernetes clusters | list, check name, get, delete, provision, provision-and-bootstrap |
| Cluster operations | runs, latest operation, operation events, nodes, health, recovery |
| Cluster lifecycle | start, restart, shutdown, redeploy, bootstrap, scale, upgrade, replace node, rotate kubeconfig |
| Cluster add-ons | MetalLB, ingress controller, Longhorn, cert-manager, observability, etcd |
| Catalogs | global Secrets, ConfigMaps, image pull secrets, saved manifests |
| Cluster resources | cluster Secrets, ConfigMaps, image pull secrets, saved manifests |
| Applications | deploy, list revisions, inspect revisions, redeploy |
| Commands | kubectl command runs and node command runs |

Role restrictions are enforced in `backend/internal/server/router.go`. Proxmox credential management and Proxmox node enablement require `super_admin`. Most operational workflows require `super_user` or `super_admin`.

## HTTPS Certificates

The frontend container expects certificates mounted at:

```text
/etc/nginx/certs/fullchain.pem
/etc/nginx/certs/privkey.pem
```

Place existing certificates in the configured host directory:

```powershell
mkdir certs -Force
Copy-Item <path-to-fullchain.pem> certs/fullchain.pem
Copy-Item <path-to-privkey.pem> certs/privkey.pem
docker compose up -d --build frontend
```

Adjust ports and certificate location with:

```text
HTTP_PORT=80
HTTPS_PORT=443
TLS_CERTS_DIR=./certs
```

## Generated Files And Work Directories

The backend writes generated artifacts to configured work directories:

| Artifact | Default path |
| --- | --- |
| Terraform cluster workspaces | `infra/terraform/runs` or `/app/backend/data/terraform/runs` in Compose |
| Ansible bootstrap workspaces | `infra/ansible/runs` or `/app/backend/data/ansible/runs` in Compose |
| Kubernetes operation manifests/scripts | `data/operations/runs` or `/app/backend/data/operations/runs` in Compose |
| Backend kubeconfig mount | `/app/backend/.kube/config` in Compose |

Generated files should be reviewed before use when auto-apply is disabled.

## Security Notes

- Change `JWT_SECRET`, `SECRET_ENCRYPTION_KEY`, `SUPER_ADMIN_USERNAME`, and `SUPER_ADMIN_PASSWORD` before non-local use.
- Use a unique high-entropy `SECRET_ENCRYPTION_KEY`; keep it outside Git and normal database backups.
- Proxmox token secrets, SSH private keys, TLS private keys, bootstrap keys, and native Kubernetes Secret values should be stored through the encrypted backend secret store.
- Keep `TERRAFORM_AUTO_APPLY=false` and `ANSIBLE_AUTO_APPLY=false` until you have reviewed generated workspaces and trust the operational flow.
- Use least-privilege Proxmox API tokens.
- Protect the mounted kubeconfig directory. It can grant administrative access to provisioned clusters.
- Review generated Terraform, Ansible, Kubernetes, monitoring, tunnel, database, and restore manifests before applying them in production-like environments.
- Treat etcd restore, node replacement, and destructive cluster/database deletion operations as maintenance-window tasks.

## Useful Commands

```powershell
# Full stack
docker compose up -d --build
docker compose logs -f backend
docker compose down

# Optional kubectl runner
docker compose --profile ops up -d kubectl-runner

# Backend tests
cd backend
go test ./...

# Frontend build
cd frontend
npm run build
```

## Implementation Status

Implemented:

- Go backend API server
- React admin portal
- PostgreSQL migrations and persistence
- JWT authentication and RBAC
- seeded super admin
- user management
- encrypted secret store helper
- network profiles, reserved IPs, and node network tags
- Proxmox credential management and inventory discovery
- SSH key and SSL certificate catalogs
- topology planner
- Kubernetes cluster provisioning workspace generation
- optional Terraform apply
- Ansible kubeadm bootstrap workspace generation
- optional Ansible execution
- provision-and-bootstrap workflow
- live operation status and event timeline polling
- cluster run history
- cluster node listing and health tracking
- cluster lifecycle operations
- recovery configuration and trigger endpoints
- MetalLB, Traefik, Longhorn, cert-manager, observability, etcd workflows
- kubectl command and node command runners
- application deployments, revisions, and redeploy
- cluster/global Secrets, ConfigMaps, image pull secrets, and saved manifests
- MariaDB/Galera database cluster workflow
- Cloudflare Tunnel workflow
- central monitoring stack and remote cluster workflow
- Docker Compose deployment with frontend, backend, PostgreSQL, and optional kubectl runner
