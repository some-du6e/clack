package popups

// The profile's name recording, as Slack's web client has it (build
// 9f450b8ad3db, 2026-11-22, read from the public CDN without a cookie;
// core/profile_write.go has the writes):
//
//   - Edit your profile's "Name recording" field: /uhB's field list puts
//     name_recording (type audio_recorder_button, label "Name recording")
//     after Pronouns and before Name pronunciation when the HEADER section
//     has the "Name Recording" file element (the header hook O0Pg's
//     isNameRecordingEnabled: the element has an id). BaseEditProfileField
//     (rguE) draws it as its label over EditAudioButton, the field 24px
//     above the next (.p-edit_profile__audio_recorder_component):
//     - "Record Audio Clip" (rimeto_user_profile): an outline Kit button,
//       the 20px microphone and the label (.p-edit_profile__audio_recorder_
//       icon: padding 16px 8px);
//     - its click: NamePronunciationAudioRecorder, the composer clip's hook
//       (useAudioWaveformRecorder 3xSY) with audio_name_pronunciation.m4a,
//       subtype slack_name_pronunciation, timeLimitSeconds 10 and
//       timeWarningSeconds 5: .p-audio_file, Cancel recording (the
//       close-filled ×, 36px, soft_solid), the strip (the 24 analyser dots,
//       the m:ss timer, red in the last 5 s), Stop recording (the ✓, 36px,
//       highlight; aria-label Attach recording). × goes back to the
//       button; ✓, or the 10 s limit, attaches the recording, which starts
//       its upload at once (finishUpload with no draft);
//     - an attached recording, and the profile's own when the form opens:
//       PendingAudioFile, MultiFileUploadItem's tile (PendingFile in
//       .p-audio_file__pending_file: radius 4, the mid .3 border) holding
//       the mini audio player, disabled while the recording is still its
//       bytes (PendingAudioMessageUpload) and the file's, which plays, once
//       uploaded (FileAudio); RemoveFileButton's spinner while it uploads,
//       its warning when the upload failed, its "Remove audio clip"
//       otherwise; removing goes back to the button.
//   - The form's Save waits for the upload (onFileUploadStart/End: the
//     form busy); an upload that failed keeps the form busy until its file
//     is removed, as EditAudioButton only ends it on the upload's file id
//     or a remove.
//   - Save writes the recording's id into the header section and deletes
//     the recording it replaced; Cancel, and removing a new recording,
//     delete that new file (EditProfileFormPane's e5: files.delete).
//   - A microphone that won't open is MicrophoneAccessErrorDialog (AJKj);
//     the recorder stays up for its ×.

