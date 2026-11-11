package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The introduction: what Clack shows on its first launch before the
// sign-in card, and again from Preferences > Clack > Replay introduction
// (over the signed-in app, ending on "You're all set").
//
// It is a short sequence of beats, one idea each: the mark, native and
// fast, your Slack 1:1, plugins unmodified, Hack Club, then the sign-in
// card (first launch) or "You're all set" (replay). Each beat comes in
// on springs with its pieces staggered, and its illustration is drawn
// from Clack's own UI (intro_beats.go). Beats move on by themselves; Enter,
// Space or → moves on now, ← goes back, Esc skips to the end. With
// reduced motion (osmotion.go) everything only fades.
//
// While a beat animates the intro asks for every frame
// (gtx.Execute(op.InvalidateCmd{})); once the last beat has settled it
// asks for none. All timing comes from gtx.Now, so CI can render it frame
// by frame on a virtual clock (intro_test.go, the login-shots workflow).

type introMode uint8

const (
	introFirstRun introMode = iota // ends on the sign-in card
	introReplay                    // over the signed-in app, ends on "You're all set"
)

const (
	beatLogo = iota
	beatNative
	beatSlack
	beatPlugins
	beatHackClub
	beatFinal // the sign-in card (first launch) or "You're all set" (replay)
	introBeats
)

// introDwell is how long each beat stays before the next comes in.
var introDwell = [introBeats]time.Duration{
	beatLogo:     3600 * time.Millisecond,
	beatNative:   5000 * time.Millisecond,
	beatSlack:    6000 * time.Millisecond,
	beatPlugins:  5600 * time.Millisecond,
	beatHackClub: 4800 * time.Millisecond,
}

// introWait is how long the first frame waits for the OS's reduce-motion
// answer and the mark's blurred copies before the sequence starts.
const introWait = 300 * time.Millisecond

type intro struct {
	mode    introMode
	reduced bool

	made time.Time // the first frame drawn
	t0   time.Time // the sequence's start (zero while it waits)

	cur, prev     int
	curAt, prevAt time.Time
	prevStart     time.Time // when the leaving beat had come in
	back          bool      // the last change went backwards

	signIn   bool // first launch: the sign-in card took over at signInAt
	signInAt time.Time
	toSignIn bool // a sign-in link came in: hand over at the next frame
	closing  bool // replay: fading out over the app since closeAt
	closeAt  time.Time
	gone     bool // over: the app drops it

	skipBtn, nextBtn, doneBtn widget.Clickable
	nextAnim, doneAnim        pressAnim
	tag                       int // keys and (replay) the shield over the app
	focusAsked                bool

	dock    springVal // the mark: 0 the hero in the middle, 1 docked top-left
	weights [introBeats]springVal
	bd      backdrop
	sprites introSprites
	mc      motionClock // this frame's

	rec *[]introDrawn // tests: every element drawn this frame
}

// pressAnim is a button's press and hover, on springs.
type pressAnim struct{ press, hover springVal }

// introDrawn is one element as drawn (tests).
type introDrawn struct {
	name         string
	a, dx, dy, s float32
	blur         float32
}

func newIntro(mode introMode, a *App) *intro {
	in := &intro{mode: mode, prev: -1}
	in.dock.s = springBouncy
	for i := range in.weights {
		in.weights[i].s = springGentle
	}
	in.sprites.done = a.Invalidate
	return in
}

// introWanted: the first launch shows the introduction before the sign-in
// card, once (CLACK_NO_INTRO=1 never shows it).
func (a *App) introWanted() bool {
	return !a.prefs.IntroSeen && !envFlag("NO_INTRO")
}

// startFirstRunIntro puts the introduction in front of the sign-in card
// (SetLogin, and New when it starts signed out).
func (a *App) startFirstRunIntro() {
	if a.login == nil || a.intro != nil || !a.introWanted() {
		return
	}
	if a.motionOverride == 0 {
		watchOSMotion()
	}
	a.intro = newIntro(introFirstRun, a)
}

// replayIntro is Preferences > Clack > Replay introduction: the whole
// sequence over the signed-in app, ending on "You're all set". It closes
// Preferences (and any menu) first.
func (a *App) replayIntro() {
	if a.login != nil {
		return
	}
	for a.dismissTop() {
	}
	if a.motionOverride == 0 {
		watchOSMotion()
	}
	a.intro = newIntro(introReplay, a)
	a.Invalidate()
}

// introSeen records that the introduction ran to the sign-in card (or was
// skipped there), so the next launch goes straight to sign-in.
func (a *App) introSeen() {
	if !a.prefs.IntroSeen {
		a.prefs.IntroSeen = true
		a.savePrefs()
	}
}

// introToSignIn ends the introduction at the sign-in card now (a sign-in
// link arrived from the browser).
func (a *App) introToSignIn() {
	if in := a.intro; in != nil && in.mode == introFirstRun && !in.signIn {
		in.toSignIn = true
		a.Invalidate()
	}
}

// goTo starts beat to coming in (the current one leaves).
func (in *intro) goTo(a *App, now time.Time, to int) {
	if to == in.cur || to < 0 || to >= introBeats || in.closing || in.signIn {
		return
	}
	in.back = to < in.cur
	in.prev, in.prevAt, in.prevStart = in.cur, now, in.curAt
	in.cur, in.curAt = to, now
	if to == beatFinal && in.mode == introFirstRun {
		in.signIn, in.signInAt = true, now
		a.introSeen()
	}
}

