package bench

import (
	"strings"
	"testing"
)

// A row whose every call answered the scaffold is not a port. Half a row is a port with a hole: dropping it
// would hide a function that never runs, keeping it would print a ratio for fmt.Errorf, so only the whole
// row goes.
func TestOnlyAWholeStubRowIsDropped(t *testing.T) {
	cases := []struct {
		row  Row
		stub bool
	}{
		{Row{Cases: 17, Unported: 17}, true},
		{Row{Cases: 17, Unported: 0}, false},
		{Row{Cases: 17, Unported: 3}, false},
		{Row{Cases: 0, Unported: 0}, false},
	}
	for _, c := range cases {
		if got := isStub(c.row); got != c.stub {
			t.Errorf("isStub(%+v) = %v, want %v", c.row, got, c.stub)
		}
	}
}

// A crate with one ported function and one stub prints one row. The missing row has to be named, or a reader
// comparing two crates sees a table that is short for no stated reason.
func TestLeftOutRowsAreNamedInTheTable(t *testing.T) {
	res := Result{
		Rows:     []Row{{Name: "twice", Cases: 17, OriginalNS: 2, PortNS: 5, Speedup: 0.4}},
		Speedup:  0.4,
		MinMS:    100,
		Unported: []string{"thrice"},
	}
	out := res.Markdown()
	if !strings.Contains(out, "Left out: `thrice`") {
		t.Errorf("the stub row is not named:\n%s", out)
	}
	if strings.Contains(out, "Left out") && strings.Contains(out, "twice`") && !strings.Contains(out, "`thrice`") {
		t.Errorf("the ported row was named as left out:\n%s", out)
	}
	clean := Result{Rows: res.Rows, Speedup: 0.4, MinMS: 100}
	if strings.Contains(clean.Markdown(), "Left out") {
		t.Errorf("a run with nothing left out says something was:\n%s", clean.Markdown())
	}
}
