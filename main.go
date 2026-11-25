// Command go-anything is the Go counterpart of rust-anything: it plans, records, scaffolds and
// proves a port into Go, one unit at a time.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"os/exec"

	"github.com/xiaobingan/go-anything/internal/analyze"
	"github.com/xiaobingan/go-anything/internal/bridge"
	"github.com/xiaobingan/go-anything/internal/diff"
	"github.com/xiaobingan/go-anything/internal/record"
	"github.com/xiaobingan/go-anything/internal/scaffold"
)

const usage = `go-anything: port a codebase into Go, one unit at a time.

  go-anything analyze  [--json] <root>            what would have to be ported, and in what order
  go-anything record   --unit <id> <crate>       run the original, store the answers as cases.jsonl
  go-anything scaffold --unit <id> <crate>       write port.go stubs, the driver and the manifest
  go-anything diff     <crate>                   compare the port against the recorded cases
  go-anything gate     <crate>                   diff, plus the checks that stop a half-done port
  go-anything build    <crate>                   build the port and the stdio bridge into one binary
  go-anything bridge   <crate>                   write the host wrappers (index.cjs, port.py)

A crate is a folder this tool made. It holds go-anything.json, cases.jsonl, port.go and cmd/godriver.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "analyze":
		err = runAnalyze(os.Args[2:])
	case "record":
		err = runRecord(os.Args[2:])
	case "scaffold":
		err = runScaffold(os.Args[2:])
	case "diff":
		err = runDiff(os.Args[2:], false)
	case "gate":
		err = runDiff(os.Args[2:], true)
	case "build":
		err = runBuild(os.Args[2:])
	case "bridge":
		err = runBridge(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "go-anything:", err)
		os.Exit(1)
	}
}

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("analyze takes one repository root")
	}
	rep, err := analyze.Run(fs.Arg(0))
	if err != nil {
		return err
	}
	if *asJSON {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("# Port plan: %s (%s)\n\n", filepath.Base(rep.Root), rep.Language)
	fmt.Printf("%d units with something a bridge can carry.\n\n", len(rep.Units))
	fmt.Printf("| Unit | Lines | Exports | Wave | Readiness | Priority |\n| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, u := range rep.Units {
		fmt.Printf("| `%s` | %d | %d | %d | %d | %d |\n", u.ID, u.Loc, len(u.Exports), u.Wave, u.Readiness, u.Priority)
	}
	if len(rep.StartHere) > 0 {
		fmt.Printf("\nStart here: %s\n", strings.Join(rep.StartHere, ", "))
	}
	for _, u := range rep.Units {
		if len(u.Exports) == 0 {
			continue
		}
		fmt.Printf("\n## Exports in `%s`\n", u.ID)
		for _, e := range u.Exports {
			var params []string
			for _, p := range e.Params {
				name := p.Name
				if p.Optional {
					name += "?"
				}
				if p.Rest {
					name = "..." + name
				}
				params = append(params, name+": "+p.Type)
			}
			rets := e.Returns
			if rets == "" {
				rets = "any"
			}
			line := fmt.Sprintf("- `%s(%s): %s`", e.Name, strings.Join(params, ", "), rets)
			if e.Method {
				line = fmt.Sprintf("- `%s.%s(...)`", e.Receiver, e.Name)
			}
			if e.Blocked != "" {
				line += " — stays in the original: " + e.Blocked
			}
			if len(e.ThrowLines) > 0 {
				line += fmt.Sprintf(" — %d throw path(s) at %s", len(e.ThrowLines), joinInts(e.ThrowLines))
			}
			fmt.Println(line)
		}
	}
	for _, u := range rep.Units {
		if len(u.Risks) == 0 {
			continue
		}
		fmt.Printf("\n## Risks in `%s`\n", u.ID)
		seen := map[string]bool{}
		for _, r := range u.Risks {
			if seen[r.Kind] {
				continue
			}
			seen[r.Kind] = true
			fmt.Printf("- %s (weight %g): %s\n", r.Kind, r.Weight, strings.Join(r.Lines, ", "))
		}
	}
	fmt.Printf("\nExports, types and risks come from a tree-sitter parse; a blocked export says why in its details.\n")
	return nil
}

// unitFrom finds the unit the manifest or the plan names.
//
// The plan root is the repository the crate was made from: the crate is a folder inside it, so the
// search starts at the nearest parent that still contains the unit's path.
func unitFrom(crateDir, id string) (analyze.Unit, error) {
	root, err := planRoot(crateDir, id)
	if err != nil {
		return analyze.Unit{}, err
	}
	rep, err := analyze.Run(root)
	if err != nil {
		return analyze.Unit{}, err
	}
	for _, u := range rep.Units {
		if u.ID == id {
			return u, nil
		}
	}
	var ids []string
	for _, u := range rep.Units {
		ids = append(ids, u.ID)
	}
	if len(ids) == 0 {
		return analyze.Unit{}, fmt.Errorf("no unit %q, and %s has no unit to port", id, root)
	}
	return analyze.Unit{}, fmt.Errorf("no unit %q in %s; the plan lists %s", id, root, strings.Join(ids, ", "))
}

// planRoot walks up from the crate until the unit's file exists under a parent.
func planRoot(crateDir, id string) (string, error) {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, id)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no folder above %s holds %s", crateDir, id)
		}
		dir = parent
	}
}

func runRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	unit := fs.String("unit", "", "unit id from analyze")
	only := fs.String("only", "", "comma-separated export names")
	inputs := fs.String("inputs", "", "JSONL of real calls to replay: {\"fn\": name, \"args\": [...]}")
	tests := fs.String("test-coverage", "", "run this test command to learn which exports no test touches")
	fs.Parse(args)
	if fs.NArg() != 1 || *unit == "" {
		return fmt.Errorf("record needs --unit <id> and a crate folder")
	}
	crate := fs.Arg(0)
	u, err := unitFrom(crate, *unit)
	if err != nil {
		return err
	}
	var names []string
	if *only != "" {
		names = strings.Split(*only, ",")
	}
	var command []string
	if *tests != "" {
		command = strings.Fields(*tests)
	}
	sum, err := record.Run(record.Options{
		CrateDir: crate, Unit: u, Only: names, Inputs: *inputs, TestCommand: command,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Recorded %d cases for %d functions (%s) from %s; %d throw paths need a failing case each.\n",
		sum.Cases, sum.Functions, sum.Fingerprint, u.ID, sum.Throws)
	if len(sum.Untested) > 0 {
		fmt.Printf("No test touches: %s\n", strings.Join(sum.Untested, ", "))
	}
	return nil
}

func runScaffold(args []string) error {
	fs := flag.NewFlagSet("scaffold", flag.ExitOnError)
	unit := fs.String("unit", "", "unit id from analyze")
	crate := fs.String("crate", "", "Go import path of the crate")
	force := fs.Bool("force", false, "overwrite the stubs")
	fs.Parse(args)
	if fs.NArg() != 1 || *unit == "" {
		return fmt.Errorf("scaffold needs --unit <id> and a crate folder")
	}
	dir := fs.Arg(0)
	u, err := unitFrom(dir, *unit)
	if err != nil {
		return err
	}
	path := *crate
	if path == "" {
		path = "example.com/" + filepath.Base(dir)
	}
	files, err := scaffold.Run(scaffold.Options{CrateDir: dir, Unit: u, Crate: path, Force: *force})
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %d files for %s (%d exports):\n", len(files), u.ID, len(u.Exports))
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	return nil
}

func runDiff(args []string, gate bool) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	tolerance := fs.Float64("tolerance", 1e-9, "numbers match within this relative tolerance")
	shown := fs.Int("shown", 20, "mismatches to print")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("diff takes one crate folder")
	}
	crate := fs.Arg(0)
	out, err := diff.Run(diff.Options{CrateDir: crate, Tolerance: *tolerance, Shown: *shown})
	if err != nil {
		return err
	}
	fmt.Printf("%d/%d recorded cases match.\n", out.Matched, out.Cases)
	if text := out.Markdown(); text != "" {
		fmt.Println()
		fmt.Print(text)
	}
	if !gate {
		return nil
	}
	throws, blocked, err := manifestChecks(crate)
	if err != nil {
		return err
	}
	checks := diff.Check(out, throws, blocked)
	for name, why := range blocked {
		fmt.Printf("stays in the original: %s (%s)\n", name, why)
	}
	if reasons := checks.Failed(out.Matched, out.Cases); len(reasons) > 0 {
		return fmt.Errorf("gate: %s", strings.Join(reasons, "; "))
	}
	fmt.Println("\ngate: every case matches, every export is in the port, and every throw path has a failing case.")
	return nil
}

// joinInts prints line numbers the way a person reads them.
func joinInts(lines []int) string {
	parts := make([]string, len(lines))
	for i, n := range lines {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

// manifestChecks reads the crate's manifest for the throw counts and the exports that stay behind.
func manifestChecks(crateDir string) (map[string]int, map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(crateDir, "go-anything.json"))
	if err != nil {
		return nil, nil, err
	}
	var m struct {
		Exports []struct {
			Name       string `json:"name"`
			ThrowLines []int  `json:"throw_lines"`
			Blocked    string `json:"blocked"`
		} `json:"exports"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, err
	}
	throws := map[string]int{}
	blocked := map[string]string{}
	for _, e := range m.Exports {
		throws[e.Name] = len(e.ThrowLines)
		if e.Blocked != "" {
			blocked[e.Name] = e.Blocked
		}
	}
	return throws, blocked, nil
}

