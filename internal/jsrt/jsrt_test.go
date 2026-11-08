package jsrt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTest starts a runtime with a fake fetch and in-memory storage.
func newTest(t testing.TB, opts ...func(*Options)) *Runtime {
	t.Helper()
	var mu sync.Mutex
	var logs []string
	o := Options{
		Log: func(level, msg string) {
			mu.Lock()
			logs = append(logs, level+": "+msg)
			mu.Unlock()
			t.Logf("js %s: %s", level, msg)
		},
		Fetch: fakeFetch,
	}
	for _, f := range opts {
		f(&o)
	}
	r, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

// fakeFetch answers http://test/… with synthetic data.
func fakeFetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	h := http.Header{}
	status := 200
	var out string
	switch req.URL.Path {
	case "/json":
		h.Set("Content-Type", "application/json")
		out = `{"ok":true,"n":42,"q":"` + req.URL.Query().Get("q") + `"}`
	case "/echo":
		h.Set("Content-Type", req.Header.Get("Content-Type"))
		h.Set("X-Method", req.Method)
		h.Set("X-Custom", req.Header.Get("X-Custom"))
		out = string(body)
	case "/slow":
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		out = "slow"
	case "/404":
		status = 404
		out = "not found"
	case "/bytes":
		h.Set("Content-Type", "application/octet-stream")
		out = string([]byte{0, 1, 2, 255})
	default:
		h.Set("Content-Type", "text/plain")
		out = "hello " + req.URL.Path
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(out)), Request: req}, nil
}

func evalString(t testing.TB, r *Runtime, src string) string {
	t.Helper()
	var s string
	err := r.Do(func(js *Ctx) error {
		v, err := js.Eval("test.js", src)
		if err != nil {
			return err
		}
		s = v.String()
		return nil
	})
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return s
}

func TestEvalBasics(t *testing.T) {
	r := newTest(t)
	cases := map[string]string{
		`1+2`:                        "3",
		`'a'.repeat(3)`:              "aaa",
		`[1,2,3].map(x=>x*2).join()`: "2,4,6",
		`typeof React.createElement`: "function",
		`(() => { class A { #x = 5; get x() { return this.#x } static #s = 1; static s() { return A.#s } }; return new A().x + A.s() })()`: "6",
		`10n ** 20n + ''`:               "100000000000000000000",
		`({a:{b:null}}).a?.b?.c ?? 'd'`: "d",
		`Object.groupBy([1,2,3], x => x % 2 ? 'odd' : 'even').odd.length`:           "2",
		`[3,1,2].toSorted().join('')`:                                               "123",
		`'x'.at(-1) + [1,2].findLast(x => x)`:                                       "x2",
		`/(?<=a)b/.test('ab') && /\p{L}/u.test('é') && /[\p{L}--[a-z]]/v.test('A')`: "true",
		`new Set([1,2]).union(new Set([3])).size`:                                   "3",
		`typeof WeakRef + typeof FinalizationRegistry`:                              "functionfunction",
		`new Proxy({}, { get: (_, k) => k + '!' }).hi`:                              "hi!",
		`RegExp.escape('a.b')`:                                                      "\\x61\\.b",
		`Map.groupBy([1,2,3], x => x > 1).get(true).length`:                         "2",
		`(function f() { return 1 }).toString()`:                                    "function f() { return 1 }",
		`new Function('a', 'return a * 2')(21)`:                                     "42",
		`[...(function*(){ yield 1; yield 2 })()].length`:                           "2",
	}
	for src, want := range cases {
		if got := evalString(t, r, src); got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	r := newTest(t)
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("e.js", `throw new TypeError("boom")`)
		return err
	})
	je, ok := err.(*JSError)
	if !ok || je.Name != "TypeError" || je.Message != "boom" {
		t.Fatalf("err = %#v", err)
	}
	err = r.Do(func(js *Ctx) error {
		_, err := js.Eval("e.js", `syntax error here(`)
		return err
	})
	if je, ok := err.(*JSError); !ok || je.Name != "SyntaxError" {
		t.Fatalf("syntax err = %#v", err)
	}
}

func TestTimeoutInterrupts(t *testing.T) {
	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond })
	start := time.Now()
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("loop.js", `for(;;){}`)
		return err
	})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("infinite loop not interrupted: %v after %v", err, time.Since(start))
	}
	// still usable afterwards
	if got := evalString(t, r, `1+1`); got != "2" {
		t.Fatalf("after interrupt: %q", got)
	}
}

