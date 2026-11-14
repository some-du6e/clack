package ui

import (
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// The macOS notification (notify_mac.go). testdata/usernotify_slack.json
// holds sample payloads and what Slack desktop's mac shell made of them:
// ld, h6 and class h0 cut out of main.bundle.cjs 4.53.276 darwin and run
// in node, with Electron's Notification stubbed to record its options and
// replay its events.

type macSlackData struct {
	Samples map[string]struct {
		State struct {
			Workspaces map[string]struct {
				Name string `json:"name"`
			} `json:"workspaces"`
		} `json:"state"`
		Payload struct {
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			Body     string `json:"body"`
			TeamID   string `json:"teamId"`
			Channel  string `json:"channel"`
			Msg      string `json:"msg"`
			ThreadTS string `json:"thread_ts"`
			ID       string `json:"id"`
			GroupID  string `json:"groupId"`
			HasReply bool   `json:"hasReply"`
			Silent   bool   `json:"silent"`
			Buttons  []struct {
				Content   string `json:"content"`
				LaunchURI string `json:"launchUri"`
			} `json:"actionButtons"`
		} `json:"payload"`
	} `json:"samples"`
	Slack map[string]struct {
		Options struct {
			Title     string `json:"title"`
			Subtitle  string `json:"subtitle"`
			Body      string `json:"body"`
			CloseText string `json:"closeButtonText"`
			Actions   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"actions"`
			HasReply bool   `json:"hasReply"`
			Sound    string `json:"sound"`
			Silent   bool   `json:"silent"`
			ID       string `json:"id"`
			GroupID  string `json:"groupId"`
		} `json:"options"`
		Events map[string]*struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"events"`
	} `json:"slack"`
}

func loadMacSlack(t *testing.T) macSlackData {
	t.Helper()
	b, err := os.ReadFile("testdata/usernotify_slack.json")
	if err != nil {
		t.Fatal(err)
	}
	var d macSlackData
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Samples) == 0 || len(d.Slack) != len(d.Samples) {
		t.Fatalf("%d samples, %d results", len(d.Samples), len(d.Slack))
	}
	return d
}

// TestMacOptionsMatchSlack: macOptions is what h0 hands Electron for each
// sample (the payload as Clack's desktopNote carries it).
func TestMacOptionsMatchSlack(t *testing.T) {
	d := loadMacSlack(t)
	for name, s := range d.Samples {
		p := s.Payload
		n := &desktopNote{ID: p.ID, GroupID: p.GroupID, Title: p.Title, Subtitle: p.Subtitle, Body: p.Body, HasReply: p.HasReply,
			Silent: p.Silent, Team: p.TeamID, Workspace: s.State.Workspaces[p.TeamID].Name, Channel: p.Channel, Msg: p.Msg, ThreadTS: p.ThreadTS}
		for _, b := range p.Buttons {
			n.Buttons = append(n.Buttons, noteButton{Content: b.Content, LaunchURI: b.LaunchURI})
		}
		got := macOptions(n)
		want := d.Slack[name].Options
		var texts []string
		for _, a := range want.Actions {
			if a.Type != "button" {
				t.Errorf("%s: an action of type %q", name, a.Type)
			}
			texts = append(texts, a.Text)
		}
		if texts == nil {
			texts = []string{}
		}
		w := desktop.UserNotification{ID: want.ID, Title: want.Title, Subtitle: want.Subtitle, Body: want.Body, GroupID: want.GroupID,
			Actions: texts, HasReply: want.HasReply, CloseText: want.CloseText, Silent: want.Silent, Sound: want.Sound}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got, w)
		}

		// h0's events: what each of Electron's becomes
		for ev, slack := range d.Slack[name].Events {
			kind, idx, _ := strings.Cut(ev, ":")
			e := desktop.UserNotificationEvent{ID: n.ID, Kind: kind, Index: -1}
			switch kind {
			case "reply":
				e.Reply = "on my way"
			case "action":
				e.Index = nsIndex(t, idx)
			}
			act, ok := macActivation(n, e)
			switch {
			case slack == nil:
				t.Errorf("%s %s: Slack emitted nothing", name, ev)
			case slack.Type == "close":
				if ok {
					t.Errorf("%s %s: routed as %+v; Slack's close does nothing", name, ev, act)
				}
			case !ok:
				t.Errorf("%s %s: not routed; Slack emits %s %s", name, ev, slack.Type, slack.Value)
			case slack.Type == "click" && act.Type != "notification",
				slack.Type == "reply" && (act.Type != "reply" || act.Reply != "on my way"),
				slack.Type == "action" && (act.Type != "action" || string(slack.Value) != jsonInt(act.Index)):
				t.Errorf("%s %s: routed as %+v; Slack emits %s %s", name, ev, act, slack.Type, slack.Value)
			}
		}
	}
}

