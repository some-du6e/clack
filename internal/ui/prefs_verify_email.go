package ui

import (
	"context"
	"errors"
	"image"
	"image/color"
	"io"
	"log"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Privacy & visibility's Send Verification Email, ported from Slack's web
// client (build d3de3a487b1c):
//
//   - prefs-dialog BlockedInvitesUnverifiedEmailSection: a
//     foreground_min_solid box (radius 8, padding 24px 64px, centered): 👋
//     (8px under), "To see whose invitations you’ve blocked, you’ll need to
//     verify your email address{memberEmail}." (16px under) and a primary
//     "Send Verification Email" (one line, truncated).
//   - boot-shared 3+f0 useEmailVerificationHandlers' handleVerifyEmail:
//     one request at a time, signup.confirmEmail (vendors knqT) for the
//     member's email in the client's locale (reason
//     request-new-email-verification-code); ok opens VerifyEmailModal
//     (stacked, "verify-email-modal"). A failed request shows nothing on
//     this page (isError isn't read here).
//   - 3+f0 VerifyEmailModal: "Enter the code we sent you", "We’ve sent a
//     6-character code to <strong>{email}</strong>. …", LJSk SplitInput
//     (isNumbersOnly false: letters and digits, uppercased; six cells in
//     two groups of three around an em dash; typing moves right,
//     Backspace in an empty cell and the arrows move, a paste fills from
//     the focused cell; the sixth filled cell submits), mFn7 CodeCheckStatus
//     under it, and Cancel (disabled while checking; so is closing). The
//     code goes to signin.confirmCode (vendors wIr1, reason "verify email
//     modal code confirmation"). Errors: failed → invalid_code ("That code
//     wasn't valid. Give it another go!"), ratelimited, expired ("…<a>Request
//     a new code</a>…", which requests one), ratelimited_blocked; the cells
//     clear and the first takes the focus. Success: "Your email has been
//     verified", the page's success message and Got It; the page then
//     counts the email as verified (5kqq isEmailVerified) and lists the
//     blocked invitations.
//
// Clack's window is a desktop one, so the split input takes its
// (width>=1024px) and (hover:hover) and (pointer:fine) rules: the cells
// joined in each group (4px outer corners), 92px tall, 50px text. Each
// cell's input is width:100% of a shrink-to-fit flex item, so Chrome sizes
// it from the input's intrinsic width and the flex row shrinks all six
// equally into the 500px-max container: (width - the dash cell) / 6.

var (
	scNS = i18n.NS("slack_connect")
	tcNS = i18n.NS("team_creation")
)

// Code check states (mFn7's enum).
const (
	codeEmpty      = "empty-status"
	codeProcessing = "processing"
	codeError      = "error"
	codeRateLimit  = "ratelimited"
	codeNewCode    = "request-new-code"
	codeBlocked    = "ratelimited-blocked"
	codeSuccess    = "success"
)

// verifyEmail is useEmailVerificationHandlers' state plus the open
// VerifyEmailModal's.
type verifyEmail struct {
	p          *prefsModal
	requesting bool // R: a code request is out
	email      string
	modal      *popups.Modal
	status     string
	verified   bool // N
	disabled   bool // I: the cells while checking
	cells      [6]widget.Editor
	prev       [6]string // each cell's text as last seen: SetText's own ChangeEvent (Gio reports it once the cell has the focus) isn't typing
	focus      int       // a FocusCmd to send (-1: none)
	focusTries int
	lastCode   string // the code onFillCode last saw
	newCode    widget.Clickable
}

// verifySection is BlockedInvitesUnverifiedEmailSection.
func (p *prefsModal) verifySection(f *prefsForm, email string) {
	gtx, a := f.gtx, f.p.a
	th := a.th
	padX, padY := gtx.Dp(64), gtx.Dp(24)
	iw := max(1, f.w-2*padX)
	label := pt("Send Verification Email")
	me := ""
	if email != "" {
		me = " (" + email + ")"
	}
	msg := prefsChain.T("To see whose invitations you’ve blocked, you’ll need to verify your email address{memberEmail}.", "memberEmail", me)
	f.record("text", "", "👋", false, true)
	f.record("text", "", msg, false, true)
	f.record("button", "button.verify_email", label, false, true)
	center := func(gtx layout.Context, s theme.TextStyle, t string) (layout.Widget, layout.Dimensions) {
		s.Align = text.Middle
		call, d := recordWidget(gtx, image.Pt(iw, 1<<20), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = iw
			return th.Layout(gtx, s, t)
		})
		return func(gtx layout.Context) layout.Dimensions { call.Add(gtx.Ops); return d }, d
	}
	wave, wd := center(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, "👋")
	para, pd := center(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, msg)
	bh := gtx.Dp(36)
	bw := min(iw, max(gtx.Dp(80), th.Measure(gtx, 15, theme.Bold, label)+gtx.Dp(24)+2))
	h := padY + wd.Size.Y + gtx.Dp(8) + pd.Size.Y + gtx.Dp(16) + bh + padY
	r := image.Rect(0, f.y, f.w, f.y+h)
	theme.RRect(gtx, r, gtx.Dp(8), skRGB(th, "sk_foreground_min_solid"))
	y := f.y + padY
	at(gtx, image.Pt(padX, y), wd.Size, wave)
	y += wd.Size.Y + gtx.Dp(8)
	at(gtx, image.Pt(padX, y), pd.Size, para)
	y += pd.Size.Y + gtx.Dp(16)
	b := p.btn("button.verify_email")
	if b.Clicked(gtx) { // before Layout drains it
		p.sendVerificationEmail(email)
		a.Invalidate()
	}
	at(gtx, image.Pt((f.w-bw)/2, y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, th, b, popups.BtnPrimary, popups.BtnMedium, label, "", float32(bw)/gtx.Metric.PxPerDp)
	})
	f.y += h + gtx.Dp(10)
}

