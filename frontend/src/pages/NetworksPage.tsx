import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Pencil, Star } from 'lucide-react';
import { getNetworks, createNetwork, updateNetwork, setDefaultNetwork, getNodeNetworkTags, upsertNodeNetworkTag, deleteNodeNetworkTag, getReservedIPs, createReservedIP, deleteReservedIP } from '../api/networks';
import { getNodes } from '../api/proxmox';
import type { NetworkProfile } from '../types';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

type NetworkFormData = {
  name: string;
  cidr: string;
  gateway: string;
  dns: string;
  bridge: string;
  vlan: number;
  is_default: boolean;
};

const emptyForm: NetworkFormData = {
  name: '',
  cidr: '',
  gateway: '',
  dns: '1.1.1.1,8.8.8.8',
  bridge: 'vmbr0',
  vlan: 0,
  is_default: false,
};

function NetworkForm({
  initial,
  onSubmit,
  loading,
  error,
  isEdit,
}: {
  initial: NetworkFormData;
  onSubmit: (data: NetworkFormData) => void;
  loading: boolean;
  error: unknown;
  isEdit: boolean;
}) {
  const [form, setForm] = useState<NetworkFormData>(initial);
  const set = <K extends keyof NetworkFormData>(k: K, v: NetworkFormData[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit(form);
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input label="Name" value={form.name} onChange={(e) => set('name', e.target.value)} required placeholder="homelab" />
      <Input label="CIDR" value={form.cidr} onChange={(e) => set('cidr', e.target.value)} required placeholder="192.168.8.0/24" />
      <Input label="Gateway" value={form.gateway} onChange={(e) => set('gateway', e.target.value)} required placeholder="192.168.8.1" />
      <Input label="DNS servers (comma-separated)" value={form.dns} onChange={(e) => set('dns', e.target.value)} placeholder="1.1.1.1,8.8.8.8" />
      <Input label="Bridge" value={form.bridge} onChange={(e) => set('bridge', e.target.value)} placeholder="vmbr0" />
      <Input label="VLAN (0 = none)" type="number" min={0} max={4094} value={form.vlan} onChange={(e) => set('vlan', Number(e.target.value))} />
      {!isEdit && (
        <label className="flex items-center gap-3">
          <input type="checkbox" checked={form.is_default} onChange={(e) => set('is_default', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
          <span className="text-sm text-slate-700">Set as default</span>
        </label>
      )}
      {!!error && <ErrorMessage error={error} />}
      <div className="flex gap-3 pt-2">
        <Button type="submit" loading={loading}>{isEdit ? 'Save Changes' : 'Create'}</Button>
      </div>
    </form>
  );
}

export function NetworksPage() {
  const qc = useQueryClient();
  const { data: networks, isLoading, error: networksError } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: nodeTags, error: nodeTagsError } = useQuery({ queryKey: ['network-node-tags'], queryFn: getNodeNetworkTags });
  const [createOpen, setCreateOpen] = useState(false);
  const [editNetwork, setEditNetwork] = useState<NetworkProfile | null>(null);
  const [reservedOpenFor, setReservedOpenFor] = useState<NetworkProfile | null>(null);
  const [reservedForm, setReservedForm] = useState({ hostname: '', ip_address: '', description: '' });

  const { data: reservedIPs, error: reservedError } = useQuery({
    queryKey: ['network-reserved-ips', reservedOpenFor?.id],
    queryFn: () => getReservedIPs(Number(reservedOpenFor?.id)),
    enabled: !!reservedOpenFor,
  });

  const createMut = useMutation({
    mutationFn: (d: NetworkFormData) =>
      createNetwork({ ...d, dns: d.dns.split(',').map((s) => s.trim()) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['networks'] }); setCreateOpen(false); },
  });

  const updateMut = useMutation({
    mutationFn: ({ id, d }: { id: number; d: NetworkFormData }) =>
      updateNetwork(id, { ...d, dns: d.dns.split(',').map((s) => s.trim()) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['networks'] }); setEditNetwork(null); },
  });

  const defaultMut = useMutation({
    mutationFn: setDefaultNetwork,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['networks'] }),
  });

  const upsertNodeTagMut = useMutation({
    mutationFn: ({ nodeName, networkProfileID }: { nodeName: string; networkProfileID: number }) =>
      upsertNodeNetworkTag(nodeName, networkProfileID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['network-node-tags'] }),
  });

  const deleteNodeTagMut = useMutation({
    mutationFn: deleteNodeNetworkTag,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['network-node-tags'] }),
  });

  const createReservedMut = useMutation({
    mutationFn: (payload: { networkID: number; hostname: string; ip_address: string; description: string }) =>
      createReservedIP(payload.networkID, {
        hostname: payload.hostname,
        ip_address: payload.ip_address,
        description: payload.description,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['network-reserved-ips', reservedOpenFor?.id] });
      setReservedForm({ hostname: '', ip_address: '', description: '' });
    },
  });

  const deleteReservedMut = useMutation({
    mutationFn: ({ networkID, reservedID }: { networkID: number; reservedID: number }) =>
      deleteReservedIP(networkID, reservedID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['network-reserved-ips', reservedOpenFor?.id] }),
  });

  const enabledNodesByName = Array.from(
    new Map((proxmoxNodes ?? []).filter((n) => n.enabled).map((n) => [n.node_name, n])).values(),
  );

  const tagMap = new Map((nodeTags ?? []).map((tag) => [tag.proxmox_node_name, tag]));

  return (
    <Layout>
      <PageHeader
        title="Networks"
        description="Manage network profiles for cluster provisioning"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" /> New Network
          </Button>
        }
      />
      {isLoading && <PageSpinner />}
      {networksError && <ErrorMessage error={networksError} />}
      {networks && (
        <Card>
          {networks.length === 0 ? (
            <div className="text-center py-16">
              <p className="text-slate-400 text-sm">No networks yet.</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">CIDR</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Gateway</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Bridge</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">VLAN</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">DNS</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {networks.map((n) => (
                    <tr key={n.id} className="hover:bg-slate-50">
                      <td className="px-6 py-4 font-medium text-slate-900 flex items-center gap-2">
                        {n.name}
                        {n.is_default && <Badge variant="success">default</Badge>}
                      </td>
                      <td className="px-6 py-4 text-slate-600 font-mono">{n.cidr}</td>
                      <td className="px-6 py-4 text-slate-600 font-mono">{n.gateway}</td>
                      <td className="px-6 py-4 text-slate-600">{n.bridge}</td>
                      <td className="px-6 py-4 text-slate-600">{n.vlan || '—'}</td>
                      <td className="px-6 py-4 text-slate-500 text-xs">{n.dns.join(', ')}</td>
                      <td className="px-6 py-4">
                        <div className="flex items-center gap-2">
                          {!n.is_default && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => defaultMut.mutate(n.id)}
                              loading={defaultMut.isPending}
                              title="Set as default"
                            >
                              <Star className="h-4 w-4" />
                            </Button>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() =>
                              setEditNetwork(n)
                            }
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setReservedOpenFor(n)}
                          >
                            Reserved IPs
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

      <div className="mt-6">
        <Card>
          <div className="px-6 py-4 border-b border-slate-200 bg-slate-50">
            <h3 className="text-base font-semibold text-slate-800">Per-Proxmox Node Network Assignment</h3>
            <p className="text-xs text-slate-600 mt-1">Assign a network profile (CIDR/gateway/bridge) per Proxmox node for cluster provisioning.</p>
          </div>
          <div className="p-6">
            {nodeTagsError && <ErrorMessage error={nodeTagsError} />}
            {enabledNodesByName.length === 0 ? (
              <p className="text-sm text-slate-500">No enabled Proxmox nodes found. Discover and enable nodes on the Proxmox page first.</p>
            ) : (
              <div className="space-y-3">
                {enabledNodesByName.map((node) => {
                  const tag = tagMap.get(node.node_name);
                  return (
                    <div key={node.node_name} className="grid grid-cols-1 sm:grid-cols-4 gap-3 items-end border border-slate-200 rounded-lg p-3">
                      <Input label="Proxmox node" value={node.node_name} readOnly />
                      <div className="sm:col-span-2">
                        <label className="block text-sm font-medium text-slate-700 mb-1">Network profile</label>
                        <select
                          className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
                          value={tag?.network_profile_id ?? 0}
                          onChange={(e) => {
                            const profileID = Number(e.target.value);
                            if (profileID <= 0) {
                              deleteNodeTagMut.mutate(node.node_name);
                              return;
                            }
                            upsertNodeTagMut.mutate({ nodeName: node.node_name, networkProfileID: profileID });
                          }}
                        >
                          <option value={0}>Use cluster-selected network profile</option>
                          {networks?.map((n) => (
                            <option key={n.id} value={n.id}>{n.name} ({n.cidr})</option>
                          ))}
                        </select>
                      </div>
                      <div className="text-xs text-slate-500">
                        {tag ? `Assigned: ${tag.network_name} (${tag.cidr})` : 'No explicit assignment'}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Card>
      </div>

      {/* Create modal */}
      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="New Network Profile">
        <NetworkForm
          initial={emptyForm}
          onSubmit={(d) => createMut.mutate(d)}
          loading={createMut.isPending}
          error={createMut.error}
          isEdit={false}
        />
      </Modal>

      {/* Edit modal */}
      <Modal open={!!editNetwork} onClose={() => setEditNetwork(null)} title="Edit Network Profile">
        {editNetwork && (
          <NetworkForm
            initial={{
              name: editNetwork.name,
              cidr: editNetwork.cidr,
              gateway: editNetwork.gateway,
              dns: editNetwork.dns.join(','),
              bridge: editNetwork.bridge,
              vlan: editNetwork.vlan,
              is_default: editNetwork.is_default,
            }}
            onSubmit={(d) => updateMut.mutate({ id: editNetwork.id, d })}
            loading={updateMut.isPending}
            error={updateMut.error}
            isEdit
          />
        )}
      </Modal>

      <Modal open={!!reservedOpenFor} onClose={() => setReservedOpenFor(null)} title={`Reserved IPs - ${reservedOpenFor?.name ?? ''}`}>
        <div className="space-y-4">
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!reservedOpenFor) return;
              createReservedMut.mutate({
                networkID: reservedOpenFor.id,
                hostname: reservedForm.hostname,
                ip_address: reservedForm.ip_address,
                description: reservedForm.description,
              });
            }}
          >
            <Input
              label="Hostname"
              value={reservedForm.hostname}
              onChange={(e) => setReservedForm((f) => ({ ...f, hostname: e.target.value }))}
              placeholder="nas01"
              required
            />
            <Input
              label="IP Address"
              value={reservedForm.ip_address}
              onChange={(e) => setReservedForm((f) => ({ ...f, ip_address: e.target.value }))}
              placeholder="192.168.8.20"
              required
            />
            <Input
              label="Description"
              value={reservedForm.description}
              onChange={(e) => setReservedForm((f) => ({ ...f, description: e.target.value }))}
              placeholder="NAS static IP"
            />
            {createReservedMut.error && <ErrorMessage error={createReservedMut.error} />}
            <Button type="submit" loading={createReservedMut.isPending}>Add Reserved IP</Button>
          </form>

          {reservedError && <ErrorMessage error={reservedError} />}

          <div className="overflow-x-auto border border-slate-200 rounded-lg">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 bg-slate-50">
                  <th className="text-left px-4 py-2 font-medium text-slate-600">Hostname</th>
                  <th className="text-left px-4 py-2 font-medium text-slate-600">IP</th>
                  <th className="text-left px-4 py-2 font-medium text-slate-600">Description</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {(reservedIPs ?? []).map((r) => (
                  <tr key={r.id}>
                    <td className="px-4 py-2 text-slate-900">{r.hostname}</td>
                    <td className="px-4 py-2 text-slate-600 font-mono">{r.ip_address}</td>
                    <td className="px-4 py-2 text-slate-600">{r.description || '—'}</td>
                    <td className="px-4 py-2 text-right">
                      <Button
                        variant="danger"
                        size="sm"
                        onClick={() => {
                          if (!reservedOpenFor) return;
                          deleteReservedMut.mutate({ networkID: reservedOpenFor.id, reservedID: r.id });
                        }}
                        loading={deleteReservedMut.isPending}
                      >
                        Remove
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {(reservedIPs ?? []).length === 0 && (
              <p className="px-4 py-3 text-sm text-slate-500">No reserved IPs for this network.</p>
            )}
          </div>
        </div>
      </Modal>
    </Layout>
  );
}
