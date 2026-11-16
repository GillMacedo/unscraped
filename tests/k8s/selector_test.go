// Package k8s checks unscraped's label selector matching against the real
// Kubernetes implementation, k8s.io/apimachinery, through the built binary.
package k8s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	keys   = []string{"release", "team", "app"}
	values = []string{"kps", "prod", "a", "b"}
)

func randLabels(r *rand.Rand) map[string]string {
	m := map[string]string{}
	for _, k := range keys {
		if r.Intn(2) == 0 {
			m[k] = values[r.Intn(len(values))]
		}
	}
	return m
}

func randSelector(r *rand.Rand) *metav1.LabelSelector {
	s := &metav1.LabelSelector{}
	if r.Intn(2) == 0 {
		s.MatchLabels = map[string]string{}
		for _, k := range keys {
			if r.Intn(3) == 0 {
				s.MatchLabels[k] = values[r.Intn(len(values))]
			}
		}
	}
	ops := []metav1.LabelSelectorOperator{metav1.LabelSelectorOpIn, metav1.LabelSelectorOpNotIn, metav1.LabelSelectorOpExists, metav1.LabelSelectorOpDoesNotExist}
	for i := r.Intn(3); i > 0; i-- {
		e := metav1.LabelSelectorRequirement{Key: keys[r.Intn(len(keys))], Operator: ops[r.Intn(len(ops))]}
		if e.Operator == metav1.LabelSelectorOpIn || e.Operator == metav1.LabelSelectorOpNotIn {
			for j := r.Intn(2) + 1; j > 0; j-- {
				e.Values = append(e.Values, values[r.Intn(len(values))])
			}
		}
		s.MatchExpressions = append(s.MatchExpressions, e)
	}
	return s
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestSelectorsMatchKubernetes builds a Prometheus with a random
// serviceMonitorSelector and a ServiceMonitor with random labels, and checks
// that unscraped reports US101 exactly when apimachinery says the selector
// doesn't match.
func TestSelectorsMatchKubernetes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "unscraped")
	build := exec.Command("go", "build", "-o", bin, "./plugin/scripts")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	r := rand.New(rand.NewSource(1))
	matched, missed := 0, 0
	for i := 0; i < 400; i++ {
		sel := randSelector(r)
		labels := randLabels(r)
		ks, err := metav1.LabelSelectorAsSelector(sel)
		if err != nil {
			t.Fatal(err)
		}
		want := ks.Matches(labelsSet(labels))
		input := fmt.Sprintf(`{"apiVersion":"v1","kind":"List","items":[
{"apiVersion":"monitoring.coreos.com/v1","kind":"Prometheus","metadata":{"name":"p","namespace":"m"},"spec":{"serviceMonitorSelector":%s,"serviceMonitorNamespaceSelector":{}}},
{"apiVersion":"monitoring.coreos.com/v1","kind":"ServiceMonitor","metadata":{"name":"s","namespace":"m","labels":%s},"spec":{"selector":{},"endpoints":[{"port":"web"}]}}
]}
`, toJSON(sel), toJSON(labels))
		cmd := exec.Command(bin, "--format", "json", "-")
		cmd.Stdin = strings.NewReader(input)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		cmd.Run()
		code := cmd.ProcessState.ExitCode()
		if code == 2 {
			// Nothing else to check means it refused: only possible when the
			// selector matched, and no Service is in the inputs.
			if !want {
				t.Fatalf("case %d refused but selector doesn't match: %s\n%s", i, errb.String(), input)
			}
			matched++
			continue
		}
		got := !strings.Contains(out.String(), `"US101"`)
		if got != want {
			t.Fatalf("case %d: unscraped says match=%v, apimachinery says %v\n%s\n%s", i, got, want, input, out.String())
		}
		if want {
			matched++
		} else {
			missed++
		}
	}
	if matched < 50 || missed < 50 {
		t.Fatalf("too few cases of each kind: %d match, %d don't", matched, missed)
	}
	os.Stdout.WriteString(fmt.Sprintf("%d matching and %d non-matching selectors agree with apimachinery\n", matched, missed))
}
