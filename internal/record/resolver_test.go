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

// A unit that imports './util' — a bundler's spelling — must record. Node's resolver wants the
// extension, so the hook is what makes a real-world unit recordable at all.
func TestAUnitThatImportsWithoutAnExtensionRecords(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/util.ts", "export function pad(text: string, width: number): string {\n"+
		"  return text.padStart(width, ' ')\n}\n")
	write("src/frame.ts", "import { pad } from './util'\n\n"+
		"export function frame(label: string, width: number): string {\n"+
		"  if (width < 4) throw new RangeError('frame: width must be at least 4')\n"+
		"  return '-' + pad(label, width - 2) + '-'\n}\n")
	crate := filepath.Join(repo, "rust", "frame-go")
	if err := os.MkdirAll(crate, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema":"go-anything/port@1","unit":"src/frame.ts","crate":"example.com/frame",` +
		`"files":["src/frame.ts"],"exports":[{"name":"frame","params":["string","number"],` +
		`"returns":"string","line":3,"throw_lines":[4],"blocked":""}],"wire":"json"}`
	write("rust/frame-go/go-anything.json", manifest)

	unit := analyze.Unit{
		ID:    "src/frame.ts",
		Files: []string{"src/frame.ts"},
		Exports: []analyze.Export{{
			Name:       "frame",
			Params:     []ts.Param{{Name: "label", Type: "string"}, {Name: "width", Type: "number"}},
			Returns:    "string",
			Line:       3,
			ThrowLines: []int{4},
		}},
	}
	sum, err := Run(Options{CrateDir: crate, Unit: unit})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if sum.Cases == 0 {
		t.Fatalf("no cases: %+v", sum)
	}
	cases, err := os.ReadFile(filepath.Join(crate, "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The recording must contain the throw, not just the happy path.
	if !strings.Contains(string(cases), "width must be at least 4") {
		t.Errorf("the throwing call is missing:\n%s", cases)
	}
}
