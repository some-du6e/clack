package messages

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// rpFetch serves a 64px PNG for any URL and counts the fetches per URL.
type rpFetch struct {
	mu sync.Mutex
	n  map[string]int
}

func (f *rpFetch) fetch(_ context.Context, u string) ([]byte, error) {
	f.mu.Lock()
	if f.n == nil {
		f.n = map[string]int{}
	}
	f.n[u]++
	f.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0xc0
	}
	img.Set(3, 3, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes(), nil
}

func (f *rpFetch) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.n {
		n += c
	}
	return n
}

// TestReactionPickDrawsAtOnce: picking an emoji from the reaction picker is
// Slack's optimistic update (submitAddOrRemoveReaction): the pill is drawn
// in the frame after the pick while reactions.add is still held
// (fakeslack Fault.ReplyDelay), with the picture the picker already loaded
// (no new fetch, no `:name:` fallback): custom emoji, a custom alias, a
// standard emoji, and a custom emoji on a thread reply. Karim 2026-11-12:
// ":verity:" took a while to show and "looked not cached properly".
func TestReactionPickDrawsAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name, emoji string
		reply       bool
	}{
		{"custom", "verity", false},
		{"custom alias", "verity_too", false},
		{"standard", "tada", false},
		{"custom on a reply", "verity", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, fs, ts := esApp(t)
			app.S.CustomEmoji.Set(map[string]string{
				"verity":     "https://emoji.slack-edge.com/" + fakeslack.TeamID + "/verity/0a1b2c.png",
				"verity_too": "alias:verity",
			})
			root := ts["status"]
			reply := root[:len(root)-1] + "9" // a reply a moment later
			if tc.reply {
				fs.SetHook(func(method string, form url.Values) (any, bool) {
					if method != "conversations.replies" {
						return nil, false
					}
					return map[string]any{"ok": true, "has_more": false, "messages": []any{
						map[string]any{"type": "message", "ts": root, "thread_ts": root, "user": "UCARA0001", "text": "Back Monday", "reply_count": 1},
						map[string]any{"type": "message", "ts": reply, "thread_ts": root, "user": "UANA00001", "text": "see you"},
					}}, true
				})
				t.Cleanup(func() { fs.SetHook(nil) })
			}
			fs.SetFault("reactions.add", fakeslack.Fault{ReplyDelay: 4 * time.Second})
			t.Cleanup(func() { fs.SetFault("reactions.add", fakeslack.Fault{}) })

			th := theme.New(false)
			f := &rpFetch{}
			imgs := imgcache.New(f.fetch, "", nil)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgs}
			var s *listState
			var lay func(gtx layout.Context) layout.Dimensions
			target := ts["inline"]
			if tc.reply {
				v := NewThreadView(host, esChan, root)
				s, lay, target = v.s, v.Layout, reply
			} else {
				v := NewChannelView(host, esChan)
				v.Preview = true
				s, lay = v.s, v.Layout
			}
			ops := new(op.Ops)
			frame := func() {
				ops.Reset()
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(700, 420)), Now: time.Now()}
				lay(gtx)
			}
			until(t, 5*time.Second, "the row", frame, func() bool { return s.rows[target] != nil && itemHeight(s, target) > 0 })

			// the picker showed it: its cell's size is loaded
			u, _, ok := app.S.Emoji(tc.emoji)
			if !ok || u == "" {
				t.Fatalf("%s doesn't resolve", tc.emoji)
			}
			const pickerPx = 44 // the picker's 22px cell at 2x, bigger than a pill's 16px
			until(t, 5*time.Second, "the picker's image", frame, func() bool { _, ok := imgs.Get(u, pickerPx); return ok })
			fetched := f.total()

			// the pick: popups.React's unwrapped path
			ev, call := app.ReactBegin(esChan, target, tc.emoji, true)
			if ev == nil {
				t.Fatal("ReactBegin changed nothing (the message isn't in the store?)")
			}
			ApplyEvent(app, *ev)
			done := make(chan error, 1)
			go func() { done <- call(context.Background()) }()

			frame() // the next frame
			r := s.rows[target]
			var url string
			for _, re := range r.m.Reactions {
				if re.Name == tc.emoji {
					if !re.Self || re.Count != 1 {
						t.Errorf("pill %+v: want the viewer's, count 1", re)
					}
					url = re.URL
				}
			}
			if url == "" {
				t.Fatalf("no %s pill in the frame after the pick: %+v", tc.emoji, r.m.Reactions)
			}
			if url != u {
				t.Errorf("the pill's picture %q, the picker's %q", url, u)
			}
			es := dpPx(16)
			if _, ok := imgs.Size(url, es); !ok {
				t.Errorf("the pill's first frame had no picture (no %dpx entry: it fell back to :%s:)", es, tc.emoji)
			}
			if n := f.total(); n != fetched {
				t.Errorf("the pill fetched again (%d → %d)", fetched, n)
			}
			select {
			case err := <-done:
				t.Fatalf("reactions.add answered (%v) before the check: the hold didn't hold", err)
			default:
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			for range 3 {
				frame()
			}
			for _, re := range s.rows[target].m.Reactions {
				if re.Name == tc.emoji && (re.Count != 1 || !re.Self) {
					t.Errorf("after the reply: %+v", re)
				}
			}
		})
	}
}

// dpPx is a dp at the test's scale (1).
func dpPx(v float32) int { return int(v + .5) }
