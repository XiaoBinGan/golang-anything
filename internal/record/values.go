package record

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/pool"
)

// The value tables for the languages whose harness is generated as source: Java, C and C++ cannot be handed
// a JSON value and asked to decode it without a parser, so the calls are written out as literals. The
// JavaScript, Python and Ruby recorders carry their own tables because their runtimes can read JSON; these
// three read the Go one.
//
// The strings come from pool.Values(), which is the same list the recorder for TypeScript uses, so a value
// nobody thought of is still in one place.

// LiteralValues answers the values one dialect type may take, as Go values.
func LiteralValues(dialect string) []any {
	strings := pool.Values()
	var out []any
	switch dialect {
	case "int":
		for _, n := range []int64{0, 1, -1, 2, 7, -13, 100, 1 << 20, -(1 << 20), math.MaxInt32, math.MinInt32} {
			out = append(out, n)
		}
	case "float":
		for _, f := range []float64{0, 1, -1, 2, 0.5, -0.5, 1e6, -1e6, math.NaN(), math.Inf(1), math.Inf(-1),
			3.0, math.SmallestNonzeroFloat64} {
			out = append(out, f)
		}
	case "boolean":
		out = append(out, true, false)
	case "String":
		for _, s := range strings {
			out = append(out, s)
		}
	case "char":
		out = append(out, "a", "Z", "0", " ", "é")
	default:
		// "any": a little of everything, so a port cannot pass by assuming one kind
		out = append(out, int64(0), int64(1), int64(-1), true, false)
		if len(strings) > 8 {
			strings = strings[:8]
		}
		for _, s := range strings {
			out = append(out, s)
		}
	}
	return out
}

// JavaLiteral writes one Go value as a Java expression.
func JavaLiteral(v any) string {
	switch x := v.(type) {
	case int64:
		if x > math.MaxInt32 || x < math.MinInt32 {
			return fmt.Sprintf("(int)%dL", x)
		}
		return strconv.FormatInt(x, 10)
	case float64:
		switch {
		case math.IsNaN(x):
			return "Double.NaN"
		case math.IsInf(x, 1):
			return "Double.POSITIVE_INFINITY"
		case math.IsInf(x, -1):
			return "Double.NEGATIVE_INFINITY"
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case string:
		if len([]rune(x)) == 1 {
			return javaCharOrString(x)
		}
		return javaString(x)
	}
	return "null"
}

// javaCharOrString writes a one-rune value as a char literal, which is what a char parameter needs.
func javaCharOrString(s string) string {
	r := []rune(s)[0]
	if r == '\'' {
		return `'\\''`
	}
	if r == '\\' {
		return `'\\\\'`
	}
	if r < 0x20 {
		return fmt.Sprintf("(char)%d", r)
	}
	return "'" + string(r) + "'"
}

// javaString writes a Java string literal, escaping what would end it.
func javaString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
