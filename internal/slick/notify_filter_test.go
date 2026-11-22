package slick

import (
	"testing"
	"time"
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
