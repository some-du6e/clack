package richview

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
)

// botRes is codeRes with a bots cache (richtext.BotNamer).
type botRes struct{ codeRes }

func (botRes) BotName(id string) string {
	switch id {
	case fakeslack.EmbedsGitHubBotID:
		return "GitHub"
	case fakeslack.EmbedsTrackerBotID:
		return "Example Tracker"
	}
	return ""
}

func embedDocWith(t testing.TB, r rt.Resolver, name string) rt.Doc {
	t.Helper()
	for _, f := range fakeslack.EmbedsFixtures("http://img.test/") {
		if f.Name == name {
			m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Text: f.Text, Attachments: f.Attachments}
			return rt.FromMessage(r, &m)
		}
	}
	t.Fatalf("no embeds fixture %q", name)
	return rt.Doc{}
}

func optHarness(d rt.Doc, o Options, h Handler) *harness {
	hs := &harness{st: newTestStyle(), v: New(d, o), h: h, w: 800}
	hs.frame()
	return hs
}

// MessageAttachmentDefaultV3's tO: a row card with text, a thumb alone,
// media or fields is wrapped in HoverCardTrigger; a row card with none of
// them, and every service or image card, is not.
func TestUnfurlPopoverTrigger(t *testing.T) {
	for name, want := range map[string]bool{"small thumb": true, "large image": true, "text only": true,
		"x post": false, "youtube": false, "image link": false, "github": false, "block kit": false} {
		v := layoutDoc(t, embedDoc(t, name), 900, Options{})
		got := false
		for _, c := range v.d.cards {
			got = got || c.popover
		}
		if got != want {
			t.Errorf("%s: hover card %v, want %v", name, got, want)
		}
	}
	// title only, no text or media: the 80px --short card, no trigger
	bare := attDoc("<https://bare.example.com/x>", slack.Attachment{ID: 1, FromURL: "https://bare.example.com/x",
		OriginalURL: "https://bare.example.com/x", TitleLink: "https://bare.example.com/x", Title: "Bare", ServiceName: "Bare"})
	v := layoutDoc(t, bare, 900, Options{})
	if len(v.d.cards) != 1 || v.d.cards[0].popover {
		t.Errorf("a title-only card: cards %+v, want one without a hover card", v.d.cards)
	}
	// the wrapper: the card and the 6px of its margin under the 114px cap
	v = layoutDoc(t, embedDoc(t, "large image"), 900, Options{})
	if c := v.d.cards[0]; c.wrap.Dy() != 114 || c.wrap.Min != c.r.Min || c.wrap.Dx() != c.r.Dx() {
		t.Errorf("hover wrapper %v for card %v, want the card plus 6px (114 tall)", c.wrap, c.r)
	}
}

// Resting on a row card reports its preview at the wrapper's bounds; the
// card opens after 350ms; a click on the card closes it; the X card
// reports nothing.
func TestUnfurlHoverTiming(t *testing.T) {
	c := &hovercard.Controller{}
	ptr := image.Point{}
	c.Pointer = func() image.Point { return ptr }
	hovercard.SetDefault(c)
	defer hovercard.SetDefault(nil)
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	hs := optHarness(embedDoc(t, "large image"), Options{}, &recHandler{})
	at := func(p f32.Point) {
		ptr = image.Pt(int(p.X+.5), int(p.Y+.5))
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	}
	card := hs.v.d.cards[0]
	p := f32.Pt(float32(card.r.Min.X+60), float32(card.r.Min.Y+30))
	at(p)
	hs.frame()
	c.Frame(t0, false, false)
	req, ok := c.Pending()
	if !ok || req.Anchor != card.wrap {
		t.Fatalf("hovering the card: pending=%v anchor %v, want %v", ok, req.Anchor, card.wrap)
	}
	pv, ok := req.Preview.(*Preview)
	if !ok || pv.v.doc.Blocks[0].Att != card.att {
		t.Fatalf("preview %T, want the card's *Preview", req.Preview)
	}
	hs.frame()
	c.Frame(t0.Add(hovercard.ShowDelay-time.Millisecond), false, false)
	if _, _, open := c.Open(); open {
		t.Fatal("open before 350ms")
	}
	hs.frame()
	c.Frame(t0.Add(hovercard.ShowDelay), false, false)
	if _, _, open := c.Open(); !open {
		t.Fatal("no hover card after 350ms on the card")
	}
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	c.Frame(t0.Add(time.Second), false, false)
	if _, _, open := c.Open(); open {
		t.Fatal("a click on the card left its hover card open")
	}

	c2 := &hovercard.Controller{Pointer: c.Pointer}
	hovercard.SetDefault(c2)
	x := optHarness(embedDoc(t, "x post"), Options{}, &recHandler{})
	xr := cardBox(t, x.v)
	x.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(xr.Min.X+40), float32(xr.Min.Y+40))})
	x.frame()
	c2.Frame(t0, false, false)
	if _, ok := c2.Pending(); ok {
		t.Error("the X card reported a hover card")
	}
}

