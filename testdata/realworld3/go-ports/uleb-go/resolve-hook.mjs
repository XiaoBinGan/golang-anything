// Written by golang-anything: resolve a TypeScript import the way a bundler does.
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
