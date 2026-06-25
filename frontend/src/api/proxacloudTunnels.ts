import client from './client';

export interface ProxacloudTunnelRouteInput {
  hostname: string;
  path: string;
  service_namespace: string;
  service_name: string;
  service_port: number;
  service_scheme: 'http' | 'https';
}

export interface ProxacloudTunnelRoute extends ProxacloudTunnelRouteInput {
  id: number;
  tunnel_id: number;
  created_at: string;
}

export interface ProxacloudTunnel {
  id: number;
  cluster_id: number;
  cluster_name?: string;
  is_global: boolean;
  name: string;
  namespace: string;
  deployment_name: string;
  proxacloud_tunnel_id: string;
  has_tunnel_token: boolean;
  gateway_url: string;
  target_url: string;
  replicas: number;
  image: string;
  image_pull_secret: string;
  status: string;
  last_manifest?: string;
  last_apply_output?: string;
  last_applied_at?: string;
  routes: ProxacloudTunnelRoute[];
  created_at: string;
  updated_at: string;
}

export interface ProxacloudTunnelEvent {
  id: number;
  tunnel_id: number;
  step: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
}

export interface CreateProxacloudTunnelRequest {
  cluster_id: number;
  is_global: boolean;
  name: string;
  namespace: string;
  deployment_name: string;
  proxacloud_tunnel_id: string;
  tunnel_token: string;
  gateway_url: string;
  target_url: string;
  replicas: number;
  image: string;
  image_pull_secret: string;
  apply: boolean;
  routes: ProxacloudTunnelRouteInput[];
}

export interface DeployProxacloudTunnelResponse {
  tunnel_id: number;
  status: string;
  output?: string;
  warnings: string[];
}

export const getProxacloudTunnels = () =>
  client.get<ProxacloudTunnel[]>('/proxacloud-tunnels').then((r) => r.data);

export const createProxacloudTunnel = (body: CreateProxacloudTunnelRequest) =>
  client.post<ProxacloudTunnel>('/proxacloud-tunnels', body).then((r) => r.data);

export const getProxacloudTunnel = (id: number) =>
  client.get<ProxacloudTunnel>(`/proxacloud-tunnels/${id}`).then((r) => r.data);

export const deployProxacloudTunnel = (id: number) =>
  client.post<DeployProxacloudTunnelResponse>(`/proxacloud-tunnels/${id}/deploy`).then((r) => r.data);

export const attachProxacloudTunnelToCluster = (id: number, body: { cluster_id: number; apply: boolean }) =>
  client.post<DeployProxacloudTunnelResponse>(`/proxacloud-tunnels/${id}/attach-cluster`, body).then((r) => r.data);

export const updateProxacloudTunnelToken = (id: number, body: { tunnel_token: string; apply: boolean }) =>
  client.post<DeployProxacloudTunnelResponse>(`/proxacloud-tunnels/${id}/token`, body).then((r) => r.data);

export const getProxacloudTunnelManifest = (id: number) =>
  client.get<{ manifest: string }>(`/proxacloud-tunnels/${id}/manifest`).then((r) => r.data);

export const deleteProxacloudTunnel = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/proxacloud-tunnels/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);

export const getProxacloudTunnelEvents = (id: number) =>
  client.get<ProxacloudTunnelEvent[]>(`/proxacloud-tunnels/${id}/events`).then((r) => r.data);
