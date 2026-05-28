import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Check, Clipboard, Play, RefreshCw, Terminal } from 'lucide-react';
import {
  getClusters,
  getClusterNodes,
  getKubectlCommandRun,
  getNodeCommandRun,
  startKubectlCommand,
  startNodeCommand,
} from '../api/clusters';
import { Layout, PageHeader } from '../components/Layout';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Select, Textarea } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';
import { StatusBadge } from '../components/ui/Badge';
import type { KubectlCommandRun, NodeCommandKind, NodeCommandRun } from '../types';

type CommandMode = 'kubernetes' | NodeCommandKind;

const COMMAND_PRESETS: Record<CommandMode, Array<{ label: string; command: string }>> = {
  kubernetes: [
    { label: 'Pods and events', command: 'kubectl get pods -A\nkubectl get events -A --sort-by=.lastTimestamp' },
    { label: 'Nodes', command: 'kubectl get nodes -o wide' },
    { label: 'Namespaces', command: 'kubectl get namespaces' },
    { label: 'Cluster info', command: 'kubectl cluster-info' },
  ],
  docker: [
    { label: 'Running containers', command: 'docker ps' },
    { label: 'Images', command: 'docker images' },
    { label: 'Docker info', command: 'docker info' },
    { label: 'Container stats', command: 'docker stats --no-stream' },
  ],
  linux: [
    { label: 'System summary', command: 'uptime' },
    { label: 'Disk usage', command: 'df -h' },
    { label: 'Memory usage', command: 'free -h' },
    { label: 'Network addresses', command: 'ip addr' },
    { label: 'Kubelet status', command: 'systemctl status kubelet --no-pager' },
    { label: 'Kubelet logs', command: 'journalctl -u kubelet -n 120 --no-pager' },
  ],
};

function isNodeCommandRun(run: KubectlCommandRun | NodeCommandRun): run is NodeCommandRun {
  return 'node' in run;
}

