// Command golang-anything is the Go counterpart of rust-anything: it plans, records, scaffolds and
// proves a port into Go, one unit at a time.
package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/XiaoBinGan/golang-anything/internal/usage"
	"github.com/XiaoBinGan/golang-anything/internal/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/XiaoBinGan/golang-anything/internal/analyze"
	"github.com/XiaoBinGan/golang-anything/internal/app"
	"github.com/XiaoBinGan/golang-anything/internal/bench"
	"github.com/XiaoBinGan/golang-anything/internal/bridge"
	"github.com/XiaoBinGan/golang-anything/internal/buildinfo"
	"github.com/XiaoBinGan/golang-anything/internal/diff"
	"github.com/XiaoBinGan/golang-anything/internal/fastbridge"
	"github.com/XiaoBinGan/golang-anything/internal/frontdoor"
	"github.com/XiaoBinGan/golang-anything/internal/fuzz"
	"github.com/XiaoBinGan/golang-anything/internal/jswire"
	"github.com/XiaoBinGan/golang-anything/internal/mapdoc"
	"github.com/XiaoBinGan/golang-anything/internal/mcp"
	"github.com/XiaoBinGan/golang-anything/internal/parsehelper"
	"github.com/XiaoBinGan/golang-anything/internal/pkg"
	"github.com/XiaoBinGan/golang-anything/internal/profile"
	"github.com/XiaoBinGan/golang-anything/internal/promote"
	"github.com/XiaoBinGan/golang-anything/internal/record"
	"github.com/XiaoBinGan/golang-anything/internal/report"
	"github.com/XiaoBinGan/golang-anything/internal/scaffold"
	"github.com/XiaoBinGan/golang-anything/internal/shadow"
	"github.com/XiaoBinGan/golang-anything/internal/switched"
	"github.com/XiaoBinGan/golang-anything/internal/toolchain"
	"github.com/XiaoBinGan/golang-anything/internal/ts"
	"github.com/XiaoBinGan/golang-anything/internal/wasmbridge"
)

// version is what the tool says about itself: the module version when the binary carries one, and the
// shape of the build otherwise.

func main() { realMain() }

