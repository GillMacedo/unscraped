package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const prom = `apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: main
  namespace: monitoring
spec:
  serviceMonitorSelector:
    matchLabels:
      release: kps
  serviceMonitorNamespaceSelector: {}
  podMonitorSelector:
    matchLabels:
      release: kps
  podMonitorNamespaceSelector: {}
  ruleSelector:
    matchLabels:
      release: kps
  ruleNamespaceSelector: {}
`

const app = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: shop
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: example.com/api:1.0.0
          ports:
            - name: metrics
              containerPort: 9090
---
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: shop
  labels:
    app.kubernetes.io/name: api
spec:
  selector:
    app: api
  ports:
    - name: http-metrics
      port: 9090
      targetPort: metrics
`

const goodSM = `apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: api
  namespace: shop
  labels:
    release: kps
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: api
  endpoints:
    - port: http-metrics
`

const goodPM = `apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: api
  namespace: shop
  labels:
    release: kps
spec:
  selector:
    matchLabels:
      app: api
  podMetricsEndpoints:
    - port: metrics
`

const goodRule = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: api
  namespace: shop
  labels:
    release: kps
spec:
  groups:
    - name: api
      rules:
        - alert: ApiDown
          expr: up{job="api"} == 0
`

func docs(parts ...string) string { return strings.Join(parts, "---\n") }

type out struct {
	code   int
	rules  []string
	rep    report
	stderr string
}

func runYAML(t *testing.T, input string, args ...string) out {
	t.Helper()
	var so, se bytes.Buffer
	code := run(append(append([]string{"--format", "json"}, args...), "-"), strings.NewReader(input), &so, &se)
	o := out{code: code, stderr: se.String()}
	if code == 2 {
		if so.Len() != 0 {
			t.Fatalf("exit 2 with stdout: %s", so.String())
		}
		return o
	}
	if err := json.Unmarshal(so.Bytes(), &o.rep); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, so.String())
	}
	for _, f := range o.rep.Findings {
		o.rules = append(o.rules, f.Rule)
	}
	sort.Strings(o.rules)
	return o
}

