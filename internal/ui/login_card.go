package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"
	"golang.org/x/image/vector"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sign-in card (login.go has its state), drawn in the introduction's
// language: the same lit backdrop, display type and springs, white on a
// dark glass panel. One primary action, Open Slack sign-in, with the
// slack:// field under it as the second way in. The rest waits behind
// Having trouble? (tips for when the link doesn't come back) and Advanced
// (the token form), which open on springs.
//
// Sizes are layout units: dp at the introduction's 1280×820 design size,
// scaled with the window by introFrame.

const (
	loginCardW    = 520 // the link view
	loginAdvCardW = 600 // Advanced
	loginPadX     = 40
	loginAdvPadX  = 36
	loginRadius   = 26
	loginTitlePt  = 30
	loginSubPt    = 16
	loginCtlH     = 48  // the primary button and the link field
	loginRefH     = 470 // about the default card's height: where its top sits
)

// signInTitle is the card's first line (its words blur in after the
// introduction; intro.prefetch asks for their blurred copies).
const signInTitle = "Sign in to Slack"

var (
	loginGlassTop = hexc(0x1d1830)
	loginGlassBot = hexc(0x110e1b)
	loginInk      = hexc(0x0b0912) // the pill's label (the introduction's)
	loginViolet   = hexc(0x7b5cff) // the mark's
	loginLilac    = hexc(0x9d8cff) // focus, the badge's ring
	loginGlow     = hexc(0xb7a8ff)
	loginRose     = hexc(0xff8f9c) // errors on the dark glass
	loginAmber    = hexc(0xffc56b) // Advanced's warning
	loginCode     = hexc(0xd4c9ff)
)

// loginLine is a line of the card's header: its words, and whether it is
// an error.
type loginLine struct {
	s   string
	err bool
}

// loginFade cross-fades a header line when it changes: the old words fade
// up and away as the new ones rise in.
type loginFade struct {
	cur, prev loginLine
	at        time.Time // when cur replaced prev (zero: cur was there from the start)
}

func (c *loginFade) set(v loginLine, now time.Time, animate bool) {
	if v == c.cur {
		return
	}
	c.prev, c.cur, c.at = c.cur, v, time.Time{}
	if animate {
		c.at = now
	}
}

// layoutLogin draws the sign-in card over the welcome backdrop
// (layoutWelcome). enter is when the introduction handed over to it (zero:
// it is simply there); f is the backdrop's frame (its scale, clock and
// motion setting).
func (a *App) layoutLogin(gtx layout.Context, enter time.Time, f *introFrame) {
	l := a.login
	l.now, l.mc, l.reduced = f.now, f.mc, f.reduced
	if l.rec != nil {
		*l.rec = (*l.rec)[:0]
	}
	a.updateLogin(gtx)
	sz := gtx.Constraints.Max
	adv := clamp01(l.follow(&l.advV, springSmooth, l.advanced))
	tr := clamp01(l.follow(&l.troubleV, springSmooth, l.trouble))
	l.follow(&l.errV, springSmooth, l.err != "")

	// the views that show, recorded so the card can be sized to them
	var linkCall, advCall op.CallOp
	var linkH, advH int
	showLink := !l.advanced || adv < 0.996
	showAdv := l.advanced || adv > 0.004
	if showLink {
		lg := gtx
		if l.advanced {
			lg = gtx.Disabled() // leaving: no clicks, no typing
		}
		linkCall, linkH = a.loginLinkView(lg, f, enter, tr)
	}
	if showAdv {
		ag := gtx
		if !l.advanced {
			ag = gtx.Disabled()
		}
		advCall, advH = a.loginAdvView(ag, f)
	}
	h := linkH
	switch {
	case !showLink:
		h = advH
	case showAdv:
		h = int(math.Round(lerp(float64(linkH), float64(advH), adv)))
	}
	w := int(math.Round(lerp(float64(f.d(loginCardW)), float64(f.d(loginAdvCardW)), adv)))
	// The top stays where the default card's would be; what opens grows
	// downward, and the card moves up only when it would leave the window.
	left := (sz.X - w) / 2
	top := (sz.Y - f.d(loginRefH)) / 2
	if bottom := sz.Y - f.d(24); top+h > bottom {
		top = bottom - h
	}
	top = max(top, f.d(16)) // centred, it never reaches the docked mark at the top left
	r := image.Rect(left, top, left+w, top+h)
	l.card = r

	// the card's motion: in from below after the introduction, a shake at
	// a new error (reduced motion: a fade, no shake)
	alpha, dx, dy, s := float32(1), float32(0), float32(0), float32(1)
	if !enter.IsZero() {
		start := enter.Add(150 * time.Millisecond)
		alpha = float32(clamp01(l.mc.spring(springSnappy, start)))
		if !l.reduced {
			e := float32(l.mc.spring(springBouncy, start))
			dy = f.df(44) * (1 - e)
			s = 0.97 + 0.03*e
		}
	}
	if !l.reduced && l.err != "" && !l.errAt.IsZero() {
		if t := l.now.Sub(l.errAt).Seconds(); t >= 0 && t < springShake.settle() {
			dx = f.df(9) * float32(springShake.kick(t))
			l.mc.moving = true
		}
	}
	c := f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	tf := op.Affine(f32.Affine2D{}.Scale(c, f32.Pt(s, s)).Offset(f32.Pt(dx, dy))).Push(gtx.Ops)
	defer tf.Pop()
	if alpha < 0.997 {
		// drawn even while invisible: its editors keep their handlers (and
		// the link field its focus) from the first frame
		defer paint.PushOpacity(gtx.Ops, alpha).Pop()
	}
	// a deep, soft shadow on the dark backdrop, then the glass
	loginShadow(gtx, r.Add(image.Pt(0, f.d(18))), f.d(loginRadius), f.d(22), alphaMul(introBlack, 0.62))
	loginGlass(gtx, f, r)
	cl := clip.UniformRRect(r, f.d(loginRadius)).Push(gtx.Ops)
	if showLink {
		// the link view slides out to the left as Advanced comes in
		e := fx{a: float32(1 - clamp01(adv*1.7)), s: 1}
		if !l.reduced {
			e.dx = -f.df(40) * float32(adv)
		}
		l.put(f, "signin-link", e, f32.Point{}, image.Pt(left+(w-f.d(loginCardW))/2, top), linkCall)
	}
	if showAdv {
		e := fx{a: float32(clamp01((adv - 0.3) / 0.7)), s: 1}
		if !l.reduced {
			e.dx = f.df(48) * float32(1-adv)
		}
		l.put(f, "signin-advanced", e, f32.Point{}, image.Pt(left+(w-f.d(loginAdvCardW))/2, top), advCall)
	}
	cl.Pop()
	l.drawn = true
}

