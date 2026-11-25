package record

// resolver is the loader hook written next to the recorder: a source file imports './util', which is a
// bundler's spelling, and Node's ESM resolver wants the extension. The hook tries the TypeScript and
// JavaScript spellings before giving up, so the original loads the way the project it came from does.
//
// It is written as a file rather than passed with -e because `node --import` takes a path, and it is
// assembled from an array of lines because the hook is JavaScript embedded in Go: no nested quoting to
// get wrong.
const resolver = `import { register } from 'node:module'

const hook = [
  'export async function resolve(specifier, context, next) {',
  "  if (specifier.startsWith('.') && !/\\.[cm]?[jt]sx?$/.test(specifier)) {",
  "    for (const ext of ['.ts', '.tsx', '.js', '.mjs', '.cjs']) {",
  '      try { return await next(specifier + ext, context) } catch {}',
  '    }',
  '  }',
  '  return next(specifier, context)',
  '}',
].join(String.fromCharCode(10))

register('data:text/javascript,' + encodeURIComponent(hook))
`
