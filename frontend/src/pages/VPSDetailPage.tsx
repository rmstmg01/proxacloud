import type { ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Play, Power, RefreshCw, RotateCcw } from 'lucide-react';
import {
  deployVPSInstance,
  getVPSEvents,
  getVPSInstance,
  refreshVPSStatus,
  restartVPSInstance,
  shutdownVPSInstance,
  startVPSInstance,
} from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

export function VPSDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const vpsID = Number(id);

  const { data: vps, isLoading, error } = useQuery({
    queryKey: ['vps', vpsID],
    queryFn: () => getVPSInstance(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const { data: events } = useQuery({
    queryKey: ['vps', vpsID, 'events'],
    queryFn: () => getVPSEvents(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0,
    refetchInterval: 15_000,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['vps'] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'events'] });
  };

  const deployMutation = useMutation({ mutationFn: deployVPSInstance, onSuccess: invalidate });
  const refreshMutation = useMutation({ mutationFn: refreshVPSStatus, onSuccess: invalidate });
  const startMutation = useMutation({ mutationFn: startVPSInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartVPSInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownVPSInstance, onSuccess: invalidate });

  if (!Number.isFinite(vpsID) || vpsID <= 0) {
    navigate('/vps');
    return null;
  }

  return (
    <Layout>
      <PageHeader
        title={vps?.name ?? 'VPS Details'}
        description="Standalone Proxmox VM configuration and lifecycle"
        actions={
          <Link to="/vps" className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm">
            <ArrowLeft className="h-4 w-4" /> Back
          </Link>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {deployMutation.error && <ErrorMessage error={deployMutation.error} />}
      {refreshMutation.error && <ErrorMessage error={refreshMutation.error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}

      {vps && (
        <div className="space-y-6">
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={() => deployMutation.mutate(vps.id)} loading={deployMutation.isPending}>
              <RefreshCw className="h-4 w-4" />
              Deploy
            </Button>
            <Button variant="secondary" onClick={() => refreshMutation.mutate(vps.id)} loading={refreshMutation.isPending}>
              <RefreshCw className="h-4 w-4" />
              Refresh Status
            </Button>
            <Button variant="secondary" onClick={() => startMutation.mutate(vps.id)} loading={startMutation.isPending}>
              <Play className="h-4 w-4" />
              Start
            </Button>
            <Button variant="secondary" onClick={() => restartMutation.mutate(vps.id)} loading={restartMutation.isPending}>
              <RotateCcw className="h-4 w-4" />
              Restart
            </Button>
            <Button variant="secondary" onClick={() => shutdownMutation.mutate(vps.id)} loading={shutdownMutation.isPending}>
              <Power className="h-4 w-4" />
              Shutdown
            </Button>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Configuration</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
                <Info label="Status" value={<StatusBadge status={vps.status} />} />
                <Info label="Proxmox node" value={vps.proxmox_node_name} />
                <Info label="Network" value={vps.network_name || String(vps.network_profile_id)} />
                <Info label="IP address" value={vps.ip_address || vps.ip_mode.toUpperCase()} />
                <Info label="Resources" value={`${vps.cpu} vCPU / ${vps.memory_mb} MB / ${vps.disk_gb} GB`} />
                <Info label="SSH user" value={vps.ssh_user} />
                <Info label="Cloud image" value={vps.cloud_init_image_path} />
                <Info label="Terraform work dir" value={vps.terraform_work_dir} />
                <Info label="Created" value={new Date(vps.created_at).toLocaleString()} />
              </dl>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Events</CardTitle>
            </CardHeader>
            <CardContent>
              {!events || events.length === 0 ? (
                <p className="text-sm text-slate-400">No VPS events yet.</p>
              ) : (
                <div className="space-y-3">
                  {events.map((event) => (
                    <div key={event.id} className="rounded-md border border-slate-200 bg-white p-3">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <StatusBadge status={event.level} />
                          <p className="text-sm font-medium text-slate-900">{event.message}</p>
                        </div>
                        <p className="text-xs text-slate-500">{new Date(event.created_at).toLocaleString()}</p>
                      </div>
                      <p className="mt-1 text-xs text-slate-500">{event.action}</p>
                      {event.output && (
                        <pre className="mt-3 max-h-72 overflow-auto rounded bg-slate-950 p-3 text-xs text-slate-100 whitespace-pre-wrap">
                          {event.output}
                        </pre>
                      )}
                    </div>
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

function Info({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-1 break-words text-slate-900">{value}</dd>
    </div>
  );
}
