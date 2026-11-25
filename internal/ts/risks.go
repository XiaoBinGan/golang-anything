package ts

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// rule is one risk: what it is called, how heavy it is, and how to see it in a syntax tree.
type rule struct {
	Kind   string
	Weight int
	// Calls match a call whose callee text is one of these (`foo(`, `a.slice(`).
	Calls []string
	// Literals match a string literal containing one of these.
	Literals []string
	// Nodes match a node type outright.
	Nodes []string
}

// rules covers the JS/TS families the porting guide warns about. They are patterns in the source, not
// proofs: `analyze` reports them so a person can look, and `record` decides what actually breaks.
var rules = []rule{
	{
		Kind:   "String offsets and slicing",
		Weight: 6,
		Calls:  []string{"slice", "substring", "substr", "indexOf", "lastIndexOf", "charCodeAt", "codePointAt", "matchAll", "split", "padStart", "padEnd"},
	},
	{
		Kind:     "Regex features the `regexp` package lacks",
		Weight:   6,
		Literals: []string{"(?<=", "(?<!", "(?=", "(?!", `\1`, `\2`, "(?"},
	},
	{
		Kind:   "Regexes built at runtime",
		Weight: 6,
		Nodes:  []string{"regex", "new_expression"},
		Calls:  []string{"RegExp"},
	},
	{
		Kind:   "Dynamic code (eval, new Function, computed import)",
		Weight: 6,
		Calls:  []string{"eval", "Function"},
	},
	{
		Kind:   "Insertion-ordered maps and object keys",
		Weight: 3,
		Calls:  []string{"Map", "Set", "keys", "entries", "values", "sort"},
	},
	{
		Kind:   "Number semantics",
		Weight: 1,
		Calls:  []string{"parseFloat", "parseInt", "toFixed"},
		Nodes:  []string{"augmented_assignment_expression"},
	},
	{
		Kind:   "Async and promises",
		Weight: 3,
		Calls:  []string{"then", "await_expression", "Promise", "all", "race"},
		Nodes:  []string{"await_expression"},
	},
	{
		Kind:   "Exceptions",
		Weight: 1,
		Nodes:  []string{"throw_statement", "try_statement"},
	},
	{
		Kind:   "Class inheritance",
		Weight: 3,
		Nodes:  []string{"class_heritage", "extends_clause"},
	},
	{
		Kind:   "Reflection and prototypes",
		Weight: 3,
		Calls:  []string{"getPrototypeOf", "defineProperty", "hasOwnProperty", "toString"},
		Nodes:  []string{"new_expression"},
	},
	{
		Kind:   "JSON round-trips",
		Weight: 1,
		Calls:  []string{"stringify", "parse"},
	},
	{
		Kind:   "Clock and random numbers",
		Weight: 1,
		Nodes:  []string{"new_expression"},
		Calls:  []string{"now", "random", "Date", "getTime", "hrtime"},
	},
}

// risks walks the tree once and collects every match with its line.
func risks(root *sitter.Node, src []byte, text func(*sitter.Node) string) []Risk {
	var out []Risk
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		line := int(n.StartPoint().Row) + 1
		switch n.Type() {
		case "call_expression", "new_expression":
			callee := ""
			if fn := n.ChildByFieldName("function"); fn != nil {
				callee = text(fn)
			} else if c := n.ChildByFieldName("constructor"); c != nil {
				callee = text(c)
			} else if n.NamedChildCount() > 0 {
				callee = text(n.NamedChild(0))
			}
			short := callee
			if i := strings.LastIndex(short, "."); i >= 0 {
				short = short[i+1:]
			}
			for _, r := range rules {
				for _, want := range r.Calls {
					if short == want || callee == want {
						out = append(out, Risk{Kind: r.Kind, Line: line, Code: firstLine(callee)})
						break
					}
				}
			}
		case "string", "template_string":
			body := text(n)
			for _, r := range rules {
				for _, want := range r.Literals {
					if strings.Contains(body, want) {
						out = append(out, Risk{Kind: r.Kind, Line: line, Code: firstLine(body)})
						break
					}
				}
			}
		case "throw_statement", "try_statement", "await_expression", "class_heritage",
			"extends_clause", "augmented_assignment_expression", "regex":
			for _, r := range rules {
				for _, want := range r.Nodes {
					if n.Type() == want {
						out = append(out, Risk{Kind: r.Kind, Line: line, Code: n.Type()})
						break
					}
				}
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	return out
}

// firstLine keeps a risk's evidence short: a whole call can span lines.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}
