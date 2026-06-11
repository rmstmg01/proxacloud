import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, KeyRound } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createClusterSecret,
  createGlobalSecret,
  deleteClusterSecretKey,
  deleteGlobalSecret,
  getClusters,
  getGlobalSecrets,
  updateClusterSecretKey,
  updateGlobalSecret,
} from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import type { ClusterSecretProvider, ClusterSecretRecord } from '../types';

const SECRET_PROVIDERS: { value: ClusterSecretProvider; label: string }[] = [
  { value: 'native', label: 'Native encrypted DB' },
  { value: 'hashicorp-vault', label: 'HashiCorp Vault' },
  { value: 'external-secrets-operator', label: 'External Secrets Operator' },
  { value: 'aws-secrets-manager', label: 'AWS Secrets Manager' },
  { value: 'azure-key-vault', label: 'Azure Key Vault' },
  { value: 'google-secret-manager', label: 'Google Secret Manager' },
  { value: 'bitnami-sealed-secrets', label: 'Bitnami Sealed Secrets' },
];

function parseKeyValues(raw: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const idx = trimmed.indexOf('=');
    if (idx <= 0) continue;
    out[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1);
  }
  return out;
}

function providerDefaults(provider: ClusterSecretProvider): Record<string, unknown> {
  if (provider === 'native' || provider === 'bitnami-sealed-secrets') return {};
  return {
    secret_store_name: 'cluster-secret-store',
    secret_store_kind: 'ClusterSecretStore',
    refresh_interval: '1h',
  };
}

