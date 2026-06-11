import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Cpu,
  Database,
  HardDrive,
  MemoryStick,
  Network,
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
import type { ClusterSummary } from '../types';
import type { DatabaseCluster } from '../api/databaseClusters';

type Tone = 'blue' | 'green' | 'red' | 'amber' | 'slate' | 'cyan' | 'violet';

type AttentionItem = {
  key: string;
  name: string;
  type: string;
  status: string;
  detail: string;
  to: string;
};

const toneClasses: Record<Tone, string> = {
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
  return value.includes('failed') || value === 'error' || value === 'unhealthy' || value === 'offline';
}

function isReadyStatus(status?: string) {
  const value = normalizeStatus(status);
  return ['ready', 'running', 'healthy', 'provisioned', 'active', 'online'].includes(value);
}

function formatMemory(gb: number) {
  if (gb >= 1024) return `${Math.round(gb / 102.4) / 10} TB`;
  return `${gb} GB`;
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

function OverviewCard({
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
  tone: Tone;
}) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-slate-500">{label}</p>
          <p className="mt-2 text-3xl font-semibold text-slate-950">{value}</p>
          <p className="mt-1 text-xs text-slate-500">{detail}</p>
        </div>
        <div className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-md ring-1 ${toneClasses[tone]}`}>
          <Icon className="h-5 w-5" />
        </div>
      </CardContent>
    </Card>
  );
}

function ResourceDashboardCard({
  title,
  description,
  to,
  icon: Icon,
  tone,
  primary,
  secondary,
}: {
  title: string;
  description: string;
  to: string;
  icon: React.ElementType;
  tone: Tone;
  primary: string;
  secondary: string;
}) {
  return (
    <Link to={to} className="block">
      <Card className="h-full transition-colors hover:border-blue-200 hover:bg-slate-50">
        <CardContent className="flex h-full flex-col gap-4">
          <div className="flex items-start justify-between gap-4">
            <div className={`flex h-10 w-10 items-center justify-center rounded-md ring-1 ${toneClasses[tone]}`}>
              <Icon className="h-5 w-5" />
            </div>
            <ArrowRight className="h-4 w-4 text-slate-400" />
          </div>
          <div>
            <p className="text-base font-semibold text-slate-950">{title}</p>
            <p className="mt-1 text-sm text-slate-500">{description}</p>
          </div>
          <div className="mt-auto grid grid-cols-2 gap-3">
            <div>
              <p className="text-lg font-semibold text-slate-950">{primary}</p>
              <p className="text-xs text-slate-500">total</p>
            </div>
            <div>
              <p className="text-lg font-semibold text-slate-950">{secondary}</p>
              <p className="text-xs text-slate-500">healthy</p>
            </div>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}

function CapacityItem({ icon: Icon, label, value, detail }: {
  icon: React.ElementType;
  label: string;
  value: string;
  detail: string;
}) {
  return (
    <div className="flex items-center gap-3 rounded-md border border-slate-200 bg-white px-4 py-3">
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-slate-100 text-slate-700">
        <Icon className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <p className="text-sm font-medium text-slate-950">{label}</p>
        <p className="mt-1 text-sm text-slate-500">{value} / {detail}</p>
      </div>
    </div>
  );
}

function AttentionList({ items }: { items: AttentionItem[] }) {
  if (items.length === 0) {
    return (
      <CardContent>
        <div className="flex items-center gap-3 rounded-md bg-emerald-50 px-4 py-4 text-emerald-800 ring-1 ring-emerald-100">
          <CheckCircle2 className="h-5 w-5 shrink-0" />
          <div>
            <p className="text-sm font-medium">No critical resource issues</p>
            <p className="mt-1 text-xs text-emerald-700">All tracked resources are clear of failed or unavailable states.</p>
          </div>
        </div>
      </CardContent>
    );
  }

  return (
    <div className="divide-y divide-slate-100">
      {items.slice(0, 6).map((item) => (
        <Link key={item.key} to={item.to} className="flex items-center justify-between gap-4 px-6 py-3 hover:bg-slate-50">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-slate-950">{item.name}</p>
            <p className="mt-1 text-xs text-slate-500">{item.type} / {item.detail}</p>
          </div>
          <StatusBadge status={item.status} />
        </Link>
      ))}
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
  const hostCPU = nodeList.reduce((sum, node) => sum + node.cpu_count, 0);
  const hostMemory = nodeList.reduce((sum, node) => sum + node.memory_total_bytes, 0);

  const attentionItems: AttentionItem[] = [
    ...failedClusters.map((cluster) => ({
      key: `cluster-${cluster.id}`,
      name: cluster.name,
      type: 'Kubernetes cluster',
      status: cluster.health_status || cluster.status,
      detail: `${cluster.topology_type} / ${cluster.kubernetes_version}`,
      to: `/clusters/${cluster.id}`,
    })),
    ...failedVPS.map((vps) => ({
      key: `vps-${vps.id}`,
      name: vps.name,
      type: 'VPS',
      status: vps.status,
      detail: vps.proxmox_node_name || 'No Proxmox node',
      to: `/vps/${vps.id}`,
    })),
    ...failedDatabaseClusters.map((cluster) => ({
      key: `database-${cluster.id}`,
      name: cluster.name,
      type: 'Database cluster',
      status: cluster.status,
      detail: `${cluster.engine} ${cluster.mariadb_version}`,
      to: '/database-clusters',
    })),
    ...unavailableNodes.map((node) => ({
      key: `node-${node.id}`,
      name: node.node_name,
      type: 'Proxmox node',
      status: node.enabled ? node.status : 'disabled',
      detail: `${node.cpu_count} CPU / ${formatBytes(node.memory_total_bytes)}`,
      to: '/proxmox',
    })),
  ];
  const alertCount = attentionItems.length;
  const totalResources = clusterList.length + vpsList.length + databaseClusterList.length + nodeList.length;

  return (
    <Layout>
      <PageHeader
        title="Dashboard"
        description="A focused cloud overview for health, resource dashboards, and infrastructure capacity."
      />

      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <OverviewCard
          label="Overall Health"
          value={alertCount === 0 ? 'Healthy' : 'Action Needed'}
          detail={`${totalResources} tracked resources`}
          icon={alertCount === 0 ? CheckCircle2 : AlertTriangle}
          tone={alertCount === 0 ? 'green' : 'red'}
        />
        <OverviewCard
          label="Open Alerts"
          value={alertCount}
          detail="Failed or unavailable resources"
          icon={AlertTriangle}
          tone={alertCount > 0 ? 'red' : 'slate'}
        />
        <OverviewCard
          label="Compute Resources"
          value={clusterList.length + vpsList.length + databaseClusterList.length}
          detail={`${totalClusterNodes(clusterList) + vpsList.length + totalDatabaseNodes(databaseClusterList)} workload nodes`}
          icon={ServerCog}
          tone="blue"
        />
        <OverviewCard
          label="Infrastructure"
          value={nodeList.length}
          detail={`${networkList.length} networks / ${unavailableNodes.length} node issues`}
          icon={Network}
          tone="slate"
        />
      </div>

      <div className="mb-6">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-base font-semibold text-slate-950">Resource Dashboards</h2>
          <p className="text-sm text-slate-500">Open a resource area for details and creation actions.</p>
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
          <ResourceDashboardCard
            title="Kubernetes"
            description="Cluster health, versions, topology, and lifecycle operations."
            to="/clusters"
            icon={Server}
            tone="blue"
            primary={String(clusterList.length)}
            secondary={String(readyClusters.length)}
          />
          <ResourceDashboardCard
            title="VPS"
            description="Virtual servers, power state, IPs, and allocated resources."
            to="/vps"
            icon={HardDrive}
            tone="cyan"
            primary={String(vpsList.length)}
            secondary={String(runningVPS.length)}
          />
          <ResourceDashboardCard
            title="Databases"
            description="MariaDB Galera clusters, bootstrap state, and database nodes."
            to="/database-clusters"
            icon={Database}
            tone="violet"
            primary={String(databaseClusterList.length)}
            secondary={String(readyDatabaseClusters.length)}
          />
          <ResourceDashboardCard
            title="Proxmox Nodes"
            description="Host capacity, enabled nodes, and infrastructure inventory."
            to="/proxmox"
            icon={Cpu}
            tone="slate"
            primary={String(nodeList.length)}
            secondary={String(nodeList.length - unavailableNodes.length)}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-5">
        <Card className="xl:col-span-3">
          <CardHeader>
            <CardTitle>Needs Attention</CardTitle>
          </CardHeader>
          <AttentionList items={attentionItems} />
        </Card>

        <Card className="xl:col-span-2">
          <CardHeader>
            <CardTitle>Capacity Snapshot</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-3">
            <CapacityItem
              icon={Cpu}
              label="Allocated CPU"
              value={`${vpsCPU + databaseCPU} vCPU`}
              detail={`${hostCPU} host CPU`}
            />
            <CapacityItem
              icon={MemoryStick}
              label="Allocated Memory"
              value={formatMemory(vpsMemoryGB + databaseMemoryGB)}
              detail={`${formatBytes(hostMemory)} host memory`}
            />
            <CapacityItem
              icon={HardDrive}
              label="Allocated Disk"
              value={`${vpsDiskGB + databaseDiskGB} GB`}
              detail={`${vpsDiskGB} VPS / ${databaseDiskGB} database`}
            />
            <CapacityItem
              icon={Network}
              label="Networks"
              value={String(networkList.length)}
              detail="profiles available"
            />
          </CardContent>
        </Card>
      </div>
    </Layout>
  );
}
