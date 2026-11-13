package huddleui

import (
	"encoding/json"
	"image"
	"image/color"
	"strconv"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The huddle's Captions panel: Slack's HuddleThreadView side panel
// (gantry-v2-async-client-huddle-thread-view 8D21) shows a "Captions" tab
// while huddleEvents.isSideBySideEnabled (mvRv M3), its content
// HuddleEventLog (gantry-v2-async-client-boot-shared jz2D): the log's
// events, sticking to the bottom, each drawn by
//
//   - JOIN, LEAVE, SCREENSHARE_START, SCREENSHARE_STOP, SEND_REACTION:
//     HuddleMetaEvent ("joined the huddle.", "left the huddle.", "started
//     screen sharing.", "stopped screen sharing.", "reacted :emoji:"),
//     which passes its other props on to HuddleBaseEvent;
//   - TRANSCRIBE: HuddleTranscribeEvent, the words on a HuddleBaseEvent;
//   - START_TRANSCRIPTION (first): HuddleEventLogHeader, "Captions are
//     being generated in {language}.";
//
// HuddleBaseEvent is .p-huddle_event_log__base_event: the person's
// avatar (medium), their name (__member_name) over the text
// (__event_text). The three are plugin patch points (slackc), and the log
// is huddleEvents as plugins present it (taut.HuddleEvents):
// HuddleActivity merges its own lines into the events and turns the
// side-by-side view on, so its log shows here.
//
// Clack's huddle has no side-by-side captions of its own (the ⋯ menu's
// Side-by-side isn't built), so without a plugin the panel never opens.
// The panel follows the app's light or dark theme, like the thread panel
// beside a call. Sizes are Slack Kit's (avatar 36, 15/22 text, 16px
// gutters): the event log's stylesheet isn't public.

const (
	ccPanelW   = 340 // the side panel
	ccTabsH    = 48  // the Tabs row
	ccAvatar   = 36  // Avatar size medium
	ccCallMinW = 455 // the compact window's MinSize: the call keeps this much
)

// panelState is the Captions panel's widgets.
type panelState struct {
	list    widget.List
	th      *theme.Theme // the app's theme, on this window's shapers
	dark    bool
	widened bool // the window grew to make room once
	count   int  // events last frame (a new one scrolls to the end)
}

// logView is huddleEvents as plugins present it, while you're in a huddle.
func (w *window) logView() (taut.HuddleEventsView, bool) {
	tt := w.c.taut()
	if tt == nil || w.c.Session() == nil {
		return taut.HuddleEventsView{}, false
	}
	return tt.HuddleEvents()
}

// captionsPanelW is the panel's width this frame (0: no panel).
func (w *window) captionsPanelW(gtx C) int {
	if w.popout != "" {
		return 0
	}
	v, ok := w.logView()
	if !ok || !v.Enabled || !v.IsSideBySideEnabled {
		return 0
	}
	pw := gtx.Dp(ccPanelW)
	if gtx.Constraints.Max.X-pw < gtx.Dp(ccCallMinW) {
		w.widen(gtx)
		pw = max(0, gtx.Constraints.Max.X-gtx.Dp(ccCallMinW))
	}
	return pw
}

// widen grows the window once by the panel's width, as Slack's huddle
// window makes room for its side panel.
func (w *window) widen(gtx C) {
	if w.panel.widened || w.w == nil {
		return
	}
	w.panel.widened = true
	px := gtx.Metric.PxPerDp
	if px <= 0 {
		px = 1
	}
	sz := gtx.Constraints.Max
	w.w.Option(app.Size(unit.Dp(float32(sz.X)/px+ccPanelW), unit.Dp(float32(sz.Y)/px)))
}

// panelTheme is the app's theme on this window's shapers.
func (w *window) panelTheme() *theme.Theme {
	dark := true
	if th := w.c.h.Theme(); th != nil {
		dark = th.Dark
	}
	p := &w.panel
	if p.th == nil {
		p.th = theme.Bare(dark)
		p.th.SetShapers(w.th.Shaper, w.th.Shaper)
		p.dark = dark
	} else if p.dark != dark {
		p.th.SetDark(dark)
		p.dark = dark
	}
	return p.th
}

// captionsPanel draws the panel in r.
func (w *window) captionsPanel(gtx C, r image.Rectangle) {
	v, ok := w.logView()
	if !ok {
		return
	}
	th := w.panelTheme()
	cl := th.C
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	sz := r.Size()
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	theme.Fill(gtx, image.Rectangle{Max: sz}, cl.Bg)
	theme.Fill(gtx, image.Rect(0, 0, gtx.Dp(1), sz.Y), cl.Border)

	// Tabs: the one "Captions" tab, selected
	tabH := gtx.Dp(ccTabsH)
	label := nsHuddles.T("Captions")
	ls := labelSize(gtx, th, 15, theme.Bold, label)
	tx := gtx.Dp(16)
	at(gtx, image.Pt(tx, (tabH-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, theme.Bold, cl.Fg, label) })
	theme.Fill(gtx, image.Rect(tx, tabH-gtx.Dp(2), tx+ls.X, tabH), cl.Fg)
	theme.Fill(gtx, image.Rect(0, tabH-gtx.Dp(1), sz.X, tabH), cl.Border)

	// HuddleEventLog
	events := v.Events
	body := image.Rect(0, tabH, sz.X, sz.Y)
	if v.IsLoading {
		d := gtx.Dp(24)
		at(gtx, image.Pt((sz.X-d)/2, body.Min.Y+(body.Dy()-d)/2), image.Pt(d, d), func(gtx C) D { spinner(gtx, d, cl.FgMuted); return D{} })
		return
	}
	header := len(events) > 0 && eventType(events[0]) == "START_TRANSCRIPTION"
	if header { // stickyItems: the header stays at the top
		hd := w.logHeader(gtx, th, body.Dx())
		body.Min.Y += hd
		events = events[1:]
	}
	p := &w.panel
	p.list.Axis = layout.Vertical
	if len(events) != p.count { // stickToBottom: a new event scrolls to it
		p.list.ScrollToEnd = true
		p.count = len(events)
	}
	at(gtx, body.Min, body.Size(), func(gtx C) D {
		gtx.Constraints = layout.Exact(body.Size())
		return p.list.Layout(gtx, len(events), func(gtx C, i int) D {
			return w.logRow(gtx, th, events, i)
		})
	})
}

// logHeader is HuddleEventLogHeader (.p-huddle_event_log__header).
func (w *window) logHeader(gtx C, th *theme.Theme, width int) int {
	// Clack's transcription has no huddle language setting: Slack's default
	txt := nsHuddles.T("Captions are being generated in {language}.", "language", "English")
	pad := gtx.Dp(16)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(0, width-2*pad), 1<<20)}
	d := th.Layout(g, theme.TextStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, Wrap: text.WrapWords}, txt)
	call := m.Stop()
	h := d.Size.Y + 2*gtx.Dp(12)
	theme.Fill(gtx, image.Rect(0, 0, width, h), th.C.BgInset)
	st := op.Offset(image.Pt(pad, gtx.Dp(12))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	return h
}

