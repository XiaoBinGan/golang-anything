//go:build !cgo

package parsehelper

import (
	"fmt"
	"os"
)

// Serve exists in this build only to say why it cannot: a build without cgo has no parser, so the
// `__parse` role belongs to a build that has one.
func Serve() {
	fmt.Fprintln(os.Stderr, "go-anything: this build has no parser (`__parse` needs a build made with CGO_ENABLED=1)")
}
