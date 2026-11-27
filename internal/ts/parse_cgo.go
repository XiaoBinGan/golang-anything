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
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
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
	case Ruby:
		return ruby.GetLanguage()
	case Java:
		return java.GetLanguage()
	case C:
		return c.GetLanguage()
	case Cpp:
		return cpp.GetLanguage()
	case Php:
		return php.GetLanguage()
	case Kotlin:
		return kotlin.GetLanguage()
	case CSharp:
		return csharp.GetLanguage()
	case PowerShell:
		// The powershell grammar lives in another module; until it is a dependency, a .ps1 file is named
		// as unread rather than half-read.
		return nil
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
	grammar := language(lang)
	if grammar == nil {
		// A language with a scanner instead of a grammar takes that path before this point.
		return ScanPowerShell(src, path), nil
	}
	p := sitter.NewParser()
	p.SetLanguage(grammar)
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
	if lang == PowerShell {
		// No grammar: the scanner in powershell.go reads the shapes PowerShell code uses.
		scanned := ScanPowerShell(src, path)
		scanned.Imports = out.Imports
		return scanned, nil
	}
	if lang == Php {
		fns, instanceMethods, free, promotable, names := phpExports(root, text)
		out.Fns = append(out.Fns, fns...)
		out.InstanceMethods, out.InstanceFree, out.InstancePromotable = instanceMethods, free, promotable
		out.Promotable = names
		return out, nil
	}
	if lang == Kotlin || lang == CSharp {
		fns, instances, free, promotable := typedExports(root, text)
		out.Fns = append(out.Fns, fns...)
		out.InstanceMethods, out.InstanceFree = instances, free
		out.InstancePromotable, out.Promotable = len(promotable), promotable
		return out, nil
	}
	if lang == C || lang == Cpp {
		out.Fns = append(out.Fns, cExports(root, text)...)
		return out, nil
	}
	if lang == Java {
		fns, instances, free, promotable := javaExports(root, text)
		out.Fns = append(out.Fns, fns...)
		out.InstanceMethods, out.InstanceFree = instances, free
		out.InstancePromotable, out.Promotable = len(promotable), promotable
		return out, nil
	}
	if lang == Ruby {
		fns, instanceMethods, free, promotable, names := rbExports(root, text)
		out.Fns = append(out.Fns, fns...)
		out.InstanceMethods, out.InstanceFree, out.InstancePromotable = instanceMethods, free, promotable
		out.Promotable = names
		return out, nil
	}
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
		// `raise X` anywhere in the body means the original has a path that throws; a `yield` makes the
		// function a generator, which answers a generator object rather than a value.
		body := n.ChildByFieldName("body")
		if body != nil {
			walkRaises(body, func(line int) {
				fn.ThrowLines = append(fn.ThrowLines, line)
			})
			if hasYield(body) {
				fn.Generator = true
			}
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

// rbExports reads the top-level methods of a Ruby file. A name starting with an underscore is private by
// convention here too, and a method defined inside a class belongs to that class.
func rbExports(root *sitter.Node, text func(*sitter.Node) string) ([]Fn, int, int, int, []Promotable) {
	var out []Fn
	// Instance methods are still not exports — the bridge has no instance to call them on — but they are
	// counted, because "0 exports" and "0 exports, 6 instance methods" are different answers for a library
	// like Carbon where every method needs an object.
	instances, free, promotable := 0, 0, 0
	var promotableNames []Promotable
	// A Ruby library keeps its callable units in three shapes: a top-level def, a module function
	// (`def self.x` or `module_function`), and a class method. The last two are what a real gem is made of —
	// sinatra has ten `def self.` and Carbon is all class methods — and only the first was read before.
	var walk func(n *sitter.Node, owner string, ownerKind string)
	walk = func(n *sitter.Node, owner, ownerKind string) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "module", "class":
				name := ""
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if c := child.NamedChild(j); c.Type() == "constant" {
						name = text(c)
						break
					}
				}
				kind := "module"
				if child.Type() == "class" {
					kind = "class"
				}
				moduleFunction := strings.Contains(text(child), "module_function")
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if body := child.NamedChild(j); body.Type() == "body_statement" {
						walk(body, name, kind+map[bool]string{true: ":module_function", false: ""}[moduleFunction])
					}
				}
			case "singleton_method":
				// `def self.name` inside a module or a class: callable as Owner.name.
				name := text(singletonName(child))
				if !usableRbName(name) {
					continue
				}
				fn := rbFn(child, name, text)
				if owner != "" {
					fn.Method = true
					fn.Receiver = owner
				}
				if rbTouchesState(rbBody(child, text)) {
					fn.State = true
				}
				out = append(out, fn)
			case "method":
				name := text(child.ChildByFieldName("name"))
				if !usableRbName(name) {
					continue
				}
				if owner != "" && !strings.Contains(ownerKind, "module_function") {
					// Inside a module or class, a plain def is an instance method: it needs an instance that
					// a bridge cannot supply. Reading it as a free function was wrong — the recorder called
					// it on Object and got NoMethodError, or worse, hit another class's method of the same
					// name.
					// A body that touches no state is the only kind that could ever be promoted, and counting
					// those separately is what ROADMAP's first item needs to start from.
					if !rbTouchesState(rbBody(child, text)) {
						free++
						// A class can be instantiated without its initialize (`allocate`); a module cannot be
						// instantiated at all, so its state-free methods are the one case promotion cannot
						// reach without changing what the module means.
						if ownerKind == "class" {
							promotable++
							promotableNames = append(promotableNames, Promotable{Owner: owner, Name: name})
						}
					}
					instances++
					continue
				}
				fn := rbFn(child, name, text)
				if strings.Contains(ownerKind, "module_function") && owner != "" {
					fn.Method = true
					fn.Receiver = owner
				}
				if rbTouchesState(rbBody(child, text)) {
					fn.State = true
				}
				out = append(out, fn)
			}
		}
	}
	walk(root, "", "")
	return out, instances, free, promotable, promotableNames
}

