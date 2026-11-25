package diff

import (
	"fmt"
	"sort"
	"strings"
)

// GateChecks lists the conditions a port has to meet beyond "every case matches".
type GateChecks struct {
	// Uncovered are exports with a `throw` and no recorded failing case: the error path is untested.
	Uncovered []string
	// Missing are exports the port does not provide at all.
	Missing []string
	// Blocked are exports that stay in the original on purpose, with the reason.
	Blocked map[string]string
	// Panicked counts recorded cases where the port panicked instead of answering.
	Panicked int
	// Untested are exports with no recorded case at all: a function nobody asked about is not proved
	// by a green gate, it is simply absent from the run.
	Untested []string
}

// Check runs the extra gate conditions over a finished outcome.
//
// The rule is the one rust-anything enforces: an error path nobody recorded is a path nobody proved.
func Check(out Outcome, throws map[string]int, blocked map[string]string) GateChecks {
	checks := GateChecks{Blocked: blocked}
	for fn, lines := range throws {
		if lines == 0 {
			continue
		}
		if !out.HasErrorCase(fn) {
			checks.Uncovered = append(checks.Uncovered, fmt.Sprintf("%s (%d throw path(s))", fn, lines))
		}
	}
	sort.Strings(checks.Uncovered)
	// Every export the port carries needs at least one recorded case. Without this rule a crate can be
	// green while a whole function was never called — which happened here: a table that started with a
	// million-length answer left `pad` with no cases and the gate still said yes.
	for fn := range out.PerFn {
		_ = fn
	}
	for fn := range blocked {
		if out.PerFn[fn] == 0 {
			checks.Untested = append(checks.Untested, fn)
		}
	}
	sort.Strings(checks.Untested)
	checks.Missing = out.Missing
	for _, m := range out.Mismatch {
		if strings.HasPrefix(m.Got, "panic ") || strings.Contains(m.Got, "\"panic\"") {
			checks.Panicked++
		}
	}
	return checks
}

// HasErrorCase reports whether any recorded case for fn raised.
func (o Outcome) HasErrorCase(fn string) bool { return o.ErrorCases[fn] > 0 }

// Failed lists every reason the gate says no, in a sentence a person can act on.
func (c GateChecks) Failed(matched, total int) []string {
	var out []string
	if matched != total {
		out = append(out, fmt.Sprintf("%d of %d cases do not match", total-matched, total))
	}
	if len(c.Untested) > 0 {
		out = append(out, fmt.Sprintf("no recorded case for %s: nothing proved it", strings.Join(c.Untested, ", ")))
	}
	if len(c.Missing) > 0 {
		out = append(out, fmt.Sprintf("no port for %s", strings.Join(c.Missing, ", ")))
	}
	if len(c.Uncovered) > 0 {
		out = append(out, fmt.Sprintf("no recorded failing case for %s", strings.Join(c.Uncovered, ", ")))
	}
	if c.Panicked > 0 {
		out = append(out, fmt.Sprintf("%d case(s) made the port panic", c.Panicked))
	}
	return out
}

// Passed is true when nothing above fired.
func (c GateChecks) Passed(matched, total int) bool {
	return len(c.Failed(matched, total)) == 0 && total > 0
}
