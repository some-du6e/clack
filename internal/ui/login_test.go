package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func loginApp(t *testing.T, login func(slack.SignInInput) error) *App {
	t.Helper()
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.SetLogin(login)
	a.intro = nil // the card itself (intro_test.go has the introduction)
	return a
}

const testMagicLink = "slack://T08PSQ7BQ/magic-login/abc123"

// signInEnv is the sign-in card (no introduction) drawn through a real
// input.Router on a virtual clock. The browser is a list of the pages
// opened, and strs is every string the card drew in the last frame.
type signInEnv struct {
	*introEnv
	opened []string
	strs   []string
}

func newSignInEnv(t *testing.T, login func(slack.SignInInput) error, sz image.Point) *signInEnv {
	t.Helper()
	if login == nil {
		login = func(slack.SignInInput) error { return nil }
	}
	a := loginApp(t, login)
	a.motionOverride = -1 // full motion, whatever this machine's setting
	s := &signInEnv{introEnv: newIntroEnv(t, a, sz)}
	stubBrowser(t, &s.opened)
	prevOS := loginOS
	loginOS = "linux"
	t.Cleanup(func() { loginOS = prevOS })
	a.login.rec = &s.strs
	s.run(time.Second) // the field takes the focus and its ring settles (springSnappy: ~0.35 s)
	return s
}

// stubBrowser records the pages the card opens instead of opening them.
func stubBrowser(t *testing.T, opened *[]string) {
	prev := loginOpenURL
	var mu sync.Mutex
	loginOpenURL = func(u string) {
		mu.Lock()
		*opened = append(*opened, u)
		mu.Unlock()
	}
	t.Cleanup(func() { loginOpenURL = prev })
}

// drew reports whether the last frame drew a string containing sub.
func (s *signInEnv) drew(sub string) bool {
	for _, x := range s.strs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// motion is what the card's last frame asked for: frames at once
// (moving), or one later (wake after the frame; 0: none). It is the
// welcome screen's own motion clock, so the field's caret blink isn't in
// it.
func (s *signInEnv) motion() (moving bool, wake time.Duration) {
	m := s.a.welcome.mc
	if m.wake.IsZero() {
		return m.moving, 0
	}
	return m.moving, m.wake.Sub(m.now)
}

// typeIn types s into the focused editor the way Gio's window does: the
// edit, then the caret after it.
func (s *signInEnv) typeIn(text string) {
	n := len([]rune(text))
	s.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: text}, key.SelectionEvent{Start: n, End: n})
	s.frame(time.Second / 60)
	s.frame(time.Second / 60)
}

