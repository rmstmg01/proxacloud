# Step 7 API Endpoints

## Observability

- `POST /api/v1/clusters/{id}/observability/deploy`
- `GET /api/v1/clusters/{id}/observability`

Deploy request example:

```json
{
  "grafana_admin_user": "admin",
  "grafana_admin_password": "StrongPass!123",
  "retention_days": 15,
  "storage_class": "longhorn",
  "apply": false
}
```

Behavior:

- Creates values file for kube-prometheus-stack under the configured operations workspace, defaulting to `backend/data/operations/runs/<cluster>/observability`
- Stores observability configuration and status in database

## MetalLB

- `POST /api/v1/clusters/{id}/metallb/deploy`
- `GET /api/v1/clusters/{id}/metallb`

## Applications

- `POST /api/v1/clusters/{id}/ingress-controller/deploy`
- `POST /api/v1/clusters/{id}/apps/deploy`

The ingress controller endpoint deploys Traefik with the Kubernetes Ingress provider, a default `IngressClass`, and a `LoadBalancer` service for MetalLB. The app deploy endpoint applies user-provided Kubernetes YAML to the selected namespace with the exported cluster kubeconfig.

Deploy request example:

```json
{
  "namespace": "metallb-system",
  "version": "v0.14.9",
  "address_pools": [
    {
      "name": "services",
      "addresses": [
        "192.168.8.220-192.168.8.240"
      ],
      "auto_assign": true,
      "avoid_buggy_ips": false
    }
  ],
  "interfaces": ["ens18"],
  "apply": false
}
```

Behavior:

- Creates cluster-specific MetalLB manifests and helper scripts under the configured operations workspace, defaulting to `backend/data/operations/runs/<cluster>/metallb`
- Stores MetalLB configuration and status in database
- `apply=true` marks the deployment as requested but does not execute `kubectl` directly from the API

Run prepared manifests via Docker example:

```powershell
docker compose --profile ops up -d kubectl-runner
docker compose run --rm -e KUBECONFIG=/kube/config -v "$env:USERPROFILE/.kube/config:/kube/config:ro" kubectl-runner "cd /app/backend/data/operations/runs/<cluster>/metallb && ./apply-metallb.sh"
```

## etcd Backup

- `POST /api/v1/clusters/{id}/etcd/snapshot`
- `GET /api/v1/clusters/{id}/etcd/snapshots`

Snapshot request example:

```json
{
  "storage_target": "backups/etcd",
  "apply": false
}
```

## etcd Restore

- `POST /api/v1/clusters/{id}/etcd/restore`

Restore request example:

```json
{
  "snapshot_id": 3,
  "apply": false
}
```

Current Step 7 behavior records deploy/backup/restore plans and runtime status. You can execute the operational runbooks with your production automation pipeline.
