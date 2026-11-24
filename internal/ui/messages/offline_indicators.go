package messages

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The offline list states, Slack's two indicators (boot-shared, namespace
// offline_mode, build 889ac449a32d). Both take the caption their view
// gives (`caption || default`).

// InlineOfflineIndicator is 7yfC: .p-inline_offline_indicator, a 96px
// row, 13px/1.38463, its one line centred: the cloud-offline icon (1em,
// inline), a space, the bold "No connection.", a space and the caption
// ("Go online to load more messages." when ""). The message list's and a
// thread's history loader row while !isClientOnline (GSO8
// MessageListInlineLoader, G9yo), the Threads view's paging row and
// Unreads' loading rows.
func InlineOfflineIndicator(gtx layout.Context, th *theme.Theme, ink color.NRGBA, caption string) layout.Dimensions {
	if caption == "" {
		caption = offlineNS.T("Go online to load more messages.")
	}
	w, h := gtx.Constraints.Max.X, dp(gtx, 96)
	reg := textStyle{size: 13, col: ink, lineH: 18}
	bold := textStyle{size: 13, weight: theme.Bold, col: ink, lineH: 18}
	head, tail := offlineNS.T("No connection."), " "+caption
	is := dp(gtx, 13)
	sp := measure(gtx, th, reg, " ").X
	hw, tw := measure(gtx, th, bold, head).X, measure(gtx, th, reg, tail).X
	lw := is + sp + hw + tw
	x, y := (w-lw)/2, (h-dp(gtx, 18))/2
	at(gtx, x, y+inlineSvgTop(gtx, 13, 18), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("cloud-offline")(gtx, unit.Dp(13), ink)
	})
	x += is + sp
	at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, bold, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, bold, head) })
	})
	at(gtx, x+hw, y, func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, reg, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, reg, tail) })
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// LargeOfflineIndicator is z2vV: .p-large_offline_indicator (absolute,
// inset 0: the view under its header; a centred flex column) holds two
// paragraphs: __headline, 28px/1.2143 Black, the cloud-offline icon then
// " No connection"; __caption, 13px/1.38463, the caption ("Go online to
// get the latest messages." when ""). Neither sets a colour or a margin,
// so each keeps the client's p margin (0 0 1rem): flex items' margins
// don't collapse, the column is 34+16+18+16px tall. The icon is Slack
// Kit's Icon at size "inherit", inline: 1em with vertical-align -.125em,
// so 28px with its bottom 3.5px under Lato's baseline (its top 3px down
// the 34px line).
func LargeOfflineIndicator(gtx layout.Context, th *theme.Theme, ink color.NRGBA, size image.Point, caption string) {
	if caption == "" {
		caption = offlineNS.T("Go online to get the latest messages.")
	}
	head := textStyle{size: 28, weight: theme.Black, col: ink, lineH: 34}
	capt := textStyle{size: 13, col: ink, lineH: 18}
	hl := " " + offlineNS.T("No connection")
	is := dp(gtx, 28)
	hw := is + measure(gtx, th, head, hl).X
	cw := measure(gtx, th, capt, caption).X
	headH, capH, mb := dp(gtx, 34), dp(gtx, 18), dp(gtx, 16)
	y := (size.Y - (headH + mb + capH + mb)) / 2
	x := (size.X - hw) / 2
	at(gtx, x, y+(headH-is)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("cloud-offline")(gtx, unit.Dp(28), ink)
	})
	at(gtx, x+is, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, head, hl) })
	y += headH + mb
	at(gtx, (size.X-cw)/2, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, capt, caption) })
}