// The sign-in card fits the window in every state, from the default
// 1440x900 down to the 800x500 minimum, and (LOGIN_SHOTS_DIR set, in the
// login-shots workflow) is saved as screenshots.
func TestLoginCardFits(t *testing.T) {
	a := loginApp(t, func(slack.SignInInput) error { return nil })
	prevOS := loginOS
	loginOS = "linux"
	defer func() { loginOS = prevOS }()
	shots := os.Getenv("LOGIN_SHOTS_DIR")
	// small: also saved at the smaller sizes
	small := map[string]bool{"login-default": true, "login-long-error": true, "login-trouble": true, "login-advanced": true, "login-trouble-waiting-windows": true}
	states := []struct {
		name string
		set  func(l *loginScreen)
	}{
		{"login-default", func(l *loginScreen) {}},
		{"login-waiting", func(l *loginScreen) { l.waiting = true }},
		{"login-sso", func(l *loginScreen) { l.waiting, l.sso = true, true }},
		{"login-busy", func(l *loginScreen) { l.busy, l.busyLink = true, true }},
		{"login-error", func(l *loginScreen) { l.err = signInErrorText(fmt.Errorf("x: %w", slack.ErrMagicLinkUsed)) }},
		{"login-long-error", func(l *loginScreen) {
			l.err = signInErrorText(errors.New("Clack got its own Slack session, but only for the org, not a workspace. Sign in to a workspace in the browser and get a new link"))
		}},
		{"login-session-ended", func(l *loginScreen) { l.err = "Your Slack session ended. Sign in again." }},
		{"login-link-pasted", func(l *loginScreen) { l.link.SetText(testMagicLink); l.linkLen = l.link.Len() }},
		{"login-trouble", func(l *loginScreen) { l.trouble = true }},
		{"login-trouble-waiting", func(l *loginScreen) { l.trouble, l.waiting = true, true }},
		{"login-advanced", func(l *loginScreen) { l.advanced = true }},
		{"login-advanced-error", func(l *loginScreen) {
			l.advanced, l.err = true, signInErrorText(fmt.Errorf("x: %w", slack.ErrSessionRejected))
		}},
		{"login-advanced-busy", func(l *loginScreen) { l.advanced, l.busy = true, true }},
		{"login-advanced-tokens", func(l *loginScreen) { l.advanced, l.tokenForm = true, true }},
		{"login-advanced-filled", func(l *loginScreen) {
			l.advanced, l.tokenForm, l.pasteNote = true, true, "Filled the workspace, token and org token."
			l.setField(&l.workspace, "hackclub.slack.com", false)
			l.setField(&l.token, "xoxc-1111111111-2222222222-3333333333-aaaabbbb", false)
			l.setField(&l.orgToken, "xoxc-9999999999-8888888888-7777777777-eeeeffff", false)
		}},
		{"login-advanced-cookie-error", func(l *loginScreen) {
			l.advanced, l.err = true, signInErrorText(fmt.Errorf("x: %w", slack.ErrCookieRejected))
		}},
		// Windows' own words (pick Clack in Windows' box)
		{"login-waiting-windows", func(l *loginScreen) { loginOS = "windows"; l.waiting = true }},
		{"login-trouble-waiting-windows", func(l *loginScreen) { loginOS = "windows"; l.trouble, l.waiting = true, true }},
	}
	for _, sz := range []image.Point{{1440, 900}, {1280, 720}, {800, 500}} {
		var win *headless.Window
		if shots != "" {
			w, err := pixeltest.NewWindow(sz.X, sz.Y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			win = w
		}
		r := &ttRenderer{t: t, a: a, sz: sz, dir: shots} // for save
		clock := time.Now()
		for _, st := range states {
			a.login = newLoginScreen()
			loginOS = "linux"
			st.set(a.login)
			var img *image.RGBA
			for i := 0; i < 3; i++ {
				// two seconds a frame: every spring has settled, so the shots
				// are stills
				ops := new(op.Ops)
				a.drain()
				clock = clock.Add(2 * time.Second)
				g := ttContext(ops, sz)
				g.Now = clock
				a.frame(g)
				if win != nil && i == 2 && (sz.X == 1440 || small[st.name]) {
					if err := win.Frame(ops); err != nil {
						t.Fatalf("frame: %v", err)
					}
					img = image.NewRGBA(image.Rectangle{Max: sz})
					if err := win.Screenshot(img); err != nil {
						t.Fatalf("screenshot: %v", err)
					}
				}
			}
			c := a.login.card
			if c.Empty() || !c.In(image.Rectangle{Max: sz}) {
				t.Errorf("%v %s: card %v doesn't fit", sz, st.name, c)
			}
			if img != nil {
				name := st.name
				if sz.X != 1440 {
					name = fmt.Sprintf("%s-%dx%d", st.name, sz.X, sz.Y)
				}
				r.save(name, img, sz.Y)
			}
		}
		if win != nil {
			win.Release()
		}
	}
}

// The default card says little: about 25 words, the one primary action,
// the field as the second way in, the secret in a few words and the two
// small affordances. The tips and the token form wait behind them.
func TestSignInDefaultIsShort(t *testing.T) {
	s := newSignInEnv(t, nil, image.Pt(1440, 900))
	if n := loginWords(s.strs); n > 26 {
		t.Errorf("the default card shows %d words (at most about 25): %q", n, s.strs)
	}
	for _, want := range []string{signInTitle, "Open Slack sign-in", "Paste the slack:// link", "The link is a secret.", "Having trouble?", "Advanced"} {
		if !s.drew(want) {
			t.Errorf("the default card doesn't show %q: %q", want, s.strs)
		}
	}
	for _, hidden := range []string{"register-url-handler", "Single sign-on", "Paste a token", "d cookie", "F12"} {
		if s.drew(hidden) {
			t.Errorf("%q shows by default", hidden)
		}
	}
	// and at rest it asks for no frames
	if mv, wk := s.motion(); mv || wk != 0 {
		t.Errorf("the default card at rest: moving %v, next frame in %v", mv, wk)
	}
}

// Open Slack sign-in opens Slack desktop's sign-in page in the browser
// (also Enter in the empty field), and the card waits: "Waiting for
// Slack…" with the badge's calm pulse (about 30 frames a second, never
// "at once"). Esc stops waiting.
func TestSignInOpenAndWait(t *testing.T) {
	s := newSignInEnv(t, nil, image.Pt(1440, 900))
	l := s.a.login
	if !s.r.Source().Focused(&l.link) {
		t.Fatal("the link field doesn't have the focus")
	}
	s.key(key.NameReturn) // nothing pasted: Enter is the primary action
	if len(s.opened) != 1 || s.opened[0] != desktopSignInURL || !l.waiting {
		t.Fatalf("Enter: opened %q, waiting %v", s.opened, l.waiting)
	}
	s.run(1500 * time.Millisecond)
	if !s.drew("Waiting for Slack…") || !s.drew("Continue in your browser.") || !s.drew("Open the page again") {
		t.Errorf("waiting: %q", s.strs)
	}
	if s.drew("Open Slack sign-in") {
		t.Error("Open Slack sign-in is still drawn after it gave way")
	}
	// a calm loop: every frame asks for the next in ~33 ms, none at once
	for i := 0; i < 20; i++ {
		s.frame(50 * time.Millisecond)
		if mv, wk := s.motion(); mv || wk <= 0 || wk > 40*time.Millisecond {
			t.Fatalf("waiting, frame %d: moving %v, next frame in %v; want ~30 fps", i, mv, wk)
		}
	}
	s.key(key.NameEscape)
	s.run(time.Second)
	if l.waiting || !s.drew("Open Slack sign-in") {
		t.Errorf("Esc: waiting %v, %q", l.waiting, s.strs)
	}
	// the button opens it too
	l.openSignIn.Click()
	s.frame(time.Second / 60)
	if len(s.opened) != 2 || !l.waiting {
		t.Errorf("click: opened %q, waiting %v", s.opened, l.waiting)
	}
	l.reopen.Click()
	s.frame(time.Second / 60)
	if len(s.opened) != 3 || s.opened[2] != desktopSignInURL {
		t.Errorf("Open the page again: opened %q", s.opened)
	}
}

// Waiting, the card advances by itself when the link arrives from the
// browser: "Signing in…" with the badge turning, then the app.
func TestSignInAdvancesWhenLinkArrives(t *testing.T) {
	release := make(chan error)
	var mu sync.Mutex
	var got []slack.SignInInput
	s := newSignInEnv(t, func(in slack.SignInInput) error {
		mu.Lock()
		got = append(got, in)
		mu.Unlock()
		return <-release
	}, image.Pt(1440, 900))
	l := s.a.login
	l.openSignIn.Click()
	s.run(time.Second)
	s.a.SignInWithLink(testMagicLink)
	s.run(500 * time.Millisecond)
	if !l.busy || !s.drew("Signing in…") || !s.drew("Slack is giving Clack its own session.") {
		t.Fatalf("busy %v: %q", l.busy, s.strs)
	}
	if mv, _ := s.motion(); !mv {
		t.Error("signing in: the badge's spinner doesn't ask for frames")
	}
	// the controls are inert meanwhile
	l.openSignIn.Click()
	s.frame(time.Second / 60)
	if len(s.opened) != 1 {
		t.Errorf("Open Slack sign-in worked while signing in: %q", s.opened)
	}
	release <- nil
	waitFor(t, s.a, "the sign-in to finish", func() bool { return s.a.login == nil })
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Link != testMagicLink {
		t.Errorf("login got %+v", got)
	}
}

// Slack desktop's magicLoginOnFocusEpic: when the window comes to the
// front, a sign-in link on the clipboard signs in, and the clipboard is
// cleared (the link is a one-time secret). Anything else on the clipboard
// is left alone.
func TestSignInClipboardOnFocus(t *testing.T) {
	calls := make(chan slack.SignInInput, 4)
	s := newSignInEnv(t, func(in slack.SignInInput) error {
		calls <- in
		return errors.New("that sign-in link has expired")
	}, image.Pt(1440, 900))
	offer := func(text string) {
		t.Helper()
		s.a.loginWindowFocused()
		s.frame(time.Second / 60)
		if !s.r.ClipboardRequested() {
			t.Fatal("the window came to the front and the card didn't ask for the clipboard")
		}
		s.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(text)) }})
		s.frame(time.Second / 60)
		s.frame(time.Second / 60)
	}
	offer("a message I copied, https://example.com")
	if _, _, ok := s.r.WriteClipboard(); ok {
		t.Error("the clipboard was written though it held no sign-in link")
	}
	select {
	case in := <-calls:
		t.Fatalf("signed in with %+v", in)
	case <-time.After(30 * time.Millisecond):
	}
	if s.a.login.busy {
		t.Fatal("busy without a link")
	}
	offer("  " + testMagicLink + "\n")
	if mime, b, ok := s.r.WriteClipboard(); !ok || len(b) != 0 || mime != "application/text" {
		t.Errorf("after taking the link the clipboard holds %q (%v)", b, ok)
	}
	select {
	case in := <-calls:
		if in.Link != testMagicLink {
			t.Errorf("signed in with %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the link on the clipboard didn't sign in")
	}
	waitFor(t, s.a, "the failure", func() bool { return !s.a.login.busy })
	if s.a.login.link.Text() != testMagicLink {
		t.Error("the link isn't in the field to try again")
	}
}