// sendVerificationEmail is handleVerifyEmail (q).
func (p *prefsModal) sendVerificationEmail(email string) {
	if p.verify == nil {
		p.verify = &verifyEmail{p: p, focus: -1}
	}
	v := p.verify
	v.email = email
	v.request(func() { v.open() })
}

// request is requestVerificationCode (B): signup.confirmEmail, then ok.
func (v *verifyEmail) request(ok func()) {
	a := v.p.a
	c := a.Core()
	if v.requesting || c == nil {
		return
	}
	v.requesting = true
	email, locale := v.email, i18n.Locale()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.RequestEmailCode(ctx, email, locale)
		cancel()
		a.Post(func() {
			v.requesting = false
			if err != nil {
				log.Printf("ui: verification code request: %v", err)
				return
			}
			if ok != nil {
				ok()
			}
			a.Invalidate()
		})
	}()
}

// open shows VerifyEmailModal.
func (v *verifyEmail) open() {
	a := v.p.a
	v.status, v.verified, v.disabled, v.lastCode = codeEmpty, false, false, ""
	for i := range v.cells {
		v.cells[i] = widget.Editor{SingleLine: true, MaxLen: 1, Filter: codeChars, Alignment: text.Middle}
		v.prev[i] = ""
	}
	v.focus, v.focusTries = 0, 0 // componentDidMount: focusInput(0)
	cancel := &popups.ModalButton{Label: scNS.T("Cancel"), Kind: popups.BtnOutline,
		Enabled: func() bool { return v.status != codeProcessing },
		Hidden:  func() bool { return v.verified }}
	gotIt := &popups.ModalButton{Label: scNS.T("Got It"), Kind: popups.BtnOutline,
		Hidden: func() bool { return !v.verified }}
	v.modal = popups.NewModal(a, popups.ModalOpts{
		TitleFunc: func() string {
			if v.verified {
				return scNS.T("Your email has been verified")
			}
			return scNS.T("Enter the code we sent you")
		},
		Body:       v.body,
		Buttons:    []*popups.ModalButton{cancel, gotIt},
		CloseGuard: func() bool { return v.status != codeProcessing },
		FocusStops: func() []event.Tag {
			if v.verified {
				return nil
			}
			out := make([]event.Tag, len(v.cells))
			for i := range v.cells {
				out[i] = &v.cells[i]
			}
			return out
		},
	})
	a.ShowOverlay(v.modal)
}

// codeChars are the characters a cell takes (isNumbersOnly false:
// /^[a-zA-Z0-9]$/).
const codeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// code is the cells' text.
func (v *verifyEmail) code() string {
	var b strings.Builder
	for i := range v.cells {
		b.WriteString(v.cells[i].Text())
	}
	return b.String()
}

