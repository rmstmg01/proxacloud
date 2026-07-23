# ProxaCloud

ProxaCloud is a private-cloud control plane for Proxmox-based infrastructure. It provides one web portal and API for provisioning and operating VPS instances, LXC containers, Kubernetes clusters, MariaDB/Galera database clusters, networking, firewalls, backups, monitoring, billing records, Cloudflare tunnels, and reusable Kubernetes resource catalogs.

The project is designed for private-cloud operators, small service providers, platform teams, labs, and homelab environments that own Proxmox capacity and want a higher-level self-service and operations layer.

![ProxaCloud dashboard](docs/assets/screenshots/dashboard.jpg)

## What ProxaCloud Does

ProxaCloud connects these layers:

```text
Users and admins
  -> React web portal
  -> Go API server
  -> PostgreSQL state
  -> Proxmox API, Terraform, Ansible, kubectl, Restic, rsync
  -> VPS, containers, Kubernetes clusters, database clusters, backups, monitoring
```

Core capabilities:

- User registration, login, JWT sessions, role-based access, and seeded super-admin bootstrap.
- Proxmox credential management, node discovery, inventory refresh, storage inventory, and node enablement.
- VPC/network profiles, reserved IPs, Proxmox node network tags, bridges, VLANs, gateways, and DNS settings.
- VPS provisioning, deployment, redeployment, power actions, status refresh, console access, and event logs.
- LXC container provisioning, deployment, redeployment, power actions, status refresh, and event logs.
- Kubernetes topology planning for single-node, dual-node, and tri-node Proxmox layouts.
- Kubernetes cluster VM provisioning through generated Terraform workspaces.
- Kubernetes bootstrap through generated Ansible/kubeadm workspaces.
- Cluster run history, live operations, node health reconciliation, recovery settings, and command execution.
- Cluster Maintenance tab for scaling workers, planning Kubernetes upgrades, replacing unhealthy nodes, and rotating kubeconfig tokens.
- Cluster add-ons: MetalLB, Traefik ingress, Longhorn, cert-manager, observability, etcd snapshot, and etcd restore workflows.
- Kubernetes catalogs for Secrets, ConfigMaps, image pull secrets, saved manifests, application deployments, revisions, and apply runs.
- MariaDB/Galera database cluster planning, provisioning, bootstrap, event logs, and Kubernetes manifest generation.
- Firewall policies, firewall rules, attachments, Proxmox apply workflow, and event logs.
- Cloudflare Tunnel records, generated Kubernetes manifests, deployment, deletion, and event logs.
- Central monitoring stack configuration, manifest generation/apply, and remote cluster agent workflow.
- Backup targets and backup jobs for Proxmox storage, Proxmox Backup Server records, S3-style targets, and Hetzner Storage Box with Restic default upload support.
- Billing accounts, prices, credits, checkout hooks, Stripe webhook entrypoint, and PayPal capture entrypoint.
- Built-in operational documents and command references.

## Technology Stack

| Layer | Technology |
| --- | --- |
| Frontend | React 18, TypeScript, Vite, Tailwind CSS, React Router, TanStack Query, Axios, lucide-react, noVNC |
| Backend | Go 1.23, chi router, JWT auth, database/sql, lib/pq |
| Database | PostgreSQL 16 |
| Infrastructure automation | Terraform, bpg/proxmox provider mirror, Ansible, kubeadm-oriented playbooks |
| Kubernetes operations | kubectl, generated manifests, Helm for monitoring stack installs |
| Proxmox integration | Proxmox VE API for credentials, nodes, storage, VM/LXC control, console, backup tasks |
| Backups | Proxmox vzdump, Restic, rsync, sshpass, Hetzner Storage Box/SFTP |
| Deployment | Docker Compose, backend Alpine image, frontend nginx image |

## Repository Layout