func eventType(raw json.RawMessage) string {
	var e struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Type
}

// logRow is event i: its component, by type (HuddleEventLog's rowRenderer).
func (w *window) logRow(gtx C, th *theme.Theme, events []json.RawMessage, i int) D {
	var ev map[string]any
	if json.Unmarshal(events[i], &ev) != nil {
		return D{}
	}
	typ, _ := ev["type"].(string)
	member, _ := ev["memberId"].(string)
	key := "huddle-event-" + strconv.Itoa(i)
	base := slackc.Props{"memberId": member, "isFirstChild": i == 0, "isLastChild": i == len(events)-1}
	switch typ {
	case "JOIN", "LEAVE", "SCREENSHARE_START", "SCREENSHARE_STOP", "SEND_REACTION":
		p := slackc.Props{"$key": key}
		for k, v := range base {
			p[k] = v
		}
		p["event"] = ev
		return slackc.Render(gtx, "HuddleMetaEvent", p, func(gtx C, p slackc.Props) D { return w.metaEvent(gtx, th, key, p) })
	case "TRANSCRIBE":
		content, _ := ev["content"].(string)
		p := slackc.Props{"$key": key + "-b"}
		for k, v := range base {
			p[k] = v
		}
		p["children"] = content
		return slackc.Render(gtx, "HuddleBaseEvent", p, func(gtx C, p slackc.Props) D { return w.baseEvent(gtx, th, p, th.C.Fg) })
	}
	return D{}
}

