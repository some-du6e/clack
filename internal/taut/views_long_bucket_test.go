package taut

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A message bucket longer than viewLongBucket keeps its entries, not its
// bytes whole a second time (viewEntry.vals): View reads the same value
// back, the injected message and the messages' views as for any bucket.
func TestLongBucketKeepsEntries(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__un = api.messages.injectMessages(() => [{type: 'message', channel: '` + chAnn + `', ts: '1700009000.000100', user: 'UBOB', text: 'injected'}])
		return 1`)
	eventually(t, 2*time.Second, "messages patched", func() bool { return h.tt.SlicePatched("messages") })
	const n = 60
	long := strings.Repeat("synthetic words\n", 100<<10/16) // 100 KB a message: 6 MB
	msgs := make([]slack.Message, n)
	for i := range msgs {
		msgs[i] = slack.Message{Type: "message", TS: fmt.Sprintf("1700004%03d.000100", i), User: "UALICE", Text: fmt.Sprintf("%d %s", i, long)}
	}
	h.app.S.SetLatest(chAnn, msgs, false)
	h.tt.mirror.invalidate("messages", chAnn)
	eventually(t, 20*time.Second, "the injected message", func() bool { return injectedText(h, chAnn) == "injected" })
	for _, i := range []int{0, n - 1} {
		ts := msgs[i].TS
		eventually(t, 10*time.Second, fmt.Sprintf("message %d's view", i), func() bool {
			m, ok := h.tt.MessageView(chAnn, ts)
			return ok && m.Text == msgs[i].Text
		})
	}
	var raw json.RawMessage
	eventually(t, 10*time.Second, "the bucket's view", func() bool {
		r, ok := h.tt.View("messages", chAnn)
		raw = r
		return ok && len(r) > 0
	})
	e := h.tt.ui.views.entry("messages", chAnn, "")
	if e == nil || e.raw != nil || e.vals == nil {
		t.Fatalf("a %d MB bucket's entry holds its bytes whole (raw %v, entries %v)", len(raw)>>20, e != nil && e.raw != nil, e != nil && e.vals != nil)
	}
	if e.rawLen != len(raw) || e.sum != viewSum(raw) {
		t.Errorf("View's bytes (%d) aren't the ones the entry was installed from (%d)", len(raw), e.rawLen)
	}
	var b map[string]struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if len(b) != n+1 || b[msgs[7].TS].Text != msgs[7].Text || b["1700009000.000100"].Text != "injected" {
		t.Errorf("View's bucket: %d entries (want %d), message 7 right %v", len(b), n+1, b[msgs[7].TS].Text == msgs[7].Text)
	}
}
