import { pathToFileURL } from 'node:url'
import { readFile, writeFile } from 'node:fs/promises'

const [unitPath, outPath, planPath, extraPath, only] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const plan = JSON.parse(await readFile(planPath, 'utf8'))

// One table per declared type. A port only has to reproduce calls the original accepts, so the
// generator never crosses a type: that was the prototype's first mistake.
const table = {
  string: ['', 'a', 'abc', '  ', 'a b  c', 'x y z', 'pad  ', 'héllo', 'naïve café', '😀😀',
           '$5 apple', '$a$b', '$', 'a$', '  \t  ', 'ünïcodé', 'ÆØÅ', '中文 字', 'a\nb', '0'],
  // Magnitudes stay small on purpose: a times argument of 1e9 makes the original allocate a
  // gigabyte and the recording never returns. Big values are what --inputs is for.
  number: [0, 1, 2, -1, 7, 1.5, -0.5, 1e6, -1e6, 0.1 + 0.2, 3.14159, 1 / 3, -0],
  boolean: [true, false],
  'string[]': [[], ['a'], ['a', 'b'], ['héllo', '😀😀'], ['$x', 'y']],
  'number[]': [[], [1], [1, 2, -1], [0.1, 0.2], [1e21]],
  object: [{}, { a: 1 }, { a: 'x', b: [1, 2] }, { ' ': 0 }],
  any: [0, 'a b  c', null, true],
}

// decode marks what JSON cannot carry, so both sides compare values rather than spellings.
const decode = v => {
  if (v === undefined) return { $undefined: true }
  if (typeof v === 'number' && Number.isNaN(v)) return { $number: 'NaN' }
  if (typeof v === 'number' && !Number.isFinite(v)) return { $number: v > 0 ? 'Infinity' : '-Infinity' }
  if (Object.is(v, -0)) return { $number: '-0' }
  if (typeof v === 'bigint') return { $bigint: v.toString() }
  if (v instanceof Date) return { $date: v.toISOString() }
  if (typeof v === 'string' && !v.isWellFormed()) return { $utf16: [...Array(v.length)].map((_, i) => v.charCodeAt(i)) }
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

const write = []
const seen = new Set()
// A call that takes longer than this is a sign the case is pathological, not that the code is slow:
// stop generating for that function and say so, instead of hanging the recording.
const BUDGET_MS = 250
const slow = new Set()
// A case has to be small enough to commit and to read in a diff. An answer past this is the
// generator's fault, not the code's: record it as skipped and stop asking that function.
const MAX_BYTES = 64 * 1024
const push = c => {
  const key = c.fn + '|' + JSON.stringify(c.args)
  if (seen.has(key)) return
  seen.add(key)
  write.push(c)
}
const call = (fn, args) => {
  if (slow.has(fn)) return
  const started = Date.now()
  try {
    const ok = decode(mod[fn](...args))
    const size = JSON.stringify(ok)?.length ?? 0
    if (size > MAX_BYTES) {
      slow.add(fn)
      process.stderr.write('go-anything: ' + fn + ' answers ' + size + ' bytes for ' +
        JSON.stringify(decode(args)) + '; that case is skipped and no more are made for it\n')
      return
    }
    push({ fn, args: decode(args), ok })
  } catch (e) {
    const type = e && e.constructor ? e.constructor.name : 'Error'
    push({ fn, args: decode(args), error: type + ': ' + String(e.message ?? e) })
  }
  if (Date.now() - started > BUDGET_MS) {
    slow.add(fn)
    process.stderr.write('go-anything: ' + fn + ' took ' + (Date.now() - started) + 'ms on ' +
      JSON.stringify(args) + '; no more cases for it\n')
  }
}

// Generated cases: each argument position walks its own table, never a cross product.
for (const [fn, types] of Object.entries(plan)) {
  if (typeof mod[fn] !== 'function') continue
  if (only && !only.split(',').includes(fn)) continue
  const values = types.map(t => table[t] ?? table.any)
  const walk = (args, i) => {
    if (i === values.length) return call(fn, args)
    for (const v of values[i]) walk([...args, v], i + 1)
  }
  walk([], 0)
}

// Calls a person supplied: the original's own tests, a bug report, a production sample.
let extra = []
try {
  extra = (await readFile(extraPath, 'utf8')).split('\n').filter(Boolean).map(l => JSON.parse(l))
} catch {}
for (const c of extra) {
  if (typeof mod[c.fn] !== 'function') continue
  if (only && !only.split(',').includes(c.fn)) continue
  call(c.fn, c.args ?? [], c.kwargs)
}

// A function that hit the budget is reported on stderr; its recorded cases are still valid.
await writeFile(outPath, write.map(c => JSON.stringify(c)).join('\n') + '\n')