import (
	"context"
	"image"
	"image/color"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// NameRecDevice, when set, replaces the microphone the name recorder opens
// (tests).
var NameRecDevice func() huddle.Device

// NameRecStartSound plays the recorder's start sound (the ui root wires it
// to Slack's CDN sound, vclip.StartSound, as for the composer's clips).
var NameRecStartSound func()

// AppName is the app's name where Slack's strings say Slack (the ui root
// sets it).
var AppName = "Clack"

var (
	nsRimetoUser = i18n.NS("rimeto_user_profile")
	nsStoriesAV  = i18n.NS("stories_av")
	nsDraftsFile = i18n.NS("drafts")
)

// The name recorder's File (NamePronunciationAudioRecorder's fileName and
// fileSubtype).
const (
	NameRecFileName = "audio_name_pronunciation.m4a"
	NameRecSubtype  = "slack_name_pronunciation"
)

// Its limits: timeLimitSeconds 10, timeWarningSeconds 5 (tests shorten
// the limit).
var (
	nameRecLimit   = 10 * time.Second
	nameRecWarning = 5 * time.Second
)

type nrMode int

const (
	nrButton    nrMode = iota // "Record Audio Clip"
	nrRecording               // NamePronunciationAudioRecorder
	nrFile                    // PendingAudioFile
)

// epNameRec is EditAudioButton with the form's recording (EditProfileFormPane's
// ea and eK): the UI goroutine's.
type epNameRec struct {
	f       *epForm
	mode    nrMode
	rec     *vclip.Recorder
	recCore *core.App // the session the recording was made in
	alerted bool      // MicrophoneAccessErrorDialog shown for rec
	// focusAttach: the recorder just mounted, and Stop recording takes the
	// focus (both its buttons autoFocus, without the ring; the later wins)
	focusAttach bool
	// the file: the recording's bytes until its upload answers (clip), the
	// uploaded file or the profile's own (file)
	clip      *vclip.File
	uploading bool
	upErr     error
	file      *slack.File
	gen       int // a remove (or a new recording) drops the upload in flight
	// was is the profile's recording when the form opened (ea), cur the
	// form's (eK): the uploaded recording's id, "" once removed
	was, cur string

	recBtn, cancelBtn, attachBtn, removeBtn widget.Clickable
	tileHov                                 widget.Clickable
	cancelTip, attachTip, removeTip         TipState
	ctl                                     *videoplayer.Controller
	row                                     *videoplayer.Row
}

// liveNameRecs are the name recorders recording now (CloseNameRecorders).
var liveNameRecs struct {
	sync.Mutex
	m map[*epNameRec]struct{}
}

// CloseNameRecorders drops every name recording in progress, as their
// forms go with the session (the sign-in screen, another account): the
// recorder's unmount stops its StreamManager, and nothing attaches or
// uploads.
func CloseNameRecorders() {
	liveNameRecs.Lock()
	ns := make([]*epNameRec, 0, len(liveNameRecs.m))
	for n := range liveNameRecs.m {
		ns = append(ns, n)
	}
	liveNameRecs.Unlock()
	for _, n := range ns {
		n.unmount()
	}
}

// opened is the header section's recording as the form opened: the
// profile's file shows as soon as it's known.
func (n *epNameRec) opened(was string) {
	n.was, n.cur = was, was
	if was == "" {
		return
	}
	a := n.f.h.Core()
	if a == nil {
		return
	}
	run(n.f.h, func(ctx context.Context) (*slack.File, error) {
		return a.ProfileRecordingFile(ctx, was)
	}, func(fl *slack.File, err error) {
		if err != nil || fl == nil || n.cur != was || n.mode != nrButton {
			return
		}
		n.file, n.mode = fl, nrFile
		n.f.h.Invalidate()
	})
}

// start is the button's click: the recorder, recording.
func (n *epNameRec) start() {
	agc := true // prefs.audio.enableAGC defaults on
	if h, ok := n.f.h.(interface{ HuddleAudioPrefs() (agc, ns bool) }); ok {
		agc, _ = h.HuddleAudioPrefs()
	}
	var dev huddle.Device
	if NameRecDevice != nil {
		dev = NameRecDevice()
	}
	n.alerted = false
	n.recCore = n.f.h.Core()
	n.rec = vclip.Start(vclip.Options{AGC: agc, Device: dev, PlayStart: NameRecStartSound, OnChange: n.f.h.Invalidate,
		TimeLimit: nameRecLimit, TimeWarning: nameRecWarning, FileName: NameRecFileName, Subtype: NameRecSubtype})
	n.mode, n.focusAttach = nrRecording, true
	liveNameRecs.Lock()
	if liveNameRecs.m == nil {
		liveNameRecs.m = map[*epNameRec]struct{}{}
	}
	liveNameRecs.m[n] = struct{}{}
	liveNameRecs.Unlock()
}

// stopRec ends the recorder's part: no recording is live after it.
func (n *epNameRec) stopRec() {
	liveNameRecs.Lock()
	delete(liveNameRecs.m, n)
	liveNameRecs.Unlock()
	n.rec = nil
}

// cancelRec is the recorder's ×: the recording goes (onCancel), and so
// does the recorder (onCancelRecording).
func (n *epNameRec) cancelRec() {
	if n.rec != nil {
		n.rec.Cancel()
	}
	n.stopRec()
	n.mode = nrButton
}

// unmount is the recorder's unmount (the form closing, the session
// ending): a recording in progress is dropped.
func (n *epNameRec) unmount() {
	if n.rec != nil {
		n.rec.Cancel()
		n.stopRec()
		n.mode = nrButton
	}
}

// step follows the recorder: a stop that made the file attaches it (the
// hook's effect: finishUpload, then onFileAttach), a microphone that
// failed opens the dialog.
func (n *epNameRec) step() {
	if n.mode != nrRecording || n.rec == nil {
		return
	}
	switch n.rec.State() {
	case vclip.Stopped:
		f, ok := n.rec.File()
		sess := n.recCore
		n.stopRec()
		if !ok || sess == nil || n.f.h.Core() != sess {
			n.mode = nrButton // the session it was recorded in went
			return
		}
		n.attach(f)
	case vclip.Failed:
		if !n.alerted {
			n.alerted = true
			ShowMicrophoneAccessError(n.f.h)
		}
	}
}

// attach is onAttachRawFile and finishUpload: the tile, uploading.
func (n *epNameRec) attach(f vclip.File) {
	a := n.f.h.Core()
	if a == nil {
		n.mode = nrButton
		return
	}
	n.mode, n.clip, n.file, n.upErr, n.uploading = nrFile, &f, nil, nil, true
	n.gen++
	gen := n.gen
	n.f.uploadBusy(true) // onFileUploadStart
	// the upload with Slack's processing poll (core.UploadPollFor), not
	// run's 15 s: a slow file_created must not orphan it and hold Save
	runFor(n.f.h, core.UploadPollFor+time.Minute, func(ctx context.Context) (core.PreparedUpload, error) {
		return a.PrepareUpload(ctx, core.UploadFile{Name: f.Name, Title: f.Name, Data: f.Data, Subtype: f.Subtype}, nil)
	}, func(pu core.PreparedUpload, err error) {
		if gen != n.gen {
			return // removed meanwhile
		}
		n.uploading = false
		if err != nil {
			n.upErr = err // the warning; the form stays busy until it's removed
			n.f.h.Invalidate()
			return
		}
		a.NoteProfileRecordingFile(pu.File)
		n.cur, n.file = pu.ID, pu.File
		n.f.uploadBusy(false) // onUploadDone: onChangeFile(id), onFileUploadEnd
		n.f.h.Invalidate()
	})
}

// remove is RemoveFileButton's click (onRemoveFile → onChangeFile(null)):
// a recording made in this form is deleted at once, the profile's own at
// Save; the button comes back.
func (n *epNameRec) remove() {
	if n.cur != "" && n.cur != n.was {
		n.deleteFile(n.cur)
	}
	n.gen++
	n.cur, n.clip, n.file, n.upErr, n.uploading = "", nil, nil, nil, false
	n.mode = nrButton
	n.f.uploadBusy(false) // onRemove: onFileUploadEnd
}

// cancelled is the form's Cancel (handleCancel): a recording not saved is
// deleted.
func (n *epNameRec) cancelled() {
	n.unmount()
	if n.cur != "" && n.cur != n.was {
		n.deleteFile(n.cur)
		n.cur = n.was
	}
}

// deleteFile is e5: files.delete, not awaited.
func (n *epNameRec) deleteFile(id string) {
	a := n.f.h.Core()
	if a == nil {
		return
	}
	run(n.f.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.DeleteFile(ctx, id)
	}, func(struct{}, error) {})
}

