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

	"time"

	"github.com/xiaobingan/go-anything/internal/analyze"
	"github.com/xiaobingan/go-anything/internal/app"
	"github.com/xiaobingan/go-anything/internal/bench"
	"github.com/xiaobingan/go-anything/internal/bridge"
	"github.com/xiaobingan/go-anything/internal/diff"
	"github.com/xiaobingan/go-anything/internal/fastbridge"
	"github.com/xiaobingan/go-anything/internal/fuzz"
	"github.com/xiaobingan/go-anything/internal/mapdoc"
	"github.com/xiaobingan/go-anything/internal/pkg"
	"github.com/xiaobingan/go-anything/internal/profile"
	"github.com/xiaobingan/go-anything/internal/record"
	"github.com/xiaobingan/go-anything/internal/report"
	"github.com/xiaobingan/go-anything/internal/scaffold"
	"github.com/xiaobingan/go-anything/internal/shadow"
	"github.com/xiaobingan/go-anything/internal/switched"
)

const usage = `go-anything: port a codebase into Go, one unit at a time.

  go-anything analyze  [--json] <root>            what would have to be ported, and in what order
  go-anything record   --unit <id> <crate>       run the original, store the answers as cases.jsonl
  go-anything scaffold --unit <id> <crate>       write port.go stubs, the driver and the manifest
  go-anything diff     <crate>                   compare the port against the recorded cases
  go-anything gate     <crate>                   diff, plus the checks that stop a half-done port
  go-anything build    <crate>                   build the port and the stdio bridge into one binary
  go-anything bridge   <crate>                   write the host wrappers (index.cjs, port.py)
  go-anything bench    <crate>                   time the cases on the original and on the port
  go-anything map      <root>                    write PORT-MAP.md: every unit, its crate, its last diff
  go-anything clean    <crate>                   remove what this tool and go build left behind
  go-anything doctor                             check the runtimes a port needs
  go-anything build --fast <crate>               also build the in-process bridge (c-shared + N-API)
  go-anything check    <crate>                   replay the cases through the host wrapper (addon or stdio)
  go-anything enable   <crate> [--only a,b] [--async]   make the original call the port
  go-anything disable  <crate>                   take that block out again, byte for byte
  go-anything shadow   <crate>                   write shadow.cjs: original and port side by side
  go-anything shadow-report <crate>              summarize what shadow.cjs has seen
  go-anything fuzz     <crate> [--count N] [--seed S] [--keep]   fresh inputs from a new seed, both sides
  go-anything refresh  <crate>                   regenerate the binding files from port.go
  go-anything report   <crate> [--out DIR]       write REPORT.md and card.svg for a PR or a README
  go-anything package  <crate> [--shape library|cli|service]   lay out what users install + CI
  go-anything app record <repo> --command CMD    run a whole program on its cases and keep them
  go-anything app replay <repo> --command CMD    run another build against the recorded cases
  go-anything profile  <repo> --command CMD       run a workload under the profiler and rank hot functions
  go-anything go       <repo> --command CMD       profile, pick the hottest unit, and hand it over

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
	case "bench":
		err = runBench(os.Args[2:])
	case "map":
		err = runMap(os.Args[2:])
	case "clean":
		err = runClean(os.Args[2:])
	case "doctor":
		err = runDoctor(os.Args[2:])
	case "fast":
		err = runFast(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "enable":
		err = runEnable(os.Args[2:])
	case "disable":
		err = runDisable(os.Args[2:])
	case "shadow":
		err = runShadow(os.Args[2:], false)
	case "shadow-report":
		err = runShadow(os.Args[2:], true)
	case "fuzz":
		err = runFuzz(os.Args[2:])
	case "refresh":
		err = runRefresh(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	case "package":
		err = runPackage(os.Args[2:])
	case "app":
		err = runApp(os.Args[2:])
	case "profile":
		err = runProfile(os.Args[2:])
	case "go":
		err = runGo(os.Args[2:])
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
	fast := fs.Bool("fast", false, "also build the in-process bridge (c-shared library + N-API addon)")
	nodeInclude := fs.String("node-include", "", "folder holding node_api.h (found automatically)")
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
	if *fast {
		return buildFast(dir, *nodeInclude)
	}
	fmt.Println("Copy it next to index.cjs (or set GO_ANYTHING_PORTCTL) and the wrappers will use it.")
	fmt.Println("For the in-process path, add --fast.")
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

// runBench times both sides and prints the table.
func runBench(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	minMS := fs.Int("min-ms", 100, "minimum measured time per function and side")
	repeat := fs.Int("repeat", 1, "calls per case in each round")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("bench takes one crate folder")
	}
	res, err := bench.Run(bench.Options{CrateDir: fs.Arg(0), MinMS: *minMS, Repeat: *repeat})
	if err != nil {
		return err
	}
	fmt.Print(res.Markdown())
	return nil
}

// runMap writes PORT-MAP.md.
func runMap(args []string) error {
	fs := flag.NewFlagSet("map", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("map takes one repository root")
	}
	text, err := mapdoc.Run(fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Print(text)
	fmt.Println("\nWrote PORT-MAP.md.")
	return nil
}

// runClean removes the build products and per-run work, and never touches a port's source.
func runClean(args []string) error {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "only say what would go")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("clean takes one crate folder")
	}
	dir := fs.Arg(0)
	var total int64
	removed := 0
	for _, name := range []string{"work", "portctl", "cmd/gobench"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := dirSize(path)
		total += size
		removed++
		fmt.Printf("%-14s %.1f MB\n", name, float64(size)/(1<<20))
		if *dry {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		_ = info
	}
	if removed == 0 {
		fmt.Println("nothing to remove: the crate has no build products or work folder")
		return nil
	}
	fmt.Printf("%d item(s), %.1f MB%s\n", removed, float64(total)/(1<<20),
		map[bool]string{true: " (dry run)", false: " removed"}[*dry])
	return nil
}

func dirSize(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// runDoctor checks what a port needs on this machine, and says the command that fixes each gap.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Parse(args)
	type check struct {
		Name    string `json:"name"`
		OK      bool   `json:"ok"`
		Version string `json:"version,omitempty"`
		Note    string `json:"note,omitempty"`
		Fix     string `json:"fix,omitempty"`
	}
	var checks []check
	probe := func(name, binary string, required bool, fix string) {
		c := check{Name: name, OK: true}
		path, err := exec.LookPath(binary)
		if err != nil {
			c.OK = false
			c.Note = "not found on PATH"
			c.Fix = fix
			if !required {
				c.Note += " (optional)"
			}
			checks = append(checks, c)
			return
		}
		out, _ := exec.Command(path, "--version").Output()
		c.Version = strings.TrimSpace(firstLine(string(out)))
		checks = append(checks, c)
	}
	probe("go", "go", true, "install Go 1.24+: https://go.dev/dl/")
	probe("node", "node", true, "install Node 22.18+ (types strip without a flag)")
	probe("python3", "python3", false, "only needed for the Python wrapper")
	// Node has to strip types by itself, or record needs the flag.
	if out, err := exec.Command("node", "-e", "process.stdout.write(process.versions.node)").Output(); err == nil {
		major, minor := 0, 0
		fmt.Sscanf(strings.TrimSpace(string(out)), "%d.%d", &major, &minor)
		// Go has no tuple comparison: spell the version rule out.
		needs := major < 22 || (major == 22 && minor < 18) || (major == 23 && minor < 6)
		checks = append(checks, check{
			Name: "node strips types",
			OK:   !needs,
			Note: strings.TrimSpace(string(out)),
			Fix:  "record adds --experimental-strip-types itself on older Node",
		})
	}
	// cgo must be on: tree-sitter is C.
	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err == nil {
		on := strings.TrimSpace(string(out)) == "1"
		checks = append(checks, check{
			Name: "cgo", OK: on, Note: strings.TrimSpace(string(out)),
			Fix: "CGO_ENABLED=1 go build: the TypeScript parser is C",
		})
	}
	failed := 0
	for _, c := range checks {
		if !c.OK {
			failed++
		}
	}
	if *asJSON {
		b, _ := json.MarshalIndent(checks, "", "  ")
		fmt.Println(string(b))
		if failed > 0 {
			return fmt.Errorf("doctor: %d check(s) failed", failed)
		}
		return nil
	}
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark = "MISS"
		}
		note := c.Note
		if note == "" {
			note = c.Version
		}
		fmt.Printf("%s %-20s %s\n", mark, c.Name, note)
		if !c.OK && c.Fix != "" {
			fmt.Printf("     fix: %s\n", c.Fix)
		}
	}
	if failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// runFast builds only the in-process bridge.
func runFast(args []string) error {
	fs := flag.NewFlagSet("fast", flag.ExitOnError)
	nodeInclude := fs.String("node-include", "", "folder holding node_api.h")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("fast takes one crate folder")
	}
	return buildFast(fs.Arg(0), *nodeInclude)
}

// buildFast compiles the c-shared library and the N-API addon for one crate.
func buildFast(dir, nodeInclude string) error {
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	// The c-shared library is built from `cmd/portlib`, which `bridge` writes; write it here when the
	// crate predates that step, so `build --fast` means what it says.
	if _, err := os.Stat(filepath.Join(dir, "cmd", "portlib")); err != nil {
		if _, err := bridge.Run(bridge.Options{
			CrateDir: dir, Crate: m.Crate, Unit: m.Unit, Exports: m.Names(),
		}); err != nil {
			return err
		}
	}
	files, err := fastbridge.Run(fastbridge.Options{
		CrateDir: dir, Crate: m.Crate, Exports: m.Names(), NodeInclude: nodeInclude,
	})
	if err != nil {
		return err
	}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() && (strings.HasSuffix(f, ".dylib") || strings.HasSuffix(f, ".so") ||
			strings.HasSuffix(f, ".node") || strings.HasSuffix(f, ".dll")) {
			fmt.Printf("  %s (%.1f MB)\n", filepath.Base(f), float64(info.Size())/(1<<20))
		}
	}
	fmt.Println("index.cjs uses the addon when it is next to it: no pipe, one function call per call.")
	return nil
}

// manifest is what the CLI needs from go-anything.json.
type manifest struct {
	Crate   string `json:"crate"`
	Unit    string `json:"unit"`
	Exports []struct {
		Name    string `json:"name"`
		Blocked string `json:"blocked"`
	} `json:"exports"`
}

// Names lists the exports the bridge carries.
func (m manifest) Names() []string {
	var out []string
	for _, e := range m.Exports {
		if e.Blocked == "" {
			out = append(out, e.Name)
		}
	}
	return out
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	raw, err := os.ReadFile(filepath.Join(dir, "go-anything.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(raw, &m)
}

// checkScript replays cases through the wrapper a caller would use, so the bridge itself is tested:
// the addon and the stdio process must answer exactly what the recorded cases expect.
const checkScript = `import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const port = require(process.argv[2] + '/index.cjs')

