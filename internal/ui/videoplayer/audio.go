package videoplayer

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Audio files play in place as Slack's FileAudio (boot-shared pBrm) →
// AudioPlayer (o/6F), build 35b42de8b870:
//
//   - which files: fileIsPlayableAudio (slack.FileIsPlayableAudio): m4a,
//     mp3 and wav uploads and audio messages; the source is getMediaURL's
//     aac || url_private (richtext.File.URL), and a build that can't
//     decode the aac rendition (no AAC on Linux) plays url_private;
//   - the style (MessageFilePreview, 2bi5): an audio message (lqEY) or a
//     name pronunciation is "default", the flat row (the play button, the
//     scrubber drawing the clip's waveform, the time, the transcript
//     button, MediaPlaybackSpeedControl, AudioClipOverflowMenu); any
//     other audio file is "card" (the header with the name and
//     "duration (size)", then the scrubber's track and thumb and the time;
//     the speed and transcript controls only with transcribe_opt_in_ui_v2);
//   - the time (eW): the duration until it plays, is scrubbed or has a
//     position, the position after; secondsToAudioTime (7foM);
//   - the play button (eU) toggles; the scrubber (Scrubber's mousedown,
//     then mousemove) seeks there and plays (setTime: currentTime = t,
//     then play() if paused);
//   - playbackRate is the media_playback_speed pref (e+f5 Cv: .25 to 2)
//     for an audio message or the flat style, 1 otherwise; the speed
//     control writes the pref (users.prefs.set);
//   - one plays at a time (MediaPlaybackCoordinator, the Manager), and
//     where the viewer stopped is kept and sent (use_media_elapsed,
//     NlgV's media.progress.update, as for videos).
//
// The audio player is a richview media slot like a video tile: richview
// draws the pillow, LayoutMedia the player in it.

// speeds is e+f5 Cv, the speed control's options.
var speeds = [...]float64{0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}

// isAudioMessage is lqEY's isAudioMessage: an audio story, a voice clip or
// a name pronunciation.
func isAudioMessage(f *richtext.File) bool {
	switch f.Subtype {
	case "audio_story", "slack_audio", "slack_name_pronunciation":
		return true
	}
	return false
}

// AudioFlat reports whether f's player is the flat row (MessageFilePreview's
// "default" style) rather than the card.
func AudioFlat(f *richtext.File) bool { return f != nil && isAudioMessage(f) }

// audioHint is a source of the kind an audio tile plays.
var audioHint = media.Source{Mime: "audio/mpeg", Ext: "mp3"}

// AudioAvailable reports whether audio files play in place here.
func (c *Controller) AudioAvailable() bool {
	return c != nil && c.env.Manager != nil && c.env.Manager.Takes(audioHint)
}

// audioPlays reports whether f plays as an audio tile.
func (c *Controller) audioPlays(f *richtext.File) bool {
	return c.AudioAvailable() && f != nil && f.IsAudio && audioURL(f) != "" && f.ID != "" && c.env.Manager.Takes(audioSource(f, audioURL(f)))
}

// audioURL is getMediaURL: the aac rendition, else the upload.
func audioURL(f *richtext.File) string {
	if f.MediaURL != "" {
		return f.MediaURL
	}
	return f.URL
}

// hasAlt reports whether f's player has the upload to fall back on (it
// plays the aac rendition).
func hasAlt(f *richtext.File) bool { return f.MediaURL != "" && f.URL != "" && f.MediaURL != f.URL }

// audioSource is the player's source for f at u: the demuxer from u's
// extension (the aac rendition is an m4a), else the filetype.
func audioSource(f *richtext.File, u string) media.Source {
	e := ""
	if pu, err := url.Parse(u); err == nil {
		e = strings.ToLower(strings.TrimPrefix(path.Ext(pu.Path), "."))
	}
	switch e {
	case "m4a", "mp3", "wav", "mp4", "mov", "webm", "aac":
	default:
		e = strings.ToLower(f.Type)
		if f.MediaURL != "" && u == f.MediaURL { // the aac rendition
			e = "m4a"
		}
	}
	mime := "audio/" + e
	switch e {
	case "m4a", "mp4", "mov", "aac":
		mime = "audio/mp4"
	case "mp3":
		mime = "audio/mpeg"
	}
	return media.Source{FileID: f.ID, Mime: mime, Ext: e}
}

