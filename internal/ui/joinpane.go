package ui

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// What Slack draws in place of the composer, ported from its web client
// (build a84662d2d135, 2026-11-08; webpack ids in gantry-v2-async-client-
// boot-{render,shared}):
//
//   - a channel the viewer isn't in (core.Conv.Preview, Opi6 isPreview):
//     MessagePaneInput renders MessagePaneInputPreview > JoinChannelPane
//     (boot-shared UY14). Classic layout (channel_join_experience_with_agents
//     off, as on the test account), centered: the bold channel name
//     (thread: "To reply to this thread, join #name"), the purpose (2
//     lines), then [Details] [Join Channel]. Under temp_join_channel the
//     Join button is a split button whose caret ("Join channel
//     temporarily") opens "Join…": Just for today / For 48 hours / For 1
//     week (conversations.join expiration_ts). Opened from the channel
//     browser, a "Back to All channels" link follows.
//   - an archived channel: MessagePaneInputDisabled (boot-render) with
//     "You are viewing #name, an archived channel" and a small outline
//     "Close Channel" button that goes back to the previous conversation.
//     In a thread: "You are viewing a thread from an archived channel".
//   - a posting-restricted channel (core/posting.go): the composer's own
//     MessageInputRoadblock with MessageInputAnnounceOnlyRoadblock's
//     message, so Taut's RestrictedChannelWarning still patches it
//     (composerRoadblock below).
//
// Not ported: the revamped layout's facepile and member captions, the
// code-channel summary, Slack Connect's connection info line, the offline
// tooltip ("Go online to join this channel"), the suggested-channel
// prefix, and "Unarchive" for viewers who may unarchive.

type footerKind uint8

const (
	footNone footerKind = iota
	footPreview
	footArchived
)

// joinPane is one conversation's footer state.
type joinPane struct {
	details, join, caret, back, close widget.Clickable
	caretTip                          popups.TipState
	joining                           bool
	// window coords of the buttons (menu anchor, tests)
	detailsRect, joinRect, caretRect image.Rectangle
}

// Slack's help article for announcement-only channels (articleId
// 0x53d1f2cb9f in MessageInputAnnounceOnlyRoadblock).
const announceOnlyHelpURL = "https://slack.com/help/articles/360004635551"

// announceOnlyMessage is MessageInputAnnounceOnlyRoadblock's text, before
// its "Learn more" link.
const announceOnlyMessage = "Only certain people can post in this channel."

// convOf is the conversation's model entry: the sidebar's, else the
// resolved one (a channel outside the sidebar).
func (a *App) convOf(ch string) *convInfo {
	if c := a.model().conv(ch); c != nil {
		return c
	}
	if a.data != nil {
		if c, ok := a.data.ext[ch]; ok {
			return c
		}
	}
	return nil
}

// footerFor says what replaces the composer of c.
func (a *App) footerFor(c *convo) footerKind {
	ci := a.convOf(c.ch)
	if ci == nil || (ci.Kind != "channel" && ci.Kind != "private") {
		return footNone
	}
	switch {
	case ci.Archived:
		return footArchived
	case ci.Preview:
		return footPreview
	}
	return footNone
}

// layoutFooter draws the footer for kind at the pane's width; org is its
// window origin.
func (a *App) layoutFooter(gtx layout.Context, c *convo, kind footerKind, org image.Point) layout.Dimensions {
	if c.jp == nil {
		c.jp = &joinPane{}
	}
	ci := a.convOf(c.ch)
	if ci == nil {
		return layout.Dimensions{}
	}
	m := gtx.Dp(20)
	if c.ts != "" {
		m = gtx.Dp(16)
	}
	W := gtx.Constraints.Max.X
	inner := gtx
	inner.Constraints = layout.Constraints{Max: image.Pt(max(W-2*m, 1), gtx.Constraints.Max.Y)}
	rec := op.Record(gtx.Ops)
	var d layout.Dimensions
	switch kind {
	case footPreview:
		d = a.layoutJoinPane(inner, c, ci, org.Add(image.Pt(m, 0)))
	case footArchived:
		d = a.layoutArchivedBar(inner, c, ci)
	}
	call := rec.Stop()
	t := op.Offset(image.Pt(m, 0)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: image.Pt(W, d.Size.Y+gtx.Dp(20))}
}

