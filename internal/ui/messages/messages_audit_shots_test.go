package messages

// TestMessagesAuditShots (MESSAGES_AUDIT_SHOTS_DIR, the
// messages-audit-shots workflow) draws the rows docs/MESSAGES-AUDIT.md
// changed, on compact_shots_test.go's synthetic #release channel (made-up
// people and messages, fakeslack's pictures), light and dark: the thread
// pane with a reply pinned by someone else, the reply also sent to the
// channel (MessageLabels' pinned and broadcast labels) and a reply with 24
// reactions (ReactionBar's collapse), and the reaction tooltip over the
// channel's :tada: pill. Nothing talks to Slack.

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

func TestMessagesAuditShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	t.Cleanup(uipref.Reset)
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, ts := shotsApp(t)
		root, ok := app.S.Find(tShots, ts["root"])
		bc, ok2 := app.S.Find(tShots, ts["bc"])
		if !ok || !ok2 {
			t.Fatal("no root or broadcast in the fixture")
		}
		reply := func(key, user, text string) slack.Message {
			return slack.Message{Type: "message", TS: ts[key], User: user, Text: text, ThreadTS: ts["root"], Blocks: shotRich(shotSection(text))}
		}
		r1 := reply("r1", "UBEN00001", "Looks good. Can we call out the new filters first?")
		r1.PinnedTo, r1.PinnedInfo = []string{tShots}, &slack.PinnedInfo{Channel: tShots, PinnedBy: "UANA00001"}
		// r3 has 24 reactions: more than two lines of the pane's bar, so
		// ReactionBar's collapse hides the rest behind "N more"
		r3 := reply("r3", "UCARA0001", "I'll add it now.")
		for i, e := range []string{"tada", "eyes", "palm_tree", "+1", "heart", "rocket", "fire", "100", "clap", "raised_hands",
			"white_check_mark", "joy", "sparkles", "pray", "star", "wave", "sunglasses", "thinking_face", "muscle", "ok_hand",
			"confetti_ball", "heart_eyes", "sob", "grinning"} {
			r3.Reactions = append(r3.Reactions, slack.Reaction{Name: e, Count: 1 + i%3, Users: []string{"UANA00001", "UBEN00001", "UCARA0001"}[:1+i%3]})
		}
		app.S.SetThread(tShots, ts["root"], []slack.Message{root, r1,
			reply("r2", "UCARA0001", "Agreed, and link the migration guide."), r3, bc})
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}

		// the thread pane: "Pinned by ana" on r1, "Also sent to the channel" on bc
		const tw, th0 = 520, 1100
		tv := NewThreadView(host, tShots, ts["root"])
		tops := new(op.Ops)
		ft := func() { shotFrame(tv, th, tw, th0, tops) }
		until(t, 5*time.Second, "the replies", ft, func() bool { return tv.s.rows[ts["bc"]] != nil && itemHeight(tv.s, ts["bc"]) > 0 })
		for i := 0; i < 40; i++ {
			ft()
			time.Sleep(10 * time.Millisecond)
		}
		ft()
		if r := tv.s.rows[ts["bc"]]; r == nil || !r.m.Broadcast {
			t.Fatalf("the broadcast reply isn't in the thread pane")
		}
		if r := tv.s.rows[ts["r3"]]; r == nil || len(r.reactRects) == 0 || len(r.reactRects) >= len(r.m.Reactions) {
			t.Errorf("r3's 24 reactions didn't collapse behind \"N more\"")
		} else {
			t.Logf("r3: %d of %d pills, then \"%d more\"", len(r.reactRects), len(r.m.Reactions), len(r.m.Reactions)-len(r.reactRects))
		}
		timg := pixeltest.Render(t, tw, th0, tops)
		ty0, ty1 := rowsSpan(tv.s)
		ty0, ty1 = ty0+tv.s.origin.Y, ty1+tv.s.origin.Y
		savePNG(t, filepath.Join(dir, "audit-thread-labels-"+mode+".png"), crop(timg, ty0, ty1))

		// the reaction tooltip over b1's :tada: (You, ana and cara)
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		var tipAt image.Rectangle
		f := func() {
			shotFrame(v, th, w, h, ops)
			r := v.s.rows[ts["b1"]]
			if r == nil || len(r.reactRects) == 0 || len(r.m.Reactions) == 0 {
				return
			}
			for i, it := range v.s.items {
				if it.kind != itMsg || it.key != ts["b1"] || i >= len(v.s.pos) {
					continue
				}
				pill := r.reactRects[0].Add(image.Pt(0, v.s.pos[i]))
				re := r.m.Reactions[0]
				url := re.URL
				if url == "" {
					url, _, _ = app.S.Emoji(re.Name)
				}
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
				reactionTooltip(gtx, host, th, colorsFor(th), reactionTip(host, &re, fakeslack.SelfID), url,
					pill, image.Rect(0, 0, w, h))
				tipAt = pill
			}
		}
		loadList(v, f)
		until(t, 5*time.Second, "the reactions", f, func() bool { return !tipAt.Empty() })
		for i := 0; i < 60; i++ {
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		img := pixeltest.Render(t, w, h, ops)
		savePNG(t, filepath.Join(dir, "audit-reaction-tip-"+mode+".png"), crop(img, max(tipAt.Min.Y-170, 0), min(tipAt.Max.Y+16, h)))

		// Karim's numbered list (2026-11-10): an ordered rich_text_list
		// with an empty third item (Slack's send keeps it) and a nested
		// level; Slack's ::marker "N. " ends 8px before the text
		const tList = "C0LISTS01"
		app.S.PutConv(slack.Conversation{ID: tList, Name: "lists", IsChannel: true, IsMember: true, NumMembers: 3})
		empty := map[string]any{"type": "rich_text_section", "elements": []any{}}
		ol := map[string]any{"type": "rich_text_list", "style": "ordered", "indent": 0,
			"elements": []any{shotSection("goofy looking alignment"), shotSection("why is the welcome there"), empty}}
		sub := map[string]any{"type": "rich_text_list", "style": "ordered", "indent": 1,
			"elements": []any{shotSection("a nested item"), shotSection("and another")}}
		ten := map[string]any{"type": "rich_text_list", "style": "ordered", "indent": 0, "offset": 8,
			"elements": []any{shotSection("ninth"), shotSection("tenth")}}
		lt := ts["c1"]
		app.S.SetLatest(tList, []slack.Message{{Type: "message", TS: lt, User: "UBEN00001", Text: "1. goofy looking alignment",
			Blocks: shotRich(ol, sub, ten)}}, false)
		lv := NewChannelView(host, tList)
		lv.Preview = true
		const lw, lh = 900, 700
		lops := new(op.Ops)
		fl := func() { shotFrame(lv, th, lw, lh, lops) }
		loadList(lv, fl)
		until(t, 5*time.Second, "the list row", fl, func() bool { return lv.s.rows[lt] != nil && itemHeight(lv.s, lt) > 0 })
		for i := 0; i < 40; i++ {
			fl()
			time.Sleep(10 * time.Millisecond)
		}
		fl()
		limg := pixeltest.Render(t, lw, lh, lops)
		ly0, ly1 := rowsSpan(lv.s)
		ly0, ly1 = ly0+lv.s.origin.Y, ly1+lv.s.origin.Y
		savePNG(t, filepath.Join(dir, "audit-ordered-list-"+mode+".png"), crop(limg, max(ly0-8, 0), min(ly1+8, lh)))
	}
}
