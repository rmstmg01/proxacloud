import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Shield, Trash2, Zap } from 'lucide-react';
import {
  applyFirewall,
  createFirewall,
  createFirewallAttachment,
  createFirewallRule,
  deleteFirewall,
  deleteFirewallAttachment,
  deleteFirewallRule,
  getFirewall,
  getFirewallEvents,
  getFirewalls,
} from '../api/firewalls';
import { getContainerInstances } from '../api/containers';
import { getVPSInstances } from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Badge, StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input, Select } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { PageSpinner } from '../components/ui/Spinner';
import type {
  FirewallAction,
  FirewallDirection,
  FirewallPolicy,
  FirewallProtocol,
  FirewallTargetType,
} from '../types';

const defaultGroupForm = {
  name: '',
  description: '',
  default_inbound_policy: 'deny' as FirewallPolicy,
  default_outbound_policy: 'allow' as FirewallPolicy,
  enabled: true,
};

const defaultRuleForm = {
  direction: 'inbound' as FirewallDirection,
  action: 'allow' as FirewallAction,
  protocol: 'tcp' as FirewallProtocol,
  source_cidr: '0.0.0.0/0',
  destination_cidr: '',
  port_start: '22',
  port_end: '22',
  priority: '100',
  description: 'SSH access',
  enabled: true,
};

