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
//
// The main path is desktop-style sign-in: the user signs in in their own
// browser, and the slack:// link Slack's page hands "the Slack app" is
// pasted here (or the browser opens Clack with it; App.SignInWithLink).
// slack.LoginMagic trades it for a session of Clack's own.
//
// Advanced is the older path: paste the token and cookie of the browser's
// own session; slack.SignIn only checks them with auth.test (no page loads,
// no sign-out), but Clack then shares the browser's session.
type loginScreen struct {
	link       widget.Editor
	linkSubmit widget.Clickable
	openSignIn widget.Clickable
	advanced   bool // the token-paste form
	advToggle  widget.Clickable

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

	card image.Rectangle // where the card was last drawn (tests)
}

// desktopSignInURL is the page Slack desktop opens in the browser for
// "Sign in to Another Workspace" (main.bundle.cjs @1033551 in
// docs/DESKTOP-SIGNIN.md). Signing in there ends with Slack offering to open
// the Slack app through a slack:// link, the link Clack wants. Clack only
// opens it in the user's browser, on a click; it never loads it itself.
var desktopSignInURL = "https://slack.com/ssb/add?s=1&v=" + slack.SlackDesktopVersion

// signInSnippet runs in the DevTools console of the user's own signed-in Slack
// tab. It copies the browser's user agent and the teams map Slack's page keeps
// in localStorage (workspace and org tokens, URLs); SignIn reads that JSON from
// the Token field. It sends nothing anywhere.
const signInSnippet = `copy(JSON.stringify({ua: navigator.userAgent, teams: JSON.parse(localStorage.localConfig_v2).teams}))`

func newLoginScreen() *loginScreen {
	l := &loginScreen{}
	l.link.SingleLine = true
	l.link.Submit = true
	l.link.Mask = '•' // a one-time secret: whoever has it first gets the session
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
		if l.advanced {
			gtx.Execute(key.FocusCmd{Tag: &l.token})
		} else {
			gtx.Execute(key.FocusCmd{Tag: &l.link})
		}
		l.focused = true
	}
	if l.advToggle.Clicked(gtx) {
		l.advanced, l.focused, l.err = !l.advanced, false, ""
	}
	if l.openSignIn.Clicked(gtx) {
		openURL(desktopSignInURL)
	}
	submit := l.submit.Clicked(gtx)
	linkSubmit := l.linkSubmit.Clicked(gtx)
	if l.copy.Clicked(gtx) {
		// Written here, not with a.copyText: frame returns before keys.process
		// (which flushes a.clipboard) while the sign-in screen is up.
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(signInSnippet))})
		l.copied = true
	}
	for {
		e, ok := l.link.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			linkSubmit = true
		}
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
	if linkSubmit && !l.advanced && !l.busy {
		if link := strings.TrimSpace(l.link.Text()); link == "" {
			l.err = "Paste the slack:// link from your browser first."
		} else if !slack.IsMagicLink(link) {
			l.err = slack.ErrNotMagicLink.Error()
		} else {
			a.startLogin(slack.SignInInput{Link: link})
		}
	}
	if submit && l.advanced && !l.busy {
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
			a.startLogin(in)
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

	var ly, fy int
	if l.advanced {
		ly, fy = a.layoutLoginPaste(gtx, x, y, colW, gap)
	} else {
		ly, fy = a.layoutLoginLink(gtx, x, y, colW, gap)
	}
	fx := x + colW + gap
	if l.err != "" {
		fy += gtx.Dp(10)
		fy += at(gtx, image.Pt(fx, fy), image.Pt(colW, gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Danger, MaxLines: 4}, l.err)
		}).Size.Y
	}
	// Switch between the two ways in.
	fy += gtx.Dp(18)
	tog := "Advanced: paste a token instead"
	if l.advanced {
		tog = "\u2190 Sign in with a Slack link instead"
	}
	tw, tth := th.Measure(gtx, 14, theme.Bold, tog), gtx.Dp(22)
	atExact(gtx, image.Pt(fx, fy), image.Pt(tw, tth), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &l.advToggle, nil, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 14, theme.Bold, lc.Link, tog)
		})
	})
	fy += tth
	content := rec.Stop()

	h := max(ly, fy) + pad
	left, top := (sz.X-w)/2, max(gtx.Dp(8), (sz.Y-h)/2)
	r := image.Rect(left, top, left+w, top+h)
	l.card = r
	theme.Shadow(gtx, r, gtx.Dp(12), gtx.Dp(12), theme.WithAlpha(lc.PanelShadow, 0x60))
	theme.RRect(gtx, r, gtx.Dp(12), lc.Bg)
	off := op.Offset(r.Min).Push(gtx.Ops)
	content.Add(gtx.Ops)
	off.Pop()
}

