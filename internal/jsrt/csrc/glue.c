// glue.c: the ABI between Go (internal/jsrt/engine.go) and QuickJS-NG,
// compiled to wasm32-wasip1 (reactor) by .github/workflows/jsrt-wasm.yml.
//
// JS values cross the boundary as u32 handles into a table. Handle 0 is
// undefined and is never stored. A returned handle with bit 31 set is an
// exception (the low bits are the handle of the thrown value).
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "quickjs.h"

#define EXPORT(name) __attribute__((export_name(#name)))
#define IMPORT(name) __attribute__((import_module("jr"), import_name(#name)))
#define EXC 0x80000000u

IMPORT(host_call) uint32_t host_call(uint32_t id, uint32_t this_h, uint32_t argc, uint32_t *argv, uint32_t new_target);
IMPORT(host_interrupt) int32_t host_interrupt(void);
IMPORT(host_module) uint64_t host_module(const char *name, uint32_t len);
IMPORT(host_rejection) void host_rejection(uint32_t promise, uint32_t reason, int32_t handled);
IMPORT(host_free) void host_free(uint32_t id);

static JSRuntime *rt;
static JSContext *ctx;

// ---- handle table ----
static JSValue *tab;
static uint32_t tab_len, tab_cap;
static uint32_t *free_list;
static uint32_t free_len, free_cap;

static uint32_t put(JSValue v) {
	if (JS_IsUndefined(v)) return 0;
	uint32_t h;
	if (free_len) {
		h = free_list[--free_len];
	} else {
		if (tab_len == tab_cap) {
			tab_cap = tab_cap ? tab_cap * 2 : 1024;
			tab = realloc(tab, tab_cap * sizeof(JSValue));
		}
		h = tab_len++;
		if (h == 0) { // reserve 0
			tab[0] = JS_UNDEFINED;
			h = tab_len++;
		}
	}
	tab[h] = v;
	return h;
}

static inline JSValue get(uint32_t h) {
	if (h == 0 || h >= tab_len) return JS_UNDEFINED;
	return tab[h];
}

static uint32_t exc(void) {
	JSValue e = JS_GetException(ctx);
	uint32_t h = put(e);
	if (h == 0) h = put(JS_NewString(ctx, "undefined thrown")); // keep "thrown undefined" distinguishable
	return h | EXC;
}

static uint32_t ret(JSValue v) {
	if (JS_IsException(v)) return exc();
	return put(v);
}

EXPORT(jr_release) void jr_release(uint32_t h) {
	if (h == 0 || h >= tab_len) return;
	JS_FreeValue(ctx, tab[h]);
	tab[h] = JS_UNDEFINED;
	if (free_len == free_cap) {
		free_cap = free_cap ? free_cap * 2 : 1024;
		free_list = realloc(free_list, free_cap * sizeof(uint32_t));
	}
	free_list[free_len++] = h;
}

EXPORT(jr_release_many) void jr_release_many(uint32_t *hs, uint32_t n) {
	for (uint32_t i = 0; i < n; i++) jr_release(hs[i]);
}

EXPORT(jr_dup) uint32_t jr_dup(uint32_t h) { return put(JS_DupValue(ctx, get(h))); }

EXPORT(jr_malloc) void *jr_malloc(uint32_t n) { return malloc(n ? n : 1); }
EXPORT(jr_free) void jr_free(void *p) { free(p); }

// ---- host functions ----
static JSValue call_host(JSContext *c, JSValueConst this_val, int argc, JSValueConst *argv, int magic, JSValueConst *data) {
	uint32_t id = (uint32_t)magic;
	uint32_t stackv[8];
	uint32_t *hs = argc <= 8 ? stackv : malloc(sizeof(uint32_t) * argc);
	for (int i = 0; i < argc; i++) hs[i] = put(JS_DupValue(c, argv[i]));
	uint32_t th = put(JS_DupValue(c, this_val));
	// Go owns (and releases) the handles of this and the arguments.
	uint32_t r = host_call(id, th, (uint32_t)argc, hs, 0);
	if (hs != stackv) free(hs);
	if (r & EXC) {
		JSValue e = JS_DupValue(c, get(r & ~EXC));
		jr_release(r & ~EXC);
		return JS_Throw(c, e);
	}
	JSValue v = JS_DupValue(c, get(r));
	jr_release(r);
	return v;
}

