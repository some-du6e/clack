package messages

import (
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The connection states' APIs the messages tests use, through helpers
// (the red tree's shim stubs them: the old code has none of them).

func setConnectionTrouble(f func(Host) time.Time) (restore func()) {
	prev := connectionTrouble
	connectionTrouble = f
	return func() { connectionTrouble = prev }
}

func setBannerTimings(tick, hide time.Duration) (restore func()) {
	t, h := DegradedBannerTick, DegradedBannerHide
	DegradedBannerTick, DegradedBannerHide = tick, hide
	return func() { DegradedBannerTick, DegradedBannerHide = t, h }
}

// backdateBanner moves the pane's banner's lastUpdatedTs back by d.
func backdateBanner(v *ChannelView, d time.Duration) {
	v.s.degraded.updated = v.s.degraded.updated.Add(-d)
}

// paneBanner is the pane's DegradedBanner as the last frame drew it.
func paneBanner(v *ChannelView) *DegradedBanner { return v.DegradedBannerState() }

func layoutBanner(b *DegradedBanner, gtx layout.Context, th *theme.Theme, since time.Time) {
	b.Layout(gtx, th, since, BannerPill, 14, gtx.Constraints.Max.X, nil)
}

func resetBanner(b *DegradedBanner) { b.Reset() }

func setClientOnline(f func(Host) bool) (restore func()) {
	prev := clientOnline
	clientOnline = f
	return func() { clientOnline = prev }
}
