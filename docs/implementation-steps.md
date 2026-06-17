# Implementation Steps

## Step 1: Core API Foundation (done)

- Create backend skeleton
- Implement login with JWT
- Seed super admin user (`k8s-admin`)
- Implement role model (`normal_user`, `super_user`, `super_admin`)
- Add user management endpoints
- Add network profile endpoints for CIDR/gateway/bridge/VLAN

## Step 2: Persistence Layer (done)

- Add PostgreSQL connection and repository implementations
- Wire SQL migrations execution
- Replace in-memory users/networks services with database-backed services

## Step 3: Proxmox Inventory + Topology (done)

- Register and validate Proxmox API credentials
- Discover available Proxmox nodes and resources
- Add topology selection: single-node, dual-node, tri-node
- Implement placement validation and preflight checks

## Step 4: Provisioning Engine (done)

- Add Terraform runner for VM creation from Ubuntu 24 cloud-init image
- Generate cloud-init per node
- Create VM templates for control plane and workers

## Step 5: Kubernetes Bootstrap (done)

- Add Ansible runner for OS + containerd prep
- Bootstrap kubeadm cluster
- Configure HA API VIP and CNI/Ingress add-ons

## Step 6: Cluster Maintenance (done)

- Scale node pools
- Upgrade Kubernetes version
- Replace unhealthy nodes
- Rotate kubeconfig

## Step 7: Observability + Backup (done)

- Prometheus/Grafana deployment
- etcd snapshot schedule and restore workflow
