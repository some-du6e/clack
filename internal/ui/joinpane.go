package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
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
//     lines), then [Details] [Join Channel]. For a public channel that
//     isn't shared or #general (OtvL Y, core.CanTempJoin) the Join button
//     is a split button whose caret ("Join channel temporarily") opens
//     "Join…": Just for today / For 48 hours / For 1 week
//     (conversations.join expiration_ts); temp_join_channel no longer
//     gates it (build 274f7c7f1b69). Opened from the channel browser, a
//     "Back to All channels" link follows. With the experiment on, the
//     revamped card with the channel's people and agents (joincard.go).
//     The strings are the archives namespace's, as in UY14 and cRQG.
//   - an archived channel: MessagePaneInputDisabled (boot-render) with
//     "You are viewing #name, an archived channel" and a small outline
//     "Close Channel" button that goes back to the previous conversation.
//     In a thread: "You are viewing a thread from an archived channel".
//     Under unarchive_channel_banner_polish, for a viewer who may
//     unarchive it (canUserUnarchiveChannel: UNARCHIVE_CHANNEL), it reads
//     "You are viewing #name, an archived channel. Unarchive it to
//     continue the discussion." with "Unarchive" a link
//     (submitUnarchive: conversations.unarchive), and no Close button.
//   - a posting-restricted channel (core/posting.go): the composer's own
//     MessageInputRoadblock with MessageInputAnnounceOnlyRoadblock's
//     message, so Taut's RestrictedChannelWarning still patches it
//     (composerRoadblock below).
//
// Not ported: the code-channel summary, Slack Connect's connection info
// line, the offline tooltip ("Go online to join this channel"), the
// suggested-channel prefix and its "Not Now".

// nsArchives is JoinChannelPane's and MessagePaneInputPreview's I18n
// namespace.
var nsArchives = i18n.NS("archives")

type footerKind uint8

const (
	footNone footerKind = iota
	footPreview
	footArchived
)

// joinPane is one conversation's footer state.
type joinPane struct {
	details, join, caret, back, close widget.Clickable
	unarchive                         widget.Clickable // the polished footer's "Unarchive" link
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
		if a.threadLockedCopy(c) != "" {
			return footNone // ThreadFooter: the locked roadblock comes before nonMember's join
		}
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
	temp := a.joinPaneActions(c)
	if svc := a.Core(); svc != nil && svc.ChannelJoinWithAgents() {
		return a.layoutJoinCard(gtx, c, ci, org, temp)
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
		dd := popups.Button(gtx, th, &jp.details, popups.BtnOutline, popups.BtnMedium, nsArchives.T("Details"), "", 0)
		cDet := mDet.Stop()
		mJoin := op.Record(gtx.Ops)
		var dj layout.Dimensions
		var caretX int
		if temp {
			dj, caretX = a.splitJoinButton(gtx, jp)
		} else {
			dj = popups.Button(gtx, th, &jp.join, popups.BtnPrimary, popups.BtnMedium, nsArchives.T("Join Channel"), "", 0)
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
		add(func(gtx layout.Context) layout.Dimensions { return a.previewBackLink(gtx, jp, true) })
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
	a.caretTooltip(gtx, jp, org, temp)
	return layout.Dimensions{Size: image.Pt(W, h)}
}

// joinPaneActions handles the pane's clicks (read before drawing, Gio
// style) and says whether Join is the split button (OtvL Y).
func (a *App) joinPaneActions(c *convo) (temp bool) {
	jp := c.jp
	temp = a.data != nil && a.data.core.CanTempJoin(c.ch)
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
	return temp
}

// caretTooltip is the caret's "Join channel temporarily" tooltip; org is
// the pane's window origin (caretRect is in window coordinates).
func (a *App) caretTooltip(gtx layout.Context, jp *joinPane, org image.Point, temp bool) {
	if jp.caret.Hovered() && temp {
		r := jp.caretRect
		t := op.Offset(r.Min.Sub(org)).Push(gtx.Ops)
		popups.Tooltip(gtx, a.th, &jp.caretTip, true, r.Size(), popups.Above, nsArchives.T("Join channel temporarily"), "")
		t.Pop()
	} else {
		popups.Tooltip(gtx, a.th, &jp.caretTip, false, image.Point{}, popups.Above, "", "")
	}
}

// previewBackLink is MessagePaneInputPreview's "Back to All channels",
// centered in the classic pane, at the start in the revamped card.
func (a *App) previewBackLink(gtx layout.Context, jp *joinPane, center bool) layout.Dimensions {
	th := a.th
	label := nsArchives.T("Back to All channels")
	top := gtx.Dp(12)
	x := 0
	if center {
		x = max((gtx.Constraints.Max.X-th.Measure(gtx, 15, theme.Regular, label))/2, 0)
	}
	t := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
	d := clickArea(gtx, &jp.back, nil, func(gtx layout.Context) layout.Dimensions {
		col := th.C.Link
		if jp.back.Hovered() {
			col = theme.Mix(col, th.C.Fg, .2)
		}
		return th.Label(gtx, 15, theme.Regular, col, label)
	})
	t.Pop()
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, top+d.Size.Y)}
}

