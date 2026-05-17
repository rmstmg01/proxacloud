# K8s Proxmox Manager

K8s Proxmox Manager is a self-hosted admin portal and API for provisioning and operating Kubernetes clusters on Proxmox. It targets homelab and private-cloud environments where Kubernetes nodes are created as Ubuntu 24 cloud-init VMs, then bootstrapped with kubeadm.

The application combines a React admin UI, a Go backend API, PostgreSQL persistence, Proxmox node discovery, Terraform VM provisioning, Ansible Kubernetes bootstrap, and day-2 cluster operations.

## What It Does

- Stores users, roles, network profiles, SSH keys, Proxmox credentials, discovered Proxmox nodes, clusters, operation history, and health data in PostgreSQL.
- Discovers Proxmox nodes from configured Proxmox credentials and tracks CPU, memory, disk, status, and placement eligibility.
- Plans cluster placement across single-node, dual-node, or tri-node Proxmox topologies.
- Generates Terraform workspaces for Ubuntu cloud-init VMs on Proxmox.
- Optionally runs Terraform apply to create VMs.
- Generates Ansible workspaces and kubeadm bootstrap playbooks.
- Optionally runs Ansible to bootstrap Kubernetes control-plane and worker nodes.
- Supports cluster lifecycle APIs for listing, detail views, run history, scaling, upgrade planning, node replacement, and kubeconfig token rotation.
- Generates MetalLB deployment manifests and helper commands.
- Generates observability configuration for Prometheus/Grafana-style deployments.
- Tracks etcd snapshot and restore plans.
- Runs background Proxmox refresh and cluster node health/recovery checks.
- Provides a web UI for common admin workflows.

## Project Structure

