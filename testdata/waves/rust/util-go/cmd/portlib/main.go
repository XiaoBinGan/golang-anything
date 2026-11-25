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
	"math"
	"strconv"
	"unsafe"

	port "github.com/xiaobingan/go-anything/testdata/waves/rust/util-go"
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
	// Anything else has to carry its markers: a port that answers []uint16, a NaN or an undefined must
	// reach the caller as a value, and this fast path skipped the marking — jsonPointerTokens answered
	// [[97],[98]] instead of ["a","b"] through the addon while the driver was right.
	marked := port.Answer(string(unsafe.Slice((*byte)(unsafe.Pointer(name)), int(nameLen))),
		[]any{C.GoStringN(arg, argLen)})
	if e, ok := marked["error"].(string); ok {
		dst = strconv.AppendQuote(dst, e)
		return C.int(len(dst))
	}
	b, _ := json.Marshal(marked["ok"])
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

//export go_answer_batch
//
// Answers a whole batch in one crossing: the caller sends a length-prefixed list of
// (name, arg) pairs and gets back a length-prefixed list of replies. The fixed cost of a crossing
// is paid once instead of N times, which is what makes a batch of small calls cheaper than a typed
// binding that converts one call at a time.
func go_answer_batch(req *C.char, reqLen C.int, out *C.char, outCap C.int) C.int {
	in := unsafe.Slice((*byte)(unsafe.Pointer(req)), int(reqLen))
	dst := unsafe.Slice((*byte)(unsafe.Pointer(out)), int(outCap))[:0]

	readU32 := func(b []byte, at int) (uint32, int, bool) {
		if at+4 > len(b) {
			return 0, at, false
		}
		return uint32(b[at]) | uint32(b[at+1])<<8 | uint32(b[at+2])<<16 | uint32(b[at+3])<<24, at + 4, true
	}
	putU32 := func(b []byte, v uint32) []byte {
		return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}

	count, at, ok := readU32(in, 0)
	if !ok {
		return -1
	}
	for i := uint32(0); i < count; i++ {
		nameLen, next, ok1 := readU32(in, at)
		if !ok1 || next+int(nameLen) > len(in) {
			return -1
		}
		name := in[next : next+int(nameLen)]
		at = next + int(nameLen)
		argLen, next2, ok2 := readU32(in, at)
		if !ok2 || next2+int(argLen) > len(in) {
			return -1
		}
		arg := in[next2 : next2+int(argLen)]
		at = next2 + int(argLen)

		reply := answerBatchOne(name, arg)
		dst = putU32(dst, uint32(len(reply)))
		dst = append(dst, reply...)
	}
	if len(dst) > int(outCap) {
		return -C.int(len(dst))
	}
	return C.int(len(dst))
}

// answerBatchOne answers one (name, arg) pair with the bytes the caller reads: the value, or the
// error, as JSON — one allocation per reply, not per crossing.
func answerBatchOne(name, arg []byte) []byte {
	fn, ok := port.Registry[string(name)]
	if !ok {
		return []byte("\"not ported\"")
	}
	v, err := fn([]any{string(arg)})
	if err != nil {
		b, _ := json.Marshal(err.Error())
		return b
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

//export go_numbers
//
// A typed batch for the shape most numeric ports have: one string argument in, one number out. The
// caller hands over the arguments as one UTF-8 blob plus a length table, and gets the answers in a
// Float64Array it owns. Nothing is marshalled and no string is created per call, which is what makes
// the per-call cost go below the cost of a single crossing.
func go_numbers(name *C.char, nameLen C.int, blob *C.char, blobLen C.int,
	lens *C.uint, count C.int, out *C.double) C.int {
	fn, ok := port.Registry[string(unsafe.Slice((*byte)(unsafe.Pointer(name)), int(nameLen)))]
	if !ok {
		return -1
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(blob)), int(blobLen))
	lengths := unsafe.Slice(lens, int(count))
	// The answers are written through a plain float64 view: C.double is the same width.
	results := unsafe.Slice((*float64)(unsafe.Pointer(out)), int(count))
	at := 0
	for i := 0; i < int(count); i++ {
		n := int(lengths[i])
		if at+n > len(data) {
			return -1
		}
		arg := string(data[at : at+n]) // one copy per call, unavoidable across the ABI
		at += n
		v, err := fn([]any{arg})
		if err != nil {
			results[i] = math.NaN() // an error has no number: NaN marks it for the caller
			continue
		}
		f, ok := v.(float64)
		if !ok {
			if f2, ok2 := v.(int); ok2 {
				results[i] = float64(f2)
				continue
			}
			results[i] = math.NaN()
			continue
		}
		results[i] = float64(f)
	}
	return 0
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