// audioState is an audio tile's player UI.
type audioState struct {
	alt        bool // playing f.URL: the aac rendition didn't decode here
	interacted bool // D: a click or a scrub (the time shows the position from then on)
	errored    bool // Q: the element's error
	stalled    bool // K: waiting for data while playing
	rate       float64
	z          audioZones
	press      audioZone
	scrub      bool
}

type audioZone uint8

const (
	azNone audioZone = iota
	azPlay
	azScrub
	azSpeed
	azMore
	azTranscript
)

// audioZones are the controls as the last layout placed them (tile
// coordinates).
type audioZones struct {
	play, scrub, track, speed, more, transcript image.Rectangle
}

func (z *audioZones) at(p image.Point) audioZone {
	switch {
	case p.In(z.play):
		return azPlay
	case p.In(z.speed):
		return azSpeed
	case p.In(z.more):
		return azMore
	case p.In(z.transcript):
		return azTranscript
	case p.In(z.scrub):
		return azScrub
	}
	return azNone
}

// timeAt is Scrubber's setTime position: the pointer's share of the
// tracks (the scrubber less its 4px and 8px insets) times the duration.
func (z *audioZones) timeAt(p image.Point, d time.Duration) time.Duration {
	w := z.track.Dx()
	if w <= 0 || d <= 0 {
		return 0
	}
	f := min(1, max(0, float64(p.X-z.track.Min.X)/float64(w)))
	return time.Duration(f * float64(d))
}

// speedLocked is eg: the media_playback_speed pref when it's one of Cv
// and the player takes the pref (the flat style, or an audio message),
// else 1.
func (c *Controller) speedLocked(f *richtext.File) float64 {
	if !isAudioMessage(f) && !c.transcribeUI() {
		return 1
	}
	return c.prefSpeedLocked()
}

// prefSpeedLocked is media_playback_speed when it's one of Cv, else 1.
func (c *Controller) prefSpeedLocked() float64 {
	if c.env.Pref == nil {
		return 1
	}
	raw, ok := c.env.Pref("media_playback_speed")
	if !ok {
		return 1
	}
	var v float64
	if json.Unmarshal(raw, &v) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 1
		}
		var err error
		if v, err = strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
			return 1
		}
	}
	for _, s := range speeds {
		if s == v {
			return v
		}
	}
	return 1
}

// transcribeUI is transcribe_opt_in_ui_v2 (JG3p Xz): the card style gets
// the speed and transcript controls too.
func (c *Controller) transcribeUI() bool {
	return c.env.Experiment != nil && c.env.Experiment("transcribe_opt_in_ui_v2") == "on"
}

// showSpeed is eh: the flat style, or the card with the experiment.
func (c *Controller) showSpeed(f *richtext.File) bool { return AudioFlat(f) || c.transcribeUI() }

// showTranscript is MediaTranscriptPopoverButton's: the team allows media
// transcriptions (Qre6 y0) and the file's region can be transcribed.
func (c *Controller) showTranscript(f *richtext.File) bool {
	if c.env.TeamPref == nil || !f.TranscriptRegion {
		return false
	}
	v, ok := c.env.TeamPref("allow_media_transcriptions")
	return ok && prefBool(v)
}

// applyAudioLocked is the audio element as AudioPlayer leaves it: full
// volume, not muted (the video prefs aren't its), playbackRate eg.
func (c *Controller) applyAudioLocked(t *tile) {
	if t.au == nil {
		t.au = &audioState{}
	}
	if !t.applied {
		t.h.SetVolume(1)
		t.h.SetMuted(false)
		t.applied = true
	}
	if r := c.speedLocked(t.f); r != t.au.rate {
		t.h.SetRate(r)
		t.au.rate = r
	}
}

// audioClickLocked is the play button's eU: the first click mounts the
// player playing, later ones toggle it; a player that failed to load
// tries again (codes 1 and 2) or stays (3 and 4).
func (c *Controller) audioClickLocked(t *tile) {
	t.au.interacted = true
	if t.h == nil {
		c.mountLocked(t, true)
		return
	}
	st := t.h.State()
	switch st.Err {
	case media.ErrAborted, media.ErrNetwork:
		c.closeLocked(t)
		t.au.errored = false
		c.mountLocked(t, true)
		return
	case media.ErrDecode, media.ErrSrcNotSupported:
		return
	}
	if st.Paused || st.Ended || t.loading {
		t.loading = false
		t.h.Play()
	} else {
		t.h.Pause()
	}
}

