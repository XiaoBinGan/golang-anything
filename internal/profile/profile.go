// Package profile runs a real workload under the runtime's profiler and says what to port.
//
// `analyze` ranks units by how they look; this ranks them by what actually ran. The workload is any
// command — a test suite, a script, a server exercised by a few requests — and the profiler is the one
// the runtime already has: `node --cpu-prof` for a JS/TS original, `go test -cpuprofile` for a Go one.
// The output is self time per function, so a hot leaf is not hidden behind its caller.
package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaobingan/go-anything/internal/analyze"
)

// Options is one profiling run.
type Options struct {
	Repo string
	// Command is the workload: argv[0] is the program, the rest is what it runs.
	Command []string
	// NodeArgs are extra arguments for node, e.g. --import=tsx.
	NodeArgs []string
	Timeout  time.Duration
	Top      int
}

// Hot is one function's share of the run.
type Hot struct {
	Function string  `json:"function"`
	File     string  `json:"file"`
	Line     int     `json:"line"`
	SelfMS   float64 `json:"self_ms,omitempty"`
	Share    float64 `json:"share"`
	Unit     string  `json:"unit,omitempty"`
	Portable string  `json:"portable,omitempty"` // why the unit cannot be ported, when it cannot
}

// Report is what a profile run found.
type Report struct {
	Command []string `json:"command"`
	Seconds float64  `json:"seconds"`
	Samples int      `json:"samples"`
	Hot     []Hot    `json:"hot"`
	Units   int      `json:"units"`
	Written string   `json:"cpuprofile"`
	Notes   []string `json:"notes,omitempty"`
}

// Run profiles the workload and maps its hot functions onto the plan's units.
func Run(opts Options) (Report, error) {
	if len(opts.Command) == 0 {
		return Report{}, fmt.Errorf("no workload: pass --command, the program to profile")
	}
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return Report{}, err
	}
	dir := filepath.Join(repo, "work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Report{}, err
	}
	// A fresh folder per run: a stale profile is worse than none.
	profDir := filepath.Join(dir, "cpuprofile")
	if err := os.RemoveAll(profDir); err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		return Report{}, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	args := append([]string{"--cpu-prof", "--cpu-prof-dir=" + profDir}, opts.NodeArgs...)
	args = append(args, opts.Command...)
	cmd := exec.Command("node", args...)
	cmd.Dir = repo
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	started := time.Now()
	if err := runWithTimeout(cmd, timeout); err != nil {
		return Report{}, fmt.Errorf("the workload failed: %w", err)
	}
	elapsed := time.Since(started).Seconds()

	files, err := filepath.Glob(filepath.Join(profDir, "*.cpuprofile"))
	if err != nil || len(files) == 0 {
		return Report{}, fmt.Errorf("no .cpuprofile in %s: did the workload start node?", profDir)
	}
	sort.Strings(files)
	path := files[len(files)-1]
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var prof cpuProfile
	if err := json.Unmarshal(raw, &prof); err != nil {
		return Report{}, fmt.Errorf("%s: %w", path, err)
	}

	hot, samples := summarise(prof, repo)
	rep := Report{Command: opts.Command, Seconds: elapsed, Samples: samples, Hot: hot, Written: path}
	if opts.Top > 0 && len(rep.Hot) > opts.Top {
		rep.Hot = rep.Hot[:opts.Top]
	}
	plan, err := analyze.Run(repo)
	if err != nil {
		rep.Notes = append(rep.Notes, "could not read the plan: "+err.Error())
		return rep, nil
	}
	rep.Units = len(plan.Units)
	byUnit := map[string]analyze.Unit{}
	for _, u := range plan.Units {
		byUnit[u.ID] = u
	}
	for i := range rep.Hot {
		unit, ok := byUnit[rep.Hot[i].File]
		if !ok {
			continue
		}
		rep.Hot[i].Unit = unit.ID
		if unit.Status == "keep" {
			rep.Hot[i].Portable = "stays in the original: " + strings.Join(unit.Notes, "; ")
		} else if unit.Bridgeable == 0 {
			rep.Hot[i].Portable = "no export a bridge can carry"
		}
	}
	return rep, nil
}

