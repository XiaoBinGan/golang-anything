import { pathToFileURL } from 'node:url'
import { readFile } from 'node:fs/promises'

const [unitPath, casesPath, outPath, minMsArg, repeatArg] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const minMs = Number(minMsArg)
const repeat = Number(repeatArg)
const all = (await readFile(casesPath, 'utf8')).split('\n').filter(Boolean).map(l => JSON.parse(l))

const decode = v => {
  if (v === undefined) return undefined
  if (v && typeof v === 'object') {
    if (v.$number === 'NaN') return NaN
    if (v.$number === 'Infinity') return Infinity
    if (v.$number === '-Infinity') return -Infinity
    if (v.$number === '-0') return -0
    if (v.$bigint) return BigInt(v.$bigint)
    if (Array.isArray(v.$utf16)) return String.fromCharCode(...v.$utf16)
    if (v.$undefined) return undefined
    if (Array.isArray(v)) return v.map(decode)
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

const byFn = new Map()
for (const c of all) {
  if (!byFn.has(c.fn)) byFn.set(c.fn, [])
  byFn.get(c.fn).push(c)
}

const rows = []
for (const [fn, list] of byFn) {
  const f = mod[fn]
  if (typeof f !== 'function') continue
  const args = list.map(c => (c.args ?? []).map(decode))
  // Warm up first: the first call pays for lazy initialisation that a real caller pays once.
  for (const a of args) { try { f(...a) } catch {} }
  let rounds = 0
  const started = process.hrtime.bigint()
  let elapsed = 0n
  do {
    for (let r = 0; r < repeat; r++) {
      for (const a of args) { try { f(...a) } catch {} }
    }
    rounds++
    elapsed = process.hrtime.bigint() - started
  } while (Number(elapsed) / 1e6 < minMs && rounds < 10000)
  const perCall = Number(elapsed) / (rounds * repeat * args.length)
  rows.push({ name: fn, cases: args.length, ns: perCall })
}
await (await import('node:fs/promises')).writeFile(outPath, JSON.stringify(rows))
