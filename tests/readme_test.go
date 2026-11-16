package tests

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type readmeCommand struct {
	cmd, want string
	line      int
}

// readmeCommands finds each indented "$ " line in the README and the output
// shown under it, up to the next command or the end of the code block.
func readmeCommands(t *testing.T) []readmeCommand {
	t.Helper()
	data, err := os.ReadFile(root("README.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var out []readmeCommand
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    $ ") {
			continue
		}
		c := readmeCommand{cmd: strings.TrimPrefix(lines[i], "    $ "), line: i + 1}
		var want []string
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "    $ ") {
				break
			}
			if l != "" && !strings.HasPrefix(l, "    ") {
				break
			}
			want = append(want, strings.TrimPrefix(l, "    "))
		}
		c.want = strings.TrimRight(strings.Join(want, "\n"), "\n")
		out = append(out, c)
	}
	return out
}

// Every command in the README runs from the repository root and prints exactly
// what the README shows.
func TestReadmeCommands(t *testing.T) {
	cmds := readmeCommands(t)
	if len(cmds) < 5 {
		t.Fatalf("found only %d commands", len(cmds))
	}
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c.cmd)
		cmd.Dir = ".."
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		cmd.Run()
		got := strings.TrimRight(out.String(), "\n")
		if got != c.want {
			t.Errorf("README line %d: %s\n--- got\n%s\n--- want\n%s\n--- stderr\n%s", c.line, c.cmd, got, c.want, errb.String())
		}
		if errb.Len() > 0 {
			t.Errorf("README line %d printed to stderr: %s", c.line, errb.String())
		}
	}
}
