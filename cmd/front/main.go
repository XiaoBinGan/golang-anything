// Command front answers --help and --version, and hands everything else to the engine.
//
// It exists for one number: a process that only prints a constant starts in about 1.8 ms, while the
// engine — twenty-seven commands, a hundred and eighteen packages — takes about 2.0 ms before it prints
// anything. `--help` is what people and scripts time, so it should not pay for the tool it describes.
//
// The handover is exec, not fork: the engine replaces this process, so a real command costs one extra
// exec (~1.5 ms on this machine) and nothing else.
package main

import (
	"fmt"

	"github.com/xiaobingan/go-anything/internal/version"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/xiaobingan/go-anything/internal/usage"
)

func main() {
	// This binary prints a constant or execs, so it has no use for the scheduler's threads. Leaving the
	// default costs a thread pool proportional to the number of cores, which on a loaded machine is time
	// spent waiting rather than working.
	runtime.GOMAXPROCS(1)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage.Text)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "--help", "-h", "help":
		os.Stdout.WriteString(usage.Text)
		return
	case "--version", "-version", "version":
		fmt.Println(version.String())
		return
	}

	engine, err := findEngine()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go-anything:", err)
		os.Exit(1)
	}
	// exec, so there is no second process to wait for and no signal forwarding to get wrong.
	if err := syscall.Exec(engine, append([]string{engine}, os.Args[1:]...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "go-anything: cannot start the engine:", err)
		os.Exit(1)
	}
}

// findEngine looks beside this binary first, then on PATH.
func findEngine() (string, error) {
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "go-anything-engine")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if path, err := filepath.Abs(os.Args[0]); err == nil {
		candidate := filepath.Join(filepath.Dir(path), "go-anything-engine")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot find go-anything-engine beside %s", os.Args[0])
}
