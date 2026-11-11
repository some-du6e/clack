package ui

import (
	"image"
	"image/color"
	"io"
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// loginScreen is the native sign-in card shown when credentials are missing.
//
// The main path is desktop-style sign-in: the user signs in in their own
// browser, and the slack:// link Slack's page hands "the Slack app" is
// pasted here (or the browser opens Clack with it; App.SignInWithLink).
// slack.LoginMagic trades it for a session of Clack's own.
//
// Advanced is the older path: paste the token and cookie of the browser's
// own session; slack.SignIn only checks them with auth.test (no page loads,
// no sign-out), but Clack then shares the browser's session.
type loginScreen struct {
	link       widget.Editor
	linkSubmit widget.Clickable
	openSignIn widget.Clickable
	advanced   bool // the token-paste form
	advToggle  widget.Clickable

	workspace widget.Editor
	token     widget.Editor
	orgToken  widget.Editor
	cookie    widget.Editor
	submit    widget.Clickable
	copy      widget.Clickable
	copied    bool
	busy      bool
	err       string
	focused   bool

	card image.Rectangle // where the card was last drawn (tests)

	// motion (motion.go): the card comes in after the introduction,
	// shakes once at a new error, fields ring when focused, buttons
	// press in
	now      time.Time // this frame's
	mc       *motionClock
	reduced  bool
	shownErr string             // the error the card last showed
	errAt    time.Time          // when it appeared (zero: it was there from the start)
	busyAt   time.Time          // when "Signing in…" started
	drawn    bool               // the card has been on screen
	rings    map[any]*springVal // focus rings, by editor or button
	presses  map[*widget.Clickable]*pressAnim
}

// desktopSignInURL is the page Slack desktop opens in the browser for
// "Sign in to Another Workspace" (main.bundle.cjs @1033551 in
// docs/DESKTOP-SIGNIN.md). Signing in there ends with Slack offering to open
// the Slack app through a slack:// link, the link Clack wants. Clack only
// opens it in the user's browser, on a click; it never loads it itself.
var desktopSignInURL = "https://slack.com/ssb/add?s=1&v=" + slack.SlackDesktopVersion

// signInSnippet runs in the DevTools console of the user's own signed-in Slack
// tab. It copies the teams map Slack's page keeps in localStorage (workspace
// and org tokens, URLs); SignIn reads that JSON from the Token field. It sends
// nothing anywhere. No user agent: Clack sends only its own (slack.UserAgent).
const signInSnippet = `copy(JSON.stringify({teams: JSON.parse(localStorage.localConfig_v2).teams}))`

func newLoginScreen() *loginScreen {
	l := &loginScreen{}
	l.link.SingleLine = true
	l.link.Submit = true
	l.link.Mask = '•' // a one-time secret: whoever has it first gets the session
	l.workspace.SingleLine = true
	l.workspace.Submit = true
	l.workspace.SetText("hackclub")
	for _, ed := range []*widget.Editor{&l.token, &l.orgToken, &l.cookie} {
		ed.SingleLine = true
		ed.Submit = true
		ed.Mask = '•'
	}
	return l
}

// layoutLogin draws the sign-in card over the welcome backdrop
// (layoutWelcome). enter is when the introduction handed over to it (zero:
// it is simply there); mc collects whether anything on it still moves.
func (a *App) layoutLogin(gtx layout.Context, enter time.Time, mc *motionClock) {
	l := a.login
	sz := gtx.Constraints.Max
	th := a.th
	lc := theme.LightPalette // the card is always light, like Slack's sign-in page
	l.now, l.mc, l.reduced = mc.now, mc, a.reduceMotion()
	if !l.focused {
		if l.advanced {
			gtx.Execute(key.FocusCmd{Tag: &l.token})
		} else {
			gtx.Execute(key.FocusCmd{Tag: &l.link})
		}
		l.focused = true
	}
	if l.advToggle.Clicked(gtx) {
		l.advanced, l.focused, l.err = !l.advanced, false, ""
	}
	if l.openSignIn.Clicked(gtx) {
		openURL(desktopSignInURL)
	}
	submit := l.submit.Clicked(gtx)
	linkSubmit := l.linkSubmit.Clicked(gtx)
	if l.copy.Clicked(gtx) {
		// Written here, not with a.copyText: frame returns before keys.process
		// (which flushes a.clipboard) while the sign-in screen is up.
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(signInSnippet))})
		l.copied = true
	}
	for {
		e, ok := l.link.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			linkSubmit = true
		}
	}
	for _, ed := range []*widget.Editor{&l.workspace, &l.token, &l.orgToken, &l.cookie} {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.SubmitEvent); ok {
				submit = true
			}
		}
	}
	if linkSubmit && !l.advanced && !l.busy {
		if link := strings.TrimSpace(l.link.Text()); link == "" {
			l.err = "Paste the slack:// link from your browser first."
		} else if !slack.IsMagicLink(link) {
			l.err = slack.ErrNotMagicLink.Error()
		} else {
			a.startLogin(slack.SignInInput{Link: link})
		}
	}
	if submit && l.advanced && !l.busy {
		in := slack.SignInInput{
			Workspace: strings.TrimSpace(l.workspace.Text()),
			Token:     strings.TrimSpace(l.token.Text()),
			OrgToken:  strings.TrimSpace(l.orgToken.Text()),
			Cookie:    strings.TrimSpace(l.cookie.Text()),
		}
		if in.Token == "" || in.Cookie == "" {
			l.err = "Paste your token and d cookie first."
		} else {
			a.startLogin(in)
		}
	}
	if l.err != l.shownErr {
		// a new error fades in, and the card shakes its head once; one it
		// opened with ("Your Slack session ended") just shows
		l.shownErr, l.errAt = l.err, time.Time{}
		if l.drawn {
			l.errAt = l.now
		}
	}
	l.drawn = true
	if l.busy && l.busyAt.IsZero() {
		l.busyAt = l.now
	} else if !l.busy {
		l.busyAt = time.Time{}
	}

	// Two columns so everything fits the default 900px-tall window: how to
	// get the values on the left, the fields on the right. The contents are
	// recorded first, so the card can be sized to them before it is painted.
	pad, gap := gtx.Dp(36), gtx.Dp(36)
	w := min(gtx.Dp(900), sz.X-gtx.Dp(16))
	colW := (w - 2*pad - gap) / 2
	rec := op.Record(gtx.Ops)
	x, y := pad, pad
	at(gtx, image.Pt(x, y), image.Pt(w-2*pad, gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 30, theme.Black, lc.Fg, "Sign in to Slack")
	})
	y += gtx.Dp(58)

	var ly, fy int
	if l.advanced {
		ly, fy = a.layoutLoginPaste(gtx, x, y, colW, gap)
	} else {
		ly, fy = a.layoutLoginLink(gtx, x, y, colW, gap)
	}
	fx := x + colW + gap
	if l.err != "" {
		fy += gtx.Dp(10)
		ea := float32(clamp01(l.mc.spring(springSmooth, l.errAt)))
		ed := 0
		if !l.reduced {
			ed = int(math.Round(float64(gtx.Dp(6)) * float64(1-ea)))
		}
		fy += at(gtx, image.Pt(fx, fy-ed), image.Pt(colW, gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: alphaMul(lc.Danger, ea), MaxLines: 4}, l.err)
		}).Size.Y
	}
	// Switch between the two ways in.
	fy += gtx.Dp(18)
	tog := "Advanced: paste a token instead"
	if l.advanced {
		tog = "\u2190 Sign in with a Slack link instead"
	}
	tw, tth := th.Measure(gtx, 14, theme.Bold, tog), gtx.Dp(22)
	atExact(gtx, image.Pt(fx, fy), image.Pt(tw, tth), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &l.advToggle, nil, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 14, theme.Bold, loginAccent, tog)
		})
	})
	fy += tth
	content := rec.Stop()

	h := max(ly, fy) + pad
	left, top := (sz.X-w)/2, max(gtx.Dp(8), (sz.Y-h)/2)
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
			dy = float32(gtx.Dp(44)) * (1 - e)
			s = 0.97 + 0.03*e
		}
	}
	if !l.reduced && l.err != "" && !l.errAt.IsZero() {
		if t := l.now.Sub(l.errAt).Seconds(); t >= 0 && t < springShake.settle() {
			dx = float32(gtx.Dp(9)) * float32(springShake.kick(t))
			l.mc.moving = true
		}
	}
	c := f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	tr := op.Affine(f32.Affine2D{}.Scale(c, f32.Pt(s, s)).Offset(f32.Pt(dx, dy))).Push(gtx.Ops)
	defer tr.Pop()
	if alpha < 0.997 {
		// drawn even while invisible: its editors keep their handlers (and
		// the link field its focus) from the first frame
		defer paint.PushOpacity(gtx.Ops, alpha).Pop()
	}
	// on the dark backdrop: a deep, soft shadow below the card (filled under
	// it too, so it has no hard edge)
	sh, shc := r.Add(image.Pt(0, gtx.Dp(10))), theme.WithAlpha(color.NRGBA{}, 0x5c)
	theme.RRect(gtx, sh, gtx.Dp(16), shc)
	theme.Shadow(gtx, sh, gtx.Dp(16), gtx.Dp(36), shc)
	theme.RRect(gtx, r, gtx.Dp(16), lc.Bg)
	off := op.Offset(r.Min).Push(gtx.Ops)
	content.Add(gtx.Ops)
	off.Pop()
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

