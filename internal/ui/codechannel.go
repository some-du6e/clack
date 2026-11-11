package ui

// Code channels (Slack Code) in the shell: the sidebar's agent status dot,
// the header's code tile and the agent context bar above the composer.
// Ported from Slack's web client (build a84662d2d135); docs/CODE-CHANNELS.md
// has the evidence. The data model is core/codechannel.go.

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ccPal is the context bar's and header tile's design tokens (Slack's
// slack-kit stylesheet, light and dark).
type ccPal struct {
	bar        color.NRGBA // --dt_color-base-pry: .p-context_bar_v2--agent-channel
	chip       color.NRGBA // --dt_color-base-sec: .chip__U2UDT
	chipHover  color.NRGBA // --dt_color-base-hgl-1
	sec        color.NRGBA // --dt_color-content-sec: .container__BAERT, .statusLabel__M9jGP
	ter        color.NRGBA // --dt_color-content-ter: .context_link__bdTEu, .no_info__RFRaQ
	pry        color.NRGBA // --dt_color-content-pry: a link's hover
	stopBg     color.NRGBA // --dt_color-base-inv-hgl-1: .stopIndicator__t3arZ
	stopFg     color.NRGBA // --dt_color-content-inv-pry
	tileBg     color.NRGBA // rgb(--dt_color-plt-aubergine-0): .container__EYszp
	tileFg     color.NRGBA // rgb(--dt_color-plt-aubergine-60)
	discText   color.NRGBA // the foreword's disclaimer (content-ter)
	foreground color.NRGBA
}

func codePalette(dark bool) ccPal {
	if dark {
		return ccPal{bar: hexc(0x1a1d21), chip: hexc(0x1a1d21), chipHover: hexc(0x001a2d), sec: hexc(0xb9babd), ter: hexc(0x9a9b9e),
			pry: hexc(0xf8f8f8), stopBg: hexc(0x0a77a7), stopFg: hexc(0xf8f8f8), tileBg: hexc(0x200221), tileFg: hexc(0xbc80ce)}
	}
	return ccPal{bar: hexc(0xffffff), chip: hexc(0xf8f8f8), chipHover: hexc(0xe3f8ff), sec: hexc(0x454447), ter: hexc(0x5e5d60),
		pry: hexc(0x1d1c1d), stopBg: hexc(0x1264a3), stopFg: hexc(0xffffff), tileBg: hexc(0xf9edff), tileFg: hexc(0xa350af)}
}

func hexc(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// ---- the sidebar's status dot (AgentChannelStatusDot) ----

// codeStatusDot draws a code channel's prefix where a channel's # goes:
// Slack's p-channel_sidebar__presence_icon with the status icon (16px,
// the row's color), or while the agent works (and the row isn't muted) the
// slow InfiniteSpinner, pulsing.
func (a *App) codeStatusDot(gtx layout.Context, conv *convInfo, fg color.NRGBA, muted bool) layout.Dimensions {
	sz := gtx.Dp(16)
	pr := ""
	if conv.Code != nil {
		pr = conv.Code.PRStatus
	}
	icon, _ := core.AgentStatusDisplay(conv.CodeStatus, pr)
	if conv.CodeStatus == core.AgentProcessing && !muted {
		// .pulsing__T4vY1: opacity 1 → .2 → 1 over 2s, ease-in-out
		t := float64(gtx.Now.UnixMilli()%2000) / 2000
		ph := t * 2
		if ph > 1 {
			ph = 2 - ph
		}
		e := ph * ph * (3 - 2*ph) // ease-in-out
		alpha := 1 - 0.8*e
		col := fg
		col.A = uint8(float64(col.A)*alpha + .5)
		codeSpinner(gtx, sz, col)
		return layout.Dimensions{Size: image.Pt(sz, sz)}
	}
	if icon == "" {
		icon = "status-member"
	}
	return icons.Get(icon)(gtx, 16, fg)
}

// codeSpinner is Slack's InfiniteSpinner (s6So) at size medium (16px),
// speed slow (the ring turns in 2.4s) and color inherit: a .25-opacity
// track and two 90° arcs in the current color on different easings.
func codeSpinner(gtx layout.Context, d int, col color.NRGBA) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		st := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	track := col
	track.A = uint8(float32(col.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	t := float64(gtx.Now.UnixMilli()%2400) / 2400
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	arc(base+float32(2*math.Pi*ccBezier(.25, .29, .54, .86, t)), dash, col)
	arc(base+float32(2*math.Pi*ccBezier(.41, .24, .64, .69, t)), dash, col)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// ccBezier is CSS cubic-bezier(x1, y1, x2, y2) at progress x.
func ccBezier(x1, y1, x2, y2, x float64) float64 {
	lo, hi := 0.0, 1.0
	for i := 0; i < 24; i++ {
		m := (lo + hi) / 2
		bx := 3*(1-m)*(1-m)*m*x1 + 3*(1-m)*m*m*x2 + m*m*m
		if bx < x {
			lo = m
		} else {
			hi = m
		}
	}
	m := (lo + hi) / 2
	return 3*(1-m)*(1-m)*m*y1 + 3*(1-m)*m*m*y2 + m*m*m
}

// ---- the header's code tile (ViewHeaderCodeIcon) ----

// codeHeaderTile is the 28px tile Slack's header shows before a code
// channel's title, where other channels have the star: aubergine-0 with
// the 20px code glyph in aubergine-60, radius 8, 1px below center.
func (a *App) codeHeaderTile(gtx layout.Context) layout.Dimensions {
	p := codePalette(a.th.Dark)
	s := gtx.Dp(28)
	off := op.Offset(image.Pt(0, gtx.Dp(1))).Push(gtx.Ops)
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(s, s)}, gtx.Dp(8), p.tileBg)
	at(gtx, image.Pt((s-gtx.Dp(20))/2, (s-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("code")(gtx, 20, p.tileFg)
	})
	off.Pop()
	return layout.Dimensions{Size: image.Pt(s, s)}
}

// ---- the agent context bar (ContextBarAgentChannelContent) ----

func init() {
	composer.ContextBar = func(gtx layout.Context, h composer.Host, ch, ts string) layout.Dimensions {
		a, ok := h.(*App)
		if !ok {
			return layout.Dimensions{}
		}
		if ts != "" { // shouldDisplayAgentChannelBanner: not in threads
			return layout.Dimensions{}
		}
		return a.layoutCodeContextBar(gtx, ch)
	}
	composer.ThreadNugget = func(gtx layout.Context, h composer.Host, ch, ts string) layout.Dimensions {
		if a, ok := h.(*App); ok {
			return a.layoutAppThreadNugget(gtx, ch, ts) // agent_nugget.go
		}
		return layout.Dimensions{}
	}
}

// ContextBarShowing is messages.ContextBarShower: the conversation's
// composer drew a context bar, so its list ends with Slack's shorter
// bottom spacer (space.100 instead of space.150).
func (a *App) ContextBarShowing(ch string) bool {
	c := a.convos[ch+"/"]
	if c == nil || c.comp == nil {
		return false
	}
	if s, ok := c.comp.(interface{ ContextBarShown() bool }); ok {
		return s.ContextBarShown()
	}
	return false
}

// codeContextBarShown is shouldDisplayAgentChannelBanner: session_channels
// on and a code channel (Slack also leaves out the share dialog and code
// channels it presents as Slackbot threads).
func (a *App) codeContextBarShown(conv *convInfo) bool {
	if conv == nil || conv.Code == nil || a.data == nil || a.data.core == nil {
		return false
	}
	return a.data.core.Experiment("session_channels") == "on"
}

// layoutCodeContextBar draws the bar: .p-context_bar_v2--agent-channel
// (base-pry, padding 6px 8px, top corners 8px) holding the container
// (12px content-sec, gap 16): the working chip while the agent works, then
// either the code channel's context_bar_items (links and action buttons)
// or, when code_channel is empty, "No branch info". A preview (not a
// member) disables the actions: "Join this channel to interact".
func (a *App) layoutCodeContextBar(gtx layout.Context, ch string) layout.Dimensions {
	conv := a.model().conv(ch)
	if conv == nil && a.data != nil {
		conv = a.data.resolved(ch)
	}
	if !a.codeContextBarShown(conv) {
		return layout.Dimensions{}
	}
	p := codePalette(a.th.Dark)
	cc := conv.Code
	w := gtx.Constraints.Max.X
	padX, padY := gtx.Dp(8), gtx.Dp(6)
	inner := w - 2*padX
	disabled := ""
	if conv.Preview {
		disabled = "Join this channel to interact"
	}
	working := conv.CodeStatus == core.AgentProcessing
	lineH := gtx.Dp(16)
	h := lineH
	if working {
		h = gtx.Dp(28) // the chip: 6px padding around a 16px line
	}
	if len(cc.ContextBar) > 0 {
		for _, it := range cc.ContextBar {
			if it.IsAction() {
				h = max(h, gtx.Dp(16))
			}
		}
	}
	H := h + 2*padY
	// the bar, square at the bottom (its filler runs into the composer)
	rr := clip.RRect{Rect: image.Rect(0, 0, w, H+gtx.Dp(6)), NW: gtx.Dp(8), NE: gtx.Dp(8)}
	paint.FillShape(gtx.Ops, p.bar, rr.Op(gtx.Ops))

	x := padX
	right := padX + inner
	if working {
		d := a.ccWorkingChip(gtx, ch, conv, p, image.Pt(x, padY+(h-gtx.Dp(28))/2), disabled)
		x += d.X + gtx.Dp(8)
	}
	switch {
	case len(cc.ContextBar) > 0:
		// .spacer pushes the items to the right; they keep their order
		gap := gtx.Dp(16)
		widths := make([]int, len(cc.ContextBar))
		total := 0
		for i, it := range cc.ContextBar {
			widths[i] = gtx.Dp(20) + a.th.Measure(gtx, 12, theme.Regular, it.Label)
			total += widths[i]
		}
		total += gap * (len(widths) - 1)
		ix := max(right-total, x+gap)
		for i, it := range cc.ContextBar {
			iw := min(widths[i], max(right-ix, 0))
			if iw <= gtx.Dp(20) {
				break
			}
			a.ccItem(gtx, ch, it, p, image.Rect(ix, padY, ix+iw, padY+h), disabled)
			ix += iw + gap
		}
	case cc.Empty:
		// the second button group: "No branch info" (margin-right 4px)
		txt := "No branch info"
		tw := a.th.Measure(gtx, 12, theme.Regular, txt)
		at(gtx, image.Pt(right-gtx.Dp(4)-tw, padY), image.Pt(tw+2, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 12, theme.Regular, p.ter, txt)
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, H)}
}

// ccWorkingChip is AgentWorkingNugget's chip (.chip__U2UDT): a 14px slow
// spinner, swapped for the stop glyph on hover, and "Stop agent"; a click
// stops the agent (agents.sessions.stop).
func (a *App) ccWorkingChip(gtx layout.Context, ch string, conv *convInfo, p ccPal, org image.Point, disabled string) image.Point {
	btn := a.clicks.get("cc.stop." + ch)
	label := "Stop agent"
	if a.ccStopping[ch] {
		label = "Stopping…"
	}
	tw := a.th.Measure(gtx, 12, theme.Regular, label)
	cw := gtx.Dp(8) + gtx.Dp(14) + gtx.Dp(6) + tw + gtx.Dp(8)
	ch28 := gtx.Dp(28)
	at(gtx, org, image.Pt(cw, ch28), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
			bg := p.chip
			if hov && disabled == "" && !a.ccStopping[ch] {
				bg = p.chipHover
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			is := gtx.Dp(14)
			iy := (ch28 - is) / 2
			hov := btn.Hovered() && disabled == "" && !a.ccStopping[ch]
			at(gtx, image.Pt(gtx.Dp(8), iy), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
				if hov { // .stopIndicator__t3arZ: a 14px inverse dot with the 8px stop glyph
					theme.Circle(gtx, image.Pt(is/2, is/2), is, p.stopBg)
					at(gtx, image.Pt((is-gtx.Dp(8))/2, (is-gtx.Dp(8))/2), image.Pt(gtx.Dp(8), gtx.Dp(8)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get("stop-filled")(gtx, 8, p.stopFg)
					})
					return layout.Dimensions{Size: image.Pt(is, is)}
				}
				sp := gtx.Dp(12) // spinner size small inside the 14px box
				off := op.Offset(image.Pt((is-sp)/2, (is-sp)/2)).Push(gtx.Ops)
				codeSpinner(gtx, sp, p.sec)
				off.Pop()
				return layout.Dimensions{Size: image.Pt(is, is)}
			})
			at(gtx, image.Pt(gtx.Dp(8)+is+gtx.Dp(6), 0), image.Pt(tw+2, ch28), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, ch28, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 12, theme.Regular, p.sec, label)
				})
			})
			return layout.Dimensions{Size: image.Pt(cw, ch28)}
		})
	})
	if clicked(btn) && disabled == "" && !a.ccStopping[ch] && a.data != nil && a.data.core != nil {
		if a.ccStopping == nil {
			a.ccStopping = map[string]bool{}
		}
		a.ccStopping[ch] = true
		c, ctx := a.data.core, a.data.ctx
		go func() {
			err := c.StopAgent(ctx, ch)
			a.Post(func() {
				delete(a.ccStopping, ch)
				if err != nil {
					a.Toast(conv.AgentName + " failed to stop response")
				}
				a.Invalidate()
			})
		}()
	}
	return image.Pt(cw, ch28)
}

