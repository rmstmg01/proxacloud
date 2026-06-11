import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, FileText } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createClusterConfigMap,
  createGlobalConfigMap,
  deleteClusterConfigMapKey,
  deleteGlobalConfigMap,
  getClusters,
  getGlobalConfigMaps,
  updateClusterConfigMapKey,
  updateGlobalConfigMap,
} from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import type { ClusterConfigMapRecord } from '../types';

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

export function ClusterConfigMapsPage() {
  const queryClient = useQueryClient();
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const { data: globalConfigMaps, error: globalConfigMapsError } = useQuery({ queryKey: ['global-config-maps'], queryFn: getGlobalConfigMaps });
  const [editing, setEditing] = useState<ClusterConfigMapRecord | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ClusterConfigMapRecord | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const [keyForm, setKeyForm] = useState({ configMapID: 0, key: '', value: '' });
  const [form, setForm] = useState({
    cluster_id: 0,
    name: '',
    namespace: 'default',
    description: '',
    immutable: false,
    is_global: false,
    data_raw: 'APP_ENV=production\nLOG_LEVEL=info',
  });

  const resetForm = () => {
    setEditing(null);
    setForm({
      cluster_id: 0,
      name: '',
      namespace: 'default',
      description: '',
      immutable: false,
      is_global: false,
      data_raw: 'APP_ENV=production\nLOG_LEVEL=info',
    });
  };

  const startEdit = (configMap: ClusterConfigMapRecord) => {
    setEditing(configMap);
    setForm({
      cluster_id: 0,
      name: configMap.name,
      namespace: configMap.namespace,
      description: configMap.description || '',
      immutable: configMap.immutable,
      is_global: true,
      data_raw: Object.entries(configMap.data || {}).map(([key, value]) => `${key}=${value}`).join('\n'),
    });
  };

  const saveMut = useMutation({
    mutationFn: () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        description: form.description.trim(),
        immutable: form.immutable,
        is_global: form.is_global || form.cluster_id <= 0,
        data: parseKeyValues(form.data_raw),
      };
      if (editing) return updateGlobalConfigMap(editing.id, { ...body, is_global: true });
      return body.is_global ? createGlobalConfigMap(body) : createClusterConfigMap(form.cluster_id, body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-config-maps'] });
      resetForm();
    },
  });

  const deleteMut = useMutation({
    mutationFn: (configMapID: number) => deleteGlobalConfigMap(configMapID),
    onSuccess: (_data, configMapID) => {
      queryClient.invalidateQueries({ queryKey: ['global-config-maps'] });
      if (editing?.id === configMapID) resetForm();
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  const updateKeyMut = useMutation({
    mutationFn: (body: { configMapID: number; key: string; value: string }) =>
      updateClusterConfigMapKey(0, body.configMapID, body.key.trim(), body.value),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-config-maps'] });
      setKeyForm({ configMapID: 0, key: '', value: '' });
    },
  });

  const deleteKeyMut = useMutation({
    mutationFn: (body: { configMapID: number; key: string }) =>
      deleteClusterConfigMapKey(0, body.configMapID, body.key),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-config-maps'] });
      setKeyForm({ configMapID: 0, key: '', value: '' });
    },
  });

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMut.mutate(deleteTarget.id);
  };

  return (
    <Layout>
      <PageHeader title="ConfigMaps" description="Create non-sensitive app configuration or manage by cluster" />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {globalConfigMapsError && <ErrorMessage error={globalConfigMapsError} />}
      {clusters && (
        <div className="space-y-6">
          <Card>
            <CardHeader><CardTitle>{editing ? 'Edit Global ConfigMap' : 'Create ConfigMap'}</CardTitle></CardHeader>
            <CardContent>
              <form onSubmit={(e) => { e.preventDefault(); saveMut.mutate(); }} className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Target cluster" value={form.cluster_id} onChange={(e) => setForm({ ...form, cluster_id: Number(e.target.value) })} disabled={!!editing}>
                    <option value={0}>No target cluster yet</option>
                    {clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}
                  </Select>
                  <Input label="ConfigMap name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
                  <Input label="Namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
                </div>
                <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} className="max-w-4xl" />
                <Textarea
                  label="Config data"
                  value={form.data_raw}
                  onChange={(e) => setForm({ ...form, data_raw: e.target.value })}
                  rows={8}
                  className="font-mono text-xs"
                  helperText="Use KEY=value per line. ConfigMaps are for non-sensitive configuration only."
                  required
                />
                <label className="flex items-center gap-3">
                  <input type="checkbox" checked={form.immutable} onChange={(e) => setForm({ ...form, immutable: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Immutable Kubernetes ConfigMap</span>
                </label>
                <label className="flex items-center gap-3">
                  <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} disabled={!!editing} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Make available to all clusters</span>
                </label>
                <div className="flex flex-wrap gap-3">
                  <Button type="submit" loading={saveMut.isPending}>{editing ? 'Save ConfigMap' : 'Create ConfigMap'}</Button>
                  {editing && <Button type="button" variant="secondary" onClick={resetForm}>Cancel</Button>}
                </div>
                {saveMut.data && (
                  <Alert variant="success" title={editing ? 'ConfigMap updated' : 'ConfigMap created'}>
                    {saveMut.data.is_global ? (
                      <span>{saveMut.data.name} is listed below and available to all clusters.</span>
                    ) : saveMut.data.cluster_id ? (
                      <Link className="text-indigo-600 hover:text-indigo-700" to={`/clusters/${saveMut.data.cluster_id}?tab=configmaps`}>
                        Open {saveMut.data.name} in cluster ConfigMaps
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
            <CardHeader><CardTitle>Manage Existing ConfigMaps</CardTitle></CardHeader>
            <CardContent>
              <div className="mb-6">
                <p className="text-sm font-medium text-slate-900">Global ConfigMaps</p>
                {!globalConfigMaps?.length ? (
                  <p className="mt-2 text-sm text-slate-400">No global ConfigMaps yet.</p>
                ) : (
                  <div className="mt-3 divide-y divide-slate-100 border-y border-slate-100">
                    {globalConfigMaps.map((configMap) => (
                      <div key={configMap.id} className="flex flex-col gap-3 py-4 lg:flex-row lg:items-start lg:justify-between">
                        <div className="flex min-w-0 gap-3">
                          <div className="h-9 w-9 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center">
                            <FileText className="h-4 w-4" />
                          </div>
                          <div className="min-w-0">
                            <p className="text-sm font-medium text-slate-900">{configMap.name}</p>
                            <p className="text-xs text-slate-500">
                              namespace: {configMap.namespace} - keys: {Object.keys(configMap.data || {}).length}
                            </p>
                            <div className="mt-2 flex flex-wrap gap-1">
                              {Object.keys(configMap.data || {}).length ? Object.keys(configMap.data || {}).map((key) => (
                                <span key={key} className="inline-flex items-center gap-1 rounded bg-slate-100 px-2 py-1">
                                  <button
                                    type="button"
                                    className="font-mono text-xs text-slate-700 hover:text-indigo-700"
                                    onClick={() => setKeyForm({ configMapID: configMap.id, key, value: configMap.data[key] || '' })}
                                  >
                                    {key}
                                  </button>
                                  <button
                                    type="button"
                                    className="text-xs text-rose-600 hover:text-rose-700"
                                    disabled={deleteKeyMut.isPending}
                                    onClick={() => {
                                      if (window.confirm(`Delete key ${key} from ${configMap.name}?`)) {
                                        deleteKeyMut.mutate({ configMapID: configMap.id, key });
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
                                className="inline-flex items-center rounded bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 hover:bg-emerald-100"
                                onClick={() => setKeyForm({ configMapID: configMap.id, key: '', value: '' })}
                              >
                                Add key
                              </button>
                            </div>
                            {keyForm.configMapID === configMap.id && (
                              <div className="mt-3 flex flex-wrap items-end gap-2">
                                <Input
                                  label="Key"
                                  value={keyForm.key}
                                  onChange={(e) => setKeyForm({ ...keyForm, key: e.target.value })}
                                  className="w-52"
                                />
                                <Input
                                  label="Value"
                                  value={keyForm.value}
                                  onChange={(e) => setKeyForm({ ...keyForm, value: e.target.value })}
                                  className="w-56"
                                />
                                <Button
                                  type="button"
                                  size="sm"
                                  loading={updateKeyMut.isPending && updateKeyMut.variables?.configMapID === configMap.id}
                                  disabled={!keyForm.key.trim()}
                                  onClick={() => updateKeyMut.mutate(keyForm)}
                                >
                                  Add/update key
                                </Button>
                              </div>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-medium text-emerald-700">Global</span>
                          <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(configMap)}>Edit</Button>
                          <Button
                            type="button"
                            size="sm"
                            variant="danger"
                            loading={deleteMut.isPending && deleteMut.variables === configMap.id}
                            onClick={() => {
                              setDeleteTarget(configMap);
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
                      to={`/clusters/${cluster.id}?tab=configmaps`}
                      className="flex items-center justify-between gap-4 py-4 hover:bg-slate-50 -mx-6 px-6 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        <div className="h-9 w-9 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center">
                          <FileText className="h-4 w-4" />
                        </div>
                        <div>
                          <p className="text-sm font-medium text-slate-900">{cluster.name}</p>
                          <p className="text-xs text-slate-500">{cluster.topology_type} - {cluster.kubernetes_version}</p>
                        </div>
                      </div>
                      <span className="inline-flex items-center gap-1 text-sm text-indigo-600">
                        Manage ConfigMaps <ChevronRight className="h-4 w-4" />
                      </span>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}
      <DeleteConfigMapModal
        configMap={deleteTarget}
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

function DeleteConfigMapModal({
  configMap,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  configMap: ClusterConfigMapRecord | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!configMap && confirmationName === configMap.name;
  return (
    <Modal open={!!configMap} onClose={onClose} title="Delete ConfigMap" maxWidth="lg">
      {configMap && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This removes the portal ConfigMap record">
            This does not automatically delete a Kubernetes ConfigMap that has already been applied to a cluster.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{configMap.name}</span> to confirm deletion.
          </div>
          <Input label="ConfigMap name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>Cancel</Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>Delete ConfigMap</Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
