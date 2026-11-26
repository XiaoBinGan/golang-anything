package diff

import (
	"strings"
	"testing"

	"github.com/XiaoBinGan/golang-anything/internal/cases"
)

func TestGateInsistsOnARecordedFailingCase(t *testing.T) {
	out := Outcome{Cases: 4, Matched: 4, ErrorCases: map[string]int{"ok": 0},
		PerFn: map[string]int{"ok": 4, "throws": 4, "generic": 4}}
	checks := Check(out, map[string]int{"ok": 0, "throws": 1}, map[string]string{"generic": "is generic"})
	if checks.Passed(out.Matched, out.Cases) {
		t.Fatal("a throw with no recorded failing case must fail the gate")
	}
	reasons := checks.Failed(out.Matched, out.Cases)
	if len(reasons) != 1 || reasons[0] == "" {
		t.Fatalf("reasons: %v", reasons)
	}
	// With a failing case recorded, the same port passes.
	out.ErrorCases["throws"] = 1
	checks = Check(out, map[string]int{"ok": 0, "throws": 1}, map[string]string{"generic": "is generic"})
	if !checks.Passed(out.Matched, out.Cases) {
		t.Fatalf("should pass: %v", checks.Failed(out.Matched, out.Cases))
	}
}

func TestGateFailsOnMismatchAndPanic(t *testing.T) {
	out := Outcome{
		Cases: 2, Matched: 1,
		// show() renders a panic as "panic <text>", which is what the gate looks for.
		Mismatch:   []Mismatch{{Fn: "f", Got: "panic index out of range"}},
		ErrorCases: map[string]int{},
	}
	checks := Check(out, nil, nil)
	if checks.Panicked != 1 {
		t.Errorf("panic counted: %+v", checks)
	}
	if checks.Passed(out.Matched, out.Cases) {
		t.Error("a mismatch must fail the gate")
	}
	// Zero recorded cases is a failure, not a vacuous pass.
	if (GateChecks{}).Passed(0, 0) {
		t.Error("no cases must not read as a pass")
	}
}

// A crate can be green while a whole function was never called: a recording whose table started with
// an answer too large to keep left one export with no cases at all, and the gate said yes. Now it says
// which export nobody asked about.
func TestGateFailsOnAnExportWithNoRecordedCase(t *testing.T) {
	out := Outcome{
		Cases: 4, Matched: 4,
		ErrorCases: map[string]int{},
		PerFn:      map[string]int{"pad": 4, "repeat": 0},
	}
	checks := Check(out, nil, map[string]string{"pad": "", "repeat": ""})
	if len(checks.Untested) != 1 || checks.Untested[0] != "repeat" {
		t.Fatalf("untested: %+v", checks.Untested)
	}
	reasons := checks.Failed(out.Matched, out.Cases)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "no recorded case for repeat") {
		t.Fatalf("reasons: %v", reasons)
	}
}

// A crate whose cases.jsonl is empty used to pass: the "no recorded case" rule read a map that only
// held blocked exports, so a portable export with zero cases was invisible. Every export has to be in
// that map for the rule to mean anything.
func TestAnEmptyRecordingFailsForEveryExport(t *testing.T) {
	out := Outcome{Cases: 0, Matched: 0, ErrorCases: map[string]int{}, PerFn: map[string]int{}}
	// The gate is told about one export that stays behind and one that does not.
	checks := Check(out, map[string]int{"portable": 0}, map[string]string{"portable": "", "blocked": "is generic"})
	// Only the portable export: one that stays in the original with a reason cannot have a case, and
	// demanding one made every crate with a blocked export permanently red.
	if len(checks.Untested) != 1 || checks.Untested[0] != "portable" {
		t.Fatalf("untested: %+v", checks.Untested)
	}
	reasons := checks.Failed(out.Matched, out.Cases)
	if len(reasons) == 0 {
		t.Fatal("an empty recording must not pass")
	}
	if !strings.Contains(strings.Join(reasons, " "), "portable") {
		t.Errorf("the portable export is not named: %v", reasons)
	}
}

// A recorded case can hold a 100,000-character string, and printing it in full turned one mismatch into
// 150 KB of terminal. Both the arguments and the answers are clipped now.
func TestAVeryLongValueIsClipped(t *testing.T) {
	long := strings.Repeat("x", 100000)
	out := Outcome{
		Cases: 1, Matched: 0,
		Mismatch: []Mismatch{{
			Fn:       "f",
			Args:     []any{long},
			Expected: show(cases.Case{Ok: long}),
			Got:      show(cases.Case{Ok: "y"}),
			Why:      "the answer differs as text",
		}},
	}
	text := out.Markdown()
	if len(text) > 1000 {
		t.Errorf("one mismatch printed %d bytes", len(text))
	}
	if !strings.Contains(text, "bytes more") {
		t.Errorf("the clipping does not say how much was cut:\n%s", text)
	}
}

// A call with no arguments arrives as `"args":[]` from the recorder and as no `args` at all from a
// driver that omits the empty field. Marshalling nil gives "null", so a function with no parameters
// never joined its own answer — every such export read "the driver did not answer this case".
func TestACallWithNoArgumentsJoins(t *testing.T) {
	if a, b := callKey("f", nil), callKey("f", []any{}); a != b {
		t.Fatalf("nil and empty args give different keys: %q vs %q", a, b)
	}
	// A call with arguments still distinguishes them.
	if a, b := callKey("f", []any{1.0}), callKey("f", []any{2.0}); a == b {
		t.Fatalf("different arguments share a key: %q", a)
	}
}
