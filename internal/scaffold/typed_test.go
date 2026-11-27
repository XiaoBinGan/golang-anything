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
	got := typedStub(analyze.Export{Name: "Twice", Params: []ts.Param{{Name: "n", Type: "int"}}, Returns: "int"})
	if got != "" {
		t.Errorf("typedStub(%q) = %q, want empty", "Twice", got)
	}
}
