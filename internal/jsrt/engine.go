package jsrt

import (
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// jsrt.wasm is QuickJS-NG + csrc/glue.c, built by .github/workflows/jsrt.yml.
//
//go:embed jsrt.wasm
var wasmBin []byte

const excBit = 0x80000000

// The compiled engine code is shared by every Runtime in the process
// through one compilation cache (on disk when Options.CacheDir is set).
var (
	cacheMu sync.Mutex
	caches  = map[string]wazero.CompilationCache{}
)

func compilationCache(dir string) wazero.CompilationCache {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if c, ok := caches[dir]; ok {
		return c
	}
	var c wazero.CompilationCache
	if dir != "" {
		if dc, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
			c = dc
		}
	}
	if c == nil {
		c = wazero.NewCompilationCache()
	}
	caches[dir] = c
	return c
}

type engine struct {
	r   *Runtime
	ctx context.Context
	wrt wazero.Runtime
	mod api.Module
	mem api.Memory

	fns    map[string]api.Function
	stacks [][]uint64 // free call stacks (calls nest through host_call)

	scratch    uint32 // wasm buffer for keys and small strings
	scratchCap uint32

	// pending unhandled rejections: promise handle → reason handle
	unhandled map[uint32]uint32
	dirty     bool // a root or the document needs flushing
}

// engineWasm is the engine binary: the embedded one, or the file named by
// CLACK_JSRT_WASM (a developer override for benchmarking engine builds).
var engineWasm = sync.OnceValue(func() []byte {
	if p := os.Getenv("CLACK_JSRT_WASM"); p != "" {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return b
		}
	}
	return wasmBin
})

func newEngine(r *Runtime) (*engine, error) {
	wasmBin := engineWasm()
	if len(wasmBin) == 0 {
		return nil, errors.New("jsrt: engine wasm missing (internal/jsrt/jsrt.wasm not built)")
	}
	ctx := context.Background()
	// Each engine has its own wazero runtime (the "jr" host module is per
	// instance); compiled code comes from the shared cache. The wasm memory
	// is capped: QuickJS's own limit covers the JS heap, the cap the rest.
	// (Not WithCloseOnContextDone: its per-call goroutine and per-loop
	// checks made React renders 2–3× slower; see Runtime.watchdog.)
	wrt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCompilationCache(compilationCache(r.opts.CacheDir)).
		WithMemoryLimitPages(memoryLimitPages(r.opts.MemoryLimit)))
	e := &engine{r: r, ctx: ctx, wrt: wrt, fns: map[string]api.Function{}, unhandled: map[uint32]uint32{}}
	ok := false
	defer func() {
		if !ok {
			_ = wrt.Close(ctx)
		}
	}()
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wrt); err != nil {
		return nil, err
	}
	cm, err := wrt.CompileModule(ctx, wasmBin)
	if err != nil {
		return nil, fmt.Errorf("jsrt: compile engine: %w", err)
	}

	i32, i64 := api.ValueTypeI32, api.ValueTypeI64
	hb := wrt.NewHostModuleBuilder("jr")
	hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(e.hostCall), []api.ValueType{i32, i32, i32, i32, i32}, []api.ValueType{i32}).Export("host_call")
	hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, st []uint64) {
		if r.interrupted() {
			st[0] = 1
		} else {
			st[0] = 0
		}
	}), nil, []api.ValueType{i32}).Export("host_interrupt")
	hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(e.hostModule), []api.ValueType{i32, i32}, []api.ValueType{i64}).Export("host_module")
	hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(e.hostRejection), []api.ValueType{i32, i32, i32}, nil).Export("host_rejection")
	hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, st []uint64) {
		// a host function was garbage collected (called from the GC; must not re-enter JS)
		delete(r.hosts, uint32(st[0]))
	}), []api.ValueType{i32}, nil).Export("host_free")
	if _, err := hb.Instantiate(ctx); err != nil {
		return nil, fmt.Errorf("jsrt: host module: %w", err)
	}

	mc := wazero.NewModuleConfig().
		WithName("qjs").
		WithStartFunctions("_initialize").
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().
		WithRandSource(cryptoRand{})
	mod, err := wrt.InstantiateModule(ctx, cm, mc)
	if err != nil {
		return nil, fmt.Errorf("jsrt: instantiate engine: %w", err)
	}
	e.mod = mod
	e.mem = mod.Memory()
	for _, name := range []string{
		"jr_init", "jr_release", "jr_release_many", "jr_dup", "jr_malloc", "jr_free", "jr_new_func", "jr_gc", "jr_memory",
		"jr_eval", "jr_run_jobs", "jr_has_jobs", "jr_global", "jr_null", "jr_new_bool", "jr_new_number", "jr_new_string",
		"jr_new_object", "jr_new_array", "jr_new_bytes", "jr_parse_json", "jr_new_error", "jr_typeof", "jr_is", "jr_truthy",
		"jr_to_float", "jr_to_string", "jr_free_cstr", "jr_exception", "jr_stringify", "jr_get", "jr_get_idx", "jr_get_val",
		"jr_set", "jr_set_idx", "jr_del", "jr_has", "jr_call", "jr_construct", "jr_strict_eq", "jr_bytes", "jr_new_promise",
		"jr_promise_state", "jr_promise_result",
	} {
		f := mod.ExportedFunction(name)
		if f == nil {
			return nil, fmt.Errorf("jsrt: engine lacks export %s", name)
		}
		e.fns[name] = f
	}
	// optional exports (older engine builds lack them)
	for _, name := range []string{"jr_compile", "jr_free_js", "jr_eval_bytecode", "jr_set_gc_threshold", "jr_gc_threshold"} {
		if f := mod.ExportedFunction(name); f != nil {
			e.fns[name] = f
		}
	}
	if rc := int32(e.call("jr_init", uint64(r.opts.MemoryLimit), 7<<20)); rc != 0 {
		return nil, fmt.Errorf("jsrt: jr_init failed (%d)", rc)
	}
	ok = true
	return e, nil
}

