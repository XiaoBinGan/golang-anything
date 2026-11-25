// Package record runs the original on generated inputs and stores what it answered.
//
// Two things it learned the hard way: cases must be generated from the declared types (a number fed to
// a string parameter is noise the port then has to reproduce), and the original's own tests are the
// best evidence of what it is really called with — so `--inputs` replays real calls and
// `--test-coverage` runs the suite to say which exports no test ever touches.
package record

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaobingan/go-anything/internal/analyze"
	"github.com/xiaobingan/go-anything/internal/cases"
	"github.com/xiaobingan/go-anything/internal/nodeflags"
	"github.com/xiaobingan/go-anything/internal/ts"
)

// driver is the JS the tool writes into the work folder and runs with node.
const driver = `import { pathToFileURL } from 'node:url'
import { readFile, writeFile } from 'node:fs/promises'

const [unitPath, outPath, planPath, extraPath, only] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const plan = JSON.parse(await readFile(planPath, 'utf8'))

// One table per declared type. A port only has to reproduce calls the original accepts, so the
// generator never crosses a type: that was the prototype's first mistake.
const table = {
  string: ['', 'a', 'abc', '  ', 'a b  c', 'x y z', 'pad  ', 'héllo', 'naïve café', '😀😀',
           '$5 apple', '$a$b', '$', 'a$', '  \t  ', 'ünïcodé', 'ÆØÅ', '中文 字', 'a\nb', '0'],
  // Byte buffers: what a parser is handed. Small, and the last one is longer than any offset below so
  // an index into it is in range for at least one case.
  bytes: [
    [], [0], [127], [128, 1], [255, 255, 255, 127], [0x80, 0x80, 0x80, 0x80, 0x08],
    [1, 2, 3, 4, 5, 6, 7, 8], [0xff, 0x00, 0xff, 0x00],
  ],
  // AbortSignal: an argument a real function cannot be exercised without. The aborted one carries a
  // reason, because that is what the throw is built from.
  signal: (() => {
    const idle = new AbortController().signal
    const stopped = new AbortController()
    stopped.abort(new Error('interrupted by the caller'))
    return [idle, stopped.signal]
  })(),
  // Magnitudes stay small on purpose: a times argument of 1e9 makes the original allocate a
  // gigabyte and the recording never returns. Big values are what --inputs is for.
  number: [0, 1, 2, -1, 7, 1.5, -0.5, 1e6, -1e6, 0.1 + 0.2, 3.14159, 1 / 3, -0],
  boolean: [true, false],
  'string[]': [[], ['a'], ['a', 'b'], ['héllo', '😀😀'], ['$x', 'y']],
  'number[]': [[], [1], [1, 2, -1], [0.1, 0.2], [1e21]],
  object: [{}, { a: 1 }, { a: 'x', b: [1, 2] }, { ' ': 0 }],
  any: [0, 'a b  c', null, true],
}

// decode marks what JSON cannot carry, so both sides compare values rather than spellings.
const decode = v => {
  if (v === undefined) return { $undefined: true }
  if (typeof v === 'number' && Number.isNaN(v)) return { $number: 'NaN' }
  if (typeof v === 'number' && !Number.isFinite(v)) return { $number: v > 0 ? 'Infinity' : '-Infinity' }
  if (Object.is(v, -0)) return { $number: '-0' }
  if (typeof v === 'bigint') return { $bigint: v.toString() }
  if (v instanceof Date) return { $date: v.toISOString() }
  // Platform objects a caller passes in: the wire carries what the port needs to answer, not the object.
  if (typeof AbortSignal !== 'undefined' && v instanceof AbortSignal) {
    return { $abortSignal: { aborted: v.aborted, reason: v.aborted ? decode(v.reason) : null } }
  }
  if (typeof v === 'string' && !v.isWellFormed()) return { $utf16: [...Array(v.length)].map((_, i) => v.charCodeAt(i)) }
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

const write = []
const seen = new Set()
// A call that takes longer than this is a sign the case is pathological, not that the code is slow:
// stop generating for that function and say so, instead of hanging the recording.
const BUDGET_MS = 250
const slow = new Set()
const big = new Map()
// A case has to be small enough to commit and to read in a diff. An answer past this is the
// generator's fault, not the code's: record it as skipped and stop asking that function.
const MAX_BYTES = 64 * 1024
// How many values of each position take part in the cross product. Six keeps a two-argument function at
// 36 combinations instead of the whole tables against each other.
const PAIRWISE_HEAD = 6
// How many oversized answers a function may produce before its remaining cases are dropped.
const MAX_OVERSIZED = 3
const push = c => {
  const key = c.fn + '|' + JSON.stringify(c.args)
  if (seen.has(key)) return
  seen.add(key)
  write.push(c)
}
const call = (fn, args) => {
  if (slow.has(fn)) return
  const started = Date.now()
  try {
    const ok = decode(mod[fn](...args))
    const size = JSON.stringify(ok)?.length ?? 0
    if (size > MAX_BYTES) {
      // One giant answer must not end the recording: the argument table has a 1,000,000 in it, and
      // giving up on the first oversized case left a whole unit with no cases at all.
      big.set(fn, (big.get(fn) ?? 0) + 1)
      if (big.get(fn) >= MAX_OVERSIZED) slow.add(fn)
      process.stderr.write('go-anything: ' + fn + ' answers ' + size + ' bytes for ' +
        JSON.stringify(decode(args)) + '; that case is skipped\n')
      return
    }
    push({ fn, args: decode(args), ok })
  } catch (e) {
    const type = e && e.constructor ? e.constructor.name : 'Error'
    push({ fn, args: decode(args), error: type + ': ' + String(e.message ?? e) })
  }
  if (Date.now() - started > BUDGET_MS) {
    slow.add(fn)
    process.stderr.write('go-anything: ' + fn + ' took ' + (Date.now() - started) + 'ms on ' +
      JSON.stringify(args) + '; no more cases for it\n')
  }
}

// cost orders a table from the values that make the smallest answer to the ones that make the largest.
const cost = v => (typeof v === 'string' ? v.length : typeof v === 'number' ? Math.abs(v) : (JSON.stringify(v) ?? '').length)

// Generated cases. The full cross product of every table against every other is what this used to do,
// and for two string parameters that is 4,000 calls per function — most of them noise a port does not
// learn from. Two runs replace it:
//
//  1. a paired walk: every value at every position, stepping the tables together (max(table) calls);
//  2. a bounded cross product over the first PAIRWISE_HEAD values of each position, for the cases where
//     two arguments interact.
for (const [fn, types] of Object.entries(plan)) {
  if (typeof mod[fn] !== 'function') continue
  if (only && !only.split(',').includes(fn)) continue
  // Cheap values first: a table that starts with 1,000,000 fills the answer limit before the small
  // ones are recorded, and a function whose three big cases are skipped ends up with no cases at all.
  const values = types.map(t => (table[t] ?? table.any).slice().sort((a, b) => cost(a) - cost(b)))
  const longest = Math.max(...values.map(v => v.length), 0)
  // Offset each position by a different stride so a paired walk is not twenty copies of one diagonal.
  for (let i = 0; i < longest; i++) {
    call(fn, values.map((v, j) => v[(i + j * 7) % v.length]))
  }
  const head = values.map(v => v.slice(0, PAIRWISE_HEAD))
  const walk = (args, i) => {
    if (i === head.length) return call(fn, args)
    for (const v of head[i]) walk([...args, v], i + 1)
  }
  walk([], 0)
}

// Calls a person supplied: the original's own tests, a bug report, a production sample.
let extra = []
try {
  extra = (await readFile(extraPath, 'utf8')).split('\n').filter(Boolean).map(l => JSON.parse(l))
} catch {}
for (const c of extra) {
  if (typeof mod[c.fn] !== 'function') continue
  if (only && !only.split(',').includes(c.fn)) continue
  call(c.fn, c.args ?? [], c.kwargs)
}

// A function that hit the budget is reported on stderr; its recorded cases are still valid.
await writeFile(outPath, write.map(c => JSON.stringify(c)).join('\n') + '\n')
`

