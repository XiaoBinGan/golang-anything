//go:build cgo

package buildinfo

// Cgo is true when this binary was built with cgo, which means it can parse TypeScript itself.
const Cgo = true
