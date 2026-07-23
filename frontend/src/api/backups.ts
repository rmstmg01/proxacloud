import client from './client';
import type {
  BackupJob,
  BackupSourceType,
  BackupSummary,
  BackupTarget,
  BackupTargetType,
} from '../types';

export interface VPSBackupPlan {
  code: 'vps_backup_daily' | 'vps_backup_weekly' | 'vps_backup_monthly';
  name: string;
  frequency: 'daily' | 'weekly' | 'monthly';
  base_monthly_cents: number;
  disk_gb_monthly_cents: number;
  retention_count: number;
  enabled: boolean;
}

export interface VPSBackupPolicy {
  id: number;
  vps_id: number;
  customer_account_id?: number;
  target_name: string;
  plan_code: VPSBackupPlan['code'];
  frequency: VPSBackupPlan['frequency'];
  weekday?: number;
  month_day?: number;
  local_time: string;
  timezone: string;
  mode: string;
  retention_count: number;
  enabled: boolean;
  monthly_price_cents: number;
  next_run_at?: string;
  last_run_at?: string;
  last_success_at?: string;
  last_error?: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export type VPSBackupJob = BackupJob & {
  policy_id?: number;
  customer_account_id?: number;
  trigger_type: 'manual' | 'scheduled';
  scheduled_for?: string;
  started_at?: string;
  finished_at?: string;
};

export interface VPSBackupOverview {
  policy?: VPSBackupPolicy;
  plans: VPSBackupPlan[];
  jobs: VPSBackupJob[];
}

export interface VPSBackupPolicyInput {
  plan_code: VPSBackupPlan['code'];
  weekday?: number;
  month_day?: number;
  local_time: string;
  timezone: string;
}

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

export const getBackupJobs = () => client.get<BackupJob[]>('/backups/jobs').then((r) => r.data);

export const startBackupJob = (body: {
  target_id: number;
  source_type: BackupSourceType;
  source_id: number;
  mode: string;
  staging_storage_id?: string;
}) => client.post<BackupJob>('/backups/jobs', body).then((r) => r.data);

export const getVPSBackupOverview = (vpsID: number) =>
  client.get<VPSBackupOverview>(`/vps/${vpsID}/backup`).then((r) => r.data);

export const saveVPSBackupPolicy = (vpsID: number, body: VPSBackupPolicyInput) =>
  client.put<VPSBackupPolicy>(`/vps/${vpsID}/backup/policy`, body).then((r) => r.data);

export const disableVPSBackupPolicy = (vpsID: number) =>
  client.delete<{ vps_id: number; enabled: boolean }>(`/vps/${vpsID}/backup/policy`).then((r) => r.data);

export const runVPSBackupNow = (vpsID: number) =>
  client.post<VPSBackupJob>(`/vps/${vpsID}/backup/run`).then((r) => r.data);
