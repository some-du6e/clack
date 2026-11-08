package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestParseMagicLinkClassic(t *testing.T) {
	sets, err := ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc-DEF_123")
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || sets[0].BaseDomain != "slack.com" || sets[0].RequiresDPoP ||
		strings.Join(sets[0].Tokens, ",") != "z-app-T08PSQ7BQ-abc-DEF_123" {
		t.Errorf("sets = %+v", sets)
	}
	// KDE's kde-open lowercases the host: the team id comes back upper case.
	sets, err = ParseMagicLink("slack://t08psq7bq/magic-login/abc123/?host=hackclub.enterprise.slack.com&dpop=1")
	if err != nil {
		t.Fatal(err)
	}
	if sets[0].Tokens[0] != "z-app-T08PSQ7BQ-abc123" || sets[0].BaseDomain != "hackclub.enterprise.slack.com" || !sets[0].RequiresDPoP {
		t.Errorf("lowercased: %+v", sets)
	}
	// An org (E) id works too.
	if sets, err := ParseMagicLink("SLACK://E0123456789/magic-login/x1"); err != nil || sets[0].Tokens[0] != "z-app-E0123456789-x1" {
		t.Errorf("org id: %+v %v", sets, err)
	}
}

func TestParseMagicLinkV2(t *testing.T) {
	sets, err := ParseMagicLink("slack://login-v2/?0.host=slack.com&0.tokens=aaa_bbb&0.dpop=1&1.host=hackclub.enterprise.slack.com&1.tokens=ccc")
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 {
		t.Fatalf("sets = %+v", sets)
	}
	if strings.Join(sets[0].Tokens, ",") != "aaa,bbb" || sets[0].BaseDomain != "slack.com" || !sets[0].RequiresDPoP {
		t.Errorf("set 0 = %+v", sets[0])
	}
	if strings.Join(sets[1].Tokens, ",") != "ccc" || sets[1].BaseDomain != "hackclub.enterprise.slack.com" || sets[1].RequiresDPoP {
		t.Errorf("set 1 = %+v", sets[1])
	}
	if _, err := ParseMagicLink("slack://login-v2/?0.host=slack.com"); err == nil {
		t.Error("set without tokens accepted")
	}
	if _, err := ParseMagicLink("slack://login-v2/?0.tokens=a"); err == nil {
		t.Error("set without host accepted")
	}
	if _, err := ParseMagicLink("slack://login-v2/"); err == nil {
		t.Error("empty login-v2 accepted")
	}
}

func TestParseMagicLinkHostile(t *testing.T) {
	for _, raw := range []string{
		"slack://T08PSQ7BQ/magic-login/abc?host=evil.com",
		"slack://T08PSQ7BQ/magic-login/abc?host=slack.com.evil.io",
		"slack://T08PSQ7BQ/magic-login/abc?host=evil.com%2Fslack.com",
		"slack://T08PSQ7BQ/magic-login/abc?host=slack.com:8443",
		"slack://T08PSQ7BQ/magic-login/abc?host=user@slack.com",
		"slack://T08PSQ7BQ/magic-login/abc?host=xn--slck-loa.com",
		"slack://T08PSQ7BQ/magic-login/abc?host=sl%C3%A4ck.com",
		"slack://T08PSQ7BQ/magic-login/abc?host=127.0.0.1",
		"slack://T08PSQ7BQ/magic-login/abc?host=evilslack.com",
		"slack://login-v2/?0.host=slack.com&0.tokens=a&1.host=evil.com&1.tokens=b",
		"slack://login-v2/?0.host=slack.com&0.tokens=a%26x%3D1",
		"slack://login-v2/?0.host=slack.com&0.tokens=a,b",
		"https://slack.com/T08PSQ7BQ/magic-login/abc",
		"slack://channel?team=T1&id=C1",
		"slack://T08PSQ7BQ/magic-login/",
		"",
	} {
		if sets, err := ParseMagicLink(raw); err == nil {
			t.Errorf("ParseMagicLink(%q) = %+v, want an error", raw, sets)
		}
	}
	if IsMagicLink("slack://channel?team=T1&id=C1") || IsMagicLink("https://x/magic-login/a") {
		t.Error("IsMagicLink matched a navigation link")
	}
	if !IsMagicLink(" slack://T08PSQ7BQ/magic-login/abc ") || !IsMagicLink("slack://login-v2/?0.host=slack.com") {
		t.Error("IsMagicLink missed a sign-in link")
	}
}