// MessageAttachmentDefaultV3HoverCard at 340px, against the live card
// (light, 2026-11-09, a 1200x630 og:image): the title row 12px down on
// the 22px line, the text 2px under it (13px/16.25, 5 lines max), the
// footer 4+4 under the text (16.25px line), the image 12px under the
// footer, 340x178 (fitted in 340x192), flush with the card's bottom.
func TestUnfurlHoverCardLayout(t *testing.T) {
	d := embedDoc(t, "large image")
	pv := New(rt.Doc{Blocks: []rt.Block{*attBlock(t, d)}}, Options{HoverCard: true})
	pv.relayout(newTestStyle(), PreviewWidth, 1, 1)
	title := findFrag(t, &pv.d, "Ten")
	if title.top != 12 {
		t.Errorf("title line top %d, want 12", title.top)
	}
	text := findFrag(t, &pv.d, "Stable")
	if text.top != 12+22+2 {
		t.Errorf("text top %d, want 36", text.top)
	}
	last := findFrag(t, &pv.d, "loops.") // the text's last line
	foot := findFrag(t, &pv.d, "Example")
	if want := last.bot + 8; foot.top < want-1 || foot.top > want+1 {
		t.Errorf("footer top %d, want %d (text bottom %d + 4 + 4)", foot.top, want, last.bot)
	}
	img := imgPrimAt(t, pv, "http://img.test/img/1200x630/blog-hero.png")
	if img.Dx() != 340 || img.Dy() != 178 || img.Min.X != 0 {
		t.Errorf("image %v, want 340x178 at x 0", img)
	}
	if want := foot.top + 16 + 12; img.Min.Y < want-1 || img.Min.Y > want+1 {
		t.Errorf("image top %d, want %d", img.Min.Y, want)
	}
	if pv.d.size.Y != img.Max.Y {
		t.Errorf("card height %d, want the image's bottom %d", pv.d.size.Y, img.Max.Y)
	}

	// the thumb alone: AttachmentThumb isHoverCard, 340x192
	tv := New(rt.Doc{Blocks: []rt.Block{*attBlock(t, embedDoc(t, "small thumb"))}}, Options{HoverCard: true})
	tv.relayout(newTestStyle(), PreviewWidth, 1, 1)
	if r := imgPrimAt(t, tv, "http://img.test/img/150x150/news-thumb.png"); r.Dx() != 340 || r.Dy() != 192 {
		t.Errorf("thumb %v, want 340x192", r)
	}
	// no media: the empty media span's 12px margin closes the card
	nv := New(rt.Doc{Blocks: []rt.Block{*attBlock(t, embedDoc(t, "text only"))}}, Options{HoverCard: true})
	nv.relayout(newTestStyle(), PreviewWidth, 1, 1)
	f := findFrag(t, &nv.d, "Docs")
	if nv.d.size.Y != f.bot+12 && nv.d.size.Y != f.top+16+12 {
		t.Errorf("text-only card height %d, footer line %d..%d: want 12px under it", nv.d.size.Y, f.top, f.bot)
	}

	// fields: two to a row, the title bold, its value 4px after it
	fd := attDoc("<https://f.example.com/a>", slack.Attachment{ID: 1, FromURL: "https://f.example.com/a", OriginalURL: "https://f.example.com/a",
		Title: "Fields", Text: "body", ServiceName: "F", Fields: []slack.AttachmentField{{Title: "Owner", Value: "ada", Short: true}, {Title: "State", Value: "open", Short: true}, {Title: "Due", Value: "soon"}}})
	fv := New(rt.Doc{Blocks: []rt.Block{*attBlock(t, fd)}}, Options{HoverCard: true})
	fv.relayout(newTestStyle(), PreviewWidth, 1, 1)
	ow, st, due := findFrag(t, &fv.d, "Owner"), findFrag(t, &fv.d, "State"), findFrag(t, &fv.d, "Due")
	if ow.top != st.top || due.top <= ow.top || int(ow.x) != 12 || int(due.x) != 12 {
		t.Errorf("fields at (%v,%d) (%v,%d) (%v,%d): want two to a row from x 12", ow.x, ow.top, st.x, st.top, due.x, due.top)
	}
	if want := float32(12 + (316-8)/2 + 8); st.x < want-1 || st.x > want+1 {
		t.Errorf("second field at x %v, want %v", st.x, want)
	}
	if v := findFrag(t, &fv.d, "ada"); v.top != ow.top || v.x < ow.x+ow.w+3 || v.x > ow.x+ow.w+5 {
		t.Errorf("value at x %v, want 4px after the title (%v)", v.x, ow.x+ow.w)
	}
}

