import { useState, useEffect, useRef, type ChangeEvent, type FormEvent } from 'react';
import { useParams, Link, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, Clipboard, Play, RefreshCw } from 'lucide-react';
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
  deployIngressController,
  deployLonghorn,
  deployApp,
  getApplicationManifests,
  getApplicationManifestRevision,
  getApplicationManifestRevisions,
  redeployApplicationManifest,
  startKubectlCommand,
  getKubectlCommandRun,
  deployObservability,
  getObservability,
  createEtcdSnapshot,
  getEtcdSnapshots,
  restoreEtcdSnapshot,
  getClusterNodes,
  getBootstrapConfig,
  getBootstrapKeys,
  getClusterSecrets,
  createClusterSecret,
  updateClusterSecret,
  deleteClusterSecret,
  applyClusterSecret,
  updateClusterSecretKey,
  deleteClusterSecretKey,
  getClusterConfigMaps,
  createClusterConfigMap,
  updateClusterConfigMap,
  deleteClusterConfigMap,
  applyClusterConfigMap,
  updateClusterConfigMapKey,
  deleteClusterConfigMapKey,
  getClusterImagePullSecrets,
  createClusterImagePullSecret,
  updateClusterImagePullSecret,
  deleteClusterImagePullSecret,
  applyClusterImagePullSecret,
  getClusterSavedManifests,
  createClusterSavedManifest,
  updateClusterSavedManifest,
  updateGlobalSavedManifest,
  deleteClusterSavedManifest,
  applyClusterSavedManifest,
  getClusterSavedManifestRevisions,
  getClusterSavedManifestRuns,
} from '../api/clusters';
import { getSSHKeys } from '../api/sshKeys';
import { getSSLCertificates } from '../api/sslCertificates';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Input, Textarea, Select } from '../components/ui/Input';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import { Modal } from '../components/ui/Modal';
import type { ClusterConfigMapRecord, ClusterImagePullSecretRecord, ClusterOperation, ClusterSavedManifestRecord, ClusterSecretProvider, ClusterSecretRecord } from '../types';

type Tab = 'overview' | 'runs' | 'bootstrap' | 'day2' | 'metallb' | 'storage' | 'secrets' | 'configmaps' | 'pullsecrets' | 'manifests' | 'applications' | 'observability' | 'etcd';

const TABS: { key: Tab; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'runs', label: 'Run History' },
  { key: 'bootstrap', label: 'Bootstrap' },
  { key: 'day2', label: 'Day-2 Ops' },
  { key: 'metallb', label: 'MetalLB' },
  { key: 'storage', label: 'Storage' },
  { key: 'secrets', label: 'Secrets' },
  { key: 'configmaps', label: 'ConfigMaps' },
  { key: 'pullsecrets', label: 'Pull Secrets' },
  { key: 'manifests', label: 'Manifests' },
  { key: 'applications', label: 'Applications' },
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

function shouldPollLatestOperation(operation: ClusterOperation | null | undefined) {
  if (!operation) return 2500;
  const status = operation.status.toLowerCase();
  if (status === 'running') return 2500;

  const action = operation.action.toLowerCase();
  const updatedAt = Date.parse(operation.updated_at);
  const recentlyUpdated = Number.isFinite(updatedAt) && Date.now() - updatedAt < 10 * 60 * 1000;
  if (recentlyUpdated && (action === 'provision' || action === 'redeploy') && status === 'succeeded') {
    return 2500;
  }

  return false;
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
  const operationTimelineRef = useRef<HTMLDivElement | null>(null);

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
    refetchInterval: (query) => shouldPollLatestOperation(query.state.data),
  });

  const { data: events } = useQuery({
    queryKey: ['cluster-operation-events', id, operation?.id ?? 0],
    queryFn: () => getClusterOperationEvents(id, operation?.id, 0, 200),
    enabled: Boolean(operation?.id),
    refetchInterval: shouldPollLatestOperation(operation),
  });

  const { data: runs, isLoading } = useQuery({ queryKey: ['cluster-runs', id], queryFn: () => getClusterRuns(id) });

  const progressPercent = Math.max(0, Math.min(100, Number(operation?.progress_percent ?? 0)));
  const latestEventID = events?.length ? events[events.length - 1].id : 0;

  useEffect(() => {
    const timeline = operationTimelineRef.current;
    if (!timeline) return;
    timeline.scrollTop = timeline.scrollHeight;
  }, [operation?.id, latestEventID]);

  if (isLoading) return <PageSpinner />;

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
            <div ref={operationTimelineRef} className="max-h-80 overflow-auto rounded-md border border-slate-200 divide-y divide-slate-100">
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
      control_plane_vip: form.deploy_kube_vip ? form.control_plane_vip.trim() : '',
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
          <Input
            label="Control plane VIP"
            value={form.control_plane_vip}
            onChange={(e) => setForm({ ...form, control_plane_vip: e.target.value })}
            placeholder="192.168.8.20"
            required={form.deploy_kube_vip}
            helperText={form.deploy_kube_vip ? 'Required when deploying kube-vip for a highly available API endpoint.' : 'Optional. Leave empty or disable kube-vip to bootstrap against the first control-plane node directly.'}
          />
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

