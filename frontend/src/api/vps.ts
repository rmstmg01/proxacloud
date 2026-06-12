import client from './client';
import type {
  VPSActionResponse,
  VPSCloudInitImage,
  VPSCreateResponse,
  VPSEvent,
  VPSInstance,
} from '../types';

export const getVPSInstances = () =>
  client.get<VPSInstance[]>('/vps').then((r) => r.data);

export const getVPSInstance = (id: number) =>
  client.get<VPSInstance>(`/vps/${id}`).then((r) => r.data);

export const getVPSEvents = (id: number) =>
  client.get<VPSEvent[]>(`/vps/${id}/events`).then((r) => r.data);

export const getVPSCloudInitImages = () =>
  client.get<VPSCloudInitImage[]>('/vps/cloud-init-images').then((r) => r.data);

export const createVPSCloudInitImage = (body: {
  name: string;
  image_path: string;
  os_type: string;
  description: string;
  is_default: boolean;
  ssh_user?: string;
  default_cpu?: number;
  default_memory_mb?: number;
  default_disk_gb?: number;
  source_path?: string;
  detected_node_name?: string;
}) => client.post<VPSCloudInitImage>('/vps/cloud-init-images', body).then((r) => r.data);

export const discoverVPSCloudInitImages = (nodeName: string) =>
  client.post<VPSCloudInitImage[]>('/vps/cloud-init-images/discover', { node_name: nodeName }).then((r) => r.data);

export const createVPSInstance = (body: {
  name: string;
  description: string;
  proxmox_credential_id: number;
  proxmox_node_name: string;
  network_profile_id: number;
  cloud_init_image_id: number;
  cloud_init_image_path: string;
  ssh_key_pair_id: number;
  ip_mode: 'dhcp' | 'manual' | 'auto';
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  ssh_user: string;
  root_password: string;
  apply: boolean;
}) => client.post<VPSCreateResponse>('/vps', body).then((r) => r.data);

export const deployVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/deploy`).then((r) => r.data);

export const redeployVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/redeploy`).then((r) => r.data);

export const refreshVPSStatus = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/refresh-status`).then((r) => r.data);

export const startVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/start`).then((r) => r.data);

export const restartVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/restart`).then((r) => r.data);

export const shutdownVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/shutdown`).then((r) => r.data);

export const deleteVPSInstance = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/vps/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);
