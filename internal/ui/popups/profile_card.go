package popups

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, profile hover card): 340 wide,
// radius 8, popover shadow (ring 1px rgba(29,28,29,.13) + 0 4px 12px
// rgba(0,0,0,.12)). Primary block padding 16: avatar 72 (radius 12), 12px
// gap, name 15/900 followed by the presence indicator, pronouns and title
// 15px #454447 (title wraps), status emoji + text. Secondary block: border
// top 1px rgba(29,28,29,.13), padding 0 16 16; clock icon 20 + "12:39 PM
// local time" 15px (margin-top 16); buttons row (margin-top 12): [Message]
// [Huddle ▾] [VIP] ⋮, outline, 28 tall, radius 4, 13/700, 16px icons.

// OpenFullProfile opens userID's full profile in the secondary pane. The ui
// root sets it (popups cannot reach the pane itself); while it is nil the
// card and its menu toast instead.
var OpenFullProfile func(userID string)

// ProfileCard is the profile hover/click card.
type ProfileCard struct {
	base
	h      Host
	id     string
	anchor image.Point
	scrim  scrim
	block  blocker
	rect   image.Rectangle
	focus  bool

	asked   bool // EnsureUsers started
	missing bool // EnsureUsers finished without the user

	avatarBtn, nameBtn                 widget.Clickable
	msgBtn, huddleBtn, vipBtn, moreBtn widget.Clickable
	moreLocal                          image.Rectangle // kebab rect, card coords
}

// NewProfileCard opens the card with its top-left corner at anchor (window
// coords), flipping/clamping at the window edges.
func NewProfileCard(h Host, anchor image.Point, userID string) *ProfileCard {
	p := &ProfileCard{h: h, id: userID, anchor: anchor}
	pfSubscribe(h, userID)
	return p
}

// Layout draws the card.
func (p *ProfileCard) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.done {
		return layout.Dimensions{}
	}
	stacked(gtx, p)
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	if !p.focus {
		gtx.Execute(key.FocusCmd{Tag: p})
		p.focus = true
	}
	if isTop(p) && escape(gtx, nil) {
		p.Dismiss()
		return layout.Dimensions{}
	}
	if p.scrim.Layout(gtx) {
		p.Dismiss()
		return layout.Dimensions{}
	}
	u, ok := lookupUser(p.h, p.id)
	if !ok && !p.asked && p.h.Core() != nil && p.id != "" {
		p.asked = true
		pfEnsure(p.h, p.id, func(found bool) { p.missing = !found })
	}
	if ok {
		p.clicks(gtx, u)
		if p.done {
			return layout.Dimensions{}
		}
		pfTick(gtx)
	}

	w := dp(gtx, 340)
	if mw := win.X - 2*dp(gtx, 8); w > mw {
		w = mw
	}
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		return p.content(gtx, th, cl, u, ok, w)
	})
	pos := place(pointRect(p.anchor), d.Size, win, dp(gtx, 8), Below)
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	p.block.Layout(gtx, p.rect)
	keyTarget(gtx, p, p.rect)
	cs := clipRRect(gtx, p.rect, rad)
	macroAt(gtx, call, pos)
	cs.Pop()
	return layout.Dimensions{Size: win}
}

func (p *ProfileCard) clicks(gtx layout.Context, u core.User) {
	h := p.h
	if p.avatarBtn.Clicked(gtx) || p.nameBtn.Clicked(gtx) {
		p.Dismiss()
		pfOpenFull(h, u.ID)
		return
	}
	if p.msgBtn.Clicked(gtx) {
		p.Dismiss()
		if pfIsSelf(h, u.ID) {
			pfSetStatus(h)
		} else {
			pfMessage(h, u.ID)
		}
		return
	}
	if p.huddleBtn.Clicked(gtx) {
		h.Toast("Huddles aren't supported in yoloslack yet")
	}
	if p.vipBtn.Clicked(gtx) {
		h.Toast("VIPs aren't supported yet")
	}
	if p.moreBtn.Clicked(gtx) {
		items := pfMoreItems(h, u, true, p.Dismiss)
		h.ShowOverlay(NewMenuOpts(h, items, MenuOpts{Anchor: p.moreLocal.Add(p.rect.Min), Side: Below}))
	}
}

