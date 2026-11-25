package shadow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesAModuleThatReadsTheOriginal(t *testing.T) {
	root := t.TempDir()
	crate := filepath.Join(root, "port-rs")
	if err := os.MkdirAll(crate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crate, "index.cjs"), []byte("module.exports = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "text.ts"), []byte("export function count(t: string) { return 0 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := Run(Options{CrateDir: crate, Unit: "src/text.ts", Exports: []string{"count", "words"}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(text)
	// The module must point at the original by a path relative to itself, and list the exports.
	for _, want := range []string{`"../src/text.ts"`, `"count"`, `"words"`, "callSync", "shadow.jsonl"} {
		if !strings.Contains(body, want) {
			t.Errorf("the module is missing %q:\n%s", want, body)
		}
	}
}

func TestRunRefusesACrateWithoutABridge(t *testing.T) {
	crate := t.TempDir()
	if _, err := Run(Options{CrateDir: crate, Unit: "src/text.ts", Exports: []string{"count"}}); err == nil {
		t.Error("shadow without index.cjs must be refused: there is nothing to compare against")
	}
}

func TestReportSaysSoWhenNothingWasSeen(t *testing.T) {
	rep, err := ReadReport(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Markdown(), "No shadow mismatch") {
		t.Errorf("empty report: %s", rep.Markdown())
	}
}
