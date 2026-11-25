// Package port is the Go port of src/domain/elfAttributeUleb.ts, made by go-anything, then filled in by
// hand. The unit is three lines and its whole content is the call it makes: leb128's reader, with the
// 32-bit cap, answering `undefined` where the reader reports failure.
package port

import (
	"fmt"
	"math"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"readElfAttributeUleb": readElfAttributeUleb,
}

// readElfAttributeUleb ports readElfAttributeUleb from src/domain/elfAttributeUleb.ts.
//
// Signature: readElfAttributeUleb(bytes: Uint8Array, offset: number, end: number):
//
//	{ value: number; next: number } | undefined
func readElfAttributeUleb(args []any) (any, error) {
	bytes, ok := bytesOf(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: bytes must be a Uint8Array")
	}
	offset, ok := args[1].(float64)
	if !ok {
		return nil, fmt.Errorf("TypeError: offset must be a number")
	}
	end, ok := args[2].(float64)
	if !ok {
		return nil, fmt.Errorf("TypeError: end must be a number")
	}
	value, next, ok := readLeb128(bytes, offset, end)
	if !ok {
		// The original answers undefined, which the wire spells as a marker of its own.
		return Undefined{}, nil
	}
	return map[string]any{"value": value, "next": next}, nil
}

// bytesOf reads a byte array argument: the wire carries it as an array of numbers.
func bytesOf(v any) ([]byte, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]byte, len(list))
	for i, item := range list {
		f, ok := item.(float64)
		if !ok {
			return nil, false
		}
		out[i] = byte(int64(f) & 0xff)
	}
	return out, true
}

// readLeb128 ports leb128's reader with the 32-bit bound. Two details decide whether it matches:
//
//   - `bytes[index]` with a fractional index is undefined in JavaScript, so the payload is 0 and there is
//     no continuation bit: `readElfAttributeUleb(bytes, 0.3, 1)` answers {value: 0, next: 1.3}, not a
//     failure;
//   - the width is checked on every byte: bits past 32 must be zero, and the read fails rather than
//     wrapping.
func readLeb128(bytes []byte, offset, end float64) (float64, float64, bool) {
	limit := math.Min(end, float64(len(bytes)))
	maximumBytes := 5 // ceil(32 / 7)
	value := uint64(0)
	shift := 0
	for index, count := offset, 0; ; index, count = index+1, count+1 {
		if count >= maximumBytes {
			return 0, 0, false // overflow
		}
		if index < 0 || index >= limit || math.IsNaN(index) {
			return 0, 0, false // truncated
		}
		// A TypedArray has no element at a fractional index.
		b := 0.0
		if index == math.Trunc(index) {
			b = float64(bytes[int(index)])
		}
		payload := int(b) & 0x7f
		if shift+7 > 32 {
			kept := 32 - shift
			if payload>>kept != 0 {
				return 0, 0, false // overflow
			}
			value |= uint64(payload&((1<<kept)-1)) << uint(shift)
		} else {
			value |= uint64(payload) << uint(shift)
		}
		shift += 7
		if int(b)&0x80 == 0 {
			return float64(value), index + 1, true
		}
	}
}
