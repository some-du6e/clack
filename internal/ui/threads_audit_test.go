package ui

// The thread pane audit (docs/THREADS-AUDIT.md) at the window's level,
// on internal/fakeslack (synthetic; nothing talks to Slack): Slack's
// notification rules for replies (the message path, the server's
// desktop_notification for followed threads, the titles), the pane's
// width (40% by default, 300 at least, the primary keeping 300), and
// Escape in the pane (mark the thread read, then close it). With
// THREADS_AUDIT_SHOTS_DIR set (the threads-audit workflow) the pane's
// states are saved, light and dark.

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// titleNotifier records the notifications' titles and bodies.
type titleNotifier struct {
	mu     sync.Mutex
	titles []string
	bodies []string
}

func (n *titleNotifier) Notify(t, b string) error {
	n.mu.Lock()
	n.titles, n.bodies = append(n.titles, t), append(n.bodies, b)
	n.mu.Unlock()
	return nil
}
func (n *titleNotifier) NotifyActions(t, b string, _ []string, _ func(string)) error {
	return n.Notify(t, b)
}
func (n *titleNotifier) got() ([]string, []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.titles...), append([]string(nil), n.bodies...)
}

func TestThreadsAuditReplyNotifications(t *testing.T) {
	const self = "U0SELF0001"
	ca := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: self}), store.New(""))
	ca.S.PutUsers([]slack.User{{ID: self, Name: "me"}})
	ca.S.Self = slack.User{ID: self, Name: "me"}
	ca.S.SetMyUsergroup("SMINE00001", true)
	reply := func(kind, text string) core.Event {
		return core.Event{Type: "message", Reply: true, ConvKind: kind, Msg: &core.Message{TS: "1700000060.000100", ThreadTS: "1700000000.000100", Text: text}}
	}
	richReply := func(kind, els string) core.Event {
		ev := reply(kind, "")
		ev.Msg.Blocks = json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + els + `]}]}]`)
		return ev
	}
	for _, c := range []struct {
		why  string
		ev   core.Event
		want bool
	}{
		{"a top-level message", core.Event{Type: "message", ConvKind: "channel", Msg: &core.Message{TS: "1700000000.000100", Text: "hi"}}, true},
		{"a reply that doesn't mention the viewer", reply("channel", "hi"), false},
		{"a reply naming the viewer", reply("channel", "hi <@"+self+">"), true},
		{"a reply's @channel", reply("channel", "<!channel> hi"), false},
		{"a reply's @everyone", reply("channel", "<!everyone> hi"), false},
		{"a reply's @here", reply("private", "<!here> hi"), true},
		{"a DM's reply naming the viewer", reply("im", "hi <@"+self+">"), false},
		{"a group DM's reply naming the viewer", reply("mpim", "hi <@"+self+">"), true},
		// checkForMentionInMessage's user groups (gap-w20)
		{"a reply naming the viewer's group", reply("channel", "<!subteam^SMINE00001|@devs> ship it"), true},
		{"a reply naming another group", reply("channel", "<!subteam^SOTHER0001|@ops> ship it"), false},
		{"a rich text reply naming the viewer's group", richReply("channel", `{"type":"usergroup","usergroup_id":"SMINE00001"}`), true},
		{"a rich text reply naming another group", richReply("channel", `{"type":"usergroup","usergroup_id":"SOTHER0001"}`), false},
		{"a rich text reply's @here", richReply("channel", `{"type":"broadcast","range":"here"}`), true},
		{"a rich text reply's @channel", richReply("channel", `{"type":"broadcast","range":"channel"}`), false},
		{"a DM's reply naming the viewer's group", reply("im", "<!subteam^SMINE00001>"), false},
	} {
		if got := replyNotifies(ca, c.ev); got != c.want {
			t.Errorf("%s: notifies %v, Slack %v", c.why, got, c.want)
		}
	}
}

type noResolver struct{}

func (noResolver) UserName(string) string                      { return "" }
func (noResolver) ChannelName(string) string                   { return "" }
func (noResolver) UsergroupName(string) string                 { return "" }
func (noResolver) Emoji(string) (url, text string, found bool) { return "", "", false }
func (noResolver) SelfID() string                              { return "" }