// runBuild compiles the port and the bridge into <crate>/portctl, the binary the wrappers spawn.
func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("build takes one crate folder")
	}
	dir := fs.Arg(0)
	out, err := filepath.Abs(filepath.Join(dir, "portctl"))
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/portctl")
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the port: %w", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	fmt.Printf("Built %s (%.1f MB).\n", out, float64(info.Size())/(1<<20))
	fmt.Println("Copy it next to index.cjs (or set GO_ANYTHING_PORTCTL) and the wrappers will use it.")
	return nil
}

// runBridge writes the host wrappers for the exports the port carries.
func runBridge(args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("bridge takes one crate folder")
	}
	dir := fs.Arg(0)
	raw, err := os.ReadFile(filepath.Join(dir, "go-anything.json"))
	if err != nil {
		return err
	}
	var m struct {
		Crate   string `json:"crate"`
		Unit    string `json:"unit"`
		Exports []struct {
			Name    string `json:"name"`
			Blocked string `json:"blocked"`
		} `json:"exports"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	var names []string
	for _, e := range m.Exports {
		if e.Blocked == "" {
			names = append(names, e.Name)
		}
	}
	files, err := bridge.Run(bridge.Options{CrateDir: dir, Crate: m.Crate, Unit: m.Unit, Exports: names})
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %d files for %s (%d exports):\n", len(files), m.Unit, len(names))
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	return nil
}
