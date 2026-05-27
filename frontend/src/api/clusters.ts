import client from './client';
import type {
  ClusterSummary,
  ProvisionRunRecord,
  ClusterOperation,
  ClusterOperationEvent,
  ClusterNodeRecord,
  ProvisionResponse,
  BootstrapResponse,
  BootstrapConfigRecord,
  BootstrapSSHKeyRecord,
  ClusterPowerActionResponse,
  ClusterRedeployResponse,
  ScaleResponse,
  UpgradeResponse,
  ReplaceNodeResponse,
  RotateKubeconfigResponse,
  DeployMetalLBResponse,
  MetalLBStatusResponse,
  DeployIngressControllerResponse,
  DeployAppResponse,
  ClusterSecretRecord,
  ClusterSecretProvider,
  ApplyClusterSecretResponse,
  ClusterConfigMapRecord,
  ApplyClusterConfigMapResponse,
  ClusterSavedManifestRecord,
  ClusterSavedManifestRevisionRecord,
  ClusterSavedManifestApplyRunRecord,
  ApplyClusterSavedManifestResponse,
  ApplicationManifestRecord,
  ApplicationManifestRevisionRecord,
  KubectlCommandRun,
  NodeCommandKind,
  NodeCommandRun,
  DeployObservabilityResponse,
  ObservabilityStatusResponse,
  EtcdSnapshotRecord,
  CreateEtcdSnapshotResponse,
  RestoreEtcdSnapshotResponse,
} from '../types';

export const getClusters = () =>
  client.get<ClusterSummary[]>('/clusters').then((r) => r.data);

export const checkClusterName = (name: string) =>
  client
    .get<{ name: string; exists: boolean }>(`/clusters/check-name?name=${encodeURIComponent(name)}`)
    .then((r) => r.data);

export const getCluster = (id: number) =>
  client.get<ClusterSummary>(`/clusters/${id}`).then((r) => r.data);

export const deleteCluster = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/clusters/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);

export const startCluster = (id: number) =>
  client.post<ClusterPowerActionResponse>(`/clusters/${id}/start`).then((r) => r.data);

export const restartCluster = (id: number) =>
  client.post<ClusterPowerActionResponse>(`/clusters/${id}/restart`).then((r) => r.data);

export const shutdownCluster = (id: number) =>
  client.post<ClusterPowerActionResponse>(`/clusters/${id}/shutdown`).then((r) => r.data);

export const redeployCluster = (id: number) =>
  client.post<ClusterRedeployResponse>(`/clusters/${id}/redeploy`).then((r) => r.data);

export const getClusterRuns = (id: number) =>
  client.get<ProvisionRunRecord[]>(`/clusters/${id}/runs`).then((r) => r.data);

export const getLatestClusterOperation = (id: number) =>
  client.get<ClusterOperation>(`/clusters/${id}/operations/latest`).then((r) => r.data);

export const getClusterOperationEvents = (
  id: number,
  operation_id?: number,
  since_id?: number,
  limit = 100,
) => {
  const params = new URLSearchParams();
  if (operation_id && operation_id > 0) params.set('operation_id', String(operation_id));
  if (since_id && since_id > 0) params.set('since_id', String(since_id));
  if (limit > 0) params.set('limit', String(limit));
  const query = params.toString();
  const path = query ? `/clusters/${id}/operations/events?${query}` : `/clusters/${id}/operations/events`;
  return client.get<ClusterOperationEvent[]>(path).then((r) => r.data);
};

export const getClusterNodes = (id: number) =>
  client.get<ClusterNodeRecord[]>(`/clusters/${id}/nodes`).then((r) => r.data);

export const getBootstrapConfig = (id: number) =>
  client.get<BootstrapConfigRecord>(`/clusters/${id}/bootstrap-config`).then((r) => r.data);

export const getBootstrapKeys = (id: number) =>
  client.get<BootstrapSSHKeyRecord[]>(`/clusters/${id}/bootstrap-keys`).then((r) => r.data);