// During the introduction a link on the clipboard skips to the card and
// signs in, as a link from the OS does.
func TestSignInClipboardDuringIntro(t *testing.T) {
	calls := make(chan slack.SignInInput, 1)
	a := introApp(t, "", func(in slack.SignInInput) error {
		calls <- in
		return errors.New("that sign-in link has expired")
	})
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.started()
	a.loginWindowFocused()
	e.frame(time.Second / 60)
	if !e.r.ClipboardRequested() {
		t.Fatal("no clipboard read during the introduction")
	}
	e.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(testMagicLink)) }})
	e.frame(time.Second / 60)
	e.frame(time.Second / 60)
	if a.intro == nil || !a.intro.signIn {
		t.Fatal("the introduction didn't step aside for the link")
	}
	select {
	case in := <-calls:
		if in.Link != testMagicLink {
			t.Errorf("signed in with %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no sign-in")
	}
}

// A whole link pasted into the empty field signs in at once; typing
// something else waits for Enter, which then says it isn't a link.
func TestSignInPasteSignsIn(t *testing.T) {
	calls := make(chan slack.SignInInput, 2)
	s := newSignInEnv(t, func(in slack.SignInInput) error {
		calls <- in
		return errors.New("that sign-in link has expired")
	}, image.Pt(1440, 900))
	l := s.a.login
	s.typeIn("https://slack.com/not-a-link")
	select {
	case in := <-calls:
		t.Fatalf("signed in with %+v", in)
	case <-time.After(30 * time.Millisecond):
	}
	s.run(500 * time.Millisecond)
	if l.link.Len() == 0 || s.drew("Paste the slack:// link") {
		t.Fatalf("nothing typed (%d runes, the hint drawn %v)", l.link.Len(), s.drew("Paste the slack:// link"))
	}
	s.key(key.NameReturn)
	if want := "That isn’t a Slack sign-in link."; l.err != want {
		t.Errorf("Enter: err %q, want %q", l.err, want)
	}
	if len(s.opened) != 0 {
		t.Errorf("Enter with text opened %q", s.opened)
	}
	l.link.SetText("")
	s.frame(time.Second / 60)
	s.typeIn(testMagicLink)
	select {
	case in := <-calls:
		if in.Link != testMagicLink {
			t.Errorf("signed in with %+v", in)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pasting the link didn't sign in")
	}
}

// Errors get a few words and the card shakes its head once (reduced
// motion: no shake); a used link takes the card back from waiting.
func TestSignInErrorShortCopy(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{slack.ErrNotMagicLink, "That isn’t a Slack sign-in link."},
		{fmt.Errorf("that sign-in link has expired or was already used (invalid_magic_token): %w", slack.ErrMagicLinkUsed), "That link has expired. Open Slack sign-in again."},
		{fmt.Errorf("couldn't reach Slack to finish signing in: %w", slack.ErrSlackUnreachable), "Couldn’t reach Slack. Check your connection."},
		{fmt.Errorf("Slack rejected the token and cookie: %w", slack.ErrSessionRejected), "Slack didn’t accept that token and cookie."},
		{&slack.MagicRedirectError{}, "Finish signing in in your browser, then use the new link."},
		{errors.New("the token should start with xoxc-"), "The token should start with xoxc-"},
		{errors.New("Slack accepted the link but didn't give Clack a session cookie; get a new link and try again"), "Slack accepted the link but didn't give Clack a session cookie; get a new link and try again."},
	}
	for _, c := range cases {
		if got := signInErrorText(c.err); got != c.want {
			t.Errorf("%v: %q, want %q", c.err, got, c.want)
		}
	}

	for _, reduced := range []bool{false, true} {
		release := make(chan error, 1)
		s := newSignInEnv(t, func(slack.SignInInput) error { return <-release }, image.Pt(1440, 900))
		if reduced {
			s.a.motionOverride = 1
		}
		l := s.a.login
		l.openSignIn.Click()
		s.run(500 * time.Millisecond)
		s.a.SignInWithLink(testMagicLink)
		s.run(200 * time.Millisecond)
		release <- cases[1].err
		waitFor(t, s.a, "the failure", func() bool { return !l.busy })
		x0 := l.card.Min.X
		moved := false
		for i := 0; i < 30; i++ {
			s.frame(time.Second / 60)
			if l.card.Min.X != x0 {
				t.Fatalf("the card's box moved (the shake is a transform): %v", l.card)
			}
			if mv, _ := s.motion(); mv {
				moved = true
			}
		}
		if l.err != cases[1].want || l.waiting || l.errAt.IsZero() || !s.drew(cases[1].want) {
			t.Errorf("reduced %v: err %q (at %v), waiting %v, %q", reduced, l.err, l.errAt, l.waiting, s.strs)
		}
		if !moved {
			t.Errorf("reduced %v: the error came in without motion", reduced)
		}
		s.run(2 * time.Second)
		if mv, wk := s.motion(); mv || wk != 0 {
			t.Errorf("reduced %v: after the shake, moving %v, next frame in %v", reduced, mv, wk)
		}
	}
}

