// Package mcp is the Model Context Protocol server: JSON-RPC over stdio, the same shape every coding
// agent speaks.
//
// The tools are the commands that answer a question or change a crate — analyze, map, diff, gate,
// record, profile — with JSON in and JSON out, so an agent can plan a port without shelling out and
// parsing a person-facing table. Every tool also writes something a person can open: the plan is
// `PORT-MAP.md`, the verdict is the gate's own output.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/XiaoBinGan/golang-anything/internal/analyze"
	"github.com/XiaoBinGan/golang-anything/internal/diff"
	"github.com/XiaoBinGan/golang-anything/internal/fuzz"
	"github.com/XiaoBinGan/golang-anything/internal/mapdoc"
	"github.com/XiaoBinGan/golang-anything/internal/profile"
	"github.com/XiaoBinGan/golang-anything/internal/record"
)

// Version is what the server reports at initialize.
const Version = "0.5.0"

// request is one JSON-RPC call.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is the reply; Result and Error are mutually exclusive.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// tool is one exposed command.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// tools is the list an agent reads at startup. Descriptions say what the tool does and what it needs,
// because an agent picks by the description.
func tools() []tool {
	return []tool{
		{
			Name: "analyze",
			Description: "Map a repository into units a Go port could take: waves, readiness, priority, " +
				"risks with file:line, and the exports a bridge can carry. Answers JSON; the plan is the " +
				"same one the CLI prints.",
			InputSchema: obj(map[string]any{
				"repo": str("repository root"),
			}, "repo"),
		},
		{
			Name: "map",
			Description: "Write PORT-MAP.md for a repository: every unit, the crate that ports it, its " +
				"case count, its last diff and whether it is built. Answers the markdown it wrote.",
			InputSchema: obj(map[string]any{
				"repo": str("repository root"),
			}, "repo"),
		},
		{
			Name: "diff",
			Description: "Replay the recorded cases against the port and answer matched/total, the " +
				"mismatches and the exports still missing. Read-only.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
				"shown": num("mismatches to return (default 10)"),
			}, "crate"),
		},
		{
			Name: "gate",
			Description: "The definition of done: every case matches, every export is in the port, and " +
				"every `throw` has a recorded failing case. Answers a verdict with the reasons that failed.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name: "record",
			Description: "Run the original on generated inputs and store what it answered as cases.jsonl. " +
				"Types come from the parse, so a string parameter only gets strings.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
				"unit":  str("unit id from analyze"),
				"count": num("cases per function (default 200)"),
			}, "crate", "unit"),
		},
		{
			Name: "fuzz",
			Description: "Throw fresh inputs from a seed at the original and the port and answer where " +
				"they differ. `keep` appends what failed to cases.jsonl so gate checks it from then on.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
				"count": num("fresh calls per function (default 50)"),
				"seed":  num("seed to replay a run; random when left out"),
				"keep":  boolean("append the failing calls to cases.jsonl"),
			}, "crate"),
		},
		{
			Name: "doctor",
			Description: "Check the machine: the toolchain, the parser, node, and whether a Go program " +
				"builds. Answers every check with what it found, not a verdict.",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name: "check",
			Description: "Replay the recorded cases through the wrapper a caller actually uses (the " +
				"in-process addon, or the pipe). The driver answering is `gate`; this is the other path.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name: "bench",
			Description: "Time the port against the original on the recorded cases, per function and as " +
				"a geometric mean.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name:        "shadow-report",
			Description: "Read the differences shadow.cjs logged while the original ran in production.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name: "enable",
			Description: "Point the original at the port: writes the block that makes the unit call the " +
				"Go port instead of itself. `disable` puts the file back byte for byte.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name:        "disable",
			Description: "Remove the block enable wrote, restoring the original file exactly.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name:        "clean",
			Description: "Remove what the tool generated in a crate folder, leaving port.go and cases.jsonl.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name: "refresh",
			Description: "Regenerate the binding for a crate whose port.go was edited: the driver, the " +
				"wrapper and the answers are rewritten, the work is kept.",
			InputSchema: obj(map[string]any{
				"crate": str("crate folder made by scaffold"),
			}, "crate"),
		},
		{
			Name: "profile",
			Description: "Run a workload under node --cpu-prof and answer the hottest functions by self " +
				"time, each mapped to the unit it belongs to. Says what is worth porting first.",
			InputSchema: obj(map[string]any{
				"repo":    str("repository root"),
				"command": str("the workload, e.g. test/index.mjs or -e <script>"),
				"top":     num("rows to return (default 12)"),
			}, "repo", "command"),
		},
	}
}

