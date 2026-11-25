//go:build cgo

package ts

import "testing"

func TestParseFindsExportsWithTypes(t *testing.T) {
	src := []byte(`import { x } from './other'

export function rangeOf(text: string, word: string) {
  const start = text.indexOf(word)
  if (!word) throw new RangeError('empty')
  return { start, end: start + word.length, rest: text.slice(start) }
}

export const clamp = (n: number, lo = 0, hi?: number): number => Math.min(hi ?? 1, n)

export class Pad {
  static left(text: string, width: number): string { return text.padStart(width) }
  right(): void {}
}
`)
	f, err := ParseSource(src, TypeScript, "src/text.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !f.ParseOK {
		t.Fatal("parse errors")
	}
	if len(f.Fns) != 3 {
		t.Fatalf("want 3 fns, got %d: %+v", len(f.Fns), f.Fns)
	}
	// `right()` is an instance method: it runs on the class, not on a value a port can make.
	for _, fn := range f.Fns {
		if fn.Name == "right" {
			t.Errorf("an instance method must not be exported: %+v", fn)
		}
	}
	if f.Fns[0].Name != "rangeOf" || len(f.Fns[0].Params) != 2 || f.Fns[0].Params[0].Type != "string" {
		t.Errorf("rangeOf: %+v", f.Fns[0])
	}
	if len(f.Fns[0].ThrowLines) != 1 {
		t.Errorf("the throw should be recorded: %+v", f.Fns[0].ThrowLines)
	}
	if f.Fns[1].Name != "clamp" || f.Fns[1].Returns != "number" {
		t.Errorf("clamp: %+v", f.Fns[1])
	}
	if !f.Fns[1].Params[1].Optional || !f.Fns[1].Params[2].Optional {
		t.Errorf("defaults and `?` are optional: %+v", f.Fns[1].Params)
	}
	if f.Fns[2].Name != "left" || !f.Fns[2].Method || f.Fns[2].Receiver != "Pad" {
		t.Errorf("static method: %+v", f.Fns[2])
	}
	if len(f.Imports) != 1 || f.Imports[0].Specifier != "./other" {
		t.Errorf("imports: %+v", f.Imports)
	}
	kinds := map[string]int{}
	for _, r := range f.Risks {
		kinds[r.Kind]++
	}
	if kinds["String offsets and slicing"] == 0 {
		t.Errorf("indexOf/slice should be a risk: %+v", f.Risks)
	}
	if kinds["Exceptions"] == 0 {
		t.Errorf("throw should be a risk: %+v", f.Risks)
	}
}
