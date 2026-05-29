import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { Plus, ChevronRight, Play, Power, RotateCcw, RefreshCw, Trash2 } from 'lucide-react';
import { deleteCluster, getClusters, redeployCluster, restartCluster, shutdownCluster, startCluster } from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import type { ClusterSummary } from '../types';

export function ClustersPage() {
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ClusterSummary | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const { data: clusters, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['clusters'],
    queryFn: getClusters,
    refetchInterval: 30_000,
  });
  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) => deleteCluster(id, confirmationName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });
  const startMutation = useMutation({
    mutationFn: startCluster,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
  });
  const restartMutation = useMutation({
    mutationFn: restartCluster,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
  });
  const shutdownMutation = useMutation({
    mutationFn: shutdownCluster,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
  });
  const redeployMutation = useMutation({
    mutationFn: redeployCluster,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
  });

  useEffect(() => {
    if (!dataUpdatedAt) return;
    if (!startMutation.isPending) startMutation.reset();
    if (!restartMutation.isPending) restartMutation.reset();
    if (!shutdownMutation.isPending) shutdownMutation.reset();
    if (!redeployMutation.isPending) redeployMutation.reset();
    if (!deleteMutation.isPending) deleteMutation.reset();
  }, [dataUpdatedAt]);

  const requestDeleteCluster = (cluster: ClusterSummary) => {
    setDeleteTarget(cluster);
    setDeleteConfirmationName('');
  };

  const confirmDeleteCluster = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) {
      return;
    }
    deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName });
  };

  return (
    <Layout>
      <PageHeader
        title="Clusters"
        description="Manage your Kubernetes clusters"
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
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}
      {redeployMutation.error && <ErrorMessage error={redeployMutation.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}
      {clusters && (
        <Card>
          {clusters.length === 0 ? (
            <div className="text-center py-16">
              <p className="text-slate-400 text-sm">No clusters yet.</p>
              <Link
                to="/clusters/new"
                className="mt-3 inline-flex items-center gap-1 text-indigo-600 hover:text-indigo-700 text-sm font-medium"
              >
                <Plus className="h-4 w-4" /> Provision your first cluster
              </Link>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Version</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Topology</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Nodes</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Health</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {clusters.map((c) => (
                    <tr key={c.id} className="hover:bg-slate-50 transition-colors">
                      <td className="px-6 py-4 font-medium text-slate-900">{c.name}</td>
                      <td className="px-6 py-4 text-slate-600">{c.kubernetes_version}</td>
                      <td className="px-6 py-4 text-slate-600 capitalize">{c.topology_type}</td>
                      <td className="px-6 py-4 text-slate-600">
                        {c.control_plane_count}CP + {c.worker_count}W
                      </td>
                      <td className="px-6 py-4">
                        <StatusBadge status={c.health_status || 'unknown'} />
                      </td>
                      <td className="px-6 py-4">
                        <StatusBadge status={c.status} />
                      </td>
                      <td className="px-6 py-4 text-slate-500">
                        {new Date(c.created_at).toLocaleDateString()}
                      </td>
                      <td className="px-6 py-4">
                        <div className="flex items-center justify-end gap-2">
                          <Link
                            to={`/clusters/${c.id}`}
                            className="inline-flex items-center text-indigo-600 hover:text-indigo-700"
                          >
                            Details <ChevronRight className="h-4 w-4" />
                          </Link>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => {
                              if (window.confirm(`Start cluster \"${c.name}\"? This sends a start command to every cluster VM.`)) {
                                startMutation.mutate(c.id);
                              }
                            }}
                            loading={startMutation.isPending && startMutation.variables === c.id}
                          >
                            <Play className="h-3.5 w-3.5" />
                            Start
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => {
                              if (window.confirm(`Restart cluster \"${c.name}\"? This sends a reboot command to every cluster VM.`)) {
                                restartMutation.mutate(c.id);
                              }
                            }}
                            loading={restartMutation.isPending && restartMutation.variables === c.id}
                          >
                            <RotateCcw className="h-3.5 w-3.5" />
                            Restart
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => {
                              if (window.confirm(`Shutdown cluster \"${c.name}\"? This sends a shutdown command to every cluster VM.`)) {
                                shutdownMutation.mutate(c.id);
                              }
                            }}
                            loading={shutdownMutation.isPending && shutdownMutation.variables === c.id}
                          >
                            <Power className="h-3.5 w-3.5" />
                            Shutdown
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => {
                              if (window.confirm(`Redeploy cluster \"${c.name}\" using the same generated Terraform configuration?`)) {
                                redeployMutation.mutate(c.id);
                              }
                            }}
                            loading={redeployMutation.isPending && redeployMutation.variables === c.id}
                          >
                            <RefreshCw className="h-3.5 w-3.5" />
                            Redeploy
                          </Button>
                          <Button
                            variant="danger"
                            size="sm"
                            onClick={() => requestDeleteCluster(c)}
                            loading={deleteMutation.isPending && deleteMutation.variables?.id === c.id}
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
        </Card>
      )}
      <DeleteClusterModal
        cluster={deleteTarget}
        confirmationName={deleteConfirmationName}
        loading={deleteMutation.isPending}
        onChange={setDeleteConfirmationName}
        onClose={() => {
          if (!deleteMutation.isPending) {
            setDeleteTarget(null);
            setDeleteConfirmationName('');
          }
        }}
        onSubmit={confirmDeleteCluster}
      />
    </Layout>
  );
}

function DeleteClusterModal({
  cluster,
  confirmationName,
  loading,
  onChange,
  onClose,
  onSubmit,
}: {
  cluster: ClusterSummary | null;
  confirmationName: string;
  loading: boolean;
  onChange: (value: string) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const canDelete = !!cluster && confirmationName === cluster.name;
  return (
    <Modal open={!!cluster} onClose={onClose} title="Delete Kubernetes Cluster" maxWidth="lg">
      {cluster && (
        <form onSubmit={onSubmit} className="space-y-5">
          <Alert variant="warning" title="This deletes infrastructure">
            Terraform destroy will be attempted for the Kubernetes cluster VMs before portal metadata is removed. Type the exact cluster name to continue.
          </Alert>
          <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            Type <span className="font-semibold text-slate-900">{cluster.name}</span> to confirm deletion.
          </div>
          <Input
            label="Cluster name confirmation"
            value={confirmationName}
            onChange={(event) => onChange(event.target.value)}
            autoFocus
            autoComplete="off"
          />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" loading={loading} disabled={!canDelete}>
              <Trash2 className="h-4 w-4" />
              Delete Cluster
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