const decode = v => {
  if (v && typeof v === 'object') {
    if (v.$number === 'NaN') return NaN
    if (v.$number === 'Infinity') return Infinity
    if (v.$number === '-Infinity') return -Infinity
    if (v.$number === '-0') return -0
    if (v.$bigint) return BigInt(v.$bigint)
    if (Array.isArray(v.$utf16)) return String.fromCharCode(...v.$utf16)
    if (v.$undefined) return undefined
    if (Array.isArray(v)) return v.map(decode)
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

// canon sorts keys: a Go map marshals alphabetically, and JSON.stringify is order-sensitive, so a
// deep compare has to normalise both sides first.
const canon = v => {
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']'
  if (v && typeof v === 'object') {
    return '{' + Object.keys(v).sort().map(k => JSON.stringify(k) + ':' + canon(v[k])).join(',') + '}'
  }
  return JSON.stringify(v === undefined ? null : v)
}

const inProcess = port.inProcess === true
const cases = (await readFile(process.argv[3], 'utf8')).split('\n').filter(Boolean).map(JSON.parse)
let matched = 0
const bad = []
for (const c of cases) {
  try {
    const args = (c.args ?? []).map(decode)
    const got = inProcess ? port.callSync(c.fn, ...args) : await port.call(c.fn, args)
    // The recorded answer carries wire markers too: decode before comparing.
    const want = decode(c.ok)
    if (canon(got) === canon(want)) matched++
    else if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want, got })
  } catch (e) {
    if (c.error && String(e.message).split(':')[0] === c.error.split(':')[0]) matched++
    else if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want: c.error, got: String(e.message) })
  }
}
console.log(JSON.stringify({ matched, total: cases.length, bad, inProcess }))
port.close?.()
`

// runCheck runs the replay above and reports what the wrapper answered.
func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("check takes one crate folder")
	}
	dir, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "work", "check.mjs")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(script, []byte(checkScript), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("node", script, dir, filepath.Join(dir, "cases.jsonl"))
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("replaying through the wrapper: %w", err)
	}
	var res struct {
		Matched   int  `json:"matched"`
		Total     int  `json:"total"`
		InProcess bool `json:"inProcess"`
		Bad       []struct {
			Fn   string `json:"fn"`
			Want any    `json:"want"`
			Got  any    `json:"got"`
		} `json:"bad"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return fmt.Errorf("reading the replay result: %w", err)
	}
	path := "stdio process"
	if res.InProcess {
		path = "in-process addon"
	}
	fmt.Printf("%d/%d cases through the %s.\n", res.Matched, res.Total, path)
	for _, b := range res.Bad {
		fmt.Printf("- %s: want %v, got %v\n", b.Fn, b.Want, b.Got)
	}
	if res.Matched != res.Total {
		return fmt.Errorf("check: %d case(s) differ through the bridge", res.Total-res.Matched)
	}
	return nil
}