// metaText is HuddleMetaEvent's line for an event ("" draws nothing).
func metaText(ev map[string]any) string {
	typ, _ := ev["type"].(string)
	switch typ {
	case "JOIN":
		return nsHuddles.T("joined the huddle.")
	case "LEAVE":
		return nsHuddles.T("left the huddle.")
	case "SCREENSHARE_START":
		return nsHuddles.T("started screen sharing.")
	case "SCREENSHARE_STOP":
		return nsHuddles.T("stopped screen sharing.")
	case "SEND_REACTION":
		emoji, _ := ev["emoji"].(string) // getEmojiTranscription: the emoji drawn as text
		return nsHuddles.T("reacted {emojiComponent}", "emojiComponent", ":"+emoji+":")
	}
	return ""
}

// metaEvent is HuddleMetaEvent's native: its text in a
// .p-huddle_event_log__meta_text span on a HuddleBaseEvent, which gets
// every other prop it was given (a plugin's among them).
func (w *window) metaEvent(gtx C, th *theme.Theme, key string, p slackc.Props) D {
	var ev map[string]any
	switch e := p["event"].(type) {
	case slackc.Props: // back from a plugin's <Original>
		ev = e
	case map[string]any:
		ev = e
	}
	t := metaText(ev)
	if t == "" {
		return D{}
	}
	bp := slackc.Props{"$key": key + "-b"}
	for k, v := range p {
		if k != "event" && k != "$key" && k != "children" {
			bp[k] = v
		}
	}
	bp["children"] = t
	return slackc.Render(gtx, "HuddleBaseEvent", bp, func(gtx C, p slackc.Props) D { return w.baseEvent(gtx, th, p, th.C.FgMuted) })
}

// baseEvent is HuddleBaseEvent's native: the avatar, the member's name,
// and the children (text, or what a plugin rendered).
func (w *window) baseEvent(gtx C, th *theme.Theme, p slackc.Props, textCol color.NRGBA) D {
	width := gtx.Constraints.Max.X
	padX, padY := gtx.Dp(16), gtx.Dp(8)
	av := gtx.Dp(ccAvatar)
	member := p.StrOr("memberId", "")
	tx := padX + av + gtx.Dp(8)
	tw := max(0, width-tx-padX)
	at(gtx, image.Pt(padX, padY), image.Pt(av, av), func(gtx C) D {
		avatar(gtx, th, w.c.h.Images(), w.c.avatarURL(member, av), image.Pt(av, av), gtx.Dp(8))
		return D{Size: image.Pt(av, av)}
	})
	name := ""
	if member != "" { // the member name selector has nothing for no member (a plugin's line)
		name = w.c.displayName(member)
	}
	nd := at(gtx, image.Pt(tx, padY), image.Pt(tw, gtx.Dp(22)), func(gtx C) D {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg, MaxLines: 1}, name)
	})
	y := padY + nd.Size.Y
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
	m := op.Record(gtx.Ops)
	var d D
	if e, ok := slackc.PropChildren(p); ok {
		d = slackc.LayoutElementText(g, e, slackc.InheritText{Size: 15, LineHeight: 22, Weight: 400, Color: textCol})
	} else {
		d = th.Layout(g, theme.TextStyle{Size: 15, LineHeight: 22, Color: textCol, Wrap: text.WrapWords}, p.StrOr("children", ""))
	}
	call := m.Stop()
	st := op.Offset(image.Pt(tx, y)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	y += d.Size.Y
	return D{Size: image.Pt(width, max(y, padY+av)+padY)}
}
