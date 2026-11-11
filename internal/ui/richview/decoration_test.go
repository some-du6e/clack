package richview

// Link and text decorations as Slack's CSS draws them (synthetic text,
// nothing talks to Slack):
//
//   - text-decoration decorates an element's spaces too, so a hovered
//     link ("View community log"), the underline style and strikes are
//     one line per line box, not one per word (Karim: "shouldnt it be a
//     single line?"); spaces at a line's end are not decorated;
//   - Accessibility > "Underline links to websites" (underline_links)
//     underlines only Slack's data-stringify-link links and attachment
//     titles (jRB6), not a message card's time or "Posted in" line
//     (Karim: "everything is fucking underlined");
//   - the card's context line hovers with .context__pPPwj a:hover's
//     box-shadow: inset 0 -1px, its time with an underline, and the card
//     draws the member (CardMember): their name 8px before the time, their
//     own picture, the full date and time as the time's tooltip.
//
// With MESSAGES_AUDIT_SHOTS_DIR set (the messages-audit-shots workflow)
// TestDecorationShots saves each state, light and dark.

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.51 }

// linkDoc is "see View community log today" with the middle three words
// a link.
func linkDoc() rt.Doc {
	return doc(para(txt("see "), rt.Span{Kind: rt.Link, Text: "View community log", URL: "https://example.invalid/log"}, txt(" today")))
}

func TestLinkUnderlineSpansSpaces(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, linkDoc(), Options{}, 600)
	see, view, comm, log, today := findFrag(t, &v.d, "see"), findFrag(t, &v.d, "View"), findFrag(t, &v.d, "community"),
		findFrag(t, &v.d, "log"), findFrag(t, &v.d, "today")
	if view.target < 0 || view.target != comm.target || comm.target != log.target {
		t.Fatalf("the link's words have targets %d %d %d", view.target, comm.target, log.target)
	}
	if tg := v.d.targets[view.target]; !tg.underline || !tg.website || tg.inset {
		t.Errorf("a message link's target %+v: want underline on hover, website (underline_links), no inset", tg)
	}
	if !near(view.x+view.w+view.ulT, comm.x) || !near(comm.x+comm.w+comm.ulT, log.x) {
		t.Errorf("the underline stops at the spaces: View ends %.1f (+%.1f), community starts %.1f; community ends %.1f (+%.1f), log starts %.1f",
			view.x+view.w, view.ulT, comm.x, comm.x+comm.w, comm.ulT, log.x)
	}
	if log.ulT != 0 || see.ulT != 0 || today.ulT != 0 {
		t.Errorf("the underline runs past the link: see +%.1f, log +%.1f, today +%.1f", see.ulT, log.ulT, today.ulT)
	}
	// wrapped after "View" (too narrow for "see View community", wide
	// enough for "community"): the space at the end of the line isn't
	// decorated
	v = layoutAt(st, linkDoc(), Options{}, int(comm.x+comm.w)-1)
	view, comm = findFrag(t, &v.d, "View"), findFrag(t, &v.d, "community")
	if view.line == comm.line {
		t.Fatalf("control: the link didn't wrap after View")
	}
	if view.ulT != 0 {
		t.Errorf("the underline runs %.1fpx past the end of the line", view.ulT)
	}
}

func TestStyleDecorationSpansSpaces(t *testing.T) {
	st := newTestStyle()
	for _, s := range []rt.Style{rt.Underline, rt.Strike} {
		d := doc(para(rt.Span{Kind: rt.Text, Text: "under lined", Style: s}, txt(" plain words")))
		v := layoutAt(st, d, Options{}, 600)
		under, lined, plain := findFrag(t, &v.d, "under"), findFrag(t, &v.d, "lined"), findFrag(t, &v.d, "plain")
		if !near(under.x+under.w+under.ulU, lined.x) {
			t.Errorf("style %d: the line stops at the space (under ends %.1f +%.1f, lined starts %.1f)", s, under.x+under.w, under.ulU, lined.x)
		}
		if lined.ulU != 0 || plain.ulU != 0 {
			t.Errorf("style %d: the line runs past the styled text (lined +%.1f, plain +%.1f)", s, lined.ulU, plain.ulU)
		}
	}
}

// testCard is a message unfurl card posted now in #release, with its
// member resolved when member is set (richtext's MemberNamer).
func testCard(member bool) *rt.Attachment {
	ts := strconv.FormatInt(time.Now().Unix(), 10) + ".000100"
	a := &rt.Attachment{MsgCard: true, IsShare: true, AuthorID: "U1", AuthorName: "​", AuthorIcon: "https://avatars.test/U1-48.png",
		MsgTS: ts, FromURL: "https://ws.test/archives/C1/p1700000000000100", ShareChannel: "release", ShareChannelID: "C1",
		Context:     []rt.Span{txt("Posted in "), {Kind: rt.Channel, Text: "release", ID: "C1"}},
		ContextLink: "https://ws.test/archives/C1/p1700000000000100?context",
		Text:        []rt.Block{para(txt("Release checklist is in the doc"))}}
	if member {
		a.CardMember, a.CardAuthor, a.CardAvatar = true, "ally", "https://avatars.test/U1-72.png"
	}
	return a
}

