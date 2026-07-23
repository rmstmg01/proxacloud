import { useState, type FormEvent, type ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, Copy, ExternalLink, Eye, EyeOff, History, Monitor, Pencil, Play, Plus, Power, RefreshCw, RotateCcw, Shield, Trash2 } from 'lucide-react';
import {
  createFirewall,
  createFirewallAttachment,
  createFirewallRule,
  deleteFirewallRule,
  getFirewall,
  getFirewalls,
  updateFirewallRule,
} from '../api/firewalls';
import {
  getVPSEvents,
  getVPSInstance,
  getVPSLoginDetails,
  getVPSTunnel,
  redeployVPSInstance,
  refreshVPSStatus,
  resetVPSRootPassword,
  restartVPSInstance,
  retryVPSTunnel,
  shutdownVPSInstance,
  startVPSInstance,
  updateVPSHostname,
} from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { VPSBackupProtection } from '../components/VPSBackupProtection';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';
import { useAuth } from '../context/AuthContext';
import type { FirewallAction, FirewallDirection, FirewallGroup, FirewallPolicy, FirewallProtocol, FirewallRule, VPSTunnelAttachment } from '../types';

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
  enabled: true,
};

type VPSFirewallRule = FirewallRule & {
  group_id: number;
  group_name: string;
};

