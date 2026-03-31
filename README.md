# K8s Proxmox Manager

Step-by-step implementation of a self-hosted application to provision production-style Kubernetes clusters on Proxmox using Ubuntu 24 cloud-init VMs.

## Step 1 (Implemented)

- Backend API scaffold in Go
- JWT authentication
- Role model (`normal_user`, `super_user`, `super_admin`)
- Seeded super admin user: `k8s-admin`
- User management endpoints
- Network profile management endpoints (CIDR/gateway configurable)
- Seeded default network profile (`192.168.8.0/24`)

## Step 2 (Implemented)

- PostgreSQL-backed user and network profile services
- SQL migration runner on startup (`AUTO_MIGRATE=true`)
- Database seeding for super admin and default network profile

## Step 3 (Implemented)

- Proxmox credential APIs
- Proxmox node discovery and inventory APIs
- Node enable/disable control for placement eligibility
- Topology planner API for single, dual, and tri-node deployment options

## Step 4 (Implemented)

- Cluster provisioning API endpoint
- Terraform workspace and VM template generation
- Cloud-init generation per VM (Ubuntu 24 cloud image flow)
- Optional Terraform apply execution controlled by request/config

## Step 5 (Implemented)

- Ansible bootstrap runner and workspace generation
- Kubeadm bootstrap playbook generation for control plane and workers
- Cluster bootstrap API endpoint with optional ansible-playbook execution

## Step 6 (Implemented)

- Cluster list/get/run history APIs
- Worker scaling API
- Kubernetes version upgrade planning API
- Unhealthy worker replacement API
- Kubeconfig token rotation API

## Step 7 (Implemented)

- MetalLB deployment configuration API and manifest generation
- Observability deployment configuration API (Prometheus/Grafana values generation)
- etcd snapshot planning and restore planning APIs
- Snapshot and restore tracking persistence

## Run Backend

1. Copy `.env.example` to `.env` and update values.
2. Ensure PostgreSQL is running and matches `DATABASE_URL`.
3. Start API:

```powershell
cd backend
go mod tidy
go run ./cmd/api-server
```

API default address: `http://localhost:8080`

## Run With Docker Compose

```powershell
cd .
docker compose up -d --build
```

Check logs:

```powershell
docker compose logs -f backend
```

Stop stack:

```powershell
docker compose down
```

Notes:

- Backend API will be at `http://localhost:8080`
- PostgreSQL will be at `localhost:5432`
- Terraform/Ansible apply operations are disabled by default in compose (`*_AUTO_APPLY=false`)
- A dedicated `kubectl-runner` service is available under the `ops` compose profile for operational apply actions

## Apply MetalLB From Docker

After preparing MetalLB from the API/UI, use the generated `manifest_dir` with the ops runner.

PowerShell example:

```powershell
cd .
docker compose --profile ops up -d kubectl-runner
docker compose run --rm -e KUBECONFIG=/kube/config -v "$env:USERPROFILE/.kube/config:/kube/config:ro" kubectl-runner "cd /app/backend/data/operations/runs/<cluster>/metallb && ./apply-metallb.sh"
```

Bash example:

```bash
cd .
docker compose --profile ops up -d kubectl-runner
docker compose run --rm -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" kubectl-runner "cd /app/backend/data/operations/runs/<cluster>/metallb && ./apply-metallb.sh"
```

Replace `<cluster>` with the cluster slug from `manifest_dir`.

## Next Steps

- Step 2: PostgreSQL persistence + migrations wiring
- Step 3: Proxmox node discovery API and topology planner (1/2/3 nodes)
- Step 4: Terraform + cloud-init VM provisioning orchestration
- Step 5: kubeadm HA bootstrap workflows
