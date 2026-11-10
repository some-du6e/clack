package views

// The Activity rows' marks and buttons (activity_rows.go) against
// internal/fakeslack's feed: the read rows' marks, Mark Complete and
// Reschedule, the Slack Connect invite buttons and their modals, the
// footers' Slackbot and file labels, the item menu's new rows, the
// clear-all toast, the offline footer and the saved views' density.
// Synthetic content only; every write is fakeslack's recorded one.

import (
	"context"
	"errors"
	"image"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// vaFakeActivity is the Activity view on fakeslack's feed, with the
// conversations the rows' marks read cached: your reply after the
// channel's mentions, a DM message you reacted to, the views fixture's
// channel draft, your DM with Slackbot.
func vaFakeActivity(t *testing.T, dark bool) (*Activity, *vaHost, *vaEnv, *fakeslack.Server) {
	t.Helper()
	app, fs := vaFake(t)
	app.SetExperimentGroup("slackbot_unread_v2", "on")
	s := app.S
	s.PutConv(slack.Conversation{ID: "D0FAKE00000", IsIM: true, User: "U0FAKE00001", IsMember: true})
	s.PutConv(slack.Conversation{ID: fakeslack.ActivitySlackbotDM, IsIM: true, User: "USLACKBOT", IsMember: true})
	msg := func(ts, user string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: user, Text: "a synthetic message"}
	}
	s.SetLatest(fakeslack.ViewsChannel, []slack.Message{msg("1762700100.000100", "U0FAKE00001"), msg("1762700200.000100", "U0FAKE00002"),
		msg("1762700400.000100", "U0FAKE00002"), msg("1762700900.000100", fakeslack.SelfID)}, false)
	reacted := msg("1762700750.000100", "U0FAKE00001")
	reacted.Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{fakeslack.SelfID}}}
	s.SetLatest("D0FAKE00000", []slack.Message{msg("1762700700.000100", "U0FAKE00001"), reacted}, false)
	if _, err := app.Drafts(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 1400)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "the activity feed", func() bool { return len(actPage(v).Items) >= 15 })
	e.until(t, "the reminders' saved items", func() bool { return len(v.saved) >= 3 })
	e.until(t, "the reminder file's title", func() bool { return v.fileTitles[fakeslack.ActivityReminderFile] != "" })
	e.frame()
	e.frame()
	return v, h, e, fs
}

func actFakeWrite(fs *fakeslack.Server, method string, ok func(url.Values) bool) bool {
	for _, w := range fs.ActivityWrites() {
		if w.Method == method && (ok == nil || ok(w.Form)) {
			return true
		}
	}
	return false
}

// U5s4 ActivityRowContentStatusIndicatorWithResolvedReply on read rows:
// your reply (getYouRepliedMessageTs), your reaction, the channel's draft
// (before the reply), the completed to-do; unread rows keep the badge and
// rows without any have none.
func TestActivityReadMarksLikeSlack(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	cases := []struct{ key, icon, tip string }{
		{"fa-at-channel", "check-filled", "You replied"},
		{"fa-keyword", "check-filled", "You replied"},
		{"fa-dm-reacted", "check-filled", "You reacted: :eyes:"},
		{"fa-channel", "edit-filled", "Draft: “a synthetic draft about the plan”"},
		{"fa-reminder-done", "check-filled", "You completed this to-do"},
	}
	for _, c := range cases {
		r := v.rowFor(c.key)
		if !r.markOK || r.mark.icon != c.icon || !strings.HasPrefix(r.mark.tip, c.tip) {
			t.Errorf("%s: mark %v %+v, want %s %q", c.key, r.markOK, r.mark, c.icon, c.tip)
		}
		if !icons.HasSlack(c.icon) {
			t.Errorf("%s: no glyph for %s", c.key, c.icon)
		}
	}
	// the to-do that isn't done has no mark (E8: incomplete)
	if r := v.rowFor("fa-reminder-file"); r.markOK {
		t.Errorf("fa-reminder-file: mark %+v on an incomplete to-do", r.mark)
	}
	vaShot(t, e, "activity-rows-detailed", false)
	v.dense, v.rows = true, map[string]*actRow{}
	v.dirty = true
	e.frame()
	e.frame()
	vaShot(t, e, "activity-rows-dense", false)
}

