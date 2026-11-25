package wire

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMarkersRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want func(float64) bool
	}{
		{`{"$number":"NaN"}`, math.IsNaN},
		{`{"$number":"Infinity"}`, func(f float64) bool { return math.IsInf(f, 1) }},
		{`{"$number":"-Infinity"}`, func(f float64) bool { return math.IsInf(f, -1) }},
		{`{"$number":"-0"}`, func(f float64) bool { return f == 0 && math.Signbit(f) }},
	} {
		var v any
		if err := json.Unmarshal([]byte(tc.in), &v); err != nil {
			t.Fatal(err)
		}
		got, ok := Decode(v).(float64)
		if !ok || !tc.want(got) {
			t.Errorf("%s decoded to %#v", tc.in, Decode(v))
		}
	}
	if got := Decode(map[string]any{"$bigint": "7"}); got != "7" {
		t.Errorf("bigint: %#v", got)
	}
	if got := Decode(map[string]any{"$undefined": true}); got != nil {
		t.Errorf("undefined: %#v", got)
	}
	// A real object keeps its keys.
	got := Decode(map[string]any{"a": float64(1), "$b": true}).(map[string]any)
	if got["a"] != float64(1) || len(got) != 2 {
		t.Errorf("object: %#v", got)
	}
}
