package popups

import (
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// The media viewer's video items: Slack's MediaViewer (viWS, the
// media-viewer chunk of build 2c0d02ca130f) showing a video, with
// MediaViewerContentVideo (Ivom) and its player (internal/ui/videoplayer's
// Viewer), the MediaVideoCoordinatorContext's state (2CjY), the footer's
// MediaProgressBar (Jm6q) and MediaViewerControls (Upfn: TogglePlay VG1W,
// ToggleMute lGYe, the PlaybackTimestamp v8k6), the keys (DBSG and the
// viewer's use_key_commands) and useMouseIsOverElement (LqYI).
//
//   - The item's background is the blurred poster (MediaViewerBlurryBackground:
//     thumb_video_ts || thumb_video at blur(40px) brightness(.4), covering).
//   - The video fills the panel (.p-media_viewer__video: width and height
//     100%, contained); the poster shows in its place until the first
//     frame. A click on it plays or pauses (BaseMediaVideo's container).
//   - The header and the footer slide over the video (--slide_over_content)
//     and hide while it plays and the pointer hasn't moved for 1300 ms
//     (--controls_hidden: opacity 0 after .16s, the header up 20px, the
//     footer down 20px, the arrows faded), unless the pointer is on them
//     (data-prevent-fade) or a menu is open; the cursor hides with them.
//   - The footer: the progress bar on its top edge (8px, 16px in), then
//     Play/Pause ("Space"), Mute/Unmute ("M") with its volume slider
//     opening beside it, and "m:ss / m:ss"; on the right Show thread and
//     More actions (Download is in it for a video; Open in new window
//     is hidden with clips_pip on, as it is for Karim).
//   - Space plays or pauses, m mutes; at the end the viewer moves to the
//     next item. A new item starts from where the viewer left it, playing
//     (tC: autoPlay on, the time from the file's progress).
//
//   - Dock to corner (MediaViewerPipButton, left of Show thread) docks the
//     video into the mini player from its time and closes the viewer
//     (VideoDocker; videoplayer/pip.go).
//
// Not yet: playback speed (j/l, the speed button) and captions, and
// one-click reactions on the video.

// lbHideAfter is how long the viewer's controls stay after the pointer
// last moved while the video plays (LqYI).
const lbHideAfter = 1300 * time.Millisecond

// lbVideo is the video item's state.
type lbVideo struct {
	vc  *videoplayer.Controller
	p   *videoplayer.Viewer
	idx int // the item p plays
	// start and auto are the first item's (Expand's time, and whether
	// the inline video played); first until that item opened
	start time.Duration
	auto  bool
	first bool
	ended bool // the item's end was seen (the viewer moved on)
	// useMouseIsOverElement: over since the pointer entered or moved,
	// until lbHideAfter after its last move; hold while it's on the
	// header or the footer (data-prevent-fade)
	over, hold bool
	moveAt     time.Time
	ptr        f32.Point
	// shown is whether the controls show, shownAt when that last changed
	shown   bool
	shownAt time.Time
	// the video's container (a click plays or pauses), the pass-through
	// pointer watch, the progress bar and the volume slider
	areaTag, moveTag, barTag int
	volTag, volHoverTag      int
	pressed                  bool
	scrub, resume            bool
	barR, volR               image.Rectangle // their tracks as drawn
	playR, muteR             image.Rectangle
	barHot                   bool
	barLeft                  time.Time // the pointer left the bar (its thumb shrinks .5s later)
	vol, volOpen             bool
	volLeft                  time.Time
	playBtn, muteBtn         widget.Clickable
	tips                     [2]TipState
	dockBtn                  widget.Clickable // Dock to corner (MediaViewerPipButton)
	dockTip                  TipState
	dockR                    image.Rectangle
	wakeAt                   time.Time // the layout asked for (later)
}

// later asks for a layout after d through the host (time.AfterFunc: no
// op.InvalidateCmd mid-layout, which loses the window's other input),
// once for each due time not already covered by an earlier one.
func (lb *Lightbox) later(d time.Duration) {
	now := time.Now()
	due := now.Add(d)
	if w := lb.vid.wakeAt; w.After(now) && !w.After(due) {
		return
	}
	lb.vid.wakeAt = due
	time.AfterFunc(d, lb.h.Invalidate)
}

// lbVideos is the window's players (the App's videoplayer.Controller),
// nil where videos don't play.
func lbVideos(h Host) *videoplayer.Controller {
	if v, ok := h.(interface {
		Videos() *videoplayer.Controller
	}); ok {
		if c := v.Videos(); c.Available() {
			return c
		}
	}
	return nil
}

// lbVideoItem is a playable video file's item: its poster for the still
// and the background, the video's own URL when it has none.
func lbVideoItem(f *richtext.File) lbItem {
	var im richtext.Image
	if f.Thumb != nil {
		im = *f.Thumb
	}
	if im.URL == "" {
		im.URL = f.URL
	}
	if im.Title == "" {
		im.Title = f.Title
	}
	if im.Title == "" {
		im.Title = f.Name
	}
	return lbItem{img: im, file: f, video: true}
}

// NewVideoViewer opens the media viewer at f, one of m's files: a video
// clicked in a gallery (fIJO's tE: timeElapsed 0, autoPlay), or Expand
// (tR: the inline video's time, and whether it was playing). nil where
// videos don't play in place.
func NewVideoViewer(h Host, m core.Message, f *richtext.File, at time.Duration, autoplay bool) *Lightbox {
	if lbVideos(h) == nil || f == nil || !f.IsVideo {
		return nil
	}
	lb := &Lightbox{h: h, m: m, zoom: 1}
	lbCollect(m.Doc.Blocks, &lb.items)
	lb.idx = -1
	for i, it := range lb.items {
		if it.file != nil && it.file.ID == f.ID {
			lb.idx = i
			break
		}
	}
	if lb.idx < 0 {
		lb.items = append([]lbItem{lbVideoItem(f)}, lb.items...)
		lb.idx = 0
	}
	lb.vid.start, lb.vid.auto, lb.vid.first = at, autoplay, true
	return lb
}

// Dismiss closes the viewer and its video's player.
func (lb *Lightbox) Dismiss() {
	lb.closeVideo()
	lb.base.Dismiss()
}

func (lb *Lightbox) closeVideo() {
	if lb.vid.p != nil {
		lb.vid.p.Close()
		lb.vid.p = nil
	}
}

// VideoState is the viewer's video item: its player's state, the file,
// whether the controls show; ok false when the item shown isn't a
// playing video item (tests and the inspect tool).
func (lb *Lightbox) VideoState() (st videoplayer.ViewerState, f *richtext.File, shown, ok bool) {
	if lb == nil || lb.idx < 0 || lb.idx >= len(lb.items) || !lb.cur().video || lb.vid.p == nil {
		return st, nil, false, false
	}
	return lb.vid.p.State(), lb.cur().file, lb.vid.shown, true
}

// Position is the item shown and how many the viewer has.
func (lb *Lightbox) Position() (idx, n int) { return lb.idx, len(lb.items) }

// VideoParts are the video footer's controls as last drawn (window
// coordinates): Play/Pause, Mute, the progress bar's track.
func (lb *Lightbox) VideoParts() (play, mute, bar image.Rectangle) {
	return lb.vid.playR, lb.vid.muteR, lb.vid.barR
}

// syncVideo keeps the player of the item shown: a new one for a video
// item (the first with the caller's time and autoplay, the next ones
// from the file's progress, playing: tC), none for an image.
func (lb *Lightbox) syncVideo(now time.Time) {
	v := &lb.vid
	if v.p != nil && v.idx == lb.idx {
		return
	}
	lb.closeVideo()
	it := lb.cur()
	if !it.video {
		return
	}
	if v.vc == nil {
		v.vc = lbVideos(lb.h)
	}
	if v.vc == nil {
		return
	}
	start, auto := time.Duration(0), true
	if v.first {
		start, auto = v.start, v.auto
	}
	v.first = false
	v.p = v.vc.OpenViewer(it.file, start, auto)
	v.idx, v.ended = lb.idx, false
	// the viewer opens under the pointer: Chromium's fake mouse move
	// enters it, so the controls show for a while
	v.over, v.moveAt = true, now
}

// videoShown is whether the controls show now: not --controls_hidden
// (the pointer over the viewer, the video paused, or a menu open).
func (lb *Lightbox) videoShown(now time.Time, st videoplayer.ViewerState) bool {
	v := &lb.vid
	if v.over && !v.hold && now.Sub(v.moveAt) >= lbHideAfter {
		v.over = false
	}
	paused := st.Mounted && !st.Playing
	menu := !isTop(lb) // a menu (More actions) over the viewer
	return v.over || v.hold || paused || menu || v.scrub || v.vol || !st.Mounted
}

// videoFade is the chrome's opacity for shown (80ms, after .16s when
// hiding), asking for frames while it moves.
func (lb *Lightbox) videoFade(gtx layout.Context, now time.Time, shown bool) float32 {
	v := &lb.vid
	if shown != v.shown {
		v.shown, v.shownAt = shown, now
	}
	t := float32(now.Sub(v.shownAt)) / float32(80*time.Millisecond)
	if !shown {
		t = float32(now.Sub(v.shownAt)-160*time.Millisecond) / float32(80*time.Millisecond)
	}
	t = min(max(t, 0), 1)
	if t < 1 {
		lb.later(16 * time.Millisecond)
	}
	if shown {
		return t
	}
	return 1 - t
}

// videoInput takes the video item's pointer events (last frame's areas)
// and its keys, before the item is drawn.
func (lb *Lightbox) videoInput(gtx layout.Context, now time.Time) {
	v := &lb.vid
	p := v.p
	if p == nil {
		return
	}
	for { // the pass-through watch: the pointer anywhere on the panel
		e, ok := gtx.Event(pointer.Filter{Target: &v.moveTag, Kinds: pointer.Enter | pointer.Move | pointer.Drag | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move, pointer.Drag:
			v.over, v.moveAt, v.ptr = true, now, pe.Position
		case pointer.Leave, pointer.Cancel:
			v.over = false
		}
	}
	for { // the video's container: a click plays or pauses
		e, ok := gtx.Event(pointer.Filter{Target: &v.areaTag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			v.pressed = pe.Buttons == pointer.ButtonPrimary || pe.Buttons == 0
		case pointer.Release:
			if v.pressed {
				p.TogglePlay()
			}
			v.pressed = false
		case pointer.Cancel:
			v.pressed = false
		}
	}
	for { // MediaProgressBar: a press pauses and seeks, a drag seeks, the release plays on
		e, ok := gtx.Event(pointer.Filter{Target: &v.barTag, Kinds: pointer.Enter | pointer.Leave | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		st := p.State()
		switch pe.Kind {
		case pointer.Enter:
			v.barHot = true
		case pointer.Leave:
			v.barHot, v.barLeft = false, now
		case pointer.Press:
			v.scrub, v.resume = true, st.Playing
			p.Pause()
			p.Seek(barTime(v.barR, pe.Position.X, st.Duration))
		case pointer.Drag:
			if v.scrub {
				p.Seek(barTime(v.barR, pe.Position.X, st.Duration))
			}
		case pointer.Release, pointer.Cancel:
			if v.scrub && v.resume {
				p.Play()
			}
			v.scrub, v.resume = false, false
		}
	}
	for { // the volume control hovered (the button and its slider): the slider opens
		e, ok := gtx.Event(pointer.Filter{Target: &v.volHoverTag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter:
			v.volOpen = true
		case pointer.Leave, pointer.Cancel:
			if v.volOpen {
				v.volLeft = now
			}
			v.volOpen = false
		}
	}
	for { // the volume slider (MediaVolumeSlider, normal mode)
		e, ok := gtx.Event(pointer.Filter{Target: &v.volTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			v.vol = true
			p.SetVolume(volAt(v.volR, pe.Position.X), true)
		case pointer.Drag:
			if v.vol {
				p.SetVolume(volAt(v.volR, pe.Position.X), true)
			}
		case pointer.Release, pointer.Cancel:
			if v.vol {
				st := p.State()
				p.SetVolume(st.Volume, false) // its mouseup writes media_volume
			}
			v.vol = false
		}
	}
	if v.playBtn.Clicked(gtx) {
		p.TogglePlay()
	}
	if v.muteBtn.Clicked(gtx) {
		p.ToggleMute()
	}
}

// videoKeys are DBSG's: Space plays or pauses, m mutes.
func (lb *Lightbox) videoKeys(gtx layout.Context) {
	p := lb.vid.p
	for {
		e, ok := gtx.Event(key.Filter{Focus: lb, Name: key.NameSpace}, key.Filter{Focus: lb, Name: "M"})
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press || p == nil {
			continue
		}
		switch ke.Name {
		case key.NameSpace:
			p.TogglePlay()
		case "M":
			p.ToggleMute()
		}
	}
}

// barTime is the progress bar's value at x (X7vT: the place along the
// track, of the duration).
func barTime(r image.Rectangle, x float32, d time.Duration) time.Duration {
	if r.Dx() <= 0 || d <= 0 {
		return 0
	}
	f := min(max((x-float32(r.Min.X))/float32(r.Dx()), 0), 1)
	return time.Duration(float64(f) * float64(d))
}

// volAt is the volume slider's value at x, in hundredths
// (MediaVolumeSlider: Math.round(e)/100).
func volAt(r image.Rectangle, x float32) float64 {
	if r.Dx() <= 0 {
		return 0
	}
	f := min(max((x-float32(r.Min.X))/float32(r.Dx()), 0), 1)
	return float64(int(f*100+.5)) / 100
}

// videoItem draws the video item under the chrome: the blurred poster,
// the poster then the video's frames contained in the panel, and the
// container's input area. It ends the item when it played to its end:
// the viewer moves to the next one.
func (lb *Lightbox) videoItem(gtx layout.Context, th *theme.Theme, panel image.Rectangle, now time.Time) {
	v := &lb.vid
	lb.syncVideo(now)
	it := lb.cur()
	lbBlurryBackground(gtx, lb.h, it, panel)
	st := v.p.State()
	if st.Ended && !v.ended { // tS: the progress at 0 (gnNF), then tI
		v.ended = true
		if lb.idx+1 < len(lb.items) {
			lb.go2(lb.idx + 1)
			lb.syncVideo(now)
			it, st = lb.cur(), v.p.State()
			if !it.video {
				return
			}
		}
	}
	nat := st.Size
	if (nat.X <= 0 || nat.Y <= 0) && it.file != nil {
		nat = image.Pt(it.file.OrigW, it.file.OrigH)
	}
	if !st.Playing || st.Elapsed == 0 { // the <video>'s poster until its first frame
		if img, ok := lb.h.Images().Get(it.img.URL, 0); ok && it.img.URL != it.file.URL {
			if nat.X <= 0 || nat.Y <= 0 {
				nat = img.Size()
			}
			if r := videoplayer.Contain(panel, nat); !r.Empty() {
				lbDrawStretched(gtx, img, r)
			}
		}
	}
	v.p.LayoutVideo(gtx, panel)
	cur := pointer.CursorDefault
	if !v.shown && st.Playing {
		cur = pointer.CursorNone
	}
	lbArea(gtx, panel, &v.areaTag, cur)
}

// lbDrawStretched draws img scaled into r.
func lbDrawStretched(gtx layout.Context, img paint.ImageOp, r image.Rectangle) {
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 || r.Empty() {
		return
	}
	sx, sy := float32(r.Dx())/float32(is.X), float32(r.Dy())/float32(is.Y)
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, sy)).Offset(layout.FPt(r.Min))).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// lbBlurryBackground is MediaViewerBlurryBackground: the item's poster
// covering the panel and 100px past it (inset: -100px), at blur(40px)
// brightness(.4) over #000. Clack draws a 24px copy of the poster
// stretched with linear filtering (a smooth field like the blur's), then
// .6 black for the brightness.
func lbBlurryBackground(gtx layout.Context, h Host, it lbItem, panel image.Rectangle) {
	fill(gtx, panel, rgb(0x000000))
	if it.img.URL == "" || it.file == nil || it.img.URL == it.file.URL {
		return
	}
	img, ok := h.Images().Get(it.img.URL, 24)
	if !ok {
		return
	}
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 {
		return
	}
	cov := panel.Inset(-dp(gtx, 100))
	s := max(float32(cov.Dx())/float32(is.X), float32(cov.Dy())/float32(is.Y))
	w, hh := int(float32(is.X)*s+.5), int(float32(is.Y)*s+.5)
	r := image.Rect(0, 0, w, hh).Add(image.Pt(cov.Min.X+(cov.Dx()-w)/2, cov.Min.Y+(cov.Dy()-hh)/2))
	cl := clip.Rect(panel).Push(gtx.Ops)
	lbDrawStretched(gtx, img, r)
	fill(gtx, panel, rgba(0x000000, .6))
	cl.Pop()
}

// videoChrome draws the header, the footer and the arrows over the
// video, faded and slid by the auto-hide.
func (lb *Lightbox) videoChrome(gtx layout.Context, th *theme.Theme, panel image.Rectangle, now time.Time) {
	v := &lb.vid
	st := v.p.State()
	shown := lb.videoShown(now, st)
	a := lb.videoFade(gtx, now, shown)
	v.p.SetShown(a > 0)
	if shown && v.over && st.Playing {
		// a layout when the controls are due to hide
		lb.later(time.Until(v.moveAt.Add(lbHideAfter)) + time.Millisecond)
	}
	headH := dp(gtx, 85)
	slide := int(float32(dp(gtx, 20)) * (1 - a))
	hr := image.Rect(panel.Min.X, panel.Min.Y, panel.Max.X, panel.Min.Y+headH)
	fr := image.Rect(panel.Min.X, panel.Max.Y-headH, panel.Max.X, panel.Max.Y)
	v.hold = v.over && (v.ptr.Y < float32(panel.Min.Y+dp(gtx, 64)) || v.ptr.Y >= float32(panel.Max.Y-dp(gtx, 60)))

	func() {
		o := paint.PushOpacity(gtx.Ops, a)
		defer o.Pop()
		defer op.Offset(image.Pt(0, -slide)).Push(gtx.Ops).Pop()
		pc := clipRRect(gtx, panel, dp(gtx, 8))
		lbGradient(gtx, hr, true)
		pc.Pop()
		lb.headBlock.Layout(gtx, hr)
		lb.header(gtx, th, panel)
	}()
	func() {
		o := paint.PushOpacity(gtx.Ops, a)
		defer o.Pop()
		defer op.Offset(image.Pt(0, slide)).Push(gtx.Ops).Pop()
		pc := clipRRect(gtx, panel, dp(gtx, 8))
		lbGradient(gtx, fr, false)
		pc.Pop()
		lb.footBlock.Layout(gtx, fr)
		lb.videoFooter(gtx, th, panel, st, now, slide)
	}()
	func() {
		o := paint.PushOpacity(gtx.Ops, a)
		defer o.Pop()
		lb.arrows(gtx, panel)
	}()
	// the watch for the auto-hide, passing every event on
	pass := pointer.PassOp{}.Push(gtx.Ops)
	cl := clip.Rect(panel).Push(gtx.Ops)
	event.Op(gtx.Ops, &v.moveTag)
	cl.Pop()
	pass.Pop()
}

// videoFooter draws the footer of a video item: the progress bar on its
// top edge, Play/Pause, Mute with its slider, the clock, then Show thread
// and More actions on the right. slide is the hide's offset (the parts'
// rects are where they show).
func (lb *Lightbox) videoFooter(gtx layout.Context, th *theme.Theme, panel image.Rectangle, st videoplayer.ViewerState, now time.Time, slide int) {
	v := &lb.vid
	white := rgb(0xffffff)
	hover := rgba(0xffffff, .1)
	footTop := panel.Max.Y - dp(gtx, 52)
	y := footTop + dp(gtx, 8)
	// MediaProgressBar, --dark: the gray-100 container (sk_foreground_low_solid
	// in a light client), the elapsed part and its 14px thumb lagoon-50
	bar := image.Rect(panel.Min.X+dp(gtx, 16), footTop-dp(gtx, 8), panel.Max.X-dp(gtx, 16), footTop)
	v.barR = bar.Add(image.Pt(0, slide))
	dark := th.Dark
	bg := rgb(0xdddddd)
	if dark {
		bg = theme.PltColor("gray", "100", true)
	}
	lagoon := theme.PltColor("lagoon", "50", dark)
	rad := dp(gtx, 12)
	rrect(gtx, bar, min(rad, bar.Dy()/2), bg)
	elapsed := st.Elapsed
	if f := barFrac(elapsed, st.Duration); f > 0 {
		w := int(float32(bar.Dx())*f + .5)
		rrect(gtx, image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+w, bar.Max.Y), min(dp(gtx, 4), bar.Dy()/2), lagoon)
	}
	hot := v.barHot || v.scrub
	if s := thumbScale(hot, now.Sub(v.barLeft)); s > 0 || hot {
		f := barFrac(elapsed, st.Duration)
		cx := bar.Min.X + int(float32(bar.Dx())*f+.5)
		d := int(float32(dp(gtx, 14)) * max(s, .5))
		cy := bar.Min.Y + dp(gtx, 3)
		if hot {
			rrect(gtx, image.Rect(cx-d/2, cy-d/2, cx-d/2+d, cy-d/2+d), d/2, lagoon)
		}
		if !hot && now.Sub(v.barLeft) < 580*time.Millisecond {
			lb.later(16 * time.Millisecond)
		}
	}
	func() { // its hit area: the bar and 4px above it (the 12px :before)
		hit := image.Rect(bar.Min.X, bar.Max.Y-dp(gtx, 12), bar.Max.X, bar.Max.Y)
		defer clip.Rect(hit).Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		event.Op(gtx.Ops, &v.barTag)
	}()

	// MediaViewerControls: 12px apart, from the footer's 12px padding
	x := panel.Min.X + dp(gtx, 12)
	playLabel, playIcon := nsViewer.T("Play"), "play"
	if st.Playing {
		playLabel, playIcon = nsViewer.T("Pause"), "pause"
	}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &v.playBtn, playIcon, 36, 20, white, hover, 8)
		Tooltip(gtx, th, &v.tips[0], v.playBtn.Hovered(), d.Size, Above, playLabel, nsViewer.T("Space"))
		return d
	})
	v.playR = image.Rect(x, y, x+dp(gtx, 36), y+dp(gtx, 36)).Add(image.Pt(0, slide))
	x += dp(gtx, 36) + dp(gtx, 12)
	muteLabel := nsViewer.T("Mute")
	vol := st.Volume
	if st.Muted {
		muteLabel, vol = nsViewer.T("Unmute"), 0
	}
	ic := "sound-off"
	switch {
	case st.Muted:
	case st.Volume >= .6:
		ic = "sound-up"
	case st.Volume >= .3:
		ic = "sound-medium"
	default:
		ic = "sound-down"
	}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &v.muteBtn, ic, 36, 20, white, hover, 8)
		Tooltip(gtx, th, &v.tips[1], v.muteBtn.Hovered(), d.Size, Above, muteLabel, "M")
		return d
	})
	v.muteR = image.Rect(x, y, x+dp(gtx, 36), y+dp(gtx, 36)).Add(image.Pt(0, slide))
	// the slider container opens to 70px while the control is hovered
	// (80ms), the slider padded 4px, a 4px #ffffff80 track, the
	// highlight_accent fill and its 8px thumb
	open := v.volOpen || v.vol
	sw := dp(gtx, 70)
	ow := volWidth(open, now, &v.volLeft, sw)
	if ow > 0 && ow < sw {
		lb.later(16 * time.Millisecond)
	}
	ctl := image.Rect(x, y, x+dp(gtx, 36)+ow, y+dp(gtx, 36))
	if ow > 0 {
		cl := clip.Rect(image.Rect(x+dp(gtx, 36), y, x+dp(gtx, 36)+ow, y+dp(gtx, 36))).Push(gtx.Ops)
		tr := image.Rect(x+dp(gtx, 36)+dp(gtx, 4), y+dp(gtx, 16), x+dp(gtx, 36)+sw-dp(gtx, 4), y+dp(gtx, 20))
		v.volR = tr.Add(image.Pt(0, slide))
		rrect(gtx, tr, dp(gtx, 2), rgba(0xffffff, .5))
		accent := rgb(0x1d9bd1)
		fw := int(float32(tr.Dx())*float32(vol) + .5)
		rrect(gtx, image.Rect(tr.Min.X, tr.Min.Y, tr.Min.X+fw, tr.Max.Y), dp(gtx, 2), accent)
		kd := dp(gtx, 8)
		if !(v.volOpen || v.vol) {
			kd = dp(gtx, 4)
		}
		kx, ky := tr.Min.X+fw, tr.Min.Y+tr.Dy()/2
		rrect(gtx, image.Rect(kx-kd/2, ky-kd/2, kx-kd/2+kd, ky-kd/2+kd), kd/2, accent)
		cl.Pop()
	}
	if ow > 0 { // the slider takes its presses; the button keeps its own
		func() {
			defer clip.Rect(image.Rect(x+dp(gtx, 36), y, ctl.Max.X, ctl.Max.Y)).Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			event.Op(gtx.Ops, &v.volTag)
		}()
	}
	func() { // the control's :hover, passing its events on
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect(ctl).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &v.volHoverTag)
	}()
	x = ctl.Max.X + dp(gtx, 12) + dp(gtx, 8)
	clk := videoplayer.ViewerClock(st.Elapsed) + " / " + videoplayer.ViewerClock(st.Duration)
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 36), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, white, clk)
		})
	})

	// MediaViewerActions for a video: Show thread, More actions (with
	// Download in it)
	btn := func(x int, c *widget.Clickable, ic string, tip *TipState, label string) image.Rectangle {
		p := image.Pt(x, y)
		d := at(gtx, p, func(gtx layout.Context) layout.Dimensions {
			d := iconButton(gtx, c, ic, 36, 20, white, hover, 8)
			Tooltip(gtx, th, tip, c.Hovered(), d.Size, Above, label, "")
			return d
		})
		return image.Rectangle{Min: p, Max: p.Add(d.Size)}
	}
	rx := panel.Max.X - dp(gtx, 12) - dp(gtx, 36)
	lb.moreRect = btn(rx, &lb.moreBtn, "more", &lb.tips[4], nsViewer.T("More actions"))
	if lb.m.Channel != "" && lb.m.TS != "" {
		rx -= dp(gtx, 48)
		btn(rx, &lb.threadBtn, "panel-right", &lb.tips[6], nsViewer.T("Show thread"))
	}
	// MediaViewerPipButton (before Show thread in FileViewerActions'
	// children): any playable video, where the host docks it
	if _, ok := lb.h.(VideoDocker); ok && lb.vid.p != nil {
		rx -= dp(gtx, 48)
		lb.vid.dockR = btn(rx, &lb.vid.dockBtn, "surface", &lb.vid.dockTip, nsViewer.T("Dock to corner"))
	} else {
		lb.vid.dockR = image.Rectangle{}
	}
}

