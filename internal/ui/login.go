package ui

import (
	"errors"
	"image"
	"io"
	"log"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/slack"
)

// loginScreen is the sign-in card shown while Clack has no session
// (login_card.go draws it).
//
// The main path is desktop-style sign-in, done the way Slack desktop does
// it. "Open Slack sign-in" opens Slack's sign-in page in the user's
// browser and the card waits. Signing in there ends with Slack's page
// handing "the Slack app" a one-time slack:// link, which reaches Clack in
// one of three ways: the OS opens Clack with it (App.SignInWithLink), the
// user copies it and Clack takes it from the clipboard when its window
// comes to the front (loginClipboard), or it is pasted into the field.
// slack.LoginMagic trades it for a session of Clack's own.
//
// Advanced is the older path, on the browser's own session: paste its d
// cookie, and slack.SignIn fetches the session's tokens from Slack's
// token page (the desktop sign-in's own step) and checks the workspace
// token with auth.test; or, behind "Use a token instead", paste the token
// too. Either way Clack then shares the browser's session.
type loginScreen struct {
	link       widget.Editor
	linkSubmit widget.Clickable // the field's ↵ button
	openSignIn widget.Clickable // the primary action
	reopen     widget.Clickable // waiting: open the page again
	troubleBtn widget.Clickable // "Having trouble?"
	advToggle  widget.Clickable // "Advanced"
	advBack    widget.Clickable // Advanced's back button
	copyPage   widget.Clickable // Having trouble? > Copy page link
	pageCopied bool

	workspace widget.Editor
	token     widget.Editor
	orgToken  widget.Editor
	cookie    widget.Editor
	submit    widget.Clickable
	copy      widget.Clickable // the console snippet
	copied    bool
	tokenBtn  widget.Clickable // Advanced's "Use a token instead"
	tokenForm bool             // it is open: the snippet and the token fields

	advanced bool // the token form
	trouble  bool // "Having trouble?" is open
	waiting  bool // the sign-in page was opened: waiting for the link
	sso      bool // waiting for single sign-on to finish in the browser
	busy     bool
	busyLink bool // signing in with a link (not the token form)
	err      string
	focused  bool

	clipAsk bool // look at the clipboard at the next frame (the window came to the front)
	clipTag int
	linkLen int // the link field's length after the last frame

	// smart paste (login_paste.go)
	pasteTag  int                          // Ctrl/⌘+V on the card: its clipboard read
	prevText  map[*widget.Editor]string    // each field's text after its last change
	filled    map[*widget.Editor]time.Time // fields a paste filled: their ticks
	orgURL    string                       // the org's address, from a pasted teams JSON
	pasteNote string                       // what the last paste filled, said under the fields
	pasteAt   time.Time
	focusTo   *widget.Editor // the field to focus next (nil: the view's first)

	card image.Rectangle // where the card was last drawn (tests)

	// motion (motion.go): the card comes in after the introduction, its
	// parts staggered; the title and the line under it cross-fade between
	// states; Having trouble? and Advanced open on springs; the card
	// shakes once at a new error; fields ring when focused; buttons press
	// in; the badge pulses while the card waits
	now      time.Time // this frame's
	mc       *motionClock
	reduced  bool
	shownErr string    // the error the card last showed
	errAt    time.Time // when it appeared (zero: it was there from the start)
	busyAt   time.Time // when "Signing in…" started
	waitAt   time.Time // when the waiting started (zero: at the next frame)
	drawn    bool      // the card has been on screen
	rings    map[any]*springVal
	presses  map[*widget.Clickable]*pressAnim
	advV     springVal // 0 the link view … 1 Advanced
	troubleV springVal // Having trouble? 0 closed … 1 open
	errV     springVal // the badge's rose while an error shows
	sendV    springVal // the field's ↵ button popping in (springPop: it overshoots)
	waitV    springVal // Open Slack sign-in giving way to "Open the page again"
	tokenV   springVal // Advanced's token fields opening
	dimV     springVal // the controls dimming while Clack signs in
	keyV     springVal // the badge's globe turning into a key while it does
	subH     springVal // the height of the line under the title
	title    loginFade
	sub      loginFade

	rec *[]string // tests: every string drawn this frame
}

// desktopSignInURL is the page Slack desktop opens in the browser for
// "Sign in to Another Workspace" (main.bundle.cjs @1033352, also in
// docs/DESKTOP-SIGNIN.md). Signing in there ends with Slack offering to
// open the Slack app through a slack:// link, the link Clack wants. Clack
// only opens it in the user's browser, on a click; it never loads it.
var desktopSignInURL = "https://slack.com/ssb/add?s=1&v=" + slack.SlackDesktopVersion

// signInSnippet runs in the DevTools console of the user's own signed-in Slack
// tab. It copies the teams map Slack's page keeps in localStorage (workspace
// and org tokens, URLs); SignIn reads that JSON from the Token field. It sends
// nothing anywhere. No user agent: Clack sends only its own (slack.UserAgent).
const signInSnippet = `copy(JSON.stringify({teams: JSON.parse(localStorage.localConfig_v2).teams}))`