// focusStops are EditAudioButton's tab stops in document order for its
// mode: Record Audio Clip; the recorder's Cancel recording then Stop
// recording; the tile's RemoveFileButton.
func (n *epNameRec) focusStops() []event.Tag {
	switch n.mode {
	case nrRecording:
		return []event.Tag{&n.cancelBtn, &n.attachBtn}
	case nrFile:
		return []event.Tag{&n.removeBtn}
	}
	return []event.Tag{&n.recBtn}
}

// Recording is the form's name recording (tests): its file id, and
// whether it's recording, uploading, or its upload failed.
func (f *epForm) Recording() (id string, recording, uploading, failed bool) {
	n := &f.nr
	return n.cur, n.mode == nrRecording, n.uploading, n.upErr != nil
}

// NameRecorder is the form's recorder while it records (tests).
func (f *epForm) NameRecorder() *vclip.Recorder { return f.nr.rec }

// RecordName, StopName, CancelName and RemoveName are the field's clicks
// (tests): Record Audio Clip, ✓, × and the tile's remove.
func (f *epForm) RecordName() {
	if f.nr.mode == nrButton {
		f.nr.start()
	}
}

func (f *epForm) StopName() {
	if r := f.nr.rec; r != nil && r.State() == vclip.Recording {
		go r.Stop()
	}
}