// focusRing is the keyboard focus ring around r (Slack's: the link blue
// at 30%, 3–4px outside the control).
func focusRing(gtx layout.Context, r image.Rectangle, rad int, v float32) {
	if v <= 0.01 {
		return
	}
	g := int(math.Round(float64(gtx.Dp(4)) * float64(v)))
	theme.RRect(gtx, r.Inset(-g), rad+g, theme.WithAlpha(loginRing, uint8(0x48*v)))
}

// startLogin runs the sign-in off the UI goroutine and shows its outcome on
// the card. in.Link set: desktop-style sign-in with that link.
func (a *App) startLogin(in slack.SignInInput) {
	l := a.login
	if l == nil || l.busy || a.opts.Login == nil {
		return
	}
	l.busy, l.err = true, ""
	go func() {
		err := a.opts.Login(in)
		a.Post(func() {
			l.busy = false
			if err != nil {
				l.err = err.Error()
				return
			}
			for _, ed := range []*widget.Editor{&l.link, &l.token, &l.orgToken, &l.cookie} {
				ed.SetText("")
			}
			if a.login == l {
				a.login = nil
			}
			if a.data != nil {
				a.data.start(a)
			}
		})
	}()
}

// SignInWithLink signs in with a slack:// magic-login link the OS handed
// Clack (its URL handler, a second launch forwarding its argument, or a
// macOS URL event). Safe from any goroutine, also before the window is up.
//
// It only signs in from the sign-in screen. While Clack is signed in, a
// link from outside doesn't replace the session: any web page can make the
// browser open a slack:// link, and one for someone else's account must not
// silently swap the account messages are sent from. The user signs out
// first (or pastes the link on the sign-in screen).
func (a *App) SignInWithLink(link string) {
	a.Post(func() {
		l := a.login
		if l == nil {
			a.Toast("Clack got a Slack sign-in link, but you're already signed in. To use it, sign out, then open or paste the link again.")
			return
		}
		if l.busy {
			return
		}
		l.advanced, l.focused = false, false
		l.link.SetText(link)
		a.introToSignIn() // the introduction steps aside for the card
		a.startLogin(slack.SignInInput{Link: link})
		a.Invalidate()
	})
}