// DockRect is the footer's Dock to corner as the last frame drew it (tests).
func (lb *Lightbox) DockRect() image.Rectangle { return lb.vid.dockR }

// VideoDocker is optional on the Host: the media viewer's Dock to corner
// (the viewer's popToSidebar, tG) docks the video item into the mini
// player from at, playing if playing; gallery is a gallery's video (it
// can't play inline). The viewer closes first.
type VideoDocker interface {
	DockVideo(f *richtext.File, m core.Message, at time.Duration, playing, gallery bool) bool
}

// dock is the Dock to corner button's click.
func (lb *Lightbox) dock() {
	d, ok := lb.h.(VideoDocker)
	it := lb.cur()
	if !ok || lb.vid.p == nil || !it.video || it.file == nil {
		return
	}
	st := lb.vid.p.State()
	f, m, gallery := it.file, lb.m, len(lb.items) > 1
	lb.Dismiss()
	d.DockVideo(f, m, st.Elapsed, st.Playing, gallery)
}

// barFrac is the elapsed share of the duration, 0..1.
func barFrac(e, d time.Duration) float32 {
	if d <= 0 {
		return 0
	}
	return min(max(float32(e)/float32(d), 0), 1)
}

// thumbScale is the progress bar's thumb (:after): scale 1 while hovered,
// .5 again 80ms after .5s off it.
func thumbScale(hot bool, since time.Duration) float32 {
	if hot {
		return 1
	}
	t := float32(since-500*time.Millisecond) / float32(80*time.Millisecond)
	return 1 - min(max(t, 0), 1)
}

// volWidth is the volume slider container's width: 0 to full over 80ms
// while the control is hovered, back when it's left.
func volWidth(open bool, now time.Time, left *time.Time, full int) int {
	if open {
		return full
	}
	if left.IsZero() {
		return 0
	}
	t := float32(now.Sub(*left)) / float32(80*time.Millisecond)
	return int(float32(full) * (1 - min(max(t, 0), 1)))
}
