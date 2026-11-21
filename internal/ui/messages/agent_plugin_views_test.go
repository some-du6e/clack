package messages

// A plan drawn through a plugin's msg (MessageText's rewrite, a Blocks
// patch: withElementsFor's plugin branch) in two rows at once (a thread's
// root in the channel and in the pane) keeps one drawing per row, as the
// plain path does (blockkit.InView): at two widths each settles at its own
// height, and the frames after go quiet (no relayout, no frame asked for).

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentPluginPathViews(t *testing.T) {
	app, _, _ := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	fx := fakeslack.AgentFixtures()["plan-grow-title"] // a title that wraps differently at the two widths
	raw, _ := json.Marshal(fx)
	sm := slack.Message{Type: "message", TS: "1762520300.000100", User: fakeslack.AgentUserID, BotID: fakeslack.AgentBotID, Blocks: raw, Text: "plan"}
	m := app.MessageFromSlack(tShots, &sm)
	var blocks []any
	_ = json.Unmarshal(raw, &blocks)
	msg := slackc.Props{"channel": tShots, "ts": sm.TS, "type": "message", "user": sm.User, "bot_id": sm.BotID, "text": "plan", "blocks": blocks}
	mk := func() *row {
		e := entry{rev: 1, m: m}
		r := newRow(&e)
		r.rc = &rowCtx{h: host, selfID: "USELF"}
		r.setDoc()
		r.withElementsFor(host, msg)
		return r
	}
	a, b := mk(), mk()
	ea, eb := a.rv.Options().Elements, b.rv.Options().Elements
	if blockkit.HostOf(ea) == nil || blockkit.HostOf(ea) != blockkit.HostOf(eb) {
		t.Fatalf("the plugin path's rows don't share the message's host: %T %T", ea, eb)
	}
	if ea == eb {
		t.Errorf("both rows draw the plugin's msg as one drawing: their plans share toggles and the height transition")
	}
	st := styleForTheme(host.th, nil)
	// spinners hold still (an inactive window): the frames asked for are
	// the drawings' alone
	prevActive := blockkit.IndicatorActive
	blockkit.IndicatorActive = func() bool { return false }
	t.Cleanup(func() { blockkit.IndicatorActive = prevActive })
	var r input.Router
	now := time.Now().Add(time.Hour) // ahead of the wall clock blockkit measures on
	frame := func() {
		now = now.Add(100 * time.Millisecond)
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: now, Source: r.Source()}
		for _, d := range []struct {
			r *row
			x int
			w int
		}{{a, 0, 480}, {b, 520, 300}} {
			off := op.Offset(image.Pt(d.x, 0)).Push(ops)
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(d.w, 1200)}
			d.r.rv.Layout(g, st, &d.r.handler)
			off.Pop()
		}
		r.Frame(ops)
	}
	for i := 0; i < 8; i++ {
		frame()
	}
	me := blockkit.HostOf(ea)
	v0 := me.ElementsVersion()
	r.WakeupTime() // read what the frames so far asked for
	frame()
	frame()
	if v := me.ElementsVersion(); v != v0 {
		t.Errorf("not quiet: the elements' version moved %d to %d (the two widths fight over one drawing)", v0, v)
	}
	if at, ok := r.WakeupTime(); ok {
		t.Errorf("not quiet: a frame asked for another at %v", at.Sub(now))
	}
}