// rewriteTransport sends every request to srv, keeping the original host in
// X-Orig-Host. The client (and its cookie jar) still sees https://*.slack.com.
type rewriteTransport struct{ srv *httptest.Server }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(rt.srv.URL)
	r2 := r.Clone(r.Context())
	r2.Header.Set("X-Orig-Host", r.URL.Host)
	r2.Header.Set("X-Orig-Scheme", r.URL.Scheme)
	r2.URL.Scheme, r2.URL.Host, r2.Host = u.Scheme, u.Host, ""
	return http.DefaultTransport.RoundTrip(r2)
}

// fakeDesktopSlack is just enough of Slack for desktop sign-in.
type fakeDesktopSlack struct {
	mu       sync.Mutex
	srv      *httptest.Server
	reqs     []*http.Request
	exchange func(w http.ResponseWriter, r *http.Request)
	bootHTML string
	rotateDS int // d-s value set on every API answer, incremented each time
}

func (f *fakeDesktopSlack) requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.reqs...)
}

func newFakeDesktopSlack(t *testing.T) *fakeDesktopSlack {
	f := &fakeDesktopSlack{rotateDS: 1794000000,
		bootHTML: `<html><script>(function(){ if (!window.parent) return; sessionStorage.incomingConfig = JSON.stringify({"teams":{"T08PSQ7BQ":{"id":"T08PSQ7BQ","name":"Hack Club","url":"https:\/\/hackclub.slack.com\/","domain":"hackclub","token":"xoxc-vended-1-2-3abc"}}}); window.parent.postMessage('gantry_auth_notify', "https:\/\/app.slack.com"); })();</script></html>`}
	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "d", Value: "xoxd-vended%2Fabc", Domain: ".slack.com", Path: "/", Secure: true, HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "lc", Value: "1794000000", Domain: ".slack.com", Path: "/", Secure: true})
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "token_results": map[string]any{
			"z-app-T08PSQ7BQ-abc": map[string]any{"team": map[string]any{"id": "T08PSQ7BQ", "url": "https://hackclub.slack.com/"}, "user": "U1"}}})
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reqs = append(f.reqs, r.Clone(context.Background()))
		f.mu.Unlock()
		host := r.Header.Get("X-Orig-Host")
		switch {
		case r.URL.Path == "/api/auth.loginMagicBulk":
			f.exchange(w, r)
		case r.URL.Path == "/auth" && host == "app.slack.com":
			if c, err := r.Cookie("d"); err != nil || c.Value != "xoxd-vended%2Fabc" {
				w.Write([]byte("<html>sign in</html>"))
				return
			}
			w.Write([]byte(f.bootHTML))
		case strings.HasPrefix(r.URL.Path, "/api/"):
			f.mu.Lock()
			f.rotateDS++
			ds := f.rotateDS
			f.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "d-s", Value: strconv.Itoa(ds), Domain: ".slack.com", Path: "/", Secure: true, HttpOnly: true})
			r.ParseForm()
			switch r.FormValue("token") {
			case "xoxc-vended-1-2-3abc":
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": "T08PSQ7BQ", "user_id": "U1", "url": "https://hackclub.slack.com/", "enterprise_id": "E1"})
			case "xoxc-org-1-2-3abcd":
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": "E1", "user_id": "U1", "url": "https://hackclub.enterprise.slack.com/"})
			default:
				json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid_auth"})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	magicTransport = rewriteTransport{f.srv}
	t.Cleanup(func() { magicTransport = nil })
	return f
}

