// Package port is the Go port of src/text.ts, made by go-anything, then filled in by hand.
//
// The two functions that look trivial are not:
//
//   - JavaScript indexes strings in UTF-16 code units, so this port works on []uint16. A tail that
//     starts on the second half of a surrogate pair stays a lone code unit; Go has no such string, so
//     the answer travels as code units and the wire spells it.
//   - `words` is `/(?<=\$)\w+/g`, and Go's regexp has no lookbehind, so the scan is written by hand.
package port

import (
	"fmt"
	"unicode/utf16"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on.
var Registry = map[string]Fn{
	"rangeOf": rangeOf,
	"words":   words,
	"count":   count,
	"ratio":   ratio,
}

// text returns the argument as UTF-16 code units, the way JavaScript sees a string.
func text(v any) ([]uint16, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected a string, got %T", v)
	}
	return utf16.Encode([]rune(s)), nil
}

// rangeOf ports rangeOf from src/text.ts: indexOf, then `end` and the tail from that index.
func rangeOf(args []any) (any, error) {
	haystack, err := text(args[0])
	if err != nil {
		return nil, err
	}
	needle, err := text(args[1])
	if err != nil {
		return nil, err
	}
	start := indexOf(haystack, needle) // -1 when absent, as indexOf says
	return map[string]any{
		"start": start,
		"end":   start + len(needle),
		// The tail can start on the second half of a surrogate pair; JavaScript keeps that half, so
		// the port hands back code units and lets the wire spell them.
		"rest": slice(haystack, start),
	}, nil
}

// words ports words: every `\w+` that follows a `$`, with no lookbehind in the pattern.
func words(args []any) (any, error) {
	units, err := text(args[0])
	if err != nil {
		return nil, err
	}
	out := []string{}
	runes := utf16.Decode(units)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(runes) && isWordRune(runes[j]) {
			j++
		}
		if j > i+1 {
			out = append(out, string(runes[i+1:j]))
		}
		i = j - 1
	}
	return out, nil
}

// count ports count: ASCII spaces, counted in code units (the same as runes for a space).
func count(args []any) (any, error) {
	units, err := text(args[0])
	if err != nil {
		return nil, err
	}
	n := 0
	for _, u := range units {
		if u == ' ' {
			n++
		}
	}
	return n, nil
}

// ratio ports ratio: a RangeError when the denominator is zero, as the original raises.
func ratio(args []any) (any, error) {
	part, ok := args[0].(float64)
	if !ok {
		return nil, nil // the original answers NaN for a non-number
	}
	whole, ok := args[1].(float64)
	if !ok {
		return nil, nil
	}
	if whole == 0 {
		return nil, fmt.Errorf("RangeError: ratio: whole must not be zero")
	}
	return part / whole, nil
}

// indexOf is string.indexOf: the first index of needle, or -1.
func indexOf(haystack, needle []uint16) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// slice is string.slice with one index: a negative index counts from the end, past the end stops.
func slice(units []uint16, from int) []uint16 {
	if from < 0 {
		from = len(units) + from
		if from < 0 {
			from = 0
		}
	}
	if from > len(units) {
		return []uint16{}
	}
	return units[from:]
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
