import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Cpu, HardDrive, MemoryStick, Pencil, Plus, RefreshCw, ToggleLeft, ToggleRight } from 'lucide-react';
import { getCredentials, createCredential, updateCredential, deleteCredential, discoverNodes, getNodes, toggleNode } from '../api/proxmox';
import { getStorageInventory } from '../api/storage';
import { useAuth } from '../context/AuthContext';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { ProxmoxNode, ProxmoxCredential, NodeStorage } from '../types';

type AuthMethod = 'token' | 'password';

// ─── Helpers ──────────────────────────────────────────────────────────────────

function fmtBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const gb = bytes / 1_073_741_824;
  if (gb >= 1) return `${gb.toFixed(1)} GB`;
  const mb = bytes / 1_048_576;
  return `${mb.toFixed(0)} MB`;
}

function pct(used: number, total: number): number {
  if (total <= 0) return 0;
  return Math.min(100, Math.round((used / total) * 100));
}

function progressColor(p: number): string {
  if (p >= 90) return 'bg-red-500';
  if (p >= 70) return 'bg-amber-400';
  return 'bg-emerald-500';
}

function capacityLabel(stats: ConnectionStats): string {
  return `CPU ${pct(stats.cpuUsed, stats.cpuTotal)}% · MEM ${pct(stats.memoryUsed, stats.memoryTotal)}% · DISK ${pct(stats.diskUsed, stats.diskTotal)}%`;
}

function capacityTitle(stats: ConnectionStats): string {
  return [
    `CPU: ${stats.cpuUsed.toFixed(1)} / ${stats.cpuTotal} cores`,
    `Memory: ${fmtBytes(stats.memoryUsed)} / ${fmtBytes(stats.memoryTotal)}`,
    `Storage: ${fmtBytes(stats.diskUsed)} / ${fmtBytes(stats.diskTotal)}`,
  ].join('\n');
}

type NodeDiskUsage = {
  total: number;
  used: number;
  available: number;
  records: number;
  fromStorageInventory: boolean;
};

function aggregateStorageByNode(items: NodeStorage[]): Record<string, NodeDiskUsage> {
  return items.reduce<Record<string, NodeDiskUsage>>((acc, item) => {
    const current = acc[item.node_name] ?? { total: 0, used: 0, available: 0, records: 0, fromStorageInventory: true };
    current.total += item.total_bytes;
    current.used += item.used_bytes;
    current.available += item.available_bytes;
    current.records += 1;
    acc[item.node_name] = current;
    return acc;
  }, {});
}

function nodeDiskUsage(node: ProxmoxNode, storageByNode: Record<string, NodeDiskUsage>): NodeDiskUsage {
  const fromInventory = storageByNode[node.node_name];
  if (fromInventory && fromInventory.records > 0) return fromInventory;
  return {
    total: node.disk_total_bytes,
    used: node.disk_used_bytes,
    available: Math.max(0, node.disk_total_bytes - node.disk_used_bytes),
    records: 0,
    fromStorageInventory: false,
  };
}

type ConnectionStats = {
  nodes: ProxmoxNode[];
  totalNodes: number;
  onlineNodes: number;
  enabledNodes: number;
  status: string;
  lastSync: string;
  cpuUsed: number;
  cpuTotal: number;
  memoryUsed: number;
  memoryTotal: number;
  diskUsed: number;
  diskTotal: number;
};

