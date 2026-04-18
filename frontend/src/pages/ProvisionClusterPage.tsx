import { useEffect, useState, type FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { checkClusterName, provisionAndBootstrapCluster, provisionCluster } from '../api/clusters';
import { getNetworks, getNodeNetworkTags } from '../api/networks';
import { getCredentials, getNodes } from '../api/proxmox';
import { getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { Input, Select, Textarea } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';

export function ProvisionClusterPage() {
  const navigate = useNavigate();
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: nodeTags } = useQuery({ queryKey: ['network-node-tags'], queryFn: getNodeNetworkTags });
  const { data: credentials } = useQuery({ queryKey: ['proxmox-credentials'], queryFn: getCredentials });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: sshKeys } = useQuery({ queryKey: ['ssh-keys'], queryFn: getSSHKeys });
  const [debouncedName, setDebouncedName] = useState('');

  const [form, setForm] = useState({
    name: '',
    topology_type: 'tri',
    control_plane_count: 3,
    worker_count: 3,
    network_profile_id: 0,
    proxmox_credential_id: 0,
    node_ip_pool_start: '192.168.8.100',
    kubernetes_version: 'v1.31.4',
    ubuntu_image: 'local:iso/ubuntu-24.04-server-cloudimg-amd64.img',
    ssh_public_key: '',
    control_plane_cpu: 4,
    control_plane_ram_mb: 8192,
    control_plane_disk_gb: 80,
    worker_cpu: 4,
    worker_ram_mb: 8192,
    worker_disk_gb: 120,
    ssh_key_pair_id: 0,
    node_credential_ids: {} as Record<string, number>,
    node_network_profile_ids: {} as Record<string, number>,
    node_ip_pool_start_by_node: {} as Record<string, string>,
    bootstrap_after_provision: false,
    bootstrap_ssh_user: 'ubuntu',
    bootstrap_deploy_kube_vip: false,
    bootstrap_control_plane_vip: '',
    bootstrap_pod_cidr: '10.244.0.0/16',
    bootstrap_service_cidr: '10.96.0.0/12',
    bootstrap_cni: 'calico',
    apply: false,
  });

  const enabledNodesByName = Array.from(
    new Map((proxmoxNodes ?? []).filter((n) => n.enabled).map((n) => [n.node_name, n])).values(),
  );

  useEffect(() => {
    if (enabledNodesByName.length === 0) return;
    setForm((prev) => {
      const nextMap = { ...prev.node_credential_ids };
      let changed = false;
      for (const node of enabledNodesByName) {
        if (!nextMap[node.node_name] && node.credential_id > 0) {
          nextMap[node.node_name] = node.credential_id;
          changed = true;
        }
      }
      if (!changed) return prev;
      return { ...prev, node_credential_ids: nextMap };
    });
  }, [enabledNodesByName]);

  useEffect(() => {
    if (!nodeTags || nodeTags.length === 0) return;
    setForm((prev) => {
      const nextMap = { ...prev.node_network_profile_ids };
      let changed = false;
      for (const tag of nodeTags) {
        if (!nextMap[tag.proxmox_node_name] && tag.network_profile_id > 0) {
          nextMap[tag.proxmox_node_name] = tag.network_profile_id;
          changed = true;
        }
      }
      if (!changed) return prev;
      return { ...prev, node_network_profile_ids: nextMap };
    });
  }, [nodeTags]);

  const mutation = useMutation({ mutationFn: provisionCluster });
  const combinedMutation = useMutation({ mutationFn: provisionAndBootstrapCluster });

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

    const nodeCredentialIDs = Object.fromEntries(
      Object.entries(form.node_credential_ids)
        .filter(([nodeName, credentialId]) => nodeName.trim() !== '' && Number(credentialId) > 0)
        .map(([nodeName, credentialId]) => [nodeName, Number(credentialId)]),
    );

    const nodeNetworkProfileIDs = Object.fromEntries(
      Object.entries(form.node_network_profile_ids)
        .filter(([nodeName, profileId]) => nodeName.trim() !== '' && Number(profileId) > 0)
        .map(([nodeName, profileId]) => [nodeName, Number(profileId)]),
    );

    const nodeIPPoolStartByNode = Object.fromEntries(
      Object.entries(form.node_ip_pool_start_by_node)
        .filter(([nodeName, ip]) => nodeName.trim() !== '' && ip.trim() !== '')
        .map(([nodeName, ip]) => [nodeName, ip.trim()]),
    );

    const fallbackCredentialID =
      Number(form.proxmox_credential_id) > 0
        ? Number(form.proxmox_credential_id)
        : Number(Object.values(nodeCredentialIDs)[0] ?? 0);

    const provisionPayload = {
      ...form,
      network_profile_id: Number(form.network_profile_id),
      proxmox_credential_id: fallbackCredentialID,
      ssh_key_pair_id: Number(form.ssh_key_pair_id) || undefined,
      node_credential_ids: nodeCredentialIDs,
      node_network_profile_ids: nodeNetworkProfileIDs,
      node_ip_pool_start_by_node: nodeIPPoolStartByNode,
      node_ip_pool_start: form.node_ip_pool_start.trim() || '192.168.8.100',
      apply: form.bootstrap_after_provision ? true : form.apply,
    };

    const result = form.bootstrap_after_provision
      ? await combinedMutation.mutateAsync({
          provision: provisionPayload,
          bootstrap: {
            ssh_user: form.bootstrap_ssh_user.trim(),
            ssh_key_pair_id: Number(form.ssh_key_pair_id) || undefined,
            deploy_kube_vip: form.bootstrap_deploy_kube_vip,
            control_plane_vip: form.bootstrap_deploy_kube_vip ? form.bootstrap_control_plane_vip.trim() : '',
            pod_cidr: form.bootstrap_pod_cidr.trim(),
            service_cidr: form.bootstrap_service_cidr.trim(),
            cni: form.bootstrap_cni.trim(),
            node_ips: {},
            apply: true,
          },
        })
      : await mutation.mutateAsync(provisionPayload);
    navigate(`/clusters/${result.cluster_id}?tab=runs`);
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

            {form.topology_type === 'tri' && (
              <div className="sm:col-span-2 rounded-lg border border-slate-200 bg-slate-50 p-4 space-y-3">
                <p className="text-sm font-medium text-slate-800">Per-node credential mapping (Tri-node HA)</p>
                <p className="text-xs text-slate-600">
                  Override credentials per Proxmox node. Leave a node on default by keeping "Use default cluster credential".
                </p>
                <p className="text-xs text-amber-700">
                  Note: "apply" runs in background. For multi-credential topologies, Terraform is applied per credential group.
                </p>
                {enabledNodesByName.length === 0 ? (
                  <p className="text-xs text-slate-500">No enabled Proxmox nodes found. Discover nodes first in the Proxmox page.</p>
                ) : (
                  <div className="space-y-2">
                    {enabledNodesByName.map((node) => (
                      <div key={node.node_name} className="grid grid-cols-1 sm:grid-cols-2 gap-3 items-end">
                        <Input
                          label="Proxmox node"
                          value={node.node_name}
                          readOnly
                        />
                        <Select
                          label="Credential"
                          value={form.node_credential_ids[node.node_name] ?? 0}
                          onChange={(e) => {
                            const selected = Number(e.target.value);
                            setForm((prev) => ({
                              ...prev,
                              node_credential_ids: {
                                ...prev.node_credential_ids,
                                [node.node_name]: selected,
                              },
                            }));
                          }}
                        >
                          <option value={0}>Use default cluster credential</option>
                          {credentials?.map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.name} ({c.endpoint})
                            </option>
                          ))}
                        </Select>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            <div className="sm:col-span-2 rounded-lg border border-slate-200 bg-slate-50 p-4 space-y-3">
              <p className="text-sm font-medium text-slate-800">Per-node network profile mapping</p>
              <p className="text-xs text-slate-600">
                Assign CIDR source per Proxmox node. If not set, cluster Network profile is used.
              </p>
              {enabledNodesByName.length === 0 ? (
                <p className="text-xs text-slate-500">No enabled Proxmox nodes found. Discover nodes first in the Proxmox page.</p>
              ) : (
                <div className="space-y-2">
                  {enabledNodesByName.map((node) => (
                    <div key={node.node_name} className="grid grid-cols-1 sm:grid-cols-3 gap-3 items-end">
                      <Input
                        label="Proxmox node"
                        value={node.node_name}
                        readOnly
                      />
                      <Select
                        label="Network profile"
                        value={form.node_network_profile_ids[node.node_name] ?? 0}
                        onChange={(e) => {
                          const selected = Number(e.target.value);
                          setForm((prev) => ({
                            ...prev,
                            node_network_profile_ids: {
                              ...prev.node_network_profile_ids,
                              [node.node_name]: selected,
                            },
                          }));
                        }}
                      >
                        <option value={0}>Use selected cluster network profile</option>
                        {networks?.map((n) => (
                          <option key={n.id} value={n.id}>
                            {n.name} ({n.cidr})
                          </option>
                        ))}
                      </Select>
                      <Input
                        label="IP pool start override (optional)"
                        value={form.node_ip_pool_start_by_node[node.node_name] ?? ''}
                        onChange={(e) => {
                          const value = e.target.value;
                          setForm((prev) => ({
                            ...prev,
                            node_ip_pool_start_by_node: {
                              ...prev.node_ip_pool_start_by_node,
                              [node.node_name]: value,
                            },
                          }));
                        }}
                        placeholder="e.g. 10.10.20.100"
                      />
                    </div>
                  ))}
                </div>
              )}
            </div>
            <Input
              label="Kubernetes version"
              value={form.kubernetes_version}
              onChange={(e) => setField('kubernetes_version', e.target.value)}
              placeholder="v1.31.4"
              required
            />
            <Select
              label="SSH key pair"
              value={form.ssh_key_pair_id}
              onChange={(e) => {
                const selectedID = Number(e.target.value);
                const selectedKey = sshKeys?.find((key) => key.id === selectedID);
                setForm((prev) => ({
                  ...prev,
                  ssh_key_pair_id: selectedID,
                  ssh_public_key: selectedKey ? selectedKey.public_key : prev.ssh_public_key,
                  bootstrap_ssh_user: selectedKey ? selectedKey.ssh_user : prev.bootstrap_ssh_user,
                }));
              }}
            >
              <option value={0}>-- Select saved key pair (optional) --</option>
              {sshKeys?.map((key) => (
                <option key={key.id} value={key.id}>
                  {key.name} ({key.ssh_user})
                </option>
              ))}
            </Select>
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
              helperText="Pick a saved key pair above or paste a public key manually"
              required={Number(form.ssh_key_pair_id) === 0}
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
              min={2048}
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
              min={2048}
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
            <label className="flex items-center gap-3 cursor-pointer mb-3">
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

            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={form.bootstrap_after_provision}
                onChange={(e) => setField('bootstrap_after_provision', e.target.checked)}
                className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
              />
              <span className="text-sm text-slate-700">Run bootstrap automatically after provision apply</span>
            </label>

            {form.bootstrap_after_provision && (
              <div className="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-4 rounded-lg border border-slate-200 bg-slate-50 p-4">
                <Input
                  label="Bootstrap SSH user"
                  value={form.bootstrap_ssh_user}
                  onChange={(e) => setField('bootstrap_ssh_user', e.target.value)}
                  required
                />
                <Input
                  label="Control-plane VIP"
                  value={form.bootstrap_control_plane_vip}
                  onChange={(e) => setField('bootstrap_control_plane_vip', e.target.value)}
                  placeholder="e.g. 192.168.8.50"
                  helperText={form.bootstrap_deploy_kube_vip ? 'Required when Deploy kube-vip is enabled for an HA endpoint.' : 'Optional. If kube-vip is off, bootstrap will use the first control-plane node directly.'}
                />
                <Input
                  label="Pod CIDR"
                  value={form.bootstrap_pod_cidr}
                  onChange={(e) => setField('bootstrap_pod_cidr', e.target.value)}
                  placeholder="10.244.0.0/16"
                />
                <Input
                  label="Service CIDR"
                  value={form.bootstrap_service_cidr}
                  onChange={(e) => setField('bootstrap_service_cidr', e.target.value)}
                  placeholder="10.96.0.0/12"
                />
                <Select
                  label="CNI"
                  value={form.bootstrap_cni}
                  onChange={(e) => setField('bootstrap_cni', e.target.value)}
                >
                  <option value="calico">calico</option>
                  <option value="flannel">flannel</option>
                </Select>
                <label className="flex items-center gap-3 self-end pb-2">
                  <input
                    type="checkbox"
                    checked={form.bootstrap_deploy_kube_vip}
                    onChange={(e) => setField('bootstrap_deploy_kube_vip', e.target.checked)}
                    className="h-4 w-4 rounded border-slate-300 text-indigo-600"
                  />
                  <span className="text-sm text-slate-700">Deploy kube-vip</span>
                </label>
                <p className="sm:col-span-2 text-xs text-amber-700">
                  Auto-bootstrap requires a saved SSH key pair with private key.
                </p>
              </div>
            )}
          </CardContent>
        </Card>

        {mutation.data && !form.bootstrap_after_provision && (
          <Alert variant="success" title="Cluster created!">
            <p>Cluster ID: <strong>{mutation.data.cluster_id}</strong> — Status: <strong>{mutation.data.status}</strong></p>
            {mutation.data.warnings.length > 0 && (
              <ul className="mt-2 list-disc list-inside text-amber-800">
                {mutation.data.warnings.map((w, i) => <li key={i}>{w}</li>)}
              </ul>
            )}
          </Alert>
        )}
        {combinedMutation.data && form.bootstrap_after_provision && (
          <Alert variant="success" title="Provision + bootstrap started!">
            <p>Cluster ID: <strong>{combinedMutation.data.cluster_id}</strong> — Status: <strong>{combinedMutation.data.status}</strong></p>
            {combinedMutation.data.warnings.length > 0 && (
              <ul className="mt-2 list-disc list-inside text-amber-800">
                {combinedMutation.data.warnings.map((w, i) => <li key={i}>{w}</li>)}
              </ul>
            )}
          </Alert>
        )}
        {mutation.error && <ErrorMessage error={mutation.error} />}
        {combinedMutation.error && <ErrorMessage error={combinedMutation.error} />}
        {nameCheckQuery.error && <ErrorMessage error={nameCheckQuery.error} />}

        <div className="flex gap-3">
          <Button
            type="submit"
            loading={mutation.isPending || combinedMutation.isPending}
            disabled={isNameTaken || nameCheckQuery.isFetching}
            size="lg"
          >
            {form.bootstrap_after_provision ? 'Provision + Bootstrap Cluster' : 'Provision Cluster'}
          </Button>
          <Button type="button" variant="secondary" size="lg" onClick={() => navigate('/clusters')}>
            Cancel
          </Button>
        </div>
      </form>
    </Layout>
  );
}
