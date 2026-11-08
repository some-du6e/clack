package taut

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// A saved account sends d and never its stale d-s (TestCheckAuthStaysOnSlack
// covers non-Slack hosts), and switching writes an owner-only creds.json
// only for Slack hosts.
func TestAccountSendsOnlyDAndWritesOwnerOnly(t *testing.T) {
	var got []*http.Request
	tt := &Taut{opts: Options{Fetch: func(ctx context.Context, req *http.Request) (*http.Response, error) {
		got = append(got, req)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: req}, nil
	}}}
	ctx := context.Background()
	a := storedAccount{Creds: slack.Creds{Token: "xoxc-1", Cookie: "xoxd-1", CookieDS: "ds1", TeamURL: "https://hackclub.slack.com/"}}
	if s := tt.checkAuth(ctx, a); s != "valid" {
		t.Fatalf("checkAuth(slack) = %s", s)
	}
	if len(got) != 1 || got[0].URL.Host != "hackclub.slack.com" {
		t.Fatalf("requests: %v", got)
	}
	if c := got[0].Header.Get("Cookie"); c != "d=xoxd-1" {
		t.Errorf("Cookie = %q, want d only (never d-s)", c)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clack", "creds.json")
	if writeCreds(path, slack.Creds{Token: "xoxc-1", TeamURL: "https://evil.example/"}) == nil {
		t.Error("wrote creds for a non-Slack host")
	}
	if err := writeCreds(path, a.Creds); err != nil {
		t.Fatal(err)
	}
	if goruntime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("creds.json mode %v (%v), want 0600", fi.Mode().Perm(), err)
		}
	}
}