// startLogin runs the sign-in off the UI goroutine and shows its outcome on
// the card. in.Link set: desktop-style sign-in with that link.
func (a *App) startLogin(in slack.SignInInput) {
	l := a.login
	if l == nil || l.busy || a.opts.Login == nil {
		return
	}
	l.busy, l.err = true, ""
	go func() {
		err := a.opts.Login(in)
		a.Post(func() {
			l.busy = false
			if err != nil {
				l.err = err.Error()
				return
			}
			for _, ed := range []*widget.Editor{&l.link, &l.token, &l.orgToken, &l.cookie} {
				ed.SetText("")
			}
			if a.login == l {
				a.login = nil
			}
			if a.data != nil {
				a.data.start(a)
			}
		})
	}()
}

// SignInWithLink signs in with a slack:// magic-login link the OS handed
// Clack (its URL handler, a second launch forwarding its argument, or a
// macOS URL event). Safe from any goroutine, also before the window is up.
//
// It only signs in from the sign-in screen. While Clack is signed in, a
// link from outside doesn't replace the session: any web page can make the
// browser open a slack:// link, and one for someone else's account must not
// silently swap the account messages are sent from. The user signs out
// first (or pastes the link on the sign-in screen).
func (a *App) SignInWithLink(link string) {
	a.Post(func() {
		l := a.login
		if l == nil {
			a.Toast("Clack got a Slack sign-in link, but you're already signed in. To use it, sign out, then open or paste the link again.")
			return
		}
		if l.busy {
			return
		}
		l.advanced, l.focused = false, false
		l.link.SetText(link)
		a.startLogin(slack.SignInInput{Link: link})
		a.Invalidate()
	})
}

// loginPara draws wrapped text at (x, y) and returns its height.
func (a *App) loginPara(gtx layout.Context, x, y, w int, st theme.TextStyle, txt string) int {
	return at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, st, txt)
	}).Size.Y
}

// loginBox draws text in a tinted, bordered box and returns its height.
func (a *App) loginBox(gtx layout.Context, x, y, w int, bg, border color.NRGBA, st theme.TextStyle, txt string) int {
	ip := gtx.Dp(12)
	m := op.Record(gtx.Ops)
	th := a.loginPara(gtx, x+ip, y+ip, w-2*ip, st, txt)
	call := m.Stop()
	r := image.Rect(x, y, x+w, y+th+2*ip)
	theme.RRect(gtx, r, gtx.Dp(6), bg)
	theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), border)
	call.Add(gtx.Ops)
	return r.Dy()
}

// loginField draws a labelled single-line field and returns its height.
func (a *App) loginField(gtx layout.Context, x, y, w int, label string, ed *widget.Editor, hint string) int {
	th, lc := a.th, theme.LightPalette
	at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, lc.Fg, label)
	})
	y += gtx.Dp(26)
	fr := image.Rect(x, y, x+w, y+gtx.Dp(44))
	theme.RRect(gtx, fr, gtx.Dp(6), lc.Bg)
	bc := lc.BorderStrong
	if gtx.Focused(ed) {
		bc = lc.Link
	}
	theme.Border(gtx, fr, gtx.Dp(6), gtx.Dp(1), bc)
	at(gtx, image.Pt(x+gtx.Dp(12), y), image.Pt(w-gtx.Dp(24), gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, gtx.Dp(44), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if ed.Len() == 0 {
				th.Label(gtx, 15, theme.Regular, lc.FgSubtle, hint)
			}
			return th.Editor(gtx, ed, 15, lc.Fg, lc.Selection)
		})
	})
	return gtx.Dp(26 + 56)
}