function connectionStats(credential: ProxmoxCredential, nodes: ProxmoxNode[], storageByNode: Record<string, NodeDiskUsage>): ConnectionStats {
  const connectionNodes = nodes.filter((node) => node.credential_id === credential.id);
  const onlineNodes = connectionNodes.filter((node) => node.status === 'online');
  const enabledNodes = connectionNodes.filter((node) => node.enabled);
  const latestSync = connectionNodes
    .map((node) => new Date(node.last_discovered_at).getTime())
    .filter((time) => Number.isFinite(time))
    .sort((a, b) => b - a)[0];

  let status = 'Needs discovery';
  if (connectionNodes.length > 0 && onlineNodes.length === connectionNodes.length) status = 'Connected';
  else if (onlineNodes.length > 0) status = 'Degraded';
  else if (connectionNodes.length > 0) status = 'Unavailable';

  return {
    nodes: connectionNodes,
    totalNodes: connectionNodes.length,
    onlineNodes: onlineNodes.length,
    enabledNodes: enabledNodes.length,
    status,
    lastSync: latestSync ? new Date(latestSync).toLocaleString() : 'Never',
    cpuUsed: onlineNodes.reduce((sum, node) => sum + node.cpu_usage * node.cpu_count, 0),
    cpuTotal: onlineNodes.reduce((sum, node) => sum + node.cpu_count, 0),
    memoryUsed: onlineNodes.reduce((sum, node) => sum + node.memory_used_bytes, 0),
    memoryTotal: onlineNodes.reduce((sum, node) => sum + node.memory_total_bytes, 0),
    diskUsed: onlineNodes.reduce((sum, node) => sum + nodeDiskUsage(node, storageByNode).used, 0),
    diskTotal: onlineNodes.reduce((sum, node) => sum + nodeDiskUsage(node, storageByNode).total, 0),
  };
}

function UsageBar({ value, max, label }: { value: number; max: number; label?: string }) {
  const p = pct(value, max);
  return (
    <div className="space-y-0.5">
      {label && <span className="text-xs text-slate-500">{label}</span>}
      <div className="flex items-center gap-2">
        <div className="flex-1 h-1.5 rounded-full bg-slate-200 overflow-hidden">
          <div className={`h-full rounded-full ${progressColor(p)}`} style={{ width: `${p}%` }} />
        </div>
        <span className="text-xs font-medium text-slate-600 w-8 text-right">{p}%</span>
      </div>
    </div>
  );
}

function ConnectionStatusBadge({ status }: { status: string }) {
  if (status === 'Connected') return <Badge variant="success">Connected</Badge>;
  if (status === 'Degraded') return <Badge variant="warning">Degraded</Badge>;
  if (status === 'Unavailable') return <Badge variant="danger">Unavailable</Badge>;
  return <Badge variant="info">Needs discovery</Badge>;
}

function SummaryCards({ nodes, storageByNode }: { nodes: ProxmoxNode[]; storageByNode: Record<string, NodeDiskUsage> }) {
  const onlineNodes = nodes.filter((n) => n.status === 'online');

  const totalCPU = onlineNodes.reduce((s, n) => s + n.cpu_count, 0);
  const usedCPUCores = onlineNodes.reduce((s, n) => s + n.cpu_usage * n.cpu_count, 0);

  const totalMem = onlineNodes.reduce((s, n) => s + n.memory_total_bytes, 0);
  const usedMem = onlineNodes.reduce((s, n) => s + n.memory_used_bytes, 0);

  const totalDisk = onlineNodes.reduce((s, n) => s + nodeDiskUsage(n, storageByNode).total, 0);
  const usedDisk = onlineNodes.reduce((s, n) => s + nodeDiskUsage(n, storageByNode).used, 0);

  const cards = [
    {
      icon: <Cpu className="h-5 w-5 text-indigo-500" />,
      title: 'CPU',
      primary: `${totalCPU} cores`,
      secondary: `${usedCPUCores.toFixed(1)} cores in use`,
      used: usedCPUCores,
      total: totalCPU,
    },
    {
      icon: <MemoryStick className="h-5 w-5 text-violet-500" />,
      title: 'Memory',
      primary: fmtBytes(totalMem),
      secondary: `${fmtBytes(usedMem)} used`,
      used: usedMem,
      total: totalMem,
    },
    {
      icon: <HardDrive className="h-5 w-5 text-sky-500" />,
      title: 'Disk',
      primary: fmtBytes(totalDisk),
      secondary: `${fmtBytes(usedDisk)} used`,
      used: usedDisk,
      total: totalDisk,
    },
  ];

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
      {cards.map((c) => (
        <Card key={c.title}>
          <CardContent className="p-4">
            <div className="flex items-center gap-3 mb-3">
              {c.icon}
              <span className="text-sm font-semibold text-slate-700">{c.title}</span>
              <span className="ml-auto text-xs text-slate-400">{onlineNodes.length} online node{onlineNodes.length !== 1 ? 's' : ''}</span>
            </div>
            <p className="text-xl font-bold text-slate-900">{c.primary}</p>
            <p className="text-xs text-slate-500 mb-2">{c.secondary}</p>
            <UsageBar value={c.used} max={c.total} />
          </CardContent>
        </Card>
      ))}
    </div>
  );
}


