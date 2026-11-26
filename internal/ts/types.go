// Package ts reads TypeScript and JavaScript: the declarations a bridge can carry, the parameter and
// return types, and the calls that usually cost a rewrite its correctness.
//
// The parse itself is tree-sitter and needs cgo. Everything a caller touches without parsing — the
// types below and LangOf — lives in this file so a build without cgo can still name them; the parse
// is compiled in only when cgo is available.
package ts

import "strings"

type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "any" when the source does not say
	Optional bool   `json:"optional"` // `a?: T` or a default
	Rest     bool   `json:"rest"`     // `...rest: T[]`
}

// Fn is one callable a bridge can carry.
type Fn struct {
	Name      string  `json:"name"`
	Params    []Param `json:"params"`
	Returns   string  `json:"returns"`
	Line      int     `json:"line"`
	EndLine   int     `json:"end_line"`
	Async     bool    `json:"async,omitempty"`
	Generator bool    `json:"generator,omitempty"`
	Method    bool    `json:"method,omitempty"`
	Receiver  string  `json:"receiver,omitempty"` // the class, for a static method
	// ThrowLines are the lines of `throw` statements: each needs a recorded failing case.
	ThrowLines []int `json:"throw_lines,omitempty"`
	// State marks a method whose body reads or writes state outside its arguments: `$this->`, `self::$x`,
	// `@ivar`. It cannot cross a bridge — the port has no instance — and pretending otherwise records
	// answers that depend on a call order nobody wrote down.
	State bool `json:"state,omitempty"`
	// Generic marks a declaration with type parameters, which a bridge cannot carry as-is.
	Generic bool `json:"generic,omitempty"`
}

// Import is one ES import, kept for the wave order.
type Import struct {
	Specifier string `json:"specifier"`
	Dynamic   bool   `json:"dynamic,omitempty"`
	Line      int    `json:"line"`
}

// Risk is one suspicious construct, with the line that raised it.
type Risk struct {
	Kind string `json:"kind"`
	Line int    `json:"line"`
	Code string `json:"code"`
}

// File is one parsed source file.
type File struct {
	Path string `json:"path"`
	Loc  int    `json:"loc"`
	// InstanceMethods counts the methods that are not exports and never will be: a bridge has no instance to
	// call them on. Counted rather than dropped, because a library whose whole surface is instance methods
	// then reads "0 exports, 6 of them need an object" instead of just "0".
	InstanceMethods int      `json:"instance_methods,omitempty"`
	Fns             []Fn     `json:"fns"`
	Imports         []Import `json:"imports"`
	Risks           []Risk   `json:"risks"`
	ParseOK         bool     `json:"parse_ok"`
}

// Lang picks the grammar.
type Lang string

const (
	TypeScript Lang = "typescript"
	TSX        Lang = "tsx"
	JavaScript Lang = "javascript"
	// Python is the second language: it needs its own grammar and its own recorder, because the original
	// has to be run by python3 rather than node. Everything after that — the driver, the bridge, gate,
	// diff, check — is language-agnostic.
	Python Lang = "python"
	Ruby   Lang = "ruby"
	Java   Lang = "java"
	// C and C++ share a grammar in this tree-sitter binding: the same walker reads both, and the compile
	// step is what differs.
	C   Lang = "c"
	Cpp Lang = "cpp"
	// The four that arrived last: each records through its own runtime (php, kotlinc, dotnet, pwsh).
	Php        Lang = "php"
	Kotlin     Lang = "kotlin"
	CSharp     Lang = "csharp"
	PowerShell Lang = "powershell"
)

// LangOf picks a grammar from a file name, and reports whether it knows the language.
func LangOf(path string) (Lang, bool) {
	switch {
	case strings.HasSuffix(path, ".tsx"), strings.HasSuffix(path, ".jsx"):
		return TSX, true
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".mts"), strings.HasSuffix(path, ".cts"):
		return TypeScript, true
	case strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".mjs"), strings.HasSuffix(path, ".cjs"):
		return JavaScript, true
	case strings.HasSuffix(path, ".py"), strings.HasSuffix(path, ".pyi"):
		return Python, true
	case strings.HasSuffix(path, ".rb"), strings.HasSuffix(path, ".rake"):
		return Ruby, true
	case strings.HasSuffix(path, ".java"):
		return Java, true
	case strings.HasSuffix(path, ".c"):
		return C, true
	case strings.HasSuffix(path, ".cc"), strings.HasSuffix(path, ".cpp"), strings.HasSuffix(path, ".cxx"),
		strings.HasSuffix(path, ".hpp"), strings.HasSuffix(path, ".hh"):
		return Cpp, true
	case strings.HasSuffix(path, ".php"):
		return Php, true
	case strings.HasSuffix(path, ".kt"), strings.HasSuffix(path, ".kts"):
		return Kotlin, true
	case strings.HasSuffix(path, ".cs"):
		return CSharp, true
	case strings.HasSuffix(path, ".ps1"), strings.HasSuffix(path, ".psm1"):
		return PowerShell, true
	}
	return "", false
}