// loginPara draws wrapped text at (x, y) and returns its height.
func (a *App) loginPara(gtx layout.Context, x, y, w int, st theme.TextStyle, txt string) int {
	return at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, st, txt)
	}).Size.Y
}

// loginBox draws text in a tinted, bordered box and returns its height.
func (a *App) loginBox(gtx layout.Context, x, y, w int, bg, border color.NRGBA, st theme.TextStyle, txt string) int {
	ip := gtx.Dp(12)
	m := op.Record(gtx.Ops)
	th := a.loginPara(gtx, x+ip, y+ip, w-2*ip, st, txt)
	call := m.Stop()
	r := image.Rect(x, y, x+w, y+th+2*ip)
	theme.RRect(gtx, r, gtx.Dp(6), bg)
	theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), border)
	call.Add(gtx.Ops)
	return r.Dy()
}

// loginField draws a labelled single-line field and returns its height.
// Focused, it rings in Slack's blue (the ring grows in on a spring).
func (a *App) loginField(gtx layout.Context, x, y, w int, label string, ed *widget.Editor, hint string) int {
	th, lc := a.th, theme.LightPalette
	at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, lc.Fg, label)
	})
	y += gtx.Dp(26)
	fr := image.Rect(x, y, x+w, y+gtx.Dp(44))
	v := a.login.ring(ed, gtx.Focused(ed))
	focusRing(gtx, fr, gtx.Dp(10), v)
	theme.RRect(gtx, fr, gtx.Dp(10), lc.Bg)
	bc := theme.Mix(lc.BorderStrong, loginAccent, v)
	theme.Border(gtx, fr, gtx.Dp(10), gtx.Dp(1), bc)
	at(gtx, image.Pt(x+gtx.Dp(12), y), image.Pt(w-gtx.Dp(24), gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, gtx.Dp(44), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if ed.Len() == 0 {
				th.Label(gtx, 15, theme.Regular, lc.FgSubtle, hint)
			}
			return th.Editor(gtx, ed, 15, lc.Fg, lc.Selection)
		})
	})
	return gtx.Dp(26 + 56)
}