// follow moves sv toward on (1) or off (0) on spring sp and returns it
// (past 1 for a moment on an underdamped spring).
func (l *loginScreen) follow(sv *springVal, sp spring, on bool) float64 {
	sv.s = sp
	t := 0.0
	if on {
		t = 1
	}
	v, moving := sv.get(l.now, t)
	if moving && l.mc != nil {
		l.mc.moving = true
	}
	return v
}

// put adds call at p under e's offset, scale and opacity, also when e is
// invisible: what's in it keeps its input handlers and the focus.
func (l *loginScreen) put(f *introFrame, name string, e fx, pivot f32.Point, p image.Point, call op.CallOp) {
	f.record(name, e)
	aff := f32.Affine2D{}.Offset(layout.FPt(p))
	if e.s != 1 && e.s > 0 {
		aff = aff.Scale(pivot.Add(layout.FPt(p)), f32.Pt(e.s, e.s))
	}
	aff = aff.Offset(f32.Pt(e.dx, e.dy))
	t := op.Affine(aff).Push(f.gtx.Ops)
	if e.a < 0.997 {
		o := paint.PushOpacity(f.gtx.Ops, max(e.a, 0))
		call.Add(f.gtx.Ops)
		o.Pop()
	} else {
		call.Add(f.gtx.Ops)
	}
	t.Pop()
}

// note keeps s for tests (every string the card draws).
func (l *loginScreen) note(s string) {
	if l.rec != nil && s != "" {
		*l.rec = append(*l.rec, s)
	}
}

// text draws one line at p (top-left) and returns its width.
func (l *loginScreen) text(f *introFrame, p image.Point, size float32, w font.Weight, c color.NRGBA, s string) int {
	l.note(s)
	return f.label(p, size, w, c, s)
}

// block lays out wrapped text w px wide at p, centred or from the left,
// and returns its height. Centred text is balanced: two lines of about the
// same length, never one word alone.
func (l *loginScreen) block(f *introFrame, p image.Point, w int, size, lh float32, wt font.Weight, c color.NRGBA, align text.Alignment, s string) int {
	l.note(s)
	if align == text.Middle {
		if full := f.measure(size, wt, s); full > w {
			lines := (full + w - 1) / w
			bw := min(w, full/lines+f.d(size*2.2))
			p.X += (w - bw) / 2
			w = bw
		}
	}
	g := f.gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
	defer op.Offset(p).Push(f.gtx.Ops).Pop()
	return f.a.th.Layout(g, theme.TextStyle{Size: f.sp(size), LineHeight: f.sp(lh), Weight: wt, Color: c, Align: align, MaxLines: 4}, s).Size.Y
}

// icon draws a glyph of side size (units) with its top-left at p.
func (f *introFrame) icon(name string, p image.Point, size float32, c color.NRGBA) {
	defer op.Offset(p).Push(f.gtx.Ops).Pop()
	g := f.gtx
	g.Constraints = layout.Constraints{Max: image.Pt(f.d(size), f.d(size))}
	icons.Get(name)(g, f.idp(size), c)
}

// ---- the link view ----

