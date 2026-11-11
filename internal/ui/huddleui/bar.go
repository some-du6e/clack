package huddleui

import (
	"image"
	"image/color"

	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// BarHeight is the room the sidebar leaves under its list for the huddle
// UI there: Slack's HuddleInvite card while an invite rings without its
// window (canRenderActiveInviteCardInSidebar). While you're in a huddle
// its controls are the docked bar under the workspace (dock.go), as in
// Slack's 2026 client, not the old sidebar footer.
func (c *Controller) BarHeight(gtx C) int {
	return c.cardHeight(gtx, c.h.Theme(), gtx.Constraints.Max.X)
}

// barMicRow is the docked bar's microphone warning row (dp).
const barMicRow = 30

type barState struct{}

// LayoutBar draws the sidebar's huddle card (see BarHeight).
func (c *Controller) LayoutBar(gtx C, th *theme.Theme) D {
	inv := c.cardInvite()
	if inv == nil {
		return D{}
	}
	return layoutCard(gtx, th, c, c.view(inv), &c.card, gtx.Constraints.Max.X, false)
}

var hudGreenLight = color.NRGBA{R: 0x5b, G: 0xe0, B: 0x9b, A: 0xff}

// ---- channel header ----

// The header's huddle button while a huddle is on, as Slack's
// HuddleChannelHeaderButton draws it (container --polish: 28px tall,
// radius 8, no border; a main button and a separate 20px caret):
//
//   - live, you're not in it (--active): jade-5 behind a facepile of up to
//     3 people (20px, radius 5, overlapped, no "+N"), the headphones and a
//     bare count ("2", 15px). The "Huddle" word only shows to people new to
//     huddles (isSelfUserNewToHuddles), so not here. Click: join.
//   - you're in it (--in_huddle): a jade-50 fill, a white headphones icon
//     that turns into an X on hover, no faces or count. Click: leave.
type headerState struct {
	click, caret  widget.Clickable
	tip, caretTip popups.TipState
	caretHit      bool // the caret was clicked (TakeCaret)
}

// TakeCaret reports (once) whether the live button's caret for ch was
// clicked; the header then opens MenuItems(ch) under it.
func (c *Controller) TakeCaret(ch string) bool {
	hs := c.hdr[ch]
	if hs == nil || !hs.caretHit {
		return false
	}
	hs.caretHit = false
	return true
}

// HeaderCaretX is the caret's x inside the live button (it's hdrCaret dp
// wide, the button's full height).
func (c *Controller) HeaderCaretX(gtx C, th *theme.Theme, ch string) int {
	return c.headerMainWidth(gtx, th, ch)
}

// HeaderLive reports whether channel's header should show the live
// huddle button (a huddle is on there, or you're in or joining it).
func (c *Controller) HeaderLive(ch string) bool {
	_, live := c.Live(ch)
	return live || c.In(ch)
}

// headerPeople is who's in ch's huddle (from the room, or your session).
func (c *Controller) headerPeople(ch string) []string {
	people := []string{}
	if r, ok := c.Live(ch); ok {
		people = r.Participants
	}
	if s := c.Session(); s != nil && s.Channel() == ch {
		st := s.State()
		people = people[:0:0]
		for _, p := range st.Participants {
			if !contains(people, p.UserID) {
				people = append(people, p.UserID)
			}
		}
	}
	return people
}

// Slack's jade palette for the button (light, dark).
var (
	hdrJade5    = [2]color.NRGBA{{R: 0xc3, G: 0xf6, B: 0xe0, A: 0xff}, {R: 0x06, G: 0x32, B: 0x25, A: 0xff}}
	hdrJade10   = [2]color.NRGBA{{R: 0xa5, G: 0xed, B: 0xce, A: 0xff}, {R: 0x08, G: 0x3f, B: 0x2e, A: 0xff}}
	hdrJade100  = [2]color.NRGBA{{R: 0x0a, G: 0x40, B: 0x32, A: 0xff}, {R: 0xd8, G: 0xf5, B: 0xe9, A: 0xff}}
	hdrJade50   = [2]color.NRGBA{{R: 0x20, G: 0xa2, B: 0x71, A: 0xff}, {R: 0x25, G: 0x9b, B: 0x69, A: 0xff}}
	hdrInvInk   = [2]color.NRGBA{{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, {R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}}
	hdrInvHover = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x2e} // surf-inv
)

func mode(th *theme.Theme) int {
	if th.Dark {
		return 1
	}
	return 0
}

const (
	hdrFace    = 20   // facepile avatar (x_small)
	hdrFaceGap = 18.5 // avatar step: margin-left -2.235% overlaps them by ~1.5px
	hdrCaret   = 20
	hdrIcon    = 20
)

// headerMainWidth is the main button's width (the caret follows).
func (c *Controller) headerMainWidth(gtx C, th *theme.Theme, ch string) int {
	if c.In(ch) {
		return gtx.Dp(4 + hdrIcon + 4)
	}
	people := c.headerPeople(ch)
	w := gtx.Dp(4) + gtx.Dp(hdrIcon) + gtx.Dp(4)
	if n := min(len(people), 3); n > 0 {
		w = gtx.Dp(2+8) + gtx.Dp(hdrFace) + (n-1)*gtx.Dp(hdrFaceGap) + gtx.Dp(8) + gtx.Dp(hdrIcon) + gtx.Dp(4)
		w += gtx.Dp(4) + labelSize(gtx, th, 15, theme.Regular, itoa(len(people))).X
	}
	return w
}

// HeaderPillWidth is the live button's width for ch.
func (c *Controller) HeaderPillWidth(gtx C, th *theme.Theme, ch string) int {
	return c.headerMainWidth(gtx, th, ch) + gtx.Dp(hdrCaret)
}

// headerTip is the main button's tooltip.
func (c *Controller) headerTip(ch string, in bool, people []string) string {
	if in {
		return nsHuddles.T("Leave huddle")
	}
	if contains(people, c.selfID()) {
		return nsHuddles.T("Joined on another device") + "\n" + nsHuddles.T("Join on this device") // LiveHuddleButton
	}
	if i18n.Locale() != i18n.EnUS {
		join := nsHuddles.T("Join huddle in {channelName}", "channelName", c.channelTitle(ch))
		if conv, ok := c.h.Core().S.Conv(ch); ok && conv.IsIM {
			if n := c.userName(conv.User); n != "" {
				join = nsHuddles.T("Join huddle with {channelDisplayName}", "channelDisplayName", n)
			}
		}
		return join + "\n" + nsHuddles.T("{memberCount, plural, =1 {# person} other {# people}}", "memberCount", len(people))
	}
	where := "in " + c.channelTitle(ch)
	if conv, ok := c.h.Core().S.Conv(ch); ok && conv.IsIM {
		if n := c.userName(conv.User); n != "" {
			where = "with " + n
		}
	}
	return "Join huddle " + where + "\n" + itoa(len(people)) + " in the huddle"
}

// StartTip is the idle Huddle button's tooltip (getStartHuddleTip): "Start
// huddle with {name}" in a DM, else "Start huddle in #channel", with
// addJoinLeaveKeyboardShortcuts' hint. sz is the button.
func (c *Controller) StartTip(gtx C, th *theme.Theme, ch string, hovered bool, sz image.Point) {
	hs := c.hdrState(ch)
	tip := nsHuddles.T("Start huddle")
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(ch); ok {
			switch {
			case conv.Kind == "im" || conv.Kind == "mpim":
				if names := c.dmNames(ch); names != "" {
					tip = nsHuddles.T("Start huddle with {dmPersons}", "dmPersons", names)
				}
			case conv.Name != "":
				tip = nsHuddles.T("Start huddle in {channelName}", "channelName", "#"+conv.Name)
			}
		}
	}
	popups.Tooltip(gtx, th, &hs.tip, hovered, sz, popups.Below, tip, leaveShortcut())
}