// loginButton draws the card's primary button and returns its height. It
// darkens on hover and presses in on a spring; while signing in, a
// spinner turns beside its label.
func (a *App) loginButton(gtx layout.Context, x, y, w int, c *widget.Clickable, txt string) int {
	th, lc, l := a.th, theme.LightPalette, a.login
	br := image.Rect(x, y, x+w, y+gtx.Dp(46))
	hov, prs := l.press(c)
	focusRing(gtx, br, gtx.Dp(10), l.ring(c, gtx.Focused(c)))
	sc := 1 - 0.03*prs
	ctr := f32.Pt(float32(br.Min.X+br.Max.X)/2, float32(br.Min.Y+br.Max.Y)/2)
	tr := op.Affine(f32.Affine2D{}.Scale(ctr, f32.Pt(sc, sc))).Push(gtx.Ops)
	atExact(gtx, br.Min, br.Size(), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, c, func(gtx layout.Context, s image.Point, _ bool) {
			bg := theme.Mix(loginAccent, loginAccentDark, hov)
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(10), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			tw := th.Measure(gtx, 18, theme.Bold, txt)
			sw := 0
			if l.busy {
				sw = gtx.Dp(26)
			}
			lx := (br.Dx() - tw - sw) / 2
			if l.busy {
				l.spinner(gtx, image.Pt(lx+gtx.Dp(9), br.Dy()/2), gtx.Dp(16), lc.SendText)
			}
			at(gtx, image.Pt(lx+sw, 0), image.Pt(tw+2, br.Dy()), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, br.Dy(), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 18, theme.Bold, lc.SendText, txt)
				})
			})
			return layout.Dimensions{Size: br.Size()}
		})
	})
	tr.Pop()
	return br.Dy()
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

