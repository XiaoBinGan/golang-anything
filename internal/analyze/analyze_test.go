//go:build cgo

package analyze

import (
	"path/filepath"
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
