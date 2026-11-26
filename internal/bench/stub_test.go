package bench

import "testing"

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
