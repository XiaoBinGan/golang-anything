// Package analyze walks a repository and lays out what a Go port would have to cover.
//
// The numbers are the ones rust-anything scores with, so a plan from either tool reads the same way:
// readiness starts at 100 and subtracts risk points, size and fan-in; priority is half readiness and
// half value (compute density, tests, size); `start_here` keeps ready units of at least 20 lines.
package analyze

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/ts"
)

// Export is one function a bridge could carry.
type Export struct {
	Name    string     `json:"name"`
	Params  []ts.Param `json:"params"`
	Returns string     `json:"returns"`
	Line    int        `json:"line"`
	EndLine int        `json:"end_line"`
	Method  bool       `json:"method,omitempty"`
	// Receiver is the class of a static method.
	Receiver   string `json:"receiver,omitempty"`
	Async      bool   `json:"async,omitempty"`
	Generic    bool   `json:"generic,omitempty"`
	ThrowLines []int  `json:"throw_lines,omitempty"`
	// Blocked says why the bridge cannot carry it, when it cannot.
	Blocked string `json:"blocked,omitempty"`
}

// Risk is something that usually costs a rewrite its correctness.
type Risk struct {
	Kind   string   `json:"kind"`
	Weight float64  `json:"weight"`
	Lines  []string `json:"lines"`
}

// Unit is a file that ports as one piece.
type Unit struct {
	ID                 string   `json:"id"`
	Files              []string `json:"files"`
	Loc                int      `json:"loc"`
	Exports            []Export `json:"exports"`
	Bridgeable         int      `json:"bridgeable"`
	InstanceOnly       int      `json:"instance_methods,omitempty"`
	InstanceFree       int      `json:"instance_free,omitempty"`
	InstancePromotable int      `json:"instance_promotable,omitempty"`
	Readiness          int      `json:"readiness"`
	Priority           int      `json:"priority"`
	Density            float64  `json:"density"`
	Risks              []Risk   `json:"risks"`
	Wave               int      `json:"wave"`
	InternalDeps       []string `json:"internal_deps"`
	ParseErrors        bool     `json:"parse_errors,omitempty"`
	Status             string   `json:"status"` // ready, caution, blocked, keep
	Notes              []string `json:"notes,omitempty"`
}

// Report is the whole plan.
type Report struct {
	Root      string   `json:"root"`
	Language  string   `json:"language"`
	Units     []Unit   `json:"units"`
	StartHere []string `json:"start_here"`
	Files     int      `json:"files"`
	Lines     int      `json:"lines"`
	// Unsupported counts the source files here that this tool cannot read, by extension. A plan that
	// silently reports zero units for a Java repository has told the reader nothing.
	Unsupported map[string]int `json:"unsupported,omitempty"`
}

// riskWeights follows rust-anything's severity table, so the two tools agree on what is dangerous.
var riskWeights = map[string]float64{
	"String offsets and slicing":                         6,
	"Regex features the `regexp` package lacks":          6,
	"Regexes built at runtime":                           6,
	"Dynamic code (eval, new Function, computed import)": 6,
	"Insertion-ordered maps and object keys":             3,
	"Number semantics":                                   1,
	"Async and promises":                                 3,
	"Exceptions":                                         1,
	"Class inheritance":                                  3,
	"Reflection and prototypes":                          3,
	"JSON round-trips":                                   1,
	"Clock and random numbers":                           1,
}

// skipDir is everything that is not source a person wrote.
var skipDir = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "out": true, ".git": true,
	"coverage": true, ".next": true, ".nuxt": true, "vendor": true, "__pycache__": true,
	"rust": true, "port": true, "golang-anything": true,
}

// Reasons tallies, per blocking reason, how many exports stay in the original. A plan for a real
// repository is thousands of units long; the tally is what a person reads first, because it says which
// shapes the bridge cannot carry rather than which file they are in.
func (r *Report) Reasons() map[string]int {
	out := map[string]int{}
	for _, u := range r.Units {
		for _, e := range u.Exports {
			if e.Blocked != "" {
				out[e.Blocked]++
			}
		}
	}
	return out
}