// realMain is the dispatch, split out so a test can call it with arguments of its own.
func realMain() {
	// A cgo build of this tool answers parse requests itself: that is what makes the fast build's
	// sidecar free — it is the same program, started for one thing.
	if len(os.Args) > 1 && os.Args[1] == "__parse" {
		parsehelper.Serve()
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage.Text)
		os.Exit(2)
	}
	// A tool that cannot say what version it is makes every bug report a guess. `--version` and
	// `version` both answer, and neither needs the parser.
	switch os.Args[1] {
	case "--version", "-version", "version":
		fmt.Println(version.String())
		return
	case "--help", "-h", "help":
		// WriteString, not Fprint: the fmt printer lazily builds its own state on first use, and that
		// one-time cost (tens of microseconds) is most of what --help spends inside main.
		os.Stdout.WriteString(usage.Text)
		return
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
	case "wasm":
		err = runWasm(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	case "promote":
		err = runPromote(os.Args[2:])
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
	case "front-door":
		err = runFrontDoor(os.Args[2:])
	case "profile":
		err = runProfile(os.Args[2:])
	case "go":
		err = runGo(os.Args[2:])
	case "mcp":
		err = runMCP()
	case "setup":
		err = runSetup(os.Args[2:])
	case "-h", "--help", "help":
		os.Stdout.WriteString(usage.Text)
		return
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usage.Text)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "golang-anything:", err)
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
	// What is here that this tool cannot read, before the table: an empty plan with no explanation reads
	// as an empty repository.
	if len(rep.Unsupported) > 0 {
		kinds := make([]string, 0, len(rep.Unsupported))
		total := 0
		for ext, n := range rep.Unsupported {
			kinds = append(kinds, fmt.Sprintf("%s ×%d", ext, n))
			total += n
		}
		sort.Strings(kinds)
		fmt.Printf("%d source file(s) in languages this tool does not read: %s\n"+
			"  it reads TypeScript, TSX, JavaScript and Python.\n\n", total, strings.Join(kinds, ", "))
	}
	fmt.Printf("%d units with something a bridge can carry.\n\n", len(rep.Units))
	fmt.Printf("| Unit | Lines | Exports | Wave | Readiness | Priority |\n| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, u := range rep.Units {
		fmt.Printf("| `%s` | %d | %d | %d | %d | %d |\n", u.ID, u.Loc, len(u.Exports), u.Wave, u.Readiness, u.Priority)
	}
	if len(rep.StartHere) > 0 {
		fmt.Printf("\nStart here: %s\n", strings.Join(rep.StartHere, ", "))
	}
	// What cannot be ported, and why, before a thousand per-file sections: this is the list a person
	// acts on when the repository is large.
	if reasons := rep.Reasons(); len(reasons) > 0 {
		type row struct {
			why string
			n   int
		}
		rows := make([]row, 0, len(reasons))
		for why, n := range reasons {
			rows = append(rows, row{why, n})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].why < rows[j].why
		})
		fmt.Printf("\n## Why exports stay behind\n\n| Reason | Exports |\n| --- | ---: |\n")
		for _, r := range rows {
			fmt.Printf("| %s | %d |\n", r.why, r.n)
		}
	}
	// The notes were computed and only ever read in JSON: a person looking at the table saw "0 exports" with
	// no sentence about the six instance methods behind it. `analyze --json` had them all along.
	noteLines := 0
	for _, u := range rep.Units {
		if len(u.Notes) > 0 {
			noteLines++
		}
	}
	if noteLines > 0 {
		fmt.Printf("\n## Notes\n\n")
		for _, u := range rep.Units {
			for _, n := range u.Notes {
				fmt.Printf("- `%s`: %s\n", u.ID, n)
			}
		}
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
	// cmd/godriver names port.Undefined, which answer.go declares, and answer.go is written by `bridge`. A
	// `go build ./...` between the two steps answers `undefined: port.Undefined` with nothing pointing at the
	// missing step.
	fmt.Println("\nNext: `golang-anything bridge " + filepath.ToSlash(dir) + "` writes answer.go, which " +
		"cmd/godriver needs; build after that.")
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
	// Only an export that really stays behind gets that line: the map now names every export, portable
	// ones with an empty reason.
	for name, why := range blocked {
		if why != "" {
			fmt.Printf("stays in the original: %s (%s)\n", name, why)
		}
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
	raw, err := os.ReadFile(filepath.Join(crateDir, "golang-anything.json"))
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
	// Every export goes in, with an empty reason when it is portable: the gate's "no recorded case"
	// rule reads this map, and a crate with an empty cases.jsonl passed while only blocked exports were
	// listed.
	blocked := map[string]string{}
	for _, e := range m.Exports {
		throws[e.Name] = len(e.ThrowLines)
		blocked[e.Name] = e.Blocked
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
	cmd := exec.Command(toolchain.Go(), "build", "-o", out, "./cmd/portctl")
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
	// The driver is what `diff` runs in a loop: build it here so the comparison does not pay a compile.
	driver, err := filepath.Abs(filepath.Join(dir, "work", "godriver"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(driver), 0o755); err != nil {
		return err
	}
	buildDriver := exec.Command("go", "build", "-o", driver, "./cmd/godriver")
	buildDriver.Dir = dir
	buildDriver.Stderr = os.Stderr
	if err := buildDriver.Run(); err != nil {
		return fmt.Errorf("building the driver: %w", err)
	}
	fmt.Printf("Built %s: `diff` now runs the comparison without compiling first.\n", driver)
	if *fast {
		return buildFast(dir, *nodeInclude)
	}
	fmt.Println("Copy it next to index.cjs (or set GOLANG_ANYTHING_PORTCTL) and the wrappers will use it.")
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
	raw, err := os.ReadFile(filepath.Join(dir, "golang-anything.json"))
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
	asJSON := fs.Bool("json", false, "print the measurement as JSON")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("bench takes one crate folder")
	}
	res, err := bench.Run(bench.Options{CrateDir: fs.Arg(0), MinMS: *minMS, Repeat: *repeat})
	if err != nil {
		return err
	}
	if *asJSON {
		// The table is for a person; a number that another tool has to read should not be scraped out of
		// markdown. `report` and the sweep both want one row per function, and the rows already carry their
		// own JSON tags.
		raw, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
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
	// bench times a C or C++ unit by compiling it, so the compiler belongs in the list: without it the run
	// ends at `compiling the unit and the timing harness: exec: "clang": executable file not found in $PATH`,
	// which names the gap only after the user has already asked for a number.
	probe("clang (bench)", "clang", false, "xcode-select --install, or install LLVM: only needed to bench C and C++ units")
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
	// A GOROOT that does not match the `go` binary compiles nothing, and the error it gives names neither:
	// "compile: version go1.22.5 does not match go tool version go1.26.2". It cost an hour here, so the
	// check compiles something instead of trusting `go env`.
	{
		dir, err := os.MkdirTemp("", "golang-anything-doctor")
		if err == nil {
			defer os.RemoveAll(dir)
			src := filepath.Join(dir, "main.go")
			_ = os.WriteFile(src, []byte("package main\n\nfunc main() {}\n"), 0o644)
			_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.24\n"), 0o644)
			build := exec.Command("go", "build", "-o", filepath.Join(dir, "probe"), ".")
			build.Dir = dir
			var stderr strings.Builder
			build.Stderr = &stderr
			runErr := build.Run()
			c := check{Name: "go builds a program", OK: runErr == nil}
			if runErr != nil {
				c.Note = strings.TrimSpace(firstLine(stderr.String()))
				if c.Note == "" {
					c.Note = runErr.Error()
				}
				c.Fix = "the go binary and its GOROOT disagree: set GOROOT to the installation the binary " +
					"came from (export GOROOT=$(brew --prefix go)/libexec on Homebrew)"
			}
			checks = append(checks, c)
		}
	}
	// The parser is a second build of this tool. A build made with cgo is its own parser; a fast build
	// needs one next to it or named by GOLANG_ANYTHING_PARSE.
	{
		ok, note := true, "this build parses TypeScript itself (cgo)"
		if !buildinfo.Cgo {
			// Ask the code that will actually start the helper, not a second copy of its search: doctor
			// reported a missing parser for a setup that worked, because it looked in fewer places.
			if path, err := ts.ParserPath(); err == nil {
				ok, note = true, path
			} else {
				ok = false
				note = "no cgo in this build: set GOLANG_ANYTHING_PARSE or put golang-anything-cgo beside it"
			}
		}
		checks = append(checks, check{
			Name: "parser available", OK: ok, Note: note,
			Fix: "CGO_ENABLED=1 go build -o golang-anything-cgo . — needed by analyze, record, scaffold and refresh",
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

// manifest is what the CLI needs from golang-anything.json.
type manifest struct {
	Crate   string   `json:"crate"`
	Unit    string   `json:"unit"`
	Exports []export `json:"exports"`
}

// export is one manifest entry: what the bridge carries and what shape it has.
type export struct {
	Name       string   `json:"name"`
	Params     []string `json:"params"`
	Returns    string   `json:"returns"`
	Blocked    string   `json:"blocked"`
	ThrowLines []int    `json:"throw_lines"`
	Receiver   string   `json:"receiver,omitempty"`
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
	raw, err := os.ReadFile(filepath.Join(dir, "golang-anything.json"))
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
%JSWIRE_CANON%

// show spells a value the way a person can tell apart: -0 is not 0, NaN is not null.
const show = v => {
  if (v === undefined) return 'undefined'
  if (typeof v === 'number') {
    if (Number.isNaN(v)) return 'NaN'
    if (Object.is(v, -0)) return '-0'
    if (!Number.isFinite(v)) return String(v)
    return String(v)
  }
  if (typeof v === 'bigint') return v.toString() + 'n'
  if (Array.isArray(v)) return '[' + v.map(show).join(',') + ']'
  if (v && typeof v === 'object') return '{' + Object.keys(v).sort().map(k => k + ':' + show(v[k])).join(',') + '}'
  return JSON.stringify(v)
}

const inProcess = port.inProcess === true
const cases = (await readFile(process.argv[3], 'utf8')).split('\n').filter(Boolean).map(JSON.parse)
// Two calls per case, so matched counts calls, not cases: reporting it against the case count is how the
// first version printed "-82 case(s) differ" for a crate that had simply gained a second pass.
let matched = 0
let calls = 0
const bad = []
// Counted over every call, not over the bad list: that list stops at five, and "3 of them are the scaffold"
// when seventeen functions have not been written is the kind of number this file keeps having to correct.
let unported = 0
const unportedFns = new Set()
for (const c of cases) {
  try {
    // The arguments go to the bridge as they were recorded, markers and all: decoding them here turns
    // {"$number":"-0"} into -0, and JSON.stringify([-0]) is "[0]" — the marker never reaches the port,
    // which is how isNegativeZero answered false through the bridge and true through the driver.
    // Two calls per case, because they exercise different halves of the wire:
    //   the markers as recorded — the path the driver takes, and what check has always done;
    //   the values decoded from those markers — the path a real caller takes, where the wrapper has to
    //   encode live -0, NaN and undefined or the port never sees them.
    // Only the second one catches a broken encode, and the first version of this file only did the first:
    // removing the -0 branch from index.cjs left check green while a real call answered false.
    const asRecorded = c.args ?? []
    const asValue = decode(c.args ?? [])
    const want = decode(c.ok)
    for (const args of [asRecorded, asValue]) {
      calls++
      const got = inProcess ? port.callSync(c.fn, ...args) : await port.call(c.fn, args)
      if (canon(got) === canon(want)) { matched++; continue }
      // A mismatch has to be readable: "want 0, got 0" is what a -0 against 0 looks like when the values
      // are printed the ordinary way.
      const why = show(got)
      if (String(why).includes('not implemented: port ')) { unported++; unportedFns.add(c.fn) }
      if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want: show(want), got: why })
      break
    }
  } catch (e) {
    if (c.error && String(e.message).split(':')[0] === c.error.split(':')[0]) matched++
    else {
      const why = String(e.message)
      if (why.includes('not implemented: port ')) { unported++; unportedFns.add(c.fn) }
      if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want: c.error, got: why })
    }
  }
}
console.log(JSON.stringify({ matched, total: calls, cases: cases.length, bad, inProcess, unported, unportedFns: [...unportedFns] }))
port.close?.()
`

// errorLine is the line of a node crash a person can act on: the `Error: …` line, not the frame above it.
// `check` reported `exit status 1` on an unbridged crate until this, and the cause — `Cannot find module
// '…/index.cjs'` — was sitting in stderr the whole time.
func errorLine(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Error:") {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(firstLine(stderr))
}

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
	text := strings.Replace(checkScript, "%JSWIRE_CANON%", jswire.Canon, 1)
	if err := os.WriteFile(script, []byte(text), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("node", script, dir, filepath.Join(dir, "cases.jsonl"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// The wrapper dies before printing anything when the port is still the scaffold, and `check` used to
		// answer that with `exit status 1` — a message that names neither the port nor the function.
		if strings.Contains(stderr.String(), "not implemented: port ") {
			return fmt.Errorf("check: the port is still the scaffold — every call answers "+
				"`not implemented: port …`, so there is nothing to compare: fill in %s",
				filepath.ToSlash(filepath.Join(dir, "port.go")))
		}
		if detail := errorLine(stderr.String()); detail != "" {
			return fmt.Errorf("replaying through the wrapper: %w (%s)", err, detail)
		}
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
		Unported    int      `json:"unported"`
		UnportedFns []string `json:"unportedFns"`
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
		// A crate whose port was never written and a crate whose port is wrong both read as "cases differ",
		// and they need different work. The scaffold's own words separate them.
		if res.Unported > 0 {
			names := append([]string(nil), res.UnportedFns...)
			sort.Strings(names)
			return fmt.Errorf("check: %d case(s) differ through the bridge; %d of them because the port still "+
				"answers `not implemented: port …` (%s)", res.Total-res.Matched, res.Unported, strings.Join(names, ", "))
		}
		return fmt.Errorf("check: %d case(s) differ through the bridge", res.Total-res.Matched)
	}
	return nil
}

// runPromote writes the Ruby shim that makes a state-free instance method callable as Owner.name(...).
//
// Nothing is invented: the methods come from the same parse `analyze` reports, and only the ones it calls
// promotable — a class, and a body that reads no state. A module's methods are never offered because a module
// cannot be instantiated, and the shim would answer NoMethodError on the first call.
func runPromote(args []string) error {
	fs := flag.NewFlagSet("promote", flag.ExitOnError)
	revert := fs.Bool("revert", false, "remove the shim block instead of writing it")
	unit := fs.String("unit", "", "the unit to promote; read from the manifest when it exists")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("promote takes one crate folder")
	}
	dir := fs.Arg(0)
	// A library that is all instance methods has no bridgeable export, so `scaffold` refuses to write a
	// manifest — and promotion is exactly what would give it one. `--unit` is the way in before that exists.
	m, err := readManifest(dir)
	if err != nil {
		if *unit == "" {
			return fmt.Errorf("%w; pass --unit to promote before a manifest exists", err)
		}
		m = manifest{Unit: *unit}
	}
	if *unit != "" {
		m.Unit = *unit
	}
	root, err := unitRoot(dir, m.Unit)
	if err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(m.Unit))
	if *revert {
		removed := 0
		for _, target := range []string{path, shimTarget(path)} {
			raw, err := os.ReadFile(target)
			if err != nil {
				continue
			}
			for _, match := range promoteMarker.FindAllStringSubmatch(string(raw), -1) {
				ok, err := promote.Revert(target, match[1])
				if err != nil {
					return err
				}
				if ok {
					removed++
					fmt.Printf("Removed the %s shim from %s.\n", match[1], filepath.ToSlash(target))
				}
			}
		}
		if removed == 0 {
			fmt.Printf("%s has no shim: nothing to remove.\n", filepath.ToSlash(path))
		}
		// A proxy file that held nothing but its header is not part of the original: leaving it behind makes the
		// revert visibly incomplete.
		if target := shimTarget(path); target != path {
			if raw, err := os.ReadFile(target); err == nil && strings.TrimSpace(string(raw)) == "<?php" {
				_ = os.Remove(target)
				fmt.Printf("Removed %s: it held only the proxy.\n", filepath.ToSlash(target))
			}
		}
		return nil
	}
	// The shim is a reopening of the class, and only Ruby lets a class be reopened. Writing the Ruby block into a
	// PHP file (which is what this command did before the guard) produces a file that no longer parses, reported
	// as success — `class Timer` twice is a fatal error in PHP, and PHP's only route is a generated proxy class
	// plus a recorder that knows to call it, which does not exist yet.
	if err := promoteSupported(path, m.Unit); err != nil {
		return err
	}
	file, err := ts.Parse(path)
	if err != nil {
		return err
	}
	byOwner := map[string][]string{}
	for _, p := range file.Promotable {
		byOwner[p.Owner] = append(byOwner[p.Owner], p.Name)
	}
	if len(byOwner) == 0 {
		return fmt.Errorf("nothing to promote in %s: no class has a state-free instance method", m.Unit)
	}
	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	target := shimTarget(path)
	for _, owner := range owners {
		names := byOwner[owner]
		sort.Strings(names)
		insert := promote.Insert
		if strings.EqualFold(filepath.Ext(target), ".php") {
			insert = promote.InsertPHP
		}
		added, err := insert(target, owner, names)
		if err != nil {
			return err
		}
		if !added {
			fmt.Printf("%s already has a shim in %s.\n", owner, filepath.ToSlash(target))
			continue
		}
		if strings.EqualFold(filepath.Ext(target), ".php") {
			fmt.Printf("%sPromoted::%s now reaches the instance method through a proxy class (%s).\n",
				owner, strings.Join(names, ", "), filepath.ToSlash(target))
			continue
		}
		fmt.Printf("%s.%s now takes an instance through allocate (promoted).\n", owner, strings.Join(names, ", "))
	}
	return nil
}

// promoteSupported refuses a unit whose language has no shim. The shim is a reopening of the class, and only
// Ruby lets a class be reopened: PHP answers `class Timer` twice with a fatal error, so its route is a generated
// proxy class plus a recorder that calls it, and that does not exist yet.
func promoteSupported(path, unit string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".rb", ".php":
		return nil
	}
	return fmt.Errorf("promote has no shim for %s: Ruby reopens the class and PHP gets a proxy class, and no other "+
		"language is written yet", unit)
}

// shimTarget is the file a shim goes into. Ruby reopens the class in place; PHP cannot, so the proxy class lives in
// promoted.php beside the unit and `analyze` finds it as its own file.
func shimTarget(unitPath string) string {
	if strings.EqualFold(filepath.Ext(unitPath), ".php") {
		return filepath.Join(filepath.Dir(unitPath), "promoted.php")
	}
	return unitPath
}

// promoteMarker finds the owners a revert can undo, in the file itself: the shim is already there, and the
// manifest does not record it.
var promoteMarker = regexp.MustCompile(`(?m)^# >>> go-anything promote: (.+)$`)

// unitRoot walks up from the crate folder until the unit file resolves, the way every other command finds it.
func unitRoot(crateDir, unit string) (string, error) {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(unit))); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no folder above %s holds %s", crateDir, unit)
		}
		dir = parent
	}
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
	owned := make([]switched.Owned, 0, len(m.Exports))
	for _, e := range m.Exports {
		if e.Blocked != "" {
			continue
		}
		owned = append(owned, switched.Owned{Name: e.Name, Owner: e.Receiver})
	}
	path, err := switched.Enable(switched.Options{
		CrateDir: dir, Unit: m.Unit, Crate: m.Crate, Exports: m.Names(), Mode: mode, Only: names,
		Owners: owned,
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
	fmt.Println("`golang-anything disable` puts the file back byte for byte.")
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
	fmt.Println("`golang-anything shadow-report` summarizes shadow.jsonl.")
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
	// A call the original declined is neither a match nor a difference: it is left out of the count, and
	// said out loud, because "61 of 80" reads like nineteen regressions when nineteen calls were skipped.
	compared := res.Calls - res.Skipped
	fmt.Printf("%d/%d fresh calls match (seed %d).\n", res.Matched, compared, res.Seed)
	if res.Skipped > 0 {
		fmt.Printf("%d call(s) skipped: the original would not answer them within the limits.\n", res.Skipped)
	}
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
	if compared > 0 && res.Matched != compared {
		return fmt.Errorf("fuzz: %d of %d fresh calls differ (replay with --seed %d)",
			compared-res.Matched, compared, res.Seed)
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
	fmt.Printf("Next: fill in %s, then `golang-anything gate %s`.\n", filepath.Join(out, "port.go"), out)
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

// runMCP serves the protocol; nothing but frames may be written to stdout.
func runMCP() error {
	// The tools that already exist as commands are wired to those commands: the same code answers both
	// front ends, so a fix cannot land in one and miss the other.
	mcp.SetCommands(map[string]func(map[string]any) (string, error){
		"check": func(a map[string]any) (string, error) {
			return captured(func() error { return runCheck([]string{str(a, "crate")}) })
		},
		"doctor": func(a map[string]any) (string, error) {
			return captured(func() error { return runDoctor(nil) })
		},
		"bench": func(a map[string]any) (string, error) {
			return captured(func() error { return runBench([]string{str(a, "crate")}) })
		},
		"shadow-report": func(a map[string]any) (string, error) {
			return captured(func() error { return runShadow([]string{str(a, "crate")}, true) })
		},
		"enable": func(a map[string]any) (string, error) {
			return captured(func() error { return runEnable([]string{str(a, "crate")}) })
		},
		"disable": func(a map[string]any) (string, error) {
			return captured(func() error { return runDisable([]string{str(a, "crate")}) })
		},
		"clean": func(a map[string]any) (string, error) {
			return captured(func() error { return runClean([]string{str(a, "crate")}) })
		},
		"refresh": func(a map[string]any) (string, error) {
			return captured(func() error { return runRefresh([]string{str(a, "crate")}) })
		},
	})
	return mcp.Serve(os.Stdin, os.Stdout, os.Stderr)
}

// captured runs a command with stdout redirected, so a tool answers with the command's own output. The
// alternative — a second implementation of what the command does — is the drift this project keeps paying
// for, so the redirect is the cheaper honesty.
func captured(run func() error) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		io.Copy(&b, r)
		done <- b.String()
	}()
	runErr := run()
	os.Stdout = saved
	w.Close()
	out := <-done
	r.Close()
	if runErr != nil {
		return "", runErr
	}
	return out, nil
}

// str reads a string argument by name, or "" when it is absent.
func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// runSetup registers the MCP server with the coding agents found on this machine.
func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	agent := fs.String("agent", "all", "claude, cursor, codex, or all")
	print := fs.Bool("print", false, "only print the configuration")
	fs.Parse(args)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"golang-anything": map[string]any{"command": exe, "args": []string{"mcp"}},
		},
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if *print {
		fmt.Println(string(raw))
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	type target struct{ name, path string }
	targets := []target{
		{"claude", filepath.Join(home, ".claude.json")},
		{"cursor", filepath.Join(home, ".cursor", "mcp.json")},
	}
	var wrote []string
	for _, t := range targets {
		if *agent != "all" && *agent != t.name {
			continue
		}
		if *agent == "all" {
			// Only touch an agent that is already installed: writing a config for a tool that is not
			// here is clutter, not setup.
			dir := filepath.Dir(t.path)
			if _, err := os.Stat(dir); err != nil {
				continue
			}
		}
		if err := mergeServers(t.path, cfg); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		wrote = append(wrote, t.name+" ("+t.path+")")
	}
	if *agent == "codex" {
		return fmt.Errorf("codex reads TOML at ~/.codex/config.toml: add this by hand:\n%s", raw)
	}
	if len(wrote) == 0 {
		fmt.Println("No agent configuration found. Print the block and paste it where your agent keeps MCP servers:")
		fmt.Println(string(raw))
		return nil
	}
	for _, w := range wrote {
		fmt.Printf("Registered golang-anything with %s\n", w)
	}
	return nil
}

