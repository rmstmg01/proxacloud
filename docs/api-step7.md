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

- Creates values file for kube-prometheus-stack under `infra/operations/runs/<cluster>/observability`
- Stores observability configuration and status in database

## MetalLB

- `POST /api/v1/clusters/{id}/metallb/deploy`
- `GET /api/v1/clusters/{id}/metallb`

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

- Creates cluster-specific MetalLB manifests and helper scripts under `infra/operations/runs/<cluster>/metallb`
- Stores MetalLB configuration and status in database
- `apply=true` marks the deployment as requested but does not execute `kubectl` directly from the API

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
