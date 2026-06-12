import { useState, type FormEvent, type ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Play, Power, RefreshCw, RotateCcw } from 'lucide-react';
import {
  getContainerEvents,
  getContainerInstance,
  redeployContainerInstance,
  refreshContainerStatus,
  restartContainerInstance,
  shutdownContainerInstance,
  startContainerInstance,
} from '../api/containers';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';

export function ContainerDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const containerID = Number(id);
  const [redeployOpen, setRedeployOpen] = useState(false);
  const [redeployConfirmationName, setRedeployConfirmationName] = useState('');

  const { data: container, isLoading, error } = useQuery({
    queryKey: ['containers', containerID],
    queryFn: () => getContainerInstance(containerID),
    enabled: Number.isFinite(containerID) && containerID > 0,
  });
  const { data: events } = useQuery({
    queryKey: ['containers', containerID, 'events'],
    queryFn: () => getContainerEvents(containerID),
    enabled: Number.isFinite(containerID) && containerID > 0,
    refetchInterval: 15_000,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['containers'] });
    queryClient.invalidateQueries({ queryKey: ['containers', containerID] });
    queryClient.invalidateQueries({ queryKey: ['containers', containerID, 'events'] });
  };

  const redeployMutation = useMutation({
    mutationFn: redeployContainerInstance,
    onSuccess: () => {
      invalidate();
      setRedeployOpen(false);
      setRedeployConfirmationName('');
    },
  });
  const refreshMutation = useMutation({ mutationFn: refreshContainerStatus, onSuccess: invalidate });
  const startMutation = useMutation({ mutationFn: startContainerInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartContainerInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownContainerInstance, onSuccess: invalidate });

  const confirmRedeploy = (event: FormEvent) => {
    event.preventDefault();
    if (!container || redeployConfirmationName !== container.name) return;
    redeployMutation.mutate(container.id);
  };

  if (!Number.isFinite(containerID) || containerID <= 0) {
    navigate('/containers');
    return null;
  }

  return (
    <Layout>
      <PageHeader
        title={container?.name ?? 'Container Details'}
        description="Proxmox LXC configuration and lifecycle"
        actions={<Link to="/containers" className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm"><ArrowLeft className="h-4 w-4" /> Back</Link>}
      />
      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {redeployMutation.error && <ErrorMessage error={redeployMutation.error} />}
      {refreshMutation.error && <ErrorMessage error={refreshMutation.error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}

      {container && (
        <div className="space-y-6">
          <div className="flex flex-wrap gap-2">
            <Button variant="danger" onClick={() => { setRedeployOpen(true); setRedeployConfirmationName(''); }} loading={redeployMutation.isPending}>
              <RefreshCw className="h-4 w-4" /> Redeploy
            </Button>
            <Button variant="secondary" onClick={() => refreshMutation.mutate(container.id)} loading={refreshMutation.isPending}>
              <RefreshCw className="h-4 w-4" /> Refresh Status
            </Button>
            <Button variant="secondary" onClick={() => startMutation.mutate(container.id)} loading={startMutation.isPending}>
              <Play className="h-4 w-4" /> Start
            </Button>
            <Button variant="secondary" onClick={() => restartMutation.mutate(container.id)} loading={restartMutation.isPending}>
              <RotateCcw className="h-4 w-4" /> Restart
            </Button>
            <Button variant="secondary" onClick={() => shutdownMutation.mutate(container.id)} loading={shutdownMutation.isPending}>
              <Power className="h-4 w-4" /> Shutdown
            </Button>
          </div>

          <Card>
            <CardHeader><CardTitle>Configuration</CardTitle></CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
                <Info label="Status" value={<StatusBadge status={container.status} />} />
                <Info label="Proxmox node" value={container.proxmox_node_name} />
                <Info label="Network" value={container.network_name || String(container.network_profile_id)} />
                <Info label="IP address" value={container.ip_address || container.ip_mode.toUpperCase()} />
                <Info label="Resources" value={`${container.cpu} vCPU / ${container.memory_mb} MB / ${container.disk_gb} GB`} />
                <Info label="Template" value={container.template_path} />
                <Info label="Mode" value={`${container.unprivileged ? 'Unprivileged' : 'Privileged'} / nesting ${container.nesting ? 'enabled' : 'disabled'}`} />
                <Info label="Terraform work dir" value={container.terraform_work_dir} />
                <Info label="Created" value={new Date(container.created_at).toLocaleString()} />
              </dl>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Events</CardTitle></CardHeader>
            <CardContent>
              {!events || events.length === 0 ? (
                <p className="text-sm text-slate-400">No container events yet.</p>
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
                      {event.output && <pre className="mt-3 max-h-72 overflow-auto rounded bg-slate-950 p-3 text-xs text-slate-100 whitespace-pre-wrap">{event.output}</pre>}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      <Modal open={redeployOpen} onClose={() => { if (!redeployMutation.isPending) { setRedeployOpen(false); setRedeployConfirmationName(''); } }} title="Redeploy Container" maxWidth="lg">
        {container && (
          <form onSubmit={confirmRedeploy} className="space-y-5">
            <Alert variant="warning" title="This reinstalls the LXC container">
              Redeploy destroys the current container infrastructure from Terraform state and applies it again from the saved template and configuration.
            </Alert>
            <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
              Type <span className="font-semibold text-slate-900">{container.name}</span> to confirm redeploy.
            </div>
            <Input label="Container name confirmation" value={redeployConfirmationName} onChange={(event) => setRedeployConfirmationName(event.target.value)} autoFocus autoComplete="off" />
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setRedeployOpen(false)} disabled={redeployMutation.isPending}>Cancel</Button>
              <Button type="submit" variant="danger" loading={redeployMutation.isPending} disabled={redeployConfirmationName !== container.name}>
                <RefreshCw className="h-4 w-4" /> Redeploy Container
              </Button>
            </div>
          </form>
        )}
      </Modal>
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
