import { useState, useEffect, type FormEvent } from 'react';
import { useParams, Link, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import {
  getCluster,
  getClusterRuns,
  getLatestClusterOperation,
  getClusterOperationEvents,
  bootstrapCluster,
  scaleCluster,
  upgradeCluster,
  replaceNode,
  rotateKubeconfig,
  deployMetalLB,
  getMetalLB,
  deployObservability,
  getObservability,
  createEtcdSnapshot,
  getEtcdSnapshots,
  restoreEtcdSnapshot,
  getClusterNodes,
  getBootstrapConfig,
  getBootstrapKeys,
} from '../api/clusters';
import { getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Input, Textarea, Select } from '../components/ui/Input';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import { Modal } from '../components/ui/Modal';

type Tab = 'overview' | 'runs' | 'bootstrap' | 'day2' | 'metallb' | 'observability' | 'etcd';

const TABS: { key: Tab; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'runs', label: 'Run History' },
  { key: 'bootstrap', label: 'Bootstrap' },
  { key: 'day2', label: 'Day-2 Ops' },
  { key: 'metallb', label: 'MetalLB' },
  { key: 'observability', label: 'Observability' },
  { key: 'etcd', label: 'etcd Backup' },
];

function ipv4ToInt(ip: string): number | null {
  const parts = ip.split('.').map((p) => Number(p));
  if (parts.length !== 4 || parts.some((p) => !Number.isInteger(p) || p < 0 || p > 255)) return null;
  return (((parts[0] << 24) >>> 0) + (parts[1] << 16) + (parts[2] << 8) + parts[3]) >>> 0;
}

function intToIPv4(value: number): string {
  return [
    (value >>> 24) & 255,
    (value >>> 16) & 255,
    (value >>> 8) & 255,
    value & 255,
  ].join('.');
}

function suggestVip(ips: string[]): string {
  const nums = ips.map(ipv4ToInt).filter((n): n is number => n !== null);
  if (nums.length === 0) return '';
  let candidate = Math.max(...nums) + 1;
  while (candidate < 0xffffffff) {
    const lastOctet = candidate & 255;
    if (lastOctet !== 0 && lastOctet !== 255) return intToIPv4(candidate);
    candidate += 1;
  }
  return '';
}

// ─── Sub-tabs ────────────────────────────────────────────────────────────────

