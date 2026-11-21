package core

// An app card whose fetch fails follows Slack's UnknownFetchManager (0mwJ)
// and the apps refetcher (FftE over p7oS): only timeouts are tried again,
// three attempts in all; any other error leaves the app non-existent,
// answered to every caller without a request, until a slow reconnect
// (_maybeResetState's "Connection count a -> n") or an app event
// (apps_installed, apps_changed, apps_uninstalled, bot_changed) marks it
// stale. One caller's deadline ends its own wait, not the shared fetch.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	cardOKFmt   = `{"ok":true,"app_profile":{"id":"%s","name":"Atlas","is_agent_app":true}}`
	cardRefused = `{"ok":false,"error":"app_not_found"}`
)

// cardApp is testApp with bot users UBOT1…UBOT6 (bots BBOT1…, apps
// ABOT1…), and the client's failure backoff at 1 ms, so a repeated request
// the code sends reaches the fake.
func cardApp(t *testing.T) (*App, *fakeSlack) {
	t.Helper()
	a, f := testApp(t)
	var us []slack.User
	for i := 1; i <= 6; i++ {
		n := string(rune('0' + i))
		us = append(us, slack.User{ID: "UBOT" + n, Name: "atlas" + n, IsBot: true, Profile: slack.Profile{BotID: "BBOT" + n, APIAppID: "ABOT" + n}})
	}
	a.S.PutUsers(us)
	prev := slack.BackoffBase
	slack.BackoffBase = time.Millisecond
	t.Cleanup(func() { slack.BackoffBase = prev })
	return a, f
}

// setCardReply sets apps.profile.get's answer and hook (nil: none).
func setCardReply(f *fakeSlack, body string, hook func(url.Values)) {
	f.mu.Lock()
	f.reply["apps.profile.get"] = body
	if hook == nil {
		delete(f.hooks, "apps.profile.get")
	} else {
		f.hooks["apps.profile.get"] = hook
	}
	f.mu.Unlock()
}

func cardOK(app string) string { return fmt.Sprintf(cardOKFmt, app) }

func TestAppCardNonExistentUntilStale(t *testing.T) {
	a, f := cardApp(t)
	ctx := context.Background()
	a.socketConnected(false) // the boot connection: the cards' first epoch
	evs, stop := a.Subscribe()
	defer stop()

	setCardReply(f, cardRefused, nil)
	if _, err := a.AppCardOf(ctx, "UBOT1", ""); err == nil {
		t.Fatal("a refused fetch answered a card")
	}
	if n := f.count("apps.profile.get"); n != 1 {
		t.Fatalf("a refused fetch sent %d requests, want 1 (only a timeout is tried again)", n)
	}
	if _, ok, known := a.AppCardSettled("UBOT1", ""); ok || !known {
		t.Errorf("after the refusal: settled ok %v known %v, want known with no card (non-existent)", ok, known)
	}
	setCardReply(f, cardOK("ABOT1"), nil)
	for i := 0; i < 3; i++ {
		if _, err := a.AppCardOf(ctx, "UBOT1", ""); err == nil {
			t.Errorf("read %d after the refusal answered a card, want the app non-existent until it is marked stale", i+1)
		}
	}
	a.socketConnected(true) // a fast reconnect (a resume) marks nothing
	if _, err := a.AppCardOf(ctx, "UBOT1", ""); err == nil {
		t.Error("a fast reconnect made the non-existent app ask again")
	}
	if n := f.count("apps.profile.get"); n != 1 {
		t.Fatalf("the non-existent app sent %d requests in all, want only the refused one", n)
	}

	// a slow reconnect marks every card stale: the next read asks again
	a.socketConnected(false)
	next(t, evs, "app_card")
	if _, _, known := a.AppCardSettled("UBOT1", ""); known {
		t.Error("after a slow reconnect the failure is still known, want it forgotten")
	}
	card, err := a.AppCardOf(ctx, "UBOT1", "")
	if err != nil || card.AppID != "ABOT1" || !card.AgentApp {
		t.Errorf("after the slow reconnect: %+v %v, want the card", card, err)
	}
	if n := f.count("apps.profile.get"); n != 2 {
		t.Errorf("%d requests, want the refused one and one after the reconnect", n)
	}

	// each app event marks its app's (or bot's) card stale
	for i, c := range []struct {
		typ    string
		fields func(app, bot string) map[string]any
	}{
		{"apps_installed", func(app, _ string) map[string]any { return map[string]any{"app": map[string]any{"id": app}} }},
		{"apps_changed", func(app, _ string) map[string]any { return map[string]any{"app": map[string]any{"id": app}} }},
		{"apps_uninstalled", func(app, _ string) map[string]any { return map[string]any{"app_id": app} }},
		{"bot_changed", func(app, bot string) map[string]any {
			return map[string]any{"bot": map[string]any{"id": bot, "app_id": app}}
		}},
	} {
		n := string(rune('2' + i))
		user, app, bot := "UBOT"+n, "ABOT"+n, "BBOT"+n
		setCardReply(f, cardRefused, nil)
		if _, err := a.AppCardOf(ctx, user, ""); err == nil {
			t.Fatalf("%s: the refused fetch answered a card", c.typ)
		}
		before := f.count("apps.profile.get")
		setCardReply(f, cardOK(app), nil)
		a.handle(rtm(c.typ, c.fields("AOTHER", "BOTHER"))) // another app's event marks nothing
		if _, err := a.AppCardOf(ctx, user, ""); err == nil || f.count("apps.profile.get") != before {
			t.Errorf("%s for another app made %s ask again", c.typ, user)
		}
		a.handle(rtm(c.typ, c.fields(app, bot)))
		next(t, evs, "app_card")
		card, err := a.AppCardOf(ctx, user, "")
		if err != nil || card.AppID != app {
			t.Errorf("%s: after the event %+v %v, want the card asked for again", c.typ, card, err)
		}
		if got := f.count("apps.profile.get"); got != before+1 {
			t.Errorf("%s: %d requests after the event, want 1", c.typ, got-before)
		}
	}
}

