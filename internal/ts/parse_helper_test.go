//go:build !cgo

package ts

import (
	"os"
	"path/filepath"
	"testing"
)

// The cgo-free build parses through a helper. A missing helper must say what to do rather than fail
// with a fork error, and that is what this pins down.
func TestAMissingHelperSaysWhatToDo(t *testing.T) {
	shared = helper{} // a fresh helper, so the failure path is exercised
	t.Setenv("GOLANG_ANYTHING_PARSE", filepath.Join(t.TempDir(), "absent"))
	_, err := Parse(source)
	if err == nil {
		t.Fatal("a missing helper must be an error")
	}
	for _, want := range []string{"parser helper", "GOLANG_ANYTHING_PARSE", "CGO_ENABLED=1"} {
		if !contains(err.Error(), want) {
			t.Errorf("the error should mention %q:\n%s", want, err)
		}
	}
}

// With a helper that answers, the same file parses the same way the cgo build would.
func TestTheHelperAnswersWhatTheCgoBuildWould(t *testing.T) {
	helperPath := os.Getenv("GOLANG_ANYTHING_PARSE_FOR_TEST")
	if helperPath == "" {
		t.Skip("set GOLANG_ANYTHING_PARSE_FOR_TEST to a built golang-anything-parse to run this")
	}
	shared = helper{}
	t.Setenv("GOLANG_ANYTHING_PARSE", helperPath)
	file, err := Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(file.Fns) == 0 {
		t.Errorf("the helper returned no functions: %+v", file)
	}
}

const source = "../../testdata/sample/src/text.ts"

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
