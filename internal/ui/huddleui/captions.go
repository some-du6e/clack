package huddleui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Closed captions, as Slack's HuddleClosedCaptions draws them
// (gantry-v2-async-client-boot-shared PWaJ, client-boot mvRv, and
// client-boot-styles' .p-huddle_closed_caption* rules, bv1-13):
//
//   - huddleEvents' captions: a speaker's TRANSCRIBE events that start
//     within 2 s of their previous one join into one event; the last 5
//     events show.
//   - .p-huddle_closed_captions: at the content's bottom left (bottom
//     48px, padding 0 8px 20px, so 12px above the toolbar), at most half
//     its width, newest at the bottom.
//   - each event is its own black box (constants-black, radius 6, padding
//     4px 8px, 4px apart, as wide as its text): the speaker's name and a
//     colon in bold, 8px, then the words in white.
//   - an event fades out (.p-huddle_closed_caption_event__wrapper_fade_out,
//     opacity .1s) 5 s after its text last changed; hovering the captions
//     shows them all again.

const (
	ccJoin  = 2 * time.Second // mvRv: e.timestampMs - t.endTimeMs < 2e3
	ccShown = 5               // .slice(-5)
	ccFade  = 5 * time.Second // HuddleClosedCaptionBaseEvent's timeout
)

// ccEvent is one grouped caption event; begin is the event log's
// START_TRANSCRIPTION ("Captions have been turned on.").
type ccEvent struct {
	user, text string
	start, end time.Time
	begin      bool
}

// ccStarted is HuddleClosedCaptionStartEvent's text.
const ccStarted = "Captions have been turned on."

// ccState tracks when each event's text last changed (for the fade).
type ccState struct {
	seen    map[string]ccSeen // by user + start
	hovered bool
}

type ccSeen struct {
	text string
	at   time.Time
}

// captionEvents groups st's caption lines like Slack's huddleEvents
// selector and keeps the last five.
func captionEvents(lines []huddle.Caption) []ccEvent {
	var out []ccEvent
	for _, l := range lines {
		if l.Start { // a non-TRANSCRIBE event: its own, ending the one before
			out = append(out, ccEvent{user: l.UserID, text: nsHuddles.T(ccStarted), start: l.At, end: l.At, begin: true})
			continue
		}
		t := strings.TrimSpace(l.Text)
		if t == "" {
			continue
		}
		if n := len(out); n > 0 && !out[n-1].begin && out[n-1].user == l.UserID && l.At.Sub(out[n-1].end) < ccJoin {
			out[n-1].text += " " + t
			out[n-1].end = l.At
			continue
		}
		out = append(out, ccEvent{user: l.UserID, text: t, start: l.At, end: l.At})
	}
	if len(out) > ccShown {
		out = out[len(out)-ccShown:]
	}
	return out
}

func (w *window) captions(gtx C, st huddle.State, stage image.Rectangle, now time.Time) {
	evs := captionEvents(st.Captions)
	if len(evs) == 0 {
		return
	}
	if w.cc.seen == nil {
		w.cc.seen = map[string]ccSeen{}
	}
	keep := map[string]ccSeen{}
	maxW := stage.Dx() / 2
	pad, padY := gtx.Dp(8), gtx.Dp(4)
	type box struct {
		call  op.CallOp
		sz    image.Point
		alpha float32
	}
	var boxes []box
	visible := false
	for _, e := range evs {
		k := e.user + "/" + strconv.FormatInt(e.start.UnixNano(), 10)
		if e.begin {
			k = "start-event"
		}
		s, ok := w.cc.seen[k]
		if !ok || s.text != e.text {
			s = ccSeen{text: e.text, at: now}
		}
		keep[k] = s
		alpha := float32(1)
		if age := now.Sub(s.at); age > ccFade && !w.cc.hovered {
			alpha = max(0, 1-float32(age-ccFade)/float32(100*time.Millisecond))
		} else if age <= ccFade {
			gtx.Execute(op.InvalidateCmd{At: s.at.Add(ccFade)})
		}
		if alpha > 0 {
			visible = true
		}
		m := op.Record(gtx.Ops)
		name := w.c.displayName(e.user) + ":"
		if e.begin { // no member name: just the words
			name = ""
		}
		sz := w.ccEvent(gtx, name, e.text, max(0, maxW-2*pad))
		call := m.Stop()
		boxes = append(boxes, box{call: call, sz: sz.Add(image.Pt(2*pad, 2*padY)), alpha: alpha})
	}
	w.cc.seen = keep
	if !visible && !w.cc.hovered {
		return
	}
	y := stage.Max.Y - gtx.Dp(12)
	x := stage.Min.X
	var area image.Rectangle
	for i := len(boxes) - 1; i >= 0; i-- {
		b := boxes[i]
		r := image.Rect(x, y-b.sz.Y, x+b.sz.X, y)
		area = area.Union(r)
		y -= b.sz.Y + gtx.Dp(4)
		if b.alpha <= 0 {
			continue
		}
		func() {
			defer paint.PushOpacity(gtx.Ops, b.alpha).Pop()
			theme.RRect(gtx, r, gtx.Dp(6), color.NRGBA{A: 0xff})
			defer op.Offset(image.Pt(r.Min.X+pad, r.Min.Y+padY)).Push(gtx.Ops).Pop()
			b.call.Add(gtx.Ops)
		}()
	}
	// .p-huddle_closed_captions:hover shows every event again
	hv := w.click("cc-hover")
	w.cc.hovered = hv.Hovered()
	at(gtx, area.Min, area.Size(), func(gtx C) D { return passHover(gtx, hv, area.Size()) })
}

