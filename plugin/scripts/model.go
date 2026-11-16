package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// meta is what every object has.
type meta struct {
	file   string
	line   int
	kind   string
	ns     string
	name   string
	labels map[string]string
}

func (m meta) ref() string {
	if m.ns == "" {
		return m.kind + " " + m.name
	}
	return m.kind + " " + m.ns + "/" + m.name
}

func (m meta) at() string { return fmt.Sprintf("%s:%d", m.file, m.line) }

// cport is a port a container declares.
type cport struct {
	name   string
	number int
}

// podTemplate is the pods a workload creates.
type podTemplate struct {
	meta
	podLabels map[string]string
	ports     []cport
}

type svcPort struct {
	name         string
	port         int
	target       string // name or number, as written; the port number when unset
	targetIsName bool
}

type service struct {
	meta
	selector map[string]string // nil when the Service has no selector
	ports    []svcPort
	external bool
}

type endpoint struct {
	line         int
	port         string
	target       string
	targetIsName bool
	portNumber   int
	interval     string
	timeout      string
}

// monitor is a ServiceMonitor or a PodMonitor.
type monitor struct {
	meta
	sel       *selector
	nsAny     bool
	nsNames   []string
	endpoints []endpoint
}

// server is a Prometheus, PrometheusAgent or ThanosRuler: something that
// selects monitors or rules.
type server struct {
	meta
	smSel, smNs     *selector
	pmSel, pmNs     *selector
	ruleSel, ruleNs *selector
	scrapes         bool // has ServiceMonitor and PodMonitor selectors
	rules           bool // has rule selectors
	unmanaged       bool // all scrape selectors are null
	ignoreNs        bool
}

type inventory struct {
	files      int
	objects    int
	pods       []*podTemplate
	services   []*service
	namespaces map[string]map[string]string
	monitors   []*monitor
	rules      []*meta
	servers    []*server
	skipped    []string
	defaultNs  string
}

var workloadKinds = map[string]bool{
	"Deployment": true, "StatefulSet": true, "DaemonSet": true, "ReplicaSet": true,
	"ReplicationController": true, "Job": true, "CronJob": true, "Pod": true, "PodTemplate": true,
}

var monitoringKinds = map[string]bool{
	"ServiceMonitor": true, "PodMonitor": true, "PrometheusRule": true,
	"Prometheus": true, "PrometheusAgent": true, "ThanosRuler": true,
}

// clusterKinds are kinds that have no namespace. Only the ones that matter here.
var clusterKinds = map[string]bool{
	"Namespace": true, "ClusterRole": true, "ClusterRoleBinding": true,
	"CustomResourceDefinition": true, "MutatingWebhookConfiguration": true,
	"ValidatingWebhookConfiguration": true, "PriorityClass": true, "StorageClass": true,
	"IngressClass": true, "PersistentVolume": true, "APIService": true,
}

// load parses every source into an inventory.
func load(srcs []source, defaultNs string) (*inventory, error) {
	inv := &inventory{namespaces: map[string]map[string]string{}, defaultNs: defaultNs}
	seen := map[string]string{}
	for _, s := range srcs {
		if err := checkBytes(s.name, s.data); err != nil {
			return nil, err
		}
		if isHelmTemplate(s.data) {
			if s.explicit {
				return nil, untrusted{s.name, "this is an unrendered Helm template. Run helm template and pass its output"}
			}
			inv.skipped = append(inv.skipped, s.name+": Helm template")
			continue
		}
		docs, err := yamlDocs(s.name, s.data)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			if s.explicit {
				return nil, untrusted{s.name, "the file has only comments, no objects"}
			}
			inv.skipped = append(inv.skipped, s.name+": comments only")
			continue
		}
		if !anyKind(docs) {
			if s.explicit || !allMappings(docs) {
				return nil, untrusted{s.name, "this isn't Kubernetes manifests: no document has apiVersion and kind (" + describe(docs[0]) + ")"}
			}
			inv.skipped = append(inv.skipped, s.name+": not Kubernetes manifests")
			continue
		}
		if key := danglingKey(docs[len(docs)-1]); key != "" {
			return nil, untrusted{s.name, fmt.Sprintf("the last object ends with %q and no value, so the file looks cut off", key+":")}
		}
		inv.files++
		for _, d := range docs {
			objs, err := expandList(s.name, d)
			if err != nil {
				return nil, err
			}
			for _, o := range objs {
				m, err := readMeta(s.name, o, defaultNs)
				if err != nil {
					return nil, err
				}
				key := m.kind + "/" + m.ns + "/" + m.name
				if prev, ok := seen[key]; ok {
					return nil, untrusted{m.at(), fmt.Sprintf("%s appears twice (first at %s). Pass each object once, so it's clear which version to check", m.ref(), prev)}
				}
				seen[key] = m.at()
				inv.objects++
				if err := inv.add(m, o); err != nil {
					return nil, err
				}
			}
		}
	}
	return inv, nil
}

