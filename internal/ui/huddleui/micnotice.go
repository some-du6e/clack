package huddleui

import (
	"image"
	"image/color"
	"runtime"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/huddle/avperm"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// micNote is what to tell the user about their microphone, like Slack's
// "Slack can't access your microphone" banner: a title and a line of
// detail, the settings page to open (if the OS has one for it) and whether
// retrying may help.
type micNote struct {
	title, body string
	settings    string // privacy pane URL, "" = no button
	retry       bool
}

// micGOOS and micSettings are runtime.GOOS and avperm.SettingsURL (tests
// draw the macOS banner on Linux).
var (
	micGOOS     = runtime.GOOS
	micSettings = avperm.SettingsURL
)

// settingsLabel is the button that opens the privacy pane.
func settingsLabel() string {
	if micGOOS == "darwin" {
		return "Open System Settings"
	}
	return "Open Settings"
}

func micNoteFor(st huddle.State) (micNote, bool) {
	page := micSettings(avperm.Mic)
	where := "your system's privacy settings"
	switch micGOOS {
	case "darwin":
		where = "System Settings › Privacy & Security › Microphone"
	case "windows":
		where = "Settings › Privacy › Microphone"
	}
	switch st.Mic {
	case huddle.MicDenied:
		return micNote{title: AppName + " can't access your microphone",
			body: "Others can't hear you. Turn on " + AppName + " in " + where + ", then try again.", settings: page, retry: true}, true
	case huddle.MicRestricted:
		return micNote{title: "Your microphone is blocked",
			body: "Its use is restricted on this computer (by an administrator or Screen Time), so others can't hear you."}, true
	case huddle.MicNoUsage:
		return micNote{title: AppName + " can't ask for your microphone",
			body: "This build's Info.plist has no microphone usage description. Install an official build."}, true
	case huddle.MicFailed:
		body := "Others can't hear you."
		if st.MicErr != "" {
			body = st.MicErr + "."
		}
		return micNote{title: AppName + " couldn't start your microphone", body: body, retry: true}, true
	case huddle.MicSilent:
		body := "Check your input device and that it isn't muted."
		if page != "" {
			body = "Check your input device, and that " + AppName + " is allowed in " + where + "."
		}
		return micNote{title: "Your microphone isn't picking up any sound", body: body, settings: page, retry: true}, true
	}
	return micNote{}, false
}

// micBanner is the notice's buttons and dismissal (per status).
type micBanner struct {
	settings, retry, close widget.Clickable
	dismissed              huddle.MicStatus // the status the user closed; MicOK = none
}

// visible reports the note to show for st, after the user's dismissal.
func (b *micBanner) visible(st huddle.State) (micNote, bool) {
	n, ok := micNoteFor(st)
	if !ok {
		b.dismissed = huddle.MicOK
		return n, false
	}
	return n, b.dismissed != st.Mic
}

// handle runs the banner's clicks (on the window's goroutine; opening the
// settings page and the retry both return at once).
func (b *micBanner) handle(gtx C, s *huddle.Session, st huddle.State, n micNote) {
	if b.settings.Clicked(gtx) && n.settings != "" {
		_ = openurl.OpenSettings(n.settings)
	}
	if b.retry.Clicked(gtx) && s != nil {
		s.RetryMic()
	}
	if b.close.Clicked(gtx) {
		b.dismissed = st.Mic
	}
}

var (
	hudWarnBg = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	hudWarn   = color.NRGBA{R: 0xec, G: 0xb2, B: 0x2e, A: 0xff} // Slack's yellow warning
)

// layoutMicBanner draws the notice in width w: warning icon, wrapped text,
// then the buttons on their own row. It returns the height used.
func layoutMicBanner(gtx C, th *theme.Theme, b *micBanner, n micNote, w int) D {
	pad, gap := gtx.Dp(10), gtx.Dp(8)
	ic := gtx.Dp(16)
	xs := gtx.Dp(20)
	tw := max(0, w-2*pad-ic-gap-xs-gap)
	tg := gtx
	tg.Constraints.Min = image.Point{}
	tg.Constraints.Max = image.Pt(tw, gtx.Dp(200))
	// measure the text, then draw the card under it
	tm := recordPara(tg, th, theme.Bold, n.title)
	bm := recordPara(tg, th, theme.Regular, n.body)
	bh := gtx.Dp(26)
	h := pad + max(tm.size.Y, ic) + gtx.Dp(2) + bm.size.Y + pad
	type btn struct {
		c     *widget.Clickable
		label string
	}
	var btns []btn
	if n.settings != "" {
		btns = append(btns, btn{&b.settings, settingsLabel()})
	}
	if n.retry {
		btns = append(btns, btn{&b.retry, "Try again"})
	}
	if len(btns) > 0 {
		h += bh + pad
	}
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(8), hudWarnBg)
	at(gtx, image.Pt(pad, pad+gtx.Dp(1)), image.Pt(ic, ic), func(gtx C) D {
		iconAt(gtx, image.Pt(ic, ic), "warning", 16, hudWarn)
		return D{Size: image.Pt(ic, ic)}
	})
	at(gtx, image.Pt(pad+ic+gap, pad), tm.size, func(gtx C) D { tm.call.Add(gtx.Ops); return D{Size: tm.size} })
	at(gtx, image.Pt(pad+ic+gap, pad+tm.size.Y+gtx.Dp(2)), bm.size, func(gtx C) D { bm.call.Add(gtx.Ops); return D{Size: bm.size} })
	at(gtx, image.Pt(w-pad-xs, pad-gtx.Dp(2)), image.Pt(xs, xs), func(gtx C) D {
		return button(gtx, &b.close, image.Pt(xs, xs), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(xs, xs)}, gtx.Dp(4), hudBtn)
			}
		}, func(gtx C) D { iconAt(gtx, image.Pt(xs, xs), "close", 12, hudWhite); return D{} })
	})
	x := pad + ic + gap
	y := h - pad - bh
	for i, bt := range btns {
		lw := labelSize(gtx, th, 13, theme.Bold, bt.label).X + 2*gtx.Dp(12)
		primary := i == 0
		bt := bt
		at(gtx, image.Pt(x, y), image.Pt(lw, bh), func(gtx C) D {
			return button(gtx, bt.c, image.Pt(lw, bh), func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				if primary {
					bg = hudWhite
					if hov {
						bg = hudBtnOn
					}
				}
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(lw, bh)}, gtx.Dp(6), bg)
			}, func(gtx C) D {
				fg := hudWhite
				if primary {
					fg = hudInk
				}
				centerLabel(gtx, th, image.Pt(lw, bh), 13, theme.Bold, fg, bt.label)
				return D{}
			})
		})
		x += lw + gap
	}
	return D{Size: image.Pt(w, h)}
}

