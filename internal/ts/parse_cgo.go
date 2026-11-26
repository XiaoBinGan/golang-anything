//go:build cgo

// The parse: tree-sitter, and everything that walks its nodes. Without cgo this file is not compiled
// and Parse/ParseSource come from parser_stub.go instead.
package ts

import (
	"context"
	"errors"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

func language(l Lang) *sitter.Language {
	switch l {
	case TSX:
		return tsx.GetLanguage()
	case JavaScript:
		return javascript.GetLanguage()
	case Python:
		return python.GetLanguage()
	default:
		return typescript.GetLanguage()
	}
}

// Warm exists so a caller can say "I will parse" before it does: with the parser in this build there is
// nothing to start, so it does nothing.
func Warm() {}

// Parse reads one file.
func Parse(path string) (*File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lang, ok := LangOf(path)
	if !ok {
		return &File{Path: path}, nil
	}
	return ParseSource(src, lang, path)
}

// ParseSource parses bytes, so tests do not need files.
func ParseSource(src []byte, lang Lang, path string) (*File, error) {
	p := sitter.NewParser()
	p.SetLanguage(language(lang))
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	out := &File{Path: path, ParseOK: !tree.RootNode().HasError()}
	text := func(n *sitter.Node) string {
		if n == nil {
			return ""
		}
		return n.Content(src)
	}
	if string(src) != "" {
		out.Loc = 1 + strings.Count(strings.TrimRight(string(src), "\n"), "\n")
	}

	root := tree.RootNode()
	if lang == Python {
		out.Fns = append(out.Fns, pyExports(root, src, text)...)
		for i := 0; i < int(root.NamedChildCount()); i++ {
			n := root.NamedChild(i)
			if n.Type() == "import_from_statement" || n.Type() == "import_statement" {
				out.Imports = append(out.Imports, Import{
					Specifier: strings.TrimSpace(text(n)),
					Line:      int(n.StartPoint().Row) + 1,
				})
			}
		}
		return out, nil
	}
	var walk func(n *sitter.Node, inClass string)
	walk = func(n *sitter.Node, inClass string) {
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			name := text(n.ChildByFieldName("name"))
			exported := false
			if parent := n.Parent(); parent != nil && parent.Type() == "export_statement" {
				exported = true
			}
			if exported || inClass != "" {
				fn := readFn(n, text, name)
				fn.Method = inClass != ""
				fn.Receiver = inClass
				out.Fns = append(out.Fns, fn)
			}
		case "lexical_declaration", "variable_declaration":
			// `export const f = (a: string) => …`: a function a bridge can carry, spelled as a value.
			for i := 0; i < int(n.NamedChildCount()); i++ {
				decl := n.NamedChild(i)
				if decl.Type() != "variable_declarator" {
					continue
				}
				value := decl.ChildByFieldName("value")
				if value == nil || (value.Type() != "arrow_function" && value.Type() != "function_expression") {
					continue
				}
				exported := false
				if parent := n.Parent(); parent != nil && parent.Type() == "export_statement" {
					exported = true
				}
				if !exported {
					continue
				}
				fn := readFn(value, text, text(decl.ChildByFieldName("name")))
				fn.Line = int(decl.StartPoint().Row) + 1
				fn.EndLine = int(decl.EndPoint().Row) + 1
				out.Fns = append(out.Fns, fn)
			}
		case "method_definition":
			if inClass != "" && text(methodModifiers(n)) == "static" {
				fn := readFn(n, text, text(n.ChildByFieldName("name")))
				fn.Method = true
				fn.Receiver = inClass
				out.Fns = append(out.Fns, fn)
			}
		case "class_declaration":
			name := text(n.ChildByFieldName("name"))
			for i := 0; i < int(n.NamedChildCount()); i++ {
				child := n.NamedChild(i)
				if child.Type() == "class_body" {
					walk(child, name)
					return
				}
			}
		case "import_statement":
			if source := n.ChildByFieldName("source"); source != nil {
				out.Imports = append(out.Imports, Import{
					Specifier: strings.Trim(text(source), `"'`),
					Line:      int(n.StartPoint().Row) + 1,
				})
			}
		case "call_expression":
			if fn := n.ChildByFieldName("function"); fn != nil && fn.Type() == "import" {
				if args := n.ChildByFieldName("arguments"); args != nil && args.NamedChildCount() > 0 {
					out.Imports = append(out.Imports, Import{
						Specifier: strings.Trim(text(args.NamedChild(0)), `"'`),
						Dynamic:   true,
						Line:      int(n.StartPoint().Row) + 1,
					})
				}
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i), inClass)
		}
	}
	walk(root, "")
	out.Risks = risks(root, src, text)
	return out, nil
}

