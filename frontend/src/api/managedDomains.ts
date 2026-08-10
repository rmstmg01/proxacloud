import client from './client';

export interface ManagedDomain {
  id: number;
  name: string;
  base_domain: string;
  cloudflare_zone_id: string;
  dns_target: string;
  dns_record_type: 'A' | 'AAAA' | 'CNAME';
  dns_proxied: boolean;
  reserved_labels: string[];
  available_for_vps: boolean;
  active: boolean;
  is_default: boolean;
  has_cloudflare_token: boolean;
}

export const getAvailableManagedDomains = () =>
  client.get<Array<Pick<ManagedDomain, 'id' | 'name' | 'base_domain' | 'reserved_labels' | 'is_default'>>>('/managed-domains/available').then((response) => response.data);

export interface ManagedDomainInput {
  name: string; base_domain: string; cloudflare_zone_id: string; cloudflare_api_token: string;
  dns_target: string; dns_record_type: 'A' | 'AAAA' | 'CNAME'; dns_proxied: boolean;
  reserved_labels: string[]; available_for_vps: boolean; active: boolean; is_default: boolean;
}

export const getManagedDomains = () => client.get<ManagedDomain[]>('/managed-domains').then((response) => response.data);
export const createManagedDomain = (body: ManagedDomainInput) => client.post<ManagedDomain>('/managed-domains', body).then((response) => response.data);
export const updateManagedDomain = (id: number, body: ManagedDomainInput) => client.put<ManagedDomain>(`/managed-domains/${id}`, body).then((response) => response.data);
export const deleteManagedDomain = (id: number) => client.delete(`/managed-domains/${id}`);

export const checkManagedHostname = (domainID: number, label: string) =>
  client.get<{hostname: string; available: boolean}>(`/managed-domains/${domainID}/hostname-availability`, { params: { label } }).then((response) => response.data);
