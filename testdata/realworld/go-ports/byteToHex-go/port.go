// Package port is the Go port of src/kernel/byteToHex.ts, made by golang-anything, then filled in by hand.
//
// The original answers `byte.toString(16).padStart(2, "0")` for every number it is given, not only for
// 0..255: `byteToHex(1.5)` is "1.8" and `byteToHex(-1000)` is "-3e8". So this is not a byte formatter;
// it is Number.prototype.toString(16) with a two-character floor, and the port has to reproduce that.
package port

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"byteToHex": byteToHex,
}

// byteToHex ports byteToHex from src/byteToHex.ts (line 6, 1 parameter).
//
// Signature: byteToHex(byte: number): string
func byteToHex(args []any) (any, error) {
	n, ok := number(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: byte must be a number")
	}
	out := radix16(n)
	// padStart counts the whole string, sign included: `byteToHex(-1)` is "-1", already two wide.
	for len(out) < 2 {
		out = "0" + out
	}
	return out, nil
}

// radix16 is Number.prototype.toString(16): integral values print exactly, and a fraction prints its
// integer part, a dot, then the hex digits of the fraction's binary expansion.
func radix16(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == 0 {
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// Split into integer and fraction, then walk the fraction bit by bit: dyadic, so it always ends.
	whole := math.Floor(f)
	frac := f - whole
	// A float64 is exact up to 2^53: the integer part fits in a uint64 for anything a caller passes.
	intPart := strconv.FormatUint(uint64(whole), 16)
	if frac == 0 {
		return sign + intPart
	}
	var b strings.Builder
	b.WriteString(intPart)
	b.WriteByte('.')
	for i := 0; i < 20 && frac > 0; i++ {
		frac *= 16
		digit := math.Floor(frac)
		b.WriteByte("0123456789abcdef"[int(digit)])
		frac -= digit
	}
	return sign + b.String()
}

// number reads a number argument as the float JavaScript uses before any coercion.
func number(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}
