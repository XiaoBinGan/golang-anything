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
