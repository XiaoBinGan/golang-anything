package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/XiaoBinGan/golang-anything/internal/analyze"
	"github.com/XiaoBinGan/golang-anything/internal/ts"
)

// The typed entry point has to come back as `(T, error)`: `internal/wasmbridge` records how many results a
// typed function returns and emits `v, err := Count(...)` for two, `return Count(...)` for one. A one-value stub
// compiled next to a hand-written two-value port and then broke `wasm_support.go` with "too many return values".
func TestTypedStubReturnsAValueAndAnError(t *testing.T) {
	dir := t.TempDir()
	unit := analyze.Unit{
		ID: "src/text.ts",
		Exports: []analyze.Export{{
			Name:    "count",
			Line:    3,
			Params:  []ts.Param{{Name: "text", Type: "string"}},
			Returns: "number",
		}},
	}
	if _, err := Run(Options{CrateDir: filepath.Join(dir, "p-go"), Unit: unit, Crate: "example.com/x/p-go"}); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "p-go", "port.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "func Count(text string) (float64, error)") {
		t.Errorf("the typed entry point is not the (T, error) shape:\n%s", src)
	}
	// It delegates, so every driver that prefers the typed path keeps working before the body is written.
	if !strings.Contains(string(src), "count([]any{text})") {
		t.Errorf("the stub does not delegate to the generic wrapper:\n%s", src)
	}
}

// A C# method is already exported (`Twice`), so a wrapper next to it would be the same identifier.
func TestTypedStubSkipsAnAlreadyExportedName(t *testing.T) {
	got := typedStub(analyze.Export{Name: "Twice", Params: []ts.Param{{Name: "n", Type: "int"}}, Returns: "int"}, []string{"M.kt"})
	if got != "" {
		t.Errorf("typedStub(%q) = %q, want empty", "Twice", got)
	}
}

// A JVM `int` is 32 bits and wraps: `int(-2147483648) * 2` is 0 in Java and -4294967296 on a Go `int`. A promoted Java
// method measured exactly that — the gate answered "expected 0, got -4294967296" — so Java and Kotlin map to the fixed
// widths.
func TestJavaTypesMapToTheirWidth(t *testing.T) {
	unit := analyze.Unit{
		ID:    "MathsPromoted.java",
		Files: []string{"MathsPromoted.java"},
		Exports: []analyze.Export{{
			Name:    "doubled",
			Line:    3,
			Params:  []ts.Param{{Name: "n", Type: "int"}},
			Returns: "int",
		}},
	}
	got := typedStub(unit.Exports[0], unit.Files)
	if !strings.Contains(got, "func Doubled(n int32) (int32, error)") {
		t.Errorf("a Java int did not become int32:\n%s", got)
	}
	// The same export in TypeScript keeps the Go widths.
	if kotlin := typedStub(unit.Exports[0], []string{"text.ts"}); !strings.Contains(kotlin, "func Doubled(n int) (int, error)") {
		t.Errorf("a TypeScript int changed width:\n%s", kotlin)
	}
}
