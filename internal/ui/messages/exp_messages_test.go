package messages

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The Messages package's experiments (docs/EXPERIMENTS.md) and the
// MESSAGES-AUDIT rows they share code with, over fakeslack: a synthetic
// #exp channel (made-up people and messages) in a real core.App.

const (
	tExp     = "C0EXPMSGS"
	tExpRO   = "C0EXPREAD" // read-only (is_read_only): you can't reply
	tExpPrev = "C0EXPPREV" // a public channel the viewer isn't in
)

var expTS = map[string]string{
	"plain": "1700000001.000100", "root": "1700000002.000100", "r1": "1700000002.000200", "r2": "1700000002.000300",
	"join": "1700000003.000100", "file": "1700000004.000100", "canvas": "1700000005.000100", "list": "1700000006.000100",
	"bcast": "1700000007.000100", "ro": "1700000008.000100", "prev": "1700000009.000100",
}

// expApp is the synthetic workspace; teamPrefs (nil: none) is what
// fakeslack's team.prefs.get answers, loaded the way boot does.
func expApp(t *testing.T, teamPrefs map[string]any) (*core.App, *fakeslack.Server) {
	t.Helper()
	SetTaut(nil)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.TeamPrefs = teamPrefs
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Fake Club"
	s.Unlock()
	self := slack.User{ID: fakeslack.SelfID, TeamID: fakeslack.TeamID, Name: "me", Profile: slack.Profile{DisplayName: "me"}}
	s.PutUsers([]slack.User{self,
		{ID: "UEXPALICE", TeamID: fakeslack.TeamID, Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice"}},
		{ID: "UEXPBOB01", TeamID: fakeslack.TeamID, Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob"}},
	})
	yes := true
	s.PutConv(slack.Conversation{ID: tExp, Name: "exp", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: tExpRO, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 3, IsReadOnly: &yes})
	s.PutConv(slack.Conversation{ID: tExpPrev, Name: "elsewhere", IsChannel: true, NumMembers: 30})
	ts := expTS
	file := func(key, id, name, filetype, mode string) slack.Message {
		return slack.Message{Type: "message", TS: ts[key], User: "UEXPALICE",
			Files: []slack.File{{ID: id, Name: name, Title: name, Mimetype: "application/octet-stream", Filetype: filetype, Mode: mode}}}
	}
	s.SetLatest(tExp, []slack.Message{
		{Type: "message", TS: ts["plain"], User: "UEXPALICE", Text: "a synthetic note"},
		{Type: "message", TS: ts["root"], User: "UEXPBOB01", Text: "a synthetic question", ThreadTS: ts["root"], ReplyCount: 2,
			ReplyUsers: []string{"UEXPALICE"}, LatestReply: ts["r2"]},
		{Type: "message", TS: ts["join"], User: "UEXPBOB01", Subtype: "channel_join", Text: "<@UEXPBOB01> has joined the channel"},
		file("file", "F0EXPPDF1", "notes.pdf", "pdf", "hosted"),
		file("canvas", "F0EXPCNV1", "Plan", "quip", "quip"),
		file("list", "F0EXPLST1", "Tasks", "list", "list"),
		{Type: "message", TS: ts["bcast"], User: "UEXPALICE", Subtype: "thread_broadcast", Text: "a synthetic broadcast reply", ThreadTS: ts["root"]},
	}, false)
	s.SetThread(tExp, ts["root"], []slack.Message{
		{Type: "message", TS: ts["root"], User: "UEXPBOB01", Text: "a synthetic question", ThreadTS: ts["root"], ReplyCount: 2},
		{Type: "message", TS: ts["r1"], User: "UEXPALICE", Text: "a synthetic answer", ThreadTS: ts["root"]},
		{Type: "message", TS: ts["r2"], User: "UEXPALICE", Text: "and a synthetic follow-up", ThreadTS: ts["root"]},
	})
	s.SetLatest(tExpRO, []slack.Message{
		{Type: "message", TS: ts["ro"], User: "UEXPBOB01", Text: "a synthetic announcement", ThreadTS: ts["ro"], ReplyCount: 1,
			ReplyUsers: []string{"UEXPALICE"}, LatestReply: "1700000008.000200"},
	}, false)
	s.SetLatest(tExpPrev, []slack.Message{{Type: "message", TS: ts["prev"], User: "UEXPBOB01", Text: "a synthetic hello"}}, false)
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	if teamPrefs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.LoadTeamPrefs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return app, fs
}

// expFrame lays v out at w×700, 1 px per dp.
func expFrame(v interface {
	Layout(gtx layout.Context) layout.Dimensions
}, w int) func() {
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, 700)), Now: time.Now()}
		v.Layout(gtx)
	}
}