func (f *epForm) CancelName() {
	if f.nr.mode == nrRecording {
		f.nr.cancelRec()
	}
}

func (f *epForm) RemoveName() {
	if f.nr.mode == nrFile {
		f.nr.remove()
	}
}

// nrColors are the recorder's and the tile's colours from Slack's
// variables for the theme's mode.
type nrColors struct {
	bg, fg70, fg, low13, mid30, soft, highlight, warnText, warnDot, shadow color.NRGBA
}

func nrPalette(th *theme.Theme) nrColors {
	vars := css.SlackVars(th.Dark)
	sk := func(name string, a float64, def color.NRGBA) color.NRGBA {
		if v, ok := vars[name]; ok {
			if c, ok := css.ParseColor("rgb(" + v + ")"); ok {
				def = c
			}
		}
		def.A = uint8(math.Round(float64(def.A) * a))
		return def
	}
	p := nrColors{
		bg:        sk("--sk_primary_background", 1, th.C.Bg),
		fg:        sk("--sk_primary_foreground", 1, th.C.Fg),
		fg70:      sk("--sk_foreground_max", .7, th.C.Fg),
		low13:     sk("--sk_foreground_low", .13, th.C.Fg),
		mid30:     sk("--sk_foreground_mid", .3, th.C.Fg),
		soft:      sk("--sk_foreground_soft_solid", 1, rgb(0xf3f3f3)),
		highlight: sk("--sk_highlight", 1, th.C.Link),
		warnText:  th.C.Danger,
		warnDot:   th.C.Danger,
		shadow:    argbC(0x14000000),
	}
	if c, ok := css.ParseColor(vars["--dt_color-content-imp"]); ok {
		p.warnText = c
	}
	if c, ok := css.ParseColor(vars["--dt_color-base-inv-imp"]); ok {
		p.warnDot = c
	}
	return p
}

func argbC(v uint32) color.NRGBA {
	return color.NRGBA{A: uint8(v >> 24), R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
}

// layout is BaseEditProfileField's audio_recorder_button: the label, the
// control, 24px under it.
func (n *epNameRec) layout(gtx layout.Context, th *theme.Theme, cl *Colors, fd *epField, w int) layout.Dimensions {
	n.step()
	if n.recBtn.Clicked(gtx) && n.mode == nrButton {
		n.start()
	}
	if n.cancelBtn.Clicked(gtx) && n.mode == nrRecording {
		n.cancelRec()
	}
	if n.attachBtn.Clicked(gtx) && n.rec != nil && n.rec.State() == vclip.Recording {
		go n.rec.Stop()
	}
	if n.removeBtn.Clicked(gtx) && n.mode == nrFile {
		n.remove()
	}
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, fd.label)
		})
	})
	y := lh + dp(gtx, 8)
	var d layout.Dimensions
	switch n.mode {
	case nrRecording:
		d = at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return n.recorder(gtx, th) })
	case nrFile:
		// the tile's status button reaches 10px over its top-right corner
		y += dp(gtx, 10)
		d = at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return n.tile(gtx, th) })
	default:
		d = at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return n.button(gtx, th, cl) })
	}
	y += d.Size.Y + dp(gtx, 24)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// button is "Record Audio Clip": an outline medium Kit button (36 tall,
