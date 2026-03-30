# Step 4 API Endpoints

## Cluster Provisioning

- `POST /api/v1/clusters/provision` (`super_user`, `super_admin`)

Creates a cluster provisioning plan, stores cluster metadata, generates Terraform workspace files, and optionally runs Terraform apply.

Request example:

```json
{
  "name": "homelab-prod",
  "topology_type": "tri",
  "control_plane_count": 3,
  "worker_count": 3,
  "network_profile_id": 1,
  "kubernetes_version": "v1.31.4",
  "ubuntu_image": "local:iso/ubuntu-24.04-server-cloudimg-amd64.img",
  "ssh_public_key": "ssh-ed25519 AAAAC3...",
  "control_plane_cpu": 4,
  "control_plane_ram_mb": 8192,
  "control_plane_disk_gb": 80,
  "worker_cpu": 4,
  "worker_ram_mb": 8192,
  "worker_disk_gb": 120,
  "apply": false
}
```

Behavior:

- Uses topology planner (`single`, `dual`, `tri`) for placement on enabled Proxmox nodes
- Generates cloud-init files per VM
- Generates Terraform files under `TERRAFORM_WORKDIR/<cluster-name>`
- Stores cluster and provisioning run metadata in database
- If `apply=true` (or `TERRAFORM_AUTO_APPLY=true`) runs Terraform init/validate/plan/apply
