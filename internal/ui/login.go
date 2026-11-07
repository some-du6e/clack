package ui

import (
	"image"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
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
	cookieDS  widget.Editor
	submit    widget.Clickable
	busy      bool
	err       string
	focused   bool
}

func newLoginScreen() *loginScreen {
	l := &loginScreen{}
	l.workspace.SingleLine = true
	l.workspace.Submit = true
	l.workspace.SetText("hackclub")
	for _, ed := range []*widget.Editor{&l.token, &l.orgToken, &l.cookie, &l.cookieDS} {
		ed.SingleLine = true
		ed.Submit = true
		ed.Mask = '•'
	}
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
	for _, ed := range []*widget.Editor{&l.workspace, &l.token, &l.orgToken, &l.cookie, &l.cookieDS} {
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
			CookieDS:  strings.TrimSpace(l.cookieDS.Text()),
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
					for _, ed := range []*widget.Editor{&l.token, &l.orgToken, &l.cookie, &l.cookieDS} {
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

	w, h := gtx.Dp(440), gtx.Dp(700)
	r := image.Rect((sz.X-w)/2, (sz.Y-h)/2, (sz.X+w)/2, (sz.Y+h)/2)
	theme.Shadow(gtx, r, gtx.Dp(12), gtx.Dp(12), theme.WithAlpha(lc.PanelShadow, 0x60))
	theme.RRect(gtx, r, gtx.Dp(12), lc.Bg)
	x := r.Min.X + gtx.Dp(32)
	y := r.Min.Y + gtx.Dp(32)
	cw := w - gtx.Dp(64)
	at(gtx, image.Pt(x, y), image.Pt(cw, gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 28, theme.Black, lc.Fg, "Sign in to Slack")
	})
	y += gtx.Dp(44)
	at(gtx, image.Pt(x, y), image.Pt(cw, gtx.Dp(70)), func(gtx layout.Context) layout.Dimensions {
		return th.Para(gtx, 15, 22, theme.Regular, lc.FgMuted, "Paste the token and cookies from your own signed-in Slack session, the same values other third-party Slack clients use. "+AppName+" only checks them with Slack and never signs that session out.")
	})
	y += gtx.Dp(88)
	field := func(label string, ed *widget.Editor, hint string) {
		at(gtx, image.Pt(x, y), image.Pt(cw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, lc.Fg, label)
		})
		y += gtx.Dp(26)
		fr := image.Rect(x, y, x+cw, y+gtx.Dp(44))
		theme.RRect(gtx, fr, gtx.Dp(6), lc.Bg)
		bc := lc.BorderStrong
		if gtx.Focused(ed) {
			bc = lc.Link
		}
		theme.Border(gtx, fr, gtx.Dp(6), gtx.Dp(1), bc)
		at(gtx, image.Pt(x+gtx.Dp(12), y), image.Pt(cw-gtx.Dp(24), gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, gtx.Dp(44), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if ed.Len() == 0 {
					th.Label(gtx, 15, theme.Regular, lc.FgSubtle, hint)
				}
				return th.Editor(gtx, ed, 15, lc.Fg, lc.Selection)
			})
		})
		y += gtx.Dp(56)
	}
	field("Workspace", &l.workspace, "hackclub")
	field("Token", &l.token, "xoxc-…")
	field("Org token (optional)", &l.orgToken, "xoxc-… (Enterprise Grid)")
	field("d cookie", &l.cookie, "xoxd-…")
	field("d-s cookie (optional)", &l.cookieDS, "")
	br := image.Rect(x, y, x+cw, y+gtx.Dp(44))
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
				txt = "Signing in…"
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
	if l.err != "" {
		at(gtx, image.Pt(x, br.Max.Y+gtx.Dp(10)), image.Pt(cw, gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, Color: lc.Danger, MaxLines: 2}, l.err)
		})
	}
}
