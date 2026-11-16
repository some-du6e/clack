package taut

import (
	"strings"
	"testing"
	"time"
)

// api.redux.refresh renders again every patch that calls usePatchVersion:
// Show Real User's MessageWrapper patch on every row, Message Logger's
// rows, and Message Logger refreshes on every edit and delete it logs.
// That was about 1,000 patches in a long channel, each re-rendering its
// native, re-serializing its props (a message's blocks and all) and
// sending its root's same tree to Go again. A patch that hands its
// <Original> what it had now stops at the native (memoized), and its
// root sends nothing; one whose output changed sends it.
func TestRefreshSendsNoSameTree(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		globalThis.__variant = ''
		api.patchComponent('MessageWrapper', (Original) => (props) => {
			const v = api.redux.usePatchVersion()
			// Show Real User's shape: the msg memo follows the patch version
			const msg = R.useMemo(() => props.msg, [props.msg, v])
			return R.createElement('div', { className: 'x-wrap' + globalThis.__variant }, R.createElement(Original, { ...props, msg }))
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
	if !hasClass(nodes, "x-wrap") || findNative(nodes, "MessageWrapper") == nil {
		t.Fatalf("first tree: %v", nodes)
	}
	drain := func() int {
		n := 0
		for {
			select {
			case r := <-h.ui.treeCh:
				if r == root {
					n++
				}
			default:
				return n
			}
		}
	}
	time.Sleep(200 * time.Millisecond)
	drain()

	for i := 0; i < 5; i++ {
		h.js(`api.redux.refresh(); return 1`)
	}
	time.Sleep(400 * time.Millisecond)
	if n := drain(); n != 0 {
		t.Errorf("5 refreshes that change nothing the patch draws sent its root's tree %d times; want 0", n)
	}

	// a refresh that changes what the patch draws sends it
	h.js(`globalThis.__variant = ' x-two'; api.redux.refresh(); return 1`)
	eventually(t, 3*time.Second, "the patch's new class", func() bool {
		return strings.Contains(string(h.rawTree(root)), "x-two")
	})
}