// loginButton draws the card's primary button and returns its height.
func (a *App) loginButton(gtx layout.Context, x, y, w int, c *widget.Clickable, txt string) int {
	th, lc := a.th, theme.LightPalette
	br := image.Rect(x, y, x+w, y+gtx.Dp(44))
	atExact(gtx, br.Min, br.Size(), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, c, func(gtx layout.Context, s image.Point, hov bool) {
			bg := lc.ChromeA
			if hov {
				bg = lc.ChromeB
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			tw := th.Measure(gtx, 18, theme.Bold, txt)
			at(gtx, image.Pt((br.Dx()-tw)/2, 0), image.Pt(tw+2, br.Dy()), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, br.Dy(), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 18, theme.Bold, lc.SendText, txt)
				})
			})
			return layout.Dimensions{Size: br.Size()}
		})
	})
	return br.Dy()
}

// loginSmallButton draws an outlined secondary button and returns its size.
func (a *App) loginSmallButton(gtx layout.Context, x, y int, c *widget.Clickable, txt string) image.Point {
	th, lc := a.th, theme.LightPalette
	bw, bh := th.Measure(gtx, 14, theme.Bold, txt)+gtx.Dp(24), gtx.Dp(30)
	atExact(gtx, image.Pt(x, y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, c, func(gtx layout.Context, s image.Point, hov bool) {
			bg := lc.Bg
			if hov {
				bg = lc.CodeBg
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(6), gtx.Dp(1), lc.BorderStrong)
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 14, theme.Bold, lc.Fg, txt)
				})
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	return image.Pt(bw, bh)
}

var loginInfoBg, loginInfoBorder = color.NRGBA{R: 0xe8, G: 0xf5, B: 0xfa, A: 0xff}, color.NRGBA{R: 0x1d, G: 0x9b, B: 0xd1, A: 0xff}

// layoutLoginLink is the main path: steps on the left, the link field on
// the right. It returns the bottoms of both columns.
func (a *App) layoutLoginLink(gtx layout.Context, x, y, colW, gap int) (int, int) {
	l, lc := a.login, theme.LightPalette
	body := theme.TextStyle{Size: 14, LineHeight: 20, Color: lc.Fg}
	small := theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.FgSubtle}
	ly := y
	ly += a.loginBox(gtx, x, ly, colW, loginInfoBg, loginInfoBorder, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Fg},
		AppName+" signs in the way Slack's desktop app does: your browser hands it a one-time link, and Slack gives "+
			AppName+" a session of its own. Nothing is copied from your browser.") + gtx.Dp(20)
	ly += a.loginPara(gtx, x, ly, colW, body, "1. Open Slack's desktop sign-in page in your browser and sign in to your workspace there.") + gtx.Dp(8)
	ly += a.loginSmallButton(gtx, x, ly, &l.openSignIn, "Open sign-in page").Y + gtx.Dp(6)
	ly += a.loginPara(gtx, x, ly, colW, small, desktopSignInURL) + gtx.Dp(16)
	ly += a.loginPara(gtx, x, ly, colW, body, "2. When Slack offers to open the Slack app, either:") + gtx.Dp(6)
	ind := gtx.Dp(16)
	ly += a.loginPara(gtx, x+ind, ly, colW-ind, body, "• right-click its “Open Slack” link (or “launch the app”), choose Copy Link, and paste it here, or") + gtx.Dp(6)
	ly += a.loginPara(gtx, x+ind, ly, colW-ind, body, "• let the browser open "+AppName+". That only works if "+AppName+" handles slack:// links: run clack --register-url-handler once. If the Slack desktop app is installed too, the two take turns owning those links, so copying is simpler.") + gtx.Dp(16)
	ly += a.loginPara(gtx, x, ly, colW, body, "3. The link works once, for a few minutes. If it has expired, sign in on the page again for a new one.")

	fx, fy := x+colW+gap, y
	fy += a.loginField(gtx, fx, fy, colW, "Sign-in link", &l.link, "slack://…/magic-login/…")
	txt := "Sign in"
	if l.busy {
		txt = "Signing in…"
	}
	fy += a.loginButton(gtx, fx, fy, colW, &l.linkSubmit, txt)
	fy += gtx.Dp(12)
	fy += a.loginPara(gtx, fx, fy, colW, small, "The link is a secret until it's used: don't send it to anyone. Workspaces with single sign-on may send you back to the browser to finish.")
	return ly, fy
}

