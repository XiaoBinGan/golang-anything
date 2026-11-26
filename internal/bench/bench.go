// Package bench times the recorded cases on both sides, bridge included.
//
// The number that matters is per call, not per run: a port that is fast in aggregate but pays 40 µs
// to cross the boundary is slower than the original for small functions. Both sides are measured the
// same way here — the same cases, the same warm-up, the same repetitions — so the ratio means
// something.
package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/cases"
	"github.com/XiaoBinGan/golang-anything/internal/nodeflags"
	"github.com/XiaoBinGan/golang-anything/internal/record"

	"github.com/XiaoBinGan/golang-anything/internal/toolchain"
)

// Options is one bench run.
type Options struct {
	CrateDir string
	Unit     string
	MinMS    int
	Repeat   int
}

// Row is one function's timing. The JSON tags are the shape both measurement scripts write, so one
// type reads either file.
type Row struct {
	Name       string  `json:"name"`
	Cases      int     `json:"cases"`
	NS         float64 `json:"ns"`
	OriginalNS float64 `json:"original_ns,omitempty"`
	PortNS     float64 `json:"port_ns,omitempty"`
	Speedup    float64 `json:"speedup,omitempty"`
	// Unported counts the calls whose answer was the scaffold's `not implemented: port …`. A row where
	// every call said that is not a port: it is a timing of fmt.Errorf, which read 17 ns / 8.83x "slower"
	// for a C function whose real port answers in 5 ns / 3.10x.
	Unported int `json:"unported,omitempty"`
}

// Result is the whole measurement.
type Result struct {
	Rows    []Row   `json:"rows"`
	Speedup float64 `json:"speedup"` // geometric mean over the rows
	MinMS   int     `json:"min_ms"`
	// Unported names the rows that were left out because every call answered the scaffold's
	// `not implemented: port …`. A crate with one ported function and one stub prints one row; without this
	// the missing second row is silent, and "the table is short" is not a reason a reader can act on.
	Unported []string `json:"unported,omitempty"`
}

// originalScript times the original: import the unit once, then each function over its own cases.
const originalScript = `import { pathToFileURL } from 'node:url'
import { readFile } from 'node:fs/promises'

const [unitPath, casesPath, outPath, minMsArg, repeatArg] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const minMs = Number(minMsArg)
const repeat = Number(repeatArg)
const all = (await readFile(casesPath, 'utf8')).split('\n').filter(Boolean).map(l => JSON.parse(l))

const decode = v => {
  if (v === undefined) return undefined
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

const byFn = new Map()
for (const c of all) {
  if (!byFn.has(c.fn)) byFn.set(c.fn, [])
  byFn.get(c.fn).push(c)
}

const rows = []
for (const [fn, list] of byFn) {
  const f = mod[fn]
  if (typeof f !== 'function') continue
  const args = list.map(c => (c.args ?? []).map(decode))
  // Warm up first: the first call pays for lazy initialisation that a real caller pays once.
  for (const a of args) { try { f(...a) } catch {} }
  let rounds = 0
  const started = process.hrtime.bigint()
  let elapsed = 0n
  do {
    for (let r = 0; r < repeat; r++) {
      for (const a of args) { try { f(...a) } catch {} }
    }
    rounds++
    elapsed = process.hrtime.bigint() - started
  } while (Number(elapsed) / 1e6 < minMs && rounds < 10000)
  const perCall = Number(elapsed) / (rounds * repeat * args.length)
  rows.push({ name: fn, cases: args.length, ns: perCall })
}
await (await import('node:fs/promises')).writeFile(outPath, JSON.stringify(rows))
`

