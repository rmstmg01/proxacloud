import client from './client';

export type MonitoringClusterMode = 'in_cluster' | 'remote_write' | 'federation';

export interface MonitoringStack {
  id: number;
  namespace: string;
  grafana_domain: string;
  storage_class: string;
  prometheus_storage_size: string;
  grafana_storage_size: string;
  retention: string;
  expose_grafana: boolean;
  expose_prometheus: boolean;
  expose_alertmanager: boolean;
  status: string;
  last_values_yaml?: string;
  last_manifest?: string;
  last_apply_output?: string;
  last_applied_at?: string;
  created_at: string;
  updated_at: string;
}

export interface MonitoredCluster {
  id: number;
  cluster_id?: number;
  cluster_name?: string;
  name: string;
  environment: string;
  mode: MonitoringClusterMode;
  status: string;
  prometheus_url: string;
  grafana_url: string;
  agent_namespace: string;
  remote_write_url: string;
  notes: string;
  last_manifest?: string;
  last_apply_output?: string;
  last_synced_at?: string;
  created_at: string;
  updated_at: string;
}

export interface MonitoringStatus {
  stack?: MonitoringStack;
  cluster_count: number;
  healthy_clusters: number;
  configured_clusters: number;
  needs_attention: number;
  recommended_mode: MonitoringClusterMode;
}

export interface UpsertMonitoringStackRequest {
  namespace: string;
  grafana_domain: string;
  storage_class: string;
  prometheus_storage_size: string;
  grafana_storage_size: string;
  retention: string;
  expose_grafana: boolean;
  expose_prometheus: boolean;
  expose_alertmanager: boolean;
}

export interface CreateMonitoredClusterRequest {
  cluster_id?: number;
  name: string;
  environment: string;
  mode: MonitoringClusterMode;
  prometheus_url: string;
  grafana_url: string;
  agent_namespace: string;
  remote_write_url: string;
  notes: string;
}

export interface MonitoringManifestResponse {
  values_yaml?: string;
  manifest: string;
  commands: string;
}

export interface SyncMonitoringClusterResponse {
  cluster_id: number;
  status: string;
  output: string;
  manifest: string;
}

export const getMonitoringStatus = () =>
  client.get<MonitoringStatus>('/monitoring/status').then((r) => r.data);

export const upsertMonitoringStack = (body: UpsertMonitoringStackRequest) =>
  client.put<MonitoringStack>('/monitoring/stack', body).then((r) => r.data);

export const getMonitoringStackManifest = () =>
  client.get<MonitoringManifestResponse>('/monitoring/stack/manifest').then((r) => r.data);

export const getMonitoredClusters = () =>
  client.get<MonitoredCluster[]>('/monitoring/clusters').then((r) => r.data);

export const createMonitoredCluster = (body: CreateMonitoredClusterRequest) =>
  client.post<MonitoredCluster>('/monitoring/clusters', body).then((r) => r.data);

export const getMonitoredClusterManifest = (id: number) =>
  client.get<MonitoringManifestResponse>(`/monitoring/clusters/${id}/manifest`).then((r) => r.data);

export const syncMonitoredCluster = (id: number) =>
  client.post<SyncMonitoringClusterResponse>(`/monitoring/clusters/${id}/sync`).then((r) => r.data);

export const deleteMonitoredCluster = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/monitoring/clusters/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);
