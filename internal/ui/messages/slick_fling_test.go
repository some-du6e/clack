package messages

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// Flinging through a channel with message-patching Slick plugins running
// (their trees draw the rows): once the list settles, every row is as tall
// as it was before the fling, so no spacing is left between rows.
func TestSlickViewFlingKeepsRowHeights(t *testing.T) {
	all := []string{"ShowSendingBot", "BetterBlock", "MessageLogger", "WhoReacted", "LocalTime"}
	for _, ps := range append([][]string{all}, []string{"LocalTime"}, []string{"ShowSendingBot"}, []string{"BetterBlock"}, []string{"MessageLogger"}, []string{"WhoReacted"}) {
		name := ps[0]
		if len(ps) > 1 {
			name = "all"
		}
		t.Run(name, func(t *testing.T) { slickFling(t, name, ps) })
	}
}

func slickFling(t *testing.T, name string, plugins []string) {
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
	for _, p := range plugins {
		e.install(t, p, nil)
	}
	time.Sleep(300 * time.Millisecond)

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
	tag := name
	shot := func(name string) {
		if shots == "" || nshot > 40 {
			return
		}
		nshot++
		img := pixeltest.Render(t, size.X, size.Y, ops)
		if f, err := os.Create(filepath.Join(shots, "fling-"+tag+"-"+name+".png")); err == nil {
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
		for i := 0; i < 100; i++ {
			frame()
			time.Sleep(10 * time.Millisecond)
		}
	}
	geom := map[string]string{}
	heights := func() map[string]int {
		s := v.s
		out := map[string]int{}
		for i := s.visible[0]; i < s.visible[1]; i++ {
			if s.items[i].kind == itMsg && s.pos[i] >= 0 && s.pos[i]+s.heights[i] <= size.Y {
				out[s.items[i].key] = s.heights[i]
				if r := s.rows[s.items[i].key]; r != nil {
					geom[s.items[i].key] = fmt.Sprintf("header %v col %v body %v nameBL %d", s.items[i].header, r.col, r.bodyRect, r.nameBL)
				}
			}
		}
		return out
	}
	// plain rows (one short line, no reactions or replies) as Slack draws
	// them at 2x: a header row 52px, a continuation 30px
	slack := func(when string) {
		s := v.s
		for i := s.visible[0]; i < s.visible[1]; i++ {
			it := s.items[i]
			if it.kind != itMsg || len(it.group) > 0 {
				continue
			}
			var n int
			if _, err := fmt.Sscanf(it.key[strings.IndexByte(it.key, '.')+1:], "%d", &n); err != nil {
				continue
			}
			if n -= 100; n%4 == 0 || n%5 == 0 || n%11 == 0 {
				continue
			}
			want := 60
			if it.header {
				want = 104
			}
			if s.heights[i] != want {
				t.Errorf("%s: plain row %d (header %v) is %dpx, Slack %dpx", when, n, it.header, s.heights[i], want)
			}
		}
	}
	// every row as tall as the same row drawn natively: these plugins
	// change nothing about its height with this workspace (nobody blocked
	// or deleted, no bots, no time zones), so any difference is layout
	nativeH := func(when string) {
		s := v.s
		names := slackc.PatchedNames()
		slackc.SetPatched(nil)
		defer slackc.SetPatched(names)
		for i := s.visible[0]; i < s.visible[1]; i++ {
			it := s.items[i]
			if it.kind != itMsg || len(it.group) > 0 || s.pos[i] < 0 || s.pos[i]+s.heights[i] > size.Y {
				continue
			}
			r := newListRow(&s.snap.msgs[it.idx], s.thread)
			rc := s.rc
			rc.header, rc.hovered, rc.highlight, rc.parent = it.header, false, 0, false
			gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Constraints{Min: image.Pt(size.X, 0), Max: image.Pt(size.X, 1<<20)}, Now: now}
			if want := r.layout(gtx, &rc).Size.Y; s.heights[i] != want {
				r0 := s.rows[it.key]
				t.Errorf("%s: row %s (header %v, %d reactions) is %dpx, %dpx drawn natively; col %v body %v", when, it.key, it.header,
					len(r0.m.Reactions), s.heights[i], want, r0.col, r0.bodyRect)
			}
		}
	}
	e.waitFor(t, "the rows", frame, func() bool { return len(v.s.rows) > 5 })
	settle()
	slack("opened")
	nativeH("opened")
	bottom := heights()
	g0 := map[string]string{}
	for k, v := range geom {
		g0[k] = v
	}
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
	slack("at the top")
	nativeH("at the top")
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
	nativeH("back at the bottom")
	for k, h := range heights() {
		if want, ok := bottom[k]; ok && h != want {
			t.Errorf("row %s: %d tall after the fling, %d before\n\tbefore %s\n\tafter  %s", k, h, want, g0[k], geom[k])
		}
	}
	// and up again: the top rows as they were
	for f := 0; f < 400 && !(v.s.list.Position.First == 0 && v.s.list.Position.Offset == 0); f++ {
		wheel(6, -500)
		frame()
	}
	settle()
	shot("5-top-again")
	nativeH("at the top again")
	for k, h := range heights() {
		if want, ok := top[k]; ok && h != want {
			t.Errorf("row %s: %d tall at the top the second time, %d the first", k, h, want)
		}
	}
}

// The idle channel and thread (idle_test.go: an image, an unfurl, bots
// with icons, a message_mention, a thread root) with the real Slick
// plugins that patch message rows: once loaded nothing re-lays out or
// keeps asking for frames, pointer away or resting on any row.
func TestSlickViewIdleRowsStayPut(t *testing.T) {
	e := newSlickEnv(t, nil)
	for _, p := range []string{"ShowSendingBot", "BetterBlock", "MessageLogger", "WhoReacted", "LocalTime"} {
		e.install(t, p, nil)
	}
	time.Sleep(300 * time.Millisecond)
	idleViews(t, e.app, e.h, func() bool { return e.ui.anyTree(`"n":"MessageWrapper"`) })
}
