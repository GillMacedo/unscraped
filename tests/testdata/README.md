# Test data

## real/
Unchanged `helm template` output of public charts, rendered with Helm v4.3.2 on 2026-11-16, each with its project's license next to it.

| File | Chart | Command options | License |
|---|---|---|---|
| `kube-prometheus-stack/rendered.yaml` | prometheus-community kube-prometheus-stack 93.3.0 | `kps -n monitoring --set grafana.enabled=false` | Apache-2.0 |
| `kube-prometheus-stack/prometheus-select-all.yaml` | the same chart | the Prometheus object only, rendered with `--set grafana.enabled=false` and `prometheus.prometheusSpec.{serviceMonitor,podMonitor,rule,probe,scrapeConfig}SelectorNilUsesHelmValues=false` | Apache-2.0 |
| `ingress-nginx/rendered.yaml` | ingress-nginx 4.15.1 | `ingress-nginx -n ingress-nginx --set controller.metrics.enabled=true --set controller.metrics.serviceMonitor.enabled=true` | Apache-2.0 |
| `ingress-nginx/rendered-release-label.yaml` | the same chart | as above, plus `--set controller.metrics.serviceMonitor.additionalLabels.release=kps` | Apache-2.0 |
| `cert-manager/rendered.yaml` | jetstack cert-manager v1.21.3 | `cert-manager -n cert-manager --set prometheus.servicemonitor.enabled=true` | Apache-2.0 |
| `argo-cd/rendered.yaml` | argo-helm argo-cd 10.18.0 | `argocd -n argocd --api-versions monitoring.coreos.com/v1 --set crds.install=false`, with `metrics.enabled` and `metrics.serviceMonitor.enabled` for the controller, server, repo server, Redis, ApplicationSet, notifications and Dex | Apache-2.0 |
| `flux2/rendered.yaml` | fluxcd-community flux2 2.20.0 | `flux -n flux-system --set installCRDs=false --set prometheus.podMonitor.create=true` | Apache-2.0 |

Grafana was left out of kube-prometheus-stack so that no generated password is stored here. The flux2 chart doesn't set namespaces, so tests pass `-n flux-system`, as `helm install -n` would.

## bad/
Inputs unscraped must refuse with exit 2. `helm-missing-chart.txt` and `helm-schema-error.txt` are real output of a failed `helm template`, `kubectl-no-server.txt` is real output of `kubectl get` with no cluster, and `go-build-error.txt` and `go-test-fail.txt` are real output of `go build` and `go test` on a broken package. `kubectl-no-resources.txt`, `kubectl-empty-list.yaml` and `api-status-error.json` were written to match kubectl's and the API server's output. The rest were written for these tests. `no-monitors.yaml` and `monitor-only.yaml` are valid manifests refused alone only because there's nothing to check.
