import client from './client';
import type { SSHKeyPair } from '../types';

export const getSSHKeys = () =>
  client.get<SSHKeyPair[]>('/ssh-keys').then((r) => r.data);

export const createSSHKey = (body: {
  name: string;
  ssh_user: string;
  public_key: string;
  private_key: string;
}) => client.post<SSHKeyPair>('/ssh-keys', body).then((r) => r.data);

export const deleteSSHKey = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/ssh-keys/${id}`).then((r) => r.data);