```text
.
|-- backend/                 Go API server
|   |-- cmd/api-server/      Backend entrypoint
|   |-- internal/            Feature packages
|   `-- migrations/          PostgreSQL migrations
|-- frontend/                React/Vite admin portal
|-- infra/
|   |-- ansible/             Ansible-related assets
|   `-- terraform/           Terraform-related assets
|-- certs/                   TLS certificates for the frontend nginx container
|-- docs/                    Step/API implementation notes
|-- scripts/                 Utility scripts
|-- docker-compose.yml       Local/container stack
`-- .env.example             Example runtime configuration
```

## Architecture

### Frontend

The frontend is a React 18 application built with Vite, TypeScript, Tailwind CSS, React Router, TanStack Query, Axios, and lucide-react icons.

Main UI routes:

- `/login` - sign in
- `/dashboard` - overview
- `/clusters` - cluster list
- `/clusters/new` - provision a new cluster
- `/clusters/:id` - cluster detail and operations
- `/networks` - network profiles, reserved IPs, node network tags
- `/proxmox` - Proxmox credentials and discovered nodes
- `/topology` - topology planning
- `/users` - user management
- `/ssh-keys` - SSH key management

### Backend

The backend is a Go API using:

- `go-chi/chi` for routing
- JWT authentication
- Role-based access control
- PostgreSQL via `database/sql` and `lib/pq`
- A startup migration runner
- Background Proxmox discovery refresh
- Background cluster health reconciliation

The backend entrypoint is:

```text
backend/cmd/api-server/main.go
```

### Database

PostgreSQL stores application state. Migrations live in:

```text
backend/migrations/
```

Migration coverage includes users/roles, network profiles, Proxmox inventory, cluster provisioning, day-2 operations, observability/backup data, bootstrap config, SSH key pairs, cluster operations, reserved IPs, node tags, Proxmox node resource usage, and cluster node health.

## User Roles

The app has three roles:

- `normal_user` - authenticated access to permitted read/workflow views
- `super_user` - operational user for cluster, network, topology, SSH key, and most cluster actions
- `super_admin` - full admin access, including user creation and Proxmox credential management

The backend seeds a super admin on startup if one does not already exist.

Default development credentials:

```text
Username: k8s-admin
Password: ChangeMeNow!
```

Change these values before using the app outside local development.

## Core Workflow

1. Start the stack.
2. Log in as the seeded super admin.
3. Add Proxmox credentials.
4. Discover Proxmox nodes.
5. Enable or disable Proxmox nodes for placement.
6. Configure network profiles, reserved IPs, node network tags, and SSH keys.
7. Use topology planning to select single, dual, or tri-node placement.
8. Provision VMs with Terraform workspace generation.
9. Optionally apply Terraform to create the VMs.
10. Bootstrap Kubernetes with generated Ansible/kubeadm workspaces.
11. Manage the cluster through day-2 operations.

## Topology Planning

Supported topology types:

- `single` - requires at least one enabled Proxmox node; not highly available
- `dual` - requires at least two enabled Proxmox nodes; weaker quorum than tri-node
- `tri` - requires at least three enabled Proxmox nodes; preferred HA layout

The planner sorts enabled Proxmox nodes by available memory and distributes control-plane and worker VM placements across the selected nodes. Nodes with less than 4 GB free memory are limited to one VM placement and generate warnings.

## Cluster Provisioning

Provisioning accepts cluster settings such as:

- Cluster name
- Topology type
- Control-plane count
- Worker count
- Kubernetes version
- Ubuntu image
- Network profile
- Optional per-Proxmox-node network profile overrides
- Optional per-Proxmox-node IP pool starts
- SSH public key or stored SSH key pair
- Control-plane and worker CPU/RAM/disk settings
- Proxmox credential or per-node credential overrides
- `apply` flag

The backend creates a cluster record, allocates VM names/IPs, writes Terraform workspace files, stores run history, and either returns the generated workspace or runs Terraform depending on request/config.

Terraform apply is disabled by default in Docker Compose:

```text
TERRAFORM_AUTO_APPLY=false
```

## Kubernetes Bootstrap

Bootstrap accepts settings such as:

- SSH user
- SSH private key, stored key pair, or existing bootstrap SSH key
- Optional kube-vip control-plane VIP
- Pod CIDR
- Service CIDR
- CNI choice
- Node IP mapping
- `apply` flag

The backend writes Ansible inventory/playbook material for kubeadm bootstrap and either returns the generated workspace or runs `ansible-playbook` depending on request/config.

Ansible apply is disabled by default in Docker Compose:

```text
ANSIBLE_AUTO_APPLY=false
```

## Day-2 Operations

Implemented cluster operation areas include:

- Cluster list/detail
- Provision run history
- Latest operation status
- Operation event polling
- Cluster node listing
- Worker scaling
- Kubernetes version upgrade planning
- Unhealthy node replacement
- Kubeconfig token rotation
- MetalLB manifest generation
- Observability values/manifest generation
- etcd snapshot tracking
- etcd restore tracking
- Cluster health status
- Recovery configuration
- Manual recovery trigger

Some operation endpoints generate plans, manifests, or helper commands rather than directly applying changes. Apply behavior is controlled by endpoint request flags and runtime configuration.

## API Overview

Base API path:

```text
/api/v1
```

Public endpoints:

- `GET /health`
- `POST /api/v1/auth/login`

Authenticated endpoints include:

- `GET /api/v1/auth/me`
- `GET /api/v1/users`
- `POST /api/v1/users`
- `GET /api/v1/networks`
- `POST /api/v1/networks`
- `PUT /api/v1/networks/{id}`
- `POST /api/v1/networks/{id}/set-default`
- `GET /api/v1/networks/{id}/reserved-ips`
- `POST /api/v1/networks/{id}/reserved-ips`
- `DELETE /api/v1/networks/{id}/reserved-ips/{reservedId}`
- `GET /api/v1/networks/node-tags`
- `PUT /api/v1/networks/node-tags/{nodeName}`
- `DELETE /api/v1/networks/node-tags/{nodeName}`
- `POST /api/v1/proxmox/credentials`
- `GET /api/v1/proxmox/credentials`
- `PUT /api/v1/proxmox/credentials/{id}`
- `DELETE /api/v1/proxmox/credentials/{id}`
- `POST /api/v1/proxmox/credentials/{id}/discover-nodes`
- `GET /api/v1/proxmox/nodes`
- `PATCH /api/v1/proxmox/nodes/{id}`
- `GET /api/v1/ssh-keys`
- `POST /api/v1/ssh-keys`
- `DELETE /api/v1/ssh-keys/{id}`
- `POST /api/v1/topology/plan`
- `GET /api/v1/clusters`
- `GET /api/v1/clusters/check-name`
- `POST /api/v1/clusters/provision`
- `POST /api/v1/clusters/provision-and-bootstrap`
- `GET /api/v1/clusters/{id}`
- `DELETE /api/v1/clusters/{id}`
- `GET /api/v1/clusters/{id}/runs`
- `GET /api/v1/clusters/{id}/operations/latest`
- `GET /api/v1/clusters/{id}/operations/events`
- `GET /api/v1/clusters/{id}/nodes`
- `GET /api/v1/clusters/{id}/bootstrap-config`
- `GET /api/v1/clusters/{id}/bootstrap-keys`
- `POST /api/v1/clusters/{id}/start`
- `POST /api/v1/clusters/{id}/restart`
- `POST /api/v1/clusters/{id}/shutdown`
- `POST /api/v1/clusters/{id}/bootstrap`
- `POST /api/v1/clusters/{id}/scale`
- `POST /api/v1/clusters/{id}/upgrade`
- `POST /api/v1/clusters/{id}/replace-node`
- `POST /api/v1/clusters/{id}/rotate-kubeconfig`
- `POST /api/v1/clusters/{id}/metallb/deploy`
- `GET /api/v1/clusters/{id}/metallb`
- `POST /api/v1/clusters/{id}/observability/deploy`
- `GET /api/v1/clusters/{id}/observability`
- `POST /api/v1/clusters/{id}/etcd/snapshot`
- `GET /api/v1/clusters/{id}/etcd/snapshots`
- `POST /api/v1/clusters/{id}/etcd/restore`
- `GET /api/v1/clusters/{id}/health`
- `POST /api/v1/clusters/{id}/recovery/config`
- `POST /api/v1/clusters/{id}/recovery/trigger`

Role restrictions are enforced in the backend router. Proxmox credential management is super-admin only. Most cluster, network, topology, and SSH key operations require `super_user` or `super_admin`.

## Configuration

Copy `.env.example` to `.env` and adjust values.

Important variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | Backend listen address |
| `DATABASE_URL` | local PostgreSQL URL | Backend PostgreSQL connection |
| `AUTO_MIGRATE` | `true` | Run migrations on backend startup |
| `JWT_SECRET` | `change-me-in-prod` | JWT signing secret |
| `JWT_TTL_MINUTES` | `120` | JWT lifetime |
| `SUPER_ADMIN_USERNAME` | `k8s-admin` | Seeded admin username |
| `SUPER_ADMIN_PASSWORD` | `ChangeMeNow!` | Seeded admin password |
| `DEFAULT_NETWORK_NAME` | `homelab-default` | Seeded network profile name |
| `DEFAULT_NETWORK_CIDR` | `192.168.8.0/24` | Seeded network CIDR |
| `DEFAULT_NETWORK_GATEWAY` | `192.168.8.1` | Seeded network gateway |
| `DEFAULT_NETWORK_DNS` | `1.1.1.1,8.8.8.8` | Seeded DNS servers |
| `DEFAULT_NETWORK_BRIDGE` | `vmbr0` | Proxmox bridge |
| `DEFAULT_NETWORK_VLAN` | `0` | Proxmox VLAN tag |
| `TERRAFORM_BINARY` | `terraform` | Terraform executable |
| `TERRAFORM_WORKDIR` | `infra/terraform/runs` | Local Terraform run directory |
| `TERRAFORM_PLUGIN_DIR` | `/terraform-providers` | Terraform plugin directory |
| `TERRAFORM_AUTO_APPLY` | `false` | Allow backend-triggered Terraform apply |
| `ANSIBLE_BINARY` | `ansible-playbook` | Ansible executable |
| `ANSIBLE_WORKDIR` | `infra/ansible/runs` | Local Ansible run directory |
| `ANSIBLE_AUTO_APPLY` | `false` | Allow backend-triggered Ansible execution |
| `OPERATIONS_WORKDIR` | `data/operations/runs` | Generated operation manifests/scripts |
| `HTTP_PORT` | `80` | Frontend HTTP port in Docker Compose |
| `HTTPS_PORT` | `443` | Frontend HTTPS port in Docker Compose |
| `TLS_CERTS_DIR` | `./certs` | Host TLS cert directory for frontend nginx |

## Run With Docker Compose

Build and start the full stack:

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
- Frontend portal: `https://<your-domain>` or `https://localhost` depending on TLS/browser setup

