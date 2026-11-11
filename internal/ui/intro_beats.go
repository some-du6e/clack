package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The beats' illustrations, drawn from Clack's own pieces at a smaller
// scale: the aubergine window with its rail, sidebar and message pane,
// the composer, Preferences' plugin rows and their switches, a workspace
// tile and channel names. Every piece comes in on its own spring,
// staggered, and multiplies the stage's look (its slide and fade).

var (
	introWhite  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	introBlack  = color.NRGBA{A: 0xff}
	miniFg      = hexc(0x1d1c1d)
	miniMuted   = hexc(0x616061)
	miniSubtle  = hexc(0x868686)
	miniBorder  = hexc(0xe2e1e2)
	miniLink    = hexc(0x1264a3)
	miniGreen   = hexc(0x007a5a)
	miniSwitch  = hexc(0x2bac76)
	miniChromeA = hexc(0x4a154b)
	miniChromeB = hexc(0x2c0a2d)
	miniActive  = hexc(0xf9edff)
	miniActiveT = hexc(0x39063a)
)

// avatar colours for the illustration's people
var introPeople = map[string]color.NRGBA{
	"Ava": hexc(0xe8912d), "Leo": hexc(0x2bac76), "Zara": hexc(0x1d9bd1),
	"Orpheus": hexc(0xe01e5a), "Malik": hexc(0x9b59b6), "You": hexc(0x7b5cff), "orphie": hexc(0xe01e5a),
}

// stageKit places illustration pieces in stage units (dp at the window's
// scale, from the stage's top-left).
type stageKit struct {
	f    *introFrame
	o    image.Point
	w, h float32 // the stage in units
}

func (f *introFrame) kit(g introGeom) stageKit {
	return stageKit{f: f, o: g.stage.Min, w: float32(g.stage.Dx()) / f.u, h: float32(g.stage.Dy()) / f.u}
}

func (k stageKit) pt(x, y float32) image.Point { return k.o.Add(image.Pt(k.f.d(x), k.f.d(y))) }
func (k stageKit) fpt(x, y float32) f32.Point {
	return f32.Pt(float32(k.o.X)+k.f.df(x), float32(k.o.Y)+k.f.df(y))
}
func (k stageKit) rect(x, y, w, h float32) image.Rectangle {
	return image.Rectangle{Min: k.pt(x, y), Max: k.pt(x+w, y+h)}
}
func center(r image.Rectangle) f32.Point {
	return f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
}

// fade is e at a parent's opacity.
func (e fx) fade(a float32) fx { e.a *= a; return e }

// rise is a piece arriving: fading in, rising rise device px and growing
// from s0 on the bouncy spring.
func (f *introFrame) rise(start time.Time, rise, s0 float32) fx {
	if f.reduced {
		return fx{a: float32(clamp01(f.mc.spring(springSmooth, start))), s: 1}
	}
	e := float32(f.mc.spring(springBouncy, start))
	return fx{a: float32(clamp01(f.mc.spring(springSnappy, start))), dy: (1 - e) * rise, s: s0 + (1-s0)*e}
}

// slideX is a piece arriving from dx device px to the side.
func (f *introFrame) slideX(start time.Time, dx float32) fx {
	if f.reduced {
		return fx{a: float32(clamp01(f.mc.spring(springSmooth, start))), s: 1}
	}
	e := float32(f.mc.spring(springBouncy, start))
	return fx{a: float32(clamp01(f.mc.spring(springSnappy, start))), dx: (1 - e) * dx, s: 1}
}

// transform pushes e's offset and scale (not its opacity: the pieces
// under it take that) and returns the pop.
func (f *introFrame) transform(e fx, pivot f32.Point) func() {
	f.record("group", e)
	aff := f32.Affine2D{}
	if e.s != 1 && e.s > 0 {
		aff = aff.Scale(pivot, f32.Pt(e.s, e.s))
	}
	t := op.Affine(aff.Offset(f32.Pt(e.dx, e.dy))).Push(f.gtx.Ops)
	return t.Pop
}

// card is a floating panel: a soft shadow below it on the dark backdrop
// (filled under the card too, so it has no hard edge) and the panel.
func (f *introFrame) card(r image.Rectangle, rad int, bg color.NRGBA) {
	sh := r.Add(image.Pt(0, f.d(8)))
	c := alphaMul(introBlack, 0.36)
	theme.RRect(f.gtx, sh, rad, c)
	theme.Shadow(f.gtx, sh, rad, f.d(30), c)
	theme.RRect(f.gtx, r, rad, bg)
}

// avatar is a person's initial on their colour.
func (f *introFrame) avatar(r image.Rectangle, name string) {
	c, ok := introPeople[name]
	if !ok {
		c = miniMuted
	}
	theme.RRect(f.gtx, r, r.Dx()/4, c)
	s := float32(r.Dy()) / f.u * 0.5
	ch := string([]rune(name)[0])
	if name == "orphie" {
		ch = "O"
	}
	tw := f.measure(s, theme.Black, ch)
	f.label(image.Pt(r.Min.X+(r.Dx()-tw)/2, r.Min.Y+(r.Dy()-f.d(s*1.25))/2), s, theme.Black, introWhite, ch)
}

// msgRow is a message: avatar, name, time, text (units from the kit).
func (k stageKit) msgRow(x, y float32, name, when, text string, nameA float32) {
	f := k.f
	f.avatar(k.rect(x, y, 30, 30), name)
	nx := k.pt(x+40, y-1)
	nw := f.label(nx, 13.5, theme.Black, alphaMul(miniFg, nameA), name)
	f.label(image.Pt(nx.X+nw+f.d(7), nx.Y+f.d(2)), 11, theme.Regular, miniSubtle, when)
	f.label(k.pt(x+40, y+17), 13, theme.Regular, miniFg, text)
}

func (in *intro) drawStage(f *introFrame, b int, g introGeom, start time.Time, stage fx) {
	switch b {
	case beatNative:
		in.drawNativeStage(f, g, start, stage)
	case beatSlack:
		in.drawSlackStage(f, g, start, stage)
	case beatPlugins:
		in.drawPluginsStage(f, g, start, stage)
	case beatHackClub:
		in.drawHackClubStage(f, g, start, stage)
	}
}

