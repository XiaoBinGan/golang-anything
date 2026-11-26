package ts

import (
	"regexp"
	"strings"
)

// PowerShell has no grammar in the tree-sitter binding this project depends on (the module exists, but it is
// not vendored here and the network was not available to add it). Rather than claim the language and then
// panic, a .ps1 file is read by a small scanner: it finds `function Verb-Noun { param(...) ... }` and reads
// the parameters' declared types. It is not a parser, and the plan says so through the usual channels — the
// exports it finds are the ones the scanner can see.
//
// The two shapes it reads are the ones PowerShell code actually uses for a callable unit:
//
//	function Get-Thing { param([string]$Name, [int]$Limit) ... }
//	function Get-Thing([string]$Name) { ... }
var (
	psFunction = regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z][\w-]*)\s*(?:\(([^)]*)\))?\s*\{`)
	psParam    = regexp.MustCompile(`\[([\w\.\[\]]+)\]\s*\$(\w+)`)
	psThrow    = regexp.MustCompile(`(?m)\bthrow\b`)
)

// ScanPowerShell reads the functions of a PowerShell file.
func ScanPowerShell(src []byte, path string) *File {
	text := string(src)
	out := &File{Path: path, ParseOK: true}
	out.Loc = 1 + strings.Count(strings.TrimRight(text, "\n"), "\n")
	matches := psFunction.FindAllStringSubmatchIndex(text, -1)
	for i, m := range matches {
		name := text[m[2]:m[3]]
		if strings.HasPrefix(name, "_") {
			continue
		}
		start := m[1]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := text[start:end]
		fn := Fn{Name: name, Line: 1 + strings.Count(text[:m[0]], "\n"),
			EndLine: 1 + strings.Count(text[:end], "\n"), Params: []Param{}}

		// Parameters: the parenthesised list, or the param() block inside the body.
		list := ""
		if m[4] >= 0 {
			list = text[m[4]:m[5]]
		} else if at := strings.Index(body, "param"); at >= 0 {
			if open := strings.Index(body[at:], "("); open >= 0 {
				depth, j := 0, at+open
				for ; j < len(body); j++ {
					switch body[j] {
					case '(':
						depth++
					case ')':
						depth--
						if depth == 0 {
							list = body[at+open+1 : j]
							j = len(body)
						}
					}
				}
			}
		}
		for _, p := range psParam.FindAllStringSubmatch(list, -1) {
			fn.Params = append(fn.Params, Param{Name: p[2], Type: psDialect(p[1])})
		}
		for _, t := range psThrow.FindAllStringIndex(body, -1) {
			fn.ThrowLines = append(fn.ThrowLines, 1+strings.Count(text[:start+t[0]], "\n"))
		}
		out.Fns = append(out.Fns, fn)
	}
	return out
}

// psDialect narrows a PowerShell type accelerator.
func psDialect(t string) string {
	switch strings.ToLower(t) {
	case "int", "int32", "int64", "long":
		return "int"
	case "double", "float", "decimal", "single":
		return "float"
	case "bool", "boolean", "switch":
		return "boolean"
	case "string":
		return "String"
	case "char":
		return "char"
	}
	return "any"
}