// update reads the intro's keys and buttons and moves it along. Called
// before anything else in the frame reads keys.
func (in *intro) update(gtx layout.Context, a *App) {
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	in.mc = motionClock{now: now}
	if in.made.IsZero() {
		in.made = now
		in.prefetch(gtx, a)
	}
	if in.toSignIn {
		in.toSignIn = false
		if in.t0.IsZero() {
			in.t0, in.curAt = now, now
		}
		in.goTo(a, now, beatFinal)
	}
	if in.signIn {
		// the card has the keys now; the intro only finishes its exit
		if now.Sub(in.signInAt) > 1700*time.Millisecond {
			in.gone = true
		}
		if in.prev >= 0 && now.Sub(in.prevAt) > 900*time.Millisecond {
			in.prev = -1
		}
		return
	}
	if in.t0.IsZero() {
		_, known := osReduceMotion()
		if (known && in.sprites.ready()) || now.Sub(in.made) >= introWait {
			in.t0, in.curAt = now, now
		} else {
			in.mc.moving = true // waiting: look again next frame
			in.keys(gtx)        // drop keys meanwhile
			return
		}
	}
	in.reduced = a.reduceMotion()
	next, back, skip := in.keys(gtx)
	if in.skipBtn.Clicked(gtx) {
		skip = true
	}
	if in.nextBtn.Clicked(gtx) {
		next = true
	}
	if in.doneBtn.Clicked(gtx) {
		next = true
	}
	switch {
	case in.closing || in.signIn:
	case in.cur == beatFinal: // replay's "You're all set"
		if next || skip {
			in.closing, in.closeAt = true, now
		}
	case skip:
		in.goTo(a, now, beatFinal)
	case next:
		in.goTo(a, now, in.cur+1)
	case back && in.cur > beatLogo:
		in.goTo(a, now, in.cur-1)
	default:
		if due := in.curAt.Add(introDwell[in.cur]); !now.Before(due) {
			in.goTo(a, now, in.cur+1)
		} else {
			in.mc.wakeAt(due)
		}
	}
	if in.prev >= 0 && now.Sub(in.prevAt) > 900*time.Millisecond {
		in.prev = -1 // gone off screen
	}
	if in.closing && now.Sub(in.closeAt) > 650*time.Millisecond {
		in.gone = true
	}
}

// keys reports this frame's Enter/Space/→ (next), ← (back) and Esc (skip).
func (in *intro) keys(gtx layout.Context) (next, back, skip bool) {
	for {
		e, ok := gtx.Event(
			key.Filter{Name: key.NameReturn}, key.Filter{Name: key.NameEnter}, key.Filter{Name: key.NameSpace},
			key.Filter{Name: key.NameRightArrow}, key.Filter{Name: key.NameLeftArrow}, key.Filter{Name: key.NameEscape},
		)
		if !ok {
			return
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameReturn, key.NameEnter, key.NameSpace, key.NameRightArrow:
			next = true
		case key.NameLeftArrow:
			back = true
		case key.NameEscape:
			skip = true
		}
	}
}

// prefetch asks for the blurred copies of every beat's display text.
func (in *intro) prefetch(gtx layout.Context, a *App) {
	f := in.frame(gtx, a)
	in.sprites.mark(int(math.Round(float64(f.df(heroMark)))))
	for _, r := range introWordmark {
		in.sprites.word(spriteKey{text: string(r), weight: theme.Black, px: f.textPx(heroWordmark)})
	}
	for b := beatNative; b < introBeats; b++ {
		for _, w := range splitWords(introCopy[b].title) {
			in.sprites.word(spriteKey{text: w, weight: theme.Black, px: f.textPx(titleSize)})
		}
	}
}

// ---- the frame ----

// introFrame is one frame's drawing state.
type introFrame struct {
	gtx     layout.Context
	a       *App
	in      *intro
	mc      *motionClock
	now     time.Time
	reduced bool
	u       float32 // device px per layout unit (a dp at the window's scale)
	sz      image.Point
}

func (in *intro) frame(gtx layout.Context, a *App) *introFrame {
	sz := gtx.Constraints.Max
	ppd := gtx.Metric.PxPerDp
	if ppd <= 0 {
		ppd = 1
	}
	// the layout is designed at 1280×820 dp and scales with the window
	k := float32(math.Min(float64(sz.X)/float64(1280*ppd), float64(sz.Y)/float64(820*ppd)))
	k = float32(math.Max(0.66, math.Min(1.25, float64(k))))
	now := in.mc.now
	if now.IsZero() {
		now = gtx.Now
	}
	return &introFrame{gtx: gtx, a: a, in: in, mc: &in.mc, now: now, reduced: in.reduced, u: ppd * k, sz: sz}
}

func (f *introFrame) d(v float32) int      { return int(math.Round(float64(v * f.u))) }
func (f *introFrame) df(v float32) float32 { return v * f.u }
func (f *introFrame) textPx(v float32) int { return 2 * int(math.Round(float64(v*f.u)/2)) }
func (f *introFrame) sp(v float32) unit.Sp {
	return unit.Sp(float32(f.textPx(v)) / f.gtx.Metric.PxPerSp)
}

// idp is v layout units as a Dp (icons take Dp).
func (f *introFrame) idp(v float32) unit.Dp {
	return unit.Dp(v * f.u / f.gtx.Metric.PxPerDp)
}

// fx is an element's look this frame: opacity, offset (device px), scale
// around its pivot, and blur (0 sharp … 1 the widest).
type fx struct {
	a, dx, dy, s, blur float32
}

// fxOn is fully there.
var fxOn = fx{a: 1, s: 1}

