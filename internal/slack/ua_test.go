package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDefaultUserAgentMatchesOS(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":   "(X11; Linux x86_64)",
		"darwin":  "(Macintosh; Intel Mac OS X 10_15_7)",
		"windows": "(Windows NT 10.0; Win64; x64)",
	} {
		ua := defaultUserAgent(goos)
		if !strings.Contains(ua, want) || !strings.Contains(ua, "Chrome/"+chromeMajor+".0.0.0") || CleanUserAgent(ua) != ua {
			t.Errorf("%s: %s", goos, ua)
		}
	}
}

func TestCleanUserAgent(t *testing.T) {
	good := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"
	for in, want := range map[string]string{
		good:                      good,
		"  " + good + "\n":        good,
		`"` + good + `"`:          good,
		"User-Agent: " + good:     good,
		"curl/8.0":                "",
		"Mozilla/5.0 (x)\r\nX: y": "",
		"":                        "",
	} {
		if got := CleanUserAgent(in); got != want {
			t.Errorf("CleanUserAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientHints(t *testing.T) {
	h := clientHints("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36")
	if h["sec-ch-ua-platform"] != `"Windows"` || h["sec-ch-ua-mobile"] != "?0" || !strings.Contains(h["sec-ch-ua"], `"Google Chrome";v="157"`) {
		t.Errorf("chrome/windows: %v", h)
	}
	if h := clientHints("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"); h != nil {
		t.Errorf("firefox sends no client hints: %v", h)
	}
	if h := clientHints("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36 Edg/157.0.0.0"); h["sec-ch-ua-platform"] != `"macOS"` || !strings.Contains(h["sec-ch-ua"], "Microsoft Edge") {
		t.Errorf("edge/mac: %v", h)
	}
}

// API calls carry the session's own browser User-Agent and its client hints.
func TestCallsSendSessionUserAgent(t *testing.T) {
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36"
	var gotUA, gotPlat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotPlat = r.Header.Get("User-Agent"), r.Header.Get("sec-ch-ua-platform")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/", UserAgent: ua})
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotUA != ua || gotPlat != `"macOS"` {
		t.Errorf("UA %q platform %q", gotUA, gotPlat)
	}
	c = New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	if err := c.Call(context.Background(), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if gotUA != UserAgent {
		t.Errorf("fallback UA %q, want %q", gotUA, UserAgent)
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

func TestSignInUserAgent(t *testing.T) {
	in := SignInInput{Workspace: "hackclub", Token: "xoxc-1", Cookie: "xoxd-1", UserAgent: "curl/8"}
	if _, err := SignIn(context.Background(), in); err == nil || !strings.Contains(err.Error(), "user agent") {
		t.Errorf("bad UA accepted: %v", err)
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"
	p := SignInInput{Workspace: "hackclub", Token: `{"ua":"` + ua + `","teams":{"T1":{"id":"T1","url":"https://hackclub.slack.com/","token":"xoxc-team"},"E1":{"id":"E1","token":"xoxc-org"}}}`}
	if err := expandPastedTeams(&p); err != nil {
		t.Fatal(err)
	}
	if p.Token != "xoxc-team" || p.OrgToken != "xoxc-org" || p.UserAgent != ua || p.Workspace != "https://hackclub.slack.com/" {
		t.Errorf("wrapper JSON: %+v", p)
	}
}
