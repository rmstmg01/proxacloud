import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Activity, BarChart3, Clipboard, Eye, Gauge, Plus, RefreshCw, Server, Trash2 } from 'lucide-react';
import { getClusters } from '../api/clusters';
import {
  applyMonitoringStack,
  createMonitoredCluster,
  deleteMonitoredCluster,
  getMonitoredClusterManifest,
  getMonitoredClusters,
  getMonitoringStackManifest,
  getMonitoringStatus,
  syncMonitoredCluster,
  upsertMonitoringStack,
  type MonitoredCluster,
  type MonitoringClusterMode,
} from '../api/monitoring';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';

const stackDefaults = {
  namespace: 'monitoring',
  grafana_domain: 'grafana.proxaops.com',
  storage_class: '',
  prometheus_storage_size: '50Gi',
  grafana_storage_size: '10Gi',
  retention: '15d',
  expose_grafana: true,
  expose_prometheus: false,
  expose_alertmanager: false,
};

const clusterDefaults = {
  cluster_id: '',
  name: 'pscs-prod',
  environment: 'production',
  mode: 'in_cluster',
  prometheus_url: '',
  grafana_url: 'https://grafana.proxaops.com',
  agent_namespace: 'monitoring',
  remote_write_url: 'http://kube-prometheus-stack-prometheus.monitoring:9090/api/v1/write',
  notes: '',
};

type StackForm = typeof stackDefaults;
type ClusterForm = typeof clusterDefaults;

