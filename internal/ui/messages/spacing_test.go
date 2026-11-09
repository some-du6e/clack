package messages

// Message list geometry against Slack's CSS (bv1-13 web client,
// 2026-11-08): three consecutive messages from one sender, in a channel
// and in a thread pane, at 1x and 2x. Synthetic workspace only.
//
// Slack (CSS px): .p-message_pane_message__message .c-message_kit__gutter
// { padding: 8px 20px }; thread .c-message_kit__gutter { padding: 8px 24px
// 8px 16px }, the root's padding-top 20; .c-message_kit__gutter__right
// { margin: -12px -8px -16px -16px; padding: 8px 8px 8px 16px };
// .c-message_kit__message { font-size: 15px; line-height: 1.46668 } (22px);
// .c-message_kit__blocks--rich_text { margin-bottom: 4px }. Lato's hhea
// (1974/-426 of 2000) puts a 15px baseline 16.8px into a 22px line.

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const tSpacing = "C0SPACING"

// spacingTS are the root and its three replies, then three top-level
// messages, all from Alice within a minute: one header row, then
// continuation rows.
var spacingTS = struct{ root, r1, r2, r3, m1, m2, m3 string }{
	"1700000000.000100", "1700000005.000100", "1700000010.000100", "1700000015.000100",
	"1700000020.000100", "1700000025.000100", "1700000030.000100",
}

func spacingBlocks(s string) json.RawMessage {
	return json.RawMessage(`[{"type":"rich_text","block_id":"b` + s + `","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}]`)
}

func spacingHost(t *testing.T, dark bool) coreHost {
	t.Helper()
	SetTaut(nil)
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: tSpacing, Name: "clack", IsChannel: true, IsMember: true, NumMembers: 3})
	msg := func(ts, text, thread string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: "UALICE", Text: text, Blocks: spacingBlocks(text), ThreadTS: thread}
	}
	ts := spacingTS
	root := msg(ts.root, "i think it was in a thread", ts.root)
	root.ReplyCount, root.LatestReply, root.ReplyUsers = 3, ts.r3, []string{"UALICE"}
	app.S.SetLatest(tSpacing, []slack.Message{root, msg(ts.m1, "test1", ""), msg(ts.m2, "test2", ""), msg(ts.m3, "test3", "")}, false)
	app.S.SetThread(tSpacing, ts.root, []slack.Message{root, msg(ts.r1, "test1", ts.root), msg(ts.r2, "test2", ts.root), msg(ts.r3, "tesst3", ts.root)})
	return coreHost{app: app, th: theme.New(dark)}
}

// scaledFrame lays v out at scale (PxPerDp = PxPerSp).
func scaledFrame(v interface {
	Layout(gtx layout.Context) layout.Dimensions
}, scale float32, w, h int) func() {
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
		v.Layout(gtx)
	}
}

// rowGeom is one message row as laid out: its list item height, the body
// column origin, and baselines (row-local px).
type rowGeom struct {
	h, colX, colY  int
	bodyBL, nameBL int
}

func geomOf(t *testing.T, s *listState, ts string) rowGeom {
	t.Helper()
	r := s.rows[ts]
	if r == nil {
		t.Fatalf("no row %s", ts)
	}
	g := rowGeom{h: itemHeight(s, ts), colX: r.col.X, colY: r.col.Y, nameBL: -1}
	if bl := r.rv.Baselines(); len(bl) > 0 {
		g.bodyBL = r.bodyRect.Min.Y + bl[0]
	}
	for i := range s.items {
		if s.items[i].kind == itMsg && s.items[i].key == ts && s.items[i].header {
			g.nameBL = r.col.Y + r.nameBL
		}
	}
	return g
}

// px is a CSS length at scale, rounded the way the row rounds it.
func px(v, scale float32) int { return int(v*scale + 0.5) }

func near(got int, want, scale float32) bool {
	d := float32(got) - want*scale
	return d > -1 && d < 1
}

func checkRow(t *testing.T, what string, g rowGeom, h, colX, colY, bodyBL, nameBL, scale float32) {
	t.Helper()
	if g.h != px(h, scale) || g.colX != px(colX, scale) || g.colY != px(colY, scale) {
		t.Errorf("%s: height %d, column at (%d,%d); Slack %v, (%v,%v) at %vx", what, g.h, g.colX, g.colY, h*scale, colX*scale, colY*scale, scale)
	}
	if !near(g.bodyBL, bodyBL, scale) {
		t.Errorf("%s: body baseline %d, Slack %.1f", what, g.bodyBL, bodyBL*scale)
	}
	if nameBL > 0 && !near(g.nameBL, nameBL, scale) {
		t.Errorf("%s: sender baseline %d, Slack %.1f", what, g.nameBL, nameBL*scale)
	}
}