func anyKind(docs []*Node) bool {
	for _, d := range docs {
		if str(d, "kind") != "" && str(d, "apiVersion") != "" {
			return true
		}
	}
	return false
}

func allMappings(docs []*Node) bool {
	for _, d := range docs {
		if d.Kind != MappingNode {
			return false
		}
	}
	return true
}

func describe(d *Node) string {
	switch d.Kind {
	case MappingNode:
		if len(d.Content) > 0 {
			return "it starts with the key " + strconv.Quote(clip(d.Content[0].Value, 60))
		}
		return "it's an empty mapping"
	case SequenceNode:
		return "it's a list"
	default:
		return "it's plain text: " + strconv.Quote(clip(strings.TrimSpace(d.Value), 80))
	}
}

// expandList returns the objects in d, unwrapping kind: List.
func expandList(file string, d *Node) ([]*Node, error) {
	at := fmt.Sprintf("%s:%d", file, d.Line)
	if d.Kind != MappingNode {
		return nil, untrusted{at, "a document isn't a Kubernetes object (" + describe(d) + ")"}
	}
	kind := str(d, "kind")
	if kind == "Status" {
		return nil, untrusted{at, "this is a Kubernetes API error, not manifests: " + clip(str(d, "message"), 160)}
	}
	if kind == "List" || (strings.HasSuffix(kind, "List") && mapGet(d, "items") != nil) {
		items := mapGet(d, "items")
		if isNull(items) || items.Kind != SequenceNode || len(items.Content) == 0 {
			return nil, untrusted{at, "the List has no items. If this came from kubectl get, nothing matched"}
		}
		for _, it := range items.Content {
			if it.Kind != MappingNode {
				return nil, untrusted{fmt.Sprintf("%s:%d", file, it.Line), "a List item isn't an object"}
			}
		}
		return items.Content, nil
	}
	return []*Node{d}, nil
}

// fieldName is what a top-level Kubernetes field looks like.
var fieldName = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

var errorKeys = map[string]bool{"error": true, "errors": true, "fatal": true, "panic": true, "warning": true}

func readMeta(file string, o *Node, defaultNs string) (meta, error) {
	m := meta{file: file, line: o.Line, kind: str(o, "kind"), name: str(o, "metadata", "name")}
	for i := 0; i+1 < len(o.Content); i += 2 {
		k := o.Content[i]
		if !fieldName.MatchString(k.Value) || errorKeys[k.Value] {
			return m, untrusted{fmt.Sprintf("%s:%d", file, k.Line), fmt.Sprintf("%q isn't a Kubernetes field. This looks like command output mixed into the manifests", clip(k.Value+": "+o.Content[i+1].Value, 120))}
		}
	}
	switch {
	case str(o, "apiVersion") == "":
		return m, untrusted{m.at(), "an object has no apiVersion. The file may be cut off or not be a manifest"}
	case m.kind == "":
		return m, untrusted{m.at(), "an object has no kind. The file may be cut off or not be a manifest"}
	case m.name == "":
		return m, untrusted{m.at(), m.kind + " has no metadata.name. The file may be cut off, or it may use generateName, which isn't supported"}
	}
	if !clusterKinds[m.kind] {
		m.ns = str(o, "metadata", "namespace")
		if m.ns == "" {
			m.ns = defaultNs
		}
	}
	labels, err := stringMap(file, path(o, "metadata", "labels"), "metadata.labels")
	if err != nil {
		return m, err
	}
	m.labels = labels
	return m, nil
}

func stringMap(file string, n *Node, what string) (map[string]string, error) {
	out := map[string]string{}
	if isNull(n) {
		return out, nil
	}
	if n.Kind != MappingNode {
		return nil, untrusted{fmt.Sprintf("%s:%d", file, n.Line), what + " isn't a mapping"}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		v, ok := scalar(n.Content[i+1])
		if !ok {
			return nil, untrusted{fmt.Sprintf("%s:%d", file, n.Content[i].Line), what + "." + n.Content[i].Value + " isn't a string"}
		}
		out[n.Content[i].Value] = v
	}
	return out, nil
}

func isMonitoringGroup(o *Node) bool {
	return strings.HasPrefix(str(o, "apiVersion"), "monitoring.coreos.com/")
}