// the 20px microphone, 4px, the label).
func (n *epNameRec) button(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	label := nsRimetoUser.T("Record Audio Clip")
	h := dp(gtx, 36)
	tw := textW(gtx, th, 15, theme.Bold, label)
	w := dp(gtx, 8) + dp(gtx, 20) + dp(gtx, 4) + tw + dp(gtx, 8) + 2*border1(gtx)
	ring := n.f.m.ringOn(gtx, &n.recBtn)
	return clickBox(gtx, &n.recBtn, func(gtx layout.Context, sz image.Point, hovered bool) {
		r := image.Rectangle{Max: sz}
		if ring {
			focusRing(gtx, cl, r, dp(gtx, 8))
		}
		bg := cl.BtnBg
		if hovered {
			bg = cl.BtnHover
		}
		rrect(gtx, r, dp(gtx, 8), bg)
		stroke(gtx, r, dp(gtx, 8), border1(gtx), cl.BtnBorder)
	}, func(gtx layout.Context) layout.Dimensions {
		x := dp(gtx, 8) + border1(gtx)
		at(gtx, image.Pt(x, (h-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "microphone", 20, cl.Fg)
		})
		x += dp(gtx, 24)
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, label)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// recorder is NamePronunciationAudioRecorder's .p-audio_file: the 36px
// Cancel recording, 12px, the strip (12 in, the 24 dots, the timer 16px
// after them, 16 in), 8px and 4px, the 36px Stop recording.
func (n *epNameRec) recorder(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	pal := nrPalette(th)
	cl := colorsOf(th)
	r := n.rec
	b := dp(gtx, 36)
	x := 0
	if n.focusAttach {
		n.focusAttach = false
		n.f.m.focusTo(gtx, &n.attachBtn, false) // autoFocus, visibleAutoFocus off
	}
	// Cancel recording
	if n.f.m.ringOn(gtx, &n.cancelBtn) {
		focusRing(gtx, cl, image.Rect(x, 0, x+b, b), dp(gtx, 4))
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(b, b))
		return n.cancelBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rrect(gtx, image.Rect(0, 0, b, b), dp(gtx, 4), pal.soft)
			at(gtx, image.Pt((b-dp(gtx, 20))/2, (b-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "close-filled", 20, pal.fg70)
			})
			pointerCursor(gtx, image.Pt(b, b))
			Tooltip(gtx, th, &n.cancelTip, n.cancelBtn.Hovered(), image.Pt(b, b), Above, nsRimetoUser.T("Cancel recording"), "")
			return layout.Dimensions{Size: image.Pt(b, b)}
		})
	})
	x += b + dp(gtx, 12)
	// the strip
	warn := r != nil && r.Warning()
	timer := "0:00"
	var dots []float64
	if r != nil {
		timer = nrTime(math.Floor(r.Elapsed().Seconds()))
		dots = r.Dots()
	}
	tw := textW(gtx, th, 13, theme.Bold, timer)
	waveW := vclip.WaveDots*dp(gtx, 6) - dp(gtx, 3)
	sw := dp(gtx, 12) + waveW + dp(gtx, 16) + tw + dp(gtx, 16)
	strip := image.Rect(x, 0, x+sw, b)
	rad := dp(gtx, 4)
	rrect(gtx, strip.Add(image.Pt(0, dp(gtx, 1))).Inset(-border1(gtx)), rad+border1(gtx), pal.shadow) // 0 1px 3px #00000014
	rrect(gtx, strip.Inset(-border1(gtx)), rad+border1(gtx), pal.low13)                               // 0 0 0 1px
	rrect(gtx, strip, rad, pal.bg)
	hi := pal.highlight
	if warn {
		hi = pal.warnDot
	}
	for i, v := range dots {
		h := dp(gtx, float32(vclip.DotHeight(v, 3)))
		dx := x + dp(gtx, 12) + i*dp(gtx, 6)
		col := pal.mid30
		if v >= 0 {
			col = hi
		}
		dy := (b - h) / 2
		rrect(gtx, image.Rect(dx, dy, dx+dp(gtx, 3), dy+h), maxInt(1, dp(gtx, 1.5)), col)
	}
	tcol := pal.fg
	if warn {
		tcol = pal.warnText
	}
	at(gtx, image.Pt(x+dp(gtx, 12)+waveW+dp(gtx, 16), 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, b, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, tcol, timer)
		})
	})
	x += sw + dp(gtx, 8) + dp(gtx, 4)
	// Stop recording (aria-label Attach recording)
	if n.f.m.ringOn(gtx, &n.attachBtn) {
		focusRing(gtx, cl, image.Rect(x, 0, x+b, b), dp(gtx, 4))
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(b, b))
		return n.attachBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rrect(gtx, image.Rect(0, 0, b, b), dp(gtx, 4), pal.highlight)
			at(gtx, image.Pt((b-dp(gtx, 20))/2, (b-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "check-large-bold", 20, rgb(0xffffff))
			})
			pointerCursor(gtx, image.Pt(b, b))
			Tooltip(gtx, th, &n.attachTip, n.attachBtn.Hovered(), image.Pt(b, b), Above, nsRimetoUser.T("Stop recording"), "")
			return layout.Dimensions{Size: image.Pt(b, b)}
		})
	})
	x += b
	return layout.Dimensions{Size: image.Pt(x, b)}
}

