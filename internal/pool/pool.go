// Package pool is the one list of values both generators draw from.
//
// record writes a fixed table and fuzz draws from an alphabet, and the two drifted three times: the
// recorder learned that apostrophes matter and fuzz did not; the recorder learned that "/" and "~" are
// what a JSON-pointer escaper is about and fuzz did not; each time a port passed one generator and
// failed the other. One list, read by both, is the fix that does not need a fourth repair.
package pool

import "strings"

// Structural are the characters that change what a function does. A generator that cannot produce them
// cannot test the function that exists to handle them.
const Structural = "'\"\\/`|;&<>()[]{}*?~#%!.,:=@+$"

// Curated are strings a person would write, next to the structural ones. The order matters only for
// replay: a seed fixes the draw, and the table is emitted in this order.
var Curated = []string{
	"", "a", "abc", "  ", "a b  c", "x y z", "pad  ", "héllo", "naïve café", "😀😀",
	"$5 apple", "$a$b", "$", "a$", "  \t  ", "ünïcodé", "ÆØÅ", "中文 字", "a\nb", "0",
	// Values built from the structural characters, in shapes a function meets: one at each end, in the
	// middle, doubled, and next to something that is not structural.
	"it's", "''", "a'b'c", "\\", "a\\b", "\"", "a\"b", "cmd --flag | tee", "$(id)", "a|b", "a;b",
	"--flag", "/a/b", "~0~1", "~", "/", "a/b", "~/x", "a~b", "a/b/c", "1/2", "a.b", "a:b", "a=b",
	"a&b", "a?b", "a#b", "a%b", "a+b", "a@b", "a,b", "a*b", "a!b", "[]", "{}", "()",
}

// Values is the recorder's table: the curated strings, then one string per structural character so no
// character is only reachable inside a longer example.
func Values() []string {
	out := append([]string{}, Curated...)
	for _, r := range Structural {
		out = append(out, string(r))
	}
	return out
}

// Alphabet is the pool fuzz draws characters from: letters, whitespace, the awkward spaces and
// scripts, and every structural character.
func Alphabet() []rune {
	return []rune("ab  $\t\n.-_0é😀中\u00a0\u200bß" + Structural)
}

// JSValues renders the table as a JavaScript array literal, for the recorder's generated script.
func JSValues() string {
	quoted := make([]string, len(Values()))
	for i, v := range Values() {
		quoted[i] = jsString(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// jsString quotes one value as a JavaScript string literal, escaping the characters that would end it.
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\u00a0':
			b.WriteString(`\u00a0`)
		case '\u200b':
			b.WriteString(`\u200b`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}