// Single sign-on: like Slack desktop, the workspace's https Slack
// auth_redir opens in the browser and the card waits for the next link.
func TestSignInSSORedirect(t *testing.T) {
	sso := "https://hackclub.slack.com/sso/saml/start"
	s := newSignInEnv(t, func(slack.SignInInput) error { return &slack.MagicRedirectError{URL: sso} }, image.Pt(1440, 900))
	l := s.a.login
	s.a.SignInWithLink(testMagicLink)
	s.frame(time.Second / 60)
	waitFor(t, s.a, "the redirect", func() bool { return !l.busy })
	s.run(time.Second)
	if len(s.opened) != 1 || s.opened[0] != sso || !l.waiting || !l.sso || l.err != "" {
		t.Fatalf("opened %q, waiting %v sso %v, err %q", s.opened, l.waiting, l.sso, l.err)
	}
	if !s.drew("Waiting for Slack…") || !s.drew("Finish single sign-on in your browser.") {
		t.Errorf("%q", s.strs)
	}
	// a redirect off Slack isn't opened (slack.LoginMagic clears its URL)
	s2 := newSignInEnv(t, func(slack.SignInInput) error { return &slack.MagicRedirectError{} }, image.Pt(1440, 900))
	s2.a.SignInWithLink(testMagicLink)
	s2.frame(time.Second / 60)
	waitFor(t, s2.a, "the redirect", func() bool { return !s2.a.login.busy })
	if len(s2.opened) != 0 || s2.a.login.waiting || s2.a.login.err == "" {
		t.Errorf("no URL: opened %q, waiting %v, err %q", s2.opened, s2.a.login.waiting, s2.a.login.err)
	}
}

