//go:build cgo

package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run drives the server with the frames given and returns the replies.
func run(t *testing.T, frames ...string) []map[string]any {
	t.Helper()
	var out, log bytes.Buffer
	in := strings.NewReader(strings.Join(frames, "\n") + "\n")
	if err := Serve(in, &out, &log); err != nil {
		t.Fatalf("serve: %v (log: %s)", err, log.String())
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("the server wrote a non-JSON line: %q", line)
		}
		replies = append(replies, m)
	}
	return replies
}

func TestHandshakeListsToolsAndIgnoresNotifications(t *testing.T) {
	replies := run(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	// The notification must not produce a reply: a client reads the stream positionally.
	if len(replies) != 2 {
		t.Fatalf("want 2 replies, got %d: %+v", len(replies), replies)
	}
	info := replies[0]["result"].(map[string]any)["serverInfo"].(map[string]any)
	if info["name"] != "go-anything" {
		t.Errorf("serverInfo: %+v", info)
	}
	list := replies[1]["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, entry := range list {
		tool := entry.(map[string]any)
		names = append(names, tool["name"].(string))
		// Every tool needs a schema, or an agent cannot call it.
		if tool["inputSchema"] == nil || tool["description"] == "" {
			t.Errorf("tool without schema or description: %+v", tool)
		}
	}
	for _, want := range []string{"analyze", "map", "diff", "gate", "record", "fuzz", "profile"} {
		if !contains(names, want) {
			t.Errorf("missing tool %q in %v", want, names)
		}
	}
}

func TestUnknownToolAndBadMethodAnswerWithAnError(t *testing.T) {
	replies := run(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"explode"}`,
		`not json`,
	)
	if len(replies) != 3 {
		t.Fatalf("replies: %+v", replies)
	}
	// A tool that fails answers content with isError: an agent can read why.
	res := replies[0]["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("unknown tool should say so in the content: %+v", res)
	}
	// An unknown method is a protocol error.
	if replies[1]["error"] == nil {
		t.Errorf("unknown method should be a protocol error: %+v", replies[1])
	}
	// A line that is not JSON is a parse error, not a crash.
	if replies[2]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Errorf("parse error: %+v", replies[2])
	}
}

func TestAnalyzeToolAnswersThePlan(t *testing.T) {
	repo := filepath.Join("..", "..", "testdata", "waves")
	if _, err := os.Stat(repo); err != nil {
		t.Skip("fixture moved")
	}
	abs, _ := filepath.Abs(repo)
	replies := run(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"analyze","arguments":{"repo":"`+filepath.ToSlash(abs)+`"}}}`)
	text := replies[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var plan struct {
		Units []struct {
			ID string `json:"id"`
		} `json:"units"`
	}
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("analyze did not answer JSON: %v\n%s", err, text)
	}
	if len(plan.Units) != 4 {
		t.Errorf("units: %+v", plan.Units)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