// unsupported names the extensions that mark a source file this tool cannot read. They are counted so the
// plan can say "there are 812 Java files here and this tool reads TypeScript and Python" instead of
// reporting an empty repository.
var unsupported = map[string]bool{
	".java": true, ".kt": true, ".kts": true, ".cs": true, ".rb": true, ".php": true, ".ps1": true,
	".cc": true, ".cpp": true, ".cxx": true, ".c": true, ".h": true, ".hpp": true, ".go": true,
	".rs": true, ".swift": true, ".scala": true, ".m": true, ".mm": true, ".ex": true, ".exs": true,
	".lua": true, ".pl": true, ".sh": true, ".bash": true, ".zsh": true, ".sql": true,
}

// Run analyzes root.
func Run(root string) (*Report, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rep := &Report{Root: abs}
	// The plan names the languages it actually found. It used to say "typescript" for a Python or Java
	// folder and report zero units, which reads as "nothing here" rather than "nothing supported".
	seenLang := map[string]bool{}
	unsupportedSeen := map[string]int{}
	// Start the parser while the walk below is still looking for files: a build without cgo pays a
	// process start for it, and this is the one moment it costs nothing.
	ts.Warm()
	var units []Unit
	byPath := map[string]int{}
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			// A folder that holds a crate is this tool's output, not source a person wrote: its helper
			// files (resolve.mjs, check.mjs) would otherwise appear in the plan as units of their own.
			if _, err := os.Stat(filepath.Join(path, "golang-anything.json")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		lang, ok := ts.LangOf(d.Name())
		if !ok {
			// A source file in a language this tool cannot read is worth naming: silence looks like an
			// empty repository, and the difference matters to whoever is deciding what to do next.
			if ext := strings.ToLower(filepath.Ext(d.Name())); unsupported[ext] {
				unsupportedSeen[ext]++
			}
			return nil
		}
		seenLang[string(lang)] = true
		if isTestFile(d.Name()) {
			return nil
		}
		parsed, err := ts.Parse(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if len(parsed.Fns) == 0 {
			return nil
		}
		units = append(units, unitOf(rel, parsed))
		byPath[rel] = len(units) - 1
		rep.Files++
		rep.Lines += parsed.Loc
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The naming and the census are part of the report on every path out, including the one where
	// nothing was found — that is exactly the case where a reader needs to know why.
	rep.Language = languageName(seenLang)
	rep.Unsupported = unsupportedSeen
	if len(units) == 0 {
		return rep, nil
	}
	resolveDeps(abs, units, byPath)
	orderWaves(units, byPath)
	for i := range units {
		units[i].Readiness, units[i].Priority, units[i].Density = score(units[i])
		units[i].Status = status(units[i])
		units[i].Notes = notes(units[i])
	}
	sort.Slice(units, func(i, j int) bool {
		if units[i].Wave != units[j].Wave {
			return units[i].Wave < units[j].Wave
		}
		if units[i].Priority != units[j].Priority {
			return units[i].Priority > units[j].Priority
		}
		return units[i].ID < units[j].ID
	})
	rep.Units = units
	rep.StartHere = startHere(units)
	return rep, nil
}

func isTestFile(name string) bool {
	for _, suffix := range []string{".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.js", ".spec.js", ".d.ts",
		// Every language names its tests the same way; only the extensions above were here, so Click's
		// tests were 30 of its 36 units and the plan pointed at code nobody would port.
		"_test.py", "_test.go", "_test.rb", "_spec.rb", "Test.java", "Tests.java",
		"_test.c", "_test.cpp", "_test.cc", "Test.cs", "Tests.cs", "_test.kt", "Test.kt",
		".test.ps1", ".Tests.ps1"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	// A file called test_foo.py or foo_test is a test whatever the runner calls it.
	base := strings.ToLower(name)
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}

// isComponent spots a React-shaped export: a capitalised name in a .tsx/.jsx file, or one that
// returns `null` — markup no bridge can carry across a language boundary.
func isComponent(rel string, e Export) bool {
	if !strings.HasSuffix(rel, ".tsx") && !strings.HasSuffix(rel, ".jsx") {
		return false
	}
	name := e.Name
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// unitOf turns a parsed file into a unit: exports with their bridge blocks, risks with file:line.
func unitOf(rel string, f *ts.File) Unit {
	u := Unit{ID: rel, Files: []string{rel}, Loc: f.Loc, ParseErrors: !f.ParseOK, InstanceOnly: f.InstanceMethods,
		InstanceFree: f.InstanceFree, InstancePromotable: f.InstancePromotable}
	for _, fn := range f.Fns {
		e := Export{
			Name:       fn.Name,
			Params:     fn.Params,
			Returns:    fn.Returns,
			Line:       fn.Line,
			EndLine:    fn.EndLine,
			Method:     fn.Method,
			Receiver:   fn.Receiver,
			Async:      fn.Async,
			Generic:    fn.Generic,
			ThrowLines: fn.ThrowLines,
		}
		switch {
		case fn.Generator:
			e.Blocked = "is a generator"
		case fn.Generic:
			e.Blocked = "is generic: the bridge carries concrete types only"
		case fn.Async:
			e.Blocked = "is async: the call would have to cross asynchronously"
		}
		if e.Blocked == "" && isComponent(rel, e) {
			e.Blocked = "is a UI component: it returns markup"
		}
		// A Promise return is asynchronous work: the bridge answers in the same turn or not at all. The
		// `async` keyword is not the only way to write one — most of the real repository declares
		// `: Promise<T>` on an arrow.
		if e.Blocked == "" && strings.Contains(e.Returns, "Promise") {
			e.Blocked = "returns a Promise: the bridge answers in one turn"
		}
		// A Map, a Set or a symbol has no JSON form: a port would have to invent one and the recording
		// would be comparing two different inventions.
		if e.Blocked == "" {
			for _, what := range append([]string{e.Returns}, paramTypes(e.Params)...) {
				if kind := unbridgeableKind(what); kind != "" {
					switch kind {
					case "Iterable", "AsyncIterable", "Generator", "AsyncGenerator":
						// A generator is lazy: it answers nothing until it is pulled, and the bridge has no
						// way to pull.
						e.Blocked = "carries " + article(kind) + " " + kind + ": it is lazy, and the bridge cannot pull"
					case "Readable", "Writable", "Duplex", "Transform":
						// A stream of bytes is a long-lived object, not a value.
						e.Blocked = "carries " + article(kind) + " " + kind + ": a stream is not a value"
					default:
						e.Blocked = "carries " + article(kind) + " " + kind + ": it has no JSON form"
					}
					break
				}
			}
		}
		// A method that reads or writes state outside its arguments is not a function: the port has no
		// instance and no class variables, so a recorded answer would depend on a call order.
		if e.Blocked == "" && fn.State {
			e.Blocked = "touches state outside its arguments: the bridge carries functions, not objects"
		}
		// A callback parameter cannot cross a JSON bridge: recording it produced calls like
		// edgeSpan("a", 0) whose only outcome was "predicate is not a function", which proves nothing.
		// An export that needs one is a redesign, not a port.
		if e.Blocked == "" {
			for _, p := range e.Params {
				if isFunctionType(p.Type) {
					e.Blocked = "takes a function: a callback cannot cross the bridge"
					break
				}
				if p.Type == "buffer" {
					// A C buffer parameter is written by the function and read back by the caller: the
					// effect is the buffer, not the return value, and the wire carries one value.
					e.Blocked = "takes a mutable buffer: the effect is the buffer, which the wire cannot carry"
					break
				}
			}
		}
		if e.Blocked == "" {
			u.Bridgeable++
		}
		u.Exports = append(u.Exports, e)
	}
	grouped := map[string]*Risk{}
	for _, r := range f.Risks {
		weight, ok := riskWeights[r.Kind]
		if !ok {
			weight = 1
		}
		g, ok := grouped[r.Kind]
		if !ok {
			g = &Risk{Kind: r.Kind, Weight: weight}
			grouped[r.Kind] = g
		}
		g.Lines = append(g.Lines, fmt.Sprintf("%s:%d %s", rel, r.Line, r.Code))
	}
	for _, r := range grouped {
		sort.Strings(r.Lines)
		u.Risks = append(u.Risks, *r)
	}
	sort.Slice(u.Risks, func(i, j int) bool { return u.Risks[i].Kind < u.Risks[j].Kind })
	return u
}

// resolveDeps keeps the imports that land on another unit of this repository.
func resolveDeps(root string, units []Unit, byPath map[string]int) {
	for i := range units {
		deps := map[string]bool{}
		for _, imp := range importsOf(filepath.Join(root, units[i].ID)) {
			if !strings.HasPrefix(imp, ".") {
				continue
			}
			base := filepath.ToSlash(filepath.Join(filepath.Dir(units[i].ID), imp))
			for _, candidate := range []string{base, base + ".ts", base + ".tsx", base + ".js",
				strings.TrimSuffix(base, ".js") + ".ts", strings.TrimSuffix(base, ".ts") + ".js",
				base + "/index.ts", base + "/index.js"} {
				if idx, ok := byPath[candidate]; ok && candidate != units[i].ID {
					deps[units[idx].ID] = true
				}
			}
		}
		for dep := range deps {
			units[i].InternalDeps = append(units[i].InternalDeps, dep)
		}
		sort.Strings(units[i].InternalDeps)
	}
}

// importsOf re-reads one file for its import specifiers. Cheap next to a port.
func importsOf(path string) []string {
	f, err := ts.Parse(path)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		out = append(out, imp.Specifier)
	}
	return out
}

// orderWaves puts a unit after the units it imports; a cycle stays in one wave, as the Rust tool does.
func orderWaves(units []Unit, byPath map[string]int) {
	wave := make([]int, len(units))
	state := make([]int, len(units)) // 0 unvisited, 1 in progress, 2 done
	var visit func(i int) int
	visit = func(i int) int {
		switch state[i] {
		case 1:
			return 0 // cycle
		case 2:
			return wave[i]
		}
		state[i] = 1
		w := 0
		for _, dep := range units[i].InternalDeps {
			j, ok := byPath[dep]
			if !ok {
				continue
			}
			if next := visit(j) + 1; next > w {
				w = next
			}
		}
		state[i] = 2
		wave[i] = w
		return w
	}
	for i := range units {
		visit(i)
	}
	for i := range units {
		units[i].Wave = wave[i]
	}
}

// score is readiness, priority and density, with the same terms rust-anything documents.
func score(u Unit) (readiness, priority int, density float64) {
	riskPoints := 0.0
	for _, r := range u.Risks {
		riskPoints += r.Weight * math.Log(1+float64(len(r.Lines)))
	}
	sizePenalty := float64(u.Loc) / 40
	fanIn := 0.0 // a file has no fan-in of its own; the folder model in the Rust tool does
	readinessF := 100 - riskPoints - sizePenalty - fanIn
	readinessF = math.Max(0, math.Min(100, readinessF))

	ops := 0.0
	for _, r := range u.Risks {
		switch r.Kind {
		case "String offsets and slicing", "Number semantics":
			ops += float64(len(r.Lines))
		}
	}
	if u.Loc > 0 {
		density = math.Round(ops*100/float64(u.Loc)*10) / 10
	}
	compute := math.Min(density*4, 50)
	size := float64(u.Loc) / 4
	switch {
	case u.Loc >= 600:
		size = 10
	case u.Loc >= 80:
		size = 20
	}
	value := compute + size // tests are not modelled: they add 30 or 15 in the Rust tool
	priorityF := readinessF*0.5 + value*0.5
	priorityF = math.Max(0, math.Min(100, priorityF))
	return int(math.Round(readinessF)), int(math.Round(priorityF)), density
}

// status is what `map` prints: ready, caution, blocked or keep.
func status(u Unit) string {
	switch {
	case u.Bridgeable == 0 && len(u.Exports) > 0:
		return "blocked"
	case u.Bridgeable == 0:
		return "keep"
	case u.ParseErrors:
		return "caution"
	case u.Readiness >= 60:
		return "ready"
	}
	return "caution"
}

// notes says what a reader needs to know about this unit, in the words a plan uses.
func notes(u Unit) []string {
	var out []string
	if u.ParseErrors {
		out = append(out, "parse errors: results for this unit are partial")
	}
	blocked := 0
	for _, e := range u.Exports {
		if e.Blocked != "" {
			blocked++
		}
	}
	if blocked > 0 {
		out = append(out, fmt.Sprintf("%d of %d exports stay in the original", blocked, len(u.Exports)))
	}
	if u.InstanceOnly > 0 {
		out = append(out, fmt.Sprintf("%d instance method(s) need an object: a bridge carries functions, so "+
			"their state has to be passed in explicitly", u.InstanceOnly))
	}
	if u.InstanceFree > 0 {
		out = append(out, fmt.Sprintf("%d of them touch no state and could be promoted to `Owner.name(...)` "+
			"once the crate has a shim that supplies the instance", u.InstanceFree))
	}
	if u.InstancePromotable > 0 {
		out = append(out, fmt.Sprintf("%d of those live in a class, where `allocate` can supply the instance; "+
			"the rest sit in a module, which cannot be instantiated", u.InstancePromotable))
	}
	if len(u.InternalDeps) > 0 {
		out = append(out, fmt.Sprintf("imports %s: port %s first, or together",
			strings.Join(quoteAll(u.InternalDeps), ", "), pronoun(len(u.InternalDeps))))
	}
	throws := 0
	for _, e := range u.Exports {
		throws += len(e.ThrowLines)
	}
	if throws > 0 {
		out = append(out, fmt.Sprintf("%d throw paths need a recorded failing case each", throws))
	}
	return out
}

func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = "`" + s + "`"
	}
	return out
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// startHere is the shortlist: ready units of at least 20 lines, ten at most.
func startHere(units []Unit) []string {
	pick := func(statuses []string, minLoc int) []Unit {
		var list []Unit
		for _, u := range units {
			ok := false
			for _, s := range statuses {
				if u.Status == s {
					ok = true
					break
				}
			}
			if ok && u.Loc >= minLoc && u.Bridgeable > 0 {
				list = append(list, u)
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Priority != list[j].Priority {
				return list[i].Priority > list[j].Priority
			}
			if list[i].Readiness != list[j].Readiness {
				return list[i].Readiness > list[j].Readiness
			}
			if list[i].Wave != list[j].Wave {
				return list[i].Wave < list[j].Wave
			}
			if list[i].Density != list[j].Density {
				return list[i].Density > list[j].Density
			}
			return list[i].ID < list[j].ID
		})
		return list
	}
	const floor = 20
	list := pick([]string{"ready"}, floor)
	if len(list) < 3 {
		list = pick([]string{"ready", "caution"}, floor)
	}
	if len(list) == 0 {
		list = pick([]string{"ready", "caution"}, 1)
	}
	var out []string
	for i, u := range list {
		if i == 10 {
			break
		}
		out = append(out, u.ID)
	}
	return out
}

// isFunctionType reports whether a declared type is a function or a callback.
func isFunctionType(t string) bool {
	t = strings.TrimSpace(t)
	if strings.Contains(t, "=>") {
		return true
	}
	// `(character: string) => boolean`, `() => void` and `() => Promise<void>` all carry "=>".
	return strings.HasPrefix(t, "Function") || strings.Contains(t, "Function)")
}

// paramTypes lists the declared types of a parameter list.
func paramTypes(params []ts.Param) []string {
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = p.Type
	}
	return out
}

// unbridgeableKind names the reason a declared type cannot cross the wire, or "" when it can.
//
// Only the names that are unambiguously not data: an interface called SetupOptions crosses as an object,
// a Map does not.
func unbridgeableKind(t string) string {
	t = strings.TrimSpace(t)
	for _, kind := range []string{"Iterable", "AsyncIterable", "Generator", "AsyncGenerator",
		"Readable", "Writable", "Duplex", "Transform"} {
		if strings.HasPrefix(t, kind+"<") || t == kind || strings.Contains(t, "<"+kind+"<") ||
			strings.Contains(t, " "+kind+"<") || strings.Contains(t, "| "+kind) {
			return kind
		}
	}
	for _, kind := range []string{"Map", "Set", "WeakMap", "WeakSet"} {
		if strings.HasPrefix(t, kind+"<") || strings.HasPrefix(t, kind+" |") || t == kind ||
			strings.HasPrefix(t, "readonly "+kind+"<") || strings.Contains(t, " "+kind+"<") {
			return kind
		}
	}
	if t == "Symbol" || strings.HasPrefix(t, "symbol<") || t == "symbol" {
		return "symbol"
	}
	return ""
}

// article picks "a" or "an" for a type name, so a reason reads like a sentence.
func article(kind string) string {
	switch kind[0] {
	case 'A', 'E', 'I', 'O', 'U':
		return "an"
	}
	return "a"
}

// languageName answers what the plan read, and says so plainly when it read nothing.
func languageName(seen map[string]bool) string {
	if len(seen) == 0 {
		return "nothing this tool reads"
	}
	names := make([]string, 0, len(seen))
	for l := range seen {
		names = append(names, l)
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}