// ---- native and fast: Clack's window assembling, switching instantly ----

var nativeChannels = []string{"announcements", "ship", "lounge", "code", "scrapbook", "hq"}

var nativeMessages = [3][3][3]string{
	{{"Zara", "2:41 PM", "shipped the arcade leaderboard!"}, {"Orpheus", "2:43 PM", "reviewing it now"}, {"Malik", "2:44 PM", "it loads before I can blink"}},
	{{"Ava", "3:02 PM", "who's around for a call later?"}, {"Leo", "3:05 PM", "me, after dinner"}, {"Zara", "3:06 PM", "count me in"}},
	{{"Malik", "4:10 PM", "anyone tried the new Gio release?"}, {"Orpheus", "4:12 PM", "text shaping got a lot faster"}, {"Leo", "4:15 PM", "and the window resizes smoothly"}},
}

func (in *intro) drawNativeStage(f *introFrame, g introGeom, start time.Time, stage fx) {
	k := f.kit(g)
	wx, wy, ww, wh := (k.w-610)/2, float32(2), float32(610), float32(324)
	win := k.rect(wx, wy, ww, wh)
	we := f.rise(start, f.df(26), 0.965).mul(stage)
	pop := f.transform(fx{dx: we.dx, dy: we.dy, s: we.s}, center(win))
	defer pop()
	A := we.a
	// the frame: Clack's aubergine chrome and the search field
	f.with("native-window", fx{a: A, s: 1}, f32.Point{}, func() {
		rad := f.d(12)
		f.card(win, rad, miniChromeA)
		func() {
			defer clip.UniformRRect(win, rad).Push(f.gtx.Ops).Pop()
			paint.LinearGradientOp{Stop1: layout.FPt(win.Min), Color1: miniChromeA, Stop2: layout.FPt(win.Max), Color2: miniChromeB}.Add(f.gtx.Ops)
			paint.PaintOp{}.Add(f.gtx.Ops)
		}()
		// a hairline of light: the dark window against the dark backdrop
		theme.Border(f.gtx, win, rad, max(1, f.d(1)), alphaMul(introWhite, 0.12))
		sr := k.rect(wx+ww*0.3, wy+8, ww*0.4, 17)
		theme.RRect(f.gtx, sr, f.d(5), alphaMul(miniActive, 0.2))
		at(f.gtx, k.pt(wx+ww*0.3+8, wy+10.5), image.Pt(f.d(12), f.d(12)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("search")(gtx, f.idp(12), alphaMul(introWhite, 0.8))
		})
		f.label(k.pt(wx+ww*0.3+25, wy+10), 10.5, theme.Regular, alphaMul(introWhite, 0.75), "Search Hack Club")
	})
	// the rail: the workspace, then Home, DMs, Activity
	for i, name := range []string{"", "home", "dms", "bell"} {
		e := f.pop(start.Add(msec(60+40*float64(i))), 0.55, springPop).fade(A)
		r := k.rect(wx+11, wy+36+float32(i)*40, 26, 26)
		f.with("native-rail", e, center(r), func() {
			if i == 0 {
				theme.RRect(f.gtx, r, f.d(7), hexc(0xec3750))
				tw := f.measure(15, theme.Black, "h")
				f.label(image.Pt(r.Min.X+(r.Dx()-tw)/2, r.Min.Y+(r.Dy()-f.d(15*1.25))/2-f.d(1)), 15, theme.Black, introWhite, "h")
				return
			}
			if i == 1 {
				theme.RRect(f.gtx, r, f.d(7), alphaMul(miniActive, 0.25))
			}
			ir := r.Inset(f.d(5))
			at(f.gtx, ir.Min, ir.Size(), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(name)(gtx, f.idp(16), introWhite)
			})
		})
	}
	// the sidebar slides in, its rows after it
	sx := wx + 48
	sb := k.rect(sx, wy+32, 158, wh-38)
	se := f.slideX(start.Add(msec(90)), -f.df(30)).fade(A)
	f.with("native-sidebar", se, f32.Point{}, func() {
		theme.RRect(f.gtx, sb, f.d(8), alphaMul(miniActive, 0.07))
		f.label(k.pt(sx+12, wy+42), 13.5, theme.Black, introWhite, "Hack Club")
		at(f.gtx, k.pt(sx+86, wy+44), image.Pt(f.d(12), f.d(12)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("chevron-down")(gtx, f.idp(11), alphaMul(introWhite, 0.8))
		})
	})
	// which channel is open: #ship, then #lounge, then #code
	t1, t2 := start.Add(msec(1750)), start.Add(msec(3050))
	pos := 1 + f.mc.spring(springBouncy, t1) + f.mc.spring(springBouncy, t2)
	rowY := func(i float64) float32 { return wy + 72 + float32(i)*24 }
	// the selected row's pill glides between rows
	if f.reduced {
		for i, w := range [3]float64{1 - clamp01(f.mc.spring(springSmooth, t1)), clamp01(f.mc.spring(springSmooth, t1)) - clamp01(f.mc.spring(springSmooth, t2)), clamp01(f.mc.spring(springSmooth, t2))} {
			if w > 0.004 {
				e := f.slideX(start.Add(msec(120+40*float64(i+1))), 0).fade(A * float32(w))
				r := k.rect(sx+6, rowY(float64(i+1))-3, 146, 22)
				f.with("native-active", e, f32.Point{}, func() { theme.RRect(f.gtx, r, f.d(6), miniActive) })
			}
		}
	} else {
		e := f.slideX(start.Add(msec(120+40)), -f.df(18)).fade(A)
		e.dy = f.df(24) * float32(pos-1)
		r := k.rect(sx+6, rowY(1)-3, 146, 22)
		f.with("native-active", e, f32.Point{}, func() { theme.RRect(f.gtx, r, f.d(6), miniActive) })
	}
	for i, ch := range nativeChannels {
		e := f.slideX(start.Add(msec(120+40*float64(i))), -f.df(18)).fade(A)
		y := rowY(float64(i))
		active := math.Abs(pos-float64(i)) < 0.5
		f.with("native-row", e, f32.Point{}, func() {
			c, w := alphaMul(introWhite, 0.78), theme.Regular
			if i == 0 {
				c, w = introWhite, theme.Black // unread
			}
			if active && !f.reduced {
				c, w = miniActiveT, theme.Black
			}
			at(f.gtx, k.pt(sx+14, y+1.5), image.Pt(f.d(12), f.d(12)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("hash")(gtx, f.idp(12), c)
			})
			f.label(k.pt(sx+31, y), 12.5, w, c, ch)
			if ch == "scrapbook" {
				br := k.rect(sx+126, y+0.5, 20, 15)
				theme.RRect(f.gtx, br, br.Dy()/2, hexc(0xeabdfb))
				tw := f.measure(10, theme.Black, "2")
				f.label(image.Pt(br.Min.X+(br.Dx()-tw)/2, br.Min.Y+(br.Dy()-f.d(12.5))/2), 10, theme.Black, miniActiveT, "2")
			}
		})
	}
	// the message pane
	px := sx + 164
	pane := k.rect(px, wy+32, ww-(px-wx)-6, wh-38)
	pe := f.rise(start.Add(msec(110)), f.df(10), 1).fade(A)
	f.with("native-pane", pe, f32.Point{}, func() {
		theme.RRect(f.gtx, pane, f.d(9), introWhite)
	})
	// its content: one channel, then the next, swapped in a blink
	ws := [3]float64{1 - clamp01(f.mc.spring(springSnappy, t1)), clamp01(f.mc.spring(springSnappy, t1)) - clamp01(f.mc.spring(springSnappy, t2)), clamp01(f.mc.spring(springSnappy, t2))}
	for c := 0; c < 3; c++ {
		if ws[c] <= 0.004 {
			continue
		}
		base := start.Add(msec(170))
		switch c {
		case 1:
			base = t1
		case 2:
			base = t2
		}
		name := nativeChannels[c+1]
		he := f.enter(base, f.df(6), springSmooth, false).fade(A * float32(ws[c]))
		f.with("native-header", he, f32.Point{}, func() {
			f.label(k.pt(px+16, wy+44), 14.5, theme.Black, miniFg, "# "+name)
			theme.Fill(f.gtx, k.rect(px, wy+70, ww-(px-wx)-6, 0.6), miniBorder)
		})
		for i, m := range nativeMessages[c] {
			e := f.enter(base.Add(msec(60+60*float64(i))), f.df(14), springBouncy, false).fade(A * float32(ws[c]))
			f.with("native-message", e, f32.Point{}, func() {
				k.msgRow(px+16, wy+86+float32(i)*58, m[0], m[1], m[2], 1)
			})
		}
	}
	// a pointer clicks #lounge, then #code
	if !f.reduced {
		appear := start.Add(msec(1150))
		ca := float32(clamp01(f.mc.spring(springSmooth, appear))) * A
		if ca > 0.004 {
			p0 := k.fpt(px+150, wy+250)
			p1 := k.fpt(sx+70, rowY(2)+9)
			p2 := k.fpt(sx+60, rowY(3)+9)
			m1 := float32(f.mc.spring(springSmooth, start.Add(msec(1300))))
			m2 := float32(f.mc.spring(springSmooth, start.Add(msec(2650))))
			p := p0.Add(p1.Sub(p0).Mul(m1)).Add(p2.Sub(p1).Mul(m2))
			for _, click := range []time.Time{t1, t2} {
				if c := f.now.Sub(click.Add(-msec(60))).Seconds() / 0.5; c > 0 && c < 1 {
					f.mc.moving = true
					ringAt(f, p, f.df(4+16*float32(c)), alphaMul(introWhite, 0.55*float32(1-c)*ca), f.df(1.6))
				}
			}
			press := float32(1)
			for _, click := range []time.Time{t1, t2} {
				if c := f.now.Sub(click.Add(-msec(70))).Seconds(); c > 0 && c < 0.16 {
					press = 0.86
				}
			}
			f.record("native-pointer", fx{a: ca, dx: p.X, dy: p.Y, s: press})
			drawPointer(f, p, f.df(17)*press, ca)
		}
	}
}