// cpuProfile is the part of the V8 profile format this needs.
type cpuProfile struct {
	Nodes []struct {
		ID        int `json:"id"`
		CallFrame struct {
			FunctionName string `json:"functionName"`
			URL          string `json:"url"`
			LineNumber   int    `json:"lineNumber"`
		} `json:"callFrame"`
		HitCount int `json:"hitCount"`
	} `json:"nodes"`
	Samples []int `json:"samples"`
}

// summarise turns samples into self time per function, keeping only the repository's own code.
func summarise(prof cpuProfile, repo string) ([]Hot, int) {
	byID := map[int]int{}
	for i, n := range prof.Nodes {
		byID[n.ID] = i
	}
	_ = byID
	// Time per sample is not recorded; the profile's samples are the unit, so the share is sample
	// count over total. That is what a person needs: which function owns the run.
	count := map[int]int{}
	for _, id := range prof.Samples {
		if i, ok := byID[id]; ok {
			count[i]++
		}
	}
	total := len(prof.Samples)
	if total == 0 {
		return nil, 0
	}
	merged := map[string]*Hot{}
	for i, n := range prof.Nodes {
		c := count[i]
		if c == 0 {
			continue
		}
		name := n.CallFrame.FunctionName
		if name == "" {
			name = "(top level)"
		}
		url := n.CallFrame.URL
		if strings.HasPrefix(url, "file://") {
			url = strings.TrimPrefix(url, "file://")
		}
		// The runtime's own frames and vm internals are not something a port replaces; an anonymous
		// frame is the module's top level, which has no name to port but is still its code.
		if url == "" || strings.HasPrefix(url, "node:") || strings.HasPrefix(name, "wasm-function") ||
			strings.Contains(url, "/node_modules/") {
			continue
		}
		file := relTo(repo, url)
		key := name + "|" + file
		h, ok := merged[key]
		if !ok {
			h = &Hot{Function: name, File: file, Line: n.CallFrame.LineNumber + 1}
			merged[key] = h
		}
		h.Share += float64(c) / float64(total) * 100
	}
	out := make([]Hot, 0, len(merged))
	for _, h := range merged {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Share != out[j].Share {
			return out[i].Share > out[j].Share
		}
		return out[i].Function < out[j].Function
	})
	return out, total
}

// relTo keeps a path relative when it is inside the repository, and says `<outside>` otherwise.
//
// Both sides are resolved through symlinks first: on macOS /tmp is a link to /private/tmp, and the
// profiler reports the resolved path while the caller passed the other one.
func relTo(repo, path string) string {
	root := realish(repo)
	abs := realish(path)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "<outside>"
	}
	return filepath.ToSlash(rel)
}

// realish resolves symlinks in the longest prefix that exists: a profile can name a file that was
// deleted since, and /tmp is a link to /private/tmp on macOS, so both sides must be resolved the same
// way or the comparison is meaningless.
func realish(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	var tail []string
	current := abs
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		if real, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{real}, tail...)...)
		}
		current = parent
	}
}

// runWithTimeout runs a command and kills it when the workload never finishes.
func runWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("the workload did not finish in %s", timeout)
	case err := <-done:
		return err
	}
}

// Markdown is the table `profile` prints.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d samples over %.2fs: `%s`\n\n", r.Samples, r.Seconds, strings.Join(r.Command, " "))
	if len(r.Hot) == 0 {
		b.WriteString("Nothing in the repository ran: the workload never reached its own code.\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "note: %s\n", n)
		}
		return b.String()
	}
	b.WriteString("| Function | Where | Share | Unit | Port? |\n| --- | --- | ---: | --- | --- |\n")
	for _, h := range r.Hot {
		port := "yes"
		switch {
		case h.Portable != "":
			port = h.Portable
		case h.Unit == "":
			port = "not part of a unit"
		}
		fmt.Fprintf(&b, "| `%s` | `%s:%d` | %.1f%% | %s | %s |\n",
			h.Function, h.File, h.Line, h.Share, orDash(h.Unit), esc(port))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	fmt.Fprintf(&b, "\nProfile kept at `%s`.\n", r.Written)
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return "`" + s + "`"
}

func esc(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