// On Windows the waiting card says to pick Clack if Windows asks which
// app opens the link, and Having trouble? keeps to what is known to work
// there: Windows' box and Copy page link. No tip about copying the link
// (Slack's page never shows it) and none about -register-url-handler
// (every start registers Clack). Open Slack sign-in runs
// EnsureLinkHandler before the page opens.
func TestSignInWindowsWords(t *testing.T) {
	s := newSignInEnv(t, nil, image.Pt(1440, 900))
	loginOS = "windows" // newSignInEnv's cleanup puts it back
	l := s.a.login
	ensured := 0
	s.a.opts.EnsureLinkHandler = func() { ensured++ }
	s.key(key.NameReturn) // nothing pasted: Open Slack sign-in
	if ensured != 1 || len(s.opened) != 1 || !l.waiting {
		t.Fatalf("Open Slack sign-in: EnsureLinkHandler ran %d times, opened %q, waiting %v", ensured, s.opened, l.waiting)
	}
	s.run(1500 * time.Millisecond)
	if !s.drew("Continue in your browser. If Windows asks, pick Clack.") {
		t.Errorf("waiting on Windows: %q", s.strs)
	}
	l.troubleBtn.Click()
	s.frame(time.Second / 60)
	s.run(1500 * time.Millisecond)
	for _, want := range []string{"Browser didn’t open?", "Copy page link", "If Windows asks for an app, pick Clack, then Just once or Always."} {
		if !s.drew(want) {
			t.Errorf("Windows tips: no %q in %q", want, s.strs)
		}
	}
	for _, gone := range []string{"register-url-handler", "Copied the slack:// link?", "F12", "Console", "external handler"} {
		if s.drew(gone) {
			t.Errorf("Windows tips show %q: %q", gone, s.strs)
		}
	}
	if !l.card.In(image.Rect(0, 0, 1440, 900)) {
		t.Errorf("the card %v leaves the window", l.card)
	}
}

