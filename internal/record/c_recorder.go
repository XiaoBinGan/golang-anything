package record

import (
	"fmt"
	"strings"
)

// cRecorder writes a C or C++ program that calls every planned function with literal arguments and prints
// one line per call, the same shape the Java recorder emits. A C compiler cannot be handed JSON either, so
// the calls are source.
//
// A C++ unit is compiled as C++ so `throw` and std::string work; a C unit is compiled as C.
func cRecorder(plan map[string]planEntry, cpp bool) string {
	var b strings.Builder
	if cpp {
		b.WriteString("#include <string>\n#include <cstdio>\n#include <cmath>\n#include <stdexcept>\n\n")
		b.WriteString("static void quote(const char *s) {\n")
		b.WriteString("\tputchar('\"');\n")
		b.WriteString("\tfor (const char *p = s; *p; p++) {\n")
		b.WriteString("\t\tunsigned char c = (unsigned char) *p;\n")
		b.WriteString("\t\tif (c == '\"') fputs(\"\\\\\\\"\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\\\') fputs(\"\\\\\\\\\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\n') fputs(\"\\\\n\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\r') fputs(\"\\\\r\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\t') fputs(\"\\\\t\", stdout);\n")
		b.WriteString("\t\telse if (c < 0x20) printf(\"\\\\u%04x\", c);\n")
		b.WriteString("\t\telse putchar(c);\n")
		b.WriteString("\t}\n\tputchar('\"');\n}\n\n")
	} else {
		b.WriteString("#include <stdio.h>\n#include <string.h>\n#include <math.h>\n\n")
		b.WriteString("static void quote(const char *s) {\n")
		b.WriteString("\tputchar('\"');\n")
		b.WriteString("\tfor (const char *p = s; *p; p++) {\n")
		b.WriteString("\t\tunsigned char c = (unsigned char) *p;\n")
		b.WriteString("\t\tif (c == '\"') fputs(\"\\\\\\\"\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\\\') fputs(\"\\\\\\\\\", stdout);\n")
		b.WriteString("\t\telse if (c == '\\n') fputs(\"\\\\n\", stdout);\n")
		b.WriteString("\t\telse if (c < 0x20) printf(\"\\\\u%04x\", c);\n")
		b.WriteString("\t\telse putchar(c);\n")
		b.WriteString("\t}\n\tputchar('\"');\n}\n\n")
	}

	// Emitters, one per return kind: the caller knows what the function returns from the plan's type.
	b.WriteString("static void emit_int(const char *fn, const char *args, long v) {\n")
	b.WriteString("\tprintf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":%ld}\\n\", fn, args, v);\n}\n")
	b.WriteString("static void emit_float(const char *fn, const char *args, double v) {\n")
	b.WriteString("\tif (isnan(v)) printf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":{\\\"$number\\\":\\\"NaN\\\"}}\\n\", fn, args);\n")
	b.WriteString("\telse if (isinf(v)) printf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":{\\\"$number\\\":\\\"%s\\\"}}\\n\", fn, args, v > 0 ? \"Infinity\" : \"-Infinity\");\n")
	b.WriteString("\telse if (v == 0 && signbit(v)) printf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":{\\\"$number\\\":\\\"-0\\\"}}\\n\", fn, args);\n")
	b.WriteString("\telse if (v == (long long) v && fabs(v) < 1e15) printf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":%lld}\\n\", fn, args, (long long) v);\n")
	b.WriteString("\telse printf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":%.17g}\\n\", fn, args, v);\n}\n")
	b.WriteString("static void emit_str(const char *fn, const char *args, const char *v) {\n")
	b.WriteString("\tprintf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":\", fn, args);\n\tquote(v ? v : \"\");\n\tprintf(\"}\\n\");\n}\n")
	b.WriteString("static void emit_bool(const char *fn, const char *args, int v) {\n")
	b.WriteString("\tprintf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"ok\\\":%s}\\n\", fn, args, v ? \"true\" : \"false\");\n}\n")
	b.WriteString("static void emit_error(const char *fn, const char *args, const char *kind, const char *msg) {\n")
	b.WriteString("\tprintf(\"{\\\"fn\\\":\\\"%s\\\",\\\"args\\\":%s,\\\"error\\\":\\\"%s: %s\\\"}\\n\", fn, args, kind, msg ? msg : \"\");\n}\n\n")

	b.WriteString("int main(void) {\n")
	for name, entry := range plan {
		calls := planCalls(entry.Types)
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = CLiteral(v, entry.Types[i])
			}
			call := fmt.Sprintf("%s(%s)", name, strings.Join(lits, ", "))
			argsJSON := cArgsLiteral(args, entry.Types)
			if cpp {
				b.WriteString("\ttry { emit_" + emitterFor(entry.Returns) + "(" + cString(name) + ", " + argsJSON + ", " + call + "); }\n")
				b.WriteString("\tcatch (const std::exception &e) { emit_error(" + cString(name) + ", " + argsJSON +
					", \"Error\", e.what()); }\n")
				b.WriteString("\tcatch (const char *e) { emit_error(" + cString(name) + ", " + argsJSON + ", \"Error\", e); }\n")
				b.WriteString("\tcatch (...) { emit_error(" + cString(name) + ", " + argsJSON + ", \"Error\", \"unknown\"); }\n")
			} else {
				b.WriteString("\temit_" + emitterFor(entry.Returns) + "(" + cString(name) + ", " + argsJSON + ", " + call + ");\n")
			}
		}
	}
	b.WriteString("\treturn 0;\n}\n")
	return b.String()
}

// emitterFor picks the printer for a return type.
func emitterFor(dialect string) string {
	switch dialect {
	case "int":
		return "int"
	case "float":
		return "float"
	case "String":
		return "str"
	case "boolean":
		return "bool"
	}
	// An unknown return type is printed as a number: the recording shows what it was, and a port that
	// disagrees fails gate.
	return "int"
}

// CLiteral writes one Go value as a C or C++ expression.
func CLiteral(v any, dialect string) string {
	switch x := v.(type) {
	case int64:
		return fmt.Sprintf("%dL", x)
	case float64:
		switch {
		case x != x:
			return "NAN"
		case x > 1e308:
			return "INFINITY"
		case x < -1e308:
			return "-INFINITY"
		}
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "1"
		}
		return "0"
	case string:
		if dialect == "char" {
			return cChar(x)
		}
		return cString(x)
	}
	return "0"
}

// cChar writes a one-rune string as a C char literal.
func cChar(s string) string {
	r := []rune(s)[0]
	switch r {
	case '\'':
		return `'\''`
	case '\\':
		return `'\\'`
	case '\n':
		return `'\n'`
	}
	if r < 0x20 || r > 0x7e {
		return fmt.Sprintf("(char)%d", r)
	}
	return "'" + string(r) + "'"
}

// cString writes a C string literal, escaping what would end it.
func cString(s string) string {
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
		case '?':
			// Trigraph sequences start with ?, and a literal "??!" in a string is one.
			b.WriteString(`\?`)
		default:
			if r < 0x20 || r > 0x7e {
				// UTF-8 bytes, which is what the original's runtime passes around.
				for _, by := range []byte(string(r)) {
					fmt.Fprintf(&b, `\%03o`, by)
				}
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cArgsLiteral writes the arguments as a C string literal holding the JSON the case file needs.
func cArgsLiteral(args []any, types []string) string {
	parts := make([]string, len(args))
	for i, v := range args {
		parts[i] = wireJSON(v, types[i])
	}
	return cString("[" + strings.Join(parts, ",") + "]")
}
