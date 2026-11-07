package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// channelPane holds per-channel shell state (the views are cached in
// a.views so switching back is instant).
type channelPane struct {
	tab int // 0 = Messages, then bookmarks/pins/canvases
}

func (a *App) layoutChannel(gtx layout.Context, ch string) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &a.th.C
	conv := a.model().conv(ch)
	if conv == nil && a.data != nil {
		conv = a.data.resolved(ch) // closed DM / channel outside the sidebar
	}
	hh := gtx.Dp(theme.HeaderH)
	th := gtx.Dp(theme.TabsH)

	atExact(gtx, image.Pt(0, 0), image.Pt(sz.X, hh), func(gtx layout.Context) layout.Dimensions {
		return a.layoutChannelHeader(gtx, ch, conv)
	})
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, th), func(gtx layout.Context) layout.Dimensions {
		return a.layoutChannelTabs(gtx, ch)
	})
	theme.Fill(gtx, image.Rect(0, hh+th-gtx.Dp(1), sz.X, hh+th), c.Border)

	body := image.Rect(0, hh+th, sz.X, sz.Y)
	if a.chBodyW > 0 && a.chBodyW < sz.X {
		body.Max.X = a.chBodyW
	}
	cv := a.convoFor(ch, "")
	atExact(gtx, body.Min, body.Size(), func(gtx layout.Context) layout.Dimensions {
		mp := clsMessagePane.Enter(gtx, 0, 0)
		defer mp.Leave()
		return a.layoutConvo(gtx, cv, a.primaryOrigin.Add(body.Min))
	})
	return layout.Dimensions{Size: sz}
}

