package composer

import (
	"image"
	"image/color"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	gclip "gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// Audio clips: the composer's microphone button records a voice clip the
// way Slack's does (internal/clip has the recording and the file).
//
//   - The button (AudioComposerButton, boot-shared VIJ/): "Record audio
//     clip", the microphone SvgIcon, shown when InputContainer's
//     canPostAudioMessage holds (Qre6 hP: the team's clip prefs,
//     core.AudioClipsAllowed, and window.MediaRecorder, here a build that
//     can open a microphone). The video button stays hidden: Clack
//     doesn't record video clips yet.
//   - A click opens AudioPopover: a PopoverButton at "top-left" with
//     offsetX -64, offsetY 28 and allowanceX 4, so its footer's Stop
//     recording button sits on the microphone button. It doesn't close on
//     an outside click (shouldCloseOnOverlayClick false): the strip flashes
//     the focus ring for 1 s (p-audio_popover__strip--speedbump). Escape
//     and the × (Cancel recording) close it and drop the recording. While
//     it's open AudioPopoverOverlay veils the composer
//     (.p-audio_popover_overlay).
//   - The strip is AudioWaveformRecorder's 24 dots and the m:ss timer;
//     both turn red in the last 10 s, and at 5:00 the recording stops and
//     attaches itself. A failed microphone opens MicrophoneAccessErrorDialog.
//   - Stop recording (✓, autoFocus: Return and Space press it) attaches the
//     clip: the popover closes, the composer gets the focus back, and the
//     clip is a pending file (PendingAudioMessageUpload) uploading at once
//     (finishUpload: addAndUploadPendingFile with subtype slack_audio).

// clipRecorder is the clip buttons' window.MediaRecorder: this build can
// open a microphone. Tests replace it.
var clipRecorder = huddle.HasMicrophone

// ClipDevice, when set, replaces the microphone the recorder opens (tests:
// a synthetic tone; CLACK_CLIP_TONE=<hz> sets one up at start, for machines
// without a microphone).
var ClipDevice func() huddle.Device

// ClipStartSound plays the recorder's start sound (the ui root wires it to
// Slack's CDN sound, vclip.StartSound).
var ClipStartSound func()

// AppName is the app's name where Slack's strings name Slack (the ui root
// sets it).
var AppName = "Clack"

// clipTimeLimit, when set (tests), replaces the recording's 5:00 limit.
var clipTimeLimit time.Duration

func init() {
	if hz, _ := strconv.ParseFloat(os.Getenv("CLACK_CLIP_TONE"), 64); hz > 0 {
		ClipDevice = func() huddle.Device { return huddle.NewToneDevice(hz) }
		clipRecorder = func() bool { return true }
	}
}

// nsStoriesAV is the clip recorder's strings (stories_av).
var nsStoriesAV = i18n.NS("stories_av")

// clipHide is the clip buttons this composer leaves out: the video one
// always, the audio one unless audioClipOn.
func (c *Composer) clipHide() uint32 {
	h := uint32(1) << crVideo
	if !c.audioClipOn() {
		h |= 1 << crAudio
	}
	return h
}

// audioClipOn is enableAudioButton for this composer: MessagePaneInput's
// canPostAudioMessage (the edit composer and Block Kit's rich_text_input
// have no clip buttons).
func (c *Composer) audioClipOn() bool {
	return c.edit == nil && c.field == nil && c.api.a != nil && clipRecorder() && c.api.a.AudioClipsAllowed(c.ch)
}

// audioPopover is AudioPopover with its recorder (useAudioWaveformRecorder).
type audioPopover struct {
	c   *Composer
	rec *vclip.Recorder
	pop *popups.Popover

	attach, cancel      widget.Clickable
	attachTip, closeTip popups.TipState
	bump                time.Time // the speedbump's start
	done                bool      // attached (or nothing left to do)
	alerted             bool      // MicrophoneAccessErrorDialog shown
	// core is the session the popover opened in: its clip attaches (and
	// uploads) only while that is still the composer's
	core *core.App
	// unmounted: closed with its composer (another conversation, the pane
	// or view gone, the session ended), not by the user: AudioPopover
	// unmounts without its onClose, so the focus isn't given back
	unmounted bool
}

// livePops are the recorder popovers open in any composer (CloseClips).
var livePops struct {
	sync.Mutex
	m map[*audioPopover]struct{}
}

// CloseClips closes every open clip recorder and drops its recording, as
// their composers go with the session (the sign-in screen, another
// account): Slack's MessagePaneInput unmounts with the client, and
// useAudioWaveformRecorder's unmount stops the StreamManager (w.stop())
// with no recordingend, so nothing attaches or uploads, here or in the
// next session.
func CloseClips() {
	livePops.Lock()
	ps := make([]*audioPopover, 0, len(livePops.m))
	for p := range livePops.m {
		ps = append(ps, p)
	}
	livePops.Unlock()
	for _, p := range ps {
		p.unmount()
	}
}

// unmount closes the popover as its composer goes: the recording is
// dropped (closed's Cancel), and nothing gets the focus.
func (p *audioPopover) unmount() {
	p.unmounted = true
	p.pop.Dismiss()
}

// openAudioRecorder is AudioComposerButton's click: the popover at the
// button (anchor, window coordinates) and the microphone opening.
func (c *Composer) openAudioRecorder(anchor image.Rectangle) {
	if p := c.clipPop; p != nil && !p.pop.Dismissed() {
		return
	}
	agc := true // prefs.audio.enableAGC defaults on
	if h, ok := c.h.(interface{ HuddleAudioPrefs() (agc, ns bool) }); ok {
		agc, _ = h.HuddleAudioPrefs()
	}
	var dev huddle.Device
	if ClipDevice != nil {
		dev = ClipDevice()
	}
	p := &audioPopover{c: c, core: c.h.Core()}
	p.attachTip.Delay, p.closeTip.Delay = 300*time.Millisecond, 300*time.Millisecond
	p.rec = vclip.Start(vclip.Options{AGC: agc, Device: dev, PlayStart: ClipStartSound, OnChange: c.h.Invalidate, TimeLimit: clipTimeLimit})
	p.pop = popups.NewPopover(c.h, p.layout, popups.PopoverOpts{
		Anchor: anchor, Popover: "top-left", OffsetX: -64, OffsetY: 28, AllowanceX: 4,
		Bare: true, OnOverlayClick: p.speedbump, OnEnter: p.stop, OnClose: p.closed,
	})
	c.clipPop = p
	livePops.Lock()
	if livePops.m == nil {
		livePops.m = map[*audioPopover]struct{}{}
	}
	livePops.m[p] = struct{}{}
	livePops.Unlock()
	c.h.ShowOverlay(p.pop)
}

// ClipRecorder is the open recorder (tests), nil when the popover is closed.
func (c *Composer) ClipRecorder() *vclip.Recorder {
	if p := c.clipPop; p != nil && !p.pop.Dismissed() {
		return p.rec
	}
	return nil
}

// StopClip is what a click on the popover's Stop recording (✓) does
// (tests).
func (c *Composer) StopClip() {
	if p := c.clipPop; p != nil && !p.pop.Dismissed() {
		p.stop()
	}
}

// stop is the attach button's onClick (V → onAttach): before the file
// exists it stops the recording, and recordingend attaches it.
func (p *audioPopover) stop() {
	if p.rec.State() == vclip.Recording {
		go p.rec.Stop()
	}
}

// speedbump is a click on the popover's overlay (K: the target is
// .c-popover): the strip's focus ring for 1 s.
func (p *audioPopover) speedbump() {
	p.bump = time.Now()
	p.c.h.Invalidate()
}

// closeClip closes an open recorder popover without keeping the recording
// (the composer goes away: the hook unmounts).
func (c *Composer) closeClip() {
	if p := c.clipPop; p != nil {
		p.unmount()
	}
}

// closed is the popover's onClose: the recorder hook unmounts (w.stop(),
// nothing kept unless it attached) and AudioComposerButton gives the
// composer the focus back, unless the composer went with it.
func (p *audioPopover) closed() {
	livePops.Lock()
	delete(livePops.m, p)
	livePops.Unlock()
	if !p.done {
		p.rec.Cancel()
	}
	if p.c.clipPop == p {
		p.c.clipPop = nil
	}
	if !p.unmounted {
		p.c.Focus()
	}
}

// Popover geometry (dp): .p-audio_popover is 220 wide; the strip (padding
// 12, a 34px waveform) has margin-right 12; the footer (margin-top 8,
// padding-left 64) holds the 28px attach button; the 20px close button
// sits 10 above the content's top, 2 in from its right. The content box
// here starts at the close button's top.
const (
	apW, apOver       = 220, 10
	apStripW, apStrip = 208, 58
	apFootGap, apBtn  = 8, 28
	apFootPad         = 64
	apClose           = 20
	apH               = apOver + apStrip + apFootGap + apBtn
)

func (p *audioPopover) layout(gtx layout.Context) layout.Dimensions {
	c := p.c
	// gone with its composer, which wasn't drawn this frame (the shell lays
	// the views out before the overlays): AudioPopover is the composer's
	// child and unmounts with it when another conversation opens (Mod+K,
	// Alt+arrows, the Switch to Channel menu), its pane closes or the
	// session ends; the recording goes, and ✓ can't attach it to the
	// conversation that was
	if c.done || !c.drawnAt.Equal(gtx.Now) {
		p.unmount()
		return layout.Dimensions{}
	}
	th := c.h.Theme()
	switch p.rec.State() {
	case vclip.Stopped:
		if !p.done {
			p.done = true
			// never into another session: a composer outliving its
			// session (CloseClips missed it) drops the clip
			if f, ok := p.rec.File(); ok && c.h.Core() == p.core {
				c.attachClip(f)
			}
			p.pop.Dismiss() // onFileAttach: the popover closes
		}
	case vclip.Failed:
		if !p.alerted {
			p.alerted = true
			c.micAccessError()
		}
	}
	if p.attach.Clicked(gtx) {
		p.stop()
	}
	if p.cancel.Clicked(gtx) {
		p.pop.Dismiss() // z: onClose, then onCancel (closed)
	}
	pal := clipPalette(th)
	p.layoutOverlay(gtx, pal)

	dp := func(v float32) int { return gtx.Dp(unit.Dp(v)) }
	strip := image.Rect(0, dp(apOver), dp(apStripW), dp(apOver+apStrip))
	bump := !p.bump.IsZero() && gtx.Now.Sub(p.bump) < time.Second
	if bump {
		gtx.Execute(op.InvalidateCmd{At: p.bump.Add(time.Second)})
		rad := dp(8) // --sk_input_styles-radius
		fillRRect(gtx, strip.Inset(-dp(5)), rad+dp(5), pal.focus30)
		fillRRect(gtx, strip.Inset(-dp(1)), rad+dp(1), pal.focus)
		fillRRect(gtx, strip, rad, pal.bg)
	} else {
		rad := dp(4)
		fillRRect(gtx, strip.Add(image.Pt(0, dp(1))).Inset(-dp(1)), rad+dp(1), pal.shadow) // 0 1px 3px #00000014
		fillRRect(gtx, strip.Inset(-dp(1)), rad+dp(1), pal.low13)                          // 0 0 0 1px
		fillRRect(gtx, strip, rad, pal.bg)
	}
	warn := p.rec.Warning()
	// AudioWaveformRecorder: 24 dots, highlight mode data (a drawn dot is
	// blue, red in the last 10 s), minHeight 3
	dots := p.rec.Dots()
	hi := pal.highlight
	if warn {
		hi = pal.warnDot
	}
	top := strip.Min.Y + dp(12)
	for i, v := range dots {
		h := gtx.Dp(unit.Dp(float32(vclip.DotHeight(v, 3))))
		x := dp(12) + i*dp(6)
		col := pal.mid30
		if v >= 0 {
			col = hi
		}
		y := top + (dp(34)-h)/2
		fillRRect(gtx, image.Rect(x, y, x+dp(3), y+h), max(1, dp(1.5)), col)
	}
	tcol := pal.fg
	if warn {
		tcol = pal.warnText
	}
	tx := dp(12) + vclip.WaveDots*dp(6) - dp(3) + dp(16)
	offset(gtx, tx, top+(dp(34)-dp(18))/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(strip.Max.X-dp(12)-tx, dp(18))}
		return label(gtx, th.Shaper, textStyle{Size: 13, LineHeight: 18, Weight: theme.Bold, Color: tcol, MaxLines: 1},
			audioTime(math.Floor(p.rec.Elapsed().Seconds())))
	})

	// the footer: Stop recording
	ay := strip.Max.Y + dp(apFootGap)
	offset(gtx, dp(apFootPad), ay, func(gtx layout.Context) layout.Dimensions {
		s := dp(apBtn)
		gtx.Constraints = layout.Exact(image.Pt(s, s))
		return p.attach.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			fillRRect(gtx, image.Rect(0, 0, s, s), dp(4), pal.highlight)
			ic := dp(18)
			offset(gtx, (s-ic)/2, (s-ic)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("check-large-bold")(gtx, unit.Dp(18), rgb(0xffffff))
			})
			pointer.CursorPointer.Add(gtx.Ops)
			popups.Tooltip(gtx, th, &p.attachTip, p.attach.Hovered(), image.Pt(s, s), popups.Above, nsStoriesAV.T("Stop recording"), "")
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
	})

	// Cancel recording: a 20px circle in the primary foreground with the
	// background's ×, ringed in the background (box-shadow -4px 4px 0 0,
	// 0 0 0 4px at .5)
	cx := dp(apW) - dp(2) - dp(apClose)
	offset(gtx, cx, 0, func(gtx layout.Context) layout.Dimensions {
		s := dp(apClose)
		gtx.Constraints = layout.Exact(image.Pt(s, s))
		return p.cancel.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, s, s)
			fillRRect(gtx, r.Inset(-dp(4)), s/2+dp(4), withAlpha(pal.bg, pal.bg.A/2))
			fillRRect(gtx, r.Add(image.Pt(-dp(4), dp(4))), s/2, pal.bg)
			fillRRect(gtx, r, s/2, pal.fg)
			icons.Get("times-small")(gtx, unit.Dp(apClose), pal.bg)
			pointer.CursorPointer.Add(gtx.Ops)
			popups.Tooltip(gtx, th, &p.closeTip, p.cancel.Hovered(), image.Pt(s, s), popups.Above, nsStoriesAV.T("Cancel recording"), "")
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
	})
	return layout.Dimensions{Size: image.Pt(dp(apW), dp(apH))}
}

