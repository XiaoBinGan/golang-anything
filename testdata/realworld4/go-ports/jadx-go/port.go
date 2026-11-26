// Package port is the Go port of src/android/JadxInterruption.ts, made by golang-anything, then filled in by
// hand. The whole unit is one line: an aborted signal throws. Everything else about it — the class that
// carries the reason, the `cause` option, the name it sets — only matters to the caller that catches it.
package port

import (
	"errors"
)

// Fn is one callable unit function: arguments in, answer out.
type Fn func(args []any) (any, error)

// Registry is what the driver dispatches on. The name is the original's export name.
var Registry = map[string]Fn{
	"throwIfJadxInterrupted": throwIfJadxInterrupted,
}

// ErrJadxInterrupted is the error the original throws: a JadxInterruption whose name is AbortError.
var ErrJadxInterrupted = errors.New("JadxInterruption: Android operation interrupted")

// throwIfJadxInterrupted ports throwIfJadxInterrupted from src/android/JadxInterruption.ts.
//
// Signature: throwIfJadxInterrupted(signal?: AbortSignal): void
//
// The parameter is optional: with no signal at all there is nothing to be interrupted by, and the
// original answers undefined rather than throwing.
func throwIfJadxInterrupted(args []any) (any, error) {
	if signal, ok := args[0].(AbortSignal); ok && signal.Aborted {
		return nil, ErrJadxInterrupted
	}
	return Undefined{}, nil
}