func nsIndex(t *testing.T, s string) int {
	t.Helper()
	var i int
	if err := json.Unmarshal([]byte(s), &i); err != nil {
		t.Fatal(err)
	}
	return i
}

func jsonInt(i int) string { b, _ := json.Marshal(i); return string(b) }

// TestMacActivationRoutes: a click carries the note's conversation, a
// reply its text; a button with a slack: link carries the link (the deep
// link, Join), one without carries none (handleNotificationAction:
// Decline's Do, Dismiss nothing).
func TestMacActivationRoutes(t *testing.T) {
	n := &desktopNote{ID: "T|huddle|C1|R1", Team: "T", Channel: "C1", Msg: "huddle_C1_1", ThreadTS: "", Buttons: []noteButton{
		{Content: "Dismiss"}, {Content: "Join", LaunchURI: "slack://huddle?id=C1&team=T"}, {Content: "Decline"}}}
	act, ok := macActivation(n, desktop.UserNotificationEvent{ID: n.ID, Kind: "click", Index: -1})
	if !ok || act.Type != "notification" || act.Channel != "C1" || act.MessageID != "huddle_C1_1" || act.NotificationID != n.ID || act.Team != "T" {
		t.Errorf("click: %+v %t", act, ok)
	}
	for _, c := range []struct {
		ev   desktop.UserNotificationEvent
		idx  int
		link string
	}{
		{desktop.UserNotificationEvent{Kind: "close", Index: -1}, 0, ""},
		{desktop.UserNotificationEvent{Kind: "action", Index: 0}, 1, "slack://huddle?id=C1&team=T"},
		{desktop.UserNotificationEvent{Kind: "action", Index: 1}, 2, ""},
	} {
		act, ok := macActivation(n, c.ev)
		if !ok || act.Type != "action" || act.Index != c.idx || act.LaunchURI != c.link || act.ID != n.ID {
			t.Errorf("%+v: %+v %t, want button %d link %q", c.ev, act, ok, c.idx, c.link)
		}
	}
	if act, ok := macActivation(n, desktop.UserNotificationEvent{Kind: "action", Index: 2}); ok {
		t.Errorf("a button the note doesn't have: %+v", act)
	}
	msg := &desktopNote{ID: "T_1.2", Channel: "C1", Msg: "1.2", ThreadTS: "1.1"}
	if act, ok := macActivation(msg, desktop.UserNotificationEvent{Kind: "reply", Reply: "hi"}); !ok || act.Type != "reply" || act.Reply != "hi" || act.ThreadTS != "1.1" {
		t.Errorf("reply: %+v %t", act, ok)
	}
	if act, ok := macActivation(msg, desktop.UserNotificationEvent{Kind: "close"}); ok {
		t.Errorf("a message's close: %+v", act)
	}
}

// fakeCenter is Notification Center for macNotifier: it records what's
// shown and answers as told.
type fakeCenter struct {
	mu        sync.Mutex
	on        func(desktop.UserNotificationEvent)
	handlers  int
	requests  int
	statuses  int
	status    desktop.UserNotificationAuth
	noAnswer  bool // Show never answers
	showErr   error
	shown     []desktop.UserNotification
	requested chan struct{}
}

func newFakeCenter() *fakeCenter {
	return &fakeCenter{status: desktop.AuthAuthorized, requested: make(chan struct{}, 16)}
}

func (f *fakeCenter) SetHandler(on func(desktop.UserNotificationEvent)) {
	f.mu.Lock()
	f.on, f.handlers = on, f.handlers+1
	f.mu.Unlock()
}

// Request never answers, as while macOS's prompt waits.
func (f *fakeCenter) Request(func(bool, error)) {
	f.mu.Lock()
	f.requests++
	f.mu.Unlock()
	f.requested <- struct{}{}
}

func (f *fakeCenter) Status(done func(desktop.UserNotificationAuth, error)) {
	f.mu.Lock()
	f.statuses++
	st := f.status
	f.mu.Unlock()
	done(st, nil)
}

func (f *fakeCenter) Show(n desktop.UserNotification, done func(error)) {
	f.mu.Lock()
	f.shown = append(f.shown, n)
	quiet, err := f.noAnswer, f.showErr
	f.mu.Unlock()
	if !quiet {
		done(err)
	}
}

func (f *fakeCenter) counts() (handlers, requests, statuses, shown int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handlers, f.requests, f.statuses, len(f.shown)
}