// loginLinkView lays out the link path: the badge, the header, Open Slack
// sign-in, the field and its secret, the footer and Having trouble? (tr:
// how far that is open). It returns the recording and its height; x runs
// from the card's left edge.
func (a *App) loginLinkView(gtx layout.Context, f *introFrame, enter time.Time, tr float64) (op.CallOp, int) {
	l := a.login
	m := op.Record(gtx.Ops)
	W := f.d(loginCardW)
	x0, x1 := f.d(loginPadX), W-f.d(loginPadX)
	cw, cx := x1-x0, W/2
	// after the introduction the parts come in one after another
	piece := func(ms float64, rise float32) fx {
		if enter.IsZero() {
			return fxOn
		}
		return f.enter(enter.Add(msec(ms)), f.df(rise), springBouncy, false)
	}
	// While Clack signs in the controls dim. They stay enabled (the field
	// keeps its focus); updateLogin ignores them meanwhile.
	dim := float32(1 - 0.62*clamp01(l.follow(&l.dimV, springSmooth, l.busy)))
	ctl := gtx
	send := l.follow(&l.sendV, springPop, l.link.Len() > 0)

	y := f.d(38)
	// the badge
	bd := f.df(60)
	bc := f32.Pt(float32(cx), float32(y)+bd/2)
	be := fxOn
	if !enter.IsZero() {
		be = f.pop(enter.Add(msec(260)), 0.6, springPop)
	}
	f.with("signin-badge", be, bc, func() { l.badge(f, bc, bd) })
	y += int(bd) + f.d(22)

	// the title: after the introduction its words come into focus, like
	// the beats' lines; later changes cross-fade
	l.title.set(loginLine{s: l.titleText()}, l.now, l.drawn)
	if !enter.IsZero() && l.title.at.IsZero() {
		l.titleWords(f, cx, y, enter.Add(msec(330)))
	} else {
		l.fadeLine(f, &l.title, x0, y, cw, loginTitlePt, 1.25, theme.Black, introWhite)
	}
	y += f.d(loginTitlePt*1.25) + f.d(8)

	// the line under it (an error takes its place)
	l.sub.set(l.subLine(), l.now, l.drawn)
	subC, subH := recordOps(gtx, func() int {
		return l.fadeLine(f, &l.sub, x0, y, cw, loginSubPt, 1.45, theme.Regular, alphaMul(introWhite, 0.64))
	})
	l.put(f, "signin-sub", piece(430, 12), f32.Point{}, image.Point{}, subC)
	y += l.height(&l.subH, subH) + f.d(26)

	// the primary action, or while waiting a quieter way to open the page again
	wv := float32(clamp01(l.follow(&l.waitV, springSmooth, l.waiting)))
	pr := image.Rect(x0, y, x1, y+f.d(loginCtlH))
	prC, _ := recordOps(gtx, func() int {
		if wv < 0.996 {
			pg := ctl
			if l.waiting {
				pg = gtx.Disabled()
			}
			o := paint.PushOpacity(gtx.Ops, (1-wv)*dim)
			// the ↵ keycap goes once there's a link: Enter sends it then
			a.loginPill(pg, f, &l.openSignIn, pr, "Open Slack sign-in", "↵", float32(1-clamp01(send)), false)
			o.Pop()
		}
		if wv > 0.004 {
			rg := ctl
			if !l.waiting {
				rg = gtx.Disabled()
			}
			o := paint.PushOpacity(gtx.Ops, wv*dim)
			a.loginGlassButton(rg, f, &l.reopen, pr, "Open the page again", 15)
			o.Pop()
		}
		return pr.Dy()
	})
	l.put(f, "signin-primary", piece(490, 14), f32.Point{}, image.Point{}, prC)
	y += pr.Dy() + f.d(22)

	// "or"
	dvC, _ := recordOps(gtx, func() int {
		ly := y + f.d(8)
		ow := f.measure(12.5, theme.Bold, "or")
		gap := f.d(14)
		lc := alphaMul(introWhite, 0.10*dim)
		th := max(1, f.d(1))
		theme.Fill(gtx, image.Rect(x0, ly, cx-ow/2-gap, ly+th), lc)
		theme.Fill(gtx, image.Rect(cx+(ow+1)/2+gap, ly, x1, ly+th), lc)
		l.text(f, image.Pt(cx-ow/2, ly-f.d(12.5*1.25)/2), 12.5, theme.Bold, alphaMul(introWhite, 0.38*dim), "or")
		return f.d(16)
	})
	l.put(f, "signin-or", piece(530, 10), f32.Point{}, image.Point{}, dvC)
	y += f.d(16) + f.d(18)

	// the field: paste the slack:// link (a whole link pasted signs in)
	fr := image.Rect(x0, y, x1, y+f.d(loginCtlH))
	fdC, _ := recordOps(gtx, func() int {
		o := paint.PushOpacity(gtx.Ops, dim)
		a.loginField(ctl, f, fr, &l.link, "link", "Paste the slack:// link", &l.linkSubmit, float32(send), fr.Dy()/2, 15)
		o.Pop()
		return fr.Dy()
	})
	l.put(f, "signin-field", piece(570, 10), f32.Point{}, image.Point{}, fdC)
	y += fr.Dy() + f.d(12)

	// the secret, in a few words
	ntC, _ := recordOps(gtx, func() int {
		nt := "The link is a secret."
		is := f.d(13)
		tw := f.measure(13, theme.Regular, nt)
		nx := cx - (is+f.d(6)+tw)/2
		f.icon("lock", image.Pt(nx, y+(f.d(13*1.25)-is)/2), 13, alphaMul(introWhite, 0.40*dim))
		l.text(f, image.Pt(nx+is+f.d(6), y), 13, theme.Regular, alphaMul(introWhite, 0.46*dim), nt)
		return f.d(18)
	})
	l.put(f, "signin-secret", piece(610, 8), f32.Point{}, image.Point{}, ntC)
	y += f.d(18) + f.d(24)

	// Having trouble? · Advanced
	ftC, _ := recordOps(gtx, func() int {
		ta, aa := "Having trouble?", "Advanced"
		gap := f.d(30)
		w1, w2 := a.loginTextButtonW(f, ta), a.loginTextButtonW(f, aa)
		fx0 := cx - (w1+gap+w2)/2
		o := paint.PushOpacity(gtx.Ops, dim)
		a.loginTextButton(ctl, f, &l.troubleBtn, image.Pt(fx0, y), ta, "chevron-down", float32(tr)*math.Pi)
		theme.Circle(gtx, image.Pt(fx0+w1+gap/2, y+f.d(10)), max(2, f.d(3)), alphaMul(introWhite, 0.24))
		a.loginTextButton(ctl, f, &l.advToggle, image.Pt(fx0+w1+gap, y), aa, "chevron-right", 0)
		o.Pop()
		return f.d(20)
	})
	l.put(f, "signin-footer", piece(650, 8), f32.Point{}, image.Point{}, ftC)
	y += f.d(20)

	// Having trouble? opens under the footer: the card grows on the
	// spring and the tips come in one after another
	if l.trouble || tr > 0.004 {
		y0 := y
		tpC, th := recordOps(gtx, func() int { return a.loginTips(ctl, f, x0, y+f.d(22), cw, tr) })
		l.put(f, "signin-tips", fxOn, f32.Point{}, image.Point{}, tpC)
		y = y0 + int(math.Round(float64(f.d(22)+th)*tr))
	}
	y += f.d(30)
	return m.Stop(), y
}

// recordOps records w's drawing and returns it with the height w reports.
func recordOps(gtx layout.Context, w func() int) (op.CallOp, int) {
	m := op.Record(gtx.Ops)
	h := w()
	return m.Stop(), h
}

// height follows h on a smooth spring (a line that grows for a long error
// pushes what's under it down smoothly).
func (l *loginScreen) height(sv *springVal, h int) int {
	sv.s = springSmooth
	v, moving := sv.get(l.now, float64(h))
	if moving && l.mc != nil {
		l.mc.moving = true
	}
	return int(math.Round(v))
}

func clamp01f(v float32) float32 { return float32(clamp01(float64(v))) }

// titleText is the card's first line for its state.
func (l *loginScreen) titleText() string {
	switch {
	case l.busy:
		return "Signing in…"
	case l.waiting:
		return "Waiting for Slack…"
	}
	return signInTitle
}

// subLine is the line under the title: what to do now, or the error.
func (l *loginScreen) subLine() loginLine {
	switch {
	case l.err != "":
		return loginLine{s: l.err, err: true}
	case l.busy && l.busyLink:
		return loginLine{s: "Slack is giving Clack its own session."}
	case l.busy:
		return loginLine{s: "Checking your token with Slack."}
	case l.waiting && l.sso:
		return loginLine{s: "Finish single sign-on in your browser."}
	case l.waiting:
		return loginLine{s: "Continue in your browser."}
	}
	return loginLine{s: "Clack signs in through your browser."}
}

