package ui

import (
	"image"
	"io"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/transfer"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic credentials only: the right shape, nobody's session.
const (
	pasteXoxc    = "xoxc-1111111111-2222222222-3333333333-aaaabbbbccccdddd"
	pasteXoxcOrg = "xoxc-9999999999-8888888888-7777777777-eeeeffff00001111"
	pasteXoxd    = "xoxd-AbC%2Fdef%2BGhI%3D%3D"
	pasteTeams   = `{"teams":{"T0AAA":{"id":"T0AAA","name":"Other","url":"https://other.slack.com/","token":"` + pasteXoxcOrg + `"},` +
		`"T0BBB":{"id":"T0BBB","name":"Hack Club","url":"https://hackclub.slack.com/","token":"` + pasteXoxc + `"},` +
		`"E0CCC":{"id":"E0CCC","name":"Hack Club Org","url":"https://hackclub.enterprise.slack.com/","token":"` + pasteXoxcOrg + `"}}}`
)

// pasteEnv is the sign-in card with its sign-ins recorded.
type pasteEnv struct {
	*signInEnv
	calls chan slack.SignInInput
}

func newPasteEnv(t *testing.T) *pasteEnv {
	calls := make(chan slack.SignInInput, 4)
	s := newSignInEnv(t, func(in slack.SignInInput) error {
		calls <- in
		return slack.ErrSessionRejected
	}, image.Pt(1440, 900))
	return &pasteEnv{signInEnv: s, calls: calls}
}

// ctrlV presses Ctrl/⌘+V with no field focused and answers the clipboard
// read with text.
func (s *pasteEnv) ctrlV(text string) {
	s.t.Helper()
	s.r.Source().Execute(key.FocusCmd{}) // nothing focused: the card's own shortcut
	s.frame(time.Second / 60)
	s.r.Queue(key.Event{Name: "V", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "V", Modifiers: key.ModShortcut, State: key.Release})
	s.frame(time.Second / 60)
	if !s.r.ClipboardRequested() {
		s.t.Fatal("Ctrl+V on the card didn't ask for the clipboard")
	}
	s.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(text)) }})
	s.frame(time.Second / 60)
	s.frame(time.Second / 60)
}

// cleared reports whether the card wrote an empty clipboard.
func (s *pasteEnv) cleared() bool {
	mime, b, ok := s.r.WriteClipboard()
	return ok && len(b) == 0 && mime == "application/text"
}

// noSignIn fails if the card signed in.
func (s *pasteEnv) noSignIn() {
	s.t.Helper()
	select {
	case in := <-s.calls:
		s.t.Fatalf("signed in with %+v", in)
	case <-time.After(30 * time.Millisecond):
	}
}

func (s *pasteEnv) signedIn() slack.SignInInput {
	s.t.Helper()
	select {
	case in := <-s.calls:
		return in
	case <-time.After(5 * time.Second):
		s.t.Fatal("no sign-in")
	}
	return slack.SignInInput{}
}

// The Advanced snippet's JSON, then the d cookie, each by Ctrl+V on the
// card: the JSON fills Workspace, Token and Org token (the matching team)
// and opens Advanced with the focus on d cookie; the cookie signs in
// without a click. Each credential is taken off the clipboard.
func TestSmartPasteJSONThenCookie(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	s.ctrlV(pasteTeams)
	if !s.cleared() {
		t.Error("the JSON was left on the clipboard")
	}
	s.noSignIn()
	s.run(1500 * time.Millisecond)
	if !l.advanced {
		t.Fatal("the JSON didn't open Advanced")
	}
	if got := l.workspace.Text(); got != "hackclub.slack.com" {
		t.Errorf("Workspace %q", got)
	}
	if l.token.Text() != pasteXoxc || l.orgToken.Text() != pasteXoxcOrg || l.cookie.Text() != "" {
		t.Errorf("Token %q, Org token %q, d cookie %q", l.token.Text(), l.orgToken.Text(), l.cookie.Text())
	}
	if !s.r.Source().Focused(&l.cookie) {
		t.Error("the focus isn't on d cookie, the one thing left")
	}
	if !s.drew("Filled the workspace, token and org token.") || !s.drew(loginCookieWhere) {
		t.Errorf("the card doesn't say what it filled and where d is: %q", s.strs)
	}
	if l.err != "" {
		t.Errorf("err %q", l.err)
	}
	s.ctrlV("d=" + pasteXoxd + "; d-s=1794000000")
	if !s.cleared() {
		t.Error("the cookie was left on the clipboard")
	}
	in := s.signedIn()
	if in.Token != pasteXoxc || in.Cookie != pasteXoxd || in.OrgToken != pasteXoxcOrg || in.OrgURL != "https://hackclub.enterprise.slack.com/" || in.Workspace != "hackclub.slack.com" {
		t.Errorf("signed in with %+v", in)
	}
}

