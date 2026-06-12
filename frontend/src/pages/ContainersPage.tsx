import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronRight, Play, Plus, Power, RotateCcw, Trash2 } from 'lucide-react';
import {
  deleteContainerInstance,
  getContainerInstances,
  restartContainerInstance,
  shutdownContainerInstance,
  startContainerInstance,
} from '../api/containers';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import type { ContainerInstance } from '../types';

export function ContainersPage() {
  const queryClient = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<ContainerInstance | null>(null);
  const [deleteConfirmationName, setDeleteConfirmationName] = useState('');
  const { data: instances, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['containers'],
    queryFn: getContainerInstances,
    refetchInterval: 30_000,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['containers'] });
  const startMutation = useMutation({ mutationFn: startContainerInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartContainerInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownContainerInstance, onSuccess: invalidate });
  const deleteMutation = useMutation({
    mutationFn: ({ id, confirmationName }: { id: number; confirmationName: string }) => deleteContainerInstance(id, confirmationName),
    onSuccess: () => {
      invalidate();
      setDeleteTarget(null);
      setDeleteConfirmationName('');
    },
  });

  useEffect(() => {
    if (!dataUpdatedAt) return;
    if (!startMutation.isPending) startMutation.reset();
    if (!restartMutation.isPending) restartMutation.reset();
    if (!shutdownMutation.isPending) shutdownMutation.reset();
    if (!deleteMutation.isPending) deleteMutation.reset();
  }, [dataUpdatedAt]);

  const confirmDelete = (event: FormEvent) => {
    event.preventDefault();
    if (!deleteTarget || deleteConfirmationName !== deleteTarget.name) return;
    deleteMutation.mutate({ id: deleteTarget.id, confirmationName: deleteConfirmationName });
  };

  return (
    <Layout>
      <PageHeader
        title="Containers"
        description="Create and manage Proxmox LXC containers"
        actions={
          <Link to="/containers/new" className="inline-flex items-center gap-2 bg-indigo-600 text-white px-4 py-2 rounded-md text-sm font-medium hover:bg-indigo-700 transition-colors">
            <Plus className="h-4 w-4" />
            New Container
          </Link>
        }
      />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}
      {deleteMutation.error && <ErrorMessage error={deleteMutation.error} />}

      {instances && (
        <Card>
          {instances.length === 0 ? (
            <CardContent>
              <div className="text-center py-12">
                <p className="text-slate-400 text-sm">No LXC containers yet.</p>
                <Link to="/containers/new" className="mt-3 inline-flex items-center gap-1 text-indigo-600 hover:text-indigo-700 text-sm font-medium">
                  <Plus className="h-4 w-4" /> Create your first container
                </Link>
              </div>
            </CardContent>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 bg-slate-50">
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Node</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">IP</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Resources</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {instances.map((item) => (
                    <tr key={item.id} className="hover:bg-slate-50 transition-colors">
                      <td className="px-6 py-4">
                        <p className="font-medium text-slate-900">{item.name}</p>
                        <p className="text-xs text-slate-500">{item.network_name || 'Network selected'}</p>
                      </td>
                      <td className="px-6 py-4 text-slate-600">{item.proxmox_node_name}</td>
                      <td className="px-6 py-4 text-slate-600">{item.ip_address || item.ip_mode.toUpperCase()}</td>
                      <td className="px-6 py-4 text-slate-600">{item.cpu} vCPU / {item.memory_mb} MB / {item.disk_gb} GB</td>
                      <td className="px-6 py-4"><StatusBadge status={item.status} /></td>
                      <td className="px-6 py-4 text-slate-500">{new Date(item.created_at).toLocaleDateString()}</td>
                      <td className="px-6 py-4">
                        <div className="flex items-center justify-end gap-2">
                          <Link to={`/containers/${item.id}`} className="inline-flex items-center text-indigo-600 hover:text-indigo-700">
                            Details <ChevronRight className="h-4 w-4" />
                          </Link>
                          <Button variant="secondary" size="sm" onClick={() => startMutation.mutate(item.id)} loading={startMutation.isPending && startMutation.variables === item.id}>
                            <Play className="h-3.5 w-3.5" /> Start
                          </Button>
                          <Button variant="secondary" size="sm" onClick={() => restartMutation.mutate(item.id)} loading={restartMutation.isPending && restartMutation.variables === item.id}>
                            <RotateCcw className="h-3.5 w-3.5" /> Restart
                          </Button>
                          <Button variant="secondary" size="sm" onClick={() => shutdownMutation.mutate(item.id)} loading={shutdownMutation.isPending && shutdownMutation.variables === item.id}>
                            <Power className="h-3.5 w-3.5" /> Shutdown
                          </Button>
                          <Button variant="danger" size="sm" onClick={() => { setDeleteTarget(item); setDeleteConfirmationName(''); }} loading={deleteMutation.isPending && deleteMutation.variables?.id === item.id}>
                            <Trash2 className="h-3.5 w-3.5" /> Delete
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

      <Modal open={!!deleteTarget} onClose={() => { if (!deleteMutation.isPending) { setDeleteTarget(null); setDeleteConfirmationName(''); } }} title="Delete Container" maxWidth="lg">
        {deleteTarget && (
          <form onSubmit={confirmDelete} className="space-y-4">
            <p className="text-sm text-slate-600">
              Terraform destroy will be attempted before metadata is removed. Type <span className="font-semibold text-slate-900">{deleteTarget.name}</span> to continue.
            </p>
            <Input label="Container name confirmation" value={deleteConfirmationName} onChange={(event) => setDeleteConfirmationName(event.target.value)} autoFocus autoComplete="off" />
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>Cancel</Button>
              <Button type="submit" variant="danger" loading={deleteMutation.isPending} disabled={deleteConfirmationName !== deleteTarget.name}>
                <Trash2 className="h-4 w-4" /> Delete Container
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </Layout>
  );
}
