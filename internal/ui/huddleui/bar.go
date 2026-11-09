package huddleui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// BarHeight is the sidebar huddle bar's height (0 when you're not in a
// huddle), so the sidebar can leave room for it.
func (c *Controller) BarHeight(gtx C) int {
	if !c.Active() {
		return 0
	}
	if s := c.Session(); s != nil && s.State().Mic.Problem() {
		return gtx.Dp(112 + barMicRow)
	}
	return gtx.Dp(112)
}

// barMicRow is the bar's microphone warning row (dp).
const barMicRow = 30

type barState struct {
	mic, deaf, open, leave, title widget.Clickable
	micRow, micFix                widget.Clickable
}

// LayoutBar draws the huddle bar that sits at the bottom of the sidebar
// while you're in a huddle: where it is, who's talking, and mic / deafen /
// window / leave. Size it with BarHeight.
func (c *Controller) LayoutBar(gtx C, th *theme.Theme) D {
	h := c.BarHeight(gtx)
	if h == 0 {
		return D{}
	}
	// Slack's HuddleMiniPanel (LetMeSee adds after it)
	if slackc.Patched("HuddleMiniPanel") {
		var room, ch string
		if s := c.Session(); s != nil {
			room, ch = s.RoomID(), s.State().Channel
		}
		return slackc.Render(gtx, "HuddleMiniPanel", slackc.Props{"$key": "huddle-mini-panel", "huddleId": room, "channelId": ch,
			"className": "p-huddle_mini_panel"}, func(gtx C, _ slackc.Props) D { return c.layoutBar(gtx, th, h) })
	}
	return c.layoutBar(gtx, th, h)
}