// nrTime is secondsToAudioTime (7foM): m:ss.
func nrTime(s float64) string {
	m, sec := int(s)/60, int(s)%60
	ss := strconv.Itoa(sec)
	if sec < 10 {
		ss = "0" + ss
	}
	return strconv.Itoa(m) + ":" + ss
}

// tile is PendingAudioFile: the 210px pillow (radius 4, the mid .3
// border, 12 in) with the 184px mini audio player, and RemoveFileButton's
// 22px circle over its top-right corner.
func (n *epNameRec) tile(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	pal := nrPalette(th)
	sz := image.Pt(dp(gtx, 210), dp(gtx, 60))
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 4)
	n.tileHov.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rrect(gtx, r, rad, pal.bg)
		stroke(gtx, r, rad, border1(gtx), pal.mid30)
		if f := n.uploadedFile(); f != nil {
			if row := n.playerRow(); row != nil && row.MediaTile(f, 1, false) {
				gtx.Constraints = layout.Exact(sz)
				row.LayoutMedia(gtx, richview.MediaSlot{File: f, Mini: true})
			} else {
				n.pendingPlayer(gtx, th, pal, r, int64(f.DurationMS), f.Waveform)
			}
		} else if n.clip != nil {
			var wave []int
			for _, s := range n.clip.WaveSamples {
				wave = append(wave, int(math.Round(s)))
			}
			n.pendingPlayer(gtx, th, pal, r, int64(math.Round(n.clip.Duration*1000)), wave)
		}
		return layout.Dimensions{Size: sz}
	})
	// RemoveFileButton, over the tile's top-right corner and so outside the
	// tile's Clickable (which clips what it holds to the tile): a spinner
	// while it uploads, the warning when it failed, "Remove audio clip"
	// hovered or focused (4dwn: onFocus shows it as onMouseEnter does). It
	// is laid out whenever there is a file, drawing nothing at rest, so the
	// Tab order reaches it as Slack's DOM always has it.
	d := dp(gtx, 22)
	focused := gtx.Focused(&n.removeBtn)
	mode := 0
	switch {
	case n.removeBtn.Hovered() || focused:
		mode = 3
	case n.uploading:
		mode = 1
	case n.upErr != nil:
		mode = 2
	case n.tileHov.Hovered():
		mode = 3
	}
	bp := image.Pt(sz.X-d+dp(gtx, 10), -dp(gtx, 10))
	if n.f.m.ringOn(gtx, &n.removeBtn) {
		focusRing(gtx, colorsOf(th), image.Rectangle{Min: bp, Max: bp.Add(image.Pt(d, d))}, d/2)
	}
	at(gtx, bp, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(d, d))
		return n.removeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			switch mode {
			case 0:
			case 1:
				rrect(gtx, image.Rect(0, 0, d, d), d/2, pal.bg)
				s := dp(gtx, 16)
				at(gtx, image.Pt((d-s)/2, (d-s)/2), func(gtx layout.Context) layout.Dimensions {
					InfiniteSpinner(gtx, s, true, th.C.FgMuted, th.C.Link)
					return layout.Dimensions{Size: image.Pt(s, s)}
				})
			case 2:
				rrect(gtx, image.Rect(0, 0, d, d), d/2, pal.bg)
				at(gtx, image.Pt((d-dp(gtx, 20))/2, (d-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "warning-filled", 20, th.C.Danger)
				})
			default:
				c := dp(gtx, 10)
				rrect(gtx, image.Rect(d/2-c, d/2-c, d/2+c, d/2+c), c, th.C.Tooltip)
				at(gtx, image.Pt((d-dp(gtx, 14))/2, (d-dp(gtx, 14))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "close", 14, th.C.TooltipText)
				})
			}
			if mode != 0 {
				pointerCursor(gtx, image.Pt(d, d))
			}
			Tooltip(gtx, th, &n.removeTip, mode != 0 && n.removeBtn.Hovered(), image.Pt(d, d), Above, nsDraftsFile.T("Remove audio clip"), "")
			return layout.Dimensions{Size: image.Pt(d, d)}
		})
	})
	return layout.Dimensions{Size: sz}
}

