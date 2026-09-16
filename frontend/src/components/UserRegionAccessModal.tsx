import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getRegions, getUserRegionAccess, setUserRegionAccess, type RegionAccessPolicy } from '../api/regions';
import { Modal } from './ui/Modal';
import { Select } from './ui/Input';
import { Button } from './ui/Button';
import { ErrorMessage } from './ui/Alert';
import { PageSpinner } from './ui/Spinner';
import type { User } from '../types';

export function UserRegionAccessModal({ user, onClose }: { user: User; onClose: () => void }) {
  const qc = useQueryClient();
  const policy = useQuery({ queryKey: ['user-region-access', user.id], queryFn: () => getUserRegionAccess(user.id) });
  const regions = useQuery({ queryKey: ['regions'], queryFn: getRegions });
  const [draft, setDraft] = useState<RegionAccessPolicy | null>(null);
  const value = draft ?? policy.data;
  const save = useMutation({
    mutationFn: (body: RegionAccessPolicy) => setUserRegionAccess(user.id, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['user-region-access', user.id] });
      qc.invalidateQueries({ queryKey: ['regions'] });
      onClose();
    },
  });
  return <Modal open onClose={() => { if (!save.isPending) onClose(); }} title={`Deployment regions: ${user.username}`}>
    <ErrorMessage error={policy.error || regions.error || save.error} />
    {policy.isLoading || regions.isLoading ? <PageSpinner /> : value && regions.data && <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); save.mutate(value); }}>
      <p className="text-sm text-slate-600">All regions remain visible. These permissions control new deployments and redeployment; existing resources remain accessible.</p>
      <Select label="Region access" value={value.mode} onChange={(event) => setDraft({ mode: event.target.value as RegionAccessPolicy['mode'], region_ids: [] })}>
        <option value="none">No regions</option>
        <option value="selected">Selected regions</option>
        <option value="all">All regions (including future regions)</option>
      </Select>
      {value.mode === 'selected' && <div className="max-h-64 space-y-2 overflow-y-auto rounded-md border border-slate-200 p-3">
        {regions.data.map((region) => <label key={region.id} className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={value.region_ids.includes(region.id)} onChange={(event) => setDraft({ ...value, region_ids: event.target.checked ? [...value.region_ids, region.id] : value.region_ids.filter((id) => id !== region.id) })} />
          {region.name}{!region.enabled && <span className="text-slate-500">(disabled)</span>}
        </label>)}
        {regions.data.length === 0 && <p className="text-sm text-slate-500">No regions have been created.</p>}
      </div>}
      <p className="text-sm font-medium">Allowed regions: {value.mode === 'all' ? `${regions.data.length} (and future regions)` : value.mode === 'none' ? 0 : value.region_ids.length}</p>
      <p className="text-xs text-slate-500">Disabled regions cannot accept deployments even when assigned.</p>
      <div className="flex gap-3">
        <Button type="submit" loading={save.isPending}>Save permissions</Button>
        <Button type="button" variant="secondary" disabled={save.isPending} onClick={onClose}>Cancel</Button>
      </div>
    </form>}
  </Modal>;
}
