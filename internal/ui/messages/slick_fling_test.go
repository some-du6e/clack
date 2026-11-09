package messages

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// Flinging through a channel with message-patching Slick plugins running
// (their trees draw the rows): once the list settles, every row is as tall
// as it was before the fling, so no spacing is left between rows.
func TestSlickViewFlingKeepsRowHeights(t *testing.T) {
	const ch = "C0FLINGS1"
	users := []string{"UALICE", "UBOB", tSelf}
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "fling", IsChannel: true, IsMember: true, NumMembers: 3})
		var msgs []slack.Message
		at := int64(1700000000)
		for i := 0; i < 160; i++ {
			at += int64(20 + (i*37)%900)
			u := users[(i/3+i%2)%len(users)]
			text := fmt.Sprintf("synthetic message %d", i)
			if i%4 == 0 {
				text += " with a longer body that wraps over more than one line of the message pane so rows differ in height"
			}
			m := slack.Message{Type: "message", TS: fmt.Sprintf("%d.%06d", at, 100+i), User: u, Text: text, Blocks: richBlocks(textEl(text))}
			if i%5 == 0 {
				m.Reactions = []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"UBOB", "UALICE"}}}
			}
			if i%11 == 0 {
				m.ThreadTS, m.ReplyCount, m.ReplyUsers, m.LatestReply = m.TS, 2, []string{"UBOB"}, fmt.Sprintf("%d.000001", at+5)
			}
			msgs = append(msgs, m)
		}
		s.SetLatest(ch, msgs, false)
	})
	e.install(t, "ShowSendingBot", nil)
	e.install(t, "BetterBlock", nil)
	e.install(t, "MessageLogger", nil)
	e.install(t, "WhoReacted", nil)
	e.install(t, "LocalTime", nil)
	patched(t, "MessageKitGutter", "MessageWrapper", "Message", "MessageText", "Reaction", "BaseBroadcastPreamble")

	v := NewChannelView(e.h, ch)
	v.Preview = true
	var r input.Router
	ops := new(op.Ops)
	size := image.Pt(1600, 1200)
	now := time.Now()
	shots := os.Getenv("SLICK_SHOTS_DIR")
	nshot := 0
	frame := func() {
		ops.Reset()
		now = now.Add(16 * time.Millisecond)
		paint.FillShape(ops, e.h.th.C.Bg, clip.Rect{Max: size}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	shot := func(name string) {
		if shots == "" || nshot > 40 {
			return
		}
		nshot++
		img := pixeltest.Render(t, size.X, size.Y, ops)
		if f, err := os.Create(filepath.Join(shots, "fling-"+name+".png")); err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
	}
	wheel := func(n int, dy float32) {
		p := f32.Pt(float32(size.X)/2, float32(size.Y)/2)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		for i := 0; i < n; i++ {
			r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(0, dy)})
		}
	}
	settle := func() {
		for i := 0; i < 60; i++ {
			frame()
			time.Sleep(10 * time.Millisecond)
		}
	}
	heights := func() map[string]int {
		s := v.s
		out := map[string]int{}
		for i := s.visible[0]; i < s.visible[1]; i++ {
			if s.items[i].kind == itMsg && s.pos[i] >= 0 && s.pos[i]+s.heights[i] <= size.Y {
				out[s.items[i].key] = s.heights[i]
			}
		}
		return out
	}
	e.waitFor(t, "the rows", frame, func() bool { return len(v.s.rows) > 5 })
	settle()
	bottom := heights()
	shot("0-bottom")
	if len(bottom) < 5 {
		t.Fatalf("only %d rows on screen", len(bottom))
	}
	for f := 0; f < 400 && !(v.s.list.Position.First == 0 && v.s.list.Position.Offset == 0); f++ {
		wheel(6, -500)
		frame()
		if f < 6 {
			shot(fmt.Sprintf("1-up-%d", f))
		}
	}
	settle()
	top := heights()
	shot("2-top")
	for f := 0; f < 400 && v.s.list.Position.BeforeEnd; f++ {
		wheel(6, 500)
		frame()
		if f < 6 {
			shot(fmt.Sprintf("3-down-%d", f))
		}
	}
	settle()
	shot("4-bottom-again")
	for k, h := range heights() {
		if want, ok := bottom[k]; ok && h != want {
			t.Errorf("row %s: %d tall after the fling, %d before", k, h, want)
		}
	}
	// and up again: the top rows as they were
	for f := 0; f < 400 && !(v.s.list.Position.First == 0 && v.s.list.Position.Offset == 0); f++ {
		wheel(6, -500)
		frame()
	}
	settle()
	shot("5-top-again")
	for k, h := range heights() {
		if want, ok := top[k]; ok && h != want {
			t.Errorf("row %s: %d tall at the top the second time, %d the first", k, h, want)
		}
	}
}
