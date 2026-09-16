import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, CheckCircle2, Copy, Eye, EyeOff, Globe2, KeyRound, Plus } from 'lucide-react';
import { createVPSInstance, discoverVPSCloudInitImagesAutomatically } from '../api/vps';
import { getNetworks } from '../api/networks';
import { getRegions } from '../api/regions';
import { createCustomerSSHKey, getCustomerSSHKeys } from '../api/customerSshKeys';
import { getAvailableManagedDomains } from '../api/managedDomains';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { architectureLabel, DistributionLogo } from '../components/DistributionLogo';
import type { VPSCloudInitImage } from '../types';

function applyImageDefaults<T extends { cloud_init_image_id: number; cloud_init_image_path: string; ssh_user: string; cpu: number; memory_mb: number; disk_gb: number }>(
  prev: T,
  image: VPSCloudInitImage,
): T {
  return {
    ...prev,
    cloud_init_image_id: image.id,
    cloud_init_image_path: image.image_path,
    ssh_user: 'root',
    cpu: image.default_cpu || prev.cpu,
    memory_mb: image.default_memory_mb || prev.memory_mb,
    disk_gb: image.default_disk_gb || prev.disk_gb,
  };
}

const distributionOrder = [
  { key: 'almalinux', label: 'AlmaLinux' },
  { key: 'rocky', label: 'Rocky Linux' },
  { key: 'ubuntu', label: 'Ubuntu' },
  { key: 'debian', label: 'Debian' },
  { key: 'fedora', label: 'Fedora' },
] as const;

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

function generateRootPassword(length = 16) {
  const size = Math.max(length, 10);
  const chars = [
    passwordGroups.lower[randomIndex(passwordGroups.lower.length)],
    passwordGroups.upper[randomIndex(passwordGroups.upper.length)],
    passwordGroups.number[randomIndex(passwordGroups.number.length)],
    passwordGroups.special[randomIndex(passwordGroups.special.length)],
  ];
  while (chars.length < size) chars.push(passwordCharacters[randomIndex(passwordCharacters.length)]);
  for (let index = chars.length - 1; index > 0; index -= 1) {
    const swapIndex = randomIndex(index + 1);
    [chars[index], chars[swapIndex]] = [chars[swapIndex], chars[index]];
  }
  return chars.join('');
}

