package messages

// Screenshots of the gap-w2-w3 work, light and dark, on compact_shots_test.go's
// synthetic #release channel (made-up people, fakeslack's pictures), drawn
// headlessly through Gio's GPU renderer. Nothing talks to Slack.
//
//   - TestMessagesAuditGapShots (MESSAGES_AUDIT_SHOTS_DIR, the
//     messages-audit-shots workflow): skin-tone pills (getReactionGroups),
//     the group's ReactionTip, and "Pinned by" with its MemberLink.
//   - TestThreadsAuditStatesShots (THREADS_AUDIT_SHOTS_DIR, the
//     threads-audit workflow): FlexpaneLoading, ThreadsError, the deleted
//     thread, and reply bars with a draft and a scheduled reply.

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestMessagesAuditGapShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, ts := shotsApp(t)
		b1, _ := app.S.Find(tShots, ts["b1"])
		b1.Reactions = []slack.Reaction{
			{Name: "+1", Count: 2, Users: []string{"UANA00001", "UCARA0001"}},
			{Name: "tada", Count: 1, Users: []string{"UBEN00001"}},
			{Name: "+1::skin-tone-4", Count: 2, Users: []string{"UBEN00001", fakeslack.SelfID}},
			{Name: "+1::skin-tone-6", Count: 1, Users: []string{"UCARA0001"}},
			{Name: "wave::skin-tone-2", Count: 1, Users: []string{"UANA00001"}},
		}
		app.S.Upsert(tShots, b1)
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}

		// the pills and the +1 group's tooltip
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		var pillAt image.Rectangle
		f := func() {
			shotFrame(v, th, w, h, ops)
			r := v.s.rows[ts["b1"]]
			if r == nil || len(r.reactRects) == 0 || len(r.groups) == 0 {
				return
			}
			for i, it := range v.s.items {
				if it.kind != itMsg || it.key != ts["b1"] || i >= len(v.s.pos) {
					continue
				}
				pill := r.reactRects[0].Add(image.Pt(0, v.s.pos[i]))
				gt := r.groupTip(host, 0, fakeslack.SelfID)
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
				reactionGroupTooltip(gtx, host, th, colorsFor(th), &gt, pill, image.Rect(0, 0, w, h))
				pillAt = pill
			}
		}
		loadList(v, f)
		until(t, 5*time.Second, "the pills", f, func() bool { return !pillAt.Empty() })
		for i := 0; i < 60; i++ { // the emoji images
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		if r := v.s.rows[ts["b1"]]; r == nil || len(r.reactRects) != 3 {
			n := 0
			if r != nil {
				n = len(r.reactRects)
			}
			t.Errorf("%s: b1 has %d pills, want 3 (+1 in three tones, tada, wave)", mode, n)
		}
		img := pixeltest.Render(t, w, h, ops)
		savePNG(t, filepath.Join(dir, "gap-skin-tones-"+mode+".png"), crop(img, max(pillAt.Min.Y-260, 0), min(pillAt.Max.Y+16, h)))

		// "Pinned by ana": the member link in the thread pane
		root, _ := app.S.Find(tShots, ts["root"])
		r1 := slack.Message{Type: "message", TS: ts["r1"], User: "UBEN00001", Text: "Looks good. Can we call out the new filters first?",
			ThreadTS: ts["root"], Blocks: shotRich(shotSection("Looks good. Can we call out the new filters first?"))}
		r1.PinnedTo, r1.PinnedInfo = []string{tShots}, &slack.PinnedInfo{Channel: tShots, PinnedBy: "UANA00001"}
		app.S.SetThread(tShots, ts["root"], []slack.Message{root, r1})
		const tw, th0 = 520, 700
		tv := NewThreadView(host, tShots, ts["root"])
		tops := new(op.Ops)
		ft := func() { shotFrame(tv, th, tw, th0, tops) }
		until(t, 5*time.Second, "the pinned reply", ft, func() bool { return itemHeight(tv.s, ts["r1"]) > 0 })
		for i := 0; i < 40; i++ {
			ft()
			time.Sleep(10 * time.Millisecond)
		}
		ft()
		timg := pixeltest.Render(t, tw, th0, tops)
		ty0, ty1 := rowsSpan(tv.s)
		savePNG(t, filepath.Join(dir, "gap-pinned-by-"+mode+".png"), crop(timg, ty0+tv.s.origin.Y, ty1+tv.s.origin.Y))
	}
}

