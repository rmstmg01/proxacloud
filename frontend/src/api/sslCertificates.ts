import client from './client';
import type { SSLCertificate } from '../types';

export const getSSLCertificates = () =>
  client.get<SSLCertificate[]>('/ssl-certificates').then((r) => r.data);

export const createSSLCertificate = (body: {
  name: string;
  fullchain_pem: string;
  private_key_pem: string;
}) => client.post<SSLCertificate>('/ssl-certificates', body).then((r) => r.data);

export const deleteSSLCertificate = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/ssl-certificates/${id}`).then((r) => r.data);
