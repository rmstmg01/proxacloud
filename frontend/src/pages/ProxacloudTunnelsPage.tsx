import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FileCode, Plus, RefreshCw, Rocket, Trash2, X } from 'lucide-react';
import {
  createProxacloudTunnel,
  deleteProxacloudTunnel,
  deployProxacloudTunnel,
  getProxacloudTunnelEvents,
  getProxacloudTunnelManifest,
  getProxacloudTunnels,
  updateProxacloudTunnelToken,
  type CreateProxacloudTunnelRequest,
  type ProxacloudTunnel,
} from '../api/proxacloudTunnels';
import { getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select, Textarea } from '../components/ui/Input';
import { PageSpinner } from '../components/ui/Spinner';

const defaultRoute = {
  hostname: 'app.example.com',
  path: '',
  service_namespace: 'traefik',
  service_name: 'traefik',
  service_port: '80',
  service_scheme: 'http',
};

const initialForm = {
  cluster_id: '',
  is_global: false,
  name: 'proxacloud-agent',
  namespace: 'proxacloud-agent',
  deployment_name: 'proxacloud-agent',
  proxacloud_tunnel_id: '',
  tunnel_token: '',
  gateway_url: 'wss://tunnel.proxaops.com/__agent/connect',
  target_url: 'http://traefik.kube-system.svc.cluster.local',
  replicas: '1',
  image: 'ghcr.io/rmstmg01/proxacloud-tunnel/agent:YOUR_COMMIT_SHA',
  image_pull_secret: 'ghcr-pull-secret',
  apply: true,
  routes: [defaultRoute],
};

type FormState = typeof initialForm;

