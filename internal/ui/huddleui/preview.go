package huddleui

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The join preview: Preferences > Audio & video "Show audio and video
// preview" (huddles_show_join_preview, unset is off).
//
// Slack's startHuddleWithConfirmation runs computeShouldShowJoinPreview:
// not from the invite window, the pref on, and a huddle other than the
// one you're in (or prejoined). Then showHuddlesJoinPreview opens the
// invite-with-preview window (openHuddleInviteWithPreviewUsingSideEffect,
// HuddleInviteWindowWithPreviewContent) before startHuddle; its Join runs
// startHuddle with fromJoinPreview and the preview's mic and camera state.
// The window is 524×760 ("Slack - Huddle Preview",
// getInitialHuddleInvitePreviewWindowDimensions with the pref on) and its
// CSS module (qki8, client-boot-styles) is:
//
//   - .huddleInviteWindow: a column, 8px gap, 0 8px padding; the
//     colorWrapper (base-inv-pry, 12px corners) then the 106px footer.
//   - .titleBar: 95px, padding 12px 12px 24px, an x_small avatar and 15/22
//     text in content-inv-pry, two lines at most: "Start huddle in
//     **#channel**", "Join huddle in **#channel**. **X** is already
//     there.", "Start huddle with **Name**" (useGetTitlebarText).
//   - .content: surf-inv-pry, a 1px otl-ter border, 12px corners, padding
//     24px 20px, 24px gap, centered; .contentBody is a square of
//     min(width, height-60) with the video (object-fit cover, mirrored,
//     12px corners) or, camera off, your avatar filling it.
//   - the mic and camera toggles (LtVx, tewJ): 48px circles 12px apart,
//     16px off the video's bottom; on, base-pry with a gray-70 icon; off,
//     base-modal with a 1px gray-30 border.
//   - talking (the level > 0 with the mic on): a 3px white outline with a
//     white glow around the video, and a white 35×27 pill with the mic at
//     its bottom left, 12px in.
//   - .deviceSelectionContainer: the microphone, speaker and camera
//     pickers in a row, 8px apart: 34px buttons, 7px corners, otl-ter
//     border on theme-surf-ter, an icon and the device's label, bold
//     content-sec; each opens its device menu above it.
//   - the footer: Cancel (close) and Start Huddle / Join (headphones,
//     the join style), labelled, 24px apart (the invite window's
//     p-huddle_invite_window_content__buttons--with_pending_invites).

const (
	previewW = 524
	previewH = 760
)

// pendingJoin is what the preview's Join hands to the session.
type pendingJoin struct {
	channel string
	muted   bool
	camera  bool
}

// previewWindow runs the preview's own Gio loop.
type previewWindow struct {
	c       *Controller
	channel string
	entry   string
	th      *theme.Theme
	p       *huddle.Preview

	mu     sync.Mutex
	w      *app.Window
	closed bool
	micOn  bool
	camOn  bool
	menu   string // the open device menu: "mic", "out", "cam" or ""
	cl     previewClicks
	vid    videoOps
	devs   map[string][]huddle.AudioDevice
	joined bool
	fx     huddle.VideoEffect // the camera's background effect
}

type previewClicks struct {
	mic, cam, cancel, join widget.Clickable
	pick                   map[string]*widget.Clickable // "mic", "out", "cam"
	item                   map[string]*widget.Clickable // menu rows by kind+id
}

func (k *previewClicks) get(m *map[string]*widget.Clickable, key string) *widget.Clickable {
	if *m == nil {
		*m = map[string]*widget.Clickable{}
	}
	b := (*m)[key]
	if b == nil {
		b = new(widget.Clickable)
		(*m)[key] = b
	}
	return b
}

