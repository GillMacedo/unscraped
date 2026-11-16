# Roadmap

## Next
- Probe and ScrapeConfig resources, with `probeSelector` and `scrapeConfigSelector`
- Compare a timeout set without an interval with the Prometheus resource's `scrapeInterval`
- `--ignore RULE` for accepted findings
- A GitHub Actions example that renders the stack and an app chart and runs unscraped on pull requests
- Read `kubectl kustomize` directories directly

## Later
- AlertmanagerConfig selection by the Alertmanager resource
- VictoriaMetrics operator kinds (VMServiceScrape, VMPodScrape, VMRule)
- A test that runs the real operator in a kind cluster and compares its selection with unscraped's
