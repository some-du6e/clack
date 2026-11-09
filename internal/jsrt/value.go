package jsrt

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Value is a reference to a JS value. The zero Value is undefined. Values
// may be stored anywhere (a Value keeps its JS value alive until Free or
// until the Go value is garbage collected), but their methods must be
// called on the JS thread: inside Do, Post, a HostFunc or a Root callback.
type Value struct{ ref *ref }

type ref struct {
	r       *Runtime
	h       uint32 // engine handle; 0 = undefined
	cleanup runtime.Cleanup
}

func (r *Runtime) wrap(h uint32) Value {
	if h == 0 {
		return Value{}
	}
	x := &ref{r: r, h: h}
	x.cleanup = runtime.AddCleanup(x, func(c cleanupArg) { c.r.releaseLater(c.h) }, cleanupArg{r, h})
	return Value{x}
}

type cleanupArg struct {
	r *Runtime
	h uint32
}

// releaseLater frees a handle whose Go Value was garbage collected.
func (r *Runtime) releaseLater(h uint32) {
	if r.closed.Load() {
		return
	}
	r.enqueue(func() {
		if r.eng != nil {
			r.eng.release(h)
		}
	})
}

func (v Value) h() uint32 {
	if v.ref == nil {
		return 0
	}
	return v.ref.h
}

func (v Value) eng() *engine {
	if v.ref == nil || v.ref.r.eng == nil {
		return nil
	}
	return v.ref.r.eng
}

// Free releases the JS value now instead of waiting for Go's GC. The Value
// (and copies of it) must not be used afterwards.
func (v Value) Free() {
	if v.ref == nil || v.ref.h == 0 {
		return
	}
	// The GC cleanup holds the handle number: left armed, it would release
	// it again later, when the engine has given the number to another
	// value (handles are reused, last freed first), and that value's slot
	// would then hold whatever comes next: a props object turning into
	// __clack, a function into a non-function.
	v.ref.cleanup.Stop()
	if e := v.eng(); e != nil {
		e.release(v.ref.h)
	}
	v.ref.h = 0
}

// Type returns typeof v ("undefined", "object", "function", …; null is
// "null", arrays are "object").
func (v Value) Type() string {
	e := v.eng()
	if e == nil {
		return "undefined"
	}
	return e.typeOf(v.h())
}

func (v Value) IsUndefined() bool { return v.Type() == "undefined" }
func (v Value) IsNull() bool      { return v.Type() == "null" }
func (v Value) IsNullish() bool   { t := v.Type(); return t == "undefined" || t == "null" }
func (v Value) IsBool() bool      { return v.Type() == "boolean" }
func (v Value) IsNumber() bool    { return v.Type() == "number" }
func (v Value) IsString() bool    { return v.Type() == "string" }
func (v Value) IsFunction() bool  { return v.Type() == "function" }
func (v Value) IsObject() bool    { t := v.Type(); return t == "object" || t == "function" }
func (v Value) IsArray() bool     { e := v.eng(); return e != nil && e.is(v.h(), isArray) }
func (v Value) IsPromise() bool   { e := v.eng(); return e != nil && e.is(v.h(), isPromise) }
func (v Value) IsError() bool     { e := v.eng(); return e != nil && e.is(v.h(), isError) }

// Bool is JS truthiness.
func (v Value) Bool() bool { e := v.eng(); return e != nil && e.truthy(v.h()) }

// Float is Number(v).
func (v Value) Float() float64 {
	e := v.eng()
	if e == nil {
		return nan()
	}
	return e.toFloat(v.h())
}

// Int is Number(v) truncated.
func (v Value) Int() int {
	f := v.Float()
	if f != f {
		return 0
	}
	return int(f)
}

// String is String(v).
func (v Value) String() string {
	e := v.eng()
	if e == nil {
		return "undefined"
	}
	return e.toString(v.h())
}

// Get reads v[key].
func (v Value) Get(key string) Value {
	e := v.eng()
	if e == nil {
		return Value{}
	}
	h, _ := e.get(v.h(), key)
	return v.ref.r.wrap(h)
}