// shouldPreview is computeShouldShowJoinPreview for a start or join click.
// computeShouldShowJoinPreview (client-boot e5et): never from the invite
// window or without huddles_preview_v2; a microphone and a camera that
// both lack the OS's permission force it (arePermissionsGrantedForInputDevices,
// kLPq gC); else the pref decides.
func (c *Controller) shouldPreview(channel, entry string) bool {
	if c.NoWindow || entry == EntryInviteWindow {
		return false
	}
	app := c.h.Core()
	if app == nil || !c.on(ExpPreviewV2) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.s != nil && c.s.Channel() == channel || c.joining == channel {
		return false
	}
	other := c.pwin == nil || c.pwin.channel != channel
	if !permissionsGranted() && other {
		return true // "Forcing display of join preview due to device permission problems"
	}
	return other && app.PrefBool("huddles_show_join_preview", false)
}

// permissionsGranted is arePermissionsGrantedForInputDevices: the
// microphone or the camera may be captured (where the OS doesn't ask,
// both are).
var permissionsGranted = func() bool {
	return avperm.Check(avperm.Mic) == avperm.Authorized || avperm.Check(avperm.Camera) == avperm.Authorized
}

// openPreview shows the join preview for channel; Join runs start.
func (c *Controller) openPreview(channel, entry string) {
	c.mu.Lock()
	old := c.pwin
	w := &previewWindow{c: c, channel: channel, entry: entry, fx: c.videoEffect()}
	app := c.h.Core()
	w.micOn = app == nil || !app.PrefBool("huddles_mute_by_default", false)
	c.pwin = w
	c.mu.Unlock()
	if old != nil {
		old.close()
	}
	o := huddle.Options{Logf: func(string, ...any) {}, OnChange: w.invalidate, VideoEffect: w.fx}
	if c.Tweak != nil {
		c.Tweak(&o)
	}
	w.p = huddle.StartPreview(o, false)
	w.loadDevices()
	go w.run()
}

// PreviewOpen reports whether the join preview is showing (tests).
func (c *Controller) PreviewOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pwin != nil
}

func (w *previewWindow) loadDevices() {
	devs := map[string][]huddle.AudioDevice{}
	if l, err := huddle.AudioDevices(true); err == nil {
		devs["mic"] = l
	}
	if l, err := huddle.AudioDevices(false); err == nil {
		devs["out"] = l
	}
	w.mu.Lock()
	w.devs = devs
	w.mu.Unlock()
}

func (w *previewWindow) effect() huddle.VideoEffect {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fx
}

func (w *previewWindow) invalidate() {
	w.mu.Lock()
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Invalidate() // this window's own loop
	}
}

func (w *previewWindow) close() {
	w.mu.Lock()
	w.closed = true
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Perform(system.ActionClose)
	}
}

