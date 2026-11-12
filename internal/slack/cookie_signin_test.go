package slack

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
	"github.com/some-du6e/clack/internal/slack/tlsfp/tlsfptest"
)

// The cookie-only sign-in goes out on the API transport only: every
// connection sends tlsfp's recorded OpenSSL ClientHello (never Go's
// crypto/tls hello) over HTTP/1.1, it makes exactly the token page and one
// auth.test, and it never requests /ssb/redirect, not even when the token
// page redirects there. The TCP dial goes to a loopback recorder that
// answers as app.slack.com and hackclub.slack.com; nothing reaches Slack.
func TestSignInCookieUsesTheRecordedHello(t *testing.T) {
	if magicTransport != nil {
		t.Fatal("a test left magicTransport set: this test needs the real API transport")
	}
	hosts := []string{"app.slack.com", "hackclub.slack.com"}
	ca := tlsfptest.NewCA(t)
	var mu sync.Mutex
	var reqs []string
	redirect := false
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Proto+" "+r.Host+" "+r.URL.Path+" cookie="+r.Header.Get("Cookie"))
		red := redirect
		mu.Unlock()
		switch {
		case r.URL.Path == "/auth" && red:
			http.Redirect(w, r, "https://hackclub.slack.com/ssb/redirect", http.StatusFound)
		case r.URL.Path == "/auth":
			io.WriteString(w, `<script>sessionStorage.incomingConfig = JSON.stringify({"teams":{"T1":{"id":"T1","url":"https://hackclub.slack.com/","token":"xoxc-fp-1-2-3abc"}}})</script>`)
		case r.URL.Path == "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true,"team_id":"T1","user_id":"U1","url":"https://hackclub.slack.com/"}`)
		default:
			http.NotFound(w, r)
		}
	}), ca.Leaf(t, hosts...))
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		for _, h := range hosts {
			if addr == h+":443" {
				return srv.Dial(network, addr)
			}
		}
		return nil, errors.New("cookie sign-in dialled " + addr)
	}
	old := apiDialer
	apiDialer = &tlsfp.Dialer{Dial: dial, RootCAs: ca.Pool}
	t.Cleanup(func() { apiDialer = old })

	c, err := SignIn(context.Background(), SignInInput{Workspace: "hackclub", Cookie: "d=xoxd-fp%2Fabc; d-s=1794000000"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "xoxc-fp-1-2-3abc" || c.Cookie != "xoxd-fp%2Fabc" || c.Team != "T1" {
		t.Errorf("creds %#v", c)
	}
	mu.Lock()
	got := append([]string(nil), reqs...)
	mu.Unlock()
	want := []string{"HTTP/1.1 app.slack.com /auth cookie=d=xoxd-fp%2Fabc", "HTTP/1.1 hackclub.slack.com /api/auth.test cookie=d=xoxd-fp%2Fabc"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	rec, err := tlsfptest.Parse(tlsfp.Record())
	if err != nil {
		t.Fatal(err)
	}
	hs := srv.Hellos()
	if len(hs) == 0 {
		t.Fatal("no TLS connection")
	}
	for _, h := range hs {
		p, err := tlsfptest.Parse(h)
		if err != nil {
			t.Fatal(err)
		}
		if tlsfptest.Diff(p, rec, hosts[0]) != nil && tlsfptest.Diff(p, rec, hosts[1]) != nil {
			t.Errorf("a ClientHello differs from the recording: %s", strings.Join(tlsfptest.Diff(p, rec, hosts[0]), "; "))
		}
	}

	// the token page redirecting to /ssb/redirect: refused before the
	// request goes out
	mu.Lock()
	redirect, reqs = true, nil
	mu.Unlock()
	if _, err := SignIn(context.Background(), SignInInput{Workspace: "hackclub", Cookie: "xoxd-fp%2Fabc"}); err == nil {
		t.Fatal("signed in through a redirect to /ssb/redirect")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, r := range reqs {
		if strings.Contains(r, "/ssb/") {
			t.Errorf("requested %q", r)
		}
	}
}
