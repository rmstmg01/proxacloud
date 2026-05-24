import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Database, FileCode, RefreshCw } from 'lucide-react';
import {
  getDatabaseCluster,
  getDatabaseClusterK8sManifest,
  getDatabaseClusters,
  type DatabaseCluster,
} from '../api/databaseClusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { PageSpinner } from '../components/ui/Spinner';
import { ErrorMessage } from '../components/ui/Alert';

export function DatabaseClustersPage() {
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [manifestID, setManifestID] = useState<number | null>(null);
  const {
    data: clusters,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['database-clusters'],
    queryFn: getDatabaseClusters,
    refetchInterval: 30_000,
  });
  const selectedCluster = clusters?.find((cluster) => cluster.id === selectedID) ?? null;
  const detailQuery = useQuery({
    queryKey: ['database-cluster', selectedID],
    queryFn: () => getDatabaseCluster(selectedID ?? 0),
    enabled: selectedID !== null,
  });
  const manifestQuery = useQuery({
    queryKey: ['database-cluster-manifest', manifestID],
    queryFn: () => getDatabaseClusterK8sManifest(manifestID ?? 0),
    enabled: manifestID !== null,
  });

  return (
    <Layout>
      <PageHeader
        title="Database Clusters"
        description="Manage external MariaDB Galera clusters for Kubernetes workloads"
        actions={
          <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {detailQuery.error && <ErrorMessage error={detailQuery.error} />}
      {manifestQuery.error && <ErrorMessage error={manifestQuery.error} />}

      {clusters && (
        <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_420px] gap-6">
          <Card>
            {clusters.length === 0 ? (
              <div className="text-center py-16">
                <Database className="h-9 w-9 text-slate-300 mx-auto mb-3" />
                <p className="text-slate-500 text-sm">No database clusters have been provisioned yet.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Engine</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Endpoint</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                      <th className="px-6 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {clusters.map((cluster) => (
                      <tr key={cluster.id} className="hover:bg-slate-50 transition-colors">
                        <td className="px-6 py-4 font-medium text-slate-900">{cluster.name}</td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.engine} {cluster.mariadb_version}
                        </td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.haproxy_service_name}.{cluster.haproxy_namespace}
                        </td>
                        <td className="px-6 py-4">
                          <StatusBadge status={cluster.status} />
                        </td>
                        <td className="px-6 py-4 text-slate-500">
                          {new Date(cluster.created_at).toLocaleDateString()}
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <Button variant="secondary" size="sm" onClick={() => setSelectedID(cluster.id)}>
                              Details
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setManifestID(cluster.id)}>
                              <FileCode className="h-3.5 w-3.5" />
                              Manifest
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <div className="space-y-6">
            <ClusterDetail cluster={detailQuery.data ?? selectedCluster} loading={detailQuery.isFetching} />
            {manifestQuery.data && (
              <Card>
                <CardHeader>
                  <CardTitle>Kubernetes Manifest</CardTitle>
                </CardHeader>
                <CardContent>
                  <pre className="max-h-96 overflow-auto rounded-md bg-slate-950 p-4 text-xs text-slate-100">
                    {manifestQuery.data.manifest}
                  </pre>
                </CardContent>
              </Card>
            )}
          </div>
        </div>
      )}
    </Layout>
  );
}

function ClusterDetail({ cluster, loading }: { cluster: DatabaseCluster | null; loading: boolean }) {
  if (!cluster) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Details</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-slate-500">Select a database cluster to inspect placement and generated workspaces.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Details</CardTitle>
      </CardHeader>
      <CardContent>
        {loading && <p className="mb-3 text-xs text-slate-400">Refreshing details...</p>}
        <dl className="space-y-3 text-sm">
          <Field label="Database" value={cluster.database_name} />
          <Field label="App user" value={cluster.app_username} />
          <Field label="Terraform" value={cluster.terraform_work_dir || 'Not generated'} />
          <Field label="Ansible" value={cluster.ansible_work_dir || 'Not generated'} />
        </dl>
        <div className="mt-5 border-t border-slate-100 pt-4">
          <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">Nodes</p>
          <div className="space-y-2">
            {(cluster.nodes ?? []).map((node) => (
              <div key={node.id} className="rounded-md border border-slate-200 px-3 py-2">
                <div className="flex items-center justify-between gap-3">
                  <p className="font-medium text-slate-900">{node.vm_name}</p>
                  <StatusBadge status={node.status} />
                </div>
                <p className="mt-1 text-xs text-slate-500">
                  {node.ip_address} on {node.proxmox_node_name}
                </p>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-1 break-words text-slate-800">{value}</dd>
    </div>
  );
}
