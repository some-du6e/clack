package slack_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// toFake sends every request to the fake, keeping the host it was for.
type toFake struct {
	srv  *url.URL
	mu   sync.Mutex
	seen []string // "host path" of every request
}

func (t *toFake) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.seen = append(t.seen, r.URL.Host+" "+r.URL.Path)
	t.mu.Unlock()
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = t.srv.Scheme, t.srv.Host, ""
	return http.DefaultTransport.RoundTrip(r2)
}

func startFake(t *testing.T) (*fakeslack.Server, *toFake, *[]string) {
	t.Helper()
	s, err := fakeslack.Start(fakeslack.Size{Users: 3, Channels: 2}) // generate() reads users[2]
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, _ := url.Parse(s.URL)
	tr := &toFake{srv: u}
	t.Cleanup(slack.SetSignInTransport(tr))
	var mu sync.Mutex
	var tokens []string
	s.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "auth.test" {
			mu.Lock()
			tokens = append(tokens, form.Get("token"))
			mu.Unlock()
		}
		return nil, false
	})
	return s, tr, &tokens
}

// The d cookie alone signs in: Clack loads fakeslack's token page with
// only that cookie, takes the workspace's token (the one named, among the
// session's two), checks it with one auth.test on the workspace host, and
// keeps the org token without ever sending it to auth.test.
func TestSignInCookieOnly(t *testing.T) {
	s, tr, tokens := startFake(t)
	c, err := slack.SignIn(context.Background(), slack.SignInInput{Workspace: "fakeclub", Cookie: "d=" + fakeslack.AuthCookie + "; d-s=1794000000"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "xoxc-fake" || c.Cookie != fakeslack.AuthCookie || c.Team != fakeslack.TeamID || c.UserID != fakeslack.SelfID ||
		c.TeamURL != fakeslack.AuthTeamURL || c.EnterpriseToken != "xoxc-fake-org" || c.EnterpriseURL != fakeslack.AuthOrgURL || c.Session != "" {
		t.Errorf("creds %#v", c)
	}
	if c.Cookies != "" {
		t.Errorf("kept cookies %q: a pasted session keeps only d", c.Cookies)
	}
	if got := strings.Join(*tokens, " "); got != "xoxc-fake" {
		t.Errorf("auth.test tokens %q, want only the workspace's", got)
	}
	want := []string{"app.slack.com /auth", "fakeclub.slack.com /api/auth.test"}
	if strings.Join(tr.seen, ", ") != strings.Join(want, ", ") {
		t.Errorf("requests %q, want %q", tr.seen, want)
	}
	loads := s.PageLoads()
	if len(loads) != 1 || !strings.HasPrefix(loads[0], "/auth?app=client&return_to=%2Fclient%2F&teams=&iframe=1") {
		t.Errorf("pages loaded %q", loads)
	}
	for _, p := range loads {
		if strings.Contains(p, "/ssb/") {
			t.Errorf("loaded %q", p)
		}
	}
}

// Another workspace of the same session, by name; a name the session
// doesn't have takes its first workspace.
func TestSignInCookieOnlyPicksWorkspace(t *testing.T) {
	startFake(t)
	c, err := slack.SignIn(context.Background(), slack.SignInInput{Workspace: "otherclub.slack.com", Cookie: fakeslack.AuthCookie})
	if err != nil || c.Token != "xoxc-fake-other" || c.TeamURL != fakeslack.AuthOtherURL {
		t.Errorf("otherclub: %v, token %q, url %q", err, c.Token, c.TeamURL)
	}
}

// A cookie Slack doesn't take: the token page has no tokens, the sign-in
// says so (ErrCookieRejected), and nothing else is loaded or called.
func TestSignInCookieRejected(t *testing.T) {
	s, tr, tokens := startFake(t)
	_, err := slack.SignIn(context.Background(), slack.SignInInput{Workspace: "fakeclub", Cookie: "xoxd-someone-elses"})
	if !errors.Is(err, slack.ErrCookieRejected) {
		t.Fatalf("err %v, want ErrCookieRejected", err)
	}
	if len(*tokens) != 0 || len(tr.seen) != 1 || len(s.PageLoads()) != 1 {
		t.Errorf("after a rejected cookie: auth.tests %q, requests %q", *tokens, tr.seen)
	}
}