// loginOpenURL opens a page in the user's browser (tests record it).
var loginOpenURL = openURL

// loginOS picks the link-handler tip under Having trouble? (tests set it).
var loginOS = runtime.GOOS

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
	return l
}

// updateLogin reads the card's buttons, fields and keys and moves its
// state along. It runs before anything on the card is drawn: a Clickable
// drops the clicks nobody asked about once it is laid out.
func (a *App) updateLogin(gtx layout.Context) {
	l := a.login
	if !l.focused {
		var tag any = &l.link
		if l.advanced {
			tag = &l.cookie // the d cookie is all Advanced needs
			if l.focusTo != nil {
				tag = l.focusTo
			}
		}
		l.focusTo = nil
		gtx.Execute(key.FocusCmd{Tag: tag})
		l.focused = true
	}
	a.loginPasteKeys(gtx)
	open := l.openSignIn.Clicked(gtx)
	if l.reopen.Clicked(gtx) {
		open = true
	}
	if l.troubleBtn.Clicked(gtx) && !l.busy {
		l.trouble = !l.trouble
	}
	if (l.advToggle.Clicked(gtx) || l.advBack.Clicked(gtx)) && !l.busy {
		l.advanced, l.focused, l.err, l.trouble = !l.advanced, false, "", false
		l.tokenForm, l.pasteNote = false, ""
	}
	if l.tokenBtn.Clicked(gtx) && !l.busy {
		l.tokenForm = !l.tokenForm
		if l.tokenForm {
			l.focusTo, l.focused = &l.token, false
		}
	}
	if l.copyPage.Clicked(gtx) {
		loginWriteClipboard(gtx, desktopSignInURL)
		l.pageCopied = true
	}
	if l.copy.Clicked(gtx) {
		// Written here, not with a.copyText: frame returns before keys.process
		// (which flushes a.clipboard) while the sign-in screen is up.
		loginWriteClipboard(gtx, signInSnippet)
		l.copied = true
	}
	submit := l.submit.Clicked(gtx)
	linkSubmit := l.linkSubmit.Clicked(gtx)
	for {
		e, ok := l.link.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			a.loginFieldPaste(gtx, &l.link) // a token or the JSON pasted here moves to Advanced
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			if strings.TrimSpace(l.link.Text()) == "" {
				// Enter with nothing pasted is the primary action (its ↵)
				open = open || !l.waiting
			} else {
				linkSubmit = true
			}
		}
	}
	// a whole link pasted into the empty field signs in right away
	if n := l.link.Len(); n != l.linkLen {
		if l.linkLen == 0 && slack.IsMagicLink(l.link.Text()) {
			linkSubmit = true
		}
		l.linkLen = n
	}
	for _, ed := range []*widget.Editor{&l.workspace, &l.token, &l.orgToken, &l.cookie} {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := e.(widget.ChangeEvent); ok && !l.busy {
				a.loginFieldPaste(gtx, ed) // a paste that belongs elsewhere moves there
			}
			if _, ok := e.(widget.SubmitEvent); ok {
				submit = true
			}
		}
	}
	esc := false
	for {
		e, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			esc = true
		}
	}
	switch {
	case l.busy:
	case esc:
		// Esc backs out one step: the tips, Advanced, the waiting
		switch {
		case l.trouble:
			l.trouble = false
		case l.advanced:
			l.advanced, l.focused, l.err = false, false, ""
			l.tokenForm, l.pasteNote = false, ""
		case l.waiting:
			l.waiting, l.sso = false, false
		}
	case open && !l.advanced:
		// Slack desktop's "Sign in to Another Workspace": the page opens in
		// the browser (shell.openExternal), and the link comes back later,
		// to whichever app the OS hands slack:// links to
		if a.opts.EnsureLinkHandler != nil {
			a.opts.EnsureLinkHandler()
		}
		loginOpenURL(desktopSignInURL)
		l.waiting, l.sso, l.waitAt, l.err = true, false, l.now, ""
	case linkSubmit && !l.advanced:
		if link := strings.TrimSpace(l.link.Text()); !slack.IsMagicLink(link) {
			l.err = signInErrorText(slack.ErrNotMagicLink)
		} else {
			a.startLogin(slack.SignInInput{Link: link})
		}
	case submit && l.advanced:
		a.loginSubmitTokens()
	}
	if l.err != l.shownErr {
		// a new error fades in, and the card shakes its head once; one it
		// opened with ("Your Slack session ended") just shows
		l.shownErr, l.errAt = l.err, time.Time{}
		if l.drawn {
			l.errAt = l.now
		}
	}
	if l.busy && l.busyAt.IsZero() {
		l.busyAt = l.now
	} else if !l.busy {
		l.busyAt = time.Time{}
	}
	if l.waiting && l.waitAt.IsZero() {
		l.waitAt = l.now
	}
}