// runEnable inserts the block that rebinds the unit's exports to the Go port.
func runEnable(args []string) error {
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	only := fs.String("only", "", "comma-separated export names to switch")
	async := fs.Bool("async", false, "await the stdio bridge instead of calling in-process")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("enable takes one crate folder")
	}
	dir := fs.Arg(0)
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	var names []string
	if *only != "" {
		names = strings.Split(*only, ",")
	}
	mode := switched.Sync
	if *async {
		mode = switched.Async
	}
	path, err := switched.Enable(switched.Options{
		CrateDir: dir, Unit: m.Unit, Crate: m.Crate, Exports: m.Names(), Mode: mode, Only: names,
	})
	if err != nil {
		return err
	}
	on, err := switched.IsOn(dir, m.Unit, m.Crate)
	if err != nil {
		return err
	}
	if !on {
		return fmt.Errorf("the block did not land in %s", path)
	}
	fmt.Printf("%s now calls the Go port (%s mode).\n", filepath.ToSlash(m.Unit), mode)
	fmt.Printf("  %s\n", path)
	fmt.Println("`go-anything disable` puts the file back byte for byte.")
	return nil
}

// runDisable removes the block and says whether there was one.
func runDisable(args []string) error {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("disable takes one crate folder")
	}
	dir := fs.Arg(0)
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	path, removed, err := switched.Disable(dir, m.Unit, m.Crate)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("%s was not switched on: nothing to remove.\n", filepath.ToSlash(m.Unit))
		return nil
	}
	fmt.Printf("Removed the block from %s.\n", path)
	return nil
}

