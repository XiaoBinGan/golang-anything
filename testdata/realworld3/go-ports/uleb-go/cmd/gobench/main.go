// Command gobench times the port over the recorded cases, the way bench times the original.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	port "github.com/XiaoBinGan/golang-anything/testdata/realworld3/go-ports/uleb-go"
)

type testCase struct {
	Fn   string `json:"fn"`
	Args []any  `json:"args"`
}

type row struct {
	Name     string  `json:"name"`
	Cases    int     `json:"cases"`
	NS       float64 `json:"ns"`
	Unported int     `json:"unported,omitempty"`
}

func main() {
	in, err := os.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	defer in.Close()
	out := os.Args[2]
	minMs := 50
	repeat := 1
	fmt.Sscan(os.Args[3], &minMs)
	fmt.Sscan(os.Args[4], &repeat)

	var all []testCase
	dec := json.NewDecoder(in)
	for {
		var c testCase
		if err := dec.Decode(&c); err != nil {
			break
		}
		all = append(all, c)
	}
	byFn := map[string][][]any{}
	order := []string{}
	for _, c := range all {
		if _, ok := byFn[c.Fn]; !ok {
			order = append(order, c.Fn)
		}
		byFn[c.Fn] = append(byFn[c.Fn], port.DecodeArgs(c.Args))
	}
	var rows []row
	for _, fn := range order {
		f, ok := port.Registry[fn]
		if !ok {
			continue
		}
		args := byFn[fn]
		unported := 0
		for _, a := range args { // warm up; counting here keeps the timed loop free of a string check
			if _, err := f(a); err != nil && strings.Contains(err.Error(), "not implemented: port ") {
				unported++
			}
		}
		// Per round, then the median: one scheduling hiccup inside a single time.Now() around the whole loop
		// moved the answer by 40% (5 ns against 7 ns on the same crate, five runs), and a mean keeps that
		// hiccup. The median discards it, which is the difference between "this crate is 3 ns" and "this
		// crate was 3 ns on the run that did not get interrupted".
		// A sample has to span more than a few calls. Seventeen calls are ~85 ns of work, and the two clock
		// reads around them are a third of that: measuring seventeen calls per sample reported 4.882 ns
		// where 4352 calls per sample report 3.973, on the same crate and the same loop.
		const chunk = 64
		started := time.Now()
		var samples []float64
		for {
			chunkStart := time.Now()
			for k := 0; k < chunk; k++ {
				for i := 0; i < repeat; i++ {
					for _, a := range args {
						_, _ = f(a)
					}
				}
			}
			samples = append(samples, float64(time.Since(chunkStart).Nanoseconds())/float64(chunk*repeat*len(args)))
			if time.Since(started) >= time.Duration(minMs)*time.Millisecond || len(samples) >= 200000 {
				break
			}
		}
		sort.Float64s(samples)
		per := samples[len(samples)/2]
		rows = append(rows, row{Name: fn, Cases: len(args), NS: per, Unported: unported})
	}
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gobench:", err)
	os.Exit(1)
}
