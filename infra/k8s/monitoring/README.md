# Monitoring Stack

This directory contains the production baseline for central Kubernetes monitoring.

Default architecture:

- `kube-prometheus-stack` in the `monitoring` namespace
- Grafana exposed through Traefik at `grafana.proxacloud.com`
- Prometheus and Alertmanager kept internal by default
- ServiceMonitors for PSCS backend/gateway metrics
- Future clusters added through remote-write agents or federation

Install the base stack:

```powershell
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack --namespace monitoring --values infra/k8s/monitoring/values.yaml
kubectl apply -f infra/k8s/monitoring/grafana-ingress.yaml
```

Apply PSCS ServiceMonitors after the chart CRDs exist:

```powershell
kubectl apply -f infra/k8s/monitoring/pscs-servicemonitors.yaml
```

For future clusters, prefer an agent with `remote_write` to a central metrics backend. Direct central scraping across cluster/network boundaries should be reserved for simple lab environments.