// enter is an element arriving at start: rising from rise device px below
// on spring sp, fading in, and (blur) coming into focus. Reduced motion:
// a fade only.
func (f *introFrame) enter(start time.Time, rise float32, sp spring, blur bool) fx {
	if f.reduced {
		return fx{a: float32(clamp01(f.mc.spring(springSmooth, start))), s: 1}
	}
	e := f.mc.spring(sp, start)
	o := f.mc.spring(springSnappy, start)
	out := fx{a: float32(clamp01(o)), dy: float32(1-e) * rise, s: 1}
	if blur {
		out.blur = float32(1 - clamp01(f.mc.spring(springSmooth, start)))
	}
	return out
}

// pop is an element growing in from scale s0 at start on spring sp.
func (f *introFrame) pop(start time.Time, s0 float32, sp spring) fx {
	if f.reduced {
		return fx{a: float32(clamp01(f.mc.spring(springSmooth, start))), s: 1}
	}
	e := f.mc.spring(sp, start)
	return fx{a: float32(clamp01(f.mc.spring(springSnappy, start))), s: s0 + (1-s0)*float32(e)}
}

// mul is e seen through a parent's look (an exit, the replay's fade).
func (e fx) mul(p fx) fx {
	return fx{a: e.a * p.a, dx: e.dx + p.dx, dy: e.dy + p.dy, s: e.s * p.s, blur: max(e.blur, p.blur)}
}

// with draws w under e (pivot in device px): one opacity layer only while
// it is partly transparent.
func (f *introFrame) with(name string, e fx, pivot f32.Point, w func()) {
	f.record(name, e)
	if e.a <= 0.004 {
		return
	}
	aff := f32.Affine2D{}
	if e.s != 1 && e.s > 0 {
		aff = aff.Scale(pivot, f32.Pt(e.s, e.s))
	}
	aff = aff.Offset(f32.Pt(e.dx, e.dy))
	t := op.Affine(aff).Push(f.gtx.Ops)
	if e.a < 0.997 {
		o := paint.PushOpacity(f.gtx.Ops, e.a)
		w()
		o.Pop()
	} else {
		w()
	}
	t.Pop()
}

func (f *introFrame) record(name string, e fx) {
	if f.in.rec != nil {
		*f.in.rec = append(*f.in.rec, introDrawn{name: name, a: e.a, dx: e.dx, dy: e.dy, s: e.s, blur: e.blur})
	}
}

// label draws one line of text with its top-left at p; it returns the
// width.
func (f *introFrame) label(p image.Point, size float32, w font.Weight, c color.NRGBA, s string) int {
	g := f.gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	defer op.Offset(p).Push(f.gtx.Ops).Pop()
	return f.a.th.Label(g, f.sp(size), w, c, s).Size.X
}

// measure is the width label would draw s at.
func (f *introFrame) measure(size float32, w font.Weight, s string) int {
	return f.a.th.Measure(f.gtx, f.sp(size), w, s)
}

// spaceW is a word space's width (measured between letters: a lone space
// can shape to nothing).
func (f *introFrame) spaceW(size float32, w font.Weight) int {
	return f.measure(size, w, "H H") - f.measure(size, w, "HH")
}

// baseline is the distance from a line's top to its baseline.
func (f *introFrame) baseline(size float32, w font.Weight) int {
	g := f.gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	m := op.Record(f.gtx.Ops)
	d := f.a.th.Label(g, f.sp(size), w, theme.LightPalette.Fg, "Hg")
	m.Stop()
	return d.Size.Y - d.Baseline
}

// word draws a display word at p (top-left, device px) in e's state: the
// blurred copies while e.blur is up, Gio's sharp text as it settles.
func (f *introFrame) word(name, s string, p image.Point, size float32, w font.Weight, c color.NRGBA, e fx) {
	f.record(name, e)
	if e.a <= 0.004 {
		return
	}
	gw := f.measure(size, w, s)
	pivot := f32.Pt(float32(p.X)+float32(gw)/2, float32(p.Y)+f.df(size)*0.6)
	aff := f32.Affine2D{}
	if e.s != 1 && e.s > 0 {
		aff = aff.Scale(pivot, f32.Pt(e.s, e.s))
	}
	t := op.Affine(aff.Offset(f32.Pt(e.dx, e.dy))).Push(f.gtx.Ops)
	defer t.Pop()
	sharp := float32(1)
	if e.blur > 0.01 {
		if tb := f.in.sprites.word(spriteKey{text: s, weight: w, px: f.textPx(size)}); tb != nil && tb[0].ok && tb[1].ok {
			bl := float32(f.baseline(size, w))
			wa, wb := float32(0), float32(0)
			if e.blur >= 0.5 {
				wa = (e.blur - 0.5) * 2
				wb = 1 - wa
				sharp = 0
			} else {
				wb = e.blur * 2
				sharp = 1 - wb
			}
			for i, wt := range [2]float32{wa, wb} {
				lv := tb[i]
				if wt*e.a < 0.004 {
					continue
				}
				sx := float32(1)
				if lv.adv > 0 {
					sx = float32(gw) / lv.adv
				}
				o := f32.Pt(float32(p.X)-lv.left*sx, float32(p.Y)+bl-lv.asc)
				st := paint.PushOpacity(f.gtx.Ops, wt*e.a)
				tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx/lv.res, 1/lv.res)).Offset(o)).Push(f.gtx.Ops)
				lv.op.Add(f.gtx.Ops)
				paint.PaintOp{}.Add(f.gtx.Ops)
				tr.Pop()
				st.Pop()
			}
		} else {
			sharp = 1 - e.blur*0.6 // no copy yet: fade the text instead
		}
	}
	if a := sharp * e.a; a > 0.004 {
		f.label(p, size, w, alphaMul(c, a), s)
	}
}

// ---- layout constants (layout units: dp at 1280×820) ----

