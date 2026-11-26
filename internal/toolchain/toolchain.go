// Package toolchain answers which go binary to run.
//
// Two installations on one machine is normal, and a `go` on PATH that disagrees with GOROOT fails with
// "compile: version go1.22.5 does not match go tool version go1.26.2" — which reads like a broken repository
// rather than a broken environment. When GOROOT is set, the toolchain it names is the one to use.
package toolchain

import (
	"os"
	"path/filepath"
	"runtime"
)

// Go is the go command to run: $GOROOT/bin/go when that exists, otherwise whatever PATH answers.
func Go() string {
	if root := os.Getenv("GOROOT"); root != "" {
		name := "go"
		if runtime.GOOS == "windows" {
			name = "go.exe"
		}
		candidate := filepath.Join(root, "bin", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "go"
}
