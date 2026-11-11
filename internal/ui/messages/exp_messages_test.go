package messages

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
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

// routed lays a view out over a real input.Router, so tests can press and
// click like a pointer.
type routed struct {
	v interface {
		Layout(gtx layout.Context) layout.Dimensions
	}
	w, h int
	r    input.Router
	ops  op.Ops
	now  time.Time
	evT  time.Duration
}

func newRouted(v interface {
	Layout(gtx layout.Context) layout.Dimensions
}, w, h int) *routed {
	return &routed{v: v, w: w, h: h, now: time.Now()}
}

func (rt *routed) frame() {
	rt.ops.Reset()
	rt.now = rt.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &rt.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(rt.w, rt.h)),
		Now: rt.now, Source: rt.r.Source()}
	rt.v.Layout(gtx)
	rt.r.Frame(&rt.ops)
}

// press is a pointer press (no release); click presses and releases.
func (rt *routed) press(p f32.Point, release bool) {
	rt.evT += 30 * time.Millisecond
	evs := []event.Event{
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: rt.evT},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: rt.evT + time.Millisecond},
	}
	if release {
		evs = append(evs, pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: rt.evT + 2*time.Millisecond})
	}
	rt.r.Queue(evs...)
	for k := 0; k < 3; k++ {
		rt.frame()
	}
}

func centre(r image.Rectangle) f32.Point {
	return f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
}

// UnreadBanner (bU1n): "N new messages" while the channel is unread,
// centred 14px under the pane; arrow-down with the "New" line in view, where
// pressing the text marks the channel read (conversations.mark) and hides
// the banner at once; arrow-up with the line above the view, where it
// jumps to the line instead; the ✕ marks read.
func TestUnreadBannerLikeSlack(t *testing.T) {
	unread := func(app *core.App) {
		app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: tExp, LastRead: expTS["file"], Latest: expTS["bcast"], HasUnreads: true}}})
	}
	// the line in view
	app, fs := expApp(t, nil)
	unread(app)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tExp)
	v.Preview = true // no automatic marking: only the banner marks
	rt := newRouted(v, 900, 700)
	until(t, 5*time.Second, "the banner", rt.frame, func() bool { return v.s.ub.text != "" })
	ub := &v.s.ub
	if ub.text != "3 new messages" || ub.icon != "arrow-down" {
		t.Errorf("banner %q %s, want \"3 new messages\" with arrow-down (the New line is in view)", ub.text, ub.icon)
	}
	all := ub.msgR.Union(ub.closeR)
	if d := (all.Min.X - 16) - (900 - 16 - all.Max.X); all.Min.Y != 14 || all.Dy() != 28 || d < -1 || d > 1 {
		t.Errorf("banner at %v, want 28px tall, 14 under the pane, centred between 16px insets", all)
	}
	if ub.closeR.Dx() != 4+20+8 {
		t.Errorf("✕ is %dpx wide, want 32 (padding 0 8 0 4 round the 20px icon)", ub.closeR.Dx())
	}
	rt.press(centre(ub.msgR), true)
	until(t, 3*time.Second, "conversations.mark", rt.frame, func() bool { return fs.CallCount("conversations.mark") > 0 })
	if ub.text != "" {
		t.Errorf("the banner still shows %q after marking read", ub.text)
	}
	until(t, 3*time.Second, "the counts", rt.frame, func() bool { return !app.Count(tExp).Unread })
	if ub.text != "" {
		t.Error("the banner came back after the counts caught up")
	}

	// the line above the view: the text jumps, the ✕ marks read
	app, fs = expApp(t, nil)
	unread(app)
	v = NewChannelView(coreHost{app: app, th: theme.New(false)}, tExp)
	v.Preview = true
	rt = newRouted(v, 900, 130)
	until(t, 5*time.Second, "the banner", rt.frame, func() bool { return v.s.ub.text != "" })
	ub = &v.s.ub
	ui := v.s.unreadIndex()
	if ub.icon != "arrow-up" || ui < 0 || ui >= v.s.visible[0] {
		t.Fatalf("banner icon %s, New line item %d, visible from %d: want arrow-up with the line above", ub.icon, ui, v.s.visible[0])
	}
	rt.press(centre(ub.msgR), true)
	for k := 0; k < 3; k++ {
		rt.frame()
	}
	if ui := v.s.unreadIndex(); ui < v.s.visible[0] || ui >= v.s.visible[1] {
		t.Errorf("after the jump the New line (item %d) isn't in view (%v)", ui, v.s.visible)
	}
	if n := fs.CallCount("conversations.mark"); n != 0 {
		t.Errorf("jumping marked the channel read (%d calls)", n)
	}
	rt.press(centre(ub.closeR), true)
	until(t, 3*time.Second, "the ✕'s conversations.mark", rt.frame, func() bool { return fs.CallCount("conversations.mark") > 0 })
}