// audioSeekLocked is Scrubber's setTime: there, and playing.
func (c *Controller) audioSeekLocked(t *tile, at time.Duration) {
	t.au.interacted = true
	t.elapsed = at
	if t.h == nil {
		t.startAt = at
		c.mountLocked(t, true)
		return
	}
	t.h.Seek(at)
	if st := t.h.State(); st.Paused || st.Ended {
		t.h.Play()
	}
}

// audioErrorLocked is the element's error: a decoder this build lacks
// (the aac rendition) moves the tile to url_private, once.
func (c *Controller) audioErrorLocked(t *tile, code int) bool {
	if t.au == nil {
		t.au = &audioState{}
	}
	if !t.au.alt && hasAlt(t.f) && t.dur == 0 && (code == media.ErrSrcNotSupported || code == media.ErrDecode) {
		t.au.alt = true
		autoplay := t.loading || t.playing
		c.closeLocked(t)
		c.mountLocked(t, autoplay)
		return true
	}
	t.au.errored = true
	return false
}

// shownTime is eW: the duration until it plays, is scrubbed or has a
// position, the position from then on.
func (t *tile) shownTime() time.Duration {
	if t.playing || t.au != nil && t.au.interacted || t.elapsed > 0 {
		return t.elapsed
	}
	return t.durLocked()
}

// layoutAudio is LayoutMedia for an audio file: its player in the pillow.
func (r *Row) layoutAudio(gtx layout.Context, s richview.MediaSlot) layout.Dimensions {
	c := r.c
	sz := gtx.Constraints.Max
	c.mu.Lock()
	t := c.tileLocked(r.key(s.File), s.File)
	if t.au == nil {
		t.au = &audioState{}
	}
	t.laid = c.frame
	if t.unmount != nil {
		t.unmount.Stop()
		t.unmount = nil
	}
	if t.h != nil {
		c.applyAudioLocked(t)
	}
	c.mu.Unlock()
	r.audioInput(gtx, t, s)
	r.drawAudio(gtx, t, s, sz)
	area := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	area.Pop()
	return layout.Dimensions{Size: sz}
}

// audioInput takes the tile's pointer events (the last frame's zones).
func (r *Row) audioInput(gtx layout.Context, t *tile, s richview.MediaSlot) {
	c := r.c
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Press | pointer.Release | pointer.Drag | pointer.Cancel})
		if !ok {
			return
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		p := image.Pt(int(e.Position.X), int(e.Position.Y))
		var menu audioZone
		c.mu.Lock()
		a := t.au
		switch e.Kind {
		case pointer.Enter, pointer.Move:
			t.hover, t.ptr = true, e.Position
		case pointer.Leave:
			t.hover = a.scrub
		case pointer.Cancel:
			a.scrub, a.press, t.hover = false, azNone, false
		case pointer.Press:
			if e.Buttons != pointer.ButtonPrimary || r.disabled(s.File) {
				break
			}
			switch z := a.z.at(p); z {
			case azScrub: // Scrubber's mousedown: the time there, playing
				a.scrub = true
				c.audioSeekLocked(t, a.z.timeAt(p, t.durLocked()))
			default:
				a.press = z
			}
		case pointer.Drag:
			t.ptr = e.Position
			if a.scrub && t.h != nil { // mousemove while held: setTime
				at := a.z.timeAt(p, t.durLocked())
				t.elapsed = at
				t.h.Seek(at)
			}
		case pointer.Release:
			if a.scrub {
				a.scrub = false
				c.reportLocked(t, true, 0)
				break
			}
			if z := a.z.at(p); a.press != azNone && z == a.press {
				switch z {
				case azPlay:
					c.audioClickLocked(t)
				case azSpeed, azMore, azTranscript:
					menu = z
				}
			}
			a.press = azNone
		}
		z := a.z
		c.mu.Unlock()
		if menu != azNone {
			r.audioMenu(gtx, t, s, menu, z)
		}
		if c.env.Repaint != nil {
			c.env.Repaint()
		}
	}
}

// disabled is the play button's isPending: the viewer's own clip still
// uploading.
func (r *Row) disabled(f *richtext.File) bool { return f.Pending }

