package usernotifyprobe

import (
	"encoding/json"
	"flag"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
)

var (
	variant = flag.String("probe", "", "run TestUserNotifyProbe as this variant (ci.yml's macos job, from a test Clack.app)")
	steps   = flag.String("probe.steps", "prompt,post", "what to do, in order: prompt (ask for alert, sound and badge), provisional (the same plus provisional), post; none only reports the settings")
	answer  = flag.Duration("probe.wait", 20*time.Second, "how long to wait for each answer")
	show    = flag.Bool("probe.show", false, "run TestShowUserNotificationInBundle (ci.yml's macos job, from a test Clack.app)")
)

// TestUserNotifyProbe logs what UNUserNotificationCenter says to this
// process, one "PROBE <variant> <what>: <answer>" line each: the bundle,
// session and signature macOS sees, whether the center exists, the
// settings before and after each step, and each step's answer (a prompt
// nobody answers leaves its request without one). It reports rather than
// asserts: the answers are the finding. Screenshots show nothing here:
// without Screen Recording access screencapture gets the desktop alone, and
// asking for it from a bundle puts up macOS's prompt.
func TestUserNotifyProbe(t *testing.T) {
	if *variant == "" {
		t.Skip("run with -probe=<variant> from a test Clack.app (ci.yml's macos job)")
	}
	secs := int(answer.Seconds())
	report := func(what, v string) { t.Logf("PROBE %s %s: %s", *variant, what, v) }
	report("process", bundle()+"; build "+build)
	if why := centerErr(); why != "" {
		report("center", why)
		return
	}
	report("center", "ok")
	report("settings", settings(secs))
	if *steps == "none" {
		return
	}
	for _, step := range strings.Split(*steps, ",") {
		switch step {
		case "prompt", "provisional":
			opts := OptAlert | OptSound | OptBadge
			if step == "provisional" {
				opts |= OptProvisional
			}
			report(step, request(opts, secs))
		case "post":
			report(step, post("probe-"+*variant, "Clack probe ("+*variant+")", "#probe", "Can an ad-hoc signed Clack.app notify?", "probe-thread", true, secs))
			time.Sleep(time.Second)
			report("delivered", delivered(secs))
		default:
			t.Fatalf("unknown step %q", step)
		}
		report("settings after "+step, settings(secs))
	}
}

type deliveredNote struct {
	ID, Title, Subtitle, Body, Thread, Category string
	Sound                                       bool
}

type category struct {
	ID            string
	CustomDismiss bool
	Actions       []categoryAction
}

type categoryAction struct {
	ID, Title                string
	Foreground, TextInput    bool
	ButtonTitle, Placeholder string
}