func TestPanicsDoNotCrash(t *testing.T) {
	r := newTest(t)
	err := r.Do(func(js *Ctx) error { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Do panic: %v", err)
	}
	r.Post(func(js *Ctx) { panic("posted boom") })
	r.Expose("bad", func(js *Ctx, this Value, args []Value) (any, error) { panic("host boom") })
	if got := evalString(t, r, `try { __clack.bad() } catch (e) { 'caught:' + e.message }`); !strings.Contains(got, "host boom") {
		t.Fatalf("host panic: %s", got)
	}
	if got := evalString(t, r, `40 + 2`); got != "42" {
		t.Fatalf("after panics: %s", got)
	}
}

func TestHostFunctions(t *testing.T) {
	r := newTest(t)
	r.Expose("add", func(js *Ctx, this Value, args []Value) (any, error) {
		return args[0].Float() + args[1].Float(), nil
	})
	r.ExposeJSON("echo", func(args []json.RawMessage) (any, error) {
		return map[string]any{"got": args}, nil
	})
	r.ExposeAsync("later", func(ctx context.Context, args []json.RawMessage) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return "done:" + string(args[0]), nil
	})
	if got := evalString(t, r, `__clack.add(2, 40)`); got != "42" {
		t.Fatalf("add = %s", got)
	}
	if got := evalString(t, r, `JSON.stringify(__clack.echo({a:1}, "x"))`); got != `{"got":[{"a":1},"x"]}` {
		t.Fatalf("echo = %s", got)
	}
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("g.js", `globalThis.api = { async run(x) { const v = await __clack.later(x); return {v, ok: true} } }`)
		return err
	})
	out, err := r.Call(context.Background(), "api.run", 7)
	if err != nil || string(out) != `{"v":"done:7","ok":true}` {
		t.Fatalf("Call = %s, %v", out, err)
	}
	// errors cross both ways
	r.Expose("fail", func(js *Ctx, this Value, args []Value) (any, error) {
		return nil, &JSError{Name: "RangeError", Message: "nope"}
	})
	if got := evalString(t, r, `try { __clack.fail() } catch (e) { e.name + ':' + e.message }`); got != "RangeError:nope" {
		t.Fatalf("fail = %s", got)
	}
}

func TestModules(t *testing.T) {
	r := newTest(t)
	r.Module("lib", `export const x = 40; export default function add(a) { return a + x }`)
	r.Module("main", `import add, { x } from "lib"; import React from "react"; export const y = add(2); export const r = typeof React.useState;`)
	// top-level await on a timer (I/O-like) settles through Runtime.Import
	r.Module("tla", `await new Promise((r) => setTimeout(r, 20)); export const ready = 'yes';`)
	tla, err := r.Import("tla")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Do(func(js *Ctx) error {
		if got := tla.Get("ready").String(); got != "yes" {
			t.Errorf("tla ready = %q", got)
		}
		return nil
	})
	ns, err := r.Import("main")
	if err != nil {
		t.Fatal(err)
	}
	var y float64
	var rs string
	_ = r.Do(func(js *Ctx) error {
		y = ns.Get("y").Float()
		rs = ns.Get("r").String()
		return nil
	})
	if y != 42 || rs != "function" {
		t.Fatalf("y=%v r=%v", y, rs)
	}
}