// ArchivesBanner: "Latest messages" only while the loaded window misses
// the newest messages, never just for scrolling up (Slack has no such
// pill); centred 16px over the bottom, 28px tall, and it goes back to the
// newest.
func TestLatestMessagesBanner(t *testing.T) {
	app, _ := expApp(t, nil)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tExp)
	v.Preview = true
	rt := newRouted(v, 900, 130)
	until(t, 5*time.Second, "the messages", rt.frame, func() bool { return v.s.snap.loaded && len(v.s.snap.msgs) >= 7 })
	v.s.list.Position = layout.Position{BeforeEnd: true, First: 0} // scrolled to the top
	for k := 0; k < 3; k++ {
		rt.frame()
	}
	if v.s.latestR != (image.Rectangle{}) {
		t.Errorf("a pill at %v while the newest messages are loaded", v.s.latestR)
	}
	v.s.m.mu.Lock()
	v.s.m.newestMissing = true
	v.s.m.rev++
	v.s.m.mu.Unlock()
	until(t, 3*time.Second, "Latest messages", rt.frame, func() bool { return v.s.latestR != (image.Rectangle{}) })
	r := v.s.latestR
	if d := r.Min.X - (900 - r.Max.X); r.Dy() != 28 || r.Max.Y != 130-16 || d < -1 || d > 1 {
		t.Errorf("pill at %v, want 28px, 16 over the bottom, centred", r)
	}
	rt.press(centre(r), true)
	if p := v.s.list.Position; p.BeforeEnd {
		t.Errorf("clicking Latest messages left the list at %+v, want the newest", p)
	}
}

// dayDividerJumperMenuOptions, worked by hand.
func TestDayJumpOptions(t *testing.T) {
	loc := time.Local
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, loc) }
	now := at(2026, 11, 11, 15) // a Wednesday
	created := at(2025, 1, 2, 9)
	names := func(o []dayJumpOption) string {
		var s []string
		for _, x := range o {
			s = append(s, x.label)
		}
		return strings.Join(s, "|")
	}
	cases := []struct {
		name                 string
		divider, latest      time.Time
		yesterday            bool
		want                 string
		firstWhen, beginning time.Time
	}{
		{"latest today", at(2026, 11, 9, 0), at(2026, 11, 11, 10), true, "Today|Yesterday|Last week|Last month|The very beginning", at(2026, 11, 11, 0), created},
		{"latest yesterday, its divider", at(2026, 11, 10, 0), at(2026, 11, 10, 18), true, "Yesterday|Last week|Last month|The very beginning", at(2026, 11, 10, 0), created},
		{"old channel, creation day", at(2025, 1, 2, 0), at(2026, 9, 15, 12), false, "Most recent", time.Time{}, time.Time{}},
	}
	for _, c := range cases {
		o := dayJumpOptions(c.divider, c.latest, created, now, c.yesterday)
		if got := names(o); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
			continue
		}
		if !o[0].when.Equal(c.firstWhen) {
			t.Errorf("%s: first option jumps to %v, want %v", c.name, o[0].when, c.firstWhen)
		}
		if last := o[len(o)-1]; last.label == "The very beginning" && !last.when.Equal(c.beginning) {
			t.Errorf("%s: the very beginning is %v, want the channel's creation %v", c.name, last.when, c.beginning)
		}
	}
	// last week starts on the Sunday a week before, last month on the 1st
	o := dayJumpOptions(at(2026, 11, 9, 0), at(2026, 11, 11, 10), created, now, true)
	if !o[2].when.Equal(at(2026, 11, 1, 0)) || !o[3].when.Equal(at(2026, 10, 1, 0)) {
		t.Errorf("last week %v, last month %v; want Sun Nov 1 and Oct 1", o[2].when, o[3].when)
	}
}

// A day pill is DayDividerJumper's menu trigger: "Jump to…", the options,
// a separator and "Jump to a specific date"; an option jumps.
func TestDayPillMenu(t *testing.T) {
	app, _ := expApp(t, nil)
	h := &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	v := NewChannelView(h, tExp)
	v.Preview = true
	rt := newRouted(v, 900, 700)
	until(t, 5*time.Second, "the rows", rt.frame, func() bool { return len(v.s.rows) >= 7 })
	day := -1
	for i := v.s.visible[0]; i < v.s.visible[1]; i++ {
		if v.s.items[i].kind == itDay {
			day = i
			break
		}
	}
	if day < 0 {
		t.Fatal("no day divider on screen")
	}
	rt.press(f32.Pt(450, float32(v.s.pos[day])+mt.dayH/2), true)
	ms := h.menus()
	if len(ms) != 1 {
		t.Fatalf("%d menus after clicking the day pill, want 1", len(ms))
	}
	var got []string
	for _, it := range ms[0].Items() {
		switch {
		case it.Header != "":
			got = append(got, "["+it.Header+"]")
		case it.Separator:
			got = append(got, "-")
		default:
			got = append(got, it.Label)
		}
	}
	// the fixture's messages are from 2023 and the channel has no creation
	// time (Slack's unix(0)): "Most recent", "The very beginning"
	if s := strings.Join(got, "|"); s != "[Jump to…]|Most recent|The very beginning|-|Jump to a specific date" {
		t.Errorf("day pill menu %q", s)
	}
	// "Most recent" goes to the newest messages
	v.s.list.Position = layout.Position{BeforeEnd: true, First: 0}
	ms[0].Items()[1].Do()
	rt.frame()
	if v.s.list.Position.BeforeEnd {
		t.Errorf("Most recent left the list at %+v", v.s.list.Position)
	}
}