function AddCredentialForm({ onSuccess }: { onSuccess: () => void }) {
  const [authMethod, setAuthMethod] = useState<AuthMethod>('token');
  const [form, setForm] = useState({
    name: '',
    endpoint: 'https://pve.local:8006',
    insecure_tls: false,
    api_token_id: '',
    api_token_secret: '',
    username: '',
    password: '',
  });
  const mutation = useMutation({ mutationFn: createCredential, onSuccess });
  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const payload =
      authMethod === 'token'
        ? { name: form.name, endpoint: form.endpoint, insecure_tls: form.insecure_tls, api_token_id: form.api_token_id, api_token_secret: form.api_token_secret }
        : { name: form.name, endpoint: form.endpoint, insecure_tls: form.insecure_tls, username: form.username, password: form.password };
    mutation.mutate(payload);
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input label="Name" value={form.name} onChange={(e) => set('name', e.target.value)} required placeholder="pve-cluster" />
      <Input label="Endpoint" value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} required placeholder="https://pve.local:8006" />
      <Select label="Auth method" value={authMethod} onChange={(e) => setAuthMethod(e.target.value as AuthMethod)}>
        <option value="token">API Token (recommended)</option>
        <option value="password">Username / Password</option>
      </Select>
      {authMethod === 'token' ? (
        <>
          <Input label="Token ID" value={form.api_token_id} onChange={(e) => set('api_token_id', e.target.value)} placeholder="root@pam!mytoken" required />
          <Input label="Token secret" type="password" value={form.api_token_secret} onChange={(e) => set('api_token_secret', e.target.value)} required />
        </>
      ) : (
        <>
          <Input label="Username" value={form.username} onChange={(e) => set('username', e.target.value)} placeholder="root@pam" required />
          <Input label="Password" type="password" value={form.password} onChange={(e) => set('password', e.target.value)} required />
        </>
      )}
      <label className="flex items-center gap-3">
        <input type="checkbox" checked={form.insecure_tls} onChange={(e) => set('insecure_tls', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
        <span className="text-sm text-slate-700">Skip TLS verification</span>
      </label>
      {mutation.error && <ErrorMessage error={mutation.error} />}
      <Button type="submit" loading={mutation.isPending}>Add Connection</Button>
    </form>
  );
}