// cardTargets finds the card's time and context link targets.
func cardTargets(t *testing.T, v *View, a *rt.Attachment) (tm, ctx int16) {
	t.Helper()
	tm, ctx = -1, -1
	for i, tg := range v.d.targets {
		switch {
		case tg.kind == tLink && tg.url == a.FromURL && tg.underline:
			tm = int16(i)
		case tg.kind == tLink && tg.url == a.ContextLink:
			ctx = int16(i)
		}
	}
	if tm < 0 || ctx < 0 {
		t.Fatalf("no time (%d) or context (%d) target in %+v", tm, ctx, v.d.targets)
	}
	return tm, ctx
}

func fragsOf(v *View, tg int16) []*frag {
	var out []*frag
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.target == tg {
			out = append(out, f)
		}
	}
	return out
}

func TestMsgCardLinks(t *testing.T) {
	st := newTestStyle()
	a := testCard(true)
	v := layoutAt(st, doc(rt.Block{Kind: rt.AttachmentBlock, Att: a}), Options{}, 600)
	tm, ctx := cardTargets(t, v, a)
	if tg := v.d.targets[tm]; tg.website || tg.inset {
		t.Errorf("the time's target %+v: underlined on hover only, never by underline_links", tg)
	}
	if tg := v.d.targets[ctx]; tg.website || tg.underline || !tg.inset {
		t.Errorf("the context's target %+v: box-shadow inset on hover, no underline", tg)
	}
	// the member: their name and picture, not author_name / author_icon
	ally := findFrag(t, &v.d, "ally")
	if v.d.targets[ally.target].kind != tUser || !v.d.targets[ally.target].underline {
		t.Errorf("the author's target %+v: the member's MemberLink", v.d.targets[ally.target])
	}
	var avatar *imgPrim
	for i := range v.d.imgs {
		if im := &v.d.imgs[i]; im.url == a.AuthorIcon {
			t.Errorf("the card draws author_icon %q", im.url)
		} else if im.url == a.CardAvatar {
			avatar = im
		}
	}
	if avatar == nil || avatar.px != 36 || avatar.r.Dx() != 36 {
		t.Fatalf("the member's avatar %+v: want CardAvatar in the 36px slot", avatar)
	}
	tf := fragsOf(v, tm)
	if len(tf) == 0 || tf[0].kind != fText {
		t.Fatalf("no time frags")
	}
	if gap := tf[0].x - (ally.x + ally.w); !near(gap, 8) {
		t.Errorf("name to time %.1fpx, want 8 (.authorName 4 + MemberName's .c-member_name 4)", gap)
	}
	// the full date and time is the time's tooltip
	for _, f := range tf {
		if f.tip == 0 || !strings.HasPrefix(v.d.tips[f.tip-1], "Today at ") || strings.Count(v.d.tips[f.tip-1], ":") != 2 {
			t.Errorf("time frag %q: tooltip %d %q", f.sh.str, f.tip, v.d.tips)
			break
		}
	}
	// hovering the card or its own links strengthens its border; its files don't count
	var card *box
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.kind == bBorder && b.hover.A > 0 {
			card = b
		}
	}
	if card == nil || !(card.target < tm && tm <= card.tgTo && card.target < ctx && ctx <= card.tgTo) {
		t.Errorf("the card's hover box %+v doesn't cover its links %d %d", card, tm, ctx)
	}

	// without the member yet: author_name and author_icon, 4px apart
	a = testCard(false)
	v = layoutAt(st, doc(rt.Block{Kind: rt.AttachmentBlock, Att: a}), Options{}, 600)
	found := false
	for _, im := range v.d.imgs {
		found = found || im.url == a.AuthorIcon
	}
	if !found {
		t.Error("an unresolved member's card lost author_icon")
	}
}

func TestFullTimestamp(t *testing.T) {
	loc := time.FixedZone("test", 0)
	now := time.Date(2026, 11, 10, 22, 0, 0, 0, loc)
	at := time.Date(2026, 11, 10, 21, 54, 15, 0, loc)
	if got := fullTimestamp(strconv.FormatInt(at.Unix(), 10)+".504199", now); got != "Today at 9:54:15 PM" {
		t.Errorf("%q, want getFullTimestamp's \"Today at 9:54:15 PM\"", got)
	}
	if got := fullTimestamp(strconv.FormatInt(at.AddDate(0, 0, -3).Unix(), 10)+".000100", now); got != "Nov 7th at 9:54:15 PM" {
		t.Errorf("%q", got)
	}
}