export function CommandCenterPage() {
  const [mode, setMode] = useState<CommandMode>('kubernetes');
  const [clusterID, setClusterID] = useState(0);
  const [nodeName, setNodeName] = useState('');
  const [preset, setPreset] = useState(COMMAND_PRESETS.kubernetes[0].label);
  const [command, setCommand] = useState(COMMAND_PRESETS.kubernetes[0].command);
  const [runID, setRunID] = useState('');
  const [runKind, setRunKind] = useState<CommandMode>('kubernetes');
  const [outputCopied, setOutputCopied] = useState(false);

  const clustersQuery = useQuery({ queryKey: ['clusters'], queryFn: getClusters });

  useEffect(() => {
    if (!clusterID && clustersQuery.data?.length) {
      setClusterID(clustersQuery.data[0].id);
    }
  }, [clusterID, clustersQuery.data]);

  const nodesQuery = useQuery({
    queryKey: ['cluster-nodes', clusterID],
    queryFn: () => getClusterNodes(clusterID),
    enabled: clusterID > 0,
  });

  useEffect(() => {
    if (!nodesQuery.data?.length) {
      setNodeName('');
      return;
    }
    if (!nodeName || !nodesQuery.data.some((node) => node.vm_name === nodeName)) {
      setNodeName(nodesQuery.data[0].vm_name);
    }
  }, [nodeName, nodesQuery.data]);

  const presets = COMMAND_PRESETS[mode];

  const resetCommandFromMode = (nextMode: CommandMode) => {
    const firstPreset = COMMAND_PRESETS[nextMode][0];
    setMode(nextMode);
    setPreset(firstPreset.label);
    setCommand(firstPreset.command);
    setRunID('');
  };

  const selectedCluster = useMemo(
    () => clustersQuery.data?.find((cluster) => cluster.id === clusterID),
    [clusterID, clustersQuery.data],
  );

  const kubectlRunQuery = useQuery({
    queryKey: ['command-center-kubectl-run', clusterID, runID],
    queryFn: () => getKubectlCommandRun(clusterID, runID),
    enabled: Boolean(runID && runKind === 'kubernetes' && clusterID),
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2000 : false),
  });

  const nodeRunQuery = useQuery({
    queryKey: ['command-center-node-run', clusterID, runID],
    queryFn: () => getNodeCommandRun(clusterID, runID),
    enabled: Boolean(runID && runKind !== 'kubernetes' && clusterID),
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2000 : false),
  });

  const kubectlMutation = useMutation({
    mutationFn: (cmd: string) => startKubectlCommand(clusterID, cmd),
    onSuccess: (data) => {
      setRunKind('kubernetes');
      setRunID(data.id);
    },
  });

  const nodeMutation = useMutation({
    mutationFn: ({ kind, cmd }: { kind: NodeCommandKind; cmd: string }) =>
      startNodeCommand(clusterID, nodeName, kind, cmd),
    onSuccess: (data) => {
      setRunKind(data.kind);
      setRunID(data.id);
    },
  });

  const activeRun = (runKind === 'kubernetes' ? kubectlRunQuery.data : nodeRunQuery.data)
    ?? kubectlMutation.data
    ?? nodeMutation.data;
  const isRunning = activeRun?.status === 'running';
  const outputText = activeRun
    ? [
      `$ ${activeRun.command}`,
      isNodeCommandRun(activeRun) ? `node: ${activeRun.node}` : '',
      activeRun.output || (activeRun.status === 'running' ? 'Running...' : ''),
      activeRun.error ? `error: ${activeRun.error}` : '',
    ].filter(Boolean).join('\n')
    : '';

  const runCommand = async (event?: FormEvent) => {
    event?.preventDefault();
    const trimmed = command.trim();
    if (!clusterID || !trimmed) return;
    if (mode === 'kubernetes') {
      await kubectlMutation.mutateAsync(trimmed);
      return;
    }
    if (!nodeName) return;
    await nodeMutation.mutateAsync({ kind: mode, cmd: trimmed });
  };

  const applyPreset = (label: string) => {
    const nextPreset = presets.find((item) => item.label === label);
    setPreset(label);
    if (nextPreset) setCommand(nextPreset.command);
  };

  const copyOutput = async () => {
    if (!outputText) return;
    await navigator.clipboard.writeText(outputText);
    setOutputCopied(true);
    window.setTimeout(() => setOutputCopied(false), 1800);
  };

  if (clustersQuery.isLoading) {
    return (
      <Layout>
        <PageSpinner />
      </Layout>
    );
  }

  return (
    <Layout>
      <PageHeader
        title="Command Center"
        description="Run approved Kubernetes, Docker, and Linux inspection commands against managed clusters and nodes."
      />

      {!clustersQuery.data?.length ? (
        <Alert variant="info" title="No clusters found">
          Create or import a cluster before running commands.
        </Alert>
      ) : (
        <div className="grid grid-cols-1 xl:grid-cols-[22rem_1fr] gap-6">
          <Card>
            <CardHeader>
              <CardTitle>Target</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <Select
                label="Command type"
                value={mode}
                onChange={(event) => resetCommandFromMode(event.target.value as CommandMode)}
              >
                <option value="kubernetes">Kubernetes</option>
                <option value="docker">Docker</option>
                <option value="linux">Linux</option>
              </Select>
              <Select
                label="Cluster"
                value={clusterID}
                onChange={(event) => {
                  setClusterID(Number(event.target.value));
                  setRunID('');
                }}
              >
                {clustersQuery.data.map((cluster) => (
                  <option key={cluster.id} value={cluster.id}>
                    {cluster.name}
                  </option>
                ))}
              </Select>
              {mode !== 'kubernetes' && (
                <Select
                  label="Node"
                  value={nodeName}
                  onChange={(event) => {
                    setNodeName(event.target.value);
                    setRunID('');
                  }}
                >
                  {nodesQuery.data?.map((node) => (
                    <option key={node.id} value={node.vm_name}>
                      {node.vm_name} {node.ip_address ? `(${node.ip_address})` : ''}
                    </option>
                  ))}
                </Select>
              )}
              <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-600">
                <p className="font-medium text-slate-800">{selectedCluster?.name}</p>
                <p className="mt-1">{mode === 'kubernetes' ? 'Cluster kubeconfig selected automatically.' : 'Node command runs over saved bootstrap SSH settings.'}</p>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between gap-3">
              <CardTitle>Command</CardTitle>
              {activeRun && <StatusBadge status={activeRun.status} />}
            </CardHeader>
            <CardContent>
              <form onSubmit={runCommand} className="space-y-4">
                <Select label="Preset" value={preset} onChange={(event) => applyPreset(event.target.value)}>
                  {presets.map((item) => (
                    <option key={item.label} value={item.label}>
                      {item.label}
                    </option>
                  ))}
                </Select>
                <Textarea
                  label="Command"
                  value={command}
                  onChange={(event) => setCommand(event.target.value)}
                  rows={mode === 'kubernetes' ? 6 : 4}
                  className="font-mono text-xs"
                  helperText={
                    mode === 'kubernetes'
                      ? 'One kubectl command per line. Kubeconfig is injected by the backend.'
                      : 'Only approved read-only Docker/Linux commands are allowed.'
                  }
                />
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="submit"
                    loading={kubectlMutation.isPending || nodeMutation.isPending}
                    disabled={!clusterID || !command.trim() || (mode !== 'kubernetes' && !nodeName)}
                  >
                    <Play className="h-4 w-4" />
                    Run
                  </Button>
                  {runID && (
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => (runKind === 'kubernetes' ? kubectlRunQuery.refetch() : nodeRunQuery.refetch())}
                      loading={(kubectlRunQuery.isFetching || nodeRunQuery.isFetching) && !isRunning}
                    >
                      <RefreshCw className={`h-4 w-4 ${isRunning ? 'animate-spin' : ''}`} />
                      Poll
                    </Button>
                  )}
                  <span className="inline-flex items-center gap-2 text-sm text-slate-500">
                    <Terminal className="h-4 w-4" />
                    {mode === 'kubernetes' ? 'cluster' : nodeName || 'node'}
                  </span>
                </div>
              </form>

              {kubectlMutation.error && <ErrorMessage error={kubectlMutation.error} />}
              {nodeMutation.error && <ErrorMessage error={nodeMutation.error} />}
              {kubectlRunQuery.error && <ErrorMessage error={kubectlRunQuery.error} />}
              {nodeRunQuery.error && <ErrorMessage error={nodeRunQuery.error} />}

              {activeRun && (
                <div className="mt-5">
                  <div className="mb-1 flex items-center justify-between gap-3">
                    <p className="text-sm font-medium text-slate-700">Output</p>
                    <button
                      type="button"
                      onClick={copyOutput}
                      className="inline-flex items-center gap-2 rounded-md border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 hover:text-slate-900"
                      aria-label={outputCopied ? 'Copied command output' : 'Copy command output'}
                      title={outputCopied ? 'Copied' : 'Copy output'}
                    >
                      {outputCopied ? <Check className="h-3.5 w-3.5" /> : <Clipboard className="h-3.5 w-3.5" />}
                      {outputCopied ? 'Copied' : 'Copy output'}
                    </button>
                  </div>
                  <pre className="min-h-[22rem] max-h-[44rem] overflow-auto rounded-md border border-slate-800 bg-slate-950 px-3 py-2 font-mono text-xs text-slate-100 whitespace-pre-wrap">
                    {outputText}
                  </pre>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}
    </Layout>
  );
}