// loginWriteClipboard puts s on the clipboard.
func loginWriteClipboard(gtx layout.Context, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// loginWindowFocused is Slack desktop's magicLoginOnFocusEpic
// (main.bundle.cjs @954278): when its window comes to the front, Slack
// desktop looks at the clipboard for a sign-in link. Clack does it on the
// sign-in screen only (a link never replaces a live session, see
// SignInWithLink).
func (a *App) loginWindowFocused() {
	if l := a.login; l != nil && !l.busy {
		l.clipAsk = true
		a.Invalidate()
	}
}

// loginClipboard asks for the clipboard once the window has come to the
// front and signs in with a sign-in link it finds there. As in Slack
// desktop (c4/c7, c8 @935978), clipboard text that is a whole
// magic-login or login-v2 link signs in, and the clipboard is cleared once
// the link is taken: it is a one-time secret. Smart paste
// (login_paste.go) takes the token form's credentials the same way.
// Anything else is left on the clipboard, and nothing read is kept. It
// runs every frame while the sign-in screen is up, the introduction
// included.
func (a *App) loginClipboard(gtx layout.Context) {
	l := a.login
	if l == nil {
		return
	}
	for {
		e, ok := gtx.Event(transfer.TargetFilter{Target: &l.clipTag, Type: "application/text"})
		if !ok {
			break
		}
		s, ok := readPaste(e)
		if !ok || l.busy {
			continue
		}
		a.loginTake(gtx, slack.ClassifyPaste(s, strings.TrimSpace(l.workspace.Text())), nil)
	}
	if l.clipAsk {
		l.clipAsk = false
		gtx.Execute(clipboard.ReadCmd{Tag: &l.clipTag})
	}
}

// startLogin runs the sign-in off the UI goroutine and shows its outcome on
// the card. in.Link set: desktop-style sign-in with that link.
func (a *App) startLogin(in slack.SignInInput) {
	l := a.login
	if l == nil || l.busy || a.opts.Login == nil {
		return
	}
	l.busy, l.busyLink, l.err = true, in.Link != "", ""
	go func() {
		err := a.opts.Login(in)
		a.Post(func() {
			l.busy = false
			if err != nil {
				log.Printf("sign-in: %v", err)
				var re *slack.MagicRedirectError
				if errors.As(err, &re) && re.URL != "" && in.Link != "" {
					// single sign-on: like Slack desktop, open the
					// workspace's auth_redir (an https Slack page) in the
					// browser and wait for the link that comes after it
					// (MAGIC_LOGIN_REDIRECT, main.bundle.cjs @952867)
					loginOpenURL(re.URL)
					l.waiting, l.sso, l.waitAt = true, true, time.Time{}
					return
				}
				l.waiting, l.sso = false, false
				l.err = signInErrorText(err)
				return
			}
			for _, ed := range []*widget.Editor{&l.link, &l.token, &l.orgToken, &l.cookie} {
				ed.SetText("")
			}
			l.orgURL, l.prevText, l.pasteNote = "", nil, ""
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
		if a.login == nil {
			log.Printf("sign-in: a sign-in link came from outside Clack while signed in; not used")
			a.Toast("Clack got a Slack sign-in link, but you're already signed in. To use it, sign out, then open or paste the link again.")
			return
		}
		log.Printf("sign-in: a sign-in link came from outside Clack; signing in with it")
		a.signInWithLinkNow(link)
		a.Invalidate()
	})
}

// signInWithLinkNow signs in with a link that reached the sign-in screen
// from outside the card (the OS, the clipboard). UI goroutine.
func (a *App) signInWithLinkNow(link string) {
	l := a.login
	if l == nil || l.busy {
		return
	}
	l.advanced, l.focused, l.trouble = false, false, false
	l.link.SetText(link)
	l.linkLen = l.link.Len() // set here: not a paste to sign in with again
	l.link.SetCaret(l.linkLen, l.linkLen)
	a.introToSignIn() // the introduction steps aside for the card
	a.startLogin(slack.SignInInput{Link: link})
}

// signInErrorText is the card's few words for a failed sign-in (the whole
// error is in the log). Errors without short words show their own text.
func signInErrorText(err error) string {
	var re *slack.MagicRedirectError
	switch {
	case errors.Is(err, slack.ErrNotMagicLink):
		return "That isn’t a Slack sign-in link."
	case errors.Is(err, slack.ErrMagicLinkUsed):
		return "That link has expired. Open Slack sign-in again."
	case errors.Is(err, slack.ErrSlackUnreachable):
		return "Couldn’t reach Slack. Check your connection."
	case errors.Is(err, slack.ErrCookieRejected):
		return "Slack didn’t accept that d cookie."
	case errors.Is(err, slack.ErrSessionRejected):
		return "Slack didn’t accept that token and cookie."
	case errors.As(err, &re):
		return "Finish signing in in your browser, then use the new link."
	}
	s := strings.TrimSpace(err.Error())
	if r, n := utf8.DecodeRuneInString(s); n > 0 && unicode.IsLower(r) {
		s = string(unicode.ToUpper(r)) + s[n:]
	}
	if r, _ := utf8.DecodeLastRuneInString(s); unicode.IsLetter(r) || unicode.IsDigit(r) {
		s += "." // a sentence on the card ("…get a new link")
	}
	return s
}
