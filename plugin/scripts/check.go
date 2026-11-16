package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Finding is one problem that stops Prometheus from scraping a target or
// loading rules.
type Finding struct {
	Rule    string `json:"rule"`
	Object  string `json:"object"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
	objLine int
}

// Unchecked is something unscraped couldn't decide from the inputs.
type Unchecked struct {
	Object string `json:"object"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type result struct {
	findings  []Finding
	unchecked []Unchecked
	checked   int // checks that reached a definite answer
}

func (r *result) find(m meta, line int, rule, msg, fix string) {
	r.findings = append(r.findings, Finding{Rule: rule, Object: m.ref(), File: m.file, Line: line, Message: msg, Fix: fix, objLine: m.line})
}

func (r *result) skip(m meta, check, reason string) {
	r.unchecked = append(r.unchecked, Unchecked{Object: m.ref(), File: m.file, Line: m.line, Check: check, Reason: reason})
}

type tri int

const (
	no tri = iota
	unknown
	yes
)

// nsLabels returns a namespace's labels, and whether they're known. Every
// namespace has kubernetes.io/metadata.name, set by the API server.
func (inv *inventory) nsLabels(ns string) (map[string]string, bool) {
	l, ok := inv.namespaces[ns]
	out := map[string]string{"kubernetes.io/metadata.name": ns}
	for k, v := range l {
		out[k] = v
	}
	return out, ok
}

// nsSelected decides whether a server's namespace selector covers ns. A null
// selector covers only the server's own namespace.
func (inv *inventory) nsSelected(sel *selector, s *server, ns string) tri {
	if sel == nil {
		if ns == s.ns {
			return yes
		}
		return no
	}
	if sel.empty() {
		return yes
	}
	labels, known := inv.nsLabels(ns)
	if !known {
		for _, k := range sel.keys() {
			if k != "kubernetes.io/metadata.name" {
				return unknown
			}
		}
	}
	if sel.matches(labels) {
		return yes
	}
	return no
}

// selection is how the servers in the inputs treat one monitor or rule.
type selection struct {
	by        []*server // servers that pick it up
	unknownBy []*server // servers whose namespace selector needs labels not in the inputs
	labelOK   []*server // servers whose label selector matches but namespace selector doesn't
}

func (inv *inventory) selectionOf(m meta) selection {
	var sel selection
	for _, s := range inv.servers {
		var ls, ns *selector
		switch {
		case m.kind == "ServiceMonitor" && s.scrapes:
			ls, ns = s.smSel, s.smNs
		case m.kind == "PodMonitor" && s.scrapes:
			ls, ns = s.pmSel, s.pmNs
		case m.kind == "PrometheusRule" && s.rules:
			ls, ns = s.ruleSel, s.ruleNs
		default:
			continue
		}
		if !ls.matches(m.labels) {
			continue
		}
		switch inv.nsSelected(ns, s, m.ns) {
		case yes:
			sel.by = append(sel.by, s)
		case unknown:
			sel.unknownBy = append(sel.unknownBy, s)
		default:
			sel.labelOK = append(sel.labelOK, s)
		}
	}
	return sel
}

func (inv *inventory) serversFor(kind string) []*server {
	var out []*server
	for _, s := range inv.servers {
		if (kind == "PrometheusRule" && s.rules) || (kind != "PrometheusRule" && s.scrapes) {
			out = append(out, s)
		}
	}
	return out
}

func selectorField(kind string) string {
	switch kind {
	case "ServiceMonitor":
		return "serviceMonitorSelector"
	case "PodMonitor":
		return "podMonitorSelector"
	}
	return "ruleSelector"
}

// checkSelection reports a monitor or rule that no server picks up.
func (inv *inventory) checkSelection(r *result, m meta) {
	servers := inv.serversFor(m.kind)
	if len(servers) == 0 {
		r.skip(m, "picked up by Prometheus", "no Prometheus, PrometheusAgent or ThanosRuler in the inputs selects this kind")
		return
	}
	sel := inv.selectionOf(m)
	if len(sel.by) > 0 {
		r.checked++
		return
	}
	if len(sel.unknownBy) > 0 {
		r.skip(m, "picked up by Prometheus", fmt.Sprintf("%s selects it by labels, but its namespace selector reads namespace labels and Namespace %s isn't in the inputs", sel.unknownBy[0].ref(), m.ns))
		return
	}
	r.checked++
	field := selectorField(m.kind)
	nsField := strings.Replace(field, "Selector", "NamespaceSelector", 1)
	if len(sel.labelOK) > 0 {
		s := sel.labelOK[0]
		ns := map[string]*selector{"serviceMonitorSelector": s.smNs, "podMonitorSelector": s.pmNs, "ruleSelector": s.ruleNs}[field]
		var why string
		if ns == nil {
			why = fmt.Sprintf("%s has no %s, so it only looks in its own namespace, %s", s.ref(), nsField, s.ns)
		} else {
			why = fmt.Sprintf("%s's %s is %s, and namespace %s doesn't match it", s.ref(), nsField, ns, m.ns)
		}
		fix := fmt.Sprintf("set %s on %s to include namespace %s, for example {} for all namespaces, or move the %s to namespace %s", nsField, s.ref(), m.ns, m.kind, s.ns)
		if ns != nil {
			if missing := missingLabels(ns, map[string]string{}); missing != "" {
				fix = fmt.Sprintf("add %s to the labels of Namespace %s, or change %s on %s", missing, m.ns, nsField, s.ref())
			}
		}
		r.find(m, m.line, "US102",
			fmt.Sprintf("The labels match, but %s isn't in a namespace Prometheus watches. %s. The operator ignores this %s without an error.", m.ref(), why, m.kind),
			fix)
		return
	}
	var parts []string
	var fix string
	for _, s := range servers {
		ls := map[string]*selector{"serviceMonitorSelector": s.smSel, "podMonitorSelector": s.pmSel, "ruleSelector": s.ruleSel}[field]
		switch {
		case s.unmanaged:
			parts = append(parts, fmt.Sprintf("%s has no serviceMonitorSelector, podMonitorSelector, probeSelector or scrapeConfigSelector, so the operator doesn't manage its scrape config at all", s.ref()))
			if fix == "" {
				fix = fmt.Sprintf("set %s on %s, for example to {} to select every %s", field, s.ref(), m.kind)
			}
		case ls == nil:
			parts = append(parts, fmt.Sprintf("%s has no %s, which selects nothing", s.ref(), field))
			if fix == "" {
				fix = fmt.Sprintf("set %s on %s, for example to {} to select every %s", field, s.ref(), m.kind)
			}
		default:
			parts = append(parts, fmt.Sprintf("%s selects %s, and %s", s.ref(), ls, mismatch(ls, m.labels)))
			if fix == "" {
				if missing := missingLabels(ls, m.labels); missing != "" {
					fix = "add " + missing + " to metadata.labels"
				} else {
					fix = fmt.Sprintf("change metadata.labels so they match %s on %s", field, s.ref())
				}
			}
		}
	}
	r.find(m, m.line, "US101",
		fmt.Sprintf("No Prometheus picks up this %s, so the operator ignores it without an error. %s.", m.kind, strings.Join(parts, "; ")),
		fix)
}

// mismatch says which labels keep sel from matching labels.
func mismatch(sel *selector, labels map[string]string) string {
	var out []string
	for _, k := range sortedKeys(sel.matchLabels) {
		got, ok := labels[k]
		switch {
		case !ok:
			out = append(out, "this one has no "+k+" label")
		case got != sel.matchLabels[k]:
			out = append(out, fmt.Sprintf("this one has %s=%s", k, got))
		}
	}
	for _, e := range sel.exprs {
		one := &selector{exprs: []expr{e}}
		if one.matches(labels) {
			continue
		}
		if got, ok := labels[e.key]; ok {
			out = append(out, fmt.Sprintf("this one has %s=%s", e.key, got))
		} else {
			out = append(out, "this one has no "+e.key+" label")
		}
	}
	if len(out) == 0 {
		return "this one doesn't match"
	}
	return strings.Join(out, " and ")
}

// missingLabels lists the matchLabels in sel that labels lacks, as k: v.
func missingLabels(sel *selector, labels map[string]string) string {
	var out []string
	for _, k := range sortedKeys(sel.matchLabels) {
		if labels[k] != sel.matchLabels[k] {
			out = append(out, fmt.Sprintf("%s: %q", k, sel.matchLabels[k]))
		}
	}
	if len(out) == 0 || len(sel.exprs) > 0 {
		return ""
	}
	return strings.Join(out, ", ")
}

// targetNamespaces is where a monitor looks. all is true for any: true.
func (inv *inventory) targetNamespaces(mo *monitor) (names map[string]bool, all bool) {
	sel := inv.selectionOf(mo.meta)
	ignore := len(sel.by) > 0
	for _, s := range sel.by {
		ignore = ignore && s.ignoreNs
	}
	if ignore || (!mo.nsAny && len(mo.nsNames) == 0) {
		return map[string]bool{mo.ns: true}, false
	}
	if mo.nsAny {
		return nil, true
	}
	names = map[string]bool{}
	for _, n := range mo.nsNames {
		names[n] = true
	}
	return names, false
}

func nsList(names map[string]bool, all bool) string {
	if all {
		return "any namespace"
	}
	ks := sortedKeys(names)
	if len(ks) == 1 {
		return "namespace " + ks[0]
	}
	return "namespaces " + strings.Join(ks, ", ")
}

// podsBehind returns the workloads a Service's selector matches.
func (inv *inventory) podsBehind(s *service) []*podTemplate {
	if s.selector == nil {
		return nil
	}
	var out []*podTemplate
	for _, p := range inv.pods {
		if p.ns == s.ns && subset(s.selector, p.podLabels) {
			out = append(out, p)
		}
	}
	return out
}

func subset(sel, labels map[string]string) bool {
	for k, v := range sel {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

func declares(pods []*podTemplate, target string, isName bool) bool {
	for _, p := range pods {
		for _, cp := range p.ports {
			if isName && cp.name == target {
				return true
			}
			if !isName && strconv.Itoa(cp.number) == target {
				return true
			}
		}
	}
	return false
}

func podNames(pods []*podTemplate) string {
	var out []string
	for _, p := range pods {
		out = append(out, p.ref())
	}
	return strings.Join(out, ", ")
}

func portNames(pods []*podTemplate) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range pods {
		for _, cp := range p.ports {
			s := strconv.Itoa(cp.number)
			if cp.name != "" {
				s = cp.name + " (" + s + ")"
			}
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		return "no ports"
	}
	return strings.Join(out, ", ")
}

func svcPortNames(svcs []*service) string {
	var out []string
	seen := map[string]bool{}
	for _, s := range svcs {
		for _, p := range s.ports {
			n := p.name
			if n == "" {
				n = "(unnamed " + strconv.Itoa(p.port) + ")"
			}
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	if len(out) == 0 {
		return "no ports"
	}
	return strings.Join(out, ", ")
}

func refs[T interface{ ref() string }](list []T) string {
	var out []string
	for _, x := range list {
		out = append(out, x.ref())
	}
	sort.Strings(out)
	if len(out) > 3 {
		return strings.Join(out[:3], ", ") + fmt.Sprintf(" and %d more", len(out)-3)
	}
	return strings.Join(out, ", ")
}

// checkServiceMonitor follows a ServiceMonitor to its Services and their pods.
func (inv *inventory) checkServiceMonitor(r *result, mo *monitor) {
	names, all := inv.targetNamespaces(mo)
	in := func(ns string) bool { return all || names[ns] }
	var matched, elsewhere, bySelector []*service
	for _, s := range inv.services {
		switch {
		case mo.sel.matches(s.labels) && in(s.ns):
			matched = append(matched, s)
		case mo.sel.matches(s.labels):
			elsewhere = append(elsewhere, s)
		case in(s.ns) && s.selector != nil && mo.sel.matches(s.selector):
			bySelector = append(bySelector, s)
		}
	}
	if len(matched) == 0 {
		switch {
		case len(elsewhere) > 0:
			r.checked++
			r.find(mo.meta, mo.line, "US201",
				fmt.Sprintf("The selector %s matches %s, but this ServiceMonitor only looks in %s, so it finds no targets.", mo.sel, refs(elsewhere), nsList(names, all)),
				fmt.Sprintf("set spec.namespaceSelector.matchNames to [%s], or move the ServiceMonitor to that namespace", elsewhere[0].ns))
		case len(bySelector) > 0:
			r.checked++
			r.find(mo.meta, mo.line, "US202",
				fmt.Sprintf("The selector %s matches the pod selector of %s, but a ServiceMonitor selects Services by the Service's own metadata.labels, which are %s. It finds no targets.", mo.sel, refs(bySelector), labelString(bySelector[0].labels)),
				"change spec.selector to match the Service's metadata.labels, or add those labels to the Service")
		default:
			r.skip(mo.meta, "selects a Service", fmt.Sprintf("no Service in the inputs in %s has labels matching %s", nsList(names, all), mo.sel))
		}
		return
	}
	r.checked++
	for _, ep := range mo.endpoints {
		switch {
		case ep.port != "":
			var with []*service
			for _, s := range matched {
				for _, p := range s.ports {
					if p.name == ep.port {
						with = append(with, s)
					}
				}
			}
			r.checked++
			if len(with) == 0 {
				r.find(mo.meta, ep.line, "US301",
					fmt.Sprintf("Endpoint port %q isn't the name of a port on %s, which has %s. port means the Service port's name, so Prometheus finds no target for this endpoint.", ep.port, refs(matched), svcPortNames(matched)),
					"set port to one of the Service's port names, or name the Service port "+strconv.Quote(ep.port))
				continue
			}
			for _, s := range with {
				for _, p := range s.ports {
					if p.name != ep.port || !p.targetIsName {
						continue
					}
					pods := inv.podsBehind(s)
					if len(pods) == 0 {
						r.skip(mo.meta, "Service targetPort "+p.target+" exists on pods", fmt.Sprintf("no workload in the inputs matches the selector of %s", s.ref()))
						continue
					}
					r.checked++
					if !declares(pods, p.target, true) {
						r.find(mo.meta, ep.line, "US302",
							fmt.Sprintf("Port %q of %s has targetPort %q, but no container in %s declares a port with that name (they have %s). The Service gets no endpoint address for it, so Prometheus has nothing to scrape.", ep.port, s.ref(), p.target, podNames(pods), portNames(pods)),
							fmt.Sprintf("set targetPort on %s to the container port's name or number", s.ref()))
					}
				}
			}
		case ep.target != "":
			var pods []*podTemplate
			for _, s := range matched {
				pods = append(pods, inv.podsBehind(s)...)
			}
			if len(pods) == 0 {
				r.skip(mo.meta, "targetPort "+ep.target+" exists on pods", "no workload in the inputs matches the selector of "+refs(matched))
				continue
			}
			r.checked++
			if !declares(pods, ep.target, ep.targetIsName) {
				r.find(mo.meta, ep.line, "US304",
					fmt.Sprintf("Endpoint targetPort %q isn't a port declared by the pods behind %s (%s, with %s), so Prometheus drops every target for this endpoint.", ep.target, refs(matched), podNames(pods), portNames(pods)),
					"set targetPort to a declared container port, or better, use port with the Service port's name")
			}
		}
	}
	if len(mo.endpoints) == 0 {
		r.find(mo.meta, mo.line, "US402", "spec.endpoints is empty, so this ServiceMonitor scrapes nothing.", "add an endpoint with the Service port's name")
	}
}

// checkPodMonitor follows a PodMonitor to its pods.
func (inv *inventory) checkPodMonitor(r *result, mo *monitor) {
	names, all := inv.targetNamespaces(mo)
	in := func(ns string) bool { return all || names[ns] }
	var matched, elsewhere []*podTemplate
	var svcs []*service
	for _, p := range inv.pods {
		if mo.sel.matches(p.podLabels) {
			if in(p.ns) {
				matched = append(matched, p)
			} else {
				elsewhere = append(elsewhere, p)
			}
		}
	}
	for _, s := range inv.services {
		if in(s.ns) && mo.sel.matches(s.labels) {
			svcs = append(svcs, s)
		}
	}
	if len(matched) == 0 {
		switch {
		case len(elsewhere) > 0:
			r.checked++
			r.find(mo.meta, mo.line, "US204",
				fmt.Sprintf("The selector %s matches pods of %s, but this PodMonitor only looks in %s, so it finds no targets.", mo.sel, refs(elsewhere), nsList(names, all)),
				fmt.Sprintf("set spec.namespaceSelector.matchNames to [%s], or move the PodMonitor to that namespace", elsewhere[0].ns))
		case len(svcs) > 0:
			r.checked++
			r.find(mo.meta, mo.line, "US203",
				fmt.Sprintf("The selector %s matches the labels of %s, but a PodMonitor selects pods by their own labels, and no pod template in the inputs has them. It finds no targets.", mo.sel, refs(svcs)),
				"change spec.selector to the pod template's labels, or use a ServiceMonitor for the Service")
		default:
			r.skip(mo.meta, "selects pods", fmt.Sprintf("no pod template in the inputs in %s has labels matching %s", nsList(names, all), mo.sel))
		}
		return
	}
	r.checked++
	for _, ep := range mo.endpoints {
		var want string
		var isName bool
		switch {
		case ep.port != "":
			want, isName = ep.port, true
		case ep.portNumber != 0:
			want = strconv.Itoa(ep.portNumber)
		case ep.target != "":
			want, isName = ep.target, ep.targetIsName
		}
		if want != "" {
			r.checked++
			if !declares(matched, want, isName) {
				what := "port"
				if !isName {
					what = "port number"
				}
				r.find(mo.meta, ep.line, "US303",
					fmt.Sprintf("Endpoint %s %q isn't declared by any container of %s, which declare %s. Prometheus only scrapes declared container ports, so it finds no target for this endpoint.", what, want, refs(matched), portNames(matched)),
					"use the name of a declared container port, or add the port to the container's ports")
			}
		}
	}
	if len(mo.endpoints) == 0 {
		r.find(mo.meta, mo.line, "US402", "spec.podMetricsEndpoints is empty, so this PodMonitor scrapes nothing.", "add an endpoint with the container port's name")
	}
}

var durationRe = regexp.MustCompile(`^(([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?$`)

// parseDuration reads a Prometheus duration, in milliseconds.
func parseDuration(s string) (int64, bool) {
	if s == "0" {
		return 0, true
	}
	m := durationRe.FindStringSubmatch(s)
	if s == "" || m == nil {
		return 0, false
	}
	units := []int64{365 * 24 * 3600 * 1000, 7 * 24 * 3600 * 1000, 24 * 3600 * 1000, 3600 * 1000, 60 * 1000, 1000, 1}
	var total int64
	for i, u := range units {
		if v := m[2+2*i]; v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, false
			}
			total += n * u
		}
	}
	return total, true
}

func (inv *inventory) checkTimeout(r *result, mo *monitor, ep endpoint) {
	if ep.interval == "" || ep.timeout == "" {
		return
	}
	iv, ok1 := parseDuration(ep.interval)
	to, ok2 := parseDuration(ep.timeout)
	if !ok1 || !ok2 {
		return
	}
	r.checked++
	if to > iv {
		r.find(mo.meta, ep.line, "US401",
			fmt.Sprintf("scrapeTimeout %s is longer than interval %s. The operator rejects a %s like this and leaves it out of the Prometheus config.", ep.timeout, ep.interval, mo.kind),
			"make scrapeTimeout no longer than interval")
	}
}

// check runs every check over the inventory.
func check(inv *inventory) *result {
	r := &result{}
	for _, mo := range inv.monitors {
		inv.checkSelection(r, mo.meta)
		for _, ep := range mo.endpoints {
			inv.checkTimeout(r, mo, ep)
		}
		if mo.kind == "ServiceMonitor" {
			inv.checkServiceMonitor(r, mo)
		} else {
			inv.checkPodMonitor(r, mo)
		}
	}
	for _, m := range inv.rules {
		inv.checkSelection(r, *m)
	}
	sort.SliceStable(r.findings, func(i, j int) bool {
		a, b := r.findings[i], r.findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.objLine != b.objLine {
			return a.objLine < b.objLine
		}
		return a.Line < b.Line
	})
	return r
}
