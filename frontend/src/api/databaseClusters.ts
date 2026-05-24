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

export const getDatabaseClusters = () =>
  client.get<DatabaseCluster[]>('/database-clusters').then((r) => r.data);

export const getDatabaseCluster = (id: number) =>
  client.get<DatabaseCluster>(`/database-clusters/${id}`).then((r) => r.data);

export const getDatabaseClusterK8sManifest = (id: number) =>
  client
    .get<{ cluster_id: number; manifest: string }>(`/database-clusters/${id}/k8s-manifests`)
    .then((r) => r.data);