// layoutOverlay is AudioPopoverOverlay's .p-audio_popover_overlay: from the
// composer's bottom 300px up, the primary background from clear to .9 over
// the first 100px, pointer-events none. It's drawn in the popover's
// content, placed by the popover's last rectangle.
func (p *audioPopover) layoutOverlay(gtx layout.Context, pal clipColors) {
	at := p.pop.Rect().Min
	c := p.c
	if at == (image.Point{}) || c.boxSize == (image.Point{}) {
		return // the first frame: the popover isn't placed yet
	}
	bottom := c.Origin.Y + c.boxSize.Y
	top := bottom - gtx.Dp(300)
	r := image.Rect(c.Origin.X, top, c.Origin.X+c.boxSize.X, bottom).Sub(at)
	clear := withAlpha(pal.bg, 0)
	solid := withAlpha(pal.bg, uint8(math.Round(float64(pal.bg.A)*.9)))
	mid := r.Min.Y + gtx.Dp(100)
	func() {
		defer gclip.Rect(image.Rect(r.Min.X, r.Min.Y, r.Max.X, mid)).Push(gtx.Ops).Pop()
		paint.LinearGradientOp{Stop1: f32.Pt(0, float32(r.Min.Y)), Color1: clear, Stop2: f32.Pt(0, float32(mid)), Color2: solid}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	fillRect(gtx, image.Rect(r.Min.X, mid, r.Max.X, r.Max.Y), solid)
}

// audioTime is secondsToAudioTime (boot-shared 7foM): m:ss, or h:mm:ss.
func audioTime(s float64) string {
	h := int(s / 3600)
	m := int((s - 3600*float64(h)) / 60)
	sec := int(s - 3600*float64(h) - 60*float64(m))
	ss := strconv.Itoa(sec)
	if sec < 10 {
		ss = "0" + ss
	}
	if h > 0 {
		mm := strconv.Itoa(m)
		if m < 10 {
			mm = "0" + mm
		}
		return strconv.Itoa(h) + ":" + mm + ":" + ss
	}
	return strconv.Itoa(m) + ":" + ss
}

// clipColors are the recorder's and the pending clip's colours, from
// Slack's variables for the theme's mode.
type clipColors struct {
	bg, fg, low13, mid30, highlight, warnText, warnDot color.NRGBA
	focus, focus30, shadow                             color.NRGBA
}

func clipPalette(th *theme.Theme) clipColors {
	vars := css.SlackVars(th.Dark)
	sk := func(name string, a float64, def color.NRGBA) color.NRGBA {
		if v, ok := vars[name]; ok {
			if c, ok := parseCSSColor("rgb(" + v + ")"); ok {
				def = c
			}
		}
		def.A = uint8(math.Round(float64(def.A) * a))
		return def
	}
	dt := func(name string, def color.NRGBA) color.NRGBA {
		if c, ok := cssVarColor(vars, name); ok {
			return c
		}
		return def
	}
	p := clipColors{
		bg:        sk("--sk_primary_background", 1, th.C.Bg),
		fg:        sk("--sk_primary_foreground", 1, th.C.Fg),
		low13:     sk("--sk_foreground_low", .13, th.C.Fg),
		mid30:     sk("--sk_foreground_mid", .3, th.C.Fg),
		highlight: sk("--sk_highlight", 1, th.C.Link),
		warnText:  dt("--dt_color-content-imp", th.C.Danger),
		warnDot:   dt("--dt_color-base-inv-imp", th.C.Danger),
		// --sk_focused-shadow-color is "initial", so the fallback
		focus:  dt("--dt_color-otl-hgl-1", th.C.Link),
		shadow: argb(0x14000000),
	}
	p.focus30 = withAlpha(p.focus, uint8(math.Round(float64(p.focus.A)*.3)))
	return p
}

// micAccessError is MicrophoneAccessErrorDialog (boot-shared AJKj): the
// desktop app's "Check your system preferences" (macOS) or "system
// settings", linked to the OS's microphone privacy page where there is
// one (ktNw mt), and a Close button.
func (c *Composer) micAccessError() {
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
	closeBtn := &popups.ModalButton{Label: nsStoriesAV.T("Close"), Kind: popups.BtnOutline}
	m := popups.NewModal(c.h, popups.ModalOpts{
		Title:     nsStoriesAV.T("Can’t access microphone"),
		TitleWrap: true,
		Buttons:   []*popups.ModalButton{closeBtn},
		AutoFocus: closeBtn,
		Body: func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			th := c.h.Theme()
			if lc.Clicked(gtx) && link != "" {
				_ = openurl.OpenSettings(link)
			}
			st := textStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}
			var spans []richSpan
			linkAt := -1
			for _, pt := range parts {
				s := st
				if pt.Tag == "a" && link != "" {
					s.Color = th.C.Link
					linkAt = len(spans)
				}
				spans = append(spans, richSpan{text: pt.Text, st: s})
			}
			d := richLabel(gtx, th.Shaper, spans)
			if linkAt >= 0 {
				// the link's box, when it fits on the first line (it does
				// at the dialog's width in every bundled locale's text)
				pre := 0
				for _, sp := range spans[:linkAt] {
					pre += clipMeasure(gtx, th, sp.st, sp.text)
				}
				lw := clipMeasure(gtx, th, spans[linkAt].st, spans[linkAt].text)
				if pre+lw <= gtx.Constraints.Max.X {
					offset(gtx, pre, 0, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Exact(image.Pt(lw, gtx.Sp(22)))
						return lc.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							pointer.CursorPointer.Add(gtx.Ops)
							return layout.Dimensions{Size: gtx.Constraints.Min}
						})
					})
				}
			}
			return d
		},
	})
	c.h.ShowOverlay(m)
}

