import client from './client';
import type { CustomerSSHPublicKey } from '../types';

export const getCustomerSSHKeys = () =>
  client.get<CustomerSSHPublicKey[]>('/customer-ssh-keys').then((response) => response.data);

export const createCustomerSSHKey = (body: { name: string; ssh_user: string; public_key: string }) =>
  client.post<CustomerSSHPublicKey>('/customer-ssh-keys', body).then((response) => response.data);

export const deleteCustomerSSHKey = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/customer-ssh-keys/${id}`).then((response) => response.data);
