package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The comparison inside the generated check script has to tell apart the values the wire carries as
// markers. It did not: JSON.stringify(-0) is "0", and NaN and undefined both stringify to null, so a
// port that returned 0 where the original returned -0 passed check.
func TestTheCheckScriptTellsTheMarkersApart(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	script := `const canon = ` + canonSource(t) + `
const pairs = [[-0, 0], [NaN, null], [Infinity, null], [undefined, null], [1, "1"], [true, "true"]]
let bad = 0
for (const [a, b] of pairs) {
  if (canon(a) === canon(b)) { console.log("same: " + String(a) + " and " + String(b)); bad++ }
}
process.exit(bad)`
	out, err := exec.Command("node", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("canon treats distinct values as equal:\n%s", out)
	}
	// And it must still consider equal things equal.
	same := `const canon = ` + canonSource(t) + `
process.exit(canon({a: 1, b: [true, "x"]}) === canon({b: [true, "x"], a: 1}) ? 0 : 1)`
	if out, err := exec.Command("node", "-e", same).CombinedOutput(); err != nil {
		t.Errorf("canon does not treat equal values as equal:\n%s", out)
	}
}

// canonSource pulls the function out of the check script so the test runs the code that ships.
func canonSource(t *testing.T) string {
	t.Helper()
	start := strings.Index(checkScript, "const canon = v => {")
	if start < 0 {
		t.Fatal("canon is not where the test expects it")
	}
	end := strings.Index(checkScript[start:], "\n}\n")
	if end < 0 {
		t.Fatal("canon does not end where the test expects it")
	}
	body := checkScript[start : start+end+2]
	return strings.TrimPrefix(body, "const canon = ")
}