// runShadow writes the shadow module, or summarizes what it has seen.
func runShadow(args []string, reportOnly bool) error {
	fs := flag.NewFlagSet("shadow", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("shadow takes one crate folder")
	}
	dir := fs.Arg(0)
	if reportOnly {
		rep, err := shadow.ReadReport(dir)
		if err != nil {
			return err
		}
		fmt.Print(rep.Markdown())
		if rep.Lines > 0 {
			return fmt.Errorf("shadow: %d mismatch(es) recorded", rep.Lines)
		}
		return nil
	}
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	path, err := shadow.Run(shadow.Options{CrateDir: dir, Unit: m.Unit, Exports: m.Names()})
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s for %d export(s).\n", path, len(m.Names()))
	fmt.Println("Require it where you require the original: it answers the original's value and logs differences.")
	fmt.Println("`go-anything shadow-report` summarizes shadow.jsonl.")
	return nil
}

// runFuzz looks for the difference the recording missed.
func runFuzz(args []string) error {
	fs := flag.NewFlagSet("fuzz", flag.ExitOnError)
	count := fs.Int("count", 50, "fresh calls per function")
	seed := fs.Int64("seed", 0, "seed to replay a run; random when left out")
	keep := fs.Bool("keep", false, "append what failed to cases.jsonl, so diff and gate check it from now on")
	tolerance := fs.Float64("tolerance", 1e-9, "numbers match within this relative tolerance")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("fuzz takes one crate folder")
	}
	res, err := fuzz.Run(fuzz.Options{
		CrateDir: fs.Arg(0), Count: *count, Seed: *seed, Keep: *keep, Tolerance: *tolerance,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%d/%d fresh calls match (seed %d).\n", res.Matched, res.Calls, res.Seed)
	if len(res.Mismatch) > 0 {
		fmt.Println()
		for _, m := range res.Mismatch {
			args, _ := json.Marshal(m.Args)
			fmt.Printf("- `%s(%s)`: expected %s, got %s (%s)\n", m.Fn, string(args[1:len(args)-1]),
				m.Expected, m.Got, m.Why)
		}
	}
	if len(res.Untouched) > 0 {
		fmt.Printf("stays in the original: %s\n", strings.Join(res.Untouched, ", "))
	}
	if res.Kept > 0 {
		fmt.Printf("kept %d failing call(s) in cases.jsonl: `diff` and `gate` now check them.\n", res.Kept)
	}
	if res.Matched != res.Calls {
		return fmt.Errorf("fuzz: %d of %d fresh calls differ (replay with --seed %d)",
			res.Calls-res.Matched, res.Calls, res.Seed)
	}
	return nil
}

// runRefresh regenerates everything but port.go from the manifest: the stubs a person filled in stay.
func runRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("refresh takes one crate folder")
	}
	dir := fs.Arg(0)
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	unit, err := unitFrom(dir, m.Unit)
	if err != nil {
		return err
	}
	// The port's own file is never rewritten here: it holds the work.
	files, err := scaffold.Run(scaffold.Options{CrateDir: dir, Unit: unit, Crate: m.Crate, SkipPort: true})
	if err != nil {
		return err
	}
	fmt.Printf("Regenerated %d file(s) from %s; port.go was left alone.\n", len(files), m.Unit)
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.cjs")); err == nil {
		if err := buildBridge(dir, m); err != nil {
			return err
		}
	}
	return nil
}

