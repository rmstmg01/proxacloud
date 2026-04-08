import client from './client';
import type { NetworkProfile, NodeNetworkTag } from '../types';

export const getNetworks = () =>
  client.get<NetworkProfile[]>('/networks').then((r) => r.data);

export const createNetwork = (body: Omit<NetworkProfile, 'id' | 'created_at' | 'updated_at'>) =>
  client.post<NetworkProfile>('/networks', body).then((r) => r.data);

export const updateNetwork = (id: number, body: Partial<Omit<NetworkProfile, 'id' | 'created_at' | 'updated_at' | 'is_default'>>) =>
  client.put<NetworkProfile>(`/networks/${id}`, body).then((r) => r.data);

export const setDefaultNetwork = (id: number) =>
  client.post<NetworkProfile>(`/networks/${id}/set-default`).then((r) => r.data);

export const getNodeNetworkTags = () =>
  client.get<NodeNetworkTag[]>('/networks/node-tags').then((r) => r.data);

export const upsertNodeNetworkTag = (nodeName: string, network_profile_id: number) =>
  client
    .put<NodeNetworkTag>(`/networks/node-tags/${encodeURIComponent(nodeName)}`, { network_profile_id })
    .then((r) => r.data);

export const deleteNodeNetworkTag = (nodeName: string) =>
  client
    .delete<{ proxmox_node_name: string; deleted: boolean }>(`/networks/node-tags/${encodeURIComponent(nodeName)}`)
    .then((r) => r.data);
