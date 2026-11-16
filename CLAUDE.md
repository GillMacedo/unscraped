# unscraped

Claude Code skill and Go CLI that reads Kubernetes manifests and `helm template` output and reports Prometheus Operator ServiceMonitors, PodMonitors and PrometheusRules that Prometheus ignores or that find no target: labels and namespaces the Prometheus selectors don't match, selectors that find no Service or pod, port names that don't exist, and timeouts the operator rejects.

## Architecture
`plugin/scripts/` is one `package main` with no dependencies outside it. `unscraped.go` reads flags and prints the report. `input.go` reads files and refuses bad input. `model.go` turns objects into Services, pod templates, Namespaces, monitors, rules and servers (Prometheus, PrometheusAgent, ThanosRuler). `selector.go` is label selector matching. `check.go` holds every rule. The `yaml_*.go` files are a copy of go.yaml.in/yaml/v3 renamed to `package main`. `plugin/skills/checking-prometheus-monitors/SKILL.md` tells Claude how to use it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests and the binary tests in `tests/`. No network.
- `make test-k8s`: compares label selector matching with `k8s.io/apimachinery` (`tests/k8s`, its own module). Needs network once.
- `make lint`: plugin manifest validation (needs Claude Code)
- `make demo`: run the examples

## Conventions
- Standard library only, plus the vendored YAML copy. No network access, ever: unscraped reads files.
- An input that can't be read completely exits 2 with nothing on stdout, with or without `--no-fail`. Never skip a bad file silently. See `docs/decisions/0002-refuse-untrusted-input.md`.
- A finding needs the inputs to settle it. If a Service, pod or Namespace isn't in the files, list the link as unchecked; never guess. See `docs/decisions/0003-findings-only-when-the-inputs-settle-it.md`.
- Each rule has a stable `USnnn` ID, a message that says what Prometheus or the operator does, and a fix. Don't reuse IDs.
- Every rule follows the Prometheus Operator API reference; cite the field docs in the PR.
- README examples are real output. `tests/readme_test.go` runs every `$ ` line and compares.

## Do and don't
- Do run a new rule on `tests/testdata/real` and read what it reports before adding it. The quiet runs in `tests/real_test.go` must stay quiet.
- Don't add checks that need a cluster or Prometheus.

## Definition of done
`make test`, `make test-k8s` and `make lint` pass, and `README.md`, `ROADMAP.md` and the skill are updated when relevant.