// singletonName is the identifier after `self.` in `def self.name`.
func singletonName(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if c := n.NamedChild(i); c.Type() == "identifier" {
			return c
		}
	}
	return nil
}

// usableRbName keeps the names a bridge can call: not private by convention, not a predicate or a bang
// method (the first answers a boolean, the second mutates), and not an operator.
func usableRbName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "_") && !strings.HasSuffix(name, "?") &&
		!strings.HasSuffix(name, "!") && !strings.HasSuffix(name, "=")
}

// rbFn reads one Ruby method definition.
func rbFn(n *sitter.Node, name string, text func(*sitter.Node) string) Fn {
	fn := Fn{Name: name, Line: int(n.StartPoint().Row) + 1, EndLine: int(n.EndPoint().Row) + 1, Params: []Param{}}
	if params := n.ChildByFieldName("parameters"); params != nil {
		for j := 0; j < int(params.NamedChildCount()); j++ {
			p := params.NamedChild(j)
			// Ruby has no annotations, so every parameter is "any" and the recorder generates across types.
			if p.Type() == "identifier" {
				fn.Params = append(fn.Params, Param{Name: text(p), Type: "any"})
			}
		}
	}
	walkRaises(n, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
	return fn
}

// javaExports reads the public static methods of the classes in a file: those are the ones a bridge can
// call without an instance.
func javaExports(root *sitter.Node, text func(*sitter.Node) string) ([]Fn, int, int, []Promotable) {
	var out []Fn
	// Java is the language where a library is a set of classes full of instance methods (jadx is one), and the plan
	// said nothing about them: no count, no sentence. Counted here, not promoted — a Java proxy class is a separate
	// piece of work.
	instances, free := 0, 0
	var promotable []Promotable
	var walkClass func(n *sitter.Node, className string, hasFields, noArgCtor bool)
	walkClass = func(n *sitter.Node, className string, hasFields, noArgCtor bool) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "class_declaration", "enum_declaration", "interface_declaration":
				name := text(child.ChildByFieldName("name"))
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if body := child.NamedChild(j); body.Type() == "class_body" {
						walkClass(body, name, classHasInstanceFields(body, text), classHasNoArgCtor(body))
					}
				}
			case "method_declaration":
				mods := ""
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if c := child.NamedChild(j); c.Type() == "modifiers" {
						mods = text(c)
						break
					}
				}
				if !strings.Contains(mods, "static") || !strings.Contains(mods, "public") {
					if strings.Contains(mods, "public") && !strings.Contains(mods, "static") &&
						!strings.Contains(mods, "abstract") && !strings.Contains(mods, "native") {
						instances++
						// A method is only state-free if the class declares no instance fields: `return n + offset;`
						// reads the same state as `this.offset`, and looking for "this." alone called that free.
						if body := child.ChildByFieldName("body"); body != nil && !hasFields &&
							!strings.Contains(text(body), "this.") {
							free++
							// Promotable needs one more thing a proxy cannot invent: a no-arg constructor to call.
							// Java supplies an implicit one when the class declares none.
							if noArgCtor {
								promotable = append(promotable, Promotable{
									Owner: className, Name: text(child.ChildByFieldName("name")),
									Params: javaParams(child, text), Returns: javaReturns(child, text),
								})
							}
						}
					}
					continue
				}
				name := text(child.ChildByFieldName("name"))
				if name == "" || name == "main" {
					continue
				}
				fn := Fn{Name: name, Returns: javaReturns(child, text), Method: true, Receiver: className,
					Line: int(child.StartPoint().Row) + 1, EndLine: int(child.EndPoint().Row) + 1,
					Params: javaParams(child, text)}
				walkRaises(child, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
				out = append(out, fn)
			}
		}
	}
	walkClass(root, "", false, false)
	return out, instances, free, promotable
}