export const provisionCluster = (body: {
  name: string;
  topology_type: string;
  control_plane_count: number;
  worker_count: number;
  network_profile_id: number;
  proxmox_credential_id: number;
  ssh_key_pair_id?: number;
  node_credential_ids?: Record<string, number>;
  node_network_profile_ids?: Record<string, number>;
  node_ip_pool_start_by_node?: Record<string, string>;
  node_ip_pool_start: string;
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

export const provisionAndBootstrapCluster = (body: {
  provision: {
    name: string;
    topology_type: string;
    control_plane_count: number;
    worker_count: number;
    network_profile_id: number;
    proxmox_credential_id: number;
    ssh_key_pair_id?: number;
    node_credential_ids?: Record<string, number>;
    node_network_profile_ids?: Record<string, number>;
    node_ip_pool_start_by_node?: Record<string, string>;
    node_ip_pool_start: string;
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
  };
  bootstrap: {
    ssh_user: string;
    ssh_key_pair_id?: number;
    ssh_private_key?: string;
    ssh_key_id?: number;
    ssh_key_name?: string;
    deploy_kube_vip?: boolean;
    control_plane_vip: string;
    pod_cidr: string;
    service_cidr: string;
    cni: string;
    node_ips: Record<string, string>;
    apply: boolean;
    metallb?: {
      namespace: string;
      version: string;
      address_pools: {
        name: string;
        addresses: string[];
        auto_assign: boolean;
        avoid_buggy_ips: boolean;
      }[];
      interfaces: string[];
      apply: boolean;
    };
  };
}) =>
  client
    .post<{ cluster_id: number; cluster_name: string; status: string; warnings: string[] }>(
      '/clusters/provision-and-bootstrap',
      body,
    )
    .then((r) => r.data);

export const bootstrapCluster = (
  id: number,
  body: {
    ssh_user: string;
    ssh_key_pair_id?: number;
    ssh_private_key?: string;
    ssh_key_id?: number;
    ssh_key_name?: string;
    deploy_kube_vip?: boolean;
    control_plane_vip: string;
    pod_cidr: string;
    service_cidr: string;
    cni: string;
    node_ips: Record<string, string>;
    apply: boolean;
    metallb?: {
      namespace: string;
      version: string;
      address_pools: {
        name: string;
        addresses: string[];
        auto_assign: boolean;
        avoid_buggy_ips: boolean;
      }[];
      interfaces: string[];
      apply: boolean;
    };
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

export const deployMetalLB = (
  id: number,
  body: {
    namespace: string;
    version: string;
    address_pools: Array<{
      name: string;
      addresses: string[];
      auto_assign: boolean;
      avoid_buggy_ips: boolean;
    }>;
    interfaces: string[];
    apply: boolean;
  },
) =>
  client
    .post<DeployMetalLBResponse>(`/clusters/${id}/metallb/deploy`, body)
    .then((r) => r.data);

export const getMetalLB = (id: number) =>
  client
    .get<MetalLBStatusResponse>(`/clusters/${id}/metallb`)
    .then((r) => r.data);

export const deployIngressController = (
  id: number,
  body: {
    namespace: string;
    image: string;
    replicas: number;
    apply: boolean;
    wait: boolean;
  },
) =>
  client
    .post<DeployIngressControllerResponse>(`/clusters/${id}/ingress-controller/deploy`, body)
    .then((r) => r.data);

export const deployApp = (
  id: number,
  body: {
    name: string;
    namespace: string;
    manifest: string;
    hostname: string;
    ingress_class: string;
    tls_secret_name: string;
    tls_certificate_id: number;
    tls_fullchain_pem: string;
    tls_private_key_pem: string;
    selected_secret_ids: number[];
    apply_selected_secrets: boolean;
    selected_config_map_ids: number[];
    apply_config_maps: boolean;
    apply: boolean;
  },
) =>
  client
    .post<DeployAppResponse>(`/clusters/${id}/apps/deploy`, body)
    .then((r) => r.data);

export const getClusterSecrets = (id: number) =>
  client.get<ClusterSecretRecord[]>(`/clusters/${id}/secrets`).then((r) => r.data);

export const createClusterSecret = (
  id: number,
  body: {
    name: string;
    namespace: string;
    provider: ClusterSecretProvider;
    type: string;
    description: string;
    external_ref: string;
    provider_config: Record<string, unknown>;
    immutable: boolean;
    is_global: boolean;
    data: Record<string, string>;
  },
) => client.post<ClusterSecretRecord>(`/clusters/${id}/secrets`, body).then((r) => r.data);

export const createGlobalSecret = (
  body: {
    name: string;
    namespace: string;
    provider: ClusterSecretProvider;
    type: string;
    description: string;
    external_ref: string;
    provider_config: Record<string, unknown>;
    immutable: boolean;
    is_global: boolean;
    data: Record<string, string>;
  },
) => client.post<ClusterSecretRecord>('/secrets', body).then((r) => r.data);

export const updateClusterSecret = (
  id: number,
  secretID: number,
  body: {
    name: string;
    namespace: string;
    provider: ClusterSecretProvider;
    type: string;
    description: string;
    external_ref: string;
    provider_config: Record<string, unknown>;
    immutable: boolean;
    is_global: boolean;
    data?: Record<string, string>;
  },
) => client.put<ClusterSecretRecord>(`/clusters/${id}/secrets/${secretID}`, body).then((r) => r.data);

export const deleteClusterSecret = (id: number, secretID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/clusters/${id}/secrets/${secretID}`).then((r) => r.data);

export const applyClusterSecret = (id: number, secretID: number) =>
  client.post<ApplyClusterSecretResponse>(`/clusters/${id}/secrets/${secretID}/apply`).then((r) => r.data);

export const updateClusterSecretKey = (id: number, secretID: number, key: string, value: string) =>
  client
    .put<ClusterSecretRecord>(`/clusters/${id}/secrets/${secretID}/keys/${encodeURIComponent(key)}`, { value })
    .then((r) => r.data);

export const deleteClusterSecretKey = (id: number, secretID: number, key: string) =>
  client
    .delete<ClusterSecretRecord>(`/clusters/${id}/secrets/${secretID}/keys/${encodeURIComponent(key)}`)
    .then((r) => r.data);

export const getClusterConfigMaps = (id: number) =>
  client.get<ClusterConfigMapRecord[]>(`/clusters/${id}/config-maps`).then((r) => r.data);

export const createClusterConfigMap = (
  id: number,
  body: {
    name: string;
    namespace: string;
    description: string;
    immutable: boolean;
    is_global: boolean;
    data: Record<string, string>;
  },
) => client.post<ClusterConfigMapRecord>(`/clusters/${id}/config-maps`, body).then((r) => r.data);

export const createGlobalConfigMap = (
  body: {
    name: string;
    namespace: string;
    description: string;
    immutable: boolean;
    is_global: boolean;
    data: Record<string, string>;
  },
) => client.post<ClusterConfigMapRecord>('/config-maps', body).then((r) => r.data);

export const updateClusterConfigMap = (
  id: number,
  configMapID: number,
  body: {
    name: string;
    namespace: string;
    description: string;
    immutable: boolean;
    is_global: boolean;
    data: Record<string, string>;
  },
) => client.put<ClusterConfigMapRecord>(`/clusters/${id}/config-maps/${configMapID}`, body).then((r) => r.data);

export const deleteClusterConfigMap = (id: number, configMapID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/clusters/${id}/config-maps/${configMapID}`).then((r) => r.data);

export const applyClusterConfigMap = (id: number, configMapID: number) =>
  client.post<ApplyClusterConfigMapResponse>(`/clusters/${id}/config-maps/${configMapID}/apply`).then((r) => r.data);

export const updateClusterConfigMapKey = (id: number, configMapID: number, key: string, value: string) =>
  client
    .put<ClusterConfigMapRecord>(`/clusters/${id}/config-maps/${configMapID}/keys/${encodeURIComponent(key)}`, { value })
    .then((r) => r.data);

export const deleteClusterConfigMapKey = (id: number, configMapID: number, key: string) =>
  client
    .delete<ClusterConfigMapRecord>(`/clusters/${id}/config-maps/${configMapID}/keys/${encodeURIComponent(key)}`)
    .then((r) => r.data);

export const getGlobalSavedManifests = () =>
  client.get<ClusterSavedManifestRecord[]>('/manifests').then((r) => r.data);

export const createGlobalSavedManifest = (
  body: {
    name: string;
    namespace: string;
    description: string;
    raw_manifest: string;
    is_global: boolean;
  },
) => client.post<ClusterSavedManifestRecord>('/manifests', body).then((r) => r.data);

export const updateGlobalSavedManifest = (
  manifestID: number,
  body: {
    name: string;
    namespace: string;
    description: string;
    raw_manifest: string;
    is_global: boolean;
  },
) => client.put<ClusterSavedManifestRecord>(`/manifests/${manifestID}`, body).then((r) => r.data);

export const deleteGlobalSavedManifest = (manifestID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/manifests/${manifestID}`).then((r) => r.data);

export const getClusterSavedManifests = (id: number) =>
  client.get<ClusterSavedManifestRecord[]>(`/clusters/${id}/manifests`).then((r) => r.data);

export const createClusterSavedManifest = (
  id: number,
  body: {
    name: string;
    namespace: string;
    description: string;
    raw_manifest: string;
    is_global: boolean;
  },
) => client.post<ClusterSavedManifestRecord>(`/clusters/${id}/manifests`, body).then((r) => r.data);

export const updateClusterSavedManifest = (
  id: number,
  manifestID: number,
  body: {
    name: string;
    namespace: string;
    description: string;
    raw_manifest: string;
    is_global: boolean;
  },
) => client.put<ClusterSavedManifestRecord>(`/clusters/${id}/manifests/${manifestID}`, body).then((r) => r.data);

export const deleteClusterSavedManifest = (id: number, manifestID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/clusters/${id}/manifests/${manifestID}`).then((r) => r.data);

export const applyClusterSavedManifest = (id: number, manifestID: number, revisionID?: number) =>
  client
    .post<ApplyClusterSavedManifestResponse>(`/clusters/${id}/manifests/${manifestID}/apply`, {
      revision_id: revisionID ?? 0,
    })
    .then((r) => r.data);

export const getClusterSavedManifestRevisions = (id: number, manifestID: number) =>
  client
    .get<ClusterSavedManifestRevisionRecord[]>(`/clusters/${id}/manifests/${manifestID}/revisions`)
    .then((r) => r.data);

export const getClusterSavedManifestRuns = (id: number, manifestID: number) =>
  client
    .get<ClusterSavedManifestApplyRunRecord[]>(`/clusters/${id}/manifests/${manifestID}/runs`)
    .then((r) => r.data);

export const getApplicationManifests = (id: number) =>
  client.get<ApplicationManifestRecord[]>(`/clusters/${id}/apps`).then((r) => r.data);

export const getApplicationManifestRevisions = (id: number, appID: number) =>
  client
    .get<ApplicationManifestRevisionRecord[]>(`/clusters/${id}/apps/${appID}/revisions`)
    .then((r) => r.data);

export const getApplicationManifestRevision = (id: number, appID: number, revisionID: number) =>
  client
    .get<ApplicationManifestRevisionRecord>(`/clusters/${id}/apps/${appID}/revisions/${revisionID}`)
    .then((r) => r.data);

export const redeployApplicationManifest = (
  id: number,
  appID: number,
  body: { revision_id?: number; apply: boolean },
) =>
  client
    .post<DeployAppResponse>(`/clusters/${id}/apps/${appID}/redeploy`, body)
    .then((r) => r.data);

export const startKubectlCommand = (id: number, command: string) =>
  client
    .post<KubectlCommandRun>(`/clusters/${id}/kubectl-commands`, { command })
    .then((r) => r.data);

export const getKubectlCommandRun = (id: number, runID: string) =>
  client
    .get<KubectlCommandRun>(`/clusters/${id}/kubectl-commands/${runID}`)
    .then((r) => r.data);

export const startNodeCommand = (
  id: number,
  nodeName: string,
  kind: NodeCommandKind,
  command: string,
) =>
  client
    .post<NodeCommandRun>(`/clusters/${id}/nodes/${encodeURIComponent(nodeName)}/commands`, {
      kind,
      node: nodeName,
      command,
    })
    .then((r) => r.data);

export const getNodeCommandRun = (id: number, runID: string) =>
  client
    .get<NodeCommandRun>(`/clusters/${id}/node-commands/${runID}`)
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