func (w *previewWindow) run() {
	ww := new(app.Window)
	ww.Option(app.Title(AppName+" - Huddle Preview"), app.Size(unit.Dp(previewW), unit.Dp(previewH)),
		app.MinSize(unit.Dp(previewW), unit.Dp(480)))
	main := w.c.h.Theme()
	dark := main != nil && main.Dark
	th := theme.Bare(dark)
	sh := theme.NewShaperFor(dark)
	th.SetShapers(sh, sh)
	if main != nil {
		th.Images, th.EmojiStyle, th.Tokens = main.Images, main.EmojiStyle, main.Tokens
	}
	w.mu.Lock()
	w.th = th
	w.w = ww
	closed := w.closed
	w.mu.Unlock()
	if closed {
		ww.Perform(system.ActionClose)
	}
	var ops op.Ops
	for {
		switch e := ww.Event().(type) {
		case app.DestroyEvent:
			w.p.Close()
			w.c.mu.Lock()
			if w.c.pwin == w {
				w.c.pwin = nil
			}
			w.c.mu.Unlock()
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			w.th.SetTextScale(gtx.Metric.PxPerSp) // the UI font's tracking
			w.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// previewView is what the preview draws (a snapshot).
type previewView struct {
	Title     []span
	Avatars   []string // the title's faces
	Self      string
	JoinLabel string
	MicOn     bool
	MicBad    bool // denied or no microphone: the red badge, the mic picker off
	CamOn     bool
	CamBad    bool // no camera backend or denied
	Talking   bool
	Frame     *image.RGBA
	FrameSeq  uint64
	Labels    [3]string // microphone, speaker, camera
	Menu      string
	MenuItems []previewItem
}

type previewItem struct {
	Key, Label string
	Checked    bool
}

// view snapshots the window for drawing.
func (w *previewWindow) view() previewView {
	c := w.c
	w.mu.Lock()
	v := previewView{MicOn: w.micOn, CamOn: w.camOn, Menu: w.menu}
	devs, fx := w.devs, w.fx
	w.mu.Unlock()
	v.Self = c.selfID()
	v.Title, v.Avatars, v.JoinLabel = c.previewTitle(w.channel)
	if p := w.p; p != nil {
		if p.VideoEffect() != fx {
			p.SetVideoEffect(fx)
		}
		st, _ := p.Mic()
		v.MicBad = st.Problem()
		v.Talking = v.MicOn && p.Talking()
		if v.CamOn {
			f := p.Frame()
			v.Frame, v.FrameSeq = f.Img, f.Seq
		}
		cs := p.Camera().State
		v.CamBad = !cameraSupported() || cs == huddle.CameraDenied || cs == huddle.CameraUnsupported
	} else {
		v.CamBad = !cameraSupported()
	}
	micSel, outSel := huddle.SelectedAudioDevices()
	v.Labels[0] = deviceLabel(devs["mic"], micSel)
	v.Labels[1] = deviceLabel(devs["out"], outSel)
	v.Labels[2] = "Default"
	if v.CamBad {
		v.Labels[2] = nsHuddles.T("No cameras found")
	}
	switch v.Menu {
	case "mic", "out":
		sel := micSel
		if v.Menu == "out" {
			sel = outSel
		}
		v.MenuItems = append(v.MenuItems, previewItem{Key: "", Label: "System default", Checked: sel == ""})
		for _, d := range devs[v.Menu] {
			v.MenuItems = append(v.MenuItems, previewItem{Key: d.ID, Label: d.Name, Checked: sel == d.ID})
		}
	case "cam":
		v.MenuItems = []previewItem{{Key: "", Label: "Default", Checked: true}}
	}
	return v
}

func deviceLabel(devs []huddle.AudioDevice, sel string) string {
	for _, d := range devs {
		if (sel == "" && d.Default) || (sel != "" && d.ID == sel) {
			return d.Name
		}
	}
	return "Default"
}

// previewTitle is useGetTitlebarText: start or join in a channel, start
// with a person in a DM; Slack's getAlreadyThereText names who's there.
func (c *Controller) previewTitle(ch string) (title []span, faces []string, join string) {
	self := c.selfID()
	var there []string
	if r, ok := c.Live(ch); ok {
		for _, u := range r.Participants {
			if u != self {
				there = append(there, u)
			}
		}
	}
	join = nsHuddles.T("Start Huddle")
	if len(there) > 0 {
		join = nsHuddles.T("Join")
	}
	kind := ""
	var dmUser string
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(ch); ok {
			kind, dmUser = conv.Kind, conv.User
		}
	}
	switch kind {
	case "im", "mpim":
		title = boldSpans("Start huddle with {dmPersons}", "dmPersons", c.channelTitle(ch))
		if dmUser != "" {
			faces = []string{dmUser}
		}
	default:
		// the channel is InlineChannelEntity, boldIcon (channel_entity.go)
		place := func(src string) []span {
			if e, ok := c.channelEnt(ch); ok {
				return entSpans(boldSpans(src, "channelName", entMark), e)
			}
			return boldSpans(src, "channelName", c.channelTitle(ch))
		}
		if len(there) == 0 {
			title = place("Start huddle in {channelName}")
		} else {
			title = append(place("Join huddle in {channelName}"), span{Text: ". "})
			names := make([]string, 0, 2)
			for _, u := range there[:min(2, len(there))] {
				names = append(names, c.displayName(u))
			}
			second := ""
			if len(names) > 1 {
				second = names[1]
			}
			// getHuddleParticipants' plain sentence, the names bold
			title = append(title, boldSpans("{participants, plural, =0 {{firstParticipant} is} =1 {{firstParticipant} and {secondParticipant} are} other {{firstParticipant} and # others are}} already there.",
				"participants", len(there)-1, "firstParticipant", names[0], "secondParticipant", second)...)
			faces = there[:min(3, len(there))]
		}
	}
	return title, faces, join
}

func (w *previewWindow) layout(gtx C) {
	c := w.c
	cl := &w.cl
	if cl.cancel.Clicked(gtx) {
		w.close()
	}
	if cl.mic.Clicked(gtx) {
		w.mu.Lock()
		w.micOn = !w.micOn
		w.mu.Unlock()
	}
	if cl.cam.Clicked(gtx) && cameraSupported() {
		w.mu.Lock()
		w.camOn = !w.camOn
		on := w.camOn
		w.mu.Unlock()
		w.p.SetCamera(on)
	}
	for _, k := range []string{"mic", "out", "cam"} {
		if cl.get(&cl.pick, k).Clicked(gtx) {
			w.mu.Lock()
			if w.menu == k {
				w.menu = ""
			} else {
				w.menu = k
			}
			w.mu.Unlock()
		}
	}
	v := w.view()
	for _, it := range v.MenuItems {
		if cl.get(&cl.item, v.Menu+"/"+it.Key).Clicked(gtx) {
			mic, out := huddle.SelectedAudioDevices()
			switch v.Menu {
			case "mic":
				mic = it.Key
			case "out":
				out = it.Key
			}
			huddle.SelectAudioDevices(mic, out)
			w.mu.Lock()
			w.menu = ""
			w.mu.Unlock()
			if v.Menu == "mic" { // reopen the preview's microphone on it
				old := w.p
				w.mu.Lock()
				cam := w.camOn
				w.mu.Unlock()
				o := huddle.Options{Logf: func(string, ...any) {}, OnChange: w.invalidate, VideoEffect: w.effect()}
				if c.Tweak != nil {
					c.Tweak(&o)
				}
				w.p = huddle.StartPreview(o, cam)
				old.Close()
			}
			v = w.view()
		}
	}
	if cl.join.Clicked(gtx) {
		w.join(v.MicBad)
	}
	layoutPreview(gtx, w.th, c, v, cl, &w.vid)
}

// join is the preview's Join (or Start Huddle): startHuddle with the
// preview's mic and camera (fromJoinPreview, usePreviewDeviceState).
func (w *previewWindow) join(micBad bool) {
	c := w.c
	w.mu.Lock()
	already := w.joined
	w.joined = true
	pj := pendingJoin{channel: w.channel, muted: !w.micOn || micBad, camera: w.camOn}
	w.mu.Unlock()
	if already {
		return
	}
	c.mu.Lock()
	c.pending = &pj
	c.mu.Unlock()
	w.close()
	ch, entry := w.channel, w.entry
	c.post(func() { c.begin(ch, entry) })
}

// previewInk is the preview's tokens beyond the invite window's.
type previewInk struct {
	invInk
	outline, surfTer, modal, active, activeInk, gray30 color.NRGBA
}

func previewInkFor(th *theme.Theme) previewInk {
	k := previewInk{invInk: invInkFor(th)}
	dark := th != nil && th.Dark
	if dark {
		k.outline = rgba(232, 232, 232, .13)
		k.surfTer = rgba(239, 225, 245, .1)
		k.modal = rgba(26, 29, 33, 1)
		k.active = rgba(248, 248, 248, 1)
	} else {
		k.outline = rgba(29, 28, 29, .13)
		k.surfTer = rgba(57, 6, 58, .08)
		k.modal = rgba(255, 255, 255, 1)
		k.active = rgba(255, 255, 255, 1)
	}
	k.activeInk = rgba(97, 96, 97, 1) // gray-70
	k.gray30 = rgba(221, 221, 221, 1)
	if dark {
		k.modal = rgba(26, 29, 33, 1)
	}
	if th != nil {
		t := th.Tokens
		theme.SetColor(t, &k.outline, "var(--dt_color-otl-ter)")
		theme.SetColor(t, &k.surfTer, "var(--dt_color-theme-surf-ter)")
		theme.SetColor(t, &k.modal, "var(--dt_color-base-modal)")
	}
	return k
}

// layoutPreview draws the preview window's content in gtx.Constraints.Max.
func layoutPreview(gtx C, th *theme.Theme, c *Controller, v previewView, cl *previewClicks, vid *videoOps) {
	sz := gtx.Constraints.Max
	k := previewInkFor(th)
	theme.Fill(gtx, image.Rectangle{Max: sz}, k.bg)
	dp := gtx.Dp
	pad, gap := dp(8), dp(8)
	footH := dp(106)
	wrap := image.Rect(pad, 0, sz.X-pad, sz.Y-footH-gap)
	theme.RRect(gtx, wrap, dp(12), k.bg)

	// the title bar
	tb := image.Rect(wrap.Min.X, wrap.Min.Y, wrap.Max.X, wrap.Min.Y+dp(95))
	tx := tb.Min.X + dp(12)
	ty := tb.Min.Y + dp(12)
	av := dp(24) // x_small
	textW := tb.Dx() - 2*dp(12)
	if len(v.Avatars) > 0 {
		n := len(v.Avatars)
		facesW := av + (n-1)*(av-dp(8)) // overlapped facepile
		lines := wrapSpans(gtx, th, 15, v.Title, textW-facesW-dp(8))
		w := facesW + dp(8) + spansWidth(gtx, th, lines)
		x0 := tb.Min.X + (tb.Dx()-w)/2
		for i := n - 1; i >= 0; i-- {
			u := v.Avatars[i]
			at(gtx, image.Pt(x0+i*(av-dp(8)), ty+dp(2)), image.Pt(av, av), func(gtx C) D {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(av, av)}.Inset(-dp(1)), dp(6), k.bg)
				avatar(gtx, th, c.h.Images(), c.avatarURL(u, av), image.Pt(av, av), dp(5))
				return D{Size: image.Pt(av, av)}
			})
		}
		tx = x0 + facesW + dp(8)
		drawSpans(gtx, th, lines[:min(2, len(lines))], image.Pt(tx, ty), k.text, k.bg)
	} else {
		lines := wrapSpans(gtx, th, 15, v.Title, textW)
		w := spansWidth(gtx, th, lines)
		drawSpans(gtx, th, lines[:min(2, len(lines))], image.Pt(tb.Min.X+(tb.Dx()-w)/2, ty), k.text, k.bg)
	}

	// the content panel
	ct := image.Rect(wrap.Min.X, tb.Max.Y, wrap.Max.X, wrap.Max.Y)
	theme.RRect(gtx, ct, dp(12), k.outline)
	theme.RRect(gtx, ct.Inset(max(1, dp(1))), dp(12)-1, k.bg) // surf-inv-pry is translucent
	theme.RRect(gtx, ct.Inset(max(1, dp(1))), dp(12)-1, k.btn)
	inner := image.Rect(ct.Min.X+dp(20), ct.Min.Y+dp(24), ct.Max.X-dp(20), ct.Max.Y-dp(24))
	side := max(0, min(inner.Dx(), inner.Dy()-dp(60)))
	devH := dp(36)
	blockH := side + dp(24) + devH
	by := inner.Min.Y + (inner.Dy()-blockH)/2
	video := image.Rect(inner.Min.X+(inner.Dx()-side)/2, by, inner.Min.X+(inner.Dx()-side)/2+side, by+side)
	rad := dp(12)
	if v.Talking {
		for i, a := range []uint8{0x18, 0x30, 0x50} { // the 8px 5px white glow
			theme.RRect(gtx, video.Inset(-dp(3)-dp(unit.Dp(8-3*i))), rad+dp(8), color.NRGBA{R: 255, G: 255, B: 255, A: a})
		}
		theme.RRect(gtx, video.Inset(-dp(3)), rad+dp(3), color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	}
	func() {
		defer op.Offset(video.Min).Push(gtx.Ops).Pop()
		vs := video.Size()
		if v.CamOn && v.Frame != nil {
			cover(gtx, vid.frame("self", v.Frame, v.FrameSeq), vs, rad)
		} else {
			avatar(gtx, th, c.h.Images(), c.avatarURL(v.Self, side), vs, rad)
			theme.Border(gtx, image.Rectangle{Max: vs}, rad, max(1, dp(1)), k.outline)
		}
		// the toggles, 16px off the bottom
		bd := dp(48)
		bx := (vs.X - (2*bd + dp(12))) / 2
		byy := vs.Y - dp(16) - bd
		toggle := func(cb *widget.Clickable, x int, on, bad bool, onIcon, offIcon string) {
			at(gtx, image.Pt(x, byy), image.Pt(bd, bd), func(gtx C) D {
				return button(gtx, cb, image.Pt(bd, bd), func(gtx C, hov bool) {
					ctr := image.Pt(bd/2, bd/2)
					if on && !bad {
						theme.Circle(gtx, ctr, bd, k.active)
						return
					}
					theme.Circle(gtx, ctr, bd, k.gray30)
					theme.Circle(gtx, ctr, bd-2*max(1, dp(1)), k.modal)
				}, func(gtx C) D {
					col, name := k.activeInk, onIcon
					if !on || bad {
						col, name = k.text, offIcon
						if bad {
							col = k.gray30
						}
					}
					iconAt(gtx, image.Pt(bd, bd), name, 20, col)
					if bad { // the warning badge
						b := dp(16)
						at(gtx, image.Pt(bd-b+dp(2), -dp(2)), image.Pt(b, b), func(gtx C) D {
							theme.Circle(gtx, image.Pt(b/2, b/2), b, hudWarn)
							iconAt(gtx, image.Pt(b, b), "warning", 10, hudWhite)
							return D{Size: image.Pt(b, b)}
						})
					}
					return D{Size: image.Pt(bd, bd)}
				})
			})
		}
		toggle(&cl.mic, bx, v.MicOn, v.MicBad, "mic", "mic-off")
		toggle(&cl.cam, bx+bd+dp(12), v.CamOn, v.CamBad, "video", "video-off")
		if v.Talking { // the talking pill, bottom left
			ps := image.Pt(dp(35), dp(27))
			at(gtx, image.Pt(dp(12), vs.Y-dp(12)-ps.Y), ps, func(gtx C) D {
				theme.RRect(gtx, image.Rectangle{Max: ps}, ps.Y/2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}) // 30px radius, clamped
				iconAt(gtx, ps, "mic", 16, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff})
				return D{Size: ps}
			})
		}
	}()

	// the device pickers
	dw := max(side, min(inner.Dx(), dp(320)))
	dr := image.Rect(inner.Min.X+(inner.Dx()-dw)/2, video.Max.Y+dp(24), inner.Min.X+(inner.Dx()-dw)/2+dw, video.Max.Y+dp(24)+devH)
	colW := (dr.Dx() - 2*dp(8)) / 3
	kinds := [3]struct{ key, icon string }{{"mic", "microphone"}, {"out", "sound"}, {"cam", "video"}}
	var menuAnchor image.Rectangle
	for i, kd := range kinds {
		r := image.Rect(dr.Min.X+i*(colW+dp(8)), dr.Min.Y, dr.Min.X+i*(colW+dp(8))+colW, dr.Max.Y)
		if kd.key == v.Menu {
			menuAnchor = r
		}
		disabled := (kd.key == "mic" && v.MicBad) || (kd.key == "cam" && v.CamBad)
		label := v.Labels[i]
		ic := kd.icon
		at(gtx, r.Min.Add(image.Pt(0, dp(1))), image.Pt(r.Dx(), dp(34)), func(gtx C) D {
			bs := image.Pt(r.Dx(), dp(34))
			draw := func(gtx C, hov bool) {
				rr := image.Rectangle{Max: bs}
				theme.RRect(gtx, rr, dp(7), k.outline)
				theme.RRect(gtx, rr.Inset(max(1, dp(1))), dp(7)-1, k.bg)
				theme.RRect(gtx, rr.Inset(max(1, dp(1))), dp(7)-1, k.surfTer)
			}
			body := func(gtx C) D {
				col := k.sec
				if disabled {
					col.A /= 2
				}
				is := dp(16)
				ls := labelSize(gtx, th, 13, theme.Bold, label)
				maxL := bs.X - 2*dp(11) - is - dp(4)
				txt := label
				if ls.X > maxL {
					txt = ellipsize(gtx, th, 13, theme.Bold, label, maxL)
					ls = labelSize(gtx, th, 13, theme.Bold, txt)
				}
				w := is + dp(4) + ls.X
				x := (bs.X - w) / 2
				at(gtx, image.Pt(x, (bs.Y-is)/2), image.Pt(is, is), func(gtx C) D { return icons.GetSlack(ic)(gtx, 16, col) })
				at(gtx, image.Pt(x+is+dp(4), (bs.Y-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, col, txt) })
				return D{Size: bs}
			}
			if disabled {
				draw(gtx, false)
				return body(gtx)
			}
			return button(gtx, cl.get(&cl.pick, kd.key), bs, draw, body)
		})
	}

	// the footer: Cancel and Start Huddle / Join
	type fbtn struct {
		cl          *widget.Clickable
		icon, label string
		join        bool
	}
	btns := []fbtn{{&cl.cancel, "close", nsHuddles.T("Cancel"), false}, {&cl.join, "headphones", v.JoinLabel, true}}
	bd := dp(48)
	lh := labelSize(gtx, th, 13, theme.Regular, nsHuddles.T("Join")).Y
	blk := bd + dp(4) + lh
	fy := sz.Y - footH + (footH-blk)/2 - dp(4)
	widths := make([]int, len(btns))
	total := dp(24) * (len(btns) - 1)
	for i, b := range btns {
		widths[i] = max(bd, labelSize(gtx, th, 13, theme.Bold, b.label).X)
		total += widths[i]
	}
	x := (sz.X - total) / 2
	for i, b := range btns {
		cw := widths[i]
		b := b
		at(gtx, image.Pt(x+(cw-bd)/2, fy), image.Pt(bd, bd), func(gtx C) D {
			return button(gtx, b.cl, image.Pt(bd, bd), func(gtx C, hov bool) {
				ctr := image.Pt(bd/2, bd/2)
				if b.join {
					bg := k.join
					if hov {
						bg = k.joinHov
					}
					theme.Circle(gtx, ctr, bd, k.joinRing)
					theme.Circle(gtx, ctr, bd-2*max(1, dp(1)), bg)
					return
				}
				bg := k.btn
				if hov {
					bg = k.btnHov
				}
				theme.Circle(gtx, ctr, bd, bg)
			}, func(gtx C) D {
				col := k.sec
				if b.join {
					col = k.joinInk
				}
				iconAt(gtx, image.Pt(bd, bd), b.icon, 20, col)
				return D{Size: image.Pt(bd, bd)}
			})
		})
		wt := theme.Regular
		if b.join { // __button_label_start_accept
			wt = theme.Bold
		}
		ls := labelSize(gtx, th, 13, wt, b.label)
		at(gtx, image.Pt(x+(cw-ls.X)/2, fy+bd+dp(4)), ls, func(gtx C) D { return th.Label(gtx, 13, wt, k.text, b.label) })
		x += cw + dp(24)
	}

	// the open device menu, above its picker
	if v.Menu != "" && len(v.MenuItems) > 0 {
		rowH := dp(32)
		mw := max(menuAnchor.Dx(), dp(240))
		mh := len(v.MenuItems)*rowH + 2*dp(6)
		mx := min(max(dp(8), menuAnchor.Min.X), sz.X-dp(8)-mw)
		mr := image.Rect(mx, menuAnchor.Min.Y-dp(4)-mh, mx+mw, menuAnchor.Min.Y-dp(4))
		theme.RRect(gtx, mr.Inset(-max(1, dp(1))), dp(8)+1, k.outline)
		theme.RRect(gtx, mr, dp(8), k.modal)
		ink := rgba(29, 28, 29, 1)
		if th != nil && th.Dark {
			ink = rgba(209, 210, 211, 1)
		}
		for i, it := range v.MenuItems {
			r := image.Rect(mr.Min.X, mr.Min.Y+dp(6)+i*rowH, mr.Max.X, mr.Min.Y+dp(6)+(i+1)*rowH)
			it := it
			at(gtx, r.Min, r.Size(), func(gtx C) D {
				return button(gtx, cl.get(&cl.item, v.Menu+"/"+it.Key), r.Size(), func(gtx C, hov bool) {
					if hov {
						theme.Fill(gtx, image.Rectangle{Max: r.Size()}, theme.WithAlpha(ink, 0x14))
					}
				}, func(gtx C) D {
					if it.Checked {
						at(gtx, image.Pt(dp(10), (r.Dy()-dp(16))/2), image.Pt(dp(16), dp(16)), func(gtx C) D { return icons.Get("check")(gtx, 16, ink) })
					}
					txt := ellipsize(gtx, th, 15, theme.Regular, it.Label, r.Dx()-dp(44))
					ls := labelSize(gtx, th, 15, theme.Regular, txt)
					at(gtx, image.Pt(dp(34), (r.Dy()-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, theme.Regular, ink, txt) })
					return D{Size: r.Size()}
				})
			})
		}
	}
}