export function ProxacloudTunnelsPage() {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<FormState>(initialForm);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [manifestID, setManifestID] = useState<number | null>(null);
  const [lastResult, setLastResult] = useState('');
  const [deleteName, setDeleteName] = useState('');
  const [showCreateForm, setShowCreateForm] = useState(false);

  const { data: tunnels, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ['proxacloud-tunnels'],
    queryFn: getProxacloudTunnels,
    refetchInterval: 30_000,
  });
  const { data: clusters } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const selectedTunnel = tunnels?.find((tunnel) => tunnel.id === selectedID) ?? null;
  const manifestQuery = useQuery({
    queryKey: ['proxacloud-tunnel-manifest', manifestID],
    queryFn: () => getProxacloudTunnelManifest(manifestID ?? 0),
    enabled: manifestID !== null,
  });
  const eventsQuery = useQuery({
    queryKey: ['proxacloud-tunnel-events', selectedID],
    queryFn: () => getProxacloudTunnelEvents(selectedID ?? 0),
    enabled: selectedID !== null,
    refetchInterval: selectedID !== null ? 5_000 : false,
  });

  const createMutation = useMutation({
    mutationFn: createProxacloudTunnel,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnels'] });
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnel-events', resp.id] });
      setSelectedID(resp.id);
      setManifestID(resp.id);
      setLastResult(resp.status === 'deployed' ? 'Proxacloud agent deployed.' : `Proxacloud tunnel saved with status ${resp.status}.`);
      setForm(initialForm);
      setShowCreateForm(false);
    },
  });

  const deployMutation = useMutation({
    mutationFn: deployProxacloudTunnel,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnels'] });
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnel-events', resp.tunnel_id] });
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnel-manifest', resp.tunnel_id] });
      setSelectedID(resp.tunnel_id);
      setManifestID(resp.tunnel_id);
      setLastResult(`Proxacloud tunnel ${resp.status}.`);
    },
  });

  const tokenMutation = useMutation({
    mutationFn: ({ id, token }: { id: number; token: string }) =>
      updateProxacloudTunnelToken(id, { tunnel_token: token, apply: true }),
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnels'] });
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnel-events', resp.tunnel_id] });
      setLastResult('Proxacloud tunnel token updated and redeploy requested.');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => deleteProxacloudTunnel(id, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['proxacloud-tunnels'] });
      setSelectedID(null);
      setManifestID(null);
      setDeleteName('');
      setLastResult('Proxacloud tunnel record deleted.');
    },
  });

  function update<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  function updateRoute<K extends keyof typeof defaultRoute>(key: K, value: (typeof defaultRoute)[K]) {
    setForm((current) => ({ ...current, routes: [{ ...current.routes[0], [key]: value }] }));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const body: CreateProxacloudTunnelRequest = {
      cluster_id: form.is_global ? 0 : Number(form.cluster_id),
      is_global: form.is_global,
      name: form.name,
      namespace: form.namespace,
      deployment_name: form.deployment_name,
      proxacloud_tunnel_id: form.proxacloud_tunnel_id,
      tunnel_token: form.tunnel_token,
      gateway_url: form.gateway_url,
      target_url: form.target_url,
      replicas: Number(form.replicas),
      image: form.image,
      image_pull_secret: form.image_pull_secret,
      apply: form.apply,
      routes: form.routes.map((route) => ({
        ...route,
        service_port: Number(route.service_port),
        service_scheme: route.service_scheme as 'http' | 'https',
      })),
    };
    createMutation.mutate(body);
  }

  const mutationError = createMutation.error || deployMutation.error || tokenMutation.error || deleteMutation.error;

  return (
    <Layout>
      <PageHeader
        title="Proxacloud Tunnels"
        description="Deploy Proxacloud Tunnel agents into Kubernetes clusters as an alternative tunnel provider."
        actions={
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => setShowCreateForm((current) => !current)}>
              {showCreateForm ? <X className="h-4 w-4" /> : <Plus className="h-4 w-4" />}
              {showCreateForm ? 'Close form' : 'Create tunnel'}
            </Button>
            <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
              <RefreshCw className="h-4 w-4" />
              Refresh
            </Button>
          </div>
        }
      />

      {lastResult && <Alert variant="success" title="Tunnel action completed">{lastResult}</Alert>}
      {error && <ErrorMessage error={error} />}
      {mutationError && <ErrorMessage error={mutationError} />}

      <Card>
        <CardHeader>
          <CardTitle>Proxacloud Tunnel Records</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <PageSpinner />
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-slate-200 text-sm">
                <thead>
                  <tr className="text-left text-slate-500">
                    <th className="py-2 pr-4">Name</th>
                    <th className="py-2 pr-4">Cluster</th>
                    <th className="py-2 pr-4">Tunnel ID</th>
                    <th className="py-2 pr-4">Target</th>
                    <th className="py-2 pr-4">Status</th>
                    <th className="py-2 pr-4">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {tunnels?.map((tunnel) => (
                    <tr key={tunnel.id}>
                      <td className="py-3 pr-4 font-medium text-slate-900">{tunnel.name}</td>
                      <td className="py-3 pr-4 text-slate-600">{tunnel.cluster_name || (tunnel.is_global ? 'Global template' : 'Unattached')}</td>
                      <td className="py-3 pr-4 font-mono text-xs text-slate-600">{tunnel.proxacloud_tunnel_id}</td>
                      <td className="py-3 pr-4 text-slate-600">{tunnel.target_url}</td>
                      <td className="py-3 pr-4"><StatusBadge status={tunnel.status} /></td>
                      <td className="py-3 pr-4">
                        <div className="flex flex-wrap gap-2">
                          <Button size="sm" variant="secondary" onClick={() => { setSelectedID(tunnel.id); setManifestID(tunnel.id); }}>
                            <FileCode className="h-3.5 w-3.5" />
                            Inspect
                          </Button>
                          <Button size="sm" onClick={() => deployMutation.mutate(tunnel.id)} loading={deployMutation.isPending}>
                            Deploy
                          </Button>
                          <Button size="sm" variant="secondary" onClick={() => replaceToken(tunnel)}>
                            Token
                          </Button>
                          <Button size="sm" variant="danger" onClick={() => confirmDelete(tunnel)} loading={deleteMutation.isPending}>
                            <Trash2 className="h-3.5 w-3.5" />
                            Delete
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  {!tunnels?.length && (
                    <tr>
                      <td className="py-6 text-center text-slate-500" colSpan={6}>No Proxacloud tunnels yet.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {showCreateForm && (
        <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
          <Card>
            <CardHeader>
              <CardTitle>Create Proxacloud Tunnel Agent</CardTitle>
              <p className="mt-1 text-sm text-slate-500">Use a tunnel ID and token generated from the Proxacloud Tunnel dashboard.</p>
            </CardHeader>
            <CardContent>
              <form onSubmit={submit} className="grid gap-4">
                <div className="grid gap-4 md:grid-cols-2">
                  <Input label="Name" value={form.name} onChange={(e) => update('name', e.target.value)} required />
                  <Select label="Cluster" value={form.cluster_id} onChange={(e) => update('cluster_id', e.target.value)} disabled={form.is_global} required={!form.is_global}>
                    <option value="">Select cluster</option>
                    {clusters?.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}
                  </Select>
                  <Input label="Namespace" value={form.namespace} onChange={(e) => update('namespace', e.target.value)} required />
                  <Input label="Deployment name" value={form.deployment_name} onChange={(e) => update('deployment_name', e.target.value)} required />
                  <Input label="Tunnel ID" value={form.proxacloud_tunnel_id} onChange={(e) => update('proxacloud_tunnel_id', e.target.value)} required />
                  <Input label="Tunnel token" type="password" value={form.tunnel_token} onChange={(e) => update('tunnel_token', e.target.value)} required />
                  <Input label="Gateway URL" value={form.gateway_url} onChange={(e) => update('gateway_url', e.target.value)} required />
                  <Input label="Target URL" value={form.target_url} onChange={(e) => update('target_url', e.target.value)} required helperText="Usually Traefik or ingress-nginx service DNS inside this cluster." />
                  <Input label="Agent image" value={form.image} onChange={(e) => update('image', e.target.value)} required helperText="Use an immutable commit tag, not latest." />
                  <Input label="Image pull secret" value={form.image_pull_secret} onChange={(e) => update('image_pull_secret', e.target.value)} helperText="Optional. Required for private GHCR packages." />
                  <Input label="Replicas" type="number" min="1" value={form.replicas} onChange={(e) => update('replicas', e.target.value)} />
                </div>

                <div className="rounded-lg border border-slate-200 bg-slate-50 p-4">
                  <h3 className="mb-3 text-sm font-semibold text-slate-900">Route metadata</h3>
                  <div className="grid gap-4 md:grid-cols-2">
                    <Input label="Public hostname" value={form.routes[0].hostname} onChange={(e) => updateRoute('hostname', e.target.value)} required />
                    <Input label="Path" value={form.routes[0].path} onChange={(e) => updateRoute('path', e.target.value)} placeholder="/" />
                    <Input label="Service namespace" value={form.routes[0].service_namespace} onChange={(e) => updateRoute('service_namespace', e.target.value)} />
                    <Input label="Service name" value={form.routes[0].service_name} onChange={(e) => updateRoute('service_name', e.target.value)} />
                    <Input label="Service port" type="number" value={form.routes[0].service_port} onChange={(e) => updateRoute('service_port', e.target.value)} />
                    <Select label="Service scheme" value={form.routes[0].service_scheme} onChange={(e) => updateRoute('service_scheme', e.target.value)}>
                      <option value="http">http</option>
                      <option value="https">https</option>
                    </Select>
                  </div>
                </div>

                <label className="flex items-center gap-2 text-sm font-medium text-slate-700">
                  <input type="checkbox" checked={form.apply} onChange={(e) => update('apply', e.target.checked)} className="h-4 w-4 rounded border-slate-300" />
                  Deploy agent manifest after creating record
                </label>

                <Button type="submit" loading={createMutation.isPending}>
                  <Rocket className="h-4 w-4" />
                  Create Proxacloud Tunnel
                </Button>
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Deployment Notes</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-slate-600">
              <p>Point public DNS for each hostname to the Proxacloud gateway or gateway load balancer.</p>
              <p>The Kubernetes agent connects outbound to the gateway. No public inbound service is created in the private cluster.</p>
              <p>For private GHCR images, create the image pull secret in the target namespace before deployment.</p>
            </CardContent>
          </Card>
        </div>
      )}

      {selectedTunnel && (
        <div className="mt-6 grid gap-6 xl:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Generated Manifest</CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea rows={18} readOnly value={manifestQuery.data?.manifest ?? selectedTunnel.last_manifest ?? ''} />
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Events</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                {eventsQuery.data?.map((event) => (
                  <div key={event.id} className="rounded-md border border-slate-200 p-3">
                    <div className="flex items-center justify-between gap-3">
                      <strong className="text-sm text-slate-900">{event.step}</strong>
                      <span className="text-xs text-slate-500">{new Date(event.created_at).toLocaleString()}</span>
                    </div>
                    <p className="mt-1 text-sm text-slate-600">{event.message}</p>
                    {event.output && <pre className="mt-2 max-h-40 overflow-auto rounded bg-slate-950 p-3 text-xs text-slate-100">{event.output}</pre>}
                  </div>
                ))}
                {!eventsQuery.data?.length && <p className="text-sm text-slate-500">No events for this tunnel yet.</p>}
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </Layout>
  );

  function replaceToken(tunnel: ProxacloudTunnel) {
    const token = window.prompt(`Paste replacement token for ${tunnel.name}`);
    if (!token) return;
    tokenMutation.mutate({ id: tunnel.id, token });
  }

  function confirmDelete(tunnel: ProxacloudTunnel) {
    const name = window.prompt(`Type ${tunnel.name} to delete this Proxacloud tunnel record`, deleteName);
    if (name !== tunnel.name) return;
    setDeleteName(name);
    deleteMutation.mutate({ id: tunnel.id, name });
  }
}
