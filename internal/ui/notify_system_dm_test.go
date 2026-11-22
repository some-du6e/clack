package ui

// The toast for a post in the DM with Slack's system user. Karim,
// 2026-11-21 (α.113, Windows): "got a message from system slack and didnt
// notice until i opened normal slack", "not slack bot but straight up
// slack". Apps post into that DM: subtype bot_message with no user, a
// username and an icons override, a bot_profile and blocks, in an
// org-shared IM with USLACK (fakeslack Size.SystemDM: the real account's
// shape, made-up content). The posts come over fakeslack's socket, and the
// toast follows Slack's rules (notify_dm.go):
//
//   - its title is the DM's own name, "New message from Slack", never the
//     post's username;
//   - its body is the blocks' text, the header and the section one line
//     apart, without the image's alt text or the button;
//   - its icon is Clack's own, never the post's icons override, USLACK's
//     picture or the bot_profile's icons;
//   - a click opens the DM at the post;
//   - an idle window's frame flashes, and the app badge doesn't count the
//     post (a bot DM's: DUy0 isBotDm);
//   - the DM's own desktop_push_enabled decides either way; a level of
//     "nothing", the global's or the DM's, never silences a DM; a mute
//     does; Do Not Disturb does, unless a VIP sent it or the sender asked
//     to notify anyway (dnd_override: "Slack is trying to reach you").

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// sysLogo stands in for Clack's icon in the toasts' pictures folder.
const sysLogo = "C:/Users/runner/AppData/Local/Temp/Clack Notification Cache/clack.png"

// sysWelcomeHeader, sysWelcomeSection and sysWelcomeAlt are the welcome
// post's (sysWelcomeBlocks) header text, section text and image alt text.
const (
	sysWelcomeHeader  = "Welcome to Fake Club!"
	sysWelcomeSection = "A synthetic welcome note: pick the channels to join."
	sysWelcomeAlt     = "A synthetic welcome banner"
	sysWelcomeButton  = "Get started"
)

// sysWelcomeBlocks are the blocks of the real DM's older post, in its
// shape (an image with alt text and no title, a header, a mrkdwn section,
// actions with a button); the image is the fake's.
func sysWelcomeBlocks(fs *fakeslack.Server) json.RawMessage {
	return sysWelcomeBlocksWith(fs, sysWelcomeSection)
}

// sysWelcomeBlocksWith is sysWelcomeBlocks with section as the section's
// text.
func sysWelcomeBlocksWith(fs *fakeslack.Server, section string) json.RawMessage {
	b, _ := json.Marshal([]any{
		map[string]any{"type": "image", "block_id": "sysw-img", "image_url": fs.URL + "img/600x200/sysw-banner.png",
			"alt_text": sysWelcomeAlt, "image_width": 600, "image_height": 200, "image_bytes": 12000,
			"is_animated": false, "fallback": "600x200px image"},
		map[string]any{"type": "header", "block_id": "sysw-head",
			"text": map[string]any{"type": "plain_text", "text": sysWelcomeHeader, "emoji": true}},
		map[string]any{"type": "section", "block_id": "sysw-body",
			"text": map[string]any{"type": "mrkdwn", "text": section, "verbatim": false}},
		map[string]any{"type": "actions", "block_id": "sysw-acts", "elements": []any{
			map[string]any{"type": "button", "action_id": "sysw-start", "value": "start", "style": "primary",
				"text": map[string]any{"type": "plain_text", "text": sysWelcomeButton, "emoji": true}},
		}},
	})
	return b
}