// ---- JoinChannelPane ----

func (a *App) layoutJoinPane(gtx layout.Context, c *convo, ci *convInfo, org image.Point) layout.Dimensions {
	jp := c.jp
	th := a.th
	W := gtx.Constraints.Max.X
	padX, padY := gtx.Dp(20), gtx.Dp(20)
	cw := max(W-2*padX, 1)
	temp := a.data != nil && a.data.core.TempJoinEnabled() && ci.Kind == "channel"

	// actions (read before drawing, Gio style)
	if clicked(&jp.details) {
		a.OpenDetails(c.ch)
	}
	if clicked(&jp.join) {
		a.joinPreview(c, "")
	}
	if clicked(&jp.caret) && temp {
		a.openTempJoinMenu(c)
	}
	if clicked(&jp.back) {
		if r := a.previewBack(c.ch); r != "" {
			a.Navigate(string(r))
		}
	}

	var rows []op.CallOp
	var hs []int
	add := func(w layout.Widget) {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
		m := op.Record(gtx.Ops)
		d := w(g)
		rows = append(rows, m.Stop())
		hs = append(hs, d.Size.Y)
	}
	// .p-message_pane_input__preview_subtitle
	add(func(gtx layout.Context) layout.Dimensions {
		return centerRow(gtx, a.joinTitle(gtx, c, ci))
	})
	// .p-message_pane_input__description-text: the purpose, 2 lines
	if ci.Purpose != "" {
		add(func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Color: th.C.FgMuted,
				MaxLines: 2, Align: text.Middle}, ci.Purpose)
		})
	}
	// .p-message_pane_input__button-wrapper: Details, Join Channel
	btnRow := len(rows)
	var locDet, locJoin, locCaret image.Rectangle // in the button row
	add(func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(8)
		top := gtx.Dp(16)
		mDet := op.Record(gtx.Ops)
		dd := popups.Button(gtx, th, &jp.details, popups.BtnOutline, popups.BtnMedium, "Details", "", 0)
		cDet := mDet.Stop()
		mJoin := op.Record(gtx.Ops)
		var dj layout.Dimensions
		var caretX int
		if temp {
			dj, caretX = a.splitJoinButton(gtx, jp)
		} else {
			dj = popups.Button(gtx, th, &jp.join, popups.BtnPrimary, popups.BtnMedium, "Join Channel", "", 0)
		}
		cJoin := mJoin.Stop()
		w := dd.Size.X + gap + dj.Size.X
		h := max(dd.Size.Y, dj.Size.Y)
		x := max((gtx.Constraints.Max.X-w)/2, 0)
		t := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
		cDet.Add(gtx.Ops)
		t.Pop()
		jx := x + dd.Size.X + gap
		t = op.Offset(image.Pt(jx, top)).Push(gtx.Ops)
		cJoin.Add(gtx.Ops)
		t.Pop()
		locDet = image.Rectangle{Min: image.Pt(x, top), Max: image.Pt(x+dd.Size.X, top+dd.Size.Y)}
		locJoin = image.Rectangle{Min: image.Pt(jx, top), Max: image.Pt(jx+dj.Size.X, top+dj.Size.Y)}
		locCaret = image.Rectangle{}
		if temp {
			locJoin.Max.X = jx + caretX
			locCaret = image.Rectangle{Min: image.Pt(jx+caretX, top), Max: image.Pt(jx+dj.Size.X, top+dj.Size.Y)}
		}
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, top+h)}
	})
	// "Back to All channels" (opened from the channel browser)
	if a.previewBack(c.ch) != "" {
		add(func(gtx layout.Context) layout.Dimensions {
			top := gtx.Dp(12)
			lw := th.Measure(gtx, 15, theme.Regular, "Back to All channels")
			x := max((gtx.Constraints.Max.X-lw)/2, 0)
			t := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
			d := clickArea(gtx, &jp.back, nil, func(gtx layout.Context) layout.Dimensions {
				col := th.C.Link
				if jp.back.Hovered() {
					col = theme.Mix(col, th.C.Fg, .2)
				}
				return th.Label(gtx, 15, theme.Regular, col, "Back to All channels")
			})
			t.Pop()
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, top+d.Size.Y)}
		})
	}
	descGap := 0
	if ci.Purpose != "" {
		descGap = gtx.Dp(4) // the description sits 4px under the name
	}
	h := padY + descGap + padY
	for _, rh := range hs {
		h += rh
	}
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(W, h)}, gtx.Dp(8), th.C.BgInset)
	y := padY
	for i, call := range rows {
		if i == 1 {
			y += descGap
		}
		t := op.Offset(image.Pt(padX, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		if i == btnRow {
			o := org.Add(image.Pt(padX, y))
			jp.detailsRect, jp.joinRect, jp.caretRect = locDet.Add(o), locJoin.Add(o), image.Rectangle{}
			if !locCaret.Empty() {
				jp.caretRect = locCaret.Add(o)
			}
		}
		y += hs[i]
	}
	if jp.caret.Hovered() && temp {
		r := jp.caretRect
		t := op.Offset(r.Min.Sub(org)).Push(gtx.Ops)
		popups.Tooltip(gtx, th, &jp.caretTip, true, r.Size(), popups.Above, "Join channel temporarily", "")
		t.Pop()
	} else {
		popups.Tooltip(gtx, th, &jp.caretTip, false, image.Point{}, popups.Above, "", "")
	}
	return layout.Dimensions{Size: image.Pt(W, h)}
}

// joinTitle draws the subtitle: the bold channel name with its icon, or
// "To reply to this thread, join #name" in a thread.
func (a *App) joinTitle(gtx layout.Context, c *convo, ci *convInfo) layout.Widget {
	th := a.th
	icon := "hash"
	if ci.Kind == "private" {
		icon = "lock"
	}
	name := ci.Name
	return func(gtx layout.Context) layout.Dimensions {
		size, weight := unit.Sp(18), theme.Black
		x := 0
		lh := gtx.Sp(26)
		if c.ts != "" {
			size, weight, lh = 15, theme.Bold, gtx.Sp(22)
			d := th.Label(gtx, 15, theme.Regular, th.C.Fg, "To reply to this thread, join ")
			x = d.Size.X
		}
		is := gtx.Sp(size)
		t := op.Offset(image.Pt(x, (lh-is)/2)).Push(gtx.Ops)
		icons.Get(icon)(gtx, unit.Dp(float32(size)), th.C.Fg)
		t.Pop()
		x += is + gtx.Dp(2)
		g := gtx
		g.Constraints.Max.X = max(gtx.Constraints.Max.X-x, 1)
		t = op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		d := th.Layout(g, theme.TextStyle{Size: size, Weight: weight, Color: th.C.Fg, MaxLines: 1, LineHeight: unit.Sp(float32(lh) / gtx.Metric.PxPerSp)}, name)
		t.Pop()
		return layout.Dimensions{Size: image.Pt(x+d.Size.X, max(lh, d.Size.Y))}
	}
}

// centerRow centers w horizontally in the constraints.
func centerRow(gtx layout.Context, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := w(g)
	call := m.Stop()
	x := max((gtx.Constraints.Max.X-d.Size.X)/2, 0)
	t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
}

// splitJoinButton is UY14's split button: a primary "Join Channel" with
// the right corners squared and a caret part (min-width 32px) behind a
// 1px rgba(255,255,255,.4) divider inset 8px top and bottom. caretX is
// where the caret part starts.
func (a *App) splitJoinButton(gtx layout.Context, jp *joinPane) (layout.Dimensions, int) {
	th := a.th
	h := gtx.Dp(36)
	pad := gtx.Dp(12)
	tw := th.Measure(gtx, 15, theme.Bold, "Join Channel")
	mainW := tw + 2*pad
	caretW := gtx.Dp(40) // Slack: the "Join channel temporarily" trigger is 40×36
	rad := gtx.Dp(8)
	bg := th.C.Send
	hover := theme.Mix(bg, color.NRGBA{A: 0xff}, .1)
	whole := image.Rect(0, 0, mainW+caretW, h)
	part := func(c *widget.Clickable, r image.Rectangle) {
		t := op.Offset(r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Min: r.Size(), Max: r.Size()}
		clickArea(g, c, nil, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: r.Size()}
		})
		t.Pop()
	}
	// background, per-half hover, clipped to the whole rounded button
	st := clip.UniformRRect(whole, rad).Push(gtx.Ops)
	theme.Fill(gtx, whole, bg)
	if jp.join.Hovered() {
		theme.Fill(gtx, image.Rect(0, 0, mainW, h), hover)
	}
	if jp.caret.Hovered() {
		theme.Fill(gtx, image.Rect(mainW, 0, mainW+caretW, h), hover)
	}
	st.Pop()
	theme.Fill(gtx, image.Rect(mainW-gtx.Dp(1), gtx.Dp(8), mainW, h-gtx.Dp(8)), color.NRGBA{R: 255, G: 255, B: 255, A: 0x66})
	lt := op.Offset(image.Pt(pad, (h-gtx.Sp(22))/2)).Push(gtx.Ops)
	th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: th.C.SendText, MaxLines: 1, LineHeight: 22}, "Join Channel")
	lt.Pop()
	is := gtx.Dp(16)
	it := op.Offset(image.Pt(mainW+(caretW-is)/2, (h-is)/2)).Push(gtx.Ops)
	icons.Get("chevron-down")(gtx, unit.Dp(16), th.C.SendText) // Slack draws a chevron, not a filled caret
	it.Pop()
	part(&jp.join, image.Rect(0, 0, mainW, h))
	part(&jp.caret, image.Rect(mainW, 0, mainW+caretW, h))
	return layout.Dimensions{Size: whole.Size()}, mainW
}