// classHasInstanceFields reports whether a Java class body declares a non-static field: the only stateless methods a
// proxy could reach are in a class that has no instance state to miss.
func classHasInstanceFields(body *sitter.Node, text func(*sitter.Node) string) bool {
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		if child.Type() != "field_declaration" {
			continue
		}
		mods := ""
		for j := 0; j < int(child.NamedChildCount()); j++ {
			if c := child.NamedChild(j); c.Type() == "modifiers" {
				mods = text(c)
				break
			}
		}
		if !strings.Contains(mods, "static") {
			return true
		}
	}
	return false
}

// classHasNoArgCtor reports whether `new Owner()` will compile: no constructor at all (Java's implicit one), or one
// declared without parameters. A constructor with parameters means a proxy cannot supply them.
func classHasNoArgCtor(body *sitter.Node) bool {
	declared := false
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		if child.Type() != "constructor_declaration" {
			continue
		}
		declared = true
		if params := child.ChildByFieldName("parameters"); params == nil || int(params.NamedChildCount()) == 0 {
			return true
		}
	}
	return !declared
}

// javaReturns reads a Java method's return type: the first child that is neither a modifier, the name, the parameter
// list nor the body, and that has no children of its own (a qualified name would).
func javaReturns(n *sitter.Node, text func(*sitter.Node) string) string {
	for j := 0; j < int(n.NamedChildCount()); j++ {
		c := n.NamedChild(j)
		switch c.Type() {
		case "modifiers", "identifier", "formal_parameters", "block":
			continue
		case "integral_type", "floating_point_type", "void_type", "boolean_type":
			// These have children of their own (`int` is an `integral_type` with a keyword), so the "no children"
			// rule skipped them and the return type came out empty — a proxy written from that would not compile.
			return javaDialect(text(c))
		}
		if c.ChildCount() == 0 {
			return javaDialect(text(c))
		}
	}
	return ""
}

// javaParams reads a Java method's parameters. Shared by the export path and the promotion path, which needs the same
// names and types to write a proxy that forwards them.
func javaParams(n *sitter.Node, text func(*sitter.Node) string) []Param {
	params := []Param{}
	list := n.ChildByFieldName("parameters")
	if list == nil {
		return params
	}
	for k := 0; k < int(list.NamedChildCount()); k++ {
		param := list.NamedChild(k)
		if param.Type() != "formal_parameter" {
			continue
		}
		typ, name := "any", ""
		if t := param.ChildByFieldName("type"); t != nil {
			typ = text(t)
		}
		if nm := param.ChildByFieldName("name"); nm != nil {
			name = text(nm)
		}
		params = append(params, Param{Name: name, Type: javaDialect(typ)})
	}
	return params
}

// javaDialect narrows a Java type to one of the generator's tables, the way dialectType does for TypeScript.
func javaDialect(t string) string {
	switch strings.TrimSpace(t) {
	case "int", "long", "short", "byte", "Integer", "Long":
		return "int"
	case "double", "float", "Double", "Float", "BigDecimal":
		return "float"
	case "boolean", "Boolean":
		return "boolean"
	case "String", "CharSequence":
		return "String"
	case "char", "Character":
		return "char"
	}
	return "any"
}

// srcText exists so the C walker can look at a node's own text without threading src through every call.
func srcText(src []byte) func(*sitter.Node) string {
	return func(n *sitter.Node) string {
		if n == nil {
			return ""
		}
		return n.Content(src)
	}
}

