package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Synthetic fixtures only.

func TestMissingNamesAreRecorded(t *testing.T) {
	a, _ := testApp(t)
	a.S.TakeMissing() // drop anything recorded by setup
	a.message("C1", &slack.Message{TS: "1.0", User: "U1", Text: "see <#C0A9ZZZZ> and <@U0NOPE> and <#C1>"})
	chans, users, _ := a.S.TakeMissing()
	if strings.Join(chans, ",") != "C0A9ZZZZ" || strings.Join(users, ",") != "U0NOPE" {
		t.Errorf("missing chans=%v users=%v", chans, users)
	}
	a.message("C1", &slack.Message{TS: "2.0", User: "U1", Text: "<#C0A9ZZZZ> again"})
	if c, _, _ := a.S.TakeMissing(); len(c) != 0 {
		t.Errorf("recorded twice: %v", c)
	}
}

func TestResolveDMsAndBots(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["bots.info"] = `{"ok":true,"bots":[{"id":"B7","name":"deploy-bot","icons":{"image_72":"https://img.example/b72.png"}}]}`
	got := a.resolve(context.Background(), []string{"D9"}, nil, []string{"B7"})
	sort.Strings(got)
	if strings.Join(got, ",") != "B7,D9" {
		t.Fatalf("resolved %v", got)
	}
	if f.last("bots.info").Get("bots") != "B7" {
		t.Errorf("bots.info params: %v", f.last("bots.info"))
	}
	if n := a.S.ChannelName("D9"); n != "Bob B" {
		t.Errorf("closed DM name = %q", n)
	}
	c, ok := a.Conv("D9")
	if !ok || c.Kind != "im" || c.User != "U2" || c.Name != "Bob B" {
		t.Errorf("Conv(D9) = %+v %v", c, ok)
	}
	if snap := (func() Snapshot { close(a.ready); return a.Snapshot() })(); len(snap.Convs) != 2 {
		t.Errorf("closed DM leaked into the sidebar: %d convs", len(snap.Convs))
	}
	v := a.message("C1", &slack.Message{TS: "3.0", BotID: "B7", Subtype: "bot_message", Text: "x"})
	if v.Avatar != "https://img.example/b72.png" || v.Name != "deploy-bot" {
		t.Errorf("bot without profile: avatar=%q name=%q", v.Avatar, v.Name)
	}
	a.message("C1", &slack.Message{TS: "4.0", BotID: "B8", Subtype: "bot_message"})
	if _, _, bots := a.S.TakeMissing(); strings.Join(bots, ",") != "B8" {
		t.Errorf("unknown bot not recorded: %v", bots)
	}
}

func TestHistoryResolvesClosedDM(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["conversations.history"] = `{"ok":true,"messages":[{"ts":"5.000001","user":"U2","text":"hey"}],"has_more":false}`
	p, err := a.History(context.Background(), "D9", "")
	if err != nil || p.Conv == nil || p.Conv.Name != "Bob B" || p.Conv.Kind != "im" || len(p.Messages) != 1 {
		t.Fatalf("closed DM history: %+v %v", p, err)
	}
}

func TestEmptyAppDMsHiddenAndSorted(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACKBOT", Name: "slackbot"}, {ID: "UZ", Name: "zeta", IsBot: true}, {ID: "UA", Name: "Alpha", IsBot: true}})
	a.S.PutConv(slack.Conversation{ID: "D5", IsIM: true, User: "USLACKBOT"})
	a.S.PutConv(slack.Conversation{ID: "D6", IsIM: true, User: "UZ"})
	a.S.PutConv(slack.Conversation{ID: "D7", IsIM: true, User: "UA"})
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "UB"})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D5", Latest: "0000000000.000000"}, {ID: "D6", Latest: "9.0"}, {ID: "D7", Latest: "8.0"}}})
	close(a.ready)
	for _, s := range a.Snapshot().Sections {
		if s.Type == "apps" {
			if got := strings.Join(s.Channels, ","); got != "D7,D2,D6" { // Alpha, bot, zeta; empty Slackbot DM hidden
				t.Errorf("apps = %s", got)
			}
		}
	}
}