// Index reads v[i].
func (v Value) Index(i int) Value {
	e := v.eng()
	if e == nil {
		return Value{}
	}
	h, _ := e.getIndex(v.h(), uint32(i))
	return v.ref.r.wrap(h)
}

// Len is v.length as an int.
func (v Value) Len() int { return v.Get("length").Int() }

// Set writes v[key] = x (x converted with Ctx.Value).
func (v Value) Set(key string, x any) error {
	e := v.eng()
	if e == nil {
		return errNoValue
	}
	xv, err := v.ref.r.js.Value(x)
	if err != nil {
		return err
	}
	return e.set(v.h(), key, xv.h())
}

// SetIndex writes v[i] = x.
func (v Value) SetIndex(i int, x any) error {
	e := v.eng()
	if e == nil {
		return errNoValue
	}
	xv, err := v.ref.r.js.Value(x)
	if err != nil {
		return err
	}
	return e.setIndex(v.h(), uint32(i), xv.h())
}

// Delete removes v[key].
func (v Value) Delete(key string) error {
	e := v.eng()
	if e == nil {
		return errNoValue
	}
	return e.del(v.h(), key)
}

// Has is `key in v`.
func (v Value) Has(key string) bool { e := v.eng(); return e != nil && e.has(v.h(), key) }

// Keys is Object.keys(v).
func (v Value) Keys() []string {
	e := v.eng()
	if e == nil {
		return nil
	}
	return e.keys(v.h())
}

// Call calls v with this = undefined. Arguments are converted with
// Ctx.Value. A returned Promise is not awaited; use Then.
func (v Value) Call(args ...any) (Value, error) { return v.CallThis(Value{}, args...) }

// CallThis calls v with an explicit this.
func (v Value) CallThis(this Value, args ...any) (Value, error) {
	e := v.eng()
	if e == nil {
		return Value{}, errNoValue
	}
	hs, keep, err := v.ref.r.js.handles(args)
	if err != nil {
		return Value{}, err
	}
	h, err := e.callFn(v.h(), this.h(), hs)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(v.ref)
	runtime.KeepAlive(this.ref)
	return v.ref.r.wrap(h), err
}

// Method calls v[name](args...) with this = v.
func (v Value) Method(name string, args ...any) (Value, error) {
	fn := v.Get(name)
	if !fn.IsFunction() {
		return Value{}, &JSError{Name: "TypeError", Message: fmt.Sprintf("%s is not a function", name)}
	}
	return fn.CallThis(v, args...)
}

// New is `new v(...args)`.
func (v Value) New(args ...any) (Value, error) {
	e := v.eng()
	if e == nil {
		return Value{}, errNoValue
	}
	hs, keep, err := v.ref.r.js.handles(args)
	if err != nil {
		return Value{}, err
	}
	h, err := e.construct(v.h(), hs)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(v.ref)
	return v.ref.r.wrap(h), err
}

// JSON is JSON.stringify(v); undefined (and functions) give "null".
func (v Value) JSON() (json.RawMessage, error) {
	e := v.eng()
	if e == nil {
		return json.RawMessage("null"), nil
	}
	return e.stringify(v.h())
}

