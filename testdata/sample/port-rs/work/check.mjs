import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const port = require(process.argv[2] + '/index.cjs')

const decode = v => {
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

// canon sorts keys: a Go map marshals alphabetically, and JSON.stringify is order-sensitive, so a
// deep compare has to normalise both sides first.
const canon = v => {
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']'
  if (v && typeof v === 'object') {
    return '{' + Object.keys(v).sort().map(k => JSON.stringify(k) + ':' + canon(v[k])).join(',') + '}'
  }
  return JSON.stringify(v === undefined ? null : v)
}

const inProcess = port.inProcess === true
const cases = (await readFile(process.argv[3], 'utf8')).split('\n').filter(Boolean).map(JSON.parse)
let matched = 0
const bad = []
for (const c of cases) {
  try {
    const args = (c.args ?? []).map(decode)
    const got = inProcess ? port.callSync(c.fn, ...args) : await port.call(c.fn, args)
    // The recorded answer carries wire markers too: decode before comparing.
    const want = decode(c.ok)
    if (canon(got) === canon(want)) matched++
    else if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want, got })
  } catch (e) {
    if (c.error && String(e.message).split(':')[0] === c.error.split(':')[0]) matched++
    else if (bad.length < 5) bad.push({ fn: c.fn, args: c.args, want: c.error, got: String(e.message) })
  }
}
console.log(JSON.stringify({ matched, total: cases.length, bad, inProcess }))
port.close?.()
