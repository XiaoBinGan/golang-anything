// Package promote writes the shim that turns a state-free instance method into a callable entry point: Ruby reopens the
// class, PHP/Java/Kotlin/C# get a generated proxy in a file of its own.
//
// A method that touches no state is still not callable as `Owner.name(...)`: the bridge has no instance. The
// shim supplies one with `allocate`, which skips `initialize` — safe precisely because the method reads nothing
// the constructor would have set. A module cannot be instantiated at all, so `analyze` never offers one.
package promote

import (
	"fmt"
	"os"
	"strings"
)

func open(owner string) string   { return "# >>> go-anything promote: " + owner }
func close_(owner string) string { return "# <<< go-anything promote: " + owner }

// Shim renders the block for one owner and its methods, sorted so the block is the same on every run.
func Shim(owner string, methods []string) string {
	var b strings.Builder
	b.WriteString(open(owner) + "\n")
	b.WriteString("class " + owner + "\n")
	for _, name := range methods {
		// `*args` and no keywords: the shim must not decide how the original is called.
		fmt.Fprintf(&b, "  def self.%s(*args)\n    allocate.%s(*args)\n  end\n", name, name)
	}
	b.WriteString("end\n")
	b.WriteString(close_(owner) + "\n")
	return b.String()
}

// ShimPHP renders the PHP proxy for one owner. PHP cannot reopen a class — `class Timer` twice is a fatal error —
// so the shim is a second class whose static methods hand the call to a fresh instance. The name is the proxy's,
// which is why `analyze` then reports `TimerPromoted::double` rather than `Timer::double`.
func ShimPHP(owner string, methods []Method, original string) string {
	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString(open(owner) + "\n")
	// The proxy lives in its own file: without this the class it instantiates is undefined the moment anything
	// loads the proxy, and the recorder answers error cases for a class that exists.
	b.WriteString("require_once __DIR__ . '/" + original + "';\n\n")
	b.WriteString("class " + owner + "Promoted\n{\n")
	for _, m := range methods {
		// The method's own types, not variadic: analyze refuses a variadic method, and the point is an export.
		params := make([]string, len(m.Params))
		args := make([]string, len(m.Params))
		for i, p := range m.Params {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("arg%d", i)
			}
			if !strings.HasPrefix(name, "$") {
				name = "$" + name
			}
			if p.Type == "" || p.Type == "any" {
				params[i] = name
			} else {
				params[i] = p.Type + " " + name
			}
			args[i] = name
		}
		ret := ""
		if m.Returns != "" && m.Returns != "any" {
			ret = ": " + m.Returns
		}
		fmt.Fprintf(&b, "    public static function %s(%s)%s\n    {\n        return (new \\%s)->%s(%s);\n    }\n\n",
			m.Name, strings.Join(params, ", "), ret, owner, m.Name, strings.Join(args, ", "))
	}
	b.WriteString("}\n")
	b.WriteString(close_(owner) + "\n")
	return b.String()
}

// Method is one method a shim has to make callable, with the types the original declares.
type Method struct {
	Name    string
	Params  []Param
	Returns string
}

// Param is one parameter as the original declares it.
type Param struct {
	Name string
	Type string
}

// ShimJava renders the Java proxy for one class: `new Owner()` needs no arguments, which is the condition `analyze`
// already required before offering the method.
func ShimJava(owner string, methods []Method) string {
	var b strings.Builder
	// Java comments start with `//`: the `#` markers Ruby and PHP share would not compile.
	b.WriteString(javaOpen(owner) + "\n")
	b.WriteString("public final class " + owner + "Promoted\n{\n")
	for _, m := range methods {
		params := make([]string, len(m.Params))
		args := make([]string, len(m.Params))
		for i, p := range m.Params {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("arg%d", i)
			}
			typ := p.Type
			if typ == "" || typ == "any" {
				typ = "Object"
			}
			params[i] = typ + " " + name
			args[i] = name
		}
		ret := m.Returns
		if ret == "" {
			ret = "void"
		}
		fmt.Fprintf(&b, "    public static %s %s(%s)\n    {\n        return new %s().%s(%s);\n    }\n\n",
			ret, m.Name, strings.Join(params, ", "), owner, m.Name, strings.Join(args, ", "))
	}
	b.WriteString("}\n")
	b.WriteString(javaClose(owner) + "\n")
	return b.String()
}

// javaOpen and javaClose are the same markers with Java's comment: `#` is not a comment there, and a file that does
// not compile is not a shim.
func javaOpen(owner string) string  { return "// >>> go-anything promote: " + owner }
func javaClose(owner string) string { return "// <<< go-anything promote: " + owner }

// ShimKotlin renders a Kotlin proxy: an `object` whose members are reachable as `TimerPromoted.name(...)`, which is the
// shape `analyze` reads now. `Owner()` needs no arguments, which is the condition that produced the entry.
func ShimKotlin(owner string, methods []Method) string {
	var b strings.Builder
	b.WriteString(javaOpen(owner) + "\n")
	b.WriteString("object " + owner + "Promoted\n{\n")
	for _, m := range methods {
		params := make([]string, len(m.Params))
		args := make([]string, len(m.Params))
		for i, p := range m.Params {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("arg%d", i)
			}
			typ := ktType(p.Type)
			if typ == "" {
				typ = "Any"
			}
			params[i] = name + ": " + typ
			args[i] = name
		}
		ret := ktType(m.Returns)
		if ret == "" {
			ret = "Unit"
		}
		fmt.Fprintf(&b, "    fun %s(%s): %s = %s().%s(%s)\n\n",
			m.Name, strings.Join(params, ", "), ret, owner, m.Name, strings.Join(args, ", "))
	}
	b.WriteString("}\n")
	b.WriteString(javaClose(owner) + "\n")
	return b.String()
}