func (c *Controller) layoutBar(gtx C, th *theme.Theme, h int) D {
	sz := image.Pt(gtx.Constraints.Max.X, h)
	s := c.Session()
	var st huddle.State
	if s != nil {
		st = s.State()
	} else {
		c.mu.Lock()
		st.Channel = c.joining
		c.mu.Unlock()
	}
	b := &c.bar
	if s != nil {
		if b.mic.Clicked(gtx) {
			s.SetMuted(!st.Muted)
		}
		if b.deaf.Clicked(gtx) {
			s.SetDeafened(!st.Deafened)
		}
		if b.leave.Clicked(gtx) {
			c.Leave()
		}
	}
	if b.open.Clicked(gtx) || b.title.Clicked(gtx) || b.micRow.Clicked(gtx) {
		c.OpenWindow() // the window's banner says more
	}
	note, micBad := micNoteFor(st)
	if b.micFix.Clicked(gtx) && micBad {
		if note.settings != "" {
			_ = openurl.OpenSettings(note.settings)
		} else if s != nil {
			s.RetryMic()
		}
	}

	pad := gtx.Dp(8)
	card := image.Rect(pad, 0, sz.X-pad, sz.Y-pad)
	theme.RRect(gtx, card, gtx.Dp(10), color.NRGBA{A: 0x40})
	now := time.Now()

	// title row
	title := "Huddle in " + c.channelTitle(st.Channel)
	switch {
	case s == nil:
		title = "Joining " + c.channelTitle(st.Channel) + "…"
	case st.Phase == huddle.Connecting:
		title = "Connecting to " + c.channelTitle(st.Channel) + "…"
	case st.Phase == huddle.Reconnecting:
		title = "Reconnecting…"
	}
	x0, y0 := card.Min.X+gtx.Dp(10), card.Min.Y+gtx.Dp(10)
	at(gtx, image.Pt(x0, y0), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx C) D {
		return icons.Get("headphones")(gtx, 16, hudGreenLight)
	})
	timer := ""
	if !st.Started.IsZero() && s != nil {
		timer = elapsed(now.Sub(st.Started))
	}
	tw := 0
	if timer != "" {
		tw = labelSize(gtx, th, 12, theme.Regular, timer).X
		at(gtx, image.Pt(card.Max.X-gtx.Dp(10)-tw, y0), image.Pt(tw, gtx.Dp(18)), func(gtx C) D {
			return th.Label(gtx, 12, theme.Regular, theme.WithAlpha(hudWhite, 0xb3), timer)
		})
	}
	titleW := card.Dx() - gtx.Dp(36) - tw - gtx.Dp(8)
	at(gtx, image.Pt(x0+gtx.Dp(22), y0-gtx.Dp(1)), image.Pt(titleW, gtx.Dp(20)), func(gtx C) D {
		return button(gtx, &b.title, image.Pt(titleW, gtx.Dp(20)), nil, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return th.Label(gtx, 13, theme.Bold, hudWhite, title)
		})
	})

	// faces
	av := gtx.Dp(24)
	y1 := y0 + gtx.Dp(26)
	x := x0
	for i, p := range orderedParticipants(st) {
		if i == 7 {
			more := "+" + itoa(len(st.Participants)-7)
			at(gtx, image.Pt(x, y1+(av-gtx.Dp(16))/2), image.Pt(gtx.Dp(30), av), func(gtx C) D {
				return th.Label(gtx, 12, theme.Bold, hudWhite, more)
			})
			break
		}
		p := p
		at(gtx, image.Pt(x, y1), image.Pt(av, av), func(gtx C) D {
			rad := gtx.Dp(6)
			avatar(gtx, th, c.h.Images(), c.avatarURL(p.UserID, av), image.Pt(av, av), rad)
			if p.Speaking(now) {
				theme.Border(gtx, image.Rectangle{Max: image.Pt(av, av)}, rad, gtx.Dp(2), hudGreenLight)
			}
			if p.Muted {
				d := gtx.Dp(12)
				theme.Circle(gtx, image.Pt(av-d/2+gtx.Dp(2), av-d/2+gtx.Dp(2)), d, hudFrame)
				at(gtx, image.Pt(av-d+gtx.Dp(2), av-d+gtx.Dp(2)), image.Pt(d, d), func(gtx C) D {
					iconAt(gtx, image.Pt(d, d), "mic-off", 9, hudWhite)
					return D{}
				})
			}
			return D{Size: image.Pt(av, av)}
		})
		x += av + gtx.Dp(4)
	}

	// the microphone warning: short text (opens the window), and a fix
	// button: the privacy settings, or try again
	if micBad && s != nil {
		ry := y1 + av + gtx.Dp(6)
		rh := gtx.Dp(22)
		fix := "Settings"
		if note.settings == "" {
			fix = "Retry"
		}
		fw := labelSize(gtx, th, 12, theme.Bold, fix).X + gtx.Dp(14)
		rw := card.Max.X - gtx.Dp(10) - x0 - fw - gtx.Dp(6)
		at(gtx, image.Pt(x0, ry), image.Pt(rw, rh), func(gtx C) D {
			return button(gtx, &b.micRow, image.Pt(rw, rh), nil, func(gtx C) D {
				iconAt(gtx, image.Pt(gtx.Dp(14), rh), "warning", 14, hudWarn)
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(max(0, rw-gtx.Dp(20)), rh)}
				txt := micShort(st.Mic)
				ls := labelSize(gtx, th, 12, theme.Bold, txt)
				at(g, image.Pt(gtx.Dp(20), (rh-ls.Y)/2), image.Pt(min(ls.X, rw-gtx.Dp(20)), ls.Y), func(gtx C) D {
					return th.Label(gtx, 12, theme.Bold, hudWarn, txt)
				})
				return D{Size: image.Pt(rw, rh)}
			})
		})
		at(gtx, image.Pt(card.Max.X-gtx.Dp(10)-fw, ry), image.Pt(fw, rh), func(gtx C) D {
			return button(gtx, &b.micFix, image.Pt(fw, rh), func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(fw, rh)}, gtx.Dp(5), bg)
			}, func(gtx C) D { centerLabel(gtx, th, image.Pt(fw, rh), 12, theme.Bold, hudWhite, fix); return D{} })
		})
	}

	// controls
	bh, bw := gtx.Dp(28), gtx.Dp(32)
	y2 := card.Max.Y - bh - gtx.Dp(8)
	ctl := func(x int, cl *widget.Clickable, icon string, lit bool) {
		at(gtx, image.Pt(x, y2), image.Pt(bw, bh), func(gtx C) D {
			return button(gtx, cl, image.Pt(bw, bh), func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				if lit {
					bg = hudBtnOn
				}
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(bw, bh)}, gtx.Dp(6), bg)
			}, func(gtx C) D {
				fg := hudWhite
				if lit {
					fg = hudInk
				}
				iconAt(gtx, image.Pt(bw, bh), icon, 16, fg)
				return D{}
			})
		})
	}
	mic, deaf := "mic", "headphones"
	if st.Muted || micBad {
		mic = "mic-off"
	}
	if st.Deafened {
		deaf = "headphones-off"
	}
	ctl(x0, &b.mic, mic, !st.Muted && !micBad && s != nil)
	if micBad && s != nil {
		warnBadge(gtx, image.Pt(x0+bw, y2), gtx.Dp(12))
	}
	ctl(x0+bw+gtx.Dp(6), &b.deaf, deaf, st.Deafened)
	ctl(x0+2*(bw+gtx.Dp(6)), &b.open, "expand", false)
	lw := gtx.Dp(64)
	at(gtx, image.Pt(card.Max.X-gtx.Dp(10)-lw, y2), image.Pt(lw, bh), func(gtx C) D {
		return button(gtx, &b.leave, image.Pt(lw, bh), func(gtx C, hov bool) {
			bg := hudLeave
			if hov {
				bg = hudLeaveHov
			}
			theme.RRect(gtx, image.Rectangle{Max: image.Pt(lw, bh)}, gtx.Dp(6), bg)
		}, func(gtx C) D { centerLabel(gtx, th, image.Pt(lw, bh), 13, theme.Bold, hudWhite, "Leave"); return D{} })
	})
	return D{Size: sz}
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
		return "Leave huddle"
	}
	if contains(people, c.selfID()) {
		return "Joined on another device\nJoin on this device"
	}
	where := "in " + c.channelTitle(ch)
	if conv, ok := c.h.Core().S.Conv(ch); ok && conv.IsIM {
		if n := c.userName(conv.User); n != "" {
			where = "with " + n
		}
	}
	return "Join huddle " + where + "\n" + itoa(len(people)) + " in the huddle"
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
	popups.Tooltip(gtx, th, &hs.tip, hs.click.Hovered(), image.Pt(mw, h), popups.Below, c.headerTip(ch, in, people), "")

	cw := gtx.Dp(hdrCaret)
	at(gtx, image.Pt(mw, 0), image.Pt(cw, h), func(gtx C) D {
		d := button(gtx, &hs.caret, image.Pt(cw, h), func(gtx C, on bool) { hover(gtx, image.Pt(cw, h), on) }, func(gtx C) D {
			at(gtx, image.Pt(gtx.Dp(1), (h-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx C) D {
				return icons.Get("chevron-down")(gtx, 14, ink)
			})
			return D{}
		})
		popups.Tooltip(gtx, th, &hs.caretTip, hs.caret.Hovered(), image.Pt(cw, h), popups.Below, "More Huddles options", "")
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
	c.Start(ch)
}