// toolbarFor is the hover toolbar's buttons for the row at ts: their
// labels in order, "|"-joined, and the data-qa of each.
func toolbarFor(t *testing.T, s *listState, app *core.App, ts string) (labels string, qa map[int]string) {
	t.Helper()
	r := s.rows[ts]
	if r == nil {
		t.Fatalf("no row %s", ts)
	}
	s.prepareButtons(app, r)
	var ls []string
	qa = map[int]string{}
	for _, b := range s.tbBtns {
		ls = append(ls, s.tbLabel[b])
		qa[b] = s.tbQAs[b]
	}
	return strings.Join(ls, "|"), qa
}

// The thread and share buttons follow Slack's selectors (t8wB, 6bWJ): the
// reply label by replies and whether you can reply, no thread button in
// the thread pane, on system messages or where nothing can be replied,
// and "Share…" unless the team allows private_message_forwarding, with
// the file, canvas and list variants for a message that is one file.
func TestToolbarThreadAndShareLikeSlack(t *testing.T) {
	for _, forwarding := range []bool{false, true} {
		var prefs map[string]any
		if forwarding {
			prefs = map[string]any{"private_message_forwarding": true}
		}
		app, _ := expApp(t, prefs)
		h := coreHost{app: app, th: theme.New(false)}
		v := NewChannelView(h, tExp)
		v.Preview = true
		f := expFrame(v, 900)
		until(t, 5*time.Second, "the rows", f, func() bool { return len(v.s.rows) >= 7 })
		msg, file := "Share message…", "Share file…"
		if forwarding {
			msg, file = "Forward message…", "Forward file…"
		}
		canvas := "Share file…"
		if forwarding {
			canvas = "Share canvas…"
		}
		want := map[string]string{
			"plain":  "Add reaction…|Reply in thread|" + msg + "|Add to-do|Add to list…|More actions",
			"root":   "Add reaction…|Reply to thread|" + msg + "|Add to-do|Add to list…|More actions",
			"file":   "Add reaction…|Reply in thread|" + file + "|Add to-do|Add to list…|More actions",
			"canvas": "Add reaction…|Reply in thread|" + canvas + "|Add to-do|Add to list…|More actions",
			"list":   "Add reaction…|Reply in thread|Share list…|Add to-do|Add to list…|More actions",
			// a broadcast reply can't be replied to (it's a reply) but has replies
			"bcast": "Add reaction…|View thread|" + msg + "|Add to-do|Add to list…|More actions",
			// system_messages_redesign_m1 (on by default): no share button; and
			// an automated subtype can't start a thread
			"join": "Add reaction…|Add to-do|Add to list…|More actions",
		}
		for key, w := range want {
			got, qa := toolbarFor(t, v.s, app, expTS[key])
			if got != w {
				t.Errorf("forwarding %v, %s: toolbar %q, want %q", forwarding, key, got, w)
			}
			if q, ok := qa[tbForward]; ok {
				wq := "share_message"
				if key == "file" || key == "canvas" || key == "list" {
					wq = "share_file"
				}
				if q != wq {
					t.Errorf("%s: share data-qa %q, want %q", key, q, wq)
				}
			}
		}
		// the redesign off: system messages are shareable again (6bWJ)
		app.SetExperimentGroup("system_messages_redesign_m1", "off")
		if got, _ := toolbarFor(t, v.s, app, expTS["join"]); got != "Add reaction…|"+msg+"|Add to-do|Add to list…|More actions" {
			t.Errorf("redesign off, join: %q", got)
		}
		app.ClearExperimentGroup("system_messages_redesign_m1")

		// the thread pane has no thread button
		tv := NewThreadView(h, tExp, expTS["root"])
		tf := expFrame(tv, 500)
		until(t, 5*time.Second, "the thread", tf, func() bool { return tv.s.rows[expTS["r1"]] != nil && tv.s.rows[expTS["root"]] != nil })
		for _, key := range []string{"root", "r1"} {
			if got, _ := toolbarFor(t, tv.s, app, expTS[key]); got != "Add reaction…|"+msg+"|Add to-do|Add to list…|More actions" {
				t.Errorf("thread pane, %s: %q", key, got)
			}
		}
	}

	// read-only channel: a root with replies says View thread; a public
	// channel you only preview has no thread button on a reply-less message
	app, _ := expApp(t, nil)
	h := coreHost{app: app, th: theme.New(false)}
	for ch, w := range map[string]string{
		tExpRO:   "Add reaction…|View thread|Share message…|Add to-do|Add to list…|More actions",
		tExpPrev: "Add reaction…|Share message…|Add to-do|Add to list…|More actions",
	} {
		v := NewChannelView(h, ch)
		v.Preview = true
		f := expFrame(v, 900)
		until(t, 5*time.Second, "the row in "+ch, f, func() bool { return len(v.s.rows) >= 1 })
		for ts := range v.s.rows {
			if got, _ := toolbarFor(t, v.s, app, ts); got != w {
				t.Errorf("%s: %q, want %q", ch, got, w)
			}
		}
	}
}

