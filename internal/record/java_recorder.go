package record

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// javaRecorder writes a Recorder.java that calls every planned function with literal arguments and prints
// one line per call. A JVM has no JSON in its standard library and writing a parser into the harness would
// be a second thing to get wrong; the literals come from values.go, which reads the same pool.
//
// The harness answers the same shape the other recorders do: {"fn":…,"ok":…} or {"fn":…,"error":…}.
func javaRecorder(plan map[string]planEntry, className string) string {
	var b strings.Builder
	b.WriteString("import java.util.*;\n\n")
	b.WriteString("public final class Recorder {\n")
	b.WriteString("\tstatic void emit(String fn, String argsJson, Object value) {\n")
	b.WriteString("\t\tSystem.out.println(\"{\\\"fn\\\":\\\"\" + fn + \"\\\",\\\"args\\\":\" + argsJson + \",\\\"ok\\\":\" + json(value) + \"}\");\n")
	b.WriteString("\t}\n")
	b.WriteString("\tstatic void fail(String fn, String argsJson, Throwable t) {\n")
	b.WriteString("\t\tString msg = t.getClass().getSimpleName() + \": \" + String.valueOf(t.getMessage());\n")
	b.WriteString("\t\tmsg = msg.replace(\"\\\\\", \"\\\\\\\\\").replace(\"\\\"\", \"\\\\\\\"\").replace(\"\\n\", \"\\\\n\");\n")
	b.WriteString("\t\tSystem.out.println(\"{\\\"fn\\\":\\\"\" + fn + \"\\\",\\\"args\\\":\" + argsJson + \",\\\"error\\\":\\\"\" + msg + \"\\\"}\");\n")
	b.WriteString("\t}\n")
	b.WriteString("\tstatic String json(Object v) {\n")
	b.WriteString("\t\tif (v == null) return \"null\";\n")
	b.WriteString("\t\tif (v instanceof String) return quote((String) v);\n")
	b.WriteString("\t\tif (v instanceof Character) return quote(String.valueOf(v));\n")
	b.WriteString("\t\tif (v instanceof Double) {\n")
	b.WriteString("\t\t\tdouble d = (Double) v;\n")
	b.WriteString("\t\t\tif (Double.isNaN(d)) return \"{\\\"$number\\\":\\\"NaN\\\"}\";\n")
	b.WriteString("\t\t\tif (Double.isInfinite(d)) return d > 0 ? \"{\\\"$number\\\":\\\"Infinity\\\"}\" : \"{\\\"$number\\\":\\\"-Infinity\\\"}\";\n")
	b.WriteString("\t\t\tif (d == 0 && 1 / d < 0) return \"{\\\"$number\\\":\\\"-0\\\"}\";\n")
	b.WriteString("\t\t\tif (d == Math.rint(d) && Math.abs(d) < 1e15) return Long.toString((long) d);\n")
	b.WriteString("\t\t\treturn Double.toString(d);\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\tif (v instanceof Float) return json(((Float) v).doubleValue());\n")
	b.WriteString("\t\tif (v instanceof Number || v instanceof Boolean) return String.valueOf(v);\n")
	b.WriteString("\t\treturn quote(String.valueOf(v));\n")
	b.WriteString("\t}\n")
	b.WriteString("\tstatic String quote(String s) {\n")
	b.WriteString("\t\tStringBuilder o = new StringBuilder(\"\\\"\");\n")
	b.WriteString("\t\tfor (char c : s.toCharArray()) {\n")
	b.WriteString("\t\t\tif (c == '\"') o.append(\"\\\\\\\"\");\n")
	b.WriteString("\t\t\telse if (c == '\\\\') o.append(\"\\\\\\\\\");\n")
	b.WriteString("\t\t\telse if (c == '\\n') o.append(\"\\\\n\");\n")
	b.WriteString("\t\t\telse if (c == '\\r') o.append(\"\\\\r\");\n")
	b.WriteString("\t\t\telse if (c == '\\t') o.append(\"\\\\t\");\n")
	b.WriteString("\t\t\telse if (c < 0x20) o.append(String.format(\"\\\\u%04x\", (int) c));\n")
	b.WriteString("\t\t\telse o.append(c);\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t\treturn o.append('\"').toString();\n")
	b.WriteString("\t}\n")
	b.WriteString("\tpublic static void main(String[] args) {\n")

	for name, entry := range plan {
		types := entry.Types
		calls := planCalls(types)
		for _, args := range calls {
			call := make([]string, len(args))
			for i, v := range args {
				if types[i] == "char" {
					call[i] = JavaLiteral(v)
					continue
				}
				call[i] = JavaLiteral(v)
			}
			invocation := fmt.Sprintf("%s.%s(%s)", className, name, strings.Join(call, ", "))
			// The args go out with the answer: a case is a call, and gate matches on the arguments.
			argsJSON := ArgsJSONLiteral(args, types)
			b.WriteString("\t\ttry { emit(" + fmt.Sprintf("%q", name) + ", " + argsJSON + ", " + invocation + "); }\n")
			b.WriteString("\t\tcatch (Throwable t) { fail(" + fmt.Sprintf("%q", name) + ", " + argsJSON + ", t); }\n")
		}
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// planCalls is the same paired walk and bounded cross product the other recorders do, over dialect names.
// PlanCalls is planCalls for the bench package: the same generated workload.
func PlanCalls(types []string) [][]any { return planCalls(types) }

func planCalls(types []string) [][]any {
	if len(types) == 0 {
		return [][]any{{}}
	}
	tables := make([][]any, len(types))
	longest := 0
	for i, t := range types {
		tables[i] = LiteralValues(t)
		if len(tables[i]) > longest {
			longest = len(tables[i])
		}
	}
	var calls [][]any
	for i := 0; i < longest; i++ {
		args := make([]any, len(tables))
		for j := range tables {
			args[j] = tables[j][i%len(tables[j])]
		}
		calls = append(calls, args)
	}
	const head = 6
	truncated := make([][]any, len(tables))
	for i := range tables {
		if len(tables[i]) > head {
			truncated[i] = tables[i][:head]
		} else {
			truncated[i] = tables[i]
		}
	}
	if len(truncated) <= 3 {
		stack := [][]any{{}}
		for _, column := range truncated {
			var next [][]any
			for _, prefix := range stack {
				for _, v := range column {
					row := append(append([]any{}, prefix...), v)
					next = append(next, row)
				}
			}
			stack = next
		}
		calls = append(calls, stack...)
	} else {
		for j := range truncated {
			for _, v := range tables[j] {
				args := make([]any, len(tables))
				for k := range tables {
					args[k] = tables[k][0]
				}
				args[j] = v
				calls = append(calls, args)
			}
		}
	}
	return calls
}

// argsJSONLiteral writes the arguments as a Java string holding the JSON the case file needs.
func ArgsJSONLiteral(args []any, types []string) string {
	parts := make([]string, len(args))
	for i, v := range args {
		parts[i] = wireJSON(v, types[i])
	}
	return javaString("[" + strings.Join(parts, ",") + "]")
}

// wireJSON is the value as the wire spells it: the same markers the other recorders write.
func wireJSON(v any, dialect string) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		switch {
		case math.IsNaN(x):
			return `{"$number":"NaN"}`
		case math.IsInf(x, 1):
			return `{"$number":"Infinity"}`
		case math.IsInf(x, -1):
			return `{"$number":"-Infinity"}`
		case x == 0 && math.Signbit(x):
			return `{"$number":"-0"}`
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case string:
		if dialect == "char" {
			return stringJSON(string([]rune(x)[:1]))
		}
		return stringJSON(x)
	}
	return "null"
}

// stringJSON quotes a string for the wire, escaping what would end it.
func stringJSON(s string) string {
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
			if r < 0x20 || r > 0x7e {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
