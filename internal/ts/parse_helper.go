//go:build !cgo

// The parse without cgo: the work happens in a helper binary that was built with cgo, one persistent
// process per run, request and reply as JSON lines.
//
// Why bother: cgo costs a fixed 1.2 ms at startup, before any of this tool's own code runs (measured
// against a minimal cgo program, PERF.md). A CLI that only reads cases and compares answers never needs
// a parser, and this build starts in the time a Go binary takes plus its own init.
package ts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// request is one parse, or one parse of bytes the caller already has.
type request struct {
	Path string `json:"path"`
	Lang string `json:"lang,omitempty"`
	Src  []byte `json:"src,omitempty"`
}

// helper is the process and the pipe to it, started the first time a parse is asked for.
type helper struct {
	once sync.Once
	cmd  *exec.Cmd
	in   *bufio.Writer
	out  *bufio.Reader
	err  error
}

var shared helper

// helperPath is where the helper binary is: next to this one, or named by GO_ANYTHING_PARSE.
func helperPath() (string, error) {
	if path := os.Getenv("GO_ANYTHING_PARSE"); path != "" {
		return path, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	// The parser build sits beside the tool under a name that says what it is.
	for _, name := range []string{"go-anything-parse", "go-anything-parser"} {
		candidate := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("go-anything-cgo"); err == nil {
		return path, nil
	}
	// A build of this tool made with cgo can parse: the commands that need it start it as a helper.
	// That keeps the common case to one binary and a flag, not a sidecar file. The running binary is
	// skipped: handing the work back to the process that has no parser is a loop, not a fallback.
	for _, candidate := range selfCandidates(filepath.Dir(self)) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		selfInfo, err := os.Stat(self)
		if err == nil && os.SameFile(info, selfInfo) {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("no parser helper next to %s: build one with "+
		"`go build -o go-anything-parse ./cmd/parse-helper` and put it beside this binary, "+
		"set GO_ANYTHING_PARSE, or use a build made with CGO_ENABLED=1", self)
}

// start brings the helper up.
func (h *helper) start() {
	path, err := helperPath()
	if err != nil {
		h.err = err
		return
	}
	cmd := exec.Command(path, "__parse")
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		h.err = err
		return
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		h.err = err
		return
	}
	if err := cmd.Start(); err != nil {
		h.err = fmt.Errorf("starting the parser helper (%s): %w\n"+
			"this build has no cgo parser of its own: put go-anything-parse next to this binary, "+
			"or set GO_ANYTHING_PARSE, or build the tool with CGO_ENABLED=1", path, err)
		return
	}
	h.cmd = cmd
	h.in = bufio.NewWriter(in)
	h.out = bufio.NewReader(out)
}

// ask sends one request and reads one answer.
func (h *helper) ask(req request) (*File, error) {
	h.once.Do(h.start)
	if h.err != nil {
		return nil, h.err
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := h.in.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("parser helper: %w", err)
	}
	if err := h.in.Flush(); err != nil {
		return nil, fmt.Errorf("parser helper: %w", err)
	}
	reply, err := h.out.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("parser helper stopped: %w", err)
	}
	var answer struct {
		File *File  `json:"file"`
		Err  string `json:"error"`
	}
	if err := json.Unmarshal(reply, &answer); err != nil {
		return nil, fmt.Errorf("parser helper sent %q: %w", reply, err)
	}
	if answer.Err != "" {
		return nil, fmt.Errorf("%s", answer.Err)
	}
	if answer.File == nil {
		return nil, fmt.Errorf("parser helper answered nothing for %s", req.Path)
	}
	return answer.File, nil
}

// Warm starts the helper now, before the caller has anything to parse. The walk that finds the files
// is the head start: 3.2 ms of process start hides behind a directory tree instead of being paid after
// the work that found it.
func Warm() { shared.once.Do(shared.start) }

// Parse reads and parses one file, through the helper.
func Parse(path string) (*File, error) {
	if _, ok := LangOf(path); !ok {
		return &File{Path: path}, nil
	}
	return shared.ask(request{Path: path})
}

// ParseSource parses bytes the caller already has, through the helper.
func ParseSource(src []byte, lang Lang, path string) (*File, error) {
	return shared.ask(request{Path: path, Lang: string(lang), Src: src})
}

// ParserPath is where the parser would be found, asked of the code that will actually start it. doctor
// used to search again on its own and reported the missing parser for a setup that worked, because it
// only looked at GO_ANYTHING_PARSE and PATH and not next to the binary.
func ParserPath() (string, error) { return helperPath() }

// selfCandidates are the other builds of this tool that can parse, in the order worth trying.
func selfCandidates(dir string) []string {
	var out []string
	for _, name := range []string{"go-anything", "go-anything-cgo"} {
		out = append(out, filepath.Join(dir, name))
	}
	return out
}