// Options is one recording run.
type Options struct {
	CrateDir string
	Unit     analyze.Unit
	Only     []string
	// Inputs is a JSONL file of `{"fn": name, "args": [...]}` calls to replay as they are.
	Inputs string
	// TestCommand runs the original's own suite to learn which exports it never touches.
	TestCommand []string
	// Timeout bounds the whole run; a pathological case must not hang it.
	Timeout time.Duration
}

// Summary is what `record` prints.
type Summary struct {
	Cases       int      `json:"cases"`
	Functions   int      `json:"functions"`
	Fingerprint string   `json:"fingerprint"`
	Throws      int      `json:"throw_paths"`
	Untested    []string `json:"untested,omitempty"`
}

// Run writes the driver, runs it, and stores cases.jsonl plus the source fingerprint.
func Run(opts Options) (Summary, error) {
	work := filepath.Join(opts.CrateDir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return Summary{}, err
	}
	script := filepath.Join(work, "record.mjs")
	if err := os.WriteFile(script, []byte(driver), 0o644); err != nil {
		return Summary{}, err
	}
	hooksPath, err := WriteResolver(work)
	if err != nil {
		return Summary{}, err
	}
	// The unit's path is relative to the plan root, not to the process: find it the same way.
	root := planRoot(opts.CrateDir, opts.Unit.Files[0])
	unitPath, err := filepath.Abs(filepath.Join(root, opts.Unit.Files[0]))
	if err != nil {
		return Summary{}, err
	}
	if _, err := os.Stat(unitPath); err != nil {
		return Summary{}, fmt.Errorf("the unit is not where the plan says: %w", err)
	}

	plan, throws := typePlan(opts.Unit)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return Summary{}, err
	}
	planPath := filepath.Join(work, "params.json")
	if err := os.WriteFile(planPath, planJSON, 0o644); err != nil {
		return Summary{}, err
	}
	extraPath := filepath.Join(work, "inputs.jsonl")
	extra := ""
	if opts.Inputs != "" {
		raw, err := os.ReadFile(opts.Inputs)
		if err != nil {
			return Summary{}, fmt.Errorf("reading %s: %w", opts.Inputs, err)
		}
		if err := os.WriteFile(extraPath, raw, 0o644); err != nil {
			return Summary{}, err
		}
		extra = extraPath
	}

	out := filepath.Join(opts.CrateDir, "cases.jsonl")
	limit := opts.Timeout
	if limit <= 0 {
		limit = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	args := append(nodeflags.Args(), "--import", hooksPath, script, unitPath, out, planPath, extra,
		strings.Join(opts.Only, ","))
	cmd := exec.CommandContext(ctx, "node", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Summary{}, fmt.Errorf("recording did not finish in %s: one generated case runs too long, "+
				"so add real inputs with --inputs instead", limit)
		}
		return Summary{}, fmt.Errorf("recording failed: %w", err)
	}
	list, err := cases.Read(out)
	if err != nil {
		return Summary{}, err
	}

	raw, err := os.ReadFile(unitPath)
	if err != nil {
		return Summary{}, err
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:8])
	meta := map[string]any{"unit": opts.Unit.ID, "sha256": fingerprint, "throws": throws}
	if err := os.WriteFile(filepath.Join(opts.CrateDir, "recorded-from.json"), mustJSON(meta), 0o644); err != nil {
		return Summary{}, err
	}

	seen := map[string]bool{}
	for _, c := range list {
		seen[c.Fn] = true
	}
	s := Summary{Cases: len(list), Functions: len(seen), Fingerprint: fingerprint, Throws: throws}
	if len(opts.TestCommand) > 0 {
		s.Untested, err = coverage(work, opts.Unit, opts.TestCommand)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-anything: test coverage: %v\n", err)
		}
	}
	return s, nil
}