export function ClusterSecretsPage() {
  const queryClient = useQueryClient();
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const { data: globalSecrets, error: globalSecretsError } = useQuery({ queryKey: ['global-secrets'], queryFn: getGlobalSecrets });
  const [editing, setEditing] = useState<ClusterSecretRecord | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ClusterSecretRecord | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [keyForm, setKeyForm] = useState({ secretID: 0, key: '', value: '' });
  const [form, setForm] = useState({
    cluster_id: 0,
    name: '',
    namespace: 'default',
    provider: 'native' as ClusterSecretProvider,
    type: 'Opaque',
    description: '',
    external_ref: '',
    provider_config_raw: '{}',
    immutable: false,
    is_global: false,
    data_raw: 'USERNAME=\nPASSWORD=',
  });

  const resetForm = () => {
    setEditing(null);
    setForm({
      cluster_id: 0,
      name: '',
      namespace: 'default',
      provider: 'native',
      type: 'Opaque',
      description: '',
      external_ref: '',
      provider_config_raw: '{}',
      immutable: false,
      is_global: false,
      data_raw: 'USERNAME=\nPASSWORD=',
    });
  };

  const startEdit = (secret: ClusterSecretRecord) => {
    setEditing(secret);
    setForm({
      cluster_id: 0,
      name: secret.name,
      namespace: secret.namespace,
      provider: secret.provider as ClusterSecretProvider,
      type: secret.type,
      description: secret.description || '',
      external_ref: secret.external_ref || '',
      provider_config_raw: JSON.stringify(secret.provider_config || providerDefaults(secret.provider as ClusterSecretProvider), null, 2),
      immutable: secret.immutable,
      is_global: true,
      data_raw: secret.keys.map((key) => `${key}=`).join('\n'),
    });
  };

  const saveMut = useMutation({
    mutationFn: async () => {
      const provider_config = JSON.parse(form.provider_config_raw || '{}');
      const parsedData = parseKeyValues(form.data_raw);
      const hasReplacementValues = Object.values(parsedData).some((value) => value.length > 0);
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        provider: form.provider,
        type: form.type.trim() || 'Opaque',
        description: form.description.trim(),
        external_ref: form.external_ref.trim(),
        provider_config,
        immutable: form.immutable,
        is_global: form.is_global || form.cluster_id <= 0,
        data: parsedData,
      };
      if (editing) {
        return updateGlobalSecret(editing.id, { ...body, is_global: true, data: hasReplacementValues ? parsedData : undefined });
      }
      return body.is_global ? createGlobalSecret(body) : createClusterSecret(form.cluster_id, body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-secrets'] });
      resetForm();
    },
  });

  const deleteMut = useMutation({
    mutationFn: (secretID: number) => deleteGlobalSecret(secretID),
    onSuccess: (_data, secretID) => {
      queryClient.invalidateQueries({ queryKey: ['global-secrets'] });
      if (editing?.id === secretID) resetForm();
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  const updateKeyMut = useMutation({
    mutationFn: (body: { secretID: number; key: string; value: string }) =>
      updateClusterSecretKey(0, body.secretID, body.key.trim(), body.value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-secrets'] });
      setKeyForm({ secretID: 0, key: '', value: '' });
    },
  });

  const deleteKeyMut = useMutation({
    mutationFn: (body: { secretID: number; key: string }) =>
      deleteClusterSecretKey(0, body.secretID, body.key),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-secrets'] });
      setKeyForm({ secretID: 0, key: '', value: '' });
    },
  });

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMut.mutate(deleteTarget.id);
  };

  const onProviderChange = (provider: ClusterSecretProvider) => {
    setForm((prev) => ({
      ...prev,
      provider,
      provider_config_raw: JSON.stringify(providerDefaults(provider), null, 2),
    }));
  };

  return (
    <Layout>
      <PageHeader title="Secrets" description="Create secrets directly or manage them by cluster" />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {globalSecretsError && <ErrorMessage error={globalSecretsError} />}
      {clusters && (
        <div className="space-y-6">
          <Card>
            <CardHeader><CardTitle>{editing ? 'Edit Global Secret' : 'Create Secret'}</CardTitle></CardHeader>
            <CardContent>
              <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Target cluster" value={form.cluster_id} onChange={(e) => setForm({ ...form, cluster_id: Number(e.target.value) })} disabled={!!editing}>
                    <option value={0}>No target cluster yet</option>
                    {clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}
                  </Select>
                  <Input label="Secret name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
                  <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Provider" value={form.provider} onChange={(e) => onProviderChange(e.target.value as ClusterSecretProvider)}>
                    {SECRET_PROVIDERS.map((provider) => <option key={provider.value} value={provider.value}>{provider.label}</option>)}
                  </Select>
                  <Input label="Kubernetes type" value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} />
                  <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
                </div>
                {form.provider !== 'native' && (
                  <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                    <Textarea
                      label={form.provider === 'bitnami-sealed-secrets' ? 'SealedSecret manifest' : 'External secret reference'}
                      value={form.external_ref}
                      onChange={(e) => setForm({ ...form, external_ref: e.target.value })}
                      rows={6}
                      className="font-mono text-xs"
                      required
                    />
                    <Textarea
                      label="Provider config JSON"
                      value={form.provider_config_raw}
                      onChange={(e) => setForm({ ...form, provider_config_raw: e.target.value })}
                      rows={6}
                      className="font-mono text-xs"
                    />
                  </div>
                )}
                <Textarea
                  label={form.provider === 'native' ? 'Secret data' : 'Secret keys'}
                  value={form.data_raw}
                  onChange={(e) => setForm({ ...form, data_raw: e.target.value })}
                  rows={7}
                  className="font-mono text-xs"
                  helperText="Use KEY=value per line. Values are encrypted for native secrets; provider-backed secrets use keys as metadata."
                  required
                />
                <label className="flex items-center gap-3">
                  <input type="checkbox" checked={form.immutable} onChange={(e) => setForm({ ...form, immutable: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Immutable Kubernetes Secret</span>
                </label>
                <label className="flex items-center gap-3">
                  <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} disabled={!!editing} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Make available to all clusters</span>
                </label>
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" loading={saveMut.isPending}>{editing ? 'Save Secret' : 'Create Secret'}</Button>
                  {editing && <Button type="button" variant="secondary" onClick={resetForm}>Cancel</Button>}
                </div>
                {saveMut.data && (
                  <Alert variant="success" title={editing ? 'Secret updated' : 'Secret created'}>
                    {saveMut.data.is_global ? (
                      <span>{saveMut.data.name} is listed below and available to all clusters.</span>
                    ) : saveMut.data.cluster_id ? (
                      <Link className="text-indigo-600 hover:text-indigo-700" to={`/clusters/${saveMut.data.cluster_id}?tab=secrets`}>
                        Open {saveMut.data.name} in cluster secrets
                      </Link>
                    ) : (
                      <span>{saveMut.data.name} is available globally and can be selected during app deployment.</span>
                    )}
                  </Alert>
                )}
                {saveMut.error && <ErrorMessage error={saveMut.error} />}
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Manage Existing Secrets</CardTitle></CardHeader>
            <CardContent>
              <div className="mb-6">
                <p className="text-sm font-medium text-slate-900">Global Secrets</p>
                {!globalSecrets?.length ? (
                  <p className="mt-2 text-sm text-slate-400">No global Secrets yet.</p>
                ) : (
                  <div className="mt-3 divide-y divide-slate-100 border-y border-slate-100">
                    {globalSecrets.map((secret) => (
                      <div key={secret.id} className="flex flex-col gap-3 py-4 lg:flex-row lg:items-start lg:justify-between">
                        <div className="flex min-w-0 gap-3">
                          <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
                            <KeyRound className="h-4 w-4" />
                          </div>
                          <div className="min-w-0">
                            <p className="text-sm font-medium text-slate-900">{secret.name}</p>
                            <p className="text-xs text-slate-500">
                              namespace: {secret.namespace} - provider: {secret.provider} - keys: {secret.keys.length}
                            </p>
                            <div className="mt-2 flex flex-wrap gap-1">
                              {secret.keys.length ? secret.keys.map((key) => (
                                <span key={key} className="inline-flex items-center gap-1 rounded bg-slate-100 px-2 py-1">
                                  <button
                                    type="button"
                                    className="font-mono text-xs text-slate-700 hover:text-indigo-700"
                                    onClick={() => setKeyForm({ secretID: secret.id, key, value: '' })}
                                  >
                                    {key}
                                  </button>
                                  <button
                                    type="button"
                                    className="text-xs text-rose-600 hover:text-rose-700"
                                    disabled={deleteKeyMut.isPending}
                                    onClick={() => {
                                      if (window.confirm(`Delete key ${key} from ${secret.name}?`)) {
                                        deleteKeyMut.mutate({ secretID: secret.id, key });
                                      }
                                    }}
                                  >
                                    delete
                                  </button>
                                </span>
                              )) : (
                                <span className="text-xs text-slate-400">No keys</span>
                              )}
                              <button
                                type="button"
                                className="inline-flex items-center rounded bg-indigo-50 px-2 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100"
                                onClick={() => setKeyForm({ secretID: secret.id, key: '', value: '' })}
                              >
                                Add key
                              </button>
                            </div>
                            {keyForm.secretID === secret.id && (
                              <div className="mt-3 flex flex-wrap items-end gap-2">
                                <Input
                                  label="Key"
                                  value={keyForm.key}
                                  onChange={(e) => setKeyForm({ ...keyForm, key: e.target.value })}
                                  className="w-52"
                                />
                                <Input
                                  label="Value"
                                  type="password"
                                  value={keyForm.value}
                                  onChange={(e) => setKeyForm({ ...keyForm, value: e.target.value })}
                                  className="w-56"
                                />
                                <Button
                                  type="button"
                                  size="sm"
                                  loading={updateKeyMut.isPending && updateKeyMut.variables?.secretID === secret.id}
                                  disabled={!keyForm.key.trim() || !keyForm.value}
                                  onClick={() => updateKeyMut.mutate(keyForm)}
                                >
                                  Add/update key
                                </Button>
                              </div>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-medium text-indigo-700">Global</span>
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(secret)}>Edit</Button>
                          <Button
                            type="button"
                            size="sm"
                            variant="danger"
                            loading={deleteMut.isPending && deleteMut.variables === secret.id}
                            onClick={() => {
                              setDeleteTarget(secret);
                              setDeleteConfirmationName('');
                            }}
                          >
                            Delete
                          </Button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
                {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
                {updateKeyMut.error && <ErrorMessage error={updateKeyMut.error} />}
                {deleteKeyMut.error && <ErrorMessage error={deleteKeyMut.error} />}
              </div>

              {clusters.length === 0 ? (
                <p className="text-sm text-slate-400">No clusters available.</p>
              ) : (
                <div className="divide-y divide-slate-100">
                  {clusters.map((cluster) => (
                    <Link
                      key={cluster.id}
                      to={`/clusters/${cluster.id}?tab=secrets`}
                      className="flex items-center justify-between gap-4 py-4 hover:bg-slate-50 -mx-6 px-6 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
                          <KeyRound className="h-4 w-4" />
                        </div>
                        <div>
                          <p className="text-sm font-medium text-slate-900">{cluster.name}</p>
                          <p className="text-xs text-slate-500">{cluster.topology_type} - {cluster.kubernetes_version}</p>
                        </div>
                      </div>
                      <span className="inline-flex items-center gap-1 text-sm text-indigo-600">
                        Manage secrets <ChevronRight className="h-4 w-4" />
                      </span>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}
      <DeleteSecretModal
        secret={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMut.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMut.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDelete}
      />
    </Layout>
  );
}

function DeleteSecretModal({
  secret,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  secret: ClusterSecretRecord | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!secret && confirmationName === secret.name;
  return (
    <Modal open={!!secret} onClose={onClose} title="Delete Secret" maxWidth="lg">
      {secret && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This removes the portal Secret record">
            This does not automatically delete a Kubernetes Secret that has already been applied to a cluster.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{secret.name}</span> to confirm deletion.
          </div>
          <Input label="Secret name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>Cancel</Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>Delete Secret</Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
