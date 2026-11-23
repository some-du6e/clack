package messages

// A day divider halfway past the message list's top: Slack's label is
// position: sticky inside the divider (.c-message_list__day_divider__label),
// so its one pill stops 6px under the list's top and nothing of it shows
// above. Clack drew the divider's own pill where the row put it, partly
// over the top, and the sticky copy under it: two pills, the first one's
// edge over the second. Synthetic data, drawn headlessly (Mesa on CI).

import (
	"fmt"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestDayPillStraddlingTheTopLikeSlack(t *testing.T) {
	const ch = "C0DAYPILL1"
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "days", IsChannel: true, IsMember: true, NumMembers: 4})
	day0 := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)
	var msgs []slack.Message
	for d := 0; d < 2; d++ {
		for i := 0; i < 20; i++ {
			at := day0.AddDate(0, 0, d).Add(time.Duration(i) * 10 * time.Minute)
			msgs = append(msgs, slack.Message{Type: "message", TS: fmt.Sprintf("%d.000100", at.Unix()), User: []string{"UALICE", "UBOB"}[i%2],
				Text: fmt.Sprintf("synthetic message %d of day %d", i, d)})
		}
	}
	app.S.SetLatest(ch, msgs, false)
	th := theme.New(false)
	v := NewChannelView(coreHost{app: app, th: th}, ch)
	const w, h = 900, 700
	ops := new(op.Ops)
	frame := func() { shotFrame(v, th, w, h, ops) }
	until(t, 5*time.Second, "the newest message", frame, func() bool { return itemHeight(v.s, msgs[39].TS) > 0 })
	// the second day's divider
	d := -1
	for i, it := range v.s.items {
		if it.kind == itDay && sameDay(it.day, day0.AddDate(0, 0, 1)) {
			d = i
		}
	}
	if d < 0 {
		t.Fatal("no divider for the second day")
	}
	// 5px of it over the list's top: its pill would start at 1px, over
	// the sticky place (6px)
	v.s.list.Position = layout.Position{First: d, Offset: 5, BeforeEnd: true}
	for i := 0; i < 3; i++ {
		frame()
	}
	if v.s.visible[0] != d || v.s.pos[d] != -5 {
		t.Fatalf("the divider is at %d (first row %d), not -5 at the top: the case tests nothing", v.s.pos[d], v.s.visible[0])
	}
	img := pixeltest.Render(t, w, h, ops)
	top := v.s.origin.Y
	cx := v.s.origin.X + v.s.size.X/2
	diff := func(a, b [4]uint8) int {
		m := 0
		for k := 0; k < 3; k++ {
			m = max(m, absDiff(int(a[k]), int(b[k])))
		}
		return m
	}
	px := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8{img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3]}
	}
	// the rows over the sticky place, across the pill: the list's
	// background as at its left edge, no pill's edge
	for y := top; y < top+3; y++ {
		ref := px(v.s.origin.X+5, y)
		for x := cx - 30; x <= cx+30; x++ {
			if dd := diff(px(x, y), ref); dd > 12 {
				t.Errorf("at (%d,%d), %dpx under the list's top, a pill's edge (%v against the background's %v): two pills, one over the top",
					x, y, y-top, px(x, y), ref)
				return
			}
		}
	}
}

func absDiff(a, b int) int {
	if a < b {
		return b - a
	}
	return a - b
}