func TestThreadsAuditStatesShots(t *testing.T) {
	dir := os.Getenv("THREADS_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("THREADS_AUDIT_SHOTS_DIR not set (the threads-audit workflow)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, ts := shotsApp(t)
		g := &repliesGate{hold: make(chan struct{})}
		fs.SetHook(g.hook)
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
		const tw, thH = 480, 640
		shoot := func(name string, tv *ThreadView, ready func() bool) {
			ops := new(op.Ops)
			f := func() { shotFrame(tv, th, tw, thH, ops) }
			until(t, 10*time.Second, name, f, ready)
			for i := 0; i < 40; i++ {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			f()
			savePNG(t, filepath.Join(dir, "thread-"+name+"-"+mode+".png"), pixeltest.Render(t, tw, thH, ops))
		}

		// LargeOfflineIndicator: offline with nothing loaded
		prevOnline := clientOnline
		clientOnline = func(Host) bool { return false }
		to := NewThreadView(host, tShots, ts["c1"])
		shoot("offline", to, func() bool { return to.s.offlineDrawn })
		clientOnline = prevOnline
		// FlexpaneLoading while conversations.replies is held
		tl := NewThreadView(host, tShots, ts["b1"])
		shoot("loading", tl, func() bool { return g.calls.Load() > 0 })
		// ThreadsError
		g.mu.Lock()
		g.failure = "internal_error"
		close(g.hold)
		g.hold = nil
		g.mu.Unlock()
		shoot("error", tl, func() bool { return tl.s.threadErrored(host) })
		// the deleted thread
		g.mu.Lock()
		g.failure = "thread_not_found"
		g.mu.Unlock()
		td := NewThreadView(host, tShots, ts["b2"])
		shoot("deleted", td, func() bool { return td.s.threadErrored(host) })
		g.mu.Lock()
		g.failure = ""
		g.mu.Unlock()

		// reply bars: the root's "3 replies, 1 scheduled, 1 draft", and a
		// message with only a draft ("✏ 1 draft")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		for _, o := range []core.DraftOpts{
			{Channel: tShots, ThreadTS: ts["root"], Text: "half a reply"},
			{Channel: tShots, ThreadTS: ts["root"], Text: "later", DateScheduled: time.Now().Add(48 * time.Hour).Unix()},
			{Channel: tShots, ThreadTS: ts["c1"], Text: "a first reply"},
		} {
			if _, err := app.SaveDraft(ctx, o); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		f := func() { shotFrame(v, th, w, h, ops) }
		loadList(v, f)
		until(t, 5*time.Second, "the reply bars", f, func() bool {
			a, b := v.s.rows[ts["root"]], v.s.rows[ts["c1"]]
			return a != nil && b != nil && a.replyLink == "3 replies, 1 scheduled, 1 draft" && b.replyLink == "1 draft"
		})
		for i := 0; i < 40; i++ {
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		img := pixeltest.Render(t, w, h, ops)
		for _, k := range []string{"root", "c1"} {
			for i, it := range v.s.items {
				if it.kind == itMsg && it.key == ts[k] && i < len(v.s.pos) {
					y0 := v.s.pos[i] + v.s.origin.Y
					savePNG(t, filepath.Join(dir, "reply-bar-draft-"+k+"-"+mode+".png"), crop(img, max(y0-8, 0), min(y0+v.s.heights[i]+8, h)))
				}
			}
		}
	}
}
