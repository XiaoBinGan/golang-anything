// Command godriver answers a cases.jsonl from the port, so diff can compare the two sides.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/xiaobingan/go-anything/testdata/realworld4/go-ports/jadx-go"
)

type testCase struct {
	Fn     string         `json:"fn"`
	Args   []any          `json:"args"`
	Kwargs map[string]any `json:"kwargs,omitempty"`
}

type answer struct {
	Fn    string `json:"fn"`
	Args  []any  `json:"args,omitempty"`
	Ok    any    `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
	Panic string `json:"panic,omitempty"`
}

func main() {
	in, err := os.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	defer in.Close()
	out, err := os.Create(os.Args[2])
	if err != nil {
		fail(err)
	}
	defer out.Close()
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	w := bufio.NewWriter(out)
	defer w.Flush()
	enc := json.NewEncoder(w)
	for scan.Scan() {
		var c testCase
		if err := json.Unmarshal(scan.Bytes(), &c); err != nil {
			fail(err)
		}
		enc.Encode(answerOne(c))
	}
	if err := scan.Err(); err != nil {
		fail(err)
	}
}

// answerOne calls the port and turns a panic into an error, as the bridge must never crash the run.
func answerOne(c testCase) (a answer) {
	a.Fn = c.Fn
	// Echo the arguments: diff joins the two sides by the call, not by line number.
	a.Args = c.Args
	fn, ok := port.Registry[c.Fn]
	if !ok {
		a.Error = "not ported: " + c.Fn
		return a
	}
	defer func() {
		if r := recover(); r != nil {
			a.Ok = nil
			a.Error = ""
			a.Panic = fmt.Sprint(r)
		}
	}()
	v, err := fn(port.DecodeArgs(c.Args))
	if err != nil {
		a.Error = err.Error()
		return a
	}
	a.Ok = mark(v)
	return a
}

// mark writes NaN, ±Infinity, -0 and ints too large for JSON the way the original's codec does, so
// diff compares values rather than spellings.
func mark(v any) any {
	switch x := v.(type) {
	case port.Undefined:
		// The crate's Undefined: JavaScript's undefined, which nil would read as null.
		return map[string]any{"$undefined": true}
	case float64:
		switch {
		case math.IsNaN(x):
			return map[string]any{"$number": "NaN"}
		case math.IsInf(x, 1):
			return map[string]any{"$number": "Infinity"}
		case math.IsInf(x, -1):
			return map[string]any{"$number": "-Infinity"}
		case x == 0 && math.Signbit(x):
			return map[string]any{"$number": "-0"}
		}
		return x
	case []uint16:
		// Code units: the only way a lone surrogate survives the JSON boundary.
		out := make([]any, len(x))
		for i, u := range x {
			out[i] = float64(u)
		}
		return map[string]any{"$utf16": out}
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = mark(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = mark(item)
		}
		return out
	}
	return v
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "godriver:", err)
	os.Exit(1)
}
