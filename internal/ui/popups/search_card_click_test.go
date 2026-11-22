package popups

import (
	"image"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Clicks on a result card's text, through a real router: SearchResult's
// onClick on the card (boot-shared vVRo, CDN build 35b42de8b870) opens the
// message for a click anywhere on it, its text included, unless the click
// is on a mention, a button or a link (those act as themselves), a drag
// past 4 px (a text selection: onMouseMove's dragStart) or a Shift+click.
// The body is a rich text view drawn over the card's click target, so the
// view takes those presses; and the pointer over the text keeps the card
// hovered (its background and the hover bar). A synthetic search answer.
func TestSearchCardTextClickOpensMessage(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ch, ts, other = "zqcardtextclick", "C0FAKE00002", "1762516100.000100", "C0FAKE00003"
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
			"pagination":         map[string]any{"total_count": 1, "page": 1, "per_page": 20, "page_count": 1, "first": 1, "last": 1},
			"filter_suggestions": map[string]any{"in": []any{}, "from": []any{}}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	h := &openerHost{tzHost: e.h}
	if !srExplore(h) {
		t.Fatal("no density card")
	}
	v := NewSearchView(h, word)
	v.SetOrigin(image.Pt(0, 0))
	d := &srDrv{size: image.Pt(1280, 800), w: v}
	d.until(t, "the results", func() bool { return v.started && !v.loading && len(v.cards) == 1 })
	d.frame()
	d.frame()
	body := v.cards[0].rich.body
	if body == nil {
		t.Fatal("the card has no rich body")
	}
	full, _, ok := d.r.Source().AreaBounds(body)
	if !ok || full.Empty() {
		t.Fatalf("the body has no input area (%v, %v)", full, ok)
	}
	// a point on the body's plain text and one on the channel link (the
	// view's own coordinates), in the window
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
		t.Fatalf("no plain text (%v) or channel link (%v) point in the body", onText, onLink)
	}
	win := func(p f32.Point) f32.Point { return p.Add(layout.FPt(full.Min)) }
	clock := 10 * time.Second
	gesture := func(at f32.Point, mods key.Modifiers, drag f32.Point) {
		clock += 2 * time.Second
		evs := []event.Event{
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at, Time: clock},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at, Modifiers: mods, Time: clock + 10*time.Millisecond}}
		end := at
		if drag != (f32.Point{}) {
			end = at.Add(drag)
			evs = append(evs, pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at.Add(drag.Mul(.5)), Time: clock + 30*time.Millisecond},
				pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: end, Time: clock + 50*time.Millisecond})
		}
		evs = append(evs, pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: end, Modifiers: mods, Time: clock + 70*time.Millisecond})
		d.r.Queue(evs...)
		for range 3 {
			d.frame()
		}
	}
	opens := func() []string {
		h.mu.Lock()
		defer h.mu.Unlock()
		return slices.Clone(h.opens)
	}
	navs := func() []string {
		e.h.mu.Lock()
		defer e.h.mu.Unlock()
		return slices.Clone(e.h.navs)
	}

	// the pointer resting on the text: the card stays hovered
	d.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: win(onText), Time: clock})
	d.frame()
	d.frame()
	if !scCardHovered(v, 0) {
		t.Error("the pointer over the card's text dropped its hover (background and hover bar)")
	}
	// a click on the text opens the message
	gesture(win(onText), 0, f32.Point{})
	if got := opens(); len(got) != 1 || !strings.HasPrefix(got[0], ch+" "+ts+" ") {
		t.Fatalf("a click on the card's text opened %q, want the message %s %s", got, ch, ts)
	}
	// the channel link is a link: its channel opens, the message doesn't
	gesture(win(onLink), 0, f32.Point{})
	if got := opens(); len(got) != 1 {
		t.Errorf("a click on the channel link also opened the message: %q", got)
	}
	if got := navs(); !slices.Contains(got, "c/"+other) {
		t.Errorf("a click on the channel link navigated %q, want c/%s", got, other)
	}
	// a drag across the text selects it and opens nothing
	gesture(win(onText), 0, f32.Pt(60, 0))
	if got := opens(); len(got) != 1 {
		t.Errorf("a drag across the text opened the message: %q", got)
	}
	if !body.HasSelection() {
		t.Error("a drag across the card's text selected nothing")
	}
	// a Shift+click opens nothing
	gesture(win(onText), key.ModShift, f32.Point{})
	if got := opens(); len(got) != 1 {
		t.Errorf("a Shift+click on the text opened the message: %q", got)
	}
}