func TestRules(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // space-separated rules, empty for quiet
	}{
		{"healthy service monitor", docs(prom, app, goodSM), ""},
		{"healthy pod monitor", docs(prom, app, goodPM), ""},
		{"healthy rule", docs(prom, goodRule), ""},
		{"US101 no release label", docs(prom, app, strings.Replace(goodSM, "release: kps", "team: shop", 1)), "US101"},
		{"US101 wrong release value", docs(prom, app, strings.Replace(goodSM, "release: kps", "release: prometheus", 1)), "US101"},
		{"US101 rule not selected", docs(prom, strings.Replace(goodRule, "release: kps", "team: shop", 1)), "US101"},
		{"US101 null selector", docs(strings.Replace(prom, "  podMonitorSelector:\n    matchLabels:\n      release: kps\n", "", 1), app, goodPM), "US101"},
		{"US101 unmanaged", docs(`apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: main
  namespace: monitoring
spec:
  replicas: 1
`, app, goodSM), "US101"},
		{"empty selector selects all", docs(strings.Replace(prom, "  serviceMonitorSelector:\n    matchLabels:\n      release: kps\n", "  serviceMonitorSelector: {}\n", 1), app, strings.Replace(goodSM, "release: kps", "team: shop", 1)), ""},
		{"US102 null namespace selector", docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "", 1), app, goodSM), "US102"},
		{"null namespace selector, same namespace", docs(strings.Replace(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "", 1), "namespace: monitoring", "namespace: shop", 1), app, goodSM), ""},
		{"US102 namespace label missing", docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "  serviceMonitorNamespaceSelector:\n    matchLabels:\n      monitoring: enabled\n", 1),
			"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: shop\n", app, goodSM), "US102"},
		{"namespace label present", docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "  serviceMonitorNamespaceSelector:\n    matchLabels:\n      monitoring: enabled\n", 1),
			"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: shop\n  labels:\n    monitoring: enabled\n", app, goodSM), ""},
		{"namespace by metadata.name without Namespace object", docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "  serviceMonitorNamespaceSelector:\n    matchLabels:\n      kubernetes.io/metadata.name: shop\n", 1), app, goodSM), ""},
		{"US201 service in another namespace", docs(prom, app, strings.Replace(goodSM, "namespace: shop", "namespace: monitoring", 1)), "US201"},
		{"namespaceSelector any", docs(prom, app, strings.Replace(strings.Replace(goodSM, "namespace: shop", "namespace: monitoring", 1), "spec:\n", "spec:\n  namespaceSelector:\n    any: true\n", 1)), ""},
		{"namespaceSelector matchNames", docs(prom, app, strings.Replace(strings.Replace(goodSM, "namespace: shop", "namespace: monitoring", 1), "spec:\n", "spec:\n  namespaceSelector:\n    matchNames: [shop]\n", 1)), ""},
		{"US202 selects pod labels", docs(prom, app, strings.Replace(goodSM, "app.kubernetes.io/name: api", "app: api", 1)), "US202"},
		{"US203 pod monitor selects service labels", docs(prom, app, strings.Replace(goodPM, "app: api", "app.kubernetes.io/name: api", 1)), "US203"},
		{"US204 pods in another namespace", docs(prom, app, strings.Replace(goodPM, "namespace: shop", "namespace: monitoring", 1)), "US204"},
		{"US301 container port name used", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "port: metrics", 1)), "US301"},
		{"US301 unnamed service port", docs(prom, strings.Replace(app, "    - name: http-metrics\n      port: 9090", "    - port: 9090", 1), goodSM), "US301"},
		{"US302 service targetPort name not on pod", docs(prom, strings.Replace(app, "targetPort: metrics", "targetPort: prom", 1), goodSM), "US302"},
		{"numeric targetPort is fine", docs(prom, strings.Replace(app, "targetPort: metrics", "targetPort: 9090", 1), goodSM), ""},
		{"US303 pod monitor port not declared", docs(prom, app, strings.Replace(goodPM, "port: metrics", "port: http-metrics", 1)), "US303"},
		{"US303 portNumber not declared", docs(prom, app, strings.Replace(goodPM, "port: metrics", "portNumber: 8080", 1)), "US303"},
		{"portNumber declared", docs(prom, app, strings.Replace(goodPM, "port: metrics", "portNumber: 9090", 1)), ""},
		{"US304 targetPort not declared", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "targetPort: prom", 1)), "US304"},
		{"targetPort declared", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "targetPort: metrics", 1)), ""},
		{"targetPort number declared", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "targetPort: 9090", 1)), ""},
		{"US401 timeout over interval", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "port: http-metrics\n      interval: 15s\n      scrapeTimeout: 1m", 1)), "US401"},
		{"timeout equal to interval", docs(prom, app, strings.Replace(goodSM, "port: http-metrics", "port: http-metrics\n      interval: 1m\n      scrapeTimeout: 60s", 1)), ""},
		{"US402 empty endpoints", docs(prom, app, strings.Replace(goodSM, "  endpoints:\n    - port: http-metrics\n", "  endpoints: []\n", 1)), "US402"},
		{"matchExpressions In", docs(prom, app, strings.Replace(goodSM, "    matchLabels:\n      app.kubernetes.io/name: api\n", "    matchExpressions:\n      - key: app.kubernetes.io/name\n        operator: In\n        values: [api, web]\n", 1)), ""},
		{"matchExpressions NotIn excludes", docs(prom, app, strings.Replace(goodSM, "    matchLabels:\n      app.kubernetes.io/name: api\n", "    matchExpressions:\n      - key: app.kubernetes.io/name\n        operator: NotIn\n        values: [api]\n      - key: app.kubernetes.io/name\n        operator: Exists\n", 1)), ""},
		{"prometheus selects by expression", docs(strings.Replace(prom, "  serviceMonitorSelector:\n    matchLabels:\n      release: kps\n", "  serviceMonitorSelector:\n    matchExpressions:\n      - key: release\n        operator: In\n        values: [kps, other]\n", 1), app, goodSM), ""},
		{"US101 expression mismatch", docs(strings.Replace(prom, "  serviceMonitorSelector:\n    matchLabels:\n      release: kps\n", "  serviceMonitorSelector:\n    matchExpressions:\n      - key: release\n        operator: DoesNotExist\n", 1), app, goodSM), "US101"},
		{"ignoreNamespaceSelectors", docs(strings.Replace(prom, "spec:\n", "spec:\n  ignoreNamespaceSelectors: true\n", 1), app, strings.Replace(strings.Replace(goodSM, "namespace: shop", "namespace: monitoring", 1), "spec:\n", "spec:\n  namespaceSelector:\n    any: true\n", 1)), "US201"},
		{"thanos ruler selects rule", docs(`apiVersion: monitoring.coreos.com/v1
kind: ThanosRuler
metadata:
  name: ruler
  namespace: shop
spec:
  ruleSelector:
    matchLabels:
      release: kps
`, goodRule), ""},
		{"agent doesn't load rules", docs(`apiVersion: monitoring.coreos.com/v1alpha1
kind: PrometheusAgent
metadata:
  name: agent
  namespace: monitoring
spec:
  serviceMonitorSelector: {}
  serviceMonitorNamespaceSelector: {}
`, app, goodSM), ""},
		{"kind List", "apiVersion: v1\nkind: List\nitems:\n" + indentList(prom, app, goodSM), ""},
		{"headless service without ports", docs(prom, app, goodSM, "apiVersion: v1\nkind: Service\nmetadata:\n  name: headless\n  namespace: shop\nspec:\n  clusterIP: None\n  selector:\n    app: api\n"), ""},
		{"several problems", docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "", 1), app, goodSM, strings.Replace(goodRule, "release: kps", "team: shop", 1)), "US101 US102"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := runYAML(t, c.input)
			if o.code == 2 {
				t.Fatalf("refused: %s", o.stderr)
			}
			got := strings.Join(o.rules, " ")
			if got != c.want {
				t.Fatalf("rules %q, want %q\n%+v", got, c.want, o.rep.Findings)
			}
			wantCode := 0
			if c.want != "" {
				wantCode = 1
			}
			if o.code != wantCode {
				t.Fatalf("exit %d, want %d", o.code, wantCode)
			}
			if c.want != "" {
				if o2 := runYAML(t, c.input, "--no-fail"); o2.code != 0 {
					t.Fatalf("--no-fail exit %d", o2.code)
				}
			}
		})
	}
}