// titleWords draws the title word by word coming into focus (the
// introduction's lines), centred on cx.
func (l *loginScreen) titleWords(f *introFrame, cx, y int, start time.Time) {
	s := l.title.cur.s
	l.note(s)
	words := splitWords(s)
	space := f.spaceW(loginTitlePt, theme.Black)
	ws := make([]int, len(words))
	total := 0
	for i, w := range words {
		ws[i] = f.measure(loginTitlePt, theme.Black, w)
		total += ws[i]
	}
	total += space * max(0, len(words)-1)
	x := cx - total/2
	for i, w := range words {
		e := f.enter(start.Add(msec(55*float64(i))), f.df(16), springBouncy, true)
		f.word("signin-title", w, image.Pt(x, y), loginTitlePt, theme.Black, introWhite, e)
		x += ws[i] + space
	}
}

// fadeLine draws c centred in [x, x+w) at y: the line it replaced fading up
// and out, the new one rising in. It returns the new line's height.
func (l *loginScreen) fadeLine(f *introFrame, c *loginFade, x, y, w int, size, lh float32, wt font.Weight, col color.NRGBA) int {
	colOf := func(v loginLine, a float32) color.NRGBA {
		if v.err {
			return alphaMul(loginRose, a)
		}
		return alphaMul(col, a)
	}
	if !c.at.IsZero() && c.prev.s != "" {
		e := l.mc.spring(springSmooth, c.at)
		if pa := float32(1 - clamp01(e*1.7)); pa > 0.004 {
			dy := 0
			if !l.reduced {
				dy = -int(math.Round(float64(f.df(6)) * e))
			}
			call, _ := recordOps(f.gtx, func() int {
				return l.block(f, image.Pt(x, y+dy), w, size, size*lh, wt, colOf(c.prev, pa), text.Middle, c.prev.s)
			})
			call.Add(f.gtx.Ops)
		}
	}
	na, dy := float32(1), 0
	if !c.at.IsZero() {
		e := l.mc.spring(springSmooth, c.at.Add(70*time.Millisecond))
		na = float32(clamp01(e))
		if !l.reduced {
			dy = int(math.Round(float64(f.df(8)) * (1 - e)))
		}
	}
	call, h := recordOps(f.gtx, func() int {
		return l.block(f, image.Pt(x, y+dy), w, size, size*lh, wt, colOf(c.cur, na), text.Middle, c.cur.s)
	})
	if na > 0.004 {
		call.Add(f.gtx.Ops)
	}
	return h
}

// badge is the card's emblem: a lilac ring around a globe (your browser).
// Waiting, light leaves it in slow rings; signing in, the ring turns and
// the globe becomes a key; an error tints it rose.
func (l *loginScreen) badge(f *introFrame, c f32.Point, d float32) {
	gtx := f.gtx
	ev := float32(l.errV.x)
	if !l.errV.init {
		ev = 0
	}
	ev = clamp01f(ev)
	ring := theme.Mix(loginLilac, loginRose, ev)
	breath := float32(1)
	if l.waiting && !l.busy {
		t := l.now.Sub(l.waitAt).Seconds()
		if l.reduced {
			// no rings leaving: the ring breathes
			breath = float32(0.55 + 0.45*(0.5+0.5*math.Cos(2*math.Pi*t/2.6)))
		} else {
			l.sonar(f, c, d/2, t)
		}
		// a calm loop: about 30 frames a second is plenty
		l.mc.wakeAt(l.now.Add(33 * time.Millisecond))
	}
	theme.Circle(gtx, c.Round(), int(d-f.df(4)), alphaMul(theme.Mix(loginViolet, loginRose, ev*0.6), 0.18))
	kv := float32(clamp01(l.follow(&l.keyV, springSnappy, l.busy)))
	if l.busy {
		ringAt(f, c, d/2, alphaMul(ring, 0.22), f.df(2.2))
		l.badgeArc(f, c, d/2, ring)
	} else {
		ringAt(f, c, d/2, alphaMul(ring, 0.85*breath), f.df(2.2))
	}
	gs := float32(26)
	gp := image.Pt(int(c.X)-f.d(gs)/2, int(c.Y)-f.d(gs)/2)
	if a := 1 - kv; a > 0.004 {
		f.icon("globe", gp, gs, alphaMul(introWhite, 0.92*a))
	}
	if kv > 0.004 {
		f.icon("key", gp, gs, alphaMul(introWhite, 0.92*kv))
	}
}

// sonar is the waiting badge's light: a ring leaves it every 1.3 s,
// widening and fading (t: seconds since the waiting began).
func (l *loginScreen) sonar(f *introFrame, c f32.Point, r0 float32, t float64) {
	const period = 2.6
	for k := 0; k < 2; k++ {
		tk := t - float64(k)*period/2
		if tk < 0 {
			continue
		}
		p := math.Mod(tk, period) / period
		e := 1 - math.Pow(1-p, 3)
		a := 0.5 * math.Pow(1-p, 1.8)
		// at most 30 units out: the badge sits 38 under the card's top
		ringAt(f, c, r0+f.df(30)*float32(e), alphaMul(loginGlow, float32(a)), f.df(1.6))
	}
}

// badgeArc is the badge's ring turning while Clack signs in (reduced
// motion: the ring pulses where it is).
func (l *loginScreen) badgeArc(f *introFrame, c f32.Point, r float32, col color.NRGBA) {
	t := l.now.Sub(l.busyAt).Seconds()
	l.mc.moving = true
	if l.reduced {
		ringAt(f, c, r, alphaMul(col, float32(0.45+0.4*(0.5+0.5*math.Sin(t*3)))), f.df(2.4))
		return
	}
	a0 := t * 2 * math.Pi * 0.85
	sweep := math.Pi * (0.55 + 0.3*math.Sin(t*2.6))
	var p clip.Path
	p.Begin(f.gtx.Ops)
	p.MoveTo(c.Add(f32.Pt(r*float32(math.Cos(a0)), r*float32(math.Sin(a0)))))
	p.ArcTo(c, c, float32(sweep))
	paint.FillShape(f.gtx.Ops, col, clip.Stroke{Path: p.End(), Width: f.df(2.6)}.Op())
}

// ---- Having trouble? ----

// loginTip is one tip: a glyph, a sentence and maybe a button.
type loginTip struct {
	icon, text string
	btn        *widget.Clickable
	label      string
}

