import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, KeyRound } from 'lucide-react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { createClusterSecret, getClusters } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import type { ClusterSecretProvider } from '../types';

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
  const { data: clusters, isLoading, error } = useQuery({ queryKey: ['clusters'], queryFn: getClusters });
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

  const createMut = useMutation({
    mutationFn: async () => {
      const provider_config = JSON.parse(form.provider_config_raw || '{}');
      return createClusterSecret(form.cluster_id, {
        name: form.name.trim(),
        namespace: form.namespace.trim() || 'default',
        provider: form.provider,
        type: form.type.trim() || 'Opaque',
        description: form.description.trim(),
        external_ref: form.external_ref.trim(),
        provider_config,
        immutable: form.immutable,
        is_global: form.is_global,
        data: parseKeyValues(form.data_raw),
      });
    },
  });

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
      {clusters && (
        <div className="space-y-6">
          <Card>
            <CardHeader><CardTitle>Create Secret</CardTitle></CardHeader>
            <CardContent>
              <form onSubmit={(e) => { e.preventDefault(); createMut.mutate(); }} className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 max-w-4xl">
                  <Select label="Target cluster" value={form.cluster_id} onChange={(e) => setForm({ ...form, cluster_id: Number(e.target.value) })} required>
                    <option value={0}>-- Select cluster --</option>
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
                  <input type="checkbox" checked={form.is_global} onChange={(e) => setForm({ ...form, is_global: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                  <span className="text-sm text-slate-700">Make available to all clusters</span>
                </label>
                <Button type="submit" loading={createMut.isPending} disabled={form.cluster_id <= 0}>Create Secret</Button>
                {createMut.data && (
                  <Alert variant="success" title="Secret created">
                    <Link className="text-indigo-600 hover:text-indigo-700" to={`/clusters/${createMut.data.cluster_id}?tab=secrets`}>
                      Open {createMut.data.name} in cluster secrets
                    </Link>
                  </Alert>
                )}
                {createMut.error && <ErrorMessage error={createMut.error} />}
              </form>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Manage Existing Secrets</CardTitle></CardHeader>
            <CardContent>
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
    </Layout>
  );
}
