//go:build cgo

package record

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobingan/go-anything/internal/analyze"
	"github.com/xiaobingan/go-anything/internal/ts"
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
