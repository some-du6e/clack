package taut

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// api.redux.refresh renders again every patch that calls usePatchVersion:
// Show Real User's MessageWrapper patch on every row, Message Logger's
// rows, and Message Logger refreshes on every edit and delete it logs.
// That was about 1,000 patches in a long channel, each re-rendering its
// native, re-serializing its props (a message's blocks and all) and
// sending its root's same tree to Go again. Natives are memoized now: a
// patch that hands its <Original> what it had stops there (the native
// doesn't render), and its root sends nothing; a change to what the
// patch draws around it, or to the native's props, still goes, and so
// does a portal mounted under the native after the refreshes it skipped.
func TestRefreshSendsNoSameTree(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		// count the MessageWrapper native's renders (wrapped before taut
		// first asks for it)
		globalThis.__nativeRenders = 0
		const native = globalThis.__clack.native
		let counted
		globalThis.__clack.native = (name) => {
			const C = native(name)
			if (name !== 'MessageWrapper') return C
			counted ??= function CountedNative(props) {
				globalThis.__nativeRenders++
				return C(props)
			}
			return counted
		}
		globalThis.__variant = ''
		globalThis.__label = 'a'
		api.patchComponent('MessageWrapper', (Original) => (props) => {
			const v = api.redux.usePatchVersion()
			// Show Real User's shape: the msg memo follows the patch version
			const msg = R.useMemo(() => props.msg, [props.msg, v])
			return R.createElement('div', { className: 'x-wrap' + globalThis.__variant }, R.createElement(Original, { ...props, msg, label: globalThis.__label }))
		})
		return 1`)
	eventually(t, 3*time.Second, "MessageWrapper patched", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		return has(h.ui.patched, "MessageWrapper")
	})
	const root = 6101
	msg := map[string]any{"type": "message", "channel": chGen, "ts": "1700000001.000100", "user": "UALICE", "text": "the job fair is today",
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "b", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": "the job fair is today"}}}}}}}
	nodes := h.render(root, "MessageWrapper", map[string]any{"msg": msg, "channelId": chGen, "viewContext": "message_pane"})
	native := findNative(nodes, "MessageWrapper")
	if !hasClass(nodes, "x-wrap") || native == nil {
		t.Fatalf("first tree: %v", nodes)
	}
	renders := func() int {
		var n int
		h.jsInto(`return globalThis.__nativeRenders`, &n)
		return n
	}
	if renders() == 0 {
		t.Fatal("the MessageWrapper native's renders weren't counted: taut made it before the test wrapped it")
	}
	drain := func(r uint64) int {
		n := 0
		for {
			select {
			case got := <-h.ui.treeCh:
				if got == r {
					n++
				}
			default:
				return n
			}
		}
	}
	time.Sleep(200 * time.Millisecond)
	drain(root)

	n0 := renders()
	for i := 0; i < 5; i++ {
		h.js(`api.redux.refresh(); return 1`)
	}
	time.Sleep(400 * time.Millisecond)
	if n := drain(root); n != 0 {
		t.Errorf("5 refreshes that change nothing the patch draws sent its root's tree %d times; want 0", n)
	}
	if n := renders() - n0; n != 0 {
		t.Errorf("5 refreshes rendered the native %d times; the patch handed it what it had (memoized natives)", n)
	}

	// a refresh that changes what the patch draws around the native sends
	// it, and the native, whose props are the same, still doesn't render
	h.js(`globalThis.__variant = ' x-two'; api.redux.refresh(); return 1`)
	eventually(t, 3*time.Second, "the patch's new class", func() bool {
		return strings.Contains(string(h.rawTree(root)), "x-two")
	})
	if n := renders() - n0; n != 0 {
		t.Errorf("a change around the native rendered it %d times", n)
	}
	// one that changes the native's props renders it
	h.js(`globalThis.__label = 'b'; api.redux.refresh(); return 1`)
	eventually(t, 3*time.Second, "the native's new props", func() bool {
		return strings.Contains(string(h.rawTree(root)), `"label":"b"`)
	})
	if renders() == n0 {
		t.Error("new props didn't render the native")
	}

	// a portal Go mounts under the native after all that still renders
	k, _ := native["k"].(float64)
	if k == 0 {
		t.Fatalf("no k on the native: %v", native)
	}
	const child = 6102
	props, _ := json.Marshal(map[string]any{"msg": msg, "isInteractive": true})
	h.tt.Bridge().RenderIn(child, root, int64(k), "MessageSender", props)
	eventually(t, 3*time.Second, "the portal's tree", func() bool {
		return strings.Contains(string(h.rawTree(child)), `"n":"MessageSender"`)
	})
}