// portScript is the Go side of the same measurement: it lives in cmd/godriver, which already knows how
// to answer one case; this asks it for the same cases in a loop and times the calls.
const portDriver = `// Command gobench times the port over the recorded cases, the way bench times the original.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	port "` + cratePlaceholder + `"
)

type testCase struct {
	Fn   string ` + "`json:\"fn\"`" + `
	Args []any  ` + "`json:\"args\"`" + `
}

type row struct {
	Name     string  ` + "`json:\"name\"`" + `
	Cases    int     ` + "`json:\"cases\"`" + `
	NS       float64 ` + "`json:\"ns\"`" + `
	Unported int     ` + "`json:\"unported,omitempty\"`" + `
}

func main() {
	in, err := os.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	defer in.Close()
	out := os.Args[2]
	minMs := 50
	repeat := 1
	fmt.Sscan(os.Args[3], &minMs)
	fmt.Sscan(os.Args[4], &repeat)

	var all []testCase
	dec := json.NewDecoder(in)
	for {
		var c testCase
		if err := dec.Decode(&c); err != nil {
			break
		}
		all = append(all, c)
	}
	byFn := map[string][][]any{}
	order := []string{}
	for _, c := range all {
		if _, ok := byFn[c.Fn]; !ok {
			order = append(order, c.Fn)
		}
		byFn[c.Fn] = append(byFn[c.Fn], port.DecodeArgs(c.Args))
	}
	var rows []row
	for _, fn := range order {
		f, ok := port.Registry[fn]
		if !ok {
			continue
		}
		args := byFn[fn]
		unported := 0
		for _, a := range args { // warm up; counting here keeps the timed loop free of a string check
			if _, err := f(a); err != nil && strings.Contains(err.Error(), "not implemented: port ") {
				unported++
			}
		}
		// Per round, then the median: one scheduling hiccup inside a single time.Now() around the whole loop
		// moved the answer by 40% (5 ns against 7 ns on the same crate, five runs), and a mean keeps that
		// hiccup. The median discards it, which is the difference between "this crate is 3 ns" and "this
		// crate was 3 ns on the run that did not get interrupted".
		started := time.Now()
		var samples []float64
		for {
			roundStart := time.Now()
			for i := 0; i < repeat; i++ {
				for _, a := range args {
					_, _ = f(a)
				}
			}
			samples = append(samples, float64(time.Since(roundStart).Nanoseconds())/float64(repeat*len(args)))
			if time.Since(started) >= time.Duration(minMs)*time.Millisecond || len(samples) >= 200000 {
				break
			}
		}
		sort.Float64s(samples)
		per := samples[len(samples)/2]
		rows = append(rows, row{Name: fn, Cases: len(args), NS: per, Unported: unported})
	}
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gobench:", err)
	os.Exit(1)
}
`

const cratePlaceholder = "__CRATE__"

