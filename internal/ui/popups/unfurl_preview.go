package popups

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/hovercard"
)

// A link preview's context menu and hover card, from Slack's web client
// (build a84662d2d135, gantry-v2-async-gantry-v2-shared-boot-async:
// MessageAttachmentDefaultV3's renderContextMenu, ContextMenu v+x+,
// HoverCardTrigger 9X4J, HoverCard vPpv; docs/EMBEDS.md).

// AttachmentMenu is what NewAttachmentMenu needs about the card.
type AttachmentMenu struct {
	// CanRemove is showDelete: the "Remove preview" item (and the
	// separator under it). Channel, TS and ID say what it removes.
	CanRemove       bool
	Channel, TS, ID string
	// CopyURL is "Copy link"'s text: title_link || from_url.
	CopyURL string
}

// NewAttachmentMenu is renderContextMenu, opened by a right-click on a v3
// card: ContextMenu at the pointer (position "right-bottom" on a
// zero-size target, so the menu's top-left corner is the pointer;
// measured live: 200 wide) with "Remove preview" (the same dialog as the
// remove button: BaseDeleteMessageAttachmentDialog), a separator, and
// "Copy link", which copies the link and toasts "Link copied to
// clipboard". Slack lists Copy link whenever the clipboard can be
// written (tT.tD canWriteText), so every card has it.
func NewAttachmentMenu(h Host, at image.Point, m AttachmentMenu) *Menu {
	var items []MenuItem
	if m.CanRemove {
		ch, ts, id := m.Channel, m.TS, m.ID
		items = append(items, MenuItem{Label: "Remove preview", Do: func() { ConfirmRemovePreview(h, ch, ts, id) }}.In(nsAttachments), Sep())
	}
	u := m.CopyURL
	items = append(items, MenuItem{Label: "Copy link", DoCtx: func(gtx layout.Context) {
		copyText(gtx, u)
		h.Toast(nsAttachments.T("Link copied to clipboard"))
	}}.In(nsAttachments))
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(at), Width: 200})
}

// PreviewContent is a link preview hover card's content
// (richview.Preview): laid out at the origin, 340dp wide.
type PreviewContent interface {
	Layout(gtx layout.Context) layout.Dimensions
	SetOrigin(o image.Point)
}

// PreviewCard is HoverCard around MessageAttachmentDefaultV3HoverCard: a
// 340px card (elevation_3, sk_primary_background, rounded-lg: radius 8,
// a 1px ring and a 0 4px 12px shadow, measured live) centered over the
// trigger 12px above it (HoverCardTrigger's position "top", offsetY
// -12; hovercard.PlaceTop flips it below when there's no room). The ui
// root draws it while hovercard keeps it open.
type PreviewCard struct {
	h      Host
	anchor image.Rectangle
	c      PreviewContent
	rect   image.Rectangle
}

// NewPreviewCard is the hover card for the trigger at anchor (window
// coordinates).
func NewPreviewCard(h Host, anchor image.Rectangle, c PreviewContent) *PreviewCard {
	return &PreviewCard{h: h, anchor: anchor, c: c}
}

// Layout draws the card in the window (gtx at the window origin).
func (p *PreviewCard) Layout(gtx layout.Context) layout.Dimensions {
	cl := colorsOf(p.h.Theme())
	win := gtx.Constraints.Max
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(win.X, 1<<20)}
	d := p.c.Layout(g)
	call := m.Stop()
	pos := hovercard.PlaceTop(p.anchor, d.Size, win, dp(gtx, hovercard.OffsetY), dp(gtx, hovercard.AllowanceX), dp(gtx, hovercard.AllowanceY))
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(d.Size)}
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	cs := clipRRect(gtx, p.rect, rad)
	t := op.Offset(pos).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	cs.Pop()
	p.c.SetOrigin(pos)
	return layout.Dimensions{Size: win}
}

// Rect is the drawn card (window coordinates).
func (p *PreviewCard) Rect() image.Rectangle { return p.rect }
