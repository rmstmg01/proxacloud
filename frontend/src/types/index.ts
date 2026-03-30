// ─── Auth / Users ─────────────────────────────────────────────────────────────

export type Role = 'normal_user' | 'super_user' | 'super_admin';

export interface User {
  id: number;
  username: string;
  role: Role;
  created_at: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

// ─── Networks ─────────────────────────────────────────────────────────────────

export interface NetworkProfile {
  id: number;
  name: string;
  cidr: string;
  gateway: string;
  dns: string[];
  bridge: string;
  vlan: number;
  is_default: boolean;
  created_at: string;
  updated_at: string;
}

// ─── Proxmox ──────────────────────────────────────────────────────────────────

export interface ProxmoxCredential {
  id: number;
  name: string;
  endpoint: string;
  username?: string;
  api_token_id?: string;
  insecure_tls: boolean;
  created_at: string;
}

export interface ProxmoxNode {
  id: number;
  credential_id: number;
  credential_name?: string;
  node_name: string;
  status: string;
  cpu_count: number;
  memory_total_bytes: number;
  memory_used_bytes: number;
  enabled: boolean;
  last_discovered_at: string;
}

// ─── Topology ─────────────────────────────────────────────────────────────────

export type TopologyType = 'single' | 'dual' | 'tri';

export interface Placement {
  role: 'control-plane' | 'worker';
  index: number;
  proxmox_node_name: string;
}

export interface PlanResponse {
  topology_type: string;
  requested_nodes: number;
  available_nodes: number;
  control_plane_count: number;
  worker_count: number;
  selected_proxmox_nodes: string[];
  placements: Placement[];
  warnings: string[];
}

// ─── Clusters ─────────────────────────────────────────────────────────────────

export interface ClusterSummary {
  id: number;
  name: string;
  topology_type: string;
  kubernetes_version: string;
  network_profile_id: number;
  control_plane_count: number;
  worker_count: number;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface ClusterNodeRecord {
  id: number;
  role: string;
  role_index: number;
  vm_name: string;
  ip_address: string;
  proxmox_node_name: string;
}

export interface ProvisionRunRecord {
  id: number;
  action: string;
  status: string;
  work_dir: string;
  output: string;
  created_at: string;
  finished_at?: string;
}

export interface ProvisionResponse {
  cluster_id: number;
  cluster_name: string;
  status: string;
  topology: PlanResponse;
  terraform_work_dir: string;
  terraform_output?: string;
  warnings: string[];
}

export interface BootstrapResponse {
  cluster_id: number;
  status: string;
  ansible_work_dir: string;
  ansible_output?: string;
  warnings: string[];
}

export interface BootstrapConfigRecord {
  cluster_id: number;
  ssh_user: string;
  last_ssh_key_id?: number | null;
  updated_at: string;
}

export interface BootstrapSSHKeyRecord {
  id: number;
  cluster_id: number;
  key_name: string;
  updated_at: string;
}

export interface ScaleResponse {
  cluster_id: number;
  previous_worker_count: number;
  new_worker_count: number;
  added_nodes: string[];
  removed_nodes: string[];
  warnings: string[];
}

export interface UpgradeResponse {
  cluster_id: number;
  previous_version: string;
  target_version: string;
  status: string;
  warning?: string;
}

export interface ReplaceNodeResponse {
  cluster_id: number;
  old_vm_name: string;
  new_vm_name: string;
  status: string;
  warning?: string;
}

export interface RotateKubeconfigResponse {
  cluster_id: number;
  token: string;
  expires_at: string;
  status: string;
}

export interface DeployObservabilityResponse {
  cluster_id: number;
  status: string;
  dashboard_url: string;
  manifest_dir: string;
  warnings: string[];
}

export interface ObservabilityStatusResponse {
  cluster_id: number;
  grafana_admin_user: string;
  retention_days: number;
  storage_class: string;
  status: string;
  dashboard_url: string;
  updated_at: string;
}

export interface EtcdSnapshotRecord {
  id: number;
  snapshot_name: string;
  storage_target: string;
  snapshot_path: string;
  status: string;
  created_at: string;
}

export interface CreateEtcdSnapshotResponse {
  cluster_id: number;
  snapshot_id: number;
  snapshot_name: string;
  snapshot_path: string;
  status: string;
  created_at: string;
  warning?: string;
}

export interface RestoreEtcdSnapshotResponse {
  cluster_id: number;
  snapshot_id: number;
  status: string;
  warning?: string;
}
