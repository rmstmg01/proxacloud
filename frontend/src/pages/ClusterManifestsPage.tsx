import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, FileCode } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createClusterSavedManifest,
  createGlobalSavedManifest,
  deleteGlobalSavedManifest,
  getClusters,
  getGlobalSavedManifests,
  updateGlobalSavedManifest,
} from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import type { ClusterSavedManifestRecord } from '../types';

const SAMPLE_MANIFEST = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80`;

export function ClusterManifestsPage() {
  const qc = useQueryClient();
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const { data: globalManifests } = useQuery({ queryKey: ['global-saved-manifests'], queryFn: getGlobalSavedManifests });
  const [editing, setEditing] = useState<ClusterSavedManifestRecord | null>(null);
  const [form, setForm] = useState({
    cluster_id: 0,
    name: '',
    namespace: 'default',
    description: '',
    raw_manifest: SAMPLE_MANIFEST,
    is_global: false,
  });

  const createMut = useMutation({
    mutationFn: () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        description: form.description.trim(),
        raw_manifest: form.raw_manifest,
        is_global: form.is_global || form.cluster_id <= 0,
      };
      if (editing) return updateGlobalSavedManifest(editing.id, body);
      return form.cluster_id > 0 ? createClusterSavedManifest(form.cluster_id, body) : createGlobalSavedManifest(body);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['global-saved-manifests'] });
      setEditing(null);
    },
  });

  const deleteMut = useMutation({
    mutationFn: (manifestID: number) => deleteGlobalSavedManifest(manifestID),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['global-saved-manifests'] }),
  });

  const startEdit = (manifest: ClusterSavedManifestRecord) => {
    setEditing(manifest);
    setForm({
      cluster_id: 0,
      name: manifest.name,
      namespace: manifest.namespace,
      description: manifest.description || '',
      raw_manifest: manifest.raw_manifest,
      is_global: true,
    });
  };

  return (
    <Layout>
      <PageHeader title="Manifests" description="Store Kubernetes YAML, then apply it to clusters with revision history and logs" />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {clusters && (
        <div className="space-y-6">
          <Card>
            <CardHeader><CardTitle>{editing ? 'Edit Global Manifest' : 'Create Manifest'}</CardTitle></CardHeader>
            <CardContent>
              <form onSubmit={(e) => { e.preventDefault(); createMut.mutate(); }} className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Target cluster" value={form.cluster_id} onChange={(e) => setForm({ ...form, cluster_id: Number(e.target.value) })} disabled={Boolean(editing)}>
                    <option value={0}>Global template</option>
                    {clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}
                  </Select>
                  <Input label="Manifest name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
                  <Input label="Default namespace" value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} required />
                </div>
                <Input label="Description" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} className="max-w-4xl" />
                <Textarea
                  label="Manifest YAML"
                  value={form.raw_manifest}
                  onChange={(e) => setForm({ ...form, raw_manifest: e.target.value })}
                  rows={18}
                  className="font-mono text-xs"
                  required
                />
                <label className="flex items-center gap-3">
                  <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Make available to all clusters</span>
                </label>
                <div className="flex gap-3">
                  <Button type="submit" loading={createMut.isPending}>{editing ? 'Update Manifest' : 'Save Manifest'}</Button>
                  {editing && <Button type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Button>}
                </div>
                {createMut.data && (
                  <Alert variant="success" title="Manifest saved">
                    {createMut.data.cluster_id ? (
                      <Link className="text-indigo-600 hover:text-indigo-700" to={`/clusters/${createMut.data.cluster_id}?tab=manifests`}>
                        Open {createMut.data.name} in cluster manifests
                      </Link>
                    ) : (
                      <span>{createMut.data.name} is available globally and can be applied from a cluster&apos;s Manifests tab.</span>
                    )}
                  </Alert>
                )}
                {createMut.error && <ErrorMessage error={createMut.error} />}
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Manage Manifests By Cluster</CardTitle></CardHeader>
            <CardContent>
              {clusters.length === 0 ? (
                <p className="text-sm text-slate-400">No clusters available.</p>
              ) : (
                <div className="divide-y divide-slate-100">
                  {clusters.map((cluster) => (
                    <Link
                      key={cluster.id}
                      to={`/clusters/${cluster.id}?tab=manifests`}
                      className="flex items-center justify-between gap-4 py-4 hover:bg-slate-50 -mx-6 px-6 transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
                          <FileCode className="h-4 w-4" />
                        </div>
                        <div>
                          <p className="text-sm font-medium text-slate-900">{cluster.name}</p>
                          <p className="text-xs text-slate-500">{cluster.topology_type} - {cluster.kubernetes_version}</p>
                        </div>
                      </div>
                      <span className="inline-flex items-center gap-1 text-sm text-indigo-600">
                        Manage Manifests <ChevronRight className="h-4 w-4" />
                      </span>
                    </Link>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Global Templates</CardTitle></CardHeader>
            <CardContent>
              {!globalManifests?.length ? (
                <p className="text-sm text-slate-400">No global manifests saved yet.</p>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {globalManifests.map((manifest) => (
                    <div key={manifest.id} className="rounded-md border border-slate-200 p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-medium text-slate-900">{manifest.name}</p>
                          <p className="text-xs text-slate-500">{manifest.namespace} - rev {manifest.latest_revision || 1}</p>
                        </div>
                        <span className="text-xs rounded-full bg-slate-100 px-2 py-1 text-slate-600">global</span>
                      </div>
                      {manifest.description && <p className="mt-2 text-sm text-slate-600">{manifest.description}</p>}
                      <div className="mt-4 flex gap-2">
                        <Button type="button" size="sm" variant="secondary" onClick={() => startEdit(manifest)}>Edit</Button>
                        <Button type="button" size="sm" variant="danger" loading={deleteMut.isPending && deleteMut.variables === manifest.id} onClick={() => deleteMut.mutate(manifest.id)}>Delete</Button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              {deleteMut.error && <ErrorMessage error={deleteMut.error} />}
            </CardContent>
          </Card>
        </div>
      )}
    </Layout>
  );
}