// sysDMEnv boots Small with the system DM (Size.SystemDM) and live
// updates on, so the posts come over the socket. The toasts are recorded,
// the window's taskbar is faked (fakeTaskbar), and the system reads as
// idle, so a toast flashes the frame (windowFlashBehavior "idle"). It
// waits for core to know the DM.
func sysDMEnv(t *testing.T) (*perfEnv, *fakeslack.Server, *noteRec, *taskbarRec) {
	t.Helper()
	sz := fakeslack.Small
	sz.SystemDM = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"mute_sounds": true}) // no audio device on the runners
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	tb := fakeTaskbar(t)
	tb.mu.Lock()
	tb.idle = 3 * systemIdleSeconds
	tb.mu.Unlock()
	rec := &noteRec{}
	e.a.notify = rec
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected && fs.Sockets() > 0 })
	e.until("core knowing the system DM", 15*time.Second, func() bool {
		c, ok := e.a.Core().Conv(fakeslack.SystemDMID)
		return ok && c.Kind == "im" && c.User == fakeslack.SlackUserID
	})
	return e, fs, rec, tb
}

// sysPost has the app post into the system DM, the socket bringing it:
// text in one section block, or the given blocks (the fake's history keeps
// the one section either way: the toast reads the event's message). It
// returns the post's ts.
func sysPost(fs *fakeslack.Server, text string, blocks json.RawMessage) string {
	ts, ev := fs.PostSystemDMQuiet(text)
	if blocks != nil {
		ev["blocks"] = blocks
	}
	fs.Push(ev)
	return ts
}

// sysNotes is how many toasts rec has.
func sysNotes(rec *noteRec) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.notes)
}

// sysToast waits for rec's n-th toast and checks it is post ts's.
func sysToast(t *testing.T, e *perfEnv, rec *noteRec, n int, ts, what string) *desktopNote {
	t.Helper()
	e.until(fmt.Sprintf("%s: toast %d", what, n), 15*time.Second, func() bool { return sysNotes(rec) >= n })
	rec.mu.Lock()
	note := rec.notes[n-1]
	rec.mu.Unlock()
	if note.Channel != fakeslack.SystemDMID || note.Msg != ts || note.ThreadTS != "" {
		t.Fatalf("%s: toast %d is for %s/%s (thread %q), want the system DM's post %s", what, n, note.Channel, note.Msg, note.ThreadTS, ts)
	}
	return note
}