func (v *verifyEmail) complete() bool {
	for i := range v.cells {
		if v.cells[i].Len() == 0 {
			return false
		}
	}
	return true
}

// clear is SplitInput.clear: every cell empty, the first focused.
func (v *verifyEmail) clear() {
	for i := range v.cells {
		v.cells[i].SetText("")
		v.prev[i] = ""
	}
	v.lastCode = ""
	v.focus, v.focusTries = 0, 0
}

// fill is onFillCode (L): check the code.
func (v *verifyEmail) fill(code string) {
	a := v.p.a
	c := a.Core()
	if c == nil {
		return
	}
	v.status, v.disabled = codeProcessing, true
	email := v.email
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.ConfirmEmailCode(ctx, email, code)
		cancel()
		a.Post(func() {
			v.confirmed(err)
			a.Invalidate()
		})
	}()
}

// confirmed is L's result: success (E, T("success"), onVerifySuccess) or
// the error's state.
func (v *verifyEmail) confirmed(err error) {
	if err == nil {
		v.verified, v.status = true, codeSuccess
		v.p.emailVerified()
		return
	}
	name := "invalid_code"
	var se *slack.Error
	if errors.As(err, &se) && se.Code != "failed" {
		name = se.Code
	}
	v.clear()
	v.disabled = false
	switch name {
	case "ratelimited":
		v.status = codeRateLimit
	case "expired":
		v.status = codeNewCode
	case "ratelimited_blocked":
		v.status = codeBlocked
	default:
		v.status = codeError
	}
}

// emailVerified is onVerifySuccess's isEmailVerified: the page lists the
// blocked invitations now.
func (p *prefsModal) emailVerified() {
	l := &p.lists
	l.blockStarted, l.verifyDone, l.verified, l.blockDone = true, true, true, false
	c := p.a.Core()
	if c == nil {
		l.blockDone = true
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		emails, _ := c.BlockedInviteEmails(ctx) // useBlockedEmailsList, now that the email counts as verified
		p.a.Post(func() { l.blocked, l.blockDone = emails, true; p.a.Invalidate() })
	}()
}

// body is VerifyEmailModal's content.
func (v *verifyEmail) body(gtx layout.Context) layout.Dimensions {
	th := v.p.a.th
	w := gtx.Constraints.Max.X
	if v.verified {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, pt("You can now manage the Slack Connect invitations you’ve blocked."))
	}
	y := 0
	if v.email != "" {
		parts := scNS.RT("We’ve sent a 6-character code to <strong>{email}</strong>. The code expires shortly, so please enter it soon.", "email", v.email)
		d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
			return richRuns(gtx, th, 15, 22, th.C.Fg, parts, nil, nil)
		})
		y += d.Size.Y + gtx.Dp(16) // p{margin:0 0 1rem}
	}
	cw := min(w, gtx.Dp(500)) // __input_container: width 100%, max-width 500px
	x0 := (w - cw) / 2
	d := at(gtx, image.Pt(x0, y), image.Pt(cw, 1<<20), v.split)
	y += d.Size.Y + gtx.Dp(8) // margin_bottom_50
	if sd := at(gtx, image.Pt(x0, y), image.Pt(cw, 1<<20), v.statusLine); sd.Size.Y > 0 {
		y += sd.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// split is SplitInput: three cells, the em dash cell, three cells.
func (v *verifyEmail) split(gtx layout.Context) layout.Dimensions {
	th := v.p.a.th
	w := gtx.Constraints.Max.X
	v.update(gtx)
	m8 := gtx.Dp(8) // margin_50
	dash := th.Measure(gtx, 15, theme.Regular, "—")
	dw := dash + 2*m8
	cell := max(1, (w-dw)/6)
	ch := gtx.Dp(92) + 2*max(1, gtx.Dp(1))
	x := 0
	for i := range v.cells {
		if i == 3 {
			at(gtx, image.Pt(x+m8, (ch-gtx.Dp(22))/2), image.Pt(dash, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, "—")
			})
			x += dw
		}
		i := i
		at(gtx, image.Pt(x, 0), image.Pt(cell, ch), func(gtx layout.Context) layout.Dimensions {
			return v.cell(gtx, i, image.Pt(cell, ch))
		})
		x += cell
	}
	v.sendFocus(gtx)
	return layout.Dimensions{Size: image.Pt(w, ch)}
}

