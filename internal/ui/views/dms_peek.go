package views

// The rail's DMs peek: Zxh6 DmsPeekView (web client build f132f0e78468), a
// PeekView with the header "Direct messages" (ia4), DMsUnreadsToggleButton
// (boot-render Tbey) as its header button, and DmsListAll (eLDn) with
// compactDisplay and isPeekView as its content:
//
//   - The list is the DMs tab's own (the same redux state in Slack: the
//     page's instance here, so the Unreads filter is shared with the tab),
//     drawn on the card: no "Find a DM" header (the page's query doesn't
//     filter it), no bottom-padding row (compactDisplay), no VIP
//     sections, no active row and no preselected DM (isPeekView).
//   - .p-peek_card--dms: each row's rule is inset 0 -1px
//     rgba(--sk_foreground_low,.13), and the preview is one line.
//   - A row closes the peek (maybeClosePeek) and selects the DM in the DMs
//     tab (forceTabWhenNavigating: DMs).
//   - Until the first fetch is in the skeleton rows show; an empty
//     Unreads list shows DmsList's empty state with "Show all DMs".

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsIA4 = i18n.NS("ia4")

// dmsCardColors is dmsPal on the peek card (peekPalOf's tokens).
func dmsCardColors(th *theme.Theme) dmsPal {
	pp := peekPalOf(th)
	c := dmsPal{
		chrome:  pp.bg,
		tint:    pp.hover,
		rule:    flatten(pp.line, pp.bg),
		inputBg: pp.bg,
		soft:    pp.sec,
		preview: pp.sec,
		time:    flatten(pp.invSec, pp.bg), // .p-dms_channel__timestamp: theme-content-inv-sec
		white:   pp.fg,
		green:   rgb(0x2bac76),
		pillBg:  pp.baseImp,
		pillFg:  pp.contImp,
		skel:    pp.skel,
		caret:   pp.otlTer,
	}
	if th != nil {
		if g := th.C.Presence; g.A > 0 {
			c.green = g
		}
		if b := th.C.Badge; b.A > 0 && th.Dark {
			c.pillBg, c.pillFg = b, th.C.BadgeText
		}
	}
	return c
}

// PeekTitle is the card's header.
func (d *DMs) PeekTitle() string { return nsIA4.T("Direct messages") }

// PeekOpened is the card's mount: its list starts at the top and the DMs
// load if nothing has yet (DmsListAll fetches unless hasFetched).
func (d *DMs) PeekOpened(closePeek func()) {
	d.peekClose = closePeek
	d.plist.Position = layout.Position{}
	d.plist.Axis = layout.Vertical
}

// LayoutPeekButtons is DMsUnreadsToggleButton ("Unreads", the tab's
// filter).
func (d *DMs) LayoutPeekButtons(gtx C) D {
	d.frame()
	return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { // .padding_right_25
		return d.peekToggle(gtx, &d.unreads, nsMentions.T("Unreads"))
	})
}

// LayoutPeek draws the list on the card, filling the constraints.
func (d *DMs) LayoutPeek(gtx C) D {
	d.peeking = true
	defer func() { d.peeking = false }()
	d.frame()
	d.dmsColors()
	d.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, d.c.chrome)
	d.peekView = d.peekView[:0]
	for i := range d.items {
		if d.unreads.Value && !dmsUnread(&d.items[i]) {
			continue
		}
		d.peekView = append(d.peekView, dmsEntry{dm: i})
	}
	_, loaded, err := d.first.get()
	switch {
	case len(d.items) == 0 && !loaded:
		return d.dmsSkeleton(gtx, 8)
	case len(d.items) == 0 && err != nil:
		return d.notice(gtx, "Couldn’t load your direct messages", friendlyErr(err), &d.retry, nsSearch.T("Try again"))
	case len(d.peekView) == 0:
		return d.dmsEmpty(gtx)
	}
	n := len(d.peekView)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dims := d.scrollList(gtx, &d.plist, n, func(gtx C, i int) D {
		dm := &d.items[d.peekView[i].dm]
		r := d.rowState(dm.Channel)
		r.v, r.dm = d, dm
		return d.dmRow(gtx, dm)
	})
	if p := d.plist.Position; p.First+p.Count >= n-3 {
		d.loadMore()
	}
	return D{Size: dims.Size}
}

// openFromPeek is a row's click on the card: close the peek, then the DM
// in the DMs tab.
func (d *DMs) openFromPeek(id string) {
	if d.peekClose != nil {
		d.peekClose()
	}
	d.sel = id
	if d.unreads.Value {
		d.dirty = true
	}
	d.h.Navigate("dms")
	if _, ok := d.h.(Panes); !ok {
		d.h.Navigate("c/" + id)
	}
	d.h.Invalidate()
}

// PeekCount is the rows the card lists (-1 before the first load).
func (d *DMs) PeekCount() int {
	if _, ok, _ := d.first.get(); !ok && len(d.items) == 0 {
		return -1
	}
	return len(d.peekView)
}

// PeekFirst is the card's first DM ("" for none).
func (d *DMs) PeekFirst() string {
	if len(d.peekView) == 0 {
		return ""
	}
	return d.items[d.peekView[0].dm].Channel
}

// Unreads reports the Unreads filter (the tab's and the peek's).
func (d *DMs) Unreads() bool { return d.unreads.Value }
