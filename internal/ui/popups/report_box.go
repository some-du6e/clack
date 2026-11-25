package popups

// The report box (Hyper+R; internal/ui/report_box.go): Clack's own
// dialog, not a Slack one, for a report to Clack's developers (Karim,
// 2026-11-24: "sentry for the app, hyper+r = report + comment"). It is
// drawn the way /feedback's UserFeedbackModal is (user_feedback.go): an
// SK modal with a FormTextareaInput for the comment and an inline
// checkbox, plus what goes with the comment, shown before anything is
// sent: the inspect report's rows (content-free: ids are one-way tags)
// and, if the box is ticked, the window's screenshot, which the inspect
// review can blur parts of first. Nothing leaves before Send.

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	rbTitle       = "Report a problem"
	rbIntro       = "Tell Clack’s developers what went wrong. This goes to them, not to Slack, and only when you click Send."
	rbAsk         = "What happened?"
	rbAskHint     = "What were you doing, what did you expect, and what happened instead?"
	rbWarn        = "Don’t include passwords or anything private."
	rbSent        = "What’s sent with it"
	rbSentNote    = "Names of Clack’s and Slack’s components, sizes, colours and settings. Ids are one-way tags: no messages, names or tokens."
	rbShot        = "Attach a screenshot of the window"
	rbShotNote    = "It shows everything on screen, messages and names included. Blur those parts first."
	rbBlur        = "Blur parts of it"
	rbBlurred1    = "1 part blurred"
	rbBlurredN    = " parts blurred"
	rbNoShot      = "No screenshot could be taken."
	rbUnavailable = "Reports can’t be sent from this build: only Clack’s releases can send them. You can still see what a report would hold."
	rbSend        = "Send"
	rbThanks      = "Report sent. Thank you!"
	rbFailed      = "Couldn’t send the report. Check your connection and try again."
	rbLimited     = "Too many reports for now. Try again in a while."
	rbTextMax     = 2000
)

// ReportRow is one row of what a report holds (the inspect report's).
type ReportRow struct{ Key, Value string }

// ReportBoxOpts are the box's contents and its two actions.
type ReportBoxOpts struct {
	Rows []ReportRow
	// Shot is the window's screenshot (nil: none could be taken).
	Shot *image.RGBA
	// Unavailable: this build can't send (no DSN): the box shows what a
	// report would hold, and Send stays disabled.
	Unavailable bool
	// Blur opens the inspect review over shot; done gets the shot with
	// the parts blurred out, and how many, or nil when the review was
	// left. false: it couldn't open.
	Blur func(shot *image.RGBA, done func(img *image.RGBA, blurred int)) bool
	// Send sends the report (the screenshot nil unless ticked; blurred
	// how many parts of it were blurred) and calls done with the outcome
	// on the UI goroutine: nil sent, ErrReportRateLimited, or another
	// error.
	Send func(comment string, shot *image.RGBA, blurred int, done func(err error))
}

// ErrReportRateLimited is the error Send passes on a rate limit.
var ErrReportRateLimited = errReport("rate limited")

type errReport string

func (e errReport) Error() string { return string(e) }

// ReportBox is the report box.
type ReportBox struct {
	h     Host
	o     ReportBoxOpts
	modal *Modal

	text      widget.Editor
	textHit   widget.Clickable
	shotOn    bool
	shotBtn   widget.Clickable
	blurBtn   widget.Clickable
	shot      *image.RGBA
	shotOp    paint.ImageOp
	blurred   int
	blurring  bool // the review is open over the window
	busy      bool
	err       string
	focused   bool // the comment had its first frame's focus
	sendCalls int  // Send calls (tests)
}

// NewReportBox shows the box.
func NewReportBox(h Host, o ReportBoxOpts) *ReportBox {
	m := &ReportBox{h: h, o: o, shot: o.Shot}
	if o.Shot != nil {
		m.shotOp = paint.NewImageOp(o.Shot)
	}
	send := &ModalButton{Label: rbSend, Kind: BtnPrimary,
		Enabled: func() bool { return m.CanSend() },
		Loading: func() bool { return m.busy },
		Do:      func() bool { m.Send(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: rbTitle, Body: m.layout,
		FocusStops: func() []event.Tag {
			stops := []event.Tag{&m.text, &m.shotBtn}
			if m.shotOn && m.shot != nil {
				stops = append(stops, &m.blurBtn)
			}
			return stops
		},
		Buttons: []*ModalButton{{Label: nsUserFeedback.T("Cancel"), Kind: BtnOutline}, send}})
	h.ShowOverlay(m)
	return m
}

func (m *ReportBox) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *ReportBox) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *ReportBox) Dismiss()                                    { m.modal.Dismiss() }

// Rows are what the box shows will be sent with the comment.
func (m *ReportBox) Rows() []ReportRow { return append([]ReportRow(nil), m.o.Rows...) }

// SetComment types the comment (tests).
func (m *ReportBox) SetComment(s string) { m.text.SetText(s) }