// wasmHeadroom is wasm memory allowed beyond the JS heap limit: the C
// stack, the handle table and buffers Go copies in (fetch bodies, sources).
const wasmHeadroom = 512 << 20

// memoryLimitPages is the wasm memory cap in 64 KiB pages for a JS heap
// limit of heap bytes (at most wasm32's 4 GiB).
func memoryLimitPages(heap int) uint32 {
	n := (uint64(heap) + wasmHeadroom) >> 16
	if heap <= 0 || n > 65536 {
		n = 65536
	}
	return uint32(n)
}

func (e *engine) close() {
	if e.mod != nil {
		_ = e.mod.Close(e.ctx)
		e.mod = nil
	}
	if e.wrt != nil {
		_ = e.wrt.Close(e.ctx)
		e.wrt = nil
	}
}

// call invokes an export. Panics from wazero (traps) become Go panics with
// context; the engine is unusable afterwards.
func (e *engine) call(name string, args ...uint64) uint64 {
	f := e.fns[name]
	var st []uint64
	if n := len(e.stacks); n > 0 {
		st = e.stacks[n-1]
		e.stacks = e.stacks[:n-1]
	} else {
		st = make([]uint64, 8)
	}
	copy(st, args)
	if err := f.CallWithStack(e.ctx, st); err != nil {
		panic(fmt.Errorf("jsrt: engine trap in %s: %w", name, err))
	}
	v := st[0]
	e.stacks = append(e.stacks, st)
	return v
}

// ---- memory helpers ----

func (e *engine) malloc(n int) uint32 {
	p := uint32(e.call("jr_malloc", uint64(n)))
	if p == 0 {
		// out of wasm memory: the engine can't go on (afterPanic closes it)
		panic(fmt.Errorf("jsrt: engine trap in jr_malloc: out of memory (%d bytes)", n))
	}
	return p
}
func (e *engine) free(p uint32) { e.call("jr_free", uint64(p)) }

// tmpBytes copies b (plus a NUL) into wasm memory. The returned free must
// be called once the callee is done with it.
func (e *engine) tmpBytes(b []byte) (ptr uint32, free func()) {
	p := e.malloc(len(b) + 1)
	e.mem.Write(p, b)
	e.mem.WriteByte(p+uint32(len(b)), 0)
	return p, func() { e.free(p) }
}

// key writes a short string into the scratch buffer (the glue copies keys
// into atoms immediately, so nested calls may reuse it afterwards).
func (e *engine) key(s string) (uint32, uint32) {
	n := uint32(len(s)) + 1
	if n > e.scratchCap {
		if e.scratch != 0 {
			e.free(e.scratch)
		}
		c := uint32(256)
		for c < n {
			c *= 2
		}
		e.scratch, e.scratchCap = e.malloc(int(c)), c
	}
	e.mem.WriteString(e.scratch, s)
	e.mem.WriteByte(e.scratch+uint32(len(s)), 0)
	return e.scratch, uint32(len(s))
}