// ringAt strokes a circle (centre c, radius r).
func ringAt(f *introFrame, c f32.Point, r float32, col color.NRGBA, w float32) {
	var p clip.Path
	p.Begin(f.gtx.Ops)
	p.MoveTo(f32.Pt(c.X+r, c.Y))
	p.ArcTo(c, c, 2*math.Pi)
	p.Close()
	paint.FillShape(f.gtx.Ops, col, clip.Stroke{Path: p.End(), Width: w}.Op())
}

// drawPointer is a mouse pointer with its tip at p, h tall.
func drawPointer(f *introFrame, p f32.Point, h, a float32) {
	pts := []f32.Point{{X: 0, Y: 0}, {X: 0, Y: 0.86}, {X: 0.22, Y: 0.66}, {X: 0.38, Y: 1}, {X: 0.5, Y: 0.95}, {X: 0.35, Y: 0.62}, {X: 0.62, Y: 0.62}}
	shape := func() clip.PathSpec {
		var path clip.Path
		path.Begin(f.gtx.Ops)
		for i, q := range pts {
			v := f32.Pt(p.X+q.X*h, p.Y+q.Y*h)
			if i == 0 {
				path.MoveTo(v)
			} else {
				path.LineTo(v)
			}
		}
		path.Close()
		return path.End()
	}
	paint.FillShape(f.gtx.Ops, alphaMul(introBlack, 0.35*a), clip.Stroke{Path: shape(), Width: h * 0.16}.Op())
	paint.FillShape(f.gtx.Ops, alphaMul(introWhite, a), clip.Outline{Path: shape()}.Op())
	paint.FillShape(f.gtx.Ops, alphaMul(miniFg, a), clip.Stroke{Path: shape(), Width: h * 0.06}.Op())
}

// ---- your Slack 1:1: a message typed, sent and reacted to ----

