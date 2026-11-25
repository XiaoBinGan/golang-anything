// Package buildinfo answers one question about the binary that is running: was it built with cgo?
//
// It matters because the parser is tree-sitter, which is C. A build without cgo has no parser of its
// own and starts a cgo build as a helper; a build with cgo parses in-process. `doctor` reports which,
// so a person can tell why `analyze` is slow or missing.
package buildinfo