// ccItem is ContextBarInfoItem (a link to its url, or plain text) or
// ContextBarActionItem (client.codeChannels.action): .context_link__bdTEu,
// a 16px icon (one of Slack's allowed glyphs, else link) 4px before the
// 12px label in content-ter, content-pry on hover, .5 opacity disabled.
func (a *App) ccItem(gtx layout.Context, ch string, it core.ContextBarItem, p ccPal, r image.Rectangle, disabled string) {
	btn := a.clicks.get("cc.item." + ch + "." + it.Key)
	icon := ccItemIcon(it.Icon)
	act := it.IsAction()
	link := !act && it.URL != ""
	pending := act && a.ccPending[ch+":"+it.Key]
	off := act && (disabled != "" || pending)
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		draw := func(gtx layout.Context) layout.Dimensions {
			col := p.ter
			if (link || act) && btn.Hovered() && !off {
				col = p.pry
			}
			if off {
				col.A /= 2
			}
			h := r.Dy()
			tx := 0
			if pending {
				sp := gtx.Dp(12)
				o := op.Offset(image.Pt((gtx.Dp(16)-sp)/2, (h-sp)/2)).Push(gtx.Ops)
				codeSpinner(gtx, sp, col)
				o.Pop()
				tx = gtx.Dp(20)
			} else if icon != "" {
				at(gtx, image.Pt(0, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon)(gtx, 16, col)
				})
				tx = gtx.Dp(20)
			}
			at(gtx, image.Pt(tx, 0), image.Pt(max(r.Dx()-tx, 0), h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 12, theme.Regular, col, it.Label)
				})
			})
			return layout.Dimensions{Size: r.Size()}
		}
		if !link && !act {
			return draw(gtx) // .context_text: plain text
		}
		return clickArea(gtx, btn, nil, draw)
	})
	if !clicked(btn) {
		return
	}
	switch {
	case link:
		a.OpenURL(it.URL)
	case act && !off && a.data != nil && a.data.core != nil:
		if a.ccPending == nil {
			a.ccPending = map[string]bool{}
		}
		k := ch + ":" + it.Key
		a.ccPending[k] = true
		c, ctx, key := a.data.core, a.data.ctx, it.Key
		go func() {
			err := c.CodeChannelAction(ctx, ch, key)
			a.Post(func() {
				delete(a.ccPending, k)
				if err != nil {
					a.Toast("Couldn’t reach " + a.ccAgentName(ch) + ". Try again.")
				}
				a.Invalidate()
			})
		}()
	}
}

