//go:build cgo

// Package parsehelper answers parse requests for a build of golang-anything that has no cgo.
//
// It reads one JSON request per line on stdin and writes one answer per line: the file tree-sitter
// produced, or the error. It exists so the tool's own binary does not pay cgo's fixed startup cost for
// the commands that never parse anything: a cgo build of this tool answers `__parse` itself, and the
// fast build starts it. One binary set, two roles.
package parsehelper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/XiaoBinGan/golang-anything/internal/ts"
)

type request struct {
	Path string `json:"path"`
	Lang string `json:"lang,omitempty"`
	Src  []byte `json:"src,omitempty"`
}

// Serve answers parse requests on stdin until it closes.
func Serve() {
	if len(os.Args) < 2 || os.Args[1] != "__parse" {
		fmt.Fprintln(os.Stderr, "golang-anything: __parse is started by the tool itself")
		os.Exit(2)
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<26)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			fmt.Fprintf(out, "{\"error\":%q}\n", "bad request: "+err.Error())
			out.Flush()
			continue
		}
		var (
			file *ts.File
			err  error
		)
		if len(req.Src) > 0 {
			lang := ts.Lang(req.Lang)
			if lang == "" {
				lang, _ = ts.LangOf(req.Path)
			}
			file, err = ts.ParseSource(req.Src, lang, req.Path)
		} else {
			file, err = ts.Parse(req.Path)
		}
		answer := struct {
			File *ts.File `json:"file,omitempty"`
			Err  string   `json:"error,omitempty"`
		}{File: file}
		if err != nil {
			answer.Err = err.Error()
		}
		body, _ := json.Marshal(answer)
		out.Write(append(body, '\n'))
		out.Flush()
	}
}