// indentList turns documents into List items.
func indentList(parts ...string) string {
	var b strings.Builder
	var split []string
	for _, p := range parts {
		split = append(split, strings.Split(p, "---\n")...)
	}
	for _, p := range split {
		lines := strings.Split(strings.TrimRight(p, "\n"), "\n")
		for i, l := range lines {
			if i == 0 {
				b.WriteString("  - " + l + "\n")
			} else {
				b.WriteString("    " + l + "\n")
			}
		}
	}
	return b.String()
}

func TestUnknownNamespaceLabelsAreNotGuessed(t *testing.T) {
	in := docs(strings.Replace(prom, "  serviceMonitorNamespaceSelector: {}\n", "  serviceMonitorNamespaceSelector:\n    matchLabels:\n      monitoring: enabled\n", 1), app, goodSM)
	o := runYAML(t, in)
	if o.code != 0 || len(o.rules) != 0 {
		t.Fatalf("exit %d rules %v", o.code, o.rules)
	}
	found := false
	for _, u := range o.rep.Unchecked {
		if u.Check == "picked up by Prometheus" && strings.Contains(u.Reason, "Namespace shop isn't in the inputs") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no unchecked entry: %+v", o.rep.Unchecked)
	}
}

func TestRefuses(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"garbage":           "hello world\nthis is not yaml: [\n",
		"no newline":        strings.TrimSuffix(docs(prom, app, goodSM), "\n"),
		"dangling key":      docs(prom, app, goodSM) + "  sampleLimit:\n",
		"cut monitor":       docs(prom, app, "apiVersion: monitoring.coreos.com/v1\nkind: ServiceMonitor\nmetadata:\n  name: api\n  namespace: shop\n"),
		"cut service":       docs(prom, goodSM, "apiVersion: v1\nkind: Service\nmetadata:\n  name: api\n  namespace: shop\n"),
		"cut deployment":    docs(prom, goodSM, "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: shop\nspec:\n  replicas: 1\n"),
		"helm error":        "Error: INSTALLATION FAILED: execution error at (shop/templates/servicemonitor.yaml:3:4): release label is required\n",
		"kubectl error":     "error: the server doesn't have a resource type \"servicemonitors\"\n",
		"nothing checked":   goodSM,
		"no monitors":       docs(prom, app),
		"duplicate":         docs(prom, app, goodSM, goodSM),
		"bad selector":      docs(prom, app, strings.Replace(goodSM, "matchLabels:\n      app.kubernetes.io/name: api", "matchExpressions:\n      - key: app\n        operator: In", 1)),
		"api status":        "kind: Status\napiVersion: v1\nstatus: Failure\nmessage: forbidden\n",
		"empty list":        "apiVersion: v1\nkind: List\nitems: []\n",
		"values file":       "serviceMonitor:\n  enabled: true\n",
		"template":          "apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ include \"x\" . }}\n",
		"yaml then error":   docs(prom, app, goodSM) + "Error: timed out\n",
		"warnings first":    "Warning: monitoring.coreos.com/v1 is deprecated\n" + docs(prom, app, goodSM),
		"wrong api group":   docs(prom, app, goodSM, strings.Replace(strings.Replace(goodSM, "monitoring.coreos.com/v1", "monitoring.coreos.io/v1", 1), "name: api", "name: api2", 1)),
		"rule without expr": docs(prom, strings.Replace(goodRule, "          expr: up{job=\"api\"} == 0\n", "", 1)),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			for _, extra := range [][]string{nil, {"--no-fail"}} {
				o := runYAML(t, in, extra...)
				if o.code != 2 {
					t.Fatalf("exit %d with %v, want 2: %+v", o.code, extra, o.rep)
				}
				if !strings.Contains(o.stderr, "can't trust the input") {
					t.Fatalf("stderr: %s", o.stderr)
				}
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	for s, want := range map[string]int64{"30s": 30000, "1m": 60000, "1m30s": 90000, "500ms": 500, "1h": 3600000, "0": 0} {
		got, ok := parseDuration(s)
		if !ok || got != want {
			t.Errorf("%s: %d %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "30", "1.5s", "s", "30 s"} {
		if _, ok := parseDuration(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
}

func TestFlags(t *testing.T) {
	var so, se bytes.Buffer
	if c := run([]string{"--format", "xml", "x"}, nil, &so, &se); c != 2 {
		t.Fatalf("bad format exit %d", c)
	}
	if c := run([]string{}, nil, &so, &se); c != 2 {
		t.Fatalf("no args exit %d", c)
	}
	so.Reset()
	if c := run([]string{"--version"}, nil, &so, &se); c != 0 || !strings.Contains(so.String(), version) {
		t.Fatalf("version: %d %s", c, so.String())
	}
}