func (l *loginScreen) tips() []loginTip {
	handler := "To let your browser open Clack, run clack --register-url-handler once. If Slack’s own app opens instead, run it again."
	switch loginOS {
	case "windows":
		handler = "To let your browser open Clack, run Clack.exe -register-url-handler once. If Slack’s own app opens instead, run it again."
	case "darwin":
		handler = "Your browser opens Clack when Slack hands over the link, unless Slack’s own app takes it."
	}
	copyLabel := "Copy page link"
	if l.pageCopied {
		copyLabel = "Copied"
	}
	return []loginTip{
		{icon: "external", text: "Browser didn’t open?", btn: &l.copyPage, label: copyLabel},
		{icon: "link", text: handler},
		{icon: "copy", text: "Copied the slack:// link? Switch back to Clack and it signs in."},
		{icon: "clock", text: "Links work once, for a few minutes. Single sign-on may reopen your browser to finish."},
	}
}

// loginTips lays out the tips from y, under a hairline; tr is how far
// they're open (each fades in after the one before). It returns their
// height.
func (a *App) loginTips(gtx layout.Context, f *introFrame, x0, y, cw int, tr float64) int {
	l := a.login
	y0 := y
	theme.Fill(gtx, image.Rect(x0, y, x0+cw, y+max(1, f.d(1))), alphaMul(introWhite, float32(0.08*tr)))
	y += f.d(18)
	for i, tp := range l.tips() {
		k := clamp01((tr - 0.12*float64(i)) / (1 - 0.12*float64(i)))
		ta := float32(k)
		dy := 0
		if !l.reduced {
			dy = int(math.Round(float64(f.df(8)) * (1 - k)))
		}
		tx := x0 + f.d(28)
		tw := cw - f.d(28)
		bw, bh := 0, 0
		if tp.btn != nil {
			bw = f.measure(13, theme.Bold, tp.label) + f.d(26)
			bh = f.d(30)
			tw -= bw + f.d(12)
		}
		call, th := recordOps(gtx, func() int {
			return l.block(f, image.Pt(tx, y+dy), tw, 13.5, 20, theme.Regular, alphaMul(introWhite, 0.72*ta), text.Start, tp.text)
		})
		rowH := max(th, bh)
		ty := (rowH - th) / 2
		func() {
			defer op.Offset(image.Pt(0, ty)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		f.icon(tp.icon, image.Pt(x0, y+dy+ty+f.d(2)), 15, alphaMul(introWhite, 0.52*ta))
		if tp.btn != nil {
			br := image.Rect(x0+cw-bw, y+dy, x0+cw, y+dy+bh)
			o := paint.PushOpacity(gtx.Ops, ta)
			a.loginGlassButton(gtx, f, tp.btn, br, tp.label, 13)
			o.Pop()
		}
		y += rowH + f.d(14)
	}
	return y - f.d(14) + f.d(4) - y0
}

// ---- Advanced: the token form ----

// loginAdvView lays out Advanced: back, the session warning, the console
// snippet, the four fields and Sign in. It returns the recording and its
// height; x runs from the (wider) card's left edge.
func (a *App) loginAdvView(gtx layout.Context, f *introFrame) (op.CallOp, int) {
	l := a.login
	m := op.Record(gtx.Ops)
	W := f.d(loginAdvCardW)
	x0, x1 := f.d(loginAdvPadX), W-f.d(loginAdvPadX)
	cw, cx := x1-x0, W/2
	ctl := gtx // while Clack signs in, updateLogin ignores the form
	y := f.d(28)
	// back, and the title
	bs := f.d(36)
	a.loginCircleButton(ctl, f, &l.advBack, image.Rect(x0-f.d(8), y, x0-f.d(8)+bs, y+bs), "chevron-left")
	tt := "Paste a token"
	tw := f.measure(22, theme.Black, tt)
	l.text(f, image.Pt(cx-tw/2, y+(bs-f.d(22*1.25))/2), 22, theme.Black, introWhite, tt)
	y += bs + f.d(18)

	// the session-sharing warning first: this way Clack runs on the
	// browser's own session, and Slack (Enterprise Grid especially) can
	// end a session it sees used from somewhere unexpected, in every app
	// at once
	y += a.loginWarn(gtx, f, x0, y, cw, "Clack shares your browser’s session this way, and Slack can end that session everywhere, browser included.") + f.d(18)

	y += l.block(f, image.Pt(x0, y), cw, 14, 20, theme.Regular, alphaMul(introWhite, 0.80), text.Start, "In Slack in your browser, press F12 and run this in the Console:") + f.d(10)
	y += a.loginSnippet(ctl, f, x0, y, cw) + f.d(20)

	// the fields, two by two
	gap := f.d(14)
	colW := (cw - gap) / 2
	fh := f.d(42)
	rowH := f.d(22) + fh + gap
	for i, fd := range []struct {
		label string
		ed    *widget.Editor
		hint  string
	}{
		{"Workspace", &l.workspace, "hackclub"},
		{"Token", &l.token, "xoxc-… or the copied JSON"},
		{"Org token", &l.orgToken, "optional"},
		{"d cookie", &l.cookie, "xoxd-…"},
	} {
		colX := x0 + (i%2)*(colW+gap)
		colY := y + (i/2)*rowH
		l.text(f, image.Pt(colX+f.d(2), colY), 12.5, theme.Bold, alphaMul(introWhite, 0.62), fd.label)
		a.loginField(ctl, f, image.Rect(colX, colY+f.d(22), colX+colW, colY+f.d(22)+fh), fd.ed, "", fd.hint, nil, 0, f.d(12), 14)
	}
	y += 2*rowH + f.d(2)

	// how to get d, or the error
	if l.err != "" {
		ea := float32(clamp01(l.mc.spring(springSmooth, l.errAt)))
		y += l.block(f, image.Pt(x0, y), cw, 13, 19, theme.Regular, alphaMul(loginRose, ea), text.Start, l.err)
	} else {
		y += l.block(f, image.Pt(x0, y), cw, 13, 19, theme.Regular, alphaMul(introWhite, 0.50), text.Start, "d is HttpOnly, so copy it from DevTools → Application → Cookies. Leave d-s out.")
	}
	y += f.d(18)

	txt, kc := "Sign in", "↵"
	if l.busy {
		txt, kc = "Signing in…", ""
	}
	a.loginPill(ctl, f, &l.submit, image.Rect(x0, y, x1, y+f.d(loginCtlH)), txt, kc, 1, l.busy)
	y += f.d(loginCtlH) + f.d(30)
	return m.Stop(), y
}

// loginWarn is Advanced's warning in an amber-tinted box; it returns the
// box's height.
func (a *App) loginWarn(gtx layout.Context, f *introFrame, x, y, w int, s string) int {
	l := a.login
	ip := f.d(14)
	is := f.d(16)
	call, th := recordOps(gtx, func() int {
		return l.block(f, image.Pt(x+ip+is+f.d(10), y+ip), w-2*ip-is-f.d(10), 13.5, 19.5, theme.Regular, alphaMul(introWhite, 0.86), text.Start, s)
	})
	r := image.Rect(x, y, x+w, y+th+2*ip)
	theme.RRect(gtx, r, f.d(12), alphaMul(loginAmber, 0.09))
	theme.Border(gtx, r, f.d(12), max(1, f.d(1)), alphaMul(loginAmber, 0.32))
	f.icon("warning", image.Pt(x+ip, y+ip+f.d(1.5)), 16, loginAmber)
	call.Add(gtx.Ops)
	return r.Dy()
}

// loginSnippet is the console snippet in a code box with Copy beside it;
// it returns the box's height.
func (a *App) loginSnippet(gtx layout.Context, f *introFrame, x, y, w int) int {
	l := a.login
	ip := f.d(14)
	label := "Copy"
	if l.copied {
		label = "Copied"
	}
	bw, bh := f.measure(13, theme.Bold, "Copied")+f.d(26), f.d(30)
	l.note(signInSnippet)
	call, th := recordOps(gtx, func() int {
		g := f.gtx
		cwid := w - 2*ip - bw - f.d(12)
		g.Constraints = layout.Constraints{Min: image.Pt(cwid, 0), Max: image.Pt(cwid, 1<<20)}
		defer op.Offset(image.Pt(x+ip, y+ip)).Push(gtx.Ops).Pop()
		return f.a.th.Layout(g, theme.TextStyle{Size: f.sp(12.5), LineHeight: f.sp(18), Color: loginCode, Wrap: text.WrapGraphemes}, signInSnippet).Size.Y
	})
	h := max(th, bh) + 2*ip
	r := image.Rect(x, y, x+w, y+h)
	theme.RRect(gtx, r, f.d(12), alphaMul(introWhite, 0.05))
	theme.Border(gtx, r, f.d(12), max(1, f.d(1)), alphaMul(introWhite, 0.10))
	call.Add(gtx.Ops)
	a.loginGlassButton(gtx, f, &l.copy, image.Rect(r.Max.X-ip-bw, y+(h-bh)/2, r.Max.X-ip, y+(h-bh)/2+bh), label, 13)
	return h
}

// ---- controls ----

// loginFocusRing is the card's keyboard focus ring around r: lilac,
// growing up to 4 units outside the control on its spring.
func loginFocusRing(gtx layout.Context, f *introFrame, r image.Rectangle, rad int, v float32) {
	if v <= 0.01 {
		return
	}
	g := int(math.Round(float64(f.df(4) * v)))
	theme.RRect(gtx, r.Inset(-g), rad+g, alphaMul(loginLilac, 0.42*v))
}

// loginPill is the card's primary button: the introduction's white pill
// at full width, its label and keycap centred (kca: the keycap's
// opacity), a soft light under it that grows on hover, pressing in on a
// spring. busy: a spinner turns beside the label.
func (a *App) loginPill(gtx layout.Context, f *introFrame, b *widget.Clickable, r image.Rectangle, label, kc string, kca float32, busy bool) {
	l := a.login
	hov, prs := l.press(b)
	h := r.Dy()
	loginFocusRing(gtx, f, r, h/2, l.ring(b, gtx.Focused(b)))
	sc := 1 - 0.035*prs
	t := op.Affine(f32.Affine2D{}.Scale(center(r), f32.Pt(sc, sc))).Push(gtx.Ops)
	defer t.Pop()
	theme.Shadow(gtx, r, h/2, f.d(14), alphaMul(loginGlow, 0.10+0.16*hov))
	theme.RRect(gtx, r, h/2, theme.Mix(hexc(0xeeeaf8), introWhite, hov))
	lw := f.measure(15.5, theme.Bold, label)
	kw := 0
	if kc != "" {
		kw = f.d(10) + f.keycapW(kc)
	}
	sw := 0
	if busy {
		sw = f.d(26)
	}
	x := r.Min.X + (r.Dx()-lw-kw-sw)/2
	if busy {
		l.spinner(gtx, image.Pt(x+f.d(9), r.Min.Y+h/2), f.d(16), loginInk)
	}
	l.text(f, image.Pt(x+sw, r.Min.Y+(h-f.d(15.5*1.25))/2), 15.5, theme.Bold, loginInk, label)
	if kc != "" && kca > 0.004 {
		o := paint.PushOpacity(gtx.Ops, kca)
		f.keycapDark(image.Pt(x+sw+lw+f.d(10), r.Min.Y+(h-f.d(22))/2), kc)
		o.Pop()
	}
	loginHit(gtx, b, r)
}

// loginHit makes r b's click area, with the pointer cursor.
func loginHit(gtx layout.Context, b *widget.Clickable, r image.Rectangle) {
	atExact(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: gtx.Constraints.Max}
		})
	})
}

