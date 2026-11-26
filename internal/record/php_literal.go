package record

import (
	"fmt"
	"strings"
)

// phpLiteral writes one Go value as a PHP expression, for the generated recorder's fallback path: PHP can
// read JSON, so the driver above mostly parses it, but the cross-product values are printed here when a
// value has no JSON form.
func phpLiteral(v any) string {
	switch x := v.(type) {
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		return fmt.Sprintf("%v", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case string:
		var b strings.Builder
		b.WriteByte('\'')
		for _, r := range x {
			if r == '\'' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('\'')
		return b.String()
	}
	return "null"
}