// Having trouble? opens under the footer on a spring (the card grows,
// downward, and the tips come in) with this OS's link-handler tip; Esc
// closes it. Advanced opens the token form in the card's place, takes the
// focus to Token, keeps its checks, and Esc comes back to the link.
func TestSignInDisclosures(t *testing.T) {
	s := newSignInEnv(t, nil, image.Pt(1440, 900))
	l := s.a.login
	h0 := l.card.Dy()
	l.troubleBtn.Click()
	s.frame(time.Second / 60)
	s.frame(time.Second / 60)
	h1 := l.card.Dy()
	s.run(1500 * time.Millisecond)
	h2 := l.card.Dy()
	if !(h0 < h1 && h1 < h2) {
		t.Errorf("Having trouble? opened %d → %d → %d px, want a growth over frames", h0, h1, h2)
	}
	if !l.card.In(image.Rect(0, 0, 1440, 900)) {
		t.Errorf("open, the card %v leaves the window", l.card)
	}
	for _, want := range []string{"Browser didn’t open?", "Copy page link", "choose Clack", "clack --register-url-handler", "Copied the slack:// link?", "Single sign-on"} {
		if !s.drew(want) {
			t.Errorf("tips: no %q in %q", want, s.strs)
		}
	}
	l.copyPage.Click()
	s.frame(time.Second / 60)
	if mime, b, ok := s.r.WriteClipboard(); !ok || string(b) != desktopSignInURL || mime != "application/text" {
		t.Errorf("Copy page link copied %q", b)
	}
	s.key(key.NameEscape)
	s.run(1500 * time.Millisecond)
	if l.trouble || l.card.Dy() != h0 {
		t.Errorf("Esc: open %v, %d px (was %d)", l.trouble, l.card.Dy(), h0)
	}

	l.advToggle.Click()
	s.frame(time.Second / 60)
	s.run(1500 * time.Millisecond)
	if !l.advanced || !s.drew("Sign in with a cookie") || !s.drew("d cookie") || !s.drew("Use a token instead") || s.drew("Open Slack sign-in") || s.drew("Org token") {
		t.Fatalf("Advanced: %v, %q", l.advanced, s.strs)
	}
	if !s.r.Source().Focused(&l.cookie) {
		t.Error("Advanced didn't take the focus to d cookie")
	}
	if !l.card.In(image.Rect(0, 0, 1440, 900)) {
		t.Errorf("Advanced: the card %v leaves the window", l.card)
	}
	s.key(key.NameReturn) // nothing pasted yet
	if l.err != "Paste the d cookie first." {
		t.Errorf("Advanced, empty: err %q", l.err)
	}
	s.key(key.NameEscape)
	s.run(1500 * time.Millisecond)
	if l.advanced || l.err != "" || !s.drew("Open Slack sign-in") {
		t.Errorf("Esc from Advanced: %v, err %q", l.advanced, l.err)
	}
	if !s.r.Source().Focused(&l.link) {
		t.Error("back from Advanced, the link field doesn't have the focus")
	}
	if mv, wk := s.motion(); mv || wk != 0 {
		t.Errorf("after the disclosures: moving %v, next frame in %v", mv, wk)
	}
}

