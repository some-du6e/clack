package composer

import (
	"testing"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The thread pane's reply box sits under .p-threads_footer__input_container
// { margin: 12px 16px 0 }: 12px between the last reply and the box. The
// channel composer has none (the list's 24px bottom spacer is above it),
// and neither has the Threads view's inline reply (.p-threads_view …
// { margin: 0 20px }). The box is drawn there too (BoxRect), the chrome
// adding no margin of its own.
func TestThreadComposerTopMargin(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	const ts = "1700000000.000100"
	for _, tc := range []struct {
		name    string
		ts      string
		threads bool
		want    int
	}{
		{"channel", "", false, 0},
		{"thread pane", ts, false, 12},
		{"Threads view", ts, true, 0},
	} {
		clearDraft(tChGen, tc.ts)
		c := New(h, tChGen, tc.ts)
		c.InThreadsView = tc.threads
		for _, scale := range []float32{1, 2} {
			gtx := gtxForProps()
			gtx.Metric.PxPerDp, gtx.Metric.PxPerSp = scale, scale
			slackc.Frame(gtx)
			d := c.Layout(gtx)
			want := int(float32(tc.want)*scale + 0.5)
			if c.top != want {
				t.Errorf("%s at %vx: box %d px below the composer's top, Slack %v", tc.name, scale, c.top, float32(tc.want)*scale)
			}
			// where the box is drawn: the chrome adds nothing on top of
			// the composer's margin (it once added the 12px again: 24px
			// under the last reply, Karim 2026-11-12)
			if b := c.BoxRect(); b.Empty() || b.Min.Y-c.Origin.Y != want {
				t.Errorf("%s at %vx: box drawn at %v, %d px below the composer's top, Slack %v", tc.name, scale, b, b.Min.Y-c.Origin.Y, float32(tc.want)*scale)
			}
			if d.Size.Y <= c.top {
				t.Errorf("%s: composer %d tall, box at %d", tc.name, d.Size.Y, c.top)
			}
		}
		c.Close()
		clearDraft(tChGen, tc.ts)
	}
}
