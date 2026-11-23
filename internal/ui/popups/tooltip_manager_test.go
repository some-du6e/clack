package popups

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// tipFrame lays a tooltip out at now, hovered or not.
func tipFrame(th *theme.Theme, st *TipState, hovered bool, now time.Time) {
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(400, 300)), Now: now}
	Tooltip(gtx, th, st, hovered, image.Pt(40, 20), Above, "Tip", "")
}

// A tooltip with no delay of its own waits BaseTooltip's 150 ms (client-
// boot HzHE: delay 150), not longer.
func TestTipDelayIsBaseTooltips(t *testing.T) {
	th := theme.New(false)
	st := &TipState{}
	t0 := time.Unix(1000, 0)
	tipFrame(th, st, true, t0)
	if st.Showing(t0.Add(100 * time.Millisecond)) {
		t.Error("the tooltip shows 100 ms into the hover; BaseTooltip waits 150 ms")
	}
	tipFrame(th, st, true, t0.Add(160*time.Millisecond))
	if !st.Showing(t0.Add(160 * time.Millisecond)) {
		t.Error("no tooltip 160 ms into the hover; BaseTooltip's delay is 150 ms")
	}
}

// Slack's TooltipManager: hideVisibleTooltip (a modal or menu opening,
// Escape) hides the tooltip up, which comes back only after the pointer
// left and came back, and waits its delay again.
func TestHideTooltipsUntilLeave(t *testing.T) {
	th := theme.New(false)
	st := &TipState{Delay: 100 * time.Millisecond} // its own delay: the control holds on any default
	t0 := time.Unix(1000, 0)
	tipFrame(th, st, true, t0)
	tipFrame(th, st, true, t0.Add(200*time.Millisecond))
	if !st.Showing(t0.Add(200 * time.Millisecond)) {
		t.Fatal("control: the tooltip isn't up 200 ms into the hover")
	}
	hideTipsT()
	for _, d := range []time.Duration{210, 400, 900} {
		tipFrame(th, st, true, t0.Add(d*time.Millisecond))
		if st.Showing(t0.Add(d * time.Millisecond)) {
			t.Fatalf("hidden at 200 ms, the tooltip is up again at %d ms with the pointer still on it", d)
		}
	}
	tipFrame(th, st, false, t0.Add(1000*time.Millisecond))
	tipFrame(th, st, true, t0.Add(1100*time.Millisecond))
	if st.Showing(t0.Add(1150 * time.Millisecond)) {
		t.Error("back on the target, the tooltip showed before its delay")
	}
	tipFrame(th, st, true, t0.Add(1300*time.Millisecond))
	if !st.Showing(t0.Add(1300 * time.Millisecond)) {
		t.Error("back on the target, the tooltip didn't show after its delay")
	}
}

// The channel header's notifications button (NotificationsButtonMenu,
// source channelHeader): "Edit notifications" over "Current: …" in a
// channel, Mute / Unmute conversation in a DM.
func TestNotifyButtonTip(t *testing.T) {
	h := mkHost(t)
	text := func(ls []TipLine) string {
		var b []string
		for _, l := range ls {
			if l.Subtitle {
				b = append(b, "("+l.Text+")")
			} else {
				b = append(b, l.Text)
			}
		}
		return strings.Join(b, " ")
	}
	cases := []struct {
		dm, muted bool
		want      string
	}{
		{false, false, "Edit notifications (Current: "},
		{false, true, "Edit notifications (Current: Muted)"},
		{true, false, "Mute conversation"},
		{true, true, "Unmute conversation"},
	}
	// a channel's setting is the workspace's (no override); a mobile
	// setting that differs adds its own subtitle after it
	for _, c := range cases {
		got := text(notifyButtonTipT(h, "C0NOTIFY01", c.dm, c.muted))
		if c.dm && got != c.want || !c.dm && !strings.HasPrefix(got, c.want) {
			t.Errorf("dm %v muted %v: %q, want %q", c.dm, c.muted, got, c.want)
		}
	}
	if got := text(notifyButtonTipT(h, "C0NOTIFY01", false, false)); !strings.Contains(got, "(Current: All new posts)") &&
		!strings.Contains(got, "(Current: Just mentions)") && !strings.Contains(got, "(Current: Nothing)") {
		t.Errorf("a channel's tip %q names no setting", got)
	}
}
