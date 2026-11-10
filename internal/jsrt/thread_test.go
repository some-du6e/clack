package jsrt

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// parkJS parks the JS thread inside the engine (a host function called
// from JS, so a jr_eval is on the wasm stack) until the returned release
// is called.
func parkJS(t *testing.T, r *Runtime) (release func()) {
	t.Helper()
	parked, unpark := make(chan struct{}), make(chan struct{})
	r.Expose("park", func(*Ctx, Value, []Value) (any, error) {
		close(parked)
		<-unpark
		return nil, nil
	})
	r.Post(func(js *Ctx) { _, _ = js.Eval("park.js", "__clack.park()") })
	select {
	case <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("the JS thread never reached the park host function")
	}
	var once sync.Once
	release = func() { once.Do(func() { close(unpark) }) }
	t.Cleanup(release)
	return release
}

// TestRemoteValueWaitsForJSThread: Runtime.Eval hands its Value to the
// caller's goroutine, and Value is a fmt.Stringer, so fmt.Sprint(v) there
// called jr_to_string from that goroutine while the JS thread was inside
// the engine too: two goroutines in one wasm instance, sharing its stack
// pointer and malloc. The plugin-bench gaps job's probe (fmt.Sprint of an
// Eval result between scroll frames) hit "wasm error: out of bounds memory
// access" in jr_call that way. Off the JS thread a Value's methods must
// wait for the JS thread instead, and so must the Values they return.
func TestRemoteValueWaitsForJSThread(t *testing.T) {
	r := newTest(t)
	v, err := r.Eval("v.js", `({s: "hello", o: {n: 42}})`)
	if err != nil {
		t.Fatal(err)
	}
	release := parkJS(t, r)

	got := make(chan string, 1)
	go func() {
		// a derived Value (Get) is as remote as the one it came from
		got <- fmt.Sprint(v.Get("s"), " ", v.Get("o").Get("n").Int())
	}()
	select {
	case s := <-got:
		t.Fatalf("Value methods ran off the JS thread while it was inside the engine (got %q)", s)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case s := <-got:
		if s != "hello 42" {
			t.Fatalf("got %q, want %q", s, "hello 42")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the off-thread Value call never completed")
	}
	if err := r.Do(func(js *Ctx) error { return nil }); err != nil {
		t.Fatalf("the runtime didn't survive: %v", err)
	}
}

// TestRemoteValueOnJSThread: a remote Value handed back to the JS thread
// (into a Do) works there directly, without a Do round trip into itself.
func TestRemoteValueOnJSThread(t *testing.T) {
	r := newTest(t)
	v, err := r.Eval("v.js", `({f(x) { return x * 2 }})`)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := r.Do(func(js *Ctx) error {
		x, err := v.Method("f", 21)
		n = x.Int()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("f(21) = %d", n)
	}
	// and from the caller's goroutine, while nothing else runs
	x, err := v.Method("f", 4)
	if err != nil || x.Int() != 8 {
		t.Fatalf("off-thread f(4) = %v, %v", x, err)
	}
}

// TestRemoteValueAfterClose: a remote Value outliving its runtime is
// undefined, not a hang or a call into a closed engine.
func TestRemoteValueAfterClose(t *testing.T) {
	r := newTest(t)
	v, err := r.Eval("v.js", `({s: "x"})`)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	<-r.Done()
	done := make(chan string, 1)
	go func() { done <- v.Get("s").String() }()
	select {
	case s := <-done:
		if s != "undefined" {
			t.Fatalf("got %q after Close", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a remote Value's method hung after Close")
	}
}

// TestPromiseSettleRace: NewPromise's resolve and reject may be called
// from any goroutine, once between them; called at the same time, both
// passed the unsynchronized once check and Posted a settle, the second
// calling its freed functions.
func TestPromiseSettleRace(t *testing.T) {
	r := newTest(t)
	const n = 64
	var settles []func()
	if err := r.Do(func(js *Ctx) error {
		for i := 0; i < n; i++ {
			_, resolve, reject := js.NewPromise()
			settles = append(settles, func() { resolve(1) }, func() { reject(errors.New("no")) })
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, f := range settles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f()
		}()
	}
	close(start)
	wg.Wait()
	if err := r.Do(func(js *Ctx) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
