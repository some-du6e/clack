package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	tTok    = "xoxc-1111-2222-secrettoken"
	tOrg    = "xoxc-3333-orgsecret"
	tCookie = "xoxd-cookiesecret%2Fabc"
	tDS     = "1794000000-dssecret"
)

var tCreds = Creds{Token: tTok, Cookie: tCookie, CookieDS: tDS, EnterpriseToken: tOrg, Team: "T1", UserID: "U1",
	TeamURL: "https://hackclub.slack.com/"}

func noSecrets(t *testing.T, what, s string) {
	t.Helper()
	for _, sec := range []string{"secrettoken", "orgsecret", "cookiesecret", "dssecret"} {
		if strings.Contains(s, sec) {
			t.Errorf("%s leaks %q: %s", what, sec, s)
		}
	}
}

func TestCredsFormattingRedacts(t *testing.T) {
	c := tCreds
	in := SignInInput{Workspace: "hackclub", Token: tTok, OrgToken: tOrg, Cookie: tCookie, CookieDS: tDS}
	cl := New(c)
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		noSecrets(t, "Creds "+f, fmt.Sprintf(f, c))
		noSecrets(t, "*Creds "+f, fmt.Sprintf(f, &c))
		noSecrets(t, "SignInInput "+f, fmt.Sprintf(f, in))
		noSecrets(t, "Client "+f, fmt.Sprintf(f, cl))
	}
	noSecrets(t, "struct", fmt.Sprintf("%+v", struct{ C Creds }{c}))
	if s := fmt.Sprint(c); !strings.Contains(s, "U1") || !strings.Contains(s, "hackclub.slack.com") {
		t.Errorf("redacted Creds lost the non-secret fields: %s", s)
	}
	// JSON (creds.json) still has the real values.
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), "secrettoken") || !strings.Contains(string(b), "dssecret") {
		t.Errorf("json lost secrets: %s", b)
	}
}

func TestRedact(t *testing.T) {
	cases := []string{
		`Get "wss://wss-primary.slack.com/?token=` + tTok + `&flannel=3": dial tcp: lookup failed`,
		"cookie d=" + tCookie + "; d-s=" + tDS,
		"form token=" + url.QueryEscape(tOrg) + "&x=1",
		"plain " + tTok,
		"xox" + "e-1-refreshsecrettoken", // split so secret scanners don't flag the fake
	}
	for _, s := range cases {
		noSecrets(t, "Redact", Redact(s))
	}
	if got := Redact("channel id=C123 ts=1.2 thread_ts=3"); got != "channel id=C123 ts=1.2 thread_ts=3" {
		t.Errorf("Redact touched ids: %s", got)
	}
}

func TestDialErrorRedacted(t *testing.T) {
	// A refused connection to a URL carrying the token: the error text must not.
	c := New(Creds{Token: tTok, Cookie: tCookie, TeamURL: "http://127.0.0.1:1/"})
	_, err := c.Download(context.Background(), "http://127.0.0.1:1/x?t="+tTok)
	if err == nil {
		t.Fatal("expected an error")
	}
	noSecrets(t, "Download error", err.Error())
	err = c.Call(context.Background(), "auth.test", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	noSecrets(t, "Call error", err.Error())

	var buf bytes.Buffer
	l := log.New(RedactWriter(&buf), "", 0)
	l.Printf("rtm: %v", &url.Error{Op: "Get", URL: "wss://x.slack.com/?token=" + tTok, Err: errors.New("boom")})
	noSecrets(t, "RedactWriter", buf.String())
}

func TestWebSocketURLRejectsForeignHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"primary_websocket_url":"wss://evil.example/","fallback_websocket_url":"ws://wss-backup.slack.com/"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "team-tok", Team: "T1", TeamURL: srv.URL + "/"})
	if u := c.WebSocketURL(context.Background(), false); !strings.HasPrefix(u, "wss://wss-primary.slack.com/?") {
		t.Errorf("primary: %s", u)
	}
	if u := c.WebSocketURL(context.Background(), true); !strings.HasPrefix(u, "wss://wss-backup.slack.com/?") {
		t.Errorf("fallback: %s", u)
	}
}

func TestWriteCredsPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "creds.json")
	if err := os.WriteFile(p, []byte(`{"token":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreds(p, tCreds); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCreds(p)
	if err != nil || got != tCreds {
		t.Fatalf("round trip: %v", err)
	}
	if runtime.GOOS == "windows" {
		return // ACLs, not mode bits
	}
	for path, want := range map[string]os.FileMode{p: 0o600, dir: 0o700} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode %o, want %o", path, fi.Mode().Perm(), want)
		}
	}
	// An existing too-open file is tightened on load.
	_ = os.Chmod(p, 0o644)
	if _, err := ReadCreds(p); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("ReadCreds left mode %o", fi.Mode().Perm())
	}
	// A bad file's error doesn't echo its contents.
	_ = os.WriteFile(p, []byte(`{"token":"`+tTok+`"`), 0o600)
	if _, err := ReadCreds(p); err == nil {
		t.Error("bad json accepted")
	} else {
		noSecrets(t, "ReadCreds error", err.Error())
	}
}

func TestReadCredsRefusesForeignHosts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "creds.json")
	for _, u := range []string{"http://hackclub.slack.com/", "https://evil.example/", "https://hackclub.slack.com.evil.example/"} {
		c := tCreds
		c.TeamURL = u
		if err := WriteCreds(p, c); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCreds(p); err == nil {
			t.Errorf("ReadCreds accepted team_url %s", u)
		}
	}
}
