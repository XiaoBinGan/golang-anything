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
		// A numeric literal is a compile-time constant, and clang folds the whole call away: `twice(7L)`
		// became `sink += 14`, the original measured 0.00 ns/call, and the report said the Go port was 32x
		// slower than a function that never ran. Each numeric argument is read through a volatile of its own
		// type instead, so the call is really made. `__typeof__` keeps this working for any numeric type
		// without a second table of types to drift from the first.
		var decls []string
		exprs := make([][]string, len(calls))
		for k, args := range calls {
			exprs[k] = make([]string, len(args))
			for i := range args {
				lit := cLiteralFor(args[i], types[i])
				slot := fmt.Sprintf("arg%d_%d", k, i)
				if decl, expr, ok := cVolatileArg(lit, slot); ok {
					decls = append(decls, decl)
					exprs[k][i] = expr
					continue
				}
				exprs[k][i] = lit
			}
		}
		b.WriteString("\t{\n")
		for _, d := range decls {
			b.WriteString("\t\t" + d + "\n")
		}
		b.WriteString("\t\tlong long start = now_ns();\n")
		b.WriteString("\t\tint rounds = 0; long long elapsed = 0;\n")
		b.WriteString("\t\tdo {\n")
		b.WriteString("\t\t\tfor (int r = 0; r < repeat; r++) {\n")
		for k := range calls {
			lits := exprs[k]
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

// cVolatileArg turns a numeric literal into a declaration plus an expression that reads it through a
// volatile. A string literal is left alone: its type is an array, which `__typeof__` describes but a
// pointer cast cannot read through, and passing a pointer is already opaque enough that clang keeps the
// call.
func cVolatileArg(lit, slot string) (decl, expr string, ok bool) {
	if lit == "" {
		return "", lit, false
	}
	c := lit[0]
	numeric := (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.'
	if !numeric {
		return "", lit, false
	}
	decl = "static __typeof__(" + lit + ") " + slot + " = " + lit + ";"
	expr = "*(volatile __typeof__(" + slot + " ) *)&" + slot
	return decl, expr, true
}
