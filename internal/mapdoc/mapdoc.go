// Package mapdoc writes the port map: every unit of the original, the crate that ports it, its cases
// and the last diff, the whole app's glue and what is still left.
package mapdoc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/analyze"
	"github.com/XiaoBinGan/golang-anything/internal/diff"
)

// Crate is one port this tool made, as its manifest describes it.
type Crate struct {
	Dir     string
	Unit    string
	Cases   int
	Errors  int
	Throws  int
	Blocked []string
	Match   int
	Total   int
	Built   bool
}

// Run writes PORT-MAP.md into the repository root and returns the text.
func Run(root string) (string, error) {
	rep, err := analyze.Run(root)
	if err != nil {
		return "", err
	}
	crates, err := cratesUnder(root)
	if err != nil {
		return "", err
	}
	byUnit := map[string]Crate{}
	for _, c := range crates {
		byUnit[c.Unit] = c
	}
	var b strings.Builder
	name := filepath.Base(root)
	fmt.Fprintf(&b, "# Port map: %s\n\n", name)
	fmt.Fprintf(&b, "%d of %d units have a Go port.\n\n", len(byUnit), len(rep.Units))
	b.WriteString("| Unit | Wave | Status | Exports | Crate | Cases | Last diff | Built |\n")
	b.WriteString("| --- | ---: | --- | ---: | --- | ---: | --- | --- |\n")
	for _, u := range rep.Units {
		c, ported := byUnit[u.ID]
		crate := "—"
		cases := "—"
		last := "not run"
		built := "—"
		if ported {
			crate = "`" + filepath.ToSlash(relTo(root, c.Dir)) + "`"
			cases = fmt.Sprint(c.Cases)
			if c.Total > 0 {
				last = fmt.Sprintf("%d/%d", c.Match, c.Total)
			}
			built = "no"
			if c.Built {
				built = "yes"
			}
		}
		fmt.Fprintf(&b, "| `%s` | %d | %s | %d | %s | %s | %s | %s |\n",
			u.ID, u.Wave, u.Status, len(u.Exports), crate, cases, last, built)
	}
	if len(byUnit) < len(rep.Units) {
		b.WriteString("\n## Not ported yet\n\nIn the order `analyze` suggests.\n\n")
		b.WriteString("| Unit | Status | Lines | Why it stays |\n| --- | --- | ---: | --- |\n")
		for _, u := range rep.Units {
			if _, ok := byUnit[u.ID]; ok {
				continue
			}
			why := strings.Join(u.Notes, "; ")
			if why == "" {
				why = "—"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %d | %s |\n", u.ID, u.Status, u.Loc, why)
		}
	}
	text := b.String()
	if err := os.WriteFile(filepath.Join(root, "PORT-MAP.md"), []byte(text), 0o644); err != nil {
		return "", err
	}
	return text, nil
}

// cratesUnder finds the manifests this tool wrote, anywhere under the root.
func cratesUnder(root string) ([]Crate, error) {
	var out []Crate
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "work":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "golang-anything.json" {
			return nil
		}
		dir := filepath.Dir(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m struct {
			Unit    string `json:"unit"`
			Exports []struct {
				ThrowLines []int  `json:"throw_lines"`
				Blocked    string `json:"blocked"`
			} `json:"exports"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil // a manifest this tool cannot read is not a crash
		}
		c := Crate{Dir: dir, Unit: m.Unit}
		for _, e := range m.Exports {
			c.Throws += len(e.ThrowLines)
			if e.Blocked != "" {
				c.Blocked = append(c.Blocked, e.Blocked)
			}
		}
		c.Cases, c.Errors = countCases(filepath.Join(dir, "cases.jsonl"))
		c.Match, c.Total = lastDiff(dir, filepath.Join(dir, "cases.jsonl"), c.Cases)
		if info, err := os.Stat(filepath.Join(dir, "portctl")); err == nil && info.Mode().IsRegular() {
			c.Built = true
		}
		out = append(out, c)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Unit < out[j].Unit })
	return out, err
}

func countCases(path string) (total, errors int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var c struct {
			Err string `json:"error"`
		}
		if err := dec.Decode(&c); err != nil {
			return total, errors
		}
		total++
		if c.Err != "" {
			errors++
		}
	}
}

// lastDiff counts the answers a previous `diff` left behind. It runs the comparison again rather than
// trusting a number: a stale answers file must not read as a passing port.
func lastDiff(dir, casesPath string, total int) (matched, cases int) {
	if total == 0 {
		return 0, 0
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "answers.jsonl")); err != nil {
		return 0, 0
	}
	out, err := diff.Run(diff.Options{CrateDir: dir, Shown: 0})
	if err != nil {
		return 0, total
	}
	return out.Matched, out.Cases
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
