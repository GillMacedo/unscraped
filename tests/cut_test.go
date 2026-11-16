package tests

import (
	"strings"
	"testing"
)

// Cut files in the middle of a line are always refused. A cut on a line break
// is refused or read as a shorter file, never as more than the full file, and
// for the broken example it never exits 0, because the earliest broken monitor
// comes before any complete one. A line cut inside the last object that leaves
// it valid can't be told apart from a complete file; the README says so.
func TestCuts(t *testing.T) {
	files := []string{"examples/broken/shop.yaml", "examples/fixed/shop.yaml", nginxRelease, flux, certManager}
	for _, f := range files {
		data := readFile(t, f)
		full, _ := runJSON(t, []byte(data), "-n", "flux-system", selectAll, "-")
		step := len(data) / 150
		if step < 1 {
			step = 1
		}
		for i := 1; i < len(data)-1; i += step {
			cut := data[:i]
			if strings.HasSuffix(cut, "\n") {
				continue
			}
			r := runTool(t, []byte(cut), "-n", "flux-system", selectAll, "-")
			refused(t, r, f+" cut mid-line")
		}
		lines := strings.SplitAfter(data, "\n")
		for n := 1; n < len(lines)-1; n++ {
			cut := strings.Join(lines[:n], "")
			rep, r := runJSON(t, []byte(cut), "-n", "flux-system", selectAll, "-")
			if r.code == 2 {
				continue
			}
			if rep.Summary.Objects > full.Summary.Objects || rep.Summary.Checks > full.Summary.Checks {
				t.Errorf("%s cut at line %d read as %d objects and %d checks, the full file has %d and %d", f, n, rep.Summary.Objects, rep.Summary.Checks, full.Summary.Objects, full.Summary.Checks)
			}
			if f == "examples/broken/shop.yaml" && r.code == 0 {
				t.Errorf("%s cut at line %d exits 0", f, n)
			}
		}
	}
}