func TestLoginMagicExchange(t *testing.T) {
	f := newFakeDesktopSlack(t)
	sets, _ := ParseMagicLink("slack://t08psq7bq/magic-login/abc")
	c, err := LoginMagic(context.Background(), sets, MagicConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Session != SessionDesktop || c.Token != "xoxc-vended-1-2-3abc" || c.Cookie != "xoxd-vended%2Fabc" ||
		c.Team != "T08PSQ7BQ" || c.UserID != "U1" || c.TeamURL != "https://hackclub.slack.com/" || c.UserAgent != "" {
		t.Errorf("creds = %#v (token %q cookie %q)", c, c.Token, c.Cookie)
	}
	if !strings.Contains(c.Cookies, "lc=1794000000") || !strings.Contains(c.Cookies, "d-s=") || strings.Contains(c.Cookies, "d=xoxd") {
		t.Errorf("other cookies = %q", c.Cookies)
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want exchange, token page, auth.test", len(reqs))
	}
	ex := reqs[0]
	if ex.Method != http.MethodGet || ex.Header.Get("X-Orig-Host") != "slack.com" || ex.Header.Get("X-Orig-Scheme") != "https" ||
		ex.URL.RawQuery != "magic_tokens=z-app-T08PSQ7BQ-abc&ssb=1" {
		t.Errorf("exchange = %s %s?%s on %s", ex.Method, ex.URL.Path, ex.URL.RawQuery, ex.Header.Get("X-Orig-Host"))
	}
	if ex.Header.Get("Cookie") != "" {
		t.Errorf("exchange sent cookies %q: nothing from the browser may go along", ex.Header.Get("Cookie"))
	}
	for _, r := range reqs {
		if ua := r.Header.Get("User-Agent"); ua != DesktopUserAgent() {
			t.Errorf("%s UA = %q, want Slack desktop's", r.URL.Path, ua)
		}
		if r.Header.Get("sec-ch-ua") != "" {
			t.Errorf("%s sent client hints", r.URL.Path)
		}
	}
	if q := reqs[1]; q.URL.Path != "/auth" || q.URL.RawQuery != "app=client&return_to=%2Fclient%2FT08PSQ7BQ&teams=&iframe=1" || q.Header.Get("X-Orig-Host") != "app.slack.com" ||
		!strings.Contains(q.Header.Get("Cookie"), "d=xoxd-vended%2Fabc") {
		t.Errorf("token page = %s?%s on %s", q.URL.Path, q.URL.RawQuery, q.Header.Get("X-Orig-Host"))
	}
	if q := reqs[2]; q.URL.Path != "/api/auth.test" || q.Header.Get("X-Orig-Host") != "hackclub.slack.com" || !strings.Contains(q.Header.Get("Cookie"), "d=xoxd-vended%2Fabc") {
		t.Errorf("auth.test = %s on %s cookie %q", q.URL.Path, q.Header.Get("X-Orig-Host"), q.Header.Get("Cookie"))
	}
}

func TestLoginMagicGridTokens(t *testing.T) {
	f := newFakeDesktopSlack(t)
	f.bootHTML = `<html><script>(function(){ if (!window.parent) return; sessionStorage.incomingConfig = JSON.stringify({"teams":{"E1":{"id":"E1","url":"https:\/\/hackclub.enterprise.slack.com\/","token":"xoxc-org-1-2-3abcd"},"T08PSQ7BQ":{"id":"T08PSQ7BQ","url":"https:\/\/hackclub.slack.com\/","token":"xoxc-vended-1-2-3abc","enterprise_api_token":"xoxc-org-1-2-3abcd"},"T999":{"id":"T999","token":"xoxc-other-1-2-3abc"}}}); window.parent.postMessage('gantry_auth_notify', "https:\/\/app.slack.com"); })();</script></html>`
	sets, _ := ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc")
	c, err := LoginMagic(context.Background(), sets, MagicConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != "xoxc-vended-1-2-3abc" || c.EnterpriseToken != "xoxc-org-1-2-3abcd" || c.EnterpriseID != "E1" || c.EnterpriseURL != "https://hackclub.enterprise.slack.com/" {
		t.Errorf("grid creds: team %q org %q %q", c.Token, c.EnterpriseToken, c.EnterpriseURL)
	}
	// One auth.test, with the workspace token: the org token is never sent
	// to auth.test (every Grid sign-out so far followed that call).
	var tests []string
	for _, r := range f.requests() {
		if r.URL.Path == "/api/auth.test" {
			r.ParseForm()
			tests = append(tests, r.Header.Get("X-Orig-Host")+" "+r.FormValue("token"))
		}
	}
	if strings.Join(tests, ",") != "hackclub.slack.com xoxc-vended-1-2-3abc" {
		t.Errorf("auth.test calls = %v, want one with the workspace token", tests)
	}
}

func TestLoginMagicErrors(t *testing.T) {
	f := newFakeDesktopSlack(t)
	sets, _ := ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc")
	ctx := context.Background()

	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error":"invalid_magic_token"}`))
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "expired or was already used") {
		t.Errorf("used link: %v", err)
	}

	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"token_results":{"x":{"auth_redir":"https://hackclub.slack.com/sso/saml/start","reason":"sso"}}}`))
	}
	_, err := LoginMagic(ctx, sets, MagicConfig{})
	var re *MagicRedirectError
	if !errors.As(err, &re) || re.URL != "https://hackclub.slack.com/sso/saml/start" || !strings.Contains(err.Error(), "browser") {
		t.Errorf("auth_redir: %v", err)
	}
	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"token_results":{"x":{"auth_redir":"https://evil.example/phish"}}}`))
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); !errors.As(err, &re) || re.URL != "" {
		t.Errorf("off-Slack auth_redir must not be shown: %v", err)
	}

	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"token_results":{"x":{"team":{"id":"T1","url":"https://evil.example/"}}}}`))
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("off-Slack team url: %v", err)
	}

	// No d cookie vended.
	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"token_results":{"x":{"team":{"id":"T1","url":"https://hackclub.slack.com/"}}}}`))
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "session cookie") {
		t.Errorf("no cookie: %v", err)
	}

	// A redirect off Slack is refused.
	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/x", http.StatusFound)
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Errorf("redirect: %v", err)
	}

	// Nor is one to /ssb/redirect, even on Slack.
	f.exchange = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://hackclub.slack.com/ssb/redirect?x=1", http.StatusFound)
	}
	if _, err := LoginMagic(ctx, sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "/ssb/redirect") {
		t.Errorf("redirect to /ssb/redirect: %v", err)
	}
	for _, r := range f.requests() {
		if strings.HasPrefix(r.URL.Path, "/ssb/redirect") {
			t.Errorf("loaded %s", r.URL.Path)
		}
	}

	// A token set built by hand still can't go anywhere but slack.com.
	if _, err := LoginMagic(ctx, []MagicTokenSet{{Tokens: []string{"a"}, BaseDomain: "evil.com"}}, MagicConfig{}); err == nil {
		t.Error("hand-built set for evil.com accepted")
	}
	if _, err := LoginMagic(ctx, []MagicTokenSet{{Tokens: []string{"a&b=1"}, BaseDomain: "slack.com"}}, MagicConfig{}); err == nil {
		t.Error("token with & accepted")
	}
}

