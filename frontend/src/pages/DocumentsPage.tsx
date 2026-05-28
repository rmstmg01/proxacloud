import { AlertTriangle, BookOpen, CheckCircle2, ClipboardList, RefreshCcw, Search, Shield, Terminal } from 'lucide-react';
import { Layout, PageHeader } from '../components/Layout';
import { Badge } from '../components/ui/Badge';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';

const namespace = 'proxaops';

const quickCommands = [
  {
    title: 'Check all cluster resources',
    command: `kubectl get nodes -o wide
kubectl get pods -A
kubectl get svc -A
kubectl get ingress -A
kubectl get events -A --sort-by=.lastTimestamp`,
  },
  {
    title: 'Check ProxaOps namespace',
    command: `kubectl get all -n ${namespace}
kubectl get pods -n ${namespace} -o wide
kubectl get events -n ${namespace} --sort-by=.lastTimestamp`,
  },
  {
    title: 'Restart ProxaOps deployments',
    command: `kubectl rollout restart deployment/backend -n ${namespace}
kubectl rollout restart deployment/frontend -n ${namespace}
kubectl rollout restart deployment/gateway -n ${namespace}
kubectl rollout restart deployment/website -n ${namespace}`,
  },
  {
    title: 'Watch rollout status',
    command: `kubectl rollout status deployment/backend -n ${namespace}
kubectl rollout status deployment/frontend -n ${namespace}
kubectl rollout status deployment/gateway -n ${namespace}
kubectl rollout status deployment/website -n ${namespace}`,
  },
];