const (
	heroMark      = 124 // the mark in the logo beat
	heroWordmark  = 64
	dockMark      = 26
	titleSize     = 46
	subSize       = 18.5
	eyebrowSize   = 12.5
	stageW        = 700
	stageH        = 330
	introBarH     = 92 // the bottom bar's band
	introBarInset = 40
)

var introWordmark = []rune("Clack")

// introCopy is each beat's words: a small label, the line, one sentence.
var introCopy = [introBeats]struct{ eyebrow, title, sub string }{
	beatLogo:     {"", "", "A native Slack client, made for Hack Club."},
	beatNative:   {"NATIVE", "Native, and fast.", "Written in Go and drawn by your GPU. No Electron, no browser tab, no waiting."},
	beatSlack:    {"YOUR SLACK", "It's your Slack, 1:1.", "The same sidebar, threads and shortcuts, signed in to your own workspace."},
	beatPlugins:  {"PLUGINS", "Your plugins, unmodified.", "Taut and Slick plugins and themes load as they are: the same JavaScript, the same components."},
	beatHackClub: {"HACK CLUB", "Made for Hack Club.", "Built for the Hack Club Slack, so #ship, #scrapbook and your DMs are a click away."},
	beatFinal:    {"", "You're all set.", "That's Clack. Your workspace is right where you left it."},
}

// introAccent is each beat's label colour.
var introAccent = [introBeats]color.NRGBA{
	beatNative:   hexc(0x9d8cff),
	beatSlack:    hexc(0xe39be8),
	beatPlugins:  hexc(0xffc56b),
	beatHackClub: hexc(0xff6b7f),
	beatFinal:    hexc(0x9d8cff),
}

func splitWords(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		if r == ' ' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// ---- drawing ----

// layoutWelcome is the window while Clack isn't signed in: the backdrop,
// the introduction on the first launch, then the sign-in card.
func (a *App) layoutWelcome(gtx layout.Context) {
	in := a.intro
	if in != nil && in.mode != introFirstRun {
		a.intro, in = nil, nil // signed out mid-replay
	}
	if in == nil && a.welcome == nil {
		a.welcome = newIntro(introFirstRun, a) // just the backdrop's state
		a.welcome.signIn = true
		if a.motionOverride == 0 {
			watchOSMotion() // the card's shake follows the OS setting
		}
	}
	host := in
	if host == nil {
		host = a.welcome
		host.mc = motionClock{now: gtx.Now}
		if host.mc.now.IsZero() {
			host.mc.now = time.Now()
		}
		host.reduced = a.reduceMotion()
	} else {
		in.update(gtx, a)
	}
	f := host.frame(gtx, a)
	if in != nil && in.t0.IsZero() {
		theme.Fill(gtx, image.Rectangle{Max: f.sz}, introBase) // waiting for the first frame of the sequence
		in.mc.schedule(gtx)
		return
	}
	host.drawBackdrop(f)
	var enter time.Time
	if in != nil {
		in.drawBeats(f)
		enter = in.signInAt
	}
	host.drawDock(f)
	if in == nil || in.signIn {
		a.layoutLogin(gtx, enter, f.mc)
	}
	if in != nil && in.gone {
		a.intro = nil
		a.welcome = nil
	}
	host.mc.schedule(gtx)
}

// introInput reads the replay's keys and buttons at the top of the
// signed-in frame, before any shortcut or focused editor can take them.
func (a *App) introInput(gtx layout.Context) {
	if in := a.intro; in != nil && in.mode == introReplay {
		in.update(gtx, a)
		if !in.focusAsked {
			// off the composer: typing during the replay goes nowhere
			gtx.Execute(key.FocusCmd{Tag: &in.tag})
			in.focusAsked = true
		}
	}
}

// layoutIntroOverlay draws the replay over the signed-in app (the last
// thing in the frame): the backdrop fades in over the app, and out again
// after "You're all set".
func (a *App) layoutIntroOverlay(gtx layout.Context) {
	in := a.intro
	if in == nil || in.mode != introReplay {
		return
	}
	f := in.frame(gtx, a)
	sz := f.sz
	// the shield: the app under the overlay gets no pointer
	area := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, &in.tag)
	pointer.CursorDefault.Add(gtx.Ops)
	area.Pop()
	if in.t0.IsZero() {
		in.mc.schedule(gtx)
		return
	}
	o := fx{a: float32(clamp01(f.mc.spring(springSmooth, in.t0))), s: 1}
	if in.closing {
		c := f.mc.spring(springSmooth, in.closeAt)
		o.a *= float32(1 - clamp01(c))
		if !f.reduced {
			o.s = 1 + 0.035*float32(clamp01(c))
		}
	}
	f.with("overlay", o, f32.Pt(float32(sz.X)/2, float32(sz.Y)/2), func() {
		in.drawBackdrop(f)
		in.drawBeats(f)
		in.drawDock(f)
	})
	if in.gone {
		a.intro = nil
	}
	in.mc.schedule(gtx)
}

// schedule asks for the next frame: now while anything moves, else at the
// next moment something will.
func (m *motionClock) schedule(gtx layout.Context) {
	switch {
	case m.moving:
		gtx.Execute(op.InvalidateCmd{})
	case !m.wake.IsZero():
		gtx.Execute(op.InvalidateCmd{At: m.wake})
	}
}