// Reminders: Detailed shows Mark Complete under an incomplete reminder's
// body (xSv9, saved.update mark completed) and the row's mark turns into
// the completed check; Dense puts Mark Complete and Reschedule in the
// hover bar ahead of the rest; Reschedule is RemindMenu (ReminderTimeMenu)
// and writes saved.update date_due.
func TestActivityReminderActionsLikeSlack(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	it := actItem(t, v, "fa-reminder")
	if !v.reminderIncomplete(it) {
		t.Fatal("fa-reminder isn't an incomplete to-do")
	}
	name := func(bs []actBtn) string {
		var out []string
		for _, b := range bs {
			_, l := actBtnIcon(b)
			out = append(out, l)
		}
		return strings.Join(out, ",")
	}
	if got := name(v.hoverButtons(it, true)); got != "Reschedule,Mark as read,Clear,More actions" {
		t.Errorf("Detailed hover %s", got)
	}
	v.dense = true
	if got := name(v.hoverButtons(it, true)); got != "Mark Complete,Reschedule,Mark as read,Clear,More actions" {
		t.Errorf("Dense hover %s", got)
	}
	if got := name(v.hoverButtons(actItem(t, v, "fa-reminder-done"), false)); got != "Mark as unread,Clear,More actions" {
		t.Errorf("a completed reminder's hover %s", got)
	}
	v.dense = false
	for _, b := range []actBtn{actBtnComplete, actBtnReschedule} {
		if icon, _ := actBtnIcon(b); !icons.HasSlack(icon) {
			t.Errorf("hover icon %q has no glyph", icon)
		}
	}
	menu := actMenuLabels(v.rescheduleMenu(h, it))
	if want := "In 30 mins;In 1 hour;In 3 hours;Tomorrow at 9:00 AM;Monday at 9:00 AM;Custom…;|;Clear reminder"; menu != want {
		t.Errorf("Reschedule menu\n got %s\nwant %s", menu, want)
	}
	// an overdue to-do's menu snoozes (ReminderTimeMenu isOverdue)
	if got := v.rescheduleMenu(h, actItem(t, v, "fa-reminder-file")); len(got) == 0 || got[0].Header != "Snooze…" || got[1].Label != "30 mins" {
		t.Errorf("overdue Reschedule menu %s", actMenuLabels(got))
	}
	before := time.Now().Add(time.Hour).Unix()
	for _, mi := range v.rescheduleMenu(h, it) {
		if mi.Label == "In 1 hour" {
			mi.Do()
		}
	}
	e.until(t, "saved.update date_due", func() bool {
		return actFakeWrite(fs, "saved.update", func(f url.Values) bool {
			due, err := strconv.ParseInt(f.Get("date_due"), 10, 64)
			return f.Get("item_type") == "message" && err == nil && due >= before-5 && due <= before+5 && !f.Has("mark")
		})
	})
	// Mark Complete in the Detailed body
	r := v.rowFor("fa-reminder")
	r.complete.Click()
	e.frame()
	e.until(t, "saved.update mark completed", func() bool {
		return actFakeWrite(fs, "saved.update", func(f url.Values) bool {
			return f.Get("mark") == "completed" && f.Get("todo_state") == "completed" && f.Get("ts") == "1762700100.000100"
		})
	})
	if v.reminderIncomplete(it) {
		t.Error("still incomplete after Mark Complete")
	}
	e.frame()
	if r := v.rowFor("fa-reminder"); !r.markOK || r.mark.icon != "check-filled" {
		// unread rows show the badge first; the mark is what the read row gets
		if !v.isUnread(it, "fa-reminder") {
			t.Errorf("mark after Mark Complete %+v", r.mark)
		}
	}
}

