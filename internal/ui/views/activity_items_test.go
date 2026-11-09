package views

// Every Activity item type through the real view, from the synthetic feed
// in internal/core/testdata (made-up ids and text, a fake Web API, no
// network). ACTIVITY_SHOTS_DIR set (the activity-shots workflow) also
// saves screenshots of the feed.

import (
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func actFeedApp(t *testing.T) *core.App {
	t.Helper()
	feed, err := os.ReadFile("../../core/testdata/activity_feed.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, "/api/") {
		case "activity.feed":
			_, _ = w.Write(feed)
		case "messages.list":
			_, _ = w.Write([]byte(`{"ok":true,"messages":{}}`)) // k-channel-gone stays unhydrated
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: "USELF", Team: "T1", EnterpriseID: "E1",
		TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = "T1", "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: "USELF", Name: "self", RealName: "Test Self"},
		{ID: "U1", Name: "alice", RealName: "Alice Example"},
		{ID: "U2", Name: "bob", RealName: "Bob Example"},
		{ID: "UB", Name: "synthbot", IsBot: true},
	})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1", IsMember: true})
	m := func(ts, user, text string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: user, Text: text}
	}
	root := m("1700000110.000100", "USELF", "a synthetic thread")
	root.ThreadTS, root.ReplyCount, root.ReplyUsers = root.TS, 1, []string{"U2"}
	reply := m("1700000111.000100", "U2", "a synthetic reply")
	reply.ThreadTS = root.TS
	joined := m("1700000120.000100", "U2", "<@U2> has joined the channel")
	joined.Subtype = "channel_join"
	s.SetLatest("C1", []slack.Message{
		m("1700000101.000100", "U2", "just a synthetic post in general"),
		m("1700000102.000100", "U1", "another synthetic post"),
		m("1700000105.000100", "U1", "<@USELF> a synthetic mention"),
		m("1700000106.000100", "U2", "<!channel> synthetic heads-up"),
		m("1700000107.000100", "U2", "<!everyone> synthetic news"),
		m("1700000108.000100", "U1", "<!subteam^S1> synthetic group ping"),
		m("1700000109.000100", "U2", "a synthetic keyword"),
		root, reply,
		m("1700000117.000100", "U1", "<@USELF> please review this synthetic change"),
		joined,
		m("1700000131.000100", "U2", "something new and synthetic"),
	}, false)
	s.SetThread("C1", root.TS, []slack.Message{root, reply})
	s.SetLatest("C2", []slack.Message{
		m("1700000114.000100", "U1", "<@USELF> join us in random"),
		m("1700000116.000100", "U2", "<@USELF> over here in random"),
	}, false)
	bot := m("1700000113.000100", "UB", "a synthetic app message")
	bot.BotID = "B1"
	s.SetLatest("D1", []slack.Message{m("1700000104.000100", "U1", "synthetic hi"), bot}, false)
	app := core.New(c, s)
	if u, ok := s.User("USELF"); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app
}

func actFrame(ops *op.Ops, sz image.Point) layout.Context {
	ops.Reset()
	return layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Now: time.Now()}
}