// cell draws one split_input_item: a 1px rgba(foreground_high, .5)
// border, the left one dropped inside a group, 4px corners at the group's
// ends; focused, a 1px highlight border and box-shadow 0 0 0 1px
// highlight, 0 0 7px highlight at 30%. The input's text is 50px/56,
// centered, sk_primary_foreground.
func (v *verifyEmail) cell(gtx layout.Context, i int, sz image.Point) layout.Dimensions {
	th := v.p.a.th
	ed := &v.cells[i]
	b := max(1, gtx.Dp(1))
	first, last := i%3 == 0, i%3 == 2
	rad := gtx.Dp(4)
	nw, ne, se, sw := 0, 0, 0, 0
	if first {
		nw, sw = rad, rad
	}
	if last {
		ne, se = rad, rad
	}
	r := image.Rectangle{Max: sz}
	border := skForeground(th, "sk_foreground_high", .5)
	hl := skRGB(th, "sk_highlight")
	focused := gtx.Focused(ed)
	box := r
	if !first {
		box.Min.X -= b // border-left: none; the neighbour's right border is the line
	}
	if focused {
		// the 7px blur at 30%, as a 3px band at half that
		g := gtx.Dp(3)
		theme.RRect(gtx, box.Inset(-b-g), rad+b+g, theme.LinearAlpha(theme.WithAlpha(hl, 0x26), th.C.Bg))
		fillRRect4(gtx, box.Inset(-b), hl, nw+b, ne+b, se+b, sw+b)
		fillRRect4(gtx, box, hl, nw, ne, se, sw)
	} else {
		fillRRect4(gtx, box, border, nw, ne, se, sw)
	}
	inner := box.Inset(b)
	if first {
		inner = r.Inset(b)
	}
	fillRRect4(gtx, inner, th.C.Bg, max(0, nw-b), max(0, ne-b), max(0, se-b), max(0, sw-b))
	ed.ReadOnly = v.disabled
	ih := gtx.Dp(56)
	at(gtx, image.Pt(inner.Min.X, (sz.Y-ih)/2), image.Pt(inner.Dx(), ih), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(inner.Dx(), ih))
		return th.Editor(gtx, ed, unit.Sp(50), skRGB(th, "sk_primary_foreground"), theme.WithAlpha(hl, 0x55))
	})
	return layout.Dimensions{Size: sz}
}

// fillRRect4 fills r with per-corner radii.
func fillRRect4(gtx layout.Context, r image.Rectangle, c color.NRGBA, nw, ne, se, sw int) {
	defer clip.RRect{Rect: r, NW: nw, NE: ne, SE: se, SW: sw}.Push(gtx.Ops).Pop()
	theme.Fill(gtx, r, c)
}

// update runs the cells' events: SplitInput's onKeyDown (←, →, Backspace
// in an empty cell), onPaste (from the focused cell on), onChange
// (uppercase, then the next cell) and componentDidUpdate (a full code
// submits; a change clears the status).
func (v *verifyEmail) update(gtx layout.Context) {
	changed := false
	for i := range v.cells {
		ed := &v.cells[i]
		for {
			fs := []event.Filter{
				key.Filter{Focus: ed, Name: key.NameLeftArrow},
				key.Filter{Focus: ed, Name: key.NameRightArrow},
				transfer.TargetFilter{Target: ed, Type: "application/text"},
			}
			if ed.Len() == 0 {
				fs = append(fs, key.Filter{Focus: ed, Name: key.NameDeleteBackward})
			}
			ev, ok := gtx.Event(fs...)
			if !ok {
				break
			}
			switch e := ev.(type) {
			case key.Event:
				if e.State != key.Press {
					continue
				}
				switch e.Name {
				case key.NameLeftArrow, key.NameDeleteBackward:
					if i > 0 {
						v.focus, v.focusTries = i-1, 0
					}
				case key.NameRightArrow:
					if i < len(v.cells)-1 {
						v.focus, v.focusTries = i+1, 0
					}
				}
			case transfer.DataEvent:
				if v.disabled {
					continue
				}
				r := e.Open()
				b, _ := io.ReadAll(io.LimitReader(r, 4096))
				r.Close()
				v.paste(i, string(b))
				changed = true
			}
		}
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.ChangeEvent); !ok {
				continue
			}
			t := ed.Text()
			if up := strings.ToUpper(t); up != t {
				ed.SetText(up)
				ed.SetCaret(1, 1)
				t = up
			}
			if t == v.prev[i] {
				continue // an echo of SetText (paste, clear, the uppercase above)
			}
			v.prev[i] = t
			changed = true
			if t != "" && !v.complete() && i < len(v.cells)-1 {
				v.focus, v.focusTries = i+1, 0
			}
		}
	}
	if changed {
		v.changed()
	}
}