// buildBridge rewrites the host wrappers when the crate already had them.
func buildBridge(dir string, m manifest) error {
	files, err := bridge.Run(bridge.Options{CrateDir: dir, Crate: m.Crate, Unit: m.Unit, Exports: m.Names()})
	if err != nil {
		return err
	}
	fmt.Printf("Rewrote %d wrapper file(s).\n", len(files))
	return nil
}

// runReport writes the two files a pull request wants.
func runReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	out := fs.String("out", "", "output folder; defaults to the crate")
	minMS := fs.Int("min-ms", 100, "minimum measured time per function and side")
	skip := fs.Bool("skip-bench", false, "only the case numbers, no timing")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("report takes one crate folder")
	}
	card, err := report.Run(report.Options{
		CrateDir: fs.Arg(0), Out: *out, MinMS: *minMS, SkipBench: *skip,
	})
	if err != nil {
		return err
	}
	fmt.Print(card.Text())
	fmt.Printf("Wrote %s and %s\n", card.Markdown, card.SVG)
	return nil
}

// runPackage lays out the installable packages and the workflow that builds the other platforms.
func runPackage(args []string) error {
	fs := flag.NewFlagSet("package", flag.ExitOnError)
	out := fs.String("out", "", "output folder; defaults to <crate>/package")
	name := fs.String("name", "", "package name; defaults to the crate folder")
	version := fs.String("version", "", "package version; defaults to 0.1.0 with a note")
	shape := fs.String("shape", "library", "library, cli or service")
	original := fs.String("original", "", "for a CLI: the npm package to fall back to, as name@version")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("package takes one crate folder")
	}
	res, err := pkg.Run(pkg.Options{
		CrateDir: fs.Arg(0), Out: *out, Name: *name, Version: *version,
		Shape: *shape, Original: *original,
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s %s (%s) → %s\n", res.Name, res.Version, res.Shape, res.Out)
	for _, f := range res.Files {
		fmt.Printf("  %s\n", f)
	}
	for _, n := range res.Notes {
		fmt.Printf("note: %s\n", n)
	}
	fmt.Println("Nothing was published: the workflow builds the other platforms, and publishing is yours.")
	return nil
}

// runApp records a whole program, or replays a build against what was recorded.
func runApp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("app takes `record` or `replay`")
	}
	what := args[0]
	fs := flag.NewFlagSet("app "+what, flag.ExitOnError)
	command := fs.String("command", "", "the program to run")
	prefix := fs.String("args", "", "comma-separated arguments every case gets first")
	inputs := fs.String("inputs", "", "JSONL of extra cases: {\"name\":…, \"argv\":[…], \"stdin\":…}")
	runs := fs.Int("runs", 2, "how many times to run each case while recording")
	timeout := fs.Duration("timeout", 30*time.Second, "how long one run may take")
	fs.Parse(args[1:])
	if fs.NArg() != 1 {
		return fmt.Errorf("app %s takes one repository root", what)
	}
	var argv []string
	if *prefix != "" {
		argv = strings.Split(*prefix, ",")
	}
	opts := app.Options{
		Repo: fs.Arg(0), Command: *command, Args: argv, Inputs: *inputs,
		Runs: *runs, Timeout: *timeout,
	}
	var (
		res app.Outcome
		err error
	)
	switch what {
	case "record":
		res, err = app.Record(opts)
	case "replay":
		res, err = app.Replay(opts)
	default:
		return fmt.Errorf("app takes `record` or `replay`, not %q", what)
	}
	if err != nil {
		return err
	}
	fmt.Print(res.Markdown())
	if what == "record" {
		fmt.Printf("Wrote %d case(s) to %s\n", res.Cases, res.Dir)
	}
	if len(res.Failed) > 0 {
		return fmt.Errorf("app replay: %d of %d cases differ", len(res.Failed), res.Cases)
	}
	return nil
}

