# Architecture

unscraped is one Go `package main` in `plugin/scripts/`, so the skill can build it with `go build` and nothing else. It reads files, builds an inventory of the objects that matter, follows each monitor and rule through the links Prometheus Operator uses, and prints a report.

## Flow

1. **Read** (`input.go`, `readArgs`): files, directories (walked for `.yaml`, `.yml` and `.json`, skipping hidden folders, `vendor` and `node_modules`) and `-` for stdin.
2. **Refuse** (`input.go`, `checkBytes`, and `model.go`, `load`): an empty file, binary data, the output of a failed command, a file without a final line break or whose last key has no value, invalid YAML, prose, an unrendered Helm template, an API error, an empty List, a top-level key that isn't a Kubernetes field (an error line appended to the YAML), an object without `apiVersion`, `kind` or `metadata.name`, the same object twice, an invalid label selector, and objects missing required parts (see below) stop the run with exit 2. In a directory walk, YAML mappings that aren't manifests (Helm values, workflows, `package.json`) and Helm templates are skipped and listed instead.
3. **Inventory** (`model.go`, `add`): Namespaces and their labels; Services with labels, selector and ports; pod templates with labels and container ports (init containers included) from every workload kind; ServiceMonitors and PodMonitors with selector, `namespaceSelector` and endpoints; PrometheusRules; and Prometheus, PrometheusAgent and ThanosRuler with their six selectors and `ignoreNamespaceSelectors`. A monitor without a selector or endpoint list, a rule without `expr`, a Service without ports (unless headless or ExternalName), or a workload without containers is treated as a cut file.
4. **Check** (`check.go`, `check`):
   - Selection: for each monitor and rule, the servers whose label selector matches it, then their namespace selector. Null and empty selectors follow the operator's API reference. A namespace selector that reads labels of a Namespace not in the inputs makes the result unknown.
   - Targets: a ServiceMonitor's selector against Service labels in the namespaces it looks in, then each endpoint's `port` against Service port names, and the Service's `targetPort` name against the containers behind it. A PodMonitor's selector against pod template labels, then its port against declared container ports. When nothing matches, nearby mistakes are recognized (another namespace, the Service's pod selector, Service labels on a PodMonitor); otherwise the link is listed as unchecked.
   - Timeouts on each endpoint.
5. **Report** (`unscraped.go`): text or JSON, sorted by file and line, and the exit code. A run where no check reached an answer exits 2.

## Tests

- `plugin/scripts/unscraped_test.go`: each rule, with cases that must stay quiet, refusals, durations and flags.
- `tests/`: the built binary on the examples, on real rendered charts (quiet, with the expected `US101` findings, and with one change that breaks each), on 29 bad inputs alone, next to a real chart, in a directory and on stdin, on files cut at byte offsets and every line break, and on every `$ ` line in the README.
- `tests/k8s/`: a separate module that checks selector matching against `k8s.io/apimachinery` on 400 random cases.