// cExports reads the functions a C or C++ file defines at file scope. A static function is private and is
// left out; so is anything inside a namespace (a bridge calls plain names).
func cExports(root *sitter.Node, text func(*sitter.Node) string) []Fn {
	var out []Fn
	for i := 0; i < int(root.NamedChildCount()); i++ {
		n := root.NamedChild(i)
		if n.Type() != "function_definition" {
			continue
		}
		decl := n.ChildByFieldName("declarator")
		for decl != nil && decl.Type() != "function_declarator" {
			decl = decl.ChildByFieldName("declarator")
		}
		if decl == nil {
			continue
		}
		name := text(decl.ChildByFieldName("declarator"))
		if name == "" || strings.HasPrefix(name, "_") || name == "main" {
			continue
		}
		// static means file-private in C, which is not something a bridge can call.
		private := false
		for j := 0; j < int(n.NamedChildCount()); j++ {
			if c := n.NamedChild(j); c.Type() == "storage_class_specifier" && text(c) == "static" {
				private = true
			}
		}
		// zlib spells it `local`, a macro for static: a name that starts with it is file-private too.
		if head := text(n); strings.HasPrefix(strings.TrimSpace(head), "local") {
			private = true
		}
		if private {
			continue
		}
		fn := Fn{Name: name, Line: int(n.StartPoint().Row) + 1, EndLine: int(n.EndPoint().Row) + 1, Params: []Param{}}
		if typ := n.ChildByFieldName("type"); typ != nil {
			fn.Returns = cDialect(text(typ))
		}
		if params := decl.ChildByFieldName("parameters"); params != nil {
			for j := 0; j < int(params.NamedChildCount()); j++ {
				p := params.NamedChild(j)
				if p.Type() == "parameter_declaration" {
					typ := "any"
					if t := p.ChildByFieldName("type"); t != nil {
						typ = cDialect(text(t))
					}
					// `char *` is a string, not a char: the pointer is in the declarator, so the type alone
					// says "char" and the recorder would generate a character for a string parameter.
					declarator := p.ChildByFieldName("declarator")
					if declarator != nil && hasPointer(declarator) && typ != "char" {
						// A pointer that is not `char *` is a buffer whose length the caller passes
						// separately: generating one without the other reads past the end, which is
						// undefined behaviour rather than a case. zlib's adler32_z(adler, buf, len) is
						// exactly that shape.
						typ = "buffer"
					}
					if typ == "char" && declarator != nil && hasPointer(declarator) {
						typ = "String"
						// `const char *` is a string; a plain `char *` is a buffer the function writes into,
						// and the effect is the buffer, which JSON cannot carry. Handing it a literal is
						// undefined behaviour, and the recorder did exactly that once: it recorded `5` for a
						// truncation, because strlen was called on read-only memory nothing had written.
						if !strings.Contains(text(p), "const") {
							typ = "buffer"
						}
					}
					fn.Params = append(fn.Params, Param{Name: text(declarator), Type: typ})
					continue
				}
				// In C++ the parameter type may be a bare qualified name.
				if p.Type() == "identifier" || p.Type() == "type_identifier" {
					fn.Params = append(fn.Params, Param{Name: text(p), Type: "any"})
				}
			}
		}
		walkRaises(n, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
		out = append(out, fn)
	}
	return out
}

// cDialect narrows a C or C++ type to one of the generator's tables.
func cDialect(t string) string {
	t = strings.TrimSpace(t)
	// C libraries are full of typedefs (uLong, z_size_t, Bytef). A name with none of string, char or pointer
	// in it is an integer: generating a string for it does not compile, which is how zlib's adler32 blocked
	// on twenty errors before this rule existed.
	lower := strings.ToLower(t)
	switch {
	case strings.Contains(lower, "char") && strings.Contains(lower, "*"):
		return "String"
	case strings.Contains(lower, "*"):
		return "buffer"
	case strings.Contains(lower, "double"), strings.Contains(lower, "float"):
		return "float"
	case strings.Contains(lower, "bool"):
		return "boolean"
	case strings.Contains(lower, "char"):
		return "char"
	case strings.Contains(lower, "int"), strings.Contains(lower, "long"), strings.Contains(lower, "short"),
		strings.Contains(lower, "size"), strings.Contains(lower, "off"), strings.Contains(lower, "ulong"):
		// z_off64_t, z_size_t, uLong: a typedef whose name says it is a number. Testing for "off_t" was not
		// enough — the name is off64_t — and the parameter fell through to "any", which generated strings for
		// a length and did not compile.
		return "int"
	}
	switch t {
	case "int", "long", "short", "unsigned", "unsigned int", "size_t", "int32_t", "int64_t":
		return "int"
	case "double", "float", "long double":
		return "float"
	case "bool", "_Bool":
		return "boolean"
	case "char *", "const char *", "std::string", "string", "char*":
		return "String"
	case "char":
		return "char"
	}
	return "any"
}

// hasPointer reports whether a C declarator involves a pointer.
func hasPointer(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if n.Type() == "pointer_declarator" {
		return true
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if hasPointer(n.NamedChild(i)) {
			return true
		}
	}
	return n.Type() == "pointer_expression"
}

// phpExports reads the free functions of a PHP file: those are the ones a bridge can call.
// Every PHP method lives in a class, so every state-free one is promotable: `(new Owner)->name(...)` needs no
// constructor arguments to exist.
func phpExports(root *sitter.Node, text func(*sitter.Node) string) ([]Fn, int, int, int, []Promotable) {
	var out []Fn
	instances, free := 0, 0
	var promotableNames []Promotable
	var walk func(n *sitter.Node, inside string)
	walk = func(n *sitter.Node, owner string) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "method_declaration":
				// `public static function x()` inside a class is callable as Class::x(), which needs no
				// instance: that is the shape a real PHP library is made of (Carbon has hundreds).
				head := text(child)
				if !strings.Contains(head, "static") || !strings.Contains(head, "public") {
					if strings.Contains(head, "public") && !strings.Contains(head, "static") {
						if !phpTouchesState(rbBody(child, text)) {
							free++
							var params []Param
							if list := child.ChildByFieldName("parameters"); list != nil {
								for j := 0; j < int(list.NamedChildCount()); j++ {
									p := list.NamedChild(j)
									if p.Type() != "simple_parameter" {
										continue
									}
									typ := "any"
									if t := p.ChildByFieldName("type"); t != nil {
										typ = cDialect(text(t))
									}
									params = append(params, Param{Name: text(p.ChildByFieldName("name")), Type: typ})
								}
							}
							returns := ""
							if typ := child.ChildByFieldName("return_type"); typ != nil {
								returns = cDialect(text(typ))
							}
							promotableNames = append(promotableNames, Promotable{
								Owner: owner, Name: text(child.ChildByFieldName("name")),
								Params: params, Returns: returns,
							})
						}
						instances++
					}
					continue
				}
				name := text(child.ChildByFieldName("name"))
				if name == "" || strings.HasPrefix(name, "__") {
					continue
				}
				fn := Fn{Name: name, Method: true, Receiver: owner,
					Line: int(child.StartPoint().Row) + 1, EndLine: int(child.EndPoint().Row) + 1, Params: []Param{}}
				if typ := child.ChildByFieldName("return_type"); typ != nil {
					fn.Returns = cDialect(text(typ))
				}
				if params := child.ChildByFieldName("parameters"); params != nil {
					for j := 0; j < int(params.NamedChildCount()); j++ {
						p := params.NamedChild(j)
						if p.Type() != "simple_parameter" {
							continue
						}
						typ := "any"
						if t := p.ChildByFieldName("type"); t != nil {
							typ = cDialect(text(t))
						}
						fn.Params = append(fn.Params, Param{Name: text(p.ChildByFieldName("name")), Type: typ})
					}
				}
				walkRaises(child, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
				if phpTouchesState(rbBody(child, text)) {
					fn.State = true
				}
				out = append(out, fn)
			case "function_definition":
				if owner != "" {
					continue
				}
				fn := Fn{Name: text(child.ChildByFieldName("name")),
					Line: int(child.StartPoint().Row) + 1, EndLine: int(child.EndPoint().Row) + 1, Params: []Param{}}
				if typ := child.ChildByFieldName("return_type"); typ != nil {
					fn.Returns = cDialect(text(typ))
				}
				if params := child.ChildByFieldName("parameters"); params != nil {
					for j := 0; j < int(params.NamedChildCount()); j++ {
						p := params.NamedChild(j)
						if p.Type() != "simple_parameter" {
							continue
						}
						typ := "any"
						if t := p.ChildByFieldName("type"); t != nil {
							typ = cDialect(text(t))
						}
						fn.Params = append(fn.Params, Param{Name: text(p.ChildByFieldName("name")), Type: typ})
					}
				}
				walkRaises(child, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
				out = append(out, fn)
			case "class_declaration", "interface_declaration", "trait_declaration":
				name := ""
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if c := child.NamedChild(j); c.Type() == "name" {
						name = text(c)
						break
					}
				}
				walk(child, name)
			case "declaration_list":
				walk(child, owner)
			}
		}
	}
	walk(root, "")
	return out, instances, free, free, promotableNames
}

