// Package cases is the wire: one JSON object per line, the same shape on both sides.
package cases

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// Skip marks a call neither side should be compared on: the original could not answer it within the
	// recording's limits (its answer was gigabytes, or the run ran out of time).
	Skip string `json:"skip,omitempty"`
}

// Read loads a JSONL file of cases.
func Read(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	// A reader, not a Scanner: one case can be a few hundred kilobytes (a long argument, a long
	// answer), and a Scanner stops at its buffer with "token too long" — which read as the tool being
	// broken rather than the case being big.
	reader := bufio.NewReaderSize(f, 1<<20)
	const maxLine = 64 << 20
	for n := 1; ; n++ {
		line, err := readLine(reader, maxLine)
		if len(line) > 0 {
			var c Case
			if jsonErr := json.Unmarshal(line, &c); jsonErr != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n, jsonErr)
			}
			out = append(out, c)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			if err == errTooLong {
				return nil, fmt.Errorf("%s:%d: the line is longer than 64 MB", path, n)
			}
			return nil, err
		}
	}
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

var errTooLong = errors.New("line too long")

// readLine reads to a newline, dropping the rest of an oversized line and saying so.
func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(buf)+len(chunk) > limit {
			for err == bufio.ErrBufferFull {
				if _, err = r.ReadSlice('\n'); err != nil && err != bufio.ErrBufferFull {
					break
				}
			}
			return nil, errTooLong
		}
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), err
	}
}
