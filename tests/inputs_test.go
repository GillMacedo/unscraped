package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aloneOnly are bad only on their own: valid manifests with nothing to check.
var aloneOnly = map[string]bool{"no-monitors.yaml": true, "monitor-only.yaml": true}

func badFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(root("tests/testdata/bad"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	if len(out) < 25 {
		t.Fatalf("only %d bad files", len(out))
	}
	return out
}

func refused(t *testing.T, r result, what string) {
	t.Helper()
	if r.code != 2 {
		t.Errorf("%s: exit %d, want 2\nstdout: %s", what, r.code, r.stdout)
	}
	if r.stdout != "" {
		t.Errorf("%s: stdout not empty: %s", what, r.stdout)
	}
	if !strings.Contains(r.stderr, "can't trust the input") {
		t.Errorf("%s: stderr: %s", what, r.stderr)
	}
}

var flagSets = [][]string{nil, {"--no-fail"}, {"--format", "json"}, {"--format", "json", "--no-fail"}}

// Every bad file is refused alone, with every flag, and on stdin.
func TestBadAlone(t *testing.T) {
	for _, name := range badFiles(t) {
		p := "tests/testdata/bad/" + name
		for _, fl := range flagSets {
			refused(t, runTool(t, nil, append(fl, p)...), name+" "+strings.Join(fl, " "))
		}
		data := readFile(t, p)
		refused(t, runTool(t, []byte(data), "-"), name+" on stdin")
	}
}

// A bad file next to a real, good one fails the whole run, in either order.
func TestBadNextToGood(t *testing.T) {
	for _, name := range badFiles(t) {
		if aloneOnly[name] {
			continue
		}
		p := "tests/testdata/bad/" + name
		for _, fl := range flagSets {
			refused(t, runTool(t, nil, append(fl, kps, p)...), name+" after good")
			refused(t, runTool(t, nil, append(fl, p, kps)...), name+" before good")
		}
	}
}

// In a directory, YAML that isn't manifests is skipped and listed, and
// everything else bad is refused.
func TestBadInDirectory(t *testing.T) {
	skipped := map[string]bool{"helm-values.yaml": true, "github-workflow.yaml": true, "json-other.json": true, "comments-only.yaml": true, "helm-unrendered.yaml": true}
	for _, name := range badFiles(t) {
		if aloneOnly[name] {
			continue
		}
		dir := t.TempDir()
		copyFile(t, root(kps), filepath.Join(dir, "a-good.yaml"))
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".json" {
			ext = ".yaml"
		}
		copyFile(t, root("tests/testdata/bad", name), filepath.Join(dir, "b-bad"+ext))
		r := runTool(t, nil, dir)
		if skipped[name] {
			if r.code != 0 || !strings.Contains(r.stdout, "Skipped 1 file") {
				t.Errorf("%s: exit %d, %s %s", name, r.code, r.stdout, r.stderr)
			}
			continue
		}
		refused(t, r, name+" in a directory")
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFileAndEmptyDir(t *testing.T) {
	refused(t, runTool(t, nil, "tests/testdata/does-not-exist.yaml"), "missing file")
	refused(t, runTool(t, nil, t.TempDir()), "empty directory")
}

func TestBadFlags(t *testing.T) {
	for _, args := range [][]string{{"--format", "xml", kps}, {"--bogus", kps}, {}, {"--namespace", "", kps}} {
		r := runTool(t, nil, args...)
		if r.code != 2 || r.stdout != "" {
			t.Errorf("%v: exit %d stdout %q", args, r.code, r.stdout)
		}
	}
}
