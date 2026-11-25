// Package fuzz looks for the difference the recording missed.
//
// `record` proves what it generated and what a person supplied; `fuzz` throws fresh inputs at both
// sides from a new seed each run. A seed that found something is printed, so the finding is repeatable:
// `--seed 12345` regenerates exactly the same calls.
package fuzz

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xiaobingan/go-anything/internal/cases"
	"github.com/xiaobingan/go-anything/internal/diff"
	"github.com/xiaobingan/go-anything/internal/nodeflags"
	"github.com/xiaobingan/go-anything/internal/record"
)

// Options is one fuzz run.
type Options struct {
	CrateDir  string
	Count     int
	Seed      int64
	Keep      bool
	Tolerance float64
	Shown     int
}

// Result is what a run found.
type Result struct {
	Seed      int64           `json:"seed"`
	Calls     int             `json:"calls"`
	Matched   int             `json:"matched"`
	Mismatch  []diff.Mismatch `json:"mismatches"`
	Kept      int             `json:"kept"`
	Untouched []string        `json:"untouched"`
}

// originalScript answers a list of calls on the original: the same wire markers the recorder uses.
const originalScript = `import { pathToFileURL } from 'node:url'
import { readFile, writeFile } from 'node:fs/promises'

const [unitPath, callsPath, outPath] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const calls = (await readFile(callsPath, 'utf8')).split('\n').filter(Boolean).map(JSON.parse)

const decode = v => {
  if (v === undefined) return { $undefined: true }
  if (typeof v === 'number' && Number.isNaN(v)) return { $number: 'NaN' }
  if (typeof v === 'number' && !Number.isFinite(v)) return { $number: v > 0 ? 'Infinity' : '-Infinity' }
  if (Object.is(v, -0)) return { $number: '-0' }
  if (typeof v === 'bigint') return { $bigint: v.toString() }
  if (typeof v === 'string' && !v.isWellFormed()) return { $utf16: [...Array(v.length)].map((_, i) => v.charCodeAt(i)) }
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

const answers = []
for (const c of calls) {
  try {
    answers.push({ fn: c.fn, args: c.args, ok: decode(mod[c.fn](...(c.args ?? []))) })
  } catch (e) {
    const type = e && e.constructor ? e.constructor.name : 'Error'
    answers.push({ fn: c.fn, args: c.args, error: type + ': ' + String(e.message ?? e) })
  }
}
await writeFile(outPath, answers.map(a => JSON.stringify(a)).join('\n') + '\n')
`

// Run generates calls, answers them on both sides, and compares.
func Run(opts Options) (Result, error) {
	dir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return Result{}, err
	}
	m, err := readManifest(dir)
	if err != nil {
		return Result{}, err
	}
	if len(m.Exports) == 0 {
		return Result{}, fmt.Errorf("nothing to fuzz: the crate carries no export")
	}
	count := opts.Count
	if count <= 0 {
		count = 50
	}
	seed := opts.Seed
	if seed == 0 {
		seed = rand.Int63()
	}
	gen := newSource(seed)
	calls := make([]cases.Case, 0, count*len(m.Exports))
	for _, e := range m.Exports {
		if e.Blocked != "" {
			continue
		}
		types := make([]string, len(e.Params))
		for i, t := range e.Params {
			types[i] = dialectType(t)
		}
		for i := 0; i < count; i++ {
			args := make([]any, len(types))
			for j, t := range types {
				args[j] = valueOf(gen, t)
			}
			calls = append(calls, cases.Case{Fn: e.Name, Args: args})
		}
	}
	if len(calls) == 0 {
		return Result{}, fmt.Errorf("every export is blocked: there is nothing to fuzz")
	}
	root, err := planRoot(dir, m.Unit)
	if err != nil {
		return Result{}, err
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return Result{}, err
	}

	callsPath := filepath.Join(work, "fuzz-calls.jsonl")
	if err := cases.Write(callsPath, calls); err != nil {
		return Result{}, err
	}
	hooksPath, err := record.WriteResolver(work)
	if err != nil {
		return Result{}, err
	}
	script := filepath.Join(work, "fuzz-original.mjs")
	if err := os.WriteFile(script, []byte(originalScript), 0o644); err != nil {
		return Result{}, err
	}
	origOut := filepath.Join(work, "fuzz-original.jsonl")
	args := append(nodeflags.Args(), "--import", hooksPath, script, filepath.Join(root, m.Unit), callsPath, origOut)
	cmd := exec.Command("node", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("answering on the original: %w", err)
	}
	want, err := cases.Read(origOut)
	if err != nil {
		return Result{}, err
	}

	// The port answers through the driver it already has: a fuzz case is a case.
	casesPath := filepath.Join(dir, "cases.jsonl")
	before, err := os.ReadFile(casesPath)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	if err := cases.Write(casesPath, calls); err != nil {
		return Result{}, err
	}
	portAnswers, runErr := diff.RunDriver(dir)
	// Put the recording back before anything else can see a fuzz file in its place.
	if err := os.WriteFile(casesPath, before, 0o644); err != nil {
		return Result{}, err
	}
	if runErr != nil {
		return Result{}, runErr
	}
	byCall := map[string]cases.Case{}
	for _, a := range portAnswers {
		byCall[key(a.Fn, a.Args)] = a
	}

	tol := opts.Tolerance
	if tol <= 0 {
		tol = 1e-9
	}
	shown := opts.Shown
	if shown <= 0 {
		shown = 10
	}
	out := Result{Seed: seed, Calls: len(want)}
	var keepList []cases.Case
	for _, w := range want {
		got, ok := byCall[key(w.Fn, w.Args)]
		if !ok {
			got = cases.Case{Fn: w.Fn, Args: w.Args, Err: "the port did not answer"}
		}
		if why := diff.Compare(w, got, tol); why == "" {
			out.Matched++
			continue
		} else if len(out.Mismatch) < shown {
			out.Mismatch = append(out.Mismatch, diff.Mismatch{
				Fn: w.Fn, Args: w.Args, Expected: show(w), Got: show(got), Why: why,
			})
		}
		keepList = append(keepList, w)
	}

	if opts.Keep && len(keepList) > 0 {
		// Appending what failed turns a fuzz finding into a case the port has to answer from then on.
		list, err := cases.Read(casesPath)
		if err != nil {
			return Result{}, err
		}
		seen := map[string]bool{}
		for _, c := range list {
			seen[key(c.Fn, c.Args)] = true
		}
		for _, c := range keepList {
			if seen[key(c.Fn, c.Args)] {
				continue
			}
			list = append(list, c)
			out.Kept++
		}
		if err := cases.Write(casesPath, list); err != nil {
			return Result{}, err
		}
	}
	for _, e := range m.Exports {
		if e.Blocked != "" {
			out.Untouched = append(out.Untouched, e.Name)
		}
	}
	return out, nil
}

