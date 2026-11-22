package popups

import (
	"fmt"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// openerHost is the search tests' Host with Slack's navigation to a
// message whose thread is known (MessageOpener: the ui root's
// OpenAtMessage), recorded.
type openerHost struct {
	*tzHost
	mu    sync.Mutex
	opens []string
}

func (h *openerHost) OpenAtMessage(ch, ts, thread string, highlightRoot bool) {
	h.mu.Lock()
	h.opens = append(h.opens, fmt.Sprintf("%s %s thread %s highlightRoot=%v", ch, ts, thread, highlightRoot))
	h.mu.Unlock()
}

// TestSearchResultOpensReplyAtReply: a search result that is a reply
// opens its thread at the reply: messageActionsJumpAction in the SEARCH
// container (w0Cl) is toThread({channelId, threadTs, replyTs}), without
// highlightRoot, from the card's body and its Open in home alike. Clack
// navigated to the thread's route, which opened the thread at its top,
// the reply neither placed nor highlighted. The result is a synthetic
// search answer.
func TestSearchResultOpensReplyAtReply(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ch, root, reply = "zqreplyprobe", "C0FAKE00002", "1762516000.000100", "1762516060.000100"
	text := "a synthetic " + word + " reply"
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "search.modules.messages" || !strings.Contains(f.Get("query"), word) {
			return nil, false
		}
		m := map[string]any{"ts": reply, "thread_ts": root, "user": e.fs.UserIDs()[1], "iid": "I" + reply, "text": text,
			"permalink": "https://fake.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(reply, ".", "") + "?thread_ts=" + root,
			"extracts":  []map[string]any{{"text": text, "truncated_head": false, "truncated_tail": false}}}
		return map[string]any{"ok": true, "query": f.Get("query"), "module": "messages", "filters": map[string]any{},
			"items": []any{map[string]any{"iid": "I" + reply, "team": fakeslack.TeamID,
				"channel":  map[string]any{"id": ch, "name": "synthetic-search", "is_channel": true, "teams": []string{fakeslack.TeamID}},
				"messages": []any{m}}},
			"pagination":         map[string]any{"total_count": 1, "page": 1, "per_page": 20, "page_count": 1, "first": 1, "last": 1},
			"filter_suggestions": map[string]any{"in": []any{}, "from": []any{}}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	h := &openerHost{tzHost: e.h}
	v := NewSearchView(h, word)
	v.SetOrigin(image.Pt(0, 0))
	d := &srDrv{size: image.Pt(1280, 800), w: v}
	d.until(t, "the results", func() bool { return v.started && !v.loading })
	if len(v.cards) != 1 || len(v.msgs) != 1 {
		t.Fatalf("%d cards for %d messages, want the one reply", len(v.cards), len(v.msgs))
	}
	for _, c := range []struct {
		name string
		b    *widget.Clickable
	}{{"the card", &v.cards[0].click}, {"Open in home", &v.cards[0].jump}} {
		c.b.Click()
		d.frame()
		d.frame()
	}
	h.mu.Lock()
	got := slices.Clone(h.opens)
	h.mu.Unlock()
	want := ch + " " + reply + " thread " + root + " highlightRoot=false"
	if !slices.Equal(got, []string{want, want}) {
		t.Errorf("the card and Open in home opened %q, want the thread at the reply twice (%q)", got, want)
	}
	e.h.mu.Lock()
	navs := slices.Clone(e.h.navs)
	e.h.mu.Unlock()
	if len(navs) > 0 {
		t.Errorf("the result navigated to %q instead", navs)
	}
}