// uploadedFile is the tile's file as FileAudio plays it (the upload's, or
// the profile's), nil while it's still the recording's bytes.
func (n *epNameRec) uploadedFile() *richtext.File {
	if n.file == nil {
		return nil
	}
	f := richtext.FileOf(n.file)
	if f != nil && len(f.Waveform) == 0 && n.clip != nil {
		for _, s := range n.clip.WaveSamples {
			f.Waveform = append(f.Waveform, int(math.Round(s)))
		}
	}
	return f
}

// playerRow is the window's player row for the form's recording (the ui
// root's Videos), nil where there is none.
func (n *epNameRec) playerRow() *videoplayer.Row {
	v, ok := n.f.h.(interface {
		Videos() *videoplayer.Controller
	})
	if !ok {
		return nil
	}
	ctl := v.Videos()
	if ctl == nil {
		return nil
	}
	if n.row == nil || n.ctl != ctl {
		n.ctl, n.row = ctl, ctl.Row("edit-profile", "", "", nameRecRowHost{})
	}
	return n.row
}

// nameRecRowHost is the form player's RowHost: the mini player has no menu
// and opens nothing.
type nameRecRowHost struct{}

func (nameRecRowHost) OpenFile(*richtext.File)                                         {}
func (nameRecRowHost) FileAction(richview.FileAction, *richtext.File, image.Rectangle) {}

