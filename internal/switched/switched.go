// Package switched inserts and removes the block that makes the original call the Go port.
//
// The point is not to rewrite the program: one marked block at the end of a file rebinds the exported
// functions to the port, `disable` takes exactly that block out again, and the file comes back byte for
// byte. This is the step that turns a passing port into a used one, so the tests are about identity of
// the original, not only about the block.
package switched

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoBinGan/golang-anything/internal/fingerprint"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mode is how the port is called.
type Mode string

const (
	// Sync uses the in-process addon: the original keeps its synchronous signature.
	Sync Mode = "sync"
	// Async awaits the stdio bridge: works without the addon, breaks callers that need a value now.
	Async Mode = "async"
)

// Options is one enable run.
type Options struct {
	CrateDir string
	Unit     string
	Crate    string
	Exports  []string
	Mode     Mode
	// Only limits the switch to these exports; empty means every export the bridge carries.
	Only []string
	// Owners say, for each export, which module or class defines it: a Ruby module function is reached
	// through its owner, a top-level def through Object. Other languages ignore it.
	Owners []Owned
}

// Owned is one export and the module or class that defines it.
type Owned struct {
	Name  string
	Owner string
}

// takeover names the block a unit gets, by the language its extension says it is written in. An
// extension with no block is refused rather than given the JavaScript one: `render` would otherwise be
// right about the names and wrong about the file, and nothing downstream would notice until a compiler
// did.
func takeover(unit string) (string, bool) {
	switch strings.ToLower(filepath.Ext(unit)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "javascript", true
	case ".rb":
		return "ruby", true
	}
	return "", false
}

// markOpen is the first line of the block: a reader and `disable` both find it by this prefix.
func markOpen(crate string) string {
	return "// >>> golang-anything: " + crate + " is on. `golang-anything disable` removes this block."
}

func markClose(crate string) string { return "// <<< golang-anything: " + crate }

// Enable inserts the block into the unit's file and returns the file it changed.
func Enable(opts Options) (string, error) {
	names := opts.Exports
	if len(opts.Only) > 0 {
		known := map[string]bool{}
		for _, n := range names {
			known[n] = true
		}
		var chosen []string
		for _, n := range opts.Only {
			if !known[n] {
				return "", fmt.Errorf("unknown export %q; the bridge carries %s", n,
					strings.Join(names, ", "))
			}
			chosen = append(chosen, n)
		}
		names = chosen
	}
	if len(names) == 0 {
		return "", fmt.Errorf("nothing to switch on: the crate has no bridgeable export")
	}
	// The block is written in the unit's own language, so a language with no block has no takeover. Each
	// language is a different way of rebinding the same names, and the missing one used to be silent: the
	// JavaScript block went into a .rs file and the run reported success on a file that no longer compiles.
	kind, ok := takeover(opts.Unit)
	if !ok {
		return "", fmt.Errorf("enable has no takeover for %s: the block has to be written in the unit's own "+
			"language, and only TypeScript/JavaScript (.ts .tsx .js .jsx .mjs .cjs) and Ruby (.rb) have one",
			opts.Unit)
	}
	// A switched call has to land somewhere: without the wrapper or the binaries the original would
	// fail at its first call, and that is worse than not switching it on.
	// What a switched call needs depends on where the block lands: a JavaScript unit talks to the addon,
	// a Ruby unit talks to the stdio binary through port.rb.
	needed := []string{"index.cjs", "addon.node"}
	how := "golang-anything build --fast"
	if kind == "ruby" {
		needed = []string{"port.rb", "portctl"}
		how = "golang-anything build"
	}
	for _, file := range needed {
		if _, err := os.Stat(filepath.Join(opts.CrateDir, file)); err != nil {
			return "", fmt.Errorf("run `golang-anything bridge %s` and `%s %s` first: %s is missing",
				opts.CrateDir, how, opts.CrateDir, file)
		}
	}
	root, err := planRoot(opts.CrateDir, opts.Unit)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, opts.Unit)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	crateDir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return "", err
	}
	text := string(raw)
	name := filepath.Base(crateDir)
	if strings.Contains(text, markOpen(name)) || strings.Contains(text, "# >>> golang-anything: "+name) {
		// Already on: enabling twice must not add a second block.
		return path, nil
	}
	// The block lives in the unit's file, so the module path is relative to that file's folder.
	rel, err := filepath.Rel(filepath.Dir(path), crateDir)
	if err != nil {
		return "", err
	}
	block := render(opts, names, filepath.ToSlash(rel), text, filepath.Base(crateDir))
	on := insert(text, block)
	// `export const f = () => …` cannot be reassigned: the binding has to become mutable first, and the
	// block records which declarations it changed so `disable` can put them back.
	on, changed := loosenConsts(on, names)
	if err := os.WriteFile(path, []byte(on), 0o644); err != nil {
		return "", err
	}
	if len(changed) > 0 {
		if err := writeLoosened(crateDir, path, changed); err != nil {
			return "", err
		}
	}
	return path, nil
}