// ccEvent draws one caption event's text at the origin and returns its
// size: the bold name with its colon, 8px (padding-right), then the words.
// Both are inline spans, so the words wrap back to the left edge, at most
// maxW wide, on 22px lines.
func (w *window) ccEvent(gtx C, name, text string, maxW int) image.Point {
	th := w.th
	lineH := gtx.Sp(22)
	free := layout.Constraints{Max: image.Pt(1<<20, lineH)}
	put := func(x, y int, wt font.Weight, s string) int {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = free
		d := th.Label(g, 15, wt, hudWhite, s)
		return d.Size.X
	}
	lh := labelSize(gtx, th, 15, theme.Regular, "Hg").Y
	dy := (lineH - lh) / 2
	x := 0
	if name != "" {
		x = put(0, dy, theme.Bold, name) + gtx.Dp(8)
	}
	width := x
	var line strings.Builder
	lx, y := x, 0
	flush := func() {
		if line.Len() > 0 {
			width = max(width, lx+put(lx, y+dy, theme.Regular, strings.TrimRight(line.String(), " ")))
		}
	}
	cur := 0 // the line's width so far
	// at the text's line break opportunities (kit.go breakUnits): after
	// spaces, and between CJK characters, so ja-JP's text wraps too
	for _, u := range breakUnits(strings.Join(strings.Fields(text), " ")) {
		fit := th.Measure(gtx, 15, theme.Regular, strings.TrimRight(u, " "))
		if (line.Len() > 0 || lx > 0) && lx+cur+fit > maxW { // wrap to the box's left edge
			flush()
			line.Reset()
			lx, y, cur = 0, y+lineH, 0
		}
		line.WriteString(u)
		cur += th.Measure(gtx, 15, theme.Regular, u)
	}
	flush()
	return image.Pt(min(width, max(maxW, x)), y+lineH)
}

// passHover tracks the pointer over sz with cl without taking clicks
// from what's underneath.
func passHover(gtx C, cl *widget.Clickable, sz image.Point) D {
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(sz)
	return cl.Layout(gtx, func(gtx C) D { return D{Size: sz} })
}

// banner is HuddlesInformationalBanner in the expanded window: a neutral
// c-banner (base-inv-pry, content-inv-pry, bold, at least 48px, radius 8,
// 4px from the top) with its icon and a close button. Clack shows the
// "user_was_muted" one: someone's mute request reached you.
type banner struct {
	text, icon string
	link       string // a link after the text ("Undo")
	do         func() // the link's action (the window's goroutine)
	at         time.Time
}

// showBanner opens the banner (any goroutine).
func (c *Controller) showBanner(text, icon string) { c.showBannerLink(text, icon, "", nil) }

// showBannerLink is showBanner with a link after the text.
func (c *Controller) showBannerLink(text, icon, link string, do func()) {
	c.mu.Lock()
	w := c.win
	c.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	w.banner = banner{text: text, icon: icon, link: link, do: do, at: time.Now()}
	w.mu.Unlock()
	w.invalidate()
}

var bannerBg = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff} // base-inv-pry

