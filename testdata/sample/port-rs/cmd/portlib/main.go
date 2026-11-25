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
	"sync"
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
	buf := replyBuf.Get().(*[]byte)
	defer replyBuf.Put(buf)
	out, err := json.Marshal(port.Answer(string(name), args))
	if err != nil {
		return []byte("{\"error\":\"reply\"}")
	}
	*buf = out
	return out
}

var replyBuf = sync.Pool{New: func() any {
	b := make([]byte, 0, 4096)
	return &b
}}

func main() {}