// loginGlassButton is a quiet button on the glass: a faint fill and rim
// that brighten on hover, pressing in on a spring.
func (a *App) loginGlassButton(gtx layout.Context, f *introFrame, b *widget.Clickable, r image.Rectangle, label string, size float32) {
	l := a.login
	hov, prs := l.press(b)
	rad := min(r.Dy()/2, f.d(24))
	loginFocusRing(gtx, f, r, rad, l.ring(b, gtx.Focused(b)))
	sc := 1 - 0.04*prs
	t := op.Affine(f32.Affine2D{}.Scale(center(r), f32.Pt(sc, sc))).Push(gtx.Ops)
	defer t.Pop()
	theme.RRect(gtx, r, rad, alphaMul(introWhite, 0.07+0.05*hov))
	theme.Border(gtx, r, rad, max(1, f.d(1)), alphaMul(introWhite, 0.16+0.12*hov))
	lw := f.measure(size, theme.Bold, label)
	l.text(f, image.Pt(r.Min.X+(r.Dx()-lw)/2, r.Min.Y+(r.Dy()-f.d(size*1.25))/2), size, theme.Bold, alphaMul(introWhite, 0.86+0.1*hov), label)
	loginHit(gtx, b, r)
}

// loginCircleButton is a round glass button with a glyph (Advanced's back).
func (a *App) loginCircleButton(gtx layout.Context, f *introFrame, b *widget.Clickable, r image.Rectangle, icon string) {
	l := a.login
	hov, prs := l.press(b)
	loginFocusRing(gtx, f, r, r.Dy()/2, l.ring(b, gtx.Focused(b)))
	sc := 1 - 0.06*prs
	t := op.Affine(f32.Affine2D{}.Scale(center(r), f32.Pt(sc, sc))).Push(gtx.Ops)
	defer t.Pop()
	theme.RRect(gtx, r, r.Dy()/2, alphaMul(introWhite, 0.06+0.07*hov))
	is := float32(18)
	f.icon(icon, image.Pt(r.Min.X+(r.Dx()-f.d(is))/2, r.Min.Y+(r.Dy()-f.d(is))/2), is, alphaMul(introWhite, 0.80+0.15*hov))
	loginHit(gtx, b, r)
}