// drawBackdrop lights the window for the beat on screen.
func (in *intro) drawBackdrop(f *introFrame) {
	st := backdropState{light: 1}
	target := in.cur
	if in.signIn {
		target = beatFinal
	}
	for i := range in.weights {
		w := 0.0
		if i == target {
			w = 1
		}
		if !in.weights[i].init {
			// the first frame: the first beat's light, and nothing else
			in.weights[i].set(f.now, w)
		}
		v, moving := in.weights[i].get(f.now, w)
		if moving {
			f.mc.moving = true
		}
		st.w[i] = float32(clamp01(v))
	}
	if !in.t0.IsZero() && in.mode == introFirstRun && !in.signIn {
		// first launch: the light comes up as the mark arrives
		st.light = float32(clamp01(f.mc.spring(springGentle, in.t0)))
	}
	if !f.reduced && !in.t0.IsZero() {
		st.t = f.now.Sub(in.t0).Seconds()
		st.drift = 1
		if in.signIn {
			// the lights come to rest under the sign-in card
			st.drift = float32(1 - clamp01(f.mc.spring(springGentle, in.signInAt)))
			if st.drift < 0.002 {
				st.drift = 0
			}
		}
		if in.cur == beatFinal && in.mode == introReplay {
			st.drift = float32(1 - clamp01(f.mc.spring(springGentle, in.curAt)))
		}
		if st.drift > 0 {
			f.mc.moving = true
		}
	}
	in.bd.draw(f.gtx, st)
}

// drawBeats draws the beat leaving and the beat coming in, then the
// bottom bar.
func (in *intro) drawBeats(f *introFrame) {
	if in.prev >= 0 && in.prev != beatFinal {
		in.drawBeat(f, in.prev, in.leave(f))
	}
	if !(in.signIn && in.cur == beatFinal) {
		in.drawBeat(f, in.cur, fxOn)
	}
	in.drawBar(f)
}

// leave is the look of the beat on its way out: a quick fade, drifting
// away from the one coming in.
func (in *intro) leave(f *introFrame) fx {
	e := f.mc.spring(spring{response: 0.3, damping: 1}, in.prevAt)
	out := fx{a: float32(1 - clamp01(e*1.25)), s: 1}
	if !f.reduced {
		dir := float32(-1)
		if in.back {
			dir = 1
		}
		out.dx = dir * f.df(48) * float32(e)
		if in.signIn {
			out.dx, out.dy = 0, -f.df(26)*float32(e)
		}
		out.blur = float32(clamp01(e)) * 0.6
	}
	return out
}

// beatStart is when beat b's pieces started coming in (the leaving
// beat keeps its own clock: a beat skipped half-way leaves as it was).
func (in *intro) beatStart(b int) time.Time {
	if b == in.cur {
		return in.curAt
	}
	return in.prevStart
}

func (in *intro) drawBeat(f *introFrame, b int, parent fx) {
	start := in.beatStart(b)
	if b == beatLogo {
		in.drawLogoBeat(f, start, parent)
		return
	}
	g := in.geom(f)
	// the illustration slides in from the side it comes from
	stage := fxOn
	if b == in.cur && !f.reduced && b != beatFinal {
		dir := float32(1)
		if in.back {
			dir = -1
		}
		e := f.mc.spring(springBouncy, start)
		stage.dx = dir * f.df(64) * float32(1-e)
	}
	stage = stage.mul(parent)
	if b == beatFinal {
		in.drawFinalBeat(f, g, start, parent)
		return
	}
	// the stage's look goes down to each piece of the illustration (each
	// multiplies it), so no layer is nested in another
	in.drawStage(f, b, g, start, stage)
	in.drawText(f, b, g, start, parent)
}

// introGeom is where a content beat's parts go this frame.
type introGeom struct {
	stage        image.Rectangle
	eyebrowY     int
	titleY, subY int
	cx           int
	barY         int // the bottom bar's centre
	textW        int // the sentence's wrap width
	titleH       int
	subLineH     int
}

func (in *intro) geom(f *introFrame) introGeom {
	sz := f.sz
	var g introGeom
	g.cx = sz.X / 2
	g.barY = sz.Y - f.d(introBarH/2) - f.d(6)
	g.titleH = f.d(titleSize * 1.18)
	g.subLineH = f.d(subSize * 1.5)
	g.textW = min(f.d(600), sz.X-f.d(80))
	// stage, gap, label, title, sub (two lines), centred above the bar
	total := f.d(stageH) + f.d(46) + f.d(eyebrowSize*1.4) + f.d(12) + g.titleH + f.d(12) + 2*g.subLineH
	avail := sz.Y - f.d(introBarH) - f.d(52)
	top := f.d(52) + max(0, (avail-total)/2)
	sw := min(f.d(stageW), sz.X-f.d(48))
	g.stage = image.Rect(g.cx-sw/2, top, g.cx+sw/2, top+f.d(stageH))
	g.eyebrowY = g.stage.Max.Y + f.d(46)
	g.titleY = g.eyebrowY + f.d(eyebrowSize*1.4) + f.d(12)
	g.subY = g.titleY + g.titleH + f.d(12)
	return g
}

// drawText is a beat's label, its line word by word coming into focus,
// and its sentence.
func (in *intro) drawText(f *introFrame, b int, g introGeom, start time.Time, parent fx) {
	cp := introCopy[b]
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if cp.eyebrow != "" {
		e := f.enter(start.Add(msec(140)), f.df(10), springSmooth, false).mul(parent)
		f.with("eyebrow", e, f32.Point{}, func() {
			f.tracked(image.Pt(g.cx, g.eyebrowY), eyebrowSize, theme.Black, introAccent[b], cp.eyebrow, 2.2)
		})
	}
	words := splitWords(cp.title)
	space := f.spaceW(titleSize, theme.Black)
	widths := make([]int, len(words))
	total := 0
	for i, w := range words {
		widths[i] = f.measure(titleSize, theme.Black, w)
		total += widths[i]
	}
	total += space * max(0, len(words)-1)
	x := g.cx - total/2
	for i, w := range words {
		e := f.enter(start.Add(msec(200+float64(i)*62)), f.df(18), springBouncy, true).mul(parent)
		f.word("title", w, image.Pt(x, g.titleY), titleSize, theme.Black, white, e)
		x += widths[i] + space
	}
	e := f.enter(start.Add(msec(200+float64(len(words))*62+90)), f.df(12), springSmooth, false).mul(parent)
	f.with("sub", e, f32.Point{}, func() {
		f.para(image.Pt(g.cx, g.subY), g.textW, subSize, alphaMul(white, 0.66), cp.sub)
	})
}

