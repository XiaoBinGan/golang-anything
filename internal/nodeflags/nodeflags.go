// Package nodeflags picks the TypeScript flag a node run needs.
//
// Node can strip types, which is fast and refuses anything that is not erasable — parameter properties,
// enums and namespaces all fail with ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX. It can also transform them, which
// runs a real TypeScript transform. A unit that extends Error with `constructor(readonly reason: unknown)`
// is ordinary TypeScript and has to load, so the transform flag is the default and the strip flag is the
// fallback for a node too old to know it.
package nodeflags

import (
	"os/exec"
	"strings"
	"sync"
)

// NodeFlag is the flag that makes node load TypeScript, or "" when node cannot.
var (
	once sync.Once
	flag string
)

// TypeScript is "--experimental-transform-types", or "--experimental-strip-types" on an older node.
func TypeScript() string {
	once.Do(func() {
		out, err := exec.Command("node", "--help").Output()
		if err != nil {
			return
		}
		if strings.Contains(string(out), "experimental-transform-types") {
			flag = "--experimental-transform-types"
			return
		}
		flag = "--experimental-strip-types"
	})
	return flag
}

// Args is the prefix every node invocation that loads TypeScript should use.
func Args() []string {
	if f := TypeScript(); f != "" {
		return []string{f, "--no-warnings=ExperimentalWarning"}
	}
	return []string{"--no-warnings=ExperimentalWarning"}
}
