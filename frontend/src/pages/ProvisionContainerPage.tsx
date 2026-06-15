import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Plus } from 'lucide-react';
import { createContainerInstance, createContainerTemplate, getContainerTemplates } from '../api/containers';
import { getNetworks } from '../api/networks';
import { getCredentials, getNodes } from '../api/proxmox';
import { getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Button } from '../components/ui/Button';
import { Input, Select, Textarea } from '../components/ui/Input';
import { PageSpinner } from '../components/ui/Spinner';

export function ProvisionContainerPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: credentials } = useQuery({ queryKey: ['proxmox-credentials'], queryFn: getCredentials });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: sshKeys } = useQuery({ queryKey: ['ssh-keys'], queryFn: getSSHKeys });
  const { data: templates } = useQuery({ queryKey: ['container-templates'], queryFn: getContainerTemplates });

  const [form, setForm] = useState({
    name: '',
    description: '',
    proxmox_credential_id: 0,
    proxmox_node_name: '',
    network_profile_id: 0,
    template_id: 0,
    template_path: '',
    ssh_key_pair_id: 0,
    ip_mode: 'dhcp' as 'dhcp' | 'manual' | 'auto',
    ip_address: '',
    cpu: 1,
    memory_mb: 512,
    disk_gb: 8,
    root_password: '',
    unprivileged: true,
    nesting: true,
    apply: true,
  });
  const [templateForm, setTemplateForm] = useState({
    name: '',
    template_path: '',
    os_type: 'debian',
    description: '',
    is_default: false,
  });

  const enabledNodes = useMemo(
    () => (proxmoxNodes ?? []).filter((node) => node.enabled && (!form.proxmox_credential_id || node.credential_id === form.proxmox_credential_id)),
    [proxmoxNodes, form.proxmox_credential_id],
  );

  useEffect(() => {
    const defaultNetwork = networks?.find((network) => network.is_default) ?? networks?.[0];
    if (defaultNetwork && form.network_profile_id === 0) setForm((prev) => ({ ...prev, network_profile_id: defaultNetwork.id }));
  }, [networks, form.network_profile_id]);

  useEffect(() => {
    const defaultTemplate = templates?.find((template) => template.is_default) ?? templates?.[0];
    if (defaultTemplate && form.template_id === 0 && form.template_path === '') {
      setForm((prev) => ({ ...prev, template_id: defaultTemplate.id, template_path: defaultTemplate.template_path }));
    }
  }, [templates, form.template_id, form.template_path]);

  useEffect(() => {
    if (form.proxmox_credential_id === 0 && credentials?.length) setForm((prev) => ({ ...prev, proxmox_credential_id: credentials[0].id }));
  }, [credentials, form.proxmox_credential_id]);

  useEffect(() => {
    if (form.proxmox_node_name !== '' && enabledNodes.some((node) => node.node_name === form.proxmox_node_name)) return;
    if (enabledNodes.length > 0) setForm((prev) => ({ ...prev, proxmox_node_name: enabledNodes[0].node_name }));
  }, [enabledNodes, form.proxmox_node_name]);

  const createMutation = useMutation({
    mutationFn: createContainerInstance,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      navigate(`/containers/${result.container_id}`);
    },
  });
  const templateMutation = useMutation({
    mutationFn: createContainerTemplate,
    onSuccess: (template) => {
      queryClient.invalidateQueries({ queryKey: ['container-templates'] });
      setForm((prev) => ({ ...prev, template_id: template.id, template_path: template.template_path }));
      setTemplateForm({ name: '', template_path: '', os_type: 'debian', description: '', is_default: false });
    },
  });

  const setField = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) => setForm((prev) => ({ ...prev, [key]: value }));

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    createMutation.mutate(form);
  };

  return (
    <Layout>
      <PageHeader
        title="New Container"
        description="Provision a standalone Proxmox LXC container"
        actions={<Link to="/containers" className="inline-flex items-center gap-1 text-slate-600 hover:text-slate-900 text-sm"><ArrowLeft className="h-4 w-4" /> Back</Link>}
      />
      {createMutation.error && <ErrorMessage error={createMutation.error} />}
      {templateMutation.error && <ErrorMessage error={templateMutation.error} />}
      {(!networks || !credentials || !proxmoxNodes || !templates) && <PageSpinner />}

      <form onSubmit={handleSubmit} className="space-y-6 max-w-3xl">
        <Card>
          <CardHeader><CardTitle>Container</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Input label="Name" value={form.name} onChange={(e) => setField('name', e.target.value)} required placeholder="app-container-01" />
              <Input label="Description" value={form.description} onChange={(e) => setField('description', e.target.value)} />
              <Select label="Proxmox credential" value={form.proxmox_credential_id} onChange={(e) => setField('proxmox_credential_id', Number(e.target.value))} required>
                <option value={0}>Select credential</option>
                {credentials?.map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}
              </Select>
              <Select label="Proxmox node" value={form.proxmox_node_name} onChange={(e) => setField('proxmox_node_name', e.target.value)} required>
                <option value="">Select node</option>
                {enabledNodes.map((node) => <option key={node.id} value={node.node_name}>{node.node_name}</option>)}
              </Select>
            </div>
            <Textarea label="Notes" value={form.description} onChange={(e) => setField('description', e.target.value)} rows={2} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Resources & Network</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Input label="CPU cores" type="number" min="1" value={form.cpu} onChange={(e) => setField('cpu', Number(e.target.value))} required />
              <Input label="Memory MB" type="number" min="128" value={form.memory_mb} onChange={(e) => setField('memory_mb', Number(e.target.value))} required />
              <Input label="Disk GB" type="number" min="1" value={form.disk_gb} onChange={(e) => setField('disk_gb', Number(e.target.value))} required />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <Select label="Subnet" value={form.network_profile_id} onChange={(e) => setField('network_profile_id', Number(e.target.value))} required>
                <option value={0}>Select subnet</option>
                {networks?.map((network) => <option key={network.id} value={network.id}>{network.vpc_name || 'default-vpc'} / {network.name} ({network.cidr})</option>)}
              </Select>
              <Select label="IP mode" value={form.ip_mode} onChange={(e) => setField('ip_mode', e.target.value as 'dhcp' | 'manual' | 'auto')}>
                <option value="dhcp">DHCP</option>
                <option value="auto">Auto allocate</option>
                <option value="manual">Manual</option>
              </Select>
              {form.ip_mode === 'manual' && <Input label="IP address" value={form.ip_address} onChange={(e) => setField('ip_address', e.target.value)} required />}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle>Template & Access</CardTitle></CardHeader>
          <CardContent className="space-y-4">
            <Select
              label="Container template"
              value={form.template_id}
              onChange={(e) => {
                const id = Number(e.target.value);
                const template = templates?.find((item) => item.id === id);
                setForm((prev) => ({ ...prev, template_id: id, template_path: template?.template_path || prev.template_path }));
              }}
            >
              <option value={0}>Custom template path</option>
              {templates?.map((template) => <option key={template.id} value={template.id}>{template.name} - {template.template_path}</option>)}
            </Select>
            <Input label="Template path" value={form.template_path} onChange={(e) => setField('template_path', e.target.value)} required placeholder="local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst" />
            <Select label="SSH key" value={form.ssh_key_pair_id} onChange={(e) => setField('ssh_key_pair_id', Number(e.target.value))}>
              <option value={0}>No SSH key</option>
              {sshKeys?.map((key) => <option key={key.id} value={key.id}>{key.name} ({key.ssh_user})</option>)}
            </Select>
            <Input label="Root password" type="password" value={form.root_password} onChange={(e) => setField('root_password', e.target.value)} required />
            <div className="flex flex-wrap gap-6">
              <label className="inline-flex items-center gap-2">
                <input type="checkbox" checked={form.unprivileged} onChange={(e) => setField('unprivileged', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                <span className="text-sm text-slate-700">Unprivileged container</span>
              </label>
              <label className="inline-flex items-center gap-2">
                <input type="checkbox" checked={form.nesting} onChange={(e) => setField('nesting', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                <span className="text-sm text-slate-700">Enable nesting</span>
              </label>
              <label className="inline-flex items-center gap-2">
                <input type="checkbox" checked={form.apply} onChange={(e) => setField('apply', e.target.checked)} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
                <span className="text-sm text-slate-700">Deploy immediately</span>
              </label>
            </div>
          </CardContent>
        </Card>

        <div className="flex items-center justify-end gap-3">
          <Button type="button" variant="secondary" onClick={() => navigate('/containers')}>Cancel</Button>
          <Button type="submit" loading={createMutation.isPending} disabled={!form.root_password.trim() || !form.template_path.trim()}>
            {form.apply ? 'Create and Deploy Container' : 'Create Container'}
          </Button>
        </div>
      </form>

      <Card className="mt-8 max-w-3xl">
        <CardHeader><CardTitle>Add Template</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={(e) => { e.preventDefault(); templateMutation.mutate(templateForm); }} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Input label="Template name" value={templateForm.name} onChange={(e) => setTemplateForm({ ...templateForm, name: e.target.value })} required />
              <Input label="OS type" value={templateForm.os_type} onChange={(e) => setTemplateForm({ ...templateForm, os_type: e.target.value })} required />
            </div>
            <Input label="Template path" value={templateForm.template_path} onChange={(e) => setTemplateForm({ ...templateForm, template_path: e.target.value })} required />
            <Textarea label="Description" value={templateForm.description} onChange={(e) => setTemplateForm({ ...templateForm, description: e.target.value })} rows={2} />
            <label className="inline-flex items-center gap-2">
              <input type="checkbox" checked={templateForm.is_default} onChange={(e) => setTemplateForm({ ...templateForm, is_default: e.target.checked })} className="h-4 w-4 rounded border-slate-300 text-indigo-600" />
              <span className="text-sm text-slate-700">Make default template</span>
            </label>
            <div>
              <Button type="submit" variant="secondary" loading={templateMutation.isPending}>
                <Plus className="h-4 w-4" /> Add Template
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {form.unprivileged === false && (
        <Alert variant="warning" title="Privileged container selected">
          Privileged LXC containers have broader host access. Use them only when workload requirements are clear.
        </Alert>
      )}
    </Layout>
  );
}
