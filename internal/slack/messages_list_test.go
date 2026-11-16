package slack_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// mlChannel is the i-th made-up channel of the messages.list tests; the
// ids sort in i order.
func mlChannel(i int) string { return fmt.Sprintf("C0ML%05d", i) }

// mlFake answers messages.list on internal/fakeslack with one made-up
// message per asked (channel, ts), and too_many_channels for any call
// that carries a channel in fail. It records each call's channels in
// order and the most calls it saw in flight at once.
type mlFake struct {
	mu       sync.Mutex
	calls    [][]string
	inFlight int
	maxIn    int
}

func startMLFake(t *testing.T, fail map[string]bool) (*slack.Client, *mlFake) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	f := &mlFake{}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "messages.list" {
			return nil, false
		}
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		_ = json.Unmarshal([]byte(form.Get("message_ids")), &want)
		var chans []string
		bad := false
		out := map[string][]map[string]any{}
		for _, w := range want {
			chans = append(chans, w.Channel)
			bad = bad || fail[w.Channel]
			for _, ts := range w.Timestamps {
				out[w.Channel] = append(out[w.Channel], map[string]any{"type": "message", "ts": ts, "user": "U0FAKE00001", "text": "synthetic " + w.Channel})
			}
		}
		f.mu.Lock()
		f.calls = append(f.calls, chans)
		f.inFlight++
		f.maxIn = max(f.maxIn, f.inFlight)
		f.mu.Unlock()
		// long enough for the other worker's call to arrive meanwhile
		time.Sleep(150 * time.Millisecond)
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
		if bad {
			return map[string]any{"ok": false, "error": "too_many_channels"}, true
		}
		return map[string]any{"ok": true, "messages": out}, true
	})
	return slack.New(fs.Creds()), f
}

// A page with 23 channels goes out the way Slack's innerFetchMessagesByIds
// sends it (client-boot ptFS: pMap(chunk(query, 10), …, {concurrency:
// 2})): the sorted channels 10 to a call, never more than 2 calls in
// flight, each channel asked once; every message comes back merged.
func TestMessagesListGroupsOfTen(t *testing.T) {
	c, f := startMLFake(t, nil)
	want := map[string][]string{}
	for i := range 23 {
		want[mlChannel(i)] = []string{"1762800100.000100", "1762800200.000200"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, answered, err := c.MessagesList(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := slices.Clone(f.calls)
	maxIn := f.maxIn
	f.mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("%d calls for 23 channels, want 3: %v", len(calls), calls)
	}
	// the workers take the groups in order, but which call lands first is
	// up to the scheduler: compare the set of groups
	var groups [][]string
	for i := 0; i < 23; i += slack.MessagesListGroup {
		var g []string
		for j := i; j < min(i+slack.MessagesListGroup, 23); j++ {
			g = append(g, mlChannel(j))
		}
		groups = append(groups, g)
	}
	for _, call := range calls {
		if len(call) > slack.MessagesListGroup {
			t.Errorf("a call carried %d channels: %v", len(call), call)
		}
		if !slices.ContainsFunc(groups, func(g []string) bool { return slices.Equal(g, call) }) {
			t.Errorf("call %v isn't one of the sorted groups of 10 %v", call, groups)
		}
	}
	if maxIn != slack.MessagesListConcurrency {
		t.Errorf("%d calls in flight at most, want %d (Slack's concurrency)", maxIn, slack.MessagesListConcurrency)
	}
	if slack.MessagesListGroup != 10 || slack.MessagesListConcurrency != 2 {
		t.Errorf("group %d, concurrency %d: Slack's are 10 and 2", slack.MessagesListGroup, slack.MessagesListConcurrency)
	}
	for i := range 23 {
		ch := mlChannel(i)
		if !answered[ch] || len(got[ch]) != 2 || got[ch][0].Text != "synthetic "+ch {
			t.Errorf("%s: answered %v, messages %+v", ch, answered[ch], got[ch])
		}
	}
}

// A group that fails (too_many_channels) costs only its own channels:
// they're unanswered with no messages, the error says so, and the other
// groups' channels are answered with their messages.
func TestMessagesListFailedGroupOnlyItsChannels(t *testing.T) {
	c, _ := startMLFake(t, map[string]bool{mlChannel(14): true})
	want := map[string][]string{}
	for i := range 23 {
		want[mlChannel(i)] = []string{"1762800100.000100"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, answered, err := c.MessagesList(ctx, want)
	if err == nil {
		t.Error("no error for the failed group")
	}
	for i := range 23 {
		ch := mlChannel(i)
		failed := i >= 10 && i < 20
		if answered[ch] == failed || (len(got[ch]) == 0) != failed {
			t.Errorf("%s (failed group %v): answered %v, %d messages", ch, failed, answered[ch], len(got[ch]))
		}
	}
}