func (a *App) layoutChannelHeader(gtx layout.Context, ch string, conv *convInfo) layout.Dimensions {
	sz := gtx.Constraints.Max
	vh := clsViewHeader.Enter(gtx, 0, 0)
	defer vh.Leave()
	if vh.Hidden() {
		return layout.Dimensions{Size: sz}
	}
	c := &a.th.C
	h := sz.Y
	x := gtx.Dp(18) // Slack: 28px star button at +18, title icon at +54
	// star
	starred := conv != nil && conv.Starred
	star := "star"
	if starred {
		star = "star-filled"
	}
	at(gtx, image.Pt(x, (h-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		col := c.Fg
		if starred {
			col = c.Warning
		}
		return iconButton(gtx, a.clicks.get("hdr.star"), star, 28, 20, col, c.BgHover, 8)
	})
	if clicked(a.clicks.get("hdr.star")) {
		a.toggleStar(ch)
	}
	x += gtx.Dp(34) // title button at +52

	// right-side controls, laid out right-to-left first so the title can
	// ellipsize into the remaining space.
	right := sz.X - gtx.Dp(14)
	btn := gtx.Dp(28)
	y := (h - btn) / 2
	kebabX := 0
	for _, k := range []string{"kebab", "search", "bell"} {
		k := k
		right -= btn
		if k == "kebab" {
			kebabX = right
		}
		at(gtx, image.Pt(right, y), image.Pt(btn, btn), func(gtx layout.Context) layout.Dimensions {
			name := k
			if k == "bell" && conv != nil && conv.Muted {
				name = "bell-off"
			}
			return iconButton(gtx, a.clicks.get("hdr."+k), name, 28, 20, c.FgMuted, c.BgHover, 8)
		})
		right -= gtx.Dp(6)
	}
	if clicked(a.clicks.get("hdr.kebab")) {
		// popups take window coordinates; the pointer is on the kebab (this
		// header is also drawn inside views' panes, where offsets differ)
		_ = kebabX
		a.openChannelMenu(ch, a.PointerPos())
	}
	if clicked(a.clicks.get("hdr.search")) {
		a.openSearch("in:<#" + ch + ">")
	}
	if clicked(a.clicks.get("hdr.bell")) {
		a.openNotifPrefs(ch)
	}

	// Huddle split button (94×28: 74 + 20). Slack 2026's --polish variant
	// has no outline, only hover fills.
	hw := gtx.Dp(94)
	right -= hw
	at(gtx, image.Pt(right, y), image.Pt(hw, btn), func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, hw, btn)
		at(gtx, image.Pt(0, 0), image.Pt(gtx.Dp(74), btn), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, a.clicks.get("hdr.huddle"), func(gtx layout.Context, s image.Point, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(8), (btn-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("huddle")(gtx, 18, c.FgMuted)
				})
				at(gtx, image.Pt(gtx.Dp(30), 0), image.Pt(gtx.Dp(44), btn), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, btn, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, c.FgMuted, "Huddle")
					})
				})
				return layout.Dimensions{Size: image.Pt(gtx.Dp(74), btn)}
			})
		})
		at(gtx, image.Pt(gtx.Dp(74), 0), image.Pt(gtx.Dp(20), btn), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, a.clicks.get("hdr.huddlemore"), nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(1), (btn-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("chevron-down")(gtx, 14, c.FgMuted)
				})
				return layout.Dimensions{Size: image.Pt(gtx.Dp(20), btn)}
			})
		})
		return layout.Dimensions{Size: r.Size()}
	})
	if clicked(a.clicks.get("hdr.huddle")) {
		a.Toast("Huddles aren't supported in " + AppName + " yet")
	}
	right -= gtx.Dp(6)

	// member facepile (up to 3 avatars, 20px, overlapped) + count: Slack's
	// BaseAvatarStack, whose width a plugin may change (HumanCount).
	if conv != nil && conv.Kind != "im" && conv.NumMembers > 0 {
		call, d := measured(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(right-x, 0), btn)}
			return slackc.Component(gtx, "BaseAvatarStack", hdrStack{a: a, ch: ch, conv: conv, btn: btn}, hdrStack.props, hdrStack.layout)
		})
		right -= d.Size.X
		place(gtx, call, image.Pt(right, y+(btn-d.Size.Y)/2))
		if clicked(a.clicks.get("hdr.members")) {
			a.OpenDetails(ch)
		}
		if d.Size.X > 0 {
			right -= gtx.Dp(6)
		}
	}

	// title: icon + name 18px/900 + chevron, clickable → details.
	name := a.convTitle(conv, ch)
	icon := "hash"
	if conv != nil {
		switch conv.Kind {
		case "private":
			icon = "lock"
		case "im", "app":
			icon = ""
		case "mpim":
			icon = "people"
		}
	}
	avail := right - x - gtx.Dp(16)
	nw := a.th.Measure(gtx, 18, theme.Black, name)
	// Slack (measured): button padding 2px 4px, 18px icon, name 20px after
	// it, no chevron, title min-width 88px; the topic starts 16px after.
	iconW := 0
	if icon != "" {
		iconW = gtx.Dp(20)
	} else if conv != nil {
		iconW = gtx.Dp(28)
	}
	titleW := max(iconW+nw, gtx.Dp(88)) + gtx.Dp(8)
	if titleW > avail {
		nw -= titleW - avail
		titleW = avail
	}
	if nw < 0 {
		nw = 0
	}
	at(gtx, image.Pt(x, (h-gtx.Dp(32))/2), image.Pt(titleW, gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, a.clicks.get("hdr.title"), func(gtx layout.Context, s image.Point, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			th := gtx.Dp(32)
			ix := gtx.Dp(4)
			if icon != "" {
				at(gtx, image.Pt(ix, (th-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 18, c.Fg)
				})
			} else if conv != nil {
				at(gtx, image.Pt(ix, (th-gtx.Dp(24))/2), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
					return a.avatar(gtx, conv.Avatar, conv.Name, gtx.Dp(24), gtx.Dp(6))
				})
			}
			at(gtx, image.Pt(ix+iconW, 0), image.Pt(nw, th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 18, theme.Black, c.Fg, name)
				})
			})
			return layout.Dimensions{Size: image.Pt(titleW, th)}
		})
	})
	if clicked(a.clicks.get("hdr.title")) {
		a.OpenDetails(ch)
	}
	x += titleW + gtx.Dp(16)

	// topic (13px muted) in the remaining space.
	if conv != nil && conv.Topic != "" && right-x > gtx.Dp(40) {
		at(gtx, image.Pt(x, 0), image.Pt(right-x-gtx.Dp(12), h), func(gtx layout.Context) layout.Dimensions {
			if len(conv.TopicSpans) == 0 {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.FgMuted, oneLine(conv.Topic))
				})
			}
			return a.inlineSpans(gtx, conv.TopicSpans, 13, c.FgMuted, h)
		})
	}
	return layout.Dimensions{Size: sz}
}