// clipMeasure is s's width set in st on one line.
func clipMeasure(gtx layout.Context, th *theme.Theme, st textStyle, s string) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Sp(st.LineHeight))}
	d := label(g, th.Shaper, st, s)
	m.Stop()
	return d.Size.X
}

// attachClip is onFileAttach then finishUpload: the clip joins the
// attachments as a pending file and uploads at once.
func (c *Composer) attachClip(f vclip.File) {
	c.att.addClip(f)
}

// clipMeta is what a pending clip carries besides its bytes: Slack's File
// fields subtype, audio_wave_samples and duration.
type clipMeta struct {
	subtype  string
	mime     string
	samples  []float64
	duration float64 // seconds
}

// addClip attaches a recorded clip (addAndUploadPendingFile with the
// clip's File). Slack's eager upload adds it whatever the draft holds
// (use_eager_upload has no count check): with 10 files already the clip
// is the 11th, and the draft can't be sent until one goes
// (isComposerDraftSendable's tooManyFiles, Composer.tooManyFiles), so a
// recording is never lost to the limit.
func (a *attachments) addClip(f vclip.File) bool {
	a.mu.Lock()
	err := attCheckAdd(0, a.total, int64(len(f.Data)))
	upload := a.h.Core() != nil
	var c *attChip
	if err == nil {
		c = &attChip{id: int(attNextID.Add(1)), name: f.Name, title: f.Name, data: f.Data, kind: attKindOf(f.Name, f.Data),
			pf: pendingClip(f), preparing: upload, ptotal: int64(len(f.Data)),
			clip: &clipMeta{subtype: f.Subtype, mime: f.Mime, samples: f.WaveSamples, duration: f.Duration}}
		a.chips = append(a.chips, c)
		a.total += int64(len(f.Data))
	}
	a.mu.Unlock()
	if err != nil {
		a.toast(attRejectText(f.Name, err))
		return false
	}
	go a.prepare(c)
	a.h.Invalidate()
	pendingChanged()
	a.notify()
	return true
}