// joinTitle draws the subtitle: the bold channel name with its icon (18px
// black), or rt("To reply to this thread, join <title>channel name</title>")
// at 15px with the channel in the tag's place.
func (a *App) joinTitle(gtx layout.Context, c *convo, ci *convInfo) layout.Widget {
	if c.ts == "" {
		return func(gtx layout.Context) layout.Dimensions {
			return a.joinHeadline(gtx, []i18n.Part{{Arg: "channel"}}, ci, 18, theme.Black, 26, "channel")
		}
	}
	parts := nsArchives.RT("To reply to this thread, join <title>channel name</title>")
	for i := range parts {
		if parts[i].Tag == "title" {
			parts[i].Arg = "title"
		}
	}
	return func(gtx layout.Context) layout.Dimensions {
		return a.joinHeadline(gtx, parts, ci, 15, theme.Regular, 22, "title")
	}
}

// joinHeadline draws parts on one line at size: plain runs in weight, and
// the part whose Arg is chanArg as the channel entity (ChannelEntity with
// boldIcon: the hash or lock glyph at the text's size, 2px, the bold name;
// black at 18px), ellipsized to the space left.
func (a *App) joinHeadline(gtx layout.Context, parts []i18n.Part, ci *convInfo, size unit.Sp, weight font.Weight, lineH int, chanArg string) layout.Dimensions {
	th := a.th
	icon := "hash"
	if ci.Kind == "private" {
		icon = "lock"
	}
	nameW := theme.Bold
	if size >= 18 {
		nameW = theme.Black
	}
	lh := gtx.Sp(unit.Sp(lineH))
	maxW := gtx.Constraints.Max.X
	// the plain runs' width first: the channel name gets what's left
	fixedW := 0
	for _, p := range parts {
		if p.Arg != chanArg {
			fixedW += th.Measure(gtx, size, weight, p.Text)
		}
	}
	is := gtx.Sp(size)
	x := 0
	for _, p := range parts {
		if p.Arg != chanArg {
			t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			d := th.Layout(gtx, theme.TextStyle{Size: size, Weight: weight, Color: th.C.Fg, MaxLines: 1, LineHeight: unit.Sp(lineH)}, p.Text)
			t.Pop()
			x += d.Size.X
			continue
		}
		t := op.Offset(image.Pt(x, (lh-is)/2)).Push(gtx.Ops)
		icons.Get(icon)(gtx, unit.Dp(float32(size)), th.C.Fg)
		t.Pop()
		x += is + gtx.Dp(2)
		g := gtx
		g.Constraints.Min = image.Point{}
		g.Constraints.Max.X = max(maxW-fixedW-is-gtx.Dp(2), 1)
		t = op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		d := th.Layout(g, theme.TextStyle{Size: size, Weight: nameW, Color: th.C.Fg, MaxLines: 1, LineHeight: unit.Sp(lineH)}, ci.Name)
		t.Pop()
		x += d.Size.X
	}
	return layout.Dimensions{Size: image.Pt(min(x, maxW), lh)}
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
	label := nsArchives.T("Join Channel")
	tw := th.Measure(gtx, 15, theme.Bold, label)
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
	th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: th.C.SendText, MaxLines: 1, LineHeight: 22}, label)
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
		{Header: nsArchives.T("Join\u2026")},
		{Label: nsArchives.T("Just for today"), Do: func() { a.joinPreview(c, core.TempToday) }},
		{Label: nsArchives.T("For 48 hours"), Do: func() { a.joinPreview(c, core.TempTwoDays) }},
		{Label: nsArchives.T("For 1 week"), Do: func() { a.joinPreview(c, core.TempWeek) }},
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
				a.data.forgetExt(ch)
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
	// renderArchivedChannelFooterText's polished variant; the link's
	// clicks are read before it is laid out (Layout drains them), and
	// every frame, so a click from before it showed doesn't linger
	polish := false
	unarchive := jp.unarchive.Clicked(gtx)
	if a.data != nil && a.data.core != nil {
		svc := a.data.core
		polish = svc.Experiment("unarchive_channel_banner_polish") == "on" && svc.CanUnarchive(c.ch)
		if polish && unarchive {
			ch := c.ch
			go func() { // onUnarchiveLinkClicked: submitUnarchive
				if err := svc.Unarchive(context.Background(), ch); err != nil {
					log.Printf("unarchive %s: %v", ch, err)
				}
			}()
		}
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
			if polish {
				piece(", an archived channel. ", theme.Regular)
				t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
				d := jp.unarchive.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					pointer.CursorPointer.Add(gtx.Ops)
					return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Link, MaxLines: 1}, "Unarchive")
				})
				t.Pop()
				x += d.Size.X
				piece(" it to continue the discussion.", theme.Regular)
				return layout.Dimensions{Size: image.Pt(x, lh)} // showCloseButton: false
			}
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
// locked thread's (threadLockedCopy), a read-only app DM's notice, or
// MessageInputAnnounceOnlyRoadblock's when the viewer may not post here
// (top level: read-only or thread-only; thread: read-only). Taut's readOnlyChannels/threadOnlyChannels patches
// (RestrictedChannelWarning) decide when a plugin wraps them.
func (a *App) composerRoadblock(c *convo, comp composerView) {
	r, ok := comp.(interface{ SetRoadblock(string) })
	if !ok || comp == c.edit {
		return
	}
	link, _ := comp.(interface{ SetRoadblockLink(label, url string) })
	msg, more := a.threadLockedCopy(c), false
	if msg == "" && c.ts == "" {
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

// nsLockedThreads is useThreadLockedCopy's I18n namespace (boot-shared
// `352G`).
var nsLockedThreads = i18n.NS("locked_threads")

// threadLockedCopy is MessageInputLockedThreadRoadblock's message for c
// ("" unless c is a thread whose root is locked): ThreadFooter (`352G`)
// draws it instead of the reply box when isThreadLocked (client-boot
// `rZb1`, core.ThreadLocked), after the archived channel's alert and
// before the other roadblocks and the join pane. useThreadLockedCopy
// says reactions are off too under feat_flag_message_hide_replies.
func (a *App) threadLockedCopy(c *convo) string {
	if c.ts == "" || a.data == nil || a.data.core == nil || !a.data.core.ThreadLocked(c.ch, c.ts) {
		return ""
	}
	if a.data.core.HideRepliesOn() {
		return nsLockedThreads.T("Replies and reactions have been turned off for this thread.")
	}
	return nsLockedThreads.T("Replies have been turned off for this thread.")
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