The Compose stack includes:

- `postgres` - PostgreSQL 16
- `backend` - Go API server
- `frontend` - nginx serving the built React app with HTTPS support
- `kubectl-runner` - optional ops profile container for running kubectl commands against generated manifests

## Run Backend Locally

1. Start PostgreSQL and make sure `DATABASE_URL` points to it.
2. Copy `.env.example` to `.env` and update values.
3. Start the API:

```powershell
cd backend
go mod tidy
go run ./cmd/api-server
```

Default backend address:

```text
http://localhost:8080
```

## Run Frontend Locally

Install dependencies and start Vite:

```powershell
cd frontend
npm install
npm run dev
```

Build production assets:

```powershell
cd frontend
npm run build
```

## HTTPS Certificates

The frontend container expects existing certificate files mounted at:

```text
/etc/nginx/certs/fullchain.pem
/etc/nginx/certs/privkey.pem
```

To use existing Let's Encrypt certificates, place them in the local `certs/` directory:

```powershell
mkdir certs -Force
Copy-Item <path-to-fullchain.pem> certs/fullchain.pem
Copy-Item <path-to-privkey.pem> certs/privkey.pem
docker compose up -d --build frontend
```

You can change host ports or certificate location with:

```text
HTTP_PORT=80
HTTPS_PORT=443
TLS_CERTS_DIR=./certs
```

