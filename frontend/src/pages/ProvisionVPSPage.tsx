import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, CheckCircle2, Plus } from 'lucide-react';
import { createVPSInstance, discoverVPSCloudInitImagesForRegion } from '../api/vps';
import { getNetworks } from '../api/networks';
import { getRegions } from '../api/regions';
import { createCustomerSSHKey, getCustomerSSHKeys } from '../api/customerSshKeys';
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

  const [sshModalOpen, setSSHModalOpen] = useState(false);
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
    apply: true,
  });

  const enabledRegions = useMemo(() => (regions ?? []).filter((region) => region.enabled && region.nodes.some((node) => node.enabled)), [regions]);
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

  const createDisabled =
    form.region_id === 0 ||
    form.network_profile_id === 0 ||
    form.cloud_init_image_path.trim() === '' ||
    form.customer_ssh_key_id === 0;

  useEffect(() => {
    const defaultRegion = enabledRegions.find((region) => region.is_default) ?? enabledRegions[0];
    if (defaultRegion && form.region_id === 0) {
      setForm((prev) => ({ ...prev, region_id: defaultRegion.id }));
    }
  }, [enabledRegions, form.region_id]);

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
    mutationFn: () => discoverVPSCloudInitImagesForRegion(form.region_id),
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
    if (form.region_id === 0) {
      return;
    }
    const discoveryKey = `region:${form.region_id}`;
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
  }, [discoverImagesMutation, form.region_id]);

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
            <Select
              label="Location"
              value={form.region_id}
              onChange={(event) => {
                setForm((prev) => ({
                  ...prev,
                  region_id: Number(event.target.value),
                  network_profile_id: 0,
                  cloud_init_image_id: 0,
                  cloud_init_image_path: '',
                }));
                setDiscoveredImages([]);
                lastAutoDiscoveryKey.current = '';
              }}
              required
            >
              <option value={0}>-- Select location --</option>
              {enabledRegions.map((region) => (
                <option key={region.id} value={region.id}>
                  {region.name}
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
                required
              >
                <option value={0}>-- Select saved public key --</option>
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
            <Input
              label="SSH user"
              value={form.ssh_user}
              onChange={() => setField('ssh_user', 'root')}
              helperText="VPS login details always use the root user."
              readOnly
              required
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Cloud Init Image</CardTitle>
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
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                            <span>{architectureLabel(image.architecture)}</span>
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
              ['Access', 'root via customer SSH public key'],
              ['Resources', `${form.cpu} vCPU, ${form.memory_mb} MB RAM, ${form.disk_gb} GB disk`],
              ['IP assignment', form.ip_mode === 'manual' ? form.ip_address || 'Manual IP required' : form.ip_mode === 'dhcp' ? 'DHCP' : 'Auto assign from network'],
            ].map(([label, value]) => (
              <div key={label} className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2">
                <p className="text-xs font-medium uppercase text-slate-500">{label}</p>
                <p className="mt-1 truncate text-sm font-semibold text-slate-900">{value}</p>
              </div>
            ))}
          </CardContent>
        </Card>

        {form.customer_ssh_key_id === 0 && (
          <Alert variant="warning" title="Access method required">
            Select or add a customer SSH public key before creating the VPS.
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