func obj(props map[string]any, required ...string) map[string]any {
	// A nil slice marshals to null, and a client that validates the schema rejects "required": null — the
	// official SDK did, for the one tool that takes no arguments.
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func num(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func boolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

// Serve answers JSON-RPC on in and out until in ends. It never writes anything but protocol frames on
// out: a log line there would break the client.
func Serve(in io.Reader, out io.Writer, log io.Writer) error {
	// Read lines with a Reader, not a Scanner: a Scanner answers "token too long" by stopping, and a
	// client that sends one big argument would take the whole server down instead of getting an error
	// for that one request.
	reader := bufio.NewReaderSize(in, 1<<20)
	enc := json.NewEncoder(out)
	const maxLine = 64 << 20
	for {
		line, err := readLine(reader, maxLine)
		if len(line) > 0 {
			if reply, ok := answerLine(line, enc, log); !ok {
				return reply
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if err == errTooLong {
				// The line was dropped; the stream continues with the next one.
				tooLong := response{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "request line longer than 64 MB"}}
				if encErr := enc.Encode(tooLong); encErr != nil {
					return encErr
				}
				continue
			}
			return err
		}
	}
}

var errTooLong = errors.New("line too long")

// readLine reads up to a newline, and reports errTooLong after dropping the rest of an oversized line so
// the next request still arrives.
func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(buf)+len(chunk) > limit {
			for err == bufio.ErrBufferFull {
				if _, err = r.ReadSlice('\n'); err != nil && err != bufio.ErrBufferFull {
					break
				}
			}
			return nil, errTooLong
		}
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), err
	}
}

// answerLine handles one request. A returned error stops the server; ok=false means that happened.
func answerLine(line []byte, enc *json.Encoder, log io.Writer) (error, bool) {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, true
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		if err := enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
			return err, false
		}
		return nil, true
	}
	reply, ok := handle(req, log)
	if !ok {
		return nil, true // a notification: no answer expected
	}
	if err := enc.Encode(reply); err != nil {
		return err, false
	}
	return nil, true
}

// handle answers one request; ok is false for notifications.
func handle(req request, log io.Writer) (response, bool) {
	reply := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		reply.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "golang-anything", "version": Version},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}
	case "notifications/initialized", "initialized":
		return response{}, false
	case "ping":
		reply.Result = map[string]any{}
	case "tools/list":
		reply.Result = map[string]any{"tools": tools()}
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			reply.Error = &rpcError{Code: -32602, Message: "tools/call needs name and arguments"}
			break
		}
		result, err := callTool(call.Name, call.Arguments, log)
		if err != nil {
			// A tool that fails answers with content, not a protocol error: the agent can read why.
			reply.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}
			break
		}
		reply.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": result}}}
	default:
		if len(req.ID) == 0 {
			return response{}, false
		}
		reply.Error = &rpcError{Code: -32601, Message: "unknown method " + req.Method}
	}
	return reply, true
}

// callTool runs one tool and answers text: JSON for a plan, a verdict sentence for the gate.
// commands holds the tools the CLI implements. main fills it, so the tool and the command are the same
// code: `golang-anything check` and tools/call "check" cannot drift apart.
var commands = map[string]func(map[string]any) (string, error){}

// SetCommands installs the delegated tools.
func SetCommands(m map[string]func(map[string]any) (string, error)) { commands = m }

