# Step 6 API Endpoints

## Cluster State

- `GET /api/v1/clusters`
- `GET /api/v1/clusters/{id}`
- `GET /api/v1/clusters/{id}/runs`

## Scale Worker Nodes

- `POST /api/v1/clusters/{id}/scale`

Request:

```json
{
  "target_worker_count": 5
}
```

## Upgrade Kubernetes Version

- `POST /api/v1/clusters/{id}/upgrade`

Request:

```json
{
  "target_kubernetes_version": "v1.32.2"
}
```

## Replace Unhealthy Worker Node

- `POST /api/v1/clusters/{id}/replace-node`

Request:

```json
{
  "failed_vm_name": "homelab-prod-wk-3",
  "new_proxmox_node_name": "pve-node-2",
  "new_ip_address": "192.168.8.53"
}
```

Current behavior supports worker replacement metadata workflow.

## Rotate Kubeconfig Token

- `POST /api/v1/clusters/{id}/rotate-kubeconfig`

Request:

```json
{
  "ttl_hours": 24
}
```

Returns a newly issued token and expiration timestamp.
