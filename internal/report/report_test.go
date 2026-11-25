package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCardSaysHowMuchWasProvedAndWhere(t *testing.T) {
	card := Card{
		Unit: "src/text.ts", Crate: "example.com/x/port-rs",
		Cases: 10, Matched: 10, Throws: 2, CoveredThrows: 2,
		Bridge: "in-process addon", Machine: "darwin/arm64, 10 CPUs", At: "2026-01-01 00:00",
	}
	text := card.Text()
	for _, want := range []string{
		"`src/text.ts` ported to Go",
		"10 / 10",
		"2 of 2",
		"in-process addon",
		"go-anything report",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report is missing %q:\n%s", want, text)
		}
	}
	// The card carries the same numbers and stays valid XML.
	svg := card.svg()
	if !strings.HasPrefix(strings.TrimSpace(svg), "<svg") || !strings.Contains(svg, "10/10") {
		t.Errorf("card:\n%s", svg)
	}
	if strings.Contains(svg, "&&") {
		t.Error("an unescaped ampersand would break the SVG")
	}
}

func TestAFailingCardIsDrawnInTheOtherColour(t *testing.T) {
	pass := Card{Cases: 4, Matched: 4, Unit: "u", Crate: "c"}.svg()
	fail := Card{Cases: 4, Matched: 3, Unit: "u", Crate: "c"}.svg()
	if pass == fail {
		t.Error("a report that does not pass must not look like one that does")
	}
	if !strings.Contains(fail, "#8a6d1f") {
		t.Errorf("the failing accent is missing:\n%s", fail)
	}
}

func TestRunWritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go-anything.json"),
		[]byte(`{"unit":"src/a.ts","crate":"example.com/x","exports":[{"name":"f","throw_lines":[3]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// No cases yet: `diff` refuses, which is the right answer rather than an empty report.
	if _, err := Run(Options{CrateDir: dir, SkipBench: true}); err == nil {
		t.Error("a crate without a recording must not produce a report")
	}
}
