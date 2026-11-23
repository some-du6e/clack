package popups

import (
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A result's time is SearchMessage's renderTimestamp: the message's
// permalink as a link (the hand cursor, underlined under the pointer)
// whose click is the card's (onTimestampClick only prevents the
// navigation: the result opens), and whose right-click is the link's
// menu. A synthetic search answer, through a real router.
func TestSearchCardTimeIsALink(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ch, ts = "zqcardtimelink", "C0FAKE00002", "1762516200.000100"
	text := "a synthetic " + word + " body"
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "search.modules.messages" || !strings.Contains(f.Get("query"), word) {
			return nil, false
		}
		m := map[string]any{"ts": ts, "user": e.fs.UserIDs()[1], "iid": "I" + ts, "text": text,
			"permalink": "https://fake.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", ""),
			"extracts":  []map[string]any{{"text": text, "truncated_head": false, "truncated_tail": false}}}
		return map[string]any{"ok": true, "query": f.Get("query"), "module": "messages", "filters": map[string]any{},
			"items": []any{map[string]any{"iid": "I" + ts, "team": fakeslack.TeamID,
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
	d.until(t, "the results", func() bool { return v.started && !v.loading && len(v.cards) == 1 })
	d.frame()
	d.frame()
	tl := &v.cards[0].time
	full, _, ok := d.r.Source().AreaBounds(tl.Tag())
	if !ok || full.Empty() {
		t.Fatalf("the result's time has no input area (%v, %v)", full, ok)
	}
	c := full.Min.Add(full.Size().Div(2))
	p := f32.Pt(float32(c.X), float32(c.Y))
	clock := 10 * time.Second
	press := func(b pointer.Buttons) {
		clock += 2 * time.Second
		d.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clock},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: p, Time: clock + 10*time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: clock + 20*time.Millisecond})
		for range 3 {
			d.frame()
		}
	}
	d.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clock})
	d.frame()
	d.frame()
	if !tl.Hovered() {
		t.Error("the pointer on the result's time doesn't hover its link")
	}
	if !v.cards[0].hov {
		t.Error("the pointer on the result's time unhovered the card (its background and hover bar)")
	}
	press(pointer.ButtonPrimary)
	h.mu.Lock()
	opens := append([]string(nil), h.opens...)
	h.mu.Unlock()
	if len(opens) != 1 || !strings.HasPrefix(opens[0], ch+" "+ts+" ") {
		t.Errorf("a click on the result's time opened %q, want the message %s %s (the card's click)", opens, ch, ts)
	}
	e.h.mu.Lock()
	n := len(e.h.shown)
	e.h.mu.Unlock()
	press(pointer.ButtonSecondary)
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	if len(e.h.shown) <= n {
		t.Fatal("a right-click on the result's time showed no menu")
	}
	m, ok := e.h.shown[len(e.h.shown)-1].(*Menu)
	if !ok || len(m.Items()) < 2 || m.Items()[0].Label != "Copy Link" || m.Items()[1].Label != "Open Link" {
		t.Errorf("the time's right-click showed %T, want the link menu (Copy Link, Open Link)", e.h.shown[len(e.h.shown)-1])
	}
}
