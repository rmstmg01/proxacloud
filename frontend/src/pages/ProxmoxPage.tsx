import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Cpu, HardDrive, MemoryStick, Pencil, Plus, RefreshCw, ToggleLeft, ToggleRight } from 'lucide-react';
import { getCredentials, createCredential, updateCredential, deleteCredential, discoverNodes, getNodes, toggleNode } from '../api/proxmox';
import { useAuth } from '../context/AuthContext';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { ProxmoxNode, ProxmoxCredential } from '../types';

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

function SummaryCards({ nodes }: { nodes: ProxmoxNode[] }) {
  const onlineNodes = nodes.filter((n) => n.status === 'online');

  const totalCPU = onlineNodes.reduce((s, n) => s + n.cpu_count, 0);
  const usedCPUCores = onlineNodes.reduce((s, n) => s + n.cpu_usage * n.cpu_count, 0);

  const totalMem = onlineNodes.reduce((s, n) => s + n.memory_total_bytes, 0);
  const usedMem = onlineNodes.reduce((s, n) => s + n.memory_used_bytes, 0);

  const totalDisk = onlineNodes.reduce((s, n) => s + n.disk_total_bytes, 0);
  const usedDisk = onlineNodes.reduce((s, n) => s + n.disk_used_bytes, 0);

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
      <Button type="submit" loading={mutation.isPending}>Add Nodes</Button>
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
      <p className="text-xs text-slate-500">For security, enter password/token secret again when editing credentials.</p>
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

  const discoverMut = useMutation({
    mutationFn: discoverNodes,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['proxmox-nodes'] }),
  });

  const deleteCredMut = useMutation({
    mutationFn: deleteCredential,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['proxmox-credentials'] });
      qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
    },
  });

  const toggleMut = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => toggleNode(id, enabled),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['proxmox-nodes'] }),
  });

  return (
    <Layout>
      <PageHeader title="Nodes" description="Manage Proxmox node inventory and connection details" />

      {/* Credentials (super_admin only) */}
      {isAdmin && (
        <section className="mb-8">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-slate-800">Credentials</h2>
            <Button onClick={() => setAddCredOpen(true)} size="sm">
              <Plus className="h-4 w-4" /> Add Nodes
            </Button>
          </div>
          {loadingCreds ? (
            <PageSpinner />
          ) : (
            <Card>
              {!credentials || credentials.length === 0 ? (
                <CardContent><p className="text-sm text-slate-400">No credentials added yet.</p></CardContent>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-slate-200 bg-slate-50">
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Endpoint</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">Auth</th>
                        <th className="text-left px-6 py-3 font-medium text-slate-600">TLS</th>
                        <th className="px-6 py-3" />
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                      {credentials.map((c) => (
                        <tr key={c.id} className="hover:bg-slate-50">
                          <td className="px-6 py-3 font-medium text-slate-900">{c.name}</td>
                          <td className="px-6 py-3 text-slate-600 font-mono text-xs">{c.endpoint}</td>
                          <td className="px-6 py-3">
                            {c.api_token_id ? (
                              <Badge variant="info">API Token</Badge>
                            ) : (
                              <Badge variant="default">Password</Badge>
                            )}
                          </td>
                          <td className="px-6 py-3">
                            {c.insecure_tls ? (
                              <Badge variant="warning">insecure</Badge>
                            ) : (
                              <Badge variant="success">verified</Badge>
                            )}
                          </td>
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
                                  if (window.confirm(`Delete credential \"${c.name}\"? This also removes its discovered nodes.`)) {
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
                      ))}
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
          <Button variant="secondary" size="sm" onClick={() => qc.invalidateQueries({ queryKey: ['proxmox-nodes'] })}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        </div>
        {loadingNodes ? (
          <PageSpinner />
        ) : !nodes || nodes.length === 0 ? (
          <Card>
            <CardContent>
              <p className="text-sm text-slate-400">
                No nodes discovered yet.
                {isAdmin ? ' Add credentials and click "Discover Nodes".' : ' Ask an admin to discover nodes.'}
              </p>
            </CardContent>
          </Card>
        ) : (
          <>
            <SummaryCards nodes={nodes} />
            <Card>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Node</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Credential</th>
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
                            {n.disk_total_bytes > 0 ? (
                              <>
                                <div className="text-slate-700 text-xs mb-1">{fmtBytes(n.disk_used_bytes)} / {fmtBytes(n.disk_total_bytes)}</div>
                                <UsageBar value={n.disk_used_bytes} max={n.disk_total_bytes} />
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

      <Modal open={addCredOpen} onClose={() => setAddCredOpen(false)} title="Add Nodes">
        <AddCredentialForm onSuccess={() => { setAddCredOpen(false); qc.invalidateQueries({ queryKey: ['proxmox-credentials'] }); }} />
      </Modal>

      <Modal open={!!editCred} onClose={() => setEditCred(null)} title="Edit Proxmox Credential">
        {editCred && (
          <EditCredentialForm
            credential={editCred}
            onSuccess={() => {
              setEditCred(null);
              qc.invalidateQueries({ queryKey: ['proxmox-credentials'] });
              qc.invalidateQueries({ queryKey: ['proxmox-nodes'] });
            }}
          />
        )}
      </Modal>
    </Layout>
  );
}
