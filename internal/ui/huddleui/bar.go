package huddleui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
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

type headerState struct{ click widget.Clickable }

// HeaderLive reports whether channel's header should show the live
// huddle pill (a huddle is on there, or you're in or joining it).
func (c *Controller) HeaderLive(ch string) bool {
	_, live := c.Live(ch)
	return live || c.In(ch)
}

func (c *Controller) headerText(ch string) (string, []string) {
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
	if len(people) == 0 {
		return "Huddle", people
	}
	return "Huddle " + itoa(len(people)), people
}

// HeaderPillWidth is the live pill's width for ch.
func (c *Controller) HeaderPillWidth(gtx C, th *theme.Theme, ch string) int {
	txt, people := c.headerText(ch)
	w := labelSize(gtx, th, 13, theme.Bold, txt).X
	faces := min(len(people), 3)
	return gtx.Dp(8) + faces*gtx.Dp(16) + boolDp(gtx, faces > 0, 6) + gtx.Dp(18+6) + w + gtx.Dp(6+14+6)
}

func boolDp(gtx C, b bool, dp unit.Dp) int {
	if b {
		return gtx.Dp(dp)
	}
	return 0
}

// HeaderPill draws Slack's mint "huddle is on" button: faces, headphones,
// "Huddle N", chevron. A click joins (or leaves, when you're in it).
func (c *Controller) HeaderPill(gtx C, th *theme.Theme, ch string, h int) D {
	hs := c.hdr[ch]
	if hs == nil {
		hs = &headerState{}
		c.hdr[ch] = hs
	}
	if hs.click.Clicked(gtx) {
		c.Toggle(ch)
	}
	txt, people := c.headerText(ch)
	w := c.HeaderPillWidth(gtx, th, ch)
	bg, ink := hudMint, hudMintInk
	if th.Dark {
		bg, ink = hudMintDark, hudGreenLight
	}
	return button(gtx, &hs.click, image.Pt(w, h), func(gtx C, hov bool) {
		col := bg
		if hov {
			col = theme.Mix(bg, ink, 0.12)
		}
		theme.RRect(gtx, image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(8), col)
	}, func(gtx C) D {
		x := gtx.Dp(8)
		fs := gtx.Dp(18)
		for i, u := range people {
			if i == 3 {
				break
			}
			u := u
			at(gtx, image.Pt(x, (h-fs)/2), image.Pt(fs, fs), func(gtx C) D {
				theme.RRect(gtx, image.Rect(-gtx.Dp(1), -gtx.Dp(1), fs+gtx.Dp(1), fs+gtx.Dp(1)), gtx.Dp(5), bg)
				avatar(gtx, th, c.h.Images(), c.avatarURL(u, fs), image.Pt(fs, fs), gtx.Dp(4))
				return D{Size: image.Pt(fs, fs)}
			})
			x += gtx.Dp(16)
		}
		if len(people) > 0 {
			x += gtx.Dp(6)
		}
		at(gtx, image.Pt(x, (h-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx C) D {
			return icons.Get("headphones")(gtx, 18, ink)
		})
		x += gtx.Dp(24)
		ls := labelSize(gtx, th, 13, theme.Bold, txt)
		at(gtx, image.Pt(x, (h-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, ink, txt) })
		x += ls.X + gtx.Dp(6)
		at(gtx, image.Pt(x, (h-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx C) D {
			return icons.Get("chevron-down")(gtx, 14, ink)
		})
		return D{}
	})
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
