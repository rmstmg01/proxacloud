import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowRightLeft, MapPin, Pencil, Plus, Trash2 } from 'lucide-react';
import { createRegion, deleteRegion, getRegions, setRegionNetworks, setRegionNodes, updateRegion } from '../api/regions';
import { getNetworks } from '../api/networks';
import { getNodes } from '../api/proxmox';
import type { NetworkProfile, ProxmoxNode, Region } from '../types';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

type RegionFormData = {
  name: string;
  description: string;
  enabled: boolean;
  is_default: boolean;
  network_profile_ids: number[];
};

type MoveNodePayload = {
  nodeID: number;
  fromRegionID: number;
  toRegionID: number;
};

const emptyRegionForm: RegionFormData = {
  name: '',
  description: '',
  enabled: true,
  is_default: false,
  network_profile_ids: [],
};

function RegionForm({
  initial,
  networks,
  onSubmit,
  loading,
  error,
  isEdit,
}: {
  initial: RegionFormData;
  networks: NetworkProfile[];
  onSubmit: (data: RegionFormData) => void;
  loading: boolean;
  error: unknown;
  isEdit: boolean;
}) {
  const [form, setForm] = useState<RegionFormData>(initial);
  const set = <K extends keyof RegionFormData>(key: K, value: RegionFormData[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));
  const selectedNumbers = (options: HTMLCollectionOf<HTMLOptionElement>) =>
    Array.from(options).filter((option) => option.selected).map((option) => Number(option.value));

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    onSubmit(form);
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input label="Region Name" value={form.name} onChange={(event) => set('name', event.target.value)} required placeholder="Kathmandu" />
      <Input label="Description" value={form.description} onChange={(event) => set('description', event.target.value)} placeholder="Primary location" />
      <label className="block">
        <span className="mb-1 block text-sm font-medium text-slate-700">Subnets</span>
        <select
          multiple
          className="h-40 w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
          value={form.network_profile_ids.map(String)}
          onChange={(event) => set('network_profile_ids', selectedNumbers(event.currentTarget.selectedOptions))}
        >
          {networks.map((network) => (
            <option key={network.id} value={network.id}>
              {network.vpc_name || 'default-vpc'} / {network.name} ({network.cidr})
            </option>
          ))}
        </select>
      </label>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="flex items-center gap-3">
          <input type="checkbox" checked={form.enabled} onChange={(event) => set('enabled', event.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
          <span className="text-sm text-slate-700">Enabled</span>
        </label>
        <label className="flex items-center gap-3">
          <input type="checkbox" checked={form.is_default} onChange={(event) => set('is_default', event.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
          <span className="text-sm text-slate-700">Default region</span>
        </label>
      </div>
      {!!error && <ErrorMessage error={error} />}
      <Button type="submit" loading={loading}>{isEdit ? 'Save Region' : 'Create Region'}</Button>
    </form>
  );
}

function regionForm(region: Region | null, networks: NetworkProfile[]): RegionFormData {
  if (!region) {
    return {
      ...emptyRegionForm,
      network_profile_ids: networks.map((network) => network.id),
    };
  }
  return {
    name: region.name,
    description: region.description,
    enabled: region.enabled,
    is_default: region.is_default,
    network_profile_ids: region.networks.map((network) => network.id),
  };
}

function nodeRegionID(regions: Region[], nodeID: number) {
  return regions.find((region) => region.nodes.some((node) => node.id === nodeID))?.id ?? 0;
}

function unassignedNodes(nodes: ProxmoxNode[], regions: Region[]) {
  const assigned = new Set(regions.flatMap((region) => region.nodes.map((node) => node.id)));
  return nodes.filter((node) => !assigned.has(node.id));
}

export function RegionsPage() {
  const queryClient = useQueryClient();
  const { data: regions, isLoading: regionsLoading, error: regionsError } = useQuery({ queryKey: ['regions'], queryFn: getRegions });
  const { data: nodes, isLoading: nodesLoading } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const [createOpen, setCreateOpen] = useState(false);
  const [editRegion, setEditRegion] = useState<Region | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Region | null>(null);
  const [moveTargets, setMoveTargets] = useState<Record<string, number>>({});

  const regionList = regions ?? [];
  const nodeList = nodes ?? [];
  const networkList = networks ?? [];
  const unassigned = unassignedNodes(nodeList, regionList);

  const invalidateRegions = () => queryClient.invalidateQueries({ queryKey: ['regions'] });

  const createMut = useMutation({
    mutationFn: async (form: RegionFormData) => {
      const region = await createRegion({
        name: form.name,
        description: form.description,
        enabled: form.enabled,
        is_default: form.is_default,
      });
      await setRegionNetworks(region.id, form.network_profile_ids);
      return region;
    },
    onSuccess: () => {
      invalidateRegions();
      setCreateOpen(false);
    },
  });

  const updateMut = useMutation({
    mutationFn: async ({ id, form }: { id: number; form: RegionFormData }) => {
      const region = await updateRegion(id, {
        name: form.name,
        description: form.description,
        enabled: form.enabled,
        is_default: form.is_default,
      });
      await setRegionNetworks(id, form.network_profile_ids);
      return region;
    },
    onSuccess: () => {
      invalidateRegions();
      setEditRegion(null);
    },
  });

  const deleteMut = useMutation({
    mutationFn: deleteRegion,
    onSuccess: () => {
      invalidateRegions();
      setDeleteTarget(null);
    },
  });

  const moveNodeMut = useMutation({
    mutationFn: async ({ nodeID, fromRegionID, toRegionID }: MoveNodePayload) => {
      if (toRegionID <= 0) {
        throw new Error('select destination region');
      }
      const fromRegion = regionList.find((region) => region.id === fromRegionID);
      const toRegion = regionList.find((region) => region.id === toRegionID);
      if (!toRegion) {
        throw new Error('destination region not found');
      }
      if (fromRegion && fromRegion.id !== toRegion.id) {
        await setRegionNodes(fromRegion.id, fromRegion.nodes.filter((node) => node.id !== nodeID).map((node) => node.id));
      }
      if (!toRegion.nodes.some((node) => node.id === nodeID)) {
        await setRegionNodes(toRegion.id, [...toRegion.nodes.map((node) => node.id), nodeID]);
      }
    },
    onSuccess: () => {
      invalidateRegions();
      setMoveTargets({});
    },
  });

  const moveTargetFor = (regionID: number, nodeID: number) => moveTargets[`${regionID}:${nodeID}`] ?? 0;
  const setMoveTargetFor = (regionID: number, nodeID: number, value: number) =>
    setMoveTargets((prev) => ({ ...prev, [`${regionID}:${nodeID}`]: value }));

  return (
    <Layout>
      <PageHeader
        title="Region"
        description="Manage locations, node placement, and the subnets available for VPS creation."
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" />
            New Region
          </Button>
        }
      />

      {(regionsLoading || nodesLoading) && <PageSpinner />}
      {regionsError && <ErrorMessage error={regionsError} />}

      <div className="grid gap-4 xl:grid-cols-2">
        {regionList.map((region) => (
          <Card key={region.id}>
            <div className="border-b border-slate-200 bg-slate-50 px-6 py-4">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="flex items-center gap-2">
                    <MapPin className="h-4 w-4 text-slate-500" />
                    <h3 className="text-base font-semibold text-slate-900">{region.name}</h3>
                  </div>
                  <p className="mt-1 text-sm text-slate-500">{region.description || 'Proxmox region'}</p>
                </div>
                <div className="flex flex-wrap justify-end gap-2">
                  {region.is_default && <Badge variant="success">default</Badge>}
                  {!region.enabled && <Badge variant="warning">disabled</Badge>}
                </div>
              </div>
            </div>
            <div className="space-y-5 p-6">
              <div>
                <p className="mb-2 text-sm font-semibold text-slate-800">Nodes</p>
                <div className="space-y-2">
                  {region.nodes.map((node) => (
                    <div key={node.id} className="flex flex-col gap-2 rounded-lg border border-slate-200 p-3 sm:flex-row sm:items-center sm:justify-between">
                      <div>
                        <p className="text-sm font-medium text-slate-900">{node.node_name}</p>
                        <p className="text-xs text-slate-500">{node.credential_name || `Credential ${node.credential_id}`} - {node.status}</p>
                      </div>
                      <div className="flex gap-2">
                        <select
                          className="min-w-40 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
                          value={moveTargetFor(region.id, node.id)}
                          onChange={(event) => setMoveTargetFor(region.id, node.id, Number(event.target.value))}
                        >
                          <option value={0}>Move to...</option>
                          {regionList.filter((target) => target.id !== region.id).map((target) => (
                            <option key={target.id} value={target.id}>{target.name}</option>
                          ))}
                        </select>
                        <Button
                          type="button"
                          variant="secondary"
                          size="sm"
                          loading={moveNodeMut.isPending}
                          onClick={() => moveNodeMut.mutate({ nodeID: node.id, fromRegionID: region.id, toRegionID: moveTargetFor(region.id, node.id) })}
                        >
                          <ArrowRightLeft className="h-4 w-4" />
                          Move
                        </Button>
                      </div>
                    </div>
                  ))}
                  {!region.nodes.length && <p className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-sm text-slate-500">No nodes assigned.</p>}
                </div>
              </div>

              <div>
                <p className="mb-2 text-sm font-semibold text-slate-800">Subnets</p>
                <div className="flex flex-wrap gap-2">
                  {region.networks.map((network) => (
                    <span key={network.id} className="rounded bg-slate-100 px-2 py-1 text-xs text-slate-700">
                      {network.vpc_name || 'default-vpc'} / {network.name}
                    </span>
                  ))}
                  {!region.networks.length && <span className="text-sm text-slate-500">No subnets assigned.</span>}
                </div>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" size="sm" onClick={() => setEditRegion(region)}>
                  <Pencil className="h-4 w-4" />
                  Edit
                </Button>
                <Button variant="danger" size="sm" onClick={() => setDeleteTarget(region)}>
                  <Trash2 className="h-4 w-4" />
                  Delete
                </Button>
              </div>
            </div>
          </Card>
        ))}
      </div>

      {unassigned.length > 0 && (
        <Card className="mt-6">
          <div className="border-b border-slate-200 bg-slate-50 px-6 py-4">
            <h3 className="text-base font-semibold text-slate-800">Unassigned Nodes</h3>
          </div>
          <div className="grid gap-3 p-6 md:grid-cols-2 xl:grid-cols-3">
            {unassigned.map((node) => (
              <div key={node.id} className="rounded-lg border border-slate-200 p-3">
                <p className="text-sm font-medium text-slate-900">{node.node_name}</p>
                <p className="text-xs text-slate-500">{node.status}</p>
                <div className="mt-3 flex gap-2">
                  <select
                    className="min-w-0 flex-1 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900"
                    value={nodeRegionID(regionList, node.id)}
                    onChange={(event) => setMoveTargetFor(0, node.id, Number(event.target.value))}
                  >
                    <option value={0}>Assign to...</option>
                    {regionList.map((region) => (
                      <option key={region.id} value={region.id}>{region.name}</option>
                    ))}
                  </select>
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    loading={moveNodeMut.isPending}
                    onClick={() => moveNodeMut.mutate({ nodeID: node.id, fromRegionID: 0, toRegionID: moveTargetFor(0, node.id) })}
                  >
                    Assign
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}

      {moveNodeMut.error && <div className="mt-4"><ErrorMessage error={moveNodeMut.error} /></div>}

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="New Region">
        <RegionForm
          key={`create-region-${networkList.length}`}
          initial={regionForm(null, networkList)}
          networks={networkList}
          onSubmit={(form) => createMut.mutate(form)}
          loading={createMut.isPending}
          error={createMut.error}
          isEdit={false}
        />
      </Modal>

      <Modal open={!!editRegion} onClose={() => setEditRegion(null)} title="Edit Region">
        {editRegion && (
          <RegionForm
            key={`edit-region-${editRegion.id}`}
            initial={regionForm(editRegion, networkList)}
            networks={networkList}
            onSubmit={(form) => updateMut.mutate({ id: editRegion.id, form })}
            loading={updateMut.isPending}
            error={updateMut.error}
            isEdit
          />
        )}
      </Modal>

      <Modal open={!!deleteTarget} onClose={() => setDeleteTarget(null)} title="Delete Region">
        {deleteTarget && (
          <div className="space-y-4">
            <p className="text-sm text-slate-600">
              Delete region <span className="font-semibold text-slate-900">{deleteTarget.name}</span>? This will fail if VPS records still reference it.
            </p>
            {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setDeleteTarget(null)} disabled={deleteMut.isPending}>
                Cancel
              </Button>
              <Button variant="danger" onClick={() => deleteMut.mutate(deleteTarget.id)} loading={deleteMut.isPending}>
                <Trash2 className="h-4 w-4" />
                Delete
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </Layout>
  );
}
