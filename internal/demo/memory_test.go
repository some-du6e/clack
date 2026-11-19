package demo

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The browser demo's wiring (StartInMemory) on a normal process: the
// client signs in, reads #general and posts to it, the realtime socket
// says hello and brings the post back, and the image fetcher reads the
// fake's pictures, all over in-memory connections with nothing listening
// on a port. In a child process, because the network lock and the
// loopback dialer are process-wide and one-way.
func TestInMemory(t *testing.T) {
	if os.Getenv("CLACK_DEMO_INMEMORY_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestInMemory$", "-test.v", "-test.count=1")
		cmd.Env = append(os.Environ(), "CLACK_DEMO_INMEMORY_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Logf("child:\n%s", out)
		if err != nil {
			t.Fatalf("child: %v", err)
		}
		return
	}
	s, err := StartInMemory(Options{Quiet: true, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !strings.HasPrefix(s.FS.URL, "http://127.0.0.1:80/") {
		t.Fatalf("the fake's URL is %q", s.FS.URL)
	}
	c, err := s.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.AuthTest(ctx); err != nil {
		t.Fatalf("auth.test: %v", err)
	}
	h, err := c.History(ctx, GeneralID, slack.HistoryOpts{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(h.Messages) == 0 {
		t.Fatal("#general has no history")
	}

	// the socket over the in-memory listener (third_party/websocket)
	r := c.NewRTM()
	hello := make(chan struct{}, 1)
	r.OnState = func(up bool) {
		if up {
			select {
			case hello <- struct{}{}:
			default:
			}
		}
	}
	go r.Run(ctx)
	select {
	case <-hello:
	case <-ctx.Done():
		t.Fatal("no hello from the realtime socket")
	}
	const text = "posted over memnet"
	if _, err := c.Post(ctx, slack.PostOpts{Channel: GeneralID, Text: text}); err != nil {
		t.Fatalf("chat.postMessage: %v", err)
	}
	for got := false; !got; {
		select {
		case ev := <-r.Events:
			got = ev.Type == "message" && strings.Contains(string(ev.Raw), text)
		case <-ctx.Done():
			t.Fatal("the post never came back over the socket")
		}
	}
	found := false
	for _, m := range s.World.History(GeneralID) {
		found = found || (m.User == SelfID && m.Text == text)
	}
	if !found {
		t.Fatal("the post isn't in #general's history")
	}

	// a picture of the fake's own, through the image cache's fetcher
	b, err := s.Fetch(ctx, s.FS.URL+"img/32x32/memnet.png")
	if err != nil || len(b) < 8 || string(b[1:4]) != "PNG" {
		t.Fatalf("fetch: %d bytes, %v", len(b), err)
	}
	if refused := s.Refused(); len(refused) != 0 {
		t.Fatalf("refused: %v", refused)
	}
}
