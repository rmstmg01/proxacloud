import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CheckCircle2,
  Cpu,
  Database,
  HardDrive,
  MemoryStick,
  Plus,
  Server,
  ServerCog,
} from 'lucide-react';
import { getClusters } from '../api/clusters';
import { getDatabaseClusters } from '../api/databaseClusters';
import { getNetworks } from '../api/networks';
import { getNodes } from '../api/proxmox';
import { getVPSInstances } from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { StatusBadge } from '../components/ui/Badge';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { PageSpinner } from '../components/ui/Spinner';
import type { ClusterSummary, ProxmoxNode, VPSInstance } from '../types';
import type { DatabaseCluster } from '../api/databaseClusters';

type SummaryTone = 'blue' | 'green' | 'red' | 'amber' | 'slate' | 'cyan' | 'violet';

const toneClasses: Record<SummaryTone, string> = {
  blue: 'bg-blue-50 text-blue-700 ring-blue-100',
  green: 'bg-emerald-50 text-emerald-700 ring-emerald-100',
  red: 'bg-red-50 text-red-700 ring-red-100',
  amber: 'bg-amber-50 text-amber-700 ring-amber-100',
  slate: 'bg-slate-100 text-slate-700 ring-slate-200',
  cyan: 'bg-cyan-50 text-cyan-700 ring-cyan-100',
  violet: 'bg-violet-50 text-violet-700 ring-violet-100',
};

function normalizeStatus(status?: string) {
  return (status || '').trim().toLowerCase();
}

function isFailedStatus(status?: string) {
  const value = normalizeStatus(status);
  return value.includes('failed') || value === 'error' || value === 'unhealthy';
}

function isReadyStatus(status?: string) {
  const value = normalizeStatus(status);
  return ['ready', 'running', 'healthy', 'provisioned', 'active'].includes(value);
}

function formatMemory(mb: number) {
  if (mb >= 1024) return `${Math.round(mb / 102.4) / 10} TB`;
  return `${mb} GB`;
}

function formatBytes(bytes: number) {
  if (bytes >= 1099511627776) return `${Math.round(bytes / 109951162777.6) / 10} TB`;
  if (bytes >= 1073741824) return `${Math.round(bytes / 107374182.4) / 10} GB`;
  return `${Math.round(bytes / 104857.6) / 10} MB`;
}

function totalClusterNodes(clusters: ClusterSummary[]) {
  return clusters.reduce((sum, cluster) => sum + cluster.control_plane_count + cluster.worker_count, 0);
}

function totalDatabaseNodes(databaseClusters: DatabaseCluster[]) {
  return databaseClusters.reduce((sum, cluster) => sum + (cluster.nodes?.length ?? 0), 0);
}

function StatusPill({ label, value, tone }: { label: string; value: number; tone: SummaryTone }) {
  return (
    <div className={`rounded-md px-3 py-2 ring-1 ${toneClasses[tone]}`}>
      <p className="text-lg font-semibold leading-5">{value}</p>
      <p className="mt-1 text-xs font-medium">{label}</p>
    </div>
  );
}