// Disable removes the block, and reports whether there was one.
func Disable(crateDir, unit, crate string) (string, bool, error) {
	crateName := filepath.Base(crateDir)
	root, err := planRoot(crateDir, unit)
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(root, unit)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	off, ok, truncated := removeChecked(string(raw), crateName)
	if truncated {
		return "", false, fmt.Errorf("%s has the opening marker for %s but not the closing one: "+
			"the block was edited by hand, so nothing was removed", path, crateName)
	}
	if !ok {
		return path, false, nil
	}
	// Put back the `const` the block had to loosen, so the file is what it was before.
	off = tightenConsts(off, readLoosened(crateDir, path))
	if err := os.WriteFile(path, []byte(off), 0o644); err != nil {
		return "", false, err
	}
	clearLoosened(crateDir, path)
	return path, true, nil
}

// IsOn reports whether the block is present.
func IsOn(crateDir, unit, crate string) (bool, error) {
	crateName := filepath.Base(crateDir)
	root, err := planRoot(crateDir, unit)
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(filepath.Join(root, unit))
	if err != nil {
		return false, err
	}
	text := string(raw)
	return strings.Contains(text, markOpen(crateName)) || strings.Contains(text, "# >>> golang-anything: "+crateName), nil
}

// render builds the block. Everything it needs comes from the caller, so the block is the same on every
// run: a stable block is one `disable` can find and a diff can ignore.
func render(opts Options, names []string, rel string, original string, name string) string {
	if kind, _ := takeover(opts.Unit); kind == "ruby" {
		return renderRuby(opts, names, rel, name)
	}
	var b strings.Builder
	// The marker names the crate folder, not its import path: short enough to read in a diff.
	b.WriteString(markOpen(name) + "\n")
	module := rel + "/index.cjs"
	if !strings.HasPrefix(rel, ".") {
		module = "./" + module
	}
	esm := strings.Contains(original, "export ") || strings.Contains(original, "import ")
	if esm {
		b.WriteString("import { createRequire as __goAnythingRequire } from 'node:module'\n")
		fmt.Fprintf(&b, "const __goAnything = __goAnythingRequire(import.meta.url)(%q)\n", module)
	} else {
		fmt.Fprintf(&b, "const __goAnything = require(%q)\n", module)
	}
	if opts.Mode == Async {
		b.WriteString("// async mode: every switched function now returns a promise\n")
	} else {
		b.WriteString("// sync mode: the in-process addon keeps the original's signature\n")
	}
	sort.Strings(names)
	for _, name := range names {
		if opts.Mode == Async {
			fmt.Fprintf(&b, "%s = (...args) => __goAnything.%s(...args)\n", name, name)
		} else {
			fmt.Fprintf(&b, "%s = (...args) => __goAnything.callSync(%q, ...args)\n", name, name)
		}
	}
	b.WriteString(markClose(name) + "\n")
	return b.String()
}

// insert puts the block at the end of the file, after a newline, so the declarations above it exist.
func insert(text, block string) string {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + block
}

// remove takes the block out and returns the text with the same bytes it had before `insert`.
func remove(text, crate string) (string, bool) {
	out, ok, _ := removeChecked(text, crate)
	return out, ok
}

// removeChecked also reports a half-removed block: an opening marker without its closing one means the
// file was edited by hand, and saying "nothing to remove" would send a person looking in the wrong place.
func removeChecked(text, crate string) (string, bool, bool) {
	open := markOpen(crate)
	close := markClose(crate)
	if strings.Contains(text, "# >>> golang-anything: "+crate) {
		// The Ruby block says the same thing in that language's comment.
		open = "# >>> golang-anything: " + crate
		close = "# <<< golang-anything: " + crate
	}
	start := strings.Index(text, open)
	if start < 0 {
		return text, false, false
	}
	end := strings.Index(text[start:], close)
	if end < 0 {
		return text, false, true
	}
	end = start + end + len(close)
	// Take the trailing newline with it, or every disable leaves one more blank line behind.
	if end < len(text) && text[end] == '\n' {
		end++
	}
	return text[:start] + text[end:], true, false
}

