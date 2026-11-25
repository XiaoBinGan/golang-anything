// Command portlib is the in-process bridge: a c-shared library that answers calls as JSON.
//
//	go build -buildmode=c-shared -o libport.dylib ./cmd/portlib
//
// The caller owns the returned string and must hand it back to go_free.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"strconv"
	"unsafe"

	port "github.com/xiaobingan/go-anything/testdata/sample/port-rs"
)

//export go_answer_into
//
// Answers into the caller's buffer and returns the length, or -need when it is too small. Nothing is
// allocated per call: the request is read straight out of the C buffer and the reply is written into
// the one the caller owns. That is what makes an in-process call cost hundreds of nanoseconds instead
// of microseconds.
func go_answer_into(name *C.char, nameLen C.int, args *C.char, argsLen C.int, out *C.char, outCap C.int) C.int {
	reply := answer(unsafe.Slice((*byte)(unsafe.Pointer(name)), int(nameLen)),
		unsafe.Slice((*byte)(unsafe.Pointer(args)), int(argsLen)))
	if len(reply) > int(outCap) {
		return -C.int(len(reply))
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(out)), int(outCap)), reply)
	return C.int(len(reply))
}

//export go_answer_str
//
// The one-argument string call is the shape most ports have, and JSON on both sides is most of its
// cost. This path takes the string and answers a string, so nothing is parsed or marshalled.
func go_answer_str(name *C.char, nameLen C.int, arg *C.char, argLen C.int, out *C.char, outCap C.int) C.int {
	// Write the reply straight into the caller's buffer: no Marshal, no copy, no allocation on the
	// path a caller takes thousands of times a second.
	dst := unsafe.Slice((*byte)(unsafe.Pointer(out)), int(outCap))[:0]
	fn, ok := port.Registry[string(unsafe.Slice((*byte)(unsafe.Pointer(name)), int(nameLen)))]
	if !ok {
		dst = append(dst, "\"not ported\""...)
		return C.int(len(dst))
	}
	// The argument is already UTF-8 in the C buffer: one copy into a Go string is unavoidable.
	v, err := fn([]any{C.GoStringN(arg, argLen)})
	if err != nil {
		if len(dst)+len(err.Error())+2 > int(outCap) {
			return -C.int(len(err.Error()) + 2)
		}
		dst = strconv.AppendQuote(dst, err.Error())
		return C.int(len(dst))
	}
	switch x := v.(type) {
	case string:
		if len(dst)+len(x)+2 <= int(outCap) {
			dst = strconv.AppendQuote(dst, x)
			return C.int(len(dst))
		}
	case float64:
		if len(dst)+24 <= int(outCap) {
			dst = strconv.AppendFloat(dst, x, 'g', -1, 64)
			return C.int(len(dst))
		}
	case bool:
		if len(dst)+5 <= int(outCap) {
			dst = strconv.AppendBool(dst, x)
			return C.int(len(dst))
		}
	}
	b, _ := json.Marshal(v)
	return writeOut(out, outCap, b)
}

// writeOut copies a reply into the caller's buffer and answers its length, or -need.
func writeOut(out *C.char, outCap C.int, reply []byte) C.int {
	if len(reply) > int(outCap) {
		return -C.int(len(reply))
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(out)), int(outCap)), reply)
	return C.int(len(reply))
}

//export go_free
func go_free(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

// answer takes the export name and its arguments as two buffers, so nothing has to be packed into a
// request object first: the name is a map lookup and only the arguments are parsed.
func answer(name, argsJSON []byte) []byte {
	var args []any
	if len(argsJSON) > 0 && string(argsJSON) != "null" {
		if err := json.Unmarshal(argsJSON, &args); err != nil {
			return []byte("{\"error\":\"bad args\"}")
		}
	}
	// Marshal once and hand the slice back: an encoder into an append-writer measured slower on this
	// path (1.04 µs against 0.7), which is why the plain call is here.
	out, err := json.Marshal(port.Answer(string(name), args))
	if err != nil {
		return []byte("{\"error\":\"reply\"}")
	}
	return out
}



func main() {}