func (e *engine) read(ptr, n uint32) []byte {
	b, ok := e.mem.Read(ptr, n)
	if !ok {
		return nil
	}
	return append([]byte(nil), b...)
}

func unpack(pl uint64) (uint32, uint32) { return uint32(pl >> 32), uint32(pl) }

// ---- results & errors ----

// res turns a returned handle into (handle, error).
func (e *engine) res(h uint32) (uint32, error) {
	if h&excBit == 0 {
		return h, nil
	}
	return 0, e.takeError(h &^ excBit)
}

// takeError converts the thrown value at h to a Go error and frees h.
func (e *engine) takeError(h uint32) error {
	err := e.toError(h)
	e.release(h)
	return err
}

// toError builds a *JSError from a thrown value (does not free h).
func (e *engine) toError(h uint32) error {
	if e.typeOf(h) == "object" || e.typeOf(h) == "function" {
		get := func(k string) string {
			x, err := e.get(h, k)
			if err != nil || x == 0 {
				return ""
			}
			defer e.release(x)
			if e.typeOf(x) != "string" {
				return ""
			}
			return e.toString(x)
		}
		je := &JSError{Name: get("name"), Message: get("message"), Stack: get("stack")}
		if je.Name == "" && je.Message == "" {
			je.Message = e.toString(h)
		}
		if je.Message == "interrupted" || strings.Contains(je.Message, "interrupted") && je.Name == "InternalError" {
			return fmt.Errorf("%w: %v", ErrTimeout, je)
		}
		return je
	}
	return &JSError{Message: "Uncaught " + e.toString(h)}
}

func (e *engine) release(h uint32) {
	if h != 0 && e.mod != nil {
		e.call("jr_release", uint64(h))
	}
}

// ---- value ops (JS thread) ----

var typeNames = [...]string{"undefined", "null", "boolean", "number", "string", "symbol", "bigint", "object", "function"}

func (e *engine) typeOf(h uint32) string {
	if h == 0 {
		return "undefined"
	}
	t := int32(e.call("jr_typeof", uint64(h)))
	if t < 0 || int(t) >= len(typeNames) {
		return "undefined"
	}
	return typeNames[t]
}

const (
	isArray   = 1
	isPromise = 2
	isError   = 3
)

func (e *engine) is(h uint32, what int) bool {
	return h != 0 && e.call("jr_is", uint64(h), uint64(what)) != 0
}

func (e *engine) truthy(h uint32) bool { return h != 0 && e.call("jr_truthy", uint64(h)) != 0 }

func (e *engine) toFloat(h uint32) float64 {
	if h == 0 {
		return math.NaN()
	}
	return math.Float64frombits(e.call("jr_to_float", uint64(h)))
}

func (e *engine) toString(h uint32) string {
	if h == 0 {
		return "undefined"
	}
	pl := e.call("jr_to_string", uint64(h))
	if pl == 0 {
		x := uint32(e.call("jr_exception")) &^ excBit
		e.release(x)
		return ""
	}
	p, n := unpack(pl)
	b, _ := e.mem.Read(p, n)
	s := string(b)
	e.call("jr_free_cstr", uint64(p))
	return s
}

func (e *engine) global() uint32 { return uint32(e.call("jr_global")) }
func (e *engine) null() uint32   { return uint32(e.call("jr_null")) }
func (e *engine) newBool(b bool) uint32 {
	x := uint64(0)
	if b {
		x = 1
	}
	return uint32(e.call("jr_new_bool", x))
}
func (e *engine) newNumber(f float64) uint32 {
	return uint32(e.call("jr_new_number", math.Float64bits(f)))
}
func (e *engine) newString(s string) uint32 {
	if len(s) < 4096 {
		p, n := e.key(s)
		h, _ := e.res(uint32(e.call("jr_new_string", uint64(p), uint64(n))))
		return h
	}
	p, free := e.tmpBytes([]byte(s))
	defer free()
	h, _ := e.res(uint32(e.call("jr_new_string", uint64(p), uint64(len(s)))))
	return h
}
func (e *engine) newObject() uint32 { return uint32(e.call("jr_new_object")) }
func (e *engine) newArray() uint32  { return uint32(e.call("jr_new_array")) }
func (e *engine) newBytes(b []byte) uint32 {
	p, free := e.tmpBytes(b)
	defer free()
	h, _ := e.res(uint32(e.call("jr_new_bytes", uint64(p), uint64(len(b)))))
	return h
}