// Each host function holds a HostRef object; when the function is garbage
// collected the finalizer tells Go to drop the Go side (host_free).
static JSClassID hostref_class;
static void hostref_finalizer(JSRuntime *r, JSValueConst val) {
	uint32_t id = (uint32_t)(uintptr_t)JS_GetOpaque(val, hostref_class);
	host_free(id);
}
static JSClassDef hostref_def = {"HostRef", .finalizer = hostref_finalizer};

EXPORT(jr_new_func) uint32_t jr_new_func(uint32_t id, const char *name, uint32_t nlen, uint32_t length) {
	JSValue ref = JS_NewObjectClass(ctx, (int)hostref_class);
	if (JS_IsException(ref)) return exc();
	JS_SetOpaque(ref, (void *)(uintptr_t)id);
	JSValue f = JS_NewCFunctionData(ctx, call_host, (int)length, (int)id, 1, &ref);
	JS_FreeValue(ctx, ref);
	if (JS_IsException(f)) return exc();
	if (nlen) {
		JSAtom a = JS_NewAtom(ctx, "name");
		JS_DefinePropertyValue(ctx, f, a, JS_NewStringLen(ctx, name, nlen), JS_PROP_CONFIGURABLE);
		JS_FreeAtom(ctx, a);
	}
	return put(f);
}

// ---- runtime hooks ----
static int interrupt(JSContext *c, void *opaque) { return host_interrupt(); }

static JSModuleDef *load_module(JSContext *c, const char *name, void *opaque) {
	uint64_t pl = host_module(name, (uint32_t)strlen(name));
	if (pl == 0) {
		JS_ThrowReferenceError(c, "could not load module '%s'", name);
		return NULL;
	}
	char *src = (char *)(uintptr_t)(pl >> 32);
	uint32_t len = (uint32_t)pl;
	JSValue fv = JS_Eval(c, src, len, name, JS_EVAL_TYPE_MODULE | JS_EVAL_FLAG_COMPILE_ONLY);
	free(src);
	if (JS_IsException(fv)) return NULL;
	JSModuleDef *m = JS_VALUE_GET_PTR(fv);
	JS_FreeValue(c, fv);
	return m;
}

static void rejection(JSContext *c, JSValueConst promise, JSValueConst reason, bool is_handled, void *opaque) {
	host_rejection(put(JS_DupValue(c, promise)), put(JS_DupValue(c, reason)), is_handled ? 1 : 0);
}

EXPORT(jr_init) int32_t jr_init(uint32_t mem_limit, uint32_t stack_size) {
	rt = JS_NewRuntime();
	if (!rt) return -1;
	if (mem_limit) JS_SetMemoryLimit(rt, mem_limit);
	JS_SetMaxStackSize(rt, stack_size);
	JS_SetInterruptHandler(rt, interrupt, NULL);
	JS_SetModuleLoaderFunc(rt, NULL, load_module, NULL);
	JS_SetHostPromiseRejectionTracker(rt, rejection, NULL);
	JS_NewClassID(rt, &hostref_class);
	if (JS_NewClass(rt, hostref_class, &hostref_def) < 0) return -3;
	ctx = JS_NewContext(rt);
	if (!ctx) return -2;
	put(JS_NewInt32(ctx, 0)); // make sure the table exists; handle 1 is a scratch 0
	return 0;
}

EXPORT(jr_gc) void jr_gc(void) { JS_RunGC(rt); }

EXPORT(jr_memory) uint64_t jr_memory(void) {
	JSMemoryUsage u;
	JS_ComputeMemoryUsage(rt, &u);
	return (uint64_t)u.memory_used_size;
}

// ---- eval & modules ----
// flags: 0 script (sloppy), 1 module (returns the module's evaluation promise)
EXPORT(jr_eval) uint32_t jr_eval(const char *src, uint32_t len, const char *name, uint32_t nlen, uint32_t flags) {
	char nbuf[256];
	if (nlen >= sizeof(nbuf)) nlen = sizeof(nbuf) - 1;
	memcpy(nbuf, name, nlen);
	nbuf[nlen] = 0;
	int f = flags & 1 ? JS_EVAL_TYPE_MODULE : JS_EVAL_TYPE_GLOBAL;
	return ret(JS_Eval(ctx, src, len, nbuf, f));
}

