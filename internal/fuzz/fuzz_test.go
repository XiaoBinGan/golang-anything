package fuzz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratorCoversTheTypesAndTheSeedRepeats(t *testing.T) {
	// The same seed must draw the same values: a finding is only useful if it can be replayed.
	for _, kind := range []string{"string", "number", "boolean", "string[]", "number[]", "object", "any"} {
		a, err := json.Marshal(draw(kind, 42, 20))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(draw(kind, 42, 20))
		if string(a) != string(b) {
			t.Errorf("%s: the same seed drew different values:\n%s\n%s", kind, a, b)
		}
		c, _ := json.Marshal(draw(kind, 43, 20))
		if string(a) == string(c) && kind != "boolean" {
			t.Errorf("%s: another seed drew the same values (%s)", kind, a)
		}
	}
}

// draw runs the generator the way Run does, with a fresh source per seed.
func draw(kind string, seed int64, n int) []any {
	gen := newSource(seed)
	out := make([]any, n)
	for i := range out {
		out[i] = valueOf(gen, kind)
	}
	return out
}

func TestRunRefusesWithoutACrate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go-anything.json"),
		[]byte(`{"unit":"src/a.ts","exports":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{CrateDir: dir}); err == nil {
		t.Error("a crate with no export has nothing to fuzz")
	}
}
