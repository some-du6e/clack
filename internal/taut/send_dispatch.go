package taut

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// CheckThenDispatch runs the plugins' send checks (SendCheck) and, when
// they let the message go, Dispatch(name, args, native); done hears
// whether they did (sent) and the dispatch's result, on another
// goroutine. With the runtime running and checks to run, both go in one JS
// call (__taut.checkThenDispatch), queued before CheckThenDispatch
// returns, on the caller's goroutine (the composer's, at Enter, ahead of
// its own re-render): Slack's prepareAndSendMessage awaits its checks and
// dispatches its thunks in one JS task. An unpatched thunk calls native
// from there too, and what native dispatches in turn (the send
// transforms, a patched sendMessage) goes on with that chain
// (jsrt.UserChain). The checks used to answer Go, which dispatched the
// thunk, and a render the loop started between the two held the send for
// its whole run; the 5 ms hold that bridged the trip idled the JS thread
// after every key and held the composer's synchronous calls.
//
// A check an interrupted task left pending forever (sendCheck's rule) is
// ignored after sendCheckGrace with nothing open for the user: the thunk
// goes out on its own then, unless the call's native already started (the
// message is on its way), and the call's native fails if its chain goes
// on later.
func CheckThenDispatch[A, R any](ctx context.Context, t *Taut, c ComposerContext, name string, args A, native func(ctx context.Context, args A) (R, error), done func(sent bool, r R, err error)) {
	var zero R
	if t == nil { // plugins went off as the send came: no checks, no patches
		go func() {
			r, err := native(ctx, args)
			done(true, r, err)
		}()
		return
	}
	rt := t.liveRuntime()
	if rt == nil || !t.HasSendChecks() {
		t.SendCheckAsync(ctx, c, func(ok bool) {
			if !ok {
				done(false, zero, nil)
				return
			}
			r, err := Dispatch(ctx, t, name, args, native)
			done(true, r, err)
		})
		return
	}
	if !t.sendHops() {
		ctx = jsrt.NoChain(ctx) // nothing comes back into JS after the native: no hold
	}
	var mu sync.Mutex
	started, abandoned := false, false
	var nativeErr error
	inner := nativeCore(name, native)
	tt := t.thunks
	id := tt.add(ctx, func(nctx context.Context, raw []json.RawMessage) (any, error) {
		mu.Lock()
		if abandoned {
			mu.Unlock()
			return nil, errors.New("taut: the message went out without this send check")
		}
		started = true
		mu.Unlock()
		v, err := inner(nctx, raw)
		if err != nil {
			mu.Lock()
			nativeErr = err // its own error, not the JS rejection that wraps it
			mu.Unlock()
		}
		return v, err
	})
	passed := tt.watchChecks(id)
	type answer struct {
		raw json.RawMessage
		err error
	}
	answered := make(chan answer, 1)
	cctx, cancel := context.WithCancel(ctx)
	rt.rt.CallThen(cctx, "__taut.checkThenDispatch", func(raw json.RawMessage, err error) {
		answered <- answer{raw, err}
	}, c, name, []any{args}, id, SyncThunks[name])
	go func() {
		defer cancel()
		defer tt.drop(id)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		var idle time.Duration
		for {
			select {
			case a := <-answered:
				mu.Lock()
				ran, nerr := started, nativeErr
				mu.Unlock()
				if a.err != nil {
					switch {
					case nerr != nil:
						done(true, zero, nerr) // the post failed: its own error
					case ran:
						t.logf("taut: a send's dispatch failed after its native ran: %v", a.err)
						done(true, zero, nil) // the message went out
					case !passed.Load():
						// the checks never answered (the runtime closed, the
						// watchdog interrupted them): a failed check lets the
						// message go, as SendCheck's does
						t.logf("taut: send check: %v", a.err)
						r, err := native(context.WithoutCancel(ctx), args)
						done(true, r, err)
					default:
						done(true, zero, a.err) // a patch threw or rejected: not sent
					}
					return
				}
				var out struct {
					Sent   bool            `json:"sent"`
					Result json.RawMessage `json:"result"`
				}
				if err := json.Unmarshal(a.raw, &out); err != nil {
					done(true, zero, err)
					return
				}
				if !out.Sent {
					done(false, zero, nil)
					return
				}
				r, err := decodeDispatch[R](name, out.Result)
				done(true, r, err)
				return
			case <-tick.C:
				if passed.Load() {
					continue // the checks passed: the dispatch is as long as it takes
				}
				if rt.rt.Interrupts() == 0 || t.sendCheckWaiting(ctx, rt) {
					idle = 0
					continue
				}
				if idle += time.Second; idle < sendCheckGrace {
					continue
				}
				mu.Lock()
				out := started
				abandoned = !started
				mu.Unlock()
				if out {
					idle = 0 // the message is on its way: its answer comes
					continue
				}
				t.logf("taut: send check: pending %v with nothing open for the user after the JS runtime interrupted a task (an interrupted promise never settles); ignoring it", idle)
				r, err := Dispatch(ctx, t, name, args, native)
				done(true, r, err)
				return
			}
		}
	}()
}

// sendHops reports whether a send's native goes back into JS (a send
// transform, a patched sendMessage or uploadFiles): only then does its
// chain hold the queue while the native runs (jsrt.UserChain).
func (t *Taut) sendHops() bool {
	return t.HasSendTransforms() || t.ThunkPatched("sendMessage") || t.ThunkPatched("uploadFiles")
}

// SendLane is ctx for a send's JS work on t (the user's lane, as a send's
// chain): with the hold only when its native goes back into JS
// (sendHops).
func SendLane(ctx context.Context, t *Taut) context.Context {
	if t == nil || !t.sendHops() {
		return jsrt.NoChain(jsrt.UserChain(ctx))
	}
	return jsrt.UserChain(ctx)
}

// LastHop is ctx for a send's last trip into JS (its sendMessage thunk,
// or its send transforms when nothing follows them): on the send's lane,
// without the hold, as only the network comes after it.
func LastHop(ctx context.Context) context.Context { return jsrt.NoChain(ctx) }