// bandCount counts pixels of col in an underline band y0..y1 over x0..x1.
func bandCount(img *image.RGBA, x0, x1 float32, y0, y1 int, col color.NRGBA) int {
	return pixeltest.Count(img, image.Rect(int(x0+0.5), y0, int(x1+0.5), y1), col, 60)
}

func TestDecorationShots(t *testing.T) {
	old := uipref.Get()
	t.Cleanup(func() { uipref.Set(*old) })
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	save := func(name string, img image.Image) {
		if dir == "" {
			return
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		st := NewStyle(themeShaper(), noImages{}, pal)
		const w, h = 560, 200
		render := func(v *View, hover int16) *image.RGBA {
			var ops op.Ops
			paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
			v.hover = hover
			v.Layout(testGtx(&ops, w), st, &recHandler{})
			v.hover = hover // Layout's events may reset it: draw with it once more
			ops.Reset()
			paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
			v.draw(testGtx(&ops, w), st)
			return pixeltest.Render(t, w, h, &ops)
		}
		// underline band of a frag (underline()'s) and the space after it
		band := func(f *frag) (int, int) {
			y := f.y + max(1, f.size/8)
			return y, y + max(1, f.size/14)
		}

		// 1. a hovered link: one underline across "View community log"
		uipref.Set(store.Prefs{})
		v := New(linkDoc(), Options{})
		v.relayout(st, w, 1, 1)
		view, comm := findFrag(t, &v.d, "View"), findFrag(t, &v.d, "community")
		img := render(v, view.target)
		save("links-underline-hover-"+mode, img)
		y0, y1 := band(view)
		gap := pal.Link
		if n, want := bandCount(img, view.x+view.w, comm.x, y0, y1, gap), int(comm.x-view.x-view.w)*(y1-y0)/2; n < want {
			t.Errorf("%s: the hovered link's underline has %d px over the space after View, want %d (a single line)", mode, n, want)
		}

		// 2. underline_links on: the message link is underlined, the
		// card's time and context aren't
		uipref.Set(store.Prefs{UnderlineLinks: true})
		a := testCard(true)
		d := doc(para(txt("see "), rt.Span{Kind: rt.Link, Text: "View community log", URL: "https://example.invalid/log"}),
			rt.Block{Kind: rt.AttachmentBlock, Att: a})
		v = New(d, Options{})
		v.relayout(st, w, 1, 1)
		img = render(v, -1)
		save("links-card-underline-pref-"+mode, img)
		view = findFrag(t, &v.d, "View")
		y0, y1 = band(view)
		if n := bandCount(img, view.x, view.x+view.w, y0, y1, gap); n < int(view.w)/2 {
			t.Errorf("%s: control: the message link isn't underlined with underline_links on (%d px)", mode, n)
		}
		tm, ctx := cardTargets(t, v, a)
		ter := pal.CardFooter
		for _, tg := range []int16{tm, ctx} {
			for _, f := range fragsOf(v, tg) {
				if f.kind != fText {
					continue
				}
				y0, y1 := band(f)
				if n := bandCount(img, f.x, f.x+f.w, y0, y1, ter); n > int(f.w)/4 {
					t.Errorf("%s: the card's %q is underlined by underline_links (%d px)", mode, f.sh.str, n)
				}
			}
		}

		// 3. the time hovered: underlined; the context hovered: the inset
		// line on its box's bottom edge, icon included, no underline
		uipref.Set(store.Prefs{})
		img = render(v, tm)
		save("links-card-hover-time-"+mode, img)
		tf := fragsOf(v, tm)
		y0, y1 = band(tf[0])
		if n := bandCount(img, tf[0].x, tf[0].x+tf[0].w, y0, y1, ter); n < int(tf[0].w)/2 {
			t.Errorf("%s: the hovered time isn't underlined (%d px)", mode, n)
		}
		img = render(v, ctx)
		save("links-card-hover-context-"+mode, img)
		cf := fragsOf(v, ctx)
		var x0, x1 float32 = 1e9, 0
		var text *frag
		for _, f := range cf {
			x0, x1 = min(x0, f.x), max(x1, f.x+f.w)
			if f.kind == fText && text == nil {
				text = f
			}
		}
		yb := text.y + int(text.sh.desc+0.5)
		if n := bandCount(img, x0, x1, yb-1, yb, ter); n < int(x1-x0)*3/4 {
			t.Errorf("%s: the hovered context has %d px of its inset line over %.0f px", mode, n, x1-x0)
		}
		y0, y1 = band(text)
		if y1 <= yb-1 { // the underline band sits above the box's bottom edge
			if n := bandCount(img, text.x, text.x+text.w, y0, y1, ter); n > int(text.w)/4 {
				t.Errorf("%s: the hovered context is underlined (%d px)", mode, n)
			}
		}
	}
}