func TestPluginIIFE(t *testing.T) {
	r := newTest(t)
	// the shape taut's build:user-plugin emits
	src := `(() => {
var TautPlugin = globalThis.TautPlugin;
class Hello extends TautPlugin { name = "Hello"; #n = 1; start() { return this.#n } }
return Hello;
})()`
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("taut.js", `globalThis.TautPlugin = class TautPlugin {}`)
		return err
	})
	err := r.Do(func(js *Ctx) error {
		cls, err := js.Eval("Hello.js", src)
		if err != nil {
			return err
		}
		inst, err := cls.New()
		if err != nil {
			return err
		}
		v, err := inst.Method("start")
		if err != nil {
			return err
		}
		if v.Int() != 1 || inst.Get("name").String() != "Hello" {
			t.Errorf("plugin: %v %v", v.Int(), inst.Get("name").String())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestJSSuites runs every testdata/*.test.js in a fresh runtime. A suite
// registers tests with test(name, fn) (fn may be async) and uses assert /
// assertEq / assertThrows.
func TestJSSuites(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.test.js")
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			r := newTest(t)
			if err := r.Do(func(js *Ctx) error {
				_, err := js.Eval("harness.js", jsHarness)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.Do(func(js *Ctx) error {
				_, err := js.Eval(filepath.Base(f), string(src))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := r.Call(ctx, "__runTests")
			if err != nil {
				t.Fatal(err)
			}
			var res []struct {
				Name string `json:"name"`
				OK   bool   `json:"ok"`
				Err  string `json:"err"`
				MS   float64
			}
			if err := json.Unmarshal(out, &res); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if len(res) == 0 {
				t.Fatal("no tests")
			}
			for _, x := range res {
				if !x.OK {
					t.Errorf("%s: %s", x.Name, x.Err)
				}
			}
		})
	}
}

const jsHarness = `
globalThis.__tests = [];
globalThis.test = (name, fn) => __tests.push({ name, fn });
const show = (v) => { try { return JSON.stringify(v) } catch { return String(v) } };
globalThis.assert = (c, msg) => { if (!c) throw new Error('assert failed' + (msg ? ': ' + msg : '')) };
globalThis.assertEq = (a, b, msg) => {
  const sa = show(a), sb = show(b);
  if (sa !== sb || (typeof a !== typeof b)) throw new Error((msg ? msg + ': ' : '') + 'got ' + sa + ' (' + typeof a + '), want ' + sb + ' (' + typeof b + ')');
};
globalThis.assertThrows = async (fn, name) => {
  try { await fn() } catch (e) { if (name && e.name !== name) throw new Error('threw ' + e.name + ': ' + e.message + ', want ' + name); return e }
  throw new Error('did not throw' + (name ? ' ' + name : ''));
};
globalThis.sleep = (ms) => new Promise((r) => setTimeout(r, ms));
globalThis.__runTests = async () => {
  const out = [];
  for (const t of __tests) {
    const t0 = performance.now();
    try {
      await Promise.race([t.fn(), new Promise((_, rej) => setTimeout(() => rej(new Error('test timed out')), 10000))]);
      out.push({ name: t.name, ok: true, MS: performance.now() - t0 });
    } catch (e) {
      out.push({ name: t.name, ok: false, err: (e && e.stack) ? e.message + '\n' + e.stack : String(e) });
    }
  }
  return out;
};
`

// TestLocalTimeZone: Date's local time is the host's zone, the same one Intl
// uses (Slick's OfficeHours builds its windows with getHours/setHours). It
// runs itself again with TZ pinned to a zone that isn't UTC.
func TestLocalTimeZone(t *testing.T) {
	if os.Getenv("JSRT_TZ_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLocalTimeZone$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "JSRT_TZ_CHILD=1", "TZ=America/New_York")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestLocalTimeZone") {
			t.Fatalf("TZ=America/New_York: %v\n%s", err, out)
		}
		return
	}
	if time.Local.String() != "America/New_York" {
		t.Fatalf("child zone %q", time.Local)
	}
	r := newTest(t)
	got := evalString(t, r, `JSON.stringify((() => {
		const winter = new Date(Date.UTC(2026, 0, 15, 17, 0)), summer = new Date(Date.UTC(2026, 6, 4, 13, 30))
		const set = new Date(Date.UTC(2026, 10, 7, 12, 0)); set.setHours(9, 0, 0, 0)
		return {
			zone: Intl.DateTimeFormat().resolvedOptions().timeZone,
			winter: [winter.getTimezoneOffset(), winter.getHours(), winter.getDate()],
			summer: [summer.getTimezoneOffset(), summer.getHours(), summer.getDay()],
			local: new Date(2026, 6, 4, 9, 30).getTime() === summer.getTime(),
			setHours: set.toISOString(),
			parsed: new Date('2026-01-15T12:00:00').getTime() === winter.getTime(),
			str: winter.toString().includes('GMT-0500'),
			intl: winter.toLocaleTimeString('en-US') === winter.toLocaleTimeString('en-US', { timeZone: 'America/New_York' }),
		}
	})())`)
	want := `{"zone":"America/New_York","winter":[300,12,15],"summer":[240,9,6],"local":true,"setHours":"2026-11-07T14:00:00.000Z","parsed":true,"str":true,"intl":true}`
	if got != want {
		t.Errorf("local time\n got %s\nwant %s", got, want)
	}
}
