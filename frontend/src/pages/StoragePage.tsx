import { useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Database, HardDrive, RefreshCw, Server } from 'lucide-react';
import { getStorageInventory } from '../api/storage';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Select } from '../components/ui/Input';
import { Badge } from '../components/ui/Badge';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { NodeStorage } from '../types';

function fmtBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const tb = bytes / 1_099_511_627_776;
  if (tb >= 1) return `${tb.toFixed(2)} TB`;
  const gb = bytes / 1_073_741_824;
  if (gb >= 1) return `${gb.toFixed(1)} GB`;
  return `${(bytes / 1_048_576).toFixed(0)} MB`;
}

function usageColor(percent: number): string {
  if (percent >= 90) return 'bg-red-500';
  if (percent >= 75) return 'bg-amber-400';
  return 'bg-emerald-500';
}

function StatCard({ icon, label, value, detail }: { icon: ReactNode; label: string; value: string; detail: string }) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="mb-3 flex items-center gap-3">
          {icon}
          <p className="text-sm font-semibold text-slate-700">{label}</p>
        </div>
        <p className="text-xl font-bold text-slate-900">{value}</p>
        <p className="mt-1 text-xs text-slate-500">{detail}</p>
      </CardContent>
    </Card>
  );
}

export function StoragePage() {
  const [nodeFilter, setNodeFilter] = useState('all');
  const [contentFilter, setContentFilter] = useState('all');
  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['storage-inventory'],
    queryFn: getStorageInventory,
  });

  const items = data?.items ?? [];
  const nodes = Array.from(new Set(items.map((item) => item.node_name))).sort();
  const contentTypes = Array.from(
    new Set(items.flatMap((item) => item.content.split(',').map((part) => part.trim()).filter(Boolean))),
  ).sort();

  const filtered = items.filter((item) => {
    const nodeOk = nodeFilter === 'all' || item.node_name === nodeFilter;
    const contentOk = contentFilter === 'all' || item.content.split(',').map((part) => part.trim()).includes(contentFilter);
    return nodeOk && contentOk;
  });

  const summary = useMemo(() => {
    const total = filtered.reduce((sum, item) => sum + item.total_bytes, 0);
    const used = filtered.reduce((sum, item) => sum + item.used_bytes, 0);
    const available = filtered.reduce((sum, item) => sum + item.available_bytes, 0);
    return { total, used, available };
  }, [filtered]);

  return (
    <Layout>
      <PageHeader
        title="Storage"
        description="Inspect Proxmox storage capacity, content types, and node placement."
        actions={
          <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      {isLoading ? (
        <PageSpinner />
      ) : error ? (
        <ErrorMessage error={error} />
      ) : (
        <div className="space-y-6">
          {data?.errors?.length ? (
            <Alert variant="warning" title="Some nodes could not be queried">
              {data.errors.map((item) => `${item.node_name}: ${item.error}`).join(' | ')}
            </Alert>
          ) : null}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <StatCard icon={<Database className="h-5 w-5 text-indigo-500" />} label="Total capacity" value={fmtBytes(summary.total)} detail={`${filtered.length} storage record(s)`} />
            <StatCard icon={<HardDrive className="h-5 w-5 text-emerald-500" />} label="Available" value={fmtBytes(summary.available)} detail={`${fmtBytes(summary.used)} used`} />
            <StatCard icon={<Server className="h-5 w-5 text-sky-500" />} label="Nodes" value={String(nodes.length)} detail={`${contentTypes.length} content type(s)`} />
          </div>

          <Card>
            <CardContent>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                <Select label="Node" value={nodeFilter} onChange={(e) => setNodeFilter(e.target.value)}>
                  <option value="all">All nodes</option>
                  {nodes.map((node) => (
                    <option key={node} value={node}>{node}</option>
                  ))}
                </Select>
                <Select label="Content" value={contentFilter} onChange={(e) => setContentFilter(e.target.value)}>
                  <option value="all">All content</option>
                  {contentTypes.map((content) => (
                    <option key={content} value={content}>{content}</option>
                  ))}
                </Select>
              </div>
            </CardContent>
          </Card>

          <Card>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Storage</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Node</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Type</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Content</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600 min-w-[190px]">Usage</th>
                    <th className="px-6 py-3 text-left font-medium text-slate-600">Flags</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {filtered.map((item: NodeStorage) => (
                    <tr key={`${item.credential_id}-${item.node_name}-${item.storage_id}`} className="hover:bg-slate-50">
                      <td className="px-6 py-3 font-medium text-slate-900">{item.storage_id}</td>
                      <td className="px-6 py-3 text-slate-600">{item.node_name}</td>
                      <td className="px-6 py-3 text-slate-600">{item.type}</td>
                      <td className="px-6 py-3 text-slate-600">{item.content || '-'}</td>
                      <td className="px-6 py-3">
                        <div className="mb-1 flex items-center justify-between gap-3 text-xs text-slate-600">
                          <span>{fmtBytes(item.used_bytes)} / {fmtBytes(item.total_bytes)}</span>
                          <span>{item.usage_percent}%</span>
                        </div>
                        <div className="h-1.5 overflow-hidden rounded-full bg-slate-200">
                          <div className={`h-full rounded-full ${usageColor(item.usage_percent)}`} style={{ width: `${Math.min(100, item.usage_percent)}%` }} />
                        </div>
                      </td>
                      <td className="px-6 py-3">
                        <div className="flex flex-wrap gap-2">
                          <Badge variant={item.enabled ? 'success' : 'default'}>{item.enabled ? 'enabled' : 'disabled'}</Badge>
                          <Badge variant={item.active ? 'info' : 'default'}>{item.active ? 'active' : 'inactive'}</Badge>
                          {item.shared && <Badge variant="warning">shared</Badge>}
                        </div>
                      </td>
                    </tr>
                  ))}
                  {filtered.length === 0 && (
                    <tr>
                      <td colSpan={6} className="px-6 py-8 text-center text-sm text-slate-500">No storage records found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>
        </div>
      )}
    </Layout>
  );
}
