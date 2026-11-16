package tests

import (
	"sort"
	"strings"
	"testing"
)

// The real charts stay quiet when the Prometheus selects them.
func TestRealChartsQuiet(t *testing.T) {
	runs := [][]string{
		{kps},
		{kps, nginxRelease},
		{"-n", "flux-system", selectAll, nginx, certManager, argoCD, flux},
	}
	for _, args := range runs {
		rep, r := runJSON(t, nil, args...)
		if r.code != 0 || len(rep.Findings) != 0 {
			t.Errorf("%v: exit %d, findings %+v, stderr %s", args, r.code, rep.Findings, r.stderr)
		}
		if rep.Summary.Checks == 0 {
			t.Errorf("%v: no checks", args)
		}
	}
	// With every link in the inputs, the app charts leave nothing unchecked.
	rep, _ := runJSON(t, nil, "-n", "flux-system", selectAll, nginx, certManager, argoCD, flux)
	if rep.Summary.Unchecked != 0 || rep.Summary.ServiceMonitors != 9 || rep.Summary.PodMonitors != 1 {
		t.Errorf("summary %+v, unchecked %+v", rep.Summary, rep.Unchecked)
	}
}

// With kube-prometheus-stack's default release-label selectors, every
// monitor the app charts render without that label is ignored, and nothing
// else is reported.
func TestRealChartsWithoutReleaseLabel(t *testing.T) {
	rep, r := runJSON(t, nil, "-n", "flux-system", kps, nginx, certManager, argoCD, flux)
	if r.code != 1 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var got []string
	for _, f := range rep.Findings {
		if f.Rule != "US101" || !strings.Contains(f.Message, "selects release=kps, and this one has no release label") {
			t.Errorf("unexpected finding %+v", f)
		}
		got = append(got, f.Object)
	}
	sort.Strings(got)
	want := []string{
		"PodMonitor flux-system/flux",
		"ServiceMonitor argocd/argocd-application-controller",
		"ServiceMonitor argocd/argocd-applicationset-controller",
		"ServiceMonitor argocd/argocd-dex-server",
		"ServiceMonitor argocd/argocd-notifications-controller",
		"ServiceMonitor argocd/argocd-redis",
		"ServiceMonitor argocd/argocd-repo-server",
		"ServiceMonitor argocd/argocd-server",
		"ServiceMonitor cert-manager/cert-manager",
		"ServiceMonitor ingress-nginx/ingress-nginx-controller",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r := runTool(t, nil, "--no-fail", "-n", "flux-system", kps, nginx, certManager, argoCD, flux); r.code != 0 {
		t.Errorf("--no-fail exit %d", r.code)
	}
}

// Breaking one real chart in each way is caught, with the select-all
// Prometheus so selection doesn't hide the target checks.
func TestRealChartsBroken(t *testing.T) {
	cases := []struct{ name, file, old, new, rule string }{
		{"service monitor port renamed", nginx, "  - port: metrics\n    interval: 30s", "  - port: prometheus\n    interval: 30s", "US301"},
		{"pod monitor port renamed", flux, "    - port: http-prom", "    - port: metrics", "US303"},
		{"service monitor targetPort renamed", certManager, "  - targetPort: http-metrics", "  - targetPort: metrics", "US304"},
		{"timeout over interval", certManager, "    interval: 60s\n    scrapeTimeout: 30s", "    interval: 10s\n    scrapeTimeout: 30s", "US401"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := readFile(t, c.file)
			if !strings.Contains(data, c.old) {
				t.Fatalf("%s doesn't contain %q", c.file, c.old)
			}
			broken := strings.Replace(data, c.old, c.new, 1)
			rep, r := runJSON(t, []byte(broken), "-n", "flux-system", selectAll, "-")
			if r.code != 1 || len(rep.Findings) != 1 || rep.Findings[0].Rule != c.rule {
				t.Fatalf("exit %d, findings %+v, stderr %s", r.code, rep.Findings, r.stderr)
			}
		})
	}
}
