import { useState, type FormEvent } from 'react';
import { KeyRound, Trash2 } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createClusterImagePullSecret,
  createGlobalImagePullSecret,
  deleteGlobalImagePullSecret,
  getClusters,
  getGlobalImagePullSecrets,
  updateGlobalImagePullSecret,
} from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';
import type { ClusterImagePullSecretRecord } from '../types';

const initialForm = {
  cluster_id: 0,
  name: '',
  namespace: 'default',
  registry_server: 'ghcr.io',
  username: '',
  password: '',
  email: '',
  description: '',
  is_global: true,
};

type PullSecretForm = typeof initialForm;

export function ClusterImagePullSecretsPage() {
  const queryClient = useQueryClient();
  const { data: clusters, isLoading: clustersLoading, error: clustersError } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const { data: globalSecrets, isLoading: secretsLoading, error: secretsError } = useQuery({
    queryKey: ['global-image-pull-secrets'],
    queryFn: getGlobalImagePullSecrets,
  });
  const [form, setForm] = useState<PullSecretForm>(initialForm);
  const [editing, setEditing] = useState<ClusterImagePullSecretRecord | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ClusterImagePullSecretRecord | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');

  const resetForm = () => {
    setEditing(null);
    setForm(initialForm);
  };

  const startEdit = (secret: ClusterImagePullSecretRecord) => {
    setEditing(secret);
    setForm({
      cluster_id: 0,
      name: secret.name,
      namespace: secret.namespace,
      registry_server: secret.registry_server,
      username: secret.username,
      password: '',
      email: secret.email || '',
      description: secret.description || '',
      is_global: true,
    });
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        registry_server: form.registry_server.trim(),
        username: form.username.trim(),
        password: form.password,
        email: form.email.trim(),
        description: form.description.trim(),
        is_global: form.is_global || form.cluster_id <= 0,
      };
      if (editing) {
        return updateGlobalImagePullSecret(editing.id, { ...body, is_global: true });
      }
      return body.is_global ? createGlobalImagePullSecret(body) : createClusterImagePullSecret(form.cluster_id, body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['global-image-pull-secrets'] });
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (secretID: number) => deleteGlobalImagePullSecret(secretID),
    onSuccess: (_data, secretID) => {
      queryClient.invalidateQueries({ queryKey: ['global-image-pull-secrets'] });
      if (editing?.id === secretID) resetForm();
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMutation.mutate(deleteTarget.id);
  };

  const loading = clustersLoading || secretsLoading;

  return (
    <Layout>
      <PageHeader title="Image Pull Secrets" description="Manage registry credentials that can be reused across Kubernetes clusters" />
      {loading && <PageSpinner />}
      {clustersError && <ErrorMessage error={clustersError} />}
      {secretsError && <ErrorMessage error={secretsError} />}
      {saveMutation.error && <ErrorMessage error={saveMutation.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>{editing ? 'Edit Global Image Pull Secret' : 'Create Image Pull Secret'}</CardTitle>
          </CardHeader>
          <CardContent>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                saveMutation.mutate();
              }}
              className="space-y-4"
            >
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                <Select
                  label="Target cluster"
                  value={form.cluster_id}
                  onChange={(event) => setForm({ ...form, cluster_id: Number(event.target.value), is_global: Number(event.target.value) <= 0 })}
                  disabled={!!editing}
                >
                  <option value={0}>No target cluster yet</option>
                  {(clusters ?? []).map((cluster) => (
                    <option key={cluster.id} value={cluster.id}>
                      {cluster.name}
                    </option>
                  ))}
                </Select>
                <Input label="Secret name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} required />
                <Input label="Namespace" value={form.namespace} onChange={(event) => setForm({ ...form, namespace: event.target.value })} required />
              </div>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                <Input label="Registry server" value={form.registry_server} onChange={(event) => setForm({ ...form, registry_server: event.target.value })} required />
                <Input label="Username" value={form.username} onChange={(event) => setForm({ ...form, username: event.target.value })} required />
                <Input
                  label={editing ? 'New password or token' : 'Password or token'}
                  type="password"
                  value={form.password}
                  onChange={(event) => setForm({ ...form, password: event.target.value })}
                  required={!editing}
                />
              </div>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                <Input label="Email" value={form.email} onChange={(event) => setForm({ ...form, email: event.target.value })} />
                <Input label="Description" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
              </div>
              <label className="flex items-center gap-3">
                <input
                  type="checkbox"
                  checked={form.is_global}
                  onChange={(event) => setForm({ ...form, is_global: event.target.checked, cluster_id: event.target.checked ? 0 : form.cluster_id })}
                  disabled={!!editing}
                  className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                />
                <span className="text-sm text-slate-700">Make available to all clusters</span>
              </label>
              <div className="flex flex-wrap gap-3">
                <Button type="submit" loading={saveMutation.isPending}>
                  {editing ? 'Save Image Pull Secret' : 'Create Image Pull Secret'}
                </Button>
                {editing && (
                  <Button type="button" variant="secondary" onClick={resetForm}>
                    Cancel
                  </Button>
                )}
              </div>
              {saveMutation.data && (
                <Alert variant="success" title={editing ? 'Image pull secret updated' : 'Image pull secret created'}>
                  {saveMutation.data.is_global
                    ? `${saveMutation.data.name} is available to all clusters.`
                    : `${saveMutation.data.name} was created for the selected cluster.`}
                </Alert>
              )}
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Global Image Pull Secrets</CardTitle>
          </CardHeader>
          <CardContent>
            {!globalSecrets?.length ? (
              <div className="py-12 text-center">
                <KeyRound className="mx-auto mb-3 h-9 w-9 text-slate-300" />
                <p className="text-sm text-slate-400">No global image pull secrets yet.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="px-4 py-3 text-left font-medium text-slate-600">Name</th>
                      <th className="px-4 py-3 text-left font-medium text-slate-600">Namespace</th>
                      <th className="px-4 py-3 text-left font-medium text-slate-600">Registry</th>
                      <th className="px-4 py-3 text-left font-medium text-slate-600">Username</th>
                      <th className="px-4 py-3 text-left font-medium text-slate-600">Last applied</th>
                      <th className="px-4 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {globalSecrets.map((secret) => (
                      <tr key={secret.id}>
                        <td className="px-4 py-3 font-medium text-slate-900">{secret.name}</td>
                        <td className="px-4 py-3 text-slate-600">{secret.namespace}</td>
                        <td className="px-4 py-3 text-slate-600">{secret.registry_server}</td>
                        <td className="px-4 py-3 text-slate-600">{secret.username}</td>
                        <td className="px-4 py-3 text-slate-500">{secret.last_applied_at ? new Date(secret.last_applied_at).toLocaleString() : 'Not applied'}</td>
                        <td className="px-4 py-3">
                          <div className="flex items-center justify-end gap-2">
                            <Button variant="secondary" size="sm" onClick={() => startEdit(secret)}>
                              Edit
                            </Button>
                            <Button
                              variant="danger"
                              size="sm"
                              onClick={() => {
                                setDeleteTarget(secret);
                                setDeleteConfirmationName('');
                              }}
                              loading={deleteMutation.isPending && deleteMutation.variables === secret.id}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                              Delete
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <DeleteImagePullSecretModal
        secret={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMutation.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDelete}
      />
    </Layout>
  );
}

function DeleteImagePullSecretModal({
  secret,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  secret: ClusterImagePullSecretRecord | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  return (
    <Modal open={!!secret} onClose={onClose} title="Delete Image Pull Secret" maxWidth="lg">
      {secret && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="Global image pull secret">
            This removes the portal record and encrypted registry token. Kubernetes Secret objects already applied to clusters are not deleted automatically.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{secret.name}</span> to confirm deletion.
          </div>
          <Input label="Secret name confirmation" value={confirmationName} onChange={(event) => onChange(event.target.value)} autoFocus autoComplete="off" />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" loading={loading} disabled={confirmationName !== secret.name}>
              <Trash2 className="h-4 w-4" />
              Delete Secret
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
