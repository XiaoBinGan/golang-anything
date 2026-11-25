// The floor for an in-process port: a wasm call is a plain function call — no cgo, no napi, no pipe.
//
// Build the probe first:
//   cd scripts/floor/wasmprobe && GOOS=wasip1 GOARCH=wasm go build -o ../probe.wasm .
// Then: node scripts/floor/wasm-floor.mjs
import { readFile } from 'node:fs/promises'
const here = new URL('.', import.meta.url)
const bytes = await readFile(new URL('probe.wasm', here))
const noop = () => 0
const { instance } = await WebAssembly.instantiate(bytes, {
  wasi_snapshot_preview1: new Proxy({}, { get: () => noop }),
})
try { instance.exports._start?.() } catch {}
const fn = instance.exports.count_spaces
if (!fn) {
  console.log('the probe exports:', Object.keys(instance.exports))
  process.exit(1)
}
const N = 200000
for (let i = 0; i < 1000; i++) fn(1)
const s = process.hrtime.bigint()
let acc = 0
for (let i = 0; i < N; i++) acc += fn(3)
const ns = Number(process.hrtime.bigint() - s) / N
console.log(JSON.stringify({
  wasm_call_ns: +ns.toFixed(1),
  rust_anything_napi_ns: 73,
  verdict: ns < 73 ? 'in-process wasm has the headroom to beat napi' : 'wasm cannot beat napi either',
  acc,
}, null, 2))
