import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CheckCircle2, Copy, Database, Eye, EyeOff, FileCode, Loader2, Play, Plus, Power, RefreshCw, Rocket, RotateCcw, Trash2 } from 'lucide-react';
import {
  bootstrapDatabaseCluster,
  deleteDatabaseCluster,
  getDatabaseCluster,
  getDatabaseClusterCredentials,
  getDatabaseClusterEvents,
  getDatabaseClusterK8sManifest,
  getDatabaseClusters,
  provisionDatabaseCluster,
  restartDatabaseCluster,
  startDatabaseCluster,
  stopDatabaseCluster,
  type DatabaseCluster,
  type DatabaseClusterEvent,
} from '../api/databaseClusters';
import { getRegions } from '../api/regions';
import { discoverVPSCloudInitImagesAutomatically } from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { architectureLabel, DistributionLogo } from '../components/DistributionLogo';
import { useAuth } from '../context/AuthContext';
import type { Region, VPSCloudInitImage } from '../types';

const initialForm = {
  name: '',
  region_id: '',
  cpu: '4',
  memory_mb: '8192',
  disk_gb: '100',
  mariadb_version: '11.4',
  cloud_init_image_id: '',
  database_name: 'pscs',
  app_username: 'pscs',
  app_password: '',
  root_password: '',
  sst_username: 'sstuser',
  sst_password: '',
  generate_passwords: true,
  haproxy_namespace: 'pscs',
  haproxy_service_name: 'external-mariadb',
};