// changed is componentDidUpdate after the cells changed: a full code that
// differs from the last one submits; otherwise any text clears the status.
func (v *verifyEmail) changed() {
	code := v.code()
	if v.complete() {
		if code != v.lastCode { // SetText's own ChangeEvent repeats the same code: no second check
			v.lastCode = code
			v.fill(code)
		}
		return
	}
	v.lastCode = ""
	if code != "" && v.status != codeProcessing {
		v.status = codeEmpty // onChange: T("empty-status")
	}
}

// paste is SplitInput.onPaste: the pasted letters and digits fill the
// cells from i on, and the cell after the last one filled takes the focus.
func (v *verifyEmail) paste(i int, s string) {
	var rs []rune
	for _, r := range s {
		if r < unicode.MaxASCII && strings.ContainsRune(codeChars, r) {
			rs = append(rs, unicode.ToUpper(r))
		}
	}
	n := min(len(rs), len(v.cells)-i)
	for k := 0; k < n; k++ {
		v.cells[i+k].SetText(string(rs[k]))
		v.prev[i+k] = string(rs[k])
	}
	v.focus, v.focusTries = min(len(v.cells)-1, i+n), 0
}

// sendFocus gives the pending cell the focus (after the frame's events,
// so the FocusCmd doesn't hold the rest of them back); it retries a few
// frames while the modal settles its own focus.
func (v *verifyEmail) sendFocus(gtx layout.Context) {
	if v.focus < 0 {
		return
	}
	t := &v.cells[v.focus]
	if gtx.Focused(t) || v.focusTries > 3 {
		v.focus = -1
		return
	}
	v.focusTries++
	gtx.Execute(key.FocusCmd{Tag: t})
	gtx.Execute(op.InvalidateCmd{}) // one more frame to see it land (at most 4)
}

// statusLine is CodeCheckStatus (isCentered), margin-top 15px.
func (v *verifyEmail) statusLine(gtx layout.Context) layout.Dimensions {
	th := v.p.a.th
	w := gtx.Constraints.Max.X
	type line struct {
		parts []i18n.Part
	}
	var lines []line
	level := "" // "": transparent (the form's level_default), warning, error
	spin := false
	switch v.status {
	case codeProcessing:
		spin = true
		lines = []line{{[]i18n.Part{{Text: tcNS.T("Checking your code…")}}}}
	case codeRateLimit:
		level = "warning"
		lines = []line{{[]i18n.Part{{Text: tcNS.T("Whoa, you’re going too fast!"), Tag: "b"}}},
			{[]i18n.Part{{Text: tcNS.T("Slow down a bit. You might have better luck if you try again in a minute.")}}}}
	case codeError:
		level = "error"
		lines = []line{{[]i18n.Part{{Text: tcNS.T("That code wasn't valid. Give it another go!")}}}}
	case codeNewCode:
		level = "warning"
		lines = []line{{tcNS.RT("That code’s no longer valid. <a>Request a new code</a> and try again.")}}
	case codeBlocked:
		level = "error"
		lines = []line{{[]i18n.Part{{Text: tcNS.T("Temporarily blocked"), Tag: "b"}}},
			{[]i18n.Part{{Text: tcNS.T("You’ve hit our limit on verification attempts, and you’ll need to try again later.")}}}}
	case codeSuccess:
		lines = []line{{[]i18n.Part{{Text: tcNS.T("Success!")}}}}
	default:
		return layout.Dimensions{}
	}
	top := gtx.Dp(15) // .confirmation_code_checker
	px, py := gtx.Dp(16), gtx.Dp(12)
	b := max(1, gtx.Dp(1))
	lead := 0
	if level != "" || spin {
		lead = gtx.Dp(20 + 12) // c-icon--inline 20px, margin-right 12px
	}
	tw := max(1, w-2*px-2*b-lead)
	fg := skRGB(th, "sk_primary_foreground")
	if v.newCode.Clicked(gtx) {
		v.request(func() { v.status = codeEmpty })
	}
	call, d := recordWidget(gtx, image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
		y, mw := 0, 0
		for _, l := range lines {
			d := at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return richRuns(gtx, th, 13, 18, fg, l.parts, &v.newCode, nil)
			})
			y += d.Size.Y
			mw = max(mw, d.Size.X)
		}
		return layout.Dimensions{Size: image.Pt(mw, y)}
	})
	h := d.Size.Y + 2*py + 2*b
	r := image.Rect(0, top, w, top+h)
	switch level {
	case "warning":
		theme.RRect(gtx, r, gtx.Dp(8), cardTok(th, "surf-hgl-3"))
	case "error":
		theme.RRect(gtx, r, gtx.Dp(8), cardTok(th, "surf-imp"))
	}
	cx := (w - (lead + d.Size.X)) / 2 // c-alert--align_center
	ty := top + b + py
	if lead > 0 {
		is := gtx.Dp(13) // the icon font at the message's 13px
		at(gtx, image.Pt(cx+(gtx.Dp(20)-is)/2, ty+(gtx.Dp(18)-is)/2+gtx.Dp(1)), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			if spin {
				codeSpinner(gtx, is, fg)
			} else {
				ic := cardTok(th, "content-hgl-3")
				if level == "error" {
					ic = cardTok(th, "content-imp")
				}
				icons.Get("warning")(gtx, unit.Dp(13), ic)
			}
			return layout.Dimensions{Size: image.Pt(is, is)}
		})
	}
	at(gtx, image.Pt(cx+lead, ty), d.Size, func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return d
	})
	return layout.Dimensions{Size: image.Pt(w, top+h)}
}

