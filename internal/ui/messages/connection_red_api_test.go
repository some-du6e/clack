package messages

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Red-tree shims: the connection states' APIs don't exist on the old code.

type DegradedBanner struct {
	Text, Link string
	R, LinkR   image.Rectangle
	Aria       string
}

func setConnectionTrouble(f func(Host) time.Time) (restore func())                         { return func() {} }
func setBannerTimings(tick, hide time.Duration) (restore func())                           { return func() {} }
func backdateBanner(v *ChannelView, d time.Duration)                                       {}
func paneBanner(v *ChannelView) *DegradedBanner                                            { return &DegradedBanner{} }
func layoutBanner(b *DegradedBanner, gtx layout.Context, th *theme.Theme, since time.Time) {}
func resetBanner(b *DegradedBanner)                                                        {}

func setClientOnline(f func(Host) bool) (restore func()) {
	prev := clientOnline
	clientOnline = f
	return func() { clientOnline = prev }
}
