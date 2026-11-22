package slick

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/taut"
)

// A main half's notifications.filter runs over the toast about to show,
// as Slick's Notification.prototype.show patch: it sees the toast's
// title, subtitle and body, one that doesn't permit it keeps it, and with
// no filter left a toast asks no JS.
func TestAllowNotificationRunsTheFilters(t *testing.T) {
	h := newHarness(t)
	toast := NotificationOptions{Title: "New message in #synthetic", Subtitle: "Fake Club", Body: "someone: quiet please"}
	if !h.s.AllowNotification(h.ctx(), toast) {
		t.Fatal("with no plugin a toast was kept")
	}
	mustImport(t, h, probePlugin("QuietProbe", `{}`, ""))
	mustImport(t, h, probeMain("QuietProbe", `["notifications"]`, `
  async ready(ctx) {
    globalThis.__quietSeen = [];
    return ctx.notifications.filter((o) => {
      globalThis.__quietSeen.push({ title: o.title, subtitle: o.subtitle, body: o.body, silent: o.silent });
      return !String(o.body).includes("quiet please");
    });
  }`))
	if err := h.s.SetEnabled(h.ctx(), "QuietProbe", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the filter registered", func() bool { return h.s.noteFilters.Load() == 1 })
	if h.s.AllowNotification(h.ctx(), toast) {
		t.Error("a toast the filter doesn't permit was shown")
	}
	if !h.s.AllowNotification(h.ctx(), NotificationOptions{Title: "New message from someone", Body: "a synthetic line"}) {
		t.Error("a toast the filter permits was kept")
	}
	var seen []NotificationOptions
	h.jsInto(`return globalThis.__quietSeen`, &seen)
	if len(seen) != 2 || seen[0] != toast {
		t.Errorf("the filter saw %+v, want the two toasts as they were about to show, first %+v", seen, toast)
	}
	if err := h.s.SetEnabled(h.ctx(), "QuietProbe", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the filter gone", func() bool { return h.s.noteFilters.Load() == 0 })
	if !h.s.AllowNotification(h.ctx(), toast) {
		t.Error("with the plugin off a toast was kept")
	}
}

// A showNotification dispatch is filtered as it comes, as Slick's patch
// sees every notification Slack shows: one a filter doesn't permit never
// reaches its native (Streamer Mode, Shut Up Slackbot), and with no
// filter the thunk isn't patched. Clack's own toasts (_clackToast) pass
// the thunk: they're asked as they're about to show, with the title and
// body they show (AllowNotification). Round 3 asked only there, and a
// dispatch with a native of its own skipped the filters.
func TestShowNotificationDispatchRunsTheFilters(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, probePlugin("HushProbe", `{}`, ""))
	mustImport(t, h, probeMain("HushProbe", `["notifications"]`, `
  async ready(ctx) {
    return ctx.notifications.filter((o) => !String(o.body).includes("hush"));
  }`))
	if err := h.s.SetEnabled(h.ctx(), "HushProbe", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the filter registered", func() bool { return h.s.noteFilters.Load() == 1 })
	eventually(t, 10*time.Second, "the thunk patched", func() bool { return h.tt.ThunkPatched("showNotification") })
	shown := 0
	native := func(context.Context, map[string]any) (any, error) { shown++; return nil, nil }
	dispatch := func(args map[string]any) {
		t.Helper()
		if _, err := taut.Dispatch(h.ctx(), h.tt, "showNotification", args, native); err != nil {
			t.Fatal(err)
		}
	}
	dispatch(map[string]any{"title": "someone", "message": "hush now"})
	if shown != 0 {
		t.Error("a showNotification the filter doesn't permit reached its native")
	}
	dispatch(map[string]any{"title": "someone", "message": "a synthetic line"})
	if shown != 1 {
		t.Errorf("a permitted showNotification reached its native %d times, want once", shown)
	}
	dispatch(map[string]any{"title": "someone", "message": "hush now", "_clackToast": true})
	if shown != 2 {
		t.Error("Clack's own toast was filtered at the thunk: it's asked as it's about to show")
	}
	if err := h.s.SetEnabled(h.ctx(), "HushProbe", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the thunk unpatched", func() bool { return !h.tt.ThunkPatched("showNotification") })
}
