package taut

import (
	"testing"
	"time"
)

// patchComponentInner's patch (a runtime's own: Slick's MAMenu patch)
// sits innermost whenever it comes, so the plugins' patches registered
// before it still wrap it, in their own order: with A, then the inner one,
// then B, MAMenu renders B > A > inner > Original. While a plugin's patch
// is there, Taut doesn't report the component as patched only by the
// runtime's own (OnlyOwnPatch: a message actions submenu waits for its
// tree then); once they're gone it does, and once the inner one goes too,
// nothing patches it.
func TestPatchComponentInnerComposition(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		globalThis.__order = []
		const wrap = (tag) => (Original) => (props) => { globalThis.__order.push(tag); return R.createElement(Original, props) }
		globalThis.__offA = T.patchComponent('MAMenu', wrap('A'))
		globalThis.__offI = T.patchComponentInner('MAMenu', wrap('inner'))
		globalThis.__offB = T.patchComponent('MAMenu', wrap('B'))
		return 1`)
	eventually(t, 3*time.Second, "MAMenu patched", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		return has(h.ui.patched, "MAMenu")
	})
	h.render(1, "MAMenu", map[string]any{"actions": []any{}})
	var order string
	h.jsInto(`return globalThis.__order.slice(0, 3).join(",")`, &order)
	if order != "B,A,inner" {
		t.Errorf("MAMenu's patches rendered %q, want B,A,inner: the inner one next to Original, the others in their order outside it", order)
	}
	if onlyOwnPatch(h.tt, "MAMenu") {
		t.Error("MAMenu reported patched only by the runtime's own with two plugins' patches on it")
	}
	h.js(`globalThis.__offA(); globalThis.__offB(); return 1`)
	eventually(t, 3*time.Second, "only the runtime's own patch left", func() bool { return onlyOwnPatch(h.tt, "MAMenu") })
	h.js(`globalThis.__offI(); return 1`)
	eventually(t, 3*time.Second, "nothing patching MAMenu", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		return !has(h.ui.patched, "MAMenu") && !onlyOwnPatch(h.tt, "MAMenu")
	})
}