// typedExports reads Kotlin and C# functions: top-level `fun` for Kotlin, public static methods for C#.
func typedExports(root *sitter.Node, text func(*sitter.Node) string) ([]Fn, int, int, []Promotable) {
	var out []Fn
	// Kotlin's libraries are classes full of member functions, and the plan said nothing about them. Counted, not
	// promoted: a Kotlin proxy is a separate piece of work.
	instances, free := 0, 0
	var promotable []Promotable
	var walk func(n *sitter.Node, className string, hasProps, noArgCtor bool)
	walk = func(n *sitter.Node, className string, hasProps, noArgCtor bool) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "function_declaration":
				// Kotlin: name and parameters are fields, the return type is a field called "type".
				name := kotlinName(child, text)
				if name == "" || strings.HasPrefix(name, "_") || name == "main" {
					continue
				}
				fn := Fn{Name: name, Line: int(child.StartPoint().Row) + 1, EndLine: int(child.EndPoint().Row) + 1, Params: []Param{}}
				if className != "" {
					fn.Method = true
					fn.Receiver = className
				}
				if typ := kotlinField(child, text, "user_type", "nullable_type"); typ != "" {
					fn.Returns = ktDialect(typ)
				}
				if params := kotlinParams(child); params != nil {
					for j := 0; j < int(params.NamedChildCount()); j++ {
						p := params.NamedChild(j)
						if p.Type() != "parameter" && p.Type() != "class_parameter" {
							continue
						}
						name, typ := "", "any"
						for k := 0; k < int(p.NamedChildCount()); k++ {
							c := p.NamedChild(k)
							switch c.Type() {
							case "simple_identifier":
								if name == "" {
									name = text(c)
								}
							case "user_type", "nullable_type":
								typ = ktDialect(text(c))
							}
						}
						fn.Params = append(fn.Params, Param{Name: name, Type: typ})
					}
				}
				walkRaises(child, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
				out = append(out, fn)
				if className != "" {
					instances++
					// A member function is only state-free when the class declares no property: `n + offset` reads
					// the same state `this.offset` reads, so looking for `this.` alone is not enough.
					if !hasProps && !strings.Contains(text(child), "this.") {
						free++
						// A Kotlin proxy is `Owner().name(args)`, so the class needs a no-arg constructor: none
						// declared (the implicit one) or one declared without parameters.
						if noArgCtor {
							promotable = append(promotable, Promotable{
								Owner: className, Name: fn.Name, Params: fn.Params, Returns: fn.Returns,
							})
						}
					}
				}
			case "method_declaration":
				name := text(child.ChildByFieldName("name"))
				if name == "" || className == "" || name == "Main" {
					continue
				}
				mods := ""
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if c := child.NamedChild(j); c.Type() == "modifier" || c.Type() == "modifiers" {
						mods += text(c) + " "
					}
				}
				if !strings.Contains(mods, "static") || !strings.Contains(mods, "public") {
					// A C# library is classes full of instance methods too, and the plan said nothing about them.
					if strings.Contains(mods, "public") && !strings.Contains(mods, "static") &&
						!strings.Contains(mods, "abstract") && className != "" {
						instances++
						if body := csMethodBody(child); body != nil && !hasProps && !strings.Contains(text(body), "this.") {
							free++
							if noArgCtor {
								promotable = append(promotable, Promotable{
									Owner: className, Name: name, Params: fnParams(child, text, csDialect),
									Returns: csReturns(child, text),
								})
							}
						}
					}
					continue
				}
				fn := Fn{Name: name, Method: true, Receiver: className,
					Line: int(child.StartPoint().Row) + 1, EndLine: int(child.EndPoint().Row) + 1, Params: []Param{}}
				if typ := child.ChildByFieldName("returns"); typ != nil {
					fn.Returns = csDialect(text(typ))
				}
				if params := child.ChildByFieldName("parameters"); params != nil {
					for j := 0; j < int(params.NamedChildCount()); j++ {
						p := params.NamedChild(j)
						if p.Type() != "parameter" {
							continue
						}
						typ := "any"
						if t := p.ChildByFieldName("type"); t != nil {
							typ = csDialect(text(t))
						}
						fn.Params = append(fn.Params, Param{Name: text(p.ChildByFieldName("name")), Type: typ})
					}
				}
				walkRaises(child, func(line int) { fn.ThrowLines = append(fn.ThrowLines, line) })
				out = append(out, fn)
			case "object_declaration":
				// A Kotlin `object` is a singleton whose members are reachable as `Object.name(...)`: the same shape a
				// generated proxy needs, and the parser used to ignore the body entirely.
				name := text(child.ChildByFieldName("name"))
				if name == "" {
					for j := 0; j < int(child.NamedChildCount()); j++ {
						if c := child.NamedChild(j); c.Type() == "type_identifier" {
							name = text(c)
							break
						}
					}
				}
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if body := child.NamedChild(j); body.Type() == "class_body" {
						walk(body, name, kotlinClassHasProps(child), kotlinHasNoArgCtor(child))
					}
				}
			case "class_declaration":
				// Kotlin puts the class name in a `type_identifier` child, not in a `name` field: reading the field
				// answered "", so every member function came out with no owner and no way to say whose method it is.
				name := text(child.ChildByFieldName("name"))
				if name == "" {
					for j := 0; j < int(child.NamedChildCount()); j++ {
						if c := child.NamedChild(j); c.Type() == "type_identifier" {
							name = text(c)
							break
						}
					}
				}
				for j := 0; j < int(child.NamedChildCount()); j++ {
					if body := child.NamedChild(j); body.Type() == "class_body" || body.Type() == "declaration_list" {
						// C# reports `declaration_list`, Kotlin `class_body`: the shape helper follows the syntax.
						if body.Type() == "declaration_list" {
							fields, noArg := csClassShape(body, text)
							walk(body, name, fields, noArg)
						} else {
							walk(body, name, kotlinClassHasProps(child), kotlinHasNoArgCtor(child))
						}
					}
				}
			}
		}
	}
	walk(root, "", false, false)
	return out, instances, free, promotable
}

