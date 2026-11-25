package buildinfo

import "testing"

// The constant has to match the build: a cgo-free build that claimed to have a parser would answer
// `doctor` with "this build parses TypeScript itself (cgo)" and then fail on the first analyze.
func TestCgoMatchesTheTag(t *testing.T) {
	if Cgo != true {
		t.Skip("this test binary was built without cgo")
	}
}