// CaretTip is the idle button's caret tooltip ("More Huddles options").
func (c *Controller) CaretTip(gtx C, th *theme.Theme, ch string, hovered bool, sz image.Point) {
	popups.Tooltip(gtx, th, &c.hdrState(ch).caretTip, hovered, sz, popups.Below, nsHuddles.T("More Huddles options"), "")
}

func (c *Controller) hdrState(ch string) *headerState {
	hs := c.hdr[ch]
	if hs == nil {
		hs = &headerState{}
		c.hdr[ch] = hs
	}
	return hs
}

// HeaderPill draws the live huddle button (see headerState). A click on
// the main part joins, or leaves when you're in it.
func (c *Controller) HeaderPill(gtx C, th *theme.Theme, ch string, h int) D {
	hs := c.hdr[ch]
	if hs == nil {
		hs = &headerState{}
		c.hdr[ch] = hs
	}
	if hs.click.Clicked(gtx) {
		c.Toggle(ch)
	}
	if hs.caret.Clicked(gtx) {
		hs.caretHit = true
	}
	in := c.In(ch)
	people := c.headerPeople(ch)
	m := mode(th)
	mw := c.headerMainWidth(gtx, th, ch)
	w := mw + gtx.Dp(hdrCaret)
	rad := gtx.Dp(8)

	// the container: radius 8, overflow hidden
	cl := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, rad).Push(gtx.Ops)
	bg, ink, hov := hdrJade5[m], hdrJade100[m], hdrJade10[m]
	if in {
		bg, ink = hdrJade50[m], hdrInvInk[m]
	}
	theme.Fill(gtx, image.Rectangle{Max: image.Pt(w, h)}, bg)
	hover := func(gtx C, sz image.Point, on bool) {
		if !on {
			return
		}
		if in {
			theme.Fill(gtx, image.Rectangle{Max: sz}, hdrInvHover)
		} else {
			theme.Fill(gtx, image.Rectangle{Max: sz}, hov)
		}
	}

	button(gtx, &hs.click, image.Pt(mw, h), func(gtx C, on bool) { hover(gtx, image.Pt(mw, h), on) }, func(gtx C) D {
		ic := gtx.Dp(hdrIcon)
		if in {
			name := "headphones"
			if hs.click.Hovered() {
				name = "close"
			}
			at(gtx, image.Pt((mw-ic)/2, (h-ic)/2), image.Pt(ic, ic), func(gtx C) D { return icons.Get(name)(gtx, hdrIcon, ink) })
			return D{}
		}
		x := gtx.Dp(4)
		if n := min(len(people), 3); n > 0 {
			x = gtx.Dp(2 + 8)
			fs := gtx.Dp(hdrFace)
			under := bg
			if hs.click.Hovered() {
				under = hov
			}
			for i, u := range people[:n] {
				u := u
				at(gtx, image.Pt(x+i*gtx.Dp(hdrFaceGap), (h-fs)/2), image.Pt(fs, fs), func(gtx C) D {
					if i > 0 { // the crescent mask's gap where it overlaps the one before
						theme.RRect(gtx, image.Rect(-gtx.Dp(1.5), -gtx.Dp(1.5), fs, fs+gtx.Dp(1.5)), gtx.Dp(6), under)
					}
					avatar(gtx, th, c.h.Images(), c.avatarURL(u, fs), image.Pt(fs, fs), gtx.Dp(5))
					return D{Size: image.Pt(fs, fs)}
				})
			}
			x += fs + (n-1)*gtx.Dp(hdrFaceGap) + gtx.Dp(8)
		}
		at(gtx, image.Pt(x, (h-ic)/2), image.Pt(ic, ic), func(gtx C) D { return icons.Get("headphones")(gtx, hdrIcon, ink) })
		x += ic
		if len(people) > 0 {
			txt := itoa(len(people))
			ls := labelSize(gtx, th, 15, theme.Regular, txt)
			x += gtx.Dp(4)
			at(gtx, image.Pt(x, (h-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, theme.Regular, ink, txt) })
		}
		return D{}
	})
	popups.Tooltip(gtx, th, &hs.tip, hs.click.Hovered(), image.Pt(mw, h), popups.Below, c.headerTip(ch, in, people), leaveShortcut())

	cw := gtx.Dp(hdrCaret)
	at(gtx, image.Pt(mw, 0), image.Pt(cw, h), func(gtx C) D {
		d := button(gtx, &hs.caret, image.Pt(cw, h), func(gtx C, on bool) { hover(gtx, image.Pt(cw, h), on) }, func(gtx C) D {
			at(gtx, image.Pt(gtx.Dp(1), (h-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx C) D {
				return icons.Get("chevron-down")(gtx, 14, ink)
			})
			return D{}
		})
		popups.Tooltip(gtx, th, &hs.caretTip, hs.caret.Hovered(), image.Pt(cw, h), popups.Below, nsHuddles.T("More Huddles options"), "")
		return d
	})
	cl.Pop()
	return D{Size: image.Pt(w, h)}
}

// Toggle is the header button: join the huddle in ch, or leave it when
// you're in it.
func (c *Controller) Toggle(ch string) {
	if c.In(ch) {
		c.Leave()
		return
	}
	c.Begin(ch, EntryHeaderButton)
}