// csClassShape reports what a C# proxy needs: whether the class declares instance fields, and whether `new Owner()`
// compiles (no constructor, or one without parameters).
func csClassShape(body *sitter.Node, text func(*sitter.Node) string) (hasFields, noArgCtor bool) {
	noArgCtor, declared := true, false
	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		switch child.Type() {
		case "field_declaration":
			mods := ""
			for j := 0; j < int(child.NamedChildCount()); j++ {
				if c := child.NamedChild(j); c.Type() == "modifier" || c.Type() == "modifiers" {
					mods += text(c) + " "
				}
			}
			if !strings.Contains(mods, "static") && !strings.Contains(mods, "const") {
				hasFields = true
			}
		case "constructor_declaration":
			declared = true
			params := child.ChildByFieldName("parameters")
			noArgCtor = params != nil && int(params.NamedChildCount()) == 0
		}
	}
	return hasFields, noArgCtor && (!declared || noArgCtor)
}

// csMethodBody finds the block of a C# method, whose body is a `block` child rather than a field.
func csMethodBody(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if c := n.NamedChild(i); c.Type() == "block" {
			return c
		}
	}
	return nil
}

// fnParams reads a parameter list with the language's own dialect, shared by the languages whose nodes agree.
func fnParams(n *sitter.Node, text func(*sitter.Node) string, dialect func(string) string) []Param {
	params := []Param{}
	list := n.ChildByFieldName("parameters")
	if list == nil {
		return params
	}
	for k := 0; k < int(list.NamedChildCount()); k++ {
		p := list.NamedChild(k)
		if p.Type() != "parameter" {
			continue
		}
		typ := "any"
		if t := p.ChildByFieldName("type"); t != nil {
			typ = dialect(text(t))
		}
		params = append(params, Param{Name: text(p.ChildByFieldName("name")), Type: typ})
	}
	return params
}