const slackTyped = "count me in, reviewing it now"

// typedAt is when each character of slackTyped lands after the typing
// starts: a person's rhythm (quick runs, a beat after the comma).
func typedAt(i int) time.Duration {
	d := time.Duration(0)
	for j := 0; j < i; j++ {
		h := uint32(j*2654435761) >> 27 // 0..31
		d += msec(34 + float64(h))
		if slackTyped[j] == ',' {
			d += msec(170)
		}
		if slackTyped[j] == ' ' {
			d += msec(16)
		}
	}
	return d
}

func (in *intro) drawSlackStage(f *introFrame, g introGeom, start time.Time, stage fx) {
	k := f.kit(g)
	cw, ch := float32(600), float32(330)
	cx := (k.w - cw) / 2
	card := k.rect(cx, 0, cw, ch)
	ce := f.rise(start, f.df(26), 0.97).mul(stage)
	pop := f.transform(fx{dx: ce.dx, dy: ce.dy, s: ce.s}, center(card))
	defer pop()
	A := ce.a
	f.with("slack-card", fx{a: A, s: 1}, f32.Point{}, func() {
		f.card(card, f.d(14), introWhite)
	})
	he := f.enter(start.Add(msec(70)), f.df(8), springSmooth, false).fade(A)
	f.with("slack-header", he, f32.Point{}, func() {
		w := f.label(k.pt(cx+20, 13), 16, theme.Black, miniFg, "# lounge")
		f.label(image.Pt(k.pt(cx+20, 0).X+w+f.d(12), k.pt(0, 16).Y), 12.5, theme.Regular, miniSubtle, "Ship it, then show it")
		theme.Fill(f.gtx, k.rect(cx, 44, cw, 0.7), miniBorder)
	})
	msgs := [2][3]string{{"Ava", "2:41 PM", "anyone shipping tonight?"}, {"Leo", "2:42 PM", "pushing the arcade build in 10"}}
	for i, m := range msgs {
		e := f.enter(start.Add(msec(130+60*float64(i))), f.df(14), springBouncy, false).fade(A)
		f.with("slack-message", e, f32.Point{}, func() { k.msgRow(cx+20, 60+float32(i)*54, m[0], m[1], m[2], 1) })
	}
	// the composer
	cy := ch - 92
	box := k.rect(cx+16, cy, cw-32, 70)
	typeAt := start.Add(msec(750))
	n := 0
	for n < len(slackTyped) && !f.now.Before(typeAt.Add(typedAt(n+1))) {
		n++
	}
	sendAt := typeAt.Add(typedAt(len(slackTyped)) + msec(330))
	if n < len(slackTyped) && !f.now.Before(typeAt) {
		f.mc.wakeAt(typeAt.Add(typedAt(n + 1)))
	}
	if f.reduced {
		n = 0 // no typing: the message fades in sent
	}
	sent := !f.now.Before(sendAt)
	be := f.enter(start.Add(msec(200)), f.df(12), springSmooth, false).fade(A)
	f.with("slack-composer", be, f32.Point{}, func() {
		theme.RRect(f.gtx, box, f.d(9), introWhite)
		focus := float32(clamp01(f.mc.spring(springSnappy, typeAt.Add(-msec(150)))))
		theme.Border(f.gtx, box, f.d(9), max(1, f.d(1)), theme.Mix(hexc(0xbcbcbc), hexc(0x868686), focus))
		// the text, the caret after it
		txt := slackTyped[:n]
		ta := float32(1)
		if sent {
			ta = float32(1 - clamp01(f.mc.spring(springSnappy, sendAt)*1.6))
		}
		tp := k.pt(cx+30, cy+12)
		if n == 0 || ta < 0.01 {
			f.label(tp, 13.5, theme.Regular, miniSubtle, "Message #lounge")
		}
		tw := 0
		if n > 0 && ta > 0.004 {
			tw = f.label(tp, 13.5, theme.Regular, alphaMul(miniFg, ta), txt)
		}
		blink := f.now.Sub(typeAt.Add(typedAt(n))).Milliseconds()/530%2 == 0
		if !f.now.Before(typeAt) && !sent && (n < len(slackTyped) || blink) && !f.reduced {
			theme.Fill(f.gtx, image.Rect(tp.X+tw+f.d(1), tp.Y+f.d(1), tp.X+tw+f.d(1)+max(1, f.d(1.5)), tp.Y+f.d(17)), miniLink)
			if n >= len(slackTyped) {
				f.mc.wakeAt(f.now.Add(msec(530 - float64(f.now.Sub(typeAt.Add(typedAt(n))).Milliseconds()%530))))
			}
		}
		// the toolbar and the send button
		for i, name := range []string{"bold", "italic", "strike", "link"} {
			at(f.gtx, k.pt(cx+30+float32(i)*26, cy+44), image.Pt(f.d(16), f.d(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(name)(gtx, f.idp(15), hexc(0xa0a0a0))
			})
		}
		sb := k.rect(cx+cw-16-46, cy+40, 32, 24)
		armed := float32(0)
		if n > 0 && !sent {
			armed = 1
		}
		if sent {
			armed = float32(1 - clamp01(f.mc.spring(springSmooth, sendAt.Add(msec(120)))))
		}
		ps := float32(1)
		if !f.reduced {
			if d := f.now.Sub(sendAt); d >= -msec(90) && d < 0 {
				ps = 0.88
				f.mc.moving = true
			} else if d >= 0 {
				ps = 0.88 + 0.12*float32(f.mc.spring(springPop, sendAt))
			}
		}
		func() {
			t := op.Affine(f32.Affine2D{}.Scale(center(sb), f32.Pt(ps, ps))).Push(f.gtx.Ops)
			defer t.Pop()
			theme.RRect(f.gtx, sb, f.d(5), theme.Mix(introWhite, miniGreen, armed))
			ir := image.Rectangle{Min: center(sb).Round().Sub(image.Pt(f.d(7), f.d(7)))}
			at(f.gtx, ir.Min, image.Pt(f.d(14), f.d(14)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("send")(gtx, f.idp(14), theme.Mix(hexc(0xb0b0b0), introWhite, armed))
			})
		}()
	})
	// the sent message lands in the list
	slotY := float32(60 + 2*54)
	landAt := sendAt
	if f.reduced {
		landAt = start.Add(msec(1400))
	}
	if !f.now.Before(landAt) {
		e := f.enter(landAt, k.f.df(cy-slotY-4), springBouncy, false).fade(A)
		f.with("slack-sent", e, f32.Point{}, func() { k.msgRow(cx+20, slotY, "You", "2:42 PM", slackTyped, 1) })
		// a reaction, then a second person on it
		ra := landAt.Add(msec(620))
		re := f.pop(ra, 0.45, springPop).fade(A)
		pr := k.rect(cx+60, slotY+40, 46, 22)
		f.with("slack-reaction", re, f32.Pt(float32(pr.Min.X), center(pr).Y), func() {
			theme.RRect(f.gtx, pr, pr.Dy()/2, hexc(0xe8f5fa))
			theme.Border(f.gtx, pr, pr.Dy()/2, max(1, f.d(1)), hexc(0x1d9bd1))
			at(f.gtx, k.pt(cx+68, slotY+43.5), image.Pt(f.d(15), f.d(15)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("thumbs-up")(gtx, f.idp(15), miniLink)
			})
			// the count rolls from 1 to 2
			roll := float32(clamp01(f.mc.spring(springBouncy, ra.Add(msec(560)))))
			if f.reduced {
				roll = float32(clamp01(f.mc.spring(springSmooth, ra.Add(msec(560)))))
			}
			cp := k.pt(cx+88, slotY+44)
			func() {
				defer clip.Rect(pr).Push(f.gtx.Ops).Pop()
				dy := 0
				if !f.reduced {
					dy = f.d(12 * roll)
				}
				if roll < 0.999 {
					f.label(image.Pt(cp.X, cp.Y-dy), 12, theme.Black, alphaMul(miniLink, 1-roll), "1")
				}
				if roll > 0.001 {
					f.label(image.Pt(cp.X, cp.Y+f.d(12)-dy), 12, theme.Black, alphaMul(miniLink, roll), "2")
				}
			}()
		})
	}
	// someone else is typing
	tyAt := landAt.Add(msec(1250))
	te := f.enter(tyAt, f.df(4), springSmooth, false).fade(A)
	f.with("slack-typing", te, f32.Point{}, func() {
		p := k.pt(cx+18, ch-17)
		dots := f.d(20)
		for i := 0; i < 3; i++ {
			lift := float32(0)
			if !f.reduced {
				ph := f.now.Sub(tyAt).Seconds()*2*math.Pi*1.35 - float64(i)*0.75
				lift = float32(math.Max(0, math.Sin(ph))) * f.df(2.6)
				f.mc.moving = true
			}
			theme.Circle(f.gtx, image.Pt(p.X+f.d(3)+i*f.d(6), p.Y+f.d(7)-int(lift)), max(2, f.d(4)), miniSubtle)
		}
		f.label(image.Pt(p.X+dots, p.Y), 11.5, theme.Regular, miniMuted, "Leo is typing")
	})
}

// ---- plugins unmodified: switches flip, the conversation changes ----

type pluginRow struct{ name, desc, system string }

var introPlugins = []pluginRow{
	{"ChatTabs", "Conversations as tabs", "Slick"},
	{"Nicknames", "Your own names for people", "Taut"},
	{"ClearURLs", "Tracking stripped from links", "Taut"},
}

func (in *intro) drawPluginsStage(f *introFrame, g introGeom, start time.Time, stage fx) {
	k := f.kit(g)
	pw, gap := float32(310), float32(22)
	lx := (k.w - (2*pw + gap)) / 2
	rx := lx + pw + gap
	list := k.rect(lx, 6, pw, 318)
	conv := k.rect(rx, 6, pw, 318)
	le := f.rise(start, f.df(24), 0.97).mul(stage)
	flips := [3]time.Time{start.Add(msec(850)), start.Add(msec(1650)), start.Add(msec(2450))}
	func() {
		pop := f.transform(fx{dx: le.dx, dy: le.dy, s: le.s}, center(list))
		defer pop()
		A := le.a
		f.with("plugins-card", fx{a: A, s: 1}, f32.Point{}, func() {
			f.card(list, f.d(14), introWhite)
			f.label(k.pt(lx+20, 18), 16, theme.Black, miniFg, "Plugins")
			f.label(k.pt(lx+20, 41), 12, theme.Regular, miniSubtle, "Preferences · Installed")
			theme.Fill(f.gtx, k.rect(lx, 66, pw, 0.7), miniBorder)
		})
		for i, p := range introPlugins {
			y := 80 + float32(i)*80
			e := f.enter(start.Add(msec(90+60*float64(i))), f.df(14), springBouncy, false).fade(A)
			f.with("plugins-row", e, f32.Point{}, func() {
				f.label(k.pt(lx+20, y), 14.5, theme.Black, miniFg, p.name)
				nw := f.measure(14.5, theme.Black, p.name)
				// the plugin system, as Preferences shows it
				chip := p.system
				cwid := f.measure(10, theme.Black, chip) + f.d(12)
				cr := image.Rectangle{Min: k.pt(lx+20, y+2.5)}
				cr.Min.X += nw + f.d(8)
				cr.Max = cr.Min.Add(image.Pt(cwid, f.d(16)))
				bg, fg := hexc(0xf1ecfe), hexc(0x5b3fd6)
				if chip == "Taut" {
					bg, fg = hexc(0xfdf0e0), hexc(0xa85b00)
				}
				theme.RRect(f.gtx, cr, cr.Dy()/2, bg)
				f.label(image.Pt(cr.Min.X+f.d(6), cr.Min.Y+(cr.Dy()-f.d(12.5))/2), 10, theme.Black, fg, chip)
				f.label(k.pt(lx+20, y+23), 12.5, theme.Regular, miniMuted, p.desc)
				if i < len(introPlugins)-1 {
					theme.Fill(f.gtx, k.rect(lx+20, y+58, pw-40, 0.6), miniBorder)
				}
			})
			// the switch
			se := f.enter(start.Add(msec(120+60*float64(i))), f.df(14), springBouncy, false).fade(A)
			sr := k.rect(lx+pw-20-34, y+8, 34, 20)
			f.with("plugins-switch", se, f32.Point{}, func() { in.drawSwitch(f, sr, flips[i]) })
		}
	}()
	// the conversation the plugins change
	ce := f.rise(start.Add(msec(60)), f.df(24), 0.97).mul(stage)
	pop := f.transform(fx{dx: ce.dx, dy: ce.dy, s: ce.s}, center(conv))
	defer pop()
	A := ce.a
	f.with("plugins-conv", fx{a: A, s: 1}, f32.Point{}, func() {
		f.card(conv, f.d(14), introWhite)
		f.label(k.pt(rx+20, 18), 16, theme.Black, miniFg, "# lounge")
		theme.Fill(f.gtx, k.rect(rx, 50, pw, 0.7), miniBorder)
	})
	// ChatTabs: a tab bar slides in under the header, the messages make room
	tabsOn := f.mc.spring(springBouncy, flips[0].Add(msec(160)))
	if f.reduced {
		tabsOn = clamp01(f.mc.spring(springSmooth, flips[0].Add(msec(160))))
	}
	shift := float32(0)
	if !f.reduced {
		shift = f.df(36) * float32(tabsOn)
	}
	for i, t := range []string{"lounge", "ship", "code"} {
		e := f.enter(flips[0].Add(msec(160+60*float64(i))), -f.df(10), springBouncy, false).fade(A)
		x := rx + 14 + float32(i)*92
		r := k.rect(x, 58, 86, 28)
		f.with("plugins-tab", e, f32.Point{}, func() {
			bg := hexc(0xf4f4f4)
			if i == 0 {
				bg = hexc(0xf1ecfe)
			}
			theme.RRect(f.gtx, r, f.d(7), bg)
			c := miniMuted
			if i == 0 {
				c = hexc(0x5b3fd6)
			}
			f.label(k.pt(x+10, 64), 12, theme.Black, c, "# "+t)
		})
	}
	// the messages
	nick := float32(clamp01(f.mc.spring(springSmooth, flips[1].Add(msec(180)))))
	me := f.enter(start.Add(msec(160)), f.df(12), springBouncy, false).fade(A)
	me.dy += shift
	f.with("plugins-message", me, f32.Point{}, func() {
		y := float32(64)
		f.avatar(k.rect(rx+20, y, 30, 30), "Orpheus")
		np := k.pt(rx+60, y-1)
		// Nicknames: Orpheus becomes orphie
		if 1-nick > 0.004 {
			f.label(image.Pt(np.X, np.Y-f.d(6*nick)), 13.5, theme.Black, alphaMul(miniFg, 1-nick), "Orpheus")
		}
		if nick > 0.004 {
			hl := image.Rect(np.X-f.d(3), np.Y-f.d(1), np.X+f.measure(13.5, theme.Black, "orphie")+f.d(3), np.Y+f.d(18))
			glow := float32(math.Max(0, 1-math.Abs(float64(f.now.Sub(flips[1].Add(msec(500))).Seconds()))*1.6))
			if glow > 0.004 {
				theme.RRect(f.gtx, hl, f.d(4), alphaMul(hexc(0xffe9a8), glow))
				f.mc.moving = true
			}
			f.label(image.Pt(np.X, np.Y+f.d(6*(1-nick))), 13.5, theme.Black, alphaMul(miniFg, nick), "orphie")
		}
		f.label(k.pt(rx+60, y+17), 13, theme.Regular, miniFg, "the arcade build is live")
	})
	m2 := f.enter(start.Add(msec(220)), f.df(12), springBouncy, false).fade(A)
	m2.dy += shift
	f.with("plugins-message", m2, f32.Point{}, func() {
		y := float32(128)
		k.msgRow(rx+20, y, "Ava", "5:20 PM", "release notes:", 1)
		// ClearURLs: the tracking part turns red and is gone
		lp := k.pt(rx+60, y+36)
		host := "hackclub.com/arcade"
		tail := "?utm_source=slack"
		hw := f.label(lp, 13, theme.Regular, miniLink, host)
		redden := float32(clamp01(f.mc.spring(springSnappy, flips[2].Add(msec(160)))))
		cut := float32(clamp01(f.mc.spring(springSmooth, flips[2].Add(msec(520)))))
		full := f.measure(13, theme.Regular, tail)
		vis := full
		if !f.reduced {
			vis = int(float32(full) * (1 - cut))
		}
		ta := 1 - cut
		if vis > 0 && ta > 0.004 {
			r := image.Rect(lp.X+hw, lp.Y-f.d(2), lp.X+hw+vis, lp.Y+f.d(19))
			func() {
				defer clip.Rect(r).Push(f.gtx.Ops).Pop()
				if redden > 0.004 {
					theme.RRect(f.gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+full, r.Max.Y), f.d(3), alphaMul(hexc(0xfde8ec), redden*ta))
				}
				f.label(image.Pt(lp.X+hw, lp.Y), 13, theme.Regular, alphaMul(theme.Mix(miniLink, hexc(0xe01e5a), redden), ta), tail)
				if redden > 0.004 {
					theme.Fill(f.gtx, image.Rect(r.Min.X, lp.Y+f.d(8.5), r.Min.X+int(float32(full)*redden), lp.Y+f.d(8.5)+max(1, f.d(1.2))), alphaMul(hexc(0xe01e5a), ta))
				}
			}()
		}
		// the link's underline follows its new length
		theme.Fill(f.gtx, image.Rect(lp.X, lp.Y+f.d(17), lp.X+hw+int(float32(vis)*(1-redden)), lp.Y+f.d(17)+max(1, f.d(0.8))), alphaMul(miniLink, 0.5))
	})
	m3 := f.enter(start.Add(msec(280)), f.df(12), springBouncy, false).fade(A)
	m3.dy += shift
	f.with("plugins-message", m3, f32.Point{}, func() {
		k.msgRow(rx+20, 196, "Zara", "5:24 PM", "same plugins as in the browser", 1)
	})
}

