# unscraped: find ServiceMonitors and PodMonitors that Prometheus silently ignores

[![CI](https://github.com/GillMacedo/unscraped/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/unscraped/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for people who run Prometheus Operator or kube-prometheus-stack. It reads your Kubernetes manifests or `helm template` output and reports the ServiceMonitors, PodMonitors and PrometheusRules that Prometheus will never use: a missing `release` label, a namespace the Prometheus doesn't watch, a ServiceMonitor `port` that names the container port instead of the Service port, a selector copied from the Service's pod selector, a PodMonitor port the container doesn't declare, and a `scrapeTimeout` the operator rejects.**

None of these fail a deploy. `kubectl apply` succeeds, the operator ignores the object or generates a job with no targets, and the metrics or alerts are simply missing. You find out when a dashboard is empty or an alert never fires. unscraped follows each monitor through the Prometheus selectors, the Service and the pods in the same files, so it runs in CI next to your charts and never connects to a cluster.

## What you can ask Claude

- "Why doesn't my ServiceMonitor show up in Prometheus targets?"
- "Will kube-prometheus-stack pick up the ServiceMonitor in this chart?"
- "Check the monitors in our rendered Helm chart before I merge."
- "Our new alert never fires. Is the PrometheusRule even loaded?"
- "Does this PodMonitor's port match the container?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/unscraped
    claude plugin install unscraped@unscraped-marketplace

Or run it with Go 1.22 or newer. It has no dependencies outside this repository:

    helm template app ./chart -n shop > app.yaml
    kubectl get prometheus -A -o yaml > prometheus.yaml
    go run github.com/GillMacedo/unscraped/plugin/scripts@v0.1.0 prometheus.yaml app.yaml

## Example

`examples/broken/shop.yaml` is a made-up shop with one Prometheus, three Namespaces and four services, each monitor with a mistake people really make. `--no-fail` keeps the exit code at 0 for this demo; without it, the run exits 1:

    $ go run ./plugin/scripts --no-fail examples/broken/shop.yaml
    US301 ServiceMonitor shop/checkout (examples/broken/shop.yaml:100)
      Endpoint port "metrics" isn't the name of a port on Service shop/checkout, which has http, http-metrics. port means the Service port's name, so Prometheus finds no target for this endpoint.
      fix: set port to one of the Service's port names, or name the Service port "metrics"

    US202 ServiceMonitor shop/cart (examples/broken/shop.yaml:141)
      The selector app=cart matches the pod selector of Service shop/cart, but a ServiceMonitor selects Services by the Service's own metadata.labels, which are app.kubernetes.io/name=cart. It finds no targets.
      fix: change spec.selector to match the Service's metadata.labels, or add those labels to the Service

    US401 ServiceMonitor shop/cart (examples/broken/shop.yaml:153)
      scrapeTimeout 20s is longer than interval 15s. The operator rejects a ServiceMonitor like this and leaves it out of the Prometheus config.
      fix: make scrapeTimeout no longer than interval

    US102 PodMonitor payments/payments (examples/broken/shop.yaml:182)
      The labels match, but PodMonitor payments/payments isn't in a namespace Prometheus watches. Prometheus monitoring/main's podMonitorNamespaceSelector is monitoring=enabled, and namespace payments doesn't match it. The operator ignores this PodMonitor without an error.
      fix: add monitoring: "enabled" to the labels of Namespace payments, or change podMonitorNamespaceSelector on Prometheus monitoring/main

    US303 PodMonitor shop/search (examples/broken/shop.yaml:232)
      Endpoint port "metrics" isn't declared by any container of StatefulSet shop/search, which declare http (8080). Prometheus only scrapes declared container ports, so it finds no target for this endpoint.
      fix: use the name of a declared container port, or add the port to the container's ports

    US101 PrometheusRule shop/shop-alerts (examples/broken/shop.yaml:235)
      No Prometheus picks up this PrometheusRule, so the operator ignores it without an error. Prometheus monitoring/main selects release=monitoring, and this one has no release label.
      fix: add release: "monitoring" to metadata.labels

    6 findings: 13 checks in 2 ServiceMonitors, 2 PodMonitors and 1 PrometheusRule, against 1 Prometheus resource, from 1 file.

The same file with each mistake fixed stays quiet:

    $ go run ./plugin/scripts examples/fixed/shop.yaml
    No problems: 16 checks in 2 ServiceMonitors, 2 PodMonitors and 1 PrometheusRule, against 1 Prometheus resource, from 1 file.

## On real charts

`tests/testdata/real` holds unchanged `helm template` output of public charts. kube-prometheus-stack's Prometheus only selects objects labeled `release: <its release name>`. The ingress-nginx and cert-manager charts, rendered with their ServiceMonitors turned on and default labels, don't set that label, so Prometheus ignores both:

    $ go run ./plugin/scripts --no-fail tests/testdata/real/kube-prometheus-stack/rendered.yaml tests/testdata/real/ingress-nginx/rendered.yaml tests/testdata/real/cert-manager/rendered.yaml
    US101 ServiceMonitor cert-manager/cert-manager (tests/testdata/real/cert-manager/rendered.yaml:1304)
      No Prometheus picks up this ServiceMonitor, so the operator ignores it without an error. Prometheus monitoring/kps-kube-prometheus-stack-prometheus selects release=kps, and this one has no release label.
      fix: add release: "kps" to metadata.labels

    US101 ServiceMonitor ingress-nginx/ingress-nginx-controller (tests/testdata/real/ingress-nginx/rendered.yaml:548)
      No Prometheus picks up this ServiceMonitor, so the operator ignores it without an error. Prometheus monitoring/kps-kube-prometheus-stack-prometheus selects release=kps, and this one has no release label.
      fix: add release: "kps" to metadata.labels

    2 findings: 79 checks in 14 ServiceMonitors and 35 PrometheusRules, against 1 Prometheus resource, from 3 files.
    Not checked: 4 links the inputs don't settle, such as a Service whose pods aren't in the files. --format json lists them.

The 4 links not checked are kube-prometheus-stack's own: the kubelet and API server Services, which the cluster creates, and the pods of Prometheus and Alertmanager, which the operator creates. Rendered with `controller.metrics.serviceMonitor.additionalLabels.release=kps`, ingress-nginx is quiet:

    $ go run ./plugin/scripts tests/testdata/real/kube-prometheus-stack/rendered.yaml tests/testdata/real/ingress-nginx/rendered-release-label.yaml
    No problems: 75 checks in 13 ServiceMonitors and 35 PrometheusRules, against 1 Prometheus resource, from 2 files.
    Not checked: 4 links the inputs don't settle, such as a Service whose pods aren't in the files. --format json lists them.

The exit code tells CI what happened:

    $ go build -o /tmp/unscraped ./plugin/scripts && /tmp/unscraped examples/broken/shop.yaml > /dev/null; echo "exit $?"
    exit 1

Add `--format json` for machine-readable output, with the rule, object, file, line, message and fix of each finding, and every link that wasn't checked with the reason.

## What it reports

| Rule | Finds |
|---|---|
| `US101` | No Prometheus, PrometheusAgent or ThanosRuler in the inputs selects the object's labels, often a missing `release` label. Also a Prometheus whose selector is null (matches nothing) or that has no scrape selectors at all |
| `US102` | The labels match, but the Prometheus's namespace selector excludes the object's namespace. A null namespace selector means the Prometheus's own namespace only |
| `US201` | A ServiceMonitor's selector matches a Service in another namespace, and its `namespaceSelector` doesn't look there |
| `US202` | A ServiceMonitor's selector matches a Service's `spec.selector` (the pod labels), not the Service's own labels |
| `US203` | A PodMonitor's selector matches Service labels, and no pod template has them |
| `US204` | A PodMonitor's selector matches pods in another namespace, and its `namespaceSelector` doesn't look there |
| `US301` | A ServiceMonitor endpoint `port` that isn't the name of a port on the selected Services |
| `US302` | The Service port it names has a `targetPort` name that no container behind the Service declares, so the Service has no endpoint for it |
| `US303` | A PodMonitor `port`, `portNumber` or `targetPort` that no selected container declares |
| `US304` | A ServiceMonitor `targetPort` that the pods behind the selected Services don't declare |
| `US401` | `scrapeTimeout` longer than `interval` on the same endpoint, which the operator rejects |
| `US402` | An empty `endpoints` or `podMetricsEndpoints` list |

Every finding is something the inputs settle. When they don't, unscraped lists the link as not checked instead of guessing: a Service whose pods aren't in the files (often pods an operator creates), a monitor whose selector matches no Service in the files at all, a namespace selector that reads labels of a Namespace that isn't in the files, or no Prometheus in the files.

## The rules it follows

The selection rules follow the Prometheus Operator API reference (`Documentation/api-reference/api.md`): for `serviceMonitorSelector`, `podMonitorSelector` and `ruleSelector`, an empty selector matches everything and a null one matches nothing; a null namespace selector means the Prometheus's own namespace; when all scrape selectors are null the configuration is unmanaged; `ignoreNamespaceSelectors` makes monitors look only in their own namespace. A ServiceMonitor's `port` is the Service port's name, its `targetPort` a container port, and a PodMonitor scrapes declared container ports only. `scrapeTimeout` "cannot be greater than the scrape interval otherwise the operator will reject the resource." Label selectors (`matchLabels`, and `matchExpressions` with `In`, `NotIn`, `Exists`, `DoesNotExist`) are checked against Kubernetes' own implementation in `tests/k8s`.

## What it reads

- YAML or JSON with one or more objects, `kind: List` from `kubectl get -o yaml`, `helm template` and `kubectl kustomize` output, directories (walked for `.yaml`, `.yml` and `.json`) and `-` for stdin.
- ServiceMonitor, PodMonitor and PrometheusRule (`monitoring.coreos.com`); Prometheus, PrometheusAgent and ThanosRuler; Service; Namespace; Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, ReplicationController, Job, CronJob, PodTemplate, and any kind with `spec.template.spec.containers`, such as an Argo Rollout. Init containers count, since Prometheus discovers their ports too.
- `-n NS` sets the namespace for objects without one, as `helm template -n` and `kubectl apply -n` do. The default is `default`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | No findings, or `--no-fail` was given. |
| 1 | Findings. |
| 2 | An input can't be trusted, the flags are wrong, there's no monitor or rule in the inputs, or nothing could be checked. Stdout is empty. |

## When it refuses

unscraped exits 2 and prints only the reason when it can't trust what it read, with or without `--no-fail`. For example, when `helm template` failed and its error went into the file:

    $ go build -o /tmp/unscraped ./plugin/scripts && /tmp/unscraped tests/testdata/bad/helm-schema-error.txt 2>&1; echo "exit $?"
    unscraped: can't trust the input, nothing was checked: tests/testdata/bad/helm-schema-error.txt: this looks like the output of a failed command, not manifests: "Error: values don't meet the specifications of the schema(s) in the following chart(s):"
    exit 2

It does the same for an empty file, comments only, random text, binary data, the output of a failed `helm template`, `kubectl`, `go build`, `go test` or `curl`, an HTML error page, a Kubernetes API error (`kind: Status`), an empty `kind: List`, Helm values, a workflow or other YAML that isn't a manifest, an unrendered Helm template, invalid YAML, an error line appended after the manifests, a ServiceMonitor or other operator kind with an apiVersion outside `monitoring.coreos.com`, the same object twice, an invalid label selector, and a missing file. A file must end with a line break, its last key must have a value, and every object needs `apiVersion`, `kind` and `metadata.name`. A ServiceMonitor or PodMonitor needs a selector and an endpoint list, a PrometheusRule needs groups whose rules have `expr`, a Service needs ports unless it's headless, and a workload needs containers with a name and image. One bad file among good ones fails the whole run. Inputs with no monitor or rule, and inputs where not a single link could be checked (for example a ServiceMonitor alone), are refused as having nothing to check.

A file cut exactly on a line break can still be a valid, shorter file, and no tool can tell it apart from a complete one. Most such cuts leave a key without a value or an object without required fields and are refused; the rest are read as the shorter file. Check the counts in the summary line against what you expect.

## How it compares

- **Prometheus Operator itself** reports selection only on a live cluster, in the object's status or the operator's logs, after the deploy.
- **Manifest linters** such as kube-linter, kube-score and kubeconform check one object or the Service-to-pod link; kube-linter's `dangling-servicemonitor` checks that a ServiceMonitor's selector matches a Service. None of them reads the Prometheus selectors or the port names, as far as their documented checks show.
- **`promtool` and `pint`** check the rule expressions inside a PrometheusRule, not whether Prometheus loads it.
- **Live debuggers** such as k8s-monitor-debugger read the same links from a running cluster.

unscraped checks the whole chain from files, before anything is deployed.

## Limits

- **It reads only what you pass.** Services and pods created by operators or by other charts, and labels on Namespaces you didn't pass, can't be checked. They're listed, not guessed.
- **It doesn't scrape.** It can't tell whether the endpoint serves metrics, the pods are ready, a NetworkPolicy blocks Prometheus, or relabeling drops the target.
- **Probe and ScrapeConfig** resources, `scrapeClass`, and `additionalScrapeConfigs` aren't read.
- **Alertmanager config** (AlertmanagerConfig selection and routing) isn't checked.
- **Timeouts** are compared only when both `interval` and `scrapeTimeout` are set on the endpoint.

## FAQ

**Why doesn't my ServiceMonitor show up in Prometheus targets?** The most common reasons are that the Prometheus resource's `serviceMonitorSelector` doesn't match its labels (kube-prometheus-stack wants `release: <release name>`), the namespace isn't selected, or the endpoint `port` isn't the name of a Service port. unscraped reports each of these with the file and line.

**What label does kube-prometheus-stack need on a ServiceMonitor?** By default `release` set to the Helm release name of the stack. Rendering the stack's chart with your values and passing it to unscraped shows the exact selector.

**Is the ServiceMonitor port the container port or the Service port?** `port` is the name of a port in the Service's `spec.ports`. `targetPort` is a container port. A PodMonitor's `port` is a container port name.

**Why isn't my PrometheusRule loaded?** The Prometheus resource's `ruleSelector` and `ruleNamespaceSelector` must match it. That's `US101` and `US102`.

**Does it need a cluster?** No. It reads files: rendered charts, manifests, or `kubectl get -o yaml` output you saved.

**Is it an MCP server?** No. It's a skill with one Go program, so `go run` or `go build` is all it needs.

## Tested on

- **Real charts:** `helm template` output (Helm v4.3.2) of kube-prometheus-stack 93.3.0, ingress-nginx 4.15.1, cert-manager v1.21.3, argo-cd 10.18.0 and flux2 2.20.0, with their monitors turned on. kube-prometheus-stack alone (12 ServiceMonitors, 35 PrometheusRules) is quiet. With its default selectors, the 10 monitors the other four charts render without a `release` label are reported as `US101`, and nothing else. With a Prometheus that selects everything (the stack rendered with the `*SelectorNilUsesHelmValues=false` values), all 9 ServiceMonitors and the PodMonitor of those charts are followed to their Services and pods, every link resolves, and the run is quiet. Breaking a port name, a `targetPort` or a timeout in those real files is caught each time.
- **Kubernetes' selector code:** `tests/k8s` runs 400 random label selectors and label sets through the built binary and `k8s.io/apimachinery` v0.31.4, and they agree on every one.
- **Input it can't trust:** 29 files in `tests/testdata/bad`, including real output of a failed `helm template` (a missing chart and a schema error), `kubectl` with no cluster, `go build` and `go test`, are each refused alone, with and without `--no-fail`, in text and JSON, and on stdin. All but two (valid manifests with nothing to check) are also refused next to a real chart in either order, and in a directory, where valid YAML that isn't a manifest is skipped and listed instead. Other tests cut five files at regular byte offsets and at every line break: every mid-line cut is refused, and line cuts are refused or read as shorter files. For the broken example no cut exits 0.
- **Go versions:** `make test` passes with Go 1.22.12 and Go 1.27.3 on Linux arm64, offline. CI runs it on Linux amd64 with Go 1.22 and the current release.

Not tested: a live cluster with Prometheus Operator, so the operator's behavior comes from its API reference, not from running it; Prometheus Operator versions before the current API reference; macOS and Windows; a fresh Claude Code session using the skill. The `go run ...@v0.1.0` line works only once the repository is published.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/unscraped/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `unscraped` found your missing targets, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
