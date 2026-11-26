// A real MCP client, because printf'd JSON-RPC never checked what the spec requires.
//
//   npm i @modelcontextprotocol/sdk     (once)
//   node scripts/mcp-client.mjs
//
// It initializes, lists tools, calls every advertised tool once, and fails if a listed tool cannot answer
// or if the schema a client validates is wrong. The first run of this found three things: doctor was not
// a tool, "required": null broke the SDK's schema validation, and nothing checked either.
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
import { resolve } from 'node:path'

const root = resolve(process.argv[2] ?? '.')
const bin = resolve(root, 'bin/golang-anything')
const crate = resolve(root, process.env.CRATE ?? 'testdata/realworld5/go-ports/jp-go')
const repo = resolve(root, process.env.REPO ?? 'testdata/realworld5')
const unit = process.env.UNIT ?? 'src/domain/jsonPointer.ts'

const client = new Client({ name: 'golang-anything-check', version: '1.0.0' }, { capabilities: {} })
const transport = new StdioClientTransport({ command: bin, args: ['mcp'], cwd: root })
await client.connect(transport)

const tools = (await client.listTools()).tools
console.log(`${tools.length} tools: ${tools.map(t => t.name).join(', ')}`)

let bad = 0
for (const t of tools) {
  const need = t.inputSchema?.required ?? []
  const args = {}
  for (const key of need) args[key] = key === 'repo' ? repo : key === 'unit' ? unit : crate
  if (t.name === 'profile') continue // needs a workload; scripts/perf.sh covers it
  try {
    const r = await client.callTool({ name: t.name, arguments: args })
    const text = (r.content ?? []).map(c => c.text ?? '').join('')
    const ok = !r.isError && text.length > 0
    if (!ok) bad++
    console.log(`${ok ? 'ok  ' : 'FAIL'} ${t.name.padEnd(14)} ${text.split('\n')[0].slice(0, 60)}`)
  } catch (e) {
    bad++
    console.log(`FAIL ${t.name.padEnd(14)} threw ${String(e.message).slice(0, 50)}`)
  }
}

const unknown = await client.callTool({ name: 'no-such-tool', arguments: {} })
if (unknown.isError !== true) {
  bad++
  console.log('FAIL an unknown tool answered without isError')
} else {
  console.log('ok   unknown tool → isError')
}

await client.close()
process.exit(bad ? 1 : 0)
