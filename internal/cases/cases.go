// Package cases is the wire: one JSON object per line, the same shape on both sides.
package cases

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Case is one call of one function: the arguments in, the answer out.
//
// `Ok` and `Err` are mutually exclusive; `Panic` carries the Go side's recovered panic text, which
// the original would have raised as an exception.
type Case struct {
	Fn     string         `json:"fn"`
	Args   []any          `json:"args"`
	Kwargs map[string]any `json:"kwargs,omitempty"`
	Ok     any            `json:"ok,omitempty"`
	Err    string         `json:"error,omitempty"`
	Panic  string         `json:"panic,omitempty"`
}

// Read loads a JSONL file of cases.
func Read(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; scan.Scan(); n++ {
		line := scan.Bytes()
		if len(line) == 0 {
			continue
		}
		var c Case
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, c)
	}
	return out, scan.Err()
}

// Write appends cases to a JSONL file.
func Write(path string, list []Case) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, c := range list {
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}