const documentSections = [
  {
    title: 'Cluster Health Checks',
    icon: CheckCircle2,
    badge: 'Basic',
    description:
      'Start here when you want to know whether the cluster is healthy before changing deployments.',
    commands: [
      {
        label: 'Nodes, control plane, and system pods',
        command: `kubectl cluster-info
kubectl get nodes -o wide
kubectl get componentstatuses
kubectl get pods -n kube-system -o wide
kubectl get pods -A --field-selector=status.phase!=Running`,
      },
      {
        label: 'Resource pressure and capacity',
        command: `kubectl top nodes
kubectl top pods -A
kubectl describe nodes`,
      },
    ],
    notes: [
      'Nodes should be Ready. NotReady usually means kubelet, networking, disk pressure, or node connectivity failed.',
      'System pods in kube-system should be Running. Repeated restarts on CoreDNS, kube-proxy, or CNI pods can affect every application.',
      'If metrics commands fail, metrics-server may not be installed or working.',
    ],
  },
  {
    title: 'Deployments And Restarts',
    icon: RefreshCcw,
    badge: 'Daily Use',
    description:
      'Use these commands when a new image has been published, configuration changed, or pods need to be recreated.',
    commands: [
      {
        label: 'Restart all ProxaOps application deployments',
        command: `kubectl rollout restart deployment/backend -n ${namespace}
kubectl rollout restart deployment/frontend -n ${namespace}
kubectl rollout restart deployment/gateway -n ${namespace}
kubectl rollout restart deployment/website -n ${namespace}
kubectl get pods -n ${namespace} -w`,
      },
      {
        label: 'Update images explicitly',
        command: `kubectl set image deployment/backend backend=ghcr.io/OWNER/backend:TAG -n ${namespace}
kubectl set image deployment/frontend frontend=ghcr.io/OWNER/frontend:TAG -n ${namespace}
kubectl set image deployment/gateway gateway=ghcr.io/OWNER/gateway:TAG -n ${namespace}
kubectl set image deployment/website website=ghcr.io/OWNER/website:TAG -n ${namespace}`,
      },
      {
        label: 'Roll back a bad deployment',
        command: `kubectl rollout history deployment/backend -n ${namespace}
kubectl rollout undo deployment/backend -n ${namespace}
kubectl rollout status deployment/backend -n ${namespace}`,
      },
    ],
    notes: [
      'Prefer versioned image tags or digests over latest. This makes rollback and audit much easier.',
      'A rollout restart recreates pods. It does not change the image tag stored in the deployment.',
      'If the same tag is reused, set imagePullPolicy to Always so each new pod checks the registry.',
    ],
  },
  {
    title: 'GHCR Image Pulling',
    icon: Shield,
    badge: 'Registry',
    description:
      'Use this when pods do not pull the latest image or private GHCR images fail authentication.',
    commands: [
      {
        label: 'Force pull policy for deployments',
        command: `kubectl patch deployment backend -n ${namespace} -p '{"spec":{"template":{"spec":{"containers":[{"name":"backend","imagePullPolicy":"Always"}]}}}}'
kubectl patch deployment frontend -n ${namespace} -p '{"spec":{"template":{"spec":{"containers":[{"name":"frontend","imagePullPolicy":"Always"}]}}}}'
kubectl patch deployment gateway -n ${namespace} -p '{"spec":{"template":{"spec":{"containers":[{"name":"gateway","imagePullPolicy":"Always"}]}}}}'
kubectl patch deployment website -n ${namespace} -p '{"spec":{"template":{"spec":{"containers":[{"name":"website","imagePullPolicy":"Always"}]}}}}'`,
      },
      {
        label: 'Create GHCR pull secret',
        command: `kubectl create secret docker-registry ghcr-secret \\
  --docker-server=ghcr.io \\
  --docker-username=GITHUB_USERNAME \\
  --docker-password=GITHUB_TOKEN \\
  --docker-email=EMAIL_ADDRESS \\
  -n ${namespace}`,
      },
      {
        label: 'Attach GHCR pull secret to the default service account',
        command: `kubectl patch serviceaccount default -n ${namespace} -p '{"imagePullSecrets":[{"name":"ghcr-secret"}]}'`,
      },
    ],
    notes: [
      'For private GHCR packages, the GitHub token needs permission to read packages.',
      'ImagePullBackOff often means the image tag is wrong, the package is private, the token is invalid, or nodes cannot reach ghcr.io.',
      'After changing pull secrets, restart the deployment so new pods use the updated credentials.',
    ],
  },
  {
    title: 'Pod Troubleshooting',
    icon: Search,
    badge: 'Errors',
    description:
      'Use describe, logs, and events together. The pod status names are clues, but the real reason is usually in events or container logs.',
    commands: [
      {
        label: 'Inspect a failing pod',
        command: `kubectl get pods -n ${namespace}
kubectl describe pod POD_NAME -n ${namespace}
kubectl logs POD_NAME -n ${namespace}
kubectl logs POD_NAME -n ${namespace} --previous`,
      },
      {
        label: 'Check by deployment',
        command: `kubectl describe deployment backend -n ${namespace}
kubectl get replicasets -n ${namespace}
kubectl describe replicaset REPLICASET_NAME -n ${namespace}`,
      },
      {
        label: 'Open a shell inside a running pod',
        command: `kubectl exec -it POD_NAME -n ${namespace} -- sh`,
      },
    ],
    notes: [
      'CrashLoopBackOff means the container starts and exits repeatedly. Check logs and the previous logs.',
      'Pending means Kubernetes could not schedule the pod. Check node capacity, taints, PVCs, and events.',
      'CreateContainerConfigError usually points to a missing Secret, ConfigMap, invalid env var reference, or invalid volume mount.',
      'RunContainerError can mean the image entrypoint, command, permissions, or mounted files are wrong.',
    ],
  },
  {
    title: 'Networking And DNS',
    icon: ClipboardList,
    badge: 'Service',
    description:
      'Use these checks when pods run but the application cannot connect internally or externally.',
    commands: [
      {
        label: 'Services, endpoints, ingress',
        command: `kubectl get svc -n ${namespace}
kubectl get endpoints -n ${namespace}
kubectl get ingress -n ${namespace}
kubectl describe svc SERVICE_NAME -n ${namespace}
kubectl describe ingress INGRESS_NAME -n ${namespace}`,
      },
      {
        label: 'DNS test from inside the cluster',
        command: `kubectl run dns-test -n ${namespace} --rm -it --image=busybox:1.36 --restart=Never -- nslookup kubernetes.default
kubectl run dns-test -n ${namespace} --rm -it --image=busybox:1.36 --restart=Never -- nslookup SERVICE_NAME.${namespace}.svc.cluster.local`,
      },
      {
        label: 'Check CoreDNS',
        command: `kubectl get pods -n kube-system -l k8s-app=kube-dns
kubectl logs -n kube-system -l k8s-app=kube-dns`,
      },
    ],
    notes: [
      'A Service with no endpoints usually means its selector does not match the pod labels or the pods are not Ready.',
      'DNS failures often come from CoreDNS, CNI/network policy, or pods using the wrong namespace or service name.',
      'Ingress problems can be caused by wrong host rules, TLS secret issues, service port mismatches, or ingress controller errors.',
    ],
  },
  {
    title: 'Storage, Config, And Secrets',
    icon: Terminal,
    badge: 'Advanced',
    description:
      'Use these checks when pods are Pending, configuration is missing, or applications fail after startup.',
    commands: [
      {
        label: 'Secrets and ConfigMaps',
        command: `kubectl get secrets -n ${namespace}
kubectl get configmaps -n ${namespace}
kubectl describe secret SECRET_NAME -n ${namespace}
kubectl describe configmap CONFIGMAP_NAME -n ${namespace}`,
      },
      {
        label: 'Persistent volumes and claims',
        command: `kubectl get pvc -n ${namespace}
kubectl describe pvc PVC_NAME -n ${namespace}
kubectl get pv
kubectl describe pv PV_NAME`,
      },
      {
        label: 'Probe and environment debugging',
        command: `kubectl describe pod POD_NAME -n ${namespace}
kubectl get deployment backend -n ${namespace} -o yaml`,
      },
    ],
    notes: [
      'FailedMount means Kubernetes could not mount a volume, Secret, ConfigMap, or PVC.',
      'Unbound PVCs keep pods Pending until storage can be provisioned or a matching PV exists.',
      'Readiness probe failures remove pods from service endpoints. Liveness probe failures restart containers.',
    ],
  },
  {
    title: 'How Clusters Commonly Fail',
    icon: AlertTriangle,
    badge: 'Failure Modes',
    description:
      'These are common cluster-level failure patterns and the first commands to run when they appear.',
    commands: [
      {
        label: 'Node NotReady',
        command: `kubectl get nodes -o wide
kubectl describe node NODE_NAME
kubectl get pods -A -o wide --field-selector spec.nodeName=NODE_NAME`,
      },
      {
        label: 'API or control plane symptoms',
        command: `kubectl cluster-info
kubectl get pods -n kube-system
kubectl logs -n kube-system POD_NAME`,
      },
      {
        label: 'CNI or networking symptoms',
        command: `kubectl get pods -n kube-system -o wide
kubectl get daemonsets -n kube-system
kubectl describe daemonset calico-node -n kube-system`,
      },
    ],
    notes: [
      'Node NotReady can be caused by kubelet failure, network loss, disk pressure, memory pressure, expired certificates, or container runtime failure.',
      'Widespread DNS failures often look like application errors first. Always check CoreDNS and CNI pods.',
      'Control plane instability can show up as slow kubectl commands, leader election errors, API timeouts, or failing scheduler/controller pods.',
      'DiskPressure can block scheduling and cause evictions. Check node describe output and free disk on the node.',
    ],
  },
];

