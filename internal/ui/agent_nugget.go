package ui

// The thread footer's AppThreadNugget (boot-shared 352G, CDN build
// 4d0c78085232), behind app_thread_nugget_fe: while an app has a status
// in a thread (assistant.threads.setStatus), a nugget above the thread's
// composer says it's working, and the composer's typing line leaves the
// app out (suppressBotTyping, core.SuppressBotTyping). With
// app_nugget_refinement it's a chip (AppAgentChipNugget); without, a
// shimmering row. core.AppThreadNugget decides when and what.
//
//	.chipContainer   padding 6px 16px, flex wrap, gap 8
//	.chip            light-dark(base-sec, base-ter), radius 8, padding 4px
//	                 8px, gap 8, nowrap: ChipIcon (the 20px app avatar),
//	                 .chipLabel (15px/1.46668 content-pry, ellipsis) and
//	                 ChipSpinner (a 14px box holding the small, slow, blue
//	                 InfiniteSpinner, noMargin); not stoppable, its tooltip
//	                 is the label over "Stop isn’t available for this
//	                 agent" (a TooltipSubtitle)
//	.nuggetContainer padding 0 16px
//	.statusRow       base-pry, radius 8, padding 8px 8px 8px 16px, gap 8:
//	                 the 20px avatar and .statusShimmer, 15px/1.46668 text
//	                 painted with a 200%-wide gradient (gray-30 0%, gray-50
//	                 40%, gray-60 50%, gray-50 60%, gray-30 100%) moving
//	                 from background-position 100% to -100% every 1s,
//	                 cubic-bezier(.22,.01,.77,.99)
//
// The label is one of the status's loading_messages, the next every 2.4s
// (f3+t), else "{app} is working" (chip) or "{app} is working…" (row).

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// appNuggetState is one thread's nugget: when its label started turning
// and its tooltip.
type appNuggetState struct {
	since time.Time
	key   string // the status it shows (a new one restarts the labels)
	hover gesture.Hover
	tip   popups.TipState
}

var appNuggets = map[*App]map[string]*appNuggetState{} // UI goroutine only

func (a *App) appNuggetState(ch, ts string) *appNuggetState {
	m := appNuggets[a]
	if m == nil {
		m = map[string]*appNuggetState{}
		appNuggets[a] = m
	}
	k := ch + "/" + ts
	st := m[k]
	if st == nil {
		st = &appNuggetState{}
		st.tip.Delay = 150 * time.Millisecond // BaseTooltip's default
		m[k] = st
	}
	return st
}

// appNuggetLabel is the nugget's text at now: the loading messages in
// turn (one every 2.4s from since), else Slack's working text.
func appNuggetLabel(n core.AppNugget, since, now time.Time) string {
	if len(n.LoadingMessages) > 0 {
		i := int(now.Sub(since) / (2400 * time.Millisecond))
		if i < 0 {
			i = 0
		}
		if m := n.LoadingMessages[i%len(n.LoadingMessages)]; m != "" {
			return m
		}
	}
	if n.Chip { // useWorkingStatusText under app_nugget_refinement
		return n.Name + " is working"
	}
	return n.Name + " is working…"
}

// nuggetTok is a Slack design token in the current mode.
func (a *App) nuggetTok(name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(a.th.Dark)["--dt_color-"+name])
	return c
}