// drawSwitch is Slack's switch turning on at flip: the knob springs
// across, the track turns green, a ring of light leaves the knob.
// Reduced motion: the two states cross-fade.
func (in *intro) drawSwitch(f *introFrame, r image.Rectangle, flip time.Time) {
	h := r.Dy()
	kd := h - f.d(4)
	off, on := r.Min.X+f.d(2), r.Max.X-f.d(2)-kd
	draw := func(t float32, a float32) {
		theme.RRect(f.gtx, r, h/2, alphaMul(theme.Mix(hexc(0xc8c8c8), miniSwitch, t), a))
		x := float32(off) + float32(on-off)*t
		c := image.Pt(int(math.Round(float64(x)))+kd/2, r.Min.Y+h/2)
		theme.Circle(f.gtx, c.Add(image.Pt(0, max(1, f.d(0.7)))), kd, alphaMul(introBlack, 0.12*a))
		theme.Circle(f.gtx, c, kd, alphaMul(introWhite, a))
	}
	if f.reduced {
		t := float32(clamp01(f.mc.spring(springSmooth, flip)))
		draw(0, 1-t)
		draw(1, t)
		return
	}
	t := float32(f.mc.spring(springBouncy, flip))
	tint := float32(clamp01(f.mc.spring(springSnappy, flip)))
	theme.RRect(f.gtx, r, h/2, theme.Mix(hexc(0xc8c8c8), miniSwitch, tint))
	x := float32(off) + float32(on-off)*t
	c := f32.Pt(x+float32(kd)/2, float32(r.Min.Y)+float32(h)/2)
	if p := f.now.Sub(flip.Add(msec(80))).Seconds() / 0.6; p > 0 && p < 1 {
		f.mc.moving = true
		ringAt(f, f32.Pt(float32(on)+float32(kd)/2, c.Y), float32(kd)/2+f.df(12)*float32(p), alphaMul(miniSwitch, 0.5*float32(1-p)), f.df(1.8))
	}
	// the knob stretches a little while it travels
	stretch := float32(math.Min(1, math.Abs(float64(t)-float64(tint)))) * f.df(5)
	kr := image.Rect(int(c.X-float32(kd)/2-stretch/2), int(c.Y)-kd/2, int(c.X+float32(kd)/2+stretch/2), int(c.Y)-kd/2+kd)
	theme.RRect(f.gtx, kr.Add(image.Pt(0, max(1, f.d(0.7)))), kd/2, alphaMul(introBlack, 0.12))
	theme.RRect(f.gtx, kr, kd/2, introWhite)
}