const commonErrors = [
  ['ImagePullBackOff', 'Kubernetes cannot pull the image. Check image name, tag, registry auth, pull secret, and node access to ghcr.io.'],
  ['ErrImagePull', 'Initial image pull failed. Describe the pod for the exact registry error.'],
  ['CrashLoopBackOff', 'The container keeps exiting. Check current and previous logs, env vars, commands, and app startup dependencies.'],
  ['Pending', 'The pod cannot be scheduled. Check node resources, taints, tolerations, PVC binding, and events.'],
  ['CreateContainerConfigError', 'Kubernetes cannot build container config. Check referenced Secrets, ConfigMaps, env vars, and volumes.'],
  ['RunContainerError', 'The runtime could not start the container. Check entrypoint, permissions, filesystem mounts, and security context.'],
  ['OOMKilled', 'The container exceeded memory limits. Increase limits or reduce memory usage.'],
  ['FailedMount', 'A volume, Secret, ConfigMap, or PVC could not be mounted. Describe the pod and PVC.'],
  ['Readiness probe failed', 'The pod is running but not receiving traffic. Check health endpoint, port, startup time, and dependencies.'],
  ['Liveness probe failed', 'Kubernetes restarts the container because the health check fails. Check app health path and timeout settings.'],
];

function CodeBlock({ command }: { command: string }) {
  return (
    <pre className="mt-2 overflow-x-auto rounded-md bg-slate-950 p-4 text-xs leading-6 text-slate-100">
      <code>{command}</code>
    </pre>
  );
}

