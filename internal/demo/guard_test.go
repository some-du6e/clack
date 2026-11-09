package demo

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// A demo session refuses any host that isn't the loopback fake: demo
// credentials for another host don't make a client, and once the process
// is locked (clack demo locks it first thing) every transport refuses
// every other host, whatever the URL, with or without credentials. No
// request in this test leaves the machine: the lock comes first.
func TestDemoRefusesNonLoopbackHosts(t *testing.T) {
	LockNetwork()
	if !tlsfp.IsLoopbackOnly() {
		t.Fatal("LockNetwork didn't turn loopback-only mode on")
	}
	for _, u := range []string{"https://slack.com/", "https://hackclub.slack.com/", "https://app.slack.com/",
		"https://hackclub.enterprise.slack.com/", "http://10.0.0.1/", "http://192.168.1.1:8080/", "http://example.com/",
		"http://127.0.0.1.nip.io/", "http://user@127.0.0.1/", "ftp://127.0.0.1/", "", "127.0.0.1"} {
		if _, err := NewClient(slack.Creds{Token: "xoxc-x", TeamURL: u}); err == nil {
			t.Errorf("NewClient accepted team URL %q", u)
		}
		if _, err := NewClient(slack.Creds{Token: "xoxc-x", TeamURL: "http://127.0.0.1:1/", EnterpriseURL: u}); err == nil && u != "" {
			t.Errorf("NewClient accepted enterprise URL %q", u)
		}
	}

	s, err := Start(Options{Quiet: true, Speed: 50, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	cr := s.Creds()
	for _, u := range []string{cr.TeamURL, cr.EnterpriseURL} {
		if err := CheckLoopback(u); err != nil {
			t.Fatalf("the session's own credentials: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	var who struct {
		Team   string `json:"team"`
		UserID string `json:"user_id"`
	}
	if err := c.Call(ctx, "auth.test", nil, &who); err != nil || who.UserID != SelfID || who.Team != TeamName {
		t.Fatalf("auth.test on the fake: %+v %v", who, err)
	}

	refused := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: went through", what)
		} else if !strings.Contains(err.Error(), "non-loopback") {
			t.Errorf("%s: %v, want the loopback guard's refusal", what, err)
		}
	}
	// a Slack client pointed at real Slack (as a bug might build one)
	real := slack.New(slack.Creds{Token: "xoxc-x", Cookie: "xoxd-x", TeamURL: "https://slack.com/"})
	refused("slack client to slack.com", real.Call(ctx, "auth.test", nil, nil))
	// the credential-free public client (CDN, image proxy)
	_, err = slack.PublicHTTP.Get("https://a.slack-edge.com/production-standard-emoji-assets/16.0/apple-large/1f600.png")
	refused("slack.PublicHTTP", err)
	// tlsfp's mixed client (plugins, themes, the update check, ring sounds)
	_, err = tlsfp.Client.Get("https://slack.com/api/api.test")
	refused("tlsfp.Client to Slack", err)
	_, err = tlsfp.Client.Get("https://example.com/")
	refused("tlsfp.Client elsewhere", err)
	// Go's default client (anything else)
	_, err = http.Get("http://93.184.216.34/")
	refused("http.DefaultClient", err)

	// the session's image fetcher: the fake and locally drawn standard
	// emoji only, never the CDN or anything else
	u, ok := emoji.StandardURL("smile", "apple", "large")
	if !ok {
		t.Fatal("no standard URL for smile")
	}
	if b, err := s.Fetch(ctx, u); err != nil || len(b) == 0 {
		t.Errorf("standard emoji drawn locally: %d bytes, %v", len(b), err)
	}
	if _, err := s.Fetch(ctx, "https://example.com/cat.png"); err == nil {
		t.Error("Fetch fetched a non-demo URL")
	}
	if b, err := s.Fetch(ctx, s.FS.URL+"img/demo/team/88.png"); err != nil || len(b) == 0 {
		t.Errorf("the team icon from the fake: %d bytes, %v", len(b), err)
	}
	// and the session still works after all that
	if err := c.Call(ctx, "auth.test", nil, &who); err != nil {
		t.Fatalf("auth.test after the refusals: %v", err)
	}
}