// audioMenu opens the speed control's options, More actions or the
// transcript at the control's button (window coordinates).
func (r *Row) audioMenu(gtx layout.Context, t *tile, s richview.MediaSlot, z audioZone, zs audioZones) {
	c := r.c
	var btn image.Rectangle
	switch z {
	case azSpeed:
		btn = zs.speed
	case azMore:
		btn = zs.more
	case azTranscript:
		btn = zs.transcript
	}
	view := btn.Add(s.At)
	win := image.Rectangle{}
	if full, _, ok := gtx.Source.AreaBounds(t); ok {
		win = btn.Add(full.Min.Sub(c.frameOrigin()))
	}
	switch z {
	case azSpeed:
		if c.env.SpeedMenu == nil {
			return
		}
		c.mu.Lock()
		cur := c.prefSpeedLocked()
		c.mu.Unlock()
		sel := 3
		for i, v := range speeds {
			if v == cur {
				sel = i
			}
		}
		labels := make([]string, len(speeds))
		for i, v := range speeds {
			labels[i] = i18n.T("stories_av", "{speed}x", "speed", strconv.FormatFloat(v, 'f', -1, 64))
		}
		at := win
		if at.Empty() {
			at = view
		}
		c.env.SpeedMenu(at, labels, sel, func(i int) {
			// MediaPlaybackSpeedControl's onSelectionChange: the pref, which
			// the players' rate follows
			c.setPref("media_playback_speed", speeds[i])
			if c.env.Repaint != nil {
				c.env.Repaint()
			}
		})
	case azMore:
		if mo, ok := r.h.(AudioMenuOpener); ok && mo.OpenAudioClipMenu(s.File, win, view, nil) {
			return
		}
		if r.h != nil {
			r.h.FileAction(richview.FileMore, s.File, view)
		}
	case azTranscript:
		if to, ok := r.h.(TranscriptOpener); ok {
			to.OpenTranscript(s.File, win, view)
		}
	}
}

// AudioMenuOpener is optional on a RowHost: an audio clip's More actions
// (AudioClipOverflowMenu) under its button.
type AudioMenuOpener interface {
	OpenAudioClipMenu(f *richtext.File, win, view image.Rectangle, onClose func()) bool
}

// TranscriptOpener is optional on a RowHost: MediaTranscriptPopoverButton's
// popover, the file's transcript.
type TranscriptOpener interface {
	OpenTranscript(f *richtext.File, win, view image.Rectangle)
}

// audioPal are the player's colours from Slack's variables.
type audioPal struct {
	highlight, highlightHover, mid30, fg, fg70, lowSolid, highSolid color.NRGBA
	otlPry, basePry, max70                                          color.NRGBA
	cardBtn, cardBtnHover, cardIcon                                 color.NRGBA
	errText                                                         color.NRGBA
}

func audioPalette(dark bool) audioPal {
	vars := css.SlackVars(dark)
	sk := func(name string, a float64, def color.NRGBA) color.NRGBA {
		if v, ok := vars[name]; ok {
			if c, ok := css.ParseColor("rgb(" + v + ")"); ok {
				def = c
			}
		}
		def.A = uint8(math.Round(float64(def.A) * a))
		return def
	}
	dt := func(name string, def color.NRGBA) color.NRGBA {
		if v, ok := vars[name]; ok {
			if c, ok := css.ParseColor(v); ok {
				return c
			}
		}
		return def
	}
	ink := color.NRGBA{R: 29, G: 28, B: 29, A: 255}
	return audioPal{
		highlight:      sk("--sk_highlight", 1, color.NRGBA{R: 18, G: 100, B: 163, A: 255}),
		highlightHover: sk("--sk_highlight_hover", 1, color.NRGBA{R: 11, G: 76, B: 140, A: 255}),
		mid30:          sk("--sk_foreground_mid", .3, ink),
		fg:             sk("--sk_primary_foreground", 1, ink),
		fg70:           sk("--sk_primary_foreground", .7, ink),
		lowSolid:       sk("--sk_foreground_low_solid", 1, color.NRGBA{R: 221, G: 221, B: 221, A: 255}),
		highSolid:      sk("--sk_foreground_high_solid", 1, color.NRGBA{R: 134, G: 134, B: 134, A: 255}),
		otlPry:         dt("--dt_color-otl-pry", color.NRGBA{R: 29, G: 28, B: 29, A: 77}),
		basePry:        dt("--dt_color-base-pry", color.NRGBA{R: 255, G: 255, B: 255, A: 255}),
		max70:          sk("--sk_foreground_max", .7, ink),
		cardBtn:        color.NRGBA{R: 0xe6, G: 0xef, B: 0xf6, A: 255},
		cardBtnHover:   color.NRGBA{R: 0xd3, G: 0xe3, B: 0xef, A: 255},
		cardIcon:       color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 255},
		errText:        dt("--dt_color-content-imp", color.NRGBA{R: 192, G: 19, B: 67, A: 255}),
	}
}

