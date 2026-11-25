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

	"github.com/xiaobingan/go-anything/internal/cases"
	"github.com/xiaobingan/go-anything/internal/nodeflags"
	"github.com/xiaobingan/go-anything/internal/record"
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
}

// Result is the whole measurement.
type Result struct {
	Rows    []Row   `json:"rows"`
	Speedup float64 `json:"speedup"` // geometric mean over the rows
	MinMS   int     `json:"min_ms"`
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
	"time"

	port "` + cratePlaceholder + `"
)

type testCase struct {
	Fn   string ` + "`json:\"fn\"`" + `
	Args []any  ` + "`json:\"args\"`" + `
}

type row struct {
	Name  string  ` + "`json:\"name\"`" + `
	Cases int     ` + "`json:\"cases\"`" + `
	NS    float64 ` + "`json:\"ns\"`" + `
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
		for _, a := range args { // warm up
			_, _ = f(a)
		}
		started := time.Now()
		rounds := 0
		for {
			for i := 0; i < repeat; i++ {
				for _, a := range args {
					_, _ = f(a)
				}
			}
			rounds++
			if time.Since(started) >= time.Duration(minMs)*time.Millisecond || rounds >= 10000 {
				break
			}
		}
		per := float64(time.Since(started).Nanoseconds()) / float64(rounds*repeat*len(args))
		rows = append(rows, row{Name: fn, Cases: len(args), NS: per})
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
	raw, err := os.ReadFile(filepath.Join(dir, "go-anything.json"))
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
	cmd := exec.Command("node", append(nodeflags.Args(),
		"--import", hooksPath,
		script, unitPath, casesPath, origOut, fmt.Sprint(minMS), fmt.Sprint(repeat))...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("timing the original: %w", err)
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
	run := exec.Command("go", "run", "./cmd/gobench", casesPath, portOut,
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
		row := Row{Name: p.Name, Cases: p.Cases, OriginalNS: o.NS, PortNS: p.NS, Speedup: o.NS / p.NS}
		res.Rows = append(res.Rows, row)
		if row.Speedup > 0 {
			ratios = append(ratios, row.Speedup)
		}
	}
	sort.Slice(res.Rows, func(i, j int) bool { return res.Rows[i].Name < res.Rows[j].Name })
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
