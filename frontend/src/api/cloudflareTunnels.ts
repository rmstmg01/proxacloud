import client from './client';

export type CloudflareTunnelRoutingMode = 'traefik' | 'direct';
export type CloudflareTunnelProvisioningMode = 'manual_token' | 'cloudflare_api';

export interface CloudflareTunnelRouteInput {
  hostname: string;
  path: string;
  service_namespace: string;
  service_name: string;
  service_port: number;
  service_scheme: 'http' | 'https';
}

export interface CloudflareTunnelRoute extends CloudflareTunnelRouteInput {
  id: number;
  tunnel_id: number;
  created_at: string;
}

export interface CloudflareTunnel {
  id: number;
  cluster_id: number;
  cluster_name?: string;
  is_global: boolean;
  name: string;
  namespace: string;
  deployment_name: string;
  routing_mode: CloudflareTunnelRoutingMode;
  provisioning_mode: CloudflareTunnelProvisioningMode;
  cloudflare_account_id?: string;
  cloudflare_zone_id?: string;
  cloudflare_tunnel_id?: string;
  cloudflare_tunnel_name?: string;
  has_tunnel_token: boolean;
  has_api_token: boolean;
  replicas: number;
  image: string;
  ingress_namespace: string;
  ingress_service_name: string;
  ingress_service_port: number;
  ingress_service_scheme: 'http' | 'https';
  configure_dns: boolean;
  status: string;
  last_manifest?: string;
  last_apply_output?: string;
  last_applied_at?: string;
  routes: CloudflareTunnelRoute[];
  created_at: string;
  updated_at: string;
}

export interface CloudflareTunnelEvent {
  id: number;
  tunnel_id: number;
  step: string;
  level: string;
  message: string;
  output: string;
  created_at: string;
}

export interface CreateCloudflareTunnelRequest {
  cluster_id: number;
  is_global: boolean;
  name: string;
  namespace: string;
  deployment_name: string;
  routing_mode: CloudflareTunnelRoutingMode;
  provisioning_mode: CloudflareTunnelProvisioningMode;
  tunnel_token: string;
  cloudflare_api_token: string;
  cloudflare_account_id: string;
  cloudflare_zone_id: string;
  cloudflare_tunnel_id: string;
  cloudflare_tunnel_name: string;
  replicas: number;
  image: string;
  ingress_namespace: string;
  ingress_service_name: string;
  ingress_service_port: number;
  ingress_service_scheme: 'http' | 'https';
  configure_dns: boolean;
  apply: boolean;
  routes: CloudflareTunnelRouteInput[];
}

export interface DeployCloudflareTunnelResponse {
  tunnel_id: number;
  status: string;
  output?: string;
  warnings: string[];
}

export const getCloudflareTunnels = () =>
  client.get<CloudflareTunnel[]>('/cloudflare-tunnels').then((r) => r.data);

export const createCloudflareTunnel = (body: CreateCloudflareTunnelRequest) =>
  client.post<CloudflareTunnel>('/cloudflare-tunnels', body).then((r) => r.data);

export const getCloudflareTunnel = (id: number) =>
  client.get<CloudflareTunnel>(`/cloudflare-tunnels/${id}`).then((r) => r.data);

export const deployCloudflareTunnel = (id: number) =>
  client.post<DeployCloudflareTunnelResponse>(`/cloudflare-tunnels/${id}/deploy`).then((r) => r.data);

export const getCloudflareTunnelManifest = (id: number) =>
  client.get<{ manifest: string }>(`/cloudflare-tunnels/${id}/manifest`).then((r) => r.data);

export const deleteCloudflareTunnel = (id: number, confirmationName: string) =>
  client
    .delete<{ id: number; deleted: boolean }>(`/cloudflare-tunnels/${id}`, {
      data: { confirmation_name: confirmationName },
    })
    .then((r) => r.data);

export const getCloudflareTunnelEvents = (id: number) =>
  client.get<CloudflareTunnelEvent[]>(`/cloudflare-tunnels/${id}/events`).then((r) => r.data);
