// ─── Auth / Users ─────────────────────────────────────────────────────────────

export type Role = 'normal_user' | 'super_user' | 'super_admin';
export type UserStatus = 'email_pending' | 'pending_approval' | 'active' | 'suspended' | 'rejected';

export interface User {
  id: number;
  username: string;
  first_name: string;
  last_name: string;
  full_name: string;
  email: string;
  contact_number: string;
  role: Role;
  status: UserStatus;
  email_verified_at?: string;
  created_at: string;
  two_factor_enabled: boolean;
  two_factor_method: 'none' | 'email' | 'totp';
  two_factor_enabled_at?: string;
}

export interface LoginResponse {
  token: string;
  user: User;
}

export interface Login2FAChallenge {
  requires_2fa: true;
  challenge_token: string;
  method: 'email' | 'totp';
  message: string;
}

export type LoginResult = LoginResponse | Login2FAChallenge;

export interface TwoFactorStatus {
  enabled: boolean;
  method: 'none' | 'email' | 'totp';
}

export interface TwoFactorTOTPSetup {
  otpauth_url: string;
  secret: string;
  qr_code: string;
  message: string;
}

// ─── Networks ─────────────────────────────────────────────────────────────────

export interface VPC {
  id: number;
  name: string;
  cidr: string;
  description: string;
  is_default: boolean;
  created_at: string;
  updated_at: string;
}

export interface NetworkProfile {
  id: number;
  vpc_id: number;
  vpc_name?: string;
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

export interface NodeNetworkTag {
  proxmox_node_name: string;
  network_profile_id: number;
  network_name: string;
  cidr: string;
  gateway: string;
  bridge: string;
  vlan: number;
  created_at: string;
  updated_at: string;
}

export interface NetworkReservedIP {
  id: number;
  network_profile_id: number;
  network_name: string;
  hostname: string;
  ip_address: string;
  description: string;
  created_at: string;
  updated_at: string;
}

// ─── Proxmox ──────────────────────────────────────────────────────────────────

export interface NetworkIPUsageEntry {
  ip_address: string;
  status: 'used' | 'reserved' | 'detected' | 'conflict' | string;
  source_type: string;
  source_name: string;
  cluster_id?: number;
  last_seen_at?: string;
  details?: string;
}

export interface NetworkIPUsageSummary {
  network_profile_id: number;
  cidr: string;
  gateway: string;
  total_usable: number;
  used_count: number;
  reserved_count: number;
  detected_count: number;
  available_count: number;
  conflict_count: number;
  last_scan_at?: string;
  entries: NetworkIPUsageEntry[];
  available_preview: string[];
}

export interface NetworkIPScanResponse {
  network_profile_id: number;
  scanned_count: number;
  detected_count: number;
  started_at: string;
  finished_at: string;
  warnings: string[];
  usage: NetworkIPUsageSummary;
}

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
  cpu_usage: number;
  memory_total_bytes: number;
  memory_used_bytes: number;
  disk_total_bytes: number;
  disk_used_bytes: number;
  enabled: boolean;
  last_discovered_at: string;
}

export interface NodeStorage {
  credential_id: number;
  credential_name?: string;
  node_name: string;
  storage_id: string;
  type: string;
  content: string;
  enabled: boolean;
  shared: boolean;
  active: boolean;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  usage_percent: number;
  last_discovered_at?: string;
}

export interface StorageInventoryError {
  credential_id: number;
  node_name: string;
  error: string;
}

export interface StorageInventory {
  items: NodeStorage[];
  errors: StorageInventoryError[];
}

export type BackupTargetType = 'proxmox_storage' | 'pbs' | 's3' | 'hetzner_storagebox';

export interface BackupTarget {
  id: number;
  name: string;
  type: BackupTargetType;
  config: Record<string, unknown>;
  enabled: boolean;
  has_access_key: boolean;
  has_secret_key: boolean;
  has_password: boolean;
  has_ssh_private_key: boolean;
  has_restic_password: boolean;
  created_at: string;
  updated_at: string;
}

export interface BackupSummary {
  targets: BackupTarget[];
  jobs: BackupJob[];
  target_count: number;
  enabled_count: number;
  external_count: number;
}

export type BackupSourceType = 'vps' | 'container' | 'cluster';

