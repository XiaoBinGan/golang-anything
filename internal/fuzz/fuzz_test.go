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
	if err := os.WriteFile(filepath.Join(dir, "golang-anything.json"),
		[]byte(`{"unit":"src/a.ts","exports":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{CrateDir: dir}); err == nil {
		t.Error("a crate with no export has nothing to fuzz")
	}
}

// The fuzz alphabet has to carry the punctuation that changes what a function does. It drifted from the
// recorder's table once: 200 fresh draws could not find a shellQuote that had dropped its quote
// escaping, while the recorded cases could.
func TestTheAlphabetHasLoadBearingPunctuation(t *testing.T) {
	gen := newSource(1)
	seen := map[rune]bool{}
	for i := 0; i < 20000; i++ {
		for _, r := range randomString(gen) {
			seen[r] = true
		}
	}
	for _, want := range []rune{'\'', '"', '\\', '|', ';', '$', '`', '/', '~', '=', '@'} {
		if !seen[want] {
			t.Errorf("the alphabet never produces %q", want)
		}
	}
}
