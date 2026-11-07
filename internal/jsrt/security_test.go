package jsrt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Plugins are untrusted code: they must not be able to hang the JS thread
// forever, exhaust memory, reach files, or crash Clack through a host
// function.

// callAsync defines globalThis.<name> = async () => { body } and awaits it.
func callAsync(t *testing.T, r *Runtime, body string) (string, error) {
	t.Helper()
	if _, err := r.Eval("t.js", "globalThis.__t = async () => { "+body+" }"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := r.Call(ctx, "__t")
	return string(raw), err
}

// Generic array methods over a huge array-like used to loop in C without
// polling the interrupt handler (csrc/quickjs-interrupt.patch); they stop
// at the task deadline like any other loop and the runtime keeps working.
func TestArrayLikeLoopsInterrupted(t *testing.T) {
	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond })
	for _, src := range []string{
		`Array.prototype.reverse.call({ length: 2 ** 53 - 1 })`,
		`Array.prototype.copyWithin.call({ length: 2 ** 53 - 1 }, 0, 1)`,
		`Array.prototype.join.call({ length: 2 ** 53 - 1 })`,
		`Array.prototype.includes.call({ length: 2 ** 53 - 1 }, 1)`,
		`try { Array.prototype.reverse.call({ length: 2 ** 53 - 1 }) } catch { 'caught' }`,
	} {
		start := time.Now()
		errc := make(chan error, 1)
		go func() {
			errc <- r.Do(func(js *Ctx) error {
				_, err := js.Eval("hang.js", src)
				return err
			})
		}()
		select {
		case err := <-errc:
			if !errors.Is(err, ErrTimeout) {
				t.Fatalf("%s: %v", src, err)
			}
			t.Logf("%s: stopped after %v", src, time.Since(start).Round(time.Millisecond))
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: the JS thread is still stuck after 20 s", src)
		}
		if got := evalString(t, r, `1+1`); got != "2" {
			t.Fatalf("after %s: %q", src, got)
		}
	}
}

// A task that never comes back (here a host function that blocks; in
// real life a native loop that doesn't poll) makes the watchdog give the
// Runtime up: Do returns ErrClosed and Done fires, instead of every caller
// waiting forever.
func TestWatchdogGivesUpOnStuckTask(t *testing.T) {
	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	r.Expose("stuck", func(js *Ctx, this Value, args []Value) (any, error) {
		select {
		case <-release:
		case <-time.After(30 * time.Second):
		}
		return nil, nil
	})
	start := time.Now()
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("stuck.js", `__clack.stuck()`)
		return err
	})
	if !errors.Is(err, ErrClosed) || time.Since(start) > 10*time.Second {
		t.Fatalf("stuck task: %v after %v", err, time.Since(start))
	}
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("Done didn't fire")
	}
	if err := r.Do(func(js *Ctx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Do afterwards: %v", err)
	}
	// a fresh runtime works
	if got := evalString(t, newTest(t), `6*7`); got != "42" {
		t.Fatalf("fresh runtime: %q", got)
	}
}

// A runaway microtask chain is cut off by the task deadline too.
func TestMicrotaskFloodInterrupted(t *testing.T) {
	r := newTest(t, func(o *Options) { o.Timeout = 300 * time.Millisecond })
	start := time.Now()
	r.Post(func(js *Ctx) {
		_, _ = js.Eval("flood.js", `(function f() { Promise.resolve().then(f) })()`)
	})
	if got := evalString(t, r, `'alive'`); got != "alive" {
		t.Fatalf("got %q", got)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %v", d)
	}
}

// Allocating without bound fails inside JS (QuickJS's heap limit), and the
// runtime keeps working once the garbage is gone.
func TestMemoryBombFailsCleanly(t *testing.T) {
	r := newTest(t, func(o *Options) { o.MemoryLimit = 64 << 20 })
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("bomb.js", `(() => { const a = []; for (;;) a.push(new Uint8Array(1 << 20).fill(1)) })()`)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "memory") {
		t.Fatalf("memory bomb: %v", err)
	}
	err = r.Do(func(js *Ctx) error {
		_, err := js.Eval("bomb2.js", `(() => { let s = 'x'; for (;;) s += s })()`)
		return err
	})
	if err == nil {
		t.Fatal("string bomb succeeded")
	}
	if got := evalString(t, r, `[1,2,3].map(x => x * 2).join()`); got != "2,4,6" {
		t.Fatalf("after the bombs: %q", got)
	}
	if p := memoryLimitPages(64 << 20); p != (64<<20+wasmHeadroom)>>16 {
		t.Errorf("wasm memory cap = %d pages", p)
	}
	if p := memoryLimitPages(8 << 30); p != 65536 {
		t.Errorf("wasm memory cap for 8 GiB = %d pages", p)
	}
}