```text
.
|-- backend/
|   |-- cmd/api-server/              API entrypoint
|   |-- internal/
|   |   |-- ansible/                 Ansible runner
|   |   |-- auth/                    JWT and auth middleware
|   |   |-- backups/                 Backup targets, jobs, Restic/rsync upload runner
|   |   |-- billing/                 Accounts, prices, checkout, provider webhooks
|   |   |-- cloudflaretunnels/       Cloudflare Tunnel records and manifests
|   |   |-- clusters/                Kubernetes provisioning, operations, health, apps
|   |   |-- config/                  Environment configuration
|   |   |-- containers/              LXC container lifecycle
|   |   |-- databaseclusters/        MariaDB/Galera automation
|   |   |-- db/                      PostgreSQL connection and migrations
|   |   |-- firewalls/               Firewall policies and Proxmox apply workflow
|   |   |-- mailer/                  SMTP email sender
|   |   |-- monitoring/              Central stack and remote cluster agents
|   |   |-- networks/                VPCs, network profiles, reserved IPs, node tags
|   |   |-- proxmox/                 Proxmox API client, credentials, inventory
|   |   |-- rbac/                    Role constants
|   |   |-- secrets/                 Encrypted secret storage
|   |   |-- server/                  Router and service composition
|   |   |-- sshkeys/                 SSH key-pair catalog
|   |   |-- sslcerts/                TLS certificate catalog
|   |   |-- tenant/                  Customer scoping helpers
|   |   |-- terraform/               Terraform runner
|   |   |-- topology/                Placement planner
|   |   |-- users/                   Users, registration, login, seeded admin
|   |   `-- vps/                     VPS lifecycle and noVNC console
|   `-- migrations/                  PostgreSQL schema migrations
|-- frontend/
|   |-- src/api/                     Axios clients
|   |-- src/components/              Layout, sidebar, protected routes, UI primitives
|   |-- src/context/                 Auth context
|   |-- src/pages/                   Application pages
|   |-- Dockerfile                   Production nginx image
|   `-- nginx.conf                   SPA, TLS, and API proxy config
|-- infra/
|   |-- ansible/                     Bootstrap workspace docs
|   |-- k8s/                         Kubernetes deployment and monitoring manifests
|   `-- terraform/                   Provisioning workspace docs
|-- docs/                            API notes and operational docs
|-- docker-compose.yml               Local/container deployment
|-- .env.example                     Example environment file
`-- README.md
```

## Architecture

### Runtime Diagram

```text
Browser
  |
  | HTTPS
  v
