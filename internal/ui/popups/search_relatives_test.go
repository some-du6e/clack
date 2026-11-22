package popups

import (
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Search in channel draws its results with the results page's card, as
// Slack's list renders a "message-result" item (variant card) in both:
// the rich body (a channel link is a link), the card's click on its text,
// the time's link. A synthetic answer, through a real router.
func TestSearchInChannelDrawsResultCards(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ch, ts, other = "zqinchannelcard", "C0FAKE00002", "1762516300.000100", "C0FAKE00003"
	text := "a synthetic " + word + " body that goes on, see <#" + other + "|other-place> please"
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
			"pagination":         map[string]any{"total_count": 1, "page": 1, "per_page": 10, "page_count": 1, "first": 1, "last": 1},
			"filter_suggestions": map[string]any{"in": []any{}, "from": []any{}}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	h := &openerHost{tzHost: e.h}
	v := NewSearchInChannel(h, ch)
	v.SetOrigin(image.Pt(0, 0))
	v.Search(word)
	d := &srDrv{size: image.Pt(420, 800), w: v}
	var body *richview.View
	d.until(t, "the in-channel result card", func() bool {
		cs := sicCardsT(v)
		if len(cs) != 1 {
			return false
		}
		body = cs[0].rich.body
		return body != nil
	})
	d.frame()
	d.frame()
	full, _, ok := d.r.Source().AreaBounds(body)
	if !ok || full.Empty() {
		t.Fatalf("the card's body has no input area (%v, %v)", full, ok)
	}
	var onText, onLink f32.Point
	for y := 1; y < full.Dy() && (onText == (f32.Point{}) || onLink == (f32.Point{})); y += 2 {
		for x := 1; x < full.Dx(); x++ {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			switch hit := body.HitAt(p); {
			case hit.Kind == richview.HitNone && hit.Text && onText == (f32.Point{}) && x > 20:
				onText = p
			case hit.Kind == richview.HitChannel && onLink == (f32.Point{}):
				onLink = p
			}
		}
	}
	if onText == (f32.Point{}) || onLink == (f32.Point{}) {
		t.Fatalf("no plain text (%v) or channel link (%v) in the card's body: not the results page's rich card", onText, onLink)
	}
	at := f32.Pt(onText.X+float32(full.Min.X), onText.Y+float32(full.Min.Y))
	d.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at, Time: 10 * time.Second},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at, Time: 10*time.Second + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at, Time: 10*time.Second + 30*time.Millisecond})
	for range 3 {
		d.frame()
	}
	h.mu.Lock()
	opens := append([]string(nil), h.opens...)
	h.mu.Unlock()
	if len(opens) != 1 || !strings.HasPrefix(opens[0], ch+" "+ts+" ") {
		t.Errorf("a click on the in-channel card's text opened %q, want the message %s %s", opens, ch, ts)
	}
}

// A result card keeps its address when the next page's results are
// appended (Gio routes a press by its handler's address): a card held
// while a page loads keeps its press.
func TestSearchCardsKeepTheirPlaceAcrossPages(t *testing.T) {
	h := hcEnv(t)
	v := NewSearchView(h, "synthetic")
	msgs := func(from, n int) []core.Message {
		var ms []core.Message
		for i := 0; i < n; i++ {
			ms = append(ms, core.Message{Channel: "C0FAKE00001", TS: "17000" + strings.Repeat("0", 4) + string(rune('a'+from+i%20)) + ".000100", Text: "synthetic"})
		}
		return ms
	}
	v.addMessages(core.SearchResult{Messages: msgs(0, 2), Total: 40, Pages: 2, Page: 1}, 1)
	first := cardAddrT(v, 0)
	v.addMessages(core.SearchResult{Messages: msgs(2, 30), Total: 40, Pages: 2, Page: 2}, 2)
	if cardAddrT(v, 0) != first {
		t.Error("the first result's card moved when the next page was appended: a press held on it is lost")
	}
}
