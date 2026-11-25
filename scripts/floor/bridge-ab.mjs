// One call, both bridges, alternating rounds: the number the goal calls "single-call latency".
//
//   node scripts/floor/bridge-ab.mjs testdata/sample/port-rs /tmp/racmp2/rust/text-rs
//
// Both crates must be built: the Go one with `build --fast` (N-API addon) or `wasm`, the Rust one with
// its own `build`. Each side calls its own port of `count` with the same argument.
import { createRequire } from 'node:module'
import { resolve } from 'node:path'

const require = createRequire(import.meta.url)
const [goCrate, rustCrate] = process.argv.slice(2).map(p => resolve(p))

const go = require(goCrate + '/index.cjs')
const rust = require(rustCrate + '/index.cjs')
let wasm = null
try {
  wasm = (await import(goCrate + '/index.wasm.mjs')).default
} catch {
  wasm = null // the crate was not built with `go-anything wasm`
}

const text = 'a b  c'
const N = 20000
function nsPer(fn) {
  for (let i = 0; i < 500; i++) fn()
  const s = process.hrtime.bigint()
  for (let i = 0; i < N; i++) fn()
  return Number(process.hrtime.bigint() - s) / N
}
const med = xs => xs.slice().sort((a, b) => a - b)[Math.floor(xs.length / 2)]

const rounds = {
  'go-anything callSync (cgo addon)': [],
  'rust-anything napi': [],
}
if (wasm) rounds['go-anything wasm (in-process)'] = []
for (let r = 0; r < 7; r++) {
  rounds['go-anything callSync (cgo addon)'].push(nsPer(() => go.callSync('count', text)))
  rounds['rust-anything napi'].push(nsPer(() => rust.count(text)))
  if (wasm) rounds['go-anything wasm (in-process)'].push(nsPer(() => wasm.count(text)))
}
for (const [name, xs] of Object.entries(rounds)) {
  const v = med(xs)
  console.log(`  ${name.padEnd(34)} ${(v / 1000).toFixed(3)} µs/call   ${Math.round(1e9 / v).toLocaleString()} calls/s`)
}
go.close?.()