// richRuns lays parts out as one wrapping paragraph: <strong>/<b> runs
// bold, <a> runs in the link color and clickable through link.
func richRuns(gtx layout.Context, th *theme.Theme, size, lh float32, col color.NRGBA, parts []i18n.Part, link *widget.Clickable, linkCol *color.NRGBA) layout.Dimensions {
	w := gtx.Constraints.Max.X
	type word struct {
		s          string
		bold, link bool
		space      bool // a space before it
	}
	var words []word
	pend := false
	for _, p := range parts {
		t := p.Text
		for t != "" {
			i := strings.IndexByte(t, ' ')
			var f string
			if i < 0 {
				f, t = t, ""
			} else {
				f, t = t[:i], t[i+1:]
			}
			if f == "" {
				pend = true
				continue
			}
			words = append(words, word{f, p.Tag == "strong" || p.Tag == "b", p.Tag == "a", pend})
			pend = i >= 0
		}
		if strings.HasSuffix(p.Text, " ") {
			pend = true
		}
	}
	lc := cardTok(th, "content-hgl-1")
	if linkCol != nil {
		lc = *linkCol
	}
	space := th.Measure(gtx, unit.Sp(size), theme.Regular, " ")
	x, y, maxX := 0, 0, 0
	lhp := gtx.Dp(unit.Dp(lh))
	var linkR image.Rectangle
	for _, wd := range words {
		wt := theme.Regular
		if wd.bold {
			wt = theme.Bold
		}
		ww := th.Measure(gtx, unit.Sp(size), wt, wd.s)
		gap := 0
		if wd.space && x > 0 {
			gap = space
		}
		if x > 0 && x+gap+ww > w {
			x, y, gap = 0, y+lhp, 0
		}
		x += gap
		c := col
		if wd.link {
			c = lc
			wr := image.Rect(x, y, x+ww, y+lhp)
			if linkR.Empty() {
				linkR = wr
			} else {
				linkR = linkR.Union(wr)
			}
		}
		at(gtx, image.Pt(x, y), image.Pt(ww+1, lhp), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lhp, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, unit.Sp(size), wt, c, wd.s)
			})
		})
		x += ww
		maxX = max(maxX, x)
	}
	if link != nil && !linkR.Empty() {
		at(gtx, linkR.Min, linkR.Size(), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, link, nil, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: linkR.Size()}
			})
		})
	}
	if len(words) == 0 {
		return layout.Dimensions{}
	}
	return layout.Dimensions{Size: image.Pt(maxX, y+lhp)}
}