// runProfile runs a workload under the profiler and prints what it found.
func runProfile(args []string) error {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	command := fs.String("command", "", "the workload: a script node runs, with its arguments")
	nodeArg := fs.String("node-arg", "", "extra argument for node, e.g. --import=tsx")
	top := fs.Int("top", 12, "hot functions to show")
	timeout := fs.Duration("timeout", 5*time.Minute, "how long the workload may take")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("profile takes one repository root")
	}
	var nodeArgs []string
	if *nodeArg != "" {
		nodeArgs = strings.Split(*nodeArg, ",")
	}
	rep, err := profile.Run(profile.Options{
		Repo: fs.Arg(0), Command: strings.Fields(*command), NodeArgs: nodeArgs,
		Timeout: *timeout, Top: *top,
	})
	if err != nil {
		return err
	}
	fmt.Print(rep.Markdown())
	return nil
}

// runGo is the one-command path: profile a workload, take the hottest portable unit, and set it up.
func runGo(args []string) error {
	fs := flag.NewFlagSet("go", flag.ExitOnError)
	command := fs.String("command", "", "the workload that shows what is hot")
	unit := fs.String("unit", "", "port this unit instead of the hottest one")
	crate := fs.String("crate", "", "crate folder to create; defaults to rust/<unit>")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("go takes one repository root")
	}
	repo := fs.Arg(0)
	target := *unit
	if target == "" {
		rep, err := profile.Run(profile.Options{Repo: repo, Command: strings.Fields(*command)})
		if err != nil {
			return err
		}
		fmt.Print(rep.Markdown())
		for _, h := range rep.Hot {
			if h.Unit != "" && h.Portable == "" {
				target = h.Unit
				break
			}
		}
		if target == "" {
			return fmt.Errorf("nothing portable ran: pass --unit to choose one")
		}
		fmt.Printf("\nHottest portable unit: `%s`\n\n", target)
	}
	plan, err := analyze.Run(repo)
	if err != nil {
		return err
	}
	var chosen *analyze.Unit
	for i := range plan.Units {
		if plan.Units[i].ID == target {
			chosen = &plan.Units[i]
		}
	}
	if chosen == nil {
		return fmt.Errorf("no unit %q in %s", target, repo)
	}
	if chosen.Bridgeable == 0 {
		return fmt.Errorf("`%s` has no export a bridge can carry: %s", target, strings.Join(chosen.Notes, "; "))
	}
	out := *crate
	if out == "" {
		out = filepath.Join(repo, "go-ports", strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))+"-go")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// A crate needs an import path: use the module the repository already declares, or a placeholder.
	cratePath := "example.com/" + filepath.Base(repo) + "/" + filepath.Base(out)
	if raw, err := os.ReadFile(filepath.Join(repo, "go.mod")); err == nil {
		if module := moduleOf(string(raw)); module != "" {
			rel, err := filepath.Rel(repo, out)
			if err == nil && !strings.HasPrefix(rel, "..") {
				cratePath = module + "/" + filepath.ToSlash(rel)
			}
		}
	}
	files, err := scaffold.Run(scaffold.Options{CrateDir: out, Unit: *chosen, Crate: cratePath})
	if err != nil {
		return err
	}
	sum, err := record.Run(record.Options{CrateDir: out, Unit: *chosen})
	if err != nil {
		return err
	}
	wrappers, err := bridge.Run(bridge.Options{
		CrateDir: out, Crate: cratePath, Unit: chosen.ID, Exports: exportNames(*chosen),
	})
	if err != nil {
		return err
	}
	fmt.Printf("Scaffolded %d file(s), recorded %d cases, wrote %d wrapper file(s) for `%s`.\n",
		len(files), sum.Cases, len(wrappers), target)
	fmt.Printf("Next: fill in %s, then `go-anything gate %s`.\n", filepath.Join(out, "port.go"), out)
	return nil
}

// moduleOf reads the module path from a go.mod.
func moduleOf(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// exportNames lists the exports a crate's bridge can carry.
func exportNames(u analyze.Unit) []string {
	var out []string
	for _, e := range u.Exports {
		if e.Blocked == "" {
			out = append(out, e.Name)
		}
	}
	return out
}