func (v Value) jsonOrNull() json.RawMessage {
	b, err := v.JSON()
	if err != nil || len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

// Decode unmarshals JSON.stringify(v) into dst.
func (v Value) Decode(dst any) error {
	b, err := v.JSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// Export converts v to plain Go data via JSON (map[string]any, []any,
// float64, string, bool, nil).
func (v Value) Export() any {
	var x any
	_ = v.Decode(&x)
	return x
}

// Bytes copies the contents of an ArrayBuffer, TypedArray or DataView.
func (v Value) Bytes() []byte {
	e := v.eng()
	if e == nil {
		return nil
	}
	return e.bytes(v.h())
}

// Then calls f once the Promise (or thenable, or plain value) v settles, on
// the JS thread. A rejection arrives as err (*JSError for Error objects).
func (v Value) Then(f func(v Value, err error)) {
	if v.ref == nil {
		f(Value{}, nil)
		return
	}
	v.ref.r.js.then(v, f)
}

// StrictEquals is v === w.
func (v Value) StrictEquals(w Value) bool {
	e := v.eng()
	if e == nil {
		return w.IsUndefined()
	}
	return e.strictEq(v.h(), w.h())
}

// Ctx is the JS-thread context handed to Do, Post and HostFuncs.
type Ctx struct {
	r *Runtime
}

// Runtime returns the owning runtime.
func (c *Ctx) Runtime() *Runtime { return c.r }

// Global is globalThis.
func (c *Ctx) Global() Value { return c.r.wrap(c.r.eng.global()) }

// Undefined returns undefined.
func (c *Ctx) Undefined() Value { return Value{} }

// Null returns null.
func (c *Ctx) Null() Value { return c.r.wrap(c.r.eng.null()) }

// Eval runs a classic script and returns its completion value.
func (c *Ctx) Eval(name, src string) (Value, error) {
	h, err := c.r.eng.eval(name, src, false)
	return c.r.wrap(h), err
}

// Import imports a module and returns its namespace (top-level await is
// awaited by running the event loop's microtasks; a module that awaits a
// timer or fetch returns an error asking to use Runtime.Import from
// another goroutine).
func (c *Ctx) Import(name string) (Value, error) {
	h, err := c.r.eng.importModule(c, name)
	return c.r.wrap(h), err
}

// importPromise starts import(name) and returns its Promise.
func (c *Ctx) importPromise(name string) (Value, error) {
	q, _ := jsonMarshal(name)
	h, err := c.r.eng.eval("<import>", "import("+string(q)+")", false)
	return c.r.wrap(h), err
}

// ParseJSON is JSON.parse(b).
func (c *Ctx) ParseJSON(b []byte) (Value, error) {
	h, err := c.r.eng.parse(b)
	return c.r.wrap(h), err
}

// NewObject is {}.
func (c *Ctx) NewObject() Value { return c.r.wrap(c.r.eng.newObject()) }

// NewArray is [].
func (c *Ctx) NewArray() Value { return c.r.wrap(c.r.eng.newArray()) }

// NewError is new Error(msg) (or the named error class).
func (c *Ctx) NewError(err error) Value { return c.r.wrap(c.r.eng.newError(err)) }

// Func wraps a Go function as a JS function.
func (c *Ctx) Func(name string, fn HostFunc) Value {
	return c.r.wrap(c.r.eng.newFunc(c.r.addHost(fn), name))
}

// NewPromise returns a pending Promise and its settle functions. resolve and
// reject may be called from any goroutine, once; values are converted with
// Ctx.Value on the JS thread.
func (c *Ctx) NewPromise() (p Value, resolve func(any), reject func(error)) {
	ph, rs, rj := c.r.eng.newPromise()
	p = c.r.wrap(ph)
	resV, rejV := c.r.wrap(rs), c.r.wrap(rj)
	var once onceFlag
	resolve = func(x any) {
		if !once.take() {
			return
		}
		c.r.Post(func(js *Ctx) {
			if _, err := resV.Call(x); err != nil {
				_, _ = rejV.Call(js.NewError(err))
			}
			resV.Free()
			rejV.Free()
		})
	}
	reject = func(err error) {
		if !once.take() {
			return
		}
		c.r.Post(func(js *Ctx) {
			_, _ = rejV.Call(js.NewError(err))
			resV.Free()
			rejV.Free()
		})
	}
	return p, resolve, reject
}

// Value converts a Go value to JS: Value as-is; nil → null; bool, ints,
// floats, string; []byte → Uint8Array; json.RawMessage → JSON.parse;
// error → Error; HostFunc / func(*Ctx, Value, []Value) (any, error) →
// function; func(*Ctx) (Value, error) → its result (built on the JS
// thread); anything else via encoding/json.
func (c *Ctx) Value(x any) (Value, error) {
	e := c.r.eng
	switch x := x.(type) {
	case Value:
		return x, nil
	case nil:
		return c.r.wrap(e.null()), nil
	case bool:
		return c.r.wrap(e.newBool(x)), nil
	case string:
		return c.r.wrap(e.newString(x)), nil
	case int:
		return c.r.wrap(e.newNumber(float64(x))), nil
	case int32:
		return c.r.wrap(e.newNumber(float64(x))), nil
	case int64:
		return c.r.wrap(e.newNumber(float64(x))), nil
	case uint32:
		return c.r.wrap(e.newNumber(float64(x))), nil
	case float32:
		return c.r.wrap(e.newNumber(float64(x))), nil
	case float64:
		return c.r.wrap(e.newNumber(x)), nil
	case []byte:
		return c.r.wrap(e.newBytes(x)), nil
	case json.RawMessage:
		return c.ParseJSON(x)
	case error:
		return c.NewError(x), nil
	case HostFunc:
		return c.Func("", x), nil
	case func(*Ctx, Value, []Value) (any, error):
		return c.Func("", x), nil
	case func(*Ctx) (Value, error):
		return x(c) // built on the JS thread (e.g. a NewPromise resolve from another goroutine)
	}
	b, err := json.Marshal(x)
	if err != nil {
		return Value{}, err
	}
	return c.ParseJSON(b)
}

// handles converts args to engine handles. The caller keeps the returned
// Values alive (runtime.KeepAlive) until the engine is done with the
// handles: a Value collected earlier releases its handle.
func (c *Ctx) handles(args []any) ([]uint32, []Value, error) {
	hs := make([]uint32, len(args))
	keep := make([]Value, len(args))
	for i, a := range args {
		v, err := c.Value(a)
		if err != nil {
			return nil, nil, err
		}
		hs[i] = v.h()
		keep[i] = v
	}
	return hs, keep, nil
}

func (c *Ctx) expose(name string, fn HostFunc) {
	c.r.eng.exposeHost(name, c.r.addHost(fn))
}

func (r *Runtime) addHost(fn HostFunc) uint32 {
	r.hostN++
	for r.hosts[r.hostN] != nil || r.hostN == 0 || r.hostN >= 1<<31 {
		if r.hostN >= 1<<31 {
			r.hostN = 0
		}
		r.hostN++
	}
	r.hosts[r.hostN] = fn
	return r.hostN
}

// resolvePath resolves "a.b.c" from globalThis to (fn, this).
func (c *Ctx) resolvePath(path string) (fn, this Value, err error) {
	cur := c.Global()
	parts := strings.Split(path, ".")
	for i, p := range parts {
		if i == len(parts)-1 {
			this = cur
		}
		cur = cur.Get(p)
		if cur.IsNullish() && i < len(parts)-1 {
			return Value{}, Value{}, &JSError{Name: "TypeError", Message: fmt.Sprintf("%s is %s", strings.Join(parts[:i+1], "."), cur.Type())}
		}
	}
	if !cur.IsFunction() {
		return Value{}, Value{}, &JSError{Name: "TypeError", Message: path + " is not a function"}
	}
	return cur, this, nil
}

func (c *Ctx) then(v Value, f func(Value, error)) {
	onOK := c.Func("", func(js *Ctx, _ Value, args []Value) (any, error) {
		var x Value
		if len(args) > 0 {
			x = args[0]
		}
		f(x, nil)
		return nil, nil
	})
	onErr := c.Func("", func(js *Ctx, _ Value, args []Value) (any, error) {
		var x Value
		if len(args) > 0 {
			x = args[0]
		}
		f(Value{}, c.r.eng.toError(x.h()))
		return nil, nil
	})
	pr := c.Global().Get("Promise")
	p, err := pr.Method("resolve", v)
	if err != nil {
		f(Value{}, err)
		return
	}
	_, _ = p.Method("then", onOK, onErr)
}

type onceFlag struct{ done uint32 }

func (o *onceFlag) take() bool {
	if o.done != 0 {
		return false
	}
	o.done = 1
	return true
}

var errNoValue = errors.New("jsrt: value has no runtime")

func nan() float64 { var z float64; return z / z }
