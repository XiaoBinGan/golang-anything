//go:build !cgo

package buildinfo

// Cgo is false: this build has no parser of its own and starts a cgo build as a helper.
const Cgo = false