// TestShowUserNotificationInBundle shows notifications through
// internal/desktop from inside a test Clack.app, reads back what
// Notification Center keeps (the content, the thread, the category's
// buttons in cocoa_notification.mm's shape, the sound), and hands the
// center's delegate responses as a reply, a click, a button and a
// dismissal would, checking the events desktop reports. Provisional
// authorization stands in for the user's Allow.
func TestShowUserNotificationInBundle(t *testing.T) {
	if !*show {
		t.Skip("run with -probe.show from a test Clack.app (ci.yml's macos job)")
	}
	if !desktop.UserNotificationsAvailable() {
		t.Fatal("UNUserNotificationCenter isn't available from this Clack.app: " + bundle())
	}
	events := make(chan desktop.UserNotificationEvent, 16)
	desktop.SetUserNotificationHandler(func(ev desktop.UserNotificationEvent) { events <- ev })
	if got := delegateClass(); got != "ClackUNDelegate" {
		t.Fatalf("the center's delegate is %s, want desktop's", got)
	}
	t.Logf("provisional authorization: %s", request(OptAlert|OptSound|OptBadge|OptProvisional, 10))
	if got := actionConstants(); got != "com.apple.UNNotificationDefaultActionIdentifier|com.apple.UNNotificationDismissActionIdentifier" {
		t.Errorf("the default and dismiss action identifiers are %s (desktop/usernotify.go's unDefaultAction, unDismissAction)", got)
	}

	msg := desktop.UserNotification{ID: "T0FAKE|msg|C0FAKE|1762520100.000400|1762520099.000300", Title: "New message in #general",
		Body: "Ada: look", GroupID: "T0FAKE|msg|C0FAKE", HasReply: true, Silent: true, Sound: "none"}
	inv := desktop.UserNotification{ID: "T0FAKE|huddle|C0FAKE|R0FAKE", Title: "Fake Workspace", Subtitle: "#general",
		Body: "Ada is inviting you to a huddle in #general", Actions: []string{"Join", "Decline"}, CloseText: "Dismiss", Silent: true, Sound: "none"}
	plain := desktop.UserNotification{Title: "Plain", Body: "no buttons, the default sound"}
	for _, n := range []desktop.UserNotification{msg, inv, plain} {
		done := make(chan error, 1)
		desktop.ShowUserNotification(n, func(err error) { done <- err })
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", n.Title, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no answer", n.Title)
		}
	}
	time.Sleep(time.Second)

	var notes []deliveredNote
	if err := json.Unmarshal([]byte(deliveredJSON(10)), &notes); err != nil {
		t.Fatal(err)
	}
	var cats []category
	if err := json.Unmarshal([]byte(categoriesJSON(10)), &cats); err != nil {
		t.Fatal(err)
	}
	t.Logf("delivered %+v", notes)
	t.Logf("categories %+v", cats)
	byID := map[string]deliveredNote{}
	var plainNote *deliveredNote
	for i, n := range notes {
		byID[n.ID] = n
		if n.Title == "Plain" {
			plainNote = &notes[i]
		}
	}
	catByID := map[string]category{}
	for _, c := range cats {
		catByID[c.ID] = c
	}
	check := func(n desktop.UserNotification, acts []categoryAction) {
		t.Helper()
		got, ok := byID[n.ID]
		if !ok {
			t.Errorf("%s wasn't delivered", n.ID)
			return
		}
		if got.Title != n.Title || got.Subtitle != n.Subtitle || got.Body != n.Body || got.Thread != n.GroupID || got.Sound {
			t.Errorf("%s: delivered %+v", n.ID, got)
		}
		c, ok := catByID[got.Category]
		if !strings.HasPrefix(got.Category, "CATEGORY_") || !ok {
			t.Errorf("%s: category %q, registered %t", n.ID, got.Category, ok)
			return
		}
		if !c.CustomDismiss || !reflect.DeepEqual(c.Actions, acts) {
			t.Errorf("%s: category %+v, want custom dismiss and %+v", n.ID, c, acts)
		}
	}
	check(msg, []categoryAction{{ID: "REPLY_ACTION", Title: "Reply", TextInput: true, ButtonTitle: "Reply"}})
	check(inv, []categoryAction{{ID: "ACTION_0", Title: "Join"}, {ID: "ACTION_1", Title: "Decline"}, {ID: "CLOSE_ACTION", Title: "Dismiss"}})
	if plainNote == nil {
		t.Error("the plain notification wasn't delivered")
	} else if len(plainNote.ID) != 36 || plainNote.Category != "" || !plainNote.Sound {
		t.Errorf("the plain notification: %+v, want a UUID, no category, the default sound", *plainNote)
	}

	// frontmost: list, banner and sound (Electron's willPresentNotification)
	if o := willPresent(msg.ID, 10); o != 1<<3|1<<4|1<<1 {
		t.Errorf("willPresent answered %d, want list|banner|sound", o)
	}

	for _, c := range []struct {
		id, action, text string
		input            bool
		want             desktop.UserNotificationEvent
	}{
		{msg.ID, "REPLY_ACTION", "on my way", true, desktop.UserNotificationEvent{ID: msg.ID, Kind: "reply", Index: -1, Reply: "on my way"}},
		{msg.ID, "com.apple.UNNotificationDefaultActionIdentifier", "", false, desktop.UserNotificationEvent{ID: msg.ID, Kind: "click", Index: -1}},
		{msg.ID, "com.apple.UNNotificationDismissActionIdentifier", "", false, desktop.UserNotificationEvent{ID: msg.ID, Kind: "close", Index: -1}},
		{inv.ID, "ACTION_1", "", false, desktop.UserNotificationEvent{ID: inv.ID, Kind: "action", Index: 1}},
		{inv.ID, "CLOSE_ACTION", "", false, desktop.UserNotificationEvent{ID: inv.ID, Kind: "close", Index: -1}},
	} {
		if why := respond(c.id, c.action, c.text, c.input, 10); why != "" {
			t.Errorf("%s on %s: %s", c.action, c.id, why)
			continue
		}
		select {
		case ev := <-events:
			if ev != c.want {
				t.Errorf("%s on %s: %+v, want %+v", c.action, c.id, ev, c.want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s on %s: no event", c.action, c.id)
		}
	}
}
