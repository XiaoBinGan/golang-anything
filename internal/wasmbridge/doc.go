// Package wasmbridge builds the port as a WebAssembly reactor, which is the only in-process path that
// can beat a native binding: a wasm call crosses nothing at all.
//
// The measured ladder, on this machine (see PERF.md in the repository root):
//
//	leaf port (byte loop, no alloc)      18.2 ns
//	port that allocates (utf16.Encode)   121 ns
//	this tool's JSON envelope            750 ns
//	cgo + N-API adapter                  215 ns
//	rust-anything's napi-rs call          73 ns
//
// Everything below a microsecond here is decided by what the port body and the envelope do, not by the
// call: `go_call1` alone costs 0.51 us of registry lookup, []any boxing and Marshal. The next step is
// codegen per signature — `go_count(ptr, len) float64` — which removes all three.
package wasmbridge