// loginSmallButton draws an outlined secondary button and returns its
// size. Hover tints it and a press pushes it in, both on springs.
func (a *App) loginSmallButton(gtx layout.Context, x, y int, c *widget.Clickable, txt string) image.Point {
	th, lc, l := a.th, theme.LightPalette, a.login
	bw, bh := th.Measure(gtx, 14, theme.Bold, txt)+gtx.Dp(24), gtx.Dp(32)
	hov, prs := l.press(c)
	br := image.Rect(x, y, x+bw, y+bh)
	focusRing(gtx, br, gtx.Dp(8), l.ring(c, gtx.Focused(c)))
	sc := 1 - 0.04*prs
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Pt(float32(x)+float32(bw)/2, float32(y)+float32(bh)/2), f32.Pt(sc, sc))).Push(gtx.Ops)
	defer tr.Pop()
	atExact(gtx, image.Pt(x, y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, c, func(gtx layout.Context, s image.Point, _ bool) {
			bg := theme.Mix(lc.Bg, loginInfoBg, hov)
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), theme.Mix(lc.BorderStrong, loginAccent, hov*0.6))
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 14, theme.Bold, lc.Fg, txt)
				})
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	return image.Pt(bw, bh)
}

// The card's colours: the introduction's violet (the Clack mark's) for
// the primary button, focus and the step numbers, a lilac info box.
var (
	loginInfoBg, loginInfoBorder = hexc(0xf6f3ff), hexc(0xdcd3ff)
	loginAccent, loginAccentDark = hexc(0x5b3fd6), hexc(0x4528b8)
	loginRing                    = hexc(0x7b5cff)
	loginStepBg, loginStepFg     = hexc(0xeee9ff), hexc(0x5b3fd6)
)

// loginStep is a numbered step: the number in a violet badge, the text
// beside it. It returns the step's height and the text's left edge (what
// belongs to the step goes under the text).
func (a *App) loginStep(gtx layout.Context, x, y, w, n int, txt string) (int, int) {
	lc := theme.LightPalette
	d := gtx.Dp(24)
	c := image.Pt(x+d/2, y+gtx.Dp(10))
	theme.Circle(gtx, c, d, loginStepBg)
	nt := itoa(n)
	tw := a.th.Measure(gtx, 12.5, theme.Black, nt)
	at(gtx, image.Pt(c.X-tw/2, c.Y-gtx.Dp(9)), image.Pt(tw+2, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, gtx.Dp(18), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 12.5, theme.Black, loginStepFg, nt)
		})
	})
	tx := x + d + gtx.Dp(12)
	h := a.loginPara(gtx, tx, y, w-(tx-x), theme.TextStyle{Size: 14, LineHeight: 20, Color: lc.Fg}, txt)
	return max(h, d), tx
}

// layoutLoginLink is the main path: steps on the left, the link field on
// the right. It returns the bottoms of both columns.
func (a *App) layoutLoginLink(gtx layout.Context, x, y, colW, gap int) (int, int) {
	l, lc := a.login, theme.LightPalette
	body := theme.TextStyle{Size: 14, LineHeight: 20, Color: lc.Fg}
	small := theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.FgSubtle}
	ly := y
	ly += a.loginBox(gtx, x, ly, colW, loginInfoBg, loginInfoBorder, theme.TextStyle{Size: 13, LineHeight: 19, Color: hexc(0x3b3550)},
		AppName+" signs in the way Slack’s desktop app does: your browser hands it a one-time link, and Slack gives "+
			AppName+" a session of its own. Nothing is copied from your browser.") + gtx.Dp(24)
	h, tx := a.loginStep(gtx, x, ly, colW, 1, "Open Slack’s desktop sign-in page in your browser and sign in to your workspace there.")
	ly += h + gtx.Dp(10)
	ly += a.loginSmallButton(gtx, tx, ly, &l.openSignIn, "Open sign-in page").Y + gtx.Dp(6)
	ly += a.loginPara(gtx, tx, ly, colW-(tx-x), small, desktopSignInURL) + gtx.Dp(22)
	h, tx = a.loginStep(gtx, x, ly, colW, 2, "When Slack offers to open the Slack app, either:")
	ly += h + gtx.Dp(6)
	ly += a.loginPara(gtx, tx, ly, colW-(tx-x), body, "• click it and let the browser open "+AppName+". On a Mac that works right away. On Windows, tick “Open slack:// links with Clack” in the installer; on Linux, run clack --register-url-handler once. If the Slack desktop app is installed too, the link may open it instead.") + gtx.Dp(6)
	ly += a.loginPara(gtx, tx, ly, colW-(tx-x), body, "• or, if you can see the slack:// link itself, paste it here.") + gtx.Dp(22)
	h, _ = a.loginStep(gtx, x, ly, colW, 3, "The link works once, for a few minutes. If it has expired, sign in on the page again for a new one.")
	ly += h

	fx, fy := x+colW+gap, y
	fy += a.loginField(gtx, fx, fy, colW, "Sign-in link", &l.link, "slack://…/magic-login/…")
	txt := "Sign in"
	if l.busy {
		txt = "Signing in…"
	}
	fy += a.loginButton(gtx, fx, fy, colW, &l.linkSubmit, txt)
	fy += gtx.Dp(12)
	fy += a.loginPara(gtx, fx, fy, colW, small, "The link is a secret until it’s used: don’t send it to anyone. Workspaces with single sign-on may send you back to the browser to finish.")
	return ly, fy
}