// pendingPlayer is PendingAudioMessageUpload's mini player while the
// recording uploads: the play button disabled (isPending), the waveform,
// the duration.
func (n *epNameRec) pendingPlayer(gtx layout.Context, th *theme.Theme, pal nrColors, r image.Rectangle, durMS int64, wave []int) {
	bs := dp(gtx, 34)
	x0 := r.Min.X + dp(gtx, 13)
	cy := r.Min.Y + (r.Dy()-bs)/2
	rrect(gtx, image.Rect(x0, cy, x0+bs, cy+bs), bs/2, pal.mid30)
	at(gtx, image.Pt(x0+(bs-dp(gtx, 20))/2, cy+(bs-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "play", 20, rgb(0xffffff))
	})
	dur := nrTime(math.Floor(float64(durMS) / 1000))
	tw := textW(gtx, th, 13, theme.Bold, dur)
	right := r.Max.X - dp(gtx, 13)
	sx := x0 + bs + dp(gtx, 12)
	sw := right - tw - dp(gtx, 4) - sx
	nd, _ := vclip.ScrubberDots(int(float32(sw) / maxF32(gtx.Metric.PxPerDp, 0.01)))
	samples := make([]float64, len(wave))
	for i, v := range wave {
		samples[i] = float64(v) / 100
	}
	data := vclip.Resample(samples, nd)
	for i := 0; i < nd && i < len(data); i++ {
		h := dp(gtx, float32(vclip.DotHeight(data[i], 6)))
		if math.IsNaN(data[i]) {
			h = dp(gtx, 6)
		}
		x := sx + i*dp(gtx, 6)
		rrect(gtx, image.Rect(x, cy+(bs-h)/2, x+dp(gtx, 3), cy+(bs-h)/2+h), maxInt(1, dp(gtx, 1.5)), pal.mid30)
	}
	at(gtx, image.Pt(right-tw, cy), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bs, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, pal.fg, dur)
		})
	})
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// ShowMicrophoneAccessError opens MicrophoneAccessErrorDialog (boot-shared
// AJKj): the desktop app's "Check your system preferences" (macOS) or
// "system settings", linked to the OS's microphone privacy page where
// there is one (ktNw mt), and a Close button.
func ShowMicrophoneAccessError(h Host) {
	src := "Check your <a>system settings</a> to make sure Slack has permission to access your microphone, and try again."
	if runtime.GOOS == "darwin" {
		src = "Check your <a>system preferences</a> to make sure Slack has permission to access your microphone, and try again."
	}
	parts := nsStoriesAV.RT(src)
	for i := range parts {
		parts[i].Text = strings.ReplaceAll(parts[i].Text, "Slack", AppName)
	}
	link := avperm.SettingsURL(avperm.Mic)
	if runtime.GOOS == "linux" {
		link = "" // ktNw: no page off macOS and Windows
	}
	var lc widget.Clickable
	closeBtn := &ModalButton{Label: nsStoriesAV.T("Close"), Kind: BtnOutline}
	m := NewModal(h, ModalOpts{
		Title:     nsStoriesAV.T("Can’t access microphone"),
		TitleWrap: true,
		Buttons:   []*ModalButton{closeBtn},
		AutoFocus: closeBtn,
		Body: func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			th := h.Theme()
			if lc.Clicked(gtx) && link != "" {
				_ = openurl.OpenSettings(link)
			}
			return micErrorBody(gtx, th, parts, link != "", &lc)
		},
	})
	h.ShowOverlay(m)
}

// micErrorBody lays the dialog's sentence out word by word, 15/22, the
// <a> words in the link colour over one clickable.
func micErrorBody(gtx layout.Context, th *theme.Theme, parts []i18n.Part, linked bool, lc *widget.Clickable) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	lh := dp(gtx, 22)
	space := textW(gtx, th, 15, theme.Regular, " ")
	x, y := 0, 0
	var linkBox image.Rectangle
	for _, p := range parts {
		isLink := linked && p.Tag == "a"
		col := cl.Fg
		if isLink {
			col = cl.Link
		}
		words := strings.Fields(p.Text)
		lead := strings.HasPrefix(p.Text, " ")
		for i, wd := range words {
			ww := textW(gtx, th, 15, theme.Regular, wd)
			if (i > 0 || lead) && x > 0 {
				x += space
			}
			if x > 0 && x+ww > w {
				x, y = 0, y+lh
			}
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, col, wd)
				})
			})
			if isLink {
				r := image.Rect(x, y, x+ww, y+lh)
				if linkBox.Empty() {
					linkBox = r
				} else {
					linkBox = linkBox.Union(r)
				}
			}
			x += ww
		}
		if strings.HasSuffix(p.Text, " ") && x > 0 {
			x += space
		}
	}
	if !linkBox.Empty() {
		at(gtx, linkBox.Min, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(linkBox.Size())
			return lc.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Dimensions{Size: linkBox.Size()}
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}
