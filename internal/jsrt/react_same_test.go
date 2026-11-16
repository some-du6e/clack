package jsrt

import (
	"testing"
	"time"
)

// React commits a root whenever an update ran in it, also when the update
// changed nothing on screen (a plugin's usePatchVersion after
// api.redux.refresh, a store subscription whose value didn't change, a
// portal mounted under one of its natives). Each such commit used to send
// the root's same tree to Go again: 650 to 4,000 in a perf pass. So did an
// event Go dispatched to a node (every pointer event over a plugin's node
// as rows scroll under the pointer), a handler replaced by a new closure
// and a listener added again. A commit of JS's own updates now sends one
// only when what the tree holds changed; what Go renders always answers.

// quiet fails if rec gets commits within d (it takes them all, so the
// next check starts clean).
func quiet(t *testing.T, rec *treeRecv, d time.Duration, what string) {
	t.Helper()
	n := 0
	var first []node
	for end := time.After(d); ; {
		select {
		case ns := <-rec.ch:
			if n == 0 {
				first = ns
			}
			n++
			continue
		case <-end:
		}
		break
	}
	if n > 0 {
		t.Errorf("%s: %d commits of the same tree reached Go, the first: %+v", what, n, first)
	}
}

func TestSameOutputSendsNoTree(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
globalThis.Row = __clack.native('MessageWrapper');
globalThis.keep = () => {};
globalThis.Same = function Same() {
  const [n, setN] = React.useState(0);
  const [, poke] = React.useReducer((x) => x + 1, 0);
  const ref = React.useRef(null);
  globalThis.poke = poke;
  globalThis.setLabel = setN;
  React.useLayoutEffect(() => {
    const el = ref.current;
    el.addEventListener('mouseover', keep);
    return () => el.removeEventListener('mouseover', keep);
  }, []);
  // an effect that listens afresh after every render (its type stays handled)
  React.useLayoutEffect(() => {
    const el = ref.current, f = () => {};
    el.addEventListener('mouseover', f);
    return () => el.removeEventListener('mouseover', f);
  });
  // new handler closures every render; the native gets the same props but for a label change
  return React.createElement('div', { className: 'row', ref, onClick: () => {}, onMouseEnter: () => {} },
    React.createElement(Row, { ts: '1.1', label: 'n' + n }));
};
React.createElement(Same)`)
	ns := rec.next(t)
	row := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageWrapper" })
	div := find(ns, func(n node) bool { return n.T == "div" && n.P["className"] == "row" })
	if row == nil || row.K == 0 || div == nil {
		t.Fatalf("no native row: %+v", ns)
	}
	if div.On["click"] == 0 || div.On["mouseenter"] == 0 || div.On["mouseover"] == 0 {
		t.Fatalf("the row's handled events: %v", div.On)
	}

	// a state update that renders the same (new closures, listeners
	// swapped): nothing goes to Go
	for i := 0; i < 3; i++ {
		r.Post(func(js *Ctx) { _, _ = js.Global().Get("poke").Call() })
	}
	quiet(t, rec, 300*time.Millisecond, "re-rendered with the same output")

	// pointer events that change nothing on their target (rows passing
	// under a still pointer as a list scrolls), with the target state
	// slackc sends with every event
	for _, typ := range []string{"mouseenter", "mouseover", "mouseleave"} {
		r.DispatchEvent(div.K, []byte(`{"type":"`+typ+`","clientX":4,"clientY":4,"buttons":0,"target":{},"rects":{}}`))
	}
	quiet(t, rec, 300*time.Millisecond, "pointer events over the row")

	// one that changes the native's props: its tree goes
	r.Post(func(js *Ctx) { _, _ = js.Global().Get("setLabel").Call(1) })
	rec.waitFor(t, "the native's new props", func(ns []node) bool {
		n := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageWrapper" })
		return n != nil && n.P["label"] == "n1"
	})

	// a portal mounted under the native: the portal's own tree goes, the
	// parent's (whose host nodes didn't change) doesn't
	childRec := newRecv()
	child := r.NewRoot(childRec.cb(t))
	err := r.Do(func(js *Ctx) error {
		el, err := js.Eval("child.js", `React.createElement('span', { className: 'text' }, 'inside')`)
		if err != nil {
			return err
		}
		return child.RenderPortal(js, int64(row.K), el)
	})
	if err != nil {
		t.Fatal(err)
	}
	childRec.waitFor(t, "the portal's tree", func(ns []node) bool {
		s := find(ns, func(n node) bool { return n.T == "span" })
		return s != nil && s.text() == "inside"
	})
	quiet(t, rec, 300*time.Millisecond, "a portal mounted under the native")

	// what Go renders answers, the same tree too (Go drops one it has)
	renderJS(t, r, root, `React.createElement(Same)`)
	rec.waitFor(t, "Go's render answered", func(ns []node) bool { return find(ns, func(n node) bool { return n.T == "@" }) != nil })
}

// A portal Go renders under a native that isn't drawing (hidden under a
// Suspense boundary that suspended again, which runs its layout effects'
// cleanups) waits for it, and answers once the native shows again, an
// empty portal too: a portal's tree went only when React changed a host
// node in it, so an empty one never answered and Go drew its native for
// good ("no tree yet").
func TestPortalUnderHiddenNativeAnswers(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
globalThis.Row = __clack.native('MessageWrapper');
let pending = null;
function Gate() {
  if (pending) throw pending;
  return null;
}
globalThis.Hide = function Hide() {
  const [g, setG] = React.useState(0);
  globalThis.suspend = () => {
    pending = new Promise((res) => { globalThis.resume = () => { pending = null; res(); }; });
    setG((x) => x + 1);
  };
  return React.createElement(React.Suspense, { fallback: React.createElement('i', null, 'loading') },
    React.createElement(Row, { ts: '1.1' }), React.createElement(Gate, { g }));
};
React.createElement(Hide)`)
	ns := rec.next(t)
	row := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageWrapper" })
	if row == nil || row.K == 0 {
		t.Fatalf("no native row: %+v", ns)
	}
	r.Post(func(js *Ctx) { _, _ = js.Global().Get("suspend").Call() })
	rec.waitFor(t, "the fallback over the hidden row", func(ns []node) bool {
		return find(ns, func(n node) bool { return n.T == "i" && n.text() == "loading" }) != nil
	})

	// Go renders an empty portal under the hidden row
	childRec := newRecv()
	child := r.NewRoot(childRec.cb(t))
	err := r.Do(func(js *Ctx) error {
		el, err := js.Eval("child.js", `React.createElement(function Empty() { return null; })`)
		if err != nil {
			return err
		}
		return child.RenderPortal(js, int64(row.K), el)
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Post(func(js *Ctx) { _, _ = js.Global().Get("resume").Call() })
	rec.waitFor(t, "the row shown again", func(ns []node) bool {
		return find(ns, func(n node) bool { return n.T == "i" }) == nil && find(ns, func(n node) bool { return n.T == "@" }) != nil
	})
	childRec.waitFor(t, "the empty portal's answer", func(ns []node) bool { return len(ns) == 0 })
}