// loginTextButtonW is loginTextButton's width.
func (a *App) loginTextButtonW(f *introFrame, label string) int {
	return f.measure(13.5, theme.Bold, label) + f.d(6+12)
}

// loginTextButton is a small text button with a chevron after it (rot:
// the chevron's turn), its top-left at p; it brightens on hover.
func (a *App) loginTextButton(gtx layout.Context, f *introFrame, b *widget.Clickable, p image.Point, label, icon string, rot float32) {
	l := a.login
	hov, prs := l.press(b)
	w, h := a.loginTextButtonW(f, label), f.d(20)
	r := image.Rect(p.X, p.Y, p.X+w, p.Y+h)
	loginFocusRing(gtx, f, r.Inset(-f.d(6)), f.d(8), l.ring(b, gtx.Focused(b)))
	sc := 1 - 0.04*prs
	t := op.Affine(f32.Affine2D{}.Scale(center(r), f32.Pt(sc, sc))).Push(gtx.Ops)
	defer t.Pop()
	col := alphaMul(introWhite, 0.56+0.36*hov)
	lw := l.text(f, image.Pt(p.X, p.Y+(h-f.d(13.5*1.25))/2), 13.5, theme.Bold, col, label)
	is := f.d(12)
	ic := f32.Pt(float32(p.X+lw+f.d(6))+float32(is)/2, float32(p.Y)+float32(h)/2)
	rt := op.Affine(f32.Affine2D{}.Rotate(ic, rot)).Push(gtx.Ops)
	f.icon(icon, image.Pt(p.X+lw+f.d(6), p.Y+(h-is)/2), 12, col)
	rt.Pop()
	loginHit(gtx, b, r.Inset(-f.d(6)))
}

// loginField is a single-line field in r on the glass: a faint well that
// rings lilac when focused, an optional leading glyph, the hint while it's
// empty, and (send set) a ↵ button that pops in once there's text (sendA:
// its spring, past 1 as it lands).
func (a *App) loginField(gtx layout.Context, f *introFrame, r image.Rectangle, ed *widget.Editor, icon, hint string, send *widget.Clickable, sendA float32, rad int, size float32) {
	l := a.login
	th := a.th
	v := l.ring(ed, gtx.Focused(ed))
	loginFocusRing(gtx, f, r, rad, v)
	theme.RRect(gtx, r, rad, alphaMul(introWhite, 0.055+0.025*v))
	theme.Border(gtx, r, rad, max(1, f.d(1)), theme.Mix(alphaMul(introWhite, 0.14), alphaMul(loginLilac, 0.9), v))
	x := r.Min.X + f.d(16)
	if icon != "" {
		x = r.Min.X + f.d(18)
		is := f.d(17)
		f.icon(icon, image.Pt(x, r.Min.Y+(r.Dy()-is)/2), 17, alphaMul(introWhite, 0.42+0.3*v))
		x += is + f.d(12)
	}
	right := r.Max.X - f.d(16)
	if send != nil {
		d := r.Dy() - f.d(14)
		sr := image.Rect(r.Max.X-f.d(7)-d, r.Min.Y+f.d(7), r.Max.X-f.d(7), r.Min.Y+f.d(7)+d)
		right = sr.Min.X - f.d(10)
		if sendA > 0.01 && ed.Len() > 0 {
			hov, prs := l.press(send)
			loginFocusRing(gtx, f, sr, d/2, l.ring(send, gtx.Focused(send)))
			sc := sendA * (1 - 0.06*prs)
			if l.reduced {
				sc = 1 - 0.06*prs
			}
			o := paint.PushOpacity(gtx.Ops, clamp01f(sendA))
			t := op.Affine(f32.Affine2D{}.Scale(center(sr), f32.Pt(sc, sc))).Push(gtx.Ops)
			theme.Circle(gtx, center(sr).Round(), d, theme.Mix(loginViolet, hexc(0x8d72ff), hov))
			is := f.d(17)
			f.icon("enter", image.Pt(sr.Min.X+(d-is)/2, sr.Min.Y+(d-is)/2), 17, introWhite)
			loginHit(gtx, send, sr)
			o.Pop()
			t.Pop()
		}
	}
	at(gtx, image.Pt(x, r.Min.Y), image.Pt(max(0, right-x), r.Dy()), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, r.Dy(), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if ed.Len() == 0 {
				l.note(hint)
				th.Label(gtx, f.sp(size), theme.Regular, alphaMul(introWhite, 0.40), hint)
			}
			return th.Editor(gtx, ed, f.sp(size), alphaMul(introWhite, 0.94), alphaMul(loginViolet, 0.55))
		})
	})
}

// ring is the focus ring of tag (an editor or a button), on a snappy
// spring.
func (l *loginScreen) ring(tag any, on bool) float32 {
	if l.rings == nil {
		l.rings = map[any]*springVal{}
	}
	sv := l.rings[tag]
	if sv == nil {
		sv = &springVal{s: springSnappy}
		l.rings[tag] = sv
	}
	t := 0.0
	if on {
		t = 1
	}
	v, moving := sv.get(l.now, t)
	if moving && l.mc != nil {
		l.mc.moving = true
	}
	return float32(clamp01(v))
}

