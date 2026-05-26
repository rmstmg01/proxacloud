import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Cloud, FileCode, Plus, RefreshCw, Rocket, Trash2 } from 'lucide-react';
import {
  createCloudflareTunnel,
  deleteCloudflareTunnel,
  deployCloudflareTunnel,
  getCloudflareTunnelEvents,
  getCloudflareTunnelManifest,
  getCloudflareTunnels,
  type CloudflareTunnel,
  type CloudflareTunnelEvent,
  type CloudflareTunnelProvisioningMode,
  type CloudflareTunnelRoutingMode,
} from '../api/cloudflareTunnels';
import { getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';
import type { ClusterSummary } from '../types';

const defaultRoutes = [
  {
    hostname: 'proxaops.com',
    path: '',
    service_namespace: 'pscs',
    service_name: 'website',
    service_port: '80',
    service_scheme: 'http',
  },
  {
    hostname: 'www.proxaops.com',
    path: '',
    service_namespace: 'pscs',
    service_name: 'website',
    service_port: '80',
    service_scheme: 'http',
  },
  {
    hostname: 'admin.proxaops.com',
    path: '',
    service_namespace: 'pscs',
    service_name: 'frontend',
    service_port: '80',
    service_scheme: 'http',
  },
  {
    hostname: 'api.proxaops.com',
    path: '',
    service_namespace: 'pscs',
    service_name: 'backend',
    service_port: '8080',
    service_scheme: 'http',
  },
];

const initialForm = {
  cluster_id: '',
  name: 'pscs-cloudflare',
  namespace: 'cloudflare-tunnel',
  deployment_name: 'cloudflared',
  routing_mode: 'traefik',
  provisioning_mode: 'manual_token',
  tunnel_token: '',
  cloudflare_api_token: '',
  cloudflare_account_id: '',
  cloudflare_zone_id: '',
  cloudflare_tunnel_id: '',
  cloudflare_tunnel_name: '',
  replicas: '3',
  image: 'cloudflare/cloudflared:latest',
  ingress_namespace: 'traefik',
  ingress_service_name: 'traefik',
  ingress_service_port: '80',
  ingress_service_scheme: 'http',
  configure_dns: false,
  apply: true,
  routes: defaultRoutes,
};

type TunnelForm = typeof initialForm;
type RouteForm = (typeof defaultRoutes)[number];

export function CloudflareTunnelsPage() {
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [manifestID, setManifestID] = useState<number | null>(null);
  const [form, setForm] = useState<TunnelForm>(initialForm);
  const [deleteTarget, setDeleteTarget] = useState<CloudflareTunnel | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [lastResult, setLastResult] = useState<string | null>(null);

  const {
    data: tunnels,
    isLoading,
    isFetching,
    error,
    refetch,
  } = useQuery({
    queryKey: ['cloudflare-tunnels'],
    queryFn: getCloudflareTunnels,
    refetchInterval: 30_000,
  });
  const { data: clusters } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const selectedTunnel = tunnels?.find((tunnel) => tunnel.id === selectedID) ?? null;
  const eventsQuery = useQuery({
    queryKey: ['cloudflare-tunnel-events', selectedID],
    queryFn: () => getCloudflareTunnelEvents(selectedID ?? 0),
    enabled: selectedID !== null,
    refetchInterval: selectedID !== null ? 5_000 : false,
  });
  const manifestQuery = useQuery({
    queryKey: ['cloudflare-tunnel-manifest', manifestID],
    queryFn: () => getCloudflareTunnelManifest(manifestID ?? 0),
    enabled: manifestID !== null,
  });

  const createMutation = useMutation({
    mutationFn: createCloudflareTunnel,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnels'] });
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnel-events', resp.id] });
      setSelectedID(resp.id);
      setManifestID(resp.id);
      setLastResult(resp.status === 'deployed' ? 'Cloudflare tunnel deployed.' : `Cloudflare tunnel saved with status ${resp.status}.`);
      setShowCreate(false);
      setForm(initialForm);
    },
  });

  const deployMutation = useMutation({
    mutationFn: deployCloudflareTunnel,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnels'] });
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnel-events', resp.tunnel_id] });
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnel-manifest', resp.tunnel_id] });
      setSelectedID(resp.tunnel_id);
      setManifestID(resp.tunnel_id);
      setLastResult(`Cloudflare tunnel ${resp.status}.`);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) => deleteCloudflareTunnel(id, confirmationName),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['cloudflare-tunnels'] });
      if (selectedID === resp.id) setSelectedID(null);
      if (manifestID === resp.id) setManifestID(null);
      setDeleteTarget(null);
      setDeleteConfirmationName('');
      setLastResult('Cloudflare tunnel metadata deleted.');
    },
  });

  const updateField = (field: keyof TunnelForm, value: string | boolean) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const updateRoute = (index: number, field: keyof RouteForm, value: string) => {
    setForm((current) => ({
      ...current,
      routes: current.routes.map((route, i) => (i === index ? { ...route, [field]: value } : route)),
    }));
  };

  const addRoute = () => {
    setForm((current) => ({
      ...current,
      routes: [
        ...current.routes,
        {
          hostname: '',
          path: '',
          service_namespace: 'pscs',
          service_name: '',
          service_port: '80',
          service_scheme: 'http',
        },
      ],
    }));
  };

  const removeRoute = (index: number) => {
    setForm((current) => ({
      ...current,
      routes: current.routes.filter((_, i) => i !== index),
    }));
  };

  const submitCreate = (event: FormEvent) => {
    event.preventDefault();
    createMutation.mutate({
      cluster_id: Number(form.cluster_id),
      name: form.name.trim(),
      namespace: form.namespace.trim(),
      deployment_name: form.deployment_name.trim(),
      routing_mode: form.routing_mode as CloudflareTunnelRoutingMode,
      provisioning_mode: form.provisioning_mode as CloudflareTunnelProvisioningMode,
      tunnel_token: form.tunnel_token,
      cloudflare_api_token: form.cloudflare_api_token,
      cloudflare_account_id: form.cloudflare_account_id.trim(),
      cloudflare_zone_id: form.cloudflare_zone_id.trim(),
      cloudflare_tunnel_id: form.cloudflare_tunnel_id.trim(),
      cloudflare_tunnel_name: form.cloudflare_tunnel_name.trim(),
      replicas: Number(form.replicas),
      image: form.image.trim(),
      ingress_namespace: form.ingress_namespace.trim(),
      ingress_service_name: form.ingress_service_name.trim(),
      ingress_service_port: Number(form.ingress_service_port),
      ingress_service_scheme: form.ingress_service_scheme as 'http' | 'https',
      configure_dns: form.configure_dns,
      apply: form.apply,
      routes: form.routes.map((route) => ({
        hostname: route.hostname.trim(),
        path: route.path.trim(),
        service_namespace: route.service_namespace.trim(),
        service_name: route.service_name.trim(),
        service_port: Number(route.service_port),
        service_scheme: route.service_scheme as 'http' | 'https',
      })),
    });
  };

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName });
  };

  return (
    <Layout>
      <PageHeader
        title="Cloudflare Tunnels"
        description="Deploy high availability cloudflared connectors for Kubernetes services"
        actions={
          <div className="flex items-center gap-2">
            <Button onClick={() => setShowCreate((value) => !value)}>
              <Plus className="h-4 w-4" />
              Deploy Tunnel
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
      {createMutation.error && <ErrorMessage error={createMutation.error} />}
      {deployMutation.error && <ErrorMessage error={deployMutation.error} />}
      {eventsQuery.error && <ErrorMessage error={eventsQuery.error} />}
      {manifestQuery.error && <ErrorMessage error={manifestQuery.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}
      {lastResult && (
        <Alert variant="success" className="mb-6">
          {lastResult}
        </Alert>
      )}

      {showCreate && (
        <DeployTunnelForm
          form={form}
          clusters={clusters ?? []}
          loading={createMutation.isPending}
          onChange={updateField}
          onRouteChange={updateRoute}
          onAddRoute={addRoute}
          onRemoveRoute={removeRoute}
          onSubmit={submitCreate}
        />
      )}

      {tunnels && (
        <div className="space-y-6">
          <Card>
            {tunnels.length === 0 ? (
              <div className="text-center py-16">
                <Cloud className="h-9 w-9 text-slate-300 mx-auto mb-3" />
                <p className="text-slate-500 text-sm">No Cloudflare tunnels have been deployed yet.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Cluster</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Mode</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Replicas</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                      <th className="px-6 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {tunnels.map((tunnel) => (
                      <tr key={tunnel.id} className="hover:bg-slate-50 transition-colors">
                        <td className="px-6 py-4">
                          <p className="font-medium text-slate-900">{tunnel.name}</p>
                          <p className="mt-0.5 text-xs text-slate-500">{tunnel.namespace}/{tunnel.deployment_name}</p>
                        </td>
                        <td className="px-6 py-4 text-slate-600">{tunnel.cluster_name || tunnel.cluster_id}</td>
                        <td className="px-6 py-4 text-slate-600">
                          {tunnel.routing_mode === 'traefik' ? 'Traefik' : 'Direct'} / {tunnel.provisioning_mode === 'cloudflare_api' ? 'API' : 'Manual'}
                        </td>
                        <td className="px-6 py-4 text-slate-600">{tunnel.replicas}</td>
                        <td className="px-6 py-4">
                          <StatusBadge status={tunnel.status} />
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <Button variant="secondary" size="sm" onClick={() => setSelectedID(tunnel.id)}>
                              Details
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setManifestID(tunnel.id)}>
                              <FileCode className="h-3.5 w-3.5" />
                              Manifest
                            </Button>
                            <Button
                              variant="success"
                              size="sm"
                              onClick={() => deployMutation.mutate(tunnel.id)}
                              loading={deployMutation.isPending && deployMutation.variables === tunnel.id}
                            >
                              <Rocket className="h-3.5 w-3.5" />
                              Redeploy
                            </Button>
                            <Button
                              variant="danger"
                              size="sm"
                              onClick={() => {
                                setDeleteTarget(tunnel);
                                setDeleteConfirmationName('');
                              }}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                              Delete
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <TunnelDetail tunnel={selectedTunnel} />
          <EventLog events={eventsQuery.data ?? []} loading={eventsQuery.isFetching} />
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

      <DeleteTunnelModal
        tunnel={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMutation.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDelete}
      />
    </Layout>
  );
}

function DeployTunnelForm({
  form,
  clusters,
  loading,
  onChange,
  onRouteChange,
  onAddRoute,
  onRemoveRoute,
  onSubmit,
}: {
  form: TunnelForm;
  clusters: ClusterSummary[];
  loading: boolean;
  onChange: (field: keyof TunnelForm, value: string | boolean) => void;
  onRouteChange: (index: number, field: keyof RouteForm, value: string) => void;
  onAddRoute: () => void;
  onRemoveRoute: (index: number) => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const apiMode = form.provisioning_mode === 'cloudflare_api';
  const directMode = form.routing_mode === 'direct';
  return (
    <Card className="mb-6">
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle>Deploy Cloudflare Tunnel</CardTitle>
            <p className="mt-1 text-sm text-slate-500">Run multiple cloudflared replicas and publish selected hostnames.</p>
          </div>
          <Cloud className="h-5 w-5 text-indigo-500" />
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Select label="Cluster" value={form.cluster_id} onChange={(e) => onChange('cluster_id', e.target.value)} required>
              <option value="">Select cluster</option>
              {clusters.map((cluster) => (
                <option key={cluster.id} value={cluster.id}>
                  {cluster.name} ({cluster.status})
                </option>
              ))}
            </Select>
            <Input label="Tunnel name" value={form.name} onChange={(e) => onChange('name', e.target.value)} required />
            <Input label="Replicas" type="number" min="2" value={form.replicas} onChange={(e) => onChange('replicas', e.target.value)} required />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
            <Input label="Namespace" value={form.namespace} onChange={(e) => onChange('namespace', e.target.value)} required />
            <Input label="Deployment" value={form.deployment_name} onChange={(e) => onChange('deployment_name', e.target.value)} required />
            <Select label="Routing mode" value={form.routing_mode} onChange={(e) => onChange('routing_mode', e.target.value)} required>
              <option value="traefik">Traefik ingress</option>
              <option value="direct">Direct services</option>
            </Select>
            <Select label="Provisioning" value={form.provisioning_mode} onChange={(e) => onChange('provisioning_mode', e.target.value)} required>
              <option value="manual_token">Manual token</option>
              <option value="cloudflare_api">Cloudflare API</option>
            </Select>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
            <Input label="Image" value={form.image} onChange={(e) => onChange('image', e.target.value)} required />
            <Select label="Ingress scheme" value={form.ingress_service_scheme} onChange={(e) => onChange('ingress_service_scheme', e.target.value)} disabled={directMode}>
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
            </Select>
            <Input label="Ingress namespace" value={form.ingress_namespace} onChange={(e) => onChange('ingress_namespace', e.target.value)} disabled={directMode} required />
            <Input label="Ingress service" value={form.ingress_service_name} onChange={(e) => onChange('ingress_service_name', e.target.value)} disabled={directMode} required />
            <Input label="Ingress port" type="number" min="1" value={form.ingress_service_port} onChange={(e) => onChange('ingress_service_port', e.target.value)} disabled={directMode} required />
          </div>

          {apiMode ? (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <Input label="Cloudflare account ID" value={form.cloudflare_account_id} onChange={(e) => onChange('cloudflare_account_id', e.target.value)} required />
              <Input label="Cloudflare tunnel ID" value={form.cloudflare_tunnel_id} onChange={(e) => onChange('cloudflare_tunnel_id', e.target.value)} placeholder="Leave empty to create" />
              <Input label="Cloudflare tunnel name" value={form.cloudflare_tunnel_name} onChange={(e) => onChange('cloudflare_tunnel_name', e.target.value)} placeholder="Defaults to tunnel name" />
              <Input label="Cloudflare zone ID" value={form.cloudflare_zone_id} onChange={(e) => onChange('cloudflare_zone_id', e.target.value)} required={form.configure_dns} />
              <Input label="Cloudflare API token" type="password" value={form.cloudflare_api_token} onChange={(e) => onChange('cloudflare_api_token', e.target.value)} required />
            </div>
          ) : (
            <Textarea
              label="Tunnel token"
              value={form.tunnel_token}
              onChange={(e) => onChange('tunnel_token', e.target.value)}
              rows={4}
              required
            />
          )}

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <p className="text-sm font-semibold text-slate-900">Hostname routes</p>
              <Button type="button" variant="secondary" size="sm" onClick={onAddRoute}>
                <Plus className="h-3.5 w-3.5" />
                Add Route
              </Button>
            </div>
            <div className="space-y-3">
              {form.routes.map((route, index) => (
                <div key={`${route.hostname}-${index}`} className="grid grid-cols-1 gap-3 rounded-md border border-slate-200 p-3 md:grid-cols-12">
                  <Input
                    label="Hostname"
                    value={route.hostname}
                    onChange={(e) => onRouteChange(index, 'hostname', e.target.value)}
                    className="md:col-span-3"
                    required
                  />
                  <Input label="Path" value={route.path} onChange={(e) => onRouteChange(index, 'path', e.target.value)} className="md:col-span-1" />
                  <Select label="Scheme" value={route.service_scheme} onChange={(e) => onRouteChange(index, 'service_scheme', e.target.value)} disabled={!directMode} className="md:col-span-1">
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                  </Select>
                  <Input label="Service ns" value={route.service_namespace} onChange={(e) => onRouteChange(index, 'service_namespace', e.target.value)} disabled={!directMode} className="md:col-span-2" required />
                  <Input label="Service" value={route.service_name} onChange={(e) => onRouteChange(index, 'service_name', e.target.value)} disabled={!directMode} className="md:col-span-2" required />
                  <Input label="Port" type="number" min="1" value={route.service_port} onChange={(e) => onRouteChange(index, 'service_port', e.target.value)} disabled={!directMode} className="md:col-span-1" required />
                  <div className="flex items-end md:col-span-2">
                    <Button type="button" variant="danger" size="sm" onClick={() => onRemoveRoute(index)} disabled={form.routes.length === 1}>
                      <Trash2 className="h-3.5 w-3.5" />
                      Remove
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="flex flex-wrap gap-4 border-t border-slate-100 pt-4">
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input type="checkbox" checked={form.apply} onChange={(e) => onChange('apply', e.target.checked)} />
              Deploy to Kubernetes now
            </label>
            {apiMode && (
              <label className="flex items-center gap-2 text-sm text-slate-700">
                <input type="checkbox" checked={form.configure_dns} onChange={(e) => onChange('configure_dns', e.target.checked)} />
                Create Cloudflare DNS records
              </label>
            )}
          </div>

          <div className="flex justify-end">
            <Button type="submit" loading={loading} disabled={loading}>
              <Rocket className="h-4 w-4" />
              Deploy Tunnel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function TunnelDetail({ tunnel }: { tunnel: CloudflareTunnel | null }) {
  if (!tunnel) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Details</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-slate-500">Select a Cloudflare tunnel to inspect routes and deployment state.</p>
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
        <dl className="grid grid-cols-1 gap-3 text-sm md:grid-cols-3">
          <Field label="Cloudflare tunnel ID" value={tunnel.cloudflare_tunnel_id || 'Not recorded'} />
          <Field label="Token" value={tunnel.has_tunnel_token ? 'Stored encrypted' : 'Missing'} />
          <Field label="Last applied" value={tunnel.last_applied_at ? new Date(tunnel.last_applied_at).toLocaleString() : 'Not applied'} />
        </dl>
        <div className="mt-5 border-t border-slate-100 pt-4">
          <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">Routes</p>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200">
                  <th className="py-2 text-left font-medium text-slate-600">Hostname</th>
                  <th className="py-2 text-left font-medium text-slate-600">Target</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {tunnel.routes.map((route) => (
                  <tr key={route.id}>
                    <td className="py-2 pr-4 text-slate-900">{route.hostname}{route.path}</td>
                    <td className="py-2 text-slate-600">
                      {tunnel.routing_mode === 'traefik'
                        ? `${tunnel.ingress_service_scheme}://${tunnel.ingress_service_name}.${tunnel.ingress_namespace}.svc.cluster.local:${tunnel.ingress_service_port}`
                        : `${route.service_scheme}://${route.service_name}.${route.service_namespace}.svc.cluster.local:${route.service_port}`}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function EventLog({ events, loading }: { events: CloudflareTunnelEvent[]; loading: boolean }) {
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
          <p className="text-sm text-slate-500">Select a Cloudflare tunnel to view deploy events and command output.</p>
        ) : (
          <div className="space-y-3">
            {events.map((event) => (
              <div key={event.id} className="rounded-md border border-slate-200">
                <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                  <div>
                    <p className="text-sm font-medium text-slate-900">{event.message}</p>
                    <p className="mt-0.5 text-xs text-slate-500">
                      {event.step} - {new Date(event.created_at).toLocaleString()}
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

function DeleteTunnelModal({
  tunnel,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  tunnel: CloudflareTunnel | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!tunnel && confirmationName === tunnel.name;
  return (
    <Modal open={!!tunnel} onClose={onClose} title="Delete Cloudflare Tunnel" maxWidth="lg">
      {tunnel && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="Kubernetes resources are left in place">
            This removes portal metadata and encrypted tokens. Redeploy or delete the Kubernetes resources separately if needed.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{tunnel.name}</span> to confirm deletion.
          </div>
          <Input label="Tunnel name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>
              <Trash2 className="h-4 w-4" />
              Delete Tunnel
            </Button>
          </div>
        </form>
      )}
    </Modal>
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

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-1 break-words text-slate-800">{value}</dd>
    </div>
  );
}
