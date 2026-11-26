package bench

import (
	"fmt"
	"strings"

	"github.com/xiaobingan/go-anything/internal/record"
)

// kotlinBenchSource writes Bench.kt for a Kotlin unit: the same literal calls the recorder makes, repeated
// until the budget is spent, printing the rows the other harnesses print.
func kotlinBenchSource(plan map[string][]string) string {
	var b strings.Builder
	b.WriteString("import kotlin.math.*\n\n")
	b.WriteString("var sink: Long = 0\n\n")
	b.WriteString("fun main(args: Array<String>) {\n")
	b.WriteString("    val minMs = args[0].toDouble()\n")
	b.WriteString("    val repeat = args[1].toInt()\n")
	b.WriteString("    val out = StringBuilder(\"[\")\n")
	b.WriteString("    var first = true\n")
	first := true
	for name, types := range plan {
		calls := record.PlanCalls(types)
		if !first {
			b.WriteString("    out.append(\",\")\n")
		}
		first = false
		b.WriteString("    run {\n")
		b.WriteString("        val start = System.nanoTime()\n")
		b.WriteString("        var rounds = 0\n")
		b.WriteString("        var elapsed = 0L\n")
		b.WriteString("        do {\n")
		b.WriteString("            for (r in 0 until repeat) {\n")
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = record.KotlinLiteral(v, types[i])
			}
			// The return type decides how to keep the call from being optimised away: a String has length, a
			// number does not.
			invocation := name + "(" + strings.Join(lits, ", ") + ")"
			if types != nil && len(types) > 0 {
				invocation = name + "(" + strings.Join(lits, ", ") + ")"
			}
			b.WriteString("                try { sink += " + invocation + ".toString().length.toLong() } catch (t: Throwable) {}\n")
		}
		b.WriteString("            }\n")
		b.WriteString("            rounds++\n")
		b.WriteString("            elapsed = System.nanoTime() - start\n")
		b.WriteString("        } while (elapsed / 1e6 < minMs && rounds < 10000)\n")
		b.WriteString("        out.append(\"{\\\"name\\\":\\\"" + name + "\\\",\\\"cases\\\":" + fmt.Sprint(len(calls)) +
			",\\\"ns\\\":\" + (elapsed / (rounds * repeat * " + fmt.Sprint(len(calls)) + ").toDouble()) + \"}\")\n")
		b.WriteString("    }\n")
	}
	b.WriteString("    out.append(\"]\")\n")
	b.WriteString("    println(out)\n")
	b.WriteString("    System.err.println(\"sink=\" + sink)\n")
	b.WriteString("}\n")
	return b.String()
}