// layoutLoginPaste is Advanced: paste the browser session's token and
// cookie. It returns the bottoms of both columns.
func (a *App) layoutLoginPaste(gtx layout.Context, x, y, colW, gap int) (int, int) {
	l, lc := a.login, theme.LightPalette
	ly := y
	// The session-sharing warning comes first: this way Clack runs on the
	// browser's own session, and Slack (Enterprise Grid especially) can end
	// a session it sees used from somewhere unexpected, in every app at once.
	ly += a.loginBox(gtx, x, ly, colW, bannerWarn, color.NRGBA{R: 0xe8, G: 0xb3, B: 0x2c, A: 0xff}, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Fg},
		"This way "+AppName+" shares the session of the browser you copy these from: it is not a separate sign-in. "+
			"If Slack sees that session used in a way it finds unusual, it can sign it out everywhere, browser included "+
			"(this has happened on Enterprise Grid). Prefer signing in with a Slack link.") + gtx.Dp(20)
	h, tx := a.loginStep(gtx, x, ly, colW, 1, "Open Slack in the browser you’re signed in to, press F12 and run this in the Console:")
	ly += h + gtx.Dp(10)
	ly += a.loginBox(gtx, tx, ly, colW-(tx-x), lc.CodeBg, lc.CodeBorder, theme.TextStyle{Size: 13, LineHeight: 19, Color: lc.CodeFg, Wrap: text.WrapGraphemes}, signInSnippet) + gtx.Dp(8)
	copyTxt := "Copy snippet"
	if l.copied {
		copyTxt = "Copied"
	}
	ly += a.loginSmallButton(gtx, tx, ly, &l.copy, copyTxt).Y + gtx.Dp(22)
	h, _ = a.loginStep(gtx, x, ly, colW, 2, "Paste what it copied into Token. That fills in the org token for you.")
	ly += h + gtx.Dp(18)
	h, _ = a.loginStep(gtx, x, ly, colW, 3, "The d cookie is HttpOnly, so the snippet can’t read it. Copy its value from DevTools → Application → Cookies → https://app.slack.com. Leave d-s out: Clack doesn’t use it.")
	ly += h

	fx, fy := x+colW+gap, y
	fy += a.loginField(gtx, fx, fy, colW, "Workspace", &l.workspace, "hackclub")
	fy += a.loginField(gtx, fx, fy, colW, "Token", &l.token, "xoxc-… or the copied JSON")
	fy += a.loginField(gtx, fx, fy, colW, "Org token (optional)", &l.orgToken, "filled in from the JSON")
	fy += a.loginField(gtx, fx, fy, colW, "d cookie", &l.cookie, "xoxd-…")
	txt := "Sign in"
	if l.busy {
		txt = "Signing in…"
	}
	fy += a.loginButton(gtx, fx, fy, colW, &l.submit, txt)
	return ly, fy
}
