// Package port is the Go port of src/format.ts, made by go-anything, then filled in by hand.
//
// box is built out of util.ts's pad and repeat. It ports its own copies rather than importing the other
// crate: a crate is the unit of proof, and a crate that depended on a second one would make `gate`
// answer for code its own cases never ran.
package port

import "fmt"

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"box": box,
}

// box ports box from src/format.ts: a label padded to width-2, framed by one dash on each side.
//
// Signature: box(label: string, width: number): string
func box(args []any) (any, error) {
	label, ok := Text(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: label must be a string")
	}
	width, ok := args[1].(float64)
	if !ok {
		return nil, fmt.Errorf("TypeError: width must be a number")
	}
	// The original compares the number before truncating: `width < 4` is true for 3.9, and the throw
	// comes before pad ever sees it.
	if width < 4 {
		return nil, fmt.Errorf("RangeError: box: width must be at least 4")
	}
	inner := pad(label, int(width)-2)
	out := append([]uint16{'-'}, inner...)
	return append(out, '-'), nil
}

// pad is util.ts's padStart with spaces, on code units.
func pad(text []uint16, width int) []uint16 {
	if width <= len(text) {
		return text
	}
	out := make([]uint16, 0, width)
	for i := len(text); i < width; i++ {
		out = append(out, ' ')
	}
	return append(out, text...)
}