frontend container
  - nginx
  - React/Vite static app
  - /api and /health proxy
  |
  | /api/v1/*
  v
backend container
  - Go API
  - JWT/RBAC
  - service modules
  - migration runner
  - Proxmox API client
  - Terraform/Ansible/kubectl helpers
  - Restic/rsync backup runner
  |
  | SQL
  v
postgres container

backend also talks to:
  - Proxmox VE API
  - Kubernetes clusters through kubeconfig/kubectl
  - Terraform and the Proxmox provider
  - Ansible for kubeadm bootstrap
  - Hetzner Storage Box through SFTP for Restic/rsync
  - Stripe/PayPal/SMTP when configured
```

### Backend Design

The backend is intentionally modular. Each domain has its own package with model/service/handler style code, and `backend/internal/server/router.go` wires dependencies together.

Startup flow:

1. Load environment configuration.
2. Open PostgreSQL.
3. Run `.up.sql` migrations when `AUTO_MIGRATE=true`.
4. Seed the super admin user.
5. Initialize services and handlers.
6. Seed the default network profile when no profiles exist.
7. Start background Proxmox inventory refresh every minute.
8. Start background cluster health reconciliation every 30 seconds.
9. Serve `/health` and `/api/v1/*`.

Important backend packages:

| Package | Responsibility |
| --- | --- |
| `server` | Creates services, handlers, router, background workers |
| `config` | Reads environment variables and defaults |
| `users` | Auth, registration, user records, seeded admin, active customer guard |
| `auth` | JWT token service and middleware |
| `rbac` | Role constants and validation |
| `secrets` | AES-GCM encrypted secret storage in PostgreSQL |
| `proxmox` | Proxmox credentials, nodes, storage, VM/LXC actions, console, backup APIs |
| `networks` | VPCs, network profiles, reserved IPs, node tags |
| `topology` | Cluster placement planning across enabled Proxmox nodes |
| `terraform` | Terraform execution helper |
| `ansible` | Ansible execution helper |
| `clusters` | Kubernetes cluster lifecycle, add-ons, apps, health, commands |
| `vps` | VPS lifecycle and console |
| `containers` | LXC lifecycle |
| `databaseclusters` | MariaDB/Galera provisioning and bootstrap |
| `firewalls` | Firewall policies, rules, attachments, Proxmox apply |
| `backups` | Backup targets, vzdump jobs, Restic/rsync uploads |
| `monitoring` | Central monitoring stack and remote agents |
| `cloudflaretunnels` | Tunnel records and generated Kubernetes manifests |
| `billing` | Accounts, prices, credits, Stripe/PayPal integration points |

### Frontend Design

The frontend is a single-page React application. It uses:

- `AuthContext` for login state and current user.
- `ProtectedRoute` for route-level role checks.
- TanStack Query for server state, caching, refetching, and mutations.
- Axios clients under `frontend/src/api`.
- Shared UI primitives under `frontend/src/components/ui`.
- Sidebar navigation grouped by Main, Compute, Kubernetes Resources, Network & Access, Infrastructure, and Operations.

Main routes:

| Route | Purpose |
| --- | --- |
| `/login`, `/register`, `/verify-email` | Public auth and email verification pages |
| `/dashboard` | Platform overview |
| `/vps`, `/vps/new`, `/vps/:id`, `/vps/:id/console` | VPS list, provisioning, detail, console |
| `/containers`, `/containers/new`, `/containers/:id` | LXC list, provisioning, detail |
| `/clusters`, `/clusters/new`, `/clusters/:id` | Kubernetes clusters |
| `/database-clusters` | MariaDB/Galera clusters |
| `/configmaps`, `/secrets`, `/manifests` | Global Kubernetes catalogs |
| `/networks`, `/firewalls`, `/cloudflare-tunnels`, `/ssl-certificates` | Networking and access |
| `/nodes`, `/storage`, `/backups` | Proxmox infrastructure inventory and backup operations |
| `/billing`, `/monitoring`, `/commands`, `/topology`, `/documents`, `/ssh-keys`, `/users` | Operations and administration |

## Roles And Permissions

| Role | Typical use |
| --- | --- |
| `normal_user` | Authenticated customer or read/workflow user. Can access allowed self-service routes. |
| `super_user` | Operator. Can manage most operational workflows, clusters, add-ons, catalogs, backups, monitoring, tunnels, firewalls, and billing operations. |
| `super_admin` | Full administrator. Includes user management, Proxmox credential management, and Proxmox node enablement. |

Default seeded credentials:

```text
Username: k8s-admin
Password: ChangeMeNow!
```

Change these before any non-local deployment.

## Prerequisites

For Docker Compose deployment:

- Linux server, Windows host, or workstation with Docker and Docker Compose.
- Access to at least one Proxmox VE API endpoint.
- Proxmox API user or token with permissions for node discovery, VM/LXC lifecycle, storage listing, console, and backup tasks.
- Network bridge/VLAN information for Proxmox VM/LXC placement.
- A kubeconfig directory if you want backend-driven Kubernetes operations.
- TLS certificates for the frontend nginx container if serving HTTPS.

For local backend development outside Docker:

- Go 1.23 or compatible.
- PostgreSQL 16 or compatible.
- Terraform installed and reachable on `PATH`.
- Ansible installed and reachable on `PATH`.
- `kubectl` installed for cluster operations.
- Optional: `restic`, `rsync`, and `sshpass` for backup upload workflows.

For frontend development:

- Node.js 20 or compatible.
- npm.

## Quick Start With Docker Compose

### 1. Clone And Configure

```powershell
git clone <repo-url>
cd k8s-proxmox-manager
Copy-Item .env.example .env
```

Edit `.env` and change at least:

```text
JWT_SECRET=<long-random-string>
SECRET_ENCRYPTION_KEY=<long-random-string>
SUPER_ADMIN_USERNAME=<your-admin-user>
SUPER_ADMIN_PASSWORD=<strong-password>
PUBLIC_APP_URL=https://your-domain.example
KUBECONFIG_DIR=/root/.kube
HTTP_PORT=80
HTTPS_PORT=443
TLS_CERTS_DIR=./certs
```

Generate strong secrets, for example:

```powershell
openssl rand -base64 32
```

### 2. Add TLS Certificates

The frontend nginx container expects:

```text
/etc/nginx/certs/fullchain.pem
/etc/nginx/certs/privkey.pem
```

With the default Compose mount, place certificates here:

```powershell
mkdir certs
Copy-Item <path-to-fullchain.pem> certs/fullchain.pem
Copy-Item <path-to-privkey.pem> certs/privkey.pem
```

For a local-only test, you can use a self-signed certificate, but browsers will warn until trusted.

### 3. Start The Stack

```powershell
docker compose up -d --build
```

Check service health:

```powershell
docker compose ps
docker compose logs -f backend
```

Default endpoints:

```text
Frontend: https://localhost
Backend health: http://localhost:8080/health
PostgreSQL: localhost:5432
```

The frontend proxies `/api/*` to the backend inside Docker.

### 4. Sign In

Open the frontend and sign in with the seeded admin configured in `.env`.

Immediately create your real admin account or change the seeded credentials.

### 5. Configure Proxmox

In the UI:

1. Go to `Nodes`.
2. Add Proxmox credentials.
3. Discover nodes.
4. Enable nodes that may host workloads.
5. Confirm CPU, memory, disk, storage, and node status.

### 6. Configure Network And Access

In the UI:

1. Go to `VPC Networks`.
2. Review the seeded default network.
3. Add or update CIDR, gateway, DNS, bridge, and VLAN values.
4. Add reserved IPs.
5. Add node network tags when Proxmox nodes use different bridges/VLANs.
6. Add SSH keys under `SSH Keys`.

### 7. Provision Workloads

Common paths:

- `VPS -> New VPS`
- `Container -> New Container`
- `Clusters -> New Cluster`
- `Databases -> New Database Cluster`

By default, `TERRAFORM_AUTO_APPLY=false` and `ANSIBLE_AUTO_APPLY=false`. In that mode, ProxaCloud creates records and workspaces/plans, but does not run all destructive or infrastructure-changing commands automatically unless the workflow explicitly supports and is allowed to apply.

## Environment Variables

Copy `.env.example` to `.env`.

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `dev` | Application environment label |
| `APP_ADDR` | `:8080` | Backend listen address |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/k8s_proxmox?sslmode=disable` | PostgreSQL connection string |
| `AUTO_MIGRATE` | `true` | Run backend migrations on startup |
| `TERRAFORM_BINARY` | `terraform` | Terraform executable |
| `TERRAFORM_WORKDIR` | `infra/terraform/runs` | Local Terraform workspace root |
| `TERRAFORM_PLUGIN_DIR` | `/terraform-providers` | Terraform provider mirror path used by the backend runner |
| `TERRAFORM_AUTO_APPLY` | `false` | Allow backend-triggered Terraform apply when workflows request it |
| `ANSIBLE_BINARY` | `ansible-playbook` | Ansible executable |
| `ANSIBLE_WORKDIR` | `infra/ansible/runs` | Local Ansible workspace root |
| `ANSIBLE_AUTO_APPLY` | `false` | Allow backend-triggered Ansible execution when workflows request it |
| `OPERATIONS_WORKDIR` | `data/operations/runs` | Generated Kubernetes manifests, scripts, and operation files |
| `KUBECONFIG_DIR` | `/root/.kube` | Host kubeconfig directory mounted into backend |
| `JWT_SECRET` | `change-me-in-prod` | JWT signing secret |
| `SECRET_ENCRYPTION_KEY` | `dev-only-change-me-secret-key` | Encryption material for stored secrets |
| `JWT_TTL_MINUTES` | `120` | Login token lifetime |
| `PUBLIC_APP_URL` | `http://localhost:3000` | Public URL used for email links and checkout redirects |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | empty/587 | Email delivery settings |
| `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` | empty | Stripe checkout/webhook settings |
| `PAYPAL_CLIENT_ID`, `PAYPAL_CLIENT_SECRET`, `PAYPAL_ENVIRONMENT` | empty/`sandbox` | PayPal settings |
| `SUPER_ADMIN_USERNAME` | `k8s-admin` | Seeded admin username |
| `SUPER_ADMIN_PASSWORD` | `ChangeMeNow!` | Seeded admin password |
| `DEFAULT_NETWORK_NAME` | `homelab-default` | Seeded network profile name |
| `DEFAULT_NETWORK_CIDR` | `192.168.8.0/24` | Seeded network CIDR |
| `DEFAULT_NETWORK_GATEWAY` | `192.168.8.1` | Seeded gateway |
| `DEFAULT_NETWORK_DNS` | `1.1.1.1,8.8.8.8` | Seeded DNS list |
| `DEFAULT_NETWORK_BRIDGE` | `vmbr0` | Default Proxmox bridge |
| `DEFAULT_NETWORK_VLAN` | `0` | Default VLAN tag |
| `HTTP_PORT` | `80` | Host HTTP port for frontend nginx |
| `HTTPS_PORT` | `443` | Host HTTPS port for frontend nginx |
| `TLS_CERTS_DIR` | `./certs` | Host certificate directory mounted into frontend nginx |

Compose overrides several backend paths:

```text
TERRAFORM_WORKDIR=/app/backend/data/terraform/runs
ANSIBLE_WORKDIR=/app/backend/data/ansible/runs
OPERATIONS_WORKDIR=/app/backend/data/operations/runs
KUBECONFIG=/app/backend/.kube/config
```

## Docker Compose Services

| Service | Description |
| --- | --- |
| `postgres` | PostgreSQL 16 database with persistent `postgres_data` volume |
| `backend` | Go API server, migrations, runners, Proxmox client, backup tools |
| `frontend` | nginx serving React build, TLS, SPA fallback, API proxy |
| `kubectl-runner` | Optional `ops` profile container with kubectl and mounted kubeconfig |

Useful commands:

```powershell
docker compose up -d --build
docker compose logs -f backend
docker compose logs -f frontend
docker compose ps
docker compose down
```

Start the optional kubectl helper:

```powershell
docker compose --profile ops up -d kubectl-runner
```

## Local Development

### Backend

Start PostgreSQL first, then:

```powershell
cd backend
go mod download
go run ./cmd/api-server
```

Run tests:

```powershell
cd backend
go test ./...
```

### Frontend

```powershell
cd frontend
npm install
npm run dev
```

Build:

```powershell
cd frontend
npm run build
```

The frontend dev server expects API requests to reach the configured backend. In production, nginx proxies `/api` and `/health` to the backend service.

## Main Workflows

### First Platform Setup

1. Deploy the stack.
2. Log in as the seeded super admin.
3. Add Proxmox credentials under `Nodes`.
4. Discover and enable Proxmox nodes.
5. Configure VPC/network profiles.
6. Add reserved IPs and node network tags.
7. Add SSH keys.
8. Review storage inventory.
9. Configure backup targets if needed.
10. Provision a VPS, container, Kubernetes cluster, or database cluster.

### VPS Workflow

VPS provisioning uses Terraform and the Proxmox provider. A VPS record stores Proxmox credential, node, network profile, cloud-init image, SSH key, CPU, memory, disk, and status.

Typical flow:

1. Discover or create cloud-init image records.
2. Create a VPS from `VPS -> New VPS`.
3. Review generated details.
4. Deploy with Terraform when permitted.
5. Use start/restart/shutdown/status refresh actions.
6. Open console through noVNC when needed.
7. Review VPS events for lifecycle history.

### Container Workflow

Container provisioning is similar to VPS provisioning but targets Proxmox LXC. The UI tracks template path, network profile, resources, nesting/unprivileged options, deployment status, and events.

Typical flow:

1. Review or create container templates.
2. Create a container.
3. Deploy or redeploy through Terraform.
4. Use power/status actions.
5. Review events.

### Kubernetes Cluster Workflow

Cluster provisioning creates records, node plans, VM names/IPs, Terraform workspaces, run records, and live operation timeline entries.

Recommended flow:

1. Prepare Proxmox credentials, enabled nodes, network profile, reserved IPs, and SSH keys.
2. Use `Topology` to check placement feasibility.
3. Create a cluster from `Clusters -> New Cluster`.
4. Choose topology:
   - `single`: one node, simplest, not highly available.
   - `dual`: two nodes, better spread, weaker quorum characteristics.
   - `tri`: three nodes, preferred for HA control-plane placement.
5. Choose Kubernetes version, node counts, resource sizes, network settings, and optional auto-bootstrap.
6. Review run history and live operation timeline.
7. Bootstrap Kubernetes if not auto-bootstrapped.
8. Add MetalLB, ingress, Longhorn, cert-manager, observability, apps, manifests, and backups as needed.

### Bootstrap Workflow

Bootstrap uses generated Ansible workspaces for kubeadm-oriented cluster setup.

Inputs include:

- SSH user.
- SSH private key, stored key pair, or existing bootstrap key.
- kube-vip control-plane VIP.
- pod CIDR.
- service CIDR.
- CNI choice.
- node IP mapping.
- optional apply flag.

When bootstrap succeeds, the backend can export `admin.conf` into the mounted kubeconfig directory:

```text
/app/backend/.kube/config
```

With the default Compose mount, that maps to:

```text
${KUBECONFIG_DIR:-/root/.kube}/config
```

### Cluster Detail Tabs

Admin cluster detail tabs include:

- `Overview`: cluster metadata.
- `Run History`: provisioning/bootstrap runs and live operation timeline.
- `Bootstrap`: kubeadm/Ansible bootstrap settings.
- `MetalLB`: load-balancer installation/status.
- `Storage`: Longhorn installation.
- `Secrets`: cluster-scoped Kubernetes Secret records.
- `ConfigMaps`: cluster-scoped ConfigMaps.
- `Pull Secrets`: registry pull secrets.
- `Manifests`: saved manifests and apply runs.
- `Applications`: application deployment and revisions.
- `Observability`: per-cluster observability deploy/status.
- `etcd Backup`: snapshot and restore workflow.
- `Cluster Maintenance`: scale workers, upgrade Kubernetes, replace unhealthy node, rotate kubeconfig token.

### Cluster Maintenance

Cluster Maintenance is intentionally the final cluster tab after `etcd Backup`.

It groups lifecycle tasks that operators usually run with care:

- Scale workers.
- Plan or record Kubernetes upgrades.
- Replace unhealthy nodes.
- Rotate kubeconfig tokens.

Use maintenance windows for high-impact operations.

### Add-On Workflows

Supported cluster add-ons:

- MetalLB for LoadBalancer IP allocation.
- Traefik ingress controller.
- Longhorn storage.
- cert-manager and Let's Encrypt issuer generation.
- Observability agent/resources.
- Application manifests and supporting resources.

Some workflows apply directly through the configured kubeconfig when enabled. Others generate manifests, scripts, plans, or command output for review.

### Kubernetes Catalogs And Applications

ProxaCloud supports reusable Kubernetes resource catalogs:

- Native encrypted Secrets.
- Provider-style Secret references for Vault, External Secrets Operator, AWS Secrets Manager, Azure Key Vault, Google Secret Manager, and Bitnami Sealed Secrets.
- ConfigMaps.
- Image pull secrets.
- Saved YAML manifests.
- Global/unattached catalog items.
- Cluster-scoped catalog items.
- Application deployments, revisions, redeploys, and apply logs.

### Database Cluster Workflow

The database-cluster module targets MariaDB/Galera on Proxmox VMs with Kubernetes integration manifests.

Typical flow:

1. Create a database cluster plan.
2. Review topology, node placement, network settings, and credentials.
3. Provision database VMs through generated Terraform.
4. Optionally run Terraform apply.
5. Bootstrap MariaDB/Galera through generated Ansible.
6. Review events and run history.
7. Generate Kubernetes manifests for applications that consume the database service.

More detail is available in:

```text
docs/mariadb-galera-cluster.md
```

### Firewall Workflow

Firewalls let operators define reusable policy records, rules, and attachments.

Typical flow:

1. Create a firewall.
2. Add ingress/egress rules.
3. Attach the firewall to a supported resource.
4. Apply rules to Proxmox.
5. Review firewall events and output.

### Backup Workflow

Backup targets are configured under `Backups`.

Supported target types:

- Proxmox storage.
- Proxmox Backup Server record.
- S3-compatible target record.
- Hetzner Storage Box.

For VPS and containers, `Backup Now` starts a Proxmox `vzdump` task. For Hetzner Storage Box targets, ProxaCloud uses Proxmox staging storage first and then uploads the completed artifact with Restic by default.

Customers can also enable paid backup protection from the VPS details page:

1. Choose a daily, weekly, or monthly plan.
2. Select the local backup time and, for weekly or monthly plans, the schedule day.
3. Confirm the detected IANA timezone.
4. Use `Backup now` at any time while protection is enabled.
5. Review the latest Proxmox task status from the same VPS page.

Backup protection is included in the existing monthly resource invoice. Default plan prices and retention are:

- Monthly: `$2.00 + $0.01/GB`, retaining the latest 3 backups.
- Weekly: `$5.00 + $0.02/GB`, retaining the latest 4 backups.
- Daily: `$10.00 + $0.04/GB`, retaining the latest 7 backups.

Administrators can change or disable the three `vps_backup_*` prices from Billing. Before customers can enable protection, configure at least one enabled `proxmox_storage` target under `Backups`; the oldest enabled target is used. The API worker checks due schedules and running Proxmox tasks every 30 seconds. Only one active backup job is allowed per VPS.

Restic flow:

```text
VPS/container
  -> Proxmox vzdump
  -> staging storage
  -> backend downloads artifact through Proxmox API
  -> restic backup to Hetzner Storage Box SFTP repository
```

Hetzner Restic target fields:

```text
Host: storagebox.example.com
Port: 23
Username: storage-user
Base path: /backups/proxacloud
Auth method: password or ssh key
Backup mode: restic
Password: Storage Box password when using password auth
SSH private key: when using ssh key auth
Restic password: encryption password for the Restic repository
```

Important backup notes:

- Keep the Restic password safe. Without it, restores are impossible.
- The backend Docker image includes `restic`, `rsync`, and `sshpass`.
- Existing Proxmox backups are not automatically uploaded unless a job is started through ProxaCloud or a future upload/retry action is added.
- Rsync is available as a direct file-sync option for Hetzner targets, but Restic is the recommended default because it provides encryption and deduplication.

### Monitoring Workflow

The monitoring module manages:

- Central stack configuration.
- Stack manifest generation.
- Stack apply through Helm/kubectl.
- Remote cluster registration.
- Remote cluster agent manifest generation.
- Remote cluster sync and deletion.

Additional monitoring assets live in:

```text
infra/k8s/monitoring/
```

### Cloudflare Tunnel Workflow

Cloudflare Tunnel records generate Kubernetes resources for tunnel-backed ingress.

Typical flow:

1. Create a tunnel record.
2. Review generated manifest.
3. Deploy tunnel resources to the selected cluster.
4. Review events.
5. Delete tunnel records when no longer needed.

### Billing Workflow

Billing tracks:

- Customer accounts.
- Resource prices.
- Credits.
- Monthly charge action.
- Checkout creation.
- Stripe webhook endpoint.
- PayPal capture endpoint.

The billing integration points are present, but production payment use requires real provider credentials, webhook configuration, and careful validation of your pricing/business rules.

## Generated Artifacts And Volumes

| Artifact | Local default | Compose path |
| --- | --- | --- |
| Terraform workspaces | `infra/terraform/runs` | `/app/backend/data/terraform/runs` |
| Ansible workspaces | `infra/ansible/runs` | `/app/backend/data/ansible/runs` |
| Kubernetes operation files | `data/operations/runs` | `/app/backend/data/operations/runs` |
| Backend kubeconfig | local `KUBECONFIG`/host path | `/app/backend/.kube/config` |
| Backup runner files | `backups/runs` | `/app/backend/backups/runs` |
| PostgreSQL data | Docker volume | `postgres_data` |
| Backend generated data | Docker volume | `backend_data` |

Generated workspaces should be reviewed before manual use, especially when auto-apply is disabled.

## Database And Migrations

Migrations are plain `.up.sql` files under:

```text
backend/migrations/
```

The migration runner:

- Reads files from `backend/migrations`.
- Sorts by filename.
- Executes every `.up.sql` file at startup when `AUTO_MIGRATE=true`.
- Relies on idempotent SQL such as `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`.

Migration coverage includes:

- users and roles
- network profiles and VPC mapping
- Proxmox credentials, nodes, inventory, storage
- cluster provisioning, runs, operations, node health
- bootstrap config and bootstrap SSH keys
- VPS and container resources
- firewalls
- backup targets and jobs
- billing
- customer registration and ownership
- monitoring
- Cloudflare tunnels
- database clusters
- Kubernetes resource catalogs

## API Overview

Base API path:

```text
/api/v1
```

Public endpoints:

- `GET /health`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/register`
- `GET|POST /api/v1/auth/verify-email`
- `POST /api/v1/billing/webhooks/stripe`

Authenticated groups:

| Group | Examples |
| --- | --- |
| Auth | `GET /auth/me` |
| Users | `GET /users`, `POST /users`, status updates |
| Billing | summary, accounts, credits, prices, checkout, PayPal capture |
| Networks | VPCs, network profiles, reserved IPs, node tags |
| Proxmox | credentials, discover nodes, list nodes, enable nodes, storage |
| Backups | targets, summary, jobs |
| SSH keys | list, create, delete |
| SSL certificates | list, create, delete |
| Topology | `POST /topology/plan` |
| VPS | list, create, deploy, redeploy, power actions, console, events |
| Containers | list, templates, create, deploy, redeploy, power actions, events |
| Firewalls | policies, rules, attachments, apply, events |
| Kubernetes clusters | list, get, provision, provision-and-bootstrap, runs, operations |
| Cluster lifecycle | start, restart, shutdown, redeploy, bootstrap, scale, upgrade, replace node, rotate kubeconfig |
| Cluster add-ons | MetalLB, ingress controller, Longhorn, cert-manager, observability, etcd |
| Cluster resources | Secrets, ConfigMaps, image pull secrets, saved manifests |
| Applications | app deploy, revisions, redeploy |
| Commands | kubectl commands and node commands |
| Health/recovery | cluster health, recovery config, recovery trigger |
| Database clusters | plan, provision, apply Terraform, bootstrap, manifests, events |
| Cloudflare tunnels | records, manifests, deploy, events |
| Monitoring | stack config, manifest/apply, remote clusters |

Exact role restrictions live in:

```text
backend/internal/server/router.go
```

## Production Checklist

Before exposing ProxaCloud beyond local development:

1. Change `JWT_SECRET`.
2. Change `SECRET_ENCRYPTION_KEY`.
3. Change `SUPER_ADMIN_USERNAME` and `SUPER_ADMIN_PASSWORD`.
4. Use a real PostgreSQL password and restrict database exposure.
5. Provide trusted TLS certificates.
6. Set `PUBLIC_APP_URL` to the real HTTPS URL.
7. Configure SMTP if users will register and verify email.
8. Use least-privilege Proxmox API tokens where possible.
9. Protect the mounted kubeconfig directory.
10. Keep `TERRAFORM_AUTO_APPLY=false` and `ANSIBLE_AUTO_APPLY=false` until workflows are tested.
11. Back up PostgreSQL and your secret encryption key securely.
12. Store Restic passwords outside the repository and in an operational password vault.
13. Review destructive workflows: delete, restore, node replacement, firewall apply, and redeploy.

## Security Notes

- `SECRET_ENCRYPTION_KEY` protects encrypted secret records in PostgreSQL. Losing it can make stored secret values unrecoverable.
- Do not commit `.env`, private keys, kubeconfigs, TLS private keys, Restic passwords, Proxmox tokens, or provider credentials.
- Proxmox credentials, SSH private keys, TLS private keys, bootstrap keys, native Kubernetes Secret values, and backup target secrets should be stored through the encrypted backend secret store.
- The backend kubeconfig mount can grant administrative access to managed clusters.
- etcd restore, node replacement, firewall apply, and destructive delete operations should be treated as maintenance-window tasks.
- Payment provider keys and webhook secrets must be protected like production credentials.

## Troubleshooting

### Frontend Does Not Load

Check:

```powershell
docker compose logs -f frontend
docker compose ps
```

Common causes:

- Missing `certs/fullchain.pem` or `certs/privkey.pem`.
- Host ports 80/443 already in use.
- Browser rejecting self-signed certificates.

### Backend Is Not Healthy

Check:

```powershell
docker compose logs -f backend
curl http://localhost:8080/health
```

Common causes:

- PostgreSQL not healthy.
- Migration error.
- Invalid database URL.
- Missing or invalid environment values.

### Login Fails

Check:

- `SUPER_ADMIN_USERNAME` and `SUPER_ADMIN_PASSWORD` values used at first startup.
- Backend logs for seed/login errors.
- Browser network tab for `/api/v1/auth/login`.

If you changed seeded credentials after the first startup, remember the existing user record already exists in PostgreSQL.

### Proxmox Discovery Fails

Check:

- Proxmox endpoint URL.
- API user/token permissions.
- TLS/insecure TLS setting.
- Network connectivity from backend container to Proxmox.
- Proxmox API port, usually `8006`.

### Terraform Apply Fails

Check:

- Proxmox provider mirror exists in the backend image.
- Proxmox credentials have VM permissions.
- Network bridge/VLAN exists on the selected Proxmox node.
- Storage names match Proxmox.
- `TERRAFORM_AUTO_APPLY` permits apply when the workflow requests it.

### Ansible Bootstrap Fails

Check:

- VM IP addresses are reachable from the backend container.
- SSH user is correct.
- SSH private key matches the VM authorized key.
- Security groups/firewalls allow SSH.
- `ANSIBLE_AUTO_APPLY` permits execution when the workflow requests it.

### Kubernetes Operations Fail

Check:

- `/app/backend/.kube/config` exists inside the backend container.
- Host `KUBECONFIG_DIR` is mounted correctly.
- kubeconfig user/token is valid.
- Cluster API endpoint is reachable from the backend container.

### Hetzner Restic Upload Fails

Check:

- Backend was rebuilt after Restic support was added.
- Storage Box SSH/SFTP is enabled.
- Host is the Storage Box SFTP hostname, for example `storagebox.example.com`.
- Port is usually `23`.
- Username/password or SSH key is valid.
- Restic password is set for Restic mode.
- Staging Proxmox storage contains backup content.
- Backend logs include Restic/sshpass output in the backup job upload fields.

## Useful Commands

```powershell
# Start everything
docker compose up -d --build

# Follow backend logs
docker compose logs -f backend

# Follow frontend logs
docker compose logs -f frontend

# Rebuild only backend
docker compose up -d --build backend

# Rebuild only frontend
docker compose up -d --build frontend

# Stop stack
docker compose down

# Start optional kubectl helper
docker compose --profile ops up -d kubectl-runner

# Backend tests
cd backend
go test ./...

# Frontend build
cd frontend
npm run build
```

## Related Documentation

- `docs/implementation-steps.md`: milestone summary.
- `docs/api-step*.md`: historical API notes by implementation step.
- `docs/mariadb-galera-cluster.md`: database cluster workflow details.
- `infra/terraform/README.md`: Terraform workspace notes.
- `infra/ansible/README.md`: Ansible bootstrap workspace notes.
- `infra/k8s/monitoring/README.md`: monitoring stack notes.

## Implementation Status

Implemented:

- Go backend API server.
- React admin portal.
- PostgreSQL migrations and persistence.
- JWT authentication and RBAC.
- Seeded super admin.
- Registration and email verification hooks.
- User management.
- Encrypted secret store.
- Network profiles, VPCs, reserved IPs, and node tags.
- Proxmox credential management, node discovery, storage inventory, and VM/LXC operations.
- VPS lifecycle and console.
- Container lifecycle.
- SSH key and SSL certificate catalogs.
- Topology planner.
- Kubernetes cluster provisioning workspace generation.
- Optional Terraform apply.
- Ansible/kubeadm bootstrap workspace generation.
- Optional Ansible execution.
- Provision-and-bootstrap workflow.
- Live operation status and event timeline.
- Cluster run history.
- Cluster node listing, health tracking, and recovery hooks.
- Cluster lifecycle and Cluster Maintenance workflows.
- MetalLB, Traefik, Longhorn, cert-manager, observability, and etcd workflows.
- kubectl command and node command runners.
- Application deployments, revisions, and redeploy.
- Cluster/global Secrets, ConfigMaps, image pull secrets, and saved manifests.
- MariaDB/Galera database cluster workflow.
- Firewall workflow.
- Cloudflare Tunnel workflow.
- Central monitoring stack and remote cluster workflow.
- Backup targets and jobs with Hetzner Restic default upload runner.
- Billing account, price, credit, checkout, Stripe webhook, and PayPal capture surfaces.
- Docker Compose deployment with frontend, backend, PostgreSQL, and optional kubectl runner.