// planRoot walks up from the crate until the unit's file is under a parent.
func planRoot(crateDir, rel string) string {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return crateDir
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(crateDir)
		}
		dir = parent
	}
}

// typePlan maps each export to its parameter types in the dialect the driver knows, and counts the
// throw paths that a recorded failing case has to cover.
func typePlan(u analyze.Unit) (map[string][]string, int) {
	plan := map[string][]string{}
	throws := 0
	for _, e := range u.Exports {
		if e.Blocked != "" {
			continue
		}
		types := make([]string, len(e.Params))
		for i, p := range e.Params {
			types[i] = dialectType(p.Type)
		}
		plan[e.Name] = types
		throws += len(e.ThrowLines)
	}
	return plan, throws
}

// dialectType narrows a TypeScript annotation to one of the generator's tables.
func dialectType(t string) string {
	t = strings.TrimSpace(t)
	switch {
	case t == "string":
		return "string"
	case t == "number":
		return "number"
	case t == "boolean":
		return "boolean"
	case t == "string[]" || t == "Array<string>" || t == "ReadonlyArray<string>":
		return "string[]"
	case t == "number[]" || t == "Array<number>":
		return "number[]"
	case t == "AbortSignal":
		// A signal is a platform object: a number or a string makes the original read `.aborted` off
		// undefined and answer like an idle signal, which proves nothing about the throwing path.
		return "signal"
	case t == "Uint8Array" || t == "Buffer" || t == "Readonly<Uint8Array>":
		// A byte container is its own dialect: generating a number, a null or a string for it makes the
		// original read properties off a value no caller would pass, and the recording proves nothing.
		return "bytes"
	case strings.HasPrefix(t, "{") || strings.HasSuffix(t, "}"):
		return "object"
	}
	return "any"
}