func (e *engine) parse(b []byte) (uint32, error) {
	p, free := e.tmpBytes(b)
	defer free()
	return e.res(uint32(e.call("jr_parse_json", uint64(p), uint64(len(b)))))
}

func (e *engine) newError(err error) uint32 {
	name := "Error"
	msg := err.Error()
	var je *JSError
	if errors.As(err, &je) {
		if je.Name != "" {
			name = je.Name
		}
		msg = je.Message
	}
	nb := []byte(name)
	np, nfree := e.tmpBytes(nb)
	defer nfree()
	mp, mfree := e.tmpBytes([]byte(msg))
	defer mfree()
	return uint32(e.call("jr_new_error", uint64(np), uint64(len(nb)), uint64(mp), uint64(len(msg))))
}

func (e *engine) get(o uint32, k string) (uint32, error) {
	p, n := e.key(k)
	return e.res(uint32(e.call("jr_get", uint64(o), uint64(p), uint64(n))))
}

func (e *engine) getIndex(o, i uint32) (uint32, error) {
	return e.res(uint32(e.call("jr_get_idx", uint64(o), uint64(i))))
}

func (e *engine) setResult(r uint32) error {
	if r&excBit != 0 {
		return e.takeError(r &^ excBit)
	}
	return nil
}

func (e *engine) set(o uint32, k string, v uint32) error {
	p, n := e.key(k)
	return e.setResult(uint32(e.call("jr_set", uint64(o), uint64(p), uint64(n), uint64(v))))
}

func (e *engine) setIndex(o, i, v uint32) error {
	return e.setResult(uint32(e.call("jr_set_idx", uint64(o), uint64(i), uint64(v))))
}

func (e *engine) del(o uint32, k string) error {
	p, n := e.key(k)
	return e.setResult(uint32(e.call("jr_del", uint64(o), uint64(p), uint64(n))))
}

func (e *engine) has(o uint32, k string) bool {
	p, n := e.key(k)
	return e.call("jr_has", uint64(o), uint64(p), uint64(n)) == 1
}

func (e *engine) keys(o uint32) []string {
	g := e.global()
	defer e.release(g)
	obj, _ := e.get(g, "Object")
	defer e.release(obj)
	fn, _ := e.get(obj, "keys")
	defer e.release(fn)
	arr, err := e.call2("jr_call", fn, obj, []uint32{o})
	if err != nil {
		return nil
	}
	defer e.release(arr)
	js, err := e.stringify(arr)
	if err != nil {
		return nil
	}
	var out []string
	_ = jsonUnmarshal(js, &out)
	return out
}

// argv writes handles to wasm memory for a call.
func (e *engine) argv(hs []uint32) (uint32, func()) {
	if len(hs) == 0 {
		return 0, func() {}
	}
	b := make([]byte, 4*len(hs))
	for i, h := range hs {
		binary.LittleEndian.PutUint32(b[4*i:], h)
	}
	p := e.malloc(len(b))
	e.mem.Write(p, b)
	return p, func() { e.free(p) }
}

func (e *engine) call2(name string, fn, this uint32, args []uint32) (uint32, error) {
	p, free := e.argv(args)
	defer free()
	if name == "jr_construct" {
		return e.res(uint32(e.call(name, uint64(fn), uint64(len(args)), uint64(p))))
	}
	return e.res(uint32(e.call(name, uint64(fn), uint64(this), uint64(len(args)), uint64(p))))
}

func (e *engine) callFn(fn, this uint32, args []uint32) (uint32, error) {
	return e.call2("jr_call", fn, this, args)
}

func (e *engine) construct(fn uint32, args []uint32) (uint32, error) {
	return e.call2("jr_construct", fn, 0, args)
}

func (e *engine) strictEq(a, b uint32) bool { return e.call("jr_strict_eq", uint64(a), uint64(b)) == 1 }

