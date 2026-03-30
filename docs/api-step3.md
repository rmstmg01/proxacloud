# Step 3 API Endpoints

All endpoints below require authentication.

## Proxmox Credentials

- `POST /api/v1/proxmox/credentials` (`super_admin`)
- `GET /api/v1/proxmox/credentials` (`super_admin`)

Create request supports either:

- API token: `api_token_id` + `api_token_secret` (recommended)
- Username/password: currently stored but discovery currently supports API token auth

## Proxmox Node Discovery

- `POST /api/v1/proxmox/credentials/{id}/discover-nodes` (`super_admin`)
- `GET /api/v1/proxmox/nodes` (`super_user`, `super_admin`)
- `PATCH /api/v1/proxmox/nodes/{id}` (`super_admin`) body: `{ "enabled": true|false }`

## Topology Planner

- `POST /api/v1/topology/plan` (`super_user`, `super_admin`)

Request body:

```json
{
  "topology_type": "single",
  "control_plane_count": 1,
  "worker_count": 2
}
```

`topology_type` options:

- `single`
- `dual`
- `tri`

Response includes selected Proxmox nodes, control-plane/worker placement, and resilience warnings.
