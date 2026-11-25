package diff

import "testing"

func TestGateInsistsOnARecordedFailingCase(t *testing.T) {
	out := Outcome{Cases: 4, Matched: 4, ErrorCases: map[string]int{"ok": 0}}
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