func TestMessageGeometryChannel(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		h := spacingHost(t, true)
		v := NewChannelView(h, tSpacing)
		v.Preview = true
		f := scaledFrame(v, scale, int(900*scale), int(700*scale))
		loadList(v, f)
		ts := spacingTS
		until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts.m3] != nil && itemHeight(v.s, ts.m3) > 0 })
		f()
		// The root is a header row (with its reply bar); m1..m3 continue it.
		for _, m := range []string{ts.m1, ts.m2, ts.m3} {
			// 4 + 22 + 4: the line at 4, baseline 4+16.8
			checkRow(t, "channel continuation "+m, geomOf(t, v.s, m), 30, 64, 4, 20.8, 0, scale)
		}
		root := geomOf(t, v.s, ts.root)
		checkRow(t, "channel header (sender line)", rowGeom{h: px(52, scale), colX: root.colX, colY: root.colY, bodyBL: root.bodyBL, nameBL: root.nameBL},
			52, 64, 4, 42.8, 20.8, scale)
		// rows stack with nothing between them
		for i := 1; i < len(v.s.items); i++ {
			if v.s.items[i].kind == itMsg && v.s.items[i-1].kind == itMsg && v.s.pos[i] != v.s.pos[i-1]+v.s.heights[i-1] {
				t.Errorf("gap above item %d: %d after %d+%d", i, v.s.pos[i], v.s.pos[i-1], v.s.heights[i-1])
			}
		}
		// the hover time on a continuation row sits on the body's baseline
		r := v.s.rows[ts.m2]
		rc := v.s.rc
		rc.header, rc.hovered = false, true
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(int(900*scale), int(200*scale))), Now: time.Now()}
		r.layout(gtx, &rc)
		if got, want := r.col.Y+r.compactBL, geomOf(t, v.s, ts.m2).bodyBL; got != want {
			t.Errorf("%vx: hover time baseline %d, body baseline %d (Slack: the same line)", scale, got, want)
		}
	}
}

func TestMessageGeometryThread(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		h := spacingHost(t, false)
		tv := NewThreadView(h, tSpacing, spacingTS.root)
		f := scaledFrame(tv, scale, int(420*scale), int(800*scale))
		ts := spacingTS
		until(t, 5*time.Second, "the replies", f, func() bool { return tv.s.rows[ts.r3] != nil && itemHeight(tv.s, ts.r3) > 0 })
		f()
		// root: padding-top 20, the column at 16, sender then body
		root := geomOf(t, tv.s, ts.root)
		checkRow(t, "thread root", root, 64, 60, 16, 16+22+16.8, 16+16.8, scale)
		// the "3 replies" divider: 24 tall
		for i, it := range tv.s.items {
			if it.kind == itReplies && tv.s.heights[i] != px(24, scale) {
				t.Errorf("%vx: replies divider %d tall, Slack 24", scale, tv.s.heights[i])
			}
		}
		checkRow(t, "first reply", geomOf(t, tv.s, ts.r1), 52, 60, 4, 42.8, 20.8, scale)
		for _, m := range []string{ts.r2, ts.r3} {
			checkRow(t, "continuation reply "+m, geomOf(t, tv.s, m), 30, 60, 4, 20.8, 0, scale)
		}
		// the reply column ends 16px from the pane's edge (gutter
		// padding-right 24 less gutter__right's -8 margin)
		if r := tv.s.rows[ts.r2]; r.bodyRect.Min.X != px(60, scale) {
			t.Errorf("%vx: reply body at x %d, Slack 60", scale, r.bodyRect.Min.X)
		}
	}
}

func TestThreadTime(t *testing.T) {
	now := time.Date(2026, 11, 8, 19, 30, 0, 0, time.Local)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "Just now"},
		{59 * time.Second, "Just now"},
		{60 * time.Second, "1 minute ago"},
		{89 * time.Second, "1 minute ago"},
		{90 * time.Second, "2 minutes ago"},
		{44 * time.Minute, "44 minutes ago"},
		{50 * time.Minute, "1 hour ago"},
		{2 * time.Hour, "Today at " + clock(now.Add(-2*time.Hour))},
		{24 * time.Hour, "Yesterday at " + clock(now.Add(-24*time.Hour))},
		{3 * 24 * time.Hour, "Thursday at " + clock(now.Add(-3*24*time.Hour))},
		{10 * 24 * time.Hour, "Oct 29th at " + clock(now.Add(-10*24*time.Hour))},
		{400 * 24 * time.Hour, "Oct 4th, 2025 at " + clock(now.Add(-400*24*time.Hour))},
	}
	for _, c := range cases {
		if got := threadTime(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%v ago: %q, want %q", c.ago, got, c.want)
		}
	}
}
