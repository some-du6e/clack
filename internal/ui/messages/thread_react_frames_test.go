package messages

import (
	"encoding/json"
	"image"
	"net/url"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim, 2026-11-13: "sometimes reactions make the entire thread
// flicker" (a thread with an agent app's replies and a 👍). Slack changes
// only the reacted message. Frame by frame, from the reaction to a settled
// pane: every row of the thread is there in every frame, and each row's
// y and height is either what it was or what it ends at (rows above the
// reacted one never move; rows below move once, by the pill row). Own
// reactions (the optimistic path, then the server's echo) and others'
// (reaction_added / reaction_removed), on the root and on a reply.
func TestThreadReactionChangesOnlyItsMessage(t *testing.T) {
	type tc struct {
		name   string
		reply  bool // on the second reply, else the root
		own    bool
		add    bool
		emoji  string
		reactr string
	}
	for _, c := range []tc{
		{"own add on the root", false, true, true, "+1", fakeslack.SelfID},
		{"own add on a reply", true, true, true, "+1", fakeslack.SelfID},
		{"own remove on a reply", true, true, false, "grin", fakeslack.SelfID},
		{"other add on the root", false, false, true, "tada", "UCARA0001"},
		{"other add on a reply", true, false, true, "tada", "UCARA0001"},
		{"other remove on a reply", true, false, false, "grin", "UANA00001"},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, fs, ts := esApp(t)
			root := ts["status"]
			r1, r2, r3 := root[:len(root)-1]+"7", root[:len(root)-1]+"8", root[:len(root)-1]+"9"
			// the thread's copy of the root (conversations.replies) isn't the
			// channel timeline's (esApp's: other text, no reply_count), as a
			// pane's copy and a stale channel copy can differ; a reaction on
			// the root must not swap one for the other
			fs.SetHook(func(method string, form url.Values) (any, bool) {
				if method != "conversations.replies" {
					return nil, false
				}
				return map[string]any{"ok": true, "has_more": false, "messages": []any{
					map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "UCARA0001", "text": "Back Monday", "reply_count": 3},
					map[string]any{"type": "message", "ts": r1, "thread_ts": root, "bot_id": "B0GORKIE1", "username": "gorkie", "text": "an agent's synthetic answer"},
					map[string]any{"type": "message", "ts": r2, "thread_ts": root, "user": "UANA00001", "text": "see you", "reactions": []any{
						map[string]any{"name": "grin", "count": 2, "users": []any{"UANA00001", fakeslack.SelfID}},
					}},
					map[string]any{"type": "message", "ts": r3, "thread_ts": root, "bot_id": "B0GORKIE1", "username": "gorkie", "text": "a synthetic follow-up"},
				}}, true
			})
			t.Cleanup(func() { fs.SetHook(nil) })
			host := identityHost{coreHost: coreHost{app: app, th: theme.New(true)}, imgs: imgcache.New((&rpFetch{}).fetch, "", nil)}
			v := NewThreadView(host, esChan, root)
			s := v.s
			ops := new(op.Ops)
			frame := func() {
				ops.Reset()
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(600, 1400)), Now: time.Now()}
				v.Layout(gtx)
			}
			all := []string{root, r1, r2, r3}
			type geo struct{ y, h int }
			geom := func() (map[string]geo, bool) {
				out := map[string]geo{}
				for _, k := range all {
					found := false
					for i, it := range s.items {
						if it.key == k && i < len(s.heights) && i < len(s.pos) && s.heights[i] > 0 {
							out[k], found = geo{s.pos[i], s.heights[i]}, true
						}
					}
					if !found {
						return out, false
					}
				}
				return out, true
			}
			until(t, 5*time.Second, "the thread's rows", frame, func() bool {
				_, ok := geom()
				return ok && s.rows[r2] != nil && len(s.rows[r2].m.Reactions) == 1
			})
			for range 5 {
				frame()
			}
			before, _ := geom()
			target := root
			if c.reply {
				target = r2
			}
			// the change
			if c.own {
				s.react(host, s.rows[target], c.emoji, c.add, fakeslack.SelfID)
			} else {
				typ := "reaction_removed"
				if c.add {
					typ = "reaction_added"
				}
				raw, _ := json.Marshal(map[string]any{"type": typ, "user": c.reactr, "reaction": c.emoji,
					"item": map[string]any{"type": "message", "channel": esChan, "ts": target}})
				app.Inject(slack.Event{Type: typ, Raw: raw})
			}
			var frames []map[string]geo
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				frame()
				g, ok := geom()
				if !ok {
					t.Fatalf("a frame without every row of the thread: %v (want %d rows)", g, len(all))
				}
				frames = append(frames, g)
				time.Sleep(15 * time.Millisecond)
			}
			after := frames[len(frames)-1]
			if after[target].h == before[target].h {
				t.Fatalf("the reacted row's height didn't change (%d): the reaction never drew", before[target].h)
			}
			for i, g := range frames {
				for _, k := range all {
					b, a, x := before[k], after[k], g[k]
					if x != b && x != a {
						t.Errorf("frame %d: row %s at y %d h %d, neither before (%d, %d) nor after (%d, %d)", i, k, x.y, x.h, b.y, b.h, a.y, a.h)
					}
				}
			}
			// rows above the reacted one never move; only it changes height
			for _, k := range all {
				if tsLess(k, target) && after[k] != before[k] {
					t.Errorf("row %s above the reacted one moved: %v → %v", k, before[k], after[k])
				}
				if k != target && after[k].h != before[k].h {
					t.Errorf("row %s changed height: %d → %d", k, before[k].h, after[k].h)
				}
			}
		})
	}
}