// content draws the card body at the origin (card coords).
func (p *ProfileCard) content(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, ok bool, w int) layout.Dimensions {
	pad := dp(gtx, 16)
	av := dp(gtx, 72)
	colX := pad + av + dp(gtx, 12)
	colW := w - colX - pad
	if colW < 1 {
		colW = 1
	}
	// primary block
	if ok {
		at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.avatarBtn, nil, func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, p.h, pfAvatarURL(u, false), 72)
			})
		})
	} else {
		rrect(gtx, image.Rect(pad, pad, pad+av, pad+av), dp(gtx, 12), cl.Selected)
	}
	cd := at(gtx, image.Pt(colX, pad), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
		if !ok {
			return pfSkeleton(gtx, th, cl, p.missing, p.id, colW)
		}
		return p.column(gtx, th, cl, u, colW)
	})
	y := pad + maxInt(av, cd.Size.Y) + pad

	// secondary block
	fill(gtx, image.Rect(0, y, w, y+1), cl.Ring)
	y++
	if ok && pfHasClock(u) {
		y += dp(gtx, 16)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-2*pad, 1<<20)}
			return pfIconLine(gtx, th, cl, "clock", localTime(u, gtx.Now)+" local time", 15, 22)
		})
		y += d.Size.Y
	}
	y += dp(gtx, 12)
	if ok {
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return p.buttons(gtx, th, u, w-2*pad)
		})
		p.moreLocal = p.moreLocal.Add(image.Pt(pad, y))
		y += d.Size.Y
	} else {
		rrect(gtx, image.Rect(pad, y, pad+dp(gtx, 120), y+dp(gtx, 28)), dp(gtx, 4), cl.Selected)
		y += dp(gtx, 28)
	}
	y += pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// column is the text column beside the avatar.
func (p *ProfileCard) column(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, colW int) layout.Dimensions {
	lineH := dp(gtx, 22)
	y := 0
	// name + presence
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return pfNameLine(gtx, th, &p.nameBtn, u, 15, 22, 10, colW)
	})
	y += lineH
	if !pfBlank(u.Pronouns) {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, lineH)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, u.Pronouns, 1)
		})
		y += lineH
	}
	if !pfBlank(u.Title) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, u.Title, 0)
		})
		y += maxInt(d.Size.Y, lineH)
	}
	if pfHasStatus(u, gtx.Now) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
			return pfStatusLine(gtx, p.h, th, cl, u, 15, 22, 18)
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(colW, maxInt(y, lineH))}
}

func (p *ProfileCard) buttons(gtx layout.Context, th *theme.Theme, u core.User, w int) layout.Dimensions {
	var specs []pfBtnSpec
	if pfIsSelf(p.h, u.ID) {
		specs = []pfBtnSpec{{c: &p.msgBtn, label: "Set a status", lead: "emoji"}}
	} else {
		specs = []pfBtnSpec{
			{c: &p.msgBtn, label: "Message", lead: "threads"},
			{c: &p.huddleBtn, label: "Huddle", lead: "headphones", caret: true},
			{c: &p.vipBtn, label: "VIP", lead: "user-plus"},
		}
	}
	kebab := &p.moreBtn
	if len(specs) > 1 {
		kebab = nil // Slack's card shows only [Message] [Huddle ▾] [VIP]
	}
	d, kr := pfBtnRow(gtx, th, BtnSmall, specs, kebab, w, false)
	p.moreLocal = kr
	return d
}

// ---- shared profile helpers (card + full profile) ----

// pfBlankCut is spaces plus zero-width characters (ZWSP, ZWNJ, ZWJ, word
// joiner, BOM).
const pfBlankCut = " \t\n\u200b\u200c\u200d\u2060\ufeff"

// pfBlank reports whether s is empty once spaces and zero-width characters
// are trimmed (Hack Club users may have a display name of a lone zero-width space).
func pfBlank(s string) bool {
	return strings.Trim(s, pfBlankCut) == ""
}

// pfName is the name Slack shows for u, skipping blank display names.
func pfName(u core.User) string {
	switch {
	case !pfBlank(u.Name):
		return u.Name
	case !pfBlank(u.RealName):
		return u.RealName
	case !pfBlank(u.Handle):
		return u.Handle
	}
	return u.ID
}

func pfAvatarURL(u core.User, big bool) string {
	if big && u.Avatar512 != "" {
		return u.Avatar512
	}
	if u.Avatar192 != "" {
		return u.Avatar192
	}
	if u.Avatar512 != "" {
		return u.Avatar512
	}
	return u.Avatar
}

func pfIsSelf(h Host, id string) bool {
	return h.Core() != nil && id != "" && h.Core().Self().ID == id
}