// jr_run_jobs runs all pending Promise jobs. Returns how many ran, or an
// exception handle (bit 31) when a job threw; call again to continue.
EXPORT(jr_run_jobs) uint32_t jr_run_jobs(void) {
	JSContext *c1;
	uint32_t n = 0;
	for (;;) {
		int r = JS_ExecutePendingJob(rt, &c1);
		if (r == 0) return n;
		if (r < 0) return exc();
		n++;
	}
}

EXPORT(jr_has_jobs) int32_t jr_has_jobs(void) { return JS_IsJobPending(rt) ? 1 : 0; }

// ---- values ----
EXPORT(jr_global) uint32_t jr_global(void) { return put(JS_GetGlobalObject(ctx)); }
EXPORT(jr_null) uint32_t jr_null(void) { return put(JS_NULL); }
EXPORT(jr_new_bool) uint32_t jr_new_bool(int32_t b) { return put(JS_NewBool(ctx, b)); }
EXPORT(jr_new_number) uint32_t jr_new_number(double d) { return put(JS_NewFloat64(ctx, d)); }
EXPORT(jr_new_string) uint32_t jr_new_string(const char *s, uint32_t len) { return ret(JS_NewStringLen(ctx, s, len)); }
EXPORT(jr_new_object) uint32_t jr_new_object(void) { return put(JS_NewObject(ctx)); }
EXPORT(jr_new_array) uint32_t jr_new_array(void) { return put(JS_NewArray(ctx)); }
EXPORT(jr_new_bytes) uint32_t jr_new_bytes(const uint8_t *p, uint32_t len) {
	return ret(JS_NewUint8ArrayCopy(ctx, p, len));
}
EXPORT(jr_parse_json) uint32_t jr_parse_json(const char *s, uint32_t len) {
	// JS_ParseJSON needs a NUL-terminated buffer; Go always passes one.
	return ret(JS_ParseJSON(ctx, s, len, "<json>"));
}
EXPORT(jr_new_error) uint32_t jr_new_error(const char *name, uint32_t nlen, const char *msg, uint32_t mlen) {
	JSValue e = JS_NewError(ctx);
	JS_SetPropertyStr(ctx, e, "message", JS_NewStringLen(ctx, msg, mlen));
	if (nlen) JS_SetPropertyStr(ctx, e, "name", JS_NewStringLen(ctx, name, nlen));
	return put(e);
}

// typeof codes: 0 undefined 1 null 2 boolean 3 number 4 string 5 symbol 6 bigint 7 object 8 function
EXPORT(jr_typeof) int32_t jr_typeof(uint32_t h) {
	JSValue v = get(h);
	if (JS_IsUndefined(v)) return 0;
	if (JS_IsNull(v)) return 1;
	if (JS_IsBool(v)) return 2;
	if (JS_IsNumber(v)) return 3;
	if (JS_IsString(v)) return 4;
	if (JS_IsSymbol(v)) return 5;
	if (JS_IsBigInt(v)) return 6;
	if (JS_IsFunction(ctx, v)) return 8;
	return 7;
}

// what: 1 array, 2 promise, 3 error
EXPORT(jr_is) int32_t jr_is(uint32_t h, int32_t what) {
	JSValue v = get(h);
	switch (what) {
	case 1: return JS_IsArray(v) ? 1 : 0;
	case 2: return JS_IsPromise(v) ? 1 : 0;
	case 3: return JS_IsError(v) ? 1 : 0;
	}
	return 0;
}

EXPORT(jr_truthy) int32_t jr_truthy(uint32_t h) { return JS_ToBool(ctx, get(h)) > 0; }

EXPORT(jr_to_float) double jr_to_float(uint32_t h) {
	double d;
	if (JS_ToFloat64(ctx, &d, get(h)) < 0) {
		JS_FreeValue(ctx, JS_GetException(ctx));
		return __builtin_nan("");
	}
	return d;
}

// jr_to_string returns ptr<<32|len of a UTF-8 copy (free with jr_free_cstr),
// or 0 with the exception pending (fetch it with jr_exception).
EXPORT(jr_to_string) uint64_t jr_to_string(uint32_t h) {
	size_t len;
	const char *s = JS_ToCStringLen(ctx, &len, get(h));
	if (!s) return 0;
	return ((uint64_t)(uintptr_t)s << 32) | (uint64_t)len;
}
EXPORT(jr_free_cstr) void jr_free_cstr(const char *s) { JS_FreeCString(ctx, s); }
EXPORT(jr_exception) uint32_t jr_exception(void) { return exc(); }

