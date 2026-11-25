// Package wire is the JSON both sides agree on.
//
// JSON cannot say NaN, ±Infinity, -0, undefined or a bigint, so both sides carry them as markers,
// the way rust-anything's codec does: {"$number":"NaN"}, {"$number":"-0"}, {"$undefined":true},
// {"$bigint":"7"}. Comparing raw JSON would report every such answer as a mismatch.
package wire

import "math"

// UTF16 is a JavaScript string that has no valid UTF-8 form: a lone surrogate survives as code units
// and nothing else. Go would replace it with U+FFFD, so the wire carries the units.
type UTF16 []uint16

// Decode turns a marker back into a value Go can compare.
func Decode(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = Decode(item)
		}
		return out
	case map[string]any:
		if len(x) == 1 {
			if n, ok := x["$number"].(string); ok {
				switch n {
				case "NaN":
					return math.NaN()
				case "Infinity":
					return math.Inf(1)
				case "-Infinity":
					return math.Inf(-1)
				case "-0":
					return math.Copysign(0, -1)
				}
			}
			if big, ok := x["$bigint"].(string); ok {
				return big
			}
			if units, ok := x["$utf16"].([]any); ok {
				out := make(UTF16, 0, len(units))
				for _, u := range units {
					f, ok := u.(float64)
					if !ok {
						return v
					}
					out = append(out, uint16(f))
				}
				return out
			}
			if _, ok := x["$undefined"]; ok {
				return nil
			}
		}
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = Decode(item)
		}
		return out
	}
	return v
}
