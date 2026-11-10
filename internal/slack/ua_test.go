package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The one User-Agent names Clack and nothing that looks like a browser.
func TestUserAgentIsHonest(t *testing.T) {
	defer SetVersion("dev")
	const want = "Clack/dev (+https://github.com/some-du6e/clack)"
	if got := UserAgent(); got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
	for in, want := range map[string]string{
		"v0.1.0":          "Clack/0.1.0 (+https://github.com/some-du6e/clack)",
		"1.2.3-rc.1+abc":  "Clack/1.2.3-rc.1+abc (+https://github.com/some-du6e/clack)",
		"":                "Clack/dev (+https://github.com/some-du6e/clack)",
		"1.0 Chrome/157":  "Clack/dev (+https://github.com/some-du6e/clack)",
		"v1\r\nX-Evil: 1": "Clack/dev (+https://github.com/some-du6e/clack)",
	} {
		SetVersion(in)
		got := UserAgent()
		if got != want {
			t.Errorf("SetVersion(%q): %q, want %q", in, got, want)
		}
		if tok := browserToken(got); tok != "" {
			t.Errorf("SetVersion(%q): %q contains %q", in, got, tok)
		}
	}
}

// browserToken returns the first deny-listed browser token in ua, or "".
func browserToken(ua string) string {
	for _, tok := range browserUATokens {
		if strings.Contains(strings.ToLower(ua), strings.ToLower(tok)) {
			return tok
		}
	}
	return ""
}

// HonestHeaders drops every browser-only header, whatever its case, and
// keeps the rest.
func TestHonestHeaders(t *testing.T) {
	h := http.Header{}
	for _, k := range []string{"sec-ch-ua", "Sec-CH-UA-Platform", "sec-ch-ua-mobile", "Sec-Fetch-Site", "sec-fetch-mode", "Origin", "Referer"} {
		h[k] = []string{"x"} // raw keys: a header map need not be canonical
	}
	h.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
	h.Set("Cookie", "d=xoxd-1")
	HonestHeaders(h)
	for k := range h {
		if BrowserOnlyHeader(k) {
			t.Errorf("kept %s", k)
		}
	}
	if h.Get("User-Agent") != UserAgent() || h.Get("Cookie") != "d=xoxd-1" || len(h) != 2 {
		t.Errorf("after HonestHeaders: %v", h)
	}
}

// No non-test Go file may carry a browser or Slack desktop identity: the
// strings below appear only in ua.go's deny list (browserUATokens).
func TestNoBrowserUserAgentInSource(t *testing.T) {
	deny := regexp.MustCompile(`(?m)^var browserUATokens = \[\]string\{[^\n]*\}$`)
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		if strings.HasSuffix(p, filepath.Join("slack", "ua.go")) {
			if !deny.MatchString(src) {
				t.Errorf("%s: the browserUATokens deny list is missing or moved", p)
			}
			src = deny.ReplaceAllString(src, "")
		}
		for _, s := range []string{"Slack_SSB", "Chrome/"} {
			if strings.Contains(src, s) {
				t.Errorf("%s contains %q outside the deny list", p, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// An old creds.json's browser user agent is read and never sent.
func TestLegacyUserAgentIgnored(t *testing.T) {
	legacy := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
	var gotUA, gotPlat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotPlat = r.Header.Get("User-Agent"), r.Header.Get("sec-ch-ua-platform")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte(`{"token":"xoxc-1","cookie":"xoxd-1","team_url":"https://hackclub.slack.com/","user_agent":"`+legacy+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cr, err := ReadCreds(path)
	if err != nil {
		t.Fatal(err)
	}
	if cr.UserAgent != legacy || cr.Token != "xoxc-1" {
		t.Errorf("old creds.json read as %+v", cr)
	}
	cr.TeamURL = srv.URL + "/" // the fake stands in for hackclub.slack.com
	if err := New(cr).Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotUA != UserAgent() || gotPlat != "" {
		t.Errorf("UA %q platform %q, want %q and no client hints", gotUA, gotPlat, UserAgent())
	}
}

// The cookie only goes to Slack's file hosts and paths, never to a
// workspace web page (loading those from outside the browser is what
// ended sessions).
func TestDownloadRefusesSlackPages(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	for _, u := range []string{
		"https://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/",
		"https://app.slack.com/client/T1/C1",
		"https://slack.com/signin",
		"https://hackclub.slack.com/files/U1/F1/x.png",
	} {
		if _, err := c.Download(context.Background(), u); !errors.Is(err, ErrNotAFile) {
			t.Errorf("Download(%s) = %v, want ErrNotAFile", u, err)
		}
	}
	for _, u := range []string{
		"https://files.slack.com/files-pri/T1-F1/x.png",
		"https://files-origin.slack.com/files-pri/T1-F1/x.png",
		"https://hackclub.slack.com/files-pri/T1-F1/x.png",
		"https://files.slack.com/files-tmb/T1-F1-abc/x_360.png",
	} {
		pu, _ := url.Parse(u)
		if !FileURL(pu) {
			t.Errorf("FileURL(%s) = false", u)
		}
	}
	via := []*http.Request{{URL: mustURL(t, "https://files.slack.com/files-pri/x")}}
	req := &http.Request{URL: mustURL(t, "https://hackclub.slack.com/?redir=%2Ffiles-pri%2Fx"), Header: http.Header{}}
	if err := c.checkRedirect(req, via); !errors.Is(err, ErrNotAFile) {
		t.Errorf("redirect from a file to a workspace page followed: %v", err)
	}
}

// A file URL that answers with HTML (Slack's sign-in page) isn't returned.
func TestDownloadRefusesHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html>sign in</html>"))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	if _, err := c.Download(context.Background(), srv.URL+"/files-pri/x"); !errors.Is(err, ErrNotAFile) {
		t.Errorf("HTML answer: %v, want ErrNotAFile", err)
	}
}

// agent_version is fixed for the client's life and is a past unix time,
// like the web client's build timestamp.
func TestAgentVersionStable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"primary_websocket_url":"wss://wss-primary.slack.com/","routing_context":"TE1-2"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "t", EnterpriseToken: "e", EnterpriseID: "E1", Team: "T1", TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	av := func(u string) string {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatal(err)
		}
		sa, _ := url.ParseQuery(strings.TrimPrefix(pu.Query().Get("start_args"), "?"))
		return sa.Get("agent_version")
	}
	a1 := av(c.WebSocketURL(context.Background(), false))
	a2 := av(c.WebSocketURL(context.Background(), false))
	if a1 == "" || a1 != a2 {
		t.Errorf("agent_version %q then %q", a1, a2)
	}
}

// Pasted JSON from the old snippet ({"ua": …, "teams": …}) still signs in;
// its user agent is ignored.
func TestPastedJSONUserAgentIgnored(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"
	for _, raw := range []string{
		`{"ua":"` + ua + `","teams":{"T1":{"id":"T1","url":"https://hackclub.slack.com/","token":"xoxc-team"},"E1":{"id":"E1","token":"xoxc-org"}}}`,
		`{"teams":{"T1":{"id":"T1","url":"https://hackclub.slack.com/","token":"xoxc-team"},"E1":{"id":"E1","token":"xoxc-org"}}}`,
	} {
		p := SignInInput{Workspace: "hackclub", Token: raw}
		if err := expandPastedTeams(&p); err != nil {
			t.Fatal(err)
		}
		if p.Token != "xoxc-team" || p.OrgToken != "xoxc-org" || p.Workspace != "https://hackclub.slack.com/" {
			t.Errorf("wrapper JSON: %+v", p)
		}
	}
}
