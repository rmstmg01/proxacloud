import client from './client';
import type {
  VPSActionResponse,
  VPSCloudInitImage,
  VPSConsoleResponse,
  VPSCreateResponse,
  VPSEvent,
  VPSInstance,
  VPSLoginDetails,
  VPSResetRootPasswordResponse,
  VPSTunnelAttachment,
  VPSTunnelHTTPRoute,
} from '../types';
export type ManagedVPSInstance = VPSInstance & {
  ownership_type: 'customer' | 'admin' | 'legacy';
  created_by_username?: string;
  admin_suspended_at?: string;
  admin_suspended_by?: string;
  admin_suspension_reason?: string;
  admin_suspension_error?: string;
};

export interface VPSAssignmentOption {
  user_id: number;
  customer_account_id: number;
  username: string;
  full_name: string;
}


export const getVPSInstances = () =>
  client.get<ManagedVPSInstance[]>('/vps').then((r) => r.data);

export const getVPSInstance = (id: number) =>
  client.get<ManagedVPSInstance>(`/vps/${id}`).then((r) => r.data);

export const getVPSEvents = (id: number) =>
  client.get<VPSEvent[]>(`/vps/${id}/events`).then((r) => r.data);

export const getVPSTunnel = (id: number) =>
  client.get<VPSTunnelAttachment>(`/vps/${id}/tunnel`).then((r) => r.data);

export const getVPSLoginDetails = (id: number) =>
  client.get<VPSLoginDetails>(`/vps/${id}/login-details`).then((r) => r.data);

export const retryVPSTunnel = (id: number) =>
  client.post<VPSTunnelAttachment>(`/vps/${id}/tunnel/retry`).then((r) => r.data);

export interface VPSTunnelHTTPRoutePayload {
  name: string;
  public_hostname: string;
  target_url: string;
  tls_mode: 'edge' | 'passthrough' | 'reencrypt';
  enabled: boolean;
}

export const getVPSTunnelHTTPRoutes = (id: number) =>
  client.get<VPSTunnelHTTPRoute[]>(`/vps/${id}/tunnel/http-routes`).then((r) => r.data);

export const createVPSTunnelHTTPRoute = (id: number, body: VPSTunnelHTTPRoutePayload) =>
  client.post<VPSTunnelHTTPRoute>(`/vps/${id}/tunnel/http-routes`, body).then((r) => r.data);

export const updateVPSTunnelHTTPRoute = (id: number, routeID: number, body: VPSTunnelHTTPRoutePayload) =>
  client.patch<VPSTunnelHTTPRoute>(`/vps/${id}/tunnel/http-routes/${routeID}`, body).then((r) => r.data);

export const retryVPSTunnelHTTPRoute = (id: number, routeID: number) =>
  client.post<VPSTunnelHTTPRoute>(`/vps/${id}/tunnel/http-routes/${routeID}/retry`).then((r) => r.data);

export const deleteVPSTunnelHTTPRoute = (id: number, routeID: number) =>
  client.delete(`/vps/${id}/tunnel/http-routes/${routeID}`);

export const resetVPSRootPassword = (id: number, password = '') =>
  client.post<VPSResetRootPasswordResponse>(`/vps/${id}/root-password/reset`, { password }).then((r) => r.data);

export const updateVPSHostname = (id: number, hostname: string) =>
  client.patch<VPSLoginDetails>(`/vps/${id}/hostname`, { hostname }).then((r) => r.data);

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

export const discoverVPSCloudInitImages = (credentialID: number, nodeName: string, storageID = 'cloud-images') =>
  client
    .post<VPSCloudInitImage[]>('/vps/cloud-init-images/discover', {
      proxmox_credential_id: credentialID,
      node_name: nodeName,
      storage_id: storageID,
    })
    .then((r) => r.data);

export const discoverVPSCloudInitImagesForRegion = (regionID: number, storageID = 'cloud-images') =>
  client
    .post<VPSCloudInitImage[]>('/vps/cloud-init-images/discover', {
      region_id: regionID,
      storage_id: storageID,
    })
    .then((r) => r.data);

export const discoverVPSCloudInitImagesAutomatically = (storageID = 'cloud-images') =>
  client
    .post<VPSCloudInitImage[]>('/vps/cloud-init-images/discover', { storage_id: storageID })
    .then((r) => r.data);

export const createVPSInstance = (body: {
  name: string;
  description: string;
  proxmox_credential_id: number;
  proxmox_node_name: string;
  region_id: number;
  network_profile_id: number;
  cloud_init_image_id: number;
  cloud_init_image_path: string;
  customer_ssh_key_id: number;
  ip_mode: 'dhcp' | 'manual' | 'auto';
  ip_address: string;
  cpu: number;
  memory_mb: number;
  disk_gb: number;
  ssh_user: string;
  root_password?: string;
  apply: boolean;
}) => client.post<VPSCreateResponse>('/vps', body).then((r) => r.data);

export const updateVPSInstance = (id: number, body: { name: string }) =>
  client.patch<ManagedVPSInstance>(`/vps/${id}`, body).then((r) => r.data);

export const deployVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/deploy`).then((r) => r.data);

export const redeployVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/redeploy`).then((r) => r.data);

export const refreshVPSStatus = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/refresh-status`).then((r) => r.data);

export const openVPSConsole = (id: number) =>
  client.post<VPSConsoleResponse>(`/vps/${id}/console`).then((r) => r.data);

export const startVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/start`).then((r) => r.data);

export const restartVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/restart`).then((r) => r.data);

export const shutdownVPSInstance = (id: number) =>
  client.post<VPSActionResponse>(`/vps/${id}/shutdown`).then((r) => r.data);

export const getVPSAssignmentOptions = () =>
  client.get<VPSAssignmentOption[]>('/vps/assignment-options').then((r) => r.data);

export const assignVPSInstance = (id: number, targetUserID: number, reason: string) =>
  client.put<ManagedVPSInstance>(`/vps/${id}/assignment`, { target_user_id: targetUserID, reason }).then((r) => r.data);

export const suspendVPSAdministratively = (id: number, reason: string) =>
  client.post<ManagedVPSInstance>(`/vps/${id}/administrative-suspension`, { reason }).then((r) => r.data);

export const unsuspendVPSAdministratively = (id: number) =>
  client.delete<ManagedVPSInstance>(`/vps/${id}/administrative-suspension`).then((r) => r.data);

export const deleteVPSInstance = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/vps/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);