// ---- made for Hack Club: the workspace, a burst, its channels ----

var hcChips = []struct {
	name string
	x, y float32 // from the stage's centre, units
	dot  bool
}{
	{"#ship", -238, -92, true}, {"#scrapbook", -262, 22, false}, {"#code", -150, 112, false},
	{"#lounge", 200, -104, false}, {"#hq", 262, 4, true}, {"#arcade", 176, 108, false}, {"#announcements", -8, -146, false},
}

var hcConfetti = []color.NRGBA{hexc(0xec3750), hexc(0xff8c37), hexc(0xf1c40f), hexc(0x33d6a6), hexc(0x5bc0de), hexc(0x338eda), hexc(0xa633d6)}

func (in *intro) drawHackClubStage(f *introFrame, g introGeom, start time.Time, stage fx) {
	k := f.kit(g)
	c := k.fpt(k.w/2, k.h/2-8)
	ts := f.df(96)
	tile := image.Rect(int(c.X-ts/2), int(c.Y-ts/2), int(c.X+ts/2), int(c.Y+ts/2))
	pop := f.transform(fx{dx: stage.dx, dy: stage.dy, s: 1}, c)
	defer pop()
	A := stage.a
	// the burst, behind the tile, kept to the stage (it never rains on
	// the line under it)
	burstAt := start.Add(msec(220))
	if bt := f.now.Sub(burstAt).Seconds(); bt > 0 && bt < 1.4 && !f.reduced {
		f.mc.moving = true
		cl := clip.Rect(image.Rect(g.stage.Min.X-f.d(60), g.stage.Min.Y-f.d(40), g.stage.Max.X+f.d(60), g.stage.Max.Y+f.d(6))).Push(f.gtx.Ops)
		for i := 0; i < 46; i++ {
			h := uint32(i*2654435761 + 12345)
			ang := float64(h%6283) / 1000
			spd := 360 + float64((h>>13)%300)
			vx, vy := math.Cos(ang)*spd, math.Sin(ang)*spd*0.8-140
			const drag, grav = 2.3, 540.0
			e := (1 - math.Exp(-drag*bt)) / drag
			x := vx * e
			y := vy*e + grav/drag*(bt-e)
			life := 1 - clamp01((bt-0.75)/0.6)
			if life <= 0.004 {
				continue
			}
			rot := float32(ang*3 + bt*float64(4+int(h%5)))
			p := c.Add(f32.Pt(f.df(float32(x)/1.5), f.df(float32(y)/1.5)))
			w, hh := f.df(9), f.df(4.6)
			col := hcConfetti[i%len(hcConfetti)]
			func() {
				t := op.Affine(f32.Affine2D{}.Rotate(f32.Point{}, rot).Offset(p)).Push(f.gtx.Ops)
				defer t.Pop()
				r := image.Rect(int(-w/2), int(-hh/2), int(w/2), int(hh/2))
				theme.RRect(f.gtx, r, max(1, int(hh/3)), alphaMul(col, float32(life)*A))
			}()
		}
		cl.Pop()
	}
	// the workspace tile
	te := f.pop(start.Add(msec(40)), 0.4, springPop).fade(A)
	f.with("hc-tile", te, c, func() {
		theme.Shadow(f.gtx, tile.Add(image.Pt(0, f.d(10))), f.d(26), f.d(30), alphaMul(hexc(0xec3750), 0.35))
		theme.RRect(f.gtx, tile, f.d(26), hexc(0xec3750))
		tw := f.measure(62, theme.Black, "h")
		f.label(image.Pt(tile.Min.X+(tile.Dx()-tw)/2, tile.Min.Y+(tile.Dy()-f.d(62*1.25))/2-f.d(3)), 62, theme.Black, introWhite, "h")
	})
	de := f.enter(start.Add(msec(300)), f.df(8), springSmooth, false).fade(A)
	f.with("hc-domain", de, f32.Point{}, func() {
		s := "hackclub.slack.com"
		w := f.measure(13, theme.Bold, s)
		f.label(image.Pt(int(c.X)-w/2, tile.Max.Y+f.d(18)), 13, theme.Bold, alphaMul(introWhite, 0.5), s)
	})
	// its channels, popping in around it, drifting
	for i, ch := range hcChips {
		when := start.Add(msec(360 + 50*float64(i)))
		e := f.pop(when, 0.6, springPop).fade(A)
		if !f.reduced {
			ph := f.now.Sub(start).Seconds()*1.15 + float64(i)*1.3
			e.dy = f.df(3.2) * float32(math.Sin(ph))
			f.mc.moving = true
		}
		tw := f.measure(13.5, theme.Bold, ch.name)
		pad := f.d(14)
		dotW := 0
		if ch.dot {
			dotW = f.d(14)
		}
		w, h := tw+2*pad+dotW, f.d(32)
		p := c.Add(f32.Pt(f.df(ch.x), f.df(ch.y)))
		r := image.Rect(int(p.X)-w/2, int(p.Y)-h/2, int(p.X)-w/2+w, int(p.Y)-h/2+h)
		f.with("hc-chip", e, p, func() {
			theme.RRect(f.gtx, r, h/2, alphaMul(introWhite, 0.075))
			theme.Border(f.gtx, r, h/2, max(1, f.d(1)), alphaMul(introWhite, 0.16))
			f.label(image.Pt(r.Min.X+pad, r.Min.Y+(h-f.d(13.5*1.25))/2), 13.5, theme.Bold, alphaMul(introWhite, 0.88), ch.name)
			if ch.dot {
				theme.Circle(f.gtx, image.Pt(r.Max.X-pad-f.d(3), r.Min.Y+h/2), f.d(7), hexc(0xff6b7f))
			}
		})
	}
}