// pendingClip is the clip's eager upload as the send sees it.
func pendingClip(f vclip.File) *core.PendingFile {
	pf := core.NewPendingFile(f.Name, f.Name, f.Data)
	pf.Subtype = f.Subtype
	return pf
}

// drawClipCard is a pending clip's tile: a pillow (1px border, radius 12,
// 12 in) holding the 184px flat audio message player in its mini style
// with the clip's waveform and duration. While the clip is still the
// recording's bytes it's PendingAudioMessageUpload, whose play button is
// disabled (isPending); once uploaded, PendingFile's renderContent draws
// FileAudio for the file (fileIsPlayableAudio), which plays: the window's
// player (videoplayer, the mini style: the play button, the scrubber and
// the time, at the natural rate).
func (a *attachments) drawClipCard(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	pal := clipPalette(th)
	fillRRect(gtx, r, rad, th.C.Bg)
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), th.C.Border)
	if f := a.uploadedClip(v.c); f != nil {
		if row := a.clipRow(); row != nil && row.MediaTile(f, 1, false) {
			defer op.Offset(r.Min).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(r.Size())
			row.LayoutMedia(g, richview.MediaSlot{File: f, Mini: true})
			return
		}
	}
	dp := func(f float32) int { return gtx.Dp(unit.Dp(f)) }
	bs := dp(34)
	x0 := r.Min.X + dp(12)
	cy := r.Min.Y + (r.Dy()-bs)/2
	fillRRect(gtx, image.Rect(x0, cy, x0+bs, cy+bs), bs/2, pal.mid30) // c-audio_player_play_button--disabled
	offset(gtx, x0+(bs-dp(20))/2, cy+(bs-dp(20))/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("play")(gtx, unit.Dp(20), rgb(0xffffff))
	})
	var dur string
	var samples []float64
	if m := v.c.clip; m != nil {
		dur = audioTime(math.Floor(m.duration))
		samples = make([]float64, len(m.samples))
		for i, s := range m.samples {
			samples[i] = s / 100
		}
	}
	ts := textStyle{Size: 13, LineHeight: 18, Weight: theme.Bold, Color: pal.fg, MaxLines: 1}
	tw := clipMeasure(gtx, th, ts, dur)
	right := min(r.Max.X-dp(12), x0+dp(184))
	sx := x0 + bs + dp(12)
	sw := right - tw - dp(4) - sx
	n, _ := vclip.ScrubberDots(int(float32(sw) / max(gtx.Metric.PxPerDp, 0.01))) // in CSS px
	data := vclip.Resample(samples, n)
	for i := 0; i < n && i < len(data); i++ {
		h := dp(float32(vclip.DotHeight(data[i], 6)))
		if math.IsNaN(data[i]) {
			h = dp(6)
		}
		x := sx + i*dp(6)
		fillRRect(gtx, image.Rect(x, cy+(bs-h)/2, x+dp(3), cy+(bs-h)/2+h), max(1, dp(1.5)), pal.mid30)
	}
	offset(gtx, right-tw, cy+(bs-dp(18))/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw+dp(2), dp(18))}
		return label(gtx, th.Shaper, ts, dur)
	})
}