// SetShot ticks the screenshot box or not (tests).
func (m *ReportBox) SetShot(on bool) { m.shotOn = on }

// Shot is the screenshot as it would be sent, nil when it wouldn't be
// (unticked, or none).
func (m *ReportBox) Shot() *image.RGBA {
	if !m.shotOn {
		return nil
	}
	return m.shot
}

// Blurred is how many parts of the screenshot were blurred.
func (m *ReportBox) Blurred() int { return m.blurred }

// Err is the error shown ("" none).
func (m *ReportBox) Err() string { return m.err }

// Unavailable reports a build that can't send.
func (m *ReportBox) Unavailable() bool { return m.o.Unavailable }

// SendCalls is how many times the box asked for a send (tests).
func (m *ReportBox) SendCalls() int { return m.sendCalls }

// CanSend reports Send enabled: a build that can send, a comment that
// isn't only spaces and within its limit, nothing on its way, the
// review not over the window.
func (m *ReportBox) CanSend() bool {
	t := m.text.Text()
	return !m.o.Unavailable && m.o.Send != nil && strings.TrimSpace(t) != "" && jsLen(t) <= rbTextMax && !m.busy && !m.blurring
}

// Send is the Send button: the report goes out, and the box stays until
// it's answered (closed, it says so in a toast).
func (m *ReportBox) Send() {
	if !m.CanSend() {
		return
	}
	m.err, m.busy = "", true
	m.sendCalls++
	h := m.h
	m.o.Send(m.text.Text(), m.Shot(), m.blurred, func(err error) {
		m.busy = false
		switch {
		case err == nil:
			m.modal.Dismiss()
			h.Toast(rbThanks)
		case err == ErrReportRateLimited:
			m.err = rbLimited
		default:
			m.err = rbFailed
		}
		if err != nil && m.modal.Dismissed() {
			h.Toast(m.err)
		}
		h.Invalidate()
	})
}

// Blur is "Blur parts of it": the inspect review over the screenshot.
func (m *ReportBox) Blur() {
	if m.shot == nil || m.o.Blur == nil || m.blurring {
		return
	}
	m.blurring = m.o.Blur(m.shot, func(img *image.RGBA, n int) {
		m.blurring = false
		if img != nil {
			m.shot, m.shotOp = img, paint.NewImageOp(img)
			m.blurred += n
			m.shotOn = true
		}
		m.h.Invalidate()
	})
}

func (m *ReportBox) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	if !m.focused {
		m.focused = true
		gtx.Execute(key.FocusCmd{Tag: &m.text})
	}
	if m.textHit.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.text})
	}
	if m.shotBtn.Clicked(gtx) && m.shot != nil {
		m.shotOn = !m.shotOn
	}
	if m.blurBtn.Clicked(gtx) {
		m.Blur()
	}
	for {
		if _, ok := m.text.Update(gtx); !ok {
			break
		}
	}
	w := gtx.Constraints.Max.X
	gap := dp(gtx, 20)
	y := 0
	paraAt := func(size, lh float32, wt font.Weight, c color.NRGBA, s string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, size, lh, wt, c, s, 0)
		})
		y += d.Size.Y
	}
	label := func(s string) {
		paraAt(15, 22, theme.Bold, cl.Fg, s)
		y += dp(gtx, 8) // .c-label__text padding-bottom 8
	}
	alert := func(s string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return srAlert(gtx, th, cl, s, w) })
		y += d.Size.Y + gap
	}
	if m.o.Unavailable {
		alert(rbUnavailable)
	}
	if m.err != "" {
		alert(m.err)
	}
	paraAt(15, 22, theme.Regular, cl.Fg, rbIntro)
	y += gap
	label(rbAsk)
	y += m.textarea(gtx, th, cl, y, w)
	label(rbSent)
	paraAt(13, 18, theme.Regular, cl.Fg3, rbSentNote)
	y += dp(gtx, 8)
	y += m.rowsBox(gtx, th, cl, y, w) + gap
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return m.shotRow(gtx, th, cl, w) })
	y += d.Size.Y + dp(gtx, 4)
	if m.shotOn && m.shot != nil {
		y += dp(gtx, 8)
		y += m.thumb(gtx, th, cl, y, w)
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 8))}
}

