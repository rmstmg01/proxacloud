import client from './client';
import type {
  ClusterSummary,
  ProvisionRunRecord,
  ProvisionResponse,
  BootstrapResponse,
  ScaleResponse,
  UpgradeResponse,
  ReplaceNodeResponse,
  RotateKubeconfigResponse,
  DeployObservabilityResponse,
  ObservabilityStatusResponse,
  EtcdSnapshotRecord,
  CreateEtcdSnapshotResponse,
  RestoreEtcdSnapshotResponse,
} from '../types';

export const getClusters = () =>
  client.get<ClusterSummary[]>('/clusters').then((r) => r.data);

export const getCluster = (id: number) =>
  client.get<ClusterSummary>(`/clusters/${id}`).then((r) => r.data);

export const getClusterRuns = (id: number) =>
  client.get<ProvisionRunRecord[]>(`/clusters/${id}/runs`).then((r) => r.data);

export const provisionCluster = (body: {
  name: string;
  topology_type: string;
  control_plane_count: number;
  worker_count: number;
  network_profile_id: number;
  kubernetes_version: string;
  ubuntu_image: string;
  ssh_public_key: string;
  control_plane_cpu: number;
  control_plane_ram_mb: number;
  control_plane_disk_gb: number;
  worker_cpu: number;
  worker_ram_mb: number;
  worker_disk_gb: number;
  apply: boolean;
}) => client.post<ProvisionResponse>('/clusters/provision', body).then((r) => r.data);

export const bootstrapCluster = (
  id: number,
  body: {
    ssh_user: string;
    ssh_private_key_path: string;
    control_plane_vip: string;
    pod_cidr: string;
    service_cidr: string;
    cni: string;
    node_ips: Record<string, string>;
    apply: boolean;
  },
) => client.post<BootstrapResponse>(`/clusters/${id}/bootstrap`, body).then((r) => r.data);

export const scaleCluster = (id: number, target_worker_count: number) =>
  client.post<ScaleResponse>(`/clusters/${id}/scale`, { target_worker_count }).then((r) => r.data);

export const upgradeCluster = (id: number, target_kubernetes_version: string) =>
  client
    .post<UpgradeResponse>(`/clusters/${id}/upgrade`, { target_kubernetes_version })
    .then((r) => r.data);

export const replaceNode = (
  id: number,
  body: { failed_vm_name: string; new_proxmox_node_name: string; new_ip_address: string },
) => client.post<ReplaceNodeResponse>(`/clusters/${id}/replace-node`, body).then((r) => r.data);

export const rotateKubeconfig = (id: number, ttl_hours: number) =>
  client
    .post<RotateKubeconfigResponse>(`/clusters/${id}/rotate-kubeconfig`, { ttl_hours })
    .then((r) => r.data);

export const deployObservability = (
  id: number,
  body: {
    grafana_admin_user: string;
    grafana_admin_password: string;
    retention_days: number;
    storage_class: string;
    apply: boolean;
  },
) =>
  client
    .post<DeployObservabilityResponse>(`/clusters/${id}/observability/deploy`, body)
    .then((r) => r.data);

export const getObservability = (id: number) =>
  client
    .get<ObservabilityStatusResponse>(`/clusters/${id}/observability`)
    .then((r) => r.data);

export const createEtcdSnapshot = (
  id: number,
  body: { storage_target: string; apply: boolean },
) =>
  client
    .post<CreateEtcdSnapshotResponse>(`/clusters/${id}/etcd/snapshot`, body)
    .then((r) => r.data);

export const getEtcdSnapshots = (id: number) =>
  client.get<EtcdSnapshotRecord[]>(`/clusters/${id}/etcd/snapshots`).then((r) => r.data);

export const restoreEtcdSnapshot = (
  id: number,
  body: { snapshot_id: number; apply: boolean },
) =>
  client
    .post<RestoreEtcdSnapshotResponse>(`/clusters/${id}/etcd/restore`, body)
    .then((r) => r.data);
