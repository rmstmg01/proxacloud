# Step 5 API Endpoints

## Kubernetes Bootstrap

- `POST /api/v1/clusters/{id}/bootstrap` (`super_user`, `super_admin`)

This endpoint prepares Ansible inventory and a kubeadm bootstrap playbook for the cluster nodes created during provisioning.

Request example:

```json
{
  "ssh_user": "ubuntu",
  "ssh_private_key_path": "C:/keys/homelab_id_ed25519",
  "control_plane_vip": "192.168.8.20",
  "pod_cidr": "10.244.0.0/16",
  "service_cidr": "10.96.0.0/12",
  "cni": "calico",
  "node_ips": {
    "homelab-prod-cp-1": "192.168.8.31",
    "homelab-prod-cp-2": "192.168.8.32",
    "homelab-prod-cp-3": "192.168.8.33",
    "homelab-prod-wk-1": "192.168.8.41"
  },
  "apply": false
}
```

Behavior:

- Validates cluster and node inventory from database
- Accepts or updates node IP mapping for cluster nodes
- Generates Ansible assets under `ANSIBLE_WORKDIR/<cluster-name>`
- Stores run status in `provision_runs` with action `bootstrap`
- If `apply=true` (or `ANSIBLE_AUTO_APPLY=true`) executes `ansible-playbook`
