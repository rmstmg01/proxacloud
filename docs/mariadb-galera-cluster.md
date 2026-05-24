# MariaDB Galera Cluster

This project can now generate a production-oriented three-node MariaDB Galera cluster on Proxmox VMs and expose it to Kubernetes as an external database endpoint.

## Architecture

- Three MariaDB VMs are provisioned on three distinct enabled Proxmox nodes.
- VM placement uses the existing Proxmox inventory, network profiles, SSH keys, and Terraform workspace flow.
- Bootstrap generates an Ansible workspace that installs MariaDB, Galera, `mariadb-backup`, and configures a three-node Galera cluster.
- Kubernetes consumes the database through a `Service` plus `Endpoints` object, so app manifests can keep using a stable DNS name.

## API Flow

Plan placement:

```http
POST /api/v1/database-clusters/plan
```

Provision VMs:

```http
POST /api/v1/database-clusters/provision
```

Bootstrap MariaDB Galera:

```http
POST /api/v1/database-clusters/{id}/bootstrap
```

Fetch generated Kubernetes connection manifest:

```http
GET /api/v1/database-clusters/{id}/k8s-manifests
```

Fetch deploy event logs:

```http
GET /api/v1/database-clusters/{id}/events
```

Terraform and Ansible run for database clusters when the request has `apply: true`. After Terraform creates the database VMs, the deploy workflow automatically applies the Kubernetes `Service` and `Endpoints` for `external-mariadb` by using the backend `KUBECONFIG`. The database-cluster deploy workflow does not require `TERRAFORM_AUTO_APPLY` or `ANSIBLE_AUTO_APPLY`; those flags remain relevant to the Kubernetes cluster provisioning workflow.

Set `apply: false` only when you want reviewable Terraform or Ansible workspaces without changing Proxmox or the database VMs.

## Example Provision Payload

```json
{
  "name": "pscs-prod-db",
  "network_profile_id": 1,
  "proxmox_credential_id": 1,
  "proxmox_node_names": ["pve1", "pve2", "pve3"],
  "node_ip_pool_start": "192.168.8.201",
  "cpu": 4,
  "memory_mb": 8192,
  "disk_gb": 100,
  "mariadb_version": "11.4",
  "ubuntu_image": "local:iso/ubuntu-24.04-server-cloudimg-amd64.img",
  "ssh_key_pair_id": 1,
  "database_name": "pscs",
  "app_username": "pscs",
  "app_password": "change-this-app-password",
  "root_password": "change-this-root-password",
  "sst_username": "sstuser",
  "sst_password": "change-this-sst-password",
  "haproxy_namespace": "pscs",
  "haproxy_service_name": "external-mariadb",
  "apply": false
}
```

## Example Bootstrap Payload

```json
{
  "ssh_key_pair_id": 1,
  "apply": false
}
```

## Generated Workspaces

Terraform:

```text
infra/terraform/runs/db-<cluster-name>/
```

Ansible:

```text
infra/ansible/runs/db-<cluster-name>/
```

## Required Network Ports

Open these between all database VMs:

| Port | Purpose |
| --- | --- |
| TCP 3306 | MariaDB client traffic |
| TCP/UDP 4567 | Galera replication |
| TCP 4568 | Incremental State Transfer |
| TCP 4444 | State Snapshot Transfer |

Kubernetes nodes need TCP `3306` access to the database VM IPs.

## Kubernetes Integration

The deploy workflow automatically applies:

- `Service/external-mariadb`
- `Endpoints/external-mariadb`

The generated manifest endpoint also includes:

- `Secret/external-mariadb-client`

The Secret is returned as guidance and is not auto-applied during VM provisioning, so existing app credentials are not overwritten unexpectedly. For this repository's `pscs` deployment, set application DB configuration to:

```text
DB_HOST=external-mariadb
DB_PORT=3306
DB_NAME=pscs
DB_USER=pscs
DB_PASS=<app password>
```

`infra/k8s/pscs-external-db.yaml` is a static example. Replace its IP addresses and secret values, or use the API-generated manifest for the exact cluster.

## Health Checks

After bootstrap, verify from a database node:

```bash
mariadb -uroot -p -e "SHOW STATUS LIKE 'wsrep_cluster_size'; SHOW STATUS LIKE 'wsrep_local_state_comment'; SHOW STATUS LIKE 'wsrep_ready';"
```

Expected values:

- `wsrep_cluster_size` is `3`
- `wsrep_local_state_comment` is `Synced`
- `wsrep_ready` is `ON`

## Backup Notes

Use `mariadb-backup` for physical backups. Run backups from a synced node and store them outside the database VMs. Keep VM-level snapshots as a secondary safety net, not the only backup strategy.

## Operational Notes

- Keep three nodes for quorum.
- Do not place two database VMs on the same Proxmox node for the production profile.
- Prefer single-writer application behavior or robust retry handling for Galera certification conflicts.
- Protect generated Ansible workspaces because they contain database secrets.