export function MonitoringPage() {
  const queryClient = useQueryClient();
  const [stackForm, setStackForm] = useState<StackForm>(stackDefaults);
  const [clusterForm, setClusterForm] = useState<ClusterForm>(clusterDefaults);
  const [showAddCluster, setShowAddCluster] = useState(false);
  const [manifestTarget, setManifestTarget] = useState<'stack' | number | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MonitoredCluster | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [lastResult, setLastResult] = useState<string | null>(null);

  const statusQuery = useQuery({
    queryKey: ['monitoring-status'],
    queryFn: getMonitoringStatus,
    refetchInterval: 30_000,
  });
  const clustersQuery = useQuery({
    queryKey: ['monitoring-clusters'],
    queryFn: getMonitoredClusters,
    refetchInterval: 30_000,
  });
  const managedClustersQuery = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const stackManifestQuery = useQuery({
    queryKey: ['monitoring-stack-manifest'],
    queryFn: getMonitoringStackManifest,
    enabled: manifestTarget === 'stack',
  });
  const clusterManifestQuery = useQuery({
    queryKey: ['monitoring-cluster-manifest', manifestTarget],
    queryFn: () => getMonitoredClusterManifest(Number(manifestTarget)),
    enabled: typeof manifestTarget === 'number',
  });

  const stackMutation = useMutation({
    mutationFn: upsertMonitoringStack,
    onSuccess: (stack) => {
      queryClient.invalidateQueries({ queryKey: ['monitoring-status'] });
      queryClient.invalidateQueries({ queryKey: ['monitoring-stack-manifest'] });
      setStackForm({
        namespace: stack.namespace,
        grafana_domain: stack.grafana_domain,
        storage_class: stack.storage_class,
        prometheus_storage_size: stack.prometheus_storage_size,
        grafana_storage_size: stack.grafana_storage_size,
        retention: stack.retention,
        expose_grafana: stack.expose_grafana,
        expose_prometheus: stack.expose_prometheus,
        expose_alertmanager: stack.expose_alertmanager,
      });
      setLastResult('Monitoring stack configuration saved.');
    },
  });

  const applyStackMutation = useMutation({
    mutationFn: applyMonitoringStack,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['monitoring-status'] });
      queryClient.invalidateQueries({ queryKey: ['monitoring-stack-manifest'] });
      setLastResult(`Monitoring stack ${resp.status}.`);
    },
  });

  const createClusterMutation = useMutation({
    mutationFn: createMonitoredCluster,
    onSuccess: (cluster) => {
      queryClient.invalidateQueries({ queryKey: ['monitoring-clusters'] });
      queryClient.invalidateQueries({ queryKey: ['monitoring-status'] });
      setShowAddCluster(false);
      setManifestTarget(cluster.id);
      setLastResult(`Monitoring target ${cluster.name} added.`);
    },
  });

  const syncMutation = useMutation({
    mutationFn: syncMonitoredCluster,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['monitoring-clusters'] });
      queryClient.invalidateQueries({ queryKey: ['monitoring-status'] });
      setLastResult(`Monitoring target ${resp.status}.`);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) =>
      deleteMonitoredCluster(id, confirmationName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['monitoring-clusters'] });
      queryClient.invalidateQueries({ queryKey: ['monitoring-status'] });
      setDeleteTarget(null);
      setDeleteConfirmationName('');
      setLastResult('Monitoring target removed.');
    },
  });

  const stack = statusQuery.data?.stack;
  const clusters = clustersQuery.data ?? [];
  const currentManifest = manifestTarget === 'stack' ? stackManifestQuery.data : clusterManifestQuery.data;

  const submitStack = (event: FormEvent) => {
    event.preventDefault();
    stackMutation.mutate(stackForm);
  };

  const submitCluster = (event: FormEvent) => {
    event.preventDefault();
    createClusterMutation.mutate({
      cluster_id: clusterForm.cluster_id ? Number(clusterForm.cluster_id) : undefined,
      name: clusterForm.name.trim(),
      environment: clusterForm.environment.trim(),
      mode: clusterForm.mode as MonitoringClusterMode,
      prometheus_url: clusterForm.prometheus_url.trim(),
      grafana_url: clusterForm.grafana_url.trim(),
      agent_namespace: clusterForm.agent_namespace.trim(),
      remote_write_url: clusterForm.remote_write_url.trim(),
      notes: clusterForm.notes.trim(),
    });
  };

  if (statusQuery.isLoading || clustersQuery.isLoading) {
    return (
      <Layout>
        <PageSpinner />
      </Layout>
    );
  }

  return (
    <Layout>
      <PageHeader title="Monitoring" description="Central Prometheus and Grafana operations for current and future Kubernetes clusters." />

      <div className="space-y-6">
        {lastResult && (
          <Alert variant="success">
            <div className="flex items-center justify-between gap-4">
              <span>{lastResult}</span>
              <button className="text-xs font-semibold" onClick={() => setLastResult(null)}>
                Dismiss
              </button>
            </div>
          </Alert>
        )}
        <ErrorMessage error={statusQuery.error || clustersQuery.error || stackMutation.error || applyStackMutation.error || createClusterMutation.error || syncMutation.error || deleteMutation.error} />

        <div className="grid gap-4 md:grid-cols-4">
          <MetricCard icon={Gauge} label="Stack" value={stack?.status ?? 'Not configured'} />
          <MetricCard icon={Server} label="Clusters" value={String(statusQuery.data?.cluster_count ?? 0)} />
          <MetricCard icon={Activity} label="Healthy" value={String(statusQuery.data?.healthy_clusters ?? 0)} />
          <MetricCard icon={BarChart3} label="Needs attention" value={String(statusQuery.data?.needs_attention ?? 0)} />
        </div>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between gap-4">
            <div>
              <CardTitle>Monitoring Stack</CardTitle>
              <p className="mt-1 text-sm text-slate-500">Use kube-prometheus-stack as the central monitoring foundation.</p>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setManifestTarget('stack')} disabled={!stack}>
                <Clipboard className="h-4 w-4" />
                Manifests
              </Button>
              <Button type="button" onClick={() => applyStackMutation.mutate()} disabled={!stack} loading={applyStackMutation.isPending}>
                <RefreshCw className="h-4 w-4" />
                Install / Upgrade
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <form className="grid gap-4 lg:grid-cols-3" onSubmit={submitStack}>
              <Input label="Namespace" value={stackForm.namespace} onChange={(e) => setStackForm({ ...stackForm, namespace: e.target.value })} />
              <Input label="Grafana domain" value={stackForm.grafana_domain} onChange={(e) => setStackForm({ ...stackForm, grafana_domain: e.target.value })} />
              <Input label="Storage class" value={stackForm.storage_class} onChange={(e) => setStackForm({ ...stackForm, storage_class: e.target.value })} placeholder="cluster default" />
              <Input label="Prometheus storage" value={stackForm.prometheus_storage_size} onChange={(e) => setStackForm({ ...stackForm, prometheus_storage_size: e.target.value })} />
              <Input label="Grafana storage" value={stackForm.grafana_storage_size} onChange={(e) => setStackForm({ ...stackForm, grafana_storage_size: e.target.value })} />
              <Input label="Retention" value={stackForm.retention} onChange={(e) => setStackForm({ ...stackForm, retention: e.target.value })} />
              <label className="flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={stackForm.expose_grafana} onChange={(e) => setStackForm({ ...stackForm, expose_grafana: e.target.checked })} />
                Expose Grafana through Traefik
              </label>
              <label className="flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={stackForm.expose_prometheus} onChange={(e) => setStackForm({ ...stackForm, expose_prometheus: e.target.checked })} />
                Expose Prometheus UI
              </label>
              <label className="flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={stackForm.expose_alertmanager} onChange={(e) => setStackForm({ ...stackForm, expose_alertmanager: e.target.checked })} />
                Expose Alertmanager UI
              </label>
              <div className="lg:col-span-3 flex justify-end">
                <Button type="submit" loading={stackMutation.isPending}>
                  <RefreshCw className="h-4 w-4" />
                  Save Stack Plan
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between gap-4">
            <div>
              <CardTitle>Monitored Clusters</CardTitle>
              <p className="mt-1 text-sm text-slate-500">Register this cluster now and future clusters with remote-write or federation later.</p>
            </div>
            <Button type="button" onClick={() => setShowAddCluster(true)}>
              <Plus className="h-4 w-4" />
              Add Cluster
            </Button>
          </CardHeader>
          <CardContent>
            {clusters.length === 0 ? (
              <div className="rounded-md border border-dashed border-slate-300 p-6 text-sm text-slate-500">
                No monitored clusters yet. Add the current cluster first, then use remote_write for future clusters.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-slate-200 text-sm">
                  <thead className="bg-slate-50 text-left text-xs uppercase tracking-wide text-slate-500">
                    <tr>
                      <th className="px-4 py-3">Name</th>
                      <th className="px-4 py-3">Environment</th>
                      <th className="px-4 py-3">Mode</th>
                      <th className="px-4 py-3">Status</th>
                      <th className="px-4 py-3">Managed cluster</th>
                      <th className="px-4 py-3 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {clusters.map((cluster) => (
                      <tr key={cluster.id}>
                        <td className="px-4 py-3 font-medium text-slate-900">{cluster.name}</td>
                        <td className="px-4 py-3 text-slate-600">{cluster.environment}</td>
                        <td className="px-4 py-3 text-slate-600">{cluster.mode.replace('_', ' ')}</td>
                        <td className="px-4 py-3"><StatusBadge status={cluster.status} /></td>
                        <td className="px-4 py-3 text-slate-600">{cluster.cluster_name || 'External'}</td>
                        <td className="px-4 py-3">
                          <div className="flex justify-end gap-2">
                            <Button type="button" variant="secondary" size="sm" onClick={() => setManifestTarget(cluster.id)}>
                              <Eye className="h-4 w-4" />
                              View
                            </Button>
                            <Button type="button" variant="secondary" size="sm" onClick={() => syncMutation.mutate(cluster.id)} disabled={!cluster.cluster_id} loading={syncMutation.isPending}>
                              <RefreshCw className="h-4 w-4" />
                              Sync
                            </Button>
                            <Button type="button" variant="ghost" size="sm" onClick={() => setDeleteTarget(cluster)}>
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Modal open={showAddCluster} onClose={() => setShowAddCluster(false)} title="Add Monitored Cluster" maxWidth="lg">
        <form className="space-y-4" onSubmit={submitCluster}>
          <div className="grid gap-4 md:grid-cols-2">
            <Input label="Display name" value={clusterForm.name} onChange={(e) => setClusterForm({ ...clusterForm, name: e.target.value })} required />
            <Input label="Environment" value={clusterForm.environment} onChange={(e) => setClusterForm({ ...clusterForm, environment: e.target.value })} required />
            <Select label="Monitoring mode" value={clusterForm.mode} onChange={(e) => setClusterForm({ ...clusterForm, mode: e.target.value })}>
              <option value="in_cluster">Current managed cluster</option>
              <option value="remote_write">Remote write agent</option>
              <option value="federation">Prometheus federation</option>
            </Select>
            <Select label="Managed cluster link" value={clusterForm.cluster_id} onChange={(e) => setClusterForm({ ...clusterForm, cluster_id: e.target.value })}>
              <option value="">External or not linked</option>
              {managedClustersQuery.data?.map((cluster) => (
                <option key={cluster.id} value={cluster.id}>{cluster.name}</option>
              ))}
            </Select>
            <Input label="Agent namespace" value={clusterForm.agent_namespace} onChange={(e) => setClusterForm({ ...clusterForm, agent_namespace: e.target.value })} />
            <Input label="Grafana URL" value={clusterForm.grafana_url} onChange={(e) => setClusterForm({ ...clusterForm, grafana_url: e.target.value })} />
            <Input label="Prometheus URL" value={clusterForm.prometheus_url} onChange={(e) => setClusterForm({ ...clusterForm, prometheus_url: e.target.value })} />
            <Input label="Remote write URL" value={clusterForm.remote_write_url} onChange={(e) => setClusterForm({ ...clusterForm, remote_write_url: e.target.value })} />
          </div>
          <Textarea label="Notes" value={clusterForm.notes} onChange={(e) => setClusterForm({ ...clusterForm, notes: e.target.value })} rows={3} />
          <Alert variant="info">
            For production multi-cluster monitoring, prefer remote_write agents for future clusters. Direct central scraping is more fragile across networks.
          </Alert>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setShowAddCluster(false)}>Cancel</Button>
            <Button type="submit" loading={createClusterMutation.isPending}>Add Cluster</Button>
          </div>
        </form>
      </Modal>

      <Modal open={manifestTarget !== null} onClose={() => setManifestTarget(null)} title="Monitoring Manifests" maxWidth="6xl">
        <div className="space-y-4">
          {currentManifest?.commands && <CodeBlock title="Commands" value={currentManifest.commands} />}
          {currentManifest?.values_yaml && <CodeBlock title="Helm values" value={currentManifest.values_yaml} />}
          {currentManifest?.manifest && <CodeBlock title="Kubernetes YAML / agent values" value={currentManifest.manifest} />}
          {(stackManifestQuery.isLoading || clusterManifestQuery.isLoading) && <PageSpinner />}
          <div className="flex justify-end">
            <Button type="button" variant="secondary" onClick={() => setManifestTarget(null)}>Close</Button>
          </div>
        </div>
      </Modal>

      <Modal open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title="Remove Monitoring Target">
        <div className="space-y-4">
          <Alert variant="warning">Type the target name to remove it from monitoring inventory.</Alert>
          <Input label="Confirmation name" value={deleteConfirmationName} onChange={(e) => setDeleteConfirmationName(e.target.value)} placeholder={deleteTarget?.name} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button
              type="button"
              variant="danger"
              loading={deleteMutation.isPending}
              disabled={!deleteTarget || deleteConfirmationName !== deleteTarget.name}
              onClick={() => deleteTarget && deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName })}
            >
              Remove
            </Button>
          </div>
        </div>
      </Modal>
    </Layout>
  );
}

function MetricCard({ icon: Icon, label, value }: { icon: typeof Gauge; label: string; value: string }) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3">
        <div className="flex h-10 w-10 items-center justify-center rounded-md bg-slate-100 text-slate-700">
          <Icon className="h-5 w-5" />
        </div>
        <div>
          <p className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</p>
          <p className="mt-1 text-lg font-semibold text-slate-900">{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}

function CodeBlock({ title, value }: { title: string; value: string }) {
  return (
    <div>
      <h3 className="mb-2 text-sm font-semibold text-slate-900">{title}</h3>
      <pre className="max-h-96 overflow-auto rounded-md bg-slate-950 p-4 text-xs text-slate-100">
        <code>{value}</code>
      </pre>
    </div>
  );
}