func (e *engine) stringify(h uint32) ([]byte, error) {
	if h == 0 {
		return []byte("null"), nil
	}
	s, err := e.res(uint32(e.call("jr_stringify", uint64(h))))
	if err != nil {
		return nil, err
	}
	if s == 0 {
		return []byte("null"), nil
	}
	defer e.release(s)
	return []byte(e.toString(s)), nil
}

func (e *engine) bytes(h uint32) []byte {
	if h == 0 {
		return nil
	}
	pl := e.call("jr_bytes", uint64(h))
	if pl == 0 {
		return nil
	}
	p, n := unpack(pl)
	return e.read(p, n)
}

func (e *engine) newPromise() (p, resolve, reject uint32) {
	out := e.malloc(8)
	defer e.free(out)
	ph, _ := e.res(uint32(e.call("jr_new_promise", uint64(out))))
	b, _ := e.mem.Read(out, 8)
	return ph, binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:])
}

func (e *engine) promiseState(h uint32) int { return int(int32(e.call("jr_promise_state", uint64(h)))) }
func (e *engine) promiseResult(h uint32) uint32 {
	return uint32(e.call("jr_promise_result", uint64(h)))
}

func (e *engine) newFunc(id uint32, name string) uint32 {
	p, n := e.key(name)
	h, _ := e.res(uint32(e.call("jr_new_func", uint64(id), uint64(p), uint64(n), 0)))
	return h
}

// eval runs a script (module=false) or module source.
func (e *engine) eval(name, src string, module bool) (uint32, error) {
	sp, sfree := e.tmpBytes([]byte(src))
	defer sfree()
	np, nfree := e.tmpBytes([]byte(name))
	defer nfree()
	flags := uint64(0)
	if module {
		flags = 1
	}
	return e.res(uint32(e.call("jr_eval", uint64(sp), uint64(len(src)), uint64(np), uint64(len(name)), flags)))
}

// runJobs drains the microtask queue, logging exceptions thrown by jobs.
func (e *engine) runJobs(js *Ctx) {
	for e.mod != nil {
		r := uint32(e.call("jr_run_jobs"))
		if r&excBit == 0 {
			break
		}
		err := e.takeError(r &^ excBit)
		e.r.logError("uncaught (in job)", err)
		if errors.Is(err, ErrTimeout) {
			break
		}
	}
	e.reportRejections()
}

func (e *engine) reportRejections() {
	if len(e.unhandled) == 0 {
		return
	}
	for p, reason := range e.unhandled {
		delete(e.unhandled, p)
		// window 'unhandledrejection' first; preventDefault() silences it
		report := true
		if js := e.r.js; js != nil {
			if fn := js.Global().Get("__jr").Get("unhandled"); fn.IsFunction() {
				if v, err := fn.Call(e.r.wrap(uint32(e.call("jr_dup", uint64(p)))), e.r.wrap(uint32(e.call("jr_dup", uint64(reason))))); err == nil && v.IsBool() && !v.Bool() {
					report = false
				}
			}
		}
		if report {
			e.r.logError("uncaught (in promise)", e.toError(reason))
		}
		e.release(reason)
		e.release(p)
	}
}

func (e *engine) hasDirty() bool { return e.dirty }

// gcFloorBytes keeps QuickJS's cycle collector from running every few
// hundred KiB of allocation (its threshold resets to 1.5× the live heap
// after each collection); React's fiber garbage is cyclic, so a low
// threshold means a full heap scan every couple of renders.
const gcFloorBytes = 16 << 20

func (e *engine) gcFloor() {
	if e.fns["jr_gc_threshold"] == nil || e.mod == nil {
		return
	}
	if e.call("jr_gc_threshold") < gcFloorBytes {
		e.call("jr_set_gc_threshold", gcFloorBytes)
	}
}

// ---- host imports ----