// readFn turns a function node into a Fn.
func readFn(n *sitter.Node, text func(*sitter.Node) string, name string) Fn {
	fn := Fn{
		Name:      name,
		Line:      int(n.StartPoint().Row) + 1,
		EndLine:   int(n.EndPoint().Row) + 1,
		Async:     strings.HasPrefix(text(n), "async"),
		Generator: n.Type() == "generator_function_declaration" || strings.Contains(text(n.ChildByFieldName("parameters")), "*"),
	}
	if tp := n.ChildByFieldName("type_parameters"); tp != nil {
		fn.Generic = true
	}
	if params := n.ChildByFieldName("parameters"); params != nil {
		for i := 0; i < int(params.NamedChildCount()); i++ {
			p := params.NamedChild(i)
			param := Param{Type: "any"}
			switch p.Type() {
			case "required_parameter", "optional_parameter":
				param.Name = text(p.ChildByFieldName("pattern"))
				if p.Type() == "optional_parameter" {
					param.Optional = true
				}
				if t := p.ChildByFieldName("type"); t != nil {
					param.Type = normalizeType(text(t))
				}
				if v := p.ChildByFieldName("value"); v != nil {
					param.Optional = true
				}
			case "rest_pattern":
				param.Rest = true
				param.Name = strings.TrimPrefix(text(p), "...")
			case "identifier":
				param.Name = text(p)
			default:
				param.Name = text(p)
			}
			fn.Params = append(fn.Params, param)
		}
	}
	if ret := n.ChildByFieldName("return_type"); ret != nil {
		fn.Returns = normalizeType(text(ret))
	}
	// Every `throw` is an error path that needs a recorded failing case.
	var throws func(node *sitter.Node)
	throws = func(node *sitter.Node) {
		if node.Type() == "throw_statement" {
			fn.ThrowLines = append(fn.ThrowLines, int(node.StartPoint().Row)+1)
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			throws(node.NamedChild(i))
		}
	}
	throws(n)
	return fn
}

func methodModifiers(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type() == "static" || child.Type() == "public" || child.Type() == "private" ||
			child.Type() == "async" || child.Type() == "get" || child.Type() == "set" {
			return child
		}
	}
	return nil
}

// normalizeType strips the annotation's colon and maps the handful of shorthands the dialect has.
func normalizeType(t string) string {
	t = strings.TrimSpace(t)
	for _, prefix := range []string{": ", ":"} {
		t = strings.TrimPrefix(t, prefix)
	}
	t = strings.TrimSpace(t)
	switch t {
	case "":
		return "any"
	case "String":
		return "string"
	case "Number":
		return "number"
	case "Boolean":
		return "boolean"
	}
	return t
}

// ParserPath answers where a separate parser would be. This build is the parser, so there is none: the
// function exists so the code that asks compiles in both builds.
func ParserPath() (string, error) { return "", errors.New("this build parses TypeScript itself") }

// pyExports reads the module-level functions of a Python file.
//
// A name that starts with an underscore is private, and a function defined inside a class has `self` as its
// first parameter: both are left out, because a bridge carries plain functions.
func pyExports(root *sitter.Node, src []byte, text func(*sitter.Node) string) []Fn {
	var out []Fn
	for i := 0; i < int(root.NamedChildCount()); i++ {
		n := root.NamedChild(i)
		if n.Type() != "function_definition" {
			continue
		}
		name := text(n.ChildByFieldName("name"))
		if name == "" || strings.HasPrefix(name, "_") {
			continue
		}
		fn := Fn{Name: name, Line: int(n.StartPoint().Row) + 1, EndLine: int(n.EndPoint().Row) + 1}
		params := n.ChildByFieldName("parameters")
		if params != nil {
			for j := 0; j < int(params.NamedChildCount()); j++ {
				param := params.NamedChild(j)
				switch param.Type() {
				case "identifier":
					fn.Params = append(fn.Params, Param{Name: text(param), Type: "any"})
				case "typed_parameter", "typed_default_parameter":
					name := ""
					for k := 0; k < int(param.NamedChildCount()); k++ {
						if c := param.NamedChild(k); c.Type() == "identifier" {
							name = text(c)
							break
						}
					}
					typ := "any"
					if t := param.ChildByFieldName("type"); t != nil {
						typ = text(t)
					}
					fn.Params = append(fn.Params, Param{Name: name, Type: typ})
				case "default_parameter":
					if name := param.ChildByFieldName("name"); name != nil {
						fn.Params = append(fn.Params, Param{Name: text(name), Type: "any", Optional: true})
					}
				}
			}
		}
		// `raise X` anywhere in the body means the original has a path that throws.
		body := n.ChildByFieldName("body")
		if body != nil {
			walkRaises(body, func(line int) {
				fn.ThrowLines = append(fn.ThrowLines, line)
			})
		}
		if len(fn.Params) == 0 {
			fn.Params = []Param{}
		}
		out = append(out, fn)
	}
	return out
}

// walkRaises reports the line of every raise it finds, depth first.
func walkRaises(n *sitter.Node, report func(int)) {
	if n == nil {
		return
	}
	if n.Type() == "raise_statement" {
		report(int(n.StartPoint().Row) + 1)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		walkRaises(n.NamedChild(i), report)
	}
}