export function VPSDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const vpsID = Number(id);
  const canViewEvents = user?.role === 'super_user' || user?.role === 'super_admin';
  const [redeployOpen, setRedeployOpen] = useState(false);
  const [redeployConfirmationName, setRedeployConfirmationName] = useState('');
  const [showEvents, setShowEvents] = useState(false);
  const [showFirewallRules, setShowFirewallRules] = useState(false);
  const [firewallRuleOpen, setFirewallRuleOpen] = useState(false);
  const [firewallRuleForm, setFirewallRuleForm] = useState(defaultFirewallRuleForm);
  const [editingFirewallRule, setEditingFirewallRule] = useState<VPSFirewallRule | null>(null);
  const [showRootPassword, setShowRootPassword] = useState(false);
  const [copiedKey, setCopiedKey] = useState('');
  const [rootPasswordResetNotice, setRootPasswordResetNotice] = useState(false);
  const [hostnameEditOpen, setHostnameEditOpen] = useState(false);
  const [hostnameForm, setHostnameForm] = useState('');

  const { data: vps, isLoading, error } = useQuery({
    queryKey: ['vps', vpsID],
    queryFn: () => getVPSInstance(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const { data: events, error: eventsError, isFetching: eventsFetching } = useQuery({
    queryKey: ['vps', vpsID, 'events'],
    queryFn: () => getVPSEvents(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0 && canViewEvents && showEvents,
    refetchInterval: canViewEvents && showEvents ? 15_000 : false,
  });
  const { data: tunnel, error: tunnelError, isFetching: tunnelFetching } = useQuery<VPSTunnelAttachment | null>({
    queryKey: ['vps', vpsID, 'tunnel'],
    queryFn: async () => {
      try {
        return await getVPSTunnel(vpsID);
      } catch (err: any) {
        if (err?.response?.status === 404) return null;
        throw err;
      }
    },
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const { data: loginDetails, error: loginDetailsError, isFetching: loginDetailsFetching } = useQuery({
    queryKey: ['vps', vpsID, 'login-details'],
    queryFn: () => getVPSLoginDetails(vpsID),
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const firewallsQuery = useQuery({
    queryKey: ['firewalls'],
    queryFn: getFirewalls,
    enabled: Number.isFinite(vpsID) && vpsID > 0,
  });
  const firewallDetailQueries = useQueries({
    queries: (firewallsQuery.data ?? []).map((firewall) => ({
      queryKey: ['firewalls', firewall.id],
      queryFn: () => getFirewall(firewall.id),
      enabled: Number.isFinite(vpsID) && vpsID > 0,
    })),
  });

  const vpsFirewallGroups = firewallDetailQueries
    .map((query) => query.data)
    .filter((group): group is FirewallGroup => {
      const attachments = group?.attachments ?? [];
      return attachments.length === 1 && attachments[0].target_type === 'vps' && attachments[0].target_id === vpsID;
    })
    .sort((a, b) => a.id - b.id);
  const primaryVPSFirewallGroup = vpsFirewallGroups[0];
  const vpsFirewallRules = vpsFirewallGroups.flatMap((group) =>
    (group.rules ?? []).map((rule) => ({
      ...rule,
      group_id: group.id,
      group_name: group.name,
    })),
  );
  const nextFirewallPriority = primaryVPSFirewallGroup?.rules?.length
    ? Math.max(...primaryVPSFirewallGroup.rules.map((rule) => rule.priority)) + 1
    : 100;

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['vps'] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'events'] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'tunnel'] });
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'login-details'] });
    queryClient.invalidateQueries({ queryKey: ['firewalls'] });
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
  const tunnelRetryMutation = useMutation({ mutationFn: retryVPSTunnel, onSuccess: invalidate });
  const resetRootPasswordMutation = useMutation({
    mutationFn: () => resetVPSRootPassword(vpsID),
    onSuccess: () => {
      setShowRootPassword(false);
      setRootPasswordResetNotice(true);
      window.setTimeout(() => setRootPasswordResetNotice(false), 3500);
      invalidate();
    },
  });
  const updateHostnameMutation = useMutation({
    mutationFn: () => updateVPSHostname(vpsID, hostnameForm),
    onSuccess: () => {
      setHostnameEditOpen(false);
      invalidate();
    },
  });
  const firewallMutation = useMutation({
    mutationFn: async () => {
      if (!vps) throw new Error('VPS is not loaded');
      const hasPorts = firewallRuleForm.protocol === 'tcp' || firewallRuleForm.protocol === 'udp';
      const body = {
        direction: firewallRuleForm.direction,
        action: firewallRuleForm.action,
        protocol: firewallRuleForm.protocol,
        source_cidr: firewallRuleForm.source_cidr.trim(),
        destination_cidr: firewallRuleForm.destination_cidr.trim(),
        port_start: hasPorts && firewallRuleForm.port_start ? Number(firewallRuleForm.port_start) : undefined,
        port_end: hasPorts && firewallRuleForm.port_end ? Number(firewallRuleForm.port_end) : undefined,
        priority: Number(firewallRuleForm.priority),
        description: firewallRuleForm.rule_description.trim(),
        enabled: firewallRuleForm.enabled,
      };
      if (editingFirewallRule) {
        return updateFirewallRule(editingFirewallRule.group_id, editingFirewallRule.id, body);
      }

      let group = primaryVPSFirewallGroup;
      if (!group) {
        group = await createFirewall({
          name: firewallRuleForm.name.trim() || `vps-${vps.id}-${vps.name}-firewall`,
          description: firewallRuleForm.description.trim() || `Firewall rules for ${vps.name}`,
          default_inbound_policy: firewallRuleForm.default_inbound_policy,
          default_outbound_policy: firewallRuleForm.default_outbound_policy,
          enabled: true,
        });
        await createFirewallAttachment(group.id, {
          target_type: 'vps',
          target_id: vps.id,
        });
      }
      return createFirewallRule(group.id, body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['firewalls'] });
      for (const group of vpsFirewallGroups) {
        queryClient.invalidateQueries({ queryKey: ['firewalls', group.id] });
      }
      setFirewallRuleOpen(false);
      setEditingFirewallRule(null);
      setFirewallRuleForm(defaultFirewallRuleForm);
    },
  });
  const deleteFirewallRuleMutation = useMutation({
    mutationFn: ({ groupID, ruleID }: { groupID: number; ruleID: number }) => deleteFirewallRule(groupID, ruleID),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ['firewalls'] });
      queryClient.invalidateQueries({ queryKey: ['firewalls', variables.groupID] });
    },
  });

  const confirmRedeploy = (event: FormEvent) => {
    event.preventDefault();
    if (!vps || redeployConfirmationName !== vps.name) return;
    redeployMutation.mutate(vps.id);
  };

  const openHostnameEdit = () => {
    if (!vps) return;
    updateHostnameMutation.reset();
    setHostnameForm(displayedHostname);
    setHostnameEditOpen(true);
  };

  const confirmHostnameEdit = (event: FormEvent) => {
    event.preventDefault();
    if (hostnameForm.trim() === '') return;
    updateHostnameMutation.mutate();
  };

  const openConsole = () => {
    if (!vps) return;
    window.open(`/vps/${vps.id}/console`, '_blank', 'noopener,noreferrer');
  };

  const copyText = async (key: string, value?: string) => {
    if (!value) return;
    await navigator.clipboard?.writeText(value);
    setCopiedKey(key);
    window.setTimeout(() => setCopiedKey((current) => (current === key ? '' : current)), 1800);
  };

  const copyButton = (key: string, value: string | undefined, label: string) => {
    const copied = copiedKey === key;
    return (
      <button
        type="button"
        onClick={() => copyText(key, value)}
        disabled={!value}
        className={`inline-flex h-7 shrink-0 items-center justify-center gap-1 rounded-md px-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${
          copied ? 'bg-emerald-50 text-emerald-700' : 'text-slate-500 hover:bg-slate-100 hover:text-slate-900'
        }`}
        title={copied ? 'Copied' : label}
        aria-label={copied ? 'Copied' : label}
      >
        {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
        {copied && <span>Copied</span>}
      </button>
    );
  };

  const displayedRootPassword = resetRootPasswordMutation.data?.root_password ?? loginDetails?.root_password ?? '';
  const displayedHostname = loginDetails?.hostname || vps?.hostname || vps?.vm_name || vps?.name || '';

  const openFirewallRule = () => {
    if (!vps) return;
    setEditingFirewallRule(null);
    setFirewallRuleForm({
      ...defaultFirewallRuleForm,
      name: primaryVPSFirewallGroup?.name ?? `vps-${vps.id}-${vps.name}-firewall`,
      description: primaryVPSFirewallGroup?.description ?? `Firewall rules for ${vps.name}`,
      default_inbound_policy: primaryVPSFirewallGroup?.default_inbound_policy ?? defaultFirewallRuleForm.default_inbound_policy,
      default_outbound_policy: primaryVPSFirewallGroup?.default_outbound_policy ?? defaultFirewallRuleForm.default_outbound_policy,
      priority: String(nextFirewallPriority),
    });
    setFirewallRuleOpen(true);
  };

  const openEditFirewallRule = (rule: VPSFirewallRule) => {
    const group = vpsFirewallGroups.find((item) => item.id === rule.group_id);
    setEditingFirewallRule(rule);
    setFirewallRuleForm({
      name: group?.name ?? rule.group_name,
      description: group?.description ?? '',
      default_inbound_policy: group?.default_inbound_policy ?? defaultFirewallRuleForm.default_inbound_policy,
      default_outbound_policy: group?.default_outbound_policy ?? defaultFirewallRuleForm.default_outbound_policy,
      direction: rule.direction,
      action: rule.action,
      protocol: rule.protocol,
      source_cidr: rule.source_cidr,
      destination_cidr: rule.destination_cidr,
      port_start: rule.port_start ? String(rule.port_start) : '',
      port_end: rule.port_end ? String(rule.port_end) : '',
      priority: String(rule.priority),
      rule_description: rule.description,
      enabled: rule.enabled,
    });
    setFirewallRuleOpen(true);
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
      {tunnelRetryMutation.error && <ErrorMessage error={tunnelRetryMutation.error} />}
      {loginDetailsError && <ErrorMessage error={loginDetailsError} />}
      {resetRootPasswordMutation.error && <ErrorMessage error={resetRootPasswordMutation.error} />}
      {firewallMutation.error && <ErrorMessage error={firewallMutation.error} />}
      {canViewEvents && showEvents && eventsError && <ErrorMessage error={eventsError} />}

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
            {canViewEvents && (
              <Button variant={showEvents ? 'primary' : 'secondary'} onClick={() => setShowEvents((current) => !current)}>
                <History className="h-4 w-4" />
                Events
              </Button>
            )}
            <Button variant={showFirewallRules ? 'primary' : 'secondary'} onClick={() => setShowFirewallRules((current) => !current)}>
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
                <Info label="Node" value={vps.proxmox_node_name} />
                <Info label="Network" value={vps.network_name || String(vps.network_profile_id)} />
                <Info label="IP address" value={vps.ip_address || vps.ip_mode.toUpperCase()} />
                <Info label="Resources" value={`${vps.cpu} vCPU / ${vps.memory_mb} MB / ${vps.disk_gb} GB`} />
                <Info label="SSH user" value={vps.ssh_user} />
                <Info label="Cloud image" value={formatCloudImageName(vps.cloud_init_image_path)} />
                <Info label="Location" value={vps.region_name || '-'} />
                <Info label="Created" value={new Date(vps.created_at).toLocaleString()} />
              </dl>
            </CardContent>
          </Card>

          <VPSBackupProtection vpsID={vps.id} diskGB={vps.disk_gb} />

          <Card>
            <CardHeader>
              <CardTitle>Login Details</CardTitle>
            </CardHeader>
            <CardContent>
              {rootPasswordResetNotice && (
                <Alert variant="success" title="Password has been reset">
                  Root password updated.
                </Alert>
              )}
              {loginDetailsFetching && !loginDetails ? (
                <p className="mt-4 text-sm text-slate-500">Loading login details...</p>
              ) : (
                <dl className="mt-4 grid grid-cols-1 gap-4 text-sm lg:grid-cols-2">
                  <Info
                    label="Hostname"
                    value={
                      <span className="inline-flex min-w-0 items-center gap-2">
                        <span className="truncate">{displayedHostname}</span>
                        {copyButton('hostname', displayedHostname, 'Copy hostname')}
                        <button
                          type="button"
                          onClick={openHostnameEdit}
                          className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
                          title="Edit hostname"
                          aria-label="Edit hostname"
                        >
                          <Pencil className="h-4 w-4" />
                        </button>
                      </span>
                    }
                  />
                  <Info
                    label="IP address"
                    value={
                      <span className="inline-flex min-w-0 items-center gap-2">
                        <span className="truncate">{loginDetails?.ip_address || vps.ip_address || '-'}</span>
                        {copyButton('ip-address', loginDetails?.ip_address || vps.ip_address, 'Copy IP address')}
                      </span>
                    }
                  />
                  <Info label="SSH user" value={loginDetails?.ssh_user || vps.ssh_user} />
                  <Info
                    label="Root password"
                    value={
                      displayedRootPassword ? (
                        <span className="inline-flex min-w-0 items-center gap-2">
                          <code className="truncate rounded bg-slate-100 px-2 py-1 text-xs text-slate-800">
                            {showRootPassword ? displayedRootPassword : '************'}
                          </code>
                          <button
                            type="button"
                            onClick={() => setShowRootPassword((current) => !current)}
                            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
                            title={showRootPassword ? 'Hide root password' : 'Show root password'}
                            aria-label={showRootPassword ? 'Hide root password' : 'Show root password'}
                          >
                            {showRootPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                          </button>
                          {copyButton('root-password', displayedRootPassword, 'Copy root password')}
                          <button
                            type="button"
                            onClick={() => resetRootPasswordMutation.mutate()}
                            disabled={resetRootPasswordMutation.isPending}
                            className="inline-flex h-7 shrink-0 items-center justify-center gap-1 rounded-md px-2 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:cursor-not-allowed disabled:opacity-50"
                            title="Reset root password"
                            aria-label="Reset root password"
                          >
                            <RotateCcw className={`h-4 w-4 ${resetRootPasswordMutation.isPending ? 'animate-spin' : ''}`} />
                            Reset
                          </button>
                        </span>
                      ) : loginDetails?.has_root_password ? (
                        <span className="inline-flex min-w-0 items-center gap-2">
                          <span>Stored password is unavailable</span>
                          <button
                            type="button"
                            onClick={() => resetRootPasswordMutation.mutate()}
                            disabled={resetRootPasswordMutation.isPending}
                            className="inline-flex h-7 shrink-0 items-center justify-center gap-1 rounded-md px-2 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:cursor-not-allowed disabled:opacity-50"
                            title="Reset root password"
                            aria-label="Reset root password"
                          >
                            <RotateCcw className={`h-4 w-4 ${resetRootPasswordMutation.isPending ? 'animate-spin' : ''}`} />
                            Reset
                          </button>
                        </span>
                      ) : (
                        <span className="inline-flex min-w-0 items-center gap-2">
                          <span>No root password set</span>
                          <button
                            type="button"
                            onClick={() => resetRootPasswordMutation.mutate()}
                            disabled={resetRootPasswordMutation.isPending}
                            className="inline-flex h-7 shrink-0 items-center justify-center gap-1 rounded-md px-2 text-xs font-medium text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:cursor-not-allowed disabled:opacity-50"
                            title="Reset root password"
                            aria-label="Reset root password"
                          >
                            <RotateCcw className={`h-4 w-4 ${resetRootPasswordMutation.isPending ? 'animate-spin' : ''}`} />
                            Reset
                          </button>
                        </span>
                      )
                    }
                  />
                </dl>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <CardTitle>Proxacloud Tunnel Access</CardTitle>
              <Button variant="secondary" onClick={() => tunnelRetryMutation.mutate(vps.id)} loading={tunnelRetryMutation.isPending}>
                <RefreshCw className="h-4 w-4" />
                Retry / Sync
              </Button>
            </CardHeader>
            <CardContent>
              {tunnelFetching && !tunnel ? (
                <p className="text-sm text-slate-500">Loading tunnel access...</p>
              ) : tunnelError ? (
                <ErrorMessage error={tunnelError} />
              ) : tunnel ? (
                <div className="space-y-4">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm text-slate-500">{tunnel.public_hostname}</span>
                  </div>
                  {tunnel.last_error && <Alert variant="error" title="Tunnel setup failed">{tunnel.last_error}</Alert>}
                  <dl className="grid grid-cols-1 gap-4 text-sm lg:grid-cols-2">
                    <Info
                      label="Web URL"
                      value={
                        <span className="inline-flex min-w-0 items-center gap-2">
                          <a href={tunnel.web_url} target="_blank" rel="noreferrer" className="truncate text-sky-700 hover:text-sky-900">
                            {tunnel.web_url}
                          </a>
                          <ExternalLink className="h-4 w-4 shrink-0 text-slate-400" />
                        </span>
                      }
                    />
                    <Info label="HTTP target" value={tunnel.http_target_url} />
                    <Info
                      label="DNS"
                      value={
                        tunnel.cloudflare_dns_status === 'active'
                          ? tunnel.public_hostname
                          : tunnel.cloudflare_dns_status || 'not synced'
                      }
                    />
                    <Info
                      label="SSH command"
                      value={
                        <span className="inline-flex min-w-0 items-center gap-2">
                          <code className="truncate rounded bg-slate-100 px-2 py-1 text-xs text-slate-800">{tunnel.ssh_command}</code>
                          {copyButton('ssh-command', tunnel.ssh_command, 'Copy SSH command')}
                        </span>
                      }
                    />
                    <Info label="TCP route" value={`${tunnel.public_hostname}:${tunnel.public_port} -> ${tunnel.tcp_target_host}:${tunnel.tcp_target_port}`} />
                  </dl>
                </div>
              ) : (
                <p className="text-sm text-slate-500">No Proxacloud tunnel attachment has been created for this VPS.</p>
              )}
            </CardContent>
          </Card>

          {showFirewallRules && (
            <Card>
              <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <CardTitle>Firewall Rules</CardTitle>
                  <p className="mt-1 text-sm text-slate-500">Rules shown here are attached only to this VPS.</p>
                </div>
                <Button variant="secondary" onClick={openFirewallRule}>
                  <Plus className="h-4 w-4" />
                  Rule
                </Button>
              </CardHeader>
              {firewallsQuery.isLoading || firewallDetailQueries.some((query) => query.isLoading) ? (
                <CardContent>
                  <p className="text-sm text-slate-500">Loading firewall rules...</p>
                </CardContent>
              ) : vpsFirewallRules.length ? (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-slate-200 bg-slate-50">
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Priority</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Direction</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Action</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Protocol</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">CIDR</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Ports</th>
                        <th className="px-5 py-3 text-left font-medium text-slate-600">Description</th>
                        <th className="px-5 py-3" />
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100">
                      {vpsFirewallRules.map((rule) => (
                        <tr key={`${rule.group_id}:${rule.id}`} className="hover:bg-slate-50">
                          <td className="px-5 py-3 font-medium text-slate-900">{rule.priority}</td>
                          <td className="px-5 py-3 text-slate-600">{rule.direction}</td>
                          <td className="px-5 py-3">
                            <Badge variant={rule.action === 'allow' ? 'success' : 'danger'}>{rule.action}</Badge>
                          </td>
                          <td className="px-5 py-3 text-slate-600">{rule.protocol}</td>
                          <td className="px-5 py-3 text-slate-600">
                            {rule.direction === 'inbound' ? rule.source_cidr || 'any' : rule.destination_cidr || 'any'}
                          </td>
                          <td className="px-5 py-3 text-slate-600">
                            {rule.port_start ? `${rule.port_start}-${rule.port_end ?? rule.port_start}` : 'any'}
                          </td>
                          <td className="max-w-xs truncate px-5 py-3 text-slate-600">{rule.description || '-'}</td>
                          <td className="px-5 py-3 text-right">
                            <div className="flex justify-end gap-2">
                              <Button variant="ghost" size="sm" onClick={() => openEditFirewallRule(rule)} title="Edit firewall rule">
                                <Pencil className="h-4 w-4" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => deleteFirewallRuleMutation.mutate({ groupID: rule.group_id, ruleID: rule.id })}
                                loading={
                                  deleteFirewallRuleMutation.isPending &&
                                  deleteFirewallRuleMutation.variables?.ruleID === rule.id
                                }
                                title="Delete firewall rule"
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <CardContent>
                  <p className="text-sm text-slate-500">No VPS-only firewall rules yet.</p>
                </CardContent>
              )}
              {(firewallsQuery.error || firewallDetailQueries.find((query) => query.error)?.error || deleteFirewallRuleMutation.error) && (
                <CardContent>
                  <ErrorMessage
                    error={
                      firewallsQuery.error ||
                      firewallDetailQueries.find((query) => query.error)?.error ||
                      deleteFirewallRuleMutation.error
                    }
                  />
                </CardContent>
              )}
            </Card>
          )}

          {canViewEvents && showEvents && (
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
        open={hostnameEditOpen}
        onClose={() => {
          if (!updateHostnameMutation.isPending) {
            setHostnameEditOpen(false);
          }
        }}
        title="Edit Hostname"
        maxWidth="lg"
      >
        <form onSubmit={confirmHostnameEdit} className="space-y-4">
          <Input
            label="Hostname"
            value={hostnameForm}
            onChange={(event) => setHostnameForm(event.target.value)}
            autoFocus
            autoComplete="off"
            required
          />
          {updateHostnameMutation.error && <ErrorMessage error={updateHostnameMutation.error} />}
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setHostnameEditOpen(false)} disabled={updateHostnameMutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" loading={updateHostnameMutation.isPending} disabled={hostnameForm.trim() === ''}>
              <Pencil className="h-4 w-4" />
              Save
            </Button>
          </div>
        </form>
      </Modal>
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
        open={firewallRuleOpen}
        onClose={() => {
          if (!firewallMutation.isPending) {
            setFirewallRuleOpen(false);
            setEditingFirewallRule(null);
            setFirewallRuleForm(defaultFirewallRuleForm);
          }
        }}
        title={editingFirewallRule ? 'Edit VPS Firewall Rule' : 'VPS Firewall Rule'}
        maxWidth="lg"
      >
        {vps && (
          <form onSubmit={createVPSFirewallRule} className="space-y-5">
            <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
              Rules created here are attached only to <span className="font-semibold text-slate-900">{vps.name}</span> and apply immediately.
            </div>
            {!primaryVPSFirewallGroup && !editingFirewallRule && (
              <>
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
              </>
            )}
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
            <Select
              label="State"
              value={firewallRuleForm.enabled ? 'enabled' : 'disabled'}
              onChange={(event) => setFirewallRuleForm({ ...firewallRuleForm, enabled: event.target.value === 'enabled' })}
            >
              <option value="enabled">Enabled</option>
              <option value="disabled">Disabled</option>
            </Select>
            {firewallMutation.error && <ErrorMessage error={firewallMutation.error} />}
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setFirewallRuleOpen(false);
                  setEditingFirewallRule(null);
                }}
                disabled={firewallMutation.isPending}
              >
                Cancel
              </Button>
              <Button type="submit" loading={firewallMutation.isPending}>
                <Shield className="h-4 w-4" />
                {editingFirewallRule ? 'Save and Apply' : 'Create and Apply'}
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

function formatCloudImageName(imagePath: string) {
  const filename = imagePath.split('/').pop()?.split(':').pop() || imagePath;
  const baseName = filename.replace(/\.(qcow2|img|raw|iso|vmdk)$/i, '');
  const normalized = baseName.replace(/[._]+/g, '-');

  const patterns: Array<[RegExp, (version: string) => string]> = [
    [/almalinux[- ]*(\d+(?:\.\d+)?)/i, (version) => `AlmaLinux ${version}`],
    [/rocky[- ]*(\d+(?:\.\d+)?)/i, (version) => `Rocky Linux ${version}`],
    [/centos[- ]*(\d+(?:\.\d+)?)/i, (version) => `CentOS ${version}`],
    [/ubuntu[- ]*(\d{2}\.\d{2}|\d+(?:\.\d+)?)/i, (version) => `Ubuntu ${version}`],
    [/debian[- ]*(\d+(?:\.\d+)?)/i, (version) => `Debian ${version}`],
    [/fedora[- ]*(\d+(?:\.\d+)?)/i, (version) => `Fedora ${version}`],
  ];

  for (const [pattern, label] of patterns) {
    const match = normalized.match(pattern);
    if (match?.[1]) return label(match[1]);
  }

  return baseName
    .replace(/[-_]+/g, ' ')
    .replace(/\b(genericcloud|generic|cloud|latest|x86|x64|amd64|64|qcow2)\b/gi, '')
    .replace(/\s+/g, ' ')
    .trim() || imagePath;
}
