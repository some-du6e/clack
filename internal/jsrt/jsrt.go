// Package jsrt is Clack's JavaScript runtime for Taut plugins.
//
// The engine is QuickJS-NG compiled to WebAssembly (see csrc/ and the
// jsrt-wasm workflow) and run on wazero: pure Go, no cgo, full ES2023+.
// Each Runtime owns one JS thread (a goroutine). Every JS value is touched
// only on that thread: Do, Post, HostFunc callbacks and Root callbacks all
// run there. Around the engine sits a browser-like environment written in JS
// (event loop, timers, fetch, URL, TextEncoder, DOM subset, Intl, …) and real
// React with a react-reconciler host config that renders to the JSON host
// tree described in docs/PLUGINS.md.
//
// Nothing here runs until New is called, so Clack without plugins pays
// nothing for this package.
package jsrt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// KV backs localStorage / sessionStorage. Implementations must be safe for
// use from the JS goroutine; they are only called from there.
type KV interface {
	Get(key string) (string, bool)
	Set(key, value string)
	Delete(key string)
	Keys() []string // insertion or sorted order; used by key(i)/length
	Clear()
}

// Options configures a Runtime. Every field is optional.
type Options struct {
	// Fetch performs fetch() requests (and api.fetch built on it). nil means
	// http.DefaultClient.Do. Called on its own goroutine; ctx is cancelled
	// by AbortSignal.
	Fetch func(ctx context.Context, req *http.Request) (*http.Response, error)
	// Storage returns the backing store for a namespace: "localStorage" or
	// "sessionStorage". nil (or a nil KV) means an in-memory map.
	Storage func(namespace string) KV
	// Log receives console output. level is "log", "info", "warn", "error"
	// or "debug". nil means log.Printf.
	Log func(level, msg string)
	// CacheDir holds wazero's compiled-code cache so later starts skip
	// compiling the engine (~10 ms instead of hundreds). "" = memory only.
	CacheDir string
	// Timeout bounds one JS task (a Do, a timer callback, an event). A task
	// running longer is interrupted with an uncatchable error. Default 5 s.
	Timeout time.Duration
	// MemoryLimit for the JS heap in bytes. Default 512 MiB.
	MemoryLimit int
	// OpenURL opens a link (window.open, <a href> clicks, location.assign).
	// nil ignores them.
	OpenURL func(url string)
	// ClipboardWrite / ClipboardRead back navigator.clipboard. nil rejects.
	ClipboardWrite func(text string) error
	ClipboardRead  func() (string, error)
	// Language is navigator.language (default "en-US"); Platform is
	// navigator.platform (default from GOOS: "MacIntel", "Win32", "Linux x86_64").
	Language, Platform string
}

// Errors returned by the runtime.
var (
	ErrClosed  = errors.New("jsrt: runtime closed")
	ErrTimeout = errors.New("jsrt: timeout")
)

// JSError is a JS exception surfaced to Go.
type JSError struct {
	Name    string // "TypeError", …
	Message string
	Stack   string
}

func (e *JSError) Error() string {
	if e.Name == "" {
		return e.Message
	}
	return e.Name + ": " + e.Message
}

// HostFunc is a Go function callable from JS. It runs on the JS thread and
// may use js and the args freely; the returned value is converted with
// Ctx.Value. A non-nil error is thrown as a JS Error (a *JSError keeps its
// name). args are only valid during the call unless Dup'd... they are
// ordinary Values, so keeping one keeps the JS value alive.
type HostFunc func(js *Ctx, this Value, args []Value) (any, error)

// JSONFunc is the JSON flavour of HostFunc: every argument arrives as JSON
// (undefined becomes null, functions become null) and the result is
// marshalled with encoding/json (json.RawMessage passes through, []byte
// becomes a Uint8Array, Value passes through as-is).
type JSONFunc func(args []json.RawMessage) (any, error)

// AsyncFunc is called on its own goroutine; JS gets a Promise that settles
// with its result. ctx is cancelled when the runtime closes.
type AsyncFunc func(ctx context.Context, args []json.RawMessage) (any, error)

// Module registers an ES module source under a bare name ("$taut", "react",
// …). import "name" in any module, Import(name) and dynamic import() resolve
// to it. Registering again replaces it for later imports. Any goroutine.
// "react", "react/jsx-runtime", "react-dom", "react-dom/client" and
// "scheduler" are built in.
func (r *Runtime) Module(name, src string) {
	r.mu.Lock()
	r.modules[name] = src
	r.mu.Unlock()
}

// Import imports a registered module (or anything the loader resolves) and
// returns its namespace object, after top-level await settles. Any goroutine.
func (r *Runtime) Import(name string) (Value, error) {
	var v Value
	err := r.Do(func(js *Ctx) (err error) {
		v, err = js.Import(name)
		return err
	})
	return v, err
}

// Eval runs a classic script (sloppy mode, like a <script> or new Function
// body) and returns its completion value. Taut's user plugin format is an
// IIFE expression returning the plugin class, so Eval(name, code) yields the
// class. name is used in stack traces. Any goroutine.
func (r *Runtime) Eval(name, src string) (Value, error) {
	var v Value
	err := r.Do(func(js *Ctx) (err error) {
		v, err = js.Eval(name, src)
		return err
	})
	return v, err
}

// Expose makes fn callable from JS as globalThis.__clack[name]. Any
// goroutine; takes effect before the next JS task.
func (r *Runtime) Expose(name string, fn HostFunc) {
	r.Post(func(js *Ctx) { js.expose(name, fn) })
}

// ExposeJSON is Expose with JSON-encoded arguments.
func (r *Runtime) ExposeJSON(name string, fn JSONFunc) {
	r.Expose(name, func(js *Ctx, _ Value, args []Value) (any, error) {
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i] = a.jsonOrNull()
		}
		return fn(raw)
	})
}

// ExposeAsync exposes fn as a JS function returning a Promise; fn runs on
// its own goroutine.
func (r *Runtime) ExposeAsync(name string, fn AsyncFunc) {
	r.Expose(name, func(js *Ctx, _ Value, args []Value) (any, error) {
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			raw[i] = a.jsonOrNull()
		}
		p, resolve, reject := js.NewPromise()
		go func() {
			v, err := fn(r.ctx, raw)
			if err != nil {
				reject(err)
				return
			}
			resolve(v)
		}()
		return p, nil
	})
}

// Call calls the global function at path ("a.b.c" from globalThis; `this`
// is the object holding the last segment) with args converted by
// Ctx.Value, waits for a returned Promise to settle (or ctx to end), and
// returns the result as JSON (undefined → null). Any goroutine except the
// JS thread itself.
func (r *Runtime) Call(ctx context.Context, path string, args ...any) (json.RawMessage, error) {
	type res struct {
		v   json.RawMessage
		err error
	}
	ch := make(chan res, 1)
	onJS := r.onJSThread()
	err := r.Do(func(js *Ctx) error {
		fn, this, err := js.resolvePath(path)
		if err != nil {
			return err
		}
		v, err := fn.CallThis(this, args...)
		if err != nil {
			return err
		}
		if !v.IsPromise() {
			b, err := v.JSON()
			ch <- res{b, err}
			return nil
		}
		if onJS {
			return errors.New("jsrt: Runtime.Call of an async function from the JS thread would deadlock; use Value.Then")
		}
		v.Then(func(v Value, err error) {
			if err != nil {
				ch <- res{nil, err}
				return
			}
			b, err := v.JSON()
			ch <- res{b, err}
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case x := <-ch:
		return x.v, x.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, ErrClosed
	}
}