func TestThreadsAuditNotificationTitles(t *testing.T) {
	b := &backend{}
	ev := core.Event{Type: "message", Msg: &core.Message{Name: "Robin Example", Doc: richtext.Doc{Blocks: richtext.FromMrkdwn(noResolver{}, "the synthetic plan")}}}
	for _, c := range []struct {
		conv        *convInfo
		title, body string
	}{
		{&convInfo{Kind: "channel", Name: "th-audit"}, "New message in #th-audit", "Robin Example: the synthetic plan"},
		{&convInfo{Kind: "private", Name: "th-secret-plans"}, "New message in th-secret-plans", "Robin Example: the synthetic plan"},
		{&convInfo{Kind: "mpim", Name: "alex, robin"}, "New message in alex, robin", "Robin Example: the synthetic plan"},
		{&convInfo{Kind: "im", Name: "Robin Example"}, "New message from Robin Example", "the synthetic plan"},
	} {
		if got := b.notifyTitle(ev, c.conv); got != c.title {
			t.Errorf("%s: title %q, Slack %q", c.conv.Kind, got, c.title)
		}
		if got := b.notifyBody(ev, c.conv); got != c.body {
			t.Errorf("%s: body %q, Slack %q", c.conv.Kind, got, c.body)
		}
	}
}

// shouldShowNotificationForDesktopNotification over the server's event.
func TestThreadsAuditDesktopNotification(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	n := &titleNotifier{}
	a.notify = n
	huddleui.PlayHook = func(string) {} // the message sound: no audio device
	t.Cleanup(func() { huddleui.PlayHook = nil })
	b := a.data
	const ch, root, rep = "C0THAUDIT01", "1762700000.000100", "1762700300.000100"
	b.m.Convs = map[string]*convInfo{ch: {ID: ch, Kind: "channel", Name: "th-audit"}}
	push := func(ts, thread string) {
		raw, _ := json.Marshal(map[string]any{"type": "desktop_notification", "channel": ch, "ts": ts, "msg": ts, "thread_ts": thread,
			"content": "Robin Example: a synthetic reply", "title": "Thread in #th-audit"})
		b.desktopNotification(a, core.Event{Type: "desktop_notification", Channel: ch, Raw: raw})
	}
	push(rep, root)
	waitFor(t, a, "the reply's notification", func() bool { ts, _ := n.got(); return len(ts) == 1 })
	if ts, bs := n.got(); ts[0] != "New message in #th-audit" || bs[0] != "Robin Example: a synthetic reply" {
		t.Errorf("notified %q / %q, Slack \"New message in #th-audit\" with the event's content", ts[0], bs[0])
	}
	push(root, root) // not a reply (7)
	// the thread open in the pane with the window focused (16)
	a.focused = true
	a.secondary = secondaryState{kind: secThread, channel: ch, ts: root}
	push(rep, root)
	a.focused, a.secondary = false, secondaryState{}
	// read already (20)
	ca.S.SetThread(ch, root, []slack.Message{{TS: root, ThreadTS: root, LastRead: rep}})
	push(rep, root)
	time.Sleep(200 * time.Millisecond)
	a.drain()
	if ts, _ := n.got(); len(ts) != 1 {
		t.Errorf("notified %d times, want once (a root, the thread on screen and a read reply stay quiet): %q", len(ts), ts)
	}
}

// The pane's width (client-boot-render aTtQ): 40% of the panel until the
// user drags it, at least 300, the primary keeping 300.
func TestThreadsAuditPaneWidth(t *testing.T) {
	a := &App{}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	for _, c := range []struct {
		width, panel, want int
	}{
		{0, 1038, 415}, // 1440 window: 1366 - the 328 sidebar
		{0, 1600, 640},
		{0, 600, 300}, // at least 300 …
		{350, 1038, 350},
		{900, 1038, 738}, // … the primary keeps 300
		{200, 1038, 300},
	} {
		a.secondary.width = c.width
		if got := a.secondaryWidth(gtx, c.panel); got != c.want {
			t.Errorf("width %d in a %dpx panel: %d, Slack %d", c.width, c.panel, got, c.want)
		}
	}
}