// TestSystemDMToastLikeSlack: two posts come over the socket into the
// system DM, one with one section and one in the welcome post's shape.
// Each gets a toast titled with the DM's name, the blocks' text as the
// body and Clack's own icon. The frame flashes, the app badge stays as it
// was, and a click opens the DM at the post, highlighted.
func TestSystemDMToastLikeSlack(t *testing.T) {
	e, fs, rec, tb := sysDMEnv(t)
	a := e.a
	e.settle(300 * time.Millisecond)
	badge := a.desktopUnreads()
	tb.take()

	const notice = "A synthetic notice: the sign-in email changed."
	ts1 := sysPost(fs, notice, nil)
	n1 := sysToast(t, e, rec, 1, ts1, "a section post")
	// getNotificationTitle: getDisplayNameForChannel of the IM, its
	// partner's name; the post's username never titles it
	if n1.Title != "New message from Slack" {
		t.Errorf("titled %q, Slack \"New message from Slack\" (the DM's name, not the username %q)", n1.Title, fakeslack.SystemDMUsername)
	}
	if n1.Body != notice {
		t.Errorf("the body is %q, Slack %q (the section's text, no sender before it in a DM)", n1.Body, notice)
	}

	ts2 := sysPost(fs, sysWelcomeHeader+" A synthetic welcome note.", sysWelcomeBlocks(fs))
	n2 := sysToast(t, e, rec, 2, ts2, "the welcome post")
	if n2.Title != "New message from Slack" {
		t.Errorf("the welcome post's toast is titled %q, Slack \"New message from Slack\"", n2.Title)
	}
	// convertMessageToPlainText, blockSpacing single: the header and the
	// section one line apart; an image block is its title (none here),
	// never its alt text, and an actions block nothing
	if want := sysWelcomeHeader + "\n" + sysWelcomeSection; n2.Body != want {
		t.Errorf("the welcome post's body is %q, Slack %q", n2.Body, want)
	}
	for _, s := range []string{sysWelcomeAlt, sysWelcomeButton} {
		if strings.Contains(n1.Body+n2.Body, s) {
			t.Errorf("a body carries %q: %q / %q", s, n1.Body, n2.Body)
		}
	}

	// the icon: Chrome's toast shows the app's icon (Clack's), the mac
	// shell's notification the app's too; Slack hands no sender picture
	// for a post with no user (desktopNotificationOptions' avatarImage)
	x := n2.toastXML(sysLogo)
	if got := strings.Count(x, "<image "); got != 1 ||
		!strings.Contains(x, `<image placement="appLogoOverride" src="`+toastEscape(sysLogo)+`" hint-crop="none"/>`) {
		t.Errorf("the toast's pictures: %d, want only Clack's icon as its logo: %s", got, x)
	}
	for _, s := range []string{fakeslack.SystemDMIconURL, fakeslack.SystemDMBotIcon(36), fakeslack.SystemDMBotIcon(48),
		fakeslack.SystemDMBotIcon(72), fakeslack.SystemUserAvatarURL, fakeslack.SystemDMUsername, fakeslack.SystemDMBotName} {
		if strings.Contains(x, toastEscape(s)) {
			t.Errorf("the toast carries %q: %s", s, x)
		}
	}
	if u := macOptions(n2); u.Title != n2.Title || u.Body != n2.Body {
		t.Errorf("the macOS notification: %q / %q, want the toast's %q / %q", u.Title, u.Body, n2.Title, n2.Body)
	}

	// the frame flashes for each (flashMainWindowFrame, the system idle)
	e.until("the frame flashing for both toasts", 5*time.Second, func() bool {
		tb.mu.Lock()
		defer tb.mu.Unlock()
		n := 0
		for _, c := range tb.calls {
			if c == fmt.Sprintf("flash %#x true", testHWND) {
				n++
			}
		}
		return n >= 2
	})
	// the app badge counts no bot DM's message (getTotalUnreadHighlights
	// skips them; under the inbox it is the server's Activity badge)
	e.settle(300 * time.Millisecond)
	if got := a.desktopUnreads(); got.Count != badge.Count {
		t.Errorf("the app badge went from %d to %d with the system DM's posts", badge.Count, got.Count)
	}
	for _, c := range tb.take() {
		// "overlay 0x1234 N \"…\"" (fakeTaskbar)
		if f := strings.Fields(c); len(f) > 2 && f[0] == "overlay" && f[2] != strconv.Itoa(badge.Count) {
			t.Errorf("the taskbar overlay was set to %s (%s), want %d throughout", f[2], c, badge.Count)
		}
	}
	if got := sysNotes(rec); got != 2 {
		t.Errorf("%d toasts for two posts", got)
	}

	// a click opens the DM at the post, highlighted (notificationClicked:
	// jumpToMessage)
	click(t, a, n2, "toast", 0)
	if r := a.router.Current(); r != Route("c/"+fakeslack.SystemDMID+"/m/"+ts2) {
		t.Fatalf("a click on the toast opened %q, want the DM at the post (c/%s/m/%s)", r, fakeslack.SystemDMID, ts2)
	}
	e.until("the post placed and highlighted", 15*time.Second, func() bool {
		cv := e.channelView(fakeslack.SystemDMID)
		return cv != nil && cv.JumpPending() == "" && cv.Highlighted() == ts2
	})
}

