package taut

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// api.cookies matches Electron: the d cookie is scoped to https .slack.com,
// so a url filter elsewhere gets nothing.
func TestCookieURLScope(t *testing.T) {
	h := newHarness(t)
	arg := func(u string) []json.RawMessage {
		b, _ := json.Marshal(map[string]string{"url": u, "name": "d"})
		return []json.RawMessage{b}
	}
	for _, u := range []string{"", "https://app.slack.com/client", "https://files.slack.com/x"} {
		if h.tt.cookie("get", arg(u)) == nil {
			t.Errorf("cookies.get(%q) = nil, want the d cookie", u)
		}
	}
	for _, u := range []string{"https://evil.example/", "https://slack.com.evil.io/", "http://app.slack.com/", "https://evil.example/app.slack.com"} {
		if c := h.tt.cookie("get", arg(u)); c != nil {
			t.Errorf("cookies.get(%q) = %v, want nil", u, c)
		}
		if all, _ := h.tt.cookie("getAll", arg(u)).([]any); len(all) != 0 {
			t.Errorf("cookies.getAll(%q) returned the d cookie", u)
		}
	}
}

// A saved account's token and cookie never go to a non-Slack TeamURL.
func TestCheckAuthStaysOnSlack(t *testing.T) {
	h := newHarness(t)
	var hits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer evil.Close()
	for _, base := range []string{evil.URL + "/", "https://evil.example/", "https://hackclub.slack.com.evil.io/"} {
		a := storedAccount{Creds: slack.Creds{Token: "xoxc-other", Cookie: "xoxd-other", TeamURL: base}}
		if got := h.tt.checkAuth(context.Background(), a); got != "invalid" {
			t.Errorf("checkAuth(TeamURL %s) = %q, want invalid", base, got)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("credentials sent to a non-Slack host %d times", hits.Load())
	}
}
