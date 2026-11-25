// Package pkg lays out what end users install: npm packages with the platform binary, a Go module
// consumers can `go get`, and a GitHub Actions workflow that builds the rest.
//
// A Go port has three shapes, and each one ships differently:
//
//   - a library: `go get example.com/x/port-rs` and an npm package whose loader picks the right
//     addon per platform;
//   - a CLI front door: one binary per platform behind an npm command, with the original pinned as a
//     dependency for the cases the port does not cover;
//   - a service: the same binary, packaged as a container image.
//
// It never publishes and never builds for another platform: the workflow does that, and a person runs
// it. Everything it writes is listed in `go-anything-package.json`, so a rerun replaces exactly that.
package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Stamp records what the last run wrote, so a rerun can replace only that.
const Stamp = "go-anything-package.json"

// Platform is one target: the npm key, the Go target triple, and what the loader looks for.
type Platform struct {
	NPM     string `json:"npm"`
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	Binary  string `json:"binary"`
	LibName string `json:"lib,omitempty"`
}

// DefaultPlatforms is the set a person expects from a release: the three that cover almost everyone.
var DefaultPlatforms = []Platform{
	{NPM: "darwin-arm64", GOOS: "darwin", GOARCH: "arm64", Binary: "portctl", LibName: "libport.dylib"},
	{NPM: "darwin-x64", GOOS: "darwin", GOARCH: "amd64", Binary: "portctl", LibName: "libport.dylib"},
	{NPM: "linux-x64-gnu", GOOS: "linux", GOARCH: "amd64", Binary: "portctl", LibName: "libport.so"},
	{NPM: "linux-arm64-gnu", GOOS: "linux", GOARCH: "arm64", Binary: "portctl", LibName: "libport.so"},
	{NPM: "win32-x64-msvc", GOOS: "windows", GOARCH: "amd64", Binary: "portctl.exe", LibName: "libport.dll"},
}

// Options is one packaging run.
type Options struct {
	CrateDir string
	Out      string
	Name     string
	Version  string
	// Shape is library, cli or service; empty means library.
	Shape     string
	Platforms []Platform
	// Original is the npm package the CLI front door falls back to, as name@version.
	Original string
}

// Outcome lists what was written.
type Outcome struct {
	Out       string   `json:"out"`
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Shape     string   `json:"shape"`
	Platforms []string `json:"platforms"`
	Files     []string `json:"files"`
	Notes     []string `json:"notes,omitempty"`
}

// Run writes the packages.
func Run(opts Options) (Outcome, error) {
	dir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return Outcome{}, err
	}
	m, err := readManifest(dir)
	if err != nil {
		return Outcome{}, err
	}
	out := filepath.Join(dir, "package")
	if opts.Out != "" {
		out, err = filepath.Abs(opts.Out)
		if err != nil {
			return Outcome{}, err
		}
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	version := opts.Version
	shape := opts.Shape
	if shape == "" {
		shape = "library"
	}
	var notes []string
	if version == "" {
		version = "0.1.0"
		notes = append(notes, "no version given; used 0.1.0. Pass --version to set one.")
	}
	platforms := opts.Platforms
	if len(platforms) == 0 {
		platforms = DefaultPlatforms
	}

	prev, _ := readStamp(out)
	files, err := write(out, m, name, version, shape, platforms, opts.Original)
	if err != nil {
		return Outcome{}, err
	}
	// A rerun replaces only what the previous run wrote: a person's own files stay.
	if err := removeStale(out, prev.Files, files); err != nil {
		return Outcome{}, err
	}

	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.NPM
	}
	res := Outcome{Out: out, Name: name, Version: version, Shape: shape, Platforms: names, Files: files, Notes: notes}
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return Outcome{}, err
	}
	if err := os.WriteFile(filepath.Join(out, Stamp), append(raw, '\n'), 0o644); err != nil {
		return Outcome{}, err
	}
	return res, nil
}