func callTool(name string, raw json.RawMessage, log io.Writer) (string, error) {
	var args map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("arguments must be an object: %w", err)
		}
	}
	get := func(key string) string {
		if v, ok := args[key].(string); ok {
			return v
		}
		return ""
	}
	getNum := func(key string, fallback int) int {
		if v, ok := args[key].(float64); ok {
			return int(v)
		}
		return fallback
	}
	switch name {
	case "analyze":
		repo := get("repo")
		if repo == "" {
			return "", fmt.Errorf("analyze needs `repo`")
		}
		rep, err := analyze.Run(repo)
		if err != nil {
			return "", err
		}
		return jsonText(rep)
	case "map":
		repo := get("repo")
		if repo == "" {
			return "", fmt.Errorf("map needs `repo`")
		}
		text, err := mapdoc.Run(repo)
		if err != nil {
			return "", err
		}
		return text, nil
	case "diff":
		crate := get("crate")
		if crate == "" {
			return "", fmt.Errorf("diff needs `crate`")
		}
		out, err := diff.Run(diff.Options{CrateDir: crate, Shown: getNum("shown", 10)})
		if err != nil {
			return "", err
		}
		return jsonText(map[string]any{
			"cases": out.Cases, "matched": out.Matched, "passed": out.Passed(),
			"missing": out.Missing, "mismatches": out.Mismatch,
		})
	case "gate":
		crate := get("crate")
		if crate == "" {
			return "", fmt.Errorf("gate needs `crate`")
		}
		out, err := diff.Run(diff.Options{CrateDir: crate, Shown: 5})
		if err != nil {
			return "", err
		}
		throws, blocked, err := manifestChecks(crate)
		if err != nil {
			return "", err
		}
		checks := diff.Check(out, throws, blocked)
		reasons := checks.Failed(out.Matched, out.Cases)
		return jsonText(map[string]any{
			"cases": out.Cases, "matched": out.Matched, "passed": len(reasons) == 0 && out.Cases > 0,
			"reasons": reasons, "blocked": blocked,
		})
	case "record":
		crate, unit := get("crate"), get("unit")
		if crate == "" || unit == "" {
			return "", fmt.Errorf("record needs `crate` and `unit`")
		}
		u, err := unitOf(crate, unit)
		if err != nil {
			return "", err
		}
		count := getNum("count", 200)
		sum, err := recordCases(crate, u, count)
		if err != nil {
			return "", err
		}
		return jsonText(sum)
	case "fuzz":
		crate := get("crate")
		if crate == "" {
			return "", fmt.Errorf("fuzz needs `crate`")
		}
		keep, _ := args["keep"].(bool)
		res, err := fuzz.Run(fuzz.Options{
			CrateDir: crate, Count: getNum("count", 50), Seed: int64(getNum("seed", 0)), Keep: keep,
		})
		if err != nil {
			return "", err
		}
		return jsonText(map[string]any{
			"seed": res.Seed, "calls": res.Calls, "matched": res.Matched,
			"mismatches": res.Mismatch, "kept": res.Kept,
		})
	case "profile":
		repo, command := get("repo"), get("command")
		if repo == "" || command == "" {
			return "", fmt.Errorf("profile needs `repo` and `command`")
		}
		rep, err := profile.Run(profile.Options{
			Repo: repo, Command: strings.Fields(command), Top: getNum("top", 12),
		})
		if err != nil {
			return "", err
		}
		return jsonText(rep)
	}
	// A tool the CLI already implements runs through it: the two front ends share one implementation, so a
	// fix in `check` cannot land in the command and miss the tool.
	if run, ok := commands[name]; ok {
		out, err := run(args)
		if err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// unitOf finds the unit a crate ports, by id, through the plan of the repository above it.
func unitOf(crateDir, id string) (analyze.Unit, error) {
	rep, err := analyze.Run(planRoot(crateDir, id))
	if err != nil {
		return analyze.Unit{}, err
	}
	for _, u := range rep.Units {
		if u.ID == id {
			return u, nil
		}
	}
	return analyze.Unit{}, fmt.Errorf("no unit %q in the plan", id)
}

// recordCases is the record step, kept here so the tool and the CLI cannot drift apart.
func recordCases(crateDir string, u analyze.Unit, count int) (map[string]any, error) {
	sum, err := record.Run(record.Options{CrateDir: crateDir, Unit: u})
	if err != nil {
		return nil, err
	}
	return map[string]any{"cases": sum.Cases, "functions": sum.Functions, "throw_paths": sum.Throws}, nil
}

// planRoot walks up from the crate until the unit's file is under a parent.
func planRoot(crateDir, rel string) string {
	dir, err := filepath.Abs(crateDir)
	if err != nil {
		return crateDir
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(crateDir)
		}
		dir = parent
	}
}

// manifestChecks reads the throw counts and the blocked exports the gate needs.
func manifestChecks(crateDir string) (map[string]int, map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(crateDir, "golang-anything.json"))
	if err != nil {
		return nil, nil, err
	}
	var m struct {
		Exports []struct {
			Name       string `json:"name"`
			ThrowLines []int  `json:"throw_lines"`
			Blocked    string `json:"blocked"`
		} `json:"exports"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, err
	}
	throws := map[string]int{}
	blocked := map[string]string{}
	for _, e := range m.Exports {
		throws[e.Name] = len(e.ThrowLines)
		if e.Blocked != "" {
			blocked[e.Name] = e.Blocked
		}
	}
	return throws, blocked, nil
}

func jsonText(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
