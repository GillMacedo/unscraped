// Command unscraped reads Kubernetes manifests and reports the Prometheus
// Operator ServiceMonitors, PodMonitors and PrometheusRules that Prometheus
// will silently ignore or that find no target: labels the Prometheus
// selectors don't match, namespaces it doesn't watch, port names that don't
// exist on the Service or pod, and timeouts the operator rejects. It reads
// files only.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

const usage = `usage: unscraped [--namespace NS] [--format text|json] [--no-fail] PATH...

Checks Prometheus Operator ServiceMonitors, PodMonitors and PrometheusRules
against the Prometheus, Service, workload and Namespace objects in the same
inputs. PATH can be a manifest file (YAML or JSON, helm template output,
kubectl get -o yaml), a directory, or - for stdin.

  --namespace NS  namespace for objects that don't set one (default "default")
  --format FMT    text (default) or json
  --no-fail       exit 0 even when there are findings

Exit codes: 0 no findings, 1 findings, 2 bad flags or input that can't be
trusted, or nothing could be checked. On 2, stdout is empty.
`

type summary struct {
	Files           int `json:"files"`
	Objects         int `json:"objects"`
	ServiceMonitors int `json:"serviceMonitors"`
	PodMonitors     int `json:"podMonitors"`
	PrometheusRules int `json:"prometheusRules"`
	Prometheuses    int `json:"prometheuses"`
	Checks          int `json:"checks"`
	Findings        int `json:"findings"`
	Unchecked       int `json:"unchecked"`
}

type report struct {
	Findings  []Finding   `json:"findings"`
	Unchecked []Unchecked `json:"unchecked"`
	Skipped   []string    `json:"skipped"`
	Summary   summary     `json:"summary"`
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("unscraped", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	format := fl.String("format", "text", "")
	ns := fl.String("namespace", "default", "")
	fl.StringVar(ns, "n", "default", "")
	noFail := fl.Bool("no-fail", false, "")
	showVersion := fl.Bool("version", false, "")
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		fmt.Fprintf(stderr, "unscraped: %v\n%s", err, usage)
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "unscraped", version)
		return 0
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "unscraped: --format must be text or json, not %q\n", *format)
		return 2
	}
	if *ns == "" {
		fmt.Fprintln(stderr, "unscraped: --namespace can't be empty")
		return 2
	}
	if fl.NArg() == 0 {
		fmt.Fprint(stderr, "unscraped: no input files\n"+usage)
		return 2
	}
	refuse := func(err error) int {
		fmt.Fprintf(stderr, "unscraped: can't trust the input, nothing was checked: %v\n", err)
		return 2
	}
	srcs, err := readArgs(fl.Args(), stdin)
	if err != nil {
		return refuse(err)
	}
	inv, err := load(srcs, *ns)
	if err != nil {
		return refuse(err)
	}
	if inv.files == 0 {
		return refuse(errors.New("none of the files holds Kubernetes manifests"))
	}
	if len(inv.monitors) == 0 && len(inv.rules) == 0 {
		return refuse(fmt.Errorf("the inputs have %s and no ServiceMonitor, PodMonitor or PrometheusRule (monitoring.coreos.com/v1), so there's nothing to check", plural(inv.objects, "object", "objects")))
	}
	res := check(inv)
	rep := report{Findings: res.findings, Unchecked: res.unchecked, Skipped: inv.skipped}
	if rep.Findings == nil {
		rep.Findings = []Finding{}
	}
	if rep.Unchecked == nil {
		rep.Unchecked = []Unchecked{}
	}
	if rep.Skipped == nil {
		rep.Skipped = []string{}
	}
	s := &rep.Summary
	s.Files, s.Objects, s.Checks = inv.files, inv.objects, res.checked
	s.Findings, s.Unchecked = len(res.findings), len(res.unchecked)
	for _, m := range inv.monitors {
		if m.kind == "ServiceMonitor" {
			s.ServiceMonitors++
		} else {
			s.PodMonitors++
		}
	}
	s.PrometheusRules = len(inv.rules)
	s.Prometheuses = len(inv.servers)
	if res.checked == 0 {
		return refuse(fmt.Errorf("nothing could be checked. %s. Pass the Prometheus resource (kubectl get prometheus -A -o yaml) and the Services and workloads the monitors select", firstReason(res.unchecked)))
	}

	if *format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(rep)
	} else {
		writeText(stdout, rep)
	}
	if len(res.findings) > 0 && !*noFail {
		return 1
	}
	return 0
}

func firstReason(u []Unchecked) string {
	if len(u) == 0 {
		return "No check applied"
	}
	return fmt.Sprintf("For example, %s: %s", u[0].Object, u[0].Reason)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func writeText(w io.Writer, rep report) {
	for _, f := range rep.Findings {
		fmt.Fprintf(w, "%s %s (%s:%d)\n", f.Rule, f.Object, f.File, f.Line)
		fmt.Fprintf(w, "  %s\n", f.Message)
		fmt.Fprintf(w, "  fix: %s\n\n", f.Fix)
	}
	s := rep.Summary
	var what []string
	if s.ServiceMonitors > 0 {
		what = append(what, plural(s.ServiceMonitors, "ServiceMonitor", "ServiceMonitors"))
	}
	if s.PodMonitors > 0 {
		what = append(what, plural(s.PodMonitors, "PodMonitor", "PodMonitors"))
	}
	if s.PrometheusRules > 0 {
		what = append(what, plural(s.PrometheusRules, "PrometheusRule", "PrometheusRules"))
	}
	list := strings.Join(what, ", ")
	if i := strings.LastIndex(list, ", "); i >= 0 {
		list = list[:i] + " and " + list[i+2:]
	}
	lead := "No problems"
	if s.Findings > 0 {
		lead = plural(s.Findings, "finding", "findings")
	}
	fmt.Fprintf(w, "%s: %s in %s, against %s, from %s.\n", lead,
		plural(s.Checks, "check", "checks"), list,
		plural(s.Prometheuses, "Prometheus resource", "Prometheus resources"), plural(s.Files, "file", "files"))
	if s.Unchecked > 0 {
		fmt.Fprintf(w, "Not checked: %s the inputs don't settle, such as a Service whose pods aren't in the files. --format json lists them.\n", plural(s.Unchecked, "link", "links"))
	}
	if len(rep.Skipped) > 0 {
		fmt.Fprintf(w, "Skipped %s that aren't manifests: %s.\n", plural(len(rep.Skipped), "file", "files"), strings.Join(rep.Skipped, "; "))
	}
}
