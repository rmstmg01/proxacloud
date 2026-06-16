import { useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Archive, Cloud, Plus, Server, Trash2 } from 'lucide-react';
import { createBackupTarget, deleteBackupTarget, getBackupSummary } from '../api/backups';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { BackupTargetType } from '../types';

const targetLabels: Record<BackupTargetType, string> = {
  proxmox_storage: 'Proxmox Storage',
  pbs: 'Proxmox Backup Server',
  s3: 'AWS / S3 Bucket',
  hetzner_storagebox: 'Hetzner Storage Box',
};

const initialForm = {
  name: '',
  type: 's3' as BackupTargetType,
  bucket: '',
  region: '',
  endpoint_url: '',
  prefix: '',
  path_style: false,
  host: '',
  port: '23',
  username: '',
  base_path: '',
  auth_method: 'password',
  backup_mode: 'restic',
  storage_id: '',
  node_name: '',
  datastore: '',
  access_key: '',
  secret_key: '',
  password: '',
  ssh_private_key: '',
  restic_password: '',
};

function targetConfig(form: typeof initialForm): Record<string, unknown> {
  if (form.type === 's3') {
    return {
      bucket: form.bucket,
      region: form.region,
      endpoint_url: form.endpoint_url,
      prefix: form.prefix,
      path_style: form.path_style,
    };
  }
  if (form.type === 'hetzner_storagebox') {
    return {
      host: form.host,
      port: Number(form.port || 23),
      username: form.username,
      base_path: form.base_path,
      auth_method: form.auth_method,
      backup_mode: form.backup_mode,
    };
  }
  if (form.type === 'pbs') {
    return { datastore: form.datastore };
  }
  return { storage_id: form.storage_id, node_name: form.node_name };
}

function configValue(config: Record<string, unknown>, key: string): string {
  const value = config[key];
  return value === undefined || value === null || value === '' ? '-' : String(value);
}