func (a *App) ccAgentName(ch string) string {
	if c := a.model().conv(ch); c != nil && c.AgentName != "" {
		return c.AgentName
	}
	return "Agent"
}

// ccItemIcon is getIconName: Slack draws only these glyphs, anything else
// as link; no icon, none.
func ccItemIcon(name string) string {
	switch name {
	case "":
		return ""
	case "branch", "folder", "hierarchy", "life-ring", "link", "globe", "terminal", "code", "search", "lock":
		return name
	}
	return "link"
}

// ---- the row's name tooltip (BaseChannelName) ----

// codeRowTip is one code channel row's name tooltip.
type codeRowTip struct {
	hover gesture.Hover
	tip   popups.TipState
}

// codeRowTooltip is the tooltip BaseChannelName puts over a code channel
// row's name (delay 600, position top, maxWidth small): the name when it
// is cut off, then BaseChannelNameTooltipContent's codeChannelTooltipText
// ("Public code channel"; core.CodeChannelRowTooltip follows
// session_channels and code_channels_naming) and the agent's "@name" as a
// TooltipSubtitle. r is the name's rectangle in the row.
func (a *App) codeRowTooltip(gtx layout.Context, conv *convInfo, name string, r image.Rectangle, truncated bool) {
	if a.data == nil || a.data.core == nil || r.Empty() {
		return
	}
	if a.sidebar.codeTips == nil {
		a.sidebar.codeTips = map[string]*codeRowTip{}
	}
	st := a.sidebar.codeTips[conv.ID]
	if st == nil {
		st = &codeRowTip{}
		st.tip.Delay = 600 * time.Millisecond
		st.tip.MaxWidth = 200 // .c-tooltip__tip max-width
		a.sidebar.codeTips[conv.ID] = st
	}
	t := op.Offset(r.Min).Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: r.Size()}).Push(gtx.Ops)
	st.hover.Add(gtx.Ops)
	area.Pop()
	pass.Pop()
	hovered := st.hover.Update(gtx.Source)
	var lines []popups.TipLine
	if hovered {
		kind, agent, ok := a.data.core.CodeChannelRowTooltip(conv.ID)
		if ok {
			if truncated {
				lines = append(lines, popups.TipLine{Text: name})
			}
			lines = append(lines, popups.TipLine{Text: kind})
			if agent != "" {
				lines = append(lines, popups.TipLine{Text: agent, Subtitle: true})
			}
		}
		hovered = ok
	}
	popups.TooltipLines(gtx, a.th, &st.tip, hovered, r.Size(), popups.Above, lines)
	t.Pop()
}
