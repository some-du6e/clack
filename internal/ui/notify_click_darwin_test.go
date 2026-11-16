//go:build darwin && cgo

package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/app/cocoatest"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/desktop/usernotifyprobe"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// TestMacNotificationClickJumps is a click on a macOS notification for a
// message on a real desktop (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job;
// TestMain runs app.Main): App.Run's own window, signed in to
// internal/fakeslack's thread header fixtures (synthetic). Slack's mac
// shell answers CLICK_NOTIFICATION with FOCUS_MAIN_WINDOW (show, restore if
// minimized, focus) and the webapp's notificationClicked with
// jumpToMessage, so each click must bring the window forward and land on
// the message:
//
//  1. from another conversation, a channel message: its conversation at
//     the message, highlighted;
//  2. from another conversation, a reply: its thread at the reply,
//     highlighted, the root highlighted beside it;
//  3. the window closed to the Dock (hidden): shown, key, at the message;
//  4. the window minimized: restored, key, at the message.
//
// Run from a test Clack.app (ci builds one as for the notification probe)
// the notifications go through the real UNUserNotificationCenter
// (provisional authorization stands in for the user's Allow) and each
// click is the response macOS hands the center's delegate, decoded from an
// archive (usernotifyprobe.Click): the delegate desktop sets, its event,
// notify_mac.go's routing and the app's. Run as a plain test binary the
// center is notify_mac_test.go's fake and the click its handler's call.
func TestMacNotificationClickJumps(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	cases := fs.ThreadHeaderCases()
	pub, priv, shared := cases[0].Channel, cases[1].Channel, cases[2].Channel
	root, reply := fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)

	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	views := make(chan uintptr, 1)
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		if e, ok := e.(app.AppKitViewEvent); ok && e.View != 0 {
			select {
			case views <- e.View:
			default:
			}
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	var view uintptr
	select {
	case view = <-views:
	case <-time.After(60 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !ok() {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	state := func() cocoatest.Window {
		var s cocoatest.Window
		onMain(func() { s = cocoatest.State(view) })
		return s
	}
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(15 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	until("the window", func() bool { return state().Visible })
	time.Sleep(2 * time.Second) // boot settles (the reveal, the first history)

	// the center: the real one from a Clack.app, else the fake
	inBundle := desktop.UserNotificationsAvailable()
	if os.Getenv("CLACK_MACOS_NOTIFY_BUNDLE") != "" && !inBundle {
		t.Fatal("CLACK_MACOS_NOTIFY_BUNDLE: run from a test Clack.app, but UNUserNotificationCenter isn't available here")
	}
	var fc *fakeCenter
	var sys macNoteSys
	if inBundle {
		t.Logf("from a test Clack.app: the real UNUserNotificationCenter; provisional authorization: %s", usernotifyprobe.GrantProvisional())
		sys = provisionalCenter{}
	} else {
		t.Log("not in an app bundle: notify_mac_test.go's fake center")
		fc = newFakeCenter()
		sys = fc
	}
	m := newMacNotifier(sys)
	onUI(func() { a.notify = m })
	shown := 0
	// click shows a's notification for the message ts in ch (in thread)
	// and clicks it as macOS would
	click := func(what, ch, ts, thread string) {
		t.Helper()
		var n *desktopNote
		onUI(func() {
			n = a.messageNote(ch, ts, thread)
			n.Title, n.Body = "New message", "synthetic"
		})
		_ = m.NotifyNote(n)
		shown++
		if inBundle {
			time.Sleep(time.Second) // delivered (the probe reads the delivered list)
			if why := usernotifyprobe.Click(n.ID); why != "" {
				t.Fatalf("%s: the click didn't reach the center's delegate: %s", what, why)
			}
			return
		}
		u := fc.waitShown(t, nil, shown)
		fc.mu.Lock()
		on := fc.on
		fc.mu.Unlock()
		go on(desktop.UserNotificationEvent{ID: u.ID, Kind: "click", Index: -1}) // the center's queue
	}
	route := func() (r Route) {
		onUI(func() { r = a.router.Current() })
		return r
	}
	highlighted := func(ch, thread string) (h, pending string) {
		onUI(func() {
			c := a.convos[ch+"/"+thread]
			if c == nil {
				return
			}
			if l, ok := c.list.(interface {
				Highlighted() string
				JumpPending() string
			}); ok {
				h, pending = l.Highlighted(), l.JumpPending()
			}
		})
		return h, pending
	}
	forward := func(what string) {
		t.Helper()
		until(what+": the window visible, not minimized, key and the app active", func() bool {
			s := state()
			return s.Visible && !s.Miniaturized && s.Key && s.AppActive
		})
	}
	atMessage := func(what, ch, ts string) {
		t.Helper()
		until(what+": "+ch+" opened at the message", func() bool { return route() == Route("c/"+ch+"/m/"+ts) })
		until(what+": the message placed and highlighted", func() bool {
			h, p := highlighted(ch, "")
			return h == ts && p == ""
		})
	}
	elsewhere := func(ch string) {
		t.Helper()
		onUI(func() { a.Navigate("c/" + ch) })
		until("on "+ch, func() bool { return route() == Route("c/"+ch) })
		time.Sleep(500 * time.Millisecond)
	}

	// 1. a channel message, from another conversation
	elsewhere(priv)
	click("a channel message", pub, root, "")
	forward("a channel message")
	atMessage("a channel message", pub, root)

	// 2. a reply, from another conversation
	elsewhere(priv)
	click("a reply", pub, reply, root)
	forward("a reply")
	until("a reply: its thread", func() bool { return route() == Route("c/"+pub+"/t/"+root) })
	var sec secondaryState
	onUI(func() { sec = a.secondary })
	if sec.kind != secThread || sec.channel != pub || sec.ts != root || sec.at != reply {
		t.Errorf("a reply: the thread pane %v %s/%s at %q, want the thread at the reply", sec.kind, sec.channel, sec.ts, sec.at)
	}
	until("a reply: the reply placed and highlighted", func() bool {
		h, p := highlighted(pub, root)
		return h == reply && p == ""
	})
	if h, _ := highlighted(pub, ""); h != root {
		t.Errorf("a reply: the conversation beside the thread highlights %q, want the root", h)
	}

	// 3. the window closed to the Dock
	elsewhere(priv)
	onMain(func() { cocoatest.Press(view, cocoatest.Close) })
	until("the window hidden by the close button", func() bool { return !state().Visible })
	time.Sleep(500 * time.Millisecond)
	click("hidden", shared, root, "")
	forward("hidden")
	atMessage("hidden", shared, root)

	// 4. the window minimized
	elsewhere(priv)
	onMain(func() { cocoatest.Press(view, cocoatest.Minimize) })
	until("the window minimized", func() bool { return state().Miniaturized })
	time.Sleep(500 * time.Millisecond)
	click("minimized", pub, root, "")
	forward("minimized")
	atMessage("minimized", pub, root)

	onUI(func() { a.Quit() })
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Error("Quit didn't end Run")
	}
}

// provisionalCenter is the real Notification Center with the
// authorization the test granted (provisional) standing for the request
// NotifyNote makes first: a full request puts up macOS's prompt, which
// nobody answers on the runner.
type provisionalCenter struct{ desktopUserNotifications }

func (provisionalCenter) Request(done func(bool, error)) {
	if done != nil {
		done(true, nil)
	}
}