export function BackupsPage() {
  const qc = useQueryClient();
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState(initialForm);
  const { data, isLoading, error } = useQuery({ queryKey: ['backup-summary'], queryFn: getBackupSummary });
  const targets = data?.targets ?? [];
  const externalTargets = targets.filter((target) => target.type === 's3' || target.type === 'hetzner_storagebox');

  const createMut = useMutation({
    mutationFn: createBackupTarget,
    onSuccess: () => {
      setForm(initialForm);
      setShowForm(false);
      qc.invalidateQueries({ queryKey: ['backup-summary'] });
    },
  });
  const deleteMut = useMutation({
    mutationFn: deleteBackupTarget,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['backup-summary'] }),
  });

  const providerHint = useMemo(() => {
    if (form.type === 's3') return 'Use AWS S3, MinIO, Wasabi, Backblaze B2, or another S3-compatible endpoint.';
    if (form.type === 'hetzner_storagebox') return 'Use Storage Box SFTP credentials. Restic is the default backup mode.';
    if (form.type === 'pbs') return 'Register a Proxmox Backup Server datastore as a native backup target.';
    return 'Use an existing Proxmox storage ID for fast local backup staging.';
  }, [form.type]);

  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    createMut.mutate({
      name: form.name,
      type: form.type,
      enabled: true,
      config: targetConfig(form),
      access_key: form.access_key,
      secret_key: form.secret_key,
      password: form.password,
      ssh_private_key: form.ssh_private_key,
      restic_password: form.restic_password,
    });
  };

  return (
    <Layout>
      <PageHeader
        title="Backups"
        description="Register backup targets for Proxmox, S3-compatible storage, and Hetzner Storage Box."
        actions={
          <Button onClick={() => setShowForm((open) => !open)}>
            <Plus className="h-4 w-4" /> Add Target
          </Button>
        }
      />

      {isLoading ? (
        <PageSpinner />
      ) : error ? (
        <ErrorMessage error={error} />
      ) : (
        <div className="space-y-6">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <Card>
              <CardContent className="p-4">
                <Archive className="mb-3 h-5 w-5 text-indigo-500" />
                <p className="text-xl font-bold text-slate-900">{data?.target_count ?? 0}</p>
                <p className="text-xs text-slate-500">backup target(s)</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="p-4">
                <Cloud className="mb-3 h-5 w-5 text-sky-500" />
                <p className="text-xl font-bold text-slate-900">{data?.external_count ?? 0}</p>
                <p className="text-xs text-slate-500">external provider target(s)</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="p-4">
                <Server className="mb-3 h-5 w-5 text-emerald-500" />
                <p className="text-xl font-bold text-slate-900">{data?.enabled_count ?? 0}</p>
                <p className="text-xs text-slate-500">enabled target(s)</p>
              </CardContent>
            </Card>
          </div>

          <Alert variant="info" title="Backup execution foundation">
            Targets created here are stored with encrypted secrets and are ready for the backup job runner layer: Proxmox backup artifact creation, Restic upload, retention, and restore workflows.
          </Alert>

          {showForm && (
            <Card>
              <CardHeader><CardTitle>Add Backup Target</CardTitle></CardHeader>
              <CardContent>
                <form onSubmit={handleSubmit} className="space-y-4">
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <Input label="Name" value={form.name} onChange={(e) => setField('name', e.target.value)} required placeholder="prod-s3-backups" />
                    <Select label="Provider" value={form.type} onChange={(e) => setField('type', e.target.value as BackupTargetType)}>
                      {Object.entries(targetLabels).map(([value, label]) => (
                        <option key={value} value={value}>{label}</option>
                      ))}
                    </Select>
                  </div>
                  <p className="text-xs text-slate-500">{providerHint}</p>

                  {form.type === 's3' && (
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <Input label="Bucket" value={form.bucket} onChange={(e) => setField('bucket', e.target.value)} required />
                      <Input label="Region" value={form.region} onChange={(e) => setField('region', e.target.value)} placeholder="us-east-1" />
                      <Input label="Endpoint URL" value={form.endpoint_url} onChange={(e) => setField('endpoint_url', e.target.value)} placeholder="https://s3.amazonaws.com" />
                      <Input label="Prefix" value={form.prefix} onChange={(e) => setField('prefix', e.target.value)} placeholder="proxacloud/" />
                      <Input label="Access key" value={form.access_key} onChange={(e) => setField('access_key', e.target.value)} />
                      <Input label="Secret key" type="password" value={form.secret_key} onChange={(e) => setField('secret_key', e.target.value)} />
                      <Input label="Restic password" type="password" value={form.restic_password} onChange={(e) => setField('restic_password', e.target.value)} />
                      <label className="mt-7 flex items-center gap-3 text-sm text-slate-700">
                        <input type="checkbox" checked={form.path_style} onChange={(e) => setField('path_style', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                        Path-style endpoint
                      </label>
                    </div>
                  )}

                  {form.type === 'hetzner_storagebox' && (
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <Input label="Host" value={form.host} onChange={(e) => setField('host', e.target.value)} required placeholder="u123456.your-storagebox.de" />
                      <Input label="Port" value={form.port} onChange={(e) => setField('port', e.target.value)} required />
                      <Input label="Username" value={form.username} onChange={(e) => setField('username', e.target.value)} required />
                      <Input label="Base path" value={form.base_path} onChange={(e) => setField('base_path', e.target.value)} required placeholder="/backups/proxacloud" />
                      <Select label="Auth method" value={form.auth_method} onChange={(e) => setField('auth_method', e.target.value)}>
                        <option value="password">Password</option>
                        <option value="ssh_key">SSH key</option>
                      </Select>
                      <Select label="Backup mode" value={form.backup_mode} onChange={(e) => setField('backup_mode', e.target.value)}>
                        <option value="restic">Restic</option>
                        <option value="borg">Borg</option>
                        <option value="rsync">Rsync</option>
                      </Select>
                      <Input label="Password" type="password" value={form.password} onChange={(e) => setField('password', e.target.value)} />
                      <Input label="Restic password" type="password" value={form.restic_password} onChange={(e) => setField('restic_password', e.target.value)} />
                    </div>
                  )}

                  {form.type === 'pbs' && (
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <Input label="Datastore" value={form.datastore} onChange={(e) => setField('datastore', e.target.value)} required placeholder="backup-store" />
                    </div>
                  )}

                  {form.type === 'proxmox_storage' && (
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <Input label="Storage ID" value={form.storage_id} onChange={(e) => setField('storage_id', e.target.value)} required placeholder="local" />
                      <Input label="Node name" value={form.node_name} onChange={(e) => setField('node_name', e.target.value)} placeholder="optional" />
                    </div>
                  )}

                  {createMut.error && <ErrorMessage error={createMut.error} />}
                  <Button type="submit" loading={createMut.isPending}>Save Target</Button>
                </form>
              </CardContent>
            </Card>
          )}

          <Card>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Name</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Provider</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Location</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Secrets</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Status</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {targets.map((target) => (
                    <tr key={target.id} className="hover:bg-slate-50">
                      <td className="px-6 py-3 font-medium text-slate-900">{target.name}</td>
                      <td className="px-6 py-3 text-slate-600">{targetLabels[target.type]}</td>
                      <td className="px-6 py-3 text-slate-600">
                        {target.type === 's3' && `${configValue(target.config, 'bucket')} / ${configValue(target.config, 'prefix')}`}
                        {target.type === 'hetzner_storagebox' && `${configValue(target.config, 'host')} ${configValue(target.config, 'base_path')}`}
                        {target.type === 'pbs' && configValue(target.config, 'datastore')}
                        {target.type === 'proxmox_storage' && configValue(target.config, 'storage_id')}
                      </td>
                      <td className="px-6 py-3">
                        <div className="flex flex-wrap gap-2">
                          {target.has_access_key && <Badge variant="info">access</Badge>}
                          {target.has_secret_key && <Badge variant="info">secret</Badge>}
                          {target.has_password && <Badge variant="info">password</Badge>}
                          {target.has_restic_password && <Badge variant="warning">restic</Badge>}
                          {!target.has_access_key && !target.has_secret_key && !target.has_password && !target.has_restic_password && <span className="text-xs text-slate-400">none</span>}
                        </div>
                      </td>
                      <td className="px-6 py-3"><Badge variant={target.enabled ? 'success' : 'default'}>{target.enabled ? 'enabled' : 'disabled'}</Badge></td>
                      <td className="px-6 py-3 text-right">
                        <Button
                          variant="danger"
                          size="sm"
                          onClick={() => {
                            if (window.confirm(`Delete backup target "${target.name}"?`)) deleteMut.mutate(target.id);
                          }}
                          loading={deleteMut.isPending && deleteMut.variables === target.id}
                        >
                          <Trash2 className="h-3.5 w-3.5" /> Delete
                        </Button>
                      </td>
                    </tr>
                  ))}
                  {targets.length === 0 && (
                    <tr>
                      <td colSpan={6} className="px-6 py-8 text-center text-sm text-slate-500">No backup targets configured yet.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>

          {externalTargets.length > 0 && (
            <Alert variant="success">
              External targets are ready for the next backup runner step: Proxmox artifact creation, Restic repository upload, retention pruning, and restore validation.
            </Alert>
          )}
        </div>
      )}
    </Layout>
  );
}
