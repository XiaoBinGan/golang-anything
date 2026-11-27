// Package promote writes the Ruby shim that turns a state-free instance method into a module function.
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

func open(owner string) string  { return "# >>> go-anything promote: " + owner }
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