// waitShown drains a until f has shown n notifications, and returns the
// last.
func (f *fakeCenter) waitShown(t *testing.T, a *App, n int) desktop.UserNotification {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if a != nil {
			a.drain()
		}
		f.mu.Lock()
		got := len(f.shown)
		var last desktop.UserNotification
		if got > 0 {
			last = f.shown[got-1]
		}
		f.mu.Unlock()
		if got >= n {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d notifications shown, want %d", got, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// respond is the user's response as the center's delegate hands it over
// (the handler posts to the UI goroutine, which drain runs).
func (f *fakeCenter) respond(a *App, ev desktop.UserNotificationEvent) {
	f.mu.Lock()
	on := f.on
	f.mu.Unlock()
	on(ev)
	a.drain()
}

// TestMacNotifierMessages: a DM's notification is Slack's (the title, the
// body, the reply box, silent), the center's delegate and the
// authorization request come once, before the first; a click opens the
// conversation; a reply in a thread's notification is sent there and
// leaves the window where it was.
func TestMacNotifierMessages(t *testing.T) {
	a, ca, fs, _ := noteApp(t)
	team := storeTeam(ca)
	fc := newFakeCenter()
	a.notify = newMacNotifier(fc)
	posted := make(chan url.Values, 4)
	fs.OnPost = func(f url.Values, _ map[string]any) { posted <- f }

	msg := noteMsg("1762520099.000300", "", "hello &amp; bye")
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0001", TS: msg.TS, ConvKind: "im", Msg: msg})
	u := fc.waitShown(t, a, 1)
	want := desktop.UserNotification{ID: team + "_" + msg.TS, Title: "New message from Ada", Body: "hello & bye", HasReply: true,
		Silent: true, Sound: "none", Actions: []string{}}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("the DM's notification:\n got %+v\nwant %+v", u, want)
	}
	if h, r, _, _ := fc.counts(); h != 1 || r != 1 {
		t.Errorf("before the first notification: %d delegates, %d authorization requests; want 1 and 1", h, r)
	}
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1})
	if r := a.router.Current(); r.Channel() != "D0NOTE0001" || r.Thread() != "" {
		t.Errorf("a click opened %q, want the DM", r)
	}

	ca.SetExperimentGroup("desktop_notification_actions", "on")
	reply := noteMsg("1762520100.000400", "1762520099.000300", "<!here> look")
	a.data.handle(a, core.Event{Type: "message", Channel: "C0NOTE0001", TS: reply.TS, ConvKind: "channel", Msg: reply, Reply: true, Mention: true})
	u = fc.waitShown(t, a, 2)
	if u.ID != buildNotificationID(team, "msg", "C0NOTE0001", reply.TS, reply.ThreadTS) || u.GroupID != buildNotificationID(team, "msg", "C0NOTE0001") || !u.HasReply {
		t.Errorf("the reply's notification: %+v", u)
	}
	if h, r, _, _ := fc.counts(); h != 1 || r != 1 {
		t.Errorf("after two notifications: %d delegates, %d authorization requests; want 1 and 1", h, r)
	}
	before := a.router.Current()
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "reply", Index: -1, Reply: "on my way"})
	select {
	case f := <-posted:
		if f.Get("channel") != "C0NOTE0001" || f.Get("thread_ts") != reply.ThreadTS || f.Get("text") != "on my way" {
			t.Errorf("posted %v", f)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the reply wasn't sent")
	}
	if r := a.router.Current(); r != before {
		t.Errorf("a reply moved the window to %q (was %q)", r, before)
	}
	// the notification was answered: another response finds nothing
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1})
	if r := a.router.Current(); r != before {
		t.Errorf("a second response to one notification opened %q", r)
	}
}

