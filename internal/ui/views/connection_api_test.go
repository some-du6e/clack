package views

import (
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/messages"
)

// The connection states' APIs the views tests use, through helpers (the
// red tree's shim stubs them: the old code has none of them).

func setStillLoadingAfter(d time.Duration) (restore func()) {
	prev := messages.StillLoadingAfter
	messages.StillLoadingAfter = d
	return func() { messages.StillLoadingAfter = prev }
}

func setViewsTrouble(f func(Host) time.Time) (restore func()) {
	prev := viewsTrouble
	viewsTrouble = f
	return func() { viewsTrouble = prev }
}

func setViewsOnline(f func(Host) bool) (restore func()) {
	prev := viewsClientOnline
	viewsClientOnline = f
	return func() { viewsClientOnline = prev }
}

// bannerOf is a view's DegradedBanner as the last frame drew it: its text
// and its link's rect.
func threadsBanner(v *Threads) (string, image.Rectangle) { return v.degraded.Text, v.degraded.LinkR }
func unreadsBanner(v *Unreads) (string, image.Rectangle) { return v.degraded.Text, v.degraded.LinkR }

// threadsPagingRow draws the Threads view's paging row as a page is out.
func threadsPagingRow(v *Threads, gtx layout.Context) {
	v.paging = true
	v.footer(gtx, false)
}