func (e *engine) hostCall(_ context.Context, _ api.Module, st []uint64) {
	id, thisH, argc, argvP := uint32(st[0]), uint32(st[1]), uint32(st[2]), uint32(st[3])
	r := e.r
	// argv comes from the glue; still, a bad pointer must not panic here
	if uint64(argc)*4 > uint64(e.mem.Size()) {
		st[0] = uint64(e.newError(errors.New("jsrt: bad host call arguments")) | excBit)
		return
	}
	args := make([]Value, argc)
	if argc > 0 {
		b, ok := e.mem.Read(argvP, 4*argc)
		if !ok {
			st[0] = uint64(e.newError(errors.New("jsrt: bad host call arguments")) | excBit)
			return
		}
		for i := range args {
			args[i] = r.wrap(binary.LittleEndian.Uint32(b[4*i:]))
		}
	}
	this := r.wrap(thisH)
	fn := r.hosts[id]
	if fn == nil {
		st[0] = uint64(e.newError(errors.New("jsrt: unknown host function")) | excBit)
		return
	}
	out, err := safeHost(fn, r.js, this, args)
	if err != nil {
		st[0] = uint64(e.newError(err) | excBit)
		return
	}
	v, err := r.js.Value(out)
	if err != nil {
		st[0] = uint64(e.newError(err) | excBit)
		return
	}
	// The glue takes ownership of the returned handle, so hand it a dup.
	if v.h() == 0 {
		st[0] = 0
		return
	}
	st[0] = e.call("jr_dup", uint64(v.h()))
}

func safeHost(fn HostFunc, js *Ctx, this Value, args []Value) (out any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("jsrt: host function panicked: %v", p)
		}
	}()
	return fn(js, this, args)
}

func (e *engine) hostModule(_ context.Context, _ api.Module, st []uint64) {
	nameB, ok := e.mem.Read(uint32(st[0]), uint32(st[1]))
	if !ok {
		st[0] = 0
		return
	}
	name := string(nameB)
	src, ok := e.r.moduleSource(name)
	if !ok {
		st[0] = 0
		return
	}
	p := e.malloc(len(src) + 1)
	e.mem.WriteString(p, src)
	e.mem.WriteByte(p+uint32(len(src)), 0)
	st[0] = uint64(p)<<32 | uint64(len(src))
}

func (e *engine) hostRejection(_ context.Context, _ api.Module, st []uint64) {
	// i32 params: only the low 32 bits of a stack slot are defined
	p, reason, handled := uint32(st[0]), uint32(st[1]), uint32(st[2]) != 0
	if !handled {
		// key by promise identity: the glue hands a fresh handle each time,
		// so compare against what we hold.
		e.unhandled[p] = reason
		return
	}
	for hp, hr := range e.unhandled {
		if e.strictEq(hp, p) {
			delete(e.unhandled, hp)
			e.release(hp)
			e.release(hr)
		}
	}
	e.release(p)
	e.release(reason)
}

// ---- higher level (used by Ctx) ----

func (e *engine) exposeHost(name string, id uint32) {
	fn := e.newFunc(id, name)
	defer e.release(fn)
	g := e.global()
	defer e.release(g)
	cl, _ := e.get(g, "__clack")
	if cl == 0 || e.typeOf(cl) != "object" {
		e.release(cl)
		cl = e.newObject()
		_ = e.set(g, "__clack", cl)
	}
	defer e.release(cl)
	_ = e.set(cl, name, fn)
}

// importModule imports name via dynamic import() and runs microtasks until
// the namespace promise settles.
func (e *engine) importModule(js *Ctx, name string) (uint32, error) {
	q, _ := jsonMarshal(name)
	p, err := e.eval("<import>", "import("+string(q)+")", false)
	if err != nil {
		return 0, err
	}
	defer e.release(p)
	for i := 0; i < 1000; i++ {
		switch e.promiseState(p) {
		case 1:
			return e.promiseResult(p), nil
		case 2:
			x := e.promiseResult(p)
			e.markHandled(p)
			return 0, e.takeError(x)
		}
		before := e.call("jr_has_jobs")
		e.runJobs(js)
		if before == 0 {
			break
		}
	}
	if e.promiseState(p) == 1 {
		return e.promiseResult(p), nil
	}
	return 0, fmt.Errorf("jsrt: import %q is still pending (top-level await on I/O); use Runtime.Import from another goroutine", name)
}

// markHandled drops a rejection we consumed from Go.
func (e *engine) markHandled(p uint32) {
	for hp, hr := range e.unhandled {
		if e.strictEq(hp, p) {
			delete(e.unhandled, hp)
			e.release(hp)
			e.release(hr)
		}
	}
}

// fireTimer runs JS timer id.
func (e *engine) fireTimer(js *Ctx, id int) {
	fn := js.Global().Get("__jr").Get("fire")
	if fn.IsFunction() {
		if _, err := fn.Call(id); err != nil {
			e.r.logError("uncaught", err)
		}
	}
}