func TestLoginMagicNoTokenOnPage(t *testing.T) {
	f := newFakeDesktopSlack(t)
	f.bootHTML = "<html>nothing here</html>"
	sets, _ := ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc")
	_, err := LoginMagic(context.Background(), sets, MagicConfig{})
	if err == nil || !strings.Contains(err.Error(), "had no tokens") || !strings.Contains(err.Error(), "app.slack.com/auth") {
		t.Errorf("err = %v", err)
	}
	// Only the token page: never /ssb/redirect or any other page,
	// and no auth.test without a token.
	var paths []string
	for _, r := range f.requests() {
		paths = append(paths, r.Header.Get("X-Orig-Host")+r.URL.Path)
	}
	if strings.Join(paths, " ") != "slack.com/api/auth.loginMagicBulk app.slack.com/auth" {
		t.Errorf("requests = %v", paths)
	}
}

func TestRedactMagicLinks(t *testing.T) {
	for _, s := range []string{
		"slack://T08PSQ7BQ/magic-login/secret-tok-1",
		"https://slack.com/api/auth.loginMagicBulk?magic_tokens=secret-tok-1&ssb=1",
		"slack://login-v2/?0.host=slack.com&0.tokens=secret-tok-1",
		"Cookie: lc=secret-tok-1",
	} {
		if r := Redact(s); strings.Contains(r, "secret-tok-1") {
			t.Errorf("Redact(%q) = %q", s, r)
		}
	}
	in := SignInInput{Link: "slack://T08PSQ7BQ/magic-login/secret-tok-1"}
	if strings.Contains(in.String(), "secret") {
		t.Errorf("SignInInput leaks the link: %s", in)
	}
	c := Creds{Cookies: "lc=secret-tok-1"}
	if strings.Contains(c.String(), "secret") {
		t.Errorf("Creds leaks cookies: %s", c)
	}
}