// renderContextMenu: every v3 card has Slack's menu (Remove preview for
// the sender, Copy link with title_link || from_url); v1 and v2 cards
// have none.
func TestAttachmentContextMenu(t *testing.T) {
	for _, tc := range []struct {
		name      string
		own, ok   bool
		canRemove bool
		url       string
	}{
		{"x post", true, true, true, "https://x.com/clackdevs/status/1987650000000000001"},
		{"youtube", false, true, false, "https://www.youtube.com/watch?v=c1ack0000000"},
		{"large image", false, true, false, "https://blog.example.org/posts/gio-tips"},
		{"image link", false, true, false, "https://images.example.com/sunset.png"},
		{"github", true, false, false, ""},
		{"block kit", true, false, false, ""},
	} {
		v := layoutDoc(t, embedDoc(t, tc.name), 900, Options{Own: tc.own})
		var p f32.Point
		if len(v.d.cards) > 0 {
			r := v.d.cards[0].r
			p = f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
		} else {
			p = f32.Pt(30, float32(v.d.size.Y-10))
		}
		ah, ok := v.AttachmentAt(p)
		if ok != tc.ok {
			t.Errorf("%s: menu %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if ok && (ah.CanRemove != tc.canRemove || ah.CopyURL != tc.url || ah.Att.ID != "1") {
			t.Errorf("%s: %+v, want remove %v, copy %s", tc.name, ah, tc.canRemove, tc.url)
		}
	}
	// the remove button sits outside the card but is the card's
	v := layoutDoc(t, embedDoc(t, "x post"), 900, Options{Own: true})
	rm := v.d.removes[0].r
	if _, ok := v.AttachmentAt(f32.Pt(float32(rm.Max.X-3), float32(rm.Min.Y+3))); !ok {
		t.Error("right-click on the remove button: no attachment menu")
	}
}

// AttachmentDeleteButton's rail style: v1 and v2 cards the viewer sent get
// a 20px button left of the card's top; v1 shows it while the card is
// hovered, v2 only under the pointer; a click removes the preview.
func TestRemoveRail(t *testing.T) {
	for _, tc := range []struct {
		name string
		v1   bool
	}{{"github", true}, {"block kit", false}} {
		d := embedDoc(t, tc.name)
		if v := layoutDoc(t, d, 900, Options{}); len(v.d.removes) != 0 {
			t.Errorf("%s: a rail on someone else's message", tc.name)
		}
		h := &botRec{}
		hs := optHarness(d, Options{Own: true}, h)
		var rm *removeBtn
		for i := range hs.v.d.removes {
			if hs.v.d.removes[i].rail {
				rm = &hs.v.d.removes[i]
			}
		}
		if rm == nil {
			t.Fatalf("%s: no rail button", tc.name)
		}
		if rm.r.Dx() != 20 || rm.r.Dy() != 20 || rm.r.Max.X != 0 || rm.r.Min.Y != rm.col.Min.Y {
			t.Errorf("%s: rail button %v (column %v), want 20x20 left of the card's top", tc.name, rm.r, rm.col)
		}
		if tc.v1 != !rm.area.Empty() {
			t.Errorf("%s: hover area %v", tc.name, rm.area)
		}
		p := f32.Pt(float32(rm.r.Min.X+10), float32(rm.r.Min.Y+10))
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		if hs.v.hover != rm.del {
			t.Errorf("%s: pointer on the rail button: hover %d, want %d", tc.name, hs.v.hover, rm.del)
		}
		hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
		if len(h.removed) != 1 || h.removed[0] != "1" {
			t.Errorf("%s: clicking the rail removed %v", tc.name, h.removed)
		}
	}
}

// botRec records RemoveAttachment and OpenBot.
type botRec struct {
	removeRec
	bots []string
}

func (h *botRec) OpenBot(id string, anchor image.Point) { h.bots = append(h.bots, id) }

// "Added by <app>": the v1 card's footer after " | ", the Block Kit
// attachment's line under its blocks (a click opens the bot's card), "a
// bot" until bots.info names it.
func TestAddedBy(t *testing.T) {
	gh := layoutDoc(t, embedDoc(t, "github"), 900, Options{})
	if !textHas(gh, "GitHub | ") || !textHas(gh, "Added by a bot") {
		t.Errorf("v1 footer without the attribution: %q", string(gh.d.plain))
	}
	gh = layoutDoc(t, embedDocWith(t, botRes{}, "github"), 900, Options{})
	if !textHas(gh, "Added by GitHub") {
		t.Errorf("v1 footer: %q, want Added by GitHub", string(gh.d.plain))
	}
	h := &botRec{}
	hs := optHarness(embedDocWith(t, botRes{}, "block kit"), Options{}, h)
	if !textHas(hs.v, "Added by Example Tracker") {
		t.Fatalf("Block Kit attribution missing: %q", string(hs.v.d.plain))
	}
	f := lastFrag(t, &hs.v.d, "Tracker") // the context block says it too
	p := center(f)
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.bots) != 1 || h.bots[0] != fakeslack.EmbedsTrackerBotID {
		t.Errorf("clicking the app's name opened %v", h.bots)
	}
	// the row card's footer: an app unfurl without a bot
	app := attDoc("<https://app.example.com/a>", slack.Attachment{ID: 1, FromURL: "https://app.example.com/a", OriginalURL: "https://app.example.com/a",
		Title: "App", Text: "unfurled by an app", ServiceName: "App", IsAppUnfurl: true})
	if v := layoutDoc(t, app, 900, Options{}); !strings.Contains(string(v.d.plain), "App | Added by a bot") {
		t.Errorf("v3 app unfurl footer: %q", string(v.d.plain))
	}
	// plain link previews have none
	if v := layoutDoc(t, embedDoc(t, "large image"), 900, Options{}); textHas(v, "Added by") {
		t.Error("an unfurl with no app or bot says Added by")
	}
}

// lastFrag is findFrag's last match.
func lastFrag(t *testing.T, d *display, s string) *frag {
	t.Helper()
	var out *frag
	for _, f := range textFrags(d) {
		if f.sh.str == s {
			out = f
		}
	}
	if out == nil {
		t.Fatalf("no text frag %q", s)
	}
	return out
}