// inlineSpans draws rich spans on one line (topic, previews): text, links
// and mentions as labels, emoji as images; ellipsized at the max width.
func (a *App) inlineSpans(gtx layout.Context, spans []richtext.Span, size unit.Sp, col color.NRGBA, h int) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	x := 0
	es := gtx.Sp(size * 18 / 13)
	ell := a.th.Measure(gtx, size, theme.Regular, "…")
	for _, s := range spans {
		if s.Kind == richtext.Break {
			break
		}
		if s.Kind == richtext.Emoji && s.URL != "" {
			if x+es > maxW {
				break
			}
			at(gtx, image.Pt(x, (h-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
				if img, ok := a.images.Get(s.URL, es*2); ok {
					return theme.Image(gtx, img, image.Pt(es, es), 0)
				}
				return layout.Dimensions{Size: image.Pt(es, es)}
			})
			x += es + gtx.Dp(1)
			continue
		}
		txt := strings.ReplaceAll(s.Text, "\n", " ")
		if i := strings.IndexByte(txt, '\n'); i >= 0 {
			txt = txt[:i]
		}
		if txt == "" {
			continue
		}
		w := theme.Bold
		if s.Style&richtext.Bold == 0 {
			w = theme.Regular
		}
		fg := col
		switch s.Kind {
		case richtext.Link, richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast:
			fg = a.th.C.Link
		}
		tw := a.th.Measure(gtx, size, w, txt)
		trunc := x+tw > maxW
		if trunc {
			tw = maxW - x - ell
			if tw <= 0 {
				break
			}
		}
		at(gtx, image.Pt(x, 0), image.Pt(tw+ell, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: size, Weight: w, Italic: s.Style&richtext.Italic != 0, Color: fg, MaxLines: 1}, txt)
			})
		})
		x += tw
		if trunc {
			break
		}
	}
	return layout.Dimensions{Size: image.Pt(maxW, h)}
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

type chTab struct {
	key, label, icon string
	fileID           string
}

func (a *App) channelTabs(ch string) []chTab {
	tabs := []chTab{{key: "messages", label: "Messages", icon: "threads"}}
	if a.data != nil {
		tabs = append(tabs, a.data.tabs(ch)...)
	}
	return tabs
}

func (a *App) layoutChannelTabs(gtx layout.Context, ch string) layout.Dimensions {
	sz := gtx.Constraints.Max
	c := &a.th.C
	x := gtx.Dp(16)
	cur := a.chTab[ch]
	for i, t := range a.channelTabs(ch) {
		i, t := i, t
		lw := a.th.Measure(gtx, 13, theme.Bold, t.label)
		w := gtx.Dp(8) + gtx.Dp(16) + gtx.Dp(4) + lw + gtx.Dp(8)
		if x+w > sz.X-gtx.Dp(48) {
			break
		}
		btn := a.clicks.get("tab." + ch + "." + t.key)
		at(gtx, image.Pt(x, 0), image.Pt(w, sz.Y), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				if hov && i != cur {
					theme.RRect(gtx, image.Rect(0, gtx.Dp(5), s.X, s.Y-gtx.Dp(5)), gtx.Dp(6), c.BgHover)
				}
				if i == cur {
					theme.Fill(gtx, image.Rect(0, s.Y-gtx.Dp(2), s.X, s.Y), c.Accent)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				col := c.FgMuted
				if i == cur {
					col = c.Fg
					if a.th.Dark {
						col = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff} // Slack: --dt_color-content-pry
					}
				}
				icon := t.icon
				if t.key == "messages" && i == cur {
					icon = "messages-filled" // Slack fills the active Messages bubble
				}
				at(gtx, image.Pt(gtx.Dp(8), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 16, col)
				})
				at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(lw+2, sz.Y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, col, t.label)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, sz.Y)}
			})
		})
		if clicked(btn) {
			if a.chTab == nil {
				a.chTab = map[string]int{}
			}
			if t.key == "messages" {
				a.chTab[ch] = 0
			} else {
				a.chTab[ch] = 0
				a.openTab(ch, t.key)
			}
		}
		x += w + gtx.Dp(4)
	}
	at(gtx, image.Pt(x+gtx.Dp(4), (sz.Y-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, a.clicks.get("tab.add"), "plus", 28, 18, c.FgMuted, c.BgHover, 6)
	})
	if clicked(a.clicks.get("tab.add")) {
		a.Toast("Adding canvases and lists isn't supported yet")
	}
	return layout.Dimensions{Size: sz}
}

// convTitle is the header name: channel name, DM partner, or group list.
func (a *App) convTitle(conv *convInfo, ch string) string {
	if conv == nil {
		return ch
	}
	return conv.displayName()
}