export function DocumentsPage() {
  return (
    <Layout>
      <PageHeader
        title="Documents"
        description="Kubernetes deployment, restart, status, failure, and troubleshooting reference for ProxaOps"
      />

      <div className="space-y-6">
        <Card>
          <CardHeader className="flex flex-row items-center gap-3">
            <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
              <BookOpen className="h-4 w-4" />
            </div>
            <div>
              <CardTitle>Quick Commands</CardTitle>
              <p className="mt-1 text-sm text-slate-500">Copy and paste these when you need a fast cluster check or restart.</p>
            </div>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              {quickCommands.map((item) => (
                <div key={item.title} className="rounded-md border border-slate-200 p-4">
                  <h3 className="text-sm font-semibold text-slate-900">{item.title}</h3>
                  <CodeBlock command={item.command} />
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Common Error Messages</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-slate-200 text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-slate-500">
                    <th className="py-2 pr-4 font-semibold">Error</th>
                    <th className="py-2 font-semibold">Meaning and first fix</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100">
                  {commonErrors.map(([error, fix]) => (
                    <tr key={error}>
                      <td className="py-3 pr-4 align-top font-mono text-xs text-red-700">{error}</td>
                      <td className="py-3 text-slate-700">{fix}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>

        {documentSections.map((section) => {
          const Icon = section.icon;
          return (
            <Card key={section.title}>
              <CardHeader>
                <div className="flex flex-wrap items-center gap-3">
                  <div className="h-9 w-9 rounded-md bg-slate-100 text-slate-700 flex items-center justify-center">
                    <Icon className="h-4 w-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <CardTitle>{section.title}</CardTitle>
                      <Badge variant="info">{section.badge}</Badge>
                    </div>
                    <p className="mt-1 text-sm text-slate-500">{section.description}</p>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  {section.commands.map((command) => (
                    <div key={command.label}>
                      <h3 className="text-sm font-semibold text-slate-900">{command.label}</h3>
                      <CodeBlock command={command.command} />
                    </div>
                  ))}
                  <div className="rounded-md bg-slate-50 border border-slate-200 p-4">
                    <h3 className="text-sm font-semibold text-slate-900">What to look for</h3>
                    <ul className="mt-2 space-y-2 text-sm text-slate-600">
                      {section.notes.map((note) => (
                        <li key={note} className="flex gap-2">
                          <span className="mt-1.5 h-1.5 w-1.5 flex-shrink-0 rounded-full bg-slate-400" />
                          <span>{note}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>
    </Layout>
  );
}
