import { register } from 'node:module'
import { pathToFileURL } from 'node:url'

// The hook is a file of its own, registered by URL: a data: URL is a separate module scope, and the
// helpers would not be visible inside it — which is how the first version failed.
register(pathToFileURL(new URL('./resolve-hook.mjs', import.meta.url).pathname))
