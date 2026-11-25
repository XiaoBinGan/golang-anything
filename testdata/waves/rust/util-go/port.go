// Package port is the Go port of src/util.ts, made by go-anything, then filled in by hand.
//
// Both functions pad by UTF-16 code units, not by runes: `pad('😀', 3)` is two units wide, so the
// port works on units and the wire spells what Go cannot hold.
package port

import (
	"fmt"
	"unicode/utf16"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"pad":    pad,
	"repeat": repeat,
}

// units reads a string argument as UTF-16 code units, the way JavaScript sees it.
func units(v any) ([]uint16, bool) {
	return Text(v)
}

// number reads a number argument as the float JavaScript uses before any coercion.
func number(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	if f != f { // NaN
		return 0, true
	}
	if f > 1e9 {
		return 1e9, true
	}
	if f < -1e9 {
		return -1e9, true
	}
	return f, true
}

// count reads an integer argument, truncated toward zero: `padStart` takes a length.
func count(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	if f != f { // NaN
		return 0, true
	}
	return int(f), true
}

// pad ports pad from src/util.ts: padStart with spaces.
func pad(args []any) (any, error) {
	text, ok := units(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: text must be a string")
	}
	width, ok := count(args[1])
	if !ok {
		return nil, fmt.Errorf("TypeError: width must be a number")
	}
	if width <= len(text) {
		return text, nil
	}
	out := make([]uint16, 0, width)
	for i := len(text); i < width; i++ {
		out = append(out, ' ')
	}
	return append(out, text...), nil
}

// repeat ports repeat from src/util.ts: the string, times over.
func repeat(args []any) (any, error) {
	text, ok := units(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: text must be a string")
	}
	times, ok := number(args[1])
	if !ok {
		return nil, fmt.Errorf("TypeError: times must be a number")
	}
	if times <= 0 || len(text) == 0 {
		return []uint16{}, nil
	}
	// The original is a loop, not String.repeat: `i < 1.5` runs twice, so a fractional count
	// rounds up. This is the difference the recorded case caught.
	out := make([]uint16, 0, len(text)*int(times+1))
	for i := 0.0; i < times; i++ {
		out = append(out, text...)
	}
	return out, nil
}

var _ = utf16.Encode
