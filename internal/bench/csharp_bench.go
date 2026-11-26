package bench

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/record"
)

// csharpBenchSource writes a Bench.cs for a C# unit: literal calls in a timing loop, printing rows.
func csharpBenchSource(plan map[string][]string, className string) string {
	var b strings.Builder
	b.WriteString("using System;\nusing System.Diagnostics;\nusing System.Text;\n\n")
	b.WriteString("public static class Bench\n{\n")
	b.WriteString("\tstatic long sink;\n")
	b.WriteString("\tpublic static void Main(string[] args)\n\t{\n")
	b.WriteString("\t\tdouble minMs = double.Parse(args[0]);\n")
	b.WriteString("\t\tint repeat = int.Parse(args[1]);\n")
	b.WriteString("\t\tvar outSb = new StringBuilder(\"[\");\n")
	b.WriteString("\t\tbool first = true;\n")
	for name, types := range plan {
		calls := record.PlanCalls(types)
		b.WriteString("\t\tif (!first) outSb.Append(\",\");\n\t\tfirst = false;\n")
		b.WriteString("\t\t{\n")
		b.WriteString("\t\t\tvar sw = Stopwatch.StartNew();\n")
		b.WriteString("\t\t\tint rounds = 0; long elapsed = 0;\n")
		b.WriteString("\t\t\tdo {\n")
		b.WriteString("\t\t\t\tfor (int r = 0; r < repeat; r++)\n\t\t\t\t{\n")
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = record.CsLiteral(v, types[i])
			}
			b.WriteString("\t\t\t\t\ttry { sink += (long) " + className + "." + name + "(" + strings.Join(lits, ", ") + ").ToString().Length; } catch { }\n")
		}
		b.WriteString("\t\t\t\t}\n")
		b.WriteString("\t\t\t\trounds++;\n")
		b.WriteString("\t\t\t\telapsed = sw.ElapsedTicks * (1000000000L / Stopwatch.Frequency);\n")
		b.WriteString("\t\t\t} while (elapsed / 1e6 < minMs && rounds < 10000);\n")
		b.WriteString("\t\t\toutSb.Append(\"{\\\"name\\\":\\\"" + name + "\\\",\\\"cases\\\":" + fmt.Sprint(len(calls)) +
			",\\\"ns\\\":\" + (elapsed / (double)(rounds * repeat * " + fmt.Sprint(len(calls)) + ")) + \"}\");\n")
		b.WriteString("\t\t}\n")
	}
	b.WriteString("\t\toutSb.Append(\"]\");\n\t\tConsole.WriteLine(outSb.ToString());\n")
	b.WriteString("\t\tConsole.Error.WriteLine(\"sink=\" + sink);\n")
	b.WriteString("\t}\n}\n")
	return b.String()
}

// dotnetRootForBench finds the SDK root the app host needs, from the dotnet on PATH.
func dotnetRootForBench() string {
	out, err := exec.Command("dotnet", "--info").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "Base Path:"); i >= 0 {
			base := strings.TrimSpace(line[i+len("Base Path:"):])
			return filepath.Dir(filepath.Dir(base))
		}
	}
	return ""
}
