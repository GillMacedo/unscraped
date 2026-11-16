# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-16

### Added
- Check Prometheus Operator ServiceMonitors, PodMonitors and PrometheusRules against the Prometheus, PrometheusAgent and ThanosRuler selectors in the inputs (`US101`, `US102`), follow monitors to their Services and pods (`US201` to `US204`), check port names and numbers (`US301` to `US304`), and report timeouts the operator rejects and empty endpoint lists (`US401`, `US402`).
- Links the inputs don't settle are listed as not checked, never guessed.
- `--namespace`, `--no-fail`, `--format json`, `kind: List`, directories and stdin.
- Exit 2, with nothing checked, for input that can't be trusted: empty, garbage, cut off, unrendered Helm templates, the output of a failed command, or nothing to check.
- `checking-prometheus-monitors` skill and plugin manifest.
