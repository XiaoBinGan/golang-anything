// Package port is the Go port of src/text.ts, made by golang-anything, then filled in by hand.
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
	"strings"
	"unicode/utf16"
	"unsafe"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on: the typed functions below, adapted to the []any shape a
// recorded case carries. The typed form is what the WebAssembly module calls directly — that is where
// a call stops paying for boxing.
var Registry = map[string]Fn{
	"rangeOf": adapt2s(rangeOf),
	"words":   adapt1(words),
	"count":   adapt1(count),
	"ratio":   adapt2(ratio),
}

// Count is the port of `count`, typed.
func Count(text string) (float64, error) { return count(text) }

// Ratio is the port of `ratio`, typed.
func Ratio(part, whole float64) (float64, error) { return ratio(part, whole) }

// Words is the port of `words`, typed.
func Words(text string) ([]string, error) { return words(text) }

// adapt1 turns (string) -> (T, error) into the registry's shape.
func adapt1[T any](fn func(string) (T, error)) Fn {
	return func(args []any) (any, error) {
		s, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected a string, got %T", args[0])
		}
		return fn(s)
	}
}

// adapt2s turns (string, string) -> (T, error) into the registry's shape.
func adapt2s[T any](fn func(string, string) (T, error)) Fn {
	return func(args []any) (any, error) {
		a, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected a string, got %T", args[0])
		}
		b, ok := args[1].(string)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected a string, got %T", args[1])
		}
		return fn(a, b)
	}
}

// adapt2 turns (float64, float64) -> (T, error) into the registry's shape.
func adapt2[T any](fn func(float64, float64) (T, error)) Fn {
	return func(args []any) (any, error) {
		a, ok := args[0].(float64)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected a number, got %T", args[0])
		}
		b, ok := args[1].(float64)
		if !ok {
			return nil, fmt.Errorf("TypeError: expected a number, got %T", args[1])
		}
		return fn(a, b)
	}
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
func rangeOf(text, word string) (map[string]any, error) {
	haystack := utf16.Encode([]rune(text))
	needle := utf16.Encode([]rune(word))
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
func words(text string) ([]string, error) {
	out := []string{}
	runes := []rune(text)
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
func count(text string) (float64, error) {
	n := 0
	for _, u := range text {
		if u == ' ' {
			n++
		}
	}
	return float64(n), nil
}

// ratio ports ratio: a RangeError when the denominator is zero, as the original raises.
func ratio(part, whole float64) (float64, error) {
	if whole == 0 {
		return 0, fmt.Errorf("RangeError: ratio: whole must not be zero")
	}
	return part / whole, nil
}

// indexOf is string.indexOf: the first index of needle, or -1.
//
// The units are compared as UTF-16LE bytes so strings.Index can do the search: it is SIMD-backed and
// looks for two-byte aligned matches, which is exactly what a code-unit search is. Hand-rolling the
// loop cost 14x against the original, and `bench` is what said so.
func indexOf(haystack, needle []uint16) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	at := strings.Index(utf16Bytes(haystack), utf16Bytes(needle))
	if at < 0 {
		return -1
	}
	return at / 2
}

// utf16Bytes views code units as the bytes a UTF-16LE string would have, without copying.
func utf16Bytes(units []uint16) string {
	if len(units) == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Pointer(&units[0])), len(units)*2)
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
