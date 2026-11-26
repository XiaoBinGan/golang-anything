//go:build cgo

package ts

import (
	"strings"
	"testing"
)

// The Python walker has to find the module-level functions and leave the rest out: a name starting with an
// underscore is private, and a method takes self.
func TestPythonExports(t *testing.T) {
	src := []byte(`import os

def add(a, b):
    return a + b

def typed(name: str, count: int = 0) -> str:
    return name

def _private(x):
    return x

class Point:
    def __init__(self, x, y):
        self.x = x
`)
	file, err := ParseSource(src, Python, "mathx.py")
	if err != nil {
		t.Fatal(err)
	}
	if !file.ParseOK {
		t.Error("the sample did not parse")
	}
	names := map[string]Fn{}
	for _, fn := range file.Fns {
		names[fn.Name] = fn
	}
	for _, want := range []string{"add", "typed"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing export %q: %+v", want, file.Fns)
		}
	}
	for _, unwanted := range []string{"_private", "__init__"} {
		if _, ok := names[unwanted]; ok {
			t.Errorf("%q should not be an export", unwanted)
		}
	}
	if got := names["typed"].Params; len(got) != 2 || got[0].Type != "str" || got[1].Type != "int" {
		t.Errorf("annotations were not read: %+v", got)
	}
	if got := names["add"].Params; len(got) != 2 || got[0].Type != "any" {
		t.Errorf("unannotated parameters should be any: %+v", got)
	}
}

// A file this tool cannot read must be named, not silently skipped.
func TestLangOfKnowsWhatItCannotRead(t *testing.T) {
	if _, ok := LangOf("Main.java"); ok {
		t.Error("LangOf claims to read Java")
	}
	if lang, ok := LangOf("app.py"); !ok || lang != Python {
		t.Errorf("app.py: %v %v", lang, ok)
	}
	if !strings.HasSuffix(string(Python), "python") {
		t.Errorf("the language name is %q", Python)
	}
}
