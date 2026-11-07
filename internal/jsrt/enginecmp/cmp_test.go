package enginecmp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

func vendor(t testing.TB, name string) string {
	b, err := os.ReadFile(filepath.Join("..", "vendor", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// workload is shared by both engines: a fresh reconciler with a minimal
// object host config, rendering a 100-row list and serializing it.
const workload = `
(function () {
var R = require('react'), Rec = require('react-reconciler'), K = require('react-reconciler/constants');
function noop() {}
var cfg = {
  supportsMutation: true, supportsPersistence: false, supportsHydration: false, isPrimaryRenderer: false, supportsMicrotasks: false,
  noTimeout: -1, NotPendingTransition: null, HostTransitionContext: R.createContext(null),
  scheduleTimeout: function (f, ms) { return setTimeout(f, ms); }, cancelTimeout: function (id) { clearTimeout(id); },
  getRootHostContext: function () { return {}; }, getChildHostContext: function (p) { return p; },
  getPublicInstance: function (i) { return i; }, shouldSetTextContent: function (t, p) { return typeof p.children === 'string'; },
  createInstance: function (type, props) { var o = { t: type, p: {}, c: [] }; for (var k in props) if (k !== 'children') o.p[k] = props[k]; if (typeof props.children === 'string') o.c.push({ t: '#', v: props.children }); return o; },
  createTextInstance: function (v) { return { t: '#', v: v }; },
  appendInitialChild: function (p, c) { p.c.push(c); }, finalizeInitialChildren: function () { return false; },
  commitUpdate: function (i, type, prev, next) { i.p = {}; for (var k in next) if (k !== 'children') i.p[k] = next[k]; if (typeof next.children === 'string') i.c = [{ t: '#', v: next.children }]; },
  commitTextUpdate: function (t, o, n) { t.v = n; },
  appendChild: function (p, c) { p.c.push(c); }, appendChildToContainer: function (p, c) { p.c.push(c); },
  insertBefore: function (p, c, b) { var i = p.c.indexOf(c); if (i >= 0) p.c.splice(i, 1); p.c.splice(p.c.indexOf(b), 0, c); },
  insertInContainerBefore: function (p, c, b) { cfg.insertBefore(p, c, b); },
  removeChild: function (p, c) { p.c.splice(p.c.indexOf(c), 1); }, removeChildFromContainer: function (p, c) { p.c.splice(p.c.indexOf(c), 1); },
  clearContainer: function (c) { c.c = []; }, prepareForCommit: function () { return null; }, resetAfterCommit: noop,
  setCurrentUpdatePriority: function (p) { cfg._p = p; }, getCurrentUpdatePriority: function () { return cfg._p || 0; },
  resolveUpdatePriority: function () { return cfg._p || K.DefaultEventPriority; }, resolveEventType: function () { return null; },
  resolveEventTimeStamp: function () { return -1.1; }, shouldAttemptEagerTransition: function () { return false; },
  maySuspendCommit: function () { return false; }, preloadInstance: function () { return true; }, waitForCommitToBeReady: function () { return null; },
  getInstanceFromNode: function () { return null; }, detachDeletedInstance: noop, trackSchedulerEvent: noop
};
var config = new Proxy(cfg, { get: function (t, k) { return k in t ? t[k] : (typeof k === 'string' && /^(supports|is[A-Z])/.test(k) ? false : noop); } });
var rec = Rec(config);
var container = { c: [] };
var root = rec.createContainer(container, K.ConcurrentRoot, null, false, null, '', function (e) { throw e; }, noop, noop, null);
function List(p) { var kids = []; for (var i = 0; i < 100; i++) kids.push(R.createElement('li', { key: i, className: 'item', style: { padding: 2 } }, 'row ' + (i + p.seed))); return R.createElement('ul', { className: 'list' }, kids); }
globalThis.renderOnce = function (seed) { rec.updateContainerSync(R.createElement(List, { seed: seed }), root, null, null); rec.flushSyncWork(); return JSON.stringify(container.c).length; };
globalThis.add = function (a, b) { return a + b; };
})();
`

// ---- goja ----

type gojaEnv struct {
	vm     *goja.Runtime
	timers []goja.Callable
}

func newGoja(t testing.TB) *gojaEnv {
	g := &gojaEnv{vm: goja.New()}
	vm := g.vm
	_ = vm.Set("setTimeout", func(fn goja.Callable, ms float64) int { g.timers = append(g.timers, fn); return len(g.timers) })
	_ = vm.Set("clearTimeout", func(int) {})
	_ = vm.Set("queueMicrotask", func(fn goja.Callable) { g.timers = append(g.timers, fn) })
	mods := map[string]string{
		"react":                      vendor(t, "react.production.js"),
		"scheduler":                  vendor(t, "scheduler.production.js"),
		"react-reconciler":           vendor(t, "react-reconciler.production.js"),
		"react-reconciler/constants": vendor(t, "react-reconciler-constants.production.js"),
	}
	cache := map[string]goja.Value{}
	var require func(name string) goja.Value
	require = func(name string) goja.Value {
		if v, ok := cache[name]; ok {
			return v
		}
		src, ok := mods[name]
		if !ok {
			panic(vm.NewTypeError("no module " + name))
		}
		fnv, err := vm.RunScript(name, "(function (module, exports, require) {\n"+src+"\n})")
		if err != nil {
			t.Fatal(err)
		}
		fn, _ := goja.AssertFunction(fnv)
		module := vm.NewObject()
		exports := vm.NewObject()
		_ = module.Set("exports", exports)
		cache[name] = exports
		if _, err := fn(goja.Undefined(), module, exports, vm.ToValue(require)); err != nil {
			t.Fatal(err)
		}
		cache[name] = module.Get("exports")
		return cache[name]
	}
	_ = vm.Set("require", require)
	return g
}

// ---- QuickJS-NG (jsrt) ----

func newQJS(t testing.TB) *jsrt.Runtime {
	r, err := jsrt.New(jsrt.Options{Log: func(string, string) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Do(func(js *jsrt.Ctx) error {
		_, err := js.Eval("req.js", `globalThis.require = __jr.require`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCompare(t *testing.T) {
	// startup: engine + React + reconciler ready
	t0 := time.Now()
	g := newGoja(t)
	if _, err := g.vm.RunString(workload); err != nil {
		t.Fatalf("goja workload: %v", err)
	}
	gojaStart := time.Since(t0)
	t0 = time.Now()
	q := newQJS(t)
	defer q.Close()
	if err := q.Do(func(js *jsrt.Ctx) error { _, err := js.Eval("w.js", workload); return err }); err != nil {
		t.Fatal(err)
	}
	qjsStart := time.Since(t0)
	t.Logf("startup incl. full env (qjs) / bare + React (goja): qjs %v, goja %v", qjsStart, gojaStart)

	// render 100 elements
	const n = 200
	render := g.vm.Get("renderOnce")
	rf, _ := goja.AssertFunction(render)
	t0 = time.Now()
	for i := 0; i < n; i++ {
		if _, err := rf(goja.Undefined(), g.vm.ToValue(i)); err != nil {
			t.Fatal(err)
		}
	}
	gojaRender := time.Since(t0) / n
	var qjsRender time.Duration
	_ = q.Do(func(js *jsrt.Ctx) error {
		fn := js.Global().Get("renderOnce")
		t0 := time.Now()
		for i := 0; i < n; i++ {
			if _, err := fn.Call(i); err != nil {
				return err
			}
		}
		qjsRender = time.Since(t0) / n
		return nil
	})
	t.Logf("React render+commit+serialize of 100 <li>: qjs %v, goja %v", qjsRender, gojaRender)

	// Go → JS call
	const calls = 20000
	af, _ := goja.AssertFunction(g.vm.Get("add"))
	t0 = time.Now()
	for i := 0; i < calls; i++ {
		_, _ = af(goja.Undefined(), g.vm.ToValue(i), g.vm.ToValue(1))
	}
	gojaCall := time.Since(t0) / calls
	var qjsCall time.Duration
	_ = q.Do(func(js *jsrt.Ctx) error {
		fn := js.Global().Get("add")
		t0 := time.Now()
		for i := 0; i < calls; i++ {
			_, _ = fn.Call(i, 1)
		}
		qjsCall = time.Since(t0) / calls
		return nil
	})
	t.Logf("Go→JS call add(i,1): qjs %v, goja %v", qjsCall, gojaCall)

	// CPU-bound JS
	const cpu = `(function(){ var s = 0; for (var i = 0; i < 3e6; i++) { s += (i * 7) % 13; } var a = []; for (var j = 0; j < 2e5; j++) a.push({ k: 'x' + j, v: j }); a.sort(function (x, y) { return y.v - x.v; }); return s + a.length + JSON.stringify(a.slice(0, 100)).length; })()`
	t0 = time.Now()
	if _, err := g.vm.RunString(cpu); err != nil {
		t.Fatal(err)
	}
	gojaCPU := time.Since(t0)
	t0 = time.Now()
	_ = q.Do(func(js *jsrt.Ctx) error { _, err := js.Eval("cpu.js", cpu); return err })
	qjsCPU := time.Since(t0)
	t.Logf("CPU loop + 200k object sort: qjs %v, goja %v", qjsCPU, gojaCPU)
}

// TestGojaFeatures lists the ES2023+ features Taut plugins (esbuild output,
// no downleveling) need that goja lacks.
func TestGojaFeatures(t *testing.T) {
	feats := map[string]string{
		"private fields":               `class A { #x = 1; get x() { return this.#x } }; new A().x`,
		"private methods":              `class A { #m() { return 1 } r() { return this.#m() } }; new A().r()`,
		"static blocks":                `class A { static v; static { A.v = 1 } }; A.v`,
		"#x in obj":                    `class A { #x; static has(o) { return #x in o } }; A.has(new A())`,
		"async/await":                  `(async () => { await 1; return 2 })(); 1`,
		"async generators":             `(async function* () { yield 1 })(); 1`,
		"for await":                    `(async () => { for await (const x of [1]) {} })(); 1`,
		"optional chaining":            `({a:{}}).a?.b?.c ?? 1`,
		"logical assignment":           `let a = null; a ??= 1; a ||= 2; a &&= 3; a`,
		"numeric separators":           `1_000_000`,
		"BigInt":                       `(10n ** 20n).toString()`,
		"WeakRef":                      `new WeakRef({}); 1`,
		"FinalizationRegistry":         `new FinalizationRegistry(() => {}); 1`,
		"regex lookbehind":             `/(?<=a)b/.test('ab')`,
		"regex named groups":           `'2026-11'.match(/(?<y>\d+)-(?<m>\d+)/).groups.y`,
		"regex \\p{L} /u":              `/\p{L}/u.test('é')`,
		"regex /d indices":             `/a/d.exec('a').indices[0][0]`,
		"regex /v sets":                `/[\p{L}--[a-z]]/v.test('A')`,
		"Array#at/findLast":            `[1,2].at(-1) + [1,2].findLast(x => x)`,
		"Array#toSorted/with":          `[2,1].toSorted()[0] + [1].with(0, 5)[0]`,
		"Object.hasOwn":                `Object.hasOwn({a:1}, 'a')`,
		"Object.groupBy":               `Object.groupBy([1], x => 'a').a.length`,
		"Promise.withResolvers":        `Promise.withResolvers(); 1`,
		"Set#union":                    `new Set([1]).union(new Set([2])).size`,
		"Iterator helpers":             `[1,2,3].values().map(x => x * 2).toArray().length`,
		"Error cause":                  `new Error('a', { cause: 1 }).cause`,
		"String#replaceAll":            `'aa'.replaceAll('a', 'b')`,
		"Array.fromAsync":              `typeof Array.fromAsync`,
		"top-level / dynamic import()": `typeof import('x')`,
		"Intl":                         `typeof Intl.DateTimeFormat`,
		"structuredClone":              `typeof structuredClone`,
	}
	var missing []string
	for name, src := range feats {
		vm := goja.New()
		v, err := vm.RunString(src)
		if err != nil || v == nil || v.String() == "undefined" {
			missing = append(missing, name)
		}
	}
	t.Logf("goja lacks %d/%d: %s", len(missing), len(feats), strings.Join(missing, "; "))
	// the same list on QuickJS-NG (structuredClone/Intl come from env.js/intl.js)
	q := newQJS(t)
	defer q.Close()
	var qmissing []string
	for name, src := range feats {
		_ = q.Do(func(js *jsrt.Ctx) error {
			v, err := js.Eval("f.js", src)
			if err != nil || v.IsUndefined() || v.String() == "undefined" {
				qmissing = append(qmissing, name)
			}
			return nil
		})
	}
	t.Logf("qjs lacks %d/%d: %s", len(qmissing), len(feats), strings.Join(qmissing, "; "))
}
