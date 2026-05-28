import { useState } from 'react';
import { NavLink } from 'react-router-dom';
import { AlertTriangle, BookOpen, Check, CheckCircle2, Clipboard, ClipboardList, Database, RefreshCcw, Search, Shield, Terminal } from 'lucide-react';
import { Layout, PageHeader } from '../components/Layout';
import { Badge } from '../components/ui/Badge';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';

const namespace = 'proxaops';
const dbNamespace = 'pscs';
const dbService = 'pscs-prod-db-cluster';
const dbHost = `${dbService}.${dbNamespace}`;

type DocumentCategory = 'quick-commands' | 'database-cluster' | 'kubernetes-cluster';

const documentTabs = [
  {
    to: '/documents/quick-commands',
    category: 'quick-commands',
    label: 'Quick Commands',
    description: 'Fast copy/paste checks and restart commands.',
    icon: BookOpen,
  },
  {
    to: '/documents/database-cluster',
    category: 'database-cluster',
    label: 'Database Cluster',
    description: 'Database service, endpoints, DNS, port, and app config checks.',
    icon: Database,
  },
  {
    to: '/documents/kubernetes-cluster',
    category: 'kubernetes-cluster',
    label: 'Kubernetes Cluster',
    description: 'Cluster, deployment, pod, registry, networking, and error troubleshooting.',
    icon: Terminal,
  },
] satisfies Array<{
  to: string;
  category: DocumentCategory;
  label: string;
  description: string;
  icon: typeof BookOpen;
}>;

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
    title: 'Check example application namespace',
    command: `kubectl get all -n ${namespace}
kubectl get pods -n ${namespace} -o wide
kubectl get events -n ${namespace} --sort-by=.lastTimestamp`,
  },
  {
    title: 'Restart example application deployments',
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
      'Use these commands when a new image has been published, configuration changed, or pods need to be recreated. The examples use the proxaops namespace.',
    commands: [
      {
        label: 'Restart example application deployments',
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

const databaseSections = [
  {
    title: 'Database Service And Endpoints',
    icon: Database,
    badge: 'Database',
    description:
      'Use this first to confirm Kubernetes has a stable Service name and real database node endpoints behind it.',
    commands: [
      {
        label: 'Check database service and endpoints',
        command: `kubectl get svc ${dbService} -n ${dbNamespace}
kubectl get endpoints ${dbService} -n ${dbNamespace}`,
      },
      {
        label: 'Expected healthy shape',
        command: `NAME                   TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)    AGE
${dbService}   ClusterIP   10.106.147.144   <none>        3306/TCP   3d17h

NAME                   ENDPOINTS                                               AGE
${dbService}   192.168.8.50:3306,192.168.8.51:3306,192.168.8.52:3306   3d17h`,
      },
    ],
    notes: [
      'The Service name is the DNS name applications should use inside Kubernetes.',
      'Endpoints must show the database node IPs and port 3306. Empty endpoints means the Service exists but has no backend database targets.',
      `For this cluster, the application DB host should be ${dbHost}.`,
    ],
  },
  {
    title: 'Database DNS And Port Connectivity',
    icon: Search,
    badge: 'Connectivity',
    description:
      'Use these commands when the application logs show lookup failures, timeouts, or connection refused errors.',
    commands: [
      {
        label: 'DNS lookup from inside the namespace',
        command: `kubectl run db-dns-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nslookup ${dbHost}
kubectl run db-dns-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nslookup ${dbHost}.svc.cluster.local`,
      },
      {
        label: 'TCP port test from inside Kubernetes',
        command: `kubectl run db-port-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nc -vz ${dbHost} 3306`,
      },
      {
        label: 'Direct endpoint checks',
        command: `kubectl run db-endpoint-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nc -vz 192.168.8.50 3306
kubectl run db-endpoint-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nc -vz 192.168.8.51 3306
kubectl run db-endpoint-test -n ${dbNamespace} --rm -it --image=busybox:1.36 --restart=Never -- nc -vz 192.168.8.52 3306`,
      },
    ],
    notes: [
      'If DNS fails for the Service name, check CoreDNS and confirm the Service exists in the expected namespace.',
      'If DNS works but port 3306 fails, check firewall rules, database listener binding, node routing, and database process health.',
      'If only one endpoint fails, the issue is probably on that database node rather than Kubernetes service discovery.',
    ],
  },
  {
    title: 'Application Database Configuration',
    icon: ClipboardList,
    badge: 'Config',
    description:
      'Use this when backend or gateway pods crash because they still point to an old DB hostname such as db.',
    commands: [
      {
        label: 'Check app DB config sources',
        command: `kubectl get configmap pscs-config -n ${dbNamespace} -o yaml
kubectl get secret pscs-secrets -n ${dbNamespace} -o yaml`,
      },
      {
        label: 'Confirm backend and gateway environment',
        command: `kubectl describe deployment backend -n ${dbNamespace}
kubectl describe deployment gateway -n ${dbNamespace}`,
      },
      {
        label: 'Patch DB host if it still points to db',
        command: `kubectl patch configmap pscs-config -n ${dbNamespace} --type merge -p '{"data":{"DB_HOST":"${dbHost}","DB_PORT":"3306"}}'
kubectl rollout restart deployment/backend -n ${dbNamespace}
kubectl rollout restart deployment/gateway -n ${dbNamespace}`,
      },
    ],
    notes: [
      'A log like lookup db on 10.96.0.10:53: no such host means the app is using db, but no Service named db exists.',
      'Use DB_HOST=pscs-prod-db-cluster.pscs when the database Service is in namespace pscs.',
      'After changing ConfigMaps or Secrets, restart the deployments so new pods receive the updated environment.',
    ],
  },
  {
    title: 'Database Crash And Login Errors',
    icon: AlertTriangle,
    badge: 'Errors',
    description:
      'Use these checks when the database service resolves but the application still cannot connect or authenticate.',
    commands: [
      {
        label: 'Check backend crash logs',
        command: `kubectl get pods -n ${dbNamespace} -l app=backend
kubectl logs -n ${dbNamespace} -l app=backend --tail=100
kubectl logs POD_NAME -n ${dbNamespace} --previous`,
      },
      {
        label: 'Check rollout after DB config changes',
        command: `kubectl rollout status deployment/backend -n ${dbNamespace}
kubectl rollout status deployment/gateway -n ${dbNamespace}
kubectl get pods -n ${dbNamespace} -o wide`,
      },
      {
        label: 'Check database Service events',
        command: `kubectl describe svc ${dbService} -n ${dbNamespace}
kubectl describe endpoints ${dbService} -n ${dbNamespace}
kubectl get events -n ${dbNamespace} --sort-by=.lastTimestamp`,
      },
    ],
    notes: [
      'Access denied usually means DB_USER or DB_PASS is wrong, or the database user is not allowed from the Kubernetes node/pod network.',
      'Unknown database means DB_NAME does not exist on the database cluster.',
      'Too many connections means the database cluster is reachable but connection limits or pooling need attention.',
      'Connection refused means the IP is reachable but MariaDB/MySQL is not listening on 3306, is blocked, or is down on that node.',
      'Timeout usually points to firewall, routing, network policy, database overload, or a down database node.',
    ],
  },
];

const commonErrors = [
  {
    error: 'ImagePullBackOff',
    category: 'Image',
    meaning: 'Kubernetes tried to pull a container image, failed, and is backing off before retrying.',
    causes: 'Wrong image name or tag, private registry without imagePullSecret, expired token, rate limit, DNS/network issue, or registry outage.',
    steps: 'Run kubectl describe pod POD_NAME -n proxaops, read the Events section, verify the image exists, check imagePullSecrets, then restart the deployment after fixing credentials or tags.',
  },
  {
    error: 'ErrImagePull',
    category: 'Image',
    meaning: 'The first image pull attempt failed before Kubernetes moved into backoff retry behavior.',
    causes: 'Bad tag, bad registry path, unauthorized registry access, TLS error, or node cannot reach the registry.',
    steps: 'Describe the pod, copy the exact pull error, confirm the image path manually, and check whether the namespace has the correct GHCR secret.',
  },
  {
    error: 'InvalidImageName',
    category: 'Image',
    meaning: 'The image value in the manifest is not a valid container image reference.',
    causes: 'Missing repository, invalid characters, uppercase registry path, malformed tag, or empty image value from a template variable.',
    steps: 'Inspect the deployment YAML, fix the image value, then run kubectl rollout restart deployment/DEPLOYMENT_NAME -n proxaops.',
  },
  {
    error: 'ImageInspectError',
    category: 'Image',
    meaning: 'The node pulled or found the image but the runtime could not inspect its metadata.',
    causes: 'Corrupt image on the node, runtime cache issue, unsupported image format, or container runtime problem.',
    steps: 'Restart the pod on another node, check node runtime logs, and remove the bad cached image from the affected node if needed.',
  },
  {
    error: 'CrashLoopBackOff',
    category: 'Container',
    meaning: 'The container starts, exits, and Kubernetes repeatedly restarts it with increasing delay.',
    causes: 'Application startup crash, missing env vars, bad config, database unavailable, failed migration, bad command, or app exits successfully too quickly.',
    steps: 'Run kubectl logs POD_NAME -n proxaops and kubectl logs POD_NAME -n proxaops --previous. Check env vars, secrets, config maps, probes, and dependent services.',
  },
  {
    error: 'RunContainerError',
    category: 'Container',
    meaning: 'The container runtime could not start the container after Kubernetes created its config.',
    causes: 'Bad command or args, permission denied, missing executable, invalid working directory, bad mount path, or security context conflict.',
    steps: 'Describe the pod, inspect command/args/volumeMounts/securityContext, and compare with the image Dockerfile entrypoint.',
  },
  {
    error: 'CreateContainerError',
    category: 'Container',
    meaning: 'The runtime failed while creating the container.',
    causes: 'Runtime failure, invalid mount, bad container config, node disk issue, or container runtime instability.',
    steps: 'Describe the pod, check Events, move the pod to another node if possible, and inspect kubelet/container runtime logs on the affected node.',
  },
  {
    error: 'CreateContainerConfigError',
    category: 'Config',
    meaning: 'Kubernetes cannot build the container config before handing it to the runtime.',
    causes: 'Referenced Secret or ConfigMap missing, missing key in Secret/ConfigMap, invalid env var reference, or bad projected volume.',
    steps: 'Describe the pod and check Events. Then run kubectl get secrets -n proxaops and kubectl get configmaps -n proxaops to confirm referenced objects exist.',
  },
  {
    error: 'ContainerCannotRun',
    category: 'Container',
    meaning: 'The runtime started container setup but the configured process could not run.',
    causes: 'Executable not found, permission denied, wrong CPU architecture, bad shell path, or non-executable startup script.',
    steps: 'Check the image architecture, command, args, file permissions, and whether the image contains the executable path used in the manifest.',
  },
  {
    error: 'CannotStartContainerError',
    category: 'Container',
    meaning: 'Older runtime-style error showing the container could not be started.',
    causes: 'Bad entrypoint, mount failure, permission problem, runtime issue, or missing binary.',
    steps: 'Use pod describe and previous logs, then validate the image entrypoint locally or in a temporary debug pod.',
  },
  {
    error: 'StartError',
    category: 'Container',
    meaning: 'Container startup failed before the application became usable.',
    causes: 'Entrypoint failure, runtime permission issue, invalid config, or failed pre-start hook.',
    steps: 'Describe the pod, check container state reason, inspect lifecycle hooks, and read previous logs if the container briefly started.',
  },
  {
    error: 'OOMKilled',
    category: 'Resource',
    meaning: 'The container used more memory than its limit and was killed by the kernel.',
    causes: 'Memory limit too low, memory leak, large startup workload, inefficient query, or traffic spike.',
    steps: 'Run kubectl describe pod POD_NAME -n proxaops, check Last State, review memory limits, inspect kubectl top pods -n proxaops, and increase limits or fix memory usage.',
  },
  {
    error: 'Pending',
    category: 'Scheduling',
    meaning: 'The pod exists but has not been scheduled onto a node or cannot finish setup.',
    causes: 'Insufficient CPU/memory, node taints, node selector mismatch, unbound PVC, missing storage class, or image pull setup waiting.',
    steps: 'Run kubectl describe pod POD_NAME -n proxaops and read Events. Check kubectl get nodes, kubectl get pvc -n proxaops, and node taints.',
  },
  {
    error: 'Unschedulable',
    category: 'Scheduling',
    meaning: 'The scheduler cannot find any node that satisfies the pod requirements.',
    causes: 'Resource requests too high, nodeSelector/affinity mismatch, taints without tolerations, host port conflict, or PVC zone mismatch.',
    steps: 'Describe the pod, read scheduler event messages, lower resource requests if appropriate, update tolerations/affinity, or add capacity.',
  },
  {
    error: 'Insufficient cpu',
    category: 'Scheduling',
    meaning: 'No node has enough allocatable CPU for the pod request.',
    causes: 'CPU requests too high, cluster capacity too small, many pods already scheduled, or reserved system resources.',
    steps: 'Run kubectl describe nodes and kubectl top nodes. Reduce CPU requests or add/scale nodes.',
  },
  {
    error: 'Insufficient memory',
    category: 'Scheduling',
    meaning: 'No node has enough allocatable memory for the pod request.',
    causes: 'Memory requests too high, exhausted nodes, or not enough worker capacity.',
    steps: 'Check node allocatable memory and current pod requests. Reduce memory requests carefully or add capacity.',
  },
  {
    error: 'Insufficient ephemeral-storage',
    category: 'Scheduling',
    meaning: 'A node does not have enough local ephemeral storage for pod scheduling.',
    causes: 'Large logs, image cache growth, emptyDir usage, or high ephemeral-storage requests.',
    steps: 'Describe nodes, clean node disk, set log rotation, lower ephemeral requests, or add disk capacity.',
  },
  {
    error: 'Too many pods',
    category: 'Scheduling',
    meaning: 'A node has reached its maximum pod count.',
    causes: 'Node pod CIDR limit, kubelet maxPods setting, or too many small workloads on the node.',
    steps: 'Add nodes, reduce pod count, or adjust node maxPods only if the CNI and IP capacity support it.',
  },
  {
    error: 'TaintToleration',
    category: 'Scheduling',
    meaning: 'The pod does not tolerate a taint on the available node.',
    causes: 'Control-plane taint, dedicated node taint, maintenance taint, or NoSchedule taint.',
    steps: 'Run kubectl describe node NODE_NAME, inspect Taints, then add the correct toleration or schedule the pod to a different node group.',
  },
  {
    error: 'NodeAffinity',
    category: 'Scheduling',
    meaning: 'The pod node affinity rules do not match available nodes.',
    causes: 'Required node labels missing, wrong label key/value, or overly strict affinity rules.',
    steps: 'Check pod affinity in YAML and node labels with kubectl get nodes --show-labels. Fix labels or relax affinity.',
  },
  {
    error: 'MatchNodeSelector',
    category: 'Scheduling',
    meaning: 'The pod has a nodeSelector that does not match any available node.',
    causes: 'Wrong node label, node label removed, or manifest targeting the wrong environment.',
    steps: 'Run kubectl get nodes --show-labels and update nodeSelector or node labels.',
  },
  {
    error: 'PodFitsHostPorts',
    category: 'Scheduling',
    meaning: 'The pod requests a hostPort already in use on candidate nodes.',
    causes: 'Multiple pods using the same hostPort or daemon-like workload not configured as a DaemonSet.',
    steps: 'Avoid hostPort when possible, use a Service, change the port, or schedule only one pod per node.',
  },
  {
    error: 'Preemption is not helpful for scheduling',
    category: 'Scheduling',
    meaning: 'Evicting lower priority pods would still not make the new pod schedulable.',
    causes: 'Hard constraints like node affinity, taints, PVC zone, hostPort, or resource request too large for any single node.',
    steps: 'Focus on the exact scheduler event. Add matching nodes, change constraints, or reduce pod requests.',
  },
  {
    error: 'NodeNotReady',
    category: 'Node',
    meaning: 'The node is not healthy enough to run workloads.',
    causes: 'Kubelet stopped, node network down, container runtime failure, disk pressure, memory pressure, or certificate issue.',
    steps: 'Run kubectl describe node NODE_NAME, inspect Conditions and Events, then check kubelet and container runtime on the node.',
  },
  {
    error: 'NodeUnavailable',
    category: 'Node',
    meaning: 'Kubernetes cannot use or reach the node for normal workload placement.',
    causes: 'Node powered off, network partition, kubelet not reporting, or node under maintenance.',
    steps: 'Check node status, confirm the VM/server is running, verify network connectivity, and drain/replace the node if needed.',
  },
  {
    error: 'NodeLost',
    category: 'Node',
    meaning: 'The control plane lost contact with the node for long enough to mark pods as lost.',
    causes: 'Node crash, network failure, kubelet failure, or host shutdown.',
    steps: 'Check infrastructure health, restore the node, or delete stuck pods after confirming workloads are safely rescheduled.',
  },
  {
    error: 'Evicted',
    category: 'Node',
    meaning: 'Kubelet terminated the pod to reclaim node resources.',
    causes: 'MemoryPressure, DiskPressure, PIDPressure, ephemeral storage exhaustion, or node resource pressure.',
    steps: 'Describe the pod and node, check eviction reason, clean disk or reduce resource pressure, then let the controller recreate the pod.',
  },
  {
    error: 'Unknown',
    category: 'Node',
    meaning: 'Kubernetes cannot determine the pod state, usually because the node stopped reporting.',
    causes: 'Node communication failure, kubelet down, API connectivity issue, or node crash.',
    steps: 'Check the node running the pod, inspect node status, and delete/recreate the pod only after confirming node state.',
  },
  {
    error: 'ContainerCreating',
    category: 'Lifecycle',
    meaning: 'Kubernetes is creating the container and preparing volumes, network, and image.',
    causes: 'Normal startup, slow image pull, slow volume mount, CNI delay, or stuck runtime operation.',
    steps: 'If it stays too long, describe the pod and inspect Events for pull, mount, or sandbox errors.',
  },
  {
    error: 'PodInitializing',
    category: 'Lifecycle',
    meaning: 'The pod is waiting for init containers to finish before app containers start.',
    causes: 'Long-running init container, failed init container, image pull issue, or dependency wait script.',
    steps: 'Run kubectl get pod POD_NAME -n proxaops -o jsonpath="{.status.initContainerStatuses}" and check init container logs.',
  },
  {
    error: 'Completed',
    category: 'Lifecycle',
    meaning: 'The container finished successfully.',
    causes: 'Normal for Jobs, but wrong for Deployments if the app process exits instead of staying alive.',
    steps: 'For Jobs, no fix is needed. For Deployments, ensure the container command starts a long-running server process.',
  },
  {
    error: 'Error',
    category: 'Lifecycle',
    meaning: 'A container terminated with a non-zero exit code.',
    causes: 'Application crash, bad command, failed dependency, permission error, or unhandled exception.',
    steps: 'Check kubectl logs POD_NAME -n proxaops --previous and inspect the termination exit code in kubectl describe pod.',
  },
  {
    error: 'Terminating',
    category: 'Lifecycle',
    meaning: 'The pod is being deleted and Kubernetes is waiting for containers, finalizers, or volumes to shut down.',
    causes: 'Normal deletion, stuck finalizer, node unreachable, volume detach delay, or app not handling SIGTERM.',
    steps: 'Wait for graceful shutdown, inspect finalizers with kubectl get pod POD_NAME -n proxaops -o yaml, and avoid force delete unless necessary.',
  },
  {
    error: 'BackOff',
    category: 'Lifecycle',
    meaning: 'Kubernetes is delaying retries after repeated failures.',
    causes: 'Repeated image pull failure, container crash, or failed pod sandbox creation.',
    steps: 'Look at the more specific event beside BackOff, then troubleshoot image pull, logs, or sandbox/network setup.',
  },
  {
    error: 'DeadlineExceeded',
    category: 'Lifecycle',
    meaning: 'An operation or Job exceeded its configured deadline.',
    causes: 'activeDeadlineSeconds reached, slow startup, hanging job, API timeout, or long-running hook.',
    steps: 'Check Job or pod spec deadlines, logs, and events. Increase deadline only if the workload legitimately needs more time.',
  },
  {
    error: 'FailedMount',
    category: 'Storage',
    meaning: 'Kubernetes could not mount a volume into the pod.',
    causes: 'Missing Secret/ConfigMap, unbound PVC, storage backend unavailable, permission issue, or node mount problem.',
    steps: 'Describe the pod, check PVC/PV status, verify Secrets and ConfigMaps, and inspect CSI/storage controller logs.',
  },
  {
    error: 'FailedAttachVolume',
    category: 'Storage',
    meaning: 'Kubernetes or the CSI driver could not attach a persistent volume to the node.',
    causes: 'Storage backend failure, volume already attached elsewhere, zone mismatch, CSI driver issue, or cloud/provider API error.',
    steps: 'Describe the pod and PVC, check VolumeAttachment objects, and inspect CSI controller logs.',
  },
  {
    error: 'FailedDetachVolume',
    category: 'Storage',
    meaning: 'Kubernetes could not detach a volume from a node.',
    causes: 'Node unreachable, storage backend delay, CSI issue, or stuck attachment metadata.',
    steps: 'Check node status, VolumeAttachment resources, and CSI logs before manually intervening.',
  },
  {
    error: 'Multi-Attach error',
    category: 'Storage',
    meaning: 'A volume that supports only one writer is already attached to another node.',
    causes: 'ReadWriteOnce PVC used by pods on different nodes, stale attachment after node failure, or rolling update with storage constraints.',
    steps: 'Scale down duplicate pods, wait for detach, use ReadWriteMany storage if needed, or pin the workload to one node.',
  },
  {
    error: 'ProvisioningFailed',
    category: 'Storage',
    meaning: 'Dynamic volume provisioning failed.',
    causes: 'Bad StorageClass, missing provisioner, backend quota, credentials issue, or CSI driver failure.',
    steps: 'Describe the PVC and StorageClass, check external provisioner logs, and verify storage backend capacity and credentials.',
  },
  {
    error: 'ExternalProvisioning',
    category: 'Storage',
    meaning: 'Kubernetes is waiting for an external provisioner to create the volume.',
    causes: 'Normal delay, CSI provisioner not running, wrong StorageClass provisioner name, or backend issue.',
    steps: 'Check storage controller pods and PVC events. If it stays pending, inspect the CSI provisioner logs.',
  },
  {
    error: 'WaitForFirstConsumer',
    category: 'Storage',
    meaning: 'PVC binding waits until a pod is scheduled so Kubernetes can choose storage in the correct topology.',
    causes: 'Normal for StorageClasses with volumeBindingMode WaitForFirstConsumer, but can be stuck if the pod is unschedulable.',
    steps: 'Describe the pod and PVC together. Fix scheduling constraints first if no node can host the pod.',
  },
  {
    error: 'PVC Pending',
    category: 'Storage',
    meaning: 'A PersistentVolumeClaim has not bound to a PersistentVolume.',
    causes: 'No matching PV, StorageClass issue, dynamic provisioner failure, capacity mismatch, or access mode mismatch.',
    steps: 'Run kubectl describe pvc PVC_NAME -n proxaops and kubectl get storageclass. Check provisioner logs if dynamic provisioning is expected.',
  },
  {
    error: 'MountVolume.SetUp failed',
    category: 'Storage',
    meaning: 'Volume setup failed during pod startup.',
    causes: 'Missing Secret/ConfigMap key, invalid projected volume, permission issue, unavailable storage, or mount command failure.',
    steps: 'Describe the pod and inspect the named volume in the event. Verify the referenced object, key, and mount path.',
  },
  {
    error: 'Unable to attach or mount volumes',
    category: 'Storage',
    meaning: 'Kubernetes could not complete volume attach or mount before timeout.',
    causes: 'CSI problem, storage backend issue, node issue, unbound PVC, or multi-attach conflict.',
    steps: 'Check pod events, PVC/PV status, VolumeAttachment objects, and storage controller logs.',
  },
  {
    error: 'Volume is already exclusively attached',
    category: 'Storage',
    meaning: 'The storage backend reports the volume is already attached to another node.',
    causes: 'ReadWriteOnce volume, old pod still terminating, stale attachment, or node failure.',
    steps: 'Find pods using the PVC, scale down duplicates, wait for detach, and verify the old node is healthy.',
  },
  {
    error: 'Readiness probe failed',
    category: 'Probe',
    meaning: 'The pod is running but is not marked Ready, so Services should not send traffic to it.',
    causes: 'Wrong health path, wrong port, app not ready, dependency unavailable, slow startup, or timeout too low.',
    steps: 'Describe the pod, test the health endpoint from inside the pod, and tune initialDelaySeconds, periodSeconds, timeoutSeconds, or failureThreshold.',
  },
  {
    error: 'Liveness probe failed',
    category: 'Probe',
    meaning: 'Kubernetes considers the container unhealthy and restarts it.',
    causes: 'Wrong health endpoint, app deadlock, dependency check in liveness probe, CPU starvation, or timeout too aggressive.',
    steps: 'Check restart count and logs before restart. Keep liveness checks simple and use readiness for dependency availability.',
  },
  {
    error: 'Startup probe failed',
    category: 'Probe',
    meaning: 'The application did not finish starting before the startup probe failed.',
    causes: 'Slow boot, migrations, cold cache, insufficient CPU, wrong probe path, or timeout too short.',
    steps: 'Increase startup probe thresholds or delay, fix the health endpoint, and check app startup logs.',
  },
  {
    error: 'HTTP probe failed with statuscode',
    category: 'Probe',
    meaning: 'An HTTP health probe returned a non-success status code.',
    causes: 'Wrong path, auth required on health endpoint, app returns 500, dependency failure, or wrong port.',
    steps: 'Exec into the pod and curl the probe URL. Make health endpoints unauthenticated and lightweight.',
  },
  {
    error: 'connection refused',
    category: 'Network',
    meaning: 'A network connection reached the target IP, but nothing accepted the connection on that port.',
    causes: 'App not listening, wrong service targetPort, pod not ready, wrong container port, or startup delay.',
    steps: 'Check service targetPort, pod ports, app listen address, endpoints, and logs.',
  },
  {
    error: 'context deadline exceeded',
    category: 'Network',
    meaning: 'A request timed out before completing.',
    causes: 'Network policy, DNS delay, service unavailable, overloaded app, API server slowness, or storage/network timeout.',
    steps: 'Check events, DNS, service endpoints, network policies, and component logs for the caller and target.',
  },
  {
    error: 'Client.Timeout exceeded while awaiting headers',
    category: 'Network',
    meaning: 'An HTTP client connected but did not receive response headers in time.',
    causes: 'Slow backend, blocked request, overloaded pod, bad upstream route, or dependency timeout.',
    steps: 'Check backend logs, service endpoints, ingress/controller logs, and resource usage on target pods.',
  },
  {
    error: 'no endpoints available for service',
    category: 'Network',
    meaning: 'A Service has no Ready pods behind it.',
    causes: 'Selector mismatch, pods not Ready, deployment scaled to zero, failed readiness probe, or wrong namespace.',
    steps: 'Run kubectl get endpoints SERVICE_NAME -n proxaops and compare service selectors with pod labels.',
  },
  {
    error: 'service has no active endpoints',
    category: 'Network',
    meaning: 'Ingress or another controller found a Service but no usable backend endpoints.',
    causes: 'No Ready pods, selector mismatch, readiness failure, or wrong service name/port.',
    steps: 'Check endpoints, pod readiness, service selector, and ingress backend service port.',
  },
  {
    error: 'i/o timeout',
    category: 'Network',
    meaning: 'A network operation timed out at the TCP or HTTP layer.',
    causes: 'DNS issue, firewall, network policy, unreachable service, registry/network outage, or CNI problem.',
    steps: 'Test DNS and connectivity from a debug pod, check NetworkPolicies, and inspect CNI pods.',
  },
  {
    error: 'no such host',
    category: 'DNS',
    meaning: 'DNS could not resolve the hostname.',
    causes: 'Wrong service name, wrong namespace, CoreDNS issue, upstream DNS issue, or typo.',
    steps: 'Run nslookup from a debug pod, check CoreDNS pods/logs, and verify the service DNS name.',
  },
  {
    error: 'Temporary failure in name resolution',
    category: 'DNS',
    meaning: 'DNS resolution failed temporarily.',
    causes: 'CoreDNS unavailable, upstream resolver issue, network policy blocking DNS, or overloaded DNS service.',
    steps: 'Check CoreDNS status and logs, test kubernetes.default DNS, and verify UDP/TCP 53 is not blocked.',
  },
  {
    error: 'NetworkPolicy denied traffic',
    category: 'Network',
    meaning: 'NetworkPolicy rules are blocking traffic between pods or namespaces.',
    causes: 'Default deny policy, missing ingress/egress rule, wrong podSelector, or namespaceSelector mismatch.',
    steps: 'List NetworkPolicies, identify source and destination labels, and add the minimum required ingress/egress rule.',
  },
  {
    error: 'CNI plugin not initialized',
    category: 'Network',
    meaning: 'The node networking plugin is not ready.',
    causes: 'CNI DaemonSet not running, bad CNI config, kubelet started before CNI, or node network problem.',
    steps: 'Check CNI pods in kube-system, describe the affected node, and inspect kubelet logs.',
  },
  {
    error: 'NetworkPluginNotReady',
    category: 'Network',
    meaning: 'Kubelet reports that pod networking is not ready on the node.',
    causes: 'CNI plugin missing, CNI pod crash, bad configuration, or node cannot reach control plane.',
    steps: 'Check kube-system CNI DaemonSet, node conditions, and CNI logs.',
  },
  {
    error: 'CreatePodSandboxError',
    category: 'Network',
    meaning: 'Kubernetes could not create the pod sandbox, which prepares networking and isolation.',
    causes: 'CNI failure, container runtime issue, pause image pull failure, or node-level networking problem.',
    steps: 'Describe the pod, inspect CNI and kubelet logs on the node, and verify the pause image can be pulled.',
  },
  {
    error: 'FailedCreatePodSandBox',
    category: 'Network',
    meaning: 'Event form of pod sandbox creation failure.',
    causes: 'CNI setup failure, IP allocation failure, runtime error, or node network problem.',
    steps: 'Read the full event message, check CNI DaemonSet health, and inspect node kubelet logs.',
  },
  {
    error: 'failed to setup network for sandbox',
    category: 'Network',
    meaning: 'The CNI plugin failed while assigning networking to the pod sandbox.',
    causes: 'IP pool exhausted, CNI config error, network plugin crash, or node routing issue.',
    steps: 'Check CNI logs, IP pool capacity, node routes, and whether other pods can start on the same node.',
  },
  {
    error: 'RegistryUnavailable',
    category: 'Registry',
    meaning: 'The image registry cannot be reached or is returning availability errors.',
    causes: 'Registry outage, DNS failure, firewall, proxy issue, or node egress problem.',
    steps: 'Test registry connectivity from the node, check DNS, and retry after confirming registry availability.',
  },
  {
    error: 'Unauthorized',
    category: 'Registry',
    meaning: 'The registry rejected the image pull because credentials are missing or invalid.',
    causes: 'Missing imagePullSecret, expired token, wrong GitHub token scope, or secret in the wrong namespace.',
    steps: 'Recreate the registry secret in proxaops, attach it to the service account, and restart the deployment.',
  },
  {
    error: 'Denied',
    category: 'Registry',
    meaning: 'The registry denied access to the image or repository.',
    causes: 'No permission to package, private package, wrong owner/repo, or policy preventing pull.',
    steps: 'Verify package permissions in GHCR, token scopes, image path, and imagePullSecret.',
  },
  {
    error: 'manifest unknown',
    category: 'Registry',
    meaning: 'The registry exists but does not have the requested image tag or digest.',
    causes: 'Tag not pushed, typo in tag, wrong repository, architecture manifest missing, or CI pushed to a different name.',
    steps: 'Check the exact image tag in GHCR and update the deployment image to a real tag or digest.',
  },
  {
    error: 'repository does not exist',
    category: 'Registry',
    meaning: 'The registry path points to a repository that cannot be found or accessed.',
    causes: 'Wrong image owner/name, private repo without auth, or package not published.',
    steps: 'Confirm ghcr.io/OWNER/IMAGE exists and that the token can read it.',
  },
  {
    error: 'no basic auth credentials',
    category: 'Registry',
    meaning: 'The node tried to pull from a private registry without usable credentials.',
    causes: 'Missing imagePullSecret, secret not attached to pod/service account, or secret in another namespace.',
    steps: 'Create the docker-registry secret in proxaops and add it to imagePullSecrets or the default service account.',
  },
  {
    error: 'x509: certificate signed by unknown authority',
    category: 'TLS',
    meaning: 'TLS verification failed because the registry or endpoint certificate is not trusted.',
    causes: 'Private registry with self-signed certificate, missing CA bundle on nodes, or TLS interception proxy.',
    steps: 'Install the trusted CA on cluster nodes or use a registry certificate signed by a trusted CA.',
  },
  {
    error: 'TLS handshake timeout',
    category: 'TLS',
    meaning: 'TLS negotiation to a remote endpoint timed out.',
    causes: 'Network latency, firewall, proxy, registry overload, or node egress issue.',
    steps: 'Check node network path to the endpoint, DNS, proxy settings, and registry availability.',
  },
  {
    error: 'configmap not found',
    category: 'Config',
    meaning: 'A pod references a ConfigMap that does not exist in the same namespace.',
    causes: 'ConfigMap not applied, wrong namespace, typo in name, or deployment applied before config.',
    steps: 'Create the ConfigMap in proxaops or fix the reference name, then restart affected pods.',
  },
  {
    error: 'secret not found',
    category: 'Config',
    meaning: 'A pod references a Secret that does not exist in the same namespace.',
    causes: 'Secret not created, wrong namespace, typo in name, or sealed/external secret sync failed.',
    steps: 'Create or sync the Secret in proxaops, verify key names, and restart the deployment.',
  },
  {
    error: "couldn't find key",
    category: 'Config',
    meaning: 'The Secret or ConfigMap exists, but the referenced key is missing.',
    causes: 'Wrong key name, changed config schema, incomplete secret creation, or case mismatch.',
    steps: 'Describe the Secret/ConfigMap and update the manifest or object so the key names match.',
  },
  {
    error: 'invalid environment variable',
    category: 'Config',
    meaning: 'An environment variable definition is invalid.',
    causes: 'Invalid env var name, unsupported fieldRef, bad valueFrom reference, or malformed manifest.',
    steps: 'Validate env entries in the deployment YAML and fix names to use valid shell-style variable names.',
  },
  {
    error: 'fieldRef not found',
    category: 'Config',
    meaning: 'A downward API fieldRef references a field Kubernetes cannot provide.',
    causes: 'Typo in fieldPath, unsupported field, or using a field only available in another API version.',
    steps: 'Check the env/valueFrom fieldRef path and use supported downward API fields such as metadata.name or status.podIP.',
  },
  {
    error: 'volumeMount not found',
    category: 'Config',
    meaning: 'A mount references a volume name that is not defined in the pod spec.',
    causes: 'Typo between volumeMounts.name and volumes.name or templating error.',
    steps: 'Compare volumeMounts with volumes in the deployment YAML and make names match exactly.',
  },
  {
    error: 'ProgressDeadlineExceeded',
    category: 'Deployment',
    meaning: 'A Deployment rollout did not make progress before its deadline.',
    causes: 'Pods failing readiness, image pull errors, crash loops, insufficient capacity, or bad rollout config.',
    steps: 'Run kubectl rollout status deployment/DEPLOYMENT_NAME -n proxaops and kubectl describe deployment DEPLOYMENT_NAME -n proxaops, then inspect failing pods.',
  },
  {
    error: 'ReplicaFailure',
    category: 'Deployment',
    meaning: 'The ReplicaSet or Deployment failed to create or maintain replicas.',
    causes: 'Quota exceeded, admission policy denied pod, invalid pod spec, scheduling failure, or missing config.',
    steps: 'Describe the deployment and replicaset, then read Events for FailedCreate or policy messages.',
  },
  {
    error: 'MinimumReplicasUnavailable',
    category: 'Deployment',
    meaning: 'The deployment does not have enough available replicas.',
    causes: 'Pods not Ready, failed probes, crash loops, image pull failures, or unavailable nodes.',
    steps: 'Check pod status, readiness probes, and deployment events. Fix the pod-level reason first.',
  },
  {
    error: 'FailedCreate',
    category: 'Controller',
    meaning: 'A controller could not create a child resource such as a pod.',
    causes: 'Quota, admission webhook denial, invalid spec, service account issue, or RBAC denial.',
    steps: 'Describe the parent Deployment/ReplicaSet/Job and read the event message for the exact denial reason.',
  },
  {
    error: 'FailedUpdate',
    category: 'Controller',
    meaning: 'A controller or API request failed while updating a resource.',
    causes: 'Conflict, invalid patch, admission policy denial, resource version conflict, or RBAC issue.',
    steps: 'Check controller events and audit/error logs, then reapply with a valid manifest or fix permissions.',
  },
  {
    error: 'FailedDelete',
    category: 'Controller',
    meaning: 'Kubernetes could not delete a resource.',
    causes: 'Finalizer blocking deletion, API conflict, RBAC denial, or dependent resource issue.',
    steps: 'Inspect finalizers and ownerReferences. Remove finalizers only when you understand the cleanup impact.',
  },
  {
    error: 'Deployment does not have minimum availability',
    category: 'Deployment',
    meaning: 'A Deployment has fewer available pods than required.',
    causes: 'Readiness failures, unavailable nodes, crash loops, image pull failure, or maxUnavailable settings during rollout.',
    steps: 'Check rollout status, pod readiness, deployment strategy, and pod events.',
  },
  {
    error: 'FailedKillPod',
    category: 'Kubelet',
    meaning: 'Kubelet could not stop one or more containers in a pod.',
    causes: 'Container runtime issue, stuck process, node pressure, or unresponsive runtime.',
    steps: 'Check node runtime health and kubelet logs. Restart container runtime only during an approved maintenance window.',
  },
  {
    error: 'FailedSync',
    category: 'Kubelet',
    meaning: 'Kubelet failed to sync desired pod state on the node.',
    causes: 'Runtime error, volume setup failure, CNI issue, image pull issue, or node resource problem.',
    steps: 'Read the full event message and inspect kubelet logs on the node hosting the pod.',
  },
  {
    error: 'etcdserver: request timed out',
    category: 'Control Plane',
    meaning: 'etcd did not complete a request in time.',
    causes: 'etcd overload, disk latency, network latency between control-plane nodes, or unhealthy etcd member.',
    steps: 'Check etcd pod logs, control-plane node disk latency, member health, and API server latency.',
  },
  {
    error: 'leader election lost',
    category: 'Control Plane',
    meaning: 'A controller lost leadership and another instance should take over.',
    causes: 'API server latency, network issue, overloaded controller, or normal failover during restart.',
    steps: 'If frequent, check API server health, controller logs, and control-plane resource pressure.',
  },
  {
    error: 'certificate has expired or is not yet valid',
    category: 'Certificates',
    meaning: 'A Kubernetes component or client certificate is outside its valid time window.',
    causes: 'Expired cert, node clock skew, unrenewed kubeadm certificates, or wrong system time.',
    steps: 'Check system time, certificate expiration, and renew certificates according to your cluster bootstrap method.',
  },
  {
    error: 'x509: certificate has expired',
    category: 'Certificates',
    meaning: 'TLS failed because the certificate is expired.',
    causes: 'Expired API server, kubelet, webhook, ingress, or registry certificate.',
    steps: 'Identify the endpoint from the error, renew the certificate, and restart the affected component if required.',
  },
  {
    error: 'too old resource version',
    category: 'API',
    meaning: 'A watch request used an old resourceVersion that the API server no longer has in cache.',
    causes: 'Long-running watch, API server cache compaction, controller lag, or high API churn.',
    steps: 'Usually controllers reconnect automatically. If frequent, check API server load and controller health.',
  },
  {
    error: 'unable to update cni config',
    category: 'Network',
    meaning: 'The node or CNI plugin could not write or refresh CNI configuration.',
    causes: 'Permission issue, missing CNI config directory, bad CNI DaemonSet, or read-only filesystem.',
    steps: 'Check CNI pod logs, kubelet logs, and CNI config files on the node.',
  },
  {
    error: 'kubelet stopped posting node status',
    category: 'Node',
    meaning: 'The control plane has not received node status updates from kubelet.',
    causes: 'Kubelet stopped, node network lost, API server unreachable from node, or node overloaded.',
    steps: 'Check node reachability, kubelet service, container runtime, and node resource pressure.',
  },
];

function CodeBlock({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  const copyCommand = async () => {
    await navigator.clipboard.writeText(command);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  };

  return (
    <div className="relative mt-2">
      <button
        type="button"
        onClick={copyCommand}
        className="absolute right-2 top-2 inline-flex h-8 w-8 items-center justify-center rounded-md border border-slate-700 bg-slate-900 text-slate-300 transition-colors hover:bg-slate-800 hover:text-white"
        aria-label={copied ? 'Copied command' : 'Copy command'}
        title={copied ? 'Copied' : 'Copy'}
      >
        {copied ? <Check className="h-4 w-4" /> : <Clipboard className="h-4 w-4" />}
      </button>
      <pre className="overflow-x-auto rounded-md bg-slate-950 p-4 pr-14 text-xs leading-6 text-slate-100">
        <code>{command}</code>
      </pre>
    </div>
  );
}

export function DocumentsPage({ category = 'quick-commands' }: { category?: DocumentCategory }) {
  return (
    <Layout>
      <PageHeader
        title="Documents"
        description="Operational reference for Kubernetes cluster and database cluster troubleshooting."
      />

      <div className="space-y-6">
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
          {documentTabs.map((tab) => {
            const Icon = tab.icon;
            return (
              <NavLink
                key={tab.to}
                to={tab.to}
                className={({ isActive }) =>
                  `rounded-md border p-4 transition-colors ${
                    isActive || category === tab.category
                      ? 'border-indigo-200 bg-indigo-50 text-indigo-900'
                      : 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50'
                  }`
                }
              >
                <div className="flex items-start gap-3">
                  <div className="mt-0.5 h-8 w-8 rounded-md bg-white text-indigo-600 flex items-center justify-center border border-slate-200">
                    <Icon className="h-4 w-4" />
                  </div>
                  <div>
                    <p className="text-sm font-semibold">{tab.label}</p>
                    <p className="mt-1 text-xs text-slate-500">{tab.description}</p>
                  </div>
                </div>
              </NavLink>
            );
          })}
        </div>

        {category === 'quick-commands' && (
        <Card>
          <CardHeader className="flex flex-row items-center gap-3">
            <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
              <BookOpen className="h-4 w-4" />
            </div>
            <div>
              <CardTitle>Quick Commands</CardTitle>
              <p className="mt-1 text-sm text-slate-500">
                Copy these commands when you need a fast cluster check or restart. Replace proxaops with your namespace when needed.
              </p>
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
        )}

        {category === 'database-cluster' && (
        <>
        <Card>
          <CardHeader className="flex flex-row items-center gap-3">
            <div className="h-9 w-9 rounded-md bg-emerald-50 text-emerald-700 flex items-center justify-center">
              <Database className="h-4 w-4" />
            </div>
            <div>
              <CardTitle>Database Cluster</CardTitle>
              <p className="mt-1 text-sm text-slate-500">
                Service discovery, endpoint, connectivity, and application DB configuration checks. Examples use pscs and pscs-prod-db-cluster.
              </p>
            </div>
          </CardHeader>
        </Card>

        {databaseSections.map((section) => {
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
                      <Badge variant="success">{section.badge}</Badge>
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
        </>
        )}

        {category === 'kubernetes-cluster' && (
        <>
        <Card>
          <CardHeader className="flex flex-row items-center gap-3">
            <div className="h-9 w-9 rounded-md bg-indigo-50 text-indigo-600 flex items-center justify-center">
              <Terminal className="h-4 w-4" />
            </div>
            <div>
              <CardTitle>Kubernetes Cluster</CardTitle>
              <p className="mt-1 text-sm text-slate-500">
                Cluster health, deployment, registry, pod, networking, storage, and Kubernetes error troubleshooting. Examples use proxaops as a generic namespace.
              </p>
            </div>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Common Error Messages</CardTitle>
            <p className="mt-1 text-sm text-slate-500">
              A practical catalog of Kubernetes statuses and event messages. The troubleshooting steps use proxaops as the example namespace.
            </p>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
              {commonErrors.map((item) => (
                <div key={item.error} className="rounded-md border border-slate-200 p-4">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-mono text-sm font-semibold text-red-700">{item.error}</h3>
                    <Badge variant="default">{item.category}</Badge>
                  </div>
                  <div className="mt-3 space-y-3 text-sm">
                    <div>
                      <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Meaning</p>
                      <p className="mt-1 text-slate-700">{item.meaning}</p>
                    </div>
                    <div>
                      <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Common causes</p>
                      <p className="mt-1 text-slate-700">{item.causes}</p>
                    </div>
                    <div>
                      <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">Troubleshooting</p>
                      <p className="mt-1 text-slate-700">{item.steps}</p>
                    </div>
                  </div>
                </div>
              ))}
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
        </>
        )}
      </div>
    </Layout>
  );
}