func TestDesktopUserAgent(t *testing.T) {
	got := desktopUserAgent("linux", linuxUAFrom("NAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\n", "ubuntu:GNOME"))
	want := "Mozilla/5.0 (X11; Linux x86_64; Ubuntu 24.04; ubuntuGNOME) AppleWebKit/537.36 (KHTML, like Gecko) Slack/4.53.276 Chrome/156.0.8078.49 Electron/45.1.0 Safari/537.36 Sonic Slack_SSB/4.53.276"
	if got != want {
		t.Errorf("linux UA\n got %q\nwant %q", got, want)
	}
	if got := desktopUserAgent("linux", ""); !strings.HasPrefix(got, "Mozilla/5.0 (X11; Linux x86_64) ") {
		t.Errorf("no os-release: %q", got)
	}
	if got := desktopUserAgent("darwin", ""); !strings.Contains(got, "(Macintosh; Intel Mac OS X 10_15_7)") || !strings.HasSuffix(got, " Sonic Slack_SSB/4.53.276") {
		t.Errorf("mac UA %q", got)
	}
	if got := linuxUAFrom("ID=arch\n", "KDE"); got != "; arch unversioned; KDE" {
		t.Errorf("arch: %q", got)
	}
	if got := linuxUAFrom("NAME=\"Evil) (x\"\nVERSION_ID=1;2\n", ""); strings.ContainsAny(got, "()") || strings.Count(got, ";") != 1 {
		t.Errorf("hostile os-release kept separators: %q", got)
	}
	if clientHints(DesktopUserAgent()) != nil {
		t.Error("Slack desktop sends no client hints")
	}
}

// The token page's config: the exchange's team first, the org token and
// address apart, and nothing but xoxc- values.
func TestParseIncomingConfig(t *testing.T) {
	page := []byte(`<script>sessionStorage.incomingConfig = JSON.stringify({"teams":{` +
		`"E1":{"id":"E1","url":"https:\/\/hackclub.enterprise.slack.com\/","token":"xoxc-org-1"},` +
		`"T2":{"id":"T2","token":"xoxc-two-1"},` +
		`"T1":{"id":"T1","token":"xoxc-one-1","enterprise_api_token":"xoxc-org-1"},` +
		`"T3":{"id":"T3","token":"xoxb-bot-1"}}}); </script>`)
	got := parseIncomingConfig(page, "T1")
	if strings.Join(got.Team, " ") != "xoxc-one-1 xoxc-two-1" || got.Org != "xoxc-org-1" || got.OrgURL != "https://hackclub.enterprise.slack.com/" {
		t.Errorf("T1: %+v", got)
	}
	if got := parseIncomingConfig(page, "t2"); strings.Join(got.Team, " ") != "xoxc-two-1 xoxc-one-1" {
		t.Errorf("t2: %+v", got)
	}
	evil := []byte(`incomingConfig = JSON.stringify({"teams":{"E1":{"id":"E1","url":"https://evil.example/","token":"xoxc-org-1"},"T1":{"id":"T1","token":"xoxc-one-1"}}})`)
	if got := parseIncomingConfig(evil, "T1"); got.OrgURL != "" {
		t.Errorf("off-Slack org url kept: %+v", got)
	}
	for _, bad := range []string{"", "<html>no config</html>", "incomingConfig = JSON.stringify({not json", `incomingConfig = JSON.stringify({"teams":{"T1":{"token":"xoxp-1"}}})`} {
		if got := parseIncomingConfig([]byte(bad), "T1"); len(got.Team) != 0 || got.Org != "" {
			t.Errorf("%q: %+v", bad, got)
		}
	}
}