// pfHasClock reports whether u has a meaningful local time.
func pfHasClock(u core.User) bool {
	return u.TZ != "" || u.TZOffset != 0 || !u.IsBot
}

func pfHasStatus(u core.User, now time.Time) bool {
	if pfBlank(u.StatusText) && u.StatusEmoji == "" && u.StatusEmojiURL == "" {
		return false
	}
	return u.StatusExp <= 0 || now.Unix() < u.StatusExp
}

// pfTick repaints at the next minute so local times stay current.
func pfTick(gtx layout.Context) {
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Truncate(time.Minute).Add(time.Minute)})
}

// pfSubscribe asks core for realtime presence of id (off the UI goroutine;
// it may write to the websocket).
func pfSubscribe(h Host, id string) {
	c := h.Core()
	if c == nil || id == "" {
		return
	}
	run(h, func(context.Context) (struct{}, error) {
		c.SubscribePresence([]string{id})
		return struct{}{}, nil
	}, nil)
}

// pfEnsure fetches an unknown user and reports whether it arrived.
func pfEnsure(h Host, id string, done func(found bool)) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) {
		c.EnsureUsers(ctx, []string{id})
		return struct{}{}, nil
	}, func(struct{}, error) {
		_, ok := lookupUser(h, id)
		if done != nil {
			done(ok)
		}
		h.Invalidate()
	})
}

// pfOpenFull opens the full profile pane (or toasts while unwired).
func pfOpenFull(h Host, id string) {
	if OpenFullProfile != nil {
		OpenFullProfile(id)
		return
	}
	h.Toast("Full profiles aren't available yet")
}

// pfMessage opens (or creates) the DM with id and navigates to it.
func pfMessage(h Host, id string) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) (core.Conv, error) {
		return c.OpenDM(ctx, id)
	}, func(cv core.Conv, err error) {
		if err != nil {
			h.Toast("Couldn't open the conversation: " + errText(err))
			return
		}
		if cv.ID != "" {
			h.Navigate("c/" + cv.ID)
		}
	})
}

// pfSetStatus is the self "Set a status" action.
func pfSetStatus(h Host) {
	h.ShowOverlay(NewSetStatusModal(h))
}

// pfMoreItems is the ⋮ menu of the card (withFull adds "View full
// profile") and of the full profile.
func pfMoreItems(h Host, u core.User, withFull bool, closeCard func()) []MenuItem {
	id := u.ID
	name := pfName(u)
	var items []MenuItem
	if withFull {
		items = append(items, MenuItem{Label: "View full profile", Do: func() {
			if closeCard != nil {
				closeCard()
			}
			pfOpenFull(h, id)
		}}, Sep())
	}
	items = append(items,
		MenuItem{Label: "Copy display name", DoCtx: func(gtx layout.Context) {
			copyText(gtx, name)
			h.Toast("Display name copied")
		}},
		MenuItem{Label: "Copy member ID", DoCtx: func(gtx layout.Context) {
			copyText(gtx, id)
			h.Toast("Member ID copied")
		}},
		MenuItem{Label: "Copy link to profile", DoCtx: func(gtx layout.Context) {
			copyText(gtx, "https://"+teamDomain(h)+".slack.com/team/"+id)
			h.Toast("Link copied")
		}},
	)
	return items
}

// pfImage draws url as a size-dp square with corner radius rad dp.
func pfImage(gtx layout.Context, h Host, url string, size, rad float32) layout.Dimensions {
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	r := dp(gtx, rad)
	if url != "" {
		if img, ok := h.Images().Get(url, px); ok {
			return theme.Image(gtx, img, sz, r)
		}
	}
	rrect(gtx, image.Rectangle{Max: sz}, r, colorsOf(h.Theme()).Selected)
	return layout.Dimensions{Size: sz}
}

// pfNameLine draws the bold name (clickable via c) and the presence
// indicator after it, within maxW.
func pfNameLine(gtx layout.Context, th *theme.Theme, c *widget.Clickable, u core.User, size, lh, dot float32, maxW int) layout.Dimensions {
	cl := colorsOf(th)
	lineH := dp(gtx, lh)
	name := pfName(u)
	hasDot := !u.IsBot // unknown presence draws the hollow ring, like Slack
	reserve := 0
	if hasDot {
		reserve = dp(gtx, dot) + dp(gtx, 6)
	}
	nameMax := maxInt(1, maxW-reserve)
	var nd layout.Dimensions
	if c != nil {
		nd = clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
			return para(gtx, th, size, lh, theme.Black, cl.Fg, name, 1)
		})
	} else {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
		nd = para(g, th, size, lh, theme.Black, cl.Fg, name, 1)
	}
	x := nd.Size.X
	if hasDot {
		if x > 0 {
			x += dp(gtx, 6)
		}
		at(gtx, image.Pt(x, (lineH-dp(gtx, dot))/2), func(gtx layout.Context) layout.Dimensions {
			return presenceDot(gtx, th, u.Presence == "active", dot)
		})
		x += dp(gtx, dot)
	}
	return layout.Dimensions{Size: image.Pt(x, lineH)}
}

