# unscraped plugin

This directory is the Claude Code plugin for unscraped, which checks Prometheus Operator ServiceMonitors, PodMonitors and PrometheusRules in Kubernetes manifests for the mistakes that make Prometheus ignore them or find no target. `skills/checking-prometheus-monitors/SKILL.md` is the skill, and `scripts/` is the Go program it builds. See the [repository README](https://github.com/GillMacedo/unscraped#readme) for examples and limits.

Install it with:

    claude plugin marketplace add GillMacedo/unscraped
    claude plugin install unscraped@unscraped-marketplace
