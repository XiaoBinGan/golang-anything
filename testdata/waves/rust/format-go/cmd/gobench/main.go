// Command gobench times the port over the recorded cases, the way bench times the original.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	port "github.com/XiaoBinGan/golang-anything/testdata/waves/rust/format-go"
)

type testCase struct {
	Fn   string `json:"fn"`
	Args []any  `json:"args"`
}

type row struct {
	Name  string  `json:"name"`
	Cases int     `json:"cases"`
	NS    float64 `json:"ns"`
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
		for _, a := range args { // warm up
			_, _ = f(a)
		}
		started := time.Now()
		rounds := 0
		for {
			for i := 0; i < repeat; i++ {
				for _, a := range args {
					_, _ = f(a)
				}
			}
			rounds++
			if time.Since(started) >= time.Duration(minMs)*time.Millisecond || rounds >= 10000 {
				break
			}
		}
		per := float64(time.Since(started).Nanoseconds()) / float64(rounds*repeat*len(args))
		rows = append(rows, row{Name: fn, Cases: len(args), NS: per})
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
