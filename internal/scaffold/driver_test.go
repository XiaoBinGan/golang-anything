//go:build cgo

package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobingan/go-anything/internal/analyze"
	"github.com/xiaobingan/go-anything/internal/ts"
)

// A recording of nothing leaves an empty line, and the generated driver failed on it with "unexpected
// end of JSON input": an export with no cases turned into a broken driver.
func TestTheDriverSkipsABlankLine(t *testing.T) {
	dir := t.TempDir()
	unit := analyze.Unit{
		ID:    "src/unit.ts",
		Files: []string{"src/unit.ts"},
		Exports: []analyze.Export{{
			Name:   "f",
			Params: []ts.Param{{Name: "a", Type: "string"}},
			Line:   1,
		}},
	}
	if _, err := Run(Options{CrateDir: dir, Unit: unit, Crate: "example.com/x"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cmd", "godriver", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	driver := string(raw)
	if !strings.Contains(driver, "strings.TrimSpace(scan.Text())") {
		t.Error("the driver does not skip blank lines")
	}
	if !strings.Contains(driver, `"strings"`) {
		t.Error("the driver uses strings but does not import it")
	}
}
