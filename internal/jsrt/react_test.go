package jsrt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// node mirrors the host tree for assertions.
type node struct {
	T  string          `json:"t"`
	K  int             `json:"k"`
	N  string          `json:"n"`
	V  string          `json:"v"`
	P  map[string]any  `json:"p"`
	On map[string]int  `json:"on"`
	C  []node          `json:"c"`
	F  bool            `json:"focus"`
	Rw json.RawMessage `json:"-"`
}

func (n node) text() string {
	if n.T == "#" {
		return n.V
	}
	var b strings.Builder
	for _, c := range n.C {
		b.WriteString(c.text())
	}
	return b.String()
}

func find(ns []node, pred func(node) bool) *node {
	for i := range ns {
		if pred(ns[i]) {
			return &ns[i]
		}
		if f := find(ns[i].C, pred); f != nil {
			return f
		}
	}
	return nil
}

// treeRecv collects commits of one root.
type treeRecv struct{ ch chan []node }

func newRecv() *treeRecv { return &treeRecv{ch: make(chan []node, 64)} }

func (tr *treeRecv) cb(t testing.TB) func([]byte) {
	return func(b []byte) {
		var ns []node
		if err := json.Unmarshal(b, &ns); err != nil {
			t.Errorf("bad tree %s: %v", b, err)
			return
		}
		tr.ch <- ns
	}
}

func (tr *treeRecv) next(t testing.TB) []node {
	t.Helper()
	select {
	case ns := <-tr.ch:
		return ns
	case <-time.After(5 * time.Second):
		t.Fatal("no commit")
		return nil
	}
}

// waitFor reads commits until pred holds.
func (tr *treeRecv) waitFor(t testing.TB, what string, pred func([]node) bool) []node {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ns := <-tr.ch:
			if pred(ns) {
				return ns
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
			return nil
		}
	}
}

