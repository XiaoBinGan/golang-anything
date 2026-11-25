// A registry that serves the two tarballs, so `npm install <name>` resolves optionalDependencies the way
// it does in real life. Usage:
//
//   node npm-registry.mjs --work DIR --port 4873 --name jp --platform-package jp-darwin-arm64 --version 1
import { createServer } from 'node:http'
import { createHash } from 'node:crypto'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

const arg = (name, fallback) => {
  const i = process.argv.indexOf('--' + name)
  return i > 0 ? process.argv[i + 1] : fallback
}
const work = arg('work', '.')
const port = Number(arg('port', 4873))
const name = arg('name', 'jp')
const platformPackage = arg('platform-package', name + '-darwin-arm64')
const version = arg('version', '1.0.0')

const tarballs = readdirSync(work).filter(f => f.endsWith('.tgz'))
const packages = {}
for (const file of tarballs) {
  const pkgName = file.slice(0, file.length - `-${version}.tgz`.length)
  packages[pkgName] = file
}

const server = createServer((req, res) => {
  const url = decodeURIComponent(req.url.split('?')[0])
  const manifest = url.match(/^\/(@?[^/]+)\/?$/) || url.match(/^\/(@?[^/]+)\/([^/]+)$/)
  if (manifest && packages[manifest[1]]) {
    const file = packages[manifest[1]]
    // npm verifies the shasum: a placeholder fails with EINTEGRITY and nothing installs.
    const shasum = createHash('sha1').update(readFileSync(join(work, file))).digest('hex')
    const body = {
      name: manifest[1],
      'dist-tags': { latest: version },
      versions: {
        [version]: {
          name: manifest[1],
          version,
          optionalDependencies: manifest[1] === name ? { [platformPackage]: version } : undefined,
          dist: { tarball: `http://127.0.0.1:${port}/${manifest[1]}/-/${file}`, shasum },
        },
      },
    }
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify(body))
    return
  }
  const tarball = url.match(/^.+\/-\/([^/]+\.tgz)$/)
  if (tarball) {
    res.setHeader('content-type', 'application/octet-stream')
    res.end(readFileSync(join(work, tarball[1])))
    return
  }
  res.statusCode = 404
  res.end('{}')
})
server.listen(port, () => process.stderr.write(`registry on ${port}: ${Object.keys(packages).join(', ')}\n`))