// openTempJoinMenu opens UY14's "Join…" menu above the caret, right-aligned:
// 300px wide, its right edge on the caret's (C08E083LZ9A, 2026-11-08: menu
// x 738–1038 over a caret at 998–1038).
func (a *App) openTempJoinMenu(c *convo) {
	items := []popups.MenuItem{
		{Header: "Join…"},
		{Label: "Just for today", Do: func() { a.joinPreview(c, core.TempToday) }},
		{Label: "For 48 hours", Do: func() { a.joinPreview(c, core.TempTwoDays) }},
		{Label: "For 1 week", Do: func() { a.joinPreview(c, core.TempWeek) }},
	}
	a.ShowOverlay(popups.NewMenuOpts(a, items, popups.MenuOpts{Width: 300, Anchor: c.jp.caretRect, Side: popups.Above, AlignRight: true}))
}

// joinPreview joins the previewed channel (d "" = permanently) and focuses
// the composer that replaces the pane, as joinAndFocusInput does.
func (a *App) joinPreview(c *convo, d core.TempJoin) {
	if a.data == nil || c.jp == nil || c.jp.joining {
		return
	}
	c.jp.joining = true
	svc, ch, name := a.data.core, c.ch, ""
	if ci := a.convOf(ch); ci != nil {
		name = ci.Name
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := svc.JoinFor(ctx, ch, d)
		a.Post(func() {
			c.jp.joining = false
			if err != nil {
				a.Toast("Couldn’t join #" + name + ": " + err.Error())
				return
			}
			if ci := a.convOf(ch); ci != nil {
				ci.Preview = false // the snapshot that follows agrees
			}
			if a.data != nil {
				delete(a.data.ext, ch)
			}
			if c.comp != nil {
				c.comp.Focus()
			}
			a.Invalidate()
		})
	}()
}

