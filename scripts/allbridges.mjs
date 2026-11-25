// Every bridge must answer the same thing for the values JSON cannot carry.
//
//   node scripts/allbridges.mjs testdata/markers/go-ports/markers-go
//
// Needs the crate built three ways: `build --fast` (addon), `wasm`, and `build` (stdio binary).
import { createRequire } from 'node:module'
import { resolve } from 'node:path'
const require = createRequire(import.meta.url)
const dir = resolve(process.argv[2])
const cjs = require(dir + '/index.cjs')
const wasm = (await import(dir + '/index.wasm.mjs')).default

const cases = [
  ['negativeZero', [], '-0'],
  ['loneSurrogate', [], 'lone surrogate'],
  ['undefinedValue', [], 'undefined'],
  ['divide', [1, 0], 'Infinity'],
  ['divide', [0, 0], 'NaN'],
  ['isNegativeZero', [-0], '-0 as an argument'],
]

const same = (a, b) => {
  if (typeof a === 'number' && typeof b === 'number') return Object.is(a, b)
  if (a === undefined && b === undefined) return true
  return JSON.stringify(a) === JSON.stringify(b)
}

let bad = 0
for (const [fn, args, label] of cases) {
  const inProc = cjs.callSync(fn, ...args)
  const overWasm = wasm[fn](...args)
  const async = await cjs.async[fn](...args)
  const agree = same(inProc, overWasm) && same(inProc, async)
  if (!agree) bad++
  const show = v => v === undefined ? 'undefined' : (typeof v === 'number' && Object.is(v, -0)) ? '-0' : String(v)
  console.log(`${agree ? 'ok  ' : 'DIFF'} ${label.padEnd(18)} addon=${show(inProc)} wasm=${show(overWasm)} stdio=${show(async)}`)
}
cjs.close?.()
process.exit(bad ? 1 : 0)