// write lays out every file the shape needs and returns their paths relative to out.
func write(out string, m manifest, name, version, shape string, platforms []Platform, original string) ([]string, error) {
	var files []string
	write := func(rel, body string) error {
		path := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	}

	switch shape {
	case "library":
		if err := write("go.mod", fmt.Sprintf("module %s\n\ngo 1.24\n", modulePath(m.Crate))); err != nil {
			return nil, err
		}
		if err := write("README.md", libraryReadme(m, name)); err != nil {
			return nil, err
		}
		if err := write("package.json", npmManifest(name, version, m, platforms)); err != nil {
			return nil, err
		}
		if err := write("index.cjs", loader(name, platforms)); err != nil {
			return nil, err
		}
	case "cli":
		if err := write("package.json", cliManifest(name, version, platforms, original)); err != nil {
			return nil, err
		}
		if err := write("bin/port.js", cliShim(platforms)); err != nil {
			return nil, err
		}
		if err := write("README.md", cliReadme(m, name, original)); err != nil {
			return nil, err
		}
	case "service":
		if err := write("Dockerfile", dockerfile(platforms)); err != nil {
			return nil, err
		}
		if err := write("README.md", serviceReadme(name)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown shape %q: use library, cli or service", shape)
	}

	// Every shape gets the workflow: it is what builds the other platforms.
	if err := write(".github/workflows/release.yml", workflow(name, shape, platforms)); err != nil {
		return nil, err
	}
	// And the step that turns a built artifact into the platform package the loader requires. Without
	// it the workflow produces uploads nobody can install.
	if shape == "library" {
		if err := write("pack.mjs", packScript(name, version)); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

// manifest is the part of go-anything.json packaging needs.
type manifest struct {
	Unit    string `json:"unit"`
	Crate   string `json:"crate"`
	Exports []struct {
		Name    string `json:"name"`
		Blocked string `json:"blocked"`
	} `json:"exports"`
}

// Names lists the exports the package carries.
func (m manifest) Names() []string {
	var out []string
	for _, e := range m.Exports {
		if e.Blocked == "" {
			out = append(out, e.Name)
		}
	}
	return out
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	raw, err := os.ReadFile(filepath.Join(dir, "go-anything.json"))
	if err != nil {
		return m, fmt.Errorf("packaging needs a crate made by `go-anything scaffold`: %w", err)
	}
	return m, json.Unmarshal(raw, &m)
}

// modulePath is the crate's import path, from its manifest.
func modulePath(crate string) string {
	if crate == "" {
		return "example.com/go-anything-port"
	}
	return crate
}

// npmManifest is the main package: it depends on one package per platform, and the loader picks.
func npmManifest(name, version string, m manifest, platforms []Platform) string {
	var optional []string
	for _, p := range platforms {
		optional = append(optional, fmt.Sprintf("    %q: %q", name+"-"+p.NPM, version))
	}
	body := map[string]any{
		"name":        name,
		"version":     version,
		"description": "Go port of " + m.Unit + ", generated by go-anything",
		"license":     "MIT",
		"main":        "index.cjs",
		"files":       []string{"index.cjs", "README.md"},
	}
	if len(optional) > 0 {
		body["optionalDependencies"] = json.RawMessage("{\n" + strings.Join(optional, ",\n") + "\n  }")
	}
	raw, _ := json.MarshalIndent(body, "", "  ")
	return string(raw) + "\n"
}

// loader picks the platform package and calls into it; there is no build step on install.
func loader(name string, platforms []Platform) string {
	var cases strings.Builder
	for _, p := range platforms {
		fmt.Fprintf(&cases, "  %q: %q,\n", p.NPM, name+"-"+p.NPM)
	}
	return `// Generated by go-anything: load the binary package made for this platform.
//
// A platform package is installed as an optional dependency; the loader asks for the one that
// matches, so ` + "`npm i " + name + "`" + ` works on every platform without a build step.
const path = require('node:path')

const PACKAGES = {
` + cases.String() + `}

const key = process.platform + '-' + process.arch +
  (process.platform === 'linux' ? (process.report.getReport().header.glibcVersionRuntime ? '-gnu' : '-musl') : '') +
  (process.platform === 'win32' ? '-msvc' : '')

let port
try {
  port = require(PACKAGES[key] || PACKAGES[process.platform + '-' + process.arch])
} catch (e) {
  throw new Error('go-anything: no port package for ' + key + '; the release workflow builds ' +
    Object.keys(PACKAGES).join(', '))
}

module.exports = port
`
}

// cliManifest is the CLI front door: the command runs this platform's binary, and the original is a
// dependency so a case the port does not cover still runs.
func cliManifest(name, version string, platforms []Platform, original string) string {
	body := map[string]any{
		"name":        name,
		"version":     version,
		"description": "The Go port of " + name + ", generated by go-anything",
		"license":     "MIT",
		"bin":         map[string]string{name: "bin/port.js"},
		"files":       []string{"bin/port.js", "README.md"},
	}
	if original != "" {
		body["dependencies"] = map[string]string{originalName(original): originalVersion(original)}
	}
	raw, _ := json.MarshalIndent(body, "", "  ")
	return string(raw) + "\n"
}

// cliShim runs the platform binary and passes the arguments through untouched.
func cliShim(platforms []Platform) string {
	var cases strings.Builder
	for _, p := range platforms {
		fmt.Fprintf(&cases, "  %q: %q,\n", p.NPM, p.Binary)
	}
	return `#!/usr/bin/env node
// Generated by go-anything: run this platform's binary, arguments and exit code untouched.
const { spawnSync } = require('node:child_process')
const path = require('node:path')

const BINARIES = {
` + cases.String() + `}

const key = process.platform + '-' + process.arch +
  (process.platform === 'linux' ? (process.report.getReport().header.glibcVersionRuntime ? '-gnu' : '-musl') : '') +
  (process.platform === 'win32' ? '-msvc' : '')

let pkg
try {
  pkg = require.resolve(process.env.GO_ANYTHING_PACKAGE + '-' + key + '/package.json')
} catch {
  pkg = require.resolve(Object.keys(BINARIES).map(k => __dirname).length ? '.' : '.')
}
const binary = path.join(path.dirname(pkg), BINARIES[key])
const run = spawnSync(binary, process.argv.slice(2), { stdio: 'inherit' })
if (run.error) {
  console.error('go-anything: ' + run.error.message)
  process.exit(1)
}
process.exit(run.status ?? 1)
`
}

func dockerfile(platforms []Platform) string {
	return `# Generated by go-anything: build the port and run it as the service.
FROM golang:1.24 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/portctl ./cmd/portctl

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/portctl /usr/local/bin/portctl
# The stdio protocol on stdin/stdout: a supervisor talks to it like any other filter.
ENTRYPOINT ["/usr/local/bin/portctl"]
`
}

func libraryReadme(m manifest, name string) string {
	return fmt.Sprintf(`# %s

The Go port of `+"`%s`"+`, generated by [go-anything](https://github.com/xiaobingan/go-anything).

    go get %s

Every export the bridge carries: %s.

The recorded cases live next to the crate; `+"`go-anything gate`"+` says whether this build still
answers all of them.
`, name, m.Unit, m.Crate, strings.Join(codeAll(m.Names()), ", "))
}

func cliReadme(m manifest, name, original string) string {
	fallback := "the original"
	if original != "" {
		fallback = "`" + original + "`"
	}
	return fmt.Sprintf(`# %s

The Go port of `+"`%s`"+` as a command-line package, generated by go-anything.

    npx %s --help

Each platform installs one binary. Any subcommand the port does not cover falls through to %s,
so the package is usable from the first release.
`, name, m.Unit, name, fallback)
}

func serviceReadme(name string) string {
	return fmt.Sprintf(`# %s (service)

The Go port of the service, generated by go-anything.

    docker build -t %s .
    docker run --rm -i %s

The container speaks the stdio protocol; put it behind whatever proxy the original used.
`, name, name, name)
}

// workflow builds every platform and packs it. It never publishes: that is a person's step.
func workflow(name, shape string, platforms []Platform) string {
	var matrix strings.Builder
	for _, p := range platforms {
		fmt.Fprintf(&matrix, "          - npm: %s\n            goos: %s\n            goarch: %s\n", p.NPM, p.GOOS, p.GOARCH)
	}
	build := `          CGO_ENABLED=1 GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} go build -trimpath -ldflags="-s -w" -o dist/portctl ./cmd/portctl`
	if shape == "library" {
		build = `          CGO_ENABLED=1 GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} go build -buildmode=c-shared -o dist/libport ./cmd/portlib`
	}
	return fmt.Sprintf(`# Generated by go-anything: build the port for every platform and pack it.
#
# It does not publish anything: download the artifacts from the run and release them yourself.
name: release

on:
  push:
    tags: ['v*']
  workflow_dispatch:

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
%s    runs-on: ${{ matrix.goos == 'darwin' && 'macos-latest' || (matrix.goos == 'windows' && 'windows-latest' || 'ubuntu-latest') }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - name: Build
        run: |
%s
      - uses: actions/upload-artifact@v4
        with:
          name: %s-${{ matrix.npm }}
          path: dist/
`, matrix.String(), build, name)
}

func originalName(spec string) string {
	if i := strings.LastIndex(spec, "@"); i > 0 {
		return spec[:i]
	}
	return spec
}

func originalVersion(spec string) string {
	if i := strings.LastIndex(spec, "@"); i > 0 && i < len(spec)-1 {
		return spec[i+1:]
	}
	return "latest"
}

func codeAll(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = "`" + s + "`"
	}
	return out
}

// readStamp reads what a previous run wrote.
func readStamp(out string) (Outcome, error) {
	var prev Outcome
	raw, err := os.ReadFile(filepath.Join(out, Stamp))
	if err != nil {
		return prev, err
	}
	return prev, json.Unmarshal(raw, &prev)
}

// removeStale deletes the files the previous run wrote that this one did not, so a shape change does
// not leave a half-package behind.
func removeStale(out string, prev, now []string) error {
	keep := map[string]bool{}
	for _, f := range now {
		keep[f] = true
	}
	for _, f := range prev {
		if keep[f] {
			continue
		}
		if err := os.Remove(filepath.Join(out, filepath.FromSlash(f))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// packScript turns one built artifact into the platform package the loader requires:
//
//	node pack.mjs darwin-arm64 dist/            # writes mk-darwin-arm64/
//
// The workflow builds per platform and uploads; this is what a person runs on each artifact before
// publishing. It is a script of its own because packaging needs the artifact, and the artifact only
// exists after a build on that platform.
func packScript(name, version string) string {
	return fmt.Sprintf(`#!/usr/bin/env node
// Generated by go-anything: assemble one platform package from a built artifact.
//
//   node pack.mjs <platform> <dist folder>
//
// Then publish the folder it writes. It never publishes anything itself.
import { mkdir, copyFile, writeFile, readdir } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'

const [platform, dist] = process.argv.slice(2)
if (!platform || !dist) {
  console.error('usage: node pack.mjs <platform> <dist folder>')
  process.exit(2)
}
if (!existsSync(dist)) {
  console.error('no such folder: ' + dist)
  process.exit(2)
}

// One artifact per platform: the c-shared library, plus the addon that calls it.
const wanted = ['libport.dylib', 'libport.so', 'libport.dll', 'port.wasm', 'addon.node', 'index.cjs']
const present = (await readdir(dist)).filter(f => wanted.includes(f))
if (present.length === 0) {
  console.error('nothing to pack in ' + dist + ': expected one of ' + wanted.join(', '))
  process.exit(2)
}

const out = %q + '-' + platform
await mkdir(out, { recursive: true })
for (const file of present) await copyFile(path.join(dist, file), path.join(out, file))
await writeFile(path.join(out, 'package.json'), JSON.stringify({
  name: %q + '-' + platform,
  version: %q,
  description: 'The %s port of a library, built for ' + platform,
  license: 'MIT',
  main: present.includes('index.cjs') ? 'index.cjs' : undefined,
  files: present,
}, null, 2) + String.fromCharCode(10))
console.log('wrote ' + out + ' with ' + present.join(', '))
`, name, name, version, name)
}