// Slack Connect: the DM invite offers Ignore and Accept, the channel
// invite Ignore (its Accept is Slack's multi-step modal, left out). Ignore
// opens IgnoreForkedModal; "Ignore this invitation" then Ignore Invitation
// writes conversations.ignoreInvite (channel) / sharedInvites.ignoreInvite
// (DM); "Block invitations" writes *.ignoreUser. Accept on the DM checks
// sharedInvites.canAccept and, the organisation not verified, opens the
// trust modal, whose Accept Invitation writes sharedInvites.accept.
func TestActivityInviteButtonsLikeSlack(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	dm, ch := actItem(t, v, "fa-ext-dm-invite"), actItem(t, v, "fa-ext-invite")
	if got := v.inviteActions(dm); len(got) != 2 || got[0] != core.InviteActIgnore || got[1] != core.InviteActAcceptDM {
		t.Fatalf("dm invite buttons %v", got)
	}
	if got := v.inviteActions(ch); len(got) != 1 || got[0] != core.InviteActIgnore {
		t.Fatalf("channel invite buttons %v", got)
	}
	if d := v.rowFor("fa-ext-dm-invite").d; d.teamIcon == "" || d.team != "Partner Co" {
		t.Errorf("inviter's team: %q %q", d.team, d.teamIcon)
	}
	vaShot(t, e, "activity-invite-row", false)
	last := func() *popups.Modal {
		open := h.open()
		if len(open) == 0 {
			return nil
		}
		m, _ := open[len(open)-1].(*popups.Modal)
		return m
	}
	// Ignore → forked → Ignore this invitation → Ignore Invitation
	v.rowFor("fa-ext-invite").invC[0].Click()
	e.frame()
	if last() == nil || v.forked[0] == nil {
		t.Fatal("Ignore opened no forked modal")
	}
	vaShot(t, e, "activity-ignore-forked", false)
	v.forked[0]()
	m := last()
	if m == nil {
		t.Fatal("no Ignore this invitation modal")
	}
	vaShot(t, e, "activity-ignore-this", false)
	m.PressDefault()
	e.until(t, "conversations.ignoreInvite", func() bool {
		return actFakeWrite(fs, "conversations.ignoreInvite", func(f url.Values) bool { return f.Get("invite") == "I0FAKE1" })
	})
	// Block invitations on the DM invite
	v.inviteClicked(dm, core.InviteActIgnore)
	v.forked[1]()
	if m := last(); m != nil {
		m.PressDefault()
	}
	e.until(t, "sharedInvites.ignoreUser", func() bool {
		return actFakeWrite(fs, "sharedInvites.ignoreUser", func(f url.Values) bool { return f.Get("invite") == fakeslack.ActivityInviteDM })
	})
	// IgnoreAllSuccessModal follows the block
	e.until(t, "the block's success modal", func() bool { return len(h.open()) == 1 && last() != nil })
	last().Dismiss()
	e.frame()
	// Accept: canAccept, then the trust modal (Partner Co isn't verified)
	n := len(h.open())
	v.inviteClicked(dm, core.InviteActAcceptDM)
	e.until(t, "sharedInvites.canAccept", func() bool { return actFakeWrite(fs, "sharedInvites.canAccept", nil) })
	e.until(t, "the trust modal", func() bool { return len(h.open()) > n && last() != nil })
	vaShot(t, e, "activity-trust-modal", false)
	last().PressDefault()
	e.until(t, "sharedInvites.accept", func() bool {
		return actFakeWrite(fs, "sharedInvites.accept", func(f url.Values) bool {
			return f.Get("sig") == fakeslack.ActivityInviteSig && f.Get("invite") == fakeslack.ActivityInviteDM
		})
	})
}

// VeYL: an app destination that's your Slackbot DM reads "Slackbot" with
// slackbot_unread_v2; a file reminder names the file ("Reminder from" the
// file icon and its title).
func TestActivityFooterLabelsLikeSlack(t *testing.T) {
	v, _, _, _ := vaFakeActivity(t, false)
	if d := v.rowFor("fa-slackbot").d; d.ft.name != "Slackbot" || d.loc != "Slackbot" {
		t.Errorf("Slackbot DM footer %+v loc %q", d.ft, d.loc)
	}
	d := v.rowFor("fa-reminder-file").d
	if d.ft.pre != "Reminder from " || d.ft.name != "synthetic-notes.txt" || d.ft.nameIcon != "file" || d.loc != "synthetic-notes.txt" || d.locIcon != "file" {
		t.Errorf("file reminder footer %+v loc %q %q", d.ft, d.loc, d.locIcon)
	}
	if d.body != "You set a reminder about this file" {
		t.Errorf("file reminder body %q", d.body)
	}
}

