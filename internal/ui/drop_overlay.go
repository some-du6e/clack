package ui

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Files dragged onto the window from the OS (Gio patch 11's
// app.FileDropEvent), as Slack's FileDragDrop (boot-shared oCSS, build
// 4d0c78085232) does them for the two panes that take drops:
//
//   - location MESSAGE_PANE wraps the channel's primary view body (the
//     message list and the composer, under the header), location THREAD
//     the thread flexpane's body. Each has its own drag state, so only the
//     pane under the pointer shows its overlay. Clack's panes are the
//     convos layoutConvo lays out (channel, split card, thread pane); each
//     registers its window rectangle as a drop zone per frame.
//   - A disabled one (G||$ in the message pane: a channel preview or an
//     archived channel, where the composer gives way to a footer) shows
//     nothing and takes nothing.
//   - The overlay (MessagePaneDragOverlay cCrv, threads DragOverlay 6Uf7):
//     .p-message_pane_drag_overlay / .p-thread_drag_overlay, z-index 200,
//     inset -1px 0 0, rgba(sk_primary_background, .95), centred info:
//     the sprite animation (dragdrop_sprites@2x / dragdrop_threads_sprites
//     @2x, the night cards from 20:00 to 08:00 local time), the title at
//     28px/1.2143 black, the subtitle at 15px/1.46668 in content-sec
//     (message pane) or sk_foreground_max_solid (thread) with a slim
//     KeyboardKey "Shift".
//   - onDrop: a plain drop is uploadDraftFiles (the pane's composer
//     attaches them); Shift is uploadFiles with shareData (shared at once,
//     the draft untouched). Nothing happens where the viewer can't post
//     (the overlay says "Read only") or the workspace disables uploads
//     (disable_file_uploads "disable_all": "Uploads disabled").
//
// The window answers the OS with a copy either way, as Slack's dragover
// sets dropEffect "copy"; Esc during the drag is the OS's (a DragLeave).

// dropState is the OS file drag over the window (UI goroutine).
type dropState struct {
	on    bool        // a drag carrying files is over the window
	pos   image.Point // the pointer, window pixels
	hover *convo      // the drop zone under the pointer
	since time.Time   // when the hovered pane's overlay first drew (its animation)
	zones []dropZone  // the panes that take drops, as last laid out
}

type dropZone struct {
	c *convo
	r image.Rectangle // window coordinates
}

// fileDrop handles one app.FileDropEvent.
func (a *App) fileDrop(e app.FileDropEvent) {
	p := image.Pt(int(e.Position.X+.5), int(e.Position.Y+.5))
	switch e.Kind {
	case app.FileDragEnter, app.FileDragOver:
		a.drop.on, a.drop.pos = true, p
		a.dropHover(a.dropZoneAt(p))
	case app.FileDragLeave:
		if a.drop.on || a.drop.hover != nil {
			a.drop.on = false
			a.dropHover(nil)
		}
	case app.FileDrop:
		// a drop can come without an enter (WM_DROPFILES)
		c := a.dropZoneAt(p)
		a.drop.on = false
		a.dropHover(nil)
		if c != nil {
			a.dropFiles(c, e.Paths, e.Modifiers.Contain(key.ModShift))
		}
	}
}

// dropHover shows c's overlay (nil: none), redrawing on a change.
func (a *App) dropHover(c *convo) {
	if a.drop.hover == c {
		return
	}
	a.drop.hover, a.drop.since = c, time.Time{}
	a.Invalidate()
}

// dropZoneAt is the drop zone at window point p; the last laid out wins.
func (a *App) dropZoneAt(p image.Point) *convo {
	for i := len(a.drop.zones) - 1; i >= 0; i-- {
		if z := a.drop.zones[i]; p.In(z.r) {
			return z.c
		}
	}
	return nil
}

// dropTop is where c's FileDragDrop container starts in its pane: under
// the thread pane's header (messages.ThreadView.DropTop), else the top.
func dropTop(c *convo) int {
	if t, ok := c.list.(interface{ DropTop() int }); ok {
		return t.DropTop()
	}
	return 0
}

// dropZonesReset starts a frame's drop zones.
func (a *App) dropZonesReset() { a.drop.zones = a.drop.zones[:0] }

// dropZone registers c's pane (window rectangle r) when it takes drops:
// FileDragDrop's disabled prop is a preview or archived channel, whose
// footer replaces the composer.
func (a *App) dropZone(c *convo, r image.Rectangle, foot footerKind) {
	if foot != footNone || c.comp == nil {
		return
	}
	r.Min.Y += dropTop(c)
	a.drop.zones = append(a.drop.zones, dropZone{c: c, r: r})
}

// dropAttacher is what a pane's composer does with dropped files
// (composer.Composer: AttachPaths, SharePaths).
type dropAttacher interface {
	AttachPaths(paths []string)
	SharePaths(paths []string)
}

// dropFiles is FileDragDrop.onDrop for c's pane.
func (a *App) dropFiles(c *convo, paths []string, shift bool) {
	if len(paths) == 0 || a.dropUploads() == dropUploadsNone || !a.dropCanPost(c) {
		return
	}
	at, ok := c.comp.(dropAttacher)
	if !ok {
		if b, isBox := c.comp.(composerBox); isBox {
			at, ok = b.Composer, true
		}
	}
	if !ok {
		return
	}
	if ed := dropEdit(c); ed != nil {
		// a message being edited in this pane takes the files, Shift or
		// not (messageEditUploadNewFiles, action 124)
		ed.AttachPaths(paths)
		return
	}
	if shift {
		at.SharePaths(paths) // uploadFiles with shareData
		return
	}
	at.AttachPaths(paths) // uploadDraftFiles
}

// dropEdit is the pane's inline edit when a drop goes to it
// (FileDragDrop's isMessageEdited && !fileAttachAfterSendDisabled), else
// nil.
func dropEdit(c *convo) *composer.Composer {
	l, ok := c.list.(interface{ EditComposer() *composer.Composer })
	if !ok {
		return nil
	}
	if ed := l.EditComposer(); ed != nil && ed.TakesEditFiles() {
		return ed
	}
	return nil
}

// dropCanPost is userCanPostInChannel (the message pane) or
// userCanPostInThread (a thread).
func (a *App) dropCanPost(c *convo) bool {
	readOnly, threadOnly, noThreads := a.postingRestriction(c)
	if c.ts != "" {
		return !readOnly && !noThreads
	}
	return !readOnly && !threadOnly
}

type dropUploadsKind int

const (
	dropUploadsAll        dropUploadsKind = iota
	dropUploadsImagesOnly                 // disable_all_except_images
	dropUploadsNone                       // disable_all
)

// dropUploads is the disable_file_uploads team pref (qwL3; allow_box_cfs
// allows everything).
func (a *App) dropUploads() dropUploadsKind {
	if a.data == nil {
		return dropUploadsAll
	}
	core := a.data.core
	if v, ok := core.TeamPref("allow_box_cfs"); ok && string(v) == "true" {
		return dropUploadsAll
	}
	switch core.TeamPrefString("disable_file_uploads") {
	case "disable_all":
		return dropUploadsNone
	case "disable_all_except_images":
		return dropUploadsImagesOnly
	}
	return dropUploadsAll
}

// ---- the overlay ----

// dropOverlayKind picks the overlay's state, as the two overlays'
// render methods do.
type dropOverlayKind int

const (
	dropOvUpload   dropOverlayKind = iota // the cards
	dropOvReadOnly                        // the circle and emoji-objects
	dropOvBlocked                         // the blocked-files picture
)

// dropOverlayText is the overlay's state, title and subtitle (the
// subtitle's "\x00" is where the Shift key chip goes).
func (a *App) dropOverlayText(c *convo) (dropOverlayKind, string, string) {
	uploads := a.dropUploads()
	canPost := a.dropCanPost(c)
	if c.ts != "" {
		// DragOverlay (6Uf7): announce-only, then blocked, then upload
		tr := i18n.NS("threads")
		switch {
		case !canPost:
			return dropOvReadOnly, tr.T("Read only"), tr.T("Only selected team members can post in this thread.")
		case uploads == dropUploadsNone:
			return dropOvBlocked, tr.T("Uploads disabled"), tr.T("At the request of your administrator, file uploads have been disabled on this workspace.")
		}
		return dropOvUpload, tr.T("Upload to thread"), tr.T("Hold {shiftKey} to share immediately", "shiftKey", "\x00")
	}
	// MessagePaneDragOverlay (cCrv) getTitleText / getSubtitleText
	tr := i18n.NS("files")
	if !canPost {
		return dropOvReadOnly, tr.T("Read only"), tr.T("Only selected team members can post in this channel.")
	}
	switch uploads {
	case dropUploadsNone:
		return dropOvBlocked, tr.T("Uploads disabled"), tr.T("At the request of your administrator, file uploads have been disabled on this workspace.")
	case dropUploadsImagesOnly:
		return dropOvUpload, tr.T("Uploads disabled"), tr.T("At the request of your administrator, only images can be uploaded to this workspace.")
	}
	title := tr.T("Upload to {teamName}", "teamName", a.teamName())
	name, dm := a.dropConvName(c.ch)
	if dm {
		return dropOvUpload, title, tr.T("Hold {shiftKey} to share immediately with {userName}", "shiftKey", "\x00", "userName", name)
	}
	return dropOvUpload, title, tr.T("Hold {shiftKey} to share immediately to {channelName}", "shiftKey", "\x00", "channelName", name)
}

// dropConvName is getDisplayNameForChannel(…, {imIncludeAtSign: false}):
// "#name" for a public channel, the bare name for a private one, the
// person (or people) for a DM; dm reports a DM or group DM.
func (a *App) dropConvName(ch string) (name string, dm bool) {
	ci := a.convOf(ch)
	if ci == nil {
		return "", false
	}
	switch ci.Kind {
	case "channel":
		return "#" + ci.Name, false
	case "im", "mpim", "app":
		return ci.Name, true
	}
	return ci.Name, false
}

// dropNight is the overlays' night illustration: from 20:00 to 08:00.
func dropNight(now time.Time) bool {
	h := now.Local().Hour()
	return h >= 20 || h < 8
}

// dropNightAt is dropNight (screenshot tests pin it).
var dropNightAt = dropNight

// dropOverlayAnim is how long the overlay's entrance runs (.12s).
const dropOverlayAnim = 120 * time.Millisecond

// layoutDropOverlay draws c's overlay over its pane (size sz) when the
// drag hovers it. It is the pane's last layer.
func (a *App) layoutDropOverlay(gtx layout.Context, c *convo, sz image.Point) {
	if !a.drop.on || a.drop.hover != c {
		return
	}
	if a.drop.since.IsZero() {
		a.drop.since = gtx.Now
	}
	p := float64(1)
	if !a.reduceMotion() {
		p = math.Min(1, float64(gtx.Now.Sub(a.drop.since))/float64(dropOverlayAnim))
		if p < 1 {
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	th := a.th
	thread := c.ts != ""
	kind, title, sub := a.dropOverlayText(c)

	// the whole overlay: inset -1px 0 0 over FileDragDrop's container
	// (a thread's starts under the pane's header)
	r := image.Rect(0, dropTop(c)-gtx.Dp(1), sz.X, sz.Y)
	bg := dropSK(th, "sk_primary_background")
	bgA := 0.95
	op1 := 1.0
	if !thread { // messagePaneDragOverlayOverlayTransition (ease): bg 0 → .95, opacity 0 → 1
		e := cssEase(p)
		bgA, op1 = 0.95*e, e
	}
	// threadDragOverlayFadeIn isn't defined in Slack's CSS: the thread
	// overlay appears at once
	ostack := paint.PushOpacity(gtx.Ops, float32(op1))
	theme.Fill(gtx, r, theme.WithAlpha(bg, uint8(bgA*255+.5)))

	// .p-*_drag_overlay__info: a centred column
	padX := gtx.Dp(16)
	if thread {
		padX = gtx.Dp(50)
	}
	animW, animH := gtx.Dp(171), gtx.Dp(171)
	if thread && kind == dropOvUpload {
		animW, animH = gtx.Dp(110), gtx.Dp(155)
	}
	titleTop := gtx.Dp(16) // margin-top
	if thread {
		titleTop = gtx.Dp(8)
	}
	titleLH := gtx.Dp(34) // 28px × 1.2143
	subLH := gtx.Dp(22)   // 15px × 1.46668
	availW := max(1, sz.X-2*padX)
	subLines := a.dropSubtitleLines(gtx, sub, availW)
	h := animH + titleTop + titleLH + gtx.Dp(8) + len(subLines)*subLH
	top := r.Min.Y + (r.Dy()-h)/2

	// the animation box
	func() {
		defer op.Offset(image.Pt((sz.X-animW)/2, top)).Push(gtx.Ops).Pop()
		a.dropAnim(gtx, kind, thread, p, image.Pt(animW, animH))
	}()
	y := top + animH + titleTop
	fg := th.C.Fg
	tw := th.Measure(gtx, 28, theme.Black, title)
	func() {
		defer op.Offset(image.Pt((sz.X-min(tw, availW))/2, y)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(availW, titleLH)}
		atLine(g, titleLH, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 28, theme.Black, fg, title)
		})
	}()
	y += titleLH + gtx.Dp(8)
	subCol := dropTok(th, "dt_color-content-sec")
	if thread {
		subCol = dropSK(th, "sk_foreground_max_solid")
	}
	for _, ln := range subLines {
		a.dropSubtitleLine(gtx, ln, image.Pt((sz.X-ln.w)/2, y), subLH, subCol)
		y += subLH
	}
	ostack.Pop()

}

// atLine centres a single text line vertically in a line box of lh.
func atLine(gtx layout.Context, lh int, w layout.Widget) {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (lh-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// dropLine is one wrapped line of the subtitle: its pieces ("\x00" is the
// key chip) and width.
type dropLine struct {
	parts []string
	w     int
}

// dropSubtitleLines wraps the subtitle at word boundaries to w
// (overflow-wrap: break-word, centred), the Shift chip one unbreakable
// piece.
func (a *App) dropSubtitleLines(gtx layout.Context, s string, w int) []dropLine {
	if s == "" {
		return nil
	}
	var words []string
	for i, seg := range strings.Split(s, "\x00") {
		if i > 0 {
			words = append(words, "\x00")
		}
		for j, f := range strings.Split(seg, " ") {
			if j > 0 {
				words = append(words, " ")
			}
			if f != "" {
				words = append(words, f)
			}
		}
	}
	var lines []dropLine
	var cur dropLine
	for _, wd := range words {
		ww := a.dropPartW(gtx, wd)
		if wd != " " && cur.w > 0 && cur.w+ww > w {
			lines = append(lines, cur.trim(a, gtx))
			cur = dropLine{}
		}
		if wd == " " && cur.w == 0 {
			continue
		}
		cur.parts = append(cur.parts, wd)
		cur.w += ww
	}
	if len(cur.parts) > 0 {
		lines = append(lines, cur.trim(a, gtx))
	}
	return lines
}

// trim drops a line's trailing space.
func (l dropLine) trim(a *App, gtx layout.Context) dropLine {
	for n := len(l.parts); n > 0 && l.parts[n-1] == " "; n = len(l.parts) {
		l.w -= a.dropPartW(gtx, " ")
		l.parts = l.parts[:n-1]
	}
	return l
}

// dropPartW is a subtitle piece's width: the chip with its 4px side
// margins, or the text.
func (a *App) dropPartW(gtx layout.Context, s string) int {
	if s == "\x00" {
		return a.dropKeyW(gtx) + 2*gtx.Dp(4)
	}
	return a.th.Measure(gtx, 15, theme.Regular, s)
}

// dropKeyW is the slim KeyboardKey "Shift": 4px inside, at least 20 wide.
func (a *App) dropKeyW(gtx layout.Context) int {
	return max(gtx.Dp(20), a.th.Measure(gtx, 13, theme.Regular, dropShiftLabel())+2*gtx.Dp(4))
}

// dropShiftLabel is the key's label (KeyboardKey label "Shift", not
// translated by Slack).
func dropShiftLabel() string { return "Shift" }

func (a *App) dropSubtitleLine(gtx layout.Context, ln dropLine, at image.Point, lh int, col color.NRGBA) {
	th := a.th
	x := at.X
	for _, p := range ln.parts {
		if p == "\x00" {
			x += gtx.Dp(4)
			kw := a.dropKeyW(gtx)
			// height 5 (20px), margin-block-end 4px, vertical-align middle
			kh := gtx.Dp(20)
			ky := at.Y + (lh-kh-gtx.Dp(4))/2
			kr := image.Rect(x, ky, x+kw, ky+kh)
			a.dropKeyChip(gtx, kr)
			x += kw + gtx.Dp(4)
			continue
		}
		pw := th.Measure(gtx, 15, theme.Regular, p)
		func() {
			defer op.Offset(image.Pt(x, at.Y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(pw+gtx.Dp(2), lh)}
			atLine(g, lh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Regular, col, p)
			})
		}()
		x += pw
	}
}

// dropKeyChip is KeyboardKey slim (HgMH): base.tertiary fill, radius
// base (4px), a 1px tertiary outline along the bottom, the primary text
// at the caption size.
func (a *App) dropKeyChip(gtx layout.Context, r image.Rectangle) {
	th := a.th
	rad := gtx.Dp(4)
	theme.RRect(gtx, r, rad, dropTok(th, "dt_color-otl-ter"))
	inner := r
	inner.Max.Y -= max(1, gtx.Dp(1))
	theme.RRect(gtx, inner, rad, dropTok(th, "dt_color-base-ter"))
	lbl := dropShiftLabel()
	lw := th.Measure(gtx, 13, theme.Regular, lbl)
	defer op.Offset(image.Pt(r.Min.X+(r.Dx()-lw)/2, r.Min.Y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(lw+gtx.Dp(2), inner.Dy())}
	atLine(g, inner.Dy(), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 13, theme.Regular, dropTok(th, "dt_color-content-pry"), lbl)
	})
}

// dropAnim draws .p-*_drag_overlay__anim at progress p (0..1, linear
// time; each part eases in-out over the same .12s).
func (a *App) dropAnim(gtx layout.Context, kind dropOverlayKind, thread bool, p float64, box image.Point) {
	e := cubicBezierY(0.42, 0, 0.58, 1, p) // ease-in-out
	night := dropNightAt(gtx.Now)
	sp := dropSprites()
	dp := func(v float64) float32 { return float32(v) * gtx.Metric.PxPerDp }

	// __anim_circle: #00000003, scale .5 → 1, opacity 0 → 1
	if kind != dropOvUpload || !thread {
		s := 0.5 + 0.5*e
		func() {
			defer paint.PushOpacity(gtx.Ops, float32(e)).Pop()
			d := int(float64(box.X) * s)
			theme.Circle(gtx, image.Pt(box.X/2, box.Y/2), d, color.NRGBA{A: 3})
		}()
	}
	switch kind {
	case dropOvReadOnly:
		// emoji-objects at 50px in content-sec (thread: foreground_max_solid)
		col := dropTok(a.th, "dt_color-content-sec")
		if thread {
			col = dropSK(a.th, "sk_foreground_max_solid")
		}
		is := gtx.Dp(50)
		defer op.Offset(image.Pt((box.X-is)/2, (box.Y-is)/2)).Push(gtx.Ops).Pop()
		icons.Get("emoji-objects")(gtx, unit.Dp(50), col)
		return
	case dropOvBlocked:
		// __block_files_esc: the 1x picture at its size, top 48 left 18
		if sp.ok {
			dropSprite(gtx, sp.blocked, f32.Pt(dp(18), dp(48)), f32.Point{}, f32.Pt(dp(135), dp(89)), 0, 1, 1)
		}
		return
	}
	if !sp.ok {
		return
	}
	if thread {
		// text_01 (top 3), card_01 (top 30 left 10), text_02 (top 112):
		// threadTextAnim scale .85 → 1 and 10px down, threadCard1Anim
		// scale .5 → 1 and 10px down, both fading in
		card := sp.tCardDay
		if night {
			card = sp.tCardNight
		}
		ts := 0.85 + 0.15*e
		down := f32.Pt(0, dp(10*e))
		dropSprite(gtx, sp.tText, f32.Pt(0, dp(3)), down, f32.Pt(dp(110), dp(23)), 0, float32(ts), float32(e))
		dropSprite(gtx, card, f32.Pt(dp(10), dp(30)), down, f32.Pt(dp(110), dp(77)), 0, float32(0.5+0.5*e), float32(e))
		dropSprite(gtx, sp.tText, f32.Pt(0, dp(112)), down, f32.Pt(dp(110), dp(23)), 0, float32(ts), float32(e))
		return
	}
	// card_03 (top 50 left 35, 92×77): scale .5 rotate 5° → translateY(-30)
	// card_02 (top 55 left 25): scale .5 rotate 15° → translate(-20px)
	// card_01 (top 62 left 35, 100×89): scale .5 rotate -10° → translate(30px)
	s := float32(0.5 + 0.5*e)
	dropSprite(gtx, sp.card3, f32.Pt(dp(35), dp(50)), f32.Pt(0, dp(-30*e)), f32.Pt(dp(92), dp(77)), float32(5*(1-e)), s, 1)
	dropSprite(gtx, sp.card2, f32.Pt(dp(25), dp(55)), f32.Pt(dp(-20*e), 0), f32.Pt(dp(92), dp(77)), float32(15*(1-e)), s, 1)
	card1 := sp.card1Day
	if night {
		card1 = sp.card1Night
	}
	dropSprite(gtx, card1, f32.Pt(dp(35), dp(62)), f32.Pt(dp(30*e), 0), f32.Pt(dp(100), dp(89)), float32(-10*(1-e)), s, 1)
}

// dropSprite draws one sprite piece in its CSS box (pos, size sz, both
// px) with CSS's transform: scale(s) rotate(deg) translate(t), about the
// box's centre (transform-origin 50% 50%), at opacity.
func dropSprite(gtx layout.Context, img paint.ImageOp, pos, t, sz f32.Point, deg, s, opacity float32) {
	is := img.Size()
	if is.X == 0 || opacity <= 0 {
		return
	}
	c := pos.Add(sz.Mul(.5))
	aff := f32.Affine2D{}.
		Scale(f32.Point{}, f32.Pt(sz.X/float32(is.X), sz.Y/float32(is.Y))).
		Offset(pos).
		Offset(t). // translate first (rightmost), in the box's own frame
		Rotate(c, deg*math.Pi/180).
		Scale(c, f32.Pt(s, s))
	defer paint.PushOpacity(gtx.Ops, opacity).Pop()
	defer op.Affine(aff).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// ---- the sprites ----

//go:embed assets/dragdrop/dragdrop_sprites@2x-ca59764.png
var dropSpritesPNG []byte

//go:embed assets/dragdrop/dragdrop_threads_sprites@2x-f3aaf0a.png
var dropThreadSpritesPNG []byte

//go:embed assets/dragdrop/emptystate-blockedfiles@1x-6b3ea84.png
var dropBlockedPNG []byte

// dropSpriteSet is Slack's drag-and-drop sprites, cut into the pieces the
// CSS shows (background-position in the 100×358 / 110×203 background
// sizes, so twice that in the @2x images' pixels).
type dropSpriteSet struct {
	ok                                   bool
	card1Day, card1Night, card2, card3   paint.ImageOp
	tText, tCardDay, tCardNight, blocked paint.ImageOp
}

var (
	dropSpritesOnce sync.Once
	dropSpritesSet  dropSpriteSet
)

func dropSprites() *dropSpriteSet {
	dropSpritesOnce.Do(func() {
		pane, err1 := png.Decode(bytes.NewReader(dropSpritesPNG))
		thr, err2 := png.Decode(bytes.NewReader(dropThreadSpritesPNG))
		blk, err3 := png.Decode(bytes.NewReader(dropBlockedPNG))
		if err1 != nil || err2 != nil || err3 != nil {
			return
		}
		cut := func(img image.Image, x, y, w, h int) paint.ImageOp {
			r := image.Rect(x, y, x+w, y+h).Intersect(img.Bounds())
			dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
			return paint.NewImageOp(dst)
		}
		s := &dropSpritesSet
		s.card1Day = cut(pane, 0, 0, 200, 178)     // 0 0, 100×89
		s.card1Night = cut(pane, 0, 192, 200, 178) // 0 -96 (night)
		s.card3 = cut(pane, 0, 378, 184, 154)      // 0 -189, 92×77
		s.card2 = cut(pane, 0, 546, 184, 154)      // 0 -273, 92×77
		s.tText = cut(thr, 0, 0, 220, 46)          // 0 0, 110×23
		s.tCardDay = cut(thr, 0, 100, 220, 154)    // 0 -50, 110×77
		s.tCardNight = cut(thr, 0, 250, 220, 154)  // 0 -125 (night)
		s.blocked = cut(blk, 0, 0, 135, 89)        // no background-size: 1x
		s.ok = true
	})
	return &dropSpritesSet
}

// ---- colours ----

// dropTok is var(--name) in the theme's mode (an account theme's own
// value when it sets one).
func dropTok(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(th.Dark)["--"+name])
	theme.SetColor(th.Tokens, &c, "var(--"+name+")")
	return c
}

// dropSK is rgb(var(--name)) for one of Slack's "r, g, b" sk_ variables.
func dropSK(th *theme.Theme, name string) color.NRGBA {
	c, ok := css.ParseColor("rgb(" + css.SlackVars(th.Dark)["--"+name] + ")")
	if !ok {
		c = th.C.Bg
	}
	theme.SetColor(th.Tokens, &c, "rgb(var(--"+name+"))")
	c.A = 0xff
	return c
}

// cssEase is CSS's ease timing function, cubic-bezier(.25, .1, .25, 1).
func cssEase(x float64) float64 { return cubicBezierY(0.25, 0.1, 0.25, 1, x) }

// cubicBezierY is the CSS cubic-bezier(x1, y1, x2, y2) timing function at
// x (0..1).
func cubicBezierY(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 {
		return 3*a*t*(1-t)*(1-t) + 3*b*t*t*(1-t) + t*t*t
	}
	lo, hi := 0.0, 1.0
	t := x
	for i := 0; i < 40; i++ {
		t = (lo + hi) / 2
		if bez(x1, x2, t) < x {
			lo = t
		} else {
			hi = t
		}
	}
	return bez(y1, y2, t)
}