// para draws centred, wrapped text whose top-centre is p.
func (f *introFrame) para(p image.Point, w int, size float32, c color.NRGBA, s string) {
	g := f.gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
	defer op.Offset(image.Pt(p.X-w/2, p.Y)).Push(f.gtx.Ops).Pop()
	f.a.th.Layout(g, theme.TextStyle{Size: f.sp(size), LineHeight: f.sp(size * 1.5), Color: c, Align: text.Middle, MaxLines: 3}, s)
}

// tracked draws spaced-out capitals centred on p (Gio has no letter
// spacing: one glyph at a time).
func (f *introFrame) tracked(p image.Point, size float32, w font.Weight, c color.NRGBA, s string, track float32) {
	tr := f.df(track)
	var ws []int
	total := 0
	for _, r := range s {
		cw := f.measure(size, w, string(r))
		ws = append(ws, cw)
		total += cw
	}
	total += int(tr * float32(max(0, len(ws)-1)))
	x := float32(p.X - total/2)
	i := 0
	for _, r := range s {
		f.label(image.Pt(int(math.Round(float64(x))), p.Y), size, w, c, string(r))
		x += float32(ws[i]) + tr
		i++
	}
}

// ---- the logo beat and the docked mark ----

// heroRects are the mark's places: in the middle (the logo beat) and
// docked top-left (every other beat, the sign-in card).
func (in *intro) heroRects(f *introFrame) (hero, dock frect) {
	sz := f.sz
	hs := f.df(heroMark)
	cy := float32(sz.Y)*0.43 - f.df(40)
	hero = frect{f32.Pt(float32(sz.X)/2-hs/2, cy-hs/2), f32.Pt(float32(sz.X)/2+hs/2, cy+hs/2)}
	ds := f.df(dockMark)
	dock = frect{f32.Pt(f.df(30), f.df(26)), f32.Pt(f.df(30)+ds, f.df(26)+ds)}
	return hero, dock
}

// frect is a rectangle in device px (Gio's f32 has none).
type frect struct{ Min, Max f32.Point }

func (r frect) Dx() float32 { return r.Max.X - r.Min.X }
func (r frect) Dy() float32 { return r.Max.Y - r.Min.Y }

// drawLogoBeat is the mark blurring into focus on a spring, its amber
// strokes going clack with a ring of light, then the wordmark letter by
// letter and the line under it.
func (in *intro) drawLogoBeat(f *introFrame, start time.Time, parent fx) {
	hero, _ := in.heroRects(f)
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	// the wordmark under the mark
	wy := int(hero.Max.Y) + f.d(30)
	widths := make([]int, len(introWordmark))
	total := 0
	for i, r := range introWordmark {
		widths[i] = f.measure(heroWordmark, theme.Black, string(r))
		total += widths[i]
	}
	// a little tighter than the font's own spacing, like a logotype
	tight := f.d(1.6)
	total -= tight * (len(widths) - 1)
	x := f.sz.X/2 - total/2
	for i, r := range introWordmark {
		e := f.enter(start.Add(msec(620+float64(i)*48)), f.df(22), springBouncy, true).mul(parent)
		f.word("wordmark", string(r), image.Pt(x, wy), heroWordmark, theme.Black, white, e)
		x += widths[i] - tight
	}
	e := f.enter(start.Add(msec(1080)), f.df(12), springSmooth, false).mul(parent)
	f.with("tagline", e, f32.Point{}, func() {
		f.para(image.Pt(f.sz.X/2, wy+f.d(heroWordmark*1.25)), min(f.d(620), f.sz.X-f.d(60)), 19, alphaMul(white, 0.62), introCopy[beatLogo].sub)
	})
}

// drawDock draws the mark: the hero of the logo beat, flying to the
// top-left corner (with the wordmark beside it) when the beats begin.
func (in *intro) drawDock(f *introFrame) {
	hero, dock := in.heroRects(f)
	target := 1.0
	if in.cur == beatLogo && !in.signIn && !in.t0.IsZero() {
		target = 0
	}
	if in.mode == introReplay && in.cur == beatFinal {
		target = 1
	}
	if !in.dock.init {
		in.dock.set(f.now, target)
	}
	t, moving := in.dock.get(f.now, target)
	if moving {
		f.mc.moving = true
	}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if f.reduced {
		// no flight: the hero fades where it is, the docked mark fades in
		heroA := float32(1 - clamp01(t))
		if heroA > 0.004 {
			in.drawHero(f, hero, fx{a: heroA, s: 1})
		}
		if da := float32(clamp01(t)); da > 0.004 {
			f.with("dock", fx{a: da, s: 1}, f32.Point{}, func() {
				drawMark(f.gtx, dock.Min, dock.Dx(), markParts{lines: [2]float32{1, 1}})
				f.label(image.Pt(int(dock.Max.X)+f.d(10), int(dock.Min.Y)+(int(dock.Dy())-f.d(17*1.25))/2), 17, theme.Black, white, AppName)
			})
		}
		return
	}
	tt := float32(t)
	r := frect{f32.Pt(lerpf(hero.Min.X, dock.Min.X, tt), lerpf(hero.Min.Y, dock.Min.Y, tt)), f32.Pt(lerpf(hero.Max.X, dock.Max.X, tt), lerpf(hero.Max.Y, dock.Max.Y, tt))}
	if tt < 0.002 {
		in.drawHero(f, hero, fxOn)
	} else {
		f.record("mark", fx{a: 1, dx: r.Min.X - hero.Min.X, dy: r.Min.Y - hero.Min.Y, s: r.Dx() / hero.Dx()})
		drawMark(f.gtx, r.Min, r.Dx(), markParts{lines: [2]float32{1, 1}})
	}
	// the small wordmark beside the docked mark
	if la := float32(clamp01((t - 0.55) / 0.45)); la > 0.004 {
		e := fx{a: la, dx: f.df(8) * (1 - la), s: 1}
		f.with("dock-name", e, f32.Point{}, func() {
			f.label(image.Pt(int(dock.Max.X)+f.d(10), int(dock.Min.Y)+(int(dock.Dy())-f.d(17*1.25))/2), 17, theme.Black, white, AppName)
		})
	}
}

