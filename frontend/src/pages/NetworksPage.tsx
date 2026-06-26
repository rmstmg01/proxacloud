import { useState, type FormEvent } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Pencil, Star, Radar, RefreshCw } from 'lucide-react';
import { getNetworks, createNetwork, updateNetwork, setDefaultNetwork, getNodeNetworkTags, upsertNodeNetworkTag, deleteNodeNetworkTag, getReservedIPs, createReservedIP, deleteReservedIP, getVPCs, createVPC, getNetworkIPUsage, scanNetworkIPUsage } from '../api/networks';
import { getNodes } from '../api/proxmox';
import type { NetworkIPUsageEntry, NetworkProfile, VPC } from '../types';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

type NetworkFormData = {
  vpc_id: number;
  name: string;
  cidr: string;
  gateway: string;
  dns: string;
  bridge: string;
  vlan: number;
  is_default: boolean;
};

const emptyForm: NetworkFormData = {
  vpc_id: 0,
  name: '',
  cidr: '',
  gateway: '',
  dns: '1.1.1.1,8.8.8.8',
  bridge: 'vmbr0',
  vlan: 0,
  is_default: false,
};

function ipStatusVariant(status: string) {
  switch (status) {
    case 'used':
      return 'info' as const;
    case 'reserved':
      return 'warning' as const;
    case 'detected':
      return 'purple' as const;
    case 'conflict':
      return 'danger' as const;
    default:
      return 'default' as const;
  }
}

function formatDateTime(value?: string) {
  if (!value) return '-';
  return new Date(value).toLocaleString();
}

function sourceLabel(entry: NetworkIPUsageEntry) {
  return entry.source_type.replace(/_/g, ' ');
}

function NetworkForm({
  initial,
  vpcs,
  onSubmit,
  loading,
  error,
  isEdit,
}: {
  initial: NetworkFormData;
  vpcs: VPC[];
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
      <label className="block">
        <span className="block text-sm font-medium text-slate-700 mb-1">VPC</span>
        <select
          className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
          value={form.vpc_id}
          onChange={(e) => set('vpc_id', Number(e.target.value))}
          required
        >
          <option value={0}>-- Select VPC --</option>
          {vpcs.map((vpc) => (
            <option key={vpc.id} value={vpc.id}>
              {vpc.name} ({vpc.cidr}) {vpc.is_default ? '(default)' : ''}
            </option>
          ))}
        </select>
      </label>
      <Input label="Subnet Name" value={form.name} onChange={(e) => set('name', e.target.value)} required placeholder="prod-private-a" />
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
        <Button type="submit" loading={loading}>{isEdit ? 'Save Changes' : 'Create Subnet'}</Button>
      </div>
    </form>
  );
}