export function DatabaseClustersPage() {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const isAdmin = user?.role === 'super_user' || user?.role === 'super_admin';
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [manifestID, setManifestID] = useState<number | null>(null);
  const [showDeploy, setShowDeploy] = useState(false);
  const [form, setForm] = useState(initialForm);
  const [deleteTarget, setDeleteTarget] = useState<DatabaseCluster | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [lastResult, setLastResult] = useState<{ clusterID: number; message: string; manifest?: string } | null>(null);
  const {
    data: clusters,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['database-clusters'],
    queryFn: getDatabaseClusters,
    refetchInterval: 30_000,
  });
  const { data: regions } = useQuery({ queryKey: ['regions'], queryFn: getRegions });
  const { data: images, error: imageError, isLoading: imagesLoading } = useQuery({
    queryKey: ['database-cluster-images'],
    queryFn: () => discoverVPSCloudInitImagesAutomatically(),
    staleTime: 0,
  });
  const ubuntuImages = useMemo(() => (images ?? [])
    .filter((image) => image.available !== false && image.image_path.toLowerCase().startsWith('cloud-images:import/'))
    .filter((image) => image.distribution === 'ubuntu' || (!image.distribution && image.os_type === 'ubuntu'))
    .sort((a, b) => (a.version || a.name).localeCompare(b.version || b.name, undefined, { numeric: true })), [images]);
  useEffect(() => {
    const current = ubuntuImages.some((image) => image.id === Number(form.cloud_init_image_id));
    if (!current) {
      const preferred = ubuntuImages.find((image) => image.is_default) ?? ubuntuImages[0];
      setForm((value) => ({ ...value, cloud_init_image_id: preferred ? String(preferred.id) : '' }));
    }
  }, [ubuntuImages, form.cloud_init_image_id]);
  const selectedCluster = clusters?.find((cluster) => cluster.id === selectedID) ?? null;
  const detailQuery = useQuery({
    queryKey: ['database-cluster', selectedID],
    queryFn: () => getDatabaseCluster(selectedID ?? 0),
    enabled: selectedID !== null,
    refetchInterval: selectedID !== null ? 5_000 : false,
  });
  const manifestQuery = useQuery({
    queryKey: ['database-cluster-manifest', manifestID],
    queryFn: () => getDatabaseClusterK8sManifest(manifestID ?? 0),
    enabled: manifestID !== null,
  });
  const eventsQuery = useQuery({
    queryKey: ['database-cluster-events', selectedID],
    queryFn: () => getDatabaseClusterEvents(selectedID ?? 0),
    enabled: selectedID !== null && isAdmin,
    refetchInterval: selectedID !== null ? 5_000 : false,
  });
  const invalidateClusterQueries = (clusterID?: number) => {
    queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
    if (clusterID) {
      queryClient.invalidateQueries({ queryKey: ['database-cluster', clusterID] });
      queryClient.invalidateQueries({ queryKey: ['database-cluster-events', clusterID] });
    }
  };
  const provisionMutation = useMutation({
    mutationFn: provisionDatabaseCluster,
    onSuccess: async (resp) => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
      const message = `Database cluster deployment started with status ${resp.status}.`;
      setSelectedID(resp.cluster_id);
      setManifestID(resp.cluster_id);
      queryClient.invalidateQueries({ queryKey: ['database-cluster', resp.cluster_id] });
      queryClient.invalidateQueries({ queryKey: ['database-cluster-events', resp.cluster_id] });
      setLastResult({ clusterID: resp.cluster_id, message, manifest: resp.kubernetes_yaml });
      setShowDeploy(false);
      setForm(initialForm);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
    },
  });
  const bootstrapMutation = useMutation({
    mutationFn: (id: number) => bootstrapDatabaseCluster(id, { ssh_key_pair_id: 0, apply: true }),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
      queryClient.invalidateQueries({ queryKey: ['database-cluster', resp.cluster_id] });
      queryClient.invalidateQueries({ queryKey: ['database-cluster-events', resp.cluster_id] });
      setSelectedID(resp.cluster_id);
      setLastResult({
        clusterID: resp.cluster_id,
        message: `Galera bootstrap started; watch the Event Log for live progress.`,
      });
    },
    onError: (_error, id) => {
      queryClient.invalidateQueries({ queryKey: ['database-cluster-events', id] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) => deleteDatabaseCluster(id, confirmationName),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
      if (selectedID === resp.id) {
        setSelectedID(null);
      }
      if (manifestID === resp.id) {
        setManifestID(null);
      }
      setDeleteTarget(null);
      setDeleteConfirmationName('');
      setLastResult({ clusterID: resp.id, message: 'Database cluster deleted.' });
    },
  });
  const startMutation = useMutation({
    mutationFn: startDatabaseCluster,
    onSuccess: (resp) => {
      invalidateClusterQueries(resp.cluster_id);
      setSelectedID(resp.cluster_id);
      setLastResult({ clusterID: resp.cluster_id, message: `Database cluster start sent to ${resp.nodes.length} node(s).` });
    },
  });
  const restartMutation = useMutation({
    mutationFn: restartDatabaseCluster,
    onSuccess: (resp) => {
      invalidateClusterQueries(resp.cluster_id);
      setSelectedID(resp.cluster_id);
      setLastResult({ clusterID: resp.cluster_id, message: `Database cluster restart sent to ${resp.nodes.length} node(s).` });
    },
  });
  const stopMutation = useMutation({
    mutationFn: stopDatabaseCluster,
    onSuccess: (resp) => {
      invalidateClusterQueries(resp.cluster_id);
      setSelectedID(resp.cluster_id);
      setLastResult({ clusterID: resp.cluster_id, message: `Database cluster stop sent to ${resp.nodes.length} node(s).` });
    },
  });

  const updateField = (field: keyof typeof initialForm, value: string | boolean) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const submitDeploy = (event: FormEvent) => {
    event.preventDefault();
    if (!(regions ?? []).some((region) => region.id === Number(form.region_id) && region.can_deploy && region.nodes.filter((node) => node.enabled).length >= 3)) return;
    provisionMutation.mutate({
      name: form.name.trim(),
      region_id: Number(form.region_id),
      cpu: Number(form.cpu),
      memory_mb: Number(form.memory_mb),
      disk_gb: Number(form.disk_gb),
      mariadb_version: form.mariadb_version.trim(),
      cloud_init_image_id: Number(form.cloud_init_image_id),
      database_name: form.database_name.trim(),
      app_username: form.app_username.trim(),
      app_password: form.app_password,
      root_password: form.root_password,
      sst_username: form.sst_username.trim(),
      sst_password: form.sst_password,
      generate_passwords: form.generate_passwords,
      haproxy_namespace: form.haproxy_namespace.trim(),
      haproxy_service_name: form.haproxy_service_name.trim(),
    });
  };

  const requestDeleteCluster = (cluster: DatabaseCluster) => {
    setDeleteTarget(cluster);
    setDeleteConfirmationName('');
  };

  const confirmDeleteCluster = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) {
      return;
    }
    deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName });
  };

  return (
    <Layout>
      <PageHeader
        title="Database Clusters"
        description="Manage external MariaDB Galera clusters for Kubernetes workloads"
        actions={
          <div className="flex items-center gap-2">
            <Button onClick={() => setShowDeploy((value) => !value)}>
              <Plus className="h-4 w-4" />
              Deploy Database
            </Button>
            <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
              <RefreshCw className="h-4 w-4" />
              Refresh
            </Button>
          </div>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {detailQuery.error && <ErrorMessage error={detailQuery.error} />}
      {manifestQuery.error && <ErrorMessage error={manifestQuery.error} />}
      {provisionMutation.error && <ErrorMessage error={provisionMutation.error} />}
      {bootstrapMutation.error && <ErrorMessage error={bootstrapMutation.error} />}
      {eventsQuery.error && <ErrorMessage error={eventsQuery.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {stopMutation.error && <ErrorMessage error={stopMutation.error} />}
      {lastResult && (
        <Alert variant="success" className="mb-6" title="Database deploy submitted">
          {lastResult.message}
        </Alert>
      )}
      {showDeploy && (
        <DeployDatabaseForm
          form={form}
          regions={regions ?? []}
          images={ubuntuImages}
          imageError={imageError}
          imagesLoading={imagesLoading}
          loading={provisionMutation.isPending}
          onChange={updateField}
          onSubmit={submitDeploy}
        />
      )}

      {clusters && (
        <div className="space-y-6">
          <Card>
            {clusters.length === 0 ? (
              <div className="text-center py-16">
                <Database className="h-9 w-9 text-slate-300 mx-auto mb-3" />
                <p className="text-slate-500 text-sm">No database clusters have been provisioned yet.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Engine</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Region</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Endpoint</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                      <th className="px-6 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {clusters.map((cluster) => (
                      <tr key={cluster.id} className="hover:bg-slate-50 transition-colors">
                        <td className="px-6 py-4 font-medium text-slate-900">{cluster.name}</td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.engine} {cluster.mariadb_version}
                        </td>
                        <td className="px-6 py-4 text-slate-600">{cluster.region_name || 'Legacy / unassigned'}</td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.haproxy_service_name}.{cluster.haproxy_namespace}
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center gap-2">{isDatabaseDeploymentActive(cluster.status) && <Loader2 className="h-4 w-4 animate-spin text-blue-600" />}<StatusBadge status={cluster.status} /></div>
                        </td>
                        <td className="px-6 py-4 text-slate-500">
                          {new Date(cluster.created_at).toLocaleDateString()}
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <Button variant="secondary" size="sm" onClick={() => setSelectedID(cluster.id)}>
                              Details
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setManifestID(cluster.id)}>
                              <FileCode className="h-3.5 w-3.5" />
                              Manifest
                            </Button>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => startMutation.mutate(cluster.id)}
                              loading={startMutation.isPending && startMutation.variables === cluster.id}
                            >
                              <Play className="h-3.5 w-3.5" />
                              Start
                            </Button>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => restartMutation.mutate(cluster.id)}
                              loading={restartMutation.isPending && restartMutation.variables === cluster.id}
                            >
                              <RotateCcw className="h-3.5 w-3.5" />
                              Restart
                            </Button>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => {
                                if (window.confirm(`Stop all database nodes for ${cluster.name}?`)) {
                                  stopMutation.mutate(cluster.id);
                                }
                              }}
                              loading={stopMutation.isPending && stopMutation.variables === cluster.id}
                            >
                              <Power className="h-3.5 w-3.5" />
                              Stop
                            </Button>
                            {cluster.status === 'bootstrap_failed' && (
                              <Button
                                variant="success"
                                size="sm"
                                onClick={() => bootstrapMutation.mutate(cluster.id)}
                                loading={bootstrapMutation.isPending && bootstrapMutation.variables === cluster.id}
                              >
                                Retry Bootstrap
                              </Button>
                            )}
                            {isDeletableDatabaseCluster(cluster.status) && (
                              <Button
                                variant="danger"
                                size="sm"
                                onClick={() => requestDeleteCluster(cluster)}
                                loading={deleteMutation.isPending && deleteMutation.variables?.id === cluster.id}
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                                Delete
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <ClusterDetail cluster={detailQuery.data ?? selectedCluster} loading={detailQuery.isFetching} />
          {isAdmin && <EventLog events={eventsQuery.data ?? []} loading={eventsQuery.isFetching} />}
          {manifestQuery.data && (
            <Card>
              <CardHeader>
                <CardTitle>Kubernetes Manifest</CardTitle>
              </CardHeader>
              <CardContent>
                <pre className="max-h-96 overflow-auto rounded-md bg-slate-950 p-4 text-xs text-slate-100">
                  {manifestQuery.data.manifest}
                </pre>
              </CardContent>
            </Card>
          )}
        </div>
      )}
      <DeleteDatabaseClusterModal
        cluster={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMutation.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDeleteCluster}
      />
    </Layout>
  );
}