// TestSystemDMToastPrefs: which prefs hold the system DM's posts back, as
// Slack's maybeShowNotification and shouldShowNotificationForDirectMessage
// decide. Every case is a new post; a case with a toast checks it is that
// post's, so a toast a hold only delayed would show.
func TestSystemDMToastPrefs(t *testing.T) {
	e, fs, rec, _ := sysDMEnv(t)
	a := e.a
	c := a.Core()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const dm = fakeslack.SystemDMID
	shown, posts := 0, 0
	// each post's text is plain words (the body is the section's text as
	// mrkdwn reads it)
	next := func(kind string) string {
		posts++
		return fmt.Sprintf("A synthetic %s number %d.", kind, posts)
	}
	// Each case reports what differs from Slack and goes on, counting the
	// toasts there are, so one run shows every case that differs.
	// arrived waits up to d for a toast after the shown ones and returns
	// it (nil: none came).
	arrived := func(d time.Duration) *desktopNote {
		dl := time.Now().Add(d)
		for {
			e.frame()
			rec.mu.Lock()
			var n *desktopNote
			if len(rec.notes) > shown {
				n = rec.notes[shown]
			}
			rec.mu.Unlock()
			if n != nil || time.Now().After(dl) {
				return n
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	resync := func() {
		rec.mu.Lock()
		shown = len(rec.notes)
		rec.mu.Unlock()
	}
	// check is a toast case for post ts: one toast, that post's, with
	// title and body ("=" for the post's text).
	check := func(what, ts, text, title, body string) {
		t.Helper()
		n := arrived(15 * time.Second)
		if n == nil {
			t.Errorf("%s: no toast, Slack shows one", what)
			return
		}
		shown++
		if n.Channel != fakeslack.SystemDMID || n.Msg != ts {
			t.Errorf("%s: the toast is for %s/%s, want the post %s", what, n.Channel, n.Msg, ts)
		}
		if n.Title != title {
			t.Errorf("%s: titled %q, Slack %q", what, n.Title, title)
		}
		if body == "=" {
			body = text
		}
		if n.Body != body {
			t.Errorf("%s: the body is %q, Slack %q", what, n.Body, body)
		}
		e.settle(100 * time.Millisecond)
		rec.mu.Lock()
		extra := len(rec.notes) - shown
		rec.mu.Unlock()
		if extra > 0 {
			t.Errorf("%s: %d toasts for one post", what, extra+1)
		}
		resync()
	}
	toast := func(what, title, body string) {
		t.Helper()
		text := next("notice")
		check(what, sysPost(fs, text, nil), text, title, body)
	}
	none := func(what string) string {
		t.Helper()
		ts := sysPost(fs, next("notice held back"), nil)
		if n := arrived(1500 * time.Millisecond); n != nil {
			t.Errorf("%s: a toast (%q / %q), Slack holds it back", what, n.Title, n.Body)
		}
		e.settle(100 * time.Millisecond)
		resync()
		return ts
	}
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	const title = "New message from Slack"

	toast("the default level (mentions_dms)", title, "=")

	// a DM has no level: getGlobalDmNotifySetting reads "nothing" as
	// mentions_dms, unset as everything, and the DM rule reads no override
	must("global_desktop nothing", c.SetGlobalNotifyPref(ctx, "global_desktop", "nothing"))
	toast(`global_desktop "nothing"`, title, "=")
	must("global_desktop back", c.SetGlobalNotifyPref(ctx, "global_desktop", "mentions_dms"))
	must("the DM's level nothing", c.SetNotifyPrefs(ctx, dm, core.ChannelNotify{Desktop: "nothing"}))
	toast(`the DM's own desktop "nothing"`, title, "=")
	must("the DM's level back", c.SetNotifyPrefs(ctx, dm, core.ChannelNotify{Desktop: "default"}))

	// desktop push (getCalculatedDesktopPushNotificationsEnabledSetting):
	// the global switch, then the DM's own override either way
	must("global push off", c.SetGlobalNotifyPref(ctx, "global_desktop_push_enabled", false))
	e.until("uipref seeing desktop push off", 10*time.Second, func() bool { return uipref.Get().NoDesktopPush })
	none("desktop notifications off")
	must("the DM's push on", c.SetChannelNotifyPref(ctx, dm, "desktop_push_enabled", "true"))
	toast("desktop notifications off, the DM's own on", title, "=")
	must("global push on", c.SetGlobalNotifyPref(ctx, "global_desktop_push_enabled", true))
	e.until("uipref seeing desktop push on", 10*time.Second, func() bool { return !uipref.Get().NoDesktopPush })
	must("the DM's push off", c.SetChannelNotifyPref(ctx, dm, "desktop_push_enabled", "false"))
	none("the DM's own desktop notifications off")
	must("the DM's push on again", c.SetChannelNotifyPref(ctx, dm, "desktop_push_enabled", "true"))
	toast("the DM's own desktop notifications on again", title, "=")

	// muted (12)
	must("mute", c.Mute(ctx, dm, true))
	e.until("the DM muted", 10*time.Second, func() bool {
		v, ok := c.Conv(dm)
		r := a.model().conv(dm)
		return ok && v.Muted && (r == nil || r.Muted)
	})
	none("the DM muted")
	must("unmute", c.Mute(ctx, dm, false))
	e.until("the DM unmuted", 10*time.Second, func() bool {
		v, ok := c.Conv(dm)
		r := a.model().conv(dm)
		return ok && !v.Muted && (r == nil || !r.Muted)
	})
	toast("the DM unmuted", title, "=")

	// Do Not Disturb (13), then the sender's notify anyway (dnd_override)
	snooze := time.Now().Add(time.Hour).Unix()
	fs.SetDND(map[string]any{"dnd_enabled": false, "snooze_enabled": true, "snooze_endtime": snooze})
	c.SetDNDStatus(core.DNDStatus{SnoozeEnabled: true, SnoozeEnd: snooze})
	heldText := fmt.Sprintf("A synthetic notice held back number %d.", posts+1)
	held := none("Do Not Disturb")
	fs.Push(map[string]any{"type": "dnd_override", "channel": dm, "timestamp": held})
	check("notify anyway (dnd_override)", held, heldText, "Slack is trying to reach you", "=")
	fs.Push(map[string]any{"type": "dnd_override", "channel": dm, "timestamp": held})
	if n := arrived(1500 * time.Millisecond); n != nil {
		t.Errorf("a second notify anyway for the same post: a toast (%q), Slack shows it once", n.Title)
	}
	resync()
	// a VIP's post comes through (shouldOverrideDndForMessage): a
	// bot_message whose bot_profile.user_id is a VIP, "Messages from VIPs,
	// even if notifications are paused" on
	fs.SetPrefs(map[string]any{"vip_users": fakeslack.SystemDMBotUserID, "priority_dnd_override": true})
	_, err := c.RefreshPrefs(ctx)
	must("the VIP prefs", err)
	toast("Do Not Disturb, the app's bot user a VIP", title, "=")
	fs.SetPrefs(map[string]any{"priority_dnd_override": false})
	_, err = c.RefreshPrefs(ctx)
	must("the VIP override off", err)
	none("Do Not Disturb, the VIP override off")
	fs.SetDND(nil)
	c.SetDNDStatus(core.DNDStatus{})
	toast("Do Not Disturb over", title, "=")

	// no message preview: a DM's body is empty (getContentForNotification)
	must("previews off", c.SetGlobalNotifyPref(ctx, "no_text_in_notifications", true))
	e.until("uipref seeing previews off", 10*time.Second, func() bool { return uipref.Get().NoTextInNotifications })
	toast("message previews off", title, "")
	must("previews on", c.SetGlobalNotifyPref(ctx, "no_text_in_notifications", false))
	e.until("uipref seeing previews on", 10*time.Second, func() bool { return !uipref.Get().NoTextInNotifications })

	// the DM open in the focused window (16)
	a.Navigate("c/" + dm)
	a.focused = true
	e.until("the DM open", 10*time.Second, func() bool { return a.router.Current().Channel() == dm })
	none("the DM on screen")
	a.focused = false
	toast("the DM open in a window without focus", title, "=")
}

// TestMacNotifierSystemDM: the system DM's welcome post, over the socket,
// through the macOS notifier (notify_mac_test.go's center; ci's macos job
// runs it with the other TestMacNotifier tests). It is Slack's: titled
// "New message from Slack", the blocks' text, the reply box, silent, no
// sound or buttons, the app's icon (the mac shell's getIcon returns
// nothing), and a click opens the DM at the post.
func TestMacNotifierSystemDM(t *testing.T) {
	e, fs, _, _ := sysDMEnv(t)
	a := e.a
	fc := newFakeCenter()
	a.notify = newMacNotifier(fc)
	ts := sysPost(fs, sysWelcomeHeader+" A synthetic welcome note.", sysWelcomeBlocks(fs))
	var u desktop.UserNotification
	e.until("the post's notification", 15*time.Second, func() bool {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		if len(fc.shown) == 0 {
			return false
		}
		u = fc.shown[0]
		return true
	})
	if id := a.messageNote(fakeslack.SystemDMID, ts, "").ID; u.ID != id {
		t.Errorf("the notification's id is %q, want the post's %q", u.ID, id)
	}
	if u.Title != "New message from Slack" {
		t.Errorf("titled %q, Slack \"New message from Slack\" (not the username %q)", u.Title, fakeslack.SystemDMUsername)
	}
	if want := sysWelcomeHeader + "\n" + sysWelcomeSection; u.Body != want {
		t.Errorf("the body is %q, Slack %q", u.Body, want)
	}
	if !u.HasReply || !u.Silent || u.Sound != "none" || len(u.Actions) != 0 || u.Subtitle != "" {
		t.Errorf("the notification's options: %+v", u)
	}
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1})
	if r := a.router.Current(); r != Route("c/"+fakeslack.SystemDMID+"/m/"+ts) {
		t.Fatalf("a click opened %q, want the DM at the post", r)
	}
	e.until("the post placed and highlighted", 15*time.Second, func() bool {
		cv := e.channelView(fakeslack.SystemDMID)
		return cv != nil && cv.JumpPending() == "" && cv.Highlighted() == ts
	})
}

// TestSystemDMToastPreviewsHidePluginText: a plugin's rewrite of the
// toast's text (a showNotification wrapper changing message.text, as
// Censorship does) shows with message previews on. With "Include a
// preview of the message" off nothing of the text shows, the plugin's
// included: Slack writes the body from the message the wrappers pass
// (sharedNotificationOptions, getContentForNotification) and
// shouldHideNotificationText empties a DM's.
func TestSystemDMToastPreviewsHidePluginText(t *testing.T) {
	e, fs, rec, _ := sysDMEnv(t)
	a := e.a
	c := a.Core()
	e.until("the plugins' runtime", 20*time.Second, func() bool { return a.Taut() != nil })
	rt, err := a.Taut().JS()
	if err != nil {
		t.Fatal(err)
	}
	const rewritten = "A synthetic rewritten text."
	if _, err := rt.Eval("sysdm-rewrite.js", `globalThis.__taut.redux.patchThunk('showNotification', (original) => (args, ...rest) =>
		original({...args, message: {...((args && args.message) || {}), text: '`+rewritten+`'}}, ...rest))`); err != nil {
		t.Fatal(err)
	}
	e.until("showNotification wrapped", 10*time.Second, func() bool { return a.Taut().ThunkPatched("showNotification") })

	ts := sysPost(fs, "A synthetic original text.", nil)
	if n := sysToast(t, e, rec, 1, ts, "previews on"); n.Body != rewritten {
		t.Errorf("previews on: the body is %q, want the plugin's %q", n.Body, rewritten)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := c.SetGlobalNotifyPref(ctx, "no_text_in_notifications", true); err != nil {
		t.Fatal(err)
	}
	e.until("uipref seeing previews off", 10*time.Second, func() bool { return uipref.Get().NoTextInNotifications })
	ts = sysPost(fs, "A synthetic second original text.", nil)
	n := sysToast(t, e, rec, 2, ts, "previews off")
	if n.Body != "" {
		t.Errorf("previews off: the body is %q, want nothing (the plugin's text hidden too)", n.Body)
	}
	if n.Title != "New message from Slack" {
		t.Errorf("previews off: titled %q, want \"New message from Slack\" (the title stays)", n.Title)
	}
	if x := n.toastXML(sysLogo); strings.Contains(x, toastEscape(rewritten)) {
		t.Errorf("previews off: the toast carries the plugin's text: %s", x)
	}
}
