import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronRight, Play, Plus, Power, RotateCcw } from 'lucide-react';
import {
  getVPSInstances,
  restartVPSInstance,
  shutdownVPSInstance,
  startVPSInstance,
} from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent } from '../components/ui/Card';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import { useAuth } from '../context/AuthContext';

export function VPSPage() {
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const showOwner = user?.role === 'super_user' || user?.role === 'super_admin';
  const { data: instances, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['vps'],
    queryFn: getVPSInstances,
    refetchInterval: 30_000,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['vps'] });
  };

  const startMutation = useMutation({ mutationFn: startVPSInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartVPSInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownVPSInstance, onSuccess: invalidate });

  useEffect(() => {
    if (!dataUpdatedAt) return;
    if (!startMutation.isPending) startMutation.reset();
    if (!restartMutation.isPending) restartMutation.reset();
    if (!shutdownMutation.isPending) shutdownMutation.reset();
  }, [dataUpdatedAt]);

  return (
    <Layout>
      <PageHeader
        title="VPS"
        description="Create and manage standalone cloud-init VMs"
        actions={
          <Link
            to="/vps/new"
            className="inline-flex items-center gap-2 bg-indigo-600 text-white px-4 py-2 rounded-md text-sm font-medium hover:bg-indigo-700 transition-colors"
          >
            <Plus className="h-4 w-4" />
            New VPS
          </Link>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}

      {instances && (
        <Card>
          {instances.length === 0 ? (
            <CardContent>
              <div className="text-center py-12">
                <p className="text-slate-400 text-sm">No VPS instances yet.</p>
                <Link
                  to="/vps/new"
                  className="mt-3 inline-flex items-center gap-1 text-indigo-600 hover:text-indigo-700 text-sm font-medium"
                >
                  <Plus className="h-4 w-4" /> Create your first VPS
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
                    {showOwner && <th className="text-left px-6 py-3 font-medium text-slate-600">Owner</th>}
                    <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                    <th className="px-6 py-3" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {instances.map((item) => (
                    <tr key={item.id} className="hover:bg-slate-50 transition-colors">
                      <td className="px-6 py-4">
                        <p className="font-medium text-slate-900">{item.name}</p>
                      </td>
                      <td className="px-6 py-4 text-slate-600">{item.proxmox_node_name}</td>
                      <td className="px-6 py-4 text-slate-600">{item.ip_address || item.ip_mode.toUpperCase()}</td>
                      <td className="px-6 py-4 text-slate-600">
                        {item.cpu} vCPU / {item.memory_mb} MB / {item.disk_gb} GB
                      </td>
                      <td className="px-6 py-4">
                        <div className="flex flex-wrap gap-1.5">
                          <StatusBadge status={item.status} />
                          {item.admin_suspended_at && <Badge variant="danger">Admin suspended</Badge>}
                        </div>
                      </td>
                      {showOwner && (
                        <td className="px-6 py-4 text-slate-600">
                          <div className="flex flex-wrap items-center gap-2">
                            <span>{item.owner_username || 'Legacy platform'}</span>
                            <Badge variant={item.ownership_type === 'customer' ? 'info' : item.ownership_type === 'admin' ? 'success' : 'warning'}>
                              {item.ownership_type === 'customer' ? 'Customer' : item.ownership_type === 'admin' ? 'Admin owned' : 'Claim owner'}
                            </Badge>
                          </div>
                        </td>
                      )}
                      <td className="px-6 py-4 text-slate-500">{new Date(item.created_at).toLocaleDateString()}</td>
                      <td className="px-6 py-4">
                        <div className="flex items-center justify-end gap-2">
                          <Link to={`/vps/${item.id}`} className="inline-flex items-center text-indigo-600 hover:text-indigo-700">
                            Details <ChevronRight className="h-4 w-4" />
                          </Link>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => startMutation.mutate(item.id)}
                            disabled={Boolean(item.admin_suspended_at)}
                            loading={startMutation.isPending && startMutation.variables === item.id}
                          >
                            <Play className="h-3.5 w-3.5" />
                            Start
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => restartMutation.mutate(item.id)}
                            disabled={Boolean(item.admin_suspended_at)}
                            loading={restartMutation.isPending && restartMutation.variables === item.id}
                          >
                            <RotateCcw className="h-3.5 w-3.5" />
                            Restart
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => shutdownMutation.mutate(item.id)}
                            disabled={Boolean(item.admin_suspended_at)}
                            loading={shutdownMutation.isPending && shutdownMutation.variables === item.id}
                          >
                            <Power className="h-3.5 w-3.5" />
                            Shutdown
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
    </Layout>
  );
}
