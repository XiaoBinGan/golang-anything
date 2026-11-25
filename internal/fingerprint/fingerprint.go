// Package fingerprint is a stable, short id for a blob of source.
//
// It used to be crypto/sha256, for a value that is written into recorded-from.json and into the name of
// the block `enable` writes. Nothing depends on it being cryptographic: it has to be stable across runs
// and unlikely to collide for the handful of files a crate has. FNV-1a is both, and it keeps the crypto
// packages — and their init, 0.35 ms of it — out of a binary that never does any cryptography.
package fingerprint

import "fmt"

// Of answers sixteen hex characters for the bytes.
func Of(data []byte) string {
	// FNV-1a, 64 bit: the constants are the ones from the original paper.
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	h := uint64(offset)
	for _, b := range data {
		h ^= uint64(b)
		h *= prime
	}
	return fmt.Sprintf("%016x", h)
}
