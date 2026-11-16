package jsrt

import (
	"testing"
	"time"
)

// React commits a root whenever an update ran in it, also when the update
// changed nothing on screen (a plugin's usePatchVersion after
// api.redux.refresh, a store subscription whose value didn't change, a
// portal mounted under one of its natives). Each such commit used to send
// the root's same tree to Go again: 650 to 4,000 in a perf pass. A commit
// of JS's own updates now sends one only when a host node changed; what Go
// renders always answers.

// quiet fails if rec gets a commit within d.
func quiet(t *testing.T, rec *treeRecv, d time.Duration, what string) {
	t.Helper()
	select {
	case ns := <-rec.ch:
		t.Errorf("%s: a commit of the same tree reached Go: %+v", what, ns)
	case <-time.After(d):
	}
}

func TestSameOutputSendsNoTree(t *testing.T) {
	r := newTest(t)
	rec := newRecv()
	root := r.NewRoot(rec.cb(t))
	renderJS(t, r, root, `
globalThis.Row = __clack.native('MessageWrapper');
globalThis.Same = function Same() {
  const [n, setN] = React.useState(0);
  const [, poke] = React.useReducer((x) => x + 1, 0);
  globalThis.poke = poke;
  globalThis.setLabel = setN;
  // the native gets the same props on every render but a label change
  return React.createElement('div', { className: 'row' }, React.createElement(Row, { ts: '1.1', label: 'n' + n }));
};
React.createElement(Same)`)
	ns := rec.next(t)
	row := find(ns, func(n node) bool { return n.T == "@" && n.N == "MessageWrapper" })
	if row == nil || row.K == 0 {
		t.Fatalf("no native row: %+v", ns)
	}

	// a state update that renders the same: nothing goes to Go
	for i := 0; i < 3; i++ {
		r.Post(func(js *Ctx) { _, _ = js.Global().Get("poke").Call() })
	}
	quiet(t, rec, 300*time.Millisecond, "re-rendered with the same output")

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
