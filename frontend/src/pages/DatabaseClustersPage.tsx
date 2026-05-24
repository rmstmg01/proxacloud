import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Database, FileCode, Plus, RefreshCw, Rocket } from 'lucide-react';
import {
  applyDatabaseClusterTerraform,
  bootstrapDatabaseCluster,
  getDatabaseCluster,
  getDatabaseClusterK8sManifest,
  getDatabaseClusters,
  provisionDatabaseCluster,
  type DatabaseCluster,
} from '../api/databaseClusters';
import { getNetworks } from '../api/networks';
import { getNodes } from '../api/proxmox';
import { getSSHKeys } from '../api/sshKeys';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { StatusBadge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { PageSpinner } from '../components/ui/Spinner';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Input, Select } from '../components/ui/Input';
import type { NetworkProfile, ProxmoxNode, SSHKeyPair } from '../types';

const initialForm = {
  name: '',
  network_profile_id: '',
  node_1: '',
  node_2: '',
  node_3: '',
  node_ip_pool_start: '',
  cpu: '4',
  memory_mb: '8192',
  disk_gb: '100',
  mariadb_version: '11.4',
  ubuntu_image: 'local:iso/ubuntu-24.04-server-cloudimg-amd64.img',
  ssh_key_pair_id: '',
  database_name: 'pscs',
  app_username: 'pscs',
  app_password: '',
  root_password: '',
  sst_username: 'sstuser',
  sst_password: '',
  haproxy_namespace: 'pscs',
  haproxy_service_name: 'external-mariadb',
  applyTerraform: true,
  bootstrapAfterProvision: true,
  applyAnsible: true,
};

