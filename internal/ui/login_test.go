package ui

import (
	"context"
	"errors"
	"image"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
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

// The sign-in card fits the default 1440x900 window in both modes, with an
// error showing, and (LOGIN_SHOTS_DIR set, in the login-shots workflow) is
// saved as screenshots.
func TestLoginCardFits(t *testing.T) {
	a := loginApp(t, func(slack.SignInInput) error { return nil })
	sz := image.Pt(1440, 900)
	shots := os.Getenv("LOGIN_SHOTS_DIR")
	var win *headless.Window
	if shots != "" {
		w, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		win = w
		defer w.Release()
	}
	clock := time.Now()
	frame := func() *image.RGBA {
		ops := new(op.Ops)
		a.drain()
		// two seconds a frame: every spring (the error's shake and fade)
		// has settled, so the shots are stills
		clock = clock.Add(2 * time.Second)
		g := ttContext(ops, sz)
		g.Now = clock
		a.frame(g)
		if win == nil {
			return nil
		}
		if err := win.Frame(ops); err != nil {
			t.Fatalf("frame: %v", err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		return img
	}
	r := &ttRenderer{t: t, a: a, sz: sz, dir: shots} // for save
	for _, tc := range []struct {
		name     string
		advanced bool
		err      string
	}{
		{"login-link", false, ""},
		{"login-link-error", false, "that sign-in link has expired or was already used (invalid_magic_token). Links work once and only for a few minutes: get a new one"},
		{"login-advanced", true, ""},
		{"login-advanced-error", true, "Slack rejected the token and cookie: slack auth.test: invalid_auth"},
	} {
		a.login.advanced, a.login.err, a.login.focused = tc.advanced, tc.err, false
		frame()
		img := frame() // focus settles
		c := a.login.card
		if c.Empty() || !c.In(image.Rectangle{Max: sz}) {
			t.Errorf("%s: card %v doesn't fit %v", tc.name, c, sz)
		}
		r.save(tc.name, img, sz.Y)
	}
	// A smaller laptop screen still fits the main path.
	gtx := ttContext(new(op.Ops), image.Pt(1280, 720))
	a.login.advanced, a.login.err = false, ""
	a.frame(gtx)
	if c := a.login.card; !c.In(image.Rect(0, 0, 1280, 720)) {
		t.Errorf("1280x720: card %v", c)
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
	link := "slack://T08PSQ7BQ/magic-login/abc123"
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
}

// Success clears the secrets and leaves the sign-in screen.
func TestStartLoginSuccess(t *testing.T) {
	a := loginApp(t, func(slack.SignInInput) error { return nil })
	a.login.link.SetText("slack://T08PSQ7BQ/magic-login/abc123")
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