func renderJS(t testing.TB, r *Runtime, root *Root, src string) {
	t.Helper()
	err := r.Do(func(js *Ctx) error {
		el, err := js.Eval("render.js", src)
		if err != nil {
			return err
		}
		return root.Render(js, el)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReactHooksAndEvents(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
const { useState, useEffect, useRef, createContext, useContext, useMemo, useReducer, useLayoutEffect, useId } = React;
const Theme = createContext('light');
function Counter({ start }) {
  const [n, setN] = useState(start);
  const [log, add] = useReducer((s, x) => s.concat(x), []);
  const ref = useRef(null);
  const theme = useContext(Theme);
  const id = useId();
  useLayoutEffect(() => { add('layout'); }, []);
  useEffect(() => { add('effect:' + n); }, [n]);
  const doubled = useMemo(() => n * 2, [n]);
  return React.createElement('div', { className: 'counter ' + theme, style: { padding: 4, flexGrow: 1 }, 'data-id': id },
    React.createElement('button', { ref, onClick: (e) => { e.stopPropagation(); setN(n + 1); }, 'aria-label': 'inc' }, 'n=', n),
    React.createElement('span', null, 'x2=' + doubled),
    React.createElement('i', null, log.join(',')));
}
React.createElement(Theme.Provider, { value: 'dark' }, React.createElement(Counter, { start: 1 }))`)
	ns := rec.next(t)
	div := find(ns, func(n node) bool { return n.T == "div" })
	if div == nil || div.P["className"] != "counter dark" {
		t.Fatalf("tree: %+v", ns)
	}
	if st, _ := div.P["style"].(map[string]any); st["padding"] != float64(4) || st["flexGrow"] != float64(1) {
		t.Fatalf("style kept raw: %v", div.P["style"])
	}
	btn := find(ns, func(n node) bool { return n.T == "button" })
	if btn == nil || btn.On["click"] != btn.K || btn.text() != "n=1" || btn.P["aria-label"] != "inc" {
		t.Fatalf("button: %+v", btn)
	}
	// effects run → state update → the log shows up (possibly already in the first commit)
	effectsDone := func(ns []node) bool {
		i := find(ns, func(n node) bool { return n.T == "i" })
		return i != nil && i.text() == "layout,effect:1"
	}
	if !effectsDone(ns) {
		ns = rec.waitFor(t, "effects", effectsDone)
	}
	// click the button (bubbles; stopPropagation reported)
	res, err := r.DispatchEventSync(context.Background(), btn.K, []byte(`{"type":"click","clientX":5,"clientY":5,"button":0}`))
	if err != nil || !res.PropagationStopped {
		t.Fatalf("dispatch: %+v %v", res, err)
	}
	ns = rec.waitFor(t, "n=2", func(ns []node) bool {
		b := find(ns, func(n node) bool { return n.T == "button" })
		return b != nil && b.text() == "n=2"
	})
	b2 := find(ns, func(n node) bool { return n.T == "button" })
	if b2.K != btn.K {
		t.Fatalf("k not stable: %d vs %d", b2.K, btn.K)
	}
	root.Unmount()
}

func TestReactControlledInputAndForm(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
function F() {
  const [v, setV] = React.useState('');
  const [sent, setSent] = React.useState([]);
  return React.createElement('form', { onSubmit: (e) => { e.preventDefault(); setSent(sent.concat(v)); setV(''); } },
    React.createElement('input', { value: v, placeholder: 'Type', onChange: (e) => setV(e.target.value.toUpperCase()) }),
    React.createElement('button', { type: 'submit' }, 'Send'),
    React.createElement('ul', null, sent.map((s, i) => React.createElement('li', { key: i }, s))));
}
React.createElement(F)`)
	ns := rec.next(t)
	in := find(ns, func(n node) bool { return n.T == "input" })
	if in == nil || in.P["value"] != "" || in.P["placeholder"] != "Type" || in.On["change"] == 0 {
		t.Fatalf("input: %+v", in)
	}
	r.DispatchEvent(in.K, []byte(`{"type":"input","target":{"value":"hi"}}`))
	rec.waitFor(t, "HI", func(ns []node) bool {
		i := find(ns, func(n node) bool { return n.T == "input" })
		return i != nil && i.P["value"] == "HI"
	})
	btn := find(ns, func(n node) bool { return n.T == "button" })
	r.DispatchEvent(btn.K, []byte(`{"type":"click","button":0}`))
	rec.waitFor(t, "submitted", func(ns []node) bool {
		li := find(ns, func(n node) bool { return n.T == "li" })
		i := find(ns, func(n node) bool { return n.T == "input" })
		return li != nil && li.text() == "HI" && i.P["value"] == ""
	})
}

func TestNativeComponentsPortalsSuspense(t *testing.T) {
	r := newTest(t)
	bodyCh := make(chan []node, 16)
	r.OnDocument(func(body []byte, css []string) {
		var ns []node
		_ = json.Unmarshal(body, &ns)
		bodyCh <- ns
	})
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
globalThis.clicked = 0;
const Avatar = __clack.native('BaseAvatarStack');
let resolve;
const Lazy = React.lazy(() => new Promise((r) => { resolve = r; setTimeout(() => r({ default: () => React.createElement('b', null, 'loaded') }), 20); }));
function App() {
  return React.createElement(React.Fragment, null,
    React.createElement(Avatar, { channelId: 'C1', memberCount: 3, goFn: Object.assign(() => {}, { $gofn: 7 }), slot: { $slot: 'x' }, onClick: () => { clicked++; return 'ret'; }, tooltip: React.createElement('em', null, 'tip') },
      React.createElement('span', null, 'kid')),
    React.createElement(__clack.native('MenuItem'), { $slot: 5, label: 'Set nickname', click: () => { clicked += 10; } }),
    ReactDOM.createPortal(React.createElement('div', { className: 'overlay' }, 'over'), document.body),
    React.createElement(React.Suspense, { fallback: React.createElement('i', null, 'loading') }, React.createElement(Lazy)));
}
React.createElement(App)`)
	ns := rec.next(t)
	av := find(ns, func(n node) bool { return n.T == "@" })
	if av == nil || av.N != "BaseAvatarStack" || av.P["channelId"] != "C1" || av.P["memberCount"] != float64(3) {
		t.Fatalf("native: %+v", ns)
	}
	fn, _ := av.P["onClick"].(map[string]any)
	if fn == nil || fn["$fn"] == nil {
		t.Fatalf("$fn: %v", av.P)
	}
	tip, _ := av.P["tooltip"].(map[string]any)
	if tip == nil || !strings.Contains(string(mustJSON(tip)), `"t":"em"`) {
		t.Fatalf("$el: %v", av.P["tooltip"])
	}
	if len(av.C) != 1 || av.C[0].text() != "kid" {
		t.Fatalf("native children: %+v", av.C)
	}
	if find(ns, func(n node) bool { return n.T == "i" && n.text() == "loading" }) == nil {
		t.Fatalf("suspense fallback missing: %+v", ns)
	}
	if g, _ := av.P["goFn"].(map[string]any); g["$gofn"] != float64(7) {
		t.Fatalf("$gofn passthrough: %v", av.P["goFn"])
	}
	if sl, _ := av.P["slot"].(map[string]any); sl["$slot"] != "x" {
		t.Fatalf("$slot passthrough: %v", av.P["slot"])
	}
	// a slot component's own props keep their functions (plugin menu rows)
	mi := find(ns, func(n node) bool { return n.T == "@" && n.N == "MenuItem" })
	if mi == nil || mi.P["$slot"] != float64(5) || mi.P["label"] != "Set nickname" {
		t.Fatalf("slot component: %+v", mi)
	}
	if c, _ := mi.P["click"].(map[string]any); c == nil || c["$fn"] == nil {
		t.Fatalf("slot component lost its click fn: %v", mi.P)
	}
	r.CallFn(int(fn["$fn"].(float64)), []byte(`[]`))
	if got := evalString(t, r, `clicked`); got != "1" {
		t.Fatalf("CallFn: clicked=%s", got)
	}
	if got := evalString(t, r, `__clack.fnById(`+itoa(int(fn["$fn"].(float64)))+`)()`); got != "ret" {
		t.Fatalf("fnById: %s", got)
	}
	rec.waitFor(t, "lazy loaded", func(ns []node) bool {
		return find(ns, func(n node) bool { return n.T == "b" && n.text() == "loaded" }) != nil
	})
	// the subscriber first gets the current state, then the portal's commit
	deadline := time.After(5 * time.Second)
	for found := false; !found; {
		select {
		case body := <-bodyCh:
			found = find(body, func(n node) bool { return n.T == "div" && n.P["className"] == "overlay" }) != nil
		case <-deadline:
			t.Fatal("no document update for the portal")
		}
	}
}

func TestRefsRectsAndDOMListeners(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
globalThis.seen = [];
function M() {
  const ref = React.useRef(null);
  const [w, setW] = React.useState(0);
  React.useEffect(() => {
    const el = ref.current;
    const ro = new ResizeObserver(() => setW(el.getBoundingClientRect().width));
    ro.observe(el);
    el.addEventListener('keydown', (e) => { seen.push('dom:' + e.key); if (e.key === 'Enter') e.preventDefault(); });
    window.addEventListener('keydown', (e) => seen.push('win-capture:' + e.key), true);
    return () => ro.disconnect();
  }, []);
  return React.createElement('div', { ref, onKeyDown: (e) => seen.push('react:' + e.key), tabIndex: 0 }, 'w=' + w);
}
React.createElement(M)`)
	ns := rec.next(t)
	div := find(ns, func(n node) bool { return n.T == "div" })
	if div.P["tabIndex"] != float64(0) || div.On["keydown"] == 0 {
		t.Fatalf("div: %+v", div)
	}
	r.SetRects(map[int]Rect{div.K: {X: 10, Y: 20, W: 300, H: 40}})
	rec.waitFor(t, "w=300", func(ns []node) bool {
		d := find(ns, func(n node) bool { return n.T == "div" })
		return d != nil && d.text() == "w=300"
	})
	res, err := r.DispatchEventSync(context.Background(), div.K, []byte(`{"type":"keydown","key":"Enter","code":"Enter"}`))
	if err != nil || !res.DefaultPrevented {
		t.Fatalf("keydown: %+v %v", res, err)
	}
	if got := evalString(t, r, `seen.join(' ')`); got != "win-capture:Enter dom:Enter react:Enter" {
		t.Fatalf("order: %s", got)
	}
	if !r.WantsEvent("keydown") || r.WantsEvent("mousemove") {
		t.Fatalf("WantsEvent: keydown=%v mousemove=%v", r.WantsEvent("keydown"), r.WantsEvent("mousemove"))
	}
	// a document-level mousemove (target 0) reaches document listeners
	evalString(t, r, `document.addEventListener('mousemove', (e) => seen.push('move:' + e.clientX)); 0`)
	if !r.WantsEvent("mousemove") {
		t.Fatal("WantsEvent(mousemove) after addEventListener")
	}
	if _, err := r.DispatchEventSync(context.Background(), 0, []byte(`{"type":"mousemove","clientX":42,"clientY":1}`)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, r, `seen.at(-1)`); got != "move:42" {
		t.Fatalf("document mousemove: %s", got)
	}
}

// TestSamplePlugin renders a Taut-style plugin component that wraps the
// original Slack component, the way patchComponent works.
func TestSamplePlugin(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("taut-shim.js", `
globalThis.TautPlugin = class TautPlugin { constructor(api) { this.api = api } };
globalThis.patches = new Map();
globalThis.makeAPI = () => ({ patchComponent(name, fn) { patches.set(name, fn(__clack.native(name))); return () => patches.delete(name); } });`)
		if err != nil {
			return err
		}
		cls, err := js.Eval("Pronouns.js", `(() => {
var TautPlugin = globalThis.TautPlugin;
class Pronouns extends TautPlugin {
  #cache = new Map();
  start() {
    this.api.patchComponent('MessageSender', (Original) => (props) => {
      const [p, setP] = React.useState(this.#cache.get(props.userId) ?? null);
      React.useEffect(() => { let off = false; (async () => { const r = await fetch('http://test/json?q=' + props.userId); const j = await r.json(); if (!off) { this.#cache.set(props.userId, j.q); setP(j.q); } })(); return () => { off = true } }, [props.userId]);
      return React.createElement(React.Fragment, null, React.createElement(Original, props), p && React.createElement('span', { className: 'pronouns' }, ' (', p, ')'));
    });
  }
}
return Pronouns;
})()`)
		if err != nil {
			return err
		}
		api, err := js.Eval("api.js", `makeAPI()`)
		if err != nil {
			return err
		}
		inst, err := cls.New(api)
		if err != nil {
			return err
		}
		if _, err := inst.Method("start"); err != nil {
			return err
		}
		el, err := js.Eval("el.js", `React.createElement(patches.get('MessageSender'), { userId: 'U123', name: 'karim' })`)
		if err != nil {
			return err
		}
		return root.Render(js, el)
	})
	if err != nil {
		t.Fatal(err)
	}
	ns := rec.next(t)
	if o := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageSender" }); o == nil || o.P["userId"] != "U123" {
		t.Fatalf("original: %+v", ns)
	}
	rec.waitFor(t, "pronouns via fetch", func(ns []node) bool {
		s := find(ns, func(n node) bool { return n.T == "span" && n.P["className"] == "pronouns" })
		return s != nil && s.text() == " (U123)"
	})
}

