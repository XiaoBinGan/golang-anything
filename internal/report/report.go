// Package report writes a unit's result for a pull request or a README: REPORT.md and card.svg.
//
// Every number comes from a run made for the report — `diff` for the cases, `bench` for the speedup —
// so a report cannot claim what the port does not do. The card is drawn by hand: a small SVG that reads
// at README size and carries the same three numbers.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/XiaoBinGan/golang-anything/internal/bench"
	"github.com/XiaoBinGan/golang-anything/internal/diff"
)

// Options is one report run.
type Options struct {
	CrateDir string
	Out      string
	MinMS    int
	// SkipBench keeps the report fast when a person only wants the case numbers.
	SkipBench bool
}

// Card is the report's content, and what gets written.
type Card struct {
	Unit          string      `json:"unit"`
	Crate         string      `json:"crate"`
	Cases         int         `json:"cases"`
	Matched       int         `json:"matched"`
	Throws        int         `json:"throw_paths"`
	CoveredThrows int         `json:"covered_throw_paths"`
	Blocked       []string    `json:"blocked,omitempty"`
	Speedup       float64     `json:"speedup"`
	Bench         []bench.Row `json:"bench,omitempty"`
	Bridge        string      `json:"bridge"`
	Machine       string      `json:"machine"`
	At            string      `json:"at"`
	Markdown      string      `json:"markdown_file"`
	SVG           string      `json:"svg_file"`
}

// Rate is the share of recorded cases the port answers.
func (c Card) Rate() float64 {
	if c.Cases == 0 {
		return 0
	}
	return float64(c.Matched) / float64(c.Cases) * 100
}

// Run measures the crate and writes both files.
func Run(opts Options) (Card, error) {
	dir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return Card{}, err
	}
	m, err := readManifest(dir)
	if err != nil {
		return Card{}, err
	}
	out, err := diff.Run(diff.Options{CrateDir: dir, Shown: 0})
	if err != nil {
		return Card{}, err
	}
	card := Card{
		Unit:    m.Unit,
		Crate:   m.Crate,
		Cases:   out.Cases,
		Matched: out.Matched,
		Throws:  m.Throws(),
		Blocked: m.BlockedNames(),
		Bridge:  bridgeKind(dir),
		Machine: machine(),
		At:      time.Now().Format("2006-01-02 15:04"),
	}
	// A throw path counts as proved only when a recorded case for that function raised.
	for _, e := range m.Exports {
		if len(e.ThrowLines) > 0 && out.ErrorCases[e.Name] > 0 {
			card.CoveredThrows++
		}
	}
	if !opts.SkipBench {
		res, err := bench.Run(bench.Options{CrateDir: dir, MinMS: opts.MinMS})
		if err != nil {
			return Card{}, fmt.Errorf("bench: %w", err)
		}
		card.Speedup = res.Speedup
		card.Bench = res.Rows
	}
	outDir := dir
	if opts.Out != "" {
		outDir, err = filepath.Abs(opts.Out)
		if err != nil {
			return Card{}, err
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return Card{}, err
		}
	}
	card.Markdown = filepath.Join(outDir, "REPORT.md")
	card.SVG = filepath.Join(outDir, "card.svg")
	if err := os.WriteFile(card.Markdown, []byte(card.Text()), 0o644); err != nil {
		return Card{}, err
	}
	if err := os.WriteFile(card.SVG, []byte(card.svg()), 0o644); err != nil {
		return Card{}, err
	}
	return card, nil
}

// manifest is the part of golang-anything.json a report needs.
type manifest struct {
	Unit    string `json:"unit"`
	Crate   string `json:"crate"`
	Exports []struct {
		Name       string `json:"name"`
		ThrowLines []int  `json:"throw_lines"`
		Blocked    string `json:"blocked"`
	} `json:"exports"`
}

// Throws counts the error paths the gate insists on.
func (m manifest) Throws() int {
	n := 0
	for _, e := range m.Exports {
		n += len(e.ThrowLines)
	}
	return n
}