// pfStatusEmoji draws u's status emoji (custom URL first) at size dp.
func pfStatusEmoji(gtx layout.Context, h Host, u core.User, size float32) layout.Dimensions {
	if u.StatusEmojiURL != "" {
		px := dp(gtx, size)
		sz := image.Pt(px, px)
		if img, ok := h.Images().Get(u.StatusEmojiURL, px); ok {
			return containImage(gtx, img, sz)
		}
		return layout.Dimensions{Size: sz}
	}
	return emojiImage(gtx, h, u.StatusEmoji, size)
}

// pfStatusLine draws status emoji + wrapping status text.
func pfStatusLine(gtx layout.Context, h Host, th *theme.Theme, cl *Colors, u core.User, size, lh, emoji float32) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	lineH := dp(gtx, lh)
	x := 0
	if u.StatusEmoji != "" || u.StatusEmojiURL != "" {
		at(gtx, image.Pt(0, (lineH-dp(gtx, emoji))/2), func(gtx layout.Context) layout.Dimensions {
			return pfStatusEmoji(gtx, h, u, emoji)
		})
		x = dp(gtx, emoji) + dp(gtx, 6)
	}
	hgt := lineH
	if !pfBlank(u.StatusText) {
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), 1<<20)}
			return para(gtx, th, size, lh, theme.Regular, cl.Fg, u.StatusText, 0)
		})
		hgt = maxInt(hgt, d.Size.Y)
	}
	return layout.Dimensions{Size: image.Pt(maxW, hgt)}
}

// pfIconLine draws a 20px icon (#454447) and text 8px after it.
func pfIconLine(gtx layout.Context, th *theme.Theme, cl *Colors, name, s string, size, lh float32) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	lineH := dp(gtx, lh)
	at(gtx, image.Pt(0, (lineH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, name, 20, cl.Fg2)
	})
	x := dp(gtx, 28)
	d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), lineH)}
		return para(gtx, th, size, lh, theme.Regular, cl.Fg, s, 1)
	})
	return layout.Dimensions{Size: image.Pt(x+d.Size.X, lineH)}
}

// pfSkeleton is the loading state of the text column.
func pfSkeleton(gtx layout.Context, th *theme.Theme, cl *Colors, missing bool, id string, w int) layout.Dimensions {
	if missing {
		para(gtx, th, 15, 22, theme.Black, cl.Fg, "Unknown user", 1)
		at(gtx, image.Pt(0, dp(gtx, 22)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 22))}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, id, 1)
		})
		return layout.Dimensions{Size: image.Pt(w, dp(gtx, 44))}
	}
	r := dp(gtx, 4)
	rrect(gtx, image.Rect(0, dp(gtx, 4), w*3/5, dp(gtx, 18)), r, cl.Selected)
	rrect(gtx, image.Rect(0, dp(gtx, 30), w*9/10, dp(gtx, 42)), r, cl.Selected)
	rrect(gtx, image.Rect(0, dp(gtx, 50), w*2/3, dp(gtx, 62)), r, cl.Selected)
	return layout.Dimensions{Size: image.Pt(w, dp(gtx, 72))}
}

// ---- buttons with a trailing caret, laid out in rows ----

// pfBtnSpec is one labeled button of a row.
type pfBtnSpec struct {
	c     *widget.Clickable
	label string
	lead  string // icon before the label
	caret bool   // chevron-down after the label
}

type pfBtnMetrics struct {
	h, fs, rad, isz, csz, pad float32
}

func pfMetrics(size BtnSize) pfBtnMetrics {
	if size == BtnMedium {
		return pfBtnMetrics{h: 36, fs: 15, rad: 8, isz: 18, csz: 14, pad: 12}
	}
	return pfBtnMetrics{h: 28, fs: 13, rad: 4, isz: 16, csz: 12, pad: 8}
}