func (inv *inventory) add(m meta, o *Node) error {
	bad := func(n *Node, why string) error {
		line := m.line
		if n != nil {
			line = n.Line
		}
		return untrusted{fmt.Sprintf("%s:%d", m.file, line), m.ref() + " " + why + ". The file may be cut off"}
	}
	spec := mapGet(o, "spec")
	switch {
	case m.kind == "Namespace":
		inv.namespaces[m.name] = m.labels
	case m.kind == "Service":
		if isNull(spec) || spec.Kind != MappingNode {
			return bad(spec, "has no spec")
		}
		s := &service{meta: m, external: str(spec, "type") == "ExternalName"}
		if sel := mapGet(spec, "selector"); !isNull(sel) {
			sm, err := stringMap(m.file, sel, "spec.selector")
			if err != nil {
				return err
			}
			if len(sm) > 0 {
				s.selector = sm
			}
		}
		ports := mapGet(spec, "ports")
		if !isNull(ports) {
			if ports.Kind != SequenceNode {
				return bad(ports, "has spec.ports that isn't a list")
			}
			for _, p := range ports.Content {
				num, err := strconv.Atoi(str(p, "port"))
				if err != nil {
					return bad(p, "has a port with no port number")
				}
				sp := svcPort{name: str(p, "name"), port: num, target: strconv.Itoa(num)}
				if tp := mapGet(p, "targetPort"); !isNull(tp) {
					sp.target = tp.Value
					sp.targetIsName = tp.Tag != "!!int"
				}
				s.ports = append(s.ports, sp)
			}
		} else if !s.external && str(spec, "clusterIP") != "None" {
			return bad(spec, "has no spec.ports, which only a headless or ExternalName Service may leave out")
		}
		inv.services = append(inv.services, s)
	case workloadKinds[m.kind] || !isNull(path(spec, "template", "spec", "containers")):
		pt, err := podTemplateOf(m, o)
		if err != nil {
			return err
		}
		if pt == nil {
			return bad(spec, "has no pod template with containers")
		}
		inv.pods = append(inv.pods, pt)
	case !isMonitoringGroup(o):
		if monitoringKinds[m.kind] {
			return untrusted{m.at(), fmt.Sprintf("%s has apiVersion %q. Prometheus Operator's kinds are in monitoring.coreos.com, so this one would be rejected or belongs to another operator", m.ref(), str(o, "apiVersion"))}
		}
		return nil
	case m.kind == "ServiceMonitor" || m.kind == "PodMonitor":
		if isNull(spec) || spec.Kind != MappingNode {
			return bad(spec, "has no spec")
		}
		selNode := mapGet(spec, "selector")
		if selNode == nil {
			return bad(spec, "has no spec.selector")
		}
		sel, err := parseSelector(m.file, selNode)
		if err != nil {
			return err
		}
		if sel == nil {
			sel = &selector{matchLabels: map[string]string{}}
		}
		mo := &monitor{meta: m, sel: sel}
		if nsn := mapGet(spec, "namespaceSelector"); !isNull(nsn) {
			mo.nsAny = str(nsn, "any") == "true"
			if mn := mapGet(nsn, "matchNames"); !isNull(mn) {
				if mn.Kind != SequenceNode {
					return bad(mn, "has namespaceSelector.matchNames that isn't a list")
				}
				for _, v := range mn.Content {
					mo.nsNames = append(mo.nsNames, v.Value)
				}
			}
		}
		key := "endpoints"
		if m.kind == "PodMonitor" {
			key = "podMetricsEndpoints"
		}
		eps := mapGet(spec, key)
		if eps == nil {
			return bad(spec, "has no spec."+key)
		}
		if !isNull(eps) {
			if eps.Kind != SequenceNode {
				return bad(eps, "has spec."+key+" that isn't a list")
			}
			for _, e := range eps.Content {
				if e.Kind != MappingNode {
					return bad(e, "has an endpoint that isn't a mapping")
				}
				ep := endpoint{line: e.Line, port: str(e, "port"), interval: str(e, "interval"), timeout: str(e, "scrapeTimeout")}
				if tp := mapGet(e, "targetPort"); !isNull(tp) {
					ep.target = tp.Value
					ep.targetIsName = tp.Tag != "!!int"
				}
				if pn := str(e, "portNumber"); pn != "" {
					n, err := strconv.Atoi(pn)
					if err != nil {
						return bad(e, "has a portNumber that isn't a number")
					}
					ep.portNumber = n
				}
				mo.endpoints = append(mo.endpoints, ep)
			}
		}
		inv.monitors = append(inv.monitors, mo)
	case m.kind == "PrometheusRule":
		groups := mapGet(spec, "groups")
		if isNull(groups) || groups.Kind != SequenceNode {
			return bad(spec, "has no spec.groups list")
		}
		for _, g := range groups.Content {
			rules := mapGet(g, "rules")
			if str(g, "name") == "" || isNull(rules) || rules.Kind != SequenceNode {
				return bad(g, "has a rule group with no name or no rules list")
			}
			for _, rl := range rules.Content {
				if isNull(mapGet(rl, "expr")) || (str(rl, "alert") == "" && str(rl, "record") == "") {
					return bad(rl, "has a rule with no expr, or with neither alert nor record")
				}
			}
		}
		mm := m
		inv.rules = append(inv.rules, &mm)
	case m.kind == "Prometheus" || m.kind == "PrometheusAgent" || m.kind == "ThanosRuler":
		if isNull(spec) || spec.Kind != MappingNode {
			return bad(spec, "has no spec")
		}
		s := &server{meta: m, ignoreNs: str(spec, "ignoreNamespaceSelectors") == "true"}
		sels := []struct {
			key string
			dst **selector
		}{
			{"serviceMonitorSelector", &s.smSel}, {"serviceMonitorNamespaceSelector", &s.smNs},
			{"podMonitorSelector", &s.pmSel}, {"podMonitorNamespaceSelector", &s.pmNs},
			{"ruleSelector", &s.ruleSel}, {"ruleNamespaceSelector", &s.ruleNs},
		}
		for _, x := range sels {
			sel, err := parseSelector(m.file, mapGet(spec, x.key))
			if err != nil {
				return err
			}
			*x.dst = sel
		}
		s.scrapes = m.kind != "ThanosRuler"
		s.rules = m.kind != "PrometheusAgent"
		if s.scrapes {
			s.unmanaged = s.smSel == nil && s.pmSel == nil &&
				isNull(mapGet(spec, "probeSelector")) && isNull(mapGet(spec, "scrapeConfigSelector"))
		}
		inv.servers = append(inv.servers, s)
	}
	return nil
}

