import client from './client';
import type { Region } from '../types';

export const getRegions = () =>
  client.get<Region[]>('/regions').then((r) => r.data);

export const createRegion = (body: { name: string; slug?: string; description: string; enabled: boolean; is_default: boolean }) =>
  client.post<Region>('/regions', body).then((r) => r.data);

export const updateRegion = (
  id: number,
  body: { name: string; slug?: string; description: string; enabled: boolean; is_default: boolean },
) => client.put<Region>(`/regions/${id}`, body).then((r) => r.data);

export const deleteRegion = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/regions/${id}`).then((r) => r.data);

export const setRegionNodes = (id: number, node_ids: number[]) =>
  client.put<{ id: number; updated: boolean }>(`/regions/${id}/nodes`, { node_ids }).then((r) => r.data);

export const moveRegionNode = (regionID: number, nodeID: number) =>
  client.put<{ region_id: number; node_id: number; updated: boolean }>(`/regions/${regionID}/nodes/${nodeID}`).then((r) => r.data);

export const unassignRegionNode = (regionID: number, nodeID: number) =>
  client.delete<{ region_id: number; node_id: number; unassigned: boolean }>(`/regions/${regionID}/nodes/${nodeID}`).then((r) => r.data);

export const setRegionNetworks = (id: number, network_profile_ids: number[]) =>
  client.put<{ id: number; updated: boolean }>(`/regions/${id}/networks`, { network_profile_ids }).then((r) => r.data);
