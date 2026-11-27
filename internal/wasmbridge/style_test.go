package wasmbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A port that declares a typed function gets the direct call, and the call names the identifier the file
// declares. Both halves were wrong together: detection required an exported name, so every scaffolded port was
// skipped and the typed path was dead, and the call site used the export name, so the day a port grew a typed
// `Count` the wrapper emitted `count(...)` — a compile error.
func TestATypedPortGetsTheDirectCall(t *testing.T) {
	dir := t.TempDir()
	src := `package port

func count(args []any) (any, error) { return nil, nil }

func Count(s string) float64 { return float64(len(s)) }
`
	if err := os.WriteFile(filepath.Join(dir, "port.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	detectPortStyle(dir)
	got := typedCall("count", "bytesAt(ptr, n)", "stringNumber", "string")
	if !strings.Contains(got, "return Count(") {
		t.Errorf("the wrapper does not call the typed function: %q", got)
	}
	if strings.Contains(got, "portCall") {
		t.Errorf("the registry is still in the typed path: %q", got)
	}
}

// The scaffold's `func count(args []any)` is the generic shape, not a typed function: calling it with plain
// arguments does not compile.
func TestAGenericPortFallsBackToTheRegistry(t *testing.T) {
	dir := t.TempDir()
	src := "package port\n\nfunc count(args []any) (any, error) { return nil, nil }\n"
	if err := os.WriteFile(filepath.Join(dir, "port.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	detectPortStyle(dir)
	got := typedCall("count", "bytesAt(ptr, n)", "stringNumber", "string")
	if !strings.Contains(got, "portCall") {
		t.Errorf("a generic port must go through the registry: %q", got)
	}
}
