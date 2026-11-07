package jsrt

import (
	"testing"
)

// TestRenderProfile logs where the time of a 100-row React update goes
// (JS-side phases), to guide optimization. Not a pass/fail test.
func TestRenderProfile(t *testing.T) {
	r := newTest(t)
	root := r.NewRoot(func([]byte) {})
	err := r.Do(func(js *Ctx) error {
		el, err := js.Eval("p.js", `function L({ sel }) {
  return React.createElement('ul', null, Array.from({ length: 100 }, (_, i) => React.createElement('li', { key: i, className: i === sel ? 'sel' : '' }, 'row ' + i)));
}
globalThis.prof = () => {
  const R = __jr.reconciler, N = 50, out = {};
  const t = (name, f) => { const t0 = performance.now(); for (let i = 0; i < N; i++) f(i); out[name] = ((performance.now() - t0) / N).toFixed(3) + 'ms'; };
  t('empty loop x1e4', () => { let s = 0; for (let i = 0; i < 1e4; i++) s += i; });
  t('createElement x100', (i) => L({ sel: i }));
  const c = __jr.newContainer(9999);
  const fr = R.createContainer(c, 1, null, false, null, '', () => {}, () => {}, () => {}, null);
  R.updateContainerSync(React.createElement(L, { sel: -1 }), fr, null, null); R.flushSyncWork();
  t('update (render+commit) 1 of 100', (i) => { R.updateContainerSync(React.createElement(L, { sel: i % 100 }), fr, null, null); R.flushSyncWork(); });
  const M = React.memo(({ i, s }) => React.createElement('li', { className: s ? 'sel' : '' }, 'row ' + i));
  const L2 = ({ sel }) => React.createElement('ul', null, Array.from({ length: 100 }, (_, i) => React.createElement(M, { key: i, i, s: i === sel })));
  R.updateContainerSync(React.createElement(L2, { sel: -1 }), fr, null, null); R.flushSyncWork();
  t('update memo rows 1 of 100', (i) => { R.updateContainerSync(React.createElement(L2, { sel: i % 100 }), fr, null, null); R.flushSyncWork(); });
  t('serialize 100', () => JSON.stringify(__jr.serializeNode(c)));
  t('serialize objects only', () => __jr.serializeNode(c));
  const s = JSON.stringify(__jr.serializeNode(c));
  t('host commit call (string ' + s.length + 'B)', () => __jr.host.commit(424242, s));
  t('mount 100 fresh', (i) => { const c2 = __jr.newContainer(10000 + i); const f2 = R.createContainer(c2, 1, null, false, null, '', () => {}, () => {}, () => {}, null); R.updateContainerSync(React.createElement(L, { sel: -1 }), f2, null, null); R.flushSyncWork(); __jr.dropContainer(10000 + i); });
  t('100 DOM createElement+append', () => { const u = document.createElement('ul'); for (let i = 0; i < 100; i++) { const li = document.createElement('li'); li.className = 'x'; li.appendChild(document.createTextNode('row')); u.appendChild(li); } });
  return JSON.stringify(out, null, 1);
};
React.createElement(L, { sel: -1 })`)
		if err != nil {
			return err
		}
		return root.Render(js, el)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Do(func(js *Ctx) error {
		v, err := js.Global().Get("prof").Call()
		if err != nil {
			return err
		}
		t.Logf("profile: %s", v.String())
		return nil
	})
}
