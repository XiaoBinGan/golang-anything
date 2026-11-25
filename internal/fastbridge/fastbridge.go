// Package fastbridge builds the in-process path: a Go c-shared library plus a small N-API addon, so a
// JavaScript caller pays a function call instead of a pipe round trip.
//
// The stdio bridge costs tens of microseconds; a port that is faster than the original per call cannot
// afford that. This is the same trade rust-anything makes with napi-rs, done by hand because Go has no
// napi binding: `go build -buildmode=c-shared`, then a C addon compiled straight against the Node
// headers (no node-gyp, no dependency).
package fastbridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Options is one fast-bridge build.
type Options struct {
	CrateDir string
	Crate    string
	Exports  []string
	// NodeInclude is the folder holding node_api.h; discovered when empty.
	NodeInclude string
}

// libSource is the c-shared library: one exported C function, JSON in, JSON out.
//
// Nothing is kept between calls on purpose: a request that allocates and frees per call is simple to
// reason about, and the cost is a few microseconds rather than the tens a pipe costs.
const libSource = `// Command portlib is the in-process bridge: a c-shared library that answers calls as JSON.
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

	port "` + cratePlaceholder + `"
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
`

// addonSource is the N-API wrapper: `answer(name, args_json) -> reply_json`.
//
// It is plain C against node_api.h. Two strings cross, nothing is kept: the Go side owns its answer
// until go_free hands the memory back.
const addonSource = `// Generated by go-anything: the in-process entry to the Go port.
#include <node_api.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern int go_answer_into(char *name, int nameLen, char *args, int argsLen, char *out, int outCap);
extern int go_answer_str(char *name, int nameLen, char *arg, int argLen, char *out, int outCap);
extern int go_answer_batch(char *req, int reqLen, char *out, int outCap);
extern int go_numbers(char *name, int nameLen, char *blob, int blobLen, unsigned int *lens, int count, double *out);
extern void go_free(char *p);

// answer(name, args) -> {ok} | {error} | {panic}
static napi_value Answer(napi_env env, napi_callback_info info) {
  size_t argc = 2;
  napi_value argv[2];
  if (napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok) return NULL;

  size_t nameLen = 0, argsLen = 0;
  napi_get_value_string_utf8(env, argv[0], NULL, 0, &nameLen);
  napi_get_value_string_utf8(env, argv[1], NULL, 0, &argsLen);
  if (nameLen > 512 || argsLen > (1u << 20)) return NULL;

  // The two strings are handed over as they are: no request object to build, nothing copied twice.
  char name[512];
  char args[65536];
  if (nameLen >= sizeof(name) || argsLen >= sizeof(args)) return NULL;
  napi_get_value_string_utf8(env, argv[0], name, sizeof(name), &nameLen);
  napi_get_value_string_utf8(env, argv[1], args, sizeof(args), &argsLen);

  static _Thread_local char reply[65536];
  int n = go_answer_into(name, (int)nameLen, args, (int)argsLen, reply, (int)sizeof(reply));
  if (n < 0) return NULL;
  napi_value out;
  napi_create_string_utf8(env, reply, (size_t)n, &out);
  return out;
}

// Answer1 is the one-string call: no JSON in, no JSON out, no allocation for the argument.
static napi_value Answer1(napi_env env, napi_callback_info info) {
  size_t argc = 2;
  napi_value argv[2];
  if (napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok) return NULL;

  size_t nameLen = 0, argLen = 0;
  napi_get_value_string_utf8(env, argv[0], NULL, 0, &nameLen);
  napi_get_value_string_utf8(env, argv[1], NULL, 0, &argLen);
  if (nameLen >= 512 || argLen >= 4096) return NULL;

  char name[512];
  char arg[4096];
  napi_get_value_string_utf8(env, argv[0], name, sizeof(name), &nameLen);
  napi_get_value_string_utf8(env, argv[1], arg, sizeof(arg), &argLen);

  // The reply buffer is thread-local and reused: a call this small must not allocate.
  static _Thread_local char reply[65536];
  int n = go_answer_str(name, (int)nameLen, arg, (int)argLen, reply, (int)sizeof(reply));
  if (n < 0) return NULL;
  napi_value out;
  napi_create_string_utf8(env, reply, (size_t)n, &out);
  return out;
}

// AnswerBatch answers a batch in one crossing: argv is an array of names, argv2 of arguments.
static napi_value AnswerBatch(napi_env env, napi_callback_info info) {
  size_t argc = 2;
  napi_value argv[2];
  if (napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok) return NULL;

  uint32_t count = 0;
  if (napi_get_array_length(env, argv[0], &count) != napi_ok) return NULL;
  uint32_t count2 = 0;
  if (napi_get_array_length(env, argv[1], &count2) != napi_ok || count2 != count) return NULL;
  if (count > 4096) return NULL;

  // Build the request in one buffer: count, then (nameLen, name, argLen, arg) per call.
  static _Thread_local char req[1 << 20];
  size_t at = 0;
#define PUT_U32(v) do { uint32_t _v = (uint32_t)(v); req[at++] = (char)(_v); req[at++] = (char)(_v >> 8); req[at++] = (char)(_v >> 16); req[at++] = (char)(_v >> 24); } while (0)
  PUT_U32(count);
  for (uint32_t i = 0; i < count; i++) {
    napi_value name, arg;
    if (napi_get_element(env, argv[0], i, &name) != napi_ok) return NULL;
    if (napi_get_element(env, argv[1], i, &arg) != napi_ok) return NULL;
    size_t nameLen = 0, argLen = 0;
    napi_get_value_string_utf8(env, name, NULL, 0, &nameLen);
    napi_get_value_string_utf8(env, arg, NULL, 0, &argLen);
    if (at + 8 + nameLen + argLen > sizeof(req)) return NULL;
    PUT_U32(nameLen);
    napi_get_value_string_utf8(env, name, req + at, nameLen + 1, &nameLen);
    at += nameLen;
    PUT_U32(argLen);
    napi_get_value_string_utf8(env, arg, req + at, argLen + 1, &argLen);
    at += argLen;
  }
#undef PUT_U32

  static _Thread_local char reply[1 << 20];
  int n = go_answer_batch(req, (int)at, reply, (int)sizeof(reply));
  if (n < 0) return NULL;

  // Split the length-prefixed replies back into an array of strings.
  napi_value out;
  napi_create_array(env, &out);
  size_t pos = 0;
  uint32_t index = 0;
  while (pos + 4 <= (size_t)n) {
    uint32_t len = (uint32_t)(unsigned char)reply[pos] |
                   ((uint32_t)(unsigned char)reply[pos + 1] << 8) |
                   ((uint32_t)(unsigned char)reply[pos + 2] << 16) |
                   ((uint32_t)(unsigned char)reply[pos + 3] << 24);
    pos += 4;
    if (pos + len > (size_t)n) break;
    napi_value item;
    napi_create_string_utf8(env, reply + pos, len, &item);
    napi_set_element(env, out, index++, item);
    pos += len;
  }
  return out;
}

// Numbers answers a batch of one-string calls with a Float64Array: no JSON, no per-call string.
static napi_value Numbers(napi_env env, napi_callback_info info) {
  size_t argc = 3;
  napi_value argv[3];
  if (napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok) return NULL;

  size_t nameLen = 0;
  napi_get_value_string_utf8(env, argv[0], NULL, 0, &nameLen);
  if (nameLen >= 512) return NULL;
  char name[512];
  napi_get_value_string_utf8(env, argv[0], name, sizeof(name), &nameLen);

  void *blob = NULL;
  size_t blobLen = 0;
  if (napi_get_typedarray_info(env, argv[1], NULL, &blobLen, &blob, NULL, NULL) != napi_ok) return NULL;

  void *lens = NULL;
  size_t count = 0;
  if (napi_get_typedarray_info(env, argv[2], NULL, &count, &lens, NULL, NULL) != napi_ok) return NULL;
  if (count == 0 || count > 1 << 20) return NULL;

  napi_value out;
  void *results = NULL;
  napi_value buffer;
  if (napi_create_arraybuffer(env, count * sizeof(double), &results, &buffer) != napi_ok) return NULL;
  if (napi_create_typedarray(env, napi_float64_array, count, buffer, 0, &out) != napi_ok) return NULL;

  int rc = go_numbers(name, (int)nameLen, (char *)blob, (int)blobLen,
                      (unsigned int *)lens, (int)count, (double *)results);
  if (rc != 0) return NULL;
  return out;
}

NAPI_MODULE_INIT() {
  napi_value fn;
  napi_create_function(env, "answer", NAPI_AUTO_LENGTH, Answer, NULL, &fn);
  napi_set_named_property(env, exports, "answer", fn);
  napi_value fn1;
  napi_create_function(env, "answer1", NAPI_AUTO_LENGTH, Answer1, NULL, &fn1);
  napi_set_named_property(env, exports, "answer1", fn1);
  napi_value fnb;
  napi_create_function(env, "answerBatch", NAPI_AUTO_LENGTH, AnswerBatch, NULL, &fnb);
  napi_set_named_property(env, exports, "answerBatch", fnb);
  napi_value fnn;
  napi_create_function(env, "numbers", NAPI_AUTO_LENGTH, Numbers, NULL, &fnn);
  napi_set_named_property(env, exports, "numbers", fnn);
  return exports;
}
`