// ---- MessagePaneInputDisabled (archived) ----

func (a *App) layoutArchivedBar(gtx layout.Context, c *convo, ci *convInfo) layout.Dimensions {
	jp := c.jp
	th := a.th
	W := gtx.Constraints.Max.X
	if clicked(&jp.close) {
		a.closeArchived(c.ch)
	}
	pad := gtx.Dp(12)
	row := func(gtx layout.Context) layout.Dimensions {
		if c.ts != "" { // threads footer: an inline alert, centered
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.FgMuted, Align: text.Middle},
				"You are viewing a thread from an archived channel")
		}
		return centerRow(gtx, func(gtx layout.Context) layout.Dimensions {
			x := 0
			lh := gtx.Sp(22)
			piece := func(s string, w font.Weight) {
				t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
				d := th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: w, Color: th.C.FgMuted, MaxLines: 1}, s)
				t.Pop()
				x += d.Size.X
			}
			piece("You are viewing ", theme.Regular)
			piece("#"+ci.Name, theme.Bold)
			piece(", an archived channel", theme.Regular)
			x += gtx.Dp(12)
			bm := op.Record(gtx.Ops)
			bd := popups.Button(gtx, th, &jp.close, popups.BtnOutline, popups.BtnSmall, "Close Channel", "", 0)
			bc := bm.Stop()
			h := max(lh, bd.Size.Y)
			t := op.Offset(image.Pt(x, (h-bd.Size.Y)/2)).Push(gtx.Ops)
			bc.Add(gtx.Ops)
			t.Pop()
			return layout.Dimensions{Size: image.Pt(x+bd.Size.X, h)}
		})
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(W-2*pad, 1), gtx.Constraints.Max.Y)}
	m := op.Record(gtx.Ops)
	d := row(g)
	call := m.Stop()
	h := max(d.Size.Y+2*pad, gtx.Dp(56))
	box := image.Rect(0, 0, W, h)
	theme.RRect(gtx, box, gtx.Dp(8), th.C.BgInset)
	t := op.Offset(image.Pt(pad, (h-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: box.Size()}
}

// closeArchived is onCloseButtonClicked: leave the archived channel for the
// previous conversation.
func (a *App) closeArchived(ch string) {
	prev := a.router.Current()
	a.Back()
	if a.router.Current() == prev || a.router.Current().Channel() == ch {
		a.lastChannel = ""
		a.openDefault()
	}
}

// ---- the composer's roadblock (posting restrictions, app DMs) ----

// composerRoadblock sets the composer's MessageInputRoadblock message: a
// read-only app DM's notice, or MessageInputAnnounceOnlyRoadblock's when
// the viewer may not post here (top level: read-only or thread-only;
// thread: read-only). Taut's readOnlyChannels/threadOnlyChannels patches
// (RestrictedChannelWarning) decide when a plugin wraps them.
func (a *App) composerRoadblock(c *convo, comp composerView) {
	r, ok := comp.(interface{ SetRoadblock(string) })
	if !ok || comp == c.edit {
		return
	}
	link, _ := comp.(interface{ SetRoadblockLink(label, url string) })
	msg, more := "", false
	if c.ts == "" {
		if dm := a.appDM(c.ch); dm != nil && dm.ReadOnly() {
			msg = "Sending messages to this app has been turned off."
		}
	}
	if msg == "" {
		readOnly, threadOnly, noThreads := a.postingRestriction(c)
		if (c.ts == "" && (readOnly || threadOnly)) || (c.ts != "" && (readOnly || noThreads)) {
			msg, more = announceOnlyMessage, true
		}
	}
	r.SetRoadblock(msg)
	if link != nil {
		if more {
			link.SetRoadblockLink("Learn more", announceOnlyHelpURL)
		} else {
			link.SetRoadblockLink("", "")
		}
	}
}

// postingRestriction is what the viewer may not do in c's channel, cached
// for half a second (it parses the channel's properties).
func (a *App) postingRestriction(c *convo) (readOnly, threadOnly, noThreads bool) {
	if a.data == nil {
		return
	}
	if time.Since(c.postAt) < 500*time.Millisecond {
		return c.post.ReadOnly, c.post.ThreadOnly, c.post.NoThreads
	}
	p := a.data.core.PostingIn(c.ch)
	if t := a.data.taut; t != nil {
		p.ReadOnly, p.ThreadOnly = t.ChannelRestriction(c.ch)
	}
	c.post, c.postAt = p, time.Now()
	return p.ReadOnly, p.ThreadOnly, p.NoThreads
}

// previewBack is the channel-browser route a previewed channel was opened
// from ("" when it wasn't): Slack's previewedChannelPrevRoute.
func (a *App) previewBack(ch string) Route {
	if a.previewFromCh == ch {
		return a.previewFrom
	}
	return ""
}

// trackPreviewRoute records where a channel was opened from (afterRoute).
func (a *App) trackPreviewRoute(prev, r Route) {
	ch := r.Channel()
	switch {
	case ch != "" && prev.Page() == "directories":
		a.previewFromCh, a.previewFrom = ch, prev
	case ch != a.previewFromCh:
		a.previewFromCh, a.previewFrom = "", ""
	}
}