func TestDocumentProps(t *testing.T) {
	r := newTest(t)
	ch := make(chan [2]string, 8)
	r.OnDocumentProps(func(html, body []byte) { ch <- [2]string{string(html), string(body)} })
	evalString(t, r, `document.documentElement.classList.add('streamer'); document.documentElement.style.setProperty('--sidebar-width', '260px'); document.body.dataset.theme = 'dark'; 0`)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case x := <-ch:
			if strings.Contains(x[0], `"className":"streamer"`) && strings.Contains(x[0], `"--sidebar-width":"260px"`) && strings.Contains(x[1], `"data-theme":"dark"`) {
				return
			}
		case <-deadline:
			t.Fatal("no document props")
		}
	}
}

func TestFileInputPicker(t *testing.T) {
	r := newTest(t, func(o *Options) {
		o.PickFiles = func(accept string, multiple bool) ([]PickedFile, error) {
			return []PickedFile{{Name: "name.webm", Type: "audio/webm", Data: []byte{1, 2, 3}}}, nil
		}
	})
	evalString(t, r, `globalThis.picked = new Promise((resolve) => {
  const input = document.createElement('input');
  Object.assign(input, { type: 'file', accept: 'audio/*' });
  input.style.display = 'none';
  input.addEventListener('change', async () => { const f = input.files[0]; resolve(f.name + ':' + f.type + ':' + (await f.arrayBuffer()).byteLength + ':' + (f instanceof File)); input.remove(); });
  document.body.appendChild(input);
  input.click();
}); 0`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	evalString(t, r, `globalThis.getPicked = () => picked; 0`)
	got, err := r.Call(ctx, "getPicked")
	if err != nil || string(got) != `"name.webm:audio/webm:3:true"` {
		t.Fatalf("picked: %s %v", got, err)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// ---- benchmarks & timings ----

func TestStartupTiming(t *testing.T) {
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		r, err := New(Options{Log: func(string, string) {}})
		if err != nil {
			t.Fatal(err)
		}
		st := r.Stats()
		var ph []string
		for _, p := range st.Phases {
			ph = append(ph, p.Name+"="+p.Time.Round(10*time.Microsecond).String())
		}
		t.Logf("New #%d: %v (boot %v, heap %d KiB) %s", i, time.Since(t0), st.Boot, st.Memory>>10, strings.Join(ph, " "))
		r.Close()
	}
}

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r, err := New(Options{Log: func(string, string) {}})
		if err != nil {
			b.Fatal(err)
		}
		r.Close()
	}
}

