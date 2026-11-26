// Package port is the Go port of src/domain/desktop/shellQuote.ts, made by golang-anything, then filled in
// by hand. It is three lines of TypeScript and the whole job is in the replaceAll: every single quote
// becomes `'"'"'`, which closes the quote, writes a double-quoted quote, and reopens.
package port

import (
	"fmt"
	"strings"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"shellQuote": shellQuote,
}

// shellQuote ports shellQuote from src/shellQuote.ts.
//
// Signature: shellQuote(value: string): string
func shellQuote(args []any) (any, error) {
	value, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("TypeError: value must be a string")
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'", nil
}