function EditCredentialForm({ credential, onSuccess }: { credential: ProxmoxCredential; onSuccess: () => void }) {
  const [authMethod, setAuthMethod] = useState<AuthMethod>(credential.api_token_id ? 'token' : 'password');
  const [form, setForm] = useState({
    name: credential.name,
    endpoint: credential.endpoint,
    insecure_tls: credential.insecure_tls,
    api_token_id: credential.api_token_id ?? '',
    api_token_secret: '',
    username: credential.username ?? '',
    password: '',
  });
  const mutation = useMutation({ mutationFn: (body: Parameters<typeof updateCredential>[1]) => updateCredential(credential.id, body), onSuccess });
  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    const payload =
      authMethod === 'token'
        ? { name: form.name, endpoint: form.endpoint, insecure_tls: form.insecure_tls, api_token_id: form.api_token_id, api_token_secret: form.api_token_secret }
        : { name: form.name, endpoint: form.endpoint, insecure_tls: form.insecure_tls, username: form.username, password: form.password };
    mutation.mutate(payload);
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input label="Name" value={form.name} onChange={(e) => set('name', e.target.value)} required placeholder="pve-cluster" />
      <Input label="Endpoint" value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} required placeholder="https://pve.local:8006" />
      <Select label="Auth method" value={authMethod} onChange={(e) => setAuthMethod(e.target.value as AuthMethod)}>
        <option value="token">API Token (recommended)</option>
        <option value="password">Username / Password</option>
      </Select>
      {authMethod === 'token' ? (
        <>
          <Input label="Token ID" value={form.api_token_id} onChange={(e) => set('api_token_id', e.target.value)} placeholder="root@pam!mytoken" required />
          <Input label="Token secret" type="password" value={form.api_token_secret} onChange={(e) => set('api_token_secret', e.target.value)} required />
        </>
      ) : (
        <>
          <Input label="Username" value={form.username} onChange={(e) => set('username', e.target.value)} placeholder="root@pam" required />
          <Input label="Password" type="password" value={form.password} onChange={(e) => set('password', e.target.value)} required />
        </>
      )}
      <label className="flex items-center gap-3">
        <input type="checkbox" checked={form.insecure_tls} onChange={(e) => set('insecure_tls', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
        <span className="text-sm text-slate-700">Skip TLS verification</span>
      </label>
      <p className="text-xs text-slate-500">For security, enter password/token secret again when editing this connection.</p>
      {mutation.error && <ErrorMessage error={mutation.error} />}
      <Button type="submit" loading={mutation.isPending}>Save Changes</Button>
    </form>
  );
}

