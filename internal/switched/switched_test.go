package switched

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crate makes a throwaway crate folder with the files `enable` insists on.
func crate(t *testing.T, unit string) (dir, path string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "port-rs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.cjs", "addon.node"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go-anything.json"), []byte(`{"unit":"`+unit+`","crate":"example.com/x/port-rs","exports":[{"name":"pad"},{"name":"count"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "export function pad(t: string, w: number): string { return t }\nexport function count(t: string): number { return 0 }\n"
	path = filepath.Join(root, filepath.FromSlash(unit))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestEnableThenDisableGivesTheOriginalBackByteForByte(t *testing.T) {
	dir, path := crate(t, "src/text.ts")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(Options{CrateDir: dir, Unit: "src/text.ts", Crate: "x", Exports: []string{"pad", "count"}}); err != nil {
		t.Fatal(err)
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block := string(on)
	for _, want := range []string{">>> go-anything: port-rs is on", `callSync("pad"`, `callSync("count"`, "<<< go-anything: port-rs"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block is missing %q:\n%s", want, block)
		}
	}

	// Enabling twice adds one block, not two.
	if _, err := Enable(Options{CrateDir: dir, Unit: "src/text.ts", Crate: "x", Exports: []string{"pad", "count"}}); err != nil {
		t.Fatal(err)
	}
	twice, _ := os.ReadFile(path)
	if strings.Count(string(twice), ">>> go-anything") != 1 {
		t.Errorf("the block landed twice:\n%s", twice)
	}

	// Disabling gives back exactly the bytes the file had.
	if _, removed, err := Disable(dir, "src/text.ts", "x"); err != nil || !removed {
		t.Fatalf("disable: removed=%v err=%v", removed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("not byte for byte:\nbefore=%q\nafter =%q", before, after)
	}
	// And a second disable has nothing to do.
	if _, removed, err := Disable(dir, "src/text.ts", "x"); err != nil || removed {
		t.Errorf("second disable: removed=%v err=%v", removed, err)
	}
}

func TestEnableRefusesAnUnknownExportAndAnUnbuiltCrate(t *testing.T) {
	dir, _ := crate(t, "src/text.ts")
	if _, err := Enable(Options{CrateDir: dir, Unit: "src/text.ts", Crate: "x", Exports: []string{"pad"}, Only: []string{"nope"}}); err == nil {
		t.Error("an unknown export must be refused, not silently skipped")
	}
	// A crate that was never bridged has no wrapper to call.
	empty := t.TempDir()
	if _, err := Enable(Options{CrateDir: empty, Unit: "src/text.ts", Crate: "x", Exports: []string{"pad"}}); err == nil {
		t.Error("an unbuilt crate must be refused")
	}
}

func TestOnlySwitchesTheNamedExports(t *testing.T) {
	dir, path := crate(t, "src/text.ts")
	if _, err := Enable(Options{CrateDir: dir, Unit: "src/text.ts", Crate: "x",
		Exports: []string{"pad", "count"}, Only: []string{"count"}}); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(path)
	if strings.Contains(string(text), `callSync("pad"`) {
		t.Errorf("--only count switched pad too:\n%s", text)
	}
	if !strings.Contains(string(text), `callSync("count"`) {
		t.Errorf("--only count did not switch count:\n%s", text)
	}
}

// The modern spelling is `export const f = () => …`, and a block that assigns to it throws
// "Assignment to constant variable" the moment the module loads. Enable has to loosen the declaration,
// and disable has to put the original text back exactly.
func TestAConstExportCanBeSwitchedAndRestored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "uleb-go")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.cjs", "addon.node"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"unit":"src/unit.ts","crate":"example.com/x/uleb-go","exports":[{"name":"read"}]}`
	if err := os.WriteFile(filepath.Join(dir, "go-anything.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "// A const arrow, the way most TypeScript is written today.\nexport const read = (n: number): number => n\n"
	path := filepath.Join(root, "src", "unit.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Enable(Options{CrateDir: dir, Unit: "src/unit.ts", Exports: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	on, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "export let read =") {
		t.Errorf("the declaration was not loosened, so the block cannot assign to it:\n%s", on)
	}
	if _, removed, err := Disable(dir, "src/unit.ts", ""); err != nil || !removed {
		t.Fatalf("disable: %v %v", err, removed)
	}
	off, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(off) != src {
		t.Errorf("disable did not restore the original:\nwant %q\ngot  %q", src, string(off))
	}
}

// A hand-edited block — the opening marker without its closing one — must not be removed silently, and
// must not take the code after it along. "Nothing to remove" would send a person looking in the wrong
// place.
func TestATruncatedBlockIsReportedNotRemoved(t *testing.T) {
	dir, path := crate(t, "src/unit.ts")
	if _, err := Enable(Options{CrateDir: dir, Unit: "src/unit.ts", Exports: []string{"pad"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	var kept []string
	for _, line := range lines {
		if strings.HasPrefix(line, "// <<< go-anything") {
			continue
		}
		kept = append(kept, line)
	}
	edited := strings.Join(kept, "\n") + "\nexport const after = 1\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Disable(dir, "src/unit.ts", ""); err == nil {
		t.Fatal("a truncated block must be reported")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "export const after = 1") {
		t.Errorf("the code after the block was taken with it:\n%s", after)
	}
}

// A unit written in a language with no block is refused, and refused before anything is written: the
// JavaScript block went into a .rs file, the run printed success, and the compiler found out later. The
// refusal is the only place that knows the difference between "switched on" and "no longer compiles".
func TestEnableRefusesAUnitWithNoTakeover(t *testing.T) {
	dir, path := crate(t, "src/text.rs")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Enable(Options{CrateDir: dir, Unit: "src/text.rs", Crate: "port-rs", Exports: []string{"pad"}})
	if err == nil {
		t.Fatal("enable accepted a Rust unit")
	}
	if !strings.Contains(err.Error(), "no takeover") || !strings.Contains(err.Error(), "Ruby (.rb)") {
		t.Errorf("the refusal does not say what is supported: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the refused enable wrote to the file anyway:\nwant %q\ngot  %q", before, after)
	}
}

// The languages that do have a block must keep working: a guard that refuses everything also passes the
// test above.
func TestEveryLanguageWithABlockIsAccepted(t *testing.T) {
	for _, unit := range []string{"src/a.ts", "src/a.tsx", "src/a.js", "src/a.jsx", "src/a.mjs", "src/a.cjs", "src/a.rb"} {
		if _, ok := takeover(unit); !ok {
			t.Errorf("%s has a block but enable refuses it", unit)
		}
	}
	for _, unit := range []string{"src/a.rs", "src/a.go", "src/a.py", "src/a.c", "src/a", "src/a.ts.bak"} {
		if kind, ok := takeover(unit); ok {
			t.Errorf("%s has no block but enable calls it %q", unit, kind)
		}
	}
}