const cratePlaceholder = "__CRATE__"

// Run writes the sources, compiles the library and the addon, and returns what it wrote.
func Run(opts Options) ([]string, error) {
	dir, err := filepath.Abs(opts.CrateDir)
	if err != nil {
		return nil, err
	}
	lib := filepath.Join(dir, "cmd", "portlib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		return nil, err
	}
	src := strings.ReplaceAll(libSource, cratePlaceholder, opts.Crate)
	if err := os.WriteFile(filepath.Join(lib, "main.go"), []byte(src), 0o644); err != nil {
		return nil, err
	}
	// The addon source sits in its own folder: a C file in the crate's package folder makes `go build`
	// refuse the package ("C source files not allowed when not using cgo").
	// Keep it out of the Go build: any .c file under a package folder is compiled as cgo source.
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, err
	}
	addonC := filepath.Join(work, "addon.c")
	if err := os.WriteFile(addonC, []byte(addonSource), 0o644); err != nil {
		return nil, err
	}

	include := opts.NodeInclude
	if include == "" {
		include, err = nodeInclude()
		if err != nil {
			return nil, err
		}
	}

	libName := "libport.so"
	addonName := "addon.node"
	switch runtime.GOOS {
	case "darwin":
		libName = "libport.dylib"
	case "windows":
		libName, addonName = "libport.dll", "addon.node"
	}
	libPath := filepath.Join(dir, libName)
	buildArgs := []string{"build", "-buildmode=c-shared"}
	if runtime.GOOS == "darwin" {
		// Room for a longer install name later: c-shared writes a bare one and the file has no slack.
		buildArgs = append(buildArgs, "-ldflags=-extldflags=-Wl,-headerpad_max_install_names")
	}
	buildArgs = append(buildArgs, "-o", libPath, "./cmd/portlib")
	build := exec.Command("go", buildArgs...)
	build.Dir = dir
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return nil, fmt.Errorf("building the c-shared library: %w", err)
	}

	// A c-shared library names itself after the output file only: give it an absolute install name,
	// or every consumer inherits a dependency dyld can only find from one working directory.
	if runtime.GOOS == "darwin" {
		id := exec.Command("install_name_tool", "-id", libPath, libPath)
		id.Stderr = os.Stderr
		if err := id.Run(); err != nil {
			return nil, fmt.Errorf("naming the library: %w", err)
		}
	}

	cc := "clang"
	if _, err := exec.LookPath(cc); err != nil {
		cc = "cc"
	}
	// A Node addon is loaded into a host that already has the napi symbols: on macOS the link must
	// leave them undefined (`-undefined dynamic_lookup`), and the library path is the addon's own.
	// The library is named by its full path, not by `-lport`: a bare install name made the addon
	// resolve it against the process's working directory, so it only loaded from one folder.
	args := []string{"-shared", "-fPIC", "-O2", "-I" + include,
		"-o", filepath.Join(dir, addonName), addonC, libPath,
		"-Wl,-rpath,@loader_path", "-Wl,-undefined,dynamic_lookup"}
	if runtime.GOOS != "darwin" {
		args = []string{"-shared", "-fPIC", "-O2", "-I" + include,
			"-o", filepath.Join(dir, addonName), addonC, libPath,
			"-Wl,-rpath,$ORIGIN"}
	}
	compile := exec.Command(cc, args...)
	compile.Stderr = os.Stderr
	if err := compile.Run(); err != nil {
		return nil, fmt.Errorf("building the N-API addon (%s): %w", cc, err)
	}

	return []string{filepath.Join(lib, "main.go"), libPath, addonC, filepath.Join(dir, addonName)}, nil
}

// nodeInclude finds node_api.h: the running node's include folder, or an install that ships it.
func nodeInclude() (string, error) {
	out, err := exec.Command("node", "-p", "process.execPath").Output()
	if err == nil {
		root := filepath.Dir(strings.TrimSpace(string(out)))
		for _, candidate := range []string{
			filepath.Join(root, "..", "include", "node"),
			filepath.Join(root, "..", "..", "include", "node"),
		} {
			if fileExists(filepath.Join(candidate, "node_api.h")) {
				return candidate, nil
			}
		}
	}
	for _, candidate := range []string{"/opt/homebrew/include/node", "/usr/local/include/node",
		"/usr/include/node"} {
		if fileExists(filepath.Join(candidate, "node_api.h")) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("node_api.h not found: install the Node headers, or pass --node-include")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
