import { useState, type FormEvent, type ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, History, Monitor, Play, Power, RefreshCw, RotateCcw, Shield } from 'lucide-react';
import {
  applyFirewall,
  createFirewall,
  createFirewallAttachment,
  createFirewallRule,
} from '../api/firewalls';
import {
  getVPSEvents,
  getVPSInstance,
  redeployVPSInstance,
  refreshVPSStatus,
  restartVPSInstance,
  shutdownVPSInstance,
  startVPSInstance,
} from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';
import type { FirewallAction, FirewallDirection, FirewallPolicy, FirewallProtocol } from '../types';

const defaultFirewallRuleForm = {
  name: '',
  description: '',
  default_inbound_policy: 'deny' as FirewallPolicy,
  default_outbound_policy: 'allow' as FirewallPolicy,
  direction: 'inbound' as FirewallDirection,
  action: 'allow' as FirewallAction,
  protocol: 'tcp' as FirewallProtocol,
  source_cidr: '0.0.0.0/0',
  destination_cidr: '',
  port_start: '22',
  port_end: '22',
  priority: '100',
  rule_description: 'SSH access',
};

export function VPSDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const vpsID = Number(id);
  const [redeployOpen, setRedeployOpen] = useState(false);
  const [redeployConfirmationName, setRedeployConfirmationName] = useState('');
  const [showEvents, setShowEvents] = useState(false);
  const [firewallOpen, setFirewallOpen] = useState(false);
  const [firewallRuleForm, setFirewallRuleForm] = useState(defaultFirewallRuleForm);

  const { data: vps, isLoading, error } = useQuery({
    queryKey: ['vps', vpsID],
    queryFn: () => getVPSInstance(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const { data: events, error: eventsError, isFetching: eventsFetching } = useQuery({
    queryKey: ['vps', vpsID, 'events'],
    queryFn: () => getVPSEvents(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0 && showEvents,
    refetchInterval: showEvents ? 15_000 : false,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['vps'] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'events'] });
  };

  const redeployMutation = useMutation({
    mutationFn: redeployVPSInstance,
    onSuccess: () => {
      invalidate();
      setRedeployOpen(false);
      setRedeployConfirmationName('');
    },
  });
  const refreshMutation = useMutation({ mutationFn: refreshVPSStatus, onSuccess: invalidate });
  const startMutation = useMutation({ mutationFn: startVPSInstance, onSuccess: invalidate });
  const restartMutation = useMutation({ mutationFn: restartVPSInstance, onSuccess: invalidate });
  const shutdownMutation = useMutation({ mutationFn: shutdownVPSInstance, onSuccess: invalidate });
  const firewallMutation = useMutation({
    mutationFn: async () => {
      if (!vps) throw new Error('VPS is not loaded');
      const hasPorts = firewallRuleForm.protocol === 'tcp' || firewallRuleForm.protocol === 'udp';
      const group = await createFirewall({
        name: firewallRuleForm.name.trim() || `${vps.name}-firewall`,
        description: firewallRuleForm.description.trim() || `Firewall rules for ${vps.name}`,
        default_inbound_policy: firewallRuleForm.default_inbound_policy,
        default_outbound_policy: firewallRuleForm.default_outbound_policy,
        enabled: true,
      });
      await createFirewallRule(group.id, {
        direction: firewallRuleForm.direction,
        action: firewallRuleForm.action,
        protocol: firewallRuleForm.protocol,
        source_cidr: firewallRuleForm.source_cidr.trim(),
        destination_cidr: firewallRuleForm.destination_cidr.trim(),
        port_start: hasPorts && firewallRuleForm.port_start ? Number(firewallRuleForm.port_start) : undefined,
        port_end: hasPorts && firewallRuleForm.port_end ? Number(firewallRuleForm.port_end) : undefined,
        priority: Number(firewallRuleForm.priority),
        description: firewallRuleForm.rule_description.trim(),
        enabled: true,
      });
      await createFirewallAttachment(group.id, {
        target_type: 'vps',
        target_id: vps.id,
      });
      return applyFirewall(group.id);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['firewalls'] });
      setFirewallOpen(false);
      setFirewallRuleForm(defaultFirewallRuleForm);
    },
  });

  const confirmRedeploy = (event: FormEvent) => {
    event.preventDefault();
    if (!vps || redeployConfirmationName !== vps.name) return;
    redeployMutation.mutate(vps.id);
  };

  const openConsole = () => {
    if (!vps) return;
    window.open(`/vps/${vps.id}/console`, '_blank', 'noopener,noreferrer');
  };

  const openFirewallRule = () => {
    if (!vps) return;
    setFirewallRuleForm({
      ...defaultFirewallRuleForm,
      name: `${vps.name}-firewall`,
      description: `Firewall rules for ${vps.name}`,
    });
    setFirewallOpen(true);
  };

  const createVPSFirewallRule = (event: FormEvent) => {
    event.preventDefault();
    firewallMutation.mutate();
  };

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
      {redeployMutation.error && <ErrorMessage error={redeployMutation.error} />}
      {refreshMutation.error && <ErrorMessage error={refreshMutation.error} />}
      {startMutation.error && <ErrorMessage error={startMutation.error} />}
      {restartMutation.error && <ErrorMessage error={restartMutation.error} />}
      {shutdownMutation.error && <ErrorMessage error={shutdownMutation.error} />}
      {firewallMutation.error && <ErrorMessage error={firewallMutation.error} />}
      {showEvents && eventsError && <ErrorMessage error={eventsError} />}

      {vps && (
        <div className="space-y-6">
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" onClick={openConsole}>
              <Monitor className="h-4 w-4" />
              Console
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
            <Button
              variant="danger"
              onClick={() => {
                setRedeployOpen(true);
                setRedeployConfirmationName('');
              }}
              loading={redeployMutation.isPending}
            >
              <RefreshCw className="h-4 w-4" />
              Redeploy
            </Button>
            <Button variant={showEvents ? 'primary' : 'secondary'} onClick={() => setShowEvents((current) => !current)}>
              <History className="h-4 w-4" />
              Events
            </Button>
            <Button variant="secondary" onClick={openFirewallRule}>
              <Shield className="h-4 w-4" />
              Firewall
            </Button>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Configuration</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
                <Info
                  label="Status"
                  value={
                    <div className="inline-flex items-center gap-2">
                      <StatusBadge status={vps.status} />
                      <button
                        type="button"
                        onClick={() => refreshMutation.mutate(vps.id)}
                        disabled={refreshMutation.isPending}
                        className="inline-flex h-6 w-6 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:cursor-not-allowed disabled:opacity-50"
                        aria-label="Refresh VPS status"
                        title="Refresh status"
                      >
                        <RefreshCw className={`h-3.5 w-3.5 ${refreshMutation.isPending ? 'animate-spin' : ''}`} />
                      </button>
                    </div>
                  }
                />
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

          {showEvents && (
            <Card>
              <CardHeader>
                <CardTitle>Events</CardTitle>
              </CardHeader>
              <CardContent>
                {eventsFetching && !events ? (
                  <p className="text-sm text-slate-400">Loading events...</p>
                ) : !events || events.length === 0 ? (
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
          )}
        </div>
      )}
      <Modal
        open={redeployOpen}
        onClose={() => {
          if (!redeployMutation.isPending) {
            setRedeployOpen(false);
            setRedeployConfirmationName('');
          }
        }}
        title="Redeploy VPS"
        maxWidth="lg"
      >
        {vps && (
          <form onSubmit={confirmRedeploy} className="space-y-5">
            <Alert variant="warning" title="This reinstalls the VPS operating system">
              Redeploy destroys the current VM infrastructure from Terraform state and applies it again from the saved cloud-init image and configuration.
            </Alert>
            <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
              Type <span className="font-semibold text-slate-900">{vps.name}</span> to confirm redeploy.
            </div>
            <Input
              label="VPS name confirmation"
              value={redeployConfirmationName}
              onChange={(event) => setRedeployConfirmationName(event.target.value)}
              autoFocus
              autoComplete="off"
            />
            <div className="flex items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setRedeployOpen(false)} disabled={redeployMutation.isPending}>
                Cancel
              </Button>
              <Button type="submit" variant="danger" loading={redeployMutation.isPending} disabled={redeployConfirmationName !== vps.name}>
                <RefreshCw className="h-4 w-4" />
                Redeploy VPS
              </Button>
            </div>
          </form>
        )}
      </Modal>
      <Modal
        open={firewallOpen}
        onClose={() => {
          if (!firewallMutation.isPending) {
            setFirewallOpen(false);
            setFirewallRuleForm(defaultFirewallRuleForm);
          }
        }}
        title="VPS Firewall Rule"
        maxWidth="lg"
      >
        {vps && (
          <form onSubmit={createVPSFirewallRule} className="space-y-5">
            <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
              This creates a firewall group, attaches it to <span className="font-semibold text-slate-900">{vps.name}</span>, and applies the rule.
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Input
                label="Firewall name"
                value={firewallRuleForm.name}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, name: event.target.value })}
                required
              />
              <Input
                label="Description"
                value={firewallRuleForm.description}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, description: event.target.value })}
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Select
                label="Default inbound"
                value={firewallRuleForm.default_inbound_policy}
                onChange={(event) =>
                  setFirewallRuleForm({ ...firewallRuleForm, default_inbound_policy: event.target.value as FirewallPolicy })
                }
              >
                <option value="deny">Deny</option>
                <option value="allow">Allow</option>
              </Select>
              <Select
                label="Default outbound"
                value={firewallRuleForm.default_outbound_policy}
                onChange={(event) =>
                  setFirewallRuleForm({ ...firewallRuleForm, default_outbound_policy: event.target.value as FirewallPolicy })
                }
              >
                <option value="allow">Allow</option>
                <option value="deny">Deny</option>
              </Select>
            </div>
            <div className="grid gap-4 sm:grid-cols-3">
              <Select
                label="Direction"
                value={firewallRuleForm.direction}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, direction: event.target.value as FirewallDirection })}
              >
                <option value="inbound">Inbound</option>
                <option value="outbound">Outbound</option>
              </Select>
              <Select
                label="Action"
                value={firewallRuleForm.action}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, action: event.target.value as FirewallAction })}
              >
                <option value="allow">Allow</option>
                <option value="deny">Deny</option>
              </Select>
              <Select
                label="Protocol"
                value={firewallRuleForm.protocol}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, protocol: event.target.value as FirewallProtocol })}
              >
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
                <option value="icmp">ICMP</option>
                <option value="any">Any</option>
              </Select>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Input
                label="Source CIDR"
                value={firewallRuleForm.source_cidr}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, source_cidr: event.target.value })}
                placeholder="0.0.0.0/0"
                required
              />
              <Input
                label="Destination CIDR"
                value={firewallRuleForm.destination_cidr}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, destination_cidr: event.target.value })}
                placeholder={vps.ip_address || 'Optional'}
              />
            </div>
            <div className="grid gap-4 sm:grid-cols-3">
              <Input
                label="Port start"
                type="number"
                min="1"
                max="65535"
                value={firewallRuleForm.port_start}
                disabled={firewallRuleForm.protocol === 'icmp' || firewallRuleForm.protocol === 'any'}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, port_start: event.target.value })}
              />
              <Input
                label="Port end"
                type="number"
                min="1"
                max="65535"
                value={firewallRuleForm.port_end}
                disabled={firewallRuleForm.protocol === 'icmp' || firewallRuleForm.protocol === 'any'}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, port_end: event.target.value })}
              />
              <Input
                label="Priority"
                type="number"
                min="1"
                value={firewallRuleForm.priority}
                onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, priority: event.target.value })}
                required
              />
            </div>
            <Input
              label="Rule description"
              value={firewallRuleForm.rule_description}
              onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, rule_description: event.target.value })}
              placeholder="Allow SSH"
            />
            {firewallMutation.error && <ErrorMessage error={firewallMutation.error} />}
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setFirewallOpen(false)} disabled={firewallMutation.isPending}>
                Cancel
              </Button>
              <Button type="submit" loading={firewallMutation.isPending}>
                <Shield className="h-4 w-4" />
                Create and Apply
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
