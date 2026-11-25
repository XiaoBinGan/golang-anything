// The floor for any shared-memory channel: two JavaScript threads, one SharedArrayBuffer, no cgo,
// no syscall, same process. If this round trip is already slower than 73 ns, no design that also
// involves Go can beat rust-anything's napi call.
import { Worker, isMainThread, parentPort, workerData } from 'node:worker_threads'

if (isMainThread) {
  const sab = new SharedArrayBuffer(64)
  const seq = new BigUint64Array(sab)
  const worker = new Worker(new URL(import.meta.url), { workerData: sab })

  const N = 20000
  // Warm up, then time N round trips: store a value, spin until the worker's reply lands.
  for (let i = 1; i <= 1000; i++) {
    Atomics.store(seq, 0, BigInt(i))
    while (Atomics.load(seq, 1) !== BigInt(i)) {}
  }
  const start = process.hrtime.bigint()
  for (let i = 1001; i <= 1001 + N; i++) {
    Atomics.store(seq, 0, BigInt(i))
    while (Atomics.load(seq, 1) !== BigInt(i)) {}
  }
  const ns = Number(process.hrtime.bigint() - start) / N
  console.log(JSON.stringify({
    round_trip_ns: +ns.toFixed(1),
    per_call_us: +(ns / 1000).toFixed(4),
    rust_anything_napi_ns: 73,
    verdict: ns < 73 ? 'a ring could beat it' : 'a ring cannot beat it on this machine',
  }, null, 2))
  worker.terminate()
} else {
  const seq = new BigUint64Array(workerData)
  let seen = 0n
  for (;;) {
    const v = Atomics.load(seq, 0)
    if (v !== seen) {
      seen = v
      Atomics.store(seq, 1, v)
    }
  }
}
