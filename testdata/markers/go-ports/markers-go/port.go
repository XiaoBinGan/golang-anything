// Package port is the Go port of src/markers.ts: one function per value JSON cannot carry.
//
// It exists to hold every bridge to the same contract. A marker one side writes and another does not
// read turns into a silent mismatch the first time a caller hits it, and the only way to know is to
// have a case for each.
package port

import (
	"fmt"
	"math"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on.
var Registry = map[string]Fn{
	"half":           half,
	"divide":         divide,
	"negativeZero":   negativeZero,
	"keep":           keep,
	"loneSurrogate":  loneSurrogate,
	"undefinedValue": undefinedValue,
	"isNegativeZero": isNegativeZero,
}

func number(args []any, i int) (float64, bool) {
	f, ok := args[i].(float64)
	return f, ok
}

// half ports half: n / 2, with the floats the way JavaScript has them.
func half(args []any) (any, error) {
	n, ok := number(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: n must be a number")
	}
	return n / 2, nil
}

// divide ports divide: a / b. Zero over zero is NaN, one over zero is Infinity: both cross the wire as
// markers, so the port has to produce the values, not the spellings.
func divide(args []any) (any, error) {
	a, ok := number(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: a must be a number")
	}
	b, ok := number(args, 1)
	if !ok {
		return nil, fmt.Errorf("TypeError: b must be a number")
	}
	return a / b, nil
}

// negativeZero ports negativeZero: -0 has no JSON spelling of its own.
func negativeZero(args []any) (any, error) { return math.Copysign(0, -1), nil }

// keep ports keep: the argument back, whatever it is.
func keep(args []any) (any, error) { return args[0], nil }

// loneSurrogate ports loneSurrogate: a string whose only content is a high surrogate. It has no UTF-8
// form, so the wire carries the code unit.
func loneSurrogate(args []any) (any, error) { return []uint16{0xd800}, nil }

// undefinedValue ports undefinedValue: undefined is not null.
func undefinedValue(args []any) (any, error) { return Undefined{}, nil }

// isNegativeZero ports isNegativeZero: a marker that survives the round trip must still compare equal.
func isNegativeZero(args []any) (any, error) {
	n, ok := number(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: n must be a number")
	}
	return n == 0 && math.Signbit(n), nil
}
