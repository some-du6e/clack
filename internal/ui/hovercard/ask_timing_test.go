package hovercard

// A sender's trigger asks for its bot in the frame the pointer comes onto
// it (review of thread-header-hover-4): the row worked the bot out before
// the trigger read that frame's pointer events, so a pointer that came on
// in one move and rested asked only once the card opened, and the card
// always opened on the stand-in bot.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestTriggerAsksTheFramePointerArrives(t *testing.T) {
	c := &Controller{}
	prev := current
	current = c
	t.Cleanup(func() { current = prev })
	var tr Trigger
	var rt input.Router
	ops := new(op.Ops)
	asked := 0
	bot := func(ask bool) string {
		if ask {
			asked++
		}
		return "B0BOT00001"
	}
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(200, 100)), Now: time.Now(), Source: rt.Source()}
		layoutAsking(&tr, gtx, "U0BOT00001", bot, image.Pt(100, 20))
		rt.Frame(ops)
	}
	frame()
	if asked != 0 {
		t.Fatalf("asked %d times with the pointer elsewhere", asked)
	}
	rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(10, 10), Time: time.Second})
	frame()
	if asked == 0 {
		t.Error("the frame the pointer came onto the trigger asked nothing; a sender asks while its trigger is in use, from that frame")
	}
}