func TestAppCardRetriesOnlyTimeouts(t *testing.T) {
	a, f := cardApp(t)
	prev := appCardAttempt
	appCardAttempt = 100 * time.Millisecond
	t.Cleanup(func() { appCardAttempt = prev })
	ctx := context.Background()

	// two attempts time out, the third answers
	var calls atomic.Int32
	setCardReply(f, cardOK("ABOT1"), func(url.Values) {
		if calls.Add(1) <= 2 {
			time.Sleep(400 * time.Millisecond)
		}
	})
	card, err := a.AppCardOf(ctx, "UBOT1", "")
	if err != nil || card.AppID != "ABOT1" {
		t.Fatalf("after two timeouts: %+v %v, want the third attempt's card", card, err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("%d attempts, want 3 (two timed out)", n)
	}

	// three timeouts: no fourth attempt, and the app is non-existent
	calls.Store(0)
	setCardReply(f, cardOK("ABOT2"), func(url.Values) {
		calls.Add(1)
		time.Sleep(400 * time.Millisecond)
	})
	if _, err := a.AppCardOf(ctx, "UBOT2", ""); err == nil {
		t.Fatal("three timeouts answered a card")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("%d attempts for a fetch that kept timing out, want 3", n)
	}
	if _, err := a.AppCardOf(ctx, "UBOT2", ""); err == nil || calls.Load() != 3 {
		t.Errorf("a read after three timeouts sent another request (%d in all), want the app non-existent", calls.Load())
	}

	// any other error ends the fetch at once
	calls.Store(0)
	setCardReply(f, cardRefused, func(url.Values) { calls.Add(1) })
	if _, err := a.AppCardOf(ctx, "UBOT3", ""); err == nil {
		t.Fatal("a refused fetch answered a card")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("a refused fetch made %d attempts, want 1", n)
	}
}

func TestAppCardCallerDeadlineEndsOnlyItsWait(t *testing.T) {
	a, f := cardApp(t)
	gate := make(chan struct{})
	setCardReply(f, cardOK("ABOT1"), func(url.Values) { <-gate })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.AppCardOf(ctx, "UBOT1", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the first caller's deadline: %v, want its own context's error", err)
	}
	close(gate)
	card, err := a.AppCardOf(context.Background(), "UBOT1", "")
	if err != nil || card.AppID != "ABOT1" {
		t.Errorf("a second caller after the first one's deadline: %+v %v, want the shared fetch's card", card, err)
	}
	if n := f.count("apps.profile.get"); n != 1 {
		t.Errorf("%d requests, want the one shared fetch", n)
	}
}

// A member with no bot and no app (getAppById has nothing to ask for) is
// known at once and never fetched; a slow reconnect forgets it like any
// other card.
func TestAppCardNotAnApp(t *testing.T) {
	a, f := cardApp(t)
	ctx := context.Background()
	a.socketConnected(false)
	if _, err := a.AppCardOf(ctx, "U1", ""); !errors.Is(err, ErrNotAnApp) {
		t.Fatalf("a member with no bot: %v, want ErrNotAnApp", err)
	}
	if _, ok, known := a.AppCardSettled("U1", ""); ok || !known {
		t.Errorf("settled ok %v known %v, want known with no card", ok, known)
	}
	a.socketConnected(false)
	if _, _, known := a.AppCardSettled("U1", ""); known {
		t.Error("a slow reconnect kept the member known as not an app, want it forgotten like any card")
	}
	if _, err := a.AppCardOf(ctx, "U1", ""); !errors.Is(err, ErrNotAnApp) {
		t.Errorf("read again: %v, want ErrNotAnApp", err)
	}
	if n := f.count("apps.profile.get"); n != 0 {
		t.Errorf("a member with no bot sent %d apps.profile.get requests, want none", n)
	}
}

// A reset asks again at once, even right after the failure it forgets:
// with the client's failure backoff at its default, the read after an app
// event or a slow reconnect still reaches the server (the guard would
// answer it with the refusal it remembers).
func TestAppCardResetAsksPastBackoff(t *testing.T) {
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UBOT1", Name: "atlas1", IsBot: true, Profile: slack.Profile{BotID: "BBOT1", APIAppID: "ABOT1"}}})
	ctx := context.Background()
	a.socketConnected(false)
	for i, reset := range []func(){
		func() { a.handle(rtm("apps_changed", map[string]any{"app": map[string]any{"id": "ABOT1"}})) },
		func() { a.socketConnected(false) },
	} {
		setCardReply(f, cardRefused, nil)
		if _, err := a.AppCardOf(ctx, "UBOT1", ""); err == nil {
			t.Fatalf("reset %d: the refused fetch answered a card", i)
		}
		before := f.count("apps.profile.get")
		setCardReply(f, cardOK("ABOT1"), nil)
		reset()
		card, err := a.AppCardOf(ctx, "UBOT1", "")
		if err != nil || card.AppID != "ABOT1" {
			t.Errorf("reset %d: the read right after it answered %+v %v, want the card (sent past the backoff)", i, card, err)
		}
		if n := f.count("apps.profile.get") - before; n != 1 {
			t.Errorf("reset %d: %d requests after the reset, want 1", i, n)
		}
		// make the card fail again for the next reset: stale, and no card to keep
		c := a.appCards()
		c.mu.Lock()
		delete(c.m, a.appCardKey("UBOT1", ""))
		c.mu.Unlock()
	}
}
