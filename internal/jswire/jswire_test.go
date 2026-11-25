package jswire

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The three helpers are plain JavaScript and both a CommonJS file and an ES module embed them. If one
// stops parsing, every generated wrapper stops loading — and that is discovered by a user, not here.
func TestTheHelpersParseInBothModuleForms(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := t.TempDir()
	cjs := filepath.Join(dir, "check.cjs")
	mjs := filepath.Join(dir, "check.mjs")
	source := Decode + "\n" + Encode + "\n" + Canon + "\n" +
		"const round = encode(-0)\n" +
		"const back = decode(round)\n" +
		"console.log([canon(-0), canon(0), canon(back) === canon(-0), canon(NaN), canon(null)].join('|'))\n"
	if err := os.WriteFile(cjs, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mjs, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cjs, mjs} {
		out, err := exec.Command("node", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", filepath.Base(path), err, out)
		}
		got := strings.TrimSpace(string(out))
		// -0 and 0 must differ, the round trip must survive, and NaN must not read as null.
		if got != "\"$-0\"|0|true|\"$NaN\"|null" {
			t.Errorf("%s printed %q", filepath.Base(path), got)
		}
	}
}

// The generated files must carry this text, not a copy of it: the copies drifted once already — check
// called -0 equal to 0 while shadow did not, so a port passed one and failed the other.
func TestTheGeneratedFilesCarryTheSharedText(t *testing.T) {
	if !strings.Contains(Decode, "$utf16") || !strings.Contains(Encode, "$utf16") {
		t.Error("both directions must know the code-unit marker")
	}
	for name, text := range map[string]string{"Decode": Decode, "Encode": Encode, "Canon": Canon} {
		if strings.Contains(text, "import ") || strings.Contains(text, "export ") {
			t.Errorf("%s uses a module form, which the other loader form would reject", name)
		}
	}
}

// A platform object a caller passes in has no own enumerable fields: an AbortSignal reached the port as {}
// and a port that throws on an aborted signal looked like a port that never throws. This is the branch
// that keeps it.
func TestAnAbortSignalCrossesAsAMarker(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "signal.cjs")
	source := Encode + `
const idle = new AbortController().signal
const stopped = new AbortController()
stopped.abort(new Error('interrupted'))
console.log(JSON.stringify([encode(idle), encode(stopped.signal)]))`
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	text := strings.TrimSpace(string(out))
	if !strings.Contains(text, `"$abortSignal"`) {
		t.Fatalf("the signal did not become a marker: %s", text)
	}
	if !strings.Contains(text, `"aborted":false`) || !strings.Contains(text, `"aborted":true`) {
		t.Errorf("the aborted state is not carried: %s", text)
	}
}