// uploadedClip is c's file as Slack processed the upload (files.info's
// object), nil until then. Its waveform is the server's, else the
// recording's (the samples the clip went up with).
func (a *attachments) uploadedClip(c *attChip) *richtext.File {
	a.mu.Lock()
	var sf *slack.File
	if c.prepared != nil {
		sf = c.prepared.File
	}
	a.mu.Unlock()
	f := richtext.FileOf(sf)
	if f == nil {
		return nil
	}
	if len(f.Waveform) == 0 && c.clip != nil {
		for _, s := range c.clip.samples {
			f.Waveform = append(f.Waveform, int(math.Round(s)))
		}
	}
	return f
}

// clipRow is the window's player row for the composer's uploaded clips
// (the ui root's Videos), nil where there is none.
func (a *attachments) clipRow() *videoplayer.Row {
	v, ok := a.h.(interface {
		Videos() *videoplayer.Controller
	})
	if !ok {
		return nil
	}
	ctl := v.Videos()
	if ctl == nil {
		return nil
	}
	if a.clipPlayer == nil || a.clipCtl != ctl {
		a.clipCtl, a.clipPlayer = ctl, ctl.Row("composer", "", "", clipRowHost{})
	}
	return a.clipPlayer
}

// clipRowHost is the uploaded clip's RowHost: the mini player has no menu
// and opens nothing.
type clipRowHost struct{}

func (clipRowHost) OpenFile(*richtext.File)                                         {}
func (clipRowHost) FileAction(richview.FileAction, *richtext.File, image.Rectangle) {}