// layoutAppThreadNugget draws the nugget for the thread ch/ts at the top
// of its composer (the context bar slot), or nothing.
func (a *App) layoutAppThreadNugget(gtx layout.Context, ch, ts string) layout.Dimensions {
	if a.data == nil || a.data.core == nil {
		return layout.Dimensions{}
	}
	n, ok := a.data.core.AppThreadNugget(ch, ts)
	if !ok {
		return layout.Dimensions{}
	}
	st := a.appNuggetState(ch, ts)
	if k := n.BotUserID + "\x00" + strings.Join(n.LoadingMessages, "\x00"); st.key != k || st.since.IsZero() {
		st.key, st.since = k, gtx.Now
	}
	label := appNuggetLabel(n, st.since, gtx.Now)
	if len(n.LoadingMessages) > 1 {
		next := st.since.Add((gtx.Now.Sub(st.since)/(2400*time.Millisecond) + 1) * 2400 * time.Millisecond)
		gtx.Execute(op.InvalidateCmd{At: next})
	}
	w := gtx.Constraints.Max.X
	lh := gtx.Dp(22) // 15px × 1.46668
	av := gtx.Dp(20)
	if n.Chip {
		// .chipContainer padding 6px 16px; the chip hugs its content up to the width
		padX, padY := gtx.Dp(16), gtx.Dp(6)
		cx, cy := gtx.Dp(8), gtx.Dp(4)
		gap := gtx.Dp(8)
		spin := gtx.Dp(14)
		room := max(w-2*padX-2*cx-av-spin-2*gap, 1)
		lw := min(a.th.Measure(gtx, 15, theme.Regular, label), room)
		chipW := cx + av + gap + lw + gap + spin + cx
		chipH := cy + max(av, lh) + cy
		bg := a.nuggetTok("base-sec")
		if a.th.Dark {
			bg = a.nuggetTok("base-ter")
		}
		off := op.Offset(image.Pt(padX, padY)).Push(gtx.Ops)
		theme.RRect(gtx, image.Rectangle{Max: image.Pt(chipW, chipH)}, gtx.Dp(8), bg)
		a.appNuggetAvatar(gtx, n, image.Pt(cx, (chipH-av)/2), av)
		at(gtx, image.Pt(cx+av+gap, 0), image.Pt(lw, chipH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, chipH, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = lw
				return a.th.Text(gtx, theme.LabelOpts{Font: font.Font{Typeface: theme.Typeface, Weight: theme.Regular}, Size: 15,
					Color: a.nuggetTok("content-pry"), MaxLines: 1}, label) // .chipLabel: text-overflow ellipsis
			})
		})
		at(gtx, image.Pt(chipW-cx-spin, (chipH-spin)/2), image.Pt(spin, spin), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Dp(12)
			off := op.Offset(image.Pt((spin-s)/2, (spin-s)/2)).Push(gtx.Ops)
			nuggetSpinner(gtx, s, a.nuggetTok("content-hgl-1"), a.nuggetTok("content-ter"))
			off.Pop()
			return layout.Dimensions{Size: image.Pt(spin, spin)}
		})
		pass := pointer.PassOp{}.Push(gtx.Ops)
		area := clip.Rect(image.Rectangle{Max: image.Pt(chipW, chipH)}).Push(gtx.Ops)
		st.hover.Add(gtx.Ops)
		area.Pop()
		pass.Pop()
		popups.TooltipLines(gtx, a.th, &st.tip, st.hover.Update(gtx.Source), image.Pt(chipW, chipH), popups.Above,
			[]popups.TipLine{{Text: label}, {Text: "Stop isn’t available for this agent", Subtitle: true}})
		off.Pop()
		return layout.Dimensions{Size: image.Pt(w, padY+chipH+padY)}
	}
	// .nuggetContainer padding 0 16px; .statusRow padding 8px 8px 8px 16px
	padX := gtx.Dp(16)
	rowW := max(w-2*padX, 1)
	gap := gtx.Dp(8)
	tx := gtx.Dp(16) + av + gap
	tw := max(rowW-tx-gtx.Dp(8), 1)
	lines := a.wrapNugget(gtx, label, tw)
	rowH := gtx.Dp(8) + max(av, len(lines)*lh) + gtx.Dp(8)
	off := op.Offset(image.Pt(padX, 0)).Push(gtx.Ops)
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(rowW, rowH)}, gtx.Dp(8), a.nuggetTok("base-pry"))
	a.appNuggetAvatar(gtx, n, image.Pt(gtx.Dp(16), gtx.Dp(8)), av)
	for i, ln := range lines {
		a.shimmerLine(gtx, ln, image.Pt(tx, gtx.Dp(8)+i*lh), lh, tw)
	}
	off.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
	return layout.Dimensions{Size: image.Pt(w, rowH)}
}

