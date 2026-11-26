package record

import (
	"fmt"
	"strings"
)

// csharpRecorder writes a Program.cs that calls every planned function with literal arguments and prints one
// line per call. dotnet has System.Text.Json in its standard library, but the case's own arguments still
// have to be written out: the harness is generated source, so the compiler checks every literal.
func csharpRecorder(plan map[string]planEntry, className string) string {
	var b strings.Builder
	b.WriteString("using System;\nusing System.Globalization;\nusing System.Text.Json;\n\n")
	b.WriteString("public static class Recorder\n{\n")
	b.WriteString("\tstatic string Json(object? v) => v switch\n\t{\n")
	b.WriteString("\t\tnull => \"null\",\n")
	b.WriteString("\t\tstring s => JsonSerializer.Serialize(s),\n")
	b.WriteString("\t\tchar c => JsonSerializer.Serialize(c.ToString()),\n")
	b.WriteString("\t\tdouble d when double.IsNaN(d) => \"{\\\"$number\\\":\\\"NaN\\\"}\",\n")
	b.WriteString("\t\tdouble d when double.IsPositiveInfinity(d) => \"{\\\"$number\\\":\\\"Infinity\\\"}\",\n")
	b.WriteString("\t\tdouble d when double.IsNegativeInfinity(d) => \"{\\\"$number\\\":\\\"-Infinity\\\"}\",\n")
	b.WriteString("\t\t_ => JsonSerializer.Serialize(v),\n\t};\n\n")
	b.WriteString("\tpublic static void Main()\n\t{\n")
	for name, entry := range plan {
		calls := planCalls(entry.Types)
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = csLiteral(v, entry.Types[i])
			}
			argsJSON := csString(wireArgsJSON(args, entry.Types))
			invocation := fmt.Sprintf("%s.%s(%s)", className, name, strings.Join(lits, ", "))
			b.WriteString("\t\ttry { Console.WriteLine(\"{\\\"fn\\\":\\\"\" + " + csString(name) +
				" + \"\\\",\\\"args\\\":\" + " + argsJSON + " + \",\\\"ok\\\":\" + Json(" + invocation + ") + \"}\"); }\n")
			b.WriteString("\t\tcatch (Exception e) { Console.WriteLine(\"{\\\"fn\\\":\\\"\" + " + csString(name) +
				" + \"\\\",\\\"args\\\":\" + " + argsJSON + " + \",\\\"error\\\":\\\"\" + " +
				csString("") + " + e.GetType().Name + \": \" + Escape(e.Message) + \"\\\"}\"); }\n")
		}
	}
	b.WriteString("\t}\n")
	b.WriteString("\tstatic string Escape(string s) => s.Replace(\"\\\\\", \"\\\\\\\\\").Replace(\"\\\"\", \"\\\\\\\"\").Replace(\"\\n\", \"\\\\n\");\n")
	b.WriteString("}\n")
	return b.String()
}

// csString writes a C# string literal, verbatim where that is simpler.
func csString(s string) string {
	var b strings.Builder
	b.WriteString("@\"")
	for _, r := range s {
		if r == '"' {
			b.WriteString(`""`)
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// csLiteral writes one Go value as a C# expression.
func csLiteral(v any, dialect string) string {
	switch x := v.(type) {
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		switch {
		case x != x:
			return "double.NaN"
		case x > 1e308:
			return "double.PositiveInfinity"
		case x < -1e308:
			return "double.NegativeInfinity"
		}
		return fmt.Sprintf("%v", x)
	case bool:
		return fmt.Sprintf("%v", x)
	case string:
		if dialect == "char" {
			return "'" + strings.ReplaceAll(string([]rune(x)[:1]), "'", `\'`) + "'"
		}
		return csString(x)
	}
	return "null"
}
