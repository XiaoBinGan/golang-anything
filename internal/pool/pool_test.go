package pool

import (
	"strings"
	"testing"
)

// Every structural character must be reachable in both generators, or a port that mishandles one passes
// the generator that never produces it. The two lists drifted three times before they became one.
func TestBothGeneratorsCarryEveryStructuralCharacter(t *testing.T) {
	// The list is written out here on purpose: a test that reads the same constant it checks cannot
	// notice a character being removed from it. These are the characters a function's job is made of —
	// the quote that shellQuote escapes, the slash and tilde that a JSON-pointer escaper escapes, the
	// separators a splitter splits on.
	const must = "'\"\\/`|;&<>()[]{}*?~#%!.,:=@+$"
	values := strings.Join(Values(), "\u0000")
	alphabet := string(Alphabet())
	for _, r := range must {
		if !strings.ContainsRune(Structural, r) {
			t.Errorf("%q is not in the pool at all", r)
		}
		if !strings.ContainsRune(alphabet, r) {
			t.Errorf("the fuzz alphabet cannot produce %q", r)
		}
		if !strings.ContainsRune(values, r) {
			t.Errorf("the recorder's table has no value containing %q", r)
		}
	}
}

// The JavaScript literal has to survive the characters that would end it: a lone quote or backslash in
// the table would break the generated recorder script.
func TestTheJavaScriptLiteralIsQuoted(t *testing.T) {
	js := JSValues()
	if !strings.HasPrefix(js, "[") || !strings.HasSuffix(js, "]") {
		t.Fatalf("not an array literal: %.60s", js)
	}
	// Count the quotes: one opening and one closing per value, the rest must be escaped.
	depth, escaped := 0, false
	for _, r := range js {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '\'':
			depth++
		}
	}
	if depth%2 != 0 {
		t.Errorf("odd number of quotes (%d): the literal is unbalanced", depth)
	}
	if !strings.ContainsRune(js, '\\') {
		t.Error("nothing is escaped, yet the table holds a backslash and a quote")
	}
}