// drawAudio draws the player in the pillow's content box (the pillow's
// 12px padding and 1px border in).
func (r *Row) drawAudio(gtx layout.Context, t *tile, s richview.MediaSlot, sz image.Point) {
	c := r.c
	th := c.theme()
	if th == nil {
		return
	}
	pal := audioPalette(th.Dark)
	dp := func(v float32) int { return gtx.Dp(unit.Dp(v)) }
	in := image.Rect(dp(13), dp(13), sz.X-dp(13), sz.Y-dp(13))
	c.mu.Lock()
	a := t.au
	playing := t.playing && !t.loading
	elapsed, dur := t.elapsed, t.durLocked()
	shown := t.shownTime()
	hot := t.hover
	ptr := image.Pt(int(t.ptr.X), int(t.ptr.Y))
	speed := c.prefSpeedLocked()
	flat := AudioFlat(s.File)
	showSpeed, showTr := c.showSpeed(s.File), c.showTranscript(s.File)
	errored, stalled := a.errored, a.stalled
	c.mu.Unlock()
	var z audioZones
	frac := 0.0
	if dur > 0 {
		frac = min(1, max(0, float64(elapsed)/float64(dur)))
	}
	if flat {
		bs := dp(34)
		cy := in.Min.Y + (in.Dy()-bs)/2
		z.play = image.Rect(in.Min.X, cy, in.Min.X+bs, cy+bs)
		right := in.Max.X
		z.more = image.Rect(right-dp(32), in.Min.Y+(in.Dy()-dp(32))/2, right, in.Min.Y+(in.Dy()+dp(32))/2)
		right = z.more.Min.X
		if showSpeed { // .p-media_controls__control: margin 0 8px
			_, tsz := r.text(gtx, speedLabel(speed), 13, true, pal.max70)
			w := tsz.X + dp(16) + dp(2)
			z.speed = image.Rect(right-dp(8)-w, in.Min.Y+(in.Dy()-dp(28))/2, right-dp(8), in.Min.Y+(in.Dy()+dp(28))/2)
			right = z.speed.Min.X - dp(8)
		}
		if showTr {
			z.transcript = image.Rect(right-dp(32), in.Min.Y+(in.Dy()-dp(32))/2, right, in.Min.Y+(in.Dy()+dp(32))/2)
			right = z.transcript.Min.X - dp(8) // margin_left_50
		}
		tc, tsz := r.text(gtx, audioClock(shown), 13, true, pal.fg)
		tx := right - tsz.X
		z.scrub = image.Rect(z.play.Max.X+dp(12), cy, tx-dp(4), cy+bs)
		z.track = image.Rect(z.scrub.Min.X+dp(4), z.scrub.Min.Y, z.scrub.Max.X-dp(8), z.scrub.Max.Y)
		// the play button: sk_highlight, hovered sk_highlight_hover, gray
		// while disabled
		btnCol := pal.highlight
		switch {
		case r.disabled(s.File):
			btnCol = pal.mid30
		case hot && ptr.In(z.play):
			btnCol = pal.highlightHover
		}
		fillCircle(gtx, z.play, btnCol)
		r.playIcon(gtx, z.play, playing, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		if len(s.File.Waveform) > 0 {
			r.waveform(gtx, z.scrub, s.File.Waveform, frac, pal)
		} else {
			r.track(gtx, z.track, frac, playing, pal)
		}
		off := op.Offset(image.Pt(tx, in.Min.Y+(in.Dy()-tsz.Y)/2)).Push(gtx.Ops)
		tc.Add(gtx.Ops)
		off.Pop()
		if showTr {
			r.iconButton(gtx, z.transcript, "list-view", hot && ptr.In(z.transcript), pal)
		}
		if showSpeed {
			r.speedButton(gtx, z.speed, speed, hot && ptr.In(z.speed), pal)
		}
		r.iconButton(gtx, z.more, "ellipsis-vertical-filled", hot && ptr.In(z.more), pal)
	} else {
		bs := dp(36)
		z.play = image.Rect(in.Min.X, in.Min.Y, in.Min.X+bs, in.Min.Y+bs)
		btnCol := pal.cardBtn
		if hot && ptr.In(z.play) {
			btnCol = pal.cardBtnHover
		}
		fillCircle(gtx, z.play, btnCol)
		r.playIcon(gtx, z.play, playing, pal.cardIcon)
		// AudioPlayerHeader: the name, then "m:ss (size)"
		hx := z.play.Max.X + dp(12)
		nc, nsz := r.text(gtx, audioName(s.File), 15, true, pal.fg)
		clipW := in.Max.X - hx
		func() {
			defer clip.Rect(image.Rect(hx, in.Min.Y, hx+clipW, in.Min.Y+nsz.Y)).Push(gtx.Ops).Pop()
			off := op.Offset(image.Pt(hx, in.Min.Y)).Push(gtx.Ops)
			nc.Add(gtx.Ops)
			off.Pop()
		}()
		meta := ""
		if dur > 0 {
			meta = audioClock(dur) + " "
		}
		meta += "(" + sizeText(s.File.Size) + ")"
		mc, _ := r.text(gtx, meta, 13, false, pal.fg70)
		off := op.Offset(image.Pt(hx, in.Min.Y+dp(18))).Push(gtx.Ops)
		mc.Add(gtx.Ops)
		off.Pop()
		// AudioPlayerStates: buffering while stalled, the error once it
		// failed after a click
		switch {
		case errored && a.interacted:
			ec, esz := r.text(gtx, i18n.T("audio_player", "Error"), 13, false, pal.errText)
			off := op.Offset(image.Pt(in.Max.X-esz.X, in.Min.Y+(bs-esz.Y)/2)).Push(gtx.Ops)
			ec.Add(gtx.Ops)
			off.Pop()
		case stalled && playing:
			bc, bsz := r.text(gtx, i18n.T("audio_player", "Buffering"), 13, false, pal.fg70)
			off := op.Offset(image.Pt(in.Max.X-bsz.X, in.Min.Y+(bs-bsz.Y)/2)).Push(gtx.Ops)
			bc.Add(gtx.Ops)
			off.Pop()
		}
		// the controls: the scrubber (18px), the time
		ry := z.play.Max.Y + dp(12)
		tc, tsz := r.text(gtx, audioClock(shown), 13, false, pal.fg70)
		right := in.Max.X
		if c.transcribeUI() {
			z.speed = image.Rect(right-dp(8)-dp(36), ry+(dp(18)-dp(28))/2, right-dp(8), ry+(dp(18)+dp(28))/2)
			right = z.speed.Min.X - dp(8)
		}
		tx := right - tsz.X
		z.scrub = image.Rect(in.Min.X, ry, tx-dp(4), ry+dp(18))
		z.track = image.Rect(z.scrub.Min.X+dp(4), ry, z.scrub.Max.X-dp(8), ry+dp(18))
		r.track(gtx, z.track, frac, playing, pal)
		toff := op.Offset(image.Pt(tx, ry+(dp(18)-tsz.Y)/2)).Push(gtx.Ops)
		tc.Add(gtx.Ops)
		toff.Pop()
		if c.transcribeUI() {
			r.speedButton(gtx, z.speed, speed, hot && ptr.In(z.speed), pal)
		}
	}
	if hot && (ptr.In(z.play) || ptr.In(z.scrub) || ptr.In(z.speed) || ptr.In(z.more) || ptr.In(z.transcript)) {
		pointer.CursorPointer.Add(gtx.Ops)
	}
	// the controls' tooltips: the speed control's, More's and the
	// transcript's (AudioPlayerPlayButton is an unstyled button with no
	// tooltip, only its aria-label "Play")
	if c.env.Tooltip != nil {
		if showSpeed {
			c.env.Tooltip(gtx, &a.z.speed, hot && ptr.In(z.speed), z.speed, false, 0, i18n.T("stories_av", "Playback speed"))
		}
		if flat {
			c.env.Tooltip(gtx, &a.z.more, hot && ptr.In(z.more), z.more, false, 0, i18n.T("file", "More actions"))
		}
		if showTr {
			c.env.Tooltip(gtx, &a.z.transcript, hot && ptr.In(z.transcript), z.transcript, false, 0, transcriptTip(s.File))
		}
	}
	c.mu.Lock()
	a.z = z
	c.mu.Unlock()
}

// transcriptTip is MediaTranscriptPopoverButton's label for the status.
func transcriptTip(f *richtext.File) string {
	switch f.Transcript {
	case "processing":
		return i18n.T("stories_av", "Processing transcript…")
	case "complete":
		return i18n.T("stories_av", "View transcript")
	}
	return i18n.T("stories_av", "Transcript isn’t available")
}

// audioName is AudioPlayerHeader's name: the title, else the file name.
func audioName(f *richtext.File) string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

// audioClock is secondsToAudioTime (7foM): m:ss, or h:mm:ss.
func audioClock(d time.Duration) string { return clock(d) }

// speedLabel is the speed control's "{speed}x".
func speedLabel(v float64) string {
	return i18n.T("stories_av", "{speed}x", "speed", strconv.FormatFloat(v, 'f', -1, 64))
}

// sizeText is R9ib's file size: B, KB, MB, GB in 1024s, one decimal past
// KB.
func sizeText(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return strconv.FormatInt(n, 10) + " B"
	case n < k*k:
		return strconv.FormatInt(int64(math.Round(float64(n)/k)), 10) + " KB"
	case n < k*k*k:
		return strconv.FormatFloat(math.Round(float64(n)/(k*k)*10)/10, 'f', -1, 64) + " MB"
	}
	return strconv.FormatFloat(math.Round(float64(n)/(k*k*k)*10)/10, 'f', -1, 64) + " GB"
}

