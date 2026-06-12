import client from './client';
import type {
  ContainerActionResponse,
  ContainerCreateResponse,
  ContainerEvent,
  ContainerInstance,
  ContainerTemplate,
} from '../types';

export const getContainerInstances = () =>
  client.get<ContainerInstance[]>('/containers').then((r) => r.data);

export const getContainerInstance = (id: number) =>
  client.get<ContainerInstance>(`/containers/${id}`).then((r) => r.data);

export const getContainerEvents = (id: number) =>
  client.get<ContainerEvent[]>(`/containers/${id}/events`).then((r) => r.data);

export const getContainerTemplates = () =>
  client.get<ContainerTemplate[]>('/containers/templates').then((r) => r.data);

export const createContainerTemplate = (body: {
  name: string;
  template_path: string;
  os_type: string;
  description: string;
  is_default: boolean;
}) => client.post<ContainerTemplate>('/containers/templates', body).then((r) => r.data);

export const createContainerInstance = (body: {
  name: string;
  description: string;
  proxmox_credential_id: number;
  proxmox_node_name: string;
  network_profile_id: number;
  template_id: number;
  template_path: string;
  ssh_key_pair_id: number;
  ip_mode: 'dhcp' | 'manual' | 'auto';
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  root_password: string;
  unprivileged: boolean;
  nesting: boolean;
  apply: boolean;
}) => client.post<ContainerCreateResponse>('/containers', body).then((r) => r.data);

export const deployContainerInstance = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/deploy`).then((r) => r.data);

export const redeployContainerInstance = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/redeploy`).then((r) => r.data);

export const refreshContainerStatus = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/refresh-status`).then((r) => r.data);

export const startContainerInstance = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/start`).then((r) => r.data);

export const restartContainerInstance = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/restart`).then((r) => r.data);

export const shutdownContainerInstance = (id: number) =>
  client.post<ContainerActionResponse>(`/containers/${id}/shutdown`).then((r) => r.data);

export const deleteContainerInstance = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/containers/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);
