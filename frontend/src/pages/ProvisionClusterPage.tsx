import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, CheckCircle2, Plus } from 'lucide-react';
import { checkClusterName, provisionAndBootstrapCluster } from '../api/clusters';
import { createCustomerSSHKey, getCustomerSSHKeys } from '../api/customerSshKeys';
import { discoverVPSCloudInitImagesAutomatically } from '../api/vps';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { architectureLabel, DistributionLogo } from '../components/DistributionLogo';

export function ProvisionClusterPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: sshKeys } = useQuery({ queryKey: ['customer-ssh-keys'], queryFn: getCustomerSSHKeys });
  const { data: images, error: imageDiscoveryError, isLoading: imagesLoading } = useQuery({
    queryKey: ['cluster-provision-images'],
    queryFn: () => discoverVPSCloudInitImagesAutomatically(),
    staleTime: 0,
    refetchOnMount: 'always',
  });
  const sharedImages = useMemo(
    () => (images ?? [])
      .filter((image) => image.image_path.toLowerCase().startsWith('cloud-images:import/'))
      .filter((image) => image.distribution === 'ubuntu' || (!image.distribution && image.os_type === 'ubuntu'))
      .sort((a, b) => (a.version || a.name).localeCompare(b.version || b.name, undefined, { numeric: true })),
    [images],
  );
  const [debouncedName, setDebouncedName] = useState('');
  const [sshModalOpen, setSSHModalOpen] = useState(false);
  const [sshForm, setSSHForm] = useState({ name: '', ssh_user: 'root', public_key: '' });
  const [form, setForm] = useState({
    name: '', topology_type: 'tri', control_plane_count: 3, worker_count: 3,
    kubernetes_version: 'v1.31.4', cloud_init_image_id: 0, customer_ssh_key_id: 0,
    control_plane_cpu: 2, control_plane_ram_mb: 2048, control_plane_disk_gb: 80,
    worker_cpu: 2, worker_ram_mb: 2048, worker_disk_gb: 120,
  });

  useEffect(() => {
    const preferred = sharedImages.find((image) => image.is_default) ?? sharedImages[0];
    const selectionStillExists = sharedImages.some((image) => image.id === form.cloud_init_image_id);
    if (!selectionStillExists) {
      setForm((prev) => ({ ...prev, cloud_init_image_id: preferred?.id ?? 0 }));
    }
  }, [sharedImages, form.cloud_init_image_id]);
  useEffect(() => {
    const handle = window.setTimeout(() => setDebouncedName(form.name.trim()), 350);
    return () => window.clearTimeout(handle);
  }, [form.name]);
  const nameCheck = useQuery({
    queryKey: ['cluster-name-check', debouncedName], queryFn: () => checkClusterName(debouncedName),
    enabled: debouncedName.length > 0, retry: 0,
  });
  const isNameTaken = nameCheck.data?.exists ?? false;
  const selectedKey = sshKeys?.find((key) => key.id === form.customer_ssh_key_id);
  const selectedImage = sharedImages.find((image) => image.id === form.cloud_init_image_id);

  const createSSH = useMutation({
    mutationFn: createCustomerSSHKey,
    onSuccess: (key) => {
      queryClient.invalidateQueries({ queryKey: ['customer-ssh-keys'] });
      setForm((prev) => ({ ...prev, customer_ssh_key_id: key.id }));
      setSSHModalOpen(false);
      setSSHForm({ name: '', ssh_user: 'root', public_key: '' });
    },
  });
  const provision = useMutation({ mutationFn: provisionAndBootstrapCluster });
  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => setForm((prev) => ({ ...prev, [key]: value }));

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (isNameTaken || !selectedKey || !selectedImage) return;
    const result = await provision.mutateAsync({
      provision: {
        ...form,
        network_profile_id: 0,
        proxmox_credential_id: 0,
        node_ip_pool_start: '',
        ubuntu_image: '',
        ssh_public_key: '',
        apply: true,
      },
      bootstrap: {
        ssh_user: 'root',
        deploy_kube_vip: false,
        control_plane_vip: '',
        pod_cidr: '10.244.0.0/16',
        service_cidr: '10.96.0.0/12',
        cni: 'calico',
        node_ips: {},
        apply: true,
      },
    });
    navigate(`/clusters/${result.cluster_id}?tab=runs`);
  };

  return (
    <Layout>
      <PageHeader title="Provision Cluster" description="Automatically place, provision, and bootstrap Kubernetes nodes"
        actions={<Link to="/clusters" className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900"><ArrowLeft className="h-4 w-4" /> Back</Link>} />
      <form onSubmit={handleSubmit} className="max-w-3xl space-y-6">
        <Card>
          <CardHeader><CardTitle>Cluster</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Input label="Cluster name" value={form.name} onChange={(e) => setField('name', e.target.value)} required placeholder="production" error={isNameTaken ? 'Cluster name already exists' : undefined} />
            <Select label="Topology" value={form.topology_type} onChange={(e) => {
              const topology = e.target.value;
              setForm((prev) => ({ ...prev, topology_type: topology, control_plane_count: topology === 'single' ? 1 : topology === 'dual' ? 2 : 3 }));
            }}>
              <option value="single">Single node</option><option value="dual">Dual node</option><option value="tri">Tri node (HA)</option>
            </Select>
            <Input label="Control plane count" type="number" min={1} value={form.control_plane_count} onChange={(e) => setField('control_plane_count', Number(e.target.value))} required />
            <Input label="Worker count" type="number" min={1} value={form.worker_count} onChange={(e) => setField('worker_count', Number(e.target.value))} required />
            <Input label="Kubernetes version" value={form.kubernetes_version} onChange={(e) => setField('kubernetes_version', e.target.value)} required />
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Select Operating System</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {imageDiscoveryError && <ErrorMessage error={imageDiscoveryError} />}
            {sharedImages.length === 0 && !imageDiscoveryError ? <Alert variant="warning" title={imagesLoading ? 'Checking shared images' : 'No shared images'}>{imagesLoading ? 'Synchronizing the OS image catalog with shared NFS storage.' : 'Upload an Ubuntu image to the cloud-images NFS import storage first.'}</Alert> :
              sharedImages.length > 0 &&
              <><div className="flex items-center gap-2 border-b border-slate-200 pb-2"><DistributionLogo distribution="ubuntu" className="h-6 w-6" /><h3 className="font-semibold text-slate-900">Ubuntu</h3><span className="text-xs text-slate-500">supported cluster images</span></div>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{sharedImages.map((image) => {
                const selected = image.id === form.cloud_init_image_id;
                return <button key={image.id} type="button" onClick={() => setField('cloud_init_image_id', image.id)} className={`rounded-md border p-4 text-left ${selected ? 'border-indigo-500 bg-indigo-50 ring-2 ring-indigo-100' : 'border-slate-200 hover:bg-slate-50'}`}>
                  <div className="flex gap-3"><DistributionLogo distribution="ubuntu" className="h-6 w-6 shrink-0" /><div className="min-w-0 flex-1"><div className="flex items-center justify-between gap-2"><p className="font-medium text-slate-900">{image.display_name || image.name}</p>{selected && <CheckCircle2 className="h-4 w-4 shrink-0 text-indigo-600" />}</div><p className="mt-1 text-[10px] font-normal leading-4 text-slate-500">{architectureLabel(image.architecture)}</p></div></div>
                </button>;
              })}</div></>}
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>SSH Access</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_auto]">
            <Select label="Customer SSH public key" value={form.customer_ssh_key_id} onChange={(e) => setField('customer_ssh_key_id', Number(e.target.value))} required>
              <option value={0}>-- Select saved public key --</option>
              {sshKeys?.map((key) => <option key={key.id} value={key.id}>{key.name} ({key.fingerprint})</option>)}
            </Select>
            <Button type="button" variant="secondary" onClick={() => setSSHModalOpen(true)}><Plus className="h-4 w-4" /> Paste new SSH key</Button>
          </CardContent>
        </Card>

        {[['Control Plane Resources', 'control_plane_cpu', 'control_plane_ram_mb', 'control_plane_disk_gb'], ['Worker Resources', 'worker_cpu', 'worker_ram_mb', 'worker_disk_gb']].map(([title, cpu, ram, disk]) =>
          <Card key={title}><CardHeader><CardTitle>{title}</CardTitle></CardHeader><CardContent className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Input label="CPU cores" type="number" min={1} value={form[cpu as keyof typeof form] as number} onChange={(e) => setField(cpu as keyof typeof form, Number(e.target.value) as never)} />
            <Input label="RAM (MB)" type="number" min={2048} step={1024} value={form[ram as keyof typeof form] as number} onChange={(e) => setField(ram as keyof typeof form, Number(e.target.value) as never)} />
            <Input label="Disk (GB)" type="number" min={20} value={form[disk as keyof typeof form] as number} onChange={(e) => setField(disk as keyof typeof form, Number(e.target.value) as never)} />
          </CardContent></Card>)}

        {provision.error && <ErrorMessage error={provision.error} />}
        <Button type="submit" loading={provision.isPending} disabled={isNameTaken || !selectedKey || !selectedImage}>Provision and bootstrap cluster</Button>
      </form>

      <Modal open={sshModalOpen} onClose={() => setSSHModalOpen(false)} title="Add Customer SSH Public Key">
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); createSSH.mutate(sshForm); }}>
          <Input label="Name" value={sshForm.name} onChange={(e) => setSSHForm({ ...sshForm, name: e.target.value })} required />
          <Input label="SSH user" value={sshForm.ssh_user} onChange={() => setSSHForm({ ...sshForm, ssh_user: 'root' })} helperText="Cluster node access uses root." readOnly required />
          <Textarea label="Public key" rows={3} value={sshForm.public_key} onChange={(e) => setSSHForm({ ...sshForm, public_key: e.target.value })} required />
          {createSSH.error && <ErrorMessage error={createSSH.error} />}
          <div className="flex gap-3"><Button type="submit" loading={createSSH.isPending}>Save and select</Button><Button type="button" variant="secondary" onClick={() => setSSHModalOpen(false)}>Cancel</Button></div>
        </form>
      </Modal>
    </Layout>
  );
}