## Applying MetalLB From Docker

After preparing MetalLB through the API/UI, use the generated `manifest_dir` with the optional `kubectl-runner` service.

PowerShell example:

```powershell
docker compose --profile ops up -d kubectl-runner
docker compose run --rm -e KUBECONFIG=/kube/config -v "$env:USERPROFILE/.kube/config:/kube/config:ro" kubectl-runner "cd /app/backend/data/operations/runs/<cluster>/metallb && ./apply-metallb.sh"
```

Bash example:

```bash
docker compose --profile ops up -d kubectl-runner
docker compose run --rm -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" kubectl-runner "cd /app/backend/data/operations/runs/<cluster>/metallb && ./apply-metallb.sh"
```

Replace `<cluster>` with the cluster slug shown in `manifest_dir`.

## Security Notes

- Change `JWT_SECRET`, `SUPER_ADMIN_USERNAME`, and `SUPER_ADMIN_PASSWORD` before non-local use.
- Proxmox credentials and SSH key material are sensitive. Limit database access and backups accordingly.
- Keep `TERRAFORM_AUTO_APPLY=false` and `ANSIBLE_AUTO_APPLY=false` until you are comfortable with the generated workspaces and operational flow.
- Use least-privilege Proxmox API tokens where possible.
- Review generated Terraform, Ansible, MetalLB, observability, and etcd operation files before applying them in production-like environments.

## Development Notes

Useful commands:

```powershell
# Backend
cd backend
go test ./...
go run ./cmd/api-server

# Frontend
cd frontend
npm run build
npm run dev

# Full stack
docker compose up -d --build
docker compose logs -f backend
```

The backend runs migrations automatically when `AUTO_MIGRATE=true`. Generated Terraform, Ansible, and operations files are written under the configured work directories.

## Current Implementation Status

Implemented areas include:

- Go backend API scaffold
- React admin UI
- JWT auth and RBAC
- Seeded super admin
- User management
- Network profile management
- Reserved IP and Proxmox-node network tag management
- PostgreSQL migrations and persistence
- Proxmox credential management
- Proxmox node discovery and enable/disable controls
- Topology planner
- Cluster provisioning workspace generation
- Optional Terraform apply
- Ansible kubeadm bootstrap workspace generation
- Optional Ansible execution
- Cluster list/detail/run history
- Operation progress and event tracking
- Worker scaling
- Upgrade planning
- Node replacement
- Kubeconfig token rotation
- MetalLB config generation
- Observability config generation
- etcd snapshot/restore tracking
- Cluster health and recovery endpoints
