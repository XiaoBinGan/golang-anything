// Package fastbridge builds the in-process bridge: a Go c-shared library plus a hand-written N-API
// addon, so a JavaScript caller pays a function call instead of a pipe round trip.
//
// There are three entry points, and the measured cost of each is different by an order of magnitude:
//
//   - answer(name, argsJSON) — the general path: JSON on both sides, a napi string in and out.
//   - answer1(name, arg)     — one string argument, no JSON: what most ports look like.
//   - numbers(name, blob, lens) — a batch: arguments as one UTF-8 blob and a length table, answers
//     written straight into a Float64Array. The fixed cost of a crossing is paid once, and nothing
//     is marshalled per call, which is the only shape that gets under rust-anything's napi latency.
//
// PERF.md in the repository root has the numbers and how they were taken.
package fastbridge

//
// One rule for all three: whatever the entry point, the reply goes through the crate's Answer, so the
// markers a value needs are written. The fast paths exist to skip work, not to skip correctness — the
// one-string path used to marshal the raw value and answered [[97],[98]] for a port that returns code
// units, while the driver answered ["a","b"].
