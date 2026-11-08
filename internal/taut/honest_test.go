package taut

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// A plugin's fetch to one of Slack's hosts goes out as Clack: its own
// User-Agent, no client hints, fetch metadata, Origin or Referer, whatever
// the plugin set. Other hosts get the plugin's headers unchanged.
func TestPluginFetchToSlackIsHonest(t *testing.T) {
	var got []*http.Request
	fetch := honestFetch(func(ctx context.Context, req *http.Request) (*http.Response, error) {
		got = append(got, req)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
	})
	browser := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"
	for _, u := range []string{"https://hackclub.slack.com/api/users.info", "https://edgeapi.slack.com/cache/E1/users/info", "https://a.slack-edge.com/x.png", "https://example.com/x"} {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("User-Agent", browser)
		req.Header.Set("sec-ch-ua", `"Chromium";v="157"`)
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Origin", "https://app.slack.com")
		req.Header.Set("Referer", "https://app.slack.com/client/T1")
		res, err := fetch(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	for _, r := range got[:3] {
		if ua := r.Header.Get("User-Agent"); ua != slack.UserAgent() {
			t.Errorf("%s: User-Agent %q, want %q", r.URL.Host, ua, slack.UserAgent())
		}
		for k := range r.Header {
			if slack.BrowserOnlyHeader(k) {
				t.Errorf("%s: sent %s", r.URL.Host, k)
			}
		}
	}
	if o := got[3]; o.Header.Get("User-Agent") != browser || o.Header.Get("Origin") == "" {
		t.Errorf("a non-Slack host's headers changed: %v", o.Header)
	}
}
