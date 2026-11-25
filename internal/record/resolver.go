package record

// resolver is the loader hook written next to the recorder: a source file imports './util', which is a
// bundler's spelling, and Node's ESM resolver wants the extension. The hook tries the TypeScript and
// JavaScript spellings before giving up, so the original loads the way the project it came from does.
//
// It is written as a file rather than passed with -e because `node --import` takes a path, and it is
// assembled from an array of lines because the hook is JavaScript embedded in Go: no nested quoting to
// get wrong.
const resolver = `import { register } from 'node:module'
import { pathToFileURL } from 'node:url'

// The hook is a file of its own, registered by URL: a data: URL is a separate module scope, and the
// helpers would not be visible inside it — which is how the first version failed.
register(pathToFileURL(new URL('./resolve-hook.mjs', import.meta.url).pathname))
`

// resolveHook is registered by resolver above: TypeScript's own convention writes './foo.js' for a
// file called 'foo.ts', because the emit would be JavaScript. Both spellings are tried.
const resolveHook = `// Written by go-anything: resolve a TypeScript import the way a bundler does.
const TYPE_SCRIPT = /\.[cm]?[jt]sx?$/
const JS_SUFFIX = /\.(js|mjs|cjs)$/

export async function resolve(specifier, context, next) {
  if (!specifier.startsWith('.')) return next(specifier, context)
  try {
    return await next(specifier, context)
  } catch (error) {
    if (!JS_SUFFIX.test(specifier) && TYPE_SCRIPT.test(specifier)) throw error
  }
  for (const candidate of swaps(specifier)) {
    try {
      return await next(candidate, context)
    } catch {}
  }
  return next(specifier, context)
}

function swaps(specifier) {
  const out = []
  if (/\.js$/.test(specifier)) out.push(specifier.replace(/\.js$/, '.ts'), specifier.replace(/\.js$/, '.tsx'))
  if (/\.mjs$/.test(specifier)) out.push(specifier.replace(/\.mjs$/, '.mts'))
  if (/\.cjs$/.test(specifier)) out.push(specifier.replace(/\.cjs$/, '.cts'))
  if (!TYPE_SCRIPT.test(specifier)) for (const ext of ['.ts', '.tsx', '.js', '.mjs', '.cjs']) out.push(specifier + ext)
  return out
}
`
