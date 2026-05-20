import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, FileText } from 'lucide-react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { createClusterConfigMap, createGlobalConfigMap, getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';

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
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
  const [form, setForm] = useState({
    cluster_id: 0,
    name: '',
    namespace: 'default',
    description: '',
    immutable: false,
    is_global: false,
    data_raw: 'APP_ENV=production\nLOG_LEVEL=info',
  });

  const createMut = useMutation({
    mutationFn: () => {
      const body = {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        description: form.description.trim(),
        immutable: form.immutable,
        is_global: form.is_global || form.cluster_id <= 0,
        data: parseKeyValues(form.data_raw),
      };
      return form.cluster_id > 0 ? createClusterConfigMap(form.cluster_id, body) : createGlobalConfigMap(body);
    },
  });

  return (
    <Layout>
      <PageHeader title="ConfigMaps" description="Create non-sensitive app configuration or manage by cluster" />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {clusters && (
        <div className="space-y-6">
          <Card>
            <CardHeader><CardTitle>Create ConfigMap</CardTitle></CardHeader>
            <CardContent>
              <form onSubmit={(e) => { e.preventDefault(); createMut.mutate(); }} className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Target cluster" value={form.cluster_id} onChange={(e) => setForm({ ...form, cluster_id: Number(e.target.value) })}>
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
                  <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Make available to all clusters</span>
                </label>
                <Button type="submit" loading={createMut.isPending}>Create ConfigMap</Button>
                {createMut.data && (
                  <Alert variant="success" title="ConfigMap created">
                    {createMut.data.cluster_id ? (
                      <Link className="text-indigo-600 hover:text-indigo-700" to={`/clusters/${createMut.data.cluster_id}?tab=configmaps`}>
                        Open {createMut.data.name} in cluster ConfigMaps
                      </Link>
                    ) : (
                      <span>{createMut.data.name} is available globally and can be selected during app deployment.</span>
                    )}
                  </Alert>
                )}
                {createMut.error && <ErrorMessage error={createMut.error} />}
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Manage Existing ConfigMaps</CardTitle></CardHeader>
            <CardContent>
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
    </Layout>
  );
}