// BlockedNames lists what stays in the original.
func (m manifest) BlockedNames() []string {
	var out []string
	for _, e := range m.Exports {
		if e.Blocked != "" {
			out = append(out, e.Name+" ("+e.Blocked+")")
		}
	}
	return out
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	raw, err := os.ReadFile(filepath.Join(dir, "golang-anything.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(raw, &m)
}

// bridgeKind says which path the wrapper takes, because the number means nothing without it.
func bridgeKind(dir string) string {
	if fileExists(filepath.Join(dir, "addon.node")) {
		return "in-process addon"
	}
	if fileExists(filepath.Join(dir, "portctl")) {
		return "stdio process"
	}
	return "none built"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func machine() string {
	return fmt.Sprintf("%s/%s, %d CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
}

// markdown is the report a pull request wants: what was proved, by which run, on which machine.
// Text is the report body, also written to Markdown (the field names the file).
func (c Card) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## `%s` ported to Go\n\n", c.Unit)
	b.WriteString("| | |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| Crate | `%s` |\n", c.Crate)
	fmt.Fprintf(&b, "| Recorded cases matched | %d / %d (%.1f%%) |\n", c.Matched, c.Cases, c.Rate())
	if len(c.Bench) > 0 {
		fmt.Fprintf(&b, "| Speedup, call included | %.2fx (geometric mean) |\n", c.Speedup)
	}
	fmt.Fprintf(&b, "| Throw paths with a recorded failing case | %d of %d |\n", c.CoveredThrows, c.Throws)
	fmt.Fprintf(&b, "| Bridge | %s |\n", c.Bridge)
	fmt.Fprintf(&b, "| Measured | %s on %s |\n", c.At, c.Machine)
	if len(c.Blocked) > 0 {
		fmt.Fprintf(&b, "| Stays in the original | %s |\n", strings.Join(c.Blocked, "; "))
	}
	if len(c.Bench) > 0 {
		b.WriteString("\n| Function | Cases | Original per call | Go per call | Speedup |\n| --- | ---: | ---: | ---: | ---: |\n")
		for _, r := range c.Bench {
			fmt.Fprintf(&b, "| `%s` | %d | %s | %s | %.2fx |\n", r.Name, r.Cases,
				ns(r.OriginalNS), ns(r.PortNS), r.Speedup)
		}
	}
	b.WriteString("\nEvery number above comes from `golang-anything diff` and `golang-anything bench` on this crate; ")
	b.WriteString("regenerate with `golang-anything report " + c.Crate + "`.\n")
	return b.String()
}

// svg draws the same three numbers as a card a README can embed.
func (c Card) svg() string {
	accent := "#b0231c"
	if c.Matched != c.Cases || c.Cases == 0 {
		accent = "#8a6d1f" // a report that does not pass says so in colour too
	}
	speed := "—"
	if len(c.Bench) > 0 {
		speed = fmt.Sprintf("%.2fx", c.Speedup)
	}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="840" height="240" viewBox="0 0 840 240" role="img" aria-label="golang-anything report">` + "\n")
	fmt.Fprintf(&b, `<rect width="840" height="240" rx="16" fill="#0f0d0b"/>`+"\n")
	fmt.Fprintf(&b, `<rect x="1" y="1" width="838" height="238" rx="15" fill="none" stroke="%s" stroke-opacity=".7"/>`+"\n", accent)
	fmt.Fprintf(&b, `<text x="40" y="72" font-family="ui-serif,Georgia,serif" font-size="30" fill="#f4ecdc">%s</text>`+"\n", esc(c.Unit))
	fmt.Fprintf(&b, `<text x="40" y="104" font-family="ui-monospace,monospace" font-size="15" fill="#9c8f7d">%s</text>`+"\n", esc(c.Crate))
	stats := []struct{ label, value string }{
		{"cases", fmt.Sprintf("%d/%d", c.Matched, c.Cases)},
		{"speedup", speed},
		{"throw paths", fmt.Sprintf("%d/%d", c.CoveredThrows, c.Throws)},
	}
	for i, s := range stats {
		x := 40 + i*256
		fmt.Fprintf(&b, `<text x="%d" y="170" font-family="ui-serif,Georgia,serif" font-size="38" fill="%s">%s</text>`+"\n", x, accent, esc(s.value))
		fmt.Fprintf(&b, `<text x="%d" y="198" font-family="ui-monospace,monospace" font-size="13" fill="#9c8f7d">%s</text>`+"\n", x, esc(s.label))
	}
	fmt.Fprintf(&b, `<text x="800" y="72" text-anchor="end" font-family="ui-monospace,monospace" font-size="13" fill="#9c8f7d">%s</text>`+"\n", esc(c.Bridge))
	fmt.Fprintf(&b, `<text x="800" y="198" text-anchor="end" font-family="ui-monospace,monospace" font-size="12" fill="#6f6155">%s</text>`+"\n", esc(c.Machine))
	b.WriteString("</svg>\n")
	return b.String()
}

// esc keeps the SVG well formed: a unit path can hold `&` or `<`.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func ns(v float64) string {
	switch {
	case v >= 1e9:
		return fmt.Sprintf("%.2f s", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2f ms", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.2f µs", v/1e3)
	}
	return fmt.Sprintf("%.0f ns", v)
}
