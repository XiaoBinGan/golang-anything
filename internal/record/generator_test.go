//go:build cgo

package record

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/XiaoBinGan/golang-anything/internal/analyze"
	"github.com/XiaoBinGan/golang-anything/internal/pool"
	"github.com/XiaoBinGan/golang-anything/internal/ts"
)

// writeRepo lays out a unit and a crate for it, and answers the unit.
func writeRepo(t *testing.T, source string, exports ...analyze.Export) (string, analyze.Unit) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "unit.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	crate := filepath.Join(repo, "rust", "unit-go")
	if err := os.MkdirAll(crate, 0o755); err != nil {
		t.Fatal(err)
	}
	return crate, analyze.Unit{ID: "src/unit.ts", Files: []string{"src/unit.ts"}, Exports: exports}
}

// Two string parameters used to mean twenty times twenty calls per function: 4,000 cases of noise.
// The pairing keeps every value at every position, and the bounded cross product keeps the pairs that
// matter, so the count stays in the hundreds rather than the thousands.
func TestTwoStringParametersDoNotExplode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	crate, unit := writeRepo(t,
		"export const join = (a: string, b: string): string => a + '|' + b\n",
		analyze.Export{
			Name:    "join",
			Params:  []ts.Param{{Name: "a", Type: "string"}, {Name: "b", Type: "string"}},
			Returns: "string",
			Line:    1,
		})
	sum, err := Run(Options{CrateDir: crate, Unit: unit})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Cases == 0 {
		t.Fatal("no cases recorded")
	}
	if sum.Cases > 400 {
		t.Errorf("%d cases for two string arguments: the pairing did not bound the cross product", sum.Cases)
	}
	// Every value of every position must still appear somewhere.
	raw, err := os.ReadFile(filepath.Join(crate, "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"😀😀", "中文 字", "abc"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the paired walk lost %q:\n%s", want, string(raw))
		}
	}
}

// A function whose table contains a 1,000,000 must still record: one oversized answer used to drop every
// remaining case for it, which left a real unit with zero.
func TestOneOversizedAnswerDoesNotEndTheRecording(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	crate, unit := writeRepo(t,
		"export const fill = (n: number): string => '-'.repeat(Math.max(1, Math.trunc(n)))\n",
		analyze.Export{
			Name:    "fill",
			Params:  []ts.Param{{Name: "n", Type: "number"}},
			Returns: "string",
			Line:    1,
		})
	sum, err := Run(Options{CrateDir: crate, Unit: unit})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Cases < 5 {
		t.Errorf("%d cases: one oversized answer ended the recording", sum.Cases)
	}
}

// A parameter typed Uint8Array used to be generated from the `any` table: numbers, nulls and strings
// went into a byte parser, and the recorded cases proved behaviour no caller would ever ask for.
func TestAByteContainerGetsBytes(t *testing.T) {
	if dialect := dialectType("Uint8Array"); dialect != "bytes" {
		t.Fatalf("Uint8Array maps to %q", dialect)
	}
	for _, typed := range []string{"Buffer", "Readonly<Uint8Array>"} {
		if dialect := dialectType(typed); dialect != "bytes" {
			t.Errorf("%s maps to %q", typed, dialect)
		}
	}
}

// A unit that extends Error with `constructor(readonly reason: unknown)` is ordinary TypeScript, and
// stripping types refuses it: ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX. The transform flag loads it.
func TestParameterPropertiesLoad(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	// Node 26 removed --experimental-transform-types (only strip-types is left), and stripping cannot load a parameter
	// property. The feature needs a node that can transform: on a machine whose node cannot, say so instead of failing
	// as if the code were wrong. Node 22 passes this test unchanged.
	skipUnlessNodeTransforms(t)
	crate, unit := writeRepo(t,
		"export class Stop extends Error {\n"+
			"  constructor(readonly reason: unknown) { super('stop') }\n}\n\n"+
			"export const stopWith = (reason: string): string => { throw new Stop(reason) }\n",
		analyze.Export{
			Name:       "stopWith",
			Params:     []ts.Param{{Name: "reason", Type: "string"}},
			Returns:    "string",
			Line:       6,
			ThrowLines: []int{6},
		})
	if _, err := Run(Options{CrateDir: crate, Unit: unit}); err != nil {
		t.Fatalf("a parameter property must load: %v", err)
	}
}

// The strings a case is generated from live in internal/pool: this test reads the same list the recorder
// writes, so it cannot pass while the generated table lacks a character. A port of shellQuote that
// dropped the quote escaping passed every case until "it's" was in the table, and escapeJsonPointerToken
// was recorded 64 times with no "/" or "~" in any argument.
func TestTheStringTableHasLoadBearingPunctuation(t *testing.T) {
	values := strings.Join(pool.Values(), "\u0000")
	for _, want := range []string{"'", "\\", "\"", "|", ";", "/", "~", "a/b/c", "~0~1"} {
		if !strings.Contains(values, want) {
			t.Errorf("no value in the pool contains %q", want)
		}
	}
}

// An answer that is a class instance carries its behaviour in its prototype, and JSON keeps only the
// fields: recording one produced "{}" cases no port could match. The recorder leaves the function
// unproven and says so.
func TestAnAnswerThatIsAClassInstanceIsNotRecorded(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	skipUnlessNodeTransforms(t)
	crate, unit := writeRepo(t,
		"export class Slots { constructor(public n: number) {} }\n\n"+
			"export const makeSlots = (n: number): Slots => new Slots(n)\n",
		analyze.Export{
			Name:    "makeSlots",
			Params:  []ts.Param{{Name: "n", Type: "number"}},
			Returns: "Slots",
			Line:    3,
		})
	sum, err := Run(Options{CrateDir: crate, Unit: unit})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Cases != 0 {
		t.Errorf("%d cases recorded for an answer JSON cannot carry", sum.Cases)
	}
}

// skipUnlessNodeTransforms guards the two cases whose fixture is TypeScript a parameter property needs: node 26 removed
// --experimental-transform-types and strips types only, so on such a machine the feature is unavailable, and a failure
// there would be about the runtime rather than about the recorder.
func skipUnlessNodeTransforms(t *testing.T) {
	t.Helper()
	if err := exec.Command("node", "--experimental-transform-types", "-e", "").Run(); err != nil {
		t.Skip("this node cannot transform TypeScript: these fixtures need node <= 24")
	}
}
