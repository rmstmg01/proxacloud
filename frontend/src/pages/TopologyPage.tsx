import { useState, type FormEvent } from 'react';
import { useMutation } from '@tanstack/react-query';
import { planTopology } from '../api/topology';
import type { TopologyType, PlanResponse } from '../types';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Badge } from '../components/ui/Badge';

function PlanResult({ plan }: { plan: PlanResponse }) {
  return (
    <div className="space-y-4">
      {/* Summary */}
      <Card>
        <CardHeader><CardTitle>Plan Summary</CardTitle></CardHeader>
        <div className="divide-y divide-slate-100">
          {([
            ['Topology type', plan.topology_type],
            ['Available Proxmox nodes', String(plan.available_nodes)],
            ['Required Proxmox nodes', String(plan.requested_nodes)],
            ['Control planes', String(plan.control_plane_count)],
            ['Workers', String(plan.worker_count)],
            ['Selected nodes', plan.selected_proxmox_nodes.join(', ')],
          ] as [string, string][]).map(([label, value]) => (
            <div key={label} className="flex items-center px-6 py-3">
              <span className="w-52 text-sm text-slate-500 font-medium flex-shrink-0">{label}</span>
              <span className="text-sm text-slate-900">{value}</span>
            </div>
          ))}
        </div>
      </Card>

      {/* Warnings */}
      {plan.warnings.length > 0 && (
        <Alert variant="warning" title="Warnings">
          <ul className="list-disc list-inside space-y-1">
            {plan.warnings.map((w, i) => <li key={i}>{w}</li>)}
          </ul>
        </Alert>
      )}

      {/* Placement table */}
      <Card>
        <CardHeader><CardTitle>VM Placement</CardTitle></CardHeader>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200 bg-slate-50">
                <th className="text-left px-6 py-3 font-medium text-slate-600">Role</th>
                <th className="text-left px-6 py-3 font-medium text-slate-600">Index</th>
                <th className="text-left px-6 py-3 font-medium text-slate-600">Proxmox Node</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {plan.placements.map((p) => (
                <tr key={`${p.role}-${p.index}`}>
                  <td className="px-6 py-3">
                    <Badge variant={p.role === 'control-plane' ? 'info' : 'default'}>
                      {p.role}
                    </Badge>
                  </td>
                  <td className="px-6 py-3 text-slate-600">{p.index}</td>
                  <td className="px-6 py-3 font-medium text-slate-900">{p.proxmox_node_name}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

export function TopologyPage() {
  const [form, setForm] = useState({
    topology_type: 'tri' as TopologyType,
    control_plane_count: 3,
    worker_count: 2,
  });

  const mutation = useMutation({ mutationFn: planTopology });

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    mutation.mutate(form);
  };

  return (
    <Layout>
      <PageHeader
        title="Topology Planner"
        description="Preview VM placement across Proxmox nodes before provisioning"
      />

      <div className="max-w-2xl space-y-6">
        <Card>
          <CardHeader><CardTitle>Plan Parameters</CardTitle></CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-4">
              <Select
                label="Topology type"
                value={form.topology_type}
                onChange={(e) => setForm({ ...form, topology_type: e.target.value as TopologyType })}
              >
                <option value="single">Single (1 Proxmox node required)</option>
                <option value="dual">Dual (2 Proxmox nodes required)</option>
                <option value="tri">Tri / HA (3 Proxmox nodes required)</option>
              </Select>
              <Input
                label="Control plane count"
                type="number"
                min={1}
                value={form.control_plane_count}
                onChange={(e) => setForm({ ...form, control_plane_count: Number(e.target.value) })}
              />
              <Input
                label="Worker count"
                type="number"
                min={1}
                value={form.worker_count}
                onChange={(e) => setForm({ ...form, worker_count: Number(e.target.value) })}
              />
              {mutation.error && <ErrorMessage error={mutation.error} />}
              <Button type="submit" loading={mutation.isPending}>Plan Topology</Button>
            </form>
          </CardContent>
        </Card>

        {mutation.data && <PlanResult plan={mutation.data} />}
      </div>
    </Layout>
  );
}
