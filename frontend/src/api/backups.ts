import client from './client';
import type { BackupSummary, BackupTarget, BackupTargetType } from '../types';

export interface BackupTargetInput {
  name: string;
  type: BackupTargetType;
  enabled: boolean;
  config: Record<string, unknown>;
  access_key?: string;
  secret_key?: string;
  password?: string;
  ssh_private_key?: string;
  restic_password?: string;
}

export const getBackupSummary = () => client.get<BackupSummary>('/backups/summary').then((r) => r.data);

export const getBackupTargets = () => client.get<BackupTarget[]>('/backups/targets').then((r) => r.data);

export const createBackupTarget = (body: BackupTargetInput) =>
  client.post<BackupTarget>('/backups/targets', body).then((r) => r.data);

export const deleteBackupTarget = (id: number) =>
  client.delete<{ id: number; deleted: boolean }>(`/backups/targets/${id}`).then((r) => r.data);
