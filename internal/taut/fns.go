package taut

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Functions crossing the Go/JS boundary inside taut payloads (thunk args and
// results, menu templates, composer contexts). Both sides tag their ids with
// bit 51 so they never collide with jsrt's {"$fn"} ids or slackc's
// {"$gofn"} ids, which use the low ids.

const fnBit = int64(1) << 51

// IsTautFn reports whether a {"$fn"} / {"$gofn"} id belongs to taut (vs
// jsrt's host-tree props or slackc's Go funcs).
func IsTautFn(id int64) bool { return id&fnBit != 0 }

// GoFunc is a Go function handed to JS: it marshals as {"$gofn":id} and
// JS gets a callable. Args arrive as JSON; the result is marshalled back.
type GoFunc func(args []json.RawMessage) (any, error)

type goFns struct {
	mu   sync.Mutex
	next int64
	fns  map[int64]GoFunc
}

func newGoFns() *goFns { return &goFns{fns: map[int64]GoFunc{}} }

func (g *goFns) add(f GoFunc) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	id := fnBit | g.next
	g.fns[id] = f
	return id
}

func (g *goFns) call(id int64, args []json.RawMessage) (any, error) {
	g.mu.Lock()
	f := g.fns[id]
	g.mu.Unlock()
	if f == nil {
		return nil, errors.New("taut: Go function was released")
	}
	return f(args)
}

func (g *goFns) release(id int64) {
	g.mu.Lock()
	delete(g.fns, id)
	g.mu.Unlock()
}

// Func registers f for JS and returns its handle; Release frees it.
func (t *Taut) Func(f GoFunc) *GoHandle { return &GoHandle{t: t, ID: t.fns.add(f)} }

// GoHandle is a registered GoFunc.
type GoHandle struct {
	t  *Taut
	ID int64
}

func (h *GoHandle) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]int64{"$gofn": h.ID})
}

func (h *GoHandle) Release() { h.t.fns.release(h.ID) }

// JSFunc is a JS function that came to Go as {"$fn":id} (a menu item's
// click, a plugin-built callback). The zero value is "no function".
type JSFunc struct {
	ID int64
	t  *Taut
}

func (f *JSFunc) UnmarshalJSON(b []byte) error {
	var v struct {
		Fn int64 `json:"$fn"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil // not a function: leave zero
	}
	f.ID = v.Fn
	return nil
}

func (f JSFunc) MarshalJSON() ([]byte, error) {
	if f.ID == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(map[string]int64{"$fn": f.ID})
}

// Valid reports whether this is a function.
func (f JSFunc) Valid() bool { return f.ID != 0 }

// Call calls the JS function with args, waiting for a returned Promise.
// Never call it on the UI goroutine if it may block; use Go.
func (t *Taut) Call(ctx context.Context, f JSFunc, args ...any) (json.RawMessage, error) {
	if f.ID == 0 {
		return nil, nil
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return nil, err
	}
	if args == nil {
		args = []any{}
	}
	if IsTautFn(f.ID) {
		return rt.rt.Call(ctx, "__taut.callFn", f.ID, args)
	}
	return rt.rt.Call(ctx, "__taut.callHostFn", f.ID, args)
}

// Go calls f asynchronously; done (optional) gets the result on another goroutine.
func (t *Taut) Go(f JSFunc, args []any, done func(json.RawMessage, error)) {
	go func() {
		res, err := t.Call(context.Background(), f, args...)
		if done != nil {
			done(res, err)
		}
	}()
}

// ReleaseJS frees JS functions Go no longer needs (closed menus).
func (t *Taut) ReleaseJS(fs ...JSFunc) {
	if !t.Started() {
		return
	}
	ids := make([]int64, 0, len(fs))
	for _, f := range fs {
		if IsTautFn(f.ID) {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if rt, err := t.ensureRuntime(); err == nil {
		rt.post("releaseFns", ids)
	}
}