// manifest is the part of go-anything.json fuzz needs: where the unit is and what each export takes.
type manifest struct {
	Unit    string `json:"unit"`
	Exports []struct {
		Name    string   `json:"name"`
		Params  []string `json:"params"`
		Blocked string   `json:"blocked"`
	} `json:"exports"`
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	raw, err := os.ReadFile(filepath.Join(dir, "go-anything.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(raw, &m)
}

// newSource is the seeded generator, named so a test can replay a run exactly.
func newSource(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

// dialectType narrows a declared type to one of the generator's families.
func dialectType(t string) string {
	switch t {
	case "string", "number", "boolean", "string[]", "number[]":
		return t
	case "Uint8Array", "Buffer", "Readonly<Uint8Array>":
		// A byte container is its own family here too: drawing a string for it makes the original throw
		// on a value no caller would pass, and the report is about the generator, not the port.
		return "bytes"
	}
	if strings.HasPrefix(t, "{") || strings.HasSuffix(t, "}") {
		return "object"
	}
	return "any"
}

// valueOf draws one value of a family from the seeded generator.
func valueOf(gen *rand.Rand, kind string) any {
	switch kind {
	case "string":
		return randomString(gen)
	case "number":
		return randomNumber(gen)
	case "boolean":
		return gen.Intn(2) == 0
	case "string[]":
		n := gen.Intn(4)
		out := make([]any, n)
		for i := range out {
			out[i] = randomString(gen)
		}
		return out
	case "number[]":
		n := gen.Intn(4)
		out := make([]any, n)
		for i := range out {
			out[i] = randomNumber(gen)
		}
		return out
	case "bytes":
		n := gen.Intn(9)
		out := make([]any, n)
		for i := range out {
			out[i] = float64(gen.Intn(256))
		}
		return out
	case "object":
		return map[string]any{"a": randomNumber(gen), "b": randomString(gen)}
	}
	// `any`: a different family each time, so an untyped parameter still gets breadth.
	switch gen.Intn(4) {
	case 0:
		return randomString(gen)
	case 1:
		return randomNumber(gen)
	case 2:
		return gen.Intn(2) == 0
	}
	return nil
}

// randomString leans on what breaks string code: non-ASCII, emoji and lone-carrier cases.
func randomString(gen *rand.Rand) string {
	// The alphabet carries the punctuation that changes what a function does, not only letters: the
	// recorder's table learned this in round 21 and this generator had drifted from it, so 200 fresh
	// draws could not find a shellQuote that had dropped its quote escaping.
	alphabet := []rune("ab  $\t\n.-_0é😀中\u00a0\u200bß'\"\\`|;&<>()[]{}*?~#%!,:")
	n := gen.Intn(12)
	out := make([]rune, n)
	for i := range out {
		out[i] = alphabet[gen.Intn(len(alphabet))]
	}
	return string(out)
}

// randomNumber spans the places numbers stop being the same number.
func randomNumber(gen *rand.Rand) float64 {
	switch gen.Intn(6) {
	case 0:
		return float64(gen.Intn(2000) - 1000)
	case 1:
		return gen.Float64()*2 - 1
	case 2:
		return float64(gen.Intn(1<<30)) * 1e6
	case 3:
		return 0
	case 4:
		return float64(gen.Intn(10)) / 3
	}
	return -float64(gen.Intn(1000)) / 7
}

func key(fn string, args []any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return fn
	}
	return fn + "|" + string(b)
}

func show(c cases.Case) string {
	switch {
	case c.Err != "":
		return "error " + c.Err
	case c.Panic != "":
		return "panic " + c.Panic
	}
	b, err := json.Marshal(c.Ok)
	if err != nil {
		return fmt.Sprint(c.Ok)
	}
	return string(b)
}

// planRoot walks up from the crate until the unit's file is under a parent.
func planRoot(crateDir, rel string) (string, error) {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no folder above %s holds %s", crateDir, rel)
		}
		dir = parent
	}
}