type recorded struct {
	call op.CallOp
	size image.Point
}

func recordPara(gtx C, th *theme.Theme, wt font.Weight, txt string) recorded {
	m := op.Record(gtx.Ops)
	col := hudWhite
	if wt != theme.Bold {
		col = theme.WithAlpha(hudWhite, 0xd9)
	}
	d := th.Para(gtx, 13, 18, wt, col, txt)
	return recorded{call: m.Stop(), size: d.Size}
}

// warnBadge is the yellow "!" dot on a control's top-right corner at p.
func warnBadge(gtx C, p image.Point, d int) {
	c := image.Pt(p.X-d/2+gtx.Dp(2), p.Y+d/2-gtx.Dp(2))
	theme.Circle(gtx, c, d+gtx.Dp(2), hudFrame)
	theme.Circle(gtx, c, d, hudWarn)
	at(gtx, c.Sub(image.Pt(d/2, d/2)), image.Pt(d, d), func(gtx C) D {
		iconAt(gtx, image.Pt(d, d), "warning", 9, hudInk)
		return D{Size: image.Pt(d, d)}
	})
}

// micShort is the sidebar bar's one-line version of the note.
func micShort(m huddle.MicStatus) string {
	switch m {
	case huddle.MicDenied:
		return "No microphone access"
	case huddle.MicRestricted:
		return "Microphone blocked"
	case huddle.MicNoUsage:
		return "Can't ask for the mic"
	case huddle.MicFailed:
		return "Microphone didn't start"
	case huddle.MicSilent:
		return "Your mic picks up no sound"
	}
	return ""
}