// hoverExp puts the pointer on the row at ts and lays out a frame.
func hoverExp(s *listState, f func(), ts string, x float32) {
	f()
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if it := s.items[i]; it.kind == itMsg && it.key == ts && s.heights[i] > 0 {
			s.pointerIn = true
			s.pointer = f32.Pt(x, float32(s.pos[i]+s.heights[i]/2))
		}
	}
	f()
}

// message_actions_truncate: the one-click reactions leave a toolbar that
// would be wider than the message (L2dv calculateResponsiveActions); off,
// they stay whatever the width. The drawn card has the buttons the row
// gets: no thread button in the thread pane.
func TestMessageActionsTruncate(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Set(store.Prefs{MorePrefs: store.MorePrefs{QuickReactions: true}})
	app, _ := expApp(t, nil)
	h := coreHost{app: app, th: theme.New(false)}
	for _, tc := range []struct {
		group string
		w     int
		quick int
	}{{"on", 900, 3}, {"on", 300, 0}, {"off", 300, 3}} {
		app.SetExperimentGroup("message_actions_truncate", tc.group)
		v := NewChannelView(h, tExp)
		v.Preview = true
		f := expFrame(v, tc.w)
		until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[expTS["plain"]] != nil })
		ok := false
		for k := 0; k < 20 && !ok; k++ {
			hoverExp(v.s, f, expTS["plain"], float32(tc.w)/2)
			ok = v.s.tbFor >= 0 && v.s.tbRow != nil && v.s.tbRow.ts == expTS["plain"]
		}
		if !ok {
			t.Fatalf("%s at %dpx: no toolbar on the hovered row", tc.group, tc.w)
		}
		if n := len(v.s.quick.names); n != tc.quick {
			t.Errorf("message_actions_truncate %s at %dpx: %d quick reactions, want %d", tc.group, tc.w, n, tc.quick)
		}
		bs, pad := mt.toolbarBtn, mt.toolbarPad
		if want := int(pad*2 + float32(tc.quick+len(v.s.tbBtns))*bs); v.s.tbRect.Dx() != want {
			t.Errorf("%s at %dpx: toolbar %dpx wide, want %d (%d buttons)", tc.group, tc.w, v.s.tbRect.Dx(), want, len(v.s.tbBtns))
		}
	}
}
