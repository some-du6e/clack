package views

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// pillTipShowing reports whether reaction r of the bar keyed key has its
// ReactionTip up at now.
func (b *base) pillTipShowing(key string, r *core.Reaction, now time.Time) bool {
	t := b.pillTips[key+":"+r.Name]
	return t != nil && t.hov && !now.Before(t.since.Add(messages.ReactionTipDelay))
}

// The Threads, Unreads and Activity views' reaction pills have a
// channel's ReactionTip (messages.ReactionTooltip), natively and with
// ReactionBar patched, through a router with Windows' mouse (pointer 1);
// a pill the pointer left in a frame that didn't draw it hasn't its tip
// when it's drawn again.
func TestViewsReactionPillTip(t *testing.T) {
	for _, vw := range []vpView{vpViews[0], vpViews[3]} {
		t.Run(vw.name, func(t *testing.T) {
			tt, u, h := vpEnv(t, false)
			b := vw.base(h)
			msgs := vpMessages()
			var r input.Router
			ops := new(op.Ops)
			var now time.Time
			var pts time.Duration
			frame := func(drawn bool) {
				ops.Reset()
				now = time.Now()
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(vpW, vpH)),
					Now: now, Source: r.Source()}
				slackc.Frame(gtx)
				b.frame()
				if drawn {
					y := 40
					for _, m := range msgs {
						g := gtx
						g.Constraints = layout.Constraints{Max: image.Pt(vpW-2*vpX, 200)}
						off := op.Offset(image.Pt(vpX, y)).Push(ops)
						d := b.actReactionsBar(g, m, vw.noAdd)
						off.Pop()
						y += d.Size.Y + 40
					}
				}
				r.Frame(ops)
			}
			move := func(p image.Point, drawn bool) {
				pts += 30 * time.Millisecond
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: pts})
				frame(drawn)
			}
			rest := func(p image.Point, d time.Duration) {
				for start := time.Now(); time.Since(start) < d; time.Sleep(15 * time.Millisecond) {
					move(p, true)
				}
				move(p, true)
			}
			key := "message-" + msgs[0].TS + "-" + msgs[0].Channel
			parrot := &msgs[0].Reactions[0] // the first pill: Bob's
			onPill, away := image.Pt(vpX+12, 40+12), image.Pt(vpX+12, vpH-10)
			check := func(what string) {
				t.Helper()
				rest(away, 50*time.Millisecond)
				rest(onPill, messages.ReactionTipDelay+150*time.Millisecond)
				if !b.pillTipShowing(key, parrot, now) {
					t.Fatalf("%s: resting on the first pill at %v shows no ReactionTip", what, onPill)
				}
				tip := messages.NewReactionTooltip(h, parrot, sSelf)
				if txt := tip.Text(); !strings.Contains(txt, "reacted with") {
					t.Errorf("%s: the tip reads %q", what, txt)
				}
			}
			frame(true)
			check("native")
			// the pointer leaves in a frame that doesn't draw the bars
			move(away, false)
			rest(image.Pt(vpX+12, 40+30), 50*time.Millisecond)
			if b.pillTipShowing(key, parrot, now) {
				t.Error("drawn again with the pointer off it, the pill still has its tip")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			id, err := tt.Import(ctx, vpPass, "")
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if err := tt.SetEnabled(ctx, id, true); err != nil {
				t.Fatalf("enable: %v", err)
			}
			for deadline := time.Now().Add(15 * time.Second); !slackc.Patched("ReactionBar") || !u.treeHas(`"reactionKey":"`+key+`"`); time.Sleep(15 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("timed out waiting for the patched bars")
				}
				frame(true)
			}
			check("ReactionBar patched")
		})
	}
}