func threadsAuditEnv(t *testing.T, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadsAudit = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("THREADS_AUDIT_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	return e
}

// threadsAuditShot saves the window and the pane alone.
func (e *perfEnv) threadsAuditShot(name string) {
	dir := os.Getenv("THREADS_AUDIT_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(600 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			e.tb.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			e.tb.Fatal(err)
		}
	}
	save(name, img)
	if e.a.secondary.kind == secThread && e.a.secondary.drawnW > 0 {
		w := e.a.secondary.drawnW
		save(name+"-pane", img.SubImage(image.Rect(e.size.X-w, 0, e.size.X, e.size.Y)))
	}
}

// Escape with the reply composer focused: the thread has unread replies,
// so Escape marks it read (the pane stays); the next one closes the pane.
func TestThreadsAuditEscape(t *testing.T) {
	e := threadsAuditEnv(t, false)
	ch, root := fakeslack.ThreadsAuditChannel, fakeslack.ThreadsAuditAllUnread
	e.openChannel(ch)
	e.a.OpenThread(ch, root)
	e.until("the thread", 15*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && tv.ContentHeight() > 0
	})
	e.a.FocusThreadComposer(ch, root)
	e.settle(300 * time.Millisecond)
	e.key(key.NameEscape, 0)
	e.until("Escape marks the thread read", 10*time.Second, func() bool { return len(e.fs.ThreadsAuditMarks()) == 1 })
	if e.a.secondary.kind != secThread {
		t.Fatal("the first Escape closed the pane; Slack marks the thread read first")
	}
	e.settle(500 * time.Millisecond)
	e.key(key.NameEscape, 0)
	e.until("the second Escape closes the pane", 5*time.Second, func() bool { return e.a.secondary.kind == secNone })
	if n := len(e.fs.ThreadsAuditMarks()); n != 1 {
		t.Errorf("marked %d times, want once", n)
	}
}

// The pane's states for the side-by-sides (THREADS_AUDIT_SHOTS_DIR).
func TestThreadsAuditShots(t *testing.T) {
	if os.Getenv("THREADS_AUDIT_SHOTS_DIR") == "" {
		t.Skip("THREADS_AUDIT_SHOTS_DIR unset")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := threadsAuditEnv(t, dark)
			ch := fakeslack.ThreadsAuditChannel
			e.openChannel(ch)
			e.settle(time.Second)
			e.threadsAuditShot("threads-audit-channel-" + mode)
			open := func(root, name string) {
				e.a.OpenThread(ch, root)
				e.until(name, 15*time.Second, func() bool {
					tv := e.threadView(ch, root)
					return tv != nil && len(tv.Channel()) > 0 && tv.ContentHeight() >= 0
				})
				e.settle(time.Second)
				e.threadsAuditShot("threads-audit-" + name + "-" + mode)
			}
			open(fakeslack.ThreadsAuditUnread, "new-divider")
			open(fakeslack.ThreadsAuditAllUnread, "separator-new")
			open(fakeslack.ThreadsAuditOwnFirst, "own-first-reply")
			open(fakeslack.ThreadsAuditHidden, "reply-count")
			open(fakeslack.ThreadsAuditLong, "opens-at-root")
			// the close button's tooltip
			if w := e.a.secondary.drawnW; w > 0 {
				p := image.Pt(e.size.X-12-14, 127+24)
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
				e.settle(900 * time.Millisecond)
				e.threadsAuditShot("threads-audit-close-tip-" + mode)
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(10, 10)})
			}
			// marked read with the window focused: the ghost
			e.a.focused = true
			open(fakeslack.ThreadsAuditUnread, "marked-read")
			e.settle(3500 * time.Millisecond)
			e.threadsAuditShot("threads-audit-ghost-" + mode)
			e.a.focused = false
			if m := e.fs.ThreadsAuditMarks(); len(m) == 0 || m[0].ThreadTS != fakeslack.ThreadsAuditUnread {
				t.Errorf("with the window focused the open thread wasn't marked read: %v", m)
			}
		})
	}
}
