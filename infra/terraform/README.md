# Terraform Provisioning Workspaces

Step 4 backend provisioning writes runtime Terraform workspaces to:

- `infra/terraform/runs/<cluster-name>/`

Each workspace contains:

- `main.tf`
- `variables.tf`
- `terraform.tfvars.json`
- `cloud-init/*.yaml`

Notes:

- Provider authentication details should be injected with secure env vars or environment-specific overrides.
- Generated templates target Proxmox Ubuntu 24 cloud-init VM provisioning.