// pfBtnParts returns content width (icon + label) and the caret reserve.
func pfBtnParts(gtx layout.Context, th *theme.Theme, m pfBtnMetrics, s pfBtnSpec) (iw, tw, cw int) {
	tw = textW(gtx, th, m.fs, theme.Bold, s.label)
	if s.lead != "" {
		iw = dp(gtx, m.isz) + dp(gtx, 4)
	}
	if s.caret {
		cw = dp(gtx, 6) + dp(gtx, m.csz)
	}
	return iw, tw, cw
}

func pfBtnNatural(gtx layout.Context, th *theme.Theme, m pfBtnMetrics, s pfBtnSpec) int {
	iw, tw, cw := pfBtnParts(gtx, th, m, s)
	return iw + tw + cw + 2*dp(gtx, m.pad)
}

// pfBtn draws an outline button of width w px: icon + label centered (in
// the space left of the caret when the button is at natural width), caret
// at the right edge.
func pfBtn(gtx layout.Context, th *theme.Theme, size BtnSize, s pfBtnSpec, w int) layout.Dimensions {
	cl := colorsOf(th)
	m := pfMetrics(size)
	iw, tw, cw := pfBtnParts(gtx, th, m, s)
	pad := dp(gtx, m.pad)
	hh := dp(gtx, m.h)
	sz := image.Pt(w, hh)
	return s.c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		rad := dp(gtx, m.rad)
		bg := cl.BtnBg
		if s.c.Hovered() {
			bg = cl.BtnHover
		}
		rrect(gtx, r, rad, cl.BtnBorder)
		rrect(gtx, r.Inset(1), maxInt(0, rad-1), bg)
		nat := iw + tw + cw + 2*pad
		x := pad + (w-nat)/2 // natural: icon, label and caret centered as a group
		cx := x + iw + tw + dp(gtx, 6)
		if s.caret && w > nat {
			// stretched: label centered on the button, caret at the right
			x = (w - iw - tw) / 2
			cx = w - pad - dp(gtx, m.csz)
		}
		if x < pad {
			x = pad
		}
		if s.lead != "" {
			at(gtx, image.Pt(x, (hh-dp(gtx, m.isz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, s.lead, m.isz, cl.Fg)
			})
			x += iw
		}
		lw := minInt(tw, maxInt(1, w-x-pad-cw))
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(exact(gtx, image.Pt(lw, hh)), hh, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return text(gtx, th, m.fs, theme.Bold, cl.Fg, s.label)
			})
		})
		if s.caret {
			at(gtx, image.Pt(cx, (hh-dp(gtx, m.csz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", m.csz, cl.Fg)
			})
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// pfBtnRow lays out specs then a square kebab button across width w (gap
// 8). equal gives every labeled button the same width (full profile);
// otherwise natural widths share the leftover space (card). It returns the
// row size and the kebab's rect in row coords.
func pfBtnRow(gtx layout.Context, th *theme.Theme, size BtnSize, specs []pfBtnSpec, kebab *widget.Clickable, w int, equal bool) (layout.Dimensions, image.Rectangle) {
	m := pfMetrics(size)
	hh := dp(gtx, m.h)
	gap := dp(gtx, 8)
	var widths [4]int
	n := minInt(len(specs), len(widths))
	avail := w - hh - gap*n
	if kebab == nil {
		avail = w - gap*(n-1)
	}
	sum := 0
	for i := 0; i < n; i++ {
		widths[i] = pfBtnNatural(gtx, th, m, specs[i])
		sum += widths[i]
	}
	if n > 0 {
		if equal || sum > avail {
			for i := 0; i < n; i++ {
				widths[i] = maxInt(hh, avail/n)
			}
		} else {
			extra := (avail - sum) / n
			for i := 0; i < n; i++ {
				widths[i] += extra
			}
		}
	}
	x := 0
	for i := 0; i < n; i++ {
		s := specs[i]
		bw := widths[i]
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return pfBtn(gtx, th, size, s, bw)
		})
		x += bw + gap
	}
	if kebab == nil {
		return layout.Dimensions{Size: image.Pt(w, hh)}, image.Rectangle{}
	}
	kr := image.Rect(x, 0, x+hh, hh)
	at(gtx, kr.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(hh, hh)}
		return Button(gtx, th, kebab, BtnOutline, size, "", "kebab", 0)
	})
	return layout.Dimensions{Size: image.Pt(maxInt(w, kr.Max.X), hh)}, kr
}