function SummaryCard({
  label,
  value,
  detail,
  icon: Icon,
  tone,
}: {
  label: string;
  value: number | string;
  detail: string;
  icon: React.ElementType;
  tone: SummaryTone;
}) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-slate-500">{label}</p>
          <p className="mt-2 text-3xl font-semibold tracking-normal text-slate-950">{value}</p>
          <p className="mt-1 text-xs text-slate-500">{detail}</p>
        </div>
        <div className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-md ring-1 ${toneClasses[tone]}`}>
          <Icon className="h-5 w-5" />
        </div>
      </CardContent>
    </Card>
  );
}

function SectionHeader({ title, to, linkLabel }: { title: string; to: string; linkLabel: string }) {
  return (
    <CardHeader className="flex items-center justify-between gap-3">
      <CardTitle>{title}</CardTitle>
      <Link to={to} className="text-sm font-medium text-blue-700 hover:text-blue-800">
        {linkLabel}
      </Link>
    </CardHeader>
  );
}

function EmptyState({ message, to, action }: { message: string; to: string; action: string }) {
  return (
    <CardContent>
      <div className="rounded-md border border-dashed border-slate-300 bg-slate-50 px-4 py-6 text-center">
        <p className="text-sm text-slate-500">{message}</p>
        <Link to={to} className="mt-3 inline-flex text-sm font-medium text-blue-700 hover:text-blue-800">
          {action}
        </Link>
      </div>
    </CardContent>
  );
}

function ResourceList({ children }: { children: React.ReactNode }) {
  return <div className="divide-y divide-slate-100">{children}</div>;
}

function ClusterRow({ cluster }: { cluster: ClusterSummary }) {
  return (
    <Link to={`/clusters/${cluster.id}`} className="flex items-center justify-between gap-4 px-6 py-3 hover:bg-slate-50">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-slate-950">{cluster.name}</p>
        <p className="mt-1 text-xs text-slate-500">
          {cluster.topology_type} / {cluster.control_plane_count + cluster.worker_count} nodes / {cluster.kubernetes_version}
        </p>
      </div>
      <StatusBadge status={cluster.health_status || cluster.status} />
    </Link>
  );
}

function VPSRow({ vps }: { vps: VPSInstance }) {
  return (
    <Link to={`/vps/${vps.id}`} className="flex items-center justify-between gap-4 px-6 py-3 hover:bg-slate-50">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-slate-950">{vps.name}</p>
        <p className="mt-1 text-xs text-slate-500">
          {vps.ip_address || vps.ip_mode.toUpperCase()} / {vps.cpu} vCPU / {Math.round(vps.memory_mb / 1024)} GB RAM
        </p>
      </div>
      <StatusBadge status={vps.status} />
    </Link>
  );
}

function DatabaseClusterRow({ cluster }: { cluster: DatabaseCluster }) {
  return (
    <Link to="/database-clusters" className="flex items-center justify-between gap-4 px-6 py-3 hover:bg-slate-50">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-slate-950">{cluster.name}</p>
        <p className="mt-1 text-xs text-slate-500">
          {cluster.engine} {cluster.mariadb_version} / {cluster.nodes?.length ?? 0} nodes / {cluster.topology_type}
        </p>
      </div>
      <StatusBadge status={cluster.status} />
    </Link>
  );
}

function ProxmoxNodeRow({ node }: { node: ProxmoxNode }) {
  return (
    <div className="flex items-center justify-between gap-4 px-6 py-3">
      <div className="flex min-w-0 items-center gap-3">
        <Cpu className="h-4 w-4 shrink-0 text-slate-400" />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-slate-950">{node.node_name}</p>
          <p className="mt-1 text-xs text-slate-500">
            {node.cpu_count} CPU / {formatBytes(node.memory_total_bytes)}
          </p>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <StatusBadge status={node.status} />
        {!node.enabled && <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-500">disabled</span>}
      </div>
    </div>
  );
}

export function DashboardPage() {
  const { data: clusters, isLoading: loadingClusters } = useQuery({
    queryKey: ['clusters'],
    queryFn: getClusters,
  });
  const { data: vpsInstances, isLoading: loadingVPS } = useQuery({
    queryKey: ['vps'],
    queryFn: getVPSInstances,
  });
  const { data: databaseClusters, isLoading: loadingDatabaseClusters } = useQuery({
    queryKey: ['database-clusters'],
    queryFn: getDatabaseClusters,
  });
  const { data: networks, isLoading: loadingNetworks } = useQuery({
    queryKey: ['networks'],
    queryFn: getNetworks,
  });
  const { data: nodes, isLoading: loadingNodes } = useQuery({
    queryKey: ['proxmox-nodes'],
    queryFn: getNodes,
  });

  if (loadingClusters || loadingVPS || loadingDatabaseClusters || loadingNetworks || loadingNodes) {
    return (
      <Layout>
        <PageSpinner />
      </Layout>
    );
  }

  const clusterList = clusters ?? [];
  const vpsList = vpsInstances ?? [];
  const databaseClusterList = databaseClusters ?? [];
  const networkList = networks ?? [];
  const nodeList = nodes ?? [];

  const readyClusters = clusterList.filter((cluster) => isReadyStatus(cluster.health_status || cluster.status));
  const failedClusters = clusterList.filter((cluster) => isFailedStatus(cluster.health_status || cluster.status));
  const runningVPS = vpsList.filter((vps) => isReadyStatus(vps.status));
  const failedVPS = vpsList.filter((vps) => isFailedStatus(vps.status));
  const readyDatabaseClusters = databaseClusterList.filter((cluster) => isReadyStatus(cluster.status));
  const failedDatabaseClusters = databaseClusterList.filter((cluster) => isFailedStatus(cluster.status));
  const unavailableNodes = nodeList.filter((node) => !node.enabled || isFailedStatus(node.status));

  const vpsCPU = vpsList.reduce((sum, vps) => sum + vps.cpu, 0);
  const vpsMemoryGB = Math.round(vpsList.reduce((sum, vps) => sum + vps.memory_mb, 0) / 1024);
  const vpsDiskGB = vpsList.reduce((sum, vps) => sum + vps.disk_gb, 0);
  const databaseCPU = databaseClusterList.reduce(
    (sum, cluster) => sum + (cluster.nodes?.reduce((nodeSum, node) => nodeSum + node.cpu, 0) ?? 0),
    0,
  );
  const databaseMemoryGB = Math.round(
    databaseClusterList.reduce(
      (sum, cluster) => sum + (cluster.nodes?.reduce((nodeSum, node) => nodeSum + node.memory_mb, 0) ?? 0),
      0,
    ) / 1024,
  );
  const databaseDiskGB = databaseClusterList.reduce(
    (sum, cluster) => sum + (cluster.nodes?.reduce((nodeSum, node) => nodeSum + node.disk_gb, 0) ?? 0),
    0,
  );

  const alertCount = failedClusters.length + failedVPS.length + failedDatabaseClusters.length + unavailableNodes.length;

  return (
    <Layout>
      <PageHeader
        title="Dashboard"
        description="Cloud infrastructure overview across Kubernetes, VPS, database clusters, networks, and Proxmox capacity."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/clusters/new"
              className="inline-flex items-center gap-2 rounded-md bg-blue-700 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-800"
            >
              <Plus className="h-4 w-4" />
              Cluster
            </Link>
            <Link
              to="/vps/new"
              className="inline-flex items-center gap-2 rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50"
            >
              <Plus className="h-4 w-4" />
              VPS
            </Link>
            <Link
              to="/database-clusters"
              className="inline-flex items-center gap-2 rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50"
            >
              <Plus className="h-4 w-4" />
              Database
            </Link>
          </div>
        }
      />

      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard
          label="Kubernetes Clusters"
          value={clusterList.length}
          detail={`${totalClusterNodes(clusterList)} managed nodes`}
          icon={Server}
          tone="blue"
        />
        <SummaryCard
          label="VPS Instances"
          value={vpsList.length}
          detail={`${runningVPS.length} running / ${failedVPS.length} failed`}
          icon={HardDrive}
          tone="cyan"
        />
        <SummaryCard
          label="Database Clusters"
          value={databaseClusterList.length}
          detail={`${totalDatabaseNodes(databaseClusterList)} database nodes`}
          icon={Database}
          tone="violet"
        />
        <SummaryCard
          label="Open Alerts"
          value={alertCount}
          detail="Failed resources or unavailable nodes"
          icon={alertCount > 0 ? AlertTriangle : CheckCircle2}
          tone={alertCount > 0 ? 'red' : 'green'}
        />
      </div>

      <div className="mb-6 grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card>
          <CardContent className="flex items-center gap-4">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-100 text-slate-700 ring-1 ring-slate-200">
              <Cpu className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-medium text-slate-500">Allocated Compute</p>
              <p className="mt-1 text-2xl font-semibold text-slate-950">{vpsCPU + databaseCPU} vCPU</p>
              <p className="text-xs text-slate-500">{vpsCPU} VPS / {databaseCPU} database</p>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-4">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-100 text-slate-700 ring-1 ring-slate-200">
              <MemoryStick className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-medium text-slate-500">Allocated Memory</p>
              <p className="mt-1 text-2xl font-semibold text-slate-950">{formatMemory(vpsMemoryGB + databaseMemoryGB)}</p>
              <p className="text-xs text-slate-500">{formatMemory(vpsMemoryGB)} VPS / {formatMemory(databaseMemoryGB)} database</p>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-4">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-100 text-slate-700 ring-1 ring-slate-200">
              <ServerCog className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-medium text-slate-500">Cloud Foundation</p>
              <p className="mt-1 text-2xl font-semibold text-slate-950">{nodeList.length} Proxmox nodes</p>
              <p className="text-xs text-slate-500">{networkList.length} networks / {vpsDiskGB + databaseDiskGB} GB allocated disk</p>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4 lg:grid-cols-7">
        <StatusPill label="K8s ready" value={readyClusters.length} tone="green" />
        <StatusPill label="K8s failed" value={failedClusters.length} tone={failedClusters.length ? 'red' : 'slate'} />
        <StatusPill label="VPS running" value={runningVPS.length} tone="cyan" />
        <StatusPill label="VPS failed" value={failedVPS.length} tone={failedVPS.length ? 'red' : 'slate'} />
        <StatusPill label="DB ready" value={readyDatabaseClusters.length} tone="violet" />
        <StatusPill label="DB failed" value={failedDatabaseClusters.length} tone={failedDatabaseClusters.length ? 'red' : 'slate'} />
        <StatusPill label="Node issues" value={unavailableNodes.length} tone={unavailableNodes.length ? 'amber' : 'slate'} />
      </div>

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <Card>
          <SectionHeader title="Recent Kubernetes Clusters" to="/clusters" linkLabel="View all" />
          {clusterList.length > 0 ? (
            <ResourceList>
              {clusterList.slice(0, 5).map((cluster) => (
                <ClusterRow key={cluster.id} cluster={cluster} />
              ))}
            </ResourceList>
          ) : (
            <EmptyState message="No Kubernetes clusters yet." to="/clusters/new" action="Create cluster" />
          )}
        </Card>

        <Card>
          <SectionHeader title="Recent VPS Instances" to="/vps" linkLabel="View all" />
          {vpsList.length > 0 ? (
            <ResourceList>
              {vpsList.slice(0, 5).map((vps) => (
                <VPSRow key={vps.id} vps={vps} />
              ))}
            </ResourceList>
          ) : (
            <EmptyState message="No VPS instances yet." to="/vps/new" action="Create VPS" />
          )}
        </Card>

        <Card>
          <SectionHeader title="Recent Database Clusters" to="/database-clusters" linkLabel="View all" />
          {databaseClusterList.length > 0 ? (
            <ResourceList>
              {databaseClusterList.slice(0, 5).map((cluster) => (
                <DatabaseClusterRow key={cluster.id} cluster={cluster} />
              ))}
            </ResourceList>
          ) : (
            <EmptyState message="No database clusters yet." to="/database-clusters" action="Create database cluster" />
          )}
        </Card>

        <Card>
          <SectionHeader title="Proxmox Nodes" to="/proxmox" linkLabel="Manage" />
          {nodeList.length > 0 ? (
            <ResourceList>
              {nodeList.slice(0, 5).map((node) => (
                <ProxmoxNodeRow key={node.id} node={node} />
              ))}
            </ResourceList>
          ) : (
            <EmptyState message="No Proxmox nodes discovered." to="/proxmox" action="Add credentials" />
          )}
        </Card>
      </div>
    </Layout>
  );
}