// planRoot walks up from the crate until the unit's file is under a parent.
func planRoot(crateDir, rel string) (string, error) {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no folder above %s holds %s", crateDir, rel)
		}
		dir = parent
	}
}

// ExportsOf lists what a manifest can switch on, so the CLI and the tests agree.
func ExportsOf(crateDir string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(crateDir, "golang-anything.json"))
	if err != nil {
		return nil, err
	}
	var m struct {
		Exports []struct {
			Name    string `json:"name"`
			Blocked string `json:"blocked"`
		} `json:"exports"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range m.Exports {
		if e.Blocked == "" {
			out = append(out, e.Name)
		}
	}
	return out, nil
}

// UnitOf reads the unit a manifest names.
func UnitOf(crateDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(crateDir, "golang-anything.json"))
	if err != nil {
		return "", err
	}
	var m struct {
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	return m.Unit, nil
}

// loosenConsts turns `export const <name> =` into `export let <name> =` for the exports a block
// reassigns, and answers which ones it changed. A function declaration is already assignable.
func loosenConsts(text string, names []string) (string, []string) {
	var changed []string
	for _, name := range names {
		for _, prefix := range []string{"export const " + name + " ", "export const " + name + "="} {
			at := strings.Index(text, prefix)
			if at < 0 {
				continue
			}
			// Only the declaration, not a mention of the name in a comment or a string.
			lineStart := strings.LastIndexByte(text[:at], '\n') + 1
			if strings.TrimSpace(text[lineStart:at]) != "" {
				continue
			}
			text = text[:at] + "export let " + text[at+len("export const "):]
			changed = append(changed, name)
			break
		}
	}
	return text, changed
}

// tightenConsts is the reverse: `export let <name> =` back to `export const <name> =`.
func tightenConsts(text string, names []string) string {
	for _, name := range names {
		text = strings.Replace(text, "export let "+name+" ", "export const "+name+" ", 1)
	}
	return text
}

// loosenedPath is where the record of changed declarations lives: next to the crate, not in the source.
func loosenedPath(crateDir, unitPath string) string {
	sum := fingerprint.Of([]byte(unitPath))
	return filepath.Join(crateDir, "work", "enabled-"+sum[:6]+".json")
}

func writeLoosened(crateDir, unitPath string, names []string) error {
	path := loosenedPath(crateDir, unitPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func readLoosened(crateDir, unitPath string) []string {
	body, err := os.ReadFile(loosenedPath(crateDir, unitPath))
	if err != nil {
		return nil
	}
	var names []string
	if json.Unmarshal(body, &names) != nil {
		return nil
	}
	return names
}

func clearLoosened(crateDir, unitPath string) { os.Remove(loosenedPath(crateDir, unitPath)) }

// renderRuby writes the block for a Ruby unit: require the wrapper, reopen the owner, redefine each function.
//
// Ruby lets a module or a class be reopened and a method redefined, so the block can live at the end of the
// file and `disable` can lift it out byte for byte. A top-level `def` is a private method of Object, which is
// reopened the same way.
func renderRuby(opts Options, names []string, rel, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# >>> golang-anything: %s is on. `golang-anything disable` removes this block.\n", name)
	b.WriteString("# The original below is unchanged; every call now goes to the Go port over the stdio bridge.\n")
	fmt.Fprintf(&b, "require_relative %q\n", rel+"/port")
	owners := map[string][]string{}
	for _, export := range opts.Owners {
		owners[export.Owner] = append(owners[export.Owner], export.Name)
	}
	if len(owners) == 0 {
		owners[""] = names
	}
	for _, owner := range sortedKeys(owners) {
		if owner == "" {
			b.WriteString("class Object\n")
		} else {
			fmt.Fprintf(&b, "module %s\n  class << self\n", owner)
		}
		for _, fn := range owners[owner] {
			if owner == "" {
				fmt.Fprintf(&b, "  def %s(*args)\n    GoPort.call(%q, *args)\n  end\n  private :%s\n", fn, fn, fn)
			} else {
				fmt.Fprintf(&b, "    def %s(*args)\n      GoPort.call(%q, *args)\n    end\n", fn, fn)
			}
		}
		if owner == "" {
			b.WriteString("end\n")
		} else {
			b.WriteString("  end\nend\n")
		}
	}
	fmt.Fprintf(&b, "# <<< golang-anything: %s\n", name)
	return b.String()
}

// sortedKeys answers a map's keys in order, so the block is stable across runs.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
