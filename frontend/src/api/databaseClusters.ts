import client from './client';

export type DatabaseClusterNode = {
  id: number;
  node_index: number;
  vm_name: string;
  ip_address: string;
  proxmox_node_name: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  status: string;
};

export type DatabaseCluster = {
  id: number;
  name: string;
  engine: string;
  mariadb_version: string;
  topology_type: string;
  database_name: string;
  app_username: string;
  haproxy_namespace: string;
  haproxy_service_name: string;
  terraform_work_dir: string;
  ansible_work_dir: string;
  status: string;
  created_at: string;
  updated_at: string;
  nodes?: DatabaseClusterNode[];
};

export type DatabaseClusterEvent = {
  id: number;
  cluster_id: number;
  step: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
};

export const getDatabaseClusters = () =>
  client.get<DatabaseCluster[]>('/database-clusters').then((r) => r.data);

export const getDatabaseCluster = (id: number) =>
  client.get<DatabaseCluster>(`/database-clusters/${id}`).then((r) => r.data);

export const getDatabaseClusterK8sManifest = (id: number) =>
  client
    .get<{ cluster_id: number; manifest: string }>(`/database-clusters/${id}/k8s-manifests`)
    .then((r) => r.data);

export const getDatabaseClusterEvents = (id: number) =>
  client.get<DatabaseClusterEvent[]>(`/database-clusters/${id}/events`).then((r) => r.data);

export const deleteDatabaseCluster = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/database-clusters/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);

export const provisionDatabaseCluster = (body: {
  name: string;
  network_profile_id: number;
  proxmox_credential_id: number;
  proxmox_node_names: string[];
  node_ip_pool_start: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  mariadb_version: string;
  ubuntu_image: string;
  ssh_key_pair_id: number;
  database_name: string;
  app_username: string;
  app_password: string;
  root_password: string;
  sst_username: string;
  sst_password: string;
  haproxy_namespace: string;
  haproxy_service_name: string;
  apply: boolean;
}) =>
  client
    .post<{
      cluster_id: number;
      status: string;
      terraform_work_dir: string;
      terraform_output?: string;
      kubernetes_yaml: string;
      warnings: string[];
    }>('/database-clusters/provision', body)
    .then((r) => r.data);

export const bootstrapDatabaseCluster = (
  id: number,
  body: {
    ssh_key_pair_id: number;
    apply: boolean;
  },
) =>
  client
    .post<{
      cluster_id: number;
      status: string;
      ansible_work_dir: string;
      ansible_output?: string;
      warnings: string[];
    }>(`/database-clusters/${id}/bootstrap`, body)
    .then((r) => r.data);

export const applyDatabaseClusterTerraform = (id: number) =>
  client
    .post<{
      cluster_id: number;
      status: string;
      terraform_work_dir: string;
      terraform_output?: string;
      warnings: string[];
    }>(`/database-clusters/${id}/terraform/apply`)
    .then((r) => r.data);