// drawHero is the logo beat's mark in its arrival: scale (springing past
// 1), opacity and blur, then the clack.
func (in *intro) drawHero(f *introFrame, hero frect, parent fx) {
	start := in.t0.Add(msec(120))
	if in.cur != beatLogo && in.prev != beatLogo {
		start = in.t0.Add(-time.Hour) // long arrived
	}
	e := f.pop(start, 0.62, springBouncy)
	if !f.reduced {
		e.blur = float32(1 - clamp01(f.mc.spring(spring{response: 0.5, damping: 1}, start)))
		e.dy = f.df(14) * (1 - float32(clamp01(f.mc.spring(springBouncy, start))))
	}
	e = e.mul(parent)
	f.record("mark", e)
	if e.a <= 0.004 {
		return
	}
	c := f32.Pt((hero.Min.X+hero.Max.X)/2, (hero.Min.Y+hero.Max.Y)/2)
	parts := markParts{lines: [2]float32{1, 1}}
	if !f.reduced {
		for i := range parts.lines {
			parts.lines[i] = float32(clamp01(f.mc.spring(springPop, start.Add(msec(560+float64(i)*80)))))
		}
		if p := f.now.Sub(start.Add(msec(600))).Seconds() / 1.1; p > 0 && p < 1 {
			parts.ring = float32(p)
			f.mc.moving = true
		}
		if p := f.now.Sub(start.Add(msec(1250))).Seconds() / 0.9; p > 0 && p < 1 {
			parts.sheen = float32(p)
			f.mc.moving = true
		}
	}
	aff := f32.Affine2D{}.Scale(c, f32.Pt(e.s, e.s)).Offset(f32.Pt(e.dx, e.dy))
	t := op.Affine(aff).Push(f.gtx.Ops)
	defer t.Pop()
	if parts.ring > 0 {
		drawMarkRing(f.gtx, c, hero.Dx(), parts.ring)
	}
	sharp := float32(1)
	if e.blur > 0.01 {
		if tb := in.sprites.mark(int(math.Round(float64(hero.Dx())))); tb != nil && tb[0].ok && tb[1].ok {
			wa, wb := float32(0), float32(0)
			if e.blur >= 0.5 {
				wa, sharp = (e.blur-0.5)*2, 0
				wb = 1 - wa
			} else {
				wb = e.blur * 2
				sharp = 1 - wb
			}
			for i, wt := range [2]float32{wa, wb} {
				lv := tb[i]
				if wt*e.a < 0.004 {
					continue
				}
				o := f32.Pt(hero.Min.X-lv.left, hero.Min.Y-lv.asc)
				st := paint.PushOpacity(f.gtx.Ops, wt*e.a)
				tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(1/lv.res, 1/lv.res)).Offset(o)).Push(f.gtx.Ops)
				lv.op.Add(f.gtx.Ops)
				paint.PaintOp{}.Add(f.gtx.Ops)
				tr.Pop()
				st.Pop()
			}
			// the amber strokes aren't in the copies: they go with the sharp mark
			parts.lines = [2]float32{parts.lines[0] * sharp, parts.lines[1] * sharp}
		}
	}
	if a := sharp * e.a; a > 0.004 {
		if a < 0.997 {
			st := paint.PushOpacity(f.gtx.Ops, a)
			drawMark(f.gtx, hero.Min, hero.Dx(), parts)
			st.Pop()
		} else {
			drawMark(f.gtx, hero.Min, hero.Dx(), parts)
		}
	}
}

func lerpf(a, b, t float32) float32 { return a + (b-a)*t }

// ---- the bottom bar ----