// mergeServers adds the server to an agent's JSON config without touching its other keys.
func mergeServers(path string, cfg map[string]any) error {
	existing := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("%s is not JSON: %w", path, err)
		}
	}
	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	mine, _ := cfg["mcpServers"].(map[string]any)
	for k, v := range mine {
		servers[k] = v
	}
	existing["mcpServers"] = servers
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// runFrontDoor scaffolds the front door, wires a port into it, or lifts the fallback away.
func runFrontDoor(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("front-door takes `new`, `wire` or `lift`")
	}
	what := args[0]
	fs := flag.NewFlagSet("front-door "+what, flag.ExitOnError)
	original := fs.String("original", "", "the program to fall back to; taken from the recording when empty")
	name := fs.String("name", "", "binary name; defaults to the repository folder")
	out := fs.String("out", "", "output folder; defaults to <repo>/go-app")
	key := fs.String("command", "", "for wire: the recorded command, e.g. \"--count\"")
	portModule := fs.String("port", "", "for wire: import path of the port crate")
	portFunc := fs.String("call", "", "for wire: the port function that answers the command")
	fs.Parse(args[1:])
	switch what {
	case "new":
		if fs.NArg() != 1 {
			return fmt.Errorf("front-door new takes one repository root")
		}
		res, err := frontdoor.Scaffold(frontdoor.Options{
			Repo: fs.Arg(0), Original: *original, Name: *name, Out: *out,
		})
		if err != nil {
			return err
		}
		fmt.Printf("Front door for `%s` in %s (%d recorded commands):\n", res.Original, res.Dir, len(res.Commands))
		for _, f := range res.Files {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println("Next: `golang-anything front-door wire --command … --port … --call … <dir>`, then build it.")
		return nil
	case "wire":
		given := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "command" {
				given = true
			}
		})
		if fs.NArg() != 1 || !given || *portFunc == "" {
			return fmt.Errorf("front-door wire needs --command and --call, and one front door folder")
		}
		if *portModule == "" {
			// Default to `<front door module>/portstore`: the wiring is inside this module, and a
			// path from outside it does not compile without a go.mod change.
			raw, err := os.ReadFile(filepath.Join(fs.Arg(0), "go.mod"))
			if err != nil {
				return fmt.Errorf("%s is not a front door: %w", fs.Arg(0), err)
			}
			module := moduleOf(string(raw))
			if module == "" {
				return fmt.Errorf("pass --port: %s/go.mod has no module line", fs.Arg(0))
			}
			*portModule = module + "/portstore"
		}
		path, err := frontdoor.Wire(fs.Arg(0), *key, handlerName(*key), "", *portModule, *portFunc)
		if err != nil {
			return err
		}
		fmt.Printf("Wired %q to %s.%s in %s\n", *key, *portModule, *portFunc, path)
		return nil
	case "lift":
		if fs.NArg() != 1 {
			return fmt.Errorf("front-door lift takes one front door folder")
		}
		path, err := frontdoor.Lift(fs.Arg(0))
		if err != nil {
			return err
		}
		fmt.Printf("Lifted: %s no longer carries the original.\n", path)
		return nil
	}
	return fmt.Errorf("front-door takes `new`, `wire` or `lift`, not %q", what)
}

// handlerName turns a command key into a Go identifier the wire step can name.
func handlerName(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "run"
	}
	return "run" + strings.ToUpper(b.String()[:1]) + b.String()[1:]
}

// runWasm builds the port as a WebAssembly reactor plus its wrapper.
func runWasm(args []string) error {
	fs := flag.NewFlagSet("wasm", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("wasm takes one crate folder")
	}
	dir := fs.Arg(0)
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	var typed []wasmbridge.Export
	for _, e := range m.Exports {
		if e.Blocked != "" {
			continue
		}
		typed = append(typed, wasmbridge.Export{Name: e.Name, Params: e.Params, Returns: e.Returns})
	}
	files, err := wasmbridge.Run(wasmbridge.Options{
		CrateDir: dir, Crate: m.Crate, Exports: m.Names(), Typed: typed,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Built the WebAssembly port for %d export(s):\n", len(m.Names()))
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			fmt.Printf("  %s (%.1f MB)\n", filepath.Base(f), float64(info.Size())/(1<<20))
			continue
		}
		fmt.Printf("  %s\n", f)
	}
	fmt.Println("`index.wasm.mjs` calls it in-process: no pipe, no addon, one function call per call.")
	return nil
}