// layoutLoginPaste is Advanced: paste the browser session's token and
// cookie. It returns the bottoms of both columns.
func (a *App) layoutLoginPaste(gtx layout.Context, x, y, colW, gap int) (int, int) {
	l, lc := a.login, theme.LightPalette
	body := theme.TextStyle{Size: 14, LineHeight: 20, Color: lc.Fg}
	ly := y
	// The session-sharing warning comes first: this way Clack runs on the
	// browser's own session, and Slack (Enterprise Grid especially) can end
	// a session it sees used from somewhere unexpected, in every app at once.
	ly += a.loginBox(gtx, x, ly, colW, bannerWarn, color.NRGBA{R: 0xe8, G: 0xb3, B: 0x2c, A: 0xff}, theme.TextStyle{Size: 13, LineHeight: 18, Color: lc.Fg},
		"This way "+AppName+" shares the session of the browser you copy these from: it is not a separate sign-in. "+
			"If Slack sees that session used in a way it finds unusual, it can sign it out everywhere, browser included "+
			"(this has happened on Enterprise Grid). Prefer signing in with a Slack link.") + gtx.Dp(20)
	ly += a.loginPara(gtx, x, ly, colW, body, "1. Open Slack in the browser you're signed in to, press F12 and run this in the Console:") + gtx.Dp(8)
	ly += a.loginBox(gtx, x, ly, colW, lc.CodeBg, lc.CodeBorder, theme.TextStyle{Size: 13, LineHeight: 19, Color: lc.CodeFg, Wrap: text.WrapGraphemes}, signInSnippet) + gtx.Dp(8)
	copyTxt := "Copy snippet"
	if l.copied {
		copyTxt = "Copied"
	}
	ly += a.loginSmallButton(gtx, x, ly, &l.copy, copyTxt).Y + gtx.Dp(16)
	ly += a.loginPara(gtx, x, ly, colW, body, "2. Paste what it copied into Token. That fills in the org token and user agent for you.") + gtx.Dp(12)
	ly += a.loginPara(gtx, x, ly, colW, body, "3. The d cookie is HttpOnly, so the snippet can't read it. Copy its value from DevTools → Application → Cookies → https://app.slack.com. Leave d-s out: Clack doesn't use it.")

	fx, fy := x+colW+gap, y
	fy += a.loginField(gtx, fx, fy, colW, "Workspace", &l.workspace, "hackclub")
	fy += a.loginField(gtx, fx, fy, colW, "Token", &l.token, "xoxc-… or the copied JSON")
	fy += a.loginField(gtx, fx, fy, colW, "Org token (optional)", &l.orgToken, "filled in from the JSON")
	fy += a.loginField(gtx, fx, fy, colW, "d cookie", &l.cookie, "xoxd-…")
	fy += a.loginField(gtx, fx, fy, colW, "Browser user agent (optional)", &l.userAgent, "filled in from the JSON")
	txt := "Sign in"
	if l.busy {
		txt = "Signing in…"
	}
	fy += a.loginButton(gtx, fx, fy, colW, &l.submit, txt)
	return ly, fy
}
