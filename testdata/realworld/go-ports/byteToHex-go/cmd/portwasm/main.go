// Command portwasm is the in-process port as a WebAssembly reactor.
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o port.wasm ./cmd/portwasm
//
// The caller writes the export name and its JSON arguments into the scratch buffer, calls go_call, and
// reads the reply from the second half of the same buffer. No pipe, no cgo, no N-API — the call crosses
// nothing at all.
package main

import (
	"encoding/json"
	"unsafe"

	port "github.com/XiaoBinGan/golang-anything/testdata/realworld/go-ports/byteToHex-go"
)

const (
	scratchSize = 128 << 10
	replyOffset = 64 << 10
)

// scratch is the shared buffer: [0, 64 KiB) for the request, [64 KiB, 128 KiB) for the reply.
var scratch [scratchSize]byte

//go:wasmexport go_scratch
func goScratch() uint32 {
	return uint32(uintptr(unsafe.Pointer(&scratch[0])))
}

//go:wasmexport go_call
//
// Answers one call: the reply is written at ReplyOffset and its length is returned, or 0 when the
// request does not fit or the port failed in a way it cannot report.
func goCall(nameLen uint32, argsLen uint32) uint32 {
	name := byteString(&scratch[0], nameLen)
	args := byteString(&scratch[nameLen], argsLen)
	reply := port.AnswerJSON(name, args)
	if uint32(len(reply)) > uint32(len(scratch)-replyOffset) {
		return 0
	}
	copy(scratch[replyOffset:], reply)
	return uint32(len(reply))
}



//go:wasmexport go_call1
//
// The one-string call: the argument is the string itself, not JSON, and the reply is the value's JSON
// only (no {"ok":…} envelope). Most ports are this shape, and skipping the encode is most of the win.
func goCall1(nameLen uint32, argLen uint32) uint32 {
	name := byteString(&scratch[0], nameLen)
	arg := byteString(&scratch[nameLen], argLen)
	fn, ok := port.Registry[name]
	if !ok {
		return 0
	}
	v, err := fn([]any{arg})
	var reply []byte
	if err != nil {
		reply, _ = json.Marshal(err.Error())
	} else {
		b, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		reply = b
	}
	if uint32(len(reply)) > uint32(len(scratch)-replyOffset) {
		return 0
	}
	copy(scratch[replyOffset:], reply)
	return uint32(len(reply))
}

// byteString reads n bytes as a string without copying: the bytes live in this module's memory and the
// port only reads them.
func byteString(p *byte, n uint32) string {
	if n == 0 {
		return ""
	}
	return unsafe.String(p, int(n))
}

func main() {}
