package tests

import (
	"os"
	"strings"
	"testing"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(root(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBrokenExample(t *testing.T) {
	rep, r := runJSON(t, nil, "examples/broken/shop.yaml")
	if r.code != 1 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var rules []string
	for _, f := range rep.Findings {
		rules = append(rules, f.Rule+" "+f.Object)
	}
	want := []string{
		"US401 ServiceMonitor shop/cart",
		"US301 ServiceMonitor shop/checkout",
		"US202 ServiceMonitor shop/cart",
		"US102 PodMonitor payments/payments",
		"US303 PodMonitor shop/search",
		"US101 PrometheusRule shop/shop-alerts",
	}
	got := strings.Join(rules, "\n")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %s in\n%s", w, got)
		}
	}
	if len(rules) != len(want) {
		t.Errorf("got %d findings:\n%s", len(rules), got)
	}
}

func TestFixedExample(t *testing.T) {
	rep, r := runJSON(t, nil, "examples/fixed/shop.yaml")
	if r.code != 0 || len(rep.Findings) != 0 || rep.Summary.Unchecked != 0 {
		t.Fatalf("exit %d, %+v, %+v", r.code, rep.Findings, rep.Unchecked)
	}
}

func TestTextAndJSONAgree(t *testing.T) {
	rep, _ := runJSON(t, nil, "examples/broken/shop.yaml")
	r := runTool(t, nil, "examples/broken/shop.yaml")
	for _, f := range rep.Findings {
		if !strings.Contains(r.stdout, f.Message) {
			t.Errorf("text output lacks %q", f.Message)
		}
	}
}