// TestMacNotifierHuddleInvite: an invite is titled with the workspace,
// subtitled with its conversation, its first button (Dismiss) is the close
// button and the rest are its buttons; Join joins, Decline declines,
// dismissing does neither.
func TestMacNotifierHuddleInvite(t *testing.T) {
	a, ca, _, _ := noteApp(t)
	team := storeTeam(ca)
	fc := newFakeCenter()
	a.notify = newMacNotifier(fc)
	ca.SetExperimentGroup("desktop_notification_actions", "on")
	joined, declined := 0, 0
	inv := func() huddleui.Notification {
		return huddleui.Notification{Title: "Fake Workspace", Body: "Ada is inviting you to a huddle in #general", PlainBody: "Ada is inviting you to a huddle",
			Channel: "C0NOTE0001", User: "U0FAKEUSER1", Call: "R0FAKECALL1", Subtitle: "#general", Buttons: []huddleui.NotifyAction{
				{Key: "Dismiss", Label: "Dismiss"},
				{Key: "Join", Label: "Join", URI: "slack://huddle?id=C0NOTE0001&team=" + team, Do: func() { joined++ }},
				{Key: "Decline", Label: "Decline", Do: func() { declined++ }}}}
	}
	a.notifyActions(inv())
	u := fc.waitShown(t, a, 1)
	want := desktop.UserNotification{ID: buildNotificationID(team, "huddle", "C0NOTE0001", "R0FAKECALL1"), Title: "Fake Workspace", Subtitle: "#general",
		Body: "Ada is inviting you to a huddle in #general", Actions: []string{"Join", "Decline"}, CloseText: "Dismiss", Silent: true, Sound: "none"}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("the invite:\n got %+v\nwant %+v", u, want)
	}
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "action", Index: 0})
	if joined != 1 || declined != 0 {
		t.Errorf("ACTION_0 (Join): joined %d, declined %d", joined, declined)
	}

	a.notifyActions(inv())
	u = fc.waitShown(t, a, 2)
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "action", Index: 1})
	if joined != 1 || declined != 1 {
		t.Errorf("ACTION_1 (Decline): joined %d, declined %d", joined, declined)
	}

	a.notifyActions(inv())
	u = fc.waitShown(t, a, 3)
	before := a.router.Current()
	fc.respond(a, desktop.UserNotificationEvent{ID: u.ID, Kind: "close", Index: -1})
	if joined != 1 || declined != 1 || a.router.Current() != before {
		t.Errorf("a dismissal: joined %d, declined %d, route %q (was %q)", joined, declined, a.router.Current(), before)
	}
}

// TestMacNotifierShowAndCheck: a notification the center never answers
// counts as shown after h0's timeout; the delivery check runs after it
// (at most every 5 minutes) and, while the permission is undetermined,
// asks again: at most 3 times, 10 minutes apart.
func TestMacNotifierShowAndCheck(t *testing.T) {
	fc := newFakeCenter()
	fc.noAnswer, fc.status = true, desktop.AuthNotDetermined
	m := newMacNotifier(fc)
	clock := time.Unix(1_800_000_000, 0)
	var cmu sync.Mutex
	m.now = func() time.Time { cmu.Lock(); defer cmu.Unlock(); return clock }
	advance := func(d time.Duration) { cmu.Lock(); clock = clock.Add(d); cmu.Unlock() }
	m.showWait = 20 * time.Millisecond
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	show := func() { _ = m.NotifyNote(&desktopNote{Title: "t", Body: "b", Team: "T"}) }

	show()
	<-fc.requested // the presenter's request
	waitFor("the delivery check after the timeout", func() bool { _, _, s, _ := fc.counts(); return s == 1 })
	<-fc.requested // undetermined: l9 asks again
	if _, r, _, _ := fc.counts(); r != 2 {
		t.Fatalf("%d requests, want the presenter's and the delivery check's", r)
	}
	m.mu.Lock()
	m.asking = false // the prompt answered
	m.mu.Unlock()

	show() // within 5 minutes: no check
	time.Sleep(60 * time.Millisecond)
	if _, _, s, _ := fc.counts(); s != 1 {
		t.Errorf("%d checks within 5 minutes, want 1", s)
	}
	advance(macCheckEvery)
	show() // a check; 5 minutes since the last request: too soon to ask
	waitFor("the second check", func() bool { _, _, s, _ := fc.counts(); return s == 2 })
	time.Sleep(30 * time.Millisecond)
	if _, r, _, _ := fc.counts(); r != 2 {
		t.Errorf("%d requests 5 minutes after the last, want no new one", r)
	}
	for i := 0; i < 3; i++ {
		advance(macAskEvery)
		show()
		waitFor("a check", func() bool { _, _, s, _ := fc.counts(); return s == 3+i })
		time.Sleep(30 * time.Millisecond)
		m.mu.Lock()
		m.asking = false
		m.mu.Unlock()
	}
	if _, r, _, _ := fc.counts(); r != 1+macAskMax {
		t.Errorf("%d requests, want the presenter's and %d more", r, macAskMax)
	}

	// once authorized, a change away from it is noticed once
	fc.mu.Lock()
	fc.status = desktop.AuthAuthorized
	fc.mu.Unlock()
	advance(macCheckEvery)
	show()
	waitFor("the authorized check", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.wasAuthorized })
	fc.mu.Lock()
	fc.status = desktop.AuthDenied
	fc.mu.Unlock()
	advance(macCheckEvery)
	show()
	waitFor("the denied check", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.dropped == desktop.AuthDenied })
}
