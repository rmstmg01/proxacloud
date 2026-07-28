import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ExternalLink, RefreshCw, Search } from 'lucide-react';
import {
  getManagedHTTPEndpoints,
  type ManagedHTTPEndpoint,
} from '../api/managedHttpEndpoints';
import { Layout, PageHeader } from '../components/Layout';
import { ErrorMessage } from '../components/ui/Alert';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';

function EndpointDetail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className={`mt-1 break-all text-sm text-slate-900 ${mono ? 'font-mono text-xs' : ''}`}>{value || '—'}</dd>
    </div>
  );
}

export function ManagedHTTPEndpointsPage() {
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const [selected, setSelected] = useState<ManagedHTTPEndpoint | null>(null);
  const { data = [], isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ['managed-http-endpoints'],
    queryFn: getManagedHTTPEndpoints,
    refetchInterval: 30_000,
  });

  const endpoints = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return data.filter((endpoint) => {
      if (status && endpoint.status !== status) return false;
      if (!needle) return true;
      return [
        endpoint.hostname,
        endpoint.provider,
        endpoint.externalId,
        endpoint.tunnelId,
        endpoint.httpRouteId,
        endpoint.targetUrl,
      ].some((value) => value.toLowerCase().includes(needle));
    });
  }, [data, search, status]);

  return (
    <Layout>
      <PageHeader
        title="Managed HTTP Endpoints"
        description="Platform-managed application hostnames kept separate from VPS-owned tunnel routes."
        actions={
          <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        }
      />

      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          ['Total endpoints', data.length],
          ['Active', data.filter((item) => item.status === 'active').length],
          ['Failed', data.filter((item) => item.status === 'failed').length],
          ['Providers', new Set(data.map((item) => item.provider)).size],
        ].map(([label, value]) => (
          <Card key={label}>
            <CardContent>
              <p className="text-sm text-slate-500">{label}</p>
              <p className="mt-1 text-2xl font-semibold text-slate-900">{value}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card className="mb-6">
        <CardContent className="grid gap-4 md:grid-cols-[minmax(0,1fr)_220px]">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-slate-400" />
            <Input
              aria-label="Search managed endpoints"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search hostname, provider, publication, tunnel or route"
              className="pl-9"
            />
          </div>
          <Select aria-label="Filter endpoint status" value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="">All statuses</option>
            <option value="active">Active</option>
            <option value="pending">Pending</option>
            <option value="failed">Failed</option>
            <option value="disabled">Disabled</option>
          </Select>
        </CardContent>
      </Card>

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}

      {!isLoading && !error && (
        <Card>
          <CardHeader>
            <CardTitle>Endpoint inventory</CardTitle>
          </CardHeader>
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-slate-200 text-sm">
              <thead className="bg-slate-50 text-left text-slate-600">
                <tr>
                  <th className="px-6 py-3 font-medium">Hostname</th>
                  <th className="px-6 py-3 font-medium">Provider</th>
                  <th className="px-6 py-3 font-medium">Tunnel / Route</th>
                  <th className="px-6 py-3 font-medium">DNS</th>
                  <th className="px-6 py-3 font-medium">Status</th>
                  <th className="px-6 py-3 font-medium">Updated</th>
                  <th className="px-6 py-3" />
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {endpoints.map((endpoint) => (
                  <tr key={endpoint.id} className="hover:bg-slate-50">
                    <td className="px-6 py-3">
                      <a
                        className="inline-flex items-center gap-1 font-medium text-indigo-600 hover:text-indigo-800"
                        href={`https://${endpoint.hostname}`}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {endpoint.hostname}
                        <ExternalLink className="h-3.5 w-3.5" />
                      </a>
                      <p className="mt-1 max-w-sm truncate font-mono text-xs text-slate-500">{endpoint.targetUrl}</p>
                    </td>
                    <td className="px-6 py-3 capitalize text-slate-700">{endpoint.provider}</td>
                    <td className="px-6 py-3">
                      <p className="max-w-xs truncate font-mono text-xs text-slate-700">{endpoint.tunnelId || '—'}</p>
                      <p className="mt-1 max-w-xs truncate font-mono text-xs text-slate-500">{endpoint.httpRouteId || 'No route'}</p>
                    </td>
                    <td className="px-6 py-3">
                      <p className="text-slate-700">{endpoint.dnsRecordType || '—'} {endpoint.dnsProxied ? 'proxied' : ''}</p>
                      <p className="mt-1 max-w-xs truncate font-mono text-xs text-slate-500">{endpoint.dnsRecordContent || endpoint.dnsRecordId || 'No record'}</p>
                    </td>
                    <td className="px-6 py-3">
                      <StatusBadge status={endpoint.status} />
                      {endpoint.lastError && <p className="mt-1 max-w-xs truncate text-xs text-red-600">{endpoint.lastError}</p>}
                    </td>
                    <td className="whitespace-nowrap px-6 py-3 text-slate-500">
                      {new Date(endpoint.updatedAt).toLocaleString()}
                    </td>
                    <td className="px-6 py-3 text-right">
                      <Button size="sm" variant="secondary" onClick={() => setSelected(endpoint)}>Details</Button>
                    </td>
                  </tr>
                ))}
                {!endpoints.length && (
                  <tr>
                    <td colSpan={7} className="px-6 py-10 text-center text-slate-500">
                      {data.length ? 'No managed endpoints match these filters.' : 'No managed HTTP endpoints have been recorded yet.'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <Modal
        open={selected !== null}
        onClose={() => setSelected(null)}
        title={selected?.hostname || 'Managed HTTP endpoint'}
        maxWidth="4xl"
      >
        {selected && (
          <div className="space-y-6">
            <div className="flex flex-wrap items-center gap-3">
              <StatusBadge status={selected.status} />
              <span className="text-sm capitalize text-slate-500">Provider: {selected.provider}</span>
              <a
                href={`https://${selected.hostname}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-sm font-medium text-indigo-600 hover:text-indigo-800"
              >
                Open endpoint <ExternalLink className="h-3.5 w-3.5" />
              </a>
            </div>
            {selected.lastError && (
              <div className="rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700">{selected.lastError}</div>
            )}
            <dl className="grid gap-5 sm:grid-cols-2">
              <EndpointDetail label="Publication / external ID" value={selected.externalId} mono />
              <EndpointDetail label="Target URL" value={selected.targetUrl} mono />
              <EndpointDetail label="ProxaCloud Tunnel ID" value={selected.tunnelId} mono />
              <EndpointDetail label="HTTP route ID" value={selected.httpRouteId} mono />
              <EndpointDetail label="DNS record ID" value={selected.dnsRecordId} mono />
              <EndpointDetail
                label="DNS record"
                value={[selected.dnsRecordType, selected.dnsRecordContent, selected.dnsProxied ? 'proxied' : 'DNS only'].filter(Boolean).join(' · ')}
                mono
              />
              <EndpointDetail label="Created" value={new Date(selected.createdAt).toLocaleString()} />
              <EndpointDetail label="Last updated" value={new Date(selected.updatedAt).toLocaleString()} />
            </dl>
          </div>
        )}
      </Modal>
    </Layout>
  );
}