export interface BackupJob {
  id: number;
  target_id: number;
  target_name?: string;
  source_type: BackupSourceType;
  source_id: number;
  source_name: string;
  mode: string;
  status: string;
  proxmox_node_name: string;
  proxmox_task_id: string;
  staging_storage_id: string;
  artifact_volid: string;
  artifact_path: string;
  upload_method: string;
  upload_status: string;
  remote_path: string;
  upload_output: string;
  upload_error: string;
  output: string;
  error: string;
  created_at: string;
  updated_at: string;
}

export interface SSHKeyPair {
  id: number;
  name: string;
  ssh_user: string;
  public_key: string;
  created_at: string;
  updated_at: string;
}

export interface SSLCertificate {
  id: number;
  name: string;
  subject?: string;
  dns_names: string[];
  not_after?: string;
  created_at: string;
  updated_at: string;
}

export type FirewallPolicy = 'allow' | 'deny';
export type FirewallDirection = 'inbound' | 'outbound';
export type FirewallAction = 'allow' | 'deny';
export type FirewallProtocol = 'tcp' | 'udp' | 'icmp' | 'any';
export type FirewallTargetType = 'vps' | 'container' | 'cluster_node' | 'database_node';

export interface FirewallRule {
  id: number;
  firewall_group_id: number;
  direction: FirewallDirection;
  action: FirewallAction;
  protocol: FirewallProtocol;
  source_cidr: string;
  destination_cidr: string;
  port_start?: number;
  port_end?: number;
  priority: number;
  description: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface FirewallAttachment {
  id: number;
  firewall_group_id: number;
  target_type: FirewallTargetType;
  target_id: number;
  target_name?: string;
  created_at: string;
}

export interface FirewallGroup {
  id: number;
  name: string;
  description: string;
  default_inbound_policy: FirewallPolicy;
  default_outbound_policy: FirewallPolicy;
  enabled: boolean;
  status: string;
  last_applied_at?: string;
  rules?: FirewallRule[];
  attachments?: FirewallAttachment[];
  created_at: string;
  updated_at: string;
}

export interface FirewallEvent {
  id: number;
  firewall_group_id: number;
  action: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
}

export interface FirewallApplyResponse {
  firewall_group_id: number;
  status: string;
  message: string;
  warnings: string[];
}

// ─── Topology ─────────────────────────────────────────────────────────────────

export interface VPSCloudInitImage {
  id: number;
  name: string;
  image_path: string;
  os_type: string;
  description: string;
  is_default: boolean;
  ssh_user: string;
  default_cpu: number;
  default_memory_mb: number;
  default_disk_gb: number;
  source_path: string;
  detected_node_name: string;
  created_at: string;
  updated_at: string;
}

export interface VPSInstance {
  id: number;
  name: string;
  description: string;
  proxmox_credential_id: number;
  proxmox_node_name: string;
  network_profile_id: number;
  network_name?: string;
  cloud_init_image_id: number;
  cloud_init_image_path: string;
  ssh_key_pair_id: number;
  vm_name: string;
  hostname: string;
  ip_mode: 'dhcp' | 'manual' | 'auto';
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  ssh_user: string;
  terraform_work_dir: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface VPSEvent {
  id: number;
  vps_id: number;
  action: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
}

export interface VPSActionResponse {
  vps_id: number;
  action: string;
  status: string;
  output?: string;
  warnings: string[];
}

export interface VPSLoginDetails {
  vps_id: number;
  hostname: string;
  ip_address: string;
  ssh_user: string;
  root_password?: string;
  has_root_password: boolean;
}

export interface VPSResetRootPasswordResponse {
  vps_id: number;
  root_password: string;
  output?: string;
}

export interface VPSConsoleResponse {
  vps_id: number;
  vmid: number;
  node_name: string;
  url: string;
  session_id: string;
  websocket_path: string;
  password: string;
  warnings: string[];
}

export interface VPSTunnelAttachment {
  id: number;
  vps_id: number;
  customer_account_id?: number;
  tunnel_id: string;
  http_route_id: string;
  tcp_route_id: string;
  token_id: string;
  public_hostname: string;
  public_port: number;
  http_target_url: string;
  tcp_target_host: string;
  tcp_target_port: number;
  ssh_user: string;
  install_command_redacted?: string;
  cloudflare_dns_record_id?: string;
  cloudflare_dns_record_type?: string;
  cloudflare_dns_record_content?: string;
  cloudflare_dns_proxied: boolean;
  cloudflare_dns_status?: string;
  status: string;
  last_error: string;
  emailed_at?: string;
  created_at: string;
  updated_at: string;
  ssh_command: string;
  web_url: string;
}

export interface VPSCreateResponse {
  vps_id: number;
  status: string;
  terraform_work_dir: string;
  terraform_output?: string;
  warnings: string[];
  tunnel?: VPSTunnelAttachment;
}

export interface ContainerTemplate {
  id: number;
  name: string;
  template_path: string;
  os_type: string;
  description: string;
  is_default: boolean;
  created_at: string;
  updated_at: string;
}

export interface ContainerInstance {
  id: number;
  name: string;
  description: string;
  proxmox_credential_id: number;
  proxmox_node_name: string;
  network_profile_id: number;
  network_name?: string;
  template_id: number;
  template_path: string;
  ssh_key_pair_id: number;
  container_name: string;
  ip_mode: 'dhcp' | 'manual' | 'auto';
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  unprivileged: boolean;
  nesting: boolean;
  terraform_work_dir: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface ContainerEvent {
  id: number;
  container_id: number;
  action: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
}

export interface ContainerActionResponse {
  container_id: number;
  action: string;
  status: string;
  output?: string;
  warnings: string[];
}

export interface ContainerCreateResponse {
  container_id: number;
  status: string;
  terraform_work_dir: string;
  terraform_output?: string;
  warnings: string[];
}

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
  control_plane_cpu?: number;
  control_plane_ram_mb?: number;
  control_plane_disk_gb?: number;
  worker_cpu?: number;
  worker_ram_mb?: number;
  worker_disk_gb?: number;
  status: string;
  health_status: string;
  health_checked_at?: string;
  kubeconfig_path?: string;
  kubeconfig_mode?: string;
  created_at: string;
  updated_at: string;
}

export interface ClusterPowerActionResponse {
  cluster_id: number;
  action: string;
  status: string;
  nodes: string[];
  warnings: string[];
}

export interface ClusterRedeployResponse {
  cluster_id: number;
  action: string;
  status: string;
  work_dirs: string[];
  warnings: string[];
}

export interface ClusterNodeRecord {
  id: number;
  role: string;
  role_index: number;
  vm_name: string;
  ip_address: string;
  proxmox_node_name: string;
  proxmox_credential_id: number;
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

export interface ClusterOperation {
  id: number;
  cluster_id: number;
  action: string;
  status: string;
  current_step: string;
  progress_percent: number;
  error_summary?: string;
  started_at: string;
  updated_at: string;
  finished_at?: string;
}

export interface ClusterOperationEvent {
  id: number;
  operation_id: number;
  step: string;
  level: string;
  message: string;
  created_at: string;
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
  deploy_kube_vip: boolean;
  control_plane_vip: string;
  pod_cidr: string;
  service_cidr: string;
  cni: string;
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

export interface MetalLBAddressPool {
  name: string;
  addresses: string[];
  auto_assign: boolean;
  avoid_buggy_ips: boolean;
}

export interface MetalLBCommands {
  powershell: string;
  shell: string;
}

export interface DeployMetalLBResponse {
  cluster_id: number;
  status: string;
  namespace: string;
  version: string;
  manifest_dir: string;
  commands: MetalLBCommands;
  warnings: string[];
}

export interface MetalLBStatusResponse {
  cluster_id: number;
  namespace: string;
  version: string;
  address_pools: MetalLBAddressPool[];
  interfaces: string[];
  status: string;
  manifest_dir: string;
  commands: MetalLBCommands;
  updated_at: string;
}

export interface DeployIngressControllerResponse {
  cluster_id: number;
  operation_id?: number;
  status: string;
  namespace: string;
  image: string;
  replicas: number;
  output?: string;
  warnings: string[];
}

export interface DeployLonghornResponse {
  cluster_id: number;
  status: string;
  version: string;
  output?: string;
  warnings: string[];
}

export interface DeployAppResponse {
  cluster_id: number;
  status: string;
  name: string;
  namespace: string;
  app_id?: number;
  revision_id?: number;
  revision?: number;
  hostname?: string;
  ingress_class?: string;
  tls_secret_name?: string;
  secret_names?: string[];
  config_map_names?: string[];
  image_pull_secret_names?: string[];
  output?: string;
  warnings: string[];
}

export type ClusterSecretProvider =
  | 'native'
  | 'hashicorp-vault'
  | 'external-secrets-operator'
  | 'aws-secrets-manager'
  | 'azure-key-vault'
  | 'google-secret-manager'
  | 'bitnami-sealed-secrets';

export interface ClusterSecretRecord {
  id: number;
  cluster_id: number;
  name: string;
  namespace: string;
  provider: ClusterSecretProvider;
  type: string;
  description: string;
  external_ref?: string;
  provider_config?: Record<string, unknown>;
  immutable: boolean;
  is_global: boolean;
  keys: string[];
  last_applied_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ApplyClusterSecretResponse {
  cluster_id: number;
  secret_id: number;
  name: string;
  namespace: string;
  provider: ClusterSecretProvider;
  status: string;
  output?: string;
  warnings: string[];
}

export interface ClusterConfigMapRecord {
  id: number;
  cluster_id: number;
  name: string;
  namespace: string;
  description: string;
  data: Record<string, string>;
  immutable: boolean;
  is_global: boolean;
  last_applied_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ApplyClusterConfigMapResponse {
  cluster_id: number;
  config_map_id: number;
  name: string;
  namespace: string;
  status: string;
  output?: string;
  warnings: string[];
}

export interface ClusterImagePullSecretRecord {
  id: number;
  cluster_id: number;
  name: string;
  namespace: string;
  registry_server: string;
  username: string;
  email: string;
  description: string;
  is_global: boolean;
  last_applied_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ApplyClusterImagePullSecretResponse {
  cluster_id: number;
  image_pull_secret_id: number;
  name: string;
  namespace: string;
  registry_server: string;
  status: string;
  output?: string;
  warnings: string[];
}

export interface ClusterSavedManifestRecord {
  id: number;
  cluster_id: number;
  name: string;
  namespace: string;
  description: string;
  raw_manifest: string;
  is_global: boolean;
  latest_revision_id?: number;
  latest_revision?: number;
  last_status?: string;
  last_applied_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ClusterSavedManifestRevisionRecord {
  id: number;
  manifest_id: number;
  cluster_id: number;
  revision_number: number;
  raw_manifest: string;
  created_at: string;
}

export interface ClusterSavedManifestApplyRunRecord {
  id: number;
  manifest_id: number;
  revision_id: number;
  cluster_id: number;
  status: string;
  kubectl_output: string;
  error_message: string;
  applied_at: string;
  created_at: string;
}

export interface ApplyClusterSavedManifestResponse {
  cluster_id: number;
  manifest_id: number;
  revision_id: number;
  run_id: number;
  name: string;
  namespace: string;
  status: string;
  output?: string;
  warnings: string[];
}

export interface ApplicationManifestRecord {
  id: number;
  cluster_id: number;
  name: string;
  namespace: string;
  hostname: string;
  ingress_class: string;
  tls_secret_name: string;
  tls_certificate_id?: number;
  latest_revision_id?: number;
  latest_revision?: number;
  latest_status?: string;
  created_at: string;
  updated_at: string;
}

export interface ApplicationManifestRevisionRecord {
  id: number;
  application_manifest_id: number;
  cluster_id: number;
  revision_number: number;
  raw_manifest: string;
  rendered_manifest: string;
  kubectl_output: string;
  status: string;
  error_message: string;
  apply: boolean;
  created_at: string;
}

export interface KubectlCommandRun {
  id: string;
  cluster_id: number;
  command: string;
  status: string;
  output: string;
  error?: string;
  started_at: string;
  finished_at?: string;
}

export type NodeCommandKind = 'docker' | 'linux';

export interface NodeCommandRun {
  id: string;
  cluster_id: number;
  node: string;
  kind: NodeCommandKind;
  command: string;
  status: string;
  output: string;
  error?: string;
  started_at: string;
  finished_at?: string;
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
