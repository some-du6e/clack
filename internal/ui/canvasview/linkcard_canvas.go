package canvasview

// EmbeddedCanvasUnfurl (client-boot-shared p/yH), the card Slack draws in a
// canvas for another canvas (a SLACK_DOCUMENT card, a /docs/T…/F… link):
// the canvas's entity row over FileQuip's preview in the 799×400 box, the
// loading state while the file or its preview isn't known, or
// NoAccessCanvas (QuipDeletedState). The file is files.info's and the
// preview the one Clack builds from the export (store CanvasPreview):
// never a canvas method, never files.open. Build 889ac449a32d; the notes
// are in /root/clack-ops/scratch/link-url-box-3/cards-spec.md §1.

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// canvasCardData is what EmbeddedCanvasUnfurl reads, and its views.
type canvasCardData struct {
	preview  []richtext.Block
	havePrev bool // the store has the preview (or knows there is none)
	key      string
	rv       *richview.View // the entity row and the preview
	// NoAccessCanvas's
	icon, emoji     string
	title, subtitle string
}

// shouldShowFile is vITP's for a canvas file as Clack has it.
func shouldShowFile(f *slack.File) bool {
	return f.ID != "" && richtext.FileStatusOf(richtext.FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.IsHiddenByLimit, Tombstoned: f.IsTombstoned}) == "normal"
}

// readCanvas reads the route's canvas: its file (the file cards' cache,
// files.info) and its preview; it reports whether to fetch the file.
func (v *View) readCanvas(c *linkCard) bool {
	if c.rt.kind != "canvas" || c.rt.fileID == "" {
		return false
	}
	rt := c.rt
	c.rt.kind = "file" // the file's own read, as the file card's
	fetch := v.readFile(c)
	c.rt = rt
	if a := v.h.Core(); a != nil && c.fc.known {
		c.cv.preview, c.cv.havePrev = a.S.CanvasPreview(c.rt.fileID)
	}
	return fetch
}

// decideCanvas is EmbeddedCanvasUnfurl's order.
func (v *View) decideCanvas(c *linkCard, loading bool, now time.Time) cardKind {
	d := &c.fc
	switch {
	case c.rt.fileID == "": // a channel's canvas: its properties.canvas.file_id isn't read yet
		return cardEmpty
	case d.code != "" || !cardFileIDRe.MatchString(c.rt.fileID):
		return cardCanvasNoAccess
	case !d.known:
		return cardFileLoading
	case !shouldShowFile(&d.f):
		return cardCanvasNoAccess
	}
	if !c.cv.havePrev { // the preview is on its way: look again shortly
		if a := v.h.Core(); a != nil {
			c.cv.preview, c.cv.havePrev = a.S.CanvasPreview(c.rt.fileID)
		}
	}
	if loading || !c.cv.havePrev {
		return cardFileLoading
	}
	return cardCanvasShown
}

// canvasViews makes the canvas card's views.
func (v *View) canvasViews(c *linkCard) {
	d := &c.cv
	f := &c.fc.f
	key := string(rune('0'+c.kind)) + "\x00" + f.ID + "\x00" + f.Title + "\x00" + f.Mode + "\x00" + c.fc.code
	if len(d.preview) > 0 {
		key += "\x00" + richtext.PlainText(richtext.Doc{Blocks: d.preview})
	}
	if key == d.key {
		return
	}
	d.key, d.rv, d.title, d.subtitle, d.icon, d.emoji = key, nil, "", "", "", ""
	para := func(text string) string { return text }
	switch c.kind {
	case cardCanvasShown:
		rf := richtext.FileOf(f)
		rf.Embedded, rf.CanvasPreview = true, d.preview
		d.rv = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: rf}}}, richview.Options{})
	case cardCanvasNoAccess: // QuipDeletedState, in its code order
		switch {
		case c.fc.code == "file_deleted" || c.fc.known && (f.IsDeleted || f.Mode == "tombstone"):
			// one of nAOp.Ft's at random in Slack; Clack keeps the first
			d.emoji = "fishing_pole_and_fish"
			d.title = para(nsFiles.T("This canvas has gone fishing"))
			d.subtitle = para(nsFiles.T("It’s been deleted by the owner."))
		case c.fc.known && (f.Mode == "access_denied" || f.FileAccess == "access_denied"):
			d.icon = "lock"
			d.title = para(nsFiles.T("Need permission to view"))
			d.subtitle = para(nsFiles.T("Another team owns this canvas. It’s only been shared with specific people in your organization."))
		case c.fc.known && (f.Mode == "not_visible" || f.FileAccess == "not_visible"):
			d.icon = "warning"
			d.title = para(nsFiles.T("You don’t have access to this canvas."))
		default:
			d.icon = "help"
			d.title = para(nsFiles.T("This canvas was not found."))
		}
	}
}

// layoutCanvas draws the canvas card w wide and returns its height.
func (v *View) layoutCanvas(gtx layout.Context, c *linkCard, w int, now time.Time) int {
	d := &c.cv
	box := max(w*400/799, gtx.Dp(235))
	switch c.kind {
	case cardCanvasShown:
		if d.rv == nil {
			return box
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
		d.rv.Layout(g, v.styles.card, v.handler)
		return box
	case cardCanvasNoAccess: // centred in the box: the 36px icon, the 18px title, the 15px subtitle 8 under it
		th := v.h.Theme()
		ink := cardInks(th.Dark)
		tx, tw := gtx.Dp(16), max(0, w-2*gtx.Dp(16))
		measure := func(s theme.TextStyle, txt string) (layout.Dimensions, op.CallOp) {
			m := op.Record(gtx.Ops)
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, gtx.Constraints.Max.Y)}
			dims := th.Layout(g, s, txt)
			return dims, m.Stop()
		}
		var tDims, sDims layout.Dimensions
		var tCall, sCall op.CallOp
		if d.title != "" {
			tDims, tCall = measure(theme.TextStyle{Size: 18, Weight: theme.Black, Color: ink.primary, LineHeight: 24, Align: text.Middle}, d.title)
		}
		if d.subtitle != "" {
			sDims, sCall = measure(theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.primary, LineHeight: 22, Align: text.Middle}, d.subtitle)
		}
		is := gtx.Dp(36)
		h := is + tDims.Size.Y
		if d.subtitle != "" {
			h += gtx.Dp(8) + sDims.Size.Y
		}
		h += gtx.Dp(16) // margin_bottom_100
		y := max(0, (box-h)/2)
		ix := (w - is) / 2
		switch {
		case d.emoji != "":
			if a := v.h.Core(); a != nil {
				if u, _, ok := a.Emoji(d.emoji); ok && u != "" {
					if img, ok := v.h.Images().Get(u, is*2); ok {
						func() {
							defer op.Offset(image.Pt(ix, y)).Push(gtx.Ops).Pop()
							theme.Image(gtx, img, image.Pt(is, is), 0)
						}()
					}
				}
			}
		case d.icon != "":
			func() {
				defer op.Offset(image.Pt(ix, y)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(is, is))
				icons.GetSlack(d.icon)(g, unit.Dp(36), ink.primary)
			}()
		}
		y += is
		place := func(dims layout.Dimensions, call op.CallOp) {
			defer op.Offset(image.Pt(tx, y)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
			y += dims.Size.Y
		}
		if d.title != "" {
			place(tDims, tCall)
		}
		if d.subtitle != "" {
			y += gtx.Dp(8)
			place(sDims, sCall)
		}
		return box
	}
	return 0
}
