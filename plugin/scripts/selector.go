package main

import (
	"fmt"
	"sort"
	"strings"
)

// selector is a Kubernetes metav1.LabelSelector. A nil *selector is a null
// selector, which Prometheus Operator reads as "matches nothing" for object
// selectors and "the same namespace only" for namespace selectors.
type selector struct {
	matchLabels map[string]string
	exprs       []expr
}

type expr struct {
	key, op string
	values  []string
}

// parseSelector reads a LabelSelector node. It returns nil, nil for a missing
// or null node.
func parseSelector(file string, n *Node) (*selector, error) {
	if isNull(n) {
		return nil, nil
	}
	bad := func(why string) error {
		return untrusted{fmt.Sprintf("%s:%d", file, n.Line), "invalid label selector: " + why}
	}
	if n.Kind != MappingNode {
		return nil, bad("it isn't a mapping")
	}
	s := &selector{matchLabels: map[string]string{}}
	if ml := mapGet(n, "matchLabels"); !isNull(ml) {
		if ml.Kind != MappingNode {
			return nil, bad("matchLabels isn't a mapping")
		}
		for i := 0; i+1 < len(ml.Content); i += 2 {
			v, ok := scalar(ml.Content[i+1])
			if !ok {
				return nil, bad("matchLabels." + ml.Content[i].Value + " isn't a string")
			}
			s.matchLabels[ml.Content[i].Value] = v
		}
	}
	if me := mapGet(n, "matchExpressions"); !isNull(me) {
		if me.Kind != SequenceNode {
			return nil, bad("matchExpressions isn't a list")
		}
		for _, e := range me.Content {
			x := expr{key: str(e, "key"), op: str(e, "operator")}
			if x.key == "" {
				return nil, bad("a matchExpressions entry has no key")
			}
			if vs := mapGet(e, "values"); !isNull(vs) {
				if vs.Kind != SequenceNode {
					return nil, bad("values of " + x.key + " isn't a list")
				}
				for _, v := range vs.Content {
					sv, ok := scalar(v)
					if !ok {
						return nil, bad("a value of " + x.key + " isn't a string")
					}
					x.values = append(x.values, sv)
				}
			}
			switch x.op {
			case "In", "NotIn":
				if len(x.values) == 0 {
					return nil, bad(x.op + " for " + x.key + " has no values")
				}
			case "Exists", "DoesNotExist":
				if len(x.values) > 0 {
					return nil, bad(x.op + " for " + x.key + " must not have values")
				}
			default:
				return nil, bad(fmt.Sprintf("unknown operator %q for %s", x.op, x.key))
			}
			s.exprs = append(s.exprs, x)
		}
	}
	return s, nil
}

// empty reports whether s matches everything.
func (s *selector) empty() bool { return s != nil && len(s.matchLabels) == 0 && len(s.exprs) == 0 }

// matches reports whether labels satisfy s. A nil selector matches nothing.
func (s *selector) matches(labels map[string]string) bool {
	if s == nil {
		return false
	}
	for k, v := range s.matchLabels {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	for _, e := range s.exprs {
		got, ok := labels[e.key]
		switch e.op {
		case "In":
			if !ok || !contains(e.values, got) {
				return false
			}
		case "NotIn":
			if ok && contains(e.values, got) {
				return false
			}
		case "Exists":
			if !ok {
				return false
			}
		case "DoesNotExist":
			if ok {
				return false
			}
		}
	}
	return true
}

// keys lists every label key s reads.
func (s *selector) keys() []string {
	if s == nil {
		return nil
	}
	var ks []string
	for k := range s.matchLabels {
		ks = append(ks, k)
	}
	for _, e := range s.exprs {
		ks = append(ks, e.key)
	}
	return ks
}

// String writes s the way kubectl's -l flag does.
func (s *selector) String() string {
	if s == nil {
		return "(null: matches nothing)"
	}
	var parts []string
	for _, k := range sortedKeys(s.matchLabels) {
		parts = append(parts, k+"="+s.matchLabels[k])
	}
	for _, e := range s.exprs {
		switch e.op {
		case "In":
			parts = append(parts, e.key+" in ("+strings.Join(e.values, ",")+")")
		case "NotIn":
			parts = append(parts, e.key+" notin ("+strings.Join(e.values, ",")+")")
		case "Exists":
			parts = append(parts, e.key)
		case "DoesNotExist":
			parts = append(parts, "!"+e.key)
		}
	}
	if len(parts) == 0 {
		return "(empty: matches everything)"
	}
	return strings.Join(parts, ", ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// labelString writes a label map as k=v, k=v.
func labelString(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ", ")
}