// press is c's hover and press, on springs.
func (l *loginScreen) press(c *widget.Clickable) (hover, press float32) {
	if l.presses == nil {
		l.presses = map[*widget.Clickable]*pressAnim{}
	}
	pa := l.presses[c]
	if pa == nil {
		pa = &pressAnim{press: springVal{s: springSnappy}, hover: springVal{s: springSnappy}}
		l.presses[c] = pa
	}
	ht, pt := 0.0, 0.0
	if c.Hovered() {
		ht = 1
	}
	if c.Pressed() {
		pt = 1
	}
	h, m1 := pa.hover.get(l.now, ht)
	p, m2 := pa.press.get(l.now, pt)
	if (m1 || m2) && l.mc != nil {
		l.mc.moving = true
	}
	return float32(clamp01(h)), float32(clamp01(p))
}

// spinner is a turning arc centred at c, d across (it keeps the frames
// coming only while it shows).
func (l *loginScreen) spinner(gtx layout.Context, c image.Point, d int, col color.NRGBA) {
	t := l.now.Sub(l.busyAt).Seconds()
	if l.mc != nil {
		l.mc.moving = true
	}
	r := float32(d) / 2
	w := max(2, float32(gtx.Dp(2)))
	cf := f32.Pt(float32(c.X), float32(c.Y))
	ring := func(a0, sweep float64, cl color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(cf.Add(f32.Pt(r*float32(math.Cos(a0)), r*float32(math.Sin(a0)))))
		p.ArcTo(cf, cf, float32(sweep))
		paint.FillShape(gtx.Ops, cl, clip.Stroke{Path: p.End(), Width: w}.Op())
	}
	ring(0, 2*math.Pi-0.01, theme.WithAlpha(col, 0x40))
	if l.reduced {
		// no turning: a pulse
		ring(-math.Pi/2, math.Pi*(0.6+0.4*math.Sin(t*4)), col)
		return
	}
	ring(t*2*math.Pi*1.1, math.Pi*(0.55+0.35*math.Sin(t*3)), col)
}

// ---- the glass and its shadow ----

// loginGlass is the card's panel: deep translucent glass, a little lighter
// at the top, with a hairline rim lit from above.
func loginGlass(gtx layout.Context, f *introFrame, r image.Rectangle) {
	rad := f.d(loginRadius)
	func() {
		defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
		paint.LinearGradientOp{
			Stop1: f32.Pt(0, float32(r.Min.Y)), Color1: alphaMul(loginGlassTop, 0.86),
			Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: alphaMul(loginGlassBot, 0.90),
		}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	w := float32(max(1, f.d(1)))
	defer clip.Stroke{Path: theme.RRectPath(r.Dx(), r.Dy(), rad), Width: w}.Op().Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, 0), Color1: alphaMul(introWhite, 0.22),
		Stop2: f32.Pt(0, float32(r.Dy())), Color2: alphaMul(introWhite, 0.05),
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// loginShadowSprite is a blurred rounded rectangle, for nine-slicing: the
// rectangle sits e px in from the edges, and the corner slices are m px.
type loginShadowSprite struct {
	img  paint.ImageOp
	e, m int
	n    int // the sprite's side
}

var loginShadows struct {
	sync.Mutex
	m map[[2]int]*loginShadowSprite
}

// loginShadowFor renders (once) the shadow of corner rad blurred by about
// sigma px.
func loginShadowFor(rad, sigma int) *loginShadowSprite {
	loginShadows.Lock()
	defer loginShadows.Unlock()
	k := [2]int{rad, sigma}
	if s, ok := loginShadows.m[k]; ok {
		return s
	}
	if loginShadows.m == nil || len(loginShadows.m) >= 8 {
		loginShadows.m = map[[2]int]*loginShadowSprite{}
	}
	e := 3 * sigma
	m := rad + 2*e // past the corner and the blur's reach the edges are straight
	n := 2*m + 1
	z := vector.NewRasterizer(n, n)
	rrectPoly(z, float32(e), float32(e), float32(n-2*e), float32(n-2*e), float32(rad))
	mask := image.NewAlpha(image.Rect(0, 0, n, n))
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	px := make([]float32, n*n)
	for i := range px {
		px[i] = float32(mask.Pix[i])
	}
	boxBlur3(px, n, n, float32(sigma))
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for i, v := range px {
		img.Pix[i*4+3] = uint8(math.Max(0, math.Min(255, float64(v)+0.5)))
	}
	s := &loginShadowSprite{img: paint.NewImageOp(img), e: e, m: m, n: n}
	s.img.Filter = paint.FilterLinear
	loginShadows.m[k] = s
	return s
}

// loginShadow draws a soft shadow under r (corner rad, blurred by about
// sigma px, black at col's alpha): the blurred sprite nine-sliced, so the
// card can change size every frame without a new path.
func loginShadow(gtx layout.Context, r image.Rectangle, rad, sigma int, col color.NRGBA) {
	if sigma < 1 || col.A == 0 {
		return
	}
	sp := loginShadowFor(rad, sigma)
	o := r.Inset(-sp.e)
	m := sp.m
	if o.Dx() < 2*m+1 || o.Dy() < 2*m+1 {
		return
	}
	xs := [4]int{o.Min.X, o.Min.X + m, o.Max.X - m, o.Max.X}
	ys := [4]int{o.Min.Y, o.Min.Y + m, o.Max.Y - m, o.Max.Y}
	ss := [4]int{0, m, m + 1, sp.n}
	defer paint.PushOpacity(gtx.Ops, float32(col.A)/255).Pop()
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			dst := image.Rect(xs[i], ys[j], xs[i+1], ys[j+1])
			src := image.Rect(ss[i], ss[j], ss[i+1], ss[j+1])
			if dst.Empty() {
				continue
			}
			cl := clip.Rect(dst).Push(gtx.Ops)
			k := f32.Pt(float32(dst.Dx())/float32(src.Dx()), float32(dst.Dy())/float32(src.Dy()))
			aff := f32.Affine2D{}.Offset(f32.Pt(-float32(src.Min.X), -float32(src.Min.Y))).Scale(f32.Point{}, k).Offset(layout.FPt(dst.Min))
			t := op.Affine(aff).Push(gtx.Ops)
			sp.img.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			t.Pop()
			cl.Pop()
		}
	}
}

// loginWords counts the words in what the card drew (tests: the default
// card stays at about 25).
func loginWords(strs []string) int {
	n := 0
	for _, s := range strs {
		for _, w := range strings.Fields(s) {
			if strings.IndexFunc(w, func(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }) >= 0 {
				n++
			}
		}
	}
	return n
}
