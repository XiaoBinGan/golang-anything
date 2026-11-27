package record

import (
	"fmt"
	"strings"
)

// kotlinRecorder writes a Recorder.kt for a Kotlin unit: the literal arguments, one call per line, the same
// shape the Java and C harnesses print. Kotlin is compiled, so a value of the wrong type is a compile error
// rather than a case that proves nothing.
func kotlinRecorder(plan map[string]planEntry, className string) string {
	var b strings.Builder
	b.WriteString("import kotlin.math.*\n\n")
	b.WriteString("fun emit(fn: String, args: String, value: Any?) {\n")
	b.WriteString("    println(\"{\\\"fn\\\":\\\"\" + fn + \"\\\",\\\"args\\\":\" + args + \",\\\"ok\\\":\" + json(value) + \"}\")\n")
	b.WriteString("}\n\n")
	b.WriteString("fun fail(fn: String, args: String, t: Throwable) {\n")
	b.WriteString("    val msg = (t::class.simpleName ?: \"Error\") + \": \" + (t.message ?: \"\")\n")
	b.WriteString("    val escaped = msg.replace(\"\\\\\\\\\", \"\\\\\\\\\\\\\\\\\").replace(\"\\\"\", \"\\\\\\\"\").replace(\"\\n\", \"\\\\n\")\n")
	b.WriteString("    println(\"{\\\"fn\\\":\\\"\" + fn + \"\\\",\\\"args\\\":\" + args + \",\\\"error\\\":\\\"\" + escaped + \"\\\"}\")\n")
	b.WriteString("}\n\n")
	b.WriteString(`fun json(v: Any?): String = when (v) {
    null -> "null"
    is String -> quote(v)
    is Char -> quote(v.toString())
    is Double -> when {
        v.isNaN() -> "{\"\$number\":\"NaN\"}"
        v.isInfinite() -> "{\"\$number\":\"" + (if (v > 0) "Infinity" else "-Infinity") + "\"}"
        v == 0.0 && 1.0 / v < 0 -> "{\"\$number\":\"-0\"}"
        v == floor(v) && abs(v) < 1e15 -> v.toLong().toString()
        else -> v.toString()
    }
    is Float -> json(v.toDouble())
    is Number, is Boolean -> v.toString()
    else -> quote(v.toString())
}

fun quote(s: String): String {
    val b = StringBuilder("\"")
    for (c in s) {
        when {
            c == '"' -> b.append("\\\"")
            c == '\\' -> b.append("\\\\")
            c == '\n' -> b.append("\\n")
            c == '\r' -> b.append("\\r")
            c == '\t' -> b.append("\\t")
            c < ' ' -> b.append("\\u%04x".format(c.code))
            else -> b.append(c)
        }
    }
    return b.append('"').toString()
}

fun main() {
`)
	for name, entry := range plan {
		calls := planCalls(entry.Types)
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = KotlinLiteral(v, entry.Types[i])
			}
			// A promoted unit is a proxy, so the callable is `TimerPromoted.doubled` — the plan carries that path. The
			// bare name is what a free function is called by.
			callee := name
			if entry.Path != "" {
				callee = entry.Path
			}
			invocation := fmt.Sprintf("%s(%s)", callee, strings.Join(lits, ", "))
			argsJSON := kotlinString(wireArgsJSON(args, entry.Types))
			b.WriteString("    try { emit(" + fmt.Sprintf("%q", name) + ", " + argsJSON + ", " + invocation + ") }\n")
			b.WriteString("    catch (t: Throwable) { fail(" + fmt.Sprintf("%q", name) + ", " + argsJSON + ", t) }\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// KotlinLiteral writes one Go value as a Kotlin expression.
func KotlinLiteral(v any, dialect string) string {
	switch x := v.(type) {
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		switch {
		case x != x:
			return "Double.NaN"
		case x > 1e308:
			return "Double.POSITIVE_INFINITY"
		case x < -1e308:
			return "Double.NEGATIVE_INFINITY"
		}
		return fmt.Sprintf("%v", x)
	case bool:
		return fmt.Sprintf("%v", x)
	case string:
		if dialect == "char" {
			return "'" + strings.ReplaceAll(strings.ReplaceAll(x, `\`, `\\`), "'", `\'`) + "'"
		}
		return kotlinString(x)
	}
	return "null"
}

// wireArgsJSON is the case's arguments as the wire spells them, unquoted.
func wireArgsJSON(args []any, types []string) string {
	parts := make([]string, len(args))
	for i, v := range args {
		// Raw characters, not \uXXXX: Kotlin has no \u escape in string literals, and a source file is
		// UTF-8 anyway. Writing the escape produced cases whose arguments were the six characters u d 8 3 d
		// — which the recording then showed as a mismatch in a port that was right.
		parts[i] = wireJSONUTF8(v, types[i])
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// kotlinString writes a Kotlin string literal: $ starts a template there, so it is escaped where Java would
// not need to.
func kotlinString(s string) string {
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
		case '$':
			b.WriteString(`\$`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// wireJSONUTF8 is wireJSON with real characters: only what JSON itself needs escaping is escaped.
func wireJSONUTF8(v any, dialect string) string {
	s := wireJSON(v, dialect)
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	// Rebuild the string from the value: wireJSON escaped every non-ASCII rune, and the escapes are what
	// we are undoing.
	if str, ok := v.(string); ok {
		if dialect == "char" {
			str = string([]rune(str)[:1])
		}
		return rawJSONString(str)
	}
	return s
}

// rawJSONString quotes a string for JSON, leaving every character that JSON allows as itself.
func rawJSONString(s string) string {
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
