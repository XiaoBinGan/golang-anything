// Package usage holds the help text and nothing else.
//
// It lives apart from the rest of the tool so the front binary — the one that answers --help before
// anything else is loaded — can link it without linking the tool. The text is one constant; a second copy
// would be the drift this project keeps paying for.
package usage

const Text = `go-anything: port a codebase into Go, one unit at a time.

  go-anything analyze  [--json] <root>            what would have to be ported, and in what order
  go-anything record   --unit <id> <crate>       run the original, store the answers as cases.jsonl
  go-anything scaffold --unit <id> <crate>       write port.go stubs, the driver and the manifest
  go-anything diff     <crate>                   compare the port against the recorded cases
  go-anything gate     <crate>                   diff, plus the checks that stop a half-done port
  go-anything build    <crate>                   build the port and the stdio bridge into one binary
  go-anything bridge   <crate>                   write the host wrappers (index.cjs, port.py)
  go-anything bench    <crate>                   time the cases on the original and on the port
  go-anything map      <root>                    write PORT-MAP.md: every unit, its crate, its last diff
  go-anything clean    <crate>                   remove what this tool and go build left behind
  go-anything doctor                             check the runtimes a port needs
  go-anything build --fast <crate>               also build the in-process bridge (c-shared + N-API)
  go-anything wasm     <crate>                   build the port as WebAssembly and call it in-process
  go-anything check    <crate>                   replay the cases through the host wrapper (addon or stdio)
  go-anything enable   <crate> [--only a,b] [--async]   make the original call the port
  go-anything disable  <crate>                   take that block out again, byte for byte
  go-anything shadow   <crate>                   write shadow.cjs: original and port side by side
  go-anything shadow-report <crate>              summarize what shadow.cjs has seen
  go-anything fuzz     <crate> [--count N] [--seed S] [--keep]   fresh inputs from a new seed, both sides
  go-anything refresh  <crate>                   regenerate the binding files from port.go
  go-anything report   <crate> [--out DIR]       write REPORT.md and card.svg for a PR or a README
  go-anything package  <crate> [--shape library|cli|service]   lay out what users install + CI
  go-anything app record <repo> --command CMD    run a whole program on its cases and keep them
  go-anything app replay <repo> --command CMD    run another build against the recorded cases
  go-anything profile  <repo> --command CMD       run a workload under the profiler and rank hot functions
  go-anything go       <repo> --command CMD       profile, pick the hottest unit, and hand it over
  go-anything mcp                                serve the Model Context Protocol on stdin/stdout
  go-anything setup    [--agent A] [--print]      register this tool with coding agents

A crate is a folder this tool made. It holds go-anything.json, cases.jsonl, port.go and cmd/godriver.
`
