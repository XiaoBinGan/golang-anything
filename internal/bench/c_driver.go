package bench

import (
	"fmt"
	"strings"
)

// cBenchSource writes the timing harness for a C or C++ unit: the same literal calls as the recorder,
// repeated until the budget is spent. It prints the same rows the other harnesses print.
func cBenchSource(plan map[string][]string, cpp bool) string {
	var b strings.Builder
	if cpp {
		b.WriteString("#include <cstdio>\n#include <cstdlib>\n#include <cmath>\n#include <ctime>\n\n")
	} else {
		b.WriteString("#include <stdio.h>\n#include <stdlib.h>\n#include <math.h>\n#include <time.h>\n\n")
	}
	b.WriteString("static long long sink = 0;\n")
	b.WriteString("static long long now_ns(void) {\n")
	if cpp {
		b.WriteString("\ttimespec ts;\n\tclock_gettime(CLOCK_MONOTONIC, &ts);\n")
	} else {
		b.WriteString("\tstruct timespec ts;\n\tclock_gettime(CLOCK_MONOTONIC, &ts);\n")
	}
	b.WriteString("\treturn (long long) ts.tv_sec * 1000000000LL + ts.tv_nsec;\n}\n\n")
	b.WriteString("int main(int argc, char **argv) {\n")
	b.WriteString("\tdouble min_ms = atof(argv[1]);\n")
	b.WriteString("\tint repeat = atoi(argv[2]);\n")
	b.WriteString("\tprintf(\"[\");\n")
	first := true
	for name, types := range plan {
		calls := benchCalls(types, literalValues)
		if !first {
			b.WriteString("\tprintf(\",\");\n")
		}
		first = false
		b.WriteString("\t{\n")
		b.WriteString("\t\tlong long start = now_ns();\n")
		b.WriteString("\t\tint rounds = 0; long long elapsed = 0;\n")
		b.WriteString("\t\tdo {\n")
		b.WriteString("\t\t\tfor (int r = 0; r < repeat; r++) {\n")
		for _, args := range calls {
			lits := make([]string, len(args))
			for i, v := range args {
				lits[i] = cLiteralFor(v, types[i])
			}
			if cpp {
				// The unit under test throws on some recorded inputs; a timing loop that lets the exception
				// escape kills the process, which is what the first C++ bench run did.
				b.WriteString("\t\t\t\ttry { sink += (long long) " + name + "(" + strings.Join(lits, ", ") + "); }\n")
				b.WriteString("\t\t\t\tcatch (...) { sink += 1; }\n")
			} else {
				b.WriteString("\t\t\t\tsink += (long long) " + name + "(" + strings.Join(lits, ", ") + ");\n")
			}
		}
		b.WriteString("\t\t\t}\n")
		b.WriteString("\t\t\trounds++;\n")
		b.WriteString("\t\t\telapsed = now_ns() - start;\n")
		b.WriteString("\t\t} while (elapsed / 1e6 < min_ms && rounds < 10000);\n")
		b.WriteString("\t\tprintf(\"{\\\"name\\\":\\\"" + name + "\\\",\\\"cases\\\":" + fmt.Sprint(len(calls)) +
			",\\\"ns\\\":%.6f}\", (double) elapsed / (rounds * repeat * " + fmt.Sprint(len(calls)) + "));\n")
		b.WriteString("\t}\n")
	}
	b.WriteString("\tprintf(\"]\\n\");\n")
	b.WriteString("\tfprintf(stderr, \"sink=%lld\\n\", sink);\n")
	b.WriteString("\treturn 0;\n}\n")
	return b.String()
}