function rootPasswordError(password: string) {
  if (password.trim() === '') return '';
  if (password.length < 10) return 'Root password must be at least 10 characters.';
  if (password.includes(':')) return 'Root password cannot include colon.';
  if (!/[a-z]/.test(password)) return 'Root password must include a lowercase letter.';
  if (!/[A-Z]/.test(password)) return 'Root password must include an uppercase letter.';
  if (!/[0-9]/.test(password)) return 'Root password must include a number.';
  if (!/[^A-Za-z0-9]/.test(password)) return 'Root password must include a special character.';
  return '';
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
  const { data: regions } = useQuery({ queryKey: ['regions'], queryFn: getRegions });
  const { data: sshKeys } = useQuery({ queryKey: ['customer-ssh-keys'], queryFn: getCustomerSSHKeys });
  const { data: managedDomains = [] } = useQuery({ queryKey: ['managed-domains', 'available'], queryFn: getAvailableManagedDomains });

  const [sshModalOpen, setSSHModalOpen] = useState(false);
  const [showRootPassword, setShowRootPassword] = useState(false);
  const [passwordCopied, setPasswordCopied] = useState(false);
  const [discoveredImages, setDiscoveredImages] = useState<VPSCloudInitImage[]>([]);
  const lastAutoDiscoveryKey = useRef('');
  const [sshForm, setSSHForm] = useState({
    name: '',
    ssh_user: 'root',
    public_key: '',
  });
  const [form, setForm] = useState({
    name: '',
    description: '',
    proxmox_credential_id: 0,
    proxmox_node_name: '',
    region_id: 0,
    network_profile_id: 0,
    cloud_init_image_id: 0,
    cloud_init_image_path: '',
    customer_ssh_key_id: 0,
    ssh_user: 'root',
    ip_mode: 'auto' as 'dhcp' | 'manual' | 'auto',
    ip_address: '',
    cpu: 1,
    memory_mb: 1024,
    disk_gb: 20,
    root_password: '',
    apply: true,
	create_tunnel: true,
	tunnel_domain_id: 0,
	tunnel_hostname_label: '',
  });

  const enabledRegions = useMemo(() => (regions ?? []).filter((region) => region.can_deploy && region.enabled && region.nodes.some((node) => node.enabled)), [regions]);
  const selectedRegion = useMemo(() => enabledRegions.find((region) => region.id === form.region_id), [enabledRegions, form.region_id]);
  const availableNetworks = useMemo(() => {
    if (!selectedRegion) {
      return [];
    }
    const allowedIDs = new Set(selectedRegion.networks.map((network) => network.id));
    return (networks ?? []).filter((network) => allowedIDs.has(network.id));
  }, [networks, selectedRegion]);
  const sharedImages = useMemo(
    () => discoveredImages.filter((image) => image.image_path.toLowerCase().startsWith('cloud-images:import/')),
    [discoveredImages],
  );
  const imageGroups = useMemo(
    () => distributionOrder.map((distribution) => ({
      ...distribution,
      images: sharedImages
        .filter((image) => image.distribution === distribution.key)
        .sort((a, b) => (a.version || a.name).localeCompare(b.version || b.name, undefined, { numeric: true })),
    })).filter((group) => group.images.length > 0),
    [sharedImages],
  );
  const selectedNetwork = useMemo(
    () => availableNetworks.find((network) => network.id === form.network_profile_id),
    [availableNetworks, form.network_profile_id],
  );
  const selectedImage = useMemo(
    () => sharedImages.find((image) => image.id === form.cloud_init_image_id || image.image_path === form.cloud_init_image_path),
    [sharedImages, form.cloud_init_image_id, form.cloud_init_image_path],
  );

  const passwordError = useMemo(() => rootPasswordError(form.root_password), [form.root_password]);
  const hasCustomerAccess = form.customer_ssh_key_id > 0 || form.root_password.trim() !== '';
  const createDisabled =
    !selectedRegion ||
    form.network_profile_id === 0 ||
    form.cloud_init_image_path.trim() === '' ||
    !hasCustomerAccess ||
    passwordError !== '' ||
	(form.create_tunnel && form.tunnel_domain_id === 0);

  const selectedTunnelDomain = useMemo(
	() => managedDomains.find((domain) => domain.id === form.tunnel_domain_id),
	[managedDomains, form.tunnel_domain_id],
  );
	const tunnelLabel = (form.tunnel_hostname_label || form.name || 'vps').toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63);
	const tunnelHostname = selectedTunnelDomain ? `${tunnelLabel || 'vps'}.${selectedTunnelDomain.base_domain}` : '';

  useEffect(() => {
    const defaultRegion = enabledRegions.find((region) => region.is_default) ?? enabledRegions[0];
    if (defaultRegion && form.region_id === 0) {
      setForm((prev) => ({ ...prev, region_id: defaultRegion.id }));
    }
  }, [enabledRegions, form.region_id]);

	useEffect(() => {
		if (!form.create_tunnel || form.tunnel_domain_id !== 0 || managedDomains.length === 0) return;
		const preferred = managedDomains.find((domain) => domain.is_default) ?? managedDomains[0];
		setForm((current) => ({ ...current, tunnel_domain_id: preferred.id }));
	}, [form.create_tunnel, form.tunnel_domain_id, managedDomains]);

  useEffect(() => {
    if (!selectedRegion) {
      return;
    }
    const defaultNetwork = availableNetworks.find((network) => network.is_default) ?? availableNetworks[0];
    if (form.network_profile_id !== 0 && availableNetworks.some((network) => network.id === form.network_profile_id)) {
      return;
    }
    setForm((prev) => ({ ...prev, network_profile_id: defaultNetwork?.id ?? 0 }));
  }, [availableNetworks, form.network_profile_id, selectedRegion]);

  useEffect(() => {
    const defaultImage = sharedImages.find((image) => image.is_default) ?? sharedImages[0];
    if (defaultImage && form.cloud_init_image_id === 0 && form.cloud_init_image_path === '') {
      setForm((prev) => applyImageDefaults(prev, defaultImage));
    }
  }, [sharedImages, form.cloud_init_image_id, form.cloud_init_image_path]);

  const createMutation = useMutation({
    mutationFn: createVPSInstance,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['vps'] });
      navigate(`/vps/${result.vps_id}`);
    },
  });

  const createSSHMutation = useMutation({
    mutationFn: createCustomerSSHKey,
    onSuccess: (key) => {
      queryClient.invalidateQueries({ queryKey: ['customer-ssh-keys'] });
      setForm((prev) => ({ ...prev, customer_ssh_key_id: key.id }));
      setSSHForm({ name: '', ssh_user: 'root', public_key: '' });
      setSSHModalOpen(false);
    },
  });

  const discoverImagesMutation = useMutation({
    mutationFn: () => discoverVPSCloudInitImagesAutomatically(),
    onSuccess: (discovered) => {
      const cloudImages = discovered
        .filter((image) => image.image_path.toLowerCase().startsWith('cloud-images:import/'))
        .sort((a, b) => {
          if (a.is_default !== b.is_default) {
            return a.is_default ? -1 : 1;
          }
          return a.name.localeCompare(b.name);
        });
      setDiscoveredImages(cloudImages);
      const preferred = cloudImages.find((image) => image.image_path.toLowerCase().includes('almalinux')) ?? cloudImages[0];
      setForm((prev) => {
        if (preferred) {
          return applyImageDefaults(prev, preferred);
        }
        return {
          ...prev,
          cloud_init_image_id: 0,
          cloud_init_image_path: '',
        };
      });
    },
    onError: () => {
      setDiscoveredImages([]);
      setForm((prev) => ({
        ...prev,
        cloud_init_image_id: 0,
        cloud_init_image_path: '',
      }));
    },
  });

  useEffect(() => {
    const discoveryKey = 'shared-images';
    if (lastAutoDiscoveryKey.current === discoveryKey || discoverImagesMutation.isPending) {
      return;
    }
    lastAutoDiscoveryKey.current = discoveryKey;
    setDiscoveredImages([]);
    setForm((prev) => ({
      ...prev,
      cloud_init_image_id: 0,
      cloud_init_image_path: '',
    }));
    discoverImagesMutation.mutate();
  }, [discoverImagesMutation]);

  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (createDisabled) {
      return;
    }
    createMutation.mutate({ ...form, apply: true });
  };

  const handleGenerateRootPassword = () => {
    setField('root_password', generateRootPassword());
    setShowRootPassword(true);
    setPasswordCopied(false);
  };

  const handleCopyRootPassword = async () => {
    if (form.root_password === '') return;
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
        description="Deploy a VPS"
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
            {regions && !regions.some((region) => region.can_deploy) && <Alert variant="info">No deployment regions are available to you. Contact your administrator.</Alert>}
            <Select
              label="Location"
              value={form.region_id}
              onChange={(event) => {
                setForm((prev) => ({
                  ...prev,
                  region_id: Number(event.target.value),
                  network_profile_id: 0,
                }));
              }}
              required
            >
              <option value={0}>-- Select location --</option>
              {(regions ?? []).map((region) => (
                <option key={region.id} value={region.id} disabled={!region.can_deploy || !region.nodes.some((node) => node.enabled)}>
                  {region.name}{!region.can_deploy ? ` - ${region.deployment_unavailable_reason || 'Not permitted'}` : !region.nodes.some((node) => node.enabled) ? ' - No available nodes' : ''}
                </option>
              ))}
            </Select>
          </CardContent>
        </Card>

		<Card>
		  <CardHeader>
			<CardTitle>Public access and tunnel</CardTitle>
		  </CardHeader>
		  <CardContent className="space-y-4">
			<label className="flex items-start gap-3 rounded-md border border-slate-200 bg-slate-50 p-4">
			  <input type="checkbox" className="mt-1 h-4 w-4 rounded border-slate-300 text-indigo-600" checked={form.create_tunnel} onChange={(event) => setField('create_tunnel', event.target.checked)} />
			  <span><span className="block text-sm font-semibold text-slate-900">Create ProxaCloud tunnel</span><span className="mt-1 block text-xs text-slate-500">Publish HTTP access and prepare tunneled SSH access during deployment.</span></span>
			</label>
			{form.create_tunnel && <>
			  {managedDomains.length === 0 ? <Alert variant="warning" title="No managed domains available">A super administrator must add and enable a managed tunnel domain before tunnel creation can be selected.</Alert> : <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
				<Select label="Public domain" value={form.tunnel_domain_id} onChange={(event) => setField('tunnel_domain_id', Number(event.target.value))} required>
				  <option value={0}>-- Select domain --</option>
				  {managedDomains.map((domain) => <option key={domain.id} value={domain.id}>{domain.name} — {domain.base_domain}{domain.is_default ? ' (default)' : ''}</option>)}
				</Select>
				<Input label="Hostname label" value={form.tunnel_hostname_label} onChange={(event) => setField('tunnel_hostname_label', event.target.value)} placeholder={form.name || 'web-01'} helperText="Leave empty to use the VPS name." />
			  </div>}
			  {tunnelHostname && <div className="flex items-center gap-3 rounded-md border border-indigo-200 bg-indigo-50 px-4 py-3"><Globe2 className="h-5 w-5 text-indigo-600"/><div><p className="text-xs font-medium uppercase tracking-wide text-indigo-600">Public hostname</p><p className="text-sm font-semibold text-slate-900">{tunnelHostname}</p></div></div>}
			</>}
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
              {availableNetworks.map((network) => (
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
                label="Customer SSH public key"
                value={form.customer_ssh_key_id}
                onChange={(event) => setField('customer_ssh_key_id', Number(event.target.value))}
              >
                <option value={0}>-- Continue without an SSH key --</option>
                {sshKeys?.map((key) => (
                  <option key={key.id} value={key.id}>
                    {key.name} ({key.fingerprint})
                  </option>
                ))}
              </Select>
              <Button type="button" variant="secondary" onClick={() => setSSHModalOpen(true)}>
                <Plus className="h-4 w-4" />
                Add Public Key
              </Button>
            </div>
            <p className="text-xs text-slate-500">
              Recommended: use an SSH public key for stronger, passwordless access. If you do not select one, set a root password below.
            </p>
            <Input
              label="SSH user"
              value={form.ssh_user}
              onChange={() => setField('ssh_user', 'root')}
              helperText="VPS login details always use the root user."
              readOnly
              required
            />
            <div className="space-y-2">
              <Input
                label="Root password (optional)"
                type={showRootPassword ? 'text' : 'password'}
                value={form.root_password}
                onChange={(event) => {
                  setField('root_password', event.target.value);
                  setPasswordCopied(false);
                }}
                helperText="Leave empty for SSH-key-only access, or set a password for root login."
                error={passwordError}
                autoComplete="new-password"
              />
              <div className="flex flex-wrap gap-2">
                <Button type="button" variant="secondary" size="sm" onClick={handleGenerateRootPassword}>
                  <KeyRound className="h-4 w-4" /> Generate random password
                </Button>
                <Button type="button" variant="secondary" size="sm" onClick={handleCopyRootPassword} disabled={form.root_password === ''}>
                  <Copy className="h-4 w-4" /> {passwordCopied ? 'Copied' : 'Copy'}
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => setShowRootPassword((value) => !value)} disabled={form.root_password === ''}>
                  {showRootPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  {showRootPassword ? 'Hide' : 'Show'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Select Operating System</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {sharedImages.length > 0 ? (
              <div className="space-y-6">
                {imageGroups.map((group) => <section key={group.key} className="space-y-3">
                  <div className="flex items-center gap-2 border-b border-slate-200 pb-2">
                    <DistributionLogo distribution={group.key} className="h-6 w-6" />
                    <h3 className="font-semibold text-slate-900">{group.label}</h3>
                    <span className="text-xs text-slate-500">{group.images.length} version{group.images.length === 1 ? '' : 's'}</span>
                  </div>
                  <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
                {group.images.map((image) => {
                  const selected = form.cloud_init_image_id === image.id || form.cloud_init_image_path === image.image_path;
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
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-white ring-1 ring-slate-200">
                          <DistributionLogo distribution={image.logo_key || image.distribution} className="h-6 w-6" />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 items-center justify-between gap-2">
                            <p className="truncate text-sm font-semibold text-slate-900">{image.display_name || image.name || imageFilename(image.image_path)}</p>
                            {selected && <CheckCircle2 className="h-4 w-4 shrink-0 text-indigo-600" />}
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-slate-500">
                            <span className="text-[10px] font-normal leading-4">{architectureLabel(image.architecture)}</span>
                          </div>
                        </div>
                      </div>
                    </button>
                  );
                })}
                  </div>
                </section>)}
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
          <CardHeader>
            <CardTitle>Deployment Plan</CardTitle>
          </CardHeader>
          <CardContent className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {[
              ['Location', selectedRegion?.name || 'Select location'],
              ['Subnet', selectedNetwork ? `${selectedNetwork.vpc_name || 'default-vpc'} / ${selectedNetwork.name}` : 'Select subnet'],
              ['Image', selectedImage?.name || imageFilename(form.cloud_init_image_path) || 'Select image'],
              ['Access', form.customer_ssh_key_id > 0 ? `root via SSH public key${form.root_password.trim() ? ' + password' : ''}` : 'root via password'],
              ['Resources', `${form.cpu} vCPU, ${form.memory_mb} MB RAM, ${form.disk_gb} GB disk`],
              ['IP assignment', form.ip_mode === 'manual' ? form.ip_address || 'Manual IP required' : form.ip_mode === 'dhcp' ? 'DHCP' : 'Auto assign from network'],
			  ['Tunnel', form.create_tunnel ? tunnelHostname || 'Select a managed domain' : 'Disabled'],
            ].map(([label, value]) => (
              <div key={label} className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2">
                <p className="text-xs font-medium uppercase text-slate-500">{label}</p>
                <p className="mt-1 truncate text-sm font-semibold text-slate-900">{value}</p>
              </div>
            ))}
          </CardContent>
        </Card>

        {!hasCustomerAccess && (
          <Alert variant="warning" title="Access method required">
            Set a root password or select a customer SSH public key before creating the VPS. SSH keys are recommended.
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
            Create and Deploy VPS
          </Button>
          <Button type="button" variant="secondary" size="lg" onClick={() => navigate('/vps')}>
            Cancel
          </Button>
        </div>
      </form>

      <Modal open={sshModalOpen} onClose={() => setSSHModalOpen(false)} title="Add Customer SSH Public Key">
        <form onSubmit={handleCreateSSH} className="space-y-4">
          <Input label="Name" value={sshForm.name} onChange={(event) => setSSHForm({ ...sshForm, name: event.target.value })} required />
          <Input label="SSH user" value={sshForm.ssh_user} onChange={() => setSSHForm({ ...sshForm, ssh_user: 'root' })} helperText="VPS access uses root." readOnly required />
          <Textarea label="Public key" value={sshForm.public_key} onChange={(event) => setSSHForm({ ...sshForm, public_key: event.target.value })} rows={3} required />
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