export function FirewallsPage() {
  const qc = useQueryClient();
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [ruleOpen, setRuleOpen] = useState(false);
  const [attachOpen, setAttachOpen] = useState(false);
  const [groupForm, setGroupForm] = useState(defaultGroupForm);
  const [ruleForm, setRuleForm] = useState(defaultRuleForm);
  const [attachForm, setAttachForm] = useState({
    target_type: 'vps' as FirewallTargetType,
    target_id: '',
  });

  const firewallsQuery = useQuery({ queryKey: ['firewalls'], queryFn: getFirewalls });
  const selectedFirewallQuery = useQuery({
    queryKey: ['firewalls', selectedID],
    queryFn: () => getFirewall(selectedID as number),
    enabled: selectedID !== null,
  });
  const eventsQuery = useQuery({
    queryKey: ['firewalls', selectedID, 'events'],
    queryFn: () => getFirewallEvents(selectedID as number),
    enabled: selectedID !== null,
  });
  const vpsQuery = useQuery({ queryKey: ['vps'], queryFn: getVPSInstances });
  const containersQuery = useQuery({ queryKey: ['containers'], queryFn: getContainerInstances });

  const firewalls = firewallsQuery.data ?? [];
  const firewall = selectedFirewallQuery.data;

  useEffect(() => {
    if (selectedID === null && firewalls.length > 0) {
      setSelectedID(firewalls[0].id);
    }
    if (selectedID !== null && firewalls.length > 0 && !firewalls.some((item) => item.id === selectedID)) {
      setSelectedID(firewalls[0].id);
    }
  }, [firewalls, selectedID]);

  const targetOptions = useMemo(() => {
    const vps = (vpsQuery.data ?? []).map((item) => ({
      value: `vps:${item.id}`,
      label: `VPS / ${item.name}`,
    }));
    const containers = (containersQuery.data ?? []).map((item) => ({
      value: `container:${item.id}`,
      label: `Container / ${item.name}`,
    }));
    return [...vps, ...containers];
  }, [containersQuery.data, vpsQuery.data]);

  const refreshSelected = () => {
    qc.invalidateQueries({ queryKey: ['firewalls'] });
    if (selectedID !== null) {
      qc.invalidateQueries({ queryKey: ['firewalls', selectedID] });
      qc.invalidateQueries({ queryKey: ['firewalls', selectedID, 'events'] });
    }
  };

  const createMut = useMutation({
    mutationFn: createFirewall,
    onSuccess: (created) => {
      qc.invalidateQueries({ queryKey: ['firewalls'] });
      setSelectedID(created.id);
      setCreateOpen(false);
      setGroupForm(defaultGroupForm);
    },
  });

  const deleteMut = useMutation({
    mutationFn: deleteFirewall,
    onSuccess: () => {
      setSelectedID(null);
      qc.invalidateQueries({ queryKey: ['firewalls'] });
    },
  });

  const ruleMut = useMutation({
    mutationFn: () => {
      if (selectedID === null) throw new Error('select a firewall first');
      const protocol = ruleForm.protocol;
      const hasPorts = protocol === 'tcp' || protocol === 'udp';
      return createFirewallRule(selectedID, {
        direction: ruleForm.direction,
        action: ruleForm.action,
        protocol,
        source_cidr: ruleForm.source_cidr.trim(),
        destination_cidr: ruleForm.destination_cidr.trim(),
        port_start: hasPorts && ruleForm.port_start ? Number(ruleForm.port_start) : undefined,
        port_end: hasPorts && ruleForm.port_end ? Number(ruleForm.port_end) : undefined,
        priority: Number(ruleForm.priority),
        description: ruleForm.description,
        enabled: ruleForm.enabled,
      });
    },
    onSuccess: () => {
      setRuleOpen(false);
      setRuleForm(defaultRuleForm);
      refreshSelected();
    },
  });

  const deleteRuleMut = useMutation({
    mutationFn: ({ firewallID, ruleID }: { firewallID: number; ruleID: number }) => deleteFirewallRule(firewallID, ruleID),
    onSuccess: refreshSelected,
  });

  const attachMut = useMutation({
    mutationFn: () => {
      if (selectedID === null) throw new Error('select a firewall first');
      return createFirewallAttachment(selectedID, {
        target_type: attachForm.target_type,
        target_id: Number(attachForm.target_id),
      });
    },
    onSuccess: () => {
      setAttachOpen(false);
      setAttachForm({ target_type: 'vps', target_id: '' });
      refreshSelected();
    },
  });

  const deleteAttachmentMut = useMutation({
    mutationFn: ({ firewallID, attachmentID }: { firewallID: number; attachmentID: number }) =>
      deleteFirewallAttachment(firewallID, attachmentID),
    onSuccess: refreshSelected,
  });

  const applyMut = useMutation({
    mutationFn: () => {
      if (selectedID === null) throw new Error('select a firewall first');
      return applyFirewall(selectedID);
    },
    onSuccess: refreshSelected,
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    createMut.mutate(groupForm);
  };

  const handleCreateRule = (e: FormEvent) => {
    e.preventDefault();
    ruleMut.mutate();
  };

  const handleAttach = (e: FormEvent) => {
    e.preventDefault();
    attachMut.mutate();
  };

  const handleTargetSelection = (value: string) => {
    const [targetType, targetID] = value.split(':');
    setAttachForm({
      target_type: targetType as FirewallTargetType,
      target_id: targetID,
    });
  };

  const selectedTargetValue = attachForm.target_id ? `${attachForm.target_type}:${attachForm.target_id}` : '';

  return (
    <Layout>
      <PageHeader
        title="Firewalls"
        description="Manage reusable firewall groups, rules, and compute attachments"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="h-4 w-4" /> New Firewall
          </Button>
        }
      />

      {firewallsQuery.isLoading && <PageSpinner />}
      {firewallsQuery.error && <ErrorMessage error={firewallsQuery.error} />}

      {!firewallsQuery.isLoading && firewalls.length === 0 && (
        <Card>
          <CardContent>
            <p className="text-sm text-slate-500">No firewall groups created yet.</p>
          </CardContent>
        </Card>
      )}

      {firewalls.length > 0 && (
        <div className="grid gap-5 xl:grid-cols-[320px_minmax(0,1fr)]">
          <Card>
            <CardHeader>
              <CardTitle>Firewall Groups</CardTitle>
            </CardHeader>
            <div className="divide-y divide-slate-100">
              {firewalls.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setSelectedID(item.id)}
                  className={`w-full px-5 py-4 text-left transition-colors ${
                    selectedID === item.id ? 'bg-indigo-50' : 'hover:bg-slate-50'
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-slate-900">{item.name}</p>
                      <p className="mt-1 truncate text-xs text-slate-500">{item.description || 'No description'}</p>
                    </div>
                    <StatusBadge status={item.status} />
                  </div>
                  <div className="mt-3 flex gap-2">
                    <Badge variant={item.enabled ? 'success' : 'default'}>{item.enabled ? 'Enabled' : 'Disabled'}</Badge>
                    <Badge>In {item.default_inbound_policy}</Badge>
                    <Badge>Out {item.default_outbound_policy}</Badge>
                  </div>
                </button>
              ))}
            </div>
          </Card>

          <div className="space-y-5">
            {selectedFirewallQuery.isLoading && <PageSpinner />}
            {selectedFirewallQuery.error && <ErrorMessage error={selectedFirewallQuery.error} />}
            {firewall && (
              <>
                <Card>
                  <CardHeader className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <div>
                      <div className="flex items-center gap-2">
                        <Shield className="h-5 w-5 text-indigo-600" />
                        <CardTitle>{firewall.name}</CardTitle>
                        <StatusBadge status={firewall.status} />
                      </div>
                      <p className="mt-1 text-sm text-slate-500">{firewall.description || 'No description'}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                      <Button variant="secondary" onClick={() => setRuleOpen(true)}>
                        <Plus className="h-4 w-4" /> Rule
                      </Button>
                      <Button variant="secondary" onClick={() => setAttachOpen(true)}>
                        <Plus className="h-4 w-4" /> Attach
                      </Button>
                      <Button onClick={() => applyMut.mutate()} loading={applyMut.isPending}>
                        <Zap className="h-4 w-4" /> Apply
                      </Button>
                      <Button
                        variant="danger"
                        onClick={() => {
                          if (window.confirm(`Delete firewall "${firewall.name}"?`)) {
                            deleteMut.mutate(firewall.id);
                          }
                        }}
                        loading={deleteMut.isPending && deleteMut.variables === firewall.id}
                      >
                        <Trash2 className="h-4 w-4" /> Delete
                      </Button>
                    </div>
                  </CardHeader>
                  <CardContent>
                    <div className="grid gap-4 md:grid-cols-4">
                      <Summary label="Inbound" value={firewall.default_inbound_policy} />
                      <Summary label="Outbound" value={firewall.default_outbound_policy} />
                      <Summary label="Rules" value={String(firewall.rules?.length ?? 0)} />
                      <Summary label="Attachments" value={String(firewall.attachments?.length ?? 0)} />
                    </div>
                    {applyMut.data && (
                      <Alert variant="warning" title="Apply queued" className="mt-4">
                        {applyMut.data.message}
                      </Alert>
                    )}
                    {applyMut.error && <ErrorMessage error={applyMut.error} />}
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle>Rules</CardTitle>
                  </CardHeader>
                  {firewall.rules?.length ? (
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
                          {firewall.rules.map((rule) => (
                            <tr key={rule.id} className="hover:bg-slate-50">
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
                                {rule.port_start ? `${rule.port_start}-${rule.port_end}` : 'any'}
                              </td>
                              <td className="max-w-xs truncate px-5 py-3 text-slate-600">{rule.description || '-'}</td>
                              <td className="px-5 py-3 text-right">
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => deleteRuleMut.mutate({ firewallID: firewall.id, ruleID: rule.id })}
                                  loading={deleteRuleMut.isPending && deleteRuleMut.variables?.ruleID === rule.id}
                                >
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : (
                    <CardContent>
                      <p className="text-sm text-slate-500">No firewall rules yet.</p>
                    </CardContent>
                  )}
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle>Attachments</CardTitle>
                  </CardHeader>
                  {firewall.attachments?.length ? (
                    <div className="divide-y divide-slate-100">
                      {firewall.attachments.map((attachment) => (
                        <div key={attachment.id} className="flex items-center justify-between gap-3 px-5 py-4">
                          <div>
                            <p className="text-sm font-medium text-slate-900">
                              {attachment.target_name || `${attachment.target_type}:${attachment.target_id}`}
                            </p>
                            <p className="text-xs text-slate-500">{attachment.target_type}</p>
                          </div>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() =>
                              deleteAttachmentMut.mutate({ firewallID: firewall.id, attachmentID: attachment.id })
                            }
                            loading={
                              deleteAttachmentMut.isPending &&
                              deleteAttachmentMut.variables?.attachmentID === attachment.id
                            }
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <CardContent>
                      <p className="text-sm text-slate-500">No compute targets attached.</p>
                    </CardContent>
                  )}
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle>Events</CardTitle>
                  </CardHeader>
                  <div className="divide-y divide-slate-100">
                    {(eventsQuery.data ?? []).slice(0, 8).map((event) => (
                      <div key={event.id} className="px-5 py-3">
                        <div className="flex items-center justify-between gap-3">
                          <p className="text-sm font-medium text-slate-900">{event.message}</p>
                          <Badge variant={event.level === 'warning' ? 'warning' : 'default'}>{event.action}</Badge>
                        </div>
                        <p className="mt-1 text-xs text-slate-500">{new Date(event.created_at).toLocaleString()}</p>
                      </div>
                    ))}
                    {!eventsQuery.isLoading && (eventsQuery.data ?? []).length === 0 && (
                      <CardContent>
                        <p className="text-sm text-slate-500">No events recorded yet.</p>
                      </CardContent>
                    )}
                  </div>
                </Card>
              </>
            )}
          </div>
        </div>
      )}

      <Modal open={createOpen} onClose={() => setCreateOpen(false)} title="Create Firewall">
        <form onSubmit={handleCreate} className="space-y-4">
          <Input
            label="Name"
            value={groupForm.name}
            onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })}
            placeholder="web-public"
            required
          />
          <Input
            label="Description"
            value={groupForm.description}
            onChange={(e) => setGroupForm({ ...groupForm, description: e.target.value })}
            placeholder="Public web access with SSH"
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <Select
              label="Inbound Policy"
              value={groupForm.default_inbound_policy}
              onChange={(e) => setGroupForm({ ...groupForm, default_inbound_policy: e.target.value as FirewallPolicy })}
            >
              <option value="deny">Deny</option>
              <option value="allow">Allow</option>
            </Select>
            <Select
              label="Outbound Policy"
              value={groupForm.default_outbound_policy}
              onChange={(e) => setGroupForm({ ...groupForm, default_outbound_policy: e.target.value as FirewallPolicy })}
            >
              <option value="allow">Allow</option>
              <option value="deny">Deny</option>
            </Select>
          </div>
          {createMut.error && <ErrorMessage error={createMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={createMut.isPending}>Create</Button>
            <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>

      <Modal open={ruleOpen} onClose={() => setRuleOpen(false)} title="Add Firewall Rule">
        <form onSubmit={handleCreateRule} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-3">
            <Select
              label="Direction"
              value={ruleForm.direction}
              onChange={(e) => setRuleForm({ ...ruleForm, direction: e.target.value as FirewallDirection })}
            >
              <option value="inbound">Inbound</option>
              <option value="outbound">Outbound</option>
            </Select>
            <Select
              label="Action"
              value={ruleForm.action}
              onChange={(e) => setRuleForm({ ...ruleForm, action: e.target.value as FirewallAction })}
            >
              <option value="allow">Allow</option>
              <option value="deny">Deny</option>
            </Select>
            <Select
              label="Protocol"
              value={ruleForm.protocol}
              onChange={(e) => setRuleForm({ ...ruleForm, protocol: e.target.value as FirewallProtocol })}
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
              value={ruleForm.source_cidr}
              onChange={(e) => setRuleForm({ ...ruleForm, source_cidr: e.target.value })}
              placeholder="0.0.0.0/0"
            />
            <Input
              label="Destination CIDR"
              value={ruleForm.destination_cidr}
              onChange={(e) => setRuleForm({ ...ruleForm, destination_cidr: e.target.value })}
              placeholder="10.0.0.0/24"
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <Input
              label="Port Start"
              type="number"
              min="1"
              max="65535"
              value={ruleForm.port_start}
              disabled={ruleForm.protocol === 'icmp' || ruleForm.protocol === 'any'}
              onChange={(e) => setRuleForm({ ...ruleForm, port_start: e.target.value })}
            />
            <Input
              label="Port End"
              type="number"
              min="1"
              max="65535"
              value={ruleForm.port_end}
              disabled={ruleForm.protocol === 'icmp' || ruleForm.protocol === 'any'}
              onChange={(e) => setRuleForm({ ...ruleForm, port_end: e.target.value })}
            />
            <Input
              label="Priority"
              type="number"
              min="1"
              value={ruleForm.priority}
              onChange={(e) => setRuleForm({ ...ruleForm, priority: e.target.value })}
              required
            />
          </div>
          <Input
            label="Description"
            value={ruleForm.description}
            onChange={(e) => setRuleForm({ ...ruleForm, description: e.target.value })}
            placeholder="Allow SSH"
          />
          {ruleMut.error && <ErrorMessage error={ruleMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={ruleMut.isPending}>Add Rule</Button>
            <Button type="button" variant="secondary" onClick={() => setRuleOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>

      <Modal open={attachOpen} onClose={() => setAttachOpen(false)} title="Attach Target">
        <form onSubmit={handleAttach} className="space-y-4">
          <Select
            label="Compute Target"
            value={selectedTargetValue}
            onChange={(e) => handleTargetSelection(e.target.value)}
            required
          >
            <option value="">Select a target</option>
            {targetOptions.map((target) => (
              <option key={target.value} value={target.value}>{target.label}</option>
            ))}
          </Select>
          {attachMut.error && <ErrorMessage error={attachMut.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={attachMut.isPending}>Attach</Button>
            <Button type="button" variant="secondary" onClick={() => setAttachOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>
    </Layout>
  );
}

function Summary({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-slate-200 bg-slate-50 px-4 py-3">
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-1 text-sm font-semibold text-slate-900">{value}</p>
    </div>
  );
}