// podTemplateOf finds the pod labels and container ports of a workload.
func podTemplateOf(m meta, o *Node) (*podTemplate, error) {
	var tmpl *Node
	switch m.kind {
	case "Pod":
		tmpl = o
	case "CronJob":
		tmpl = path(o, "spec", "jobTemplate", "spec", "template")
	case "PodTemplate":
		tmpl = mapGet(o, "template")
	default:
		tmpl = path(o, "spec", "template")
	}
	podSpec := mapGet(tmpl, "spec")
	cs := mapGet(podSpec, "containers")
	if isNull(cs) || cs.Kind != SequenceNode || len(cs.Content) == 0 {
		return nil, nil
	}
	labels, err := stringMap(m.file, path(tmpl, "metadata", "labels"), "pod labels")
	if err != nil {
		return nil, err
	}
	pt := &podTemplate{meta: m, podLabels: labels}
	for _, key := range []string{"initContainers", "containers"} {
		list := mapGet(podSpec, key)
		if isNull(list) {
			continue
		}
		if list.Kind != SequenceNode {
			return nil, untrusted{fmt.Sprintf("%s:%d", m.file, list.Line), m.ref() + " has " + key + " that isn't a list"}
		}
		for _, c := range list.Content {
			if str(c, "name") == "" || str(c, "image") == "" {
				return nil, untrusted{fmt.Sprintf("%s:%d", m.file, c.Line), m.ref() + " has a container with no name or image. The file may be cut off"}
			}
			ports := mapGet(c, "ports")
			if isNull(ports) {
				continue
			}
			if ports.Kind != SequenceNode {
				return nil, untrusted{fmt.Sprintf("%s:%d", m.file, ports.Line), m.ref() + " has container ports that aren't a list"}
			}
			for _, p := range ports.Content {
				n, err := strconv.Atoi(str(p, "containerPort"))
				if err != nil {
					return nil, untrusted{fmt.Sprintf("%s:%d", m.file, p.Line), m.ref() + " has a container port with no containerPort. The file may be cut off"}
				}
				pt.ports = append(pt.ports, cport{name: str(p, "name"), number: n})
			}
		}
	}
	return pt, nil
}

// danglingKey returns the last key of a document when it has no value at all,
// as in a file that ends with "  labels:". Writing null or ~ doesn't count.
func danglingKey(d *Node) string {
	n := d
	key := ""
	for {
		switch n.Kind {
		case MappingNode:
			if len(n.Content) < 2 {
				return ""
			}
			key = n.Content[len(n.Content)-2].Value
			n = n.Content[len(n.Content)-1]
		case SequenceNode:
			if len(n.Content) == 0 {
				return ""
			}
			key = ""
			n = n.Content[len(n.Content)-1]
		case ScalarNode:
			if key != "" && n.Tag == "!!null" && n.Value == "" && n.Style == 0 {
				return key
			}
			return ""
		default:
			return ""
		}
	}
}