export function ProxmoxPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === 'super_admin';
  const qc = useQueryClient();
  const [addCredOpen, setAddCredOpen] = useState(false);
  const [editCred, setEditCred] = useState<ProxmoxCredential | null>(null);

  const { data: credentials, isLoading: loadingCreds } = useQuery({
    queryKey: ['proxmox-credentials'],
    queryFn: getCredentials,
    enabled: isAdmin,
  });

  const { data: nodes, isLoading: loadingNodes } = useQuery({
    queryKey: ['proxmox-nodes'],
    queryFn: getNodes,
    refetchInterval: 60_000,
    refetchIntervalInBackground: true,
  });

  const { data: storageInventory, isLoading: loadingStorage, error: storageError } = useQuery({
    queryKey: ['storage-inventory'],
    queryFn: getStorageInventory,
    refetchInterval: 60_000,
    refetchIntervalInBackground: true,
  });
  const storageByNode = aggregateStorageByNode(storageInventory?.items ?? []);

  const discoverMut = useMutation({
    mutationFn: discoverNodes,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
      qc.invalidateQueries({ queryKey: ['storage-inventory'] });
    },
  });

  const deleteCredMut = useMutation({
    mutationFn: deleteCredential,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['proxmox-credentials'] });
      qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
      qc.invalidateQueries({ queryKey: ['storage-inventory'] });
    },
  });

  const toggleMut = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => toggleNode(id, enabled),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
      qc.invalidateQueries({ queryKey: ['storage-inventory'] });
    },
  });

  const refreshInventory = () => {
    qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
    qc.invalidateQueries({ queryKey: ['storage-inventory'] });
  };

  return (
    <Layout>
      <PageHeader title="Nodes" description="Manage Proxmox node inventory and connection details" />

      {/* Connections (super_admin only) */}
      {isAdmin && (
        <section className="mb-8">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-slate-800">Connections</h2>
            <Button onClick={() => setAddCredOpen(true)} size="sm">
              <Plus className="h-4 w-4" /> Add Connection
            </Button>
          </div>
          {loadingCreds ? (
            <PageSpinner />
          ) : (
            <Card>
              {!credentials || credentials.length === 0 ? (
                <CardContent><p className="text-sm text-slate-400">No connections added yet.</p></CardContent>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-slate-200 bg-slate-50">
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Nodes</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Enabled</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Capacity</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Last Sync</th>
                        <th className="px-6 py-3 text-right font-medium text-slate-600">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                      {credentials.map((c) => {
                        const stats = connectionStats(c, nodes ?? [], storageByNode);
                        return (
                          <tr key={c.id} className="hover:bg-slate-50">
                            <td className="px-6 py-3 font-medium text-slate-900">{c.name}</td>
                            <td className="px-6 py-3"><ConnectionStatusBadge status={stats.status} /></td>
                            <td className="px-6 py-3 text-slate-600">
                              <span className="font-medium text-slate-900">{stats.totalNodes}</span>
                              <span className="text-xs text-slate-400"> total</span>
                              {stats.totalNodes > 0 && (
                                <span className="block text-xs text-slate-500">{stats.onlineNodes} online</span>
                              )}
                            </td>
                            <td className="px-6 py-3 text-slate-600">
                              <span className="font-medium text-slate-900">{stats.enabledNodes}</span>
                              <span className="text-xs text-slate-400"> / {stats.totalNodes}</span>
                            </td>
                            <td className="px-6 py-3 text-slate-700 text-xs font-medium whitespace-nowrap" title={capacityTitle(stats)}>
                              {capacityLabel(stats)}
                            </td>
                            <td className="px-6 py-3 text-slate-500 text-xs whitespace-nowrap">{stats.lastSync}</td>
                            <td className="px-6 py-3">
                              <div className="flex items-center gap-2 justify-end">
                                <Button
                                  variant="secondary"
                                  size="sm"
                                  onClick={() => setEditCred(c)}
                                >
                                  <Pencil className="h-3.5 w-3.5" /> Edit
                                </Button>
                                <Button
                                  variant="secondary"
                                  size="sm"
                                  onClick={() => discoverMut.mutate(c.id)}
                                  loading={discoverMut.isPending && discoverMut.variables === c.id}
                                >
                                  <RefreshCw className="h-3.5 w-3.5" /> Discover Nodes
                                </Button>
                                <Button
                                  variant="danger"
                                  size="sm"
                                  onClick={() => {
                                    if (window.confirm(`Delete connection \"${c.name}\"? This also removes its discovered nodes.`)) {
                                      deleteCredMut.mutate(c.id);
                                    }
                                  }}
                                  loading={deleteCredMut.isPending && deleteCredMut.variables === c.id}
                                >
                                  Delete
                                </Button>
                              </div>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          )}
          {discoverMut.data && (
            <Alert variant="success" className="mt-3">
              Discovered {discoverMut.data.length} node(s).
            </Alert>
          )}
          {discoverMut.error && <ErrorMessage error={discoverMut.error} />}
          {deleteCredMut.error && <ErrorMessage error={deleteCredMut.error} />}
        </section>
      )}

      {/* Nodes */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-slate-800">Nodes</h2>
          <Button variant="secondary" size="sm" onClick={refreshInventory}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        </div>
        {loadingNodes || loadingStorage ? (
          <PageSpinner />
        ) : !nodes || nodes.length === 0 ? (
          <Card>
            <CardContent>
              <p className="text-sm text-slate-400">
                No nodes discovered yet.
                {isAdmin ? ' Add a connection and click "Discover Nodes".' : ' Ask an admin to discover nodes.'}
              </p>
            </CardContent>
          </Card>
        ) : (
          <>
            {storageError && <ErrorMessage error={storageError} />}
            {storageInventory?.errors?.length ? (
              <Alert variant="warning" title="Some storage inventory could not be queried">
                {storageInventory.errors.map((item) => `${item.node_name}: ${item.error}`).join(' | ')}
              </Alert>
            ) : null}
            <SummaryCards nodes={nodes} storageByNode={storageByNode} />
            <Card>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Node</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Connection</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600 min-w-[160px]">CPU</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600 min-w-[180px]">Memory</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600 min-w-[180px]">Disk</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Last Seen</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Enabled</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {nodes.map((n) => {
                      const cpuPct = Math.round(n.cpu_usage * 100);
                      const disk = nodeDiskUsage(n, storageByNode);
                      return (
                        <tr key={n.id} className="hover:bg-slate-50">
                          <td className="px-6 py-3 font-medium text-slate-900">{n.node_name}</td>
                          <td className="px-6 py-3 text-slate-500">{n.credential_name ?? `#${n.credential_id}`}</td>
                          <td className="px-6 py-3"><StatusBadge status={n.status} /></td>
                          <td className="px-6 py-3">
                            <div className="text-slate-700 text-xs mb-1">{n.cpu_count} cores · {cpuPct}%</div>
                            <UsageBar value={n.cpu_usage * n.cpu_count} max={n.cpu_count} />
                          </td>
                          <td className="px-6 py-3">
                            <div className="text-slate-700 text-xs mb-1">{fmtBytes(n.memory_used_bytes)} / {fmtBytes(n.memory_total_bytes)}</div>
                            <UsageBar value={n.memory_used_bytes} max={n.memory_total_bytes} />
                          </td>
                          <td className="px-6 py-3">
                            {disk.total > 0 ? (
                              <>
                                <div className="text-slate-700 text-xs mb-1">{fmtBytes(disk.used)} / {fmtBytes(disk.total)}</div>
                                <UsageBar value={disk.used} max={disk.total} />
                                <p className="mt-1 text-[11px] text-slate-400">
                                  {disk.fromStorageInventory ? `${disk.records} storage record${disk.records !== 1 ? 's' : ''}` : 'node disk fallback'}
                                </p>
                              </>
                            ) : (
                              <span className="text-slate-400 text-xs">—</span>
                            )}
                          </td>
                          <td className="px-6 py-3 text-slate-500 text-xs whitespace-nowrap">
                            {new Date(n.last_discovered_at).toLocaleString()}
                          </td>
                          <td className="px-6 py-3">
                            {isAdmin ? (
                              <button
                                onClick={() => toggleMut.mutate({ id: n.id, enabled: !n.enabled })}
                                disabled={toggleMut.isPending}
                                className="text-slate-500 hover:text-indigo-600 transition-colors disabled:opacity-50"
                              >
                                {n.enabled ? (
                                  <ToggleRight className="h-6 w-6 text-emerald-500" />
                                ) : (
                                  <ToggleLeft className="h-6 w-6 text-slate-400" />
                                )}
                              </button>
                            ) : (
                              <Badge variant={n.enabled ? 'success' : 'default'}>{n.enabled ? 'yes' : 'no'}</Badge>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          </>
        )}
      </section>

      <Modal open={addCredOpen} onClose={() => setAddCredOpen(false)} title="Add Connection">
        <AddCredentialForm onSuccess={() => { setAddCredOpen(false); qc.invalidateQueries({ queryKey: ['proxmox-credentials'] }); qc.invalidateQueries({ queryKey: ['storage-inventory'] }); }} />
      </Modal>

      <Modal open={!!editCred} onClose={() => setEditCred(null)} title="Edit Connection">
        {editCred && (
          <EditCredentialForm
            credential={editCred}
            onSuccess={() => {
              setEditCred(null);
              qc.invalidateQueries({ queryKey: ['proxmox-credentials'] });
              qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
              qc.invalidateQueries({ queryKey: ['storage-inventory'] });
            }}
          />
        )}
      </Modal>
    </Layout>
  );
}
