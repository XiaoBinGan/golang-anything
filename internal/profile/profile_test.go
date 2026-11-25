package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSummariseKeepsTheRepositorysOwnFrames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("paths in the fixture are POSIX")
	}
	repo := t.TempDir()
	// The profiler reports `<repo>/src/a.ts` and `node:fs`; only the first is ours.
	prof := cpuProfile{}
	add := func(name, url string, hits int) {
		prof.Nodes = append(prof.Nodes, struct {
			ID        int `json:"id"`
			CallFrame struct {
				FunctionName string `json:"functionName"`
				URL          string `json:"url"`
				LineNumber   int    `json:"lineNumber"`
			} `json:"callFrame"`
			HitCount int `json:"hitCount"`
		}{ID: len(prof.Nodes) + 1})
		n := &prof.Nodes[len(prof.Nodes)-1]
		n.CallFrame.FunctionName = name
		n.CallFrame.URL = url
		n.CallFrame.LineNumber = 4
		for i := 0; i < hits; i++ {
			prof.Samples = append(prof.Samples, n.ID)
		}
	}
	add("hot", filepath.Join(repo, "src", "a.ts"), 6)
	add("realpathSync", "node:fs", 3)
	add("", filepath.Join(repo, "src", "b.ts"), 1)

	hot, samples := summarise(prof, repo)
	if samples != 10 {
		t.Fatalf("samples: %d", samples)
	}
	var names []string
	for _, h := range hot {
		names = append(names, h.Function)
	}
	if len(hot) != 2 {
		t.Fatalf("runtime frames should be dropped: %+v", hot)
	}
	// The hottest comes first, and its share is samples over total.
	if hot[0].Function != "hot" || hot[0].File != "src/a.ts" || hot[0].Share < 59 || hot[0].Share > 61 {
		t.Errorf("first row: %+v", hot[0])
	}
	// An anonymous frame is the module's top level: it has no name to port, but it is still our code.
	if hot[1].Function != "(top level)" {
		t.Errorf("top level: %+v", hot[1])
	}
	_ = os.RemoveAll(repo)
}

func TestRunNeedsAWorkloadAndSaysSoWhenNothingRan(t *testing.T) {
	if _, err := Run(Options{Repo: t.TempDir()}); err == nil {
		t.Error("no workload must be refused")
	}
	repo := t.TempDir()
	// A workload that never touches the repository: the run must say that instead of inventing rows.
	script := filepath.Join(repo, "noop.mjs")
	if err := os.WriteFile(script, []byte("console.log('nothing')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(Options{Repo: repo, Command: []string{"noop.mjs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Hot) != 0 {
		t.Errorf("nothing of ours ran, yet rows appeared: %+v", rep.Hot)
	}
	if !contains(rep.Markdown(), "never reached its own code") {
		t.Errorf("the empty report should say why:\n%s", rep.Markdown())
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		(len(haystack) > 0 && indexOf(haystack, needle) >= 0))
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
