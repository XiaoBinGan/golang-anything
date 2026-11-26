//go:build cgo

package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/XiaoBinGan/golang-anything/internal/ts"
)

func TestPlanOrdersWavesAndNamesBlockedExports(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "waves")
	rep, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Units) != 4 {
		t.Fatalf("want 4 units, got %d: %+v", len(rep.Units), rep.Units)
	}
	wave := map[string]int{}
	blocked := map[string]string{}
	var startHere []string
	for _, u := range rep.Units {
		wave[u.ID] = u.Wave
		for _, e := range u.Exports {
			if e.Blocked != "" {
				blocked[e.Name] = e.Blocked
			}
		}
	}
	startHere = rep.StartHere

	// `format.ts` imports `util.ts`, so it comes a wave later.
	if wave["src/util.ts"] != 0 || wave["src/format.ts"] != 1 {
		t.Errorf("waves: util=%d format=%d", wave["src/util.ts"], wave["src/format.ts"])
	}
	// A generic export and a UI component stay in the original, with the reason.
	if blocked["first"] == "" || blocked["Widget"] == "" {
		t.Errorf("blocked exports: %+v", blocked)
	}
	// Only units with something to carry are offered first, and a blocked-only unit never is.
	for _, id := range startHere {
		if id == "src/generic.ts" || id == "src/widget.tsx" {
			t.Errorf("start_here offers a unit with nothing to port: %v", startHere)
		}
	}
	if len(startHere) == 0 {
		t.Error("start_here is empty for a repo that has portable units")
	}
	// The `throw` in `box` is an error path the gate will insist on.
	for _, u := range rep.Units {
		if u.ID != "src/format.ts" {
			continue
		}
		if len(u.Exports[0].ThrowLines) != 1 {
			t.Errorf("format.ts throw lines: %+v", u.Exports[0].ThrowLines)
		}
		if u.Notes == nil {
			t.Error("a unit with a throw should note it")
		}
	}
}

func TestPlanIsEmptyWithoutSource(t *testing.T) {
	rep, err := Run(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Units) != 0 || len(rep.StartHere) != 0 {
		t.Fatalf("empty repo: %+v", rep)
	}
}

// The tool's own output must not appear in the plan: a crate folder holds generated helper files
// (resolve.mjs, check.mjs, shadow.cjs) that are JavaScript by extension and would be listed as units.
func TestACrateFolderIsNotSource(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/unit.ts", "export const f = (a: string): string => a\n")
	write("go-ports/unit-go/golang-anything.json", `{"unit":"src/unit.ts","crate":"example.com/x","exports":[]}`)
	write("go-ports/unit-go/resolve-hook.mjs", "export async function resolve() {}\n")

	rep, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range rep.Units {
		if strings.Contains(u.ID, "go-ports") {
			t.Errorf("the plan contains the tool's own output: %s", u.ID)
		}
	}
	if len(rep.Units) != 1 || rep.Units[0].ID != "src/unit.ts" {
		t.Errorf("units: %+v", rep.Units)
	}
}

// A Promise return is asynchronous work; the bridge answers in the same turn or not at all. Most of the
// real repository writes `: Promise<T>` on an arrow rather than `async`, and analyze called all of them
// ready.
func TestAPromiseReturnIsBlocked(t *testing.T) {
	unit := exportNamed(t, "fetchThing", "Promise<string>", nil)
	if unit.Blocked == "" || !strings.Contains(unit.Blocked, "Promise") {
		t.Errorf("a Promise return is not blocked: %q", unit.Blocked)
	}
	// An interface of data still crosses as an object, so a named type is not blocked for being named.
	plain := exportNamed(t, "options", "SetupOptions", nil)
	if plain.Blocked != "" {
		t.Errorf("a data interface was blocked: %q", plain.Blocked)
	}
}

// A Map, a Set or a symbol has no JSON form. So does a WeakMap in a parameter.
func TestAContainerWithNoJSONFormIsBlocked(t *testing.T) {
	cases := []struct {
		name, returns string
		params        []ts.Param
		want          string
	}{
		{"counts", "Map<string, number>", nil, "Map"},
		{"uuids", "Set<string>", nil, "Set"},
		{"each", "void", []ts.Param{{Name: "m", Type: "WeakMap<object, string>"}}, "WeakMap"},
		{"tag", "symbol", nil, "symbol"},
	}
	for _, c := range cases {
		unit := exportNamed(t, c.name, c.returns, c.params)
		if !strings.Contains(unit.Blocked, c.want) {
			t.Errorf("%s(%v): %q does not mention %s", c.name, c.returns, unit.Blocked, c.want)
		}
	}
}

// exportNamed runs the real analysis over one synthetic export and answers its verdict.
func exportNamed(t *testing.T, name, returns string, params []ts.Param) Export {
	t.Helper()
	root := t.TempDir()
	src := "export const " + name + " = (" +
		paramList(params) + "): " + returns + " => undefined\n"
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "unit.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Units) != 1 || len(rep.Units[0].Exports) != 1 {
		t.Fatalf("units: %+v", rep.Units)
	}
	return rep.Units[0].Exports[0]
}

func paramList(params []ts.Param) string {
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = p.Name + ": " + p.Type
	}
	return strings.Join(parts, ", ")
}

// A generator is lazy and a stream is long-lived: the bridge answers in one turn, so neither can cross.
func TestLazyAndStreamingReturnsAreBlocked(t *testing.T) {
	cases := []struct{ returns, want string }{
		{"Generator<void, Thing>", "Generator"},
		{"AsyncIterable<Buffer>", "AsyncIterable"},
		{"Iterable<string>", "Iterable"},
		{"Readable", "Readable"},
		{"Result<Iterable<Candidate>, Failure>", "Iterable"},
	}
	for _, c := range cases {
		unit := exportNamed(t, "make", c.returns, nil)
		if !strings.Contains(unit.Blocked, c.want) {
			t.Errorf("%s: %q does not mention %s", c.returns, unit.Blocked, c.want)
		}
	}
}

// The tally is what a person reads first on a repository with thousands of units.
func TestTheReasonsTallyCountsEveryBlockedExport(t *testing.T) {
	rep := &Report{Units: []Unit{
		{Exports: []Export{{Blocked: "is a generator"}, {Blocked: "is a generator"}, {}}},
		{Exports: []Export{{Blocked: "carries a Map: it has no JSON form"}}},
	}}
	got := rep.Reasons()
	if got["is a generator"] != 2 || got["carries a Map: it has no JSON form"] != 1 || len(got) != 2 {
		t.Errorf("tally: %+v", got)
	}
}
