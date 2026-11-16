package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// untrusted is an input problem that makes the whole run untrustworthy.
// The program exits 2 and prints nothing on stdout when any input has one.
type untrusted struct {
	file   string
	reason string
}

func (u untrusted) Error() string { return u.file + ": " + u.reason }

// source is one file's content, with how it was named on the command line.
type source struct {
	name     string // path as given, or "<stdin>"
	data     []byte
	explicit bool // named on the command line, or stdin
}

// readArgs reads every file named on the command line, walking directories
// for .yaml, .yml and .json files.
func readArgs(args []string, stdin io.Reader) ([]source, error) {
	var out []source
	usedStdin := false
	for _, a := range args {
		if a == "-" {
			if usedStdin {
				return nil, untrusted{"<stdin>", "stdin is named twice"}
			}
			usedStdin = true
			data, err := io.ReadAll(stdin)
			if err != nil {
				return nil, untrusted{"<stdin>", "can't read: " + err.Error()}
			}
			out = append(out, source{name: "<stdin>", data: data, explicit: true})
			continue
		}
		info, err := os.Stat(a)
		if err != nil {
			return nil, untrusted{a, "can't read: " + plainErr(err)}
		}
		if !info.IsDir() {
			data, err := os.ReadFile(a)
			if err != nil {
				return nil, untrusted{a, "can't read: " + plainErr(err)}
			}
			out = append(out, source{name: a, data: data, explicit: true})
			continue
		}
		var found []string
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return untrusted{p, "can't read: " + plainErr(err)}
			}
			name := d.Name()
			if d.IsDir() {
				if p != a && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() && isYAMLName(name) {
				found = append(found, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, untrusted{a, "the directory has no .yaml, .yml or .json files"}
		}
		sort.Strings(found)
		for _, p := range found {
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, untrusted{p, "can't read: " + plainErr(err)}
			}
			out = append(out, source{name: p, data: data})
		}
	}
	return out, nil
}

func plainErr(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func isYAMLName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// failedOutput matches the first line of error output from helm, kubectl,
// kustomize, go, curl, shells and web servers.
var failedOutput = regexp.MustCompile(`^([45]\d\d:? |--- FAIL|=== RUN|ok  \t|Error|ERROR|error|fatal|FATAL|panic|FAIL|Usage:|usage:|WARNING:|Warning:|W\d{4} |E\d{4} |\S+\.go:\d+|The connection to the server|No resources found|Unable to connect|<!DOCTYPE|<html|<\?xml|curl: |helm: |kubectl: |bash: |sh: |zsh: |Killed|Terminated|Segmentation fault)`)

// checkBytes rejects content that can't be a complete text manifest.
func checkBytes(name string, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return untrusted{name, "the file is empty"}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return untrusted{name, "this is binary data, not a manifest"}
	}
	if first := firstLine(data); failedOutput.MatchString(first) {
		return untrusted{name, fmt.Sprintf("this looks like the output of a failed command, not manifests: %q", clip(first, 160))}
	}
	if data[len(data)-1] != '\n' && data[len(data)-1] != '}' {
		return untrusted{name, "the file doesn't end with a line break, so it may be cut off. If it's complete, add a final newline"}
	}
	return nil
}

// firstLine is the first line that isn't blank, a comment or a document marker.
func firstLine(data []byte) string {
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") && l != "---" {
			return l
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// helmAction matches Helm template actions that never survive helm template.
// Rendered PrometheusRules keep alert templates such as {{ $labels.job }}, so
// those don't count.
var helmAction = regexp.MustCompile(`\{\{-?\s*(\.Values|\.Release|\.Chart|\.Capabilities|\.Files|include\s|toYaml|tpl\s|define\s)`)

func isHelmTemplate(data []byte) bool {
	return helmAction.Match(data)
}

// yamlDocs parses every document in data. Empty documents are dropped.
func yamlDocs(name string, data []byte) ([]*Node, error) {
	dec := NewDecoder(bytes.NewReader(data))
	var docs []*Node
	for {
		var n Node
		err := dec.Decode(&n)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, untrusted{name, "not valid YAML or JSON: " + strings.TrimPrefix(err.Error(), "yaml: ")}
		}
		if n.Kind == DocumentNode && len(n.Content) == 1 {
			c := n.Content[0]
			if c.Kind == ScalarNode && c.Tag == "!!null" {
				continue
			}
			docs = append(docs, c)
		}
	}
	return docs, nil
}

// mapGet returns the value node for key in a mapping node, or nil.
func mapGet(n *Node, key string) *Node {
	if n == nil || n.Kind != MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// isNull reports whether n is absent or an explicit null.
func isNull(n *Node) bool {
	return n == nil || (n.Kind == ScalarNode && n.Tag == "!!null")
}

func scalar(n *Node) (string, bool) {
	if n == nil || n.Kind != ScalarNode || n.Tag == "!!null" {
		return "", false
	}
	return n.Value, true
}

func path(n *Node, keys ...string) *Node {
	for _, k := range keys {
		n = mapGet(n, k)
		if n == nil {
			return nil
		}
	}
	return n
}

func str(n *Node, keys ...string) string {
	s, _ := scalar(path(n, keys...))
	return s
}
