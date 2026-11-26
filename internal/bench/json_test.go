package bench

import (
	"encoding/json"
	"testing"
)

// A shape another tool reads is a promise, so it gets a golden string rather than a set of field checks:
// `ns` was printed in a finished Result for one commit and meant "zero, but not really", and a field check
// would have called that fine. Anything that reads `bench --json` sees exactly this.
func TestResultJSONShape(t *testing.T) {
	res := Result{
		Rows:    []Row{{Name: "twice", Cases: 17, OriginalNS: 1, PortNS: 4, Speedup: 0.25}},
		Speedup: 0.25,
		MinMS:   100,
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rows":[{"name":"twice","cases":17,"original_ns":1,"port_ns":4,"speedup":0.25}],"speedup":0.25,"min_ms":100}`
	if string(raw) != want {
		t.Errorf("bench --json shape changed\nwant %s\ngot  %s", want, raw)
	}

	// The two optional pieces are optional: a run with nothing left out says nothing about it, and a row
	// being read back in still carries `ns`.
	res.Unported = []string{"thrice"}
	raw, err = json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"rows":[{"name":"twice","cases":17,"original_ns":1,"port_ns":4,"speedup":0.25}],"speedup":0.25,"min_ms":100,"unported":["thrice"]}`
	if string(raw) != want {
		t.Errorf("the left-out list is not where a reader expects it\nwant %s\ngot  %s", want, raw)
	}
}

// readRows has to keep parsing what the measurement scripts write, including the `ns` and `unported` fields
// a Row carries while it is being read rather than after.
func TestRowsRoundTrip(t *testing.T) {
	written := `[{"name":"twice","cases":17,"ns":4.5,"unported":3}]`
	var rows []Row
	if err := json.Unmarshal([]byte(written), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].NS != 4.5 || rows[0].Unported != 3 {
		t.Fatalf("a measurement file no longer parses: %+v", rows)
	}
}
