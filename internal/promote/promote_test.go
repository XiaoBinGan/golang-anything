package promote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shim has to give the file back byte for byte, or the takeover is one nobody can undo.
func TestInsertThenRevertRestoresTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timer.rb")
	src := "class Timer\n  def double(n)\n    n * 2\n  end\nend\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := Insert(path, "Timer", []string{"double"})
	if err != nil || !added {
		t.Fatalf("Insert = %v, %v", added, err)
	}
	shimmed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shimmed), "def self.double(*args)") {
		t.Errorf("no shim in the file:\n%s", shimmed)
	}
	removed, err := Revert(path, "Timer")
	if err != nil || !removed {
		t.Fatalf("Revert = %v, %v", removed, err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != src {
		t.Errorf("revert did not restore the original:\nwant %q\ngot  %q", src, back)
	}
}

// A hand-edited block is refused rather than half-removed.
func TestRevertRefusesATruncatedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timer.rb")
	src := "class Timer\nend\n# >>> go-anything promote: Timer\nclass Timer\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Revert(path, "Timer"); err == nil {
		t.Fatal("a truncated block must be reported")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != src {
		t.Errorf("the refused revert changed the file:\n%s", after)
	}
}