// coverage runs the original's test suite under V8 coverage and reports the exports no test reached.
//
// This is evidence, not recording: it says which functions the tests actually call, so a plan can
// refuse to call a port tested when the original's own suite never touches it.
func coverage(work string, u analyze.Unit, command []string) ([]string, error) {
	dir := filepath.Join(work, "cov")
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = append(os.Environ(), "NODE_V8_COVERAGE="+dir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %w", command, err)
	}
	// The ranges a covered run reports for this unit tell us which lines ran.
	covered := map[int]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	target := filepath.Base(u.Files[0])
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var dump struct {
			Result []struct {
				URL       string `json:"url"`
				Functions []struct {
					Ranges []struct {
						StartOffset int `json:"startOffset"`
						EndOffset   int `json:"endOffset"`
						Count       int `json:"count"`
					} `json:"ranges"`
				} `json:"functions"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &dump) != nil {
			continue
		}
		for _, script := range dump.Result {
			if !strings.HasSuffix(script.URL, "/"+target) {
				continue
			}
			for _, fn := range script.Functions {
				for _, r := range fn.Ranges {
					if r.Count > 0 {
						covered[r.StartOffset] = true
					}
				}
			}
		}
	}
	if len(covered) == 0 {
		return nil, fmt.Errorf("no coverage for %s in %s", target, dir)
	}
	src, err := os.ReadFile(u.Files[0])
	if err != nil {
		return nil, err
	}
	var untested []string
	for _, e := range u.Exports {
		if e.Blocked != "" || !lineCovered(src, e, covered) {
			untested = append(untested, e.Name)
		}
	}
	sort.Strings(untested)
	return untested, nil
}

// lineCovered reports whether any offset on the export's first line ran.
func lineCovered(src []byte, e analyze.Export, covered map[int]bool) bool {
	start := 0
	for line := 1; line < e.Line; line++ {
		i := strings.IndexByte(string(src[start:]), '\n')
		if i < 0 {
			return false
		}
		start += i + 1
	}
	end := len(src)
	if i := strings.IndexByte(string(src[start:]), '\n'); i >= 0 {
		end = start + i
	}
	for off := start; off < end; off++ {
		if covered[off] {
			return true
		}
	}
	return false
}

// Count summarizes an existing recording.
func Count(crateDir string) (Summary, error) {
	list, err := cases.Read(filepath.Join(crateDir, "cases.jsonl"))
	if err != nil {
		return Summary{}, err
	}
	seen := map[string]bool{}
	for _, c := range list {
		seen[c.Fn] = true
	}
	out := Summary{Cases: len(list), Functions: len(seen)}
	if raw, err := os.ReadFile(filepath.Join(crateDir, "recorded-from.json")); err == nil {
		var v struct {
			Fingerprint string `json:"sha256"`
			Throws      int    `json:"throws"`
		}
		if json.Unmarshal(raw, &v) == nil {
			out.Fingerprint = v.Fingerprint
			out.Throws = v.Throws
		}
	}
	return out, nil
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(b, '\n')
}

// FingerprintOf is the fingerprint `record` writes, for tests and for `diff`.
func FingerprintOf(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8]), nil
}

var _ = ts.Param{} // the type table above is what makes a Param useful; keep the import honest
