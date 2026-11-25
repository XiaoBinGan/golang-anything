// Package port is the Go port of src/domain/jsonPointer.ts: the four RFC 6901 helpers.
//
// The one subtlety the recording pins down is the order of the two replaces in unescape: "~1" first and
// "~0" second. Swapping them turns "~01" into "~1" and then into "/", which is not what the original
// does — and there is a case for it.
package port

import (
	"fmt"
	"strings"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on.
var Registry = map[string]Fn{
	"escapeJsonPointerToken":   escapeToken,
	"unescapeJsonPointerToken": unescapeToken,
	"jsonPointerSyntaxProblem": syntaxProblem,
	"jsonPointerTokens":        tokens,
}

func text(args []any, i int) (string, bool) {
	s, ok := args[i].(string)
	return s, ok
}

// escapeToken ports escapeJsonPointerToken: "~" becomes "~0" and "/" becomes "~1", in that order.
func escapeToken(args []any) (any, error) {
	token, ok := text(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: token must be a string")
	}
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"), nil
}

// unescapeToken ports unescapeJsonPointerToken: the reverse order matters.
func unescapeToken(args []any) (any, error) {
	token, ok := text(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: token must be a string")
	}
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~"), nil
}

// syntaxProblem ports jsonPointerSyntaxProblem: the empty pointer is fine, a pointer that does not start
// with "/" is not, and "~" followed by anything but 0 or 1 is an invalid escape.
func syntaxProblem(args []any) (any, error) {
	pointer, ok := text(args, 0)
	if !ok {
		return nil, fmt.Errorf("TypeError: pointer must be a string")
	}
	if pointer == "" {
		return nil, nil // the original answers null here, which is the wire's null
	}
	if !strings.HasPrefix(pointer, "/") {
		return "missing-slash", nil
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] != '~' {
			continue
		}
		if i+1 >= len(pointer) || (pointer[i+1] != '0' && pointer[i+1] != '1') {
			return "invalid-escape", nil
		}
	}
	return nil, nil
}

// tokens ports jsonPointerTokens: drop the leading unit and split on the rest.
//
// The slice is by UTF-16 code unit, not by byte: `pointer[1:]` in Go cut one byte off a multi-byte
// character and the recording caught it — jsonPointerTokens("中文 字") answered a broken first token.
func tokens(args []any) (any, error) {
	units, ok := Text(args[0])
	if !ok {
		return nil, fmt.Errorf("TypeError: pointer must be a string")
	}
	if len(units) == 0 {
		return []any{}, nil
	}
	var out []any
	var part []uint16
	flush := func() {
		out = append(out, unescapeUnits(part))
		part = nil
	}
	for _, u := range units[1:] {
		if u == '/' {
			flush()
			continue
		}
		part = append(part, u)
	}
	flush()
	return out, nil
}

// unescapeUnits is the same two replaces as unescapeToken, on code units.
func unescapeUnits(units []uint16) []uint16 {
	out := make([]uint16, 0, len(units))
	for i := 0; i < len(units); i++ {
		if units[i] == '~' && i+1 < len(units) && units[i+1] == '1' {
			out = append(out, '/')
			i++
			continue
		}
		if units[i] == '~' && i+1 < len(units) && units[i+1] == '0' {
			out = append(out, '~')
			i++
			continue
		}
		out = append(out, units[i])
	}
	return out
}
