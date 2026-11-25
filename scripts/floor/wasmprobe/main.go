//go:build wasip1

// Package main is the smallest possible in-process port: one exported function, no allocation.
package main

//go:wasmexport count_spaces
func countSpaces(n int32) int32 { return n }

func main() {}