// Karim's α.36 case: the whole JSON pasted into a field (here the link
// field, then Token) goes where it belongs instead of staying in the
// field as one long "token".
func TestSmartPasteJSONIntoAField(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	s.typeIn(pasteTeams) // the link field has the focus
	s.noSignIn()
	s.run(1500 * time.Millisecond)
	if !l.advanced || l.link.Text() != "" || l.token.Text() != pasteXoxc {
		t.Fatalf("JSON in the link field: advanced %v, link %q, token %q", l.advanced, l.link.Text(), l.token.Text())
	}
	// into Token, after a token: the field keeps its token, the JSON's
	// team replaces it, and the org token comes along
	l.orgToken.SetText("")
	s.r.Source().Execute(key.FocusCmd{Tag: &l.token})
	s.frame(time.Second / 60)
	s.typeIn(pasteTeams)
	s.noSignIn()
	if l.token.Text() != pasteXoxc || l.orgToken.Text() != pasteXoxcOrg {
		t.Errorf("JSON in Token: token %q, org token %q", l.token.Text(), l.orgToken.Text())
	}
	// a Cookie header pasted into Token goes to d cookie and signs in
	s.typeIn("d=" + pasteXoxd + "; d-s=1794000000")
	if l.token.Text() != pasteXoxc || l.cookie.Text() != pasteXoxd {
		t.Errorf("cookie in Token: token %q, d cookie %q", l.token.Text(), l.cookie.Text())
	}
	if in := s.signedIn(); in.Cookie != pasteXoxd || in.Token != pasteXoxc {
		t.Errorf("signed in with %+v", in)
	}
}

// A bare token and a bare d cookie from the clipboard (Ctrl+V, and the
// window coming to the front): each fills its field; with both the card
// signs in. A bare xoxc pasted into Org token stays there.
func TestSmartPasteTokenAndCookie(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	s.ctrlV(pasteXoxc)
	s.noSignIn()
	if !l.advanced || l.token.Text() != pasteXoxc || !s.cleared() {
		t.Fatalf("token: advanced %v, token %q", l.advanced, l.token.Text())
	}
	s.run(1500 * time.Millisecond)
	if !s.drew("Filled the token.") {
		t.Errorf("the card doesn't say it filled the token: %q", s.strs)
	}
	if _, a := l.loginTick(&l.token); a <= 0 {
		t.Error("Token doesn't tick")
	}
	s.run(loginTickFor)
	if mv, wk := s.motion(); mv || wk != 0 {
		t.Errorf("after the tick: moving %v, next frame in %v", mv, wk)
	}
	// Org token: a bare xoxc pasted there is the org's
	s.r.Source().Execute(key.FocusCmd{Tag: &l.orgToken})
	s.frame(time.Second / 60)
	s.typeIn(pasteXoxcOrg)
	if l.orgToken.Text() != pasteXoxcOrg || l.token.Text() != pasteXoxc {
		t.Errorf("org token: %q, token %q", l.orgToken.Text(), l.token.Text())
	}
	s.noSignIn()
	// the window comes to the front with the d cookie on the clipboard
	s.a.loginWindowFocused()
	s.frame(time.Second / 60)
	if !s.r.ClipboardRequested() {
		t.Fatal("no clipboard read when the window came to the front")
	}
	s.r.Queue(transfer.DataEvent{Type: "application/text", Open: func() io.ReadCloser { return io.NopCloser(strings.NewReader(pasteXoxd + "\n")) }})
	s.frame(time.Second / 60)
	s.frame(time.Second / 60)
	if !s.cleared() {
		t.Error("the cookie was left on the clipboard")
	}
	if in := s.signedIn(); in.Token != pasteXoxc || in.Cookie != pasteXoxd {
		t.Errorf("signed in with %+v", in)
	}
}

