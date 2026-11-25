package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// script writes a tiny program that echoes its arguments and exit code, and returns its path.
func script(t *testing.T, exitOn string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "prog")
	body := "#!/bin/sh\ncase \"$1\" in\n" +
		"  --version) echo \"prog 1.0\" ;;\n" +
		"  fail) echo 'boom' >&2 ; exit 3 ;;\n" +
		"  \"\") echo 'nothing' ;;\n" +
		"  *) echo \"args: $*\" ;;\nesac\n"
	if exitOn != "" {
		body = strings.ReplaceAll(body, "prog 1.0", exitOn)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecordThenReplayIdentically(t *testing.T) {
	repo := t.TempDir()
	bin := script(t, "")
	res, err := Record(Options{Repo: repo, Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cases != len(Default) {
		t.Fatalf("cases: %+v", res)
	}
	again, err := Replay(Options{Repo: repo, Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	if again.Passed != again.Cases || len(again.Failed) > 0 {
		t.Errorf("the same program must replay identically: %+v", again)
	}
	// The recorded file is the contract, and it names the cases a person sees in a failure.
	raw, err := os.ReadFile(filepath.Join(repo, Dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"--version"`) || !strings.Contains(string(raw), `"no arguments"`) {
		t.Errorf("cases.json does not describe the runs:\n%s", raw)
	}
}

func TestReplaySeesAChangedAnswer(t *testing.T) {
	repo := t.TempDir()
	if _, err := Record(Options{Repo: repo, Command: script(t, "")}); err != nil {
		t.Fatal(err)
	}
	changed := script(t, "prog 2.0")
	res, err := Replay(Options{Repo: repo, Command: changed})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("one case should differ: %+v", res.Failed)
	}
	if !strings.Contains(res.Failed[0], "--version") || !strings.Contains(res.Failed[0], "stdout differs") {
		t.Errorf("the failure should name the case and the stream: %q", res.Failed[0])
	}
}

func TestRecordNeedsAProgramAndReplayNeedsARecording(t *testing.T) {
	if _, err := Record(Options{Repo: t.TempDir()}); err == nil {
		t.Error("record without --command must be refused")
	}
	if _, err := Replay(Options{Repo: t.TempDir(), Command: "true"}); err == nil {
		t.Error("replay without a recording must be refused")
	}
}
