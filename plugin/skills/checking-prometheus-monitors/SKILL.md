---
name: checking-prometheus-monitors
description: Checks Prometheus Operator ServiceMonitors, PodMonitors and PrometheusRules in Kubernetes manifests or helm template output against the Prometheus resource, Services, workloads and Namespaces in the same files, and reports the ones Prometheus silently ignores or that find no target. Finds a missing release label for kube-prometheus-stack, a namespace the Prometheus doesn't watch, a ServiceMonitor port that is the container port's name instead of the Service port's name, a selector copied from the Service's pod selector, a PodMonitor port the container doesn't declare, a Service targetPort no container has, and scrapeTimeout longer than interval. Use when a target is missing from the Prometheus targets page, a ServiceMonitor or alert isn't picked up, metrics or alerts are missing after a deploy, or before merging monitoring manifests.
---

# Checking Prometheus Operator monitors

unscraped reads files only. It needs Go 1.22 or newer to build, and never connects to a cluster or to Prometheus. Build it once:

    go build -o "${TMPDIR:-/tmp}/unscraped" $(ls "${CLAUDE_PLUGIN_ROOT}"/scripts/*.go | grep -v _test.go)

## Get the files

Ask before running anything that touches a cluster. unscraped needs the monitors and what they point at, together in one run:

- The monitors: the app's rendered chart (`helm template RELEASE CHART -n NS -f values.yaml > app.yaml`, check that helm exited 0), `kubectl kustomize DIR > app.yaml`, or plain manifests in the repository.
- The Prometheus resource: render the monitoring stack too (`helm template kps prometheus-community/kube-prometheus-stack -n monitoring -f values.yaml > stack.yaml`), or save the live one: `kubectl get prometheus -A -o yaml > prometheus.yaml`. Without it, whether Prometheus picks up the monitors isn't checked.
- Namespaces, if the Prometheus selects namespaces by label: `kubectl get namespaces -o yaml > namespaces.yaml`.

## Run

    "${TMPDIR:-/tmp}/unscraped" -n NS stack.yaml app.yaml

`-n` sets the namespace for objects that don't set one, as `helm template -n` and `kubectl apply -n` do. Exit 0 means no findings, 1 means findings, and 2 means an input couldn't be trusted or nothing could be checked. On exit 2, show the user the reason and fix the input. Never report exit 2 as clean. Add `--format json` for machine-readable output, including every link that wasn't checked.

## Read the findings

- `US101`: no Prometheus selects the object's labels, usually a missing `release` label. `US102`: the labels match, but the Prometheus doesn't watch that namespace.
- `US201` to `US204`: the monitor's selector finds nothing: the Service is in another namespace, it copied the Service's pod selector, a PodMonitor selects Service labels, or the pods are in another namespace.
- `US301` to `US304`: ports: a ServiceMonitor `port` that isn't a Service port name, a Service `targetPort` name no container declares, a PodMonitor port the containers don't declare, a `targetPort` the pods don't declare.
- `US401`: `scrapeTimeout` longer than `interval`, which the operator rejects. `US402`: an empty endpoint list.

Each finding names the object, the file and line, what happens, and the smallest fix.

## Report

1. Group findings by object, with the file and line, what Prometheus does, and the fix.
2. Say what wasn't checked: the summary counts the links the inputs don't settle, such as a Service whose pods come from an operator, or a namespace whose labels aren't in the files. `--format json` lists them.

## Don't

Don't edit manifests unless the user asks. Don't call scraping healthy because unscraped is quiet: it checks how the objects connect, not whether the endpoint serves metrics, the pods are ready, or network policies allow the scrape. Don't guess a Prometheus selector when none is in the inputs; ask for the Prometheus resource instead.
