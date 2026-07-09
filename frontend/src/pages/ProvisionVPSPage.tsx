import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, CheckCircle2, Copy, Eye, EyeOff, KeyRound, Plus, RefreshCw, Server } from 'lucide-react';
import { createVPSInstance, discoverVPSCloudInitImages, getVPSCloudInitImages } from '../api/vps';
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

const passwordGroups = {
  lower: 'abcdefghijklmnopqrstuvwxyz',
  upper: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
  number: '0123456789',
  special: '!@#$%^&*_-+=?',
};

const passwordCharacters = Object.values(passwordGroups).join('');

function randomIndex(max: number) {
  const values = new Uint32Array(1);
  crypto.getRandomValues(values);
  return values[0] % max;
}

function shufflePassword(chars: string[]) {
  for (let index = chars.length - 1; index > 0; index -= 1) {
    const swapIndex = randomIndex(index + 1);
    [chars[index], chars[swapIndex]] = [chars[swapIndex], chars[index]];
  }
  return chars.join('');
}

function generateRootPassword(length = 16) {
  const size = Math.max(length, 10);
  const chars = [
    passwordGroups.lower[randomIndex(passwordGroups.lower.length)],
    passwordGroups.upper[randomIndex(passwordGroups.upper.length)],
    passwordGroups.number[randomIndex(passwordGroups.number.length)],
    passwordGroups.special[randomIndex(passwordGroups.special.length)],
  ];
  while (chars.length < size) {
    chars.push(passwordCharacters[randomIndex(passwordCharacters.length)]);
  }
  return shufflePassword(chars);
}