func TestAddEmojiValidation(t *testing.T) {
	a, _ := testApp(t)
	if err := a.AddEmoji(context.Background(), " :: ", []byte("x")); err == nil {
		t.Error("empty name accepted")
	}
	if err := a.AddEmoji(context.Background(), "parrot", nil); err == nil {
		t.Error("empty image accepted")
	}
	if emojiExt([]byte("GIF89a....")) != ".gif" || emojiExt([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) != ".png" {
		t.Error("emojiExt")
	}
}

// signOutRT counts requests by whether they started after sign-out and
// tracks those in flight, so the test can wait for them without sleeping.
type signOutRT struct {
	base     http.RoundTripper
	c        *slack.Client
	mu       sync.Mutex
	after    []string // URLs of requests that started after sign-out
	inflight sync.WaitGroup
}

func (r *signOutRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.c.SignedOut() {
		r.mu.Lock()
		r.after = append(r.after, req.URL.Path)
		r.mu.Unlock()
	}
	r.inflight.Add(1)
	defer r.inflight.Done()
	return r.base.RoundTrip(req)
}

func TestAuthFailedStopsCore(t *testing.T) {
	a, f := testApp(t)
	rt := &signOutRT{base: a.C.HTTP.Transport, c: a.C}
	a.C.HTTP.Transport = rt
	f.reply["conversations.history"] = `{"ok":false,"error":"invalid_auth"}`
	evs, stop := a.Subscribe()
	defer stop()
	if _, err := a.History(context.Background(), "C1", ""); !slack.IsAuthError(err) {
		t.Fatalf("history: %v", err)
	}
	if e := next(t, evs, "auth_failed"); e.Name != "invalid_auth" {
		t.Errorf("event: %+v", e)
	}
	if a.bg.Err() == nil {
		t.Error("background context not cancelled")
	}
	rt.inflight.Wait() // requests already in flight at sign-out finish first
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	for i := 0; i < 30; i++ { // the UI's refetch loop
		if _, err := a.History(context.Background(), "C1", ""); !errors.Is(err, slack.ErrSignedOut) {
			t.Fatalf("refetch: %v", err)
		}
		a.Mark(context.Background(), "C1", "1.0")
	}
	// background work scheduled before sign-out must not send either
	<-a.later(0, func() { _ = a.loadUsergroups(a.bg) })
	_ = a.loadEmoji(a.bg)
	rt.inflight.Wait()
	f.mu.Lock()
	after := len(f.calls)
	f.mu.Unlock()
	rt.mu.Lock()
	sent := rt.after
	rt.mu.Unlock()
	if len(sent) != 0 || after != before || !a.SignedOut() {
		t.Errorf("%d requests after sign-out (server saw %d): %v", len(sent), after-before, sent)
	}
}

func TestMessageRawFieldsForPlugins(t *testing.T) {
	a, _ := testApp(t)
	var m slack.Message
	_ = json.Unmarshal([]byte(`{"ts":"1.0","bot_id":"B1","text":"x","metadata":{"event_type":"e"},
		"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hi"}}],"attachments":[{"fallback":"f","x":1}],
		"bot_profile":{"id":"B1","name":"relay","app_id":"A1","user_id":"U8","icons":{"image_48":"https://img.example/48.png"}}}`), &m)
	v := a.message("C1", &m)
	if string(v.Metadata) != `{"event_type":"e"}` || !strings.Contains(string(v.Blocks), `"section"`) || !strings.Contains(string(v.Attachments), `"x":1`) {
		t.Errorf("raw: meta=%s blocks=%s att=%s", v.Metadata, v.Blocks, v.Attachments)
	}
	if v.BotProfile == nil || v.BotProfile.UserID != "U8" || v.BotProfile.Icon != "https://img.example/48.png" {
		t.Errorf("bot profile: %+v", v.BotProfile)
	}
}
