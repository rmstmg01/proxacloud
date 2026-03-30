import { useEffect, useState, type FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { checkClusterName, provisionCluster } from '../api/clusters';
import { getNetworks } from '../api/networks';
import { getCredentials } from '../api/proxmox';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';

export function ProvisionClusterPage() {
  const navigate = useNavigate();
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: credentials } = useQuery({ queryKey: ['proxmox-credentials'], queryFn: getCredentials });
  const [debouncedName, setDebouncedName] = useState('');

  const [form, setForm] = useState({
    name: '',
    topology_type: 'tri',
    control_plane_count: 3,
    worker_count: 3,
    network_profile_id: 0,
    proxmox_credential_id: 0,
    node_ip_pool_start: '',
    kubernetes_version: 'v1.31.4',
    ubuntu_image: 'local:iso/ubuntu-24.04-server-cloudimg-amd64.img',
    ssh_public_key: '',
    control_plane_cpu: 4,
    control_plane_ram_mb: 8192,
    control_plane_disk_gb: 80,
    worker_cpu: 4,
    worker_ram_mb: 8192,
    worker_disk_gb: 120,
    apply: false,
  });

  const mutation = useMutation({ mutationFn: provisionCluster });

  useEffect(() => {
    const handle = setTimeout(() => {
      setDebouncedName(form.name.trim());
    }, 350);
    return () => clearTimeout(handle);
  }, [form.name]);

  const nameCheckQuery = useQuery({
    queryKey: ['cluster-name-check', debouncedName],
    queryFn: () => checkClusterName(debouncedName),
    enabled: debouncedName.length > 0,
    retry: 0,
  });

  const isNameTaken = nameCheckQuery.data?.exists ?? false;

  const setField = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (isNameTaken) return;
    const result = await mutation.mutateAsync({
      ...form,
      network_profile_id: Number(form.network_profile_id),
      proxmox_credential_id: Number(form.proxmox_credential_id),
    });
    navigate(`/clusters/${result.cluster_id}`);
  };

  return (
    <Layout>
      <PageHeader
        title="Provision Cluster"
        description="Create a new Kubernetes cluster on Proxmox"
        actions={
          <Link
            to="/clusters"
            className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm"
          >
            <ArrowLeft className="h-4 w-4" /> Back
          </Link>
        }
      />

      <form onSubmit={handleSubmit} className="space-y-6 max-w-3xl">
        {/* General */}
        <Card>
          <CardHeader><CardTitle>General</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Input
              label="Cluster name"
              value={form.name}
              onChange={(e) => setField('name', e.target.value)}
              error={isNameTaken ? 'Cluster name already exists' : undefined}
              helperText={
                debouncedName.length > 0 && !isNameTaken && !nameCheckQuery.isFetching
                  ? 'Cluster name is available'
                  : undefined
              }
              required
              placeholder="homelab-prod"
            />
            <Select
              label="Topology"
              value={form.topology_type}
              onChange={(e) => setField('topology_type', e.target.value)}
            >
              <option value="single">Single node</option>
              <option value="dual">Dual node</option>
              <option value="tri">Tri node (HA)</option>
            </Select>
            <Input
              label="Control plane count"
              type="number"
              min={1}
              value={form.control_plane_count}
              onChange={(e) => setField('control_plane_count', Number(e.target.value))}
              required
            />
            <Input
              label="Worker count"
              type="number"
              min={1}
              value={form.worker_count}
              onChange={(e) => setField('worker_count', Number(e.target.value))}
              required
            />
            <Select
              label="Network profile"
              value={form.network_profile_id}
              onChange={(e) => setField('network_profile_id', Number(e.target.value))}
            >
              <option value={0}>-- Select network --</option>
              {networks?.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.name} {n.is_default ? '(default)' : ''}
                </option>
              ))}
            </Select>
            <Select
              label="Proxmox credential"
              value={form.proxmox_credential_id}
              onChange={(e) => setField('proxmox_credential_id', Number(e.target.value))}
            >
              <option value={0}>-- Select credential --</option>
              {credentials?.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name} ({c.endpoint})
                </option>
              ))}
            </Select>
            <Input
              label="Kubernetes version"
              value={form.kubernetes_version}
              onChange={(e) => setField('kubernetes_version', e.target.value)}
              placeholder="v1.31.4"
              required
            />
            <Input
              label="Node IP pool start"
              value={form.node_ip_pool_start}
              onChange={(e) => setField('node_ip_pool_start', e.target.value)}
              placeholder="192.168.8.100"
              helperText="First IP to assign; each VM gets the next free address"
              required
            />
          </CardContent>
        </Card>

        {/* VM Image */}
        <Card>
          <CardHeader><CardTitle>VM Image &amp; SSH</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <Input
              label="Ubuntu image path"
              value={form.ubuntu_image}
              onChange={(e) => setField('ubuntu_image', e.target.value)}
              placeholder="local:iso/ubuntu-24.04-server-cloudimg-amd64.img"
              required
            />
            <Textarea
              label="SSH public key"
              value={form.ssh_public_key}
              onChange={(e) => setField('ssh_public_key', e.target.value)}
              rows={3}
              placeholder="ssh-ed25519 AAAA..."
              required
            />
          </CardContent>
        </Card>

        {/* Control plane resources */}
        <Card>
          <CardHeader><CardTitle>Control Plane Resources</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Input
              label="CPU cores"
              type="number"
              min={1}
              value={form.control_plane_cpu}
              onChange={(e) => setField('control_plane_cpu', Number(e.target.value))}
            />
            <Input
              label="RAM (MB)"
              type="number"
              min={1024}
              step={1024}
              value={form.control_plane_ram_mb}
              onChange={(e) => setField('control_plane_ram_mb', Number(e.target.value))}
            />
            <Input
              label="Disk (GB)"
              type="number"
              min={20}
              value={form.control_plane_disk_gb}
              onChange={(e) => setField('control_plane_disk_gb', Number(e.target.value))}
            />
          </CardContent>
        </Card>

        {/* Worker resources */}
        <Card>
          <CardHeader><CardTitle>Worker Resources</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Input
              label="CPU cores"
              type="number"
              min={1}
              value={form.worker_cpu}
              onChange={(e) => setField('worker_cpu', Number(e.target.value))}
            />
            <Input
              label="RAM (MB)"
              type="number"
              min={1024}
              step={1024}
              value={form.worker_ram_mb}
              onChange={(e) => setField('worker_ram_mb', Number(e.target.value))}
            />
            <Input
              label="Disk (GB)"
              type="number"
              min={20}
              value={form.worker_disk_gb}
              onChange={(e) => setField('worker_disk_gb', Number(e.target.value))}
            />
          </CardContent>
        </Card>

        {/* Apply toggle */}
        <Card>
          <CardContent>
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={form.apply}
                onChange={(e) => setField('apply', e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
              />
              <span className="text-sm text-slate-700">
                Apply immediately (run <code className="text-xs bg-slate-100 px-1 py-0.5 rounded">terraform apply</code>)
              </span>
            </label>
          </CardContent>
        </Card>

        {mutation.data && (
          <Alert variant="success" title="Cluster created!">
            <p>Cluster ID: <strong>{mutation.data.cluster_id}</strong> — Status: <strong>{mutation.data.status}</strong></p>
            {mutation.data.warnings.length > 0 && (
              <ul className="mt-2 list-disc list-inside text-amber-800">
                {mutation.data.warnings.map((w, i) => <li key={i}>{w}</li>)}
              </ul>
            )}
          </Alert>
        )}
        {mutation.error && <ErrorMessage error={mutation.error} />}
        {nameCheckQuery.error && <ErrorMessage error={nameCheckQuery.error} />}

        <div className="flex gap-3">
          <Button
            type="submit"
            loading={mutation.isPending}
            disabled={isNameTaken || nameCheckQuery.isFetching}
            size="lg"
          >
            Provision Cluster
          </Button>
          <Button type="button" variant="secondary" size="lg" onClick={() => navigate('/clusters')}>
            Cancel
          </Button>
        </div>
      </form>
    </Layout>
  );
}
