// Package diff compares the original's recorded answers with the port's.
//
// Numbers compare with a tolerance, object key order does not matter, and a missing function is a
// mismatch rather than a skip: a half-ported unit must not read as done.
package diff

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/xiaobingan/go-anything/internal/cases"
	"github.com/xiaobingan/go-anything/internal/wire"
)

// Options is one diff run.
type Options struct {
	CrateDir  string
	Tolerance float64
	Shown     int
}

// Mismatch is one case whose two answers differ, with the first difference found.
type Mismatch struct {
	Fn       string `json:"fn"`
	Args     []any  `json:"args"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
	Why      string `json:"why"`
}

// Outcome is what `diff` prints.
type Outcome struct {
	Cases     int        `json:"cases"`
	Matched   int        `json:"matched"`
	Missing   []string   `json:"missing"`
	Mismatch  []Mismatch `json:"mismatches"`
	Truncated bool       `json:"truncated"`
	// ErrorCases counts, per function, the recorded cases that raised: an error path with none of
	// these is a path nobody proved.
	ErrorCases map[string]int `json:"error_cases,omitempty"`
	// Cases counts, per function, the recorded cases: an export with none of these is a port nothing
	// ever asked a question about.
	PerFn map[string]int `json:"cases_by_fn,omitempty"`
}

// Passed is the sentence `gate` and a person both want.
func (o Outcome) Passed() bool { return len(o.Missing) == 0 && o.Matched == o.Cases && o.Cases > 0 }

// Run builds nothing: it runs the generated driver over the recorded cases and compares.
func Run(opts Options) (Outcome, error) {
	recorded, err := cases.Read(filepath.Join(opts.CrateDir, "cases.jsonl"))
	if err != nil {
		return Outcome{}, fmt.Errorf("no cases to compare: %w", err)
	}
	answers, err := runDriver(opts.CrateDir)
	if err != nil {
		return Outcome{}, err
	}
	tolerance := opts.Tolerance
	if tolerance <= 0 {
		tolerance = 1e-9
	}
	shown := opts.Shown
	if shown <= 0 {
		shown = 20
	}
	// Join the two sides by the call itself, not by position: a driver that skips a line (or a
	// cases file a person edited) must not shift every comparison after it.
	byCall := map[string]cases.Case{}
	for _, a := range answers {
		byCall[callKey(a.Fn, a.Args)] = a
	}
	out := Outcome{Cases: len(recorded), ErrorCases: map[string]int{}, PerFn: map[string]int{}}
	for _, want := range recorded {
		if want.Err != "" {
			out.ErrorCases[want.Fn]++
		}
		out.PerFn[want.Fn]++
	}
	missing := map[string]bool{}
	for _, want := range recorded {
		got, ok := byCall[callKey(want.Fn, want.Args)]
		if !ok {
			got = cases.Case{Fn: want.Fn, Err: "the driver did not answer this case"}
		}
		if strings.HasPrefix(got.Err, "not ported: ") || strings.Contains(got.Err, "not implemented: port ") {
			missing[want.Fn] = true
		}
		if why := same(want, got, tolerance); why == "" {
			out.Matched++
			continue
		} else if len(out.Mismatch) < shown {
			out.Mismatch = append(out.Mismatch, Mismatch{
				Fn:       want.Fn,
				Args:     want.Args,
				Expected: show(want),
				Got:      show(got),
				Why:      why,
			})
		} else {
			out.Truncated = true
		}
	}
	keys := make([]string, 0, len(missing))
	for fn := range missing {
		keys = append(keys, fn)
	}
	sort.Strings(keys)
	out.Missing = keys
	return out, nil
}

// callKey identifies one call: the function and its arguments, as JSON.
func callKey(fn string, args []any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return fn
	}
	return fn + "|" + string(b)
}

// RunDriver answers a cases file with the port's driver, for the commands that generate their own calls.
func RunDriver(crateDir string) ([]cases.Case, error) { return runDriver(crateDir) }

// Compare is Same, exported for the commands that compare outside a full diff (fuzz, shadow replay).
func Compare(want, got cases.Case, tolerance float64) string {
	return same(want, got, tolerance)
}

// runDriver executes the generated Go driver in the crate folder.
func runDriver(crateDir string) ([]cases.Case, error) {
	out := filepath.Join(crateDir, "work", "answers.jsonl")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	// The driver runs with the crate as its working directory, so both paths must be absolute.
	abs, err := filepath.Abs(crateDir)
	if err != nil {
		return nil, err
	}
	// No port.go means there is nothing to compare. Running the last driver binary would answer for code
	// that is no longer in the crate, and a person would read that as a result.
	if _, err := os.Stat(filepath.Join(abs, "port.go")); err != nil {
		return nil, fmt.Errorf("%s has no port.go: the crate has nothing to compare", crateDir)
	}
	// A driver binary `build` already made is what runs: `go run` compiles on every call and costs ten
	// milliseconds before a single case is compared.
	cmd, built := driverCommand(abs)
	if built {
		// The rebuild runs first and on its own: the case paths are arguments to the driver, not to the
		// compiler, and handing them to `go build` made it complain about a package that is a file.
		cmd.Dir = abs
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("rebuilding the driver after a source change: %w", err)
		}
		cmd, _ = driverCommand(abs)
		cmd.Args = append(cmd.Args, filepath.Join(abs, "cases.jsonl"), filepath.Join(abs, "work", "answers.jsonl"))
		cmd.Dir = abs
	} else {
		cmd.Args = append(cmd.Args, filepath.Join(abs, "cases.jsonl"), filepath.Join(abs, "work", "answers.jsonl"))
		cmd.Dir = abs
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the port's driver failed: %w%s", err, goHint(err))
	}
	return cases.Read(out)
}

// same returns "" when the two answers agree, or the first difference it finds.
func same(want, got cases.Case, tolerance float64) string {
	switch {
	case want.Err != "" && got.Err != "":
		if errorKind(want.Err) == errorKind(got.Err) {
			return ""
		}
		return "error type"
	case want.Err != "" && got.Panic != "":
		// A Go panic where the original raised: the bridge reports it, and the kind must match.
		if errorKind(want.Err) == trimPanic(got.Panic) {
			return ""
		}
		return "error type (from a panic)"
	case want.Err != "" || got.Err != "" || got.Panic != "":
		return "one side failed and the other did not"
	}
	return compare("", wire.Decode(want.Ok), wire.Decode(got.Ok), tolerance)
}

func errorKind(msg string) string {
	if i := strings.Index(msg, ":"); i > 0 {
		return strings.TrimSpace(msg[:i])
	}
	return strings.TrimSpace(msg)
}

func trimPanic(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, ":"); i > 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}

// compare walks both values; `path` names where the difference is.
func compare(path string, want, got any, tolerance float64) string {
	at := func() string {
		if path == "" {
			return "the answer"
		}
		return path
	}
	switch w := want.(type) {
	case nil:
		if got != nil {
			return at() + " is null on one side"
		}
		return ""
	case bool:
		g, ok := got.(bool)
		if !ok || g != w {
			return at() + " is a boolean on one side"
		}
		return ""
	case float64:
		g, ok := asFloat(got)
		if !ok {
			return at() + " is a number on one side"
		}
		if w == g || (math.IsNaN(w) && math.IsNaN(g)) {
			return ""
		}
		scale := math.Max(math.Abs(w), math.Abs(g))
		if scale < 1 {
			scale = 1
		}
		if math.Abs(w-g) <= tolerance*scale {
			return ""
		}
		return at() + " differs"
	case string:
		switch g := got.(type) {
		case string:
			if g != w {
				return at() + " differs as text"
			}
			return ""
		case wire.UTF16:
			// The other side had to spell the same text as code units: compare them.
			if string(utf16.Decode(g)) != w {
				return at() + " differs as text"
			}
			return ""
		}
		return at() + " is text on one side"
	case wire.UTF16:
		switch g := got.(type) {
		case wire.UTF16:
			if len(g) != len(w) {
				return fmt.Sprintf("%s has %d code units on one side, %d on the other", at(), len(w), len(g))
			}
			for i := range w {
				if g[i] != w[i] {
					return at() + " differs at a code unit"
				}
			}
			return ""
		case string:
			if string(utf16.Decode(w)) != g {
				return at() + " differs as text"
			}
			return ""
		}
		return at() + " is UTF-16 units on one side"
	case []any:
		g, ok := got.([]any)
		if !ok {
			return at() + " is an array on one side"
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s has %d elements on one side, %d on the other", at(), len(w), len(g))
		}
		for i := range w {
			if why := compare(fmt.Sprintf("%s[%d]", path, i), w[i], g[i], tolerance); why != "" {
				return why
			}
		}
		return ""
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return at() + " is an object on one side"
		}
		// Key order is not part of the answer; the sets must match.
		for k := range w {
			if _, ok := g[k]; !ok {
				return fmt.Sprintf("%s has no key %q", at(), k)
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				return fmt.Sprintf("%s has an extra key %q", at(), k)
			}
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if why := compare(path+"."+k, w[k], g[k], tolerance); why != "" {
				return why
			}
		}
		return ""
	}
	return at() + " differs"
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// show renders one answer as the message a person reads.
func show(c cases.Case) string {
	switch {
	case c.Err != "":
		return clip("error "+c.Err, shownBytes)
	case c.Panic != "":
		return clip("panic "+c.Panic, shownBytes)
	}
	b, err := json.Marshal(c.Ok)
	if err != nil {
		return clip(fmt.Sprint(c.Ok), shownBytes)
	}
	return clip(string(b), shownBytes)
}

// shownBytes is how much of a value a mismatch prints. A recorded case can hold a 100,000-character
// string, and printing it in full turned one mismatch into 150 KB of terminal.
const shownBytes = 160

// clip shortens a value and says how much was left out, so the shape stays readable.
func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf(" … (%d bytes more)", len(s)-limit)
}

// Markdown is the table `diff` prints under its summary.
func (o Outcome) Markdown() string {
	var b strings.Builder
	for _, m := range o.Mismatch {
		args, _ := json.Marshal(m.Args)
		// The arguments are clipped the same way the answers are: one 100,000-character string in a
		// recorded case otherwise prints a screenful per mismatch.
		fmt.Fprintf(&b, "- `%s(%s)`: expected %s, got %s (%s)\n",
			m.Fn, clip(string(args[1:len(args)-1]), shownBytes), m.Expected, m.Got, m.Why)
	}
	if o.Truncated {
		b.WriteString("- … more mismatches, not shown\n")
	}
	if len(o.Missing) > 0 {
		fmt.Fprintf(&b, "- kept in the original: %s\n", strings.Join(o.Missing, ", "))
	}
	return b.String()
}

// goHint adds the fix for the one toolchain problem this repository's own machine had: a GOROOT from
// one Go installation with a binary from another.
func goHint(err error) string {
	out, _ := exec.Command("go", "env", "GOROOT").Output()
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	if _, statErr := os.Stat(filepath.Join(root, "pkg", "tool")); statErr != nil {
		return " (go env GOROOT is " + root + ", which has no tool chain: install Go and set GOROOT to the " +
			"installation the `go` binary came from, e.g. export GOROOT=$(brew --prefix go)/libexec)"
	}
	return ""
}

// driverCommand is the prebuilt driver when it is newer than every source file, and a fresh build when
// it is not.
//
// The staleness check is not a nicety: a prebuilt driver from an earlier `build` answered an old port, so
// `gate` passed on a crate that no longer compiled. A green gate has to mean the code in front of you.
func driverCommand(crateDir string) (*exec.Cmd, bool) {
	for _, name := range []string{"godriver", "godriver.exe"} {
		path := filepath.Join(crateDir, "work", name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if stale, _ := sourcesNewerThan(crateDir, info.ModTime()); stale {
			// Rebuild in place: the same path, so the next call runs the new binary.
			return exec.Command("go", "build", "-o", path, "./cmd/godriver"), true
		}
		return exec.Command(path), false
	}
	return exec.Command("go", "run", "./cmd/godriver"), false
}

// sourcesNewerThan reports whether any .go file in the crate changed after the driver was built.
func sourcesNewerThan(crateDir string, built time.Time) (bool, error) {
	newer := false
	err := filepath.WalkDir(crateDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// work/ holds the driver itself and the answers; the rest is source.
			if d.Name() == "work" && filepath.Dir(path) == crateDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(built) {
			newer = true
			return filepath.SkipAll
		}
		return nil
	})
	return newer, err
}
