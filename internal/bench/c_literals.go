package bench

import (
	"github.com/xiaobingan/go-anything/internal/record"
)

// cLiteralFor and cRecorderSource are the recorder's own, so the workload timed is the workload recorded.
func cLiteralFor(v any, dialect string) string { return record.CLiteral(v, dialect) }

// CRSource is exported for tests and for the bench package's own use.
func CRSource(plan map[string][]string, cpp bool) string { return cBenchSource(plan, cpp) }