// jr_stringify returns a string handle (or exception); undefined → 0.
EXPORT(jr_stringify) uint32_t jr_stringify(uint32_t h) {
	return ret(JS_JSONStringify(ctx, get(h), JS_UNDEFINED, JS_UNDEFINED));
}

static JSAtom atom(const char *k, uint32_t klen) { return JS_NewAtomLen(ctx, k, klen); }

EXPORT(jr_get) uint32_t jr_get(uint32_t o, const char *k, uint32_t klen) {
	JSAtom a = atom(k, klen);
	JSValue v = JS_GetProperty(ctx, get(o), a);
	JS_FreeAtom(ctx, a);
	return ret(v);
}
EXPORT(jr_get_idx) uint32_t jr_get_idx(uint32_t o, uint32_t i) { return ret(JS_GetPropertyUint32(ctx, get(o), i)); }
EXPORT(jr_get_val) uint32_t jr_get_val(uint32_t o, uint32_t k) {
	JSAtom a = JS_ValueToAtom(ctx, get(k));
	if (a == JS_ATOM_NULL) return exc();
	JSValue v = JS_GetProperty(ctx, get(o), a);
	JS_FreeAtom(ctx, a);
	return ret(v);
}

// set/del/has return 0 ok, 1 false, or an exception handle (bit 31).
EXPORT(jr_set) uint32_t jr_set(uint32_t o, const char *k, uint32_t klen, uint32_t v) {
	JSAtom a = atom(k, klen);
	int r = JS_SetProperty(ctx, get(o), a, JS_DupValue(ctx, get(v)));
	JS_FreeAtom(ctx, a);
	return r < 0 ? exc() : (r ? 0 : 1);
}
EXPORT(jr_set_idx) uint32_t jr_set_idx(uint32_t o, uint32_t i, uint32_t v) {
	int r = JS_SetPropertyUint32(ctx, get(o), i, JS_DupValue(ctx, get(v)));
	return r < 0 ? exc() : (r ? 0 : 1);
}
EXPORT(jr_del) uint32_t jr_del(uint32_t o, const char *k, uint32_t klen) {
	JSAtom a = atom(k, klen);
	int r = JS_DeleteProperty(ctx, get(o), a, 0);
	JS_FreeAtom(ctx, a);
	return r < 0 ? exc() : (r ? 0 : 1);
}
EXPORT(jr_has) int32_t jr_has(uint32_t o, const char *k, uint32_t klen) {
	if (!JS_IsObject(get(o))) return 0;
	JSAtom a = atom(k, klen);
	int r = JS_HasProperty(ctx, get(o), a);
	JS_FreeAtom(ctx, a);
	if (r < 0) {
		JS_FreeValue(ctx, JS_GetException(ctx));
		return 0;
	}
	return r;
}

EXPORT(jr_call) uint32_t jr_call(uint32_t f, uint32_t this_h, uint32_t argc, const uint32_t *argv) {
	JSValue stackv[8];
	JSValue *av = argc <= 8 ? stackv : malloc(sizeof(JSValue) * argc);
	for (uint32_t i = 0; i < argc; i++) av[i] = get(argv[i]);
	JSValue r = JS_Call(ctx, get(f), get(this_h), (int)argc, av);
	if (av != stackv) free(av);
	return ret(r);
}

EXPORT(jr_construct) uint32_t jr_construct(uint32_t f, uint32_t argc, const uint32_t *argv) {
	JSValue stackv[8];
	JSValue *av = argc <= 8 ? stackv : malloc(sizeof(JSValue) * argc);
	for (uint32_t i = 0; i < argc; i++) av[i] = get(argv[i]);
	JSValue r = JS_CallConstructor(ctx, get(f), (int)argc, av);
	if (av != stackv) free(av);
	return ret(r);
}

EXPORT(jr_strict_eq) int32_t jr_strict_eq(uint32_t a, uint32_t b) { return JS_IsStrictEqual(ctx, get(a), get(b)) ? 1 : 0; }