function isDeletableDatabaseCluster(status: string) {
  return status === 'provisioned' || status === 'ready' || status === 'applied' || status === 'stopped' || status.endsWith('_failed');
}

function DeleteDatabaseClusterModal({
  cluster,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  cluster: DatabaseCluster | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!cluster && confirmationName === cluster.name;
  return (
    <Modal open={!!cluster} onClose={onClose} title="Delete Database Cluster" maxWidth="lg">
      {cluster && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This deletes infrastructure">
            Terraform destroy will be attempted for the database VM workspace before portal metadata is removed. Kubernetes Service/Endpoints and application DB config are not deleted automatically.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{cluster.name}</span> to confirm deletion.
          </div>
          <Input
            label="Cluster name confirmation"
            value={confirmationName}
            onChange={(event) => onChange(event.target.value)}
            autoFocus
            autoComplete="off"
          />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>
              <Trash2 className="h-4 w-4" />
              Delete Cluster
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

function ClusterDetail({ cluster, loading }: { cluster: DatabaseCluster | null; loading: boolean }) {
  const [credentialsRequested, setCredentialsRequested] = useState(false);
  const [showRoot, setShowRoot] = useState(false);
  const [showApp, setShowApp] = useState(false);
  const credentialsQuery = useQuery({
    queryKey: ['database-cluster-credentials', cluster?.id],
    queryFn: () => getDatabaseClusterCredentials(cluster?.id ?? 0),
    enabled: Boolean(cluster?.id && credentialsRequested),
    staleTime: 0,
  });
  if (!cluster) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Details</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-slate-500">Select a database cluster to inspect placement and generated workspaces.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Details</CardTitle>
      </CardHeader>
      <CardContent>
        {isDatabaseDeploymentActive(cluster.status) && <div className="mb-5 rounded-md border border-blue-200 bg-blue-50 p-4"><div className="flex items-center gap-2 text-sm font-medium text-blue-900"><Loader2 className="h-4 w-4 animate-spin" />{databaseStageLabel(cluster.status)}</div><div className="mt-3 h-1.5 overflow-hidden rounded-full bg-blue-100"><div className="deployment-progress-bar h-full rounded-full bg-blue-600" /></div><p className="mt-2 text-xs text-blue-700">Deployment is continuing automatically.</p></div>}
        {loading && <p className="mb-3 text-xs text-slate-400">Refreshing details...</p>}
        <dl className="space-y-3 text-sm">
          <Field label="Database" value={cluster.database_name} />
          <Field label="Region" value={cluster.region_name || 'Legacy / unassigned'} />
          <Field label="App user" value={cluster.app_username} />
          <Field label="Operating system" value={cluster.cloud_init_image_path || 'Legacy image'} />
          <Field label="Terraform" value={cluster.terraform_work_dir || 'Not generated'} />
          <Field label="Ansible" value={cluster.ansible_work_dir || 'Not generated'} />
        </dl>
        <div className="mt-5 border-t border-slate-100 pt-4">
          <div className="flex items-center justify-between gap-3"><p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Access and credentials</p>{!credentialsRequested && <Button size="sm" variant="secondary" onClick={() => setCredentialsRequested(true)}><Eye className="h-3.5 w-3.5" /> Reveal credentials</Button>}</div>
          {credentialsQuery.error && <div className="mt-3"><ErrorMessage error={credentialsQuery.error} /></div>}
          {credentialsQuery.isFetching && <p className="mt-3 text-sm text-slate-500">Loading credentials securely...</p>}
          {credentialsQuery.data && <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
            <SecretField label="Root password" value={credentialsQuery.data.root_password} shown={showRoot} onToggle={() => setShowRoot((value) => !value)} />
            <SecretField label={`${credentialsQuery.data.app_username} password`} value={credentialsQuery.data.app_password} shown={showApp} onToggle={() => setShowApp((value) => !value)} />
          </div>}
        </div>
        <div className="mt-5 border-t border-slate-100 pt-4">
          <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">Nodes</p>
          <div className="space-y-2">
            {(cluster.nodes ?? []).map((node) => (
              <div key={node.id} className="rounded-md border border-slate-200 px-3 py-2">
                <div className="flex items-center justify-between gap-3">
                  <p className="font-medium text-slate-900">{node.vm_name}</p>
                  <StatusBadge status={node.status} />
                </div>
                <p className="mt-1 text-xs text-slate-500">
                  {node.ip_address} on {node.proxmox_node_name}
                </p>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function SecretField({ label, value, shown, onToggle }: { label: string; value: string; shown: boolean; onToggle: () => void }) {
  return <div className="rounded-md border border-slate-200 p-3"><p className="text-xs font-medium text-slate-500">{label}</p><div className="mt-2 flex items-center gap-2"><code className="min-w-0 flex-1 truncate text-sm text-slate-800">{shown ? value : '••••••••••••••••'}</code><button type="button" className="text-slate-500 hover:text-slate-900" onClick={onToggle} aria-label={shown ? 'Hide password' : 'Show password'}>{shown ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</button><button type="button" className="text-slate-500 hover:text-slate-900" onClick={() => navigator.clipboard.writeText(value)} aria-label="Copy password"><Copy className="h-4 w-4" /></button></div></div>;
}

function isDatabaseDeploymentActive(status: string) {
  return ['validating', 'placing', 'planned', 'provisioning', 'applied', 'waiting_for_nodes', 'bootstrapping', 'configuring_endpoint'].includes(status.toLowerCase());
}

function databaseStageLabel(status: string) {
  switch (status.toLowerCase()) {
    case 'planned': case 'placing': return 'Selecting regional database capacity';
    case 'provisioning': case 'applied': return 'Creating database servers';
    case 'waiting_for_nodes': return 'Waiting for server access';
    case 'bootstrapping': return 'Configuring MariaDB Galera';
    case 'configuring_endpoint': return 'Configuring the database endpoint';
    default: return 'Validating database cluster configuration';
  }
}

function EventLog({ events, loading }: { events: DatabaseClusterEvent[]; loading: boolean }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle>Event Log</CardTitle>
          {loading && <span className="text-xs text-slate-400">Refreshing...</span>}
        </div>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className="text-sm text-slate-500">Select a database cluster to view deploy events and command output.</p>
        ) : (
          <div className="space-y-3">
            {events.map((event) => (
              <div key={event.id} className="rounded-md border border-slate-200">
                <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                  <div>
                    <p className="text-sm font-medium text-slate-900">{event.message}</p>
                    <p className="mt-0.5 text-xs text-slate-500">
                      {event.step} · {new Date(event.created_at).toLocaleString()}
                    </p>
                  </div>
                  <span className={eventLevelClass(event.level)}>{event.level}</span>
                </div>
                {event.output && (
                  <pre className="max-h-72 overflow-auto border-t border-slate-200 bg-slate-950 p-3 text-xs text-slate-100">
                    {event.output}
                  </pre>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function eventLevelClass(level: string) {
  switch (level) {
    case 'success':
      return 'rounded-full bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700';
    case 'warning':
      return 'rounded-full bg-amber-50 px-2 py-1 text-xs font-medium text-amber-700';
    case 'error':
      return 'rounded-full bg-red-50 px-2 py-1 text-xs font-medium text-red-700';
    default:
      return 'rounded-full bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700';
  }
}

function DeployDatabaseForm({
  form,
  regions,
  images,
  imageError,
  imagesLoading,
  loading,
  onChange,
  onSubmit,
}: {
  form: typeof initialForm;
  regions: Region[];
  images: VPSCloudInitImage[];
  imageError: unknown;
  imagesLoading: boolean;
  loading: boolean;
  onChange: (field: keyof typeof initialForm, value: string | boolean) => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const selectedRegion = regions.find((region) => region.id === Number(form.region_id));
  return (
    <Card className="mb-6">
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle>Deploy MariaDB Galera</CardTitle>
            <p className="mt-1 text-sm text-slate-500">Three eligible database hosts and the region subnet are selected automatically.</p>
          </div>
          <Rocket className="h-5 w-5 text-indigo-500" />
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input label="Cluster name" value={form.name} onChange={(e) => onChange('name', e.target.value)} placeholder="pscs-prod-db" required />
            {regions && !regions.some((region) => region.can_deploy) && <Alert variant="info">No deployment regions are available to you. Contact your administrator.</Alert>}
            <Select label="Region" value={form.region_id} onChange={(e) => onChange('region_id', e.target.value)} required>
              <option value="">Select region</option>
              {regions.map((region) => <option key={region.id} value={region.id} disabled={!region.can_deploy || region.nodes.filter((node) => node.enabled).length < 3}>{region.name}{!region.can_deploy ? ` - ${region.deployment_unavailable_reason || 'Not permitted'}` : region.nodes.filter((node) => node.enabled).length < 3 ? ' - Requires 3 nodes' : ''}</option>)}
            </Select>
          </div>
          {selectedRegion && <Alert variant="info">The backend will select 3 of {selectedRegion.nodes.filter((node) => node.enabled).length} enabled region nodes using current availability and free RAM.</Alert>}

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Input label="CPU cores" type="number" min="1" value={form.cpu} onChange={(e) => onChange('cpu', e.target.value)} required />
            <Input label="Memory MB" type="number" min="4096" value={form.memory_mb} onChange={(e) => onChange('memory_mb', e.target.value)} required />
            <Input label="Disk GB" type="number" min="40" value={form.disk_gb} onChange={(e) => onChange('disk_gb', e.target.value)} required />
          </div>

          <Card>
            <CardHeader><CardTitle>Select Operating System</CardTitle></CardHeader>
            <CardContent className="space-y-3">
              {Boolean(imageError) && <ErrorMessage error={imageError} />}
              {images.length === 0 ? <Alert variant="warning" title={imagesLoading ? 'Checking shared images' : 'No shared images'}>{imagesLoading ? 'Synchronizing the OS image catalogue with shared NFS storage.' : 'No supported Ubuntu image is currently available in shared cloud-images import storage.'}</Alert> :
                <><div className="flex items-center gap-2 border-b border-slate-200 pb-2"><DistributionLogo distribution="ubuntu" className="h-6 w-6" /><h3 className="font-semibold text-slate-900">Ubuntu</h3><span className="text-xs text-slate-500">supported database images</span></div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{images.map((image) => {
                    const selected = String(image.id) === form.cloud_init_image_id;
                    return <button key={image.id} type="button" onClick={() => onChange('cloud_init_image_id', String(image.id))} className={`rounded-md border p-4 text-left ${selected ? 'border-indigo-500 bg-indigo-50 ring-2 ring-indigo-100' : 'border-slate-200 hover:bg-slate-50'}`}>
                      <div className="flex gap-3"><DistributionLogo distribution="ubuntu" className="h-6 w-6 shrink-0" /><div className="min-w-0 flex-1"><div className="flex items-center justify-between gap-2"><p className="font-medium text-slate-900">{image.display_name || image.name}</p>{selected && <CheckCircle2 className="h-4 w-4 text-indigo-600" />}</div><p className="mt-1 text-[10px] text-slate-500">{architectureLabel(image.architecture)}</p></div></div>
                    </button>;
                  })}</div></>}
            </CardContent>
          </Card>

          <div className="grid grid-cols-1 gap-4">
            <Input label="MariaDB version" value={form.mariadb_version} onChange={(e) => onChange('mariadb_version', e.target.value)} required />
          </div>

          <div className="rounded-md border border-slate-200 p-4">
            <label className="flex items-start gap-3 text-sm text-slate-700">
              <input className="mt-1" type="checkbox" checked={form.generate_passwords} onChange={(e) => onChange('generate_passwords', e.target.checked)} />
              <span><span className="font-medium text-slate-900">Generate secure database passwords automatically</span><span className="mt-1 block text-xs text-slate-500">Recommended. Root, application and Galera synchronization passwords are generated and stored securely.</span></span>
            </label>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Input label="Database name" value={form.database_name} onChange={(e) => onChange('database_name', e.target.value)} required />
            <Input label="App user" value={form.app_username} onChange={(e) => onChange('app_username', e.target.value)} required />
            <Input label="SST user" value={form.sst_username} onChange={(e) => onChange('sst_username', e.target.value)} required />
          </div>
          {!form.generate_passwords && <div className="grid grid-cols-1 md:grid-cols-2 gap-4"><Input label="Root password" type="password" value={form.root_password} onChange={(e) => onChange('root_password', e.target.value)} required /><Input label="Application password" type="password" value={form.app_password} onChange={(e) => onChange('app_password', e.target.value)} required /></div>}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input label="Kubernetes namespace" value={form.haproxy_namespace} onChange={(e) => onChange('haproxy_namespace', e.target.value)} required />
            <Input label="Database service name" value={form.haproxy_service_name} onChange={(e) => onChange('haproxy_service_name', e.target.value)} required />
          </div>

          {loading && <div className="rounded-md border border-blue-200 bg-blue-50 p-4"><div className="flex items-center gap-2 text-sm font-medium text-blue-900"><Loader2 className="h-4 w-4 animate-spin" />Creating database servers</div><div className="mt-3 h-1.5 overflow-hidden rounded-full bg-blue-100"><div className="deployment-progress-bar h-full rounded-full bg-blue-600" /></div><p className="mt-2 text-xs text-blue-700">Placement, provisioning and Galera setup are running automatically. Keep this page open.</p></div>}

          <div className="flex justify-end">
            <Button type="submit" loading={loading} disabled={loading || !form.cloud_init_image_id || !selectedRegion?.can_deploy || selectedRegion.nodes.filter((node) => node.enabled).length < 3}>
              <Rocket className="h-4 w-4" />
              Deploy Database Cluster
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-1 break-words text-slate-800">{value}</dd>
    </div>
  );
}