// drawBar is Skip on the left, the beats' progress in the middle and
// Continue on the right; it fades in after the mark, and out for the
// sign-in card and "You're all set".
func (in *intro) drawBar(f *introFrame) {
	if in.t0.IsZero() {
		return
	}
	show := !in.signIn && in.cur != beatFinal
	var e fx
	switch {
	case show && in.prev < 0 && in.cur == beatLogo:
		e = f.enter(in.t0.Add(msec(1500)), f.df(10), springSmooth, false)
	case show:
		e = fxOn
	default:
		at := in.curAt
		if in.signIn {
			at = in.signInAt
		}
		o := f.mc.spring(spring{response: 0.3, damping: 1}, at)
		e = fx{a: float32(1 - clamp01(o*1.3)), s: 1}
	}
	if e.a <= 0.004 {
		f.record("bar", e)
		return
	}
	g := in.geom(f)
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	f.with("bar", e, f32.Point{}, func() {
		// Skip, with its key
		sx := f.d(introBarInset)
		skipW := f.measure(14, theme.Bold, "Skip") + f.d(10) + f.keycapW("esc")
		bh := f.d(38)
		atExact(f.gtx, image.Pt(sx-f.d(12), g.barY-bh/2), image.Pt(skipW+f.d(24), bh), func(gtx layout.Context) layout.Dimensions {
			return in.skipBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				hov := in.skipBtn.Hovered()
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, f.d(10), alphaMul(white, 0.07))
				}
				c := alphaMul(white, 0.58)
				if hov {
					c = alphaMul(white, 0.92)
				}
				ty := (bh - f.d(14*1.25)) / 2
				w := f.label(image.Pt(f.d(12), ty), 14, theme.Bold, c, "Skip")
				f.keycap(image.Pt(f.d(12)+w+f.d(10), (bh-f.d(20))/2), "esc", 0.62)
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		})
		// progress: one bar per beat, the current one filling
		n := beatFinal
		segW, gap, segH := f.d(34), f.d(7), max(2, f.d(3.5))
		tw := n*segW + (n-1)*gap
		px := g.cx - tw/2
		for i := 0; i < n; i++ {
			r := image.Rect(px+i*(segW+gap), g.barY-segH/2, px+i*(segW+gap)+segW, g.barY-segH/2+segH)
			theme.RRect(f.gtx, r, segH/2, alphaMul(white, 0.16))
			fill := 0.0
			switch {
			case i < in.cur:
				fill = 1
			case i == in.cur && !in.signIn:
				fill = clamp01(f.now.Sub(in.curAt).Seconds() / introDwell[i].Seconds())
				if fill < 1 {
					f.mc.moving = true
				}
			}
			if fw := int(math.Round(fill * float64(segW))); fw > 0 {
				theme.RRect(f.gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+max(fw, segH), r.Max.Y), segH/2, alphaMul(white, 0.9))
			}
		}
		// Continue, with Enter's key
		label := "Continue"
		if in.cur == beatHackClub {
			label = "Sign in"
			if in.mode == introReplay {
				label = "Finish"
			}
		}
		in.pillButton(f, &in.nextBtn, &in.nextAnim, label, image.Pt(f.sz.X-f.d(introBarInset), g.barY), true)
	})
}

// pillButton is the intro's primary button (white, dark label, Enter's
// key), its right-centre at rc (or centred on rc when !right). It presses
// in on a spring.
func (in *intro) pillButton(f *introFrame, b *widget.Clickable, pa *pressAnim, label string, rc image.Point, right bool) {
	ink := hexc(0x0b0912)
	lw := f.measure(15, theme.Bold, label)
	h := f.d(42)
	w := f.d(20) + lw + f.d(10) + f.keycapW("↵") + f.d(14)
	x := rc.X - w
	if !right {
		x = rc.X - w/2
	}
	r := image.Rect(x, rc.Y-h/2, x+w, rc.Y-h/2+h)
	press := 0.0
	if b.Pressed() {
		press = 1
	}
	hov := 0.0
	if b.Hovered() {
		hov = 1
	}
	if pa.press.s.response == 0 {
		pa.press.s, pa.hover.s = springSnappy, springSnappy
	}
	pv, m1 := pa.press.get(f.now, press)
	hv, m2 := pa.hover.get(f.now, hov)
	if m1 || m2 {
		f.mc.moving = true
	}
	s := float32(1 - 0.045*pv)
	c := f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	t := op.Affine(f32.Affine2D{}.Scale(c, f32.Pt(s, s))).Push(f.gtx.Ops)
	defer t.Pop()
	// a soft light under it, stronger on hover
	theme.Shadow(f.gtx, r, h/2, f.d(14), alphaMul(hexc(0xb7a8ff), float32(0.10+0.16*hv)))
	theme.RRect(f.gtx, r, h/2, theme.Mix(hexc(0xeeeaf8), hexc(0xffffff), float32(hv)))
	f.label(image.Pt(r.Min.X+f.d(20), r.Min.Y+(h-f.d(15*1.25))/2), 15, theme.Bold, ink, label)
	f.keycapDark(image.Pt(r.Min.X+f.d(20)+lw+f.d(10), r.Min.Y+(h-f.d(22))/2), "↵")
	atExact(f.gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: gtx.Constraints.Max}
		})
	})
}

func (f *introFrame) keycapW(s string) int {
	return max(f.d(22), f.measure(11.5, theme.Bold, s)+f.d(12))
}

// keycap is a key's name in a little outlined key (on the dark backdrop).
func (f *introFrame) keycap(p image.Point, s string, a float32) {
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	w, h := f.keycapW(s), f.d(20)
	r := image.Rect(p.X, p.Y, p.X+w, p.Y+h)
	theme.RRect(f.gtx, r, f.d(5), alphaMul(white, 0.12*a))
	theme.Border(f.gtx, r, f.d(5), max(1, f.d(1)), alphaMul(white, 0.44*a))
	tw := f.measure(11.5, theme.Bold, s)
	f.label(image.Pt(r.Min.X+(w-tw)/2, r.Min.Y+(h-f.d(11.5*1.25))/2), 11.5, theme.Bold, alphaMul(white, a), s)
}

// keycapDark is a key on the white button.
func (f *introFrame) keycapDark(p image.Point, s string) {
	w, h := f.keycapW(s), f.d(22)
	r := image.Rect(p.X, p.Y, p.X+w, p.Y+h)
	theme.RRect(f.gtx, r, f.d(6), hexc(0xdcd6ec))
	tw := f.measure(12, theme.Bold, s)
	f.label(image.Pt(r.Min.X+(w-tw)/2, r.Min.Y+(h-f.d(12*1.25))/2), 12, theme.Bold, hexc(0x3a3450), s)
}