// Reduced motion: the card's pieces only fade while it waits, opens Having
// trouble? and Advanced and comes back (no offset, scale or blur).
func TestSignInReducedMotion(t *testing.T) {
	for _, reduced := range []bool{true, false} {
		s := newSignInEnv(t, nil, image.Pt(1440, 900))
		if reduced {
			s.a.motionOverride = 1
		}
		var rec []introDrawn
		s.a.welcome.rec = &rec
		l := s.a.login
		moved := 0
		look := func(d time.Duration) {
			for t0 := time.Duration(0); t0 < d; t0 += time.Second / 30 {
				rec = rec[:0]
				s.frame(time.Second / 30)
				for _, x := range rec {
					if x.dx != 0 || x.dy != 0 || (x.s != 1 && x.s != 0) || x.blur != 0 {
						moved++
						if reduced && moved < 5 {
							t.Errorf("reduced motion: %s moved (dx %.1f dy %.1f scale %.3f blur %.2f)", x.name, x.dx, x.dy, x.s, x.blur)
						}
					}
				}
			}
		}
		l.openSignIn.Click()
		look(time.Second)
		l.troubleBtn.Click()
		look(time.Second)
		l.advToggle.Click()
		look(time.Second)
		s.key(key.NameEscape)
		look(time.Second)
		if !reduced && moved == 0 {
			t.Error("with motion on, nothing on the card moved")
		}
	}
}

