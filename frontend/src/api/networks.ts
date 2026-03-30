import client from './client';
import type { NetworkProfile } from '../types';

export const getNetworks = () =>
  client.get<NetworkProfile[]>('/networks').then((r) => r.data);

export const createNetwork = (body: Omit<NetworkProfile, 'id' | 'created_at' | 'updated_at'>) =>
  client.post<NetworkProfile>('/networks', body).then((r) => r.data);

export const updateNetwork = (id: number, body: Partial<Omit<NetworkProfile, 'id' | 'created_at' | 'updated_at' | 'is_default'>>) =>
  client.put<NetworkProfile>(`/networks/${id}`, body).then((r) => r.data);

export const setDefaultNetwork = (id: number) =>
  client.post<NetworkProfile>(`/networks/${id}/set-default`).then((r) => r.data);
