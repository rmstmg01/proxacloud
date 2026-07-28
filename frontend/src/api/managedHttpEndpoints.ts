import client from './client';

export interface ManagedHTTPEndpoint {
  id: number;
  provider: string;
  externalId: string;
  hostname: string;
  tunnelId: string;
  targetUrl: string;
  httpRouteId: string;
  dnsRecordId: string;
  dnsRecordType: string;
  dnsRecordContent: string;
  dnsProxied: boolean;
  status: 'pending' | 'active' | 'failed' | 'disabled';
  lastError: string;
  createdAt: string;
  updatedAt: string;
}

export async function getManagedHTTPEndpoints(): Promise<ManagedHTTPEndpoint[]> {
  const response = await client.get<ManagedHTTPEndpoint[]>('/managed-http-endpoints');
  return response.data;
}
