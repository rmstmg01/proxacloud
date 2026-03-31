import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { Server, Network, Cpu, Plus, AlertTriangle, CheckCircle2 } from 'lucide-react';
import { getClusters } from '../api/clusters';
import { getNetworks } from '../api/networks';
import { getNodes } from '../api/proxmox';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { PageSpinner } from '../components/ui/Spinner';

function StatCard({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: number | string;
  icon: React.ElementType;
  color: string;
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-4">
        <div className={`w-12 h-12 rounded-xl flex items-center justify-center ${color}`}>
          <Icon className="h-6 w-6 text-white" />
        </div>
        <div>
          <p className="text-2xl font-bold text-slate-900">{value}</p>
          <p className="text-sm text-slate-500">{label}</p>
        </div>
      </CardContent>
    </Card>
  );
}

export function DashboardPage() {
  const { data: clusters, isLoading: loadingClusters } = useQuery({
    queryKey: ['clusters'],
    queryFn: getClusters,
  });
  const { data: networks, isLoading: loadingNetworks } = useQuery({
    queryKey: ['networks'],
    queryFn: getNetworks,
  });
  const { data: nodes, isLoading: loadingNodes } = useQuery({
    queryKey: ['proxmox-nodes'],
    queryFn: getNodes,
  });

  const failedClusters = clusters?.filter((c) => c.status.toLowerCase() === 'failed') ?? [];
  const runningClusters =
    clusters?.filter((c) => ['running', 'active', 'completed', 'bootstrapped'].includes(c.status.toLowerCase())) ?? [];

  if (loadingClusters || loadingNetworks || loadingNodes) return <Layout><PageSpinner /></Layout>;

  return (
    <Layout>
      <PageHeader
        title="Dashboard"
        description="Overview of your Kubernetes clusters"
        actions={
          <Link
            to="/clusters/new"
            className="inline-flex items-center gap-2 bg-indigo-600 text-white px-4 py-2 rounded-md text-sm font-medium hover:bg-indigo-700 transition-colors"
          >
            <Plus className="h-4 w-4" />
            New Cluster
          </Link>
        }
      />

      {/* Stats */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <StatCard label="Total Clusters" value={clusters?.length ?? 0} icon={Server} color="bg-indigo-500" />
        <StatCard label="Running" value={runningClusters.length} icon={CheckCircle2} color="bg-emerald-500" />
        <StatCard label="Failed" value={failedClusters.length} icon={AlertTriangle} color="bg-red-500" />
        <StatCard label="Networks" value={networks?.length ?? 0} icon={Network} color="bg-amber-500" />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Clusters table */}
        <Card>
          <div className="px-6 py-4 border-b border-slate-200 flex items-center justify-between">
            <h3 className="font-semibold text-slate-900">Recent Clusters</h3>
            <Link to="/clusters" className="text-sm text-indigo-600 hover:text-indigo-700 font-medium">
              View all
            </Link>
          </div>
          {clusters && clusters.length > 0 ? (
            <div className="divide-y divide-slate-100">
              {clusters.slice(0, 5).map((c) => (
                <Link
                  key={c.id}
                  to={`/clusters/${c.id}`}
                  className="flex items-center justify-between px-6 py-3 hover:bg-slate-50 transition-colors"
                >
                  <div>
                    <p className="text-sm font-medium text-slate-900">{c.name}</p>
                    <p className="text-xs text-slate-500">
                      {c.topology_type} · {c.kubernetes_version}
                    </p>
                  </div>
                  <StatusBadge status={c.status} />
                </Link>
              ))}
            </div>
          ) : (
            <CardContent>
              <p className="text-sm text-slate-400 text-center py-4">No clusters yet.</p>
            </CardContent>
          )}
        </Card>

        {/* Proxmox nodes */}
        <Card>
          <div className="px-6 py-4 border-b border-slate-200 flex items-center justify-between">
            <h3 className="font-semibold text-slate-900">Proxmox Nodes</h3>
            <Link to="/proxmox" className="text-sm text-indigo-600 hover:text-indigo-700 font-medium">
              Manage
            </Link>
          </div>
          {nodes && nodes.length > 0 ? (
            <div className="divide-y divide-slate-100">
              {nodes.slice(0, 5).map((n) => (
                <div key={n.id} className="flex items-center justify-between px-6 py-3">
                  <div className="flex items-center gap-3">
                    <Cpu className="h-4 w-4 text-slate-400" />
                    <div>
                      <p className="text-sm font-medium text-slate-900">{n.node_name}</p>
                      <p className="text-xs text-slate-500">
                        {n.cpu_count} CPU · {Math.round(n.memory_total_bytes / 1073741824)} GB
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <StatusBadge status={n.status} />
                    {!n.enabled && (
                      <span className="text-xs text-slate-400 bg-slate-100 px-2 py-0.5 rounded-full">
                        disabled
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <CardContent>
              <p className="text-sm text-slate-400 text-center py-4">
                No nodes discovered.{' '}
                <Link to="/proxmox" className="text-indigo-600 hover:underline">
                  Add credentials
                </Link>
              </p>
            </CardContent>
          )}
        </Card>
      </div>
    </Layout>
  );
}
