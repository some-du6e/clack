package popups

import (
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

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
	sicAnswer(t, e, word, ch, ts, text)
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

// sicAnswer makes the fake answer search in channel's query for word with
// one result, message ts in ch.
func sicAnswer(t *testing.T, e *srEnv, word, ch, ts, text string) {
	t.Helper()
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
}

// sicAt draws the pane at its place in the window.
type sicAt struct {
	at image.Point
	v  *SearchInChannel
}

func (w sicAt) Layout(gtx layout.Context) layout.Dimensions {
	defer op.Offset(w.at).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(image.Pt(420, 760))
	return w.v.Layout(gtx)
}

// sicOriginHost is the ui root's: the secondary column (the pane's) at at.
type sicOriginHost struct {
	*openerHost
	at image.Point
}

func (h sicOriginHost) SecondaryOrigin() image.Point { return h.at }

// In the app the pane is in the secondary column, right of the channel:
// a card's More actions opens at its kebab in the window, the pane's
// place (the host's SecondaryOrigin) added, not at the pane's own
// coordinates.
func TestSearchInChannelMenusOpenAtThePane(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ch, ts = "zqinchannelmenu", "C0FAKE00002", "1762516400.000100"
	sicAnswer(t, e, word, ch, ts, "a synthetic "+word+" body")
	at := image.Pt(300, 60)
	h := sicOriginHost{openerHost: &openerHost{tzHost: e.h}, at: at}
	v := NewSearchInChannel(h, ch)
	v.Search(word)
	d := &srDrv{size: image.Pt(800, 900), w: sicAt{at: at, v: v}}
	d.until(t, "the in-channel result card", func() bool { return len(sicCardsT(v)) == 1 })
	d.frame()
	d.frame()
	c := sicCardsT(v)[0]
	body, _, ok := d.r.Source().AreaBounds(&c.click)
	if !ok || body.Empty() {
		t.Fatalf("the card has no click area (%v, %v)", body, ok)
	}
	clock := 10 * time.Second
	move := func(p image.Point) {
		clock += 100 * time.Millisecond
		d.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: clock})
		d.frame()
		d.frame()
	}
	move(body.Min.Add(image.Pt(30, 10)))
	kb, _, ok := d.r.Source().AreaBounds(&c.kebab)
	if !ok || kb.Empty() {
		t.Fatalf("the hovered card draws no More actions (%v, %v)", kb, ok)
	}
	k := kb.Min.Add(kb.Size().Div(2))
	move(k)
	clock += 100 * time.Millisecond
	p := f32.Pt(float32(k.X), float32(k.Y))
	e.h.mu.Lock()
	n := len(e.h.shown)
	e.h.mu.Unlock()
	d.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: clock},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: clock + 20*time.Millisecond})
	for range 3 {
		d.frame()
	}
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	if len(e.h.shown) <= n {
		t.Fatal("a click on More actions opened nothing")
	}
	m, ok := e.h.shown[len(e.h.shown)-1].(*Menu)
	if !ok {
		t.Fatalf("More actions opened %T", e.h.shown[len(e.h.shown)-1])
	}
	if tr := m.opts.Trigger; tr.Dx() == 0 || !tr.Overlaps(kb.Inset(-2)) {
		t.Errorf("the menu's trigger is %v in the window; the kebab is at %v (the pane at %v)", tr, kb, at)
	}
}