func (w *window) layoutBanner(gtx C, s *huddle.Session, stage image.Rectangle, now time.Time) {
	w.mu.Lock()
	b := w.banner
	w.mu.Unlock()
	// the banner's first choice is SpeakingWhenMuted: "You’re muted."
	// (micalerts.go), then the event-driven ones (Someone muted you.,
	// Self-view is hidden.)
	muted := w.c.mutedBanner(s)
	if muted {
		b = banner{text: nsHuddles.T("You’re muted."), icon: "mic-off"}
	}
	if b.text == "" {
		return
	}
	if w.click("banner-close").Clicked(gtx) {
		if muted {
			w.c.closeMutedBanner(s)
			return
		}
		w.mu.Lock()
		w.banner = banner{}
		w.mu.Unlock()
		return
	}
	if b.do != nil && w.click("banner-link").Clicked(gtx) {
		w.mu.Lock()
		w.banner = banner{}
		w.mu.Unlock()
		b.do()
		return
	}
	h := gtx.Dp(48)
	r := image.Rect(stage.Min.X, stage.Min.Y+gtx.Dp(4), stage.Max.X, stage.Min.Y+gtx.Dp(4)+h)
	theme.RRect(gtx, r, gtx.Dp(8), bannerBg)
	ts := labelSize(gtx, w.th, 15, theme.Bold, b.text)
	is := gtx.Dp(20)
	cw := is + gtx.Dp(8) + ts.X
	var ls image.Point
	if b.link != "" {
		ls = labelSize(gtx, w.th, 15, theme.Regular, b.link)
		cw += gtx.Dp(4) + ls.X
	}
	x := r.Min.X + (r.Dx()-cw)/2
	if b.link != "" { // a c-link in the banner's colour, underlined on hover
		lx := x + is + gtx.Dp(8) + ts.X + gtx.Dp(4)
		lp := image.Pt(lx, r.Min.Y+(h-ls.Y)/2)
		cl := w.click("banner-link")
		at(gtx, lp, ls, func(gtx C) D {
			return button(gtx, cl, ls, nil, func(gtx C) D {
				d := w.th.Label(gtx, 15, theme.Regular, hudWhite, b.link)
				if cl.Hovered() {
					theme.Fill(gtx, image.Rect(0, d.Baseline+gtx.Dp(2), ls.X, d.Baseline+gtx.Dp(3)), hudWhite)
				}
				return d
			})
		})
	}
	at(gtx, image.Pt(x, r.Min.Y+(h-is)/2), image.Pt(is, is), func(gtx C) D {
		return icons.Get(b.icon)(gtx, 20, hudWhite)
	})
	at(gtx, image.Pt(x+is+gtx.Dp(8), r.Min.Y+(h-ts.Y)/2), ts, func(gtx C) D {
		return w.th.Label(gtx, 15, theme.Bold, hudWhite, b.text)
	})
	bs := gtx.Dp(28)
	at(gtx, image.Pt(r.Max.X-bs-gtx.Dp(10), r.Min.Y+(h-bs)/2), image.Pt(bs, bs), func(gtx C) D {
		return button(gtx, w.click("banner-close"), image.Pt(bs, bs), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(6), hudBtnHover)
			}
		}, func(gtx C) D { iconAt(gtx, image.Pt(bs, bs), "close", 16, hudWhite); return D{} })
	})
}

// huddleLoading is Slack's HuddleLoading: its spinner over "Joining
// huddle…" while the join runs.
func huddleLoading(gtx C, th *theme.Theme, sz image.Point) {
	d := gtx.Dp(32)
	ls := labelSize(gtx, th, 15, theme.Bold, nsHuddles.T("Joining huddle…"))
	y := (sz.Y - d - gtx.Dp(12) - ls.Y) / 2
	at(gtx, image.Pt((sz.X-d)/2, y), image.Pt(d, d), func(gtx C) D { spinner(gtx, d, hudWhite); return D{} })
	at(gtx, image.Pt((sz.X-ls.X)/2, y+d+gtx.Dp(12)), ls, func(gtx C) D {
		return th.Label(gtx, 15, theme.Bold, hudWhite, nsHuddles.T("Joining huddle…"))
	})
}

// windowTitle is HuddleMvaWindowTitle's text: the huddle's topic when it
// has one, else "Huddle with {name}" in a DM or group DM (the members'
// names, no @), "Huddle in {channel}" (no # sign:
// publicChannelIncludeHashSign false) elsewhere; and the subtitle "{n}
// people" / "{n} person", which DMs don't get.
func (c *Controller) windowTitle(st huddle.State) (title, sub string) {
	n := max(1, len(st.Participants))
	sub = nsHuddles.T("{memberCount, plural, =1 {# person} other {# people}}", "memberCount", n)
	if t := strings.TrimSpace(st.Name); t != "" { // the huddle's topic (topic.go) is the title
		sub = c.titleSub(st, sub)
		return t, sub
	}
	app := c.h.Core()
	if app == nil {
		return nsHuddles.T("Huddle"), sub
	}
	conv, ok := app.Conv(st.Channel)
	if !ok {
		return nsHuddles.T("Huddle"), sub
	}
	switch conv.Kind {
	case "im":
		if name := c.userName(conv.User); name != "" {
			return nsHuddles.T("Huddle with {dmPersons}", "dmPersons", name), ""
		}
		return nsHuddles.T("Huddle"), ""
	case "mpim":
		var names []string
		self := c.selfID()
		for _, m := range conv.Members {
			if m != self {
				if name := c.userName(m); name != "" {
					names = append(names, name)
				}
			}
		}
		if len(names) == 0 {
			return nsHuddles.T("Huddle"), ""
		}
		return nsHuddles.T("Huddle with {dmPersons}", "dmPersons", strings.Join(names, ", ")), ""
	}
	if conv.Name == "" {
		return nsHuddles.T("Huddle"), sub
	}
	return nsHuddles.T("Huddle in {channelName}", "channelName", conv.Name), sub
}

// titleSub is the subtitle under a topic: none in a DM or group DM.
func (c *Controller) titleSub(st huddle.State, sub string) string {
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(st.Channel); ok && (conv.Kind == "im" || conv.Kind == "mpim") {
			return ""
		}
	}
	return sub
}

// withoutSelf drops your own tile (Hide self-view).
func withoutSelf(ps []huddle.Participant) []huddle.Participant {
	out := ps[:0:0]
	for _, p := range ps {
		if !p.Self {
			out = append(out, p)
		}
	}
	return out
}
