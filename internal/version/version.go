// Package version answers what build this is, from the build info the go command stamps in.
//
// Both binaries use it: the front answers --version before loading the engine, and the engine answers it
// for the commands that ask later. One function, so the two cannot disagree.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// String is what `--version` prints.
func String() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "golang-anything (unknown build)"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		v = "devel"
	}
	return fmt.Sprintf("golang-anything %s (%s)", v, runtime.Version())
}