function OverviewTab({ id }: { id: number }) {
  const { data: cluster } = useQuery({ queryKey: ['cluster', id], queryFn: () => getCluster(id) });
  if (!cluster) return <PageSpinner />;
  const fields: [string, string][] = [
    ['Name', cluster.name],
    ['Status', cluster.status],
    ['Topology', cluster.topology_type],
    ['Kubernetes Version', cluster.kubernetes_version],
    ['Control Planes', String(cluster.control_plane_count)],
    ['Workers', String(cluster.worker_count)],
    ['Network Profile ID', String(cluster.network_profile_id)],
    ['Created', new Date(cluster.created_at).toLocaleString()],
    ['Updated', new Date(cluster.updated_at).toLocaleString()],
  ];
  return (
    <Card>
      <CardHeader><CardTitle>Cluster Details</CardTitle></CardHeader>
      <div className="divide-y divide-slate-100">
        {fields.map(([label, value]) => (
          <div key={label} className="px-6 py-3 flex items-center">
            <span className="w-48 text-sm text-slate-500 font-medium flex-shrink-0">{label}</span>
            {label === 'Status' ? (
              <StatusBadge status={value} />
            ) : (
              <span className="text-sm text-slate-900">{value}</span>
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}

function RunsTab({ id }: { id: number }) {
  const { data: operation } = useQuery({
    queryKey: ['cluster-operation-latest', id],
    queryFn: async () => {
      try {
        return await getLatestClusterOperation(id);
      } catch (error: any) {
        if (error?.response?.status === 404) {
          return null;
        }
        throw error;
      }
    },
    retry: false,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2500 : false),
  });

  const { data: events } = useQuery({
    queryKey: ['cluster-operation-events', id, operation?.id ?? 0],
    queryFn: () => getClusterOperationEvents(id, operation?.id, 0, 200),
    enabled: Boolean(operation?.id),
    refetchInterval: operation?.status === 'running' ? 2500 : false,
  });

  const { data: runs, isLoading } = useQuery({ queryKey: ['cluster-runs', id], queryFn: () => getClusterRuns(id) });
  if (isLoading) return <PageSpinner />;

  const progressPercent = Math.max(0, Math.min(100, Number(operation?.progress_percent ?? 0)));

  return (
    <div className="space-y-6">
      {operation && (
        <Card>
          <CardHeader><CardTitle>Live Operation Status</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center gap-3">
              <StatusBadge status={operation.status} />
              <span className="text-sm text-slate-600">Action: {operation.action}</span>
              <span className="text-sm text-slate-600">Step: {operation.current_step || 'pending'}</span>
            </div>
            <div>
              <div className="h-2 w-full rounded bg-slate-200 overflow-hidden">
                <div className={`h-full transition-all duration-500 ${operation.status === 'failed' ? 'bg-rose-500' : 'bg-emerald-500'}`} style={{ width: `${progressPercent}%` }} />
              </div>
              <p className="mt-1 text-xs text-slate-500">{progressPercent}% complete</p>
            </div>
            {operation.error_summary && (
              <Alert variant="error" title="Operation failed">
                <p className="text-sm whitespace-pre-wrap break-words">{operation.error_summary}</p>
              </Alert>
            )}
            <div className="text-xs text-slate-500 flex flex-wrap gap-x-5 gap-y-1">
              <span>Started: {new Date(operation.started_at).toLocaleString()}</span>
              <span>Updated: {new Date(operation.updated_at).toLocaleString()}</span>
              <span>Finished: {operation.finished_at ? new Date(operation.finished_at).toLocaleString() : 'in progress'}</span>
            </div>
          </CardContent>
        </Card>
      )}

      {operation && events && events.length > 0 && (
        <Card>
          <CardHeader><CardTitle>Operation Timeline</CardTitle></CardHeader>
          <CardContent>
            <div className="max-h-80 overflow-auto rounded-md border border-slate-200 divide-y divide-slate-100">
              {events.map((event) => (
                <div key={event.id} className="px-4 py-3 text-sm">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`font-medium ${event.level === 'error' ? 'text-rose-600' : event.level === 'warn' ? 'text-amber-600' : 'text-slate-700'}`}>
                      [{event.level.toUpperCase()}]
                    </span>
                    <span className="text-slate-700">{event.step}</span>
                    <span className="text-xs text-slate-400">{new Date(event.created_at).toLocaleString()}</span>
                  </div>
                  <p className="mt-1 text-slate-600 whitespace-pre-wrap break-words">{event.message}</p>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle>Provision Runs</CardTitle></CardHeader>
        {!runs || runs.length === 0 ? (
          <CardContent><p className="text-sm text-slate-400">No runs yet.</p></CardContent>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 bg-slate-50">
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Action</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Started</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Finished</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Work Dir</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {runs.map((r) => (
                  <tr key={r.id}>
                    <td className="px-6 py-3 font-medium text-slate-900">{r.action}</td>
                    <td className="px-6 py-3"><StatusBadge status={r.status} /></td>
                    <td className="px-6 py-3 text-slate-500">{new Date(r.created_at).toLocaleString()}</td>
                    <td className="px-6 py-3 text-slate-500">{r.finished_at ? new Date(r.finished_at).toLocaleString() : '—'}</td>
                    <td className="px-6 py-3 text-slate-500 font-mono text-xs truncate max-w-xs">{r.work_dir}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

function BootstrapTab({ id }: { id: number }) {
  const [form, setForm] = useState({
    ssh_user: 'ubuntu',
    ssh_key_pair_id: 0,
    ssh_private_key: '',
    ssh_key_id: 0,
    ssh_key_name: 'default',
    deploy_kube_vip: true,
    control_plane_vip: '',
    pod_cidr: '10.244.0.0/16',
    service_cidr: '10.96.0.0/12',
    cni: 'calico',
    node_ips_raw: '',
    apply: false,
  });
  const mutation = useMutation({ mutationFn: (body: Parameters<typeof bootstrapCluster>[1]) => bootstrapCluster(id, body) });

  const nodesQuery = useQuery({
    queryKey: ['cluster-nodes', id],
    queryFn: () => getClusterNodes(id),
  });

  const savedBootstrapQuery = useQuery({
    queryKey: ['cluster-bootstrap-config', id],
    queryFn: () => getBootstrapConfig(id),
    retry: false,
  });

  const savedKeysQuery = useQuery({
    queryKey: ['cluster-bootstrap-keys', id],
    queryFn: () => getBootstrapKeys(id),
  });

  const globalKeysQuery = useQuery({
    queryKey: ['ssh-keys'],
    queryFn: getSSHKeys,
  });

  useEffect(() => {
    if (!nodesQuery.data || nodesQuery.data.length === 0) return;
    const lines = nodesQuery.data
      .filter((n) => n.ip_address)
      .map((n) => `${n.vm_name} ${n.ip_address}`);
    if (lines.length === 0) return;
    const vipSuggestion = suggestVip(nodesQuery.data.map((n) => n.ip_address).filter(Boolean));
    setForm((f) => {
      if (f.node_ips_raw && f.control_plane_vip) return f; // don't overwrite user edits
      return {
        ...f,
        node_ips_raw: f.node_ips_raw || lines.join('\n'),
        control_plane_vip: f.control_plane_vip || vipSuggestion,
      };
    });
  }, [nodesQuery.data]);

  useEffect(() => {
    if (!savedBootstrapQuery.data) return;
    setForm((f) => ({
      ...f,
      ssh_user: f.ssh_user || savedBootstrapQuery.data.ssh_user,
      ssh_key_id: f.ssh_key_id || Number(savedBootstrapQuery.data.last_ssh_key_id || 0),
    }));
  }, [savedBootstrapQuery.data]);

  const useSavedSSH = () => {
    if (!savedBootstrapQuery.data) return;
    setForm((f) => ({
      ...f,
      ssh_user: savedBootstrapQuery.data!.ssh_user,
      ssh_key_id: Number(savedBootstrapQuery.data!.last_ssh_key_id || 0),
      ssh_private_key: '',
    }));
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const node_ips: Record<string, string> = {};
    for (const line of form.node_ips_raw.split('\n')) {
      const [name, ip] = line.trim().split(/\s+/);
      if (name && ip) node_ips[name] = ip;
    }
    await mutation.mutateAsync({
      ssh_user: form.ssh_user,
      ssh_key_pair_id: form.ssh_key_pair_id || undefined,
      ssh_private_key: form.ssh_private_key.trim() || undefined,
      ssh_key_id: form.ssh_private_key.trim() || form.ssh_key_pair_id ? undefined : (form.ssh_key_id || undefined),
      ssh_key_name: form.ssh_private_key.trim() ? form.ssh_key_name.trim() || 'default' : undefined,
      deploy_kube_vip: form.deploy_kube_vip,
      control_plane_vip: form.control_plane_vip,
      pod_cidr: form.pod_cidr,
      service_cidr: form.service_cidr,
      cni: form.cni,
      node_ips,
      apply: form.apply,
    });
  };

  return (
    <Card>
      <CardHeader><CardTitle>Bootstrap Kubernetes</CardTitle></CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4 max-w-xl">
          {savedBootstrapQuery.data && (
            <Alert variant="info" title="Saved SSH settings found">
              <div className="flex items-center justify-between gap-3">
                <span className="text-sm">Use previously saved SSH user/key selection from this cluster record.</span>
                <Button type="button" variant="secondary" onClick={useSavedSSH}>Use saved values</Button>
              </div>
            </Alert>
          )}
          <Input label="SSH user" value={form.ssh_user} onChange={(e) => setForm({ ...form, ssh_user: e.target.value })} required />
          <Select
            label="Global SSH key pair"
            value={form.ssh_key_pair_id}
            onChange={(e) => {
              const selectedID = Number(e.target.value);
              const selected = globalKeysQuery.data?.find((item) => item.id === selectedID);
              setForm((prev) => ({
                ...prev,
                ssh_key_pair_id: selectedID,
                ssh_user: selected?.ssh_user || prev.ssh_user,
                ssh_private_key: selectedID > 0 ? '' : prev.ssh_private_key,
              }));
            }}
          >
            <option value={0}>-- Select global key pair (optional) --</option>
            {globalKeysQuery.data?.map((k) => (
              <option key={k.id} value={k.id}>{k.name} ({k.ssh_user})</option>
            ))}
          </Select>
          <Select label="Saved SSH key" value={form.ssh_key_id} onChange={(e) => setForm({ ...form, ssh_key_id: Number(e.target.value) })}>
            <option value={0}>-- Select saved key (optional) --</option>
            {savedKeysQuery.data?.map((k) => (
              <option key={k.id} value={k.id}>{k.key_name}</option>
            ))}
          </Select>
          <Input
            label="Key name (when saving pasted key)"
            value={form.ssh_key_name}
            onChange={(e) => setForm({ ...form, ssh_key_name: e.target.value })}
            placeholder="default"
            helperText="Used when you paste a key below; updates existing key with same name"
          />
          <Textarea
            label="SSH private key (optional)"
            value={form.ssh_private_key}
            onChange={(e) => setForm({ ...form, ssh_private_key: e.target.value })}
            rows={6}
            placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----'}
            helperText="Paste to save/update a cluster key. Leave empty to use global key pair or selected saved key."
          />
          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              checked={form.deploy_kube_vip}
              onChange={(e) => setForm({ ...form, deploy_kube_vip: e.target.checked })}
              className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
            />
            <span className="text-sm text-slate-700">Deploy kube-vip static pod before kubeadm init (HA VIP)</span>
          </label>
          {nodesQuery.isLoading && (
            <p className="text-sm text-slate-500">Loading provisioned node IPs…</p>
          )}
          {nodesQuery.data && nodesQuery.data.some((n) => n.ip_address) && form.node_ips_raw && (
            <Alert variant="info" title="IPs pre-filled from provisioning">
              Node names and IPs were detected from the last provisioning run. Review and correct if needed before bootstrapping.
            </Alert>
          )}
          <Input label="Control plane VIP" value={form.control_plane_vip} onChange={(e) => setForm({ ...form, control_plane_vip: e.target.value })} placeholder="192.168.8.20" required />
          <Input label="Pod CIDR" value={form.pod_cidr} onChange={(e) => setForm({ ...form, pod_cidr: e.target.value })} required />
          <Input label="Service CIDR" value={form.service_cidr} onChange={(e) => setForm({ ...form, service_cidr: e.target.value })} required />
          <Select label="CNI" value={form.cni} onChange={(e) => setForm({ ...form, cni: e.target.value })}>
            <option value="calico">Calico</option>
            <option value="flannel">Flannel</option>
            <option value="cilium">Cilium</option>
          </Select>
          <Textarea
            label="Node IPs (one per line: vm-name ip)"
            value={form.node_ips_raw}
            onChange={(e) => setForm({ ...form, node_ips_raw: e.target.value })}
            rows={4}
            placeholder={'homelab-prod-cp-1 192.168.8.31\nhomelab-prod-wk-1 192.168.8.41'}
            helperText="Format: vm-name IP, one per line"
          />
          <label className="flex items-center gap-3">
            <input type="checkbox" checked={form.apply} onChange={(e) => setForm({ ...form, apply: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500" />
            <span className="text-sm text-slate-700">Run ansible-playbook immediately</span>
          </label>
          {mutation.data && (
            <Alert variant="success" title="Bootstrap triggered">
              Status: {mutation.data.status}
              {mutation.data.warnings?.length > 0 && (
                <ul className="mt-1 list-disc list-inside">{mutation.data.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul>
              )}
            </Alert>
          )}
          {mutation.error && <ErrorMessage error={mutation.error} />}
          <Button type="submit" loading={mutation.isPending}>Bootstrap</Button>
        </form>
      </CardContent>
    </Card>
  );
}

function Day2Tab({ id }: { id: number }) {
  const qc = useQueryClient();
  const scaleMut = useMutation({ mutationFn: (n: number) => scaleCluster(id, n), onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster', id] }) });
  const upgradeMut = useMutation({ mutationFn: (v: string) => upgradeCluster(id, v), onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster', id] }) });
  const replaceMut = useMutation({ mutationFn: (b: { failed_vm_name: string; new_proxmox_node_name: string; new_ip_address: string }) => replaceNode(id, b) });
  const rotateMut = useMutation({ mutationFn: (h: number) => rotateKubeconfig(id, h) });

  const [scaleTarget, setScaleTarget] = useState(3);
  const [upgradeVer, setUpgradeVer] = useState('v1.32.2');
  const [replaceForm, setReplaceForm] = useState({ failed_vm_name: '', new_proxmox_node_name: '', new_ip_address: '' });
  const [rotateTtl, setRotateTtl] = useState(24);

  return (
    <div className="space-y-6">
      {/* Scale */}
      <Card>
        <CardHeader><CardTitle>Scale Workers</CardTitle></CardHeader>
        <CardContent className="flex items-end gap-3">
          <Input label="Target worker count" type="number" min={1} value={scaleTarget} onChange={(e) => setScaleTarget(Number(e.target.value))} className="w-40" />
          <Button onClick={() => scaleMut.mutate(scaleTarget)} loading={scaleMut.isPending}>Scale</Button>
          {scaleMut.data && <Alert variant="success" className="flex-1">Workers: {scaleMut.data.previous_worker_count} → {scaleMut.data.new_worker_count}</Alert>}
          {scaleMut.error && <ErrorMessage error={scaleMut.error} />}
        </CardContent>
      </Card>

      {/* Upgrade */}
      <Card>
        <CardHeader><CardTitle>Upgrade Kubernetes</CardTitle></CardHeader>
        <CardContent className="flex items-end gap-3">
          <Input label="Target version" value={upgradeVer} onChange={(e) => setUpgradeVer(e.target.value)} placeholder="v1.32.2" className="w-48" />
          <Button onClick={() => upgradeMut.mutate(upgradeVer)} loading={upgradeMut.isPending}>Upgrade</Button>
          {upgradeMut.data && <Alert variant="success" className="flex-1">{upgradeMut.data.previous_version} → {upgradeMut.data.target_version}: {upgradeMut.data.status}</Alert>}
          {upgradeMut.error && <ErrorMessage error={upgradeMut.error} />}
        </CardContent>
      </Card>

      {/* Replace node */}
      <Card>
        <CardHeader><CardTitle>Replace Unhealthy Node</CardTitle></CardHeader>
        <CardContent className="space-y-3 max-w-lg">
          <Input label="Failed VM name" value={replaceForm.failed_vm_name} onChange={(e) => setReplaceForm({ ...replaceForm, failed_vm_name: e.target.value })} placeholder="homelab-prod-wk-3" />
          <Input label="New Proxmox node" value={replaceForm.new_proxmox_node_name} onChange={(e) => setReplaceForm({ ...replaceForm, new_proxmox_node_name: e.target.value })} placeholder="pve-node-2" />
          <Input label="New IP address" value={replaceForm.new_ip_address} onChange={(e) => setReplaceForm({ ...replaceForm, new_ip_address: e.target.value })} placeholder="192.168.8.53" />
          <Button onClick={() => replaceMut.mutate(replaceForm)} loading={replaceMut.isPending}>Replace Node</Button>
          {replaceMut.data && <Alert variant="success">{replaceMut.data.old_vm_name} → {replaceMut.data.new_vm_name}: {replaceMut.data.status}</Alert>}
          {replaceMut.error && <ErrorMessage error={replaceMut.error} />}
        </CardContent>
      </Card>

      {/* Rotate kubeconfig */}
      <Card>
        <CardHeader><CardTitle>Rotate Kubeconfig Token</CardTitle></CardHeader>
        <CardContent className="space-y-3 max-w-lg">
          <Input label="TTL (hours)" type="number" min={1} value={rotateTtl} onChange={(e) => setRotateTtl(Number(e.target.value))} className="w-32" />
          <Button onClick={() => rotateMut.mutate(rotateTtl)} loading={rotateMut.isPending}>Rotate</Button>
          {rotateMut.data && (
            <Alert variant="success" title="Token rotated">
              <p>Expires: {new Date(rotateMut.data.expires_at).toLocaleString()}</p>
              <pre className="mt-2 text-xs bg-emerald-100 p-2 rounded break-all whitespace-pre-wrap">{rotateMut.data.token}</pre>
            </Alert>
          )}
          {rotateMut.error && <ErrorMessage error={rotateMut.error} />}
        </CardContent>
      </Card>
    </div>
  );
}

function ObservabilityTab({ id }: { id: number }) {
  const deployMut = useMutation({ mutationFn: (b: Parameters<typeof deployObservability>[1]) => deployObservability(id, b) });
  const { data: status, refetch } = useQuery({ queryKey: ['observability', id], queryFn: () => getObservability(id), retry: false });

  const [form, setForm] = useState({
    grafana_admin_user: 'admin',
    grafana_admin_password: '',
    retention_days: 15,
    storage_class: 'longhorn',
    apply: false,
  });

  const handleDeploy = async (e: FormEvent) => {
    e.preventDefault();
    await deployMut.mutateAsync(form);
    refetch();
  };

  return (
    <div className="space-y-6">
      {status && (
        <Card>
          <CardHeader><CardTitle>Current Status</CardTitle></CardHeader>
          <div className="divide-y divide-slate-100">
            {[
              ['Status', <StatusBadge key="s" status={status.status} />],
              ['Dashboard URL', status.dashboard_url ? <a key="u" href={status.dashboard_url} target="_blank" rel="noreferrer" className="text-indigo-600 hover:underline text-sm">{status.dashboard_url}</a> : '—'],
              ['Grafana User', status.grafana_admin_user],
              ['Retention', `${status.retention_days} days`],
              ['Storage Class', status.storage_class],
              ['Updated', new Date(status.updated_at).toLocaleString()],
            ].map(([label, value]) => (
              <div key={String(label)} className="flex items-center px-6 py-3">
                <span className="w-40 text-sm text-slate-500 font-medium flex-shrink-0">{label}</span>
                <span className="text-sm text-slate-900">{value}</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle>Deploy Observability Stack</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleDeploy} className="space-y-4 max-w-xl">
            <Input label="Grafana admin user" value={form.grafana_admin_user} onChange={(e) => setForm({ ...form, grafana_admin_user: e.target.value })} required />
            <Input label="Grafana admin password" type="password" value={form.grafana_admin_password} onChange={(e) => setForm({ ...form, grafana_admin_password: e.target.value })} required />
            <Input label="Retention days" type="number" min={1} value={form.retention_days} onChange={(e) => setForm({ ...form, retention_days: Number(e.target.value) })} />
            <Input label="Storage class" value={form.storage_class} onChange={(e) => setForm({ ...form, storage_class: e.target.value })} placeholder="longhorn" />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.apply} onChange={(e) => setForm({ ...form, apply: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Apply manifests immediately</span>
            </label>
            {deployMut.data && <Alert variant="success">Status: {deployMut.data.status}</Alert>}
            {deployMut.error && <ErrorMessage error={deployMut.error} />}
            <Button type="submit" loading={deployMut.isPending}>Deploy</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

function MetalLBTab({ id }: { id: number }) {
  const deployMut = useMutation({ mutationFn: (b: Parameters<typeof deployMetalLB>[1]) => deployMetalLB(id, b) });
  const { data: status, refetch } = useQuery({ queryKey: ['metallb', id], queryFn: () => getMetalLB(id), retry: false });
  const commandSource = deployMut.data ?? status;

  const [form, setForm] = useState({
    namespace: 'metallb-system',
    version: 'v0.14.9',
    pool_name: 'services',
    addresses_raw: '',
    interfaces_raw: '',
    auto_assign: true,
    avoid_buggy_ips: false,
    apply: false,
  });

  const handleDeploy = async (e: FormEvent) => {
    e.preventDefault();
    const addresses = form.addresses_raw
      .split('\n')
      .map((value) => value.trim())
      .filter(Boolean);
    const interfaces = form.interfaces_raw
      .split('\n')
      .map((value) => value.trim())
      .filter(Boolean);

    await deployMut.mutateAsync({
      namespace: form.namespace,
      version: form.version,
      address_pools: [{
        name: form.pool_name,
        addresses,
        auto_assign: form.auto_assign,
        avoid_buggy_ips: form.avoid_buggy_ips,
      }],
      interfaces,
      apply: form.apply,
    });
    refetch();
  };

  return (
    <div className="space-y-6">
      {commandSource && (
        <Card>
          <CardHeader><CardTitle>Operator Commands</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <Input
              label="Manifest directory"
              value={commandSource.manifest_dir}
              readOnly
            />
            <Textarea
              label="PowerShell"
              value={commandSource.commands.powershell}
              readOnly
              rows={3}
              className="font-mono text-xs"
              helperText="Run from a Windows machine with kubectl pointed at the target cluster"
            />
            <Textarea
              label="Shell"
              value={commandSource.commands.shell}
              readOnly
              rows={3}
              className="font-mono text-xs"
              helperText="Run from a Linux machine with kubectl pointed at the target cluster"
            />
          </CardContent>
        </Card>
      )}

      {status && (
        <Card>
          <CardHeader><CardTitle>Current Status</CardTitle></CardHeader>
          <div className="divide-y divide-slate-100">
            {[
              ['Status', <StatusBadge key="s" status={status.status} />],
              ['Namespace', status.namespace],
              ['Version', status.version],
              ['Address Pools', status.address_pools.map((pool) => `${pool.name}: ${pool.addresses.join(', ')}`).join(' | ') || '—'],
              ['Interfaces', status.interfaces.join(', ') || 'all eligible interfaces'],
              ['Manifest Dir', status.manifest_dir || '—'],
              ['Updated', new Date(status.updated_at).toLocaleString()],
            ].map(([label, value]) => (
              <div key={String(label)} className="flex items-center px-6 py-3">
                <span className="w-40 text-sm text-slate-500 font-medium flex-shrink-0">{label}</span>
                <span className="text-sm text-slate-900 break-all">{value}</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card>
        <CardHeader><CardTitle>Prepare MetalLB</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleDeploy} className="space-y-4 max-w-xl">
            <Alert variant="info" title="Generated deployment assets">
              This prepares cluster-specific MetalLB manifests and helper scripts in the backend operations workspace. It does not run kubectl in the cluster at request time.
            </Alert>
            <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} placeholder="metallb-system" required />
            <Input label="MetalLB version" value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} placeholder="v0.14.9" required />
            <Input label="Address pool name" value={form.pool_name} onChange={(e) => setForm({ ...form, pool_name: e.target.value })} placeholder="services" required />
            <Textarea
              label="Address ranges"
              value={form.addresses_raw}
              onChange={(e) => setForm({ ...form, addresses_raw: e.target.value })}
              rows={4}
              placeholder={'192.168.8.220-192.168.8.240\n192.168.8.250/32'}
              helperText="One CIDR, IP, or IP range per line"
              required
            />
            <Textarea
              label="L2 interfaces (optional)"
              value={form.interfaces_raw}
              onChange={(e) => setForm({ ...form, interfaces_raw: e.target.value })}
              rows={3}
              placeholder={'ens18\neth0'}
              helperText="Leave empty to advertise on all eligible interfaces"
            />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.auto_assign} onChange={(e) => setForm({ ...form, auto_assign: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Enable auto assignment from this pool</span>
            </label>
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.avoid_buggy_ips} onChange={(e) => setForm({ ...form, avoid_buggy_ips: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Avoid buggy .0 and .255 addresses</span>
            </label>
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.apply} onChange={(e) => setForm({ ...form, apply: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Mark as apply requested</span>
            </label>
            {deployMut.data && (
              <Alert variant="success" title="MetalLB assets prepared">
                <div className="space-y-2">
                  <p>Status: {deployMut.data.status}</p>
                  <p>Manifest directory: {deployMut.data.manifest_dir}</p>
                  <p className="text-xs">Use the Operator Commands card above to run the generated helper script.</p>
                </div>
              </Alert>
            )}
            {deployMut.error && <ErrorMessage error={deployMut.error} />}
            <Button type="submit" loading={deployMut.isPending}>Prepare MetalLB</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

function EtcdTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: snapshots, isLoading } = useQuery({ queryKey: ['etcd-snapshots', id], queryFn: () => getEtcdSnapshots(id) });
  const snapshotMut = useMutation({ mutationFn: (b: { storage_target: string; apply: boolean }) => createEtcdSnapshot(id, b), onSuccess: () => qc.invalidateQueries({ queryKey: ['etcd-snapshots', id] }) });
  const restoreMut = useMutation({ mutationFn: (b: { snapshot_id: number; apply: boolean }) => restoreEtcdSnapshot(id, b) });

  const [storageTarget, setStorageTarget] = useState('backups/etcd');
  const [applySnapshot, setApplySnapshot] = useState(false);
  const [restoreModal, setRestoreModal] = useState<{ open: boolean; snapshotId: number }>({ open: false, snapshotId: 0 });
  const [applyRestore, setApplyRestore] = useState(false);

  return (
    <div className="space-y-6">
      {/* Create snapshot */}
      <Card>
        <CardHeader><CardTitle>Create Snapshot</CardTitle></CardHeader>
        <CardContent className="flex items-end gap-3 flex-wrap">
          <Input label="Storage target" value={storageTarget} onChange={(e) => setStorageTarget(e.target.value)} className="w-64" />
          <label className="flex items-center gap-2 pb-1">
            <input type="checkbox" checked={applySnapshot} onChange={(e) => setApplySnapshot(e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
            <span className="text-sm text-slate-700">Apply now</span>
          </label>
          <Button onClick={() => snapshotMut.mutate({ storage_target: storageTarget, apply: applySnapshot })} loading={snapshotMut.isPending}>Create Snapshot</Button>
          {snapshotMut.data && <Alert variant="success" className="w-full">Snapshot {snapshotMut.data.snapshot_name} created (ID: {snapshotMut.data.snapshot_id})</Alert>}
          {snapshotMut.error && <ErrorMessage error={snapshotMut.error} />}
        </CardContent>
      </Card>

      {/* Snapshots list */}
      <Card>
        <CardHeader><CardTitle>Snapshots</CardTitle></CardHeader>
        {isLoading ? (
          <CardContent><PageSpinner /></CardContent>
        ) : !snapshots || snapshots.length === 0 ? (
          <CardContent><p className="text-sm text-slate-400">No snapshots yet.</p></CardContent>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 bg-slate-50">
                  <th className="text-left px-6 py-3 font-medium text-slate-600">ID</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                  <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                  <th className="px-6 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {snapshots.map((s) => (
                  <tr key={s.id}>
                    <td className="px-6 py-3 text-slate-500">{s.id}</td>
                    <td className="px-6 py-3 font-medium text-slate-900">{s.snapshot_name}</td>
                    <td className="px-6 py-3"><StatusBadge status={s.status} /></td>
                    <td className="px-6 py-3 text-slate-500">{new Date(s.created_at).toLocaleString()}</td>
                    <td className="px-6 py-3">
                      <Button variant="danger" size="sm" onClick={() => setRestoreModal({ open: true, snapshotId: s.id })}>Restore</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {restoreMut.data && <Alert variant="success">Restore status: {restoreMut.data.status}</Alert>}
      {restoreMut.error && <ErrorMessage error={restoreMut.error} />}

      <Modal open={restoreModal.open} onClose={() => setRestoreModal({ open: false, snapshotId: 0 })} title="Restore etcd Snapshot">
        <p className="text-sm text-slate-600 mb-4">
          Restore snapshot <strong>#{restoreModal.snapshotId}</strong>? This will overwrite the current cluster state.
        </p>
        <label className="flex items-center gap-3 mb-6">
          <input type="checkbox" checked={applyRestore} onChange={(e) => setApplyRestore(e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
          <span className="text-sm text-slate-700">Apply restore immediately</span>
        </label>
        <div className="flex gap-3">
          <Button
            variant="danger"
            loading={restoreMut.isPending}
            onClick={async () => {
              await restoreMut.mutateAsync({ snapshot_id: restoreModal.snapshotId, apply: applyRestore });
              setRestoreModal({ open: false, snapshotId: 0 });
            }}
          >
            Confirm Restore
          </Button>
          <Button variant="secondary" onClick={() => setRestoreModal({ open: false, snapshotId: 0 })}>Cancel</Button>
        </div>
      </Modal>
    </div>
  );
}

// ─── Main Component ───────────────────────────────────────────────────────────

export function ClusterDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const clusterId = Number(id);
  const qc = useQueryClient();
  const { data: cluster, isLoading } = useQuery({ queryKey: ['cluster', clusterId], queryFn: () => getCluster(clusterId) });
  const initialTab = (searchParams.get('tab') as Tab | null) ?? 'overview';
  const [activeTab, setActiveTab] = useState<Tab>(TABS.some((t) => t.key === initialTab) ? initialTab : 'overview');

  if (isLoading) return <Layout><PageSpinner /></Layout>;
  if (!cluster) return <Layout><p className="text-slate-500">Cluster not found.</p></Layout>;

  return (
    <Layout>
      <PageHeader
        title={cluster.name}
        description={`${cluster.topology_type} · ${cluster.kubernetes_version}`}
        actions={
          <div className="flex items-center gap-3">
            <StatusBadge status={cluster.status} />
            <Button variant="ghost" size="sm" onClick={() => qc.invalidateQueries({ queryKey: ['cluster', clusterId] })}>
              <RefreshCw className="h-4 w-4" />
            </Button>
            <Link to="/clusters" className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm">
              <ArrowLeft className="h-4 w-4" /> Back
            </Link>
          </div>
        }
      />

      {/* Tabs */}
      <div className="flex gap-1 border-b border-slate-200 mb-6 overflow-x-auto">
        {TABS.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setActiveTab(key)}
            className={`px-4 py-2.5 text-sm font-medium whitespace-nowrap transition-colors border-b-2 -mb-px ${
              activeTab === key
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-slate-500 hover:text-slate-700 hover:border-slate-300'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      {activeTab === 'overview' && <OverviewTab id={clusterId} />}
      {activeTab === 'runs' && <RunsTab id={clusterId} />}
      {activeTab === 'bootstrap' && <BootstrapTab id={clusterId} />}
      {activeTab === 'day2' && <Day2Tab id={clusterId} />}
      {activeTab === 'metallb' && <MetalLBTab id={clusterId} />}
      {activeTab === 'observability' && <ObservabilityTab id={clusterId} />}
      {activeTab === 'etcd' && <EtcdTab id={clusterId} />}
    </Layout>
  );
}
