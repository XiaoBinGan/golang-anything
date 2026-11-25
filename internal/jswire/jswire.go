// Package jswire is the JavaScript half of the wire, in one place.
//
// Four generated files need to turn wire markers into values, turn values back into markers, or write a
// canonical form for comparison: the CJS wrapper, the WebAssembly wrapper, shadow.cjs and check.mjs.
// Each one used to carry its own copy, and the copies drifted — check called -0 equal to 0 while shadow
// did not, so a port that returned 0 where the original returned -0 passed check and failed shadow.
//
// The three functions below are plain JavaScript with no imports, so a CommonJS file and an ES module can
// both embed them.
package jswire

// Decode turns wire markers back into JavaScript values: NaN, ±Infinity, -0, a bigint, a lone surrogate
// and undefined all have a marker because JSON cannot carry them.
const Decode = `function decode(v) {
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object') {
    const keys = Object.keys(v)
    if (keys.length === 1) {
      if (typeof v.$number === 'string') {
        return v.$number === 'NaN' ? NaN : v.$number === 'Infinity' ? Infinity
          : v.$number === '-Infinity' ? -Infinity : -0
      }
      if (typeof v.$bigint === 'string') return BigInt(v.$bigint)
      if (Array.isArray(v.$utf16)) return String.fromCharCode(...v.$utf16)
      if (v.$undefined === true) return undefined
    }
    const out = {}
    for (const k of keys) out[k] = decode(v[k])
    return out
  }
  return v
}`

// Encode is the inverse, and the reason a call carrying -0 or NaN reaches the port as itself:
// JSON.stringify([-0]) is "[0]".
const Encode = `function encode(v) {
  if (v === undefined) return { $undefined: true }
  if (typeof v === 'number') {
    if (Number.isNaN(v)) return { $number: 'NaN' }
    if (!Number.isFinite(v)) return { $number: v > 0 ? 'Infinity' : '-Infinity' }
    if (Object.is(v, -0)) return { $number: '-0' }
    return v
  }
  if (typeof v === 'bigint') return { $bigint: v.toString() }
  if (typeof v === 'string' && !v.isWellFormed()) {
    return { $utf16: [...Array(v.length)].map((_, i) => v.charCodeAt(i)) }
  }
  if (Array.isArray(v)) return v.map(encode)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v)) out[k] = encode(v[k])
    return out
  }
  return v
}`

// Canon writes a value as a string that compares as a value: keys sorted, and the markers spelled the way
// the wire spells them, so -0 is not 0 and NaN is not null.
const Canon = `const canon = v => {
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']'
  if (typeof v === 'number') {
    if (Number.isNaN(v)) return '"$NaN"'
    if (v === Infinity) return '"$Infinity"'
    if (v === -Infinity) return '"$-Infinity"'
    if (Object.is(v, -0)) return '"$-0"'
    return JSON.stringify(v)
  }
  if (typeof v === 'bigint') return JSON.stringify('$bigint:' + v.toString())
  if (v === undefined) return '"$undefined"'
  if (v && typeof v === 'object') {
    return '{' + Object.keys(v).sort().map(k => JSON.stringify(k) + ':' + canon(v[k])).join(',') + '}'
  }
  return JSON.stringify(v)
}`