// ---- the replay's last beat: you're all set ----

func (in *intro) drawFinalBeat(f *introFrame, g introGeom, start time.Time, parent fx) {
	sz := f.sz
	cx := float32(sz.X) / 2
	cy := float32(sz.Y)*0.40 - f.df(20)
	d := f.df(88)
	c := f32.Pt(cx, cy)
	e := f.pop(start.Add(msec(80)), 0.6, springBouncy).mul(parent)
	f.with("done-badge", e, c, func() {
		ringAt(f, c, d/2, alphaMul(hexc(0x9d8cff), 0.9), f.df(2.4))
		theme.Circle(f.gtx, image.Pt(int(cx), int(cy)), int(d-f.df(4)), alphaMul(hexc(0x7b5cff), 0.16))
		// the check draws itself
		p := float32(clamp01(f.mc.spring(springSmooth, start.Add(msec(300)))))
		if f.reduced {
			p = 1
		}
		pts := [3]f32.Point{c.Add(f32.Pt(-f.df(19), f.df(1))), c.Add(f32.Pt(-f.df(6), f.df(14))), c.Add(f32.Pt(f.df(21), -f.df(13)))}
		l1, l2 := dist32(pts[0], pts[1]), dist32(pts[1], pts[2])
		ln := p * (l1 + l2)
		if ln > 0.5 {
			var path clip.Path
			path.Begin(f.gtx.Ops)
			path.MoveTo(pts[0])
			if ln <= l1 {
				path.LineTo(pts[0].Add(pts[1].Sub(pts[0]).Mul(ln / l1)))
			} else {
				path.LineTo(pts[1])
				path.LineTo(pts[1].Add(pts[2].Sub(pts[1]).Mul((ln - l1) / l2)))
			}
			paint.FillShape(f.gtx.Ops, introWhite, clip.Stroke{Path: path.End(), Width: f.df(7)}.Op())
		}
	})
	if !f.reduced {
		if q := f.now.Sub(start.Add(msec(760))).Seconds() / 1.0; q > 0 && q < 1 && parent.a > 0.01 {
			f.mc.moving = true
			ringAt(f, c, d/2+f.df(60)*float32(q), alphaMul(hexc(0xb7a8ff), 0.45*float32(1-q)*(1-float32(q))*parent.a), f.df(2))
		}
	}
	// the line and the sentence
	ty := int(cy + d/2 + f.df(40))
	words := splitWords(introCopy[beatFinal].title)
	space := f.spaceW(titleSize, theme.Black)
	total := 0
	ws := make([]int, len(words))
	for i, w := range words {
		ws[i] = f.measure(titleSize, theme.Black, w)
		total += ws[i]
	}
	total += space * (len(words) - 1)
	x := int(cx) - total/2
	for i, w := range words {
		we := f.enter(start.Add(msec(380+62*float64(i))), f.df(18), springBouncy, true).mul(parent)
		f.word("title", w, image.Pt(x, ty), titleSize, theme.Black, introWhite, we)
		x += ws[i] + space
	}
	se := f.enter(start.Add(msec(620)), f.df(12), springSmooth, false).mul(parent)
	sy := ty + g.titleH + f.d(12)
	f.with("sub", se, f32.Point{}, func() {
		f.para(image.Pt(int(cx), sy), g.textW, subSize, alphaMul(introWhite, 0.66), introCopy[beatFinal].sub)
	})
	be := f.enter(start.Add(msec(760)), f.df(12), springBouncy, false).mul(parent)
	f.with("done", be, f32.Point{}, func() {
		in.pillButton(f, &in.doneBtn, &in.doneAnim, "Done", image.Pt(int(cx), sy+g.subLineH+f.d(64)), false)
	})
}

func dist32(a, b f32.Point) float32 {
	d := b.Sub(a)
	return float32(math.Hypot(float64(d.X), float64(d.Y)))
}