// getActivityItemMenuTemplate's new rows on fakeslack's items: Edit
// notifications (channel posts, channel invites) with the channel's
// setting, Move channel / Move to new section, "Don’t notify for @here
// and @channel" (at_channel), Add to VIP (a DM), Copy link (the
// message's), Remind ▸ Custom….
func TestActivityItemMenuNewRows(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	popups.MoveChannelItems = func(string) ([]popups.MenuItem, bool) { return nil, false }
	popups.CreateSectionWith = func(string) {}
	t.Cleanup(func() { popups.MoveChannelItems, popups.CreateSectionWith = nil, nil })
	labels := func(key string) string {
		it := actItem(t, v, key)
		return actMenuLabels(v.itemMenu(h, *it, key, v.isUnread(it, key)))
	}
	if got := labels("fa-channel"); !strings.Contains(got, "Edit notifications[") || !strings.Contains(got, "Move to new section") || !strings.Contains(got, ";Copy link;") {
		t.Errorf("channel post menu %s", got)
	}
	if got := labels("fa-at-channel"); !strings.Contains(got, "Don’t notify for @here and @channel") || strings.Contains(got, "Edit notifications") {
		t.Errorf("at_channel menu %s", got)
	}
	if got := labels("fa-dm-reacted"); !strings.Contains(got, "Add to VIP") {
		t.Errorf("DM menu %s", got)
	}
	if got := labels("fa-at-user"); !strings.Contains(got, "Remind me about this[In 20 minutes,In 1 hour,In 3 hours,Tomorrow,Next week,Custom…]") || strings.Contains(got, "Copy[") {
		t.Errorf("at_user menu %s", got)
	}
	it := actItem(t, v, "fa-at-channel")
	for _, mi := range v.itemMenu(h, *it, "fa-at-channel", false) {
		if mi.Key == "notify_broadcast_mentions" {
			mi.Do()
		}
	}
	e.until(t, "suppress_at_channel", func() bool { return v.h.Core().NotifyPrefs(fakeslack.ViewsChannel).SuppressAtChannel })
	_ = fs
	// the menu, for the screenshots
	v.actMenuOpen(image.Pt(600, 200), *actItem(t, v, "fa-channel"), "fa-channel", false)
	e.frame()
	vaShot(t, e, "activity-item-menu-new-rows", false)
}

// activity_clear_all_completed raises "Notifications cleared. Undo"; Undo
// writes activity.clearAll {undo_key} and the toast says "We brought back
// your notifications".
func TestActivityClearAllToast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		v, _, e, fs := vaFakeActivity(t, dark)
		v.clearAllCompleted("uk-fake")
		e.frame()
		vaShot(t, e, "activity-clear-all-toast", dark)
		v.clearAll.undo.Click()
		e.frame()
		e.until(t, "activity.clearAll", func() bool {
			return actFakeWrite(fs, "activity.clearAll", func(f url.Values) bool { return f.Get("undo_key") == "uk-fake" })
		})
		if v.clearAll.text != "We brought back your notifications" {
			t.Errorf("after Undo: %q", v.clearAll.text)
		}
	}
}

// ActivityRowLoader offline: a page that failed for want of a network
// shows InlineOfflineIndicator instead of the retry.
func TestActivityOfflineFooter(t *testing.T) {
	if !actOffline(&url.Error{Op: "Post", URL: "https://x", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}) {
		t.Error("a dial error isn't offline")
	}
	if actOffline(errors.New("ratelimited")) {
		t.Error("an API error is offline")
	}
	if !icons.HasSlack("cloud-offline") {
		t.Error("no cloud-offline glyph")
	}
	for _, dark := range []bool{false, true} {
		v, _, e, _ := vaFakeActivity(t, dark)
		v.offline = true
		page := actPage(v)
		v.failedCursor = "next"
		v.data.mutate(func(p *core.ActivityPage) { p.Next = "next" })
		_ = page
		e.frame()
		e.frame()
		vaShot(t, e, "activity-offline-footer", dark)
	}
}

// With activity_global_settings off the toggle saves each saved view:
// the All view only changes the page, Mentions writes
// activity.views.update {view_id, sort, density}.
func TestActivitySavedViewDensity(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	v.h.Core().SetExperimentGroup("activity_global_settings", "off")
	v.densityReq = false
	v.loadDensity()
	e.until(t, "the saved views", func() bool { return v.views[2].ID != "" })
	v.setDense(true) // the All tab
	e.frame()
	if actFakeWrite(fs, "activity.views.update", nil) || actFakeWrite(fs, "activity.prefs.update", nil) {
		t.Error("the All view's layout was written")
	}
	v.tabs.Sel = 2
	v.setDense(false)
	v.setDense(true)
	e.until(t, "activity.views.update", func() bool {
		return actFakeWrite(fs, "activity.views.update", func(f url.Values) bool {
			return f.Get("view_id") == v.views[2].ID && f.Get("density") == "dense" && f.Get("sort") == "newest"
		})
	})
	e.frame()
	vaShot(t, e, "activity-filter-bar-toggle", false)
}