// appNuggetAvatar is ChipIcon / the row's icon: the status's icon_url or
// icon_emoji, else the app's avatar, 20px.
func (a *App) appNuggetAvatar(gtx layout.Context, n core.AppNugget, p image.Point, s int) {
	url := n.IconURL
	if url == "" && n.IconEmoji != "" {
		name := strings.Trim(n.IconEmoji, ":")
		if u, _, ok := a.data.core.S.Emoji(name); ok && u != "" {
			url = u
		} else if img, _, ok := emoji.Standard(name); ok {
			url = a.th.EmojiURL(img)
		}
	}
	if url == "" {
		url = a.userAvatar(n.BotUserID)
	}
	off := op.Offset(p).Push(gtx.Ops)
	a.avatar(gtx, url, n.Name, s, gtx.Dp(4)) // clamp(4px, 22.222%, 12px) at 20px
	off.Pop()
}

// wrapNugget breaks the row's label at spaces (overflow-wrap anywhere).
func (a *App) wrapNugget(gtx layout.Context, s string, w int) []string {
	if a.th.Measure(gtx, 15, theme.Regular, s) <= w {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, wd := range strings.Fields(s) {
		next := wd
		if cur != "" {
			next = cur + " " + wd
		}
		if cur != "" && a.th.Measure(gtx, 15, theme.Regular, next) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = []string{s}
	}
	return lines
}

// shimmerLine draws one line of .statusShimmer: each character takes the
// gradient's colour under its centre, the gradient sliding as Slack's
// app_thread_nugget_shimmer animation does.
func (a *App) shimmerLine(gtx layout.Context, ln string, p image.Point, lh, width int) {
	stops := []struct {
		at float64
		c  color.NRGBA
	}{{0, a.grayTok("plt-gray-30")}, {.4, a.grayTok("plt-gray-50")}, {.5, a.grayTok("plt-gray-60")}, {.6, a.grayTok("plt-gray-50")}, {1, a.grayTok("plt-gray-30")}}
	sample := func(u float64) color.NRGBA {
		u -= math.Floor(u)
		for i := 1; i < len(stops); i++ {
			if u <= stops[i].at {
				f := (u - stops[i-1].at) / (stops[i].at - stops[i-1].at)
				mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
				c0, c1 := stops[i-1].c, stops[i].c
				return color.NRGBA{R: mix(c0.R, c1.R), G: mix(c0.G, c1.G), B: mix(c0.B, c1.B), A: 0xff}
			}
		}
		return stops[len(stops)-1].c
	}
	W := float64(max(width, 1))
	t := float64(gtx.Now.UnixNano()%int64(time.Second)) / float64(time.Second)
	offset := -W + 2*W*ccBezier(.22, .01, .77, .99, t) // background-position 100% → -100%
	rs := []rune(ln)
	x := 0
	for i, r := range rs {
		ch := string(r)
		nx := a.th.Measure(gtx, 15, theme.Regular, string(rs[:i+1]))
		mid := float64(x+nx) / 2
		col := sample((mid - offset) / (2 * W))
		at(gtx, image.Pt(p.X+x, p.Y), image.Pt(nx-x+gtx.Dp(2), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 15, theme.Regular, col, ch)
			})
		})
		x = nx
	}
}

// grayTok is a palette colour token ("R,G,B").
func (a *App) grayTok(name string) color.NRGBA {
	c, _ := css.ParseColor("rgb(" + css.SlackVars(a.th.Dark)["--dt_color-"+name] + ")")
	return c
}

// nuggetSpinner is ChipSpinner's InfiniteSpinner: size small (12px), speed
// slow (2.4s a turn), color blue (the arcs in content-hgl-1 over a .25
// track), noMargin (viewBox 0 0 78 78 around the r=35 ring).
func nuggetSpinner(gtx layout.Context, d int, arcCol, trackCol color.NRGBA) {
	sc := float32(d) / 78
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		s := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		s.Pop()
	}
	track := trackCol
	track.A = uint8(float32(track.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	t := float64(gtx.Now.UnixMilli()%2400) / 2400
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	arc(base+float32(2*math.Pi*ccBezier(.25, .29, .54, .86, t)), dash, arcCol)
	arc(base+float32(2*math.Pi*ccBezier(.41, .24, .64, .69, t)), dash, arcCol)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}