export function NetworksPage() {
  const qc = useQueryClient();
  const { data: vpcs } = useQuery({ queryKey: ['vpcs'], queryFn: getVPCs });
  const { data: networks, isLoading, error: networksError } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: nodeTags, error: nodeTagsError } = useQuery({ queryKey: ['network-node-tags'], queryFn: getNodeNetworkTags });
  const [createOpen, setCreateOpen] = useState(false);
  const [createVPCOpen, setCreateVPCOpen] = useState(false);
  const [editNetwork, setEditNetwork] = useState<NetworkProfile | null>(null);
  const [reservedOpenFor, setReservedOpenFor] = useState<NetworkProfile | null>(null);
  const [usageNetworkID, setUsageNetworkID] = useState(0);
  const [reservedForm, setReservedForm] = useState({ hostname: '', ip_address: '', description: '' });
  const [vpcForm, setVPCForm] = useState({ name: '', cidr: '10.0.0.0/16', description: '', is_default: false });

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

  const selectedUsageNetwork = (networks ?? []).find((network) => network.id === usageNetworkID) ?? networks?.[0];
  const selectedUsageNetworkID = selectedUsageNetwork?.id ?? 0;
  const { data: ipUsage, error: ipUsageError, isFetching: ipUsageFetching } = useQuery({
    queryKey: ['network-ip-usage', selectedUsageNetworkID],
    queryFn: () => getNetworkIPUsage(selectedUsageNetworkID),
    enabled: selectedUsageNetworkID > 0,
    refetchInterval: 60_000,
  });

  const createVPCMut = useMutation({
    mutationFn: createVPC,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['vpcs'] });
      setCreateVPCOpen(false);
      setVPCForm({ name: '', cidr: '10.0.0.0/16', description: '', is_default: false });
    },
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

  const scanUsageMut = useMutation({
    mutationFn: scanNetworkIPUsage,
    onSuccess: (resp) => {
      qc.setQueryData(['network-ip-usage', resp.network_profile_id], resp.usage);
      qc.invalidateQueries({ queryKey: ['network-ip-usage', resp.network_profile_id] });
    },
  });

  const enabledNodesByName = Array.from(
    new Map((proxmoxNodes ?? []).filter((n) => n.enabled).map((n) => [n.node_name, n])).values(),
  );

  const tagMap = new Map((nodeTags ?? []).map((tag) => [tag.proxmox_node_name, tag]));
  const vpcList = vpcs ?? [];
  const defaultVPCID = vpcList.find((vpc) => vpc.is_default)?.id ?? vpcList[0]?.id ?? 0;
  const createInitialForm = { ...emptyForm, vpc_id: defaultVPCID };

  return (
    <Layout>
      <PageHeader
        title="VPC Networks"
        description="Manage VPCs, private subnets, reserved IPs, and node network mapping."
        actions={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => setCreateVPCOpen(true)}>
              <Plus className="h-4 w-4" /> New VPC
            </Button>
            <Button onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" /> New Subnet
            </Button>
          </div>
        }
      />
      {isLoading && <PageSpinner />}
      {networksError && <ErrorMessage error={networksError} />}
      {vpcList.length > 0 && (
        <div className="mb-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {vpcList.map((vpc) => (
            <Card key={vpc.id}>
              <div className="p-5">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="text-sm font-semibold text-slate-900">{vpc.name}</p>
                    <p className="mt-1 font-mono text-sm text-slate-600">{vpc.cidr}</p>
                  </div>
                  {vpc.is_default && <Badge variant="success">default</Badge>}
                </div>
                <p className="mt-3 text-sm text-slate-500">{vpc.description || 'Private cloud network boundary'}</p>
                <p className="mt-3 text-xs text-slate-500">
                  {(networks ?? []).filter((network) => network.vpc_id === vpc.id).length} subnets
                </p>
              </div>
            </Card>
          ))}
        </div>
      )}
      {networks?.length ? (
        <Card className="mb-6">
          <div className="border-b border-slate-200 bg-slate-50 px-6 py-4">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <div>
                <h3 className="text-base font-semibold text-slate-800">IP Usage Dashboard</h3>
                <p className="mt-1 text-xs text-slate-600">Database inventory is calculated immediately. Live detection updates when you scan.</p>
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-slate-600">Subnet</span>
                  <select
                    className="min-w-64 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
                    value={selectedUsageNetworkID}
                    onChange={(event) => setUsageNetworkID(Number(event.target.value))}
                  >
                    {networks.map((network) => (
                      <option key={network.id} value={network.id}>
                        {network.vpc_name || 'default-vpc'} / {network.name} ({network.cidr})
                      </option>
                    ))}
                  </select>
                </label>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => selectedUsageNetworkID && scanUsageMut.mutate(selectedUsageNetworkID)}
                  loading={scanUsageMut.isPending}
                >
                  <Radar className="h-4 w-4" />
                  Scan network now
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => qc.invalidateQueries({ queryKey: ['network-ip-usage', selectedUsageNetworkID] })}
                  loading={ipUsageFetching}
                  title="Refresh IP usage"
                >
                  <RefreshCw className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
          <div className="p-6">
            {ipUsageError && <ErrorMessage error={ipUsageError} />}
            {scanUsageMut.error && <ErrorMessage error={scanUsageMut.error} />}
            {scanUsageMut.data?.warnings?.map((warning) => (
              <p key={warning} className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">{warning}</p>
            ))}
            {ipUsage ? (
              <div className="space-y-5">
                <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
                  {[
                    ['Usable', ipUsage.total_usable],
                    ['Used', ipUsage.used_count],
                    ['Reserved', ipUsage.reserved_count],
                    ['Detected', ipUsage.detected_count],
                    ['Available', ipUsage.available_count],
                    ['Conflicts', ipUsage.conflict_count],
                  ].map(([label, value]) => (
                    <div key={label} className="rounded-md border border-slate-200 px-4 py-3">
                      <p className="text-xs font-medium uppercase text-slate-500">{label}</p>
                      <p className="mt-1 text-2xl font-semibold text-slate-900">{value}</p>
                    </div>
                  ))}
                </div>

                <div className="grid gap-4 lg:grid-cols-[1.4fr_0.8fr]">
                  <div className="overflow-x-auto rounded-lg border border-slate-200">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b border-slate-200 bg-slate-50">
                          <th className="px-4 py-2 text-left font-medium text-slate-600">IP Address</th>
                          <th className="px-4 py-2 text-left font-medium text-slate-600">Status</th>
                          <th className="px-4 py-2 text-left font-medium text-slate-600">Source</th>
                          <th className="px-4 py-2 text-left font-medium text-slate-600">Used By</th>
                          <th className="px-4 py-2 text-left font-medium text-slate-600">Last Seen</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-slate-100">
                        {ipUsage.entries.map((entry, index) => (
                          <tr key={`${entry.ip_address}-${entry.source_type}-${index}`} className={entry.status === 'conflict' ? 'bg-red-50' : undefined}>
                            <td className="px-4 py-2 font-mono text-slate-800">{entry.ip_address}</td>
                            <td className="px-4 py-2"><Badge variant={ipStatusVariant(entry.status)}>{entry.status}</Badge></td>
                            <td className="px-4 py-2 text-slate-600">{sourceLabel(entry)}</td>
                            <td className="px-4 py-2 text-slate-700">{entry.source_name || '-'}</td>
                            <td className="px-4 py-2 text-slate-500">{formatDateTime(entry.last_seen_at)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {!ipUsage.entries.length && <p className="px-4 py-3 text-sm text-slate-500">No used, reserved, or detected IPs yet.</p>}
                  </div>

                  <div className="rounded-lg border border-slate-200 p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <p className="text-sm font-semibold text-slate-900">Available IP Preview</p>
                        <p className="mt-1 text-xs text-slate-500">Showing first {ipUsage.available_preview.length} available addresses.</p>
                      </div>
                      <Badge variant="success">{ipUsage.available_count} free</Badge>
                    </div>
                    <div className="mt-3 flex max-h-56 flex-wrap gap-2 overflow-auto">
                      {ipUsage.available_preview.map((ip) => (
                        <span key={ip} className="rounded bg-emerald-50 px-2 py-1 font-mono text-xs text-emerald-800">{ip}</span>
                      ))}
                      {!ipUsage.available_preview.length && <p className="text-sm text-slate-500">No available IPs in this subnet.</p>}
                    </div>
                    <p className="mt-3 text-xs text-slate-500">Last scan: {formatDateTime(ipUsage.last_scan_at)}</p>
                  </div>
                </div>
              </div>
            ) : (
              <PageSpinner />
            )}
          </div>
        </Card>
      ) : null}
      {networks && (
        <Card>
          {networks.length === 0 ? (
            <div className="text-center py-16">
              <p className="text-slate-400 text-sm">No subnets yet.</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Subnet</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">VPC</th>
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
                      <td className="px-6 py-4 text-slate-600">{n.vpc_name || 'default-vpc'}</td>
                      <td className="px-6 py-4 text-slate-600 font-mono">{n.cidr}</td>
                      <td className="px-6 py-4 text-slate-600 font-mono">{n.gateway}</td>
                      <td className="px-6 py-4 text-slate-600">{n.bridge}</td>
                      <td className="px-6 py-4 text-slate-600">{n.vlan || '-'}</td>
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
                            IP Reservations
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
            <h3 className="text-base font-semibold text-slate-800">Node Network Mapping</h3>
            <p className="text-xs text-slate-600 mt-1">Assign a default subnet per Proxmox node for cluster provisioning.</p>
          </div>
          <div className="p-6">
            {nodeTagsError && <ErrorMessage error={nodeTagsError} />}
            {enabledNodesByName.length === 0 ? (
              <p className="text-sm text-slate-500">No enabled nodes found. Discover and enable nodes on the Nodes page first.</p>
            ) : (
              <div className="space-y-3">
                {enabledNodesByName.map((node) => {
                  const tag = tagMap.get(node.node_name);
                  return (
                    <div key={node.node_name} className="grid grid-cols-1 sm:grid-cols-4 gap-3 items-end border border-slate-200 rounded-lg p-3">
                      <Input label="Proxmox node" value={node.node_name} readOnly />
                      <div className="sm:col-span-2">
                        <label className="block text-sm font-medium text-slate-700 mb-1">Subnet</label>
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
                          <option value={0}>Use workload-selected subnet</option>
                          {networks?.map((n) => (
                            <option key={n.id} value={n.id}>{n.vpc_name || 'default-vpc'} / {n.name} ({n.cidr})</option>
                          ))}
                        </select>
                      </div>
                      <div className="text-xs text-slate-500">
                        {tag ? `Assigned: ${tag.network_name} (${tag.cidr})` : 'No explicit mapping'}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Card>
      </div>

      <Modal open={createVPCOpen} onClose={() => setCreateVPCOpen(false)} title="New VPC">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            createVPCMut.mutate(vpcForm);
          }}
          className="space-y-4"
        >
          <Input label="VPC Name" value={vpcForm.name} onChange={(e) => setVPCForm({ ...vpcForm, name: e.target.value })} required placeholder="prod-vpc" />
          <Input label="CIDR" value={vpcForm.cidr} onChange={(e) => setVPCForm({ ...vpcForm, cidr: e.target.value })} required placeholder="10.0.0.0/16" />
          <Input label="Description" value={vpcForm.description} onChange={(e) => setVPCForm({ ...vpcForm, description: e.target.value })} placeholder="Production private network" />
          <label className="flex items-center gap-3">
            <input type="checkbox" checked={vpcForm.is_default} onChange={(e) => setVPCForm({ ...vpcForm, is_default: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
            <span className="text-sm text-slate-700">Set as default VPC</span>
          </label>
          {createVPCMut.error && <ErrorMessage error={createVPCMut.error} />}
          <Button type="submit" loading={createVPCMut.isPending}>Create VPC</Button>
        </form>
      </Modal>

      {/* Create modal */}
      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="New Subnet">
        <NetworkForm
          key={`create-${defaultVPCID}`}
          initial={createInitialForm}
          vpcs={vpcList}
          onSubmit={(d) => createMut.mutate(d)}
          loading={createMut.isPending}
          error={createMut.error}
          isEdit={false}
        />
      </Modal>

      {/* Edit modal */}
      <Modal open={!!editNetwork} onClose={() => setEditNetwork(null)} title="Edit Subnet">
        {editNetwork && (
          <NetworkForm
            initial={{
              vpc_id: editNetwork.vpc_id,
              name: editNetwork.name,
              cidr: editNetwork.cidr,
              gateway: editNetwork.gateway,
              dns: editNetwork.dns.join(','),
              bridge: editNetwork.bridge,
              vlan: editNetwork.vlan,
              is_default: editNetwork.is_default,
            }}
            vpcs={vpcList}
            onSubmit={(d) => updateMut.mutate({ id: editNetwork.id, d })}
            loading={updateMut.isPending}
            error={updateMut.error}
            isEdit
          />
        )}
      </Modal>

      <Modal open={!!reservedOpenFor} onClose={() => setReservedOpenFor(null)} title={`IP Reservations - ${reservedOpenFor?.name ?? ''}`}>
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
                    <td className="px-4 py-2 text-slate-600">{r.description || '-'}</td>
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
              <p className="px-4 py-3 text-sm text-slate-500">No reserved IPs for this subnet.</p>
            )}
          </div>
        </div>
      </Modal>
    </Layout>
  );
}
