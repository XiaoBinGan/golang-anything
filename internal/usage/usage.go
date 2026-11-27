// Package usage holds the help text and nothing else.
//
// It lives apart from the rest of the tool so the front binary — the one that answers --help before
// anything else is loaded — can link it without linking the tool. The text is one constant; a second copy
// would be the drift this project keeps paying for.
package usage

const Text = `golang-anything: port a codebase into Go, one unit at a time.

  golang-anything analyze  [--json] <root>            what would have to be ported, and in what order
  golang-anything record   --unit <id> <crate>       run the original, store the answers as cases.jsonl
  golang-anything scaffold --unit <id> <crate>       write port.go stubs, the driver and the manifest
  golang-anything diff     <crate>                   compare the port against the recorded cases
  golang-anything gate     <crate>                   diff, plus the checks that stop a half-done port
  golang-anything build    <crate>                   build the port and the stdio bridge into one binary
  golang-anything bridge   <crate>                   write the host wrappers (index.cjs, port.py)
  golang-anything bench    <crate>                   time the cases on the original and on the port
  golang-anything map      <root>                    write PORT-MAP.md: every unit, its crate, its last diff
  golang-anything clean    <crate>                   remove what this tool and go build left behind
  golang-anything doctor                             check the runtimes a port needs
  golang-anything build --fast <crate>               also build the in-process bridge (c-shared + N-API)
  golang-anything wasm     <crate>                   build the port as WebAssembly and call it in-process
  golang-anything check    <crate>                   replay the cases through the host wrapper (addon or stdio)
  golang-anything enable   <crate> [--only a,b] [--async]   make the original call the port
  golang-anything disable  <crate>                   take that block out again, byte for byte
  golang-anything promote  <crate> [--unit f] [--revert]   Ruby shim: a state-free instance method as Owner.name
  golang-anything shadow   <crate>                   write shadow.cjs: original and port side by side
  golang-anything shadow-report <crate>              summarize what shadow.cjs has seen
  golang-anything fuzz     <crate> [--count N] [--seed S] [--keep]   fresh inputs from a new seed, both sides
  golang-anything refresh  <crate>                   regenerate the binding files from port.go
  golang-anything report   <crate> [--out DIR]       write REPORT.md and card.svg for a PR or a README
  golang-anything package  <crate> [--shape library|cli|service]   lay out what users install + CI
  golang-anything app record <repo> --command CMD    run a whole program on its cases and keep them
  golang-anything app replay <repo> --command CMD    run another build against the recorded cases
  golang-anything profile  <repo> --command CMD       run a workload under the profiler and rank hot functions
  golang-anything go       <repo> --command CMD       profile, pick the hottest unit, and hand it over
  golang-anything mcp                                serve the Model Context Protocol on stdin/stdout
  golang-anything setup    [--agent A] [--print]      register this tool with coding agents

A crate is a folder this tool made. It holds golang-anything.json, cases.jsonl, port.go and cmd/godriver.
`