// textarea is the comment's FormTextareaInput (UserFeedbackModal's:
// 104px, padding 8 12 4, its CharacterCount while focused) with the
// warning hint under it; it returns its height with the 20px after it.
func (m *ReportBox) textarea(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	h := dp(gtx, 104)
	n := jsLen(m.text.Text())
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(w, h)
		gtx.Constraints = layout.Exact(sz)
		focused := gtx.Focused(&m.text)
		srInputBox(gtx, cl, image.Rectangle{Max: sz}, focused)
		m.textHit.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorText.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
		tw := maxInt(1, w-dp(gtx, 12)-ufCountPad(gtx, th, rbTextMax, n))
		at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, h-dp(gtx, 12))}
			if m.text.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return para(gtx, th, 15, 22, theme.Regular, cl.Placeholder, rbAskHint, 4)
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &m.text, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
		if focused {
			ufCount(gtx, th, cl, rbTextMax, n, w)
		}
		return layout.Dimensions{Size: sz}
	})
	yy := h
	if n > rbTextMax {
		d := at(gtx, image.Pt(0, y+yy), func(gtx layout.Context) layout.Dimensions {
			return srAlert(gtx, th, cl, ufTooLongText(rbTextMax), w)
		})
		return yy + d.Size.Y + dp(gtx, 20)
	}
	yy += dp(gtx, 4)
	d := at(gtx, image.Pt(0, y+yy), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, rbWarn, 0)
	})
	return yy + d.Size.Y + dp(gtx, 20)
}

// rowsBox is the report's rows in a box drawn as an input's (radius 8,
// the 1px outline), padding 8 12: each key in bold in a 96px column, its
// value wrapping beside it, 13px on 18px lines.
func (m *ReportBox) rowsBox(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	kw := dp(gtx, 96)
	vw := maxInt(1, w-2*padX-kw-dp(gtx, 8))
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		yy := 0
		for _, r := range m.o.Rows {
			at(gtx, image.Pt(0, yy), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(kw, 1<<20)}
				return para(gtx, th, 13, 18, theme.Bold, cl.Fg2, r.Key, 1)
			})
			vd := at(gtx, image.Pt(kw+dp(gtx, 8), yy), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(vw, 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg, r.Value, 0)
			})
			yy += maxInt(vd.Size.Y, dp(gtx, 18)) + dp(gtx, 2)
		}
		return layout.Dimensions{Size: image.Pt(w-2*padX, yy)}
	})
	h := d.Size.Y + 2*padY
	box := image.Rect(0, y, w, y+h)
	rad, one := dp(gtx, 8), border1(gtx)
	rrect(gtx, box, rad, cl.InputBorder)
	rrect(gtx, box.Inset(one), rad-one, cl.BgAlt)
	at(gtx, image.Pt(padX, y+padY), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return d
	})
	return h
}

// shotRow is the screenshot's inline checkbox (UserFeedbackModal's ack
// row) with its note under the label; with no screenshot it says so,
// disabled.
func (m *ReportBox) shotRow(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	noShot := m.shot == nil
	return m.shotBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var bx layout.Dimensions
		at(gtx, image.Pt(0, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			bx = Checkbox(gtx, th, m.shotOn && !noShot, false, noShot)
			return bx
		})
		x := bx.Size.X + dp(gtx, 8)
		tw := maxInt(1, w-x)
		fg := cl.Fg
		if noShot {
			fg = cl.DisText
		}
		ld := at(gtx, image.Pt(x, -dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, fg, rbShot, 0)
		})
		note := rbShotNote
		if noShot {
			note = rbNoShot
		}
		nd := at(gtx, image.Pt(x, ld.Size.Y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, note, 0)
		})
		sz := image.Pt(w, max(bx.Size.Y+dp(gtx, 1), ld.Size.Y+nd.Size.Y))
		if !noShot {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	})
}

// thumb is the screenshot as it will be sent, scaled to the body's width
// (at most 200px tall) in a 1px outline, with "Blur parts of it" and how
// many parts were blurred under it.
func (m *ReportBox) thumb(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	sb := m.shot.Bounds()
	if sb.Empty() {
		return 0
	}
	maxH := dp(gtx, 200)
	tw, thH := w, w*sb.Dy()/sb.Dx()
	if thH > maxH {
		thH, tw = maxH, maxH*sb.Dx()/sb.Dy()
	}
	one := border1(gtx)
	r := image.Rect(0, y, tw+2*one, y+thH+2*one)
	fill(gtx, r, cl.InputBorder)
	func() {
		defer op.Offset(image.Pt(one, y+one)).Push(gtx.Ops).Pop()
		defer clip.Rect{Max: image.Pt(tw, thH)}.Push(gtx.Ops).Pop()
		sx, sy := float32(tw)/float32(sb.Dx()), float32(thH)/float32(sb.Dy())
		defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sx, sy))).Push(gtx.Ops).Pop()
		img := m.shotOp
		img.Filter = paint.FilterLinear
		img.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}()
	yy := r.Dy() + dp(gtx, 8)
	bd := at(gtx, image.Pt(0, y+yy), func(gtx layout.Context) layout.Dimensions {
		return Button(gtx, th, &m.blurBtn, BtnOutline, BtnSmall, rbBlur, "", 0)
	})
	if m.blurred > 0 {
		s := rbBlurred1
		if m.blurred > 1 {
			s = strconv.Itoa(m.blurred) + rbBlurredN
		}
		at(gtx, image.Pt(bd.Size.X+dp(gtx, 12), y+yy), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bd.Size.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, s)
			})
		})
	}
	return yy + bd.Size.Y + dp(gtx, 4)
}