func BenchmarkDoRoundTrip(b *testing.B) {
	r := newTest(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Do(func(js *Ctx) error { return nil })
	}
}

func BenchmarkCallJS(b *testing.B) {
	r := newTest(b)
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("f.js", `globalThis.add = (a, b) => a + b`)
		return err
	})
	_ = r.Do(func(js *Ctx) error {
		fn := js.Global().Get("add")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := fn.Call(i, 1); err != nil {
				b.Fatal(err)
			}
		}
		return nil
	})
}

func BenchmarkHostCall(b *testing.B) {
	r := newTest(b)
	r.Expose("nop", func(js *Ctx, this Value, args []Value) (any, error) { return nil, nil })
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("f.js", `globalThis.loop = (n) => { const f = __clack.nop; for (let i = 0; i < n; i++) f(i) }`)
		return err
	})
	_ = r.Do(func(js *Ctx) error {
		fn := js.Global().Get("loop")
		b.ResetTimer()
		_, err := fn.Call(b.N)
		return err
	})
}

func BenchmarkCallJSON(b *testing.B) {
	r := newTest(b)
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("f.js", `globalThis.api = { get: (o) => ({ id: o.id, name: 'x' + o.id }) }`)
		return err
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Call(context.Background(), "api.get", map[string]any{"id": i}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReactRender100 renders a fresh 100-element list and serializes it.
func BenchmarkReactRender100(b *testing.B) {
	r := newTest(b)
	done := make(chan struct{}, 1)
	root := r.NewRoot(func([]byte) {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	_ = r.Do(func(js *Ctx) error {
		_, err := js.Eval("list.js", `globalThis.List = ({ n, seed }) => React.createElement('ul', { className: 'list' },
  Array.from({ length: n }, (_, i) => React.createElement('li', { key: i, className: 'item', style: { padding: 2 }, onClick: () => {} }, 'row ' + (i + seed))));`)
		return err
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Do(func(js *Ctx) error {
			el, err := js.Eval("el.js", `React.createElement(List, { n: 100, seed: `+itoa(i)+` })`)
			if err != nil {
				return err
			}
			return root.Render(js, el)
		})
		<-done
	}
}

// BenchmarkReactUpdate measures a state update of one row in a 100-row list
// (event → re-render → commit delivered).
func BenchmarkReactUpdate(b *testing.B) {
	r := newTest(b)
	ch := make(chan []byte, 1)
	root := r.NewRoot(func(t []byte) { ch <- t })
	_ = r.Do(func(js *Ctx) error {
		el, err := js.Eval("list.js", `function L() {
  const [sel, setSel] = React.useState(-1);
  globalThis.setSel = setSel;
  return React.createElement('ul', null, Array.from({ length: 100 }, (_, i) => React.createElement('li', { key: i, className: i === sel ? 'sel' : '' }, 'row ' + i)));
}
React.createElement(L)`)
		if err != nil {
			return err
		}
		return root.Render(js, el)
	})
	<-ch
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Post(func(js *Ctx) { _, _ = js.Global().Get("setSel").Call(i % 100) })
		<-ch
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// TestPortalRootSeesParentContext: a root rendered as a portal at a native
// component inside another root's tree (a patched MessageText under a
// patched message row) sees the providers around that native, keeps its
// own state across updates, delivers to its own root, and goes away on
// Unmount.
func TestPortalRootSeesParentContext(t *testing.T) {
	r := newTest(t)
	parentRec, childRec := newRecv(), newRecv()
	parent := r.NewRoot(parentRec.cb(t))
	child := r.NewRoot(childRec.cb(t))
	renderJS(t, r, parent, `
globalThis.Ctx = React.createContext('none');
const Row = __clack.native('MessageWrapper');
React.createElement(Ctx.Provider, { value: 'from-row-patch' }, React.createElement('div', null, React.createElement(Row, { ts: '1.1' })))`)
	ns := parentRec.next(t)
	row := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageWrapper" })
	if row == nil || row.K == 0 {
		t.Fatalf("no native row: %+v", ns)
	}
	portal := func(label string) {
		t.Helper()
		err := r.Do(func(js *Ctx) error {
			// one component type across updates, as taut's componentElement
			// gives per Slack name, so its state survives
			el, err := js.Eval("child.js", `
globalThis.PortalText = globalThis.PortalText || function PortalText(props) {
  const v = React.useContext(Ctx);
  const [n] = React.useState(() => (globalThis.mounts = (globalThis.mounts || 0) + 1));
  return React.createElement('span', { className: 'text' }, v + ':' + props.label + ':' + n);
};
React.createElement(globalThis.PortalText, { label: '`+label+`' })`)
			if err != nil {
				return err
			}
			return child.RenderPortal(js, int64(row.K), el)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	portal("a")
	childRec.waitFor(t, "the portal sees the row's provider", func(ns []node) bool {
		s := find(ns, func(n node) bool { return n.T == "span" })
		return s != nil && s.text() == "from-row-patch:a:1"
	})
	portal("b") // an update keeps the instance (state survives)
	childRec.waitFor(t, "the updated portal", func(ns []node) bool {
		s := find(ns, func(n node) bool { return n.T == "span" })
		return s != nil && s.text() == "from-row-patch:b:1"
	})
	for drained := false; !drained; {
		select {
		case pt := <-parentRec.ch:
			if find(pt, func(n node) bool { return n.T == "span" }) != nil {
				t.Errorf("the portal leaked into the parent's tree: %+v", pt)
			}
		default:
			drained = true
		}
	}
	child.Unmount()
	if got := evalString(t, r, `__clack.portalCount()`); got != "0" {
		t.Errorf("portals after Unmount: %s", got)
	}
}