// text records txt (sp px) and its size.
func (r *Row) text(gtx layout.Context, txt string, sp float32, bold bool, col color.NRGBA) (op.CallOp, image.Point) {
	th := r.c.theme()
	m := op.Record(gtx.Ops)
	var d layout.Dimensions
	if th != nil && txt != "" {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(1<<16, 1<<16)}
		w := font.Normal
		if bold {
			w = theme.Bold
		}
		d = th.Text(g, theme.LabelOpts{Font: font.Font{Weight: w}, Size: unit.Sp(sp), LineHeight: unit.Sp(18), MaxLines: 1, Color: col}, txt)
	}
	return m.Stop(), d.Size
}

func fillCircle(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	defer clip.Ellipse(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func rrect(gtx layout.Context, r image.Rectangle, rad int, col color.NRGBA) {
	defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// playIcon is AudioPlayerPlayButton's glyph: play-filled while paused,
// pause-bold while playing, 20px, centred.
func (r *Row) playIcon(gtx layout.Context, b image.Rectangle, playing bool, col color.NRGBA) {
	name := "play"
	if playing {
		name = "pause-bold"
	}
	s := gtx.Dp(20)
	off := op.Offset(image.Pt(b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2)).Push(gtx.Ops)
	icons.Get(name)(gtx, unit.Dp(20), col)
	off.Pop()
}

// waveform is the scrubber's AudioWaveform (the samples / 100, as many
// dots as its width takes: Scrubber's (w - w%3, then an odd multiple of
// 3, + 3) / 6), with the elapsed share drawn again in sk_highlight
// (c-audio_player_scrubber__waveform_elapsed_wrapper).
func (r *Row) waveform(gtx layout.Context, sc image.Rectangle, samples []int, frac float64, pal audioPal) {
	px := gtx.Metric.PxPerDp
	if px <= 0 {
		px = 1
	}
	w := int(float32(sc.Dx()) / px)
	i := w - w%3
	rw := i
	if i%6 == 0 {
		rw = i - 3
	}
	n := (rw + 3) / 6
	if w <= 0 || rw <= 0 || n <= 0 {
		return
	}
	data := make([]float64, len(samples))
	for k, s := range samples {
		data[k] = float64(s) / 100
	}
	data = resample(data, n)
	elapsedX := sc.Min.X + int(frac*float64(gtx.Dp(unit.Dp(float32(rw)))))
	for k := 0; k < n && k < len(data); k++ {
		v := data[k]
		h := math.Max(6, math.Min(6+34*v, 34))
		if math.IsNaN(v) {
			h = 6
		}
		hp := gtx.Dp(unit.Dp(float32(h)))
		x := sc.Min.X + gtx.Dp(unit.Dp(float32(6*k)))
		cy := sc.Min.Y + sc.Dy()/2
		d := image.Rect(x, cy-hp/2, x+gtx.Dp(3), cy-hp/2+hp)
		rrect(gtx, d, max(1, gtx.Dp(1.5)), pal.mid30)
		if x < elapsedX { // the elapsed wrapper clips the blue copy
			func() {
				defer clip.Rect(image.Rect(sc.Min.X, sc.Min.Y, elapsedX, sc.Max.Y)).Push(gtx.Ops).Pop()
				rrect(gtx, d, max(1, gtx.Dp(1.5)), pal.highlight)
			}()
		}
	}
}

// resample is AudioWaveform's (d0gJ): RMS over each dot's span when there
// are more samples than dots, linear interpolation when fewer.
func resample(data []float64, width int) []float64 {
	n := len(data)
	if n == 0 || width <= 0 || n == width {
		return data
	}
	out := make([]float64, 0, width)
	if n > width {
		step := float64(n) / float64(width)
		for i := 0; i < width; i++ {
			t := float64(i) * step
			lo, hi := max(0, int(math.Round(t))), min(n, int(math.Round(t+step)))
			s := 0.0
			for j := lo; j < hi; j++ {
				s += data[j] * data[j]
			}
			out = append(out, math.Sqrt(s/float64(hi-lo)))
		}
		return out
	}
	if width == 1 {
		return append(out, data[0])
	}
	step := float64(n-1) / float64(width-1)
	for i := 0; i < width; i++ {
		t := float64(i) * step
		lo := int(math.Floor(t))
		hi := min(n-1, int(math.Ceil(t)))
		f := t - float64(lo)
		out = append(out, data[hi]*f+data[lo]*(1-f))
	}
	return out
}

// track is the scrubber without a waveform: the 4px track, #1264a3 to
// the time and sk_foreground_low_solid after, and the 12px thumb there
// (sk_foreground_high_solid while paused).
func (r *Row) track(gtx layout.Context, tr image.Rectangle, frac float64, playing bool, pal audioPal) {
	if tr.Dx() <= 0 {
		return
	}
	cy := tr.Min.Y + tr.Dy()/2
	h := gtx.Dp(4)
	bar := image.Rect(tr.Min.X, cy-h/2, tr.Max.X, cy-h/2+h)
	rrect(gtx, bar, gtx.Dp(2), pal.lowSolid)
	ex := tr.Min.X + int(frac*float64(tr.Dx()))
	rrect(gtx, image.Rect(tr.Min.X, bar.Min.Y, ex, bar.Max.Y), gtx.Dp(2), color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 255})
	th := gtx.Dp(12)
	thumb := image.Rect(ex-gtx.Dp(4), cy-th/2, ex-gtx.Dp(4)+th, cy-th/2+th)
	col := color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 255}
	if !playing {
		col = pal.highSolid
	}
	fillCircle(gtx, thumb, col)
}

// speedButton is MediaPlaybackSpeedControl's small Select: 28px, 0 8px,
// a 1px otl-pry border (radius 8), base-pry, "1x" in bold 13px at
// sk_foreground_max .7, no chevron.
func (r *Row) speedButton(gtx layout.Context, b image.Rectangle, v float64, hot bool, pal audioPal) {
	rrect(gtx, b, gtx.Dp(8), pal.otlPry)
	inner := b.Inset(max(1, gtx.Dp(1)))
	rrect(gtx, inner, max(0, gtx.Dp(8)-1), pal.basePry)
	tc, tsz := r.text(gtx, speedLabel(v), 13, true, pal.max70)
	off := op.Offset(image.Pt(b.Min.X+(b.Dx()-tsz.X)/2, b.Min.Y+(b.Dy()-tsz.Y)/2)).Push(gtx.Ops)
	tc.Add(gtx.Ops)
	off.Pop()
}

// iconButton is a 32px icon button (c-icon_button--size_smedium), a 16px
// glyph at sk_foreground_max .7, tinted while hovered.
func (r *Row) iconButton(gtx layout.Context, b image.Rectangle, name string, hot bool, pal audioPal) {
	if hot {
		tint := pal.fg
		tint.A = 0x14
		rrect(gtx, b, gtx.Dp(8), tint)
	}
	s := gtx.Dp(16)
	off := op.Offset(image.Pt(b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2)).Push(gtx.Ops)
	icons.Get(name)(gtx, unit.Dp(16), pal.max70)
	off.Pop()
}
