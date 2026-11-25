package frontdoor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a repository with a recording, which is what a front door is built from.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cases := filepath.Join(dir, "go-anything-cases")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[
  {"name":"--version","argv":["--version"],"exit":0,"stdout":"prog 1.0\n"},
  {"name":"count","argv":["--count","a b"],"exit":0,"stdout":"1\n"},
  {"name":"no arguments","argv":[],"exit":2,"stderr":"prog: nothing to do\n"}
]`
	if err := os.WriteFile(filepath.Join(cases, "cases.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cases, "meta.json"),
		[]byte(`{"command":"/usr/local/bin/prog","runs":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScaffoldWritesADoorThatKnowsItsCommands(t *testing.T) {
	dir := repo(t)
	res, err := Scaffold(Options{Repo: dir, Name: "prog"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Original != "/usr/local/bin/prog" {
		t.Errorf("original: %q", res.Original)
	}
	if len(res.Commands) != 3 {
		t.Errorf("commands: %+v", res.Commands)
	}
	table, err := os.ReadFile(filepath.Join(res.Dir, "commands.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The recorded argv travels with the command: a handler needs its own arguments.
	if !strings.Contains(string(table), `argv: []string{"--count", "a b"}`) {
		t.Errorf("the recorded argv is missing:\n%s", table)
	}
	if !strings.Contains(string(table), `handler: ""`) {
		t.Errorf("nothing should be wired yet:\n%s", table)
	}
	main, err := os.ReadFile(filepath.Join(res.Dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The fallback is what makes it usable from day one, and it must be the recorded program.
	if !strings.Contains(string(main), `const original = "/usr/local/bin/prog"`) {
		t.Errorf("main.go does not carry the original:\n%s", main)
	}
}

func TestScaffoldRefusesWithoutARecording(t *testing.T) {
	empty := t.TempDir()
	if _, err := Scaffold(Options{Repo: empty, Name: "x"}); err == nil {
		t.Error("a front door needs a recording: app record first")
	}
}

func TestWireThenLift(t *testing.T) {
	dir := repo(t)
	res, err := Scaffold(Options{Repo: dir, Name: "prog"})
	if err != nil {
		t.Fatal(err)
	}
	// Wiring the recorded commands one at a time is the normal path; lifting before that must fail.
	if _, err := Lift(res.Dir); err == nil {
		t.Fatal("lift with nothing wired should refuse")
	}
	for _, c := range res.Commands {
		if _, err := Wire(res.Dir, c.Key, "runCommand", "", "example.com/prog/go-app/portstore", "echo"); err != nil {
			t.Fatalf("wire %q: %v", c.Key, err)
		}
	}
	ok, wired, total, err := CanLift(res.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || wired != total || total != len(res.Commands) {
		t.Fatalf("can lift: ok=%v wired=%d total=%d", ok, wired, total)
	}
	if _, err := Lift(res.Dir); err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(res.Dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), "os/exec") {
		t.Error("lifting leaves the fallback's import behind, which does not build")
	}
	if !strings.Contains(string(main), "lifted: every recorded command runs in Go") {
		t.Errorf("main.go was not lifted:\n%s", main)
	}
	// Lifting twice is idempotent.
	if _, err := Lift(res.Dir); err != nil {
		t.Errorf("second lift: %v", err)
	}
}

func TestWireRefusesACommandThatWasNeverRecorded(t *testing.T) {
	dir := repo(t)
	res, err := Scaffold(Options{Repo: dir, Name: "prog"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Wire(res.Dir, "--never", "runNever", "", "example.com/prog/go-app/portstore", "echo"); err == nil {
		t.Error("wiring an unrecorded command must be refused: nothing proves it")
	}
}