// csReturns reads a C# method's return type.
func csReturns(n *sitter.Node, text func(*sitter.Node) string) string {
	if t := n.ChildByFieldName("returns"); t != nil {
		return csDialect(text(t))
	}
	return ""
}

// kotlinHasNoArgCtor reports whether `Owner()` compiles: no primary constructor at all, or one that declares no
// parameter carrying `val`/`var` (a plain parameter without those is still an argument the proxy cannot supply).
func kotlinHasNoArgCtor(class *sitter.Node) bool {
	for i := 0; i < int(class.NamedChildCount()); i++ {
		c := class.NamedChild(i)
		if c.Type() != "primary_constructor" {
			continue
		}
		return int(c.NamedChildCount()) == 0
	}
	return true
}

// kotlinClassHasProps reports whether a Kotlin class body declares a property: a `val`/`var` inside the body, or a
// constructor parameter marked `val`/`var` (which the parse reports as `class_parameter`).
func kotlinClassHasProps(class *sitter.Node) bool {
	// Two places hold state: `val x = …` in the body, and a constructor parameter marked `val`/`var`, which lives in
	// the primary constructor beside the body. Checking only the body said a class with `(private val offset: Int)`
	// had no state.
	var found bool
	var scan func(n *sitter.Node, depth int)
	scan = func(n *sitter.Node, depth int) {
		if found || depth > 3 {
			return
		}
		switch n.Type() {
		case "property_declaration":
			found = true
			return
		case "class_parameter":
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if c := n.NamedChild(i); c.Type() == "binding_pattern_kind" {
					found = true
					return
				}
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			scan(n.NamedChild(i), depth+1)
		}
	}
	scan(class, 0)
	return found
}

