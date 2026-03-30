import client from './client';
import type { ProxmoxCredential, ProxmoxNode } from '../types';

export const getCredentials = () =>
  client.get<ProxmoxCredential[]>('/proxmox/credentials').then((r) => r.data);

export const createCredential = (body: {
  name: string;
  endpoint: string;
  insecure_tls: boolean;
  api_token_id?: string;
  api_token_secret?: string;
  username?: string;
  password?: string;
}) => client.post<ProxmoxCredential>('/proxmox/credentials', body).then((r) => r.data);

export const deleteCredential = (credentialId: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/proxmox/credentials/${credentialId}`).then((r) => r.data);

export const discoverNodes = (credentialId: number) =>
  client
    .post<ProxmoxNode[]>(`/proxmox/credentials/${credentialId}/discover-nodes`)
    .then((r) => r.data);

export const getNodes = () =>
  client.get<ProxmoxNode[]>('/proxmox/nodes').then((r) => r.data);

export const toggleNode = (nodeId: number, enabled: boolean) =>
  client.patch<{ id: number; enabled: boolean }>(`/proxmox/nodes/${nodeId}`, { enabled }).then((r) => r.data);
