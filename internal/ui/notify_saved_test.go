package ui

// notify-relatives part 6: a Later reminder coming due (saved_due), as
// Slack's client shows it (notify_saved.go; docs/NOTIFY-RULES.md §1, §6).
// Synthetic people and text only.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/logtag"
)

// A message's Later reminder notifies as it comes due: "Reminder due",
// the message as a message's notification writes it, the item's key as
// its msg, silent and with no reply box, under Do Not Disturb too
// (shouldShowNotificationForSavedItem has no DND check); its click opens
// Later with the message beside the list (openSavedForLaterView). With
// global_desktop stored as nothing it doesn't show (14). Clack ignored
// saved_due.
func TestLaterReminderLikeSlack(t *testing.T) {
	e, fs, c, rec := followEnv(t)
	logs := captureLog(t)
	ch := fs.ChannelIDs()[2]
	sender := fs.UserIDs()[3]
	due := func(ts string) {
		now := time.Now().Unix()
		fs.Push(map[string]any{"type": "saved_due", "saved": map[string]any{"item_id": ch, "item_type": "message", "ts": ts,
			"todo_state": "to_do", "state": "in_progress", "date_created": now - 3600, "date_due": now}})
	}
	c.SetDNDStatus(core.DNDStatus{SnoozeEnabled: true, SnoozeEnd: time.Now().Add(time.Hour).Unix()})
	ts, _ := fs.PostQuiet(ch, sender, "a synthetic errand to finish")
	due(ts)
	n := noteWith(e, rec, "a synthetic errand to finish", 15*time.Second)
	if n == nil {
		t.Fatalf("no notification for the reminder under Do Not Disturb (its notify lines: %q)", logs.matching("notify: "))
	}
	key := ch + "-" + ts
	if n.Title != "Reminder due" || n.Msg != key || n.Channel != ch || !n.Silent || n.HasReply {
		t.Errorf("the reminder: title %q, msg %q, channel %q, silent %v, reply box %v; want Reminder due, %q, %q, silent, none",
			n.Title, n.Msg, n.Channel, n.Silent, n.HasReply, key, ch)
	}
	if !strings.HasSuffix(n.Body, ": a synthetic errand to finish") || strings.HasPrefix(n.Body, ":") {
		t.Errorf("the reminder's text %q, want the sender's name and the message", n.Body)
	}
	if want := "notify: show saved_due message " + logtag.ID(ch) + "/" + logtag.ID(ts); !contains(logs.matching("notify: "), want) {
		t.Errorf("no %q in clack.log (its notify lines: %q)", want, logs.matching("notify: "))
	}
	click(t, e.a, n, "toast", 0)
	if !e.untilSoft(15*time.Second, func() bool {
		sch, sts := e.a.SecondaryAt()
		return e.a.router.Current().Page() == "later" && sch == ch && sts == ts
	}) {
		sch, sts := e.a.SecondaryAt()
		t.Errorf("after the click the route is %q with %s/%s beside it; want Later with the message", e.a.router.Current(), logtag.ID(sch), logtag.ID(sts))
	}

	// global_desktop stored as nothing (areGlobalDesktopNotificationsDisabled)
	prefs, _ := json.Marshal(map[string]any{"global": map[string]any{"global_desktop": "nothing"}})
	fs.SetPrefs(map[string]any{"all_notifications_prefs": string(prefs)})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := c.RefreshPrefs(ctx); err != nil || c.Prefs().GlobalDesktop != "nothing" {
		t.Fatalf("setup: global_desktop %q (%v)", c.Prefs().GlobalDesktop, err)
	}
	quiet, _ := fs.PostQuiet(ch, sender, "a synthetic errand nobody hears about")
	due(quiet)
	want := "notify: skip 14 saved_due message " + logtag.ID(ch) + "/" + logtag.ID(quiet)
	if !e.untilSoft(15*time.Second, func() bool { return contains(logs.matching("notify: "), want) }) {
		t.Errorf("no %q in clack.log (its notify lines: %q)", want, logs.matching("notify: "))
	}
	if n := noteWith(e, rec, "nobody hears about", 300*time.Millisecond); n != nil {
		t.Error("a reminder showed with global_desktop nothing")
	}
}
