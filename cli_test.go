package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// runWith calls the dispatch with the arguments given and answers what it printed, in order.
func runWith(t *testing.T, args ...string) (string, string) {
	t.Helper()
	oldArgs, oldOut, oldErr := os.Args, os.Stdout, os.Stderr
	defer func() { os.Args, os.Stdout, os.Stderr = oldArgs, oldOut, oldErr }()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	os.Args = append([]string{"golang-anything"}, args...)

	done := make(chan [2]string, 1)
	go func() {
		out, _ := io.ReadAll(outR)
		errOut, _ := io.ReadAll(errR)
		done <- [2]string{string(out), string(errOut)}
	}()
	func() {
		defer func() {
			outW.Close()
			errW.Close()
			_ = recover() // a path that calls os.Exit would end the test; none of these do
		}()
		realMain()
	}()
	got := <-done
	return got[0], got[1]
}

// A tool that cannot say what version it is makes every bug report a guess.
func TestVersionAnswersWithoutASubcommand(t *testing.T) {
	for _, arg := range []string{"--version", "version", "-version"} {
		out, _ := runWith(t, arg)
		if !strings.Contains(out, "golang-anything ") {
			t.Errorf("%s printed %q", arg, out)
		}
	}
}

// --help goes to stdout, the way a person and a script both expect.
func TestHelpGoesToStdout(t *testing.T) {
	out, errOut := runWith(t, "--help")
	if !strings.Contains(out, "golang-anything analyze") {
		t.Errorf("stdout: %q", out)
	}
	if strings.TrimSpace(errOut) != "" {
		t.Errorf("--help wrote to stderr: %q", errOut)
	}
}
