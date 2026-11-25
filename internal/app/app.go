// Package app records what a whole program does and replays any build against it.
//
// `record` and `diff` prove one unit; this proves the program from the outside: run it with the same
// arguments, the same stdin and the same environment, twice, and keep what it wrote. A replay then
// says whether another build — a rewrite, a Go front door — behaves the same.
//
// The cases are JSON, not a bespoke test format: argv, stdin, env in; stdout, stderr, exit out. A
// person can read them, edit them and add the one invocation that broke production.
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dir is where a repository's app cases live.
const Dir = "go-anything-cases"

// Case is one invocation and what it did.
type Case struct {
	Name   string            `json:"name"`
	Argv   []string          `json:"argv"`
	Stdin  string            `json:"stdin,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Exit   int               `json:"exit"`
	Stdout string            `json:"stdout,omitempty"`
	Stderr string            `json:"stderr,omitempty"`
	// Noisy marks a case whose output is not stable between runs (a timestamp, a pid): replay checks
	// the exit code only, and says so.
	Noisy bool `json:"noisy,omitempty"`
}

// Default is the set every CLI has: asking for help, asking for a version, and saying nothing.
var Default = [][]string{
	{"--help"},
	{"--version"},
	{},
}

// Options is one recording run.
type Options struct {
	Repo string
	// Command is the program: a path, or a name on PATH.
	Command string
	// Args are appended to every case, for a program that needs a subcommand prefix.
	Args []string
	// Extra cases come from a JSONL file of `{"name":…, "argv":[…], "stdin":…}`.
	Inputs string
	// Runs is how many times each case runs to notice output that is not stable.
	Runs int
	// Timeout bounds one run.
	Timeout time.Duration
}

// Outcome is what a record or replay produced.
type Outcome struct {
	Cases   int      `json:"cases"`
	Passed  int      `json:"passed"`
	Failed  []string `json:"failed,omitempty"`
	Dir     string   `json:"dir"`
	Notes   []string `json:"notes,omitempty"`
	Seconds float64  `json:"seconds"`
}

// Record runs the program on the cases and stores them under <repo>/go-anything-cases/.
func Record(opts Options) (Outcome, error) {
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return Outcome{}, err
	}
	bin, err := resolve(opts.Command)
	if err != nil {
		return Outcome{}, err
	}
	dir := filepath.Join(repo, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Outcome{}, err
	}
	argvs := append([][]string{}, Default...)
	var names []string
	for _, a := range Default {
		names = append(names, nameOf(a))
	}
	if opts.Inputs != "" {
		extra, err := readInputs(opts.Inputs)
		if err != nil {
			return Outcome{}, err
		}
		for _, c := range extra {
			argvs = append(argvs, c.Argv)
			names = append(names, c.Name)
		}
	}

	started := time.Now()
	runs := opts.Runs
	if runs < 2 {
		runs = 2 // twice: the second run is what says whether the output is stable
	}
	out := Outcome{Dir: dir}
	var cases []Case
	for i, argv := range argvs {
		full := append(append([]string{}, opts.Args...), argv...)
		var first Case
		stable := true
		for run := 0; run < runs; run++ {
			c, err := run1(bin, full, Case{Name: names[i]}, opts.Timeout)
			if err != nil {
				return Outcome{}, err
			}
			if run == 0 {
				first = c
				continue
			}
			if c.Stdout != first.Stdout || c.Stderr != first.Stderr || c.Exit != first.Exit {
				stable = false
			}
		}
		first.Noisy = !stable
		if !stable {
			out.Notes = append(out.Notes, fmt.Sprintf("%s: output differs between two runs, so replay checks only the exit code", first.Name))
		}
		cases = append(cases, first)
	}
	raw, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return Outcome{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), append(raw, '\n'), 0o644); err != nil {
		return Outcome{}, err
	}
	meta := map[string]any{"command": opts.Command, "args": opts.Args, "runs": runs, "recorded": time.Now().Format(time.RFC3339)}
	metaRaw, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), append(metaRaw, '\n'), 0o644); err != nil {
		return Outcome{}, err
	}
	out.Cases = len(cases)
	out.Passed = len(cases)
	out.Seconds = time.Since(started).Seconds()
	return out, nil
}

// Replay runs another build against the recorded cases.
func Replay(opts Options) (Outcome, error) {
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return Outcome{}, err
	}
	dir := filepath.Join(repo, Dir)
	raw, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		return Outcome{}, fmt.Errorf("nothing recorded here: run `go-anything app record %s` first", opts.Repo)
	}
	var cases []Case
	if err := json.Unmarshal(raw, &cases); err != nil {
		return Outcome{}, err
	}
	bin, err := resolve(opts.Command)
	if err != nil {
		return Outcome{}, err
	}
	started := time.Now()
	out := Outcome{Cases: len(cases), Dir: dir}
	for _, want := range cases {
		got, err := run1(bin, append(append([]string{}, opts.Args...), want.Argv...), want, opts.Timeout)
		if err != nil {
			return Outcome{}, err
		}
		if why := compare(want, got); why != "" {
			out.Failed = append(out.Failed, fmt.Sprintf("%s: %s", want.Name, why))
			continue
		}
		out.Passed++
	}
	out.Seconds = time.Since(started).Seconds()
	return out, nil
}

// run1 runs one case.
func run1(bin string, argv []string, in Case, timeout time.Duration) (Case, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmd := exec.Command(bin, argv...)
	cmd.Stdin = strings.NewReader(in.Stdin)
	cmd.Env = os.Environ()
	for k, v := range in.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A program that never exits must not hang the recorder: the timeout is part of the contract.
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return Case{}, err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return Case{Name: in.Name, Argv: argv, Stdin: in.Stdin, Env: in.Env,
			Exit: -9, Stderr: "timed out"}, nil
	case err := <-done:
		exit := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			} else {
				return Case{}, err
			}
		}
		// The program's own path leaks into usage messages ("Usage of /abs/path"): normalise it, or
		// every replay of a different build fails on its location instead of its behaviour.
		prog := "<prog>"
		return Case{
			Name: in.Name, Argv: argv, Stdin: in.Stdin, Env: in.Env,
			Exit: exit, Stdout: strings.ReplaceAll(stdout.String(), bin, prog),
			Stderr: strings.ReplaceAll(stderr.String(), bin, prog),
		}, nil
	}
}

// compare says why two runs differ, or "" when they agree.
func compare(want, got Case) string {
	if want.Exit != got.Exit {
		return fmt.Sprintf("exit %d, want %d", got.Exit, want.Exit)
	}
	if want.Noisy {
		return "" // the recorder already said this output is not stable
	}
	if want.Stdout != got.Stdout {
		return "stdout differs"
	}
	if want.Stderr != got.Stderr {
		return "stderr differs"
	}
	return ""
}

// nameOf turns an argv into a case name a person can read in a failure list.
func nameOf(argv []string) string {
	if len(argv) == 0 {
		return "no arguments"
	}
	return strings.Join(argv, " ")
}

// readInputs reads extra cases: one JSON object per line.
func readInputs(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Case
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		if c.Name == "" {
			c.Name = nameOf(c.Argv)
		}
		out = append(out, c)
	}
	return out, nil
}

// resolve finds the program, and refuses a path that does not exist rather than failing later.
func resolve(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("no command: pass --command, the program to record")
	}
	if strings.ContainsRune(command, filepath.Separator) {
		abs, err := filepath.Abs(command)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("%s: %w", command, err)
		}
		return abs, nil
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("%s is not on PATH: pass --command with a path", command)
	}
	return path, nil
}

// Markdown is the table record and replay print.
func (o Outcome) Markdown() string {
	var b strings.Builder
	if len(o.Failed) == 0 {
		fmt.Fprintf(&b, "%d/%d cases replay identically in %.2fs.\n", o.Passed, o.Cases, o.Seconds)
	} else {
		fmt.Fprintf(&b, "%d/%d cases replay identically; %d differ:\n", o.Passed, o.Cases, len(o.Failed))
		sorted := append([]string{}, o.Failed...)
		sort.Strings(sorted)
		for _, f := range sorted {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	for _, n := range o.Notes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	return b.String()
}