// fetch() reaches only http(s); file: and friends never get to
// Options.Fetch, and a huge body is refused.
func TestFetchSchemesAndSize(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "FILE:///C:/Windows/win.ini", "ftp://example.com/", "gopher://x/", "javascript:alert(1)", "//example.com/x", "/relative", "http:///nohost"} {
		if fetchURLAllowed(u) == nil {
			t.Errorf("fetchURLAllowed(%q) = nil", u)
		}
	}
	for _, u := range []string{"https://slack.com/api/auth.test", "http://localhost:3000/x", "HTTPS://example.com"} {
		if err := fetchURLAllowed(u); err != nil {
			t.Errorf("fetchURLAllowed(%q) = %v", u, err)
		}
	}
	var calls atomic.Int32
	old := maxFetchBytes
	maxFetchBytes = 1 << 20
	t.Cleanup(func() { maxFetchBytes = old })
	r := newTest(t, func(o *Options) {
		o.Fetch = func(ctx context.Context, req *http.Request) (*http.Response, error) {
			calls.Add(1)
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				t.Errorf("Options.Fetch got %s", req.URL)
			}
			if req.URL.Path == "/huge" {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(io.LimitReader(zeros{}, 4<<20)), Request: req}, nil
			}
			return fakeFetch(ctx, req)
		}
	})
	got, err := callAsync(t, r, `
		const out = []
		for (const u of ['file:///etc/passwd', 'file://localhost/etc/hosts']) {
			try { await fetch(u); out.push('fetched ' + u) } catch (e) { out.push(e.name) }
		}
		try { const r = await fetch('http://test/huge'); await r.arrayBuffer(); out.push('huge ok') } catch (e) { out.push(e.name + ':' + /larger/.test(e.cause?.message)) }
		out.push(await (await fetch('http://test/json')).json().then(j => j.n))
		return out.join(',')`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `"TypeError,TypeError,TypeError:true,42"` {
		t.Fatalf("got %s", got)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("Options.Fetch called %d times, want 2 (huge, json)", n)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// A panic in any host function becomes a JS exception; nested JS → Go → JS
// → Go panics too.
func TestHostPanicNested(t *testing.T) {
	r := newTest(t)
	r.Expose("boom", func(js *Ctx, this Value, args []Value) (any, error) {
		var m map[string]int
		m["x"] = 1 // nil map write
		return nil, nil
	})
	r.Expose("callback", func(js *Ctx, this Value, args []Value) (any, error) {
		return args[0].Call()
	})
	r.Expose("index", func(js *Ctx, this Value, args []Value) (any, error) {
		return args[5].String(), nil // out of range
	})
	got := evalString(t, r, `[
		(() => { try { __clack.boom() } catch (e) { return 'a:' + /panicked/.test(e.message) } })(),
		(() => { try { __clack.callback(() => __clack.boom()) } catch (e) { return 'b:' + /panicked/.test(e.message) } })(),
		(() => { try { __clack.index() } catch (e) { return 'c:' + /panicked/.test(e.message) } })(),
	].join()`)
	if got != "a:true,b:true,c:true" {
		t.Fatalf("got %s", got)
	}
}

// The boot bytecode cache replaces a symlink planted at its path instead
// of writing through it.
func TestBytecodeCacheIgnoresSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := bcKey("symlink-test", "1")
	p := bcPath(dir, k)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{p, p + ".tmp"} {
		if err := os.Symlink(victim, link); err != nil {
			t.Fatal(err)
		}
	}
	storeBC(dir, k, []byte("bytecode"))
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatalf("wrote through the symlink: %q", b)
	}
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("cache file is still the symlink: %v", err)
	}
}