export function DatabaseClustersPage() {
  const queryClient = useQueryClient();
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [manifestID, setManifestID] = useState<number | null>(null);
  const [showDeploy, setShowDeploy] = useState(false);
  const [form, setForm] = useState(initialForm);
  const [lastResult, setLastResult] = useState<{ clusterID: number; message: string; manifest?: string } | null>(null);
  const {
    data: clusters,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['database-clusters'],
    queryFn: getDatabaseClusters,
    refetchInterval: 30_000,
  });
  const { data: networks } = useQuery({ queryKey: ['networks'], queryFn: getNetworks });
  const { data: proxmoxNodes } = useQuery({ queryKey: ['proxmox-nodes'], queryFn: getNodes });
  const { data: sshKeys } = useQuery({ queryKey: ['ssh-keys'], queryFn: getSSHKeys });
  const selectedCluster = clusters?.find((cluster) => cluster.id === selectedID) ?? null;
  const detailQuery = useQuery({
    queryKey: ['database-cluster', selectedID],
    queryFn: () => getDatabaseCluster(selectedID ?? 0),
    enabled: selectedID !== null,
  });
  const manifestQuery = useQuery({
    queryKey: ['database-cluster-manifest', manifestID],
    queryFn: () => getDatabaseClusterK8sManifest(manifestID ?? 0),
    enabled: manifestID !== null,
  });
  const availableNodes = proxmoxNodes?.filter((node) => node.enabled) ?? [];
  const provisionMutation = useMutation({
    mutationFn: provisionDatabaseCluster,
    onSuccess: async (resp) => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
      let message = `Provision workspace generated at ${resp.terraform_work_dir}.`;
      if (form.bootstrapAfterProvision) {
        const boot = await bootstrapDatabaseCluster(resp.cluster_id, {
          ssh_key_pair_id: Number(form.ssh_key_pair_id),
          apply: form.applyAnsible,
        });
        message += ` Bootstrap workspace generated at ${boot.ansible_work_dir}.`;
        queryClient.invalidateQueries({ queryKey: ['database-cluster', resp.cluster_id] });
      }
      setLastResult({ clusterID: resp.cluster_id, message, manifest: resp.kubernetes_yaml });
      setSelectedID(resp.cluster_id);
      setManifestID(resp.cluster_id);
      setShowDeploy(false);
      setForm(initialForm);
    },
  });
  const applyTerraformMutation = useMutation({
    mutationFn: applyDatabaseClusterTerraform,
    onSuccess: (resp) => {
      queryClient.invalidateQueries({ queryKey: ['database-clusters'] });
      queryClient.invalidateQueries({ queryKey: ['database-cluster', resp.cluster_id] });
      const warningText = resp.warnings?.length ? ` ${resp.warnings.join(' ')}` : '';
      setLastResult({
        clusterID: resp.cluster_id,
        message: `Terraform apply status: ${resp.status}. Workspace: ${resp.terraform_work_dir}.${warningText}`,
      });
    },
  });

  const updateField = (field: keyof typeof initialForm, value: string | boolean) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const submitDeploy = (event: FormEvent) => {
    event.preventDefault();
    const nodeNames = [form.node_1, form.node_2, form.node_3].map((v) => v.trim()).filter(Boolean);
    const primaryNode = availableNodes.find((node) => node.node_name === form.node_1);
    provisionMutation.mutate({
      name: form.name.trim(),
      network_profile_id: Number(form.network_profile_id),
      proxmox_credential_id: primaryNode?.credential_id ?? 0,
      proxmox_node_names: nodeNames,
      node_ip_pool_start: form.node_ip_pool_start.trim(),
      cpu: Number(form.cpu),
      memory_mb: Number(form.memory_mb),
      disk_gb: Number(form.disk_gb),
      mariadb_version: form.mariadb_version.trim(),
      ubuntu_image: form.ubuntu_image.trim(),
      ssh_key_pair_id: Number(form.ssh_key_pair_id),
      database_name: form.database_name.trim(),
      app_username: form.app_username.trim(),
      app_password: form.app_password,
      root_password: form.root_password,
      sst_username: form.sst_username.trim(),
      sst_password: form.sst_password,
      haproxy_namespace: form.haproxy_namespace.trim(),
      haproxy_service_name: form.haproxy_service_name.trim(),
      apply: form.applyTerraform,
    });
  };

  return (
    <Layout>
      <PageHeader
        title="Database Clusters"
        description="Manage external MariaDB Galera clusters for Kubernetes workloads"
        actions={
          <div className="flex items-center gap-2">
            <Button onClick={() => setShowDeploy((value) => !value)}>
              <Plus className="h-4 w-4" />
              Deploy Database
            </Button>
            <Button variant="secondary" onClick={() => refetch()} loading={isFetching}>
              <RefreshCw className="h-4 w-4" />
              Refresh
            </Button>
          </div>
        }
      />

      {isLoading && <PageSpinner />}
      {error && <ErrorMessage error={error} />}
      {detailQuery.error && <ErrorMessage error={detailQuery.error} />}
      {manifestQuery.error && <ErrorMessage error={manifestQuery.error} />}
      {provisionMutation.error && <ErrorMessage error={provisionMutation.error} />}
      {applyTerraformMutation.error && <ErrorMessage error={applyTerraformMutation.error} />}
      <Alert variant="info" className="mb-6" title="Deploy creates infrastructure by default">
        Database cluster deploy now runs Terraform and Galera bootstrap when the form options are enabled. Uncheck the apply options only when you want reviewable workspaces without changing Proxmox.
      </Alert>
      {lastResult && (
        <Alert variant="success" className="mb-6" title="Database cluster workspace ready">
          {lastResult.message}
        </Alert>
      )}
      {showDeploy && (
        <DeployDatabaseForm
          form={form}
          networks={networks ?? []}
          nodes={availableNodes}
          sshKeys={sshKeys ?? []}
          loading={provisionMutation.isPending}
          onChange={updateField}
          onSubmit={submitDeploy}
        />
      )}

      {clusters && (
        <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_420px] gap-6">
          <Card>
            {clusters.length === 0 ? (
              <div className="text-center py-16">
                <Database className="h-9 w-9 text-slate-300 mx-auto mb-3" />
                <p className="text-slate-500 text-sm">No database clusters have been provisioned yet.</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Name</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Engine</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Endpoint</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Status</th>
                      <th className="text-left px-6 py-3 font-medium text-slate-600">Created</th>
                      <th className="px-6 py-3" />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {clusters.map((cluster) => (
                      <tr key={cluster.id} className="hover:bg-slate-50 transition-colors">
                        <td className="px-6 py-4 font-medium text-slate-900">{cluster.name}</td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.engine} {cluster.mariadb_version}
                        </td>
                        <td className="px-6 py-4 text-slate-600">
                          {cluster.haproxy_service_name}.{cluster.haproxy_namespace}
                        </td>
                        <td className="px-6 py-4">
                          <StatusBadge status={cluster.status} />
                        </td>
                        <td className="px-6 py-4 text-slate-500">
                          {new Date(cluster.created_at).toLocaleDateString()}
                        </td>
                        <td className="px-6 py-4">
                          <div className="flex items-center justify-end gap-2">
                            <Button variant="secondary" size="sm" onClick={() => setSelectedID(cluster.id)}>
                              Details
                            </Button>
                            <Button
                              variant="success"
                              size="sm"
                              onClick={() => applyTerraformMutation.mutate(cluster.id)}
                              loading={applyTerraformMutation.isPending && applyTerraformMutation.variables === cluster.id}
                            >
                              Create VMs
                            </Button>
                            <Button variant="secondary" size="sm" onClick={() => setManifestID(cluster.id)}>
                              <FileCode className="h-3.5 w-3.5" />
                              Manifest
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <div className="space-y-6">
            <ClusterDetail cluster={detailQuery.data ?? selectedCluster} loading={detailQuery.isFetching} />
            {manifestQuery.data && (
              <Card>
                <CardHeader>
                  <CardTitle>Kubernetes Manifest</CardTitle>
                </CardHeader>
                <CardContent>
                  <pre className="max-h-96 overflow-auto rounded-md bg-slate-950 p-4 text-xs text-slate-100">
                    {manifestQuery.data.manifest}
                  </pre>
                </CardContent>
              </Card>
            )}
          </div>
        </div>
      )}
    </Layout>
  );
}

function ClusterDetail({ cluster, loading }: { cluster: DatabaseCluster | null; loading: boolean }) {
  if (!cluster) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Details</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-slate-500">Select a database cluster to inspect placement and generated workspaces.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Details</CardTitle>
      </CardHeader>
      <CardContent>
        {loading && <p className="mb-3 text-xs text-slate-400">Refreshing details...</p>}
        <dl className="space-y-3 text-sm">
          <Field label="Database" value={cluster.database_name} />
          <Field label="App user" value={cluster.app_username} />
          <Field label="Terraform" value={cluster.terraform_work_dir || 'Not generated'} />
          <Field label="Ansible" value={cluster.ansible_work_dir || 'Not generated'} />
        </dl>
        <div className="mt-5 border-t border-slate-100 pt-4">
          <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">Nodes</p>
          <div className="space-y-2">
            {(cluster.nodes ?? []).map((node) => (
              <div key={node.id} className="rounded-md border border-slate-200 px-3 py-2">
                <div className="flex items-center justify-between gap-3">
                  <p className="font-medium text-slate-900">{node.vm_name}</p>
                  <StatusBadge status={node.status} />
                </div>
                <p className="mt-1 text-xs text-slate-500">
                  {node.ip_address} on {node.proxmox_node_name}
                </p>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function DeployDatabaseForm({
  form,
  networks,
  nodes,
  sshKeys,
  loading,
  onChange,
  onSubmit,
}: {
  form: typeof initialForm;
  networks: NetworkProfile[];
  nodes: ProxmoxNode[];
  sshKeys: SSHKeyPair[];
  loading: boolean;
  onChange: (field: keyof typeof initialForm, value: string | boolean) => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const selectedNodes = [form.node_1, form.node_2, form.node_3].filter(Boolean);
  const duplicateNodes = new Set(selectedNodes).size !== selectedNodes.length;
  return (
    <Card className="mb-6">
      <CardHeader>
        <div className="flex items-center justify-between gap-4">
          <div>
            <CardTitle>Deploy MariaDB Galera</CardTitle>
            <p className="mt-1 text-sm text-slate-500">Create three database VMs across three Proxmox nodes.</p>
          </div>
          <Rocket className="h-5 w-5 text-indigo-500" />
        </div>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-6">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Input label="Cluster name" value={form.name} onChange={(e) => onChange('name', e.target.value)} placeholder="pscs-prod-db" required />
            <Select label="Network" value={form.network_profile_id} onChange={(e) => onChange('network_profile_id', e.target.value)} required>
              <option value="">Select network</option>
              {networks.map((network) => (
                <option key={network.id} value={network.id}>
                  {network.name} ({network.cidr})
                </option>
              ))}
            </Select>
            <Input label="Credential mode" value="Auto from selected nodes" disabled />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <NodeSelect label="DB node 1" value={form.node_1} nodes={nodes} onChange={(value) => onChange('node_1', value)} />
            <NodeSelect label="DB node 2" value={form.node_2} nodes={nodes} onChange={(value) => onChange('node_2', value)} />
            <NodeSelect label="DB node 3" value={form.node_3} nodes={nodes} onChange={(value) => onChange('node_3', value)} />
          </div>
          {duplicateNodes && <Alert variant="warning">Choose three distinct Proxmox nodes.</Alert>}
          {nodes.length < 3 && (
            <Alert variant="warning">At least three enabled Proxmox nodes are required.</Alert>
          )}

          <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
            <Input label="First DB IP" value={form.node_ip_pool_start} onChange={(e) => onChange('node_ip_pool_start', e.target.value)} placeholder="192.168.8.201" required />
            <Input label="CPU cores" type="number" min="1" value={form.cpu} onChange={(e) => onChange('cpu', e.target.value)} required />
            <Input label="Memory MB" type="number" min="4096" value={form.memory_mb} onChange={(e) => onChange('memory_mb', e.target.value)} required />
            <Input label="Disk GB" type="number" min="40" value={form.disk_gb} onChange={(e) => onChange('disk_gb', e.target.value)} required />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Input label="MariaDB version" value={form.mariadb_version} onChange={(e) => onChange('mariadb_version', e.target.value)} required />
            <Input
              label="Ubuntu image"
              value={form.ubuntu_image}
              onChange={(e) => onChange('ubuntu_image', e.target.value)}
              placeholder="local:iso/ubuntu-24.04-server-cloudimg-amd64.img"
              helperText="Default Ubuntu 24.04 cloud image path; edit if your Proxmox storage path is different."
              required
            />
            <Select label="SSH key" value={form.ssh_key_pair_id} onChange={(e) => onChange('ssh_key_pair_id', e.target.value)} required>
              <option value="">Select SSH key</option>
              {sshKeys.map((key) => (
                <option key={key.id} value={key.id}>
                  {key.name} ({key.ssh_user})
                </option>
              ))}
            </Select>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Input label="Database name" value={form.database_name} onChange={(e) => onChange('database_name', e.target.value)} required />
            <Input label="App user" value={form.app_username} onChange={(e) => onChange('app_username', e.target.value)} required />
            <Input label="App password" type="password" value={form.app_password} onChange={(e) => onChange('app_password', e.target.value)} required />
            <Input label="Root password" type="password" value={form.root_password} onChange={(e) => onChange('root_password', e.target.value)} required />
            <Input label="SST user" value={form.sst_username} onChange={(e) => onChange('sst_username', e.target.value)} required />
            <Input label="SST password" type="password" value={form.sst_password} onChange={(e) => onChange('sst_password', e.target.value)} required />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Input label="Kubernetes namespace" value={form.haproxy_namespace} onChange={(e) => onChange('haproxy_namespace', e.target.value)} required />
            <Input label="Database service name" value={form.haproxy_service_name} onChange={(e) => onChange('haproxy_service_name', e.target.value)} required />
          </div>

          <div className="flex flex-wrap gap-4 border-t border-slate-100 pt-4">
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input type="checkbox" checked={form.applyTerraform} onChange={(e) => onChange('applyTerraform', e.target.checked)} />
              Create Proxmox VMs now
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input type="checkbox" checked={form.bootstrapAfterProvision} onChange={(e) => onChange('bootstrapAfterProvision', e.target.checked)} />
              Generate Galera bootstrap workspace
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-700">
              <input type="checkbox" checked={form.applyAnsible} onChange={(e) => onChange('applyAnsible', e.target.checked)} disabled={!form.bootstrapAfterProvision} />
              Bootstrap Galera now
            </label>
          </div>

          <div className="flex justify-end">
            <Button type="submit" loading={loading} disabled={loading}>
              <Rocket className="h-4 w-4" />
              Deploy Database Cluster
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function NodeSelect({
  label,
  value,
  nodes,
  onChange,
}: {
  label: string;
  value: string;
  nodes: ProxmoxNode[];
  onChange: (value: string) => void;
}) {
  return (
    <Select label={label} value={value} onChange={(e) => onChange(e.target.value)} required>
      <option value="">Select node</option>
      {nodes.map((node) => (
        <option key={node.id} value={node.node_name}>
          {node.node_name} ({Math.max(0, Math.round((node.memory_total_bytes - node.memory_used_bytes) / 1024 / 1024 / 1024))} GB free)
        </option>
      ))}
    </Select>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
      <dd className="mt-1 break-words text-slate-800">{value}</dd>
    </div>
  );
}