// TestActivityEveryItemTypeRenders: no item type draws an empty row (a
// bare avatar square and a time). The "all new posts" channel item was
// one (Karim, 2026-11-08: a plain message in a channel set to every
// message).
func TestActivityEveryItemTypeRenders(t *testing.T) {
	app := actFeedApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	v := NewActivity(h)
	var ops op.Ops
	sz := image.Pt(1200, 800)
	deadline := time.Now().Add(10 * time.Second)
	for len(actPage(v).Items) < 31 {
		if time.Now().After(deadline) {
			t.Fatalf("feed never loaded (%d items)", len(actPage(v).Items))
		}
		v.Layout(actFrame(&ops, sz))
		time.Sleep(10 * time.Millisecond)
	}
	page := actPage(v)
	want := map[string]struct{ name, foot, body string }{
		"k-channel":        {"Bob Example", "Post in #general", ""},
		"k-channel-many":   {"Alice Example", "Post in #general", ""},
		"k-channel-gone":   {"", "Post in #general", "New post in #general"},
		"k-dm":             {"Alice Example", "Direct message", ""},
		"k-at-channel":     {"Bob Example", "Mention in #general", ""},
		"k-quiet":          {"Alice Example", "Invitation to #random", "Alice Example added you to #random"},
		"k-invite":         {"Alice Example", "Invitation to #random", "Alice Example invited you to join #random"},
		"k-unjoined":       {"Bob Example", "Mention in #random", ""},
		"k-ext-channel":    {"eve", "Slack Connect Invitation", "eve from Example Co invited you to #partners: come chat."},
		"k-ext-dm":         {"Eve Example", "Slack Connect Invitation", "Eve Example from Example Co invited you to direct message."},
		"k-joiner":         {"Bob Example", "Workspace invitation", "Joined your workspace. A welcome from you will help them get started."},
		"k-prejoin":        {"Bob Example", "Workspace invitation", "Was invited to join Synthetic Club — take a second to say hello."},
		"k-reminder":       {"Alice Example", "Reminder from #general", ""},
		"k-reminder-plain": {"Reminder", "Reminder", "water the synthetic plants"},
		"k-calendar":       {"Synthetic standup", "Upcoming event", ""},
		"k-alert":          {"Slack", "Channel", "A synthetic channel was archived."},
		"k-future":         {"Bob Example", "In #general", ""},
		"k-future-bare":    {"Slack", "Activity", "New activity"},
	}
	for i := range page.Items {
		it := &page.Items[i]
		key := actKey(it)
		d := v.describe(it, page.Convs)
		if d.name == "" || d.foot == "" {
			t.Errorf("%s (%s): empty row: name %q foot %q", key, it.Type, d.name, d.foot)
		}
		if (it.Msg == nil || d.invite) && d.body == "" {
			t.Errorf("%s (%s): no message and no body", key, it.Type)
		}
		if strings.TrimSpace(d.prev) == "" && it.Msg == nil {
			t.Errorf("%s (%s): empty dense preview", key, it.Type)
		}
		if w, ok := want[key]; ok {
			if w.name != "" && d.name != w.name {
				t.Errorf("%s: name %q, want %q", key, d.name, w.name)
			}
			if d.foot != w.foot {
				t.Errorf("%s: foot %q, want %q", key, d.foot, w.foot)
			}
			if w.body != "" && d.body != w.body {
				t.Errorf("%s: body %q, want %q", key, d.body, w.body)
			}
		}
		if (key == "k-channel" || key == "k-channel-many") && it.Msg == nil {
			t.Errorf("%s: the channel post wasn't hydrated", key)
		}
		gtx := actFrame(&ops, image.Pt(800, 400))
		gtx.Constraints = layout.Constraints{Min: image.Pt(800, 0), Max: image.Pt(800, 400)}
		if hgt := v.row(gtx, it, 1).Size.Y; hgt <= 0 {
			t.Errorf("%s (%s): row has no height", key, it.Type)
		}
	}
	actShots(t, app)
}

// actShots saves the whole feed (Detailed and Dense, light and dark) when
// ACTIVITY_SHOTS_DIR is set.
func actShots(t *testing.T, app *core.App) {
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" {
		return
	}
	for _, sh := range []struct {
		name        string
		dark, dense bool
		h           int
	}{
		{"activity-detailed-light", false, false, 3400},
		{"activity-detailed-dark", true, false, 3400},
		{"activity-dense-light", false, true, 1500},
	} {
		sz := image.Pt(1100, sh.h)
		win, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		v := NewActivity(&slickHost{app: app, th: theme.New(sh.dark)})
		v.dense = sh.dense
		var ops op.Ops
		deadline := time.Now().Add(10 * time.Second)
		for len(actPage(v).Items) < 31 && time.Now().Before(deadline) {
			v.Layout(actFrame(&ops, sz))
			time.Sleep(10 * time.Millisecond)
		}
		for range 3 { // rows and images settle
			v.Layout(actFrame(&ops, sz))
		}
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("frame: %v", err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		win.Release()
		f, err := os.Create(filepath.Join(dir, sh.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		t.Logf("saved %s", f.Name())
	}
}
