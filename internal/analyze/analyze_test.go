//go:build cgo

package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	write("go-ports/unit-go/go-anything.json", `{"unit":"src/unit.ts","crate":"example.com/x","exports":[]}`)
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