// ktType spells a recorded type as Kotlin, or "" when it cannot be written down. The widths are the JVM's: an `int` is
// 32 bits there, which the gate found the hard way on Java.
func ktType(t string) string {
	switch strings.TrimSpace(t) {
	case "int":
		return "Int"
	case "long", "int64":
		return "Long"
	case "float", "float32":
		return "Float"
	case "double", "number", "float64":
		return "Double"
	case "string", "String":
		return "String"
	case "bool", "boolean":
		return "Boolean"
	}
	return ""
}

// InsertKotlin writes the proxy into its own file, refusing to overwrite one this tool did not write.
func InsertKotlin(path, owner string, methods []Method) (bool, error) {
	block := ShimKotlin(owner, methods)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return true, os.WriteFile(path, []byte(block), 0o644)
	}
	if strings.Contains(string(raw), javaOpen(owner)) {
		return false, nil
	}
	return false, fmt.Errorf("%s already exists and this tool did not write it", path)
}

// ShimCSharp renders a C# proxy: a static class whose methods forward to `new Owner()`. C# has no way to add a static
// member to a class from outside either, and the class it instantiates is a sibling file in the same project.
func ShimCSharp(owner string, methods []Method) string {
	var b strings.Builder
	b.WriteString(javaOpen(owner) + "\n")
	b.WriteString("public static class " + owner + "Promoted\n{\n")
	for _, m := range methods {
		params := make([]string, len(m.Params))
		args := make([]string, len(m.Params))
		for i, p := range m.Params {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("arg%d", i)
			}
			typ := p.Type
			if typ == "" || typ == "any" {
				typ = "object"
			}
			params[i] = typ + " " + name
			args[i] = name
		}
		ret := m.Returns
		if ret == "" {
			ret = "void"
		}
		fmt.Fprintf(&b, "    public static %s %s(%s)\n    {\n        return new %s().%s(%s);\n    }\n\n",
			ret, m.Name, strings.Join(params, ", "), owner, m.Name, strings.Join(args, ", "))
	}
	b.WriteString("}\n")
	b.WriteString(javaClose(owner) + "\n")
	return b.String()
}

// InsertCSharp writes the proxy into its own file, refusing to overwrite one this tool did not write.
func InsertCSharp(path, owner string, methods []Method) (bool, error) {
	block := ShimCSharp(owner, methods)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return true, os.WriteFile(path, []byte(block), 0o644)
	}
	if strings.Contains(string(raw), javaOpen(owner)) {
		return false, nil
	}
	return false, fmt.Errorf("%s already exists and this tool did not write it", path)
}

// InsertJava writes the proxy into its own file. It refuses to overwrite a file it did not write: a user's
// `MathsPromoted.java` is theirs.
func InsertJava(path, owner string, methods []Method) (bool, error) {
	block := ShimJava(owner, methods)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return true, os.WriteFile(path, []byte(block), 0o644)
	}
	if strings.Contains(string(raw), javaOpen(owner)) {
		return false, nil
	}
	return false, fmt.Errorf("%s already exists and this tool did not write it", path)
}

// Insert appends the block to the file and reports whether anything was written.
func Insert(path, owner string, methods []string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := string(raw)
	if strings.Contains(text, open(owner)) {
		return false, nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.WriteFile(path, []byte(text+"\n"+Shim(owner, methods)), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// InsertPHP writes the proxy class into the file, creating it when it is not there yet. A PHP file that already
// has a `<?php` header gets the block appended; a new one starts with the header.
func InsertPHP(path, owner string, methods []Method, original string) (bool, error) {
	block := ShimPHP(owner, methods, original)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return true, os.WriteFile(path, []byte(block), 0o644)
	}
	text := string(raw)
	if strings.Contains(text, open(owner)) {
		return false, nil
	}
	if strings.HasPrefix(text, "<?php") {
		block = strings.TrimPrefix(block, "<?php\n")
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
	}
	return true, os.WriteFile(path, []byte(text+"\n"+block), 0o644)
}

// RevertJava removes a Java proxy block. The caller deletes the file when nothing but the block was in it.
func RevertJava(path, owner string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := string(raw)
	i := strings.Index(text, javaOpen(owner))
	if i < 0 {
		return false, nil
	}
	j := strings.Index(text[i:], javaClose(owner)+"\n")
	if j < 0 {
		return false, fmt.Errorf("%s has the opening marker for %s but not the closing one: the block was edited "+
			"by hand, so nothing was removed", path, owner)
	}
	end := i + j + len(javaClose(owner)) + 1
	if err := os.WriteFile(path, []byte(text[:i]+text[end:]), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Revert removes the block again, and refuses a half-removed one: a shim that leaves the file changed is a
// shim nobody can undo.
func Revert(path, owner string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := string(raw)
	i := strings.Index(text, open(owner))
	if i < 0 {
		return false, nil
	}
	j := strings.Index(text[i:], close_(owner)+"\n")
	if j < 0 {
		return false, fmt.Errorf("%s has the opening marker for %s but not the closing one: the block was edited "+
			"by hand, so nothing was removed", path, owner)
	}
	end := i + j + len(close_(owner)) + 1
	// The blank line Insert adds belongs to the block.
	if i > 0 && text[i-1] == '\n' {
		i--
	}
	if err := os.WriteFile(path, []byte(text[:i]+text[end:]), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
