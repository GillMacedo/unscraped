// Package tests runs unscraped as a built binary, the way the skill does,
// against the examples, real rendered Helm charts in testdata/real, and inputs
// it must refuse.
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "unscraped-tests")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "unscraped")
	cmd := exec.Command("go", "build", "-o", bin, "./plugin/scripts")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building unscraped: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

// runTool runs unscraped from the repository root.
func runTool(t *testing.T, stdin []byte, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = ".."
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, out.String(), errb.String()}
}

type jsonFinding struct {
	Rule, Object, File, Message, Fix string
	Line                             int
}

type jsonReport struct {
	Findings  []jsonFinding
	Unchecked []struct{ Object, Check, Reason string }
	Summary   struct {
		Files, Objects, ServiceMonitors, PodMonitors, PrometheusRules, Prometheuses, Checks, Findings, Unchecked int
	}
}

func runJSON(t *testing.T, stdin []byte, args ...string) (jsonReport, result) {
	t.Helper()
	r := runTool(t, stdin, append([]string{"--format", "json"}, args...)...)
	var rep jsonReport
	if r.code == 2 {
		return rep, r
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatalf("bad JSON (%v):\n%s\nstderr: %s", err, r.stdout, r.stderr)
	}
	return rep, r
}

// root returns a path relative to the repository root.
func root(p ...string) string { return filepath.Join(append([]string{".."}, p...)...) }

const realDir = "tests/testdata/real/"

var (
	kps          = realDir + "kube-prometheus-stack/rendered.yaml"
	selectAll    = realDir + "kube-prometheus-stack/prometheus-select-all.yaml"
	nginx        = realDir + "ingress-nginx/rendered.yaml"
	nginxRelease = realDir + "ingress-nginx/rendered-release-label.yaml"
	certManager  = realDir + "cert-manager/rendered.yaml"
	argoCD       = realDir + "argo-cd/rendered.yaml"
	flux         = realDir + "flux2/rendered.yaml"
)