// Run measures both sides and returns the rows.
func Run(opts Options) (Result, error) {
	dir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return Result{}, err
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return Result{}, err
	}
	minMS := opts.MinMS
	if minMS <= 0 {
		minMS = 100
	}
	repeat := opts.Repeat
	if repeat <= 0 {
		repeat = 1
	}
	casesPath := filepath.Join(dir, "cases.jsonl")
	if _, err := os.Stat(casesPath); err != nil {
		return Result{}, fmt.Errorf("no cases to time: %w", err)
	}

	// The manifest says where the unit and the crate are.
	raw, err := os.ReadFile(filepath.Join(dir, "golang-anything.json"))
	if err != nil {
		return Result{}, err
	}
	var m struct {
		Unit  string `json:"unit"`
		Crate string `json:"crate"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return Result{}, err
	}

	script := filepath.Join(work, "bench.mjs")
	if err := os.WriteFile(script, []byte(originalScript), 0o644); err != nil {
		return Result{}, err
	}
	unitPath, err := unitPath(dir, m.Unit)
	if err != nil {
		return Result{}, err
	}
	hooksPath, err := record.WriteResolver(work)
	if err != nil {
		return Result{}, err
	}
	origOut := filepath.Join(work, "bench-original.json")
	var cmd *exec.Cmd
	handled := false
	if strings.HasSuffix(m.Unit, ".c") || strings.HasSuffix(m.Unit, ".cpp") ||
		strings.HasSuffix(m.Unit, ".cc") || strings.HasSuffix(m.Unit, ".cxx") {
		cpp := !strings.HasSuffix(m.Unit, ".c")
		ext, compiler := ".c", "clang"
		if cpp {
			ext, compiler = ".cpp", "clang++"
		}
		harness := filepath.Join(work, "benchh"+ext)
		include := "#include \"" + strings.ReplaceAll(unitPath, `\`, "/") + "\"\n"
		if err := os.WriteFile(harness, []byte(include+cBenchSource(javaPlan(work), cpp)), 0o644); err != nil {
			return Result{}, err
		}
		binary := filepath.Join(work, "benchh")
		args := []string{"-O1", "-o", binary, harness, "-lm"}
		if cpp {
			args = append(args, "-std=c++17")
		}
		build := exec.Command(compiler, args...)
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return Result{}, fmt.Errorf("compiling the unit and the timing harness: %w", err)
		}
		run := exec.Command(binary, fmt.Sprint(minMS), fmt.Sprint(repeat))
		run.Stderr = os.Stderr
		stdout, err := run.Output()
		if err != nil {
			return Result{}, fmt.Errorf("timing the original: %w", err)
		}
		if err := os.WriteFile(origOut, stdout, 0o644); err != nil {
			return Result{}, err
		}
		handled = true
	}
	if !handled && strings.HasSuffix(m.Unit, ".java") {
		// Java is timed from source too: Bench.java is generated beside the unit and compiled with it.
		bridge := filepath.Join(work, "Bench.java")
		src := javaBenchSource(javaClass(m.Unit), javaPlan(work), literalValues, javaLit)
		if err := os.WriteFile(bridge, []byte(src), 0o644); err != nil {
			return Result{}, err
		}
		classes := filepath.Join(work, "benchclasses")
		if err := os.MkdirAll(classes, 0o755); err != nil {
			return Result{}, err
		}
		build := exec.Command("javac", "-d", classes, unitPath, bridge)
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return Result{}, fmt.Errorf("compiling the unit and the timing harness: %w", err)
		}
		run := exec.Command("java", "-cp", classes, "Bench", fmt.Sprint(minMS), fmt.Sprint(repeat))
		run.Stderr = os.Stderr
		stdout, err := run.Output()
		if err != nil {
			return Result{}, fmt.Errorf("timing the original: %w", err)
		}
		if err := os.WriteFile(origOut, stdout, 0o644); err != nil {
			return Result{}, err
		}
		handled = true
	}
	if !handled && strings.HasSuffix(m.Unit, ".cs") {
		dir := filepath.Join(work, "benchcs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Result{}, err
		}
		src, err := os.ReadFile(unitPath)
		if err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "Unit.cs"), src, 0o644); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "Bench.cs"), []byte(csharpBenchSource(javaPlan(work), javaClass(m.Unit))), 0o644); err != nil {
			return Result{}, err
		}
		proj := "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <OutputType>Exe</OutputType>\n" +
			"    <TargetFramework>net11.0</TargetFramework>\n    <Nullable>disable</Nullable>\n" +
			"    <AssemblyName>bench</AssemblyName>\n  </PropertyGroup>\n</Project>\n"
		if err := os.WriteFile(filepath.Join(dir, "bench.csproj"), []byte(proj), 0o644); err != nil {
			return Result{}, err
		}
		outDir, absErr := filepath.Abs(filepath.Join(work, "csbenchbin"))
		if absErr != nil {
			return Result{}, absErr
		}
		build := exec.Command("dotnet", "build", "--nologo", "-c", "Release", "-o", outDir)
		build.Dir = dir
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return Result{}, fmt.Errorf("compiling the unit and the timing harness: %w", err)
		}
		run := exec.Command(filepath.Join(outDir, "bench"), fmt.Sprint(minMS), fmt.Sprint(repeat))
		if root := dotnetRootForBench(); root != "" {
			run.Env = append(os.Environ(), "DOTNET_ROOT="+root)
		}
		run.Stderr = os.Stderr
		stdout, err := run.Output()
		if err != nil {
			return Result{}, fmt.Errorf("timing the original: %w", err)
		}
		if err := os.WriteFile(origOut, stdout, 0o644); err != nil {
			return Result{}, err
		}
		handled = true
	}
	if !handled && (strings.HasSuffix(m.Unit, ".kt") || strings.HasSuffix(m.Unit, ".kts")) {
		harness := filepath.Join(work, "Bench.kt")
		if err := os.WriteFile(harness, []byte(kotlinBenchSource(javaPlan(work))), 0o644); err != nil {
			return Result{}, err
		}
		jar := filepath.Join(work, "bench.jar")
		build := exec.Command("kotlinc", unitPath, harness, "-include-runtime", "-d", jar)
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return Result{}, fmt.Errorf("compiling the unit and the timing harness: %w", err)
		}
		run := exec.Command("java", "-jar", jar, fmt.Sprint(minMS), fmt.Sprint(repeat))
		run.Stderr = os.Stderr
		stdout, err := run.Output()
		if err != nil {
			return Result{}, fmt.Errorf("timing the original: %w", err)
		}
		if err := os.WriteFile(origOut, stdout, 0o644); err != nil {
			return Result{}, err
		}
		handled = true
	}
	if !handled && strings.HasSuffix(m.Unit, ".php") {
		script := filepath.Join(work, "bench.php")
		if err := os.WriteFile(script, []byte(phpDriver), 0o644); err != nil {
			return Result{}, err
		}
		cmd = exec.Command("php", script, unitPath, casesPath, origOut, fmt.Sprint(minMS), fmt.Sprint(repeat))
		handled = true
	}
	if !handled && (strings.HasSuffix(m.Unit, ".ps1") || strings.HasSuffix(m.Unit, ".psm1")) {
		script := filepath.Join(work, "bench.ps1")
		if err := os.WriteFile(script, []byte(powershellDriver), 0o644); err != nil {
			return Result{}, err
		}
		absScript, err := filepath.Abs(script)
		if err != nil {
			return Result{}, err
		}
		cmd = exec.Command("pwsh", "-NoLogo", "-NoProfile", "-File", absScript, unitPath, casesPath, origOut,
			fmt.Sprint(minMS), fmt.Sprint(repeat))
		handled = true
	}
	if !handled && (strings.HasSuffix(m.Unit, ".rb") || strings.HasSuffix(m.Unit, ".rake")) {
		rb := filepath.Join(work, "bench.rb")
		if err := os.WriteFile(rb, []byte(rubyDriver), 0o644); err != nil {
			return Result{}, err
		}
		cmd = exec.Command("ruby", rb, unitPath, casesPath, origOut, fmt.Sprint(minMS), fmt.Sprint(repeat))
	} else if !handled && (strings.HasSuffix(m.Unit, ".py") || strings.HasSuffix(m.Unit, ".pyi")) {
		// Python units are timed by python3: the original has to be the original.
		py := filepath.Join(work, "bench.py")
		if err := os.WriteFile(py, []byte(pythonDriver), 0o644); err != nil {
			return Result{}, err
		}
		cmd = exec.Command("python3", py, unitPath, casesPath, origOut, fmt.Sprint(minMS), fmt.Sprint(repeat))
	} else if !handled {
		cmd = exec.Command("node", append(nodeflags.Args(),
			"--import", hooksPath,
			script, unitPath, casesPath, origOut, fmt.Sprint(minMS), fmt.Sprint(repeat))...)
	}
	if cmd != nil {
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return Result{}, fmt.Errorf("timing the original: %w", err)
		}
	}
	original, err := readRows(origOut)
	if err != nil {
		return Result{}, err
	}

	benchDir := filepath.Join(dir, "cmd", "gobench")
	if err := os.MkdirAll(benchDir, 0o755); err != nil {
		return Result{}, err
	}
	src := strings.ReplaceAll(portDriver, cratePlaceholder, m.Crate)
	if err := os.WriteFile(filepath.Join(benchDir, "main.go"), []byte(src), 0o644); err != nil {
		return Result{}, err
	}
	portOut := filepath.Join(work, "bench-port.json")
	run := exec.Command(toolchain.Go(), "run", "./cmd/gobench", casesPath, portOut,
		fmt.Sprint(minMS), fmt.Sprint(repeat))
	run.Dir = dir
	run.Stderr = os.Stderr
	if err := run.Run(); err != nil {
		return Result{}, fmt.Errorf("timing the port: %w", err)
	}
	port, err := readRows(portOut)
	if err != nil {
		return Result{}, err
	}

	res := Result{MinMS: minMS}
	byName := map[string]Row{}
	for _, r := range original {
		byName[r.Name] = r
	}
	var ratios []float64
	for _, p := range port {
		o, ok := byName[p.Name]
		if !ok || p.NS <= 0 {
			continue
		}
		if isStub(p) {
			res.Unported = append(res.Unported, p.Name)
			continue
		}
		row := Row{Name: p.Name, Cases: p.Cases, OriginalNS: o.NS, PortNS: p.NS, Speedup: o.NS / p.NS}
		res.Rows = append(res.Rows, row)
		if row.Speedup > 0 {
			ratios = append(ratios, row.Speedup)
		}
	}
	sort.Slice(res.Rows, func(i, j int) bool { return res.Rows[i].Name < res.Rows[j].Name })
	if len(res.Rows) == 0 && len(port) > 0 {
		return Result{}, fmt.Errorf("nothing to time in %s: every function answers `not implemented: port …`, "+
			"which is the scaffold, not a port — fill the function in and bench again", opts.CrateDir)
	}
	res.Speedup = geometric(ratios)
	return res, nil
}

// unitPath finds the unit relative to the crate's parent repository.
func unitPath(crateDir, rel string) (string, error) {
	dir := crateDir
	for {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no folder above %s holds %s", crateDir, rel)
		}
		dir = parent
	}
}

func readRows(path string) ([]Row, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

// isStub reports whether every call in a row answered the scaffold's `not implemented: port …`. Those
// rows are dropped rather than printed with a ratio: the Go number is a timing of fmt.Errorf.
func isStub(r Row) bool { return r.Unported > 0 && r.Unported == r.Cases }

// geometric is the mean that a set of ratios wants: one slow function must not hide behind one fast.
func geometric(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += math.Log(x)
	}
	return math.Exp(sum / float64(len(xs)))
}

// Markdown is the table `bench` prints.
func (r Result) Markdown() string {
	var b strings.Builder
	b.WriteString("| Function | Cases | Original per call | Go per call | Speedup |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %s |\n",
			row.Name, row.Cases, ns(row.OriginalNS), ns(row.PortNS), times(row.Speedup))
	}
	if len(r.Unported) > 0 {
		sort.Strings(r.Unported)
		fmt.Fprintf(&b, "\nLeft out: %s — every call answered `not implemented: port …` (the scaffold, not a port).\n",
			strings.Join(quoteAll(r.Unported), ", "))
	}
	fmt.Fprintf(&b, "\nGeometric mean: %s over %d function(s), %d ms per side minimum.\n",
		times(r.Speedup), len(r.Rows), r.MinMS)
	return b.String()
}

func ns(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2f s", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2f ms", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.2f µs", v/1e3)
	}
	return fmt.Sprintf("%.0f ns", v)
}

func times(v float64) string {
	if v == 0 {
		return "—"
	}
	if v >= 1 {
		return fmt.Sprintf("%.2fx faster", v)
	}
	return fmt.Sprintf("%.2fx slower", 1/v)
}

var _ = cases.Case{}

// quoteAll puts each name in backticks for the table's reader: the names are the original's export names,
// and a bare one next to a sentence reads as part of the sentence.
func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "`" + n + "`"
	}
	return out
}