function StorageTab({ id }: { id: number }) {
  const deployMut = useMutation({ mutationFn: (b: Parameters<typeof deployLonghorn>[1]) => deployLonghorn(id, b) });
  const [form, setForm] = useState({
    version: 'v1.7.2',
    set_default: true,
    wait: true,
  });

  const handleDeploy = (event: FormEvent) => {
    event.preventDefault();
    deployMut.mutate(form);
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>Longhorn Storage</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleDeploy} className="space-y-4 max-w-xl">
            <Alert variant="warning" title="Node packages required">
              Install open-iscsi and nfs-common on every Kubernetes node before using Longhorn volumes.
            </Alert>
            <Input
              label="Longhorn version"
              value={form.version}
              onChange={(e) => setForm({ ...form, version: e.target.value })}
              placeholder="v1.7.2"
              required
            />
            <label className="flex items-center gap-3">
              <input
                type="checkbox"
                checked={form.set_default}
                onChange={(e) => setForm({ ...form, set_default: e.target.checked })}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600"
              />
              <span className="text-sm text-slate-700">Set longhorn as default StorageClass</span>
            </label>
            <label className="flex items-center gap-3">
              <input
                type="checkbox"
                checked={form.wait}
                onChange={(e) => setForm({ ...form, wait: e.target.checked })}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600"
              />
              <span className="text-sm text-slate-700">Wait for Longhorn rollout</span>
            </label>
            <Button type="submit" loading={deployMut.isPending}>Install Longhorn</Button>
            {deployMut.data && (
              <Alert variant="success" title="Longhorn action completed">
                <div className="space-y-2">
                  <p>Status: {deployMut.data.status}</p>
                  <p>Version: {deployMut.data.version}</p>
                  {deployMut.data.warnings.map((warning) => (
                    <p key={warning} className="text-xs">{warning}</p>
                  ))}
                </div>
              </Alert>
            )}
            {deployMut.error && <ErrorMessage error={deployMut.error} />}
          </form>
          {deployMut.data?.output && (
            <Textarea
              label="kubectl output"
              value={deployMut.data.output}
              readOnly
              rows={12}
              className="mt-4 font-mono text-xs"
            />
          )}
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
              <span className="text-sm text-slate-700">Deploy MetalLB to cluster now</span>
            </label>
            {deployMut.data && (
              <Alert variant="success" title={form.apply ? "MetalLB deployed successfully" : "MetalLB assets prepared"}>
                <div className="space-y-2">
                  <p>Status: {deployMut.data.status}</p>
                  <p>Manifest directory: {deployMut.data.manifest_dir}</p>
                  {form.apply ? (
                    <p className="text-xs">MetalLB has been deployed to your cluster. LoadBalancer services should now receive external IPs.</p>
                  ) : (
                    <p className="text-xs">Use the Operator Commands card above to run the generated helper script.</p>
                  )}
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

const SAMPLE_APP_MANIFEST = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: sample-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: sample-app
  template:
    metadata:
      labels:
        app: sample-app
    spec:
      containers:
        - name: nginx
          image: nginx:stable
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: sample-app
spec:
  selector:
    app: sample-app
  ports:
    - name: http
      port: 80
      targetPort: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sample-app
  annotations:
    kubernetes.io/ingress.class: traefik
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: traefik
  tls:
    - hosts:
        - sample-app.example.com
      secretName: sample-app-tls
  rules:
    - host: sample-app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: sample-app
                port:
                  number: 80
`;

function KubectlCommandPanel({ id }: { id: number }) {
  const [command, setCommand] = useState('kubectl get pods -A\nkubectl get events -A --sort-by=.lastTimestamp');
  const [runID, setRunID] = useState('');

  const runQuery = useQuery({
    queryKey: ['kubectl-command-run', id, runID],
    queryFn: () => getKubectlCommandRun(id, runID),
    enabled: Boolean(runID),
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2000 : false),
  });

  const runMut = useMutation({
    mutationFn: (cmd: string) => startKubectlCommand(id, cmd),
    onSuccess: (data) => setRunID(data.id),
  });

  const activeRun = runQuery.data ?? runMut.data;
  const isRunning = activeRun?.status === 'running';

  const runCommand = async (e?: FormEvent) => {
    e?.preventDefault();
    const trimmed = command.trim();
    if (!trimmed) return;
    await runMut.mutateAsync(trimmed);
  };

  return (
    <div className="mt-4 border-t border-slate-200 pt-4">
      <form onSubmit={runCommand} className="space-y-3">
        <Textarea
          label="Kubectl commands"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          rows={5}
          className="font-mono text-xs"
          placeholder={'kubectl get pods -A\nkubectl get events -A --sort-by=.lastTimestamp'}
          helperText="Paste one kubectl command per line. The cluster kubeconfig is selected automatically."
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" loading={runMut.isPending}>
            <Play className="h-4 w-4" />
            Run
          </Button>
          <Button
            type="button"
            variant="secondary"
            onClick={() => runCommand()}
            loading={runMut.isPending}
            disabled={!command.trim()}
          >
            <RefreshCw className="h-4 w-4" />
            Refresh Output
          </Button>
          {runID && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => runQuery.refetch()}
              loading={runQuery.isFetching && !isRunning}
            >
              <RefreshCw className={`h-4 w-4 ${isRunning ? 'animate-spin' : ''}`} />
              Poll
            </Button>
          )}
          {activeRun && (
            <span className="text-sm text-slate-600">
              Status: <span className="font-medium">{activeRun.status}</span>
            </span>
          )}
        </div>
      </form>
      {runMut.error && <ErrorMessage error={runMut.error} />}
      {runQuery.error && <ErrorMessage error={runQuery.error} />}
      {activeRun && (
        <Textarea
          label="Command output"
          value={[
            `$ ${activeRun.command}`,
            activeRun.output || (activeRun.status === 'running' ? 'Running...' : ''),
            activeRun.error ? `error: ${activeRun.error}` : '',
          ].filter(Boolean).join('\n')}
          readOnly
          rows={10}
          className="mt-4 font-mono text-xs"
        />
      )}
    </div>
  );
}

const SECRET_PROVIDERS: { value: ClusterSecretProvider; label: string }[] = [
  { value: 'native', label: 'Native encrypted DB' },
  { value: 'hashicorp-vault', label: 'HashiCorp Vault' },
  { value: 'external-secrets-operator', label: 'External Secrets Operator' },
  { value: 'aws-secrets-manager', label: 'AWS Secrets Manager' },
  { value: 'azure-key-vault', label: 'Azure Key Vault' },
  { value: 'google-secret-manager', label: 'Google Secret Manager' },
  { value: 'bitnami-sealed-secrets', label: 'Bitnami Sealed Secrets' },
];

function parseSecretData(raw: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const idx = trimmed.indexOf('=');
    if (idx <= 0) continue;
    out[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1);
  }
  return out;
}

function stringifyKeyValues(data: Record<string, string>): string {
  return Object.entries(data).map(([key, value]) => `${key}=${value}`).join('\n');
}

function providerDefaults(provider: ClusterSecretProvider): Record<string, unknown> {
  if (provider === 'native' || provider === 'bitnami-sealed-secrets') return {};
  return {
    secret_store_name: 'cluster-secret-store',
    secret_store_kind: 'ClusterSecretStore',
    refresh_interval: '1h',
  };
}

function SecretsTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: secrets } = useQuery({ queryKey: ['cluster-secrets', id], queryFn: () => getClusterSecrets(id) });
  const [editing, setEditing] = useState<ClusterSecretRecord | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ClusterSecretRecord | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [form, setForm] = useState({
    name: '',
    namespace: 'default',
    provider: 'native' as ClusterSecretProvider,
    type: 'Opaque',
    description: '',
    external_ref: '',
    provider_config_raw: '{}',
    immutable: false,
    is_global: false,
    data_raw: 'USERNAME=\nPASSWORD=',
  });
  const [keyForm, setKeyForm] = useState({ secretID: 0, key: '', value: '' });

  const resetForm = () => {
    setEditing(null);
    setForm({
      name: '',
      namespace: 'default',
      provider: 'native',
      type: 'Opaque',
      description: '',
      external_ref: '',
      provider_config_raw: '{}',
      immutable: false,
      is_global: false,
      data_raw: 'USERNAME=\nPASSWORD=',
    });
  };

  const saveMut = useMutation({
    mutationFn: async () => {
      const provider_config = JSON.parse(form.provider_config_raw || '{}');
      const parsedData = parseSecretData(form.data_raw);
      const hasReplacementValues = Object.values(parsedData).some((value) => value.length > 0);
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        provider: form.provider,
        type: form.type.trim() || 'Opaque',
        description: form.description.trim(),
        external_ref: form.external_ref.trim(),
        provider_config,
        immutable: form.immutable,
        is_global: form.is_global,
      };
      return editing
        ? updateClusterSecret(id, editing.id, { ...body, data: hasReplacementValues ? parsedData : undefined })
        : createClusterSecret(id, { ...body, data: parsedData });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-secrets', id] });
      resetForm();
    },
  });

  const applyMut = useMutation({
    mutationFn: (secretID: number) => applyClusterSecret(id, secretID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-secrets', id] }),
  });

  const deleteMut = useMutation({
    mutationFn: (secretID: number) => deleteClusterSecret(id, secretID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-secrets', id] });
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  const updateKeyMut = useMutation({
    mutationFn: (body: { secretID: number; key: string; value: string }) =>
      updateClusterSecretKey(id, body.secretID, body.key.trim(), body.value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-secrets', id] });
      setKeyForm({ secretID: 0, key: '', value: '' });
    },
  });

  const deleteKeyMut = useMutation({
    mutationFn: (body: { secretID: number; key: string }) =>
      deleteClusterSecretKey(id, body.secretID, body.key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-secrets', id] }),
  });

  const startEdit = (secret: ClusterSecretRecord) => {
    setEditing(secret);
    setForm({
      name: secret.name,
      namespace: secret.namespace,
      provider: secret.provider,
      type: secret.type,
      description: secret.description || '',
      external_ref: secret.external_ref || '',
      provider_config_raw: JSON.stringify(secret.provider_config || providerDefaults(secret.provider), null, 2),
      immutable: secret.immutable,
      is_global: secret.is_global,
      data_raw: secret.keys.map((key) => `${key}=`).join('\n'),
    });
  };

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMut.mutate(deleteTarget.id);
  };

  const onProviderChange = (provider: ClusterSecretProvider) => {
    setForm((prev) => ({
      ...prev,
      provider,
      provider_config_raw: JSON.stringify(providerDefaults(provider), null, 2),
    }));
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>{editing ? 'Edit Cluster Secret' : 'Create Cluster Secret'}</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
              <Input label="Secret name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
              <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
              <Select label="Provider" value={form.provider} onChange={(e) => onProviderChange(e.target.value as ClusterSecretProvider)}>
                {SECRET_PROVIDERS.map((provider) => <option key={provider.value} value={provider.value}>{provider.label}</option>)}
              </Select>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-4xl">
              <Input label="Kubernetes type" value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} placeholder="Opaque" />
              <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            {form.provider !== 'native' && (
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                <Textarea
                  label={form.provider === 'bitnami-sealed-secrets' ? 'SealedSecret manifest' : 'External secret reference'}
                  value={form.external_ref}
                  onChange={(e) => setForm({ ...form, external_ref: e.target.value })}
                  rows={6}
                  className="font-mono text-xs"
                  required
                />
                <Textarea
                  label="Provider config JSON"
                  value={form.provider_config_raw}
                  onChange={(e) => setForm({ ...form, provider_config_raw: e.target.value })}
                  rows={6}
                  className="font-mono text-xs"
                />
              </div>
            )}
            <Textarea
              label={form.provider === 'native' ? 'Secret data' : 'Secret keys'}
              value={form.data_raw}
              onChange={(e) => setForm({ ...form, data_raw: e.target.value })}
              rows={7}
              className="font-mono text-xs"
              helperText={editing ? 'Metadata only: leave values blank to keep existing keys. Use the per-key controls below to add, update, or delete one value without replacing the full secret.' : 'Use KEY=value per line. Values are encrypted for native secrets.'}
              required
            />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.immutable} onChange={(e) => setForm({ ...form, immutable: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Immutable Kubernetes Secret</span>
            </label>
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Make available to all clusters</span>
            </label>
            <div className="flex gap-3">
              <Button type="submit" loading={saveMut.isPending}>{editing ? 'Update Secret' : 'Create Secret'}</Button>
              {editing && <Button type="button" variant="secondary" onClick={resetForm}>Cancel</Button>}
            </div>
            {saveMut.error && <ErrorMessage error={saveMut.error} />}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Cluster Secrets</CardTitle></CardHeader>
        <CardContent>
          {!secrets?.length ? (
            <p className="text-sm text-slate-400">No cluster secrets yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Name</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Namespace</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Provider</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Scope</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Keys</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Last applied</th>
                    <th className="px-4 py-2" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {secrets.map((secret) => (
                    <tr key={secret.id}>
                      <td className="px-4 py-2 font-medium text-slate-900">{secret.name}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.namespace}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.provider}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.is_global ? 'Global' : 'Cluster'}</td>
                      <td className="px-4 py-2 text-slate-600">
                        <div className="flex flex-wrap gap-1">
                          {secret.keys.length ? secret.keys.map((key) => (
                            <span key={key} className="inline-flex items-center gap-1 rounded bg-slate-100 px-2 py-1">
                              <button
                                type="button"
                                className="font-mono text-xs text-slate-700 hover:text-indigo-700"
                                disabled={secret.cluster_id !== id}
                                onClick={() => setKeyForm({ secretID: secret.id, key, value: '' })}
                              >
                                {key}
                              </button>
                              {secret.cluster_id === id && (
                                <button
                                  type="button"
                                  className="text-xs text-rose-600 hover:text-rose-700"
                                  disabled={deleteKeyMut.isPending}
                                  onClick={() => {
                                    if (window.confirm(`Delete key ${key} from ${secret.name}?`)) {
                                      deleteKeyMut.mutate({ secretID: secret.id, key });
                                    }
                                  }}
                                >
                                  delete
                                </button>
                              )}
                            </span>
                          )) : '-'}
                        </div>
                        {secret.cluster_id === id && (
                          <div className="mt-2 flex flex-wrap items-end gap-2">
                            <Input
                              label="Key"
                              value={keyForm.secretID === secret.id ? keyForm.key : ''}
                              onChange={(e) => setKeyForm({ ...keyForm, secretID: secret.id, key: e.target.value })}
                              className="w-52"
                            />
                            <Input
                              label="Value"
                              type="password"
                              value={keyForm.secretID === secret.id ? keyForm.value : ''}
                              onChange={(e) => setKeyForm({ ...keyForm, secretID: secret.id, value: e.target.value })}
                              className="w-56"
                            />
                            <Button
                              type="button"
                              size="sm"
                              loading={updateKeyMut.isPending && updateKeyMut.variables?.secretID === secret.id}
                              disabled={keyForm.secretID !== secret.id || !keyForm.key.trim() || !keyForm.value}
                              onClick={() => updateKeyMut.mutate(keyForm)}
                            >
                              Save key only
                            </Button>
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-2 text-slate-500">{secret.last_applied_at ? new Date(secret.last_applied_at).toLocaleString() : '-'}</td>
                      <td className="px-4 py-2">
                        <div className="flex justify-end gap-2">
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(secret)}>Edit</Button>
                          <Button type="button" size="sm" loading={applyMut.isPending && applyMut.variables === secret.id} onClick={() => applyMut.mutate(secret.id)}>Apply</Button>
                          <Button
                            type="button"
                            size="sm"
                            variant="danger"
                            loading={deleteMut.isPending && deleteMut.variables === secret.id}
                            onClick={() => {
                              setDeleteTarget(secret);
                              setDeleteConfirmationName('');
                            }}
                          >
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
          {applyMut.error && <ErrorMessage error={applyMut.error} />}
          {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
          {updateKeyMut.error && <ErrorMessage error={updateKeyMut.error} />}
          {deleteKeyMut.error && <ErrorMessage error={deleteKeyMut.error} />}
        </CardContent>
      </Card>
      <DeleteSecretConfirmModal
        secret={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMut.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMut.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDelete}
      />
    </div>
  );
}

function DeleteSecretConfirmModal({
  secret,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  secret: ClusterSecretRecord | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!secret && confirmationName === secret.name;
  return (
    <Modal open={!!secret} onClose={onClose} title="Delete Secret" maxWidth="lg">
      {secret && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This removes the portal Secret record">
            This does not automatically delete a Kubernetes Secret that has already been applied to a cluster.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{secret.name}</span> to confirm deletion.
          </div>
          <Input label="Secret name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>Cancel</Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>Delete Secret</Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

function ConfigMapsTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: configMaps } = useQuery({ queryKey: ['cluster-config-maps', id], queryFn: () => getClusterConfigMaps(id) });
  const [editing, setEditing] = useState<ClusterConfigMapRecord | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ClusterConfigMapRecord | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [form, setForm] = useState({
    name: '',
    namespace: 'default',
    description: '',
    immutable: false,
    is_global: false,
    data_raw: 'APP_ENV=production\nLOG_LEVEL=info',
  });
  const [keyForm, setKeyForm] = useState({ configMapID: 0, key: '', value: '' });

  const resetForm = () => {
    setEditing(null);
    setForm({
      name: '',
      namespace: 'default',
      description: '',
      immutable: false,
      is_global: false,
      data_raw: 'APP_ENV=production\nLOG_LEVEL=info',
    });
  };

  const saveMut = useMutation({
    mutationFn: async () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        description: form.description.trim(),
        immutable: form.immutable,
        is_global: form.is_global,
        data: parseSecretData(form.data_raw),
      };
      return editing
        ? updateClusterConfigMap(id, editing.id, body)
        : createClusterConfigMap(id, body);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-config-maps', id] });
      resetForm();
    },
  });

  const applyMut = useMutation({
    mutationFn: (configMapID: number) => applyClusterConfigMap(id, configMapID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-config-maps', id] }),
  });

  const deleteMut = useMutation({
    mutationFn: (configMapID: number) => deleteClusterConfigMap(id, configMapID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-config-maps', id] });
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  const updateKeyMut = useMutation({
    mutationFn: (body: { configMapID: number; key: string; value: string }) =>
      updateClusterConfigMapKey(id, body.configMapID, body.key.trim(), body.value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-config-maps', id] });
      setKeyForm({ configMapID: 0, key: '', value: '' });
    },
  });

  const deleteKeyMut = useMutation({
    mutationFn: (body: { configMapID: number; key: string }) =>
      deleteClusterConfigMapKey(id, body.configMapID, body.key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-config-maps', id] }),
  });

  const startEdit = (configMap: ClusterConfigMapRecord) => {
    setEditing(configMap);
    setForm({
      name: configMap.name,
      namespace: configMap.namespace,
      description: configMap.description || '',
      immutable: configMap.immutable,
      is_global: configMap.is_global,
      data_raw: stringifyKeyValues(configMap.data || {}),
    });
  };

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMut.mutate(deleteTarget.id);
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>{editing ? 'Edit ConfigMap' : 'Create ConfigMap'}</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
              <Input label="ConfigMap name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
              <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
              <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            <Textarea
              label="Config data"
              value={form.data_raw}
              onChange={(e) => setForm({ ...form, data_raw: e.target.value })}
              rows={8}
              className="font-mono text-xs"
              helperText={editing ? 'Full editor: saving here replaces the ConfigMap data with the lines shown. Use the per-key controls below to add, update, or delete one key only.' : 'Use KEY=value per line. ConfigMaps are for non-sensitive application configuration only.'}
              required
            />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.immutable} onChange={(e) => setForm({ ...form, immutable: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Immutable Kubernetes ConfigMap</span>
            </label>
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Make available to all clusters</span>
            </label>
            <div className="flex gap-3">
              <Button type="submit" loading={saveMut.isPending}>{editing ? 'Update ConfigMap' : 'Create ConfigMap'}</Button>
              {editing && <Button type="button" variant="secondary" onClick={resetForm}>Cancel</Button>}
            </div>
            {saveMut.error && <ErrorMessage error={saveMut.error} />}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Cluster ConfigMaps</CardTitle></CardHeader>
        <CardContent>
          {!configMaps?.length ? (
            <p className="text-sm text-slate-400">No cluster ConfigMaps yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Name</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Namespace</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Scope</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Keys</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Last applied</th>
                    <th className="px-4 py-2" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {configMaps.map((configMap) => (
                    <tr key={configMap.id}>
                      <td className="px-4 py-2 font-medium text-slate-900">{configMap.name}</td>
                      <td className="px-4 py-2 text-slate-600">{configMap.namespace}</td>
                      <td className="px-4 py-2 text-slate-600">{configMap.is_global ? 'Global' : 'Cluster'}</td>
                      <td className="px-4 py-2 text-slate-600">
                        <div className="flex flex-wrap gap-1">
                          {Object.keys(configMap.data || {}).length ? Object.keys(configMap.data || {}).map((key) => (
                            <span key={key} className="inline-flex items-center gap-1 rounded bg-slate-100 px-2 py-1">
                              <button
                                type="button"
                                className="font-mono text-xs text-slate-700 hover:text-indigo-700"
                                disabled={configMap.cluster_id !== id}
                                onClick={() => setKeyForm({ configMapID: configMap.id, key, value: configMap.data[key] || '' })}
                              >
                                {key}
                              </button>
                              {configMap.cluster_id === id && (
                                <button
                                  type="button"
                                  className="text-xs text-rose-600 hover:text-rose-700"
                                  disabled={deleteKeyMut.isPending}
                                  onClick={() => {
                                    if (window.confirm(`Delete key ${key} from ${configMap.name}?`)) {
                                      deleteKeyMut.mutate({ configMapID: configMap.id, key });
                                    }
                                  }}
                                >
                                  delete
                                </button>
                              )}
                            </span>
                          )) : '-'}
                        </div>
                        {configMap.cluster_id === id && (
                          <div className="mt-2 flex flex-wrap items-end gap-2">
                            <Input
                              label="Key"
                              value={keyForm.configMapID === configMap.id ? keyForm.key : ''}
                              onChange={(e) => setKeyForm({ ...keyForm, configMapID: configMap.id, key: e.target.value })}
                              className="w-52"
                            />
                            <Input
                              label="Value"
                              value={keyForm.configMapID === configMap.id ? keyForm.value : ''}
                              onChange={(e) => setKeyForm({ ...keyForm, configMapID: configMap.id, value: e.target.value })}
                              className="w-56"
                            />
                            <Button
                              type="button"
                              size="sm"
                              loading={updateKeyMut.isPending && updateKeyMut.variables?.configMapID === configMap.id}
                              disabled={keyForm.configMapID !== configMap.id || !keyForm.key.trim()}
                              onClick={() => updateKeyMut.mutate(keyForm)}
                            >
                              Save key only
                            </Button>
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-2 text-slate-500">{configMap.last_applied_at ? new Date(configMap.last_applied_at).toLocaleString() : '-'}</td>
                      <td className="px-4 py-2">
                        <div className="flex justify-end gap-2">
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(configMap)}>Edit</Button>
                          <Button type="button" size="sm" loading={applyMut.isPending && applyMut.variables === configMap.id} onClick={() => applyMut.mutate(configMap.id)}>Apply</Button>
                          <Button
                            type="button"
                            size="sm"
                            variant="danger"
                            loading={deleteMut.isPending && deleteMut.variables === configMap.id}
                            onClick={() => {
                              setDeleteTarget(configMap);
                              setDeleteConfirmationName('');
                            }}
                          >
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
          {applyMut.error && <ErrorMessage error={applyMut.error} />}
          {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
          {updateKeyMut.error && <ErrorMessage error={updateKeyMut.error} />}
          {deleteKeyMut.error && <ErrorMessage error={deleteKeyMut.error} />}
        </CardContent>
      </Card>
      <DeleteConfigMapConfirmModal
        configMap={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMut.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMut.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDelete}
      />
    </div>
  );
}

function DeleteConfigMapConfirmModal({
  configMap,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  configMap: ClusterConfigMapRecord | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!configMap && confirmationName === configMap.name;
  return (
    <Modal open={!!configMap} onClose={onClose} title="Delete ConfigMap" maxWidth="lg">
      {configMap && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This removes the portal ConfigMap record">
            This does not automatically delete a Kubernetes ConfigMap that has already been applied to a cluster.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{configMap.name}</span> to confirm deletion.
          </div>
          <Input label="ConfigMap name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>Cancel</Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>Delete ConfigMap</Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

function ImagePullSecretsTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: pullSecrets } = useQuery({ queryKey: ['cluster-image-pull-secrets', id], queryFn: () => getClusterImagePullSecrets(id) });
  const [editing, setEditing] = useState<ClusterImagePullSecretRecord | null>(null);
  const [form, setForm] = useState({
    name: 'ghcr-pull-secret',
    namespace: 'default',
    registry_server: 'ghcr.io',
    username: '',
    password: '',
    email: '',
    description: '',
    is_global: false,
  });

  const resetForm = () => {
    setEditing(null);
    setForm({
      name: 'ghcr-pull-secret',
      namespace: 'default',
      registry_server: 'ghcr.io',
      username: '',
      password: '',
      email: '',
      description: '',
      is_global: false,
    });
  };

  const saveMut = useMutation({
    mutationFn: async () => {
      const body = {
        name: form.name.trim() || 'ghcr-pull-secret',
        namespace: form.namespace.trim() || 'default',
        registry_server: form.registry_server.trim() || 'ghcr.io',
        username: form.username.trim(),
        password: form.password,
        email: form.email.trim(),
        description: form.description.trim(),
        is_global: form.is_global,
      };
      return editing
        ? updateClusterImagePullSecret(id, editing.id, body)
        : createClusterImagePullSecret(id, body);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-image-pull-secrets', id] });
      resetForm();
    },
  });

  const applyMut = useMutation({
    mutationFn: (secretID: number) => applyClusterImagePullSecret(id, secretID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-image-pull-secrets', id] }),
  });

  const deleteMut = useMutation({
    mutationFn: (secretID: number) => deleteClusterImagePullSecret(id, secretID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cluster-image-pull-secrets', id] }),
  });

  const startEdit = (secret: ClusterImagePullSecretRecord) => {
    setEditing(secret);
    setForm({
      name: secret.name,
      namespace: secret.namespace,
      registry_server: secret.registry_server || 'ghcr.io',
      username: secret.username,
      password: '',
      email: secret.email || '',
      description: secret.description || '',
      is_global: secret.is_global,
    });
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>{editing ? 'Edit Image Pull Secret' : 'Create Image Pull Secret'}</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
              <Input label="Secret name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
              <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
              <Input label="Registry server" value={form.registry_server} onChange={(e) => setForm({ ...form, registry_server: e.target.value })} required />
              <Input label="Username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required />
              <Input
                label={editing ? 'New token/password' : 'Token/password'}
                type="password"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                required={!editing}
                helperText={editing ? 'Leave blank to keep the existing registry token.' : 'For GHCR, use a GitHub token with package read access.'}
              />
              <Input label="Email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
              <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Available to all clusters</span>
            </label>
            <div className="flex gap-3">
              <Button type="submit" loading={saveMut.isPending}>{editing ? 'Save Pull Secret' : 'Create Pull Secret'}</Button>
              {editing && <Button type="button" variant="secondary" onClick={resetForm}>Cancel</Button>}
            </div>
            {saveMut.error && <ErrorMessage error={saveMut.error} />}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Image Pull Secrets</CardTitle></CardHeader>
        <CardContent>
          {!pullSecrets?.length ? (
            <p className="text-sm text-slate-400">No image pull secrets yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Name</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Namespace</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Registry</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Username</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Scope</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Last applied</th>
                    <th className="px-4 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {pullSecrets.map((secret) => (
                    <tr key={secret.id} className="border-b border-slate-100">
                      <td className="px-4 py-2 font-medium text-slate-900">{secret.name}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.namespace}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.registry_server}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.username}</td>
                      <td className="px-4 py-2 text-slate-600">{secret.is_global ? 'Global' : 'Cluster'}</td>
                      <td className="px-4 py-2 text-slate-500">{secret.last_applied_at ? new Date(secret.last_applied_at).toLocaleString() : '-'}</td>
                      <td className="px-4 py-2">
                        <div className="flex justify-end gap-2">
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(secret)}>Edit</Button>
                          <Button type="button" size="sm" loading={applyMut.isPending && applyMut.variables === secret.id} onClick={() => applyMut.mutate(secret.id)}>Apply</Button>
                          <Button type="button" size="sm" variant="danger" loading={deleteMut.isPending && deleteMut.variables === secret.id} onClick={() => deleteMut.mutate(secret.id)}>Delete</Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {applyMut.error && <ErrorMessage error={applyMut.error} />}
          {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
        </CardContent>
      </Card>
    </div>
  );
}

const DEFAULT_SAVED_MANIFEST = `apiVersion: v1
kind: Namespace
metadata:
  name: example`;

function ManifestsTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: manifests } = useQuery({ queryKey: ['cluster-saved-manifests', id], queryFn: () => getClusterSavedManifests(id) });
  const [viewing, setViewing] = useState<ClusterSavedManifestRecord | null>(null);
  const [editing, setEditing] = useState<ClusterSavedManifestRecord | null>(null);
  const [copiedManifestID, setCopiedManifestID] = useState<number | null>(null);
  const [selectedManifestID, setSelectedManifestID] = useState(0);
  const [selectedRunID, setSelectedRunID] = useState(0);
  const [form, setForm] = useState({
    name: '',
    namespace: 'default',
    description: '',
    raw_manifest: DEFAULT_SAVED_MANIFEST,
    is_global: false,
  });
  const [editForm, setEditForm] = useState({
    name: '',
    namespace: 'default',
    description: '',
    raw_manifest: DEFAULT_SAVED_MANIFEST,
    is_global: false,
  });

  const { data: revisions } = useQuery({
    queryKey: ['cluster-saved-manifest-revisions', id, selectedManifestID],
    queryFn: () => getClusterSavedManifestRevisions(id, selectedManifestID),
    enabled: selectedManifestID > 0,
  });
  const { data: runs } = useQuery({
    queryKey: ['cluster-saved-manifest-runs', id, selectedManifestID],
    queryFn: () => getClusterSavedManifestRuns(id, selectedManifestID),
    enabled: selectedManifestID > 0,
  });

  const resetForm = () => {
    setForm({
      name: '',
      namespace: 'default',
      description: '',
      raw_manifest: DEFAULT_SAVED_MANIFEST,
      is_global: false,
    });
  };

  const closeEdit = () => {
    setEditing(null);
    setEditForm({
      name: '',
      namespace: 'default',
      description: '',
      raw_manifest: DEFAULT_SAVED_MANIFEST,
      is_global: false,
    });
  };

  const saveMut = useMutation({
    mutationFn: async () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        description: form.description.trim(),
        raw_manifest: form.raw_manifest,
        is_global: form.is_global,
      };
      return createClusterSavedManifest(id, body);
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifests', id] });
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifest-revisions', id, data.id] });
      setSelectedManifestID(data.id);
      resetForm();
    },
  });

  const updateMut = useMutation({
    mutationFn: async () => {
      if (!editing) throw new Error('No manifest selected for editing');
      const body = {
        name: editForm.name.trim(),
        namespace: editForm.namespace.trim() || 'default',
        description: editForm.description.trim(),
        raw_manifest: editForm.raw_manifest,
        is_global: editForm.is_global,
      };
      return editing.is_global
        ? updateGlobalSavedManifest(editing.id, body)
        : updateClusterSavedManifest(id, editing.id, body);
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifests', id] });
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifest-revisions', id, data.id] });
      setSelectedManifestID(data.id);
      closeEdit();
    },
  });

  const applyMut = useMutation({
    mutationFn: (body: { manifestID: number; revisionID?: number }) => applyClusterSavedManifest(id, body.manifestID, body.revisionID),
    onSettled: (_data, _error, variables) => {
      if (!variables) return;
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifests', id] });
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifest-runs', id, variables.manifestID] });
    },
  });

  const deleteMut = useMutation({
    mutationFn: (manifestID: number) => deleteClusterSavedManifest(id, manifestID),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cluster-saved-manifests', id] });
      setSelectedManifestID(0);
      setSelectedRunID(0);
    },
  });

  const startEdit = (manifest: ClusterSavedManifestRecord) => {
    setEditing(manifest);
    setEditForm({
      name: manifest.name,
      namespace: manifest.namespace,
      description: manifest.description || '',
      raw_manifest: manifest.raw_manifest,
      is_global: manifest.is_global,
    });
  };

  const copyManifest = async (manifest: ClusterSavedManifestRecord) => {
    await navigator.clipboard.writeText(manifest.raw_manifest);
    setCopiedManifestID(manifest.id);
    window.setTimeout(() => setCopiedManifestID((current) => (current === manifest.id ? null : current)), 1600);
  };

  const selectedRun = runs?.find((run) => run.id === selectedRunID);

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>Create Manifest</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
              <Input label="Manifest name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
              <Input label="Default namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
              <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            <Textarea
              label="Manifest YAML"
              value={form.raw_manifest}
              onChange={(e) => setForm({ ...form, raw_manifest: e.target.value })}
              rows={16}
              className="font-mono text-xs"
              required
            />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Make available to all clusters</span>
            </label>
            <div className="flex gap-3">
              <Button type="submit" loading={saveMut.isPending}>Save Manifest</Button>
            </div>
            {saveMut.error && <ErrorMessage error={saveMut.error} />}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Saved Manifests</CardTitle></CardHeader>
        <CardContent>
          {!manifests?.length ? (
            <p className="text-sm text-slate-400">No saved manifests yet.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Name</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Namespace</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Scope</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Revision</th>
                    <th className="text-left px-4 py-2 font-medium text-slate-600">Last applied</th>
                    <th className="px-4 py-2" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {manifests.map((manifest) => (
                    <tr key={manifest.id}>
                      <td className="px-4 py-2 font-medium text-slate-900">{manifest.name}</td>
                      <td className="px-4 py-2 text-slate-600">{manifest.namespace}</td>
                      <td className="px-4 py-2 text-slate-600">{manifest.is_global ? 'Global' : 'Cluster'}</td>
                      <td className="px-4 py-2 text-slate-600">rev {manifest.latest_revision || 1}</td>
                      <td className="px-4 py-2 text-slate-500">{manifest.last_applied_at ? new Date(manifest.last_applied_at).toLocaleString() : '-'}</td>
                      <td className="px-4 py-2">
                        <div className="flex justify-end gap-2">
                          <Button type="button" size="sm" variant="secondary" onClick={() => setViewing(manifest)}>View</Button>
                          <Button type="button" size="sm" variant="secondary" onClick={() => { setSelectedManifestID(manifest.id); setSelectedRunID(0); }}>History</Button>
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(manifest)}>Edit</Button>
                          <Button type="button" size="sm" loading={applyMut.isPending && applyMut.variables?.manifestID === manifest.id} onClick={() => applyMut.mutate({ manifestID: manifest.id })}>Apply</Button>
                          {!manifest.is_global && <Button type="button" size="sm" variant="danger" loading={deleteMut.isPending && deleteMut.variables === manifest.id} onClick={() => deleteMut.mutate(manifest.id)}>Delete</Button>}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {applyMut.error && <ErrorMessage error={applyMut.error} />}
          {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
        </CardContent>
      </Card>

      {selectedManifestID > 0 && (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          <Card>
            <CardHeader><CardTitle>Revisions</CardTitle></CardHeader>
            <CardContent>
              {!revisions?.length ? (
                <p className="text-sm text-slate-400">No revisions recorded.</p>
              ) : (
                <div className="space-y-3">
                  {revisions.map((revision) => (
                    <div key={revision.id} className="rounded-md border border-slate-200 p-3">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-medium text-slate-900">Revision {revision.revision_number}</p>
                          <p className="text-xs text-slate-500">{new Date(revision.created_at).toLocaleString()}</p>
                        </div>
                        <Button type="button" size="sm" loading={applyMut.isPending && applyMut.variables?.revisionID === revision.id} onClick={() => applyMut.mutate({ manifestID: selectedManifestID, revisionID: revision.id })}>Apply revision</Button>
                      </div>
                      <pre className="mt-3 max-h-48 overflow-auto rounded bg-slate-950 p-3 text-xs text-slate-100">{revision.raw_manifest}</pre>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Apply Logs</CardTitle></CardHeader>
            <CardContent>
              {!runs?.length ? (
                <p className="text-sm text-slate-400">No apply runs recorded for this cluster.</p>
              ) : (
                <div className="space-y-3">
                  <div className="flex flex-wrap gap-2">
                    {runs.map((run) => (
                      <button
                        key={run.id}
                        type="button"
                        onClick={() => setSelectedRunID(run.id)}
                        className={`rounded-md border px-3 py-2 text-left text-xs ${selectedRunID === run.id ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600'}`}
                      >
                        <span className="font-medium">Run {run.id}</span>
                        <span className="block">{run.status} - {new Date(run.applied_at).toLocaleString()}</span>
                      </button>
                    ))}
                  </div>
                  {selectedRun ? (
                    <div className="space-y-3">
                      {selectedRun.error_message && <Alert variant="error">{selectedRun.error_message}</Alert>}
                      <pre className="max-h-96 overflow-auto rounded bg-slate-950 p-3 text-xs text-slate-100">{selectedRun.kubectl_output || 'No kubectl output recorded.'}</pre>
                    </div>
                  ) : (
                    <p className="text-sm text-slate-500">Select a run to view kubectl output.</p>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      <Modal open={Boolean(viewing)} onClose={() => setViewing(null)} title={viewing ? `View Manifest: ${viewing.name}` : 'View Manifest'} maxWidth="6xl">
        {viewing && (
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-4 gap-3 text-sm">
              <div>
                <p className="text-xs font-medium text-slate-500">Namespace</p>
                <p className="mt-1 text-slate-900">{viewing.namespace}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-slate-500">Scope</p>
                <p className="mt-1 text-slate-900">{viewing.is_global ? 'Global' : 'Cluster'}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-slate-500">Revision</p>
                <p className="mt-1 text-slate-900">rev {viewing.latest_revision || 1}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-slate-500">Last applied</p>
                <p className="mt-1 text-slate-900">{viewing.last_applied_at ? new Date(viewing.last_applied_at).toLocaleString() : '-'}</p>
              </div>
            </div>
            {viewing.description && <p className="text-sm text-slate-600">{viewing.description}</p>}
            <pre className="max-h-[60vh] overflow-auto rounded bg-slate-950 p-4 text-xs text-slate-100">{viewing.raw_manifest}</pre>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => copyManifest(viewing)}>
                {copiedManifestID === viewing.id ? <Check className="h-4 w-4" /> : <Clipboard className="h-4 w-4" />}
                {copiedManifestID === viewing.id ? 'Copied' : 'Copy'}
              </Button>
              <Button type="button" variant="secondary" onClick={() => setViewing(null)}>Close</Button>
              <Button
                type="button"
                onClick={() => {
                  startEdit(viewing);
                  setViewing(null);
                }}
              >
                Edit
              </Button>
            </div>
          </div>
        )}
      </Modal>

      <Modal open={Boolean(editing)} onClose={closeEdit} title={editing ? `Edit Manifest: ${editing.name}` : 'Edit Manifest'} maxWidth="6xl">
        {editing && (
          <form onSubmit={(e) => { e.preventDefault(); updateMut.mutate(); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Input label="Manifest name" value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} required />
              <Input label="Default namespace" value={editForm.namespace} onChange={(e) => setEditForm({ ...editForm, namespace: e.target.value })} required />
              <Input label="Description" value={editForm.description} onChange={(e) => setEditForm({ ...editForm, description: e.target.value })} />
            </div>
            <Textarea
              label="Manifest YAML"
              value={editForm.raw_manifest}
              onChange={(e) => setEditForm({ ...editForm, raw_manifest: e.target.value })}
              rows={18}
              className="font-mono text-xs"
              required
            />
            <label className="flex items-center gap-3">
              <input type="checkbox" checked={editForm.is_global} onChange={(e) => setEditForm({ ...editForm, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Available to all clusters</span>
            </label>
            {updateMut.error && <ErrorMessage error={updateMut.error} />}
            <div className="flex justify-end gap-3">
              <Button type="button" variant="secondary" onClick={closeEdit}>Cancel</Button>
              <Button type="submit" loading={updateMut.isPending}>Save Changes</Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
}

function ApplicationsTab({ id }: { id: number }) {
  const qc = useQueryClient();
  const { data: sslCertificates } = useQuery({ queryKey: ['ssl-certificates'], queryFn: getSSLCertificates });
  const { data: clusterSecrets } = useQuery({ queryKey: ['cluster-secrets', id], queryFn: () => getClusterSecrets(id) });
  const { data: clusterConfigMaps } = useQuery({ queryKey: ['cluster-config-maps', id], queryFn: () => getClusterConfigMaps(id) });
  const { data: clusterImagePullSecrets } = useQuery({ queryKey: ['cluster-image-pull-secrets', id], queryFn: () => getClusterImagePullSecrets(id) });
  const { data: clusterSavedManifests } = useQuery({ queryKey: ['cluster-saved-manifests', id], queryFn: () => getClusterSavedManifests(id) });
  const { data: appHistory } = useQuery({ queryKey: ['application-manifests', id], queryFn: () => getApplicationManifests(id) });
  const ingressMut = useMutation({ mutationFn: (b: Parameters<typeof deployIngressController>[1]) => deployIngressController(id, b) });
  const appMut = useMutation({
    mutationFn: (b: Parameters<typeof deployApp>[1]) => deployApp(id, b),
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ['application-manifests', id] });
      if (data.app_id) {
        qc.invalidateQueries({ queryKey: ['application-manifest-revisions', id, data.app_id] });
      }
    },
  });
  const redeployMut = useMutation({
    mutationFn: (b: { appID: number; revisionID?: number; apply: boolean }) =>
      redeployApplicationManifest(id, b.appID, { revision_id: b.revisionID, apply: b.apply }),
    onSuccess: (_data, variables) => {
      qc.invalidateQueries({ queryKey: ['application-manifests', id] });
      qc.invalidateQueries({ queryKey: ['application-manifest-revisions', id, variables.appID] });
    },
  });
  const [selectedHistoryAppID, setSelectedHistoryAppID] = useState(0);
  const { data: selectedRevisions } = useQuery({
    queryKey: ['application-manifest-revisions', id, selectedHistoryAppID],
    queryFn: () => getApplicationManifestRevisions(id, selectedHistoryAppID),
    enabled: selectedHistoryAppID > 0,
  });
  const [selectedRevision, setSelectedRevision] = useState<{
    raw_manifest: string;
    rendered_manifest: string;
    kubectl_output: string;
    error_message: string;
    revision_number: number;
    status: string;
  } | null>(null);
  const [ingressForm, setIngressForm] = useState({
    namespace: 'traefik',
    image: 'traefik:v3.3',
    replicas: '3',
    apply: true,
    wait: true,
  });
  const [appForm, setAppForm] = useState({
    name: 'sample-app',
    namespace: 'default',
    hostname: 'sample-app.example.com',
    ingress_class: 'traefik',
    tls_secret_name: 'sample-app-tls',
    tls_certificate_id: 0,
    tls_fullchain_pem: '',
    tls_private_key_pem: '',
    selected_secret_ids: [] as number[],
    apply_selected_secrets: true,
    selected_config_map_ids: [] as number[],
    apply_config_maps: true,
    selected_image_pull_secret_ids: [] as number[],
    apply_image_pull_secrets: true,
    inject_image_pull_secrets: true,
    selected_saved_manifest_id: 0,
    manifest: SAMPLE_APP_MANIFEST,
    apply: true,
  });

  const loadPEMFile = (field: 'tls_fullchain_pem' | 'tls_private_key_pem') => (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      setAppForm((f) => ({ ...f, [field]: String(reader.result ?? '') }));
    };
    reader.readAsText(file);
  };

  const loadRevisionIntoForm = async (appID: number, revisionID: number) => {
    const app = appHistory?.find((item) => item.id === appID);
    if (!app) return;
    const revision = await getApplicationManifestRevision(id, appID, revisionID);
    setAppForm({
      name: app.name,
      namespace: app.namespace,
      hostname: app.hostname,
      ingress_class: app.ingress_class || 'traefik',
      tls_secret_name: app.tls_secret_name,
      tls_certificate_id: app.tls_certificate_id || 0,
      tls_fullchain_pem: '',
      tls_private_key_pem: '',
      selected_secret_ids: [],
      apply_selected_secrets: true,
      selected_config_map_ids: [],
      apply_config_maps: true,
      selected_image_pull_secret_ids: [],
      apply_image_pull_secrets: true,
      inject_image_pull_secrets: true,
      selected_saved_manifest_id: 0,
      manifest: revision.raw_manifest,
      apply: true,
    });
  };

  const handleIngressDeploy = async (e: FormEvent) => {
    e.preventDefault();
    await ingressMut.mutateAsync({
      namespace: ingressForm.namespace.trim() || 'traefik',
      image: ingressForm.image.trim() || 'traefik:v3.3',
      replicas: Math.max(1, Number(ingressForm.replicas) || 3),
      apply: ingressForm.apply,
      wait: ingressForm.wait,
    });
  };

  const handleAppDeploy = async (e: FormEvent) => {
    e.preventDefault();
    await appMut.mutateAsync({
      name: appForm.name.trim() || 'application',
      namespace: appForm.namespace.trim() || 'default',
      hostname: appForm.hostname.trim(),
      ingress_class: appForm.ingress_class.trim() || 'traefik',
      tls_secret_name: appForm.tls_secret_name.trim() || `${appForm.name.trim() || 'application'}-tls`,
      tls_certificate_id: appForm.tls_certificate_id,
      tls_fullchain_pem: appForm.tls_fullchain_pem.trim(),
      tls_private_key_pem: appForm.tls_private_key_pem.trim(),
      selected_secret_ids: appForm.selected_secret_ids,
      apply_selected_secrets: appForm.apply_selected_secrets,
      selected_config_map_ids: appForm.selected_config_map_ids,
      apply_config_maps: appForm.apply_config_maps,
      selected_image_pull_secret_ids: appForm.selected_image_pull_secret_ids,
      apply_image_pull_secrets: appForm.apply_image_pull_secrets,
      inject_image_pull_secrets: appForm.inject_image_pull_secrets,
      manifest: appForm.manifest,
      apply: appForm.apply,
    });
  };

  const selectedHistoryApp = appHistory?.find((app) => app.id === selectedHistoryAppID);
  const availableSecrets = clusterSecrets ?? [];
  const availableConfigMaps = clusterConfigMaps ?? [];
  const availableImagePullSecrets = clusterImagePullSecrets ?? [];
  const availableSavedManifests = clusterSavedManifests ?? [];
  const toggleAppSecret = (secretID: number) => {
    setAppForm((prev) => ({
      ...prev,
      selected_secret_ids: prev.selected_secret_ids.includes(secretID)
        ? prev.selected_secret_ids.filter((id) => id !== secretID)
        : [...prev.selected_secret_ids, secretID],
    }));
  };
  const toggleAppConfigMap = (configMapID: number) => {
    setAppForm((prev) => ({
      ...prev,
      selected_config_map_ids: prev.selected_config_map_ids.includes(configMapID)
        ? prev.selected_config_map_ids.filter((id) => id !== configMapID)
        : [...prev.selected_config_map_ids, configMapID],
    }));
  };
  const toggleAppImagePullSecret = (secretID: number) => {
    setAppForm((prev) => ({
      ...prev,
      selected_image_pull_secret_ids: prev.selected_image_pull_secret_ids.includes(secretID)
        ? prev.selected_image_pull_secret_ids.filter((id) => id !== secretID)
        : [...prev.selected_image_pull_secret_ids, secretID],
    }));
  };
  const loadSavedManifestIntoAppForm = (manifest: ClusterSavedManifestRecord) => {
    setAppForm((prev) => ({
      ...prev,
      name: manifest.name || prev.name,
      namespace: manifest.namespace || prev.namespace,
      selected_saved_manifest_id: manifest.id,
      manifest: manifest.raw_manifest,
    }));
  };

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>Traefik Ingress Controller</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleIngressDeploy} className="space-y-4 max-w-xl">
            <Input
              label="Namespace"
              value={ingressForm.namespace}
              onChange={(e) => setIngressForm({ ...ingressForm, namespace: e.target.value })}
              placeholder="traefik"
              required
            />
            <Input
              label="Traefik image"
              value={ingressForm.image}
              onChange={(e) => setIngressForm({ ...ingressForm, image: e.target.value })}
              placeholder="traefik:v3.3"
              required
            />
            <Input
              label="Replicas"
              type="number"
              min={1}
              step={1}
              value={ingressForm.replicas}
              onChange={(e) => setIngressForm({ ...ingressForm, replicas: e.target.value })}
              helperText="Use 3 or more for HA Traefik deployments."
              required
            />
            <label className="flex items-center gap-3">
              <input
                type="checkbox"
                checked={ingressForm.apply}
                onChange={(e) => setIngressForm({ ...ingressForm, apply: e.target.checked })}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600"
              />
              <span className="text-sm text-slate-700">Apply to cluster now</span>
            </label>
            <label className="flex items-center gap-3">
              <input
                type="checkbox"
                checked={ingressForm.wait}
                onChange={(e) => setIngressForm({ ...ingressForm, wait: e.target.checked })}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600"
              />
              <span className="text-sm text-slate-700">Wait for Traefik rollout</span>
            </label>
            <Button type="submit" loading={ingressMut.isPending}>Deploy Traefik</Button>
            {ingressMut.data && (
              <Alert variant="success" title="Traefik deployment submitted">
                <div className="space-y-2">
                  <p>Status: {ingressMut.data.status}</p>
                  <p>Namespace: {ingressMut.data.namespace}</p>
                  <p>Replicas: {ingressMut.data.replicas}</p>
                  {ingressMut.data.operation_id && <p>Track progress in Live Operation Status.</p>}
                </div>
              </Alert>
            )}
            {ingressMut.error && <ErrorMessage error={ingressMut.error} />}
          </form>
          {ingressMut.data?.output && (
            <Textarea
              label="kubectl output"
              value={ingressMut.data.output}
              readOnly
              rows={8}
              className="mt-4 font-mono text-xs"
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Deploy Application Manifest</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleAppDeploy} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-xl">
              <Input
                label="Application name"
                value={appForm.name}
                onChange={(e) => setAppForm({ ...appForm, name: e.target.value })}
                required
              />
              <Input
                label="Namespace"
                value={appForm.namespace}
                onChange={(e) => setAppForm({ ...appForm, namespace: e.target.value })}
                required
              />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-3xl">
              <Input
                label="Application hostname"
                value={appForm.hostname}
                onChange={(e) => setAppForm({ ...appForm, hostname: e.target.value })}
                placeholder="app.example.com"
                helperText="Creates an Ingress for this host when set."
              />
              <Input
                label="Ingress class"
                value={appForm.ingress_class}
                onChange={(e) => setAppForm({ ...appForm, ingress_class: e.target.value })}
                placeholder="traefik"
              />
              <Input
                label="TLS secret name"
                value={appForm.tls_secret_name}
                onChange={(e) => setAppForm({ ...appForm, tls_secret_name: e.target.value })}
                placeholder="sample-app-tls"
              />
            </div>
            <div className="max-w-xl">
              <Select
                label="Saved TLS certificate"
                value={appForm.tls_certificate_id}
                onChange={(e) => setAppForm({
                  ...appForm,
                  tls_certificate_id: Number(e.target.value),
                  tls_fullchain_pem: '',
                  tls_private_key_pem: '',
                })}
              >
                <option value={0}>-- Upload PEMs for this deploy --</option>
                {sslCertificates?.map((cert) => (
                  <option key={cert.id} value={cert.id}>
                    {cert.name}{cert.dns_names?.length ? ` (${cert.dns_names.join(', ')})` : ''}
                  </option>
                ))}
              </Select>
            </div>
            <Alert variant="info" title="Existing certificate upload">
              Select a saved certificate or choose your Let&apos;s Encrypt fullchain.pem and privkey.pem to create/update the Kubernetes TLS secret before the app manifest is applied.
            </Alert>
            <div className="border border-slate-200 rounded-md p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium text-slate-900">Predefined application secrets</p>
                  <p className="text-xs text-slate-500">Select cluster secrets in the same namespace to apply before the manifest.</p>
                </div>
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={appForm.apply_selected_secrets}
                    onChange={(e) => setAppForm({ ...appForm, apply_selected_secrets: e.target.checked })}
                    className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                  />
                  <span className="text-sm text-slate-700">Apply selected secrets</span>
                </label>
              </div>
              {!availableSecrets.length ? (
                <p className="text-sm text-slate-400">No predefined secrets found.</p>
              ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                  {availableSecrets.map((secret) => (
                    <label key={secret.id} className="flex items-start gap-3 rounded border border-slate-200 p-3">
                      <input
                        type="checkbox"
                        checked={appForm.selected_secret_ids.includes(secret.id)}
                        onChange={() => toggleAppSecret(secret.id)}
                        className="mt-1 h-4 w-4 rounded border-slate-300 text-indigo-600"
                      />
                      <span>
                        <span className="block text-sm font-medium text-slate-900">{secret.name}</span>
                        <span className="block text-xs text-slate-500">
                          {secret.provider} · source namespace: {secret.namespace} · {secret.keys.join(', ') || 'no keys'}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className="border border-slate-200 rounded-md p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium text-slate-900">Image Pull Secrets</p>
                  <p className="text-xs text-slate-500">Select registry credentials to apply before the manifest and reference from pod templates.</p>
                </div>
                <div className="flex flex-wrap items-center gap-4">
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={appForm.apply_image_pull_secrets}
                      onChange={(e) => setAppForm({ ...appForm, apply_image_pull_secrets: e.target.checked })}
                      className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                    />
                    <span className="text-sm text-slate-700">Apply selected</span>
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={appForm.inject_image_pull_secrets}
                      onChange={(e) => setAppForm({ ...appForm, inject_image_pull_secrets: e.target.checked })}
                      className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                    />
                    <span className="text-sm text-slate-700">Inject into workloads</span>
                  </label>
                </div>
              </div>
              {!availableImagePullSecrets.length ? (
                <p className="text-sm text-slate-400">No image pull secrets found.</p>
              ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                  {availableImagePullSecrets.map((secret) => (
                    <label key={secret.id} className="flex items-start gap-3 rounded border border-slate-200 p-3">
                      <input
                        type="checkbox"
                        checked={appForm.selected_image_pull_secret_ids.includes(secret.id)}
                        onChange={() => toggleAppImagePullSecret(secret.id)}
                        className="mt-1 h-4 w-4 rounded border-slate-300 text-indigo-600"
                      />
                      <span>
                        <span className="block text-sm font-medium text-slate-900">{secret.name}</span>
                        <span className="block text-xs text-slate-500">
                          {secret.registry_server} · source namespace: {secret.namespace} · {secret.username}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className="border border-slate-200 rounded-md p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium text-slate-900">Predefined ConfigMaps</p>
                  <p className="text-xs text-slate-500">Select non-sensitive config in the same namespace to apply before the manifest.</p>
                </div>
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={appForm.apply_config_maps}
                    onChange={(e) => setAppForm({ ...appForm, apply_config_maps: e.target.checked })}
                    className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                  />
                  <span className="text-sm text-slate-700">Apply selected ConfigMaps</span>
                </label>
              </div>
              {!availableConfigMaps.length ? (
                <p className="text-sm text-slate-400">No predefined ConfigMaps found.</p>
              ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                  {availableConfigMaps.map((configMap) => (
                    <label key={configMap.id} className="flex items-start gap-3 rounded border border-slate-200 p-3">
                      <input
                        type="checkbox"
                        checked={appForm.selected_config_map_ids.includes(configMap.id)}
                        onChange={() => toggleAppConfigMap(configMap.id)}
                        className="mt-1 h-4 w-4 rounded border-slate-300 text-indigo-600"
                      />
                      <span>
                        <span className="block text-sm font-medium text-slate-900">{configMap.name}</span>
                        <span className="block text-xs text-slate-500">
                          source namespace: {configMap.namespace} · {Object.keys(configMap.data || {}).join(', ') || 'no keys'}
                        </span>
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className="border border-slate-200 rounded-md p-4 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium text-slate-900">Saved manifests</p>
                  <p className="text-xs text-slate-500">Select a stored manifest to populate the deployment YAML below.</p>
                </div>
                {appForm.selected_saved_manifest_id > 0 && (
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => setAppForm({ ...appForm, selected_saved_manifest_id: 0 })}
                  >
                    Clear selection
                  </Button>
                )}
              </div>
              {!availableSavedManifests.length ? (
                <p className="text-sm text-slate-400">No saved manifests found.</p>
              ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                  {availableSavedManifests.map((manifest) => (
                    <label key={manifest.id} className="flex items-start gap-3 rounded border border-slate-200 p-3">
                      <input
                        type="radio"
                        name="saved-manifest"
                        checked={appForm.selected_saved_manifest_id === manifest.id}
                        onChange={() => loadSavedManifestIntoAppForm(manifest)}
                        className="mt-1 h-4 w-4 border-slate-300 text-indigo-600"
                      />
                      <span>
                        <span className="block text-sm font-medium text-slate-900">{manifest.name}</span>
                        <span className="block text-xs text-slate-500">
                          {manifest.is_global ? 'global' : 'cluster'} · namespace: {manifest.namespace} · rev {manifest.latest_revision || 1}
                        </span>
                        {manifest.description && <span className="mt-1 block text-xs text-slate-500">{manifest.description}</span>}
                      </span>
                    </label>
                  ))}
                </div>
              )}
            </div>
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Input
                  label="fullchain.pem"
                  type="file"
                  accept=".pem,.crt"
                  onChange={loadPEMFile('tls_fullchain_pem')}
                  disabled={appForm.tls_certificate_id > 0}
                />
                <Textarea
                  label="Certificate PEM"
                  value={appForm.tls_fullchain_pem}
                  onChange={(e) => setAppForm({ ...appForm, tls_fullchain_pem: e.target.value })}
                  rows={6}
                  className="font-mono text-xs"
                  placeholder="-----BEGIN CERTIFICATE-----"
                  disabled={appForm.tls_certificate_id > 0}
                />
              </div>
              <div className="space-y-2">
                <Input
                  label="privkey.pem"
                  type="file"
                  accept=".pem,.key"
                  onChange={loadPEMFile('tls_private_key_pem')}
                  disabled={appForm.tls_certificate_id > 0}
                />
                <Textarea
                  label="Private key PEM"
                  value={appForm.tls_private_key_pem}
                  onChange={(e) => setAppForm({ ...appForm, tls_private_key_pem: e.target.value })}
                  rows={6}
                  className="font-mono text-xs"
                  placeholder="-----BEGIN PRIVATE KEY-----"
                  disabled={appForm.tls_certificate_id > 0}
                />
              </div>
            </div>
            <Textarea
              label="Kubernetes YAML"
              value={appForm.manifest}
              onChange={(e) => setAppForm({ ...appForm, manifest: e.target.value })}
              rows={18}
              className="font-mono text-xs"
              required
            />
            <label className="flex items-center gap-3">
              <input
                type="checkbox"
                checked={appForm.apply}
                onChange={(e) => setAppForm({ ...appForm, apply: e.target.checked })}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600"
              />
              <span className="text-sm text-slate-700">Apply manifest to cluster now</span>
            </label>
            <Button type="submit" loading={appMut.isPending}>Deploy App</Button>
            {appMut.data && (
              <Alert variant="success" title="Application deployment submitted">
                <div className="space-y-2">
                  <p>Status: {appMut.data.status}</p>
                  <p>Namespace: {appMut.data.namespace}</p>
                  {appMut.data.hostname && <p>Hostname: {appMut.data.hostname}</p>}
                  {appMut.data.tls_secret_name && <p>TLS secret: {appMut.data.tls_secret_name}</p>}
                  {appMut.data.secret_names?.length ? <p>Secrets: {appMut.data.secret_names.join(', ')}</p> : null}
                  {appMut.data.config_map_names?.length ? <p>ConfigMaps: {appMut.data.config_map_names.join(', ')}</p> : null}
                  {appMut.data.image_pull_secret_names?.length ? <p>Image pull secrets: {appMut.data.image_pull_secret_names.join(', ')}</p> : null}
                </div>
              </Alert>
            )}
            {appMut.error && <ErrorMessage error={appMut.error} />}
          </form>
          {appMut.data?.output && (
            <Textarea
              label="kubectl output"
              value={appMut.data.output}
              readOnly
              rows={10}
              className="mt-4 font-mono text-xs"
            />
          )}
          <KubectlCommandPanel id={id} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Deployed Application Manifests</CardTitle></CardHeader>
        <CardContent>
          {!appHistory?.length ? (
            <p className="text-sm text-slate-400">No application manifest deployments recorded yet.</p>
          ) : (
            <div className="space-y-4">
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-4 py-2 font-medium text-slate-600">App</th>
                      <th className="text-left px-4 py-2 font-medium text-slate-600">Namespace</th>
                      <th className="text-left px-4 py-2 font-medium text-slate-600">Hostname</th>
                      <th className="text-left px-4 py-2 font-medium text-slate-600">Revision</th>
                      <th className="text-left px-4 py-2 font-medium text-slate-600">Status</th>
                      <th className="text-left px-4 py-2 font-medium text-slate-600">Updated</th>
                      <th className="px-4 py-2" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {appHistory.map((app) => (
                      <tr key={app.id} className="hover:bg-slate-50">
                        <td className="px-4 py-2 font-medium text-slate-900">{app.name}</td>
                        <td className="px-4 py-2 text-slate-600">{app.namespace}</td>
                        <td className="px-4 py-2 text-slate-600">{app.hostname || '-'}</td>
                        <td className="px-4 py-2 text-slate-600">{app.latest_revision || '-'}</td>
                        <td className="px-4 py-2 text-slate-600">{app.latest_status || '-'}</td>
                        <td className="px-4 py-2 text-slate-500">{new Date(app.updated_at).toLocaleString()}</td>
                        <td className="px-4 py-2">
                          <div className="flex justify-end gap-2">
                            <Button
                              type="button"
                              size="sm"
                              variant="secondary"
                              onClick={() => setSelectedHistoryAppID(selectedHistoryAppID === app.id ? 0 : app.id)}
                            >
                              Revisions
                            </Button>
                            <Button
                              type="button"
                              size="sm"
                              variant="secondary"
                              disabled={!app.latest_revision_id}
                              onClick={() => app.latest_revision_id && loadRevisionIntoForm(app.id, app.latest_revision_id)}
                            >
                              Edit
                            </Button>
                            <Button
                              type="button"
                              size="sm"
                              loading={redeployMut.isPending && redeployMut.variables?.appID === app.id}
                              onClick={() => redeployMut.mutate({ appID: app.id, apply: true })}
                            >
                              Redeploy
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

              {selectedHistoryApp && (
                <div className="border border-slate-200 rounded-md overflow-hidden">
                  <div className="px-4 py-3 bg-slate-50 border-b border-slate-200">
                    <p className="font-medium text-slate-900">{selectedHistoryApp.name} revisions</p>
                  </div>
                  {!selectedRevisions?.length ? (
                    <p className="px-4 py-3 text-sm text-slate-400">No revisions found.</p>
                  ) : (
                    <div className="overflow-x-auto">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="border-b border-slate-200">
                            <th className="text-left px-4 py-2 font-medium text-slate-600">Revision</th>
                            <th className="text-left px-4 py-2 font-medium text-slate-600">Status</th>
                            <th className="text-left px-4 py-2 font-medium text-slate-600">Applied</th>
                            <th className="text-left px-4 py-2 font-medium text-slate-600">Created</th>
                            <th className="px-4 py-2" />
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100">
                          {selectedRevisions.map((revision) => (
                            <tr key={revision.id}>
                              <td className="px-4 py-2">{revision.revision_number}</td>
                              <td className="px-4 py-2">{revision.status}</td>
                              <td className="px-4 py-2">{revision.apply ? 'Yes' : 'No'}</td>
                              <td className="px-4 py-2 text-slate-500">{new Date(revision.created_at).toLocaleString()}</td>
                              <td className="px-4 py-2">
                                <div className="flex justify-end gap-2">
                                  <Button type="button" size="sm" variant="secondary" onClick={() => setSelectedRevision(revision)}>
                                    View
                                  </Button>
                                  <Button type="button" size="sm" variant="secondary" onClick={() => loadRevisionIntoForm(selectedHistoryApp.id, revision.id)}>
                                    Edit
                                  </Button>
                                  <Button
                                    type="button"
                                    size="sm"
                                    loading={redeployMut.isPending && redeployMut.variables?.revisionID === revision.id}
                                    onClick={() => redeployMut.mutate({ appID: selectedHistoryApp.id, revisionID: revision.id, apply: true })}
                                  >
                                    Redeploy
                                  </Button>
                                </div>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              )}
              {redeployMut.error && <ErrorMessage error={redeployMut.error} />}
            </div>
          )}
        </CardContent>
      </Card>

      <Modal
        open={!!selectedRevision}
        onClose={() => setSelectedRevision(null)}
        title={selectedRevision ? `Revision ${selectedRevision.revision_number}` : 'Revision'}
        maxWidth="6xl"
      >
        {selectedRevision && (
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
              <p>Status: {selectedRevision.status}</p>
              {selectedRevision.error_message && <p className="text-red-600">Error: {selectedRevision.error_message}</p>}
            </div>
            <Textarea
              label="Raw manifest"
              value={selectedRevision.raw_manifest}
              readOnly
              rows={18}
              className="font-mono text-xs min-h-[22rem] hover:min-h-[70vh] focus:min-h-[70vh]"
            />
            <Textarea
              label="Rendered manifest"
              value={selectedRevision.rendered_manifest}
              readOnly
              rows={18}
              className="font-mono text-xs min-h-[22rem] hover:min-h-[70vh] focus:min-h-[70vh]"
            />
            <Textarea
              label="kubectl output"
              value={selectedRevision.kubectl_output}
              readOnly
              rows={14}
              className="font-mono text-xs min-h-[18rem] hover:min-h-[70vh] focus:min-h-[70vh]"
            />
          </div>
        )}
      </Modal>
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
      {activeTab === 'storage' && <StorageTab id={clusterId} />}
      {activeTab === 'secrets' && <SecretsTab id={clusterId} />}
      {activeTab === 'configmaps' && <ConfigMapsTab id={clusterId} />}
      {activeTab === 'pullsecrets' && <ImagePullSecretsTab id={clusterId} />}
      {activeTab === 'manifests' && <ManifestsTab id={clusterId} />}
      {activeTab === 'applications' && <ApplicationsTab id={clusterId} />}
      {activeTab === 'observability' && <ObservabilityTab id={clusterId} />}
      {activeTab === 'etcd' && <EtcdTab id={clusterId} />}
    </Layout>
  );
}
