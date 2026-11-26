package bench

import (
	"strings"
	"testing"
)

// A literal argument was a compile-time constant, and clang folded the call away: the original measured
// 1 ns/call for a function whose whole body is `return 42`, and the report called the Go port 13x slower
// than a call that never happened. Every numeric argument has to go through a volatile.
func TestTheCHarnessDoesNotHandTheCompilerConstants(t *testing.T) {
	src := CRSource(map[string][]string{"magic": {"int"}}, false)
	if !strings.Contains(src, "volatile") {
		t.Fatal("the harness passes literals straight through: clang folds the call and the row measures the loop")
	}
	if strings.Contains(src, "magic(7L)") || strings.Contains(src, "magic(7)") {
		t.Errorf("a literal call survived in the harness:\n%s", src)
	}
}

// A string argument has to stay a literal: `__typeof__("x")` is an array type, and reading one through a
// pointer cast is not C.
func TestTheCHarnessLeavesStringArgumentsAlone(t *testing.T) {
	src := CRSource(map[string][]string{"len_of": {"char"}}, false)
	if strings.Contains(src, `__typeof__("`) {
		t.Errorf("a string literal was wrapped as a numeric argument:\n%s", src)
	}
}

// The C++ harness shares the generator, and `__typeof__` in a try block is still a declaration: the
// generated file has to keep compiling as C++ too.
func TestTheCPPAndCHarnessesAgreeOnTheArguments(t *testing.T) {
	c := CRSource(map[string][]string{"magic": {"int"}}, false)
	cpp := CRSource(map[string][]string{"magic": {"int"}}, true)
	if strings.Count(c, "volatile") != strings.Count(cpp, "volatile") {
		t.Errorf("the C and C++ harnesses wrap different numbers of arguments: %d vs %d",
			strings.Count(c, "volatile"), strings.Count(cpp, "volatile"))
	}
}