// jr_bytes returns ptr<<32|len of an ArrayBuffer / TypedArray / DataView's
// bytes inside wasm memory (valid until the next JS call), or 0.
EXPORT(jr_bytes) uint64_t jr_bytes(uint32_t h) {
	JSValue v = get(h);
	size_t len = 0;
	uint8_t *p = JS_GetArrayBuffer(ctx, &len, v);
	if (p) return ((uint64_t)(uintptr_t)p << 32) | (uint64_t)len;
	JS_FreeValue(ctx, JS_GetException(ctx));
	size_t off = 0, blen = 0, bpe = 0;
	JSValue ab = JS_GetTypedArrayBuffer(ctx, v, &off, &blen, &bpe);
	if (JS_IsException(ab)) {
		JS_FreeValue(ctx, JS_GetException(ctx));
		// DataView: read .buffer/.byteOffset/.byteLength
		JSValue b = JS_GetPropertyStr(ctx, v, "buffer");
		p = JS_GetArrayBuffer(ctx, &len, b);
		if (!p) {
			JS_FreeValue(ctx, JS_GetException(ctx));
			JS_FreeValue(ctx, b);
			return 0;
		}
		uint32_t o = 0, l = 0;
		JSValue ov = JS_GetPropertyStr(ctx, v, "byteOffset"), lv = JS_GetPropertyStr(ctx, v, "byteLength");
		JS_ToUint32(ctx, &o, ov);
		JS_ToUint32(ctx, &l, lv);
		JS_FreeValue(ctx, ov);
		JS_FreeValue(ctx, lv);
		JS_FreeValue(ctx, b);
		return ((uint64_t)(uintptr_t)(p + o) << 32) | (uint64_t)l;
	}
	p = JS_GetArrayBuffer(ctx, &len, ab);
	JS_FreeValue(ctx, ab);
	if (!p) {
		JS_FreeValue(ctx, JS_GetException(ctx));
		return 0;
	}
	return ((uint64_t)(uintptr_t)(p + off) << 32) | (uint64_t)blen;
}

// jr_new_promise returns the promise handle and writes the resolve and
// reject function handles to out[0], out[1].
EXPORT(jr_new_promise) uint32_t jr_new_promise(uint32_t *out) {
	JSValue fns[2];
	JSValue p = JS_NewPromiseCapability(ctx, fns);
	if (JS_IsException(p)) return exc();
	out[0] = put(fns[0]);
	out[1] = put(fns[1]);
	return put(p);
}

// -1 not a promise, 0 pending, 1 fulfilled, 2 rejected
EXPORT(jr_promise_state) int32_t jr_promise_state(uint32_t h) {
	JSValue v = get(h);
	if (!JS_IsPromise(v)) return -1;
	switch (JS_PromiseState(ctx, v)) {
	case JS_PROMISE_PENDING: return 0;
	case JS_PROMISE_FULFILLED: return 1;
	case JS_PROMISE_REJECTED: return 2;
	default: return -1;
	}
	return -1;
}
EXPORT(jr_promise_result) uint32_t jr_promise_result(uint32_t h) { return put(JS_PromiseResult(ctx, get(h))); }

// ---- bytecode (boot cache for the environment scripts) ----
// jr_compile compiles a classic script to QuickJS bytecode: ptr<<32|len
// (free with jr_free_js), or 0 with the exception pending.
EXPORT(jr_compile) uint64_t jr_compile(const char *src, uint32_t len, const char *name, uint32_t nlen) {
	char nbuf[256];
	if (nlen >= sizeof(nbuf)) nlen = sizeof(nbuf) - 1;
	memcpy(nbuf, name, nlen);
	nbuf[nlen] = 0;
	JSValue f = JS_Eval(ctx, src, len, nbuf, JS_EVAL_TYPE_GLOBAL | JS_EVAL_FLAG_COMPILE_ONLY);
	if (JS_IsException(f)) return 0;
	size_t size = 0;
	uint8_t *buf = JS_WriteObject(ctx, &size, f, JS_WRITE_OBJ_BYTECODE);
	JS_FreeValue(ctx, f);
	if (!buf) return 0;
	return ((uint64_t)(uintptr_t)buf << 32) | (uint64_t)size;
}
EXPORT(jr_free_js) void jr_free_js(void *p) { js_free(ctx, p); }

// jr_eval_bytecode runs bytecode from jr_compile; returns the completion value.
EXPORT(jr_eval_bytecode) uint32_t jr_eval_bytecode(const uint8_t *buf, uint32_t len) {
	JSValue f = JS_ReadObject(ctx, buf, len, JS_READ_OBJ_BYTECODE);
	if (JS_IsException(f)) return exc();
	return ret(JS_EvalFunction(ctx, f));
}
