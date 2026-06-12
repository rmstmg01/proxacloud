import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Plus } from 'lucide-react';
import { createVPSCloudInitImage, createVPSInstance, discoverVPSCloudInitImages, getVPSCloudInitImages } from '../api/vps';
import { getNetworks } from '../api/networks';
import { getCredentials, getNodes } from '../api/proxmox';
import { createSSHKey, getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import type { VPSCloudInitImage } from '../types';

function applyImageDefaults<T extends { cloud_init_image_id: number; cloud_init_image_path: string; ssh_user: string; cpu: number; memory_mb: number; disk_gb: number }>(
  prev: T,
  image: VPSCloudInitImage,
): T {
  return {
    ...prev,
    cloud_init_image_id: image.id,
    cloud_init_image_path: image.image_path,
    ssh_user: image.ssh_user || prev.ssh_user,
    cpu: image.default_cpu || prev.cpu,
    memory_mb: image.default_memory_mb || prev.memory_mb,
    disk_gb: image.default_disk_gb || prev.disk_gb,
  };
}

export function ProvisionVPSPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: credentials } = useQuery({ queryKey: ['proxmox-credentials'], queryFn: getCredentials });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: sshKeys } = useQuery({ queryKey: ['ssh-keys'], queryFn: getSSHKeys });
  const { data: images } = useQuery({ queryKey: ['vps-cloud-init-images'], queryFn: getVPSCloudInitImages });

  const [sshModalOpen, setSSHModalOpen] = useState(false);
  const [imageModalOpen, setImageModalOpen] = useState(false);
  const [sshForm, setSSHForm] = useState({
    name: '',
    ssh_user: 'ubuntu',
    public_key: '',
    private_key: '',
  });
  const [imageForm, setImageForm] = useState({
    name: '',
    image_path: '',
    os_type: 'ubuntu',
    description: '',
    is_default: false,
  });
  const [form, setForm] = useState({
    name: '',
    description: '',
    proxmox_credential_id: 0,
    proxmox_node_name: '',
    network_profile_id: 0,
    cloud_init_image_id: 0,
    cloud_init_image_path: '',
    ssh_key_pair_id: 0,
    ssh_user: 'ubuntu',
    ip_mode: 'auto' as 'dhcp' | 'manual' | 'auto',
    ip_address: '',
    cpu: 1,
    memory_mb: 1024,
    disk_gb: 20,
    root_password: '',
    apply: true,
  });

  const enabledNodes = useMemo(
    () => (proxmoxNodes ?? []).filter((node) => node.enabled && (!form.proxmox_credential_id || node.credential_id === form.proxmox_credential_id)),
    [proxmoxNodes, form.proxmox_credential_id],
  );

  useEffect(() => {
    const defaultNetwork = networks?.find((network) => network.is_default) ?? networks?.[0];
    if (defaultNetwork && form.network_profile_id === 0) {
      setForm((prev) => ({ ...prev, network_profile_id: defaultNetwork.id }));
    }
  }, [networks, form.network_profile_id]);

  useEffect(() => {
    const defaultImage = images?.find((image) => image.is_default) ?? images?.[0];
    if (defaultImage && form.cloud_init_image_id === 0 && form.cloud_init_image_path === '') {
      setForm((prev) => applyImageDefaults(prev, defaultImage));
    }
  }, [images, form.cloud_init_image_id, form.cloud_init_image_path]);

  useEffect(() => {
    if (form.proxmox_credential_id === 0 && credentials && credentials.length > 0) {
      setForm((prev) => ({ ...prev, proxmox_credential_id: credentials[0].id }));
    }
  }, [credentials, form.proxmox_credential_id]);

  useEffect(() => {
    if (form.proxmox_node_name !== '' && enabledNodes.some((node) => node.node_name === form.proxmox_node_name)) {
      return;
    }
    if (enabledNodes[0]) {
      setForm((prev) => ({ ...prev, proxmox_node_name: enabledNodes[0].node_name }));
    }
  }, [enabledNodes, form.proxmox_node_name]);

  const createMutation = useMutation({
    mutationFn: createVPSInstance,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['vps'] });
      navigate(`/vps/${result.vps_id}`);
    },
  });

  const createSSHMutation = useMutation({
    mutationFn: createSSHKey,
    onSuccess: (key) => {
      queryClient.invalidateQueries({ queryKey: ['ssh-keys'] });
      setForm((prev) => ({ ...prev, ssh_key_pair_id: key.id }));
      setSSHForm({ name: '', ssh_user: 'ubuntu', public_key: '', private_key: '' });
      setSSHModalOpen(false);
    },
  });

  const createImageMutation = useMutation({
    mutationFn: createVPSCloudInitImage,
    onSuccess: (image) => {
      queryClient.invalidateQueries({ queryKey: ['vps-cloud-init-images'] });
      setForm((prev) => applyImageDefaults(prev, image));
      setImageForm({ name: '', image_path: '', os_type: 'ubuntu', description: '', is_default: false });
      setImageModalOpen(false);
    },
  });
  const discoverImagesMutation = useMutation({
    mutationFn: () => discoverVPSCloudInitImages(form.proxmox_credential_id, form.proxmox_node_name),
    onSuccess: (discovered) => {
      queryClient.setQueryData<VPSCloudInitImage[]>(['vps-cloud-init-images'], (current = []) => {
        const merged = new Map<number, VPSCloudInitImage>();
        current.forEach((image) => merged.set(image.id, image));
        discovered.forEach((image) => merged.set(image.id, image));
        return Array.from(merged.values()).sort((a, b) => {
          if (a.is_default !== b.is_default) {
            return a.is_default ? -1 : 1;
          }
          return a.name.localeCompare(b.name);
        });
      });
      queryClient.invalidateQueries({ queryKey: ['vps-cloud-init-images'] });
      const preferred = discovered.find((image) => image.image_path.toLowerCase().includes('almalinux')) ?? discovered[0];
      if (preferred) {
        setForm((prev) => applyImageDefaults(prev, preferred));
      }
    },
  });

  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    createMutation.mutate(form);
  };

  const handleCreateSSH = (event: FormEvent) => {
    event.preventDefault();
    createSSHMutation.mutate(sshForm);
  };

  const handleCreateImage = (event: FormEvent) => {
    event.preventDefault();
    createImageMutation.mutate(imageForm);
  };

  return (
    <Layout>
      <PageHeader
        title="Create VPS"
        description="Deploy a standalone cloud-init VM on Proxmox"
        actions={
          <Link to="/vps" className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm">
            <ArrowLeft className="h-4 w-4" /> Back
          </Link>
        }
      />

      <form onSubmit={handleSubmit} className="space-y-6 max-w-3xl">
        <Card>
          <CardHeader>
            <CardTitle>General</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Input
              label="VPS name"
              value={form.name}
              onChange={(event) => setField('name', event.target.value)}
              placeholder="web-01"
              required
            />
            <Input
              label="Description"
              value={form.description}
              onChange={(event) => setField('description', event.target.value)}
              placeholder="Production web VM"
            />
            <Select
              label="Proxmox credential"
              value={form.proxmox_credential_id}
              onChange={(event) => {
                setForm((prev) => ({
                  ...prev,
                  proxmox_credential_id: Number(event.target.value),
                  proxmox_node_name: '',
                }));
              }}
              required
            >
              <option value={0}>-- Select credential --</option>
              {credentials?.map((credential) => (
                <option key={credential.id} value={credential.id}>
                  {credential.name} ({credential.endpoint})
                </option>
              ))}
            </Select>
            <Select
              label="Proxmox node"
              value={form.proxmox_node_name}
              onChange={(event) => setField('proxmox_node_name', event.target.value)}
              required
            >
              <option value="">-- Select node --</option>
              {enabledNodes.map((node) => (
                <option key={`${node.credential_id}-${node.node_name}`} value={node.node_name}>
                  {node.node_name}
                </option>
              ))}
            </Select>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Resources</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <Input
              label="vCPU"
              type="number"
              min={1}
              value={form.cpu}
              onChange={(event) => setField('cpu', Number(event.target.value))}
              required
            />
            <Input
              label="RAM (MB)"
              type="number"
              min={256}
              step={256}
              value={form.memory_mb}
              onChange={(event) => setField('memory_mb', Number(event.target.value))}
              required
            />
            <Input
              label="Disk (GB)"
              type="number"
              min={5}
              value={form.disk_gb}
              onChange={(event) => setField('disk_gb', Number(event.target.value))}
              required
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Network</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <Select
              label="Network profile"
              value={form.network_profile_id}
              onChange={(event) => setField('network_profile_id', Number(event.target.value))}
              required
            >
              <option value={0}>-- Select network --</option>
              {networks?.map((network) => (
                <option key={network.id} value={network.id}>
                  {network.name} ({network.cidr}) {network.is_default ? '(default)' : ''}
                </option>
              ))}
            </Select>
            <Select
              label="IP mode"
              value={form.ip_mode}
              onChange={(event) => setField('ip_mode', event.target.value as 'dhcp' | 'manual' | 'auto')}
            >
              <option value="auto">Auto assign from network</option>
              <option value="manual">Manual static IP</option>
              <option value="dhcp">DHCP</option>
            </Select>
            {form.ip_mode === 'manual' && (
              <Input
                label="Static IP address"
                value={form.ip_address}
                onChange={(event) => setField('ip_address', event.target.value)}
                placeholder="192.168.8.150"
                required
              />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Access</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3 items-end">
              <Select
                label="SSH key pair"
                value={form.ssh_key_pair_id}
                onChange={(event) => setField('ssh_key_pair_id', Number(event.target.value))}
              >
                <option value={0}>-- No saved SSH key --</option>
                {sshKeys?.map((key) => (
                  <option key={key.id} value={key.id}>
                    {key.name} ({key.ssh_user})
                  </option>
                ))}
              </Select>
              <Button type="button" variant="secondary" onClick={() => setSSHModalOpen(true)}>
                <Plus className="h-4 w-4" />
                Add SSH Key
              </Button>
            </div>
            <Input
              label="SSH user"
              value={form.ssh_user}
              onChange={(event) => setField('ssh_user', event.target.value)}
              required
            />
            <Input
              label="Root password"
              type="password"
              value={form.root_password}
              onChange={(event) => setField('root_password', event.target.value)}
              helperText="Required only when no SSH key is selected"
              required={form.ssh_key_pair_id === 0}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Cloud Init Image</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3 items-end">
              <Select
                label="Saved image"
                value={form.cloud_init_image_id}
                onChange={(event) => {
                  const selectedID = Number(event.target.value);
                  const selected = images?.find((image) => image.id === selectedID);
                  if (selected) {
                    setForm((prev) => applyImageDefaults(prev, selected));
                  } else {
                    setForm((prev) => ({ ...prev, cloud_init_image_id: 0, cloud_init_image_path: '' }));
                  }
                }}
              >
                <option value={0}>-- Select discovered image --</option>
                {images?.map((image) => (
                  <option key={image.id} value={image.id}>
                    {image.name} {image.is_default ? '(default)' : ''}
                  </option>
                ))}
              </Select>
              <Button
                type="button"
                variant="secondary"
                onClick={() => discoverImagesMutation.mutate()}
                loading={discoverImagesMutation.isPending}
                disabled={form.proxmox_credential_id === 0 || form.proxmox_node_name === ''}
              >
                Discover Images
              </Button>
              <Button type="button" variant="secondary" onClick={() => setImageModalOpen(true)}>
                <Plus className="h-4 w-4" />
                Add Image
              </Button>
            </div>
            <Input
              label="Detected image path"
              value={form.cloud_init_image_path}
              onChange={(event) => {
                setForm((prev) => ({
                  ...prev,
                  cloud_init_image_id: 0,
                  cloud_init_image_path: event.target.value,
                }));
              }}
              placeholder="local:import/AlmaLinux-10-GenericCloud-10.2-20260526.0.x86_64.qcow2"
              required
            />
            {form.cloud_init_image_id > 0 && (
              <p className="text-xs text-slate-500">
                Selecting an image applies its defaults for SSH user, CPU, RAM, and disk. Root password remains user-provided.
              </p>
            )}
            {discoverImagesMutation.error && <ErrorMessage error={discoverImagesMutation.error} />}
          </CardContent>
        </Card>

        <Card>
          <CardContent>
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={form.apply}
                onChange={(event) => setField('apply', event.target.checked)}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
              />
              <span className="text-sm text-slate-700">Deploy immediately with Terraform apply</span>
            </label>
          </CardContent>
        </Card>

        {form.ssh_key_pair_id === 0 && form.root_password.trim() === '' && (
          <Alert variant="warning" title="Access method required">
            Select an SSH key or set a root password before creating the VPS.
          </Alert>
        )}
        {createMutation.error && <ErrorMessage error={createMutation.error} />}

        <div className="flex gap-3">
          <Button
            type="submit"
            size="lg"
            loading={createMutation.isPending}
            disabled={form.ssh_key_pair_id === 0 && form.root_password.trim() === ''}
          >
            {form.apply ? 'Create and Deploy VPS' : 'Create VPS'}
          </Button>
          <Button type="button" variant="secondary" size="lg" onClick={() => navigate('/vps')}>
            Cancel
          </Button>
        </div>
      </form>

      <Modal open={sshModalOpen} onClose={() => setSSHModalOpen(false)} title="Add SSH Key Pair">
        <form onSubmit={handleCreateSSH} className="space-y-4">
          <Input label="Name" value={sshForm.name} onChange={(event) => setSSHForm({ ...sshForm, name: event.target.value })} required />
          <Input label="SSH user" value={sshForm.ssh_user} onChange={(event) => setSSHForm({ ...sshForm, ssh_user: event.target.value })} required />
          <Textarea label="Public key" value={sshForm.public_key} onChange={(event) => setSSHForm({ ...sshForm, public_key: event.target.value })} rows={3} required />
          <Textarea label="Private key" value={sshForm.private_key} onChange={(event) => setSSHForm({ ...sshForm, private_key: event.target.value })} rows={6} required />
          {createSSHMutation.error && <ErrorMessage error={createSSHMutation.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={createSSHMutation.isPending}>Save</Button>
            <Button type="button" variant="secondary" onClick={() => setSSHModalOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>

      <Modal open={imageModalOpen} onClose={() => setImageModalOpen(false)} title="Add Cloud Init Image">
        <form onSubmit={handleCreateImage} className="space-y-4">
          <Input label="Name" value={imageForm.name} onChange={(event) => setImageForm({ ...imageForm, name: event.target.value })} required />
          <Input
            label="Image path"
            value={imageForm.image_path}
            onChange={(event) => setImageForm({ ...imageForm, image_path: event.target.value })}
            placeholder="local:import/AlmaLinux-10-GenericCloud-10.2-20260526.0.x86_64.qcow2"
            required
          />
          <Input label="OS type" value={imageForm.os_type} onChange={(event) => setImageForm({ ...imageForm, os_type: event.target.value })} required />
          <Textarea label="Description" value={imageForm.description} onChange={(event) => setImageForm({ ...imageForm, description: event.target.value })} rows={3} />
          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              checked={imageForm.is_default}
              onChange={(event) => setImageForm({ ...imageForm, is_default: event.target.checked })}
              className="h-4 w-4 rounded border-slate-300 text-indigo-600"
            />
            <span className="text-sm text-slate-700">Use as default image</span>
          </label>
          {createImageMutation.error && <ErrorMessage error={createImageMutation.error} />}
          <div className="flex gap-3 pt-2">
            <Button type="submit" loading={createImageMutation.isPending}>Save</Button>
            <Button type="button" variant="secondary" onClick={() => setImageModalOpen(false)}>Cancel</Button>
          </div>
        </form>
      </Modal>
    </Layout>
  );
}
