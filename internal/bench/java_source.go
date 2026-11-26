package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/record"
)

// benchCalls is the recorder's own call planner: the workload timed here is the workload recorded.
func benchCalls(types []string, values func(string) []any) [][]any {
	return record.PlanCalls(types)
}

// javaClass is the public class of a .java file: its own name by convention.
func javaClass(file string) string {
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// javaPlan reads the parameter types the recorder wrote, so bench times the same calls it recorded. When
// there is no plan beside the crate, benchmark nothing rather than guessing: the caller says so.
func javaPlan(work string) map[string][]string {
	raw, err := os.ReadFile(filepath.Join(work, "params.json"))
	if err != nil {
		return map[string][]string{}
	}
	var plan map[string]struct {
		Types []string `json:"types"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return map[string][]string{}
	}
	out := map[string][]string{}
	for name, entry := range plan {
		out[name] = entry.Types
	}
	return out
}

// literalValues and javaLit are the recorder's own, reused so a literal cannot be written two ways.
func literalValues(t string) []any { return record.LiteralValues(t) }
func javaLit(v any) string         { return record.JavaLiteral(v) }

// javaBenchSource writes Bench.java for one Java unit: the calls come from the same literal tables the
// recorder uses, so the workload is the recorded workload.
func javaBenchSource(className string, plan map[string][]string, values func(string) []any, literal func(any) string) string {
	var b strings.Builder
	b.WriteString(javaDriverHeader)
	first := true
	for name, types := range plan {
		calls := benchCalls(types, values)
		var body strings.Builder
		for _, args := range calls {
			parts := make([]string, len(args))
			for i, v := range args {
				parts[i] = literal(v)
			}
			// The answer is a primitive: calling it is the work, and adding it to sink keeps the JIT from
			// proving the call is dead.
			body.WriteString("\t\t\ttry { sink += (long) " + className + "." + name + "(" + strings.Join(parts, ", ") + "); } catch (Throwable ignored) {}\n")
		}
		if first {
			first = false
		} else {
			b.WriteString("\t\tout.append(\",\");\n")
		}
		b.WriteString("\t\t{\n")
		b.WriteString("\t\t\tlong start = System.nanoTime();\n")
		b.WriteString("\t\t\tint rounds = 0;\n")
		b.WriteString("\t\t\tlong elapsed = 0;\n")
		b.WriteString("\t\t\tdo {\n")
		b.WriteString("\t\t\t\tfor (int r = 0; r < repeat; r++) {\n")
		b.WriteString(body.String())
		b.WriteString("\t\t\t\t}\n")
		b.WriteString("\t\t\t\trounds++;\n")
		b.WriteString("\t\t\t\telapsed = System.nanoTime() - start;\n")
		b.WriteString("\t\t\t} while (elapsed / 1e6 < minMs && rounds < 10000);\n")
		// The name goes in as a Java string literal: %q is Go quoting, which produces \"...\" and does not
		// compile inside a Java source file.
		b.WriteString("\t\t\tout.append(\"{\\\"name\\\":\\\"" + name + "\\\",\\\"cases\\\":" + fmt.Sprint(len(calls)) +
			",\\\"ns\\\":\" + (elapsed / (double) (rounds * repeat * " + fmt.Sprint(len(calls)) + ")) + \"}\");\n")
		b.WriteString("\t\t}\n")
	}
	b.WriteString("\t\tout.append(\"]\");\n")
	b.WriteString("\t\tSystem.out.println(out);\n")
	b.WriteString("\t\tSystem.out.print(\"\");\n")
	b.WriteString("\t\tSystem.err.println(\"sink=\" + sink);\n")
	b.WriteString("\t}\n}\n")
	return b.String()
}
