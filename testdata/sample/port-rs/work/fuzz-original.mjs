import { pathToFileURL } from 'node:url'
import { readFile, writeFile } from 'node:fs/promises'

const [unitPath, callsPath, outPath] = process.argv.slice(2)
const mod = await import(pathToFileURL(unitPath).href)
const calls = (await readFile(callsPath, 'utf8')).split('\n').filter(Boolean).map(JSON.parse)

const decode = v => {
  if (v === undefined) return { $undefined: true }
  if (typeof v === 'number' && Number.isNaN(v)) return { $number: 'NaN' }
  if (typeof v === 'number' && !Number.isFinite(v)) return { $number: v > 0 ? 'Infinity' : '-Infinity' }
  if (Object.is(v, -0)) return { $number: '-0' }
  if (typeof v === 'bigint') return { $bigint: v.toString() }
  if (typeof v === 'string' && !v.isWellFormed()) return { $utf16: [...Array(v.length)].map((_, i) => v.charCodeAt(i)) }
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v)) out[k] = decode(v[k])
    return out
  }
  return v
}

const answers = []
for (const c of calls) {
  try {
    answers.push({ fn: c.fn, args: c.args, ok: decode(mod[c.fn](...(c.args ?? []))) })
  } catch (e) {
    const type = e && e.constructor ? e.constructor.name : 'Error'
    answers.push({ fn: c.fn, args: c.args, error: type + ': ' + String(e.message ?? e) })
  }
}
await writeFile(outPath, answers.map(a => JSON.stringify(a)).join('\n') + '\n')
