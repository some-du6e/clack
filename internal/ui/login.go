package ui

import (
	"image"
	"image/color"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// loginScreen is the native sign-in card shown when credentials are missing.
// The user pastes the token and cookies of their own signed-in Slack session;
// slack.SignIn only checks them with auth.test (no page loads, no sign-out).
type loginScreen struct {
	workspace widget.Editor
	token     widget.Editor
	orgToken  widget.Editor
	cookie    widget.Editor
	userAgent widget.Editor
	submit    widget.Clickable
	copy      widget.Clickable
	copied    bool
	busy      bool
	err       string
	focused   bool
}

// signInSnippet runs in the DevTools console of the user's own signed-in Slack
// tab. It copies the browser's user agent and the teams map Slack's page keeps
// in localStorage (workspace and org tokens, URLs); SignIn reads that JSON from
// the Token field. It sends nothing anywhere.
const signInSnippet = `copy(JSON.stringify({ua: navigator.userAgent, teams: JSON.parse(localStorage.localConfig_v2).teams}))`

func newLoginScreen() *loginScreen {
	l := &loginScreen{}
	l.workspace.SingleLine = true
	l.workspace.Submit = true
	l.workspace.SetText("hackclub")
	for _, ed := range []*widget.Editor{&l.token, &l.orgToken, &l.cookie} {
		ed.SingleLine = true
		ed.Submit = true
		ed.Mask = '•'
	}
	l.userAgent.SingleLine = true // not a secret: shown so it can be checked
	l.userAgent.Submit = true
	return l
}

func (a *App) layoutLogin(gtx layout.Context) {
	l := a.login
	sz := gtx.Constraints.Max
	th := a.th
	lc := theme.LightPalette // the card is always light, like Slack's sign-in page
	if !l.focused {
		gtx.Execute(key.FocusCmd{Tag: &l.token})
		l.focused = true
	}
	submit := l.submit.Clicked(gtx)
	if l.copy.Clicked(gtx) {
		// Written here, not with a.copyText: frame returns before keys.process
		// (which flushes a.clipboard) while the sign-in screen is up.
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(signInSnippet))})
		l.copied = true
	}
	for _, ed := range []*widget.Editor{&l.workspace, &l.token, &l.orgToken, &l.cookie, &l.userAgent} {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.SubmitEvent); ok {
				submit = true
			}
		}
	}
	if submit && !l.busy && a.opts.Login != nil {
		in := slack.SignInInput{
			Workspace: strings.TrimSpace(l.workspace.Text()),
			Token:     strings.TrimSpace(l.token.Text()),
			OrgToken:  strings.TrimSpace(l.orgToken.Text()),
			Cookie:    strings.TrimSpace(l.cookie.Text()),
			UserAgent: strings.TrimSpace(l.userAgent.Text()),
		}
		if in.Token == "" || in.Cookie == "" {
			l.err = "Paste your token and d cookie first."
		} else {
			l.busy, l.err = true, ""
			go func() {
				err := a.opts.Login(in)
				a.Post(func() {
					l.busy = false
					if err != nil {
						l.err = err.Error()
						return
					}
					for _, ed := range []*widget.Editor{&l.token, &l.orgToken, &l.cookie} {
						ed.SetText("")
					}
					a.login = nil
					if a.data != nil {
						a.data.start(a)
					}
				})
			}()
		}
	}

	// Two columns so everything fits the default 900px-tall window: how to
	// get the values on the left, the fields on the right. The contents are
	// recorded first, so the card can be sized to them before it is painted.
	pad, gap := gtx.Dp(32), gtx.Dp(32)
	w := min(gtx.Dp(900), sz.X-gtx.Dp(16))
	colW := (w - 2*pad - gap) / 2
	rec := op.Record(gtx.Ops)
	x, y := pad, pad
	at(gtx, image.Pt(x, y), image.Pt(w-2*pad, gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 28, theme.Black, lc.Fg, "Sign in to Slack")
	})
	y += gtx.Dp(52)

	// Left column. The session-sharing warning comes first: Clack runs on the
	// browser's own session, and Slack (Enterprise Grid especially) can end a
	// session it sees used from somewhere unexpected, in every app at once.
	ly := y
	para := func(px, pw int, st theme.TextStyle, txt string) int {
		return at(gtx, image.Pt(px, ly), image.Pt(pw, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, st, txt)
		}).Size.Y
	}
	body := theme.TextStyle{Size: 14, LineHeight: 20, Color: lc.Fg}
	ip := gtx.Dp(12)
	wm := op.Record(gtx.Ops)
	ly += ip
	th0 := para(x+ip, colW-2*ip, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Fg},
		AppName+" shares the session of the browser you copy these from: it is not a separate sign-in. "+
			"If Slack sees that session used in a way it finds unusual, it can sign it out everywhere, browser included "+
			"(this has happened on Enterprise Grid). Copy everything from one browser, and leave live updates off unless you accept that risk.")
	ly -= ip
	wcall := wm.Stop()
	wr := image.Rect(x, ly, x+colW, ly+th0+2*ip)
	theme.RRect(gtx, wr, gtx.Dp(6), bannerWarn)
	theme.Border(gtx, wr, gtx.Dp(6), gtx.Dp(1), color.NRGBA{R: 0xe8, G: 0xb3, B: 0x2c, A: 0xff})
	wcall.Add(gtx.Ops)
	ly = wr.Max.Y + gtx.Dp(20)

	ly += para(x, colW, body, "1. Open Slack in the browser you're signed in to, press F12 and run this in the Console:") + gtx.Dp(8)
	cm := op.Record(gtx.Ops)
	ly += ip
	ch := para(x+ip, colW-2*ip, theme.TextStyle{Size: 13, LineHeight: 19, Color: lc.CodeFg, Wrap: text.WrapGraphemes}, signInSnippet)
	ly -= ip
	ccall := cm.Stop()
	cr := image.Rect(x, ly, x+colW, ly+ch+2*ip)
	theme.RRect(gtx, cr, gtx.Dp(6), lc.CodeBg)
	theme.Border(gtx, cr, gtx.Dp(6), gtx.Dp(1), lc.CodeBorder)
	ccall.Add(gtx.Ops)
	ly = cr.Max.Y + gtx.Dp(8)
	copyTxt := "Copy snippet"
	if l.copied {
		copyTxt = "Copied"
	}
	bw, bh := th.Measure(gtx, 14, theme.Bold, copyTxt)+gtx.Dp(24), gtx.Dp(30)
	atExact(gtx, image.Pt(x, ly), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &l.copy, func(gtx layout.Context, s image.Point, hov bool) {
			bg := lc.Bg
			if hov {
				bg = lc.CodeBg
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(6), gtx.Dp(1), lc.BorderStrong)
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 14, theme.Bold, lc.Fg, copyTxt)
				})
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	ly += bh + gtx.Dp(16)
	ly += para(x, colW, body, "2. Paste what it copied into Token. That fills in the org token and user agent for you.") + gtx.Dp(12)
	ly += para(x, colW, body, "3. The d cookie is HttpOnly, so the snippet can't read it. Copy its value from DevTools \u2192 Application \u2192 Cookies \u2192 https://app.slack.com. Leave d-s out: Clack doesn't use it.")

	// Right column: the fields and the button.
	fx, fy := x+colW+gap, y
	field := func(label string, ed *widget.Editor, hint string) {
		at(gtx, image.Pt(fx, fy), image.Pt(colW, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, lc.Fg, label)
		})
		fy += gtx.Dp(26)
		fr := image.Rect(fx, fy, fx+colW, fy+gtx.Dp(44))
		theme.RRect(gtx, fr, gtx.Dp(6), lc.Bg)
		bc := lc.BorderStrong
		if gtx.Focused(ed) {
			bc = lc.Link
		}
		theme.Border(gtx, fr, gtx.Dp(6), gtx.Dp(1), bc)
		at(gtx, image.Pt(fx+gtx.Dp(12), fy), image.Pt(colW-gtx.Dp(24), gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, gtx.Dp(44), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if ed.Len() == 0 {
					th.Label(gtx, 15, theme.Regular, lc.FgSubtle, hint)
				}
				return th.Editor(gtx, ed, 15, lc.Fg, lc.Selection)
			})
		})
		fy += gtx.Dp(56)
	}
	field("Workspace", &l.workspace, "hackclub")
	field("Token", &l.token, "xoxc-\u2026 or the copied JSON")
	field("Org token (optional)", &l.orgToken, "filled in from the JSON")
	field("d cookie", &l.cookie, "xoxd-\u2026")
	field("Browser user agent (optional)", &l.userAgent, "filled in from the JSON")
	br := image.Rect(fx, fy, fx+colW, fy+gtx.Dp(44))
	atExact(gtx, br.Min, br.Size(), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &l.submit, func(gtx layout.Context, s image.Point, hov bool) {
			bg := theme.LightPalette.ChromeA
			if hov {
				bg = theme.LightPalette.ChromeB
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			txt := "Sign in"
			if l.busy {
				txt = "Signing in\u2026"
			}
			tw := th.Measure(gtx, 18, theme.Bold, txt)
			at(gtx, image.Pt((br.Dx()-tw)/2, 0), image.Pt(tw+2, br.Dy()), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, br.Dy(), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 18, theme.Bold, lc.SendText, txt)
				})
			})
			return layout.Dimensions{Size: br.Size()}
		})
	})
	fy = br.Max.Y
	if l.err != "" {
		fy += gtx.Dp(10)
		fy += at(gtx, image.Pt(fx, fy), image.Pt(colW, gtx.Dp(60)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Danger, MaxLines: 3}, l.err)
		}).Size.Y
	}
	content := rec.Stop()

	h := max(ly, fy) + pad
	left, top := (sz.X-w)/2, max(gtx.Dp(8), (sz.Y-h)/2)
	r := image.Rect(left, top, left+w, top+h)
	theme.Shadow(gtx, r, gtx.Dp(12), gtx.Dp(12), theme.WithAlpha(lc.PanelShadow, 0x60))
	theme.RRect(gtx, r, gtx.Dp(12), lc.Bg)
	off := op.Offset(r.Min).Push(gtx.Ops)
	content.Add(gtx.Ops)
	off.Pop()
}