// ktDialect narrows a Kotlin type.
func ktDialect(t string) string {
	switch strings.TrimSpace(t) {
	case "Int", "Long", "Short":
		return "int"
	case "Double", "Float":
		return "float"
	case "Boolean":
		return "boolean"
	case "String", "CharSequence":
		return "String"
	case "Char":
		return "char"
	}
	return "any"
}

// csDialect narrows a C# type.
func csDialect(t string) string {
	switch strings.TrimSpace(t) {
	case "int", "long", "short", "Int32", "Int64":
		return "int"
	case "double", "float", "decimal", "Double", "Single":
		return "float"
	case "bool", "Boolean":
		return "boolean"
	case "string", "String":
		return "String"
	case "char", "Char":
		return "char"
	}
	return "any"
}

// kotlinName reads the simple identifier a Kotlin function is declared with.
func kotlinName(n *sitter.Node, text func(*sitter.Node) string) string {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if c := n.NamedChild(i); c.Type() == "simple_identifier" {
			return text(c)
		}
	}
	return ""
}

// kotlinField answers the text of the first child of one of the given types.
func kotlinField(n *sitter.Node, text func(*sitter.Node) string, types ...string) string {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		for _, want := range types {
			if c.Type() == want {
				return text(c)
			}
		}
	}
	return ""
}

// kotlinParams finds the parameter list of a Kotlin function.
func kotlinParams(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if c := n.NamedChild(i); c.Type() == "function_value_parameters" {
			return c
		}
	}
	return nil
}

// hasYield reports whether a function body contains a yield or yield from.
func hasYield(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if n.Type() == "yield" {
		return true
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if hasYield(n.NamedChild(i)) {
			return true
		}
	}
	return false
}

// rbTouchesState reports whether a Ruby method body reads or writes state outside its arguments: an
// instance variable, a class variable, or an explicit self call.
func rbTouchesState(body string) bool {
	// Coarse on purpose: `@` anywhere in the body means an instance variable may be involved, and a false
	// block is cheap (a person reads the reason) while a false pass records answers that depend on state.
	for _, marker := range []string{"@", "self."} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// phpTouchesState reports whether a PHP method body reads or writes class state.
func phpTouchesState(body string) bool {
	for _, marker := range []string{"$this", "self::", "static::", "parent::"} {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// rbBody answers a method's body text: the state check must not see the definition line, which says
// `def self.name` whether or not the body touches anything.
func rbBody(n *sitter.Node, text func(*sitter.Node) string) string {
	for _, field := range []string{"body", "body_statement"} {
		if b := n.ChildByFieldName(field); b != nil {
			return text(b)
		}
	}
	// None of the grammars agree on the field name: fall back to the last named child, which is the body in
	// every one of them.
	if n.NamedChildCount() > 0 {
		return text(n.NamedChild(int(n.NamedChildCount()) - 1))
	}
	return ""
}
