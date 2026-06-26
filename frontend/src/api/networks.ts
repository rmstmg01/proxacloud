import client from './client';
import type { NetworkIPScanResponse, NetworkIPUsageSummary, NetworkProfile, NodeNetworkTag, NetworkReservedIP, VPC } from '../types';

export const getVPCs = () =>
  client.get<VPC[]>('/vpcs').then((r) => r.data);

export const createVPC = (body: { name: string; cidr: string; description: string; is_default: boolean }) =>
  client.post<VPC>('/vpcs', body).then((r) => r.data);

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

export const getReservedIPs = (networkProfileID: number) =>
  client.get<NetworkReservedIP[]>(`/networks/${networkProfileID}/reserved-ips`).then((r) => r.data);

export const createReservedIP = (
  networkProfileID: number,
  body: { hostname: string; ip_address: string; description: string },
) => client.post<NetworkReservedIP>(`/networks/${networkProfileID}/reserved-ips`, body).then((r) => r.data);

export const deleteReservedIP = (networkProfileID: number, reservedID: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/networks/${networkProfileID}/reserved-ips/${reservedID}`).then((r) => r.data);

export const getNetworkIPUsage = (networkProfileID: number) =>
  client.get<NetworkIPUsageSummary>(`/networks/${networkProfileID}/ip-usage`).then((r) => r.data);

export const scanNetworkIPUsage = (networkProfileID: number) =>
  client.post<NetworkIPScanResponse>(`/networks/${networkProfileID}/ip-usage/scan`).then((r) => r.data);
