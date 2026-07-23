import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Archive, CalendarClock, Play, Settings, ShieldCheck, ShieldOff } from 'lucide-react';
import {
  disableVPSBackupPolicy,
  getVPSBackupOverview,
  runVPSBackupNow,
  saveVPSBackupPolicy,
  type VPSBackupPlan,
  type VPSBackupPolicyInput,
} from '../api/backups';
import { Alert, ErrorMessage } from './ui/Alert';
import { Badge, StatusBadge } from './ui/Badge';
import { Button } from './ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from './ui/Card';
import { Input, Select } from './ui/Input';
import { Modal } from './ui/Modal';

interface Props {
  vpsID: number;
  diskGB: number;
}

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function defaultTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
}

function initialForm(): VPSBackupPolicyInput {
  return {
    plan_code: 'vps_backup_weekly',
    weekday: 0,
    month_day: 1,
    local_time: '02:00',
    timezone: defaultTimezone(),
  };
}

export function VPSBackupProtection({ vpsID, diskGB }: Props) {
  const queryClient = useQueryClient();
  const [editorOpen, setEditorOpen] = useState(false);
  const [manualOpen, setManualOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [form, setForm] = useState<VPSBackupPolicyInput>(initialForm);

  const query = useQuery({
    queryKey: ['vps', vpsID, 'backup'],
    queryFn: () => getVPSBackupOverview(vpsID),
    refetchInterval: (result) => {
      const jobs = result.state.data?.jobs ?? [];
      return jobs.some((job) => ['pending', 'running', 'uploading'].includes(job.status)) ? 5_000 : 30_000;
    },
  });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['vps', vpsID, 'backup'] });
    queryClient.invalidateQueries({ queryKey: ['billing'] });
  };

  const saveMutation = useMutation({
    mutationFn: () => saveVPSBackupPolicy(vpsID, form),
    onSuccess: () => {
      setEditorOpen(false);
      refresh();
    },
  });
  const runMutation = useMutation({
    mutationFn: () => runVPSBackupNow(vpsID),
    onSuccess: () => {
      setManualOpen(false);
      refresh();
    },
  });
  const disableMutation = useMutation({
    mutationFn: () => disableVPSBackupPolicy(vpsID),
    onSuccess: () => {
      setDisableOpen(false);
      refresh();
    },
  });

  const policy = query.data?.policy;
  const enabledPlans = (query.data?.plans ?? []).filter((plan) => plan.enabled);
  const selectedPlan = enabledPlans.find((plan) => plan.code === form.plan_code);
  const selectedPrice = selectedPlan
    ? selectedPlan.base_monthly_cents + selectedPlan.disk_gb_monthly_cents * diskGB
    : 0;
  const activeJob = query.data?.jobs.find((job) => ['pending', 'running', 'uploading'].includes(job.status));

  useEffect(() => {
    if (!editorOpen) return;
    if (policy) {
      setForm({
        plan_code: policy.plan_code,
        weekday: policy.weekday ?? 0,
        month_day: policy.month_day ?? 1,
        local_time: policy.local_time.slice(0, 5),
        timezone: policy.timezone,
      });
      return;
    }
    const first = enabledPlans[0];
    setForm({
      ...initialForm(),
      plan_code: first?.code ?? 'vps_backup_weekly',
    });
  }, [editorOpen, policy, enabledPlans.length]);

  const scheduleDescription = useMemo(() => {
    if (!policy) return '';
    if (policy.frequency === 'weekly') {
      return `Every ${weekdays[policy.weekday ?? 0]} at ${policy.local_time}`;
    }
    if (policy.frequency === 'monthly') {
      return `Day ${policy.month_day ?? 1} of every month at ${policy.local_time}`;
    }
    return `Every day at ${policy.local_time}`;
  }, [policy]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    saveMutation.mutate();
  };

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Backup Protection</CardTitle>
              <Badge variant={policy?.enabled ? 'success' : 'default'}>
                {policy?.enabled ? 'Enabled' : 'Not enabled'}
              </Badge>
            </div>
            <p className="mt-1 text-sm text-slate-500">
              Scheduled Proxmox VM backups with paid retention protection.
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            {policy?.enabled ? (
              <>
                <Button variant="secondary" onClick={() => setEditorOpen(true)}>
                  <Settings className="h-4 w-4" /> Edit schedule
                </Button>
                <Button
                  variant="primary"
                  onClick={() => setManualOpen(true)}
                  disabled={Boolean(activeJob)}
                >
                  <Play className="h-4 w-4" /> Backup now
                </Button>
              </>
            ) : (
              <Button variant="primary" onClick={() => setEditorOpen(true)} disabled={!enabledPlans.length}>
                <ShieldCheck className="h-4 w-4" /> Enable backup
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {query.isLoading ? (
            <p className="text-sm text-slate-500">Loading backup protection...</p>
          ) : query.error ? (
            <ErrorMessage error={query.error} />
          ) : policy?.enabled ? (
            <div className="space-y-5">
              {policy.last_error && <Alert variant="error" title="Last backup error">{policy.last_error}</Alert>}
              {activeJob && (
                <Alert variant="info" title="Backup in progress">
                  Proxmox is processing a {activeJob.trigger_type} backup. Another backup cannot start until it finishes.
                </Alert>
              )}
              <dl className="grid grid-cols-1 gap-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
                <Detail label="Plan" value={`${capitalize(policy.frequency)} backup`} />
                <Detail label="Price" value={`${money(policy.monthly_price_cents)}/month`} />
                <Detail label="Schedule" value={`${scheduleDescription} (${policy.timezone})`} />
                <Detail label="Retention" value={`Last ${policy.retention_count} backups`} />
                <Detail label="Next backup" value={formatDate(policy.next_run_at)} />
                <Detail label="Last successful" value={formatDate(policy.last_success_at)} />
                <Detail label="Target" value={policy.target_name} />
                <Detail label="Mode" value={capitalize(policy.mode)} />
              </dl>

              <div>
                <div className="mb-3 flex items-center justify-between gap-3">
                  <h3 className="text-sm font-semibold text-slate-900">Recent backups</h3>
                  <Button variant="ghost" size="sm" onClick={() => query.refetch()}>
                    Refresh
                  </Button>
                </div>
                {query.data?.jobs.length ? (
                  <div className="overflow-x-auto rounded-md border border-slate-200">
                    <table className="w-full text-sm">
                      <thead className="bg-slate-50 text-left text-slate-600">
                        <tr>
                          <th className="px-4 py-2 font-medium">Started</th>
                          <th className="px-4 py-2 font-medium">Trigger</th>
                          <th className="px-4 py-2 font-medium">Status</th>
                          <th className="px-4 py-2 font-medium">Task</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-slate-100">
                        {query.data.jobs.map((job) => (
                          <tr key={job.id}>
                            <td className="px-4 py-3 text-slate-600">{formatDate(job.started_at || job.created_at)}</td>
                            <td className="px-4 py-3 text-slate-600">{capitalize(job.trigger_type)}</td>
                            <td className="px-4 py-3"><StatusBadge status={job.status} /></td>
                            <td className="max-w-xs truncate px-4 py-3 text-xs text-slate-500">
                              {job.error || job.proxmox_task_id || 'Preparing'}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <p className="text-sm text-slate-500">No backups have run yet.</p>
                )}
              </div>

              <div className="border-t border-slate-200 pt-4">
                <Button variant="ghost" onClick={() => setDisableOpen(true)}>
                  <ShieldOff className="h-4 w-4" /> Disable backup protection
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="grid gap-3 md:grid-cols-3">
                {enabledPlans.map((plan) => (
                  <div key={plan.code} className="rounded-lg border border-slate-200 bg-slate-50 p-4">
                    <div className="flex items-center gap-2">
                      <CalendarClock className="h-4 w-4 text-indigo-600" />
                      <p className="font-medium text-slate-900">{capitalize(plan.frequency)}</p>
                    </div>
                    <p className="mt-2 text-xl font-semibold text-slate-900">{money(planPrice(plan, diskGB))}<span className="text-xs font-normal text-slate-500">/month</span></p>
                    <p className="mt-1 text-xs text-slate-500">Keep the last {plan.retention_count} backups</p>
                  </div>
                ))}
              </div>
              {!enabledPlans.length && (
                <Alert variant="warning" title="No backup plans available">
                  Ask an administrator to enable a VPS backup price and configure a Proxmox backup target.
                </Alert>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      <Modal open={editorOpen} onClose={() => !saveMutation.isPending && setEditorOpen(false)} title={policy?.enabled ? 'Edit Backup Schedule' : 'Enable Backup Protection'} maxWidth="lg">
        <form onSubmit={submit} className="space-y-4">
          <Select label="Backup plan" value={form.plan_code} onChange={(event) => setForm({ ...form, plan_code: event.target.value as VPSBackupPolicyInput['plan_code'] })}>
            {enabledPlans.map((plan) => <option key={plan.code} value={plan.code}>{capitalize(plan.frequency)} — {money(planPrice(plan, diskGB))}/month</option>)}
          </Select>
          {selectedPlan?.frequency === 'weekly' && (
            <Select label="Day of week" value={form.weekday ?? 0} onChange={(event) => setForm({ ...form, weekday: Number(event.target.value) })}>
              {weekdays.map((day, index) => <option key={day} value={index}>{day}</option>)}
            </Select>
          )}
          {selectedPlan?.frequency === 'monthly' && (
            <Input label="Day of month" type="number" min={1} max={28} value={form.month_day ?? 1} onChange={(event) => setForm({ ...form, month_day: Number(event.target.value) })} required />
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Input label="Backup time" type="time" value={form.local_time} onChange={(event) => setForm({ ...form, local_time: event.target.value })} required />
            <Input label="Timezone" value={form.timezone} onChange={(event) => setForm({ ...form, timezone: event.target.value })} helperText="IANA timezone, for example Asia/Kathmandu" required />
          </div>
          <Alert variant="info" title="Additional monthly charge">
            This adds {money(selectedPrice)} per month for this {diskGB} GB VPS and retains the last {selectedPlan?.retention_count ?? 0} backups.
          </Alert>
          {saveMutation.error && <ErrorMessage error={saveMutation.error} />}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setEditorOpen(false)}>Cancel</Button>
            <Button type="submit" loading={saveMutation.isPending}><Archive className="h-4 w-4" /> Save and enable</Button>
          </div>
        </form>
      </Modal>

      <Modal open={manualOpen} onClose={() => !runMutation.isPending && setManualOpen(false)} title="Take Backup Now" maxWidth="lg">
        <div className="space-y-4">
          <Alert variant="warning" title="Start a full VM backup">
            Proxmox will create a snapshot-mode backup now. Only one backup can run for this VPS at a time.
          </Alert>
          {runMutation.error && <ErrorMessage error={runMutation.error} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setManualOpen(false)}>Cancel</Button>
            <Button onClick={() => runMutation.mutate()} loading={runMutation.isPending}><Play className="h-4 w-4" /> Start backup</Button>
          </div>
        </div>
      </Modal>

      <Modal open={disableOpen} onClose={() => !disableMutation.isPending && setDisableOpen(false)} title="Disable Backup Protection" maxWidth="lg">
        <div className="space-y-4">
          <Alert variant="warning" title="Scheduled backups and future charges will stop">
            Existing backup files remain on the configured Proxmox storage until its retention policy removes them.
          </Alert>
          {disableMutation.error && <ErrorMessage error={disableMutation.error} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setDisableOpen(false)}>Cancel</Button>
            <Button variant="danger" onClick={() => disableMutation.mutate()} loading={disableMutation.isPending}>Disable backup</Button>
          </div>
        </div>
      </Modal>
    </>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return <div><dt className="text-xs uppercase tracking-wide text-slate-500">{label}</dt><dd className="mt-1 font-medium text-slate-900">{value}</dd></div>;
}

function planPrice(plan: VPSBackupPlan, diskGB: number) {
  return plan.base_monthly_cents + plan.disk_gb_monthly_cents * diskGB;
}

function money(cents: number) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100);
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : 'Not yet';
}

function capitalize(value: string) {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : '';
}