// A link the OS hands Clack (URL handler, second launch, macOS URL event)
// signs in from the sign-in screen, as a desktop sign-in.
func TestSignInWithLink(t *testing.T) {
	var (
		mu  sync.Mutex
		got []slack.SignInInput
	)
	done := make(chan struct{}, 1)
	a := loginApp(t, func(in slack.SignInInput) error {
		mu.Lock()
		got = append(got, in)
		mu.Unlock()
		done <- struct{}{}
		return errors.New("that sign-in link has expired")
	})
	link := testMagicLink
	a.SignInWithLink(link)
	a.drain()
	if !a.login.busy {
		t.Fatal("not signing in")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("login not called")
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.login.busy && time.Now().Before(deadline) {
		a.drain()
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if len(got) != 1 || got[0].Link != link || got[0].Token != "" || got[0].Cookie != "" {
		t.Errorf("login got %v", got)
	}
	mu.Unlock()
	if !strings.Contains(a.login.err, "expired") || a.login.link.Text() != link {
		t.Errorf("after a failure: err %q, link kept %v", a.login.err, a.login.link.Text() == link)
	}

	// Signed in: a link from outside never swaps the session.
	a.login = nil
	a.SignInWithLink(link)
	a.drain()
	if !strings.Contains(a.toast.text, "already signed in") {
		t.Errorf("toast %q", a.toast.text)
	}
	select {
	case <-done:
		t.Error("signed in over a live session")
	case <-time.After(50 * time.Millisecond):
	}
	// …and neither does one on the clipboard
	a.loginWindowFocused()
	if a.login != nil {
		t.Error("focus made a sign-in screen")
	}
}

// Success clears the secrets and leaves the sign-in screen.
func TestStartLoginSuccess(t *testing.T) {
	a := loginApp(t, func(slack.SignInInput) error { return nil })
	a.login.link.SetText(testMagicLink)
	l := a.login
	a.startLogin(slack.SignInInput{Link: l.link.Text()})
	deadline := time.Now().Add(5 * time.Second)
	for a.login != nil && time.Now().Before(deadline) {
		a.drain()
		time.Sleep(5 * time.Millisecond)
	}
	if a.login != nil || l.link.Text() != "" {
		t.Errorf("login screen %v, link left %q", a.login != nil, l.link.Text())
	}
}

// TestSignInVideo renders the sign-in card's states headless at 60 fps
// (the login-shots workflow's intro-video job, INTRO_VIDEO_DIR): the card
// as the introduction hands over to it, Open Slack sign-in and the
// waiting badge, the link arriving and failing (the shake and its few
// words), Having trouble? opening, Advanced with a token typed and its
// error, and back; then the same with reduced motion. Key frames are
// saved as PNGs.
func TestSignInVideo(t *testing.T) {
	dir := os.Getenv("INTRO_VIDEO_DIR")
	if dir == "" {
		t.Skip("INTRO_VIDEO_DIR not set: the login-shots workflow's intro-video job renders this")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("ffmpeg: ", err)
	}
	sz := image.Pt(1440, 900)
	win, err := pixeltest.NewWindow(sz.X, sz.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	var opened []string
	stubBrowser(t, &opened)
	prevOS := loginOS
	loginOS = "linux"
	defer func() { loginOS = prevOS }()
	press := func(e *introEnv, k key.Name) {
		e.r.Queue(key.Event{Name: k, State: key.Press}, key.Event{Name: k, State: key.Release})
	}
	typeIn := func(e *introEnv, text string) {
		n := len([]rune(text))
		e.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: text}, key.SelectionEvent{Start: n, End: n})
	}
	// failWith hands the waiting login its answer and waits (in real time)
	// until the answer is posted to the UI.
	failWith := func(a *App, release chan error, err error) {
		release <- err
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a.mu.Lock()
			n := len(a.pending)
			a.mu.Unlock()
			if n > 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("the sign-in never answered")
	}
	used := fmt.Errorf("that sign-in link has expired or was already used (invalid_magic_token): %w", slack.ErrMagicLinkUsed)

	release := make(chan error, 1)
	a := introApp(t, "", func(slack.SignInInput) error { return <-release })
	e := newIntroEnv(t, a, sz)
	e.started()
	recordVideo(t, dir, win, sz, "signin", 60, e, func(step func(time.Duration), kf func(string)) {
		step(500 * time.Millisecond)
		press(e, key.NameEscape) // the introduction skips to the card
		step(450 * time.Millisecond)
		kf("entering")
		step(1600 * time.Millisecond)
		kf("default")
		press(e, key.NameReturn) // Open Slack sign-in (nothing pasted: Enter)
		step(300 * time.Millisecond)
		kf("opening")
		step(1500 * time.Millisecond)
		kf("waiting")
		step(1300 * time.Millisecond)
		kf("waiting-pulse")
		a.SignInWithLink(testMagicLink) // the browser hands the link over
		step(700 * time.Millisecond)
		kf("signing-in")
		failWith(a, release, used)
		step(130 * time.Millisecond)
		kf("shake")
		step(1500 * time.Millisecond)
		kf("error")
		a.login.troubleBtn.Click()
		step(220 * time.Millisecond)
		kf("trouble-opening")
		step(1300 * time.Millisecond)
		kf("trouble")
		press(e, key.NameEscape)
		step(1000 * time.Millisecond)
		a.login.advToggle.Click()
		step(240 * time.Millisecond)
		kf("advanced-opening")
		step(1300 * time.Millisecond)
		kf("advanced")
		typeIn(e, "xoxc-1234567890-demo")
		step(500 * time.Millisecond)
		kf("advanced-typed")
		press(e, key.NameReturn) // no d cookie yet
		step(130 * time.Millisecond)
		kf("advanced-shake")
		step(1400 * time.Millisecond)
		kf("advanced-error")
		press(e, key.NameEscape)
		step(260 * time.Millisecond)
		kf("back-opening")
		step(1400 * time.Millisecond)
		kf("back")
	})

	// reduced motion: the same states, fades only
	release2 := make(chan error, 1)
	b := loginApp(t, func(slack.SignInInput) error { return <-release2 })
	b.motionOverride = 1
	e = newIntroEnv(t, b, sz)
	recordVideo(t, dir, win, sz, "signin-reduced", 30, e, func(step func(time.Duration), kf func(string)) {
		step(time.Second)
		kf("default")
		b.login.openSignIn.Click()
		step(400 * time.Millisecond)
		kf("waiting-fading")
		step(1600 * time.Millisecond)
		kf("waiting")
		b.SignInWithLink(testMagicLink)
		step(600 * time.Millisecond)
		kf("signing-in")
		failWith(b, release2, used)
		step(1500 * time.Millisecond)
		kf("error")
		b.login.troubleBtn.Click()
		step(1500 * time.Millisecond)
		kf("trouble")
		b.login.troubleBtn.Click()
		step(800 * time.Millisecond)
		b.login.advToggle.Click()
		step(1500 * time.Millisecond)
		kf("advanced")
	})
}