function rootPasswordError(password: string) {
  if (password.trim() === '') {
    return '';
  }
  if (password.length < 10) {
    return 'Root password must be at least 10 characters.';
  }
  if (password.includes(':')) {
    return 'Root password cannot include colon.';
  }
  if (!/[a-z]/.test(password)) {
    return 'Root password must include a lowercase letter.';
  }
  if (!/[A-Z]/.test(password)) {
    return 'Root password must include an uppercase letter.';
  }
  if (!/[0-9]/.test(password)) {
    return 'Root password must include a number.';
  }
  if (!/[!@#$%^&*_\-+=?]/.test(password)) {
    return 'Root password must include a special character.';
  }
  return '';
}

function osMeta(image: VPSCloudInitImage) {
  const value = `${image.os_type} ${image.name} ${image.image_path}`.toLowerCase();
  if (value.includes('alma')) return { label: 'AlmaLinux', tone: 'bg-indigo-50 text-indigo-700 ring-indigo-200' };
  if (value.includes('ubuntu')) return { label: 'Ubuntu', tone: 'bg-orange-50 text-orange-700 ring-orange-200' };
  if (value.includes('debian')) return { label: 'Debian', tone: 'bg-rose-50 text-rose-700 ring-rose-200' };
  if (value.includes('rocky')) return { label: 'Rocky', tone: 'bg-emerald-50 text-emerald-700 ring-emerald-200' };
  if (value.includes('centos')) return { label: 'CentOS', tone: 'bg-violet-50 text-violet-700 ring-violet-200' };
  if (value.includes('fedora')) return { label: 'Fedora', tone: 'bg-sky-50 text-sky-700 ring-sky-200' };
  return { label: image.os_type || 'Linux', tone: 'bg-slate-50 text-slate-700 ring-slate-200' };
}

function imageFilename(imagePath: string) {
  const clean = imagePath.trim();
  const slash = clean.lastIndexOf('/');
  return slash >= 0 ? clean.slice(slash + 1) : clean;
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
  const [showRootPassword, setShowRootPassword] = useState(false);
  const [passwordCopied, setPasswordCopied] = useState(false);
  const lastAutoDiscoveryKey = useRef('');
  const [sshForm, setSSHForm] = useState({
    name: '',
    ssh_user: 'ubuntu',
    public_key: '',
    private_key: '',
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

  const passwordError = useMemo(() => rootPasswordError(form.root_password), [form.root_password]);
  const createDisabled =
    form.cloud_init_image_path.trim() === '' ||
    (form.ssh_key_pair_id === 0 && form.root_password.trim() === '') ||
    passwordError !== '';

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

  useEffect(() => {
    if (form.proxmox_credential_id === 0 || form.proxmox_node_name === '') {
      return;
    }
    const discoveryKey = `${form.proxmox_credential_id}:${form.proxmox_node_name}`;
    if (lastAutoDiscoveryKey.current === discoveryKey || discoverImagesMutation.isPending) {
      return;
    }
    lastAutoDiscoveryKey.current = discoveryKey;
    discoverImagesMutation.mutate();
  }, [discoverImagesMutation, form.proxmox_credential_id, form.proxmox_node_name]);

  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (createDisabled) {
      return;
    }
    createMutation.mutate(form);
  };

  const handleGenerateRootPassword = () => {
    const password = generateRootPassword();
    setField('root_password', password);
    setShowRootPassword(true);
    setPasswordCopied(false);
  };

  const handleCopyRootPassword = async () => {
    if (form.root_password === '') {
      return;
    }
    await navigator.clipboard.writeText(form.root_password);
    setPasswordCopied(true);
    window.setTimeout(() => setPasswordCopied(false), 1800);
  };

  const handleCreateSSH = (event: FormEvent) => {
    event.preventDefault();
    createSSHMutation.mutate(sshForm);
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
              label="Subnet"
              value={form.network_profile_id}
              onChange={(event) => setField('network_profile_id', Number(event.target.value))}
              required
            >
              <option value={0}>-- Select subnet --</option>
              {networks?.map((network) => (
                <option key={network.id} value={network.id}>
                  {network.vpc_name || 'default-vpc'} / {network.name} ({network.cidr}) {network.is_default ? '(default)' : ''}
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
            <div className="space-y-2">
              <Input
                label="Root password"
                type={showRootPassword ? 'text' : 'password'}
                value={form.root_password}
                onChange={(event) => {
                  setField('root_password', event.target.value);
                  setPasswordCopied(false);
                }}
                helperText="Required only when no SSH key is selected"
                error={passwordError}
                required={form.ssh_key_pair_id === 0}
                autoComplete="new-password"
              />
              <div className="flex flex-wrap gap-2">
                <Button type="button" variant="secondary" size="sm" onClick={handleGenerateRootPassword}>
                  <KeyRound className="h-4 w-4" />
                  Generate
                </Button>
                <Button type="button" variant="secondary" size="sm" onClick={handleCopyRootPassword} disabled={form.root_password === ''}>
                  <Copy className="h-4 w-4" />
                  {passwordCopied ? 'Copied' : 'Copy'}
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => setShowRootPassword((value) => !value)}>
                  {showRootPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  {showRootPassword ? 'Hide' : 'Show'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Cloud Init Image</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0">
                <p className="text-sm font-medium text-slate-800">Discovered cloud images</p>
                <p className="mt-1 text-xs text-slate-500">Images are loaded automatically for the selected Proxmox node.</p>
              </div>
              <Button
                type="button"
                variant="secondary"
                onClick={() => discoverImagesMutation.mutate()}
                loading={discoverImagesMutation.isPending}
                disabled={form.proxmox_credential_id === 0 || form.proxmox_node_name === ''}
              >
                <RefreshCw className="h-4 w-4" />
                Refresh
              </Button>
            </div>

            {images && images.length > 0 ? (
              <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                {images.map((image) => {
                  const selected = form.cloud_init_image_id === image.id || form.cloud_init_image_path === image.image_path;
                  const meta = osMeta(image);
                  return (
                    <button
                      key={image.id}
                      type="button"
                      onClick={() => setForm((prev) => applyImageDefaults(prev, image))}
                      className={`min-h-[116px] rounded-md border p-4 text-left transition-colors ${
                        selected
                          ? 'border-indigo-500 bg-indigo-50/60 ring-2 ring-indigo-100'
                          : 'border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50'
                      }`}
                    >
                      <div className="flex items-start gap-3">
                        <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-md ring-1 ${meta.tone}`}>
                          <Server className="h-5 w-5" />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 items-center justify-between gap-2">
                            <p className="truncate text-sm font-semibold text-slate-900">{image.name || imageFilename(image.image_path)}</p>
                            {selected && <CheckCircle2 className="h-4 w-4 shrink-0 text-indigo-600" />}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                            <span>{meta.label}</span>
                            {image.is_default && <span className="rounded bg-emerald-50 px-1.5 py-0.5 font-medium text-emerald-700">default</span>}
                            <span>{image.ssh_user || 'cloud-user'}</span>
                            <span>{image.default_cpu || 2} vCPU</span>
                            <span>{image.default_memory_mb || 2048} MB</span>
                            <span>{image.default_disk_gb || 20} GB</span>
                          </div>
                        </div>
                      </div>
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className="rounded-md border border-dashed border-slate-300 bg-slate-50 px-4 py-6 text-sm text-slate-600">
                {discoverImagesMutation.isPending
                  ? 'Discovering cloud images...'
                  : 'No shared cloud images discovered yet. Upload an image, then refresh.'}
              </div>
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
            disabled={createDisabled}
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

    </Layout>
  );
}