func spansWidth(gtx C, th *theme.Theme, lines [][]span) int {
	w := 0
	for _, ln := range lines {
		lw := 0
		for _, s := range ln {
			lw += spanWidth(gtx, th, 15, s)
		}
		w = max(w, lw)
	}
	return w
}

// drawSpans draws wrapped 15/22 lines from p.
func drawSpans(gtx C, th *theme.Theme, lines [][]span, p image.Point, col, bg color.NRGBA) {
	lineH := gtx.Dp(22)
	for i, ln := range lines {
		drawSpanLine(gtx, th, 15, defaultEntInk(col, bg), ln, p.X, p.Y+i*lineH, lineH)
	}
}

// frame is get for a picture the caller holds (the preview's camera).
func (v *videoOps) frame(key string, img *image.RGBA, seq uint64) paint.ImageOp {
	if v.seq == nil {
		v.seq, v.op = map[string]uint64{}, map[string]paint.ImageOp{}
	}
	if o, ok := v.op[key]; ok && v.seq[key] == seq {
		return o
	}
	v.seq[key] = seq
	v.op[key] = paint.NewImageOp(img)
	return v.op[key]
}

// cover draws img filling sz (object-fit: cover), clipped to radius.
func cover(gtx C, img paint.ImageOp, sz image.Point, radius int) {
	defer clip.UniformRRect(image.Rectangle{Max: sz}, radius).Push(gtx.Ops).Pop()
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 {
		return
	}
	s := max(float32(sz.X)/float32(is.X), float32(sz.Y)/float32(is.Y))
	offX := (float32(sz.X) - float32(is.X)*s) / 2
	offY := (float32(sz.Y) - float32(is.Y)*s) / 2
	defer op.Affine(f32Affine(s, offX, offY)).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