// The d cookie alone, by Ctrl+V on the main card: Advanced opens with it
// filled and the card signs in at once, with no token (slack.SignIn
// fetches the tokens). The token fields stay closed.
func TestSmartPasteCookieOnly(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	s.ctrlV("d=" + pasteXoxd + "; d-s=1794000000")
	if !s.cleared() {
		t.Error("the cookie was left on the clipboard")
	}
	in := s.signedIn()
	if in.Cookie != pasteXoxd || in.Token != "" || in.OrgToken != "" || in.Workspace != "hackclub" || in.Link != "" {
		t.Errorf("signed in with %+v", in)
	}
	if !l.advanced || l.tokenForm || l.cookie.Text() != pasteXoxd {
		t.Errorf("advanced %v, token form %v, d cookie %q", l.advanced, l.tokenForm, l.cookie.Text())
	}
	// Slack refused it: a few words, the cookie still there to fix
	waitFor(t, s.a, "the failure", func() bool { return !l.busy })
	s.frame(time.Second / 60)
	if want := "Slack didn’t accept that token and cookie."; l.err != want {
		t.Errorf("err %q, want %q", l.err, want)
	}
}

// Typed by hand into Advanced's one field, then Enter: the cookie alone
// signs in.
func TestAdvancedCookieByHand(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	l.advToggle.Click()
	s.frame(time.Second / 60)
	s.run(time.Second)
	if !s.r.Source().Focused(&l.cookie) {
		t.Fatal("Advanced didn't focus d cookie")
	}
	for i, r := range pasteXoxd {
		s.r.Queue(key.EditEvent{Range: key.Range{Start: i, End: i}, Text: string(r)}, key.SelectionEvent{Start: i + 1, End: i + 1})
		s.frame(time.Second / 60)
	}
	s.noSignIn()
	s.key(key.NameReturn)
	if in := s.signedIn(); in.Cookie != pasteXoxd || in.Token != "" {
		t.Errorf("signed in with %+v", in)
	}
}

// A sign-in link by Ctrl+V signs in, as it does from the field.
func TestSmartPasteLink(t *testing.T) {
	s := newPasteEnv(t)
	s.ctrlV(testMagicLink)
	if !s.cleared() {
		t.Error("the link was left on the clipboard")
	}
	if in := s.signedIn(); in.Link != testMagicLink {
		t.Errorf("signed in with %+v", in)
	}
}

// Anything else is left alone: nothing filled, no error, the clipboard
// untouched, no view change, whether pasted on the card or into a field.
func TestSmartPasteJunk(t *testing.T) {
	s := newPasteEnv(t)
	l := s.a.login
	for _, junk := range []string{
		"a message I copied, https://example.com",
		`{"hello":"world"}`,
		`{"teams":{`,
		"xox" + "b-1111111111-2222222222-aaaabbbbccccdddd", // a bot token (split: push protection reads it as a real one)
		"xoxc-123",
		"d-s=1794000000",
		"",
	} {
		s.ctrlV(junk)
		if _, _, ok := s.r.WriteClipboard(); ok {
			t.Errorf("%q: the clipboard was written", junk)
		}
		if l.advanced || l.err != "" || l.pasteNote != "" || l.token.Text() != "" || l.cookie.Text() != "" || l.link.Text() != "" {
			t.Errorf("%q: advanced %v, err %q, note %q, token %q, cookie %q, link %q", junk, l.advanced, l.err, l.pasteNote, l.token.Text(), l.cookie.Text(), l.link.Text())
		}
	}
	s.noSignIn()
	// typed into Token by hand, a key at a time, a token isn't moved or
	// signed in with mid-word
	l.advToggle.Click()
	s.frame(time.Second / 60)
	l.tokenBtn.Click()
	s.frame(time.Second / 60)
	s.run(500 * time.Millisecond)
	l.setField(&l.cookie, pasteXoxd, false) // as if typed: not a paste
	s.frame(time.Second / 60)
	s.r.Source().Execute(key.FocusCmd{Tag: &l.token})
	s.frame(time.Second / 60)
	for i, r := range pasteXoxc {
		s.r.Queue(key.EditEvent{Range: key.Range{Start: i, End: i}, Text: string(r)}, key.SelectionEvent{Start: i + 1, End: i + 1})
		s.frame(time.Second / 60)
	}
	s.frame(time.Second / 60)
	if l.token.Text() != pasteXoxc {
		t.Errorf("typed token %q", l.token.Text())
	}
	s.noSignIn()
}
