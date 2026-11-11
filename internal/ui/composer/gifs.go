package composer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// GIFs in the draft, the way Slack's composer holds them:
//
//   - Picking a GIF (the emoji button's GIFs tab, or the + menu's GIF row)
//     doesn't send it. handleSelectGif (51aS) upserts the draft's
//     gifUnfurls[url] = {from_url, image_url, image_width, image_height,
//     image_bytes, fallback: name || "GIF", original_url,
//     original_gif_url, preview_gif_url, status: "draft"} and the
//     composer's onGifSelected (MessageInput.onBlockAttachmentCreated)
//     saves the draft.
//   - ComposerAttachments (nCJ9) draws them after the files, past a 1px
//     separator: DraftUnfurlsAttachment (2Cqq) as a 62px graphic tile
//     (.p-draft_unfurls__unfurl--gif: the image_url, object-fit cover,
//     radius 4 under an inset 1px foreground-low .13 ring) that opens the
//     media viewer, with RemoveUnfurlButton (jFx2: "Remove GIF", a 20px
//     close-filled circle at -10px/-10px, shown on hover).
//   - Sending (prepareAndSendMessage, HV5D) adds EwsX's attachment per
//     GIF to chat.postMessage: {blocks: [{type: "image", block_id: uuid,
//     image_url, alt_text: fallback, image_width, image_height,
//     image_bytes, is_animated: true, fallback, title: {plain_text "GIF",
//     emoji: true}}]}. drafts.create and drafts.update carry the same
//     attachments (53Wp, iLF4).
//
// All of it needs the GIF picker: allow_native_gif_picker (XC3J BH, the
// enterprise's or the team's pref).

// draftGif is one GIF in the draft.
type draftGif struct {
	sel popups.GifSelection

	// UI goroutine only.
	hover  gesture.Hover
	open   gesture.Click
	remove widget.Clickable
	tip    popups.TipState
}

// gifPickerOn is XC3J BH: allow_native_gif_picker.
func gifPickerOn(a *core.App) bool {
	if a == nil {
		return false
	}
	v, ok := a.TeamPref("allow_native_gif_picker")
	if !ok {
		return false
	}
	var on bool
	if json.Unmarshal(v, &on) == nil {
		return on
	}
	var s string
	return json.Unmarshal(v, &s) == nil && (s == "true" || s == "1")
}

// giphyFixedWidth is the giphy_fixed_width experiment: the picker hands
// on the full gif format instead of tinygif.
func giphyFixedWidth(a *core.App) bool { return a != nil && a.Experiment("giphy_fixed_width") == "on" }

// addGif is handleSelectGif: the GIF joins the draft (one per URL; the
// same GIF again replaces it), then the draft is saved.
func (a *attachments) addGif(sel popups.GifSelection) {
	if sel.OriginalGifURL == "" {
		return
	}
	a.mu.Lock()
	replaced := false
	for _, g := range a.gifs {
		if g.sel.OriginalGifURL == sel.OriginalGifURL {
			g.sel = sel
			replaced = true
		}
	}
	if !replaced {
		a.gifs = append(a.gifs, &draftGif{sel: sel})
	}
	a.mu.Unlock()
	a.h.Invalidate()
	a.notify()
}

// removeGif is RemoveUnfurlButton's markDraftUnfurlAsRemoved for a GIF.
func (a *attachments) removeGif(g *draftGif) {
	a.mu.Lock()
	for i, x := range a.gifs {
		if x == g {
			a.gifs = append(a.gifs[:i:i], a.gifs[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
	a.h.Invalidate()
	a.notify()
}

// gifCount is how many GIFs the draft holds.
func (a *attachments) gifCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.gifs)
}

// takeGifs empties the draft's GIFs for a send (status "sending").
func (a *attachments) takeGifs() []*draftGif {
	a.mu.Lock()
	defer a.mu.Unlock()
	g := a.gifs
	a.gifs = nil
	return g
}

// putBackGifs returns a failed send's GIFs to the draft.
func (a *attachments) putBackGifs(gs []*draftGif) {
	if len(gs) == 0 {
		return
	}
	a.mu.Lock()
	a.gifs = append(gs, a.gifs...)
	a.mu.Unlock()
	a.h.Invalidate()
	a.notify()
}

// gifAttachments is EwsX's attachments for gifs (nil for none).
func gifAttachments(gs []*draftGif, pickerOn bool) json.RawMessage {
	if len(gs) == 0 {
		return nil
	}
	title := "GIF"
	if pickerOn {
		title = i18n.T("emoji_picker", "GIF")
	}
	type block struct {
		Type        string         `json:"type"`
		BlockID     string         `json:"block_id"`
		ImageURL    string         `json:"image_url"`
		AltText     string         `json:"alt_text"`
		ImageWidth  int            `json:"image_width"`
		ImageHeight int            `json:"image_height"`
		ImageBytes  int64          `json:"image_bytes"`
		IsAnimated  bool           `json:"is_animated"`
		Fallback    string         `json:"fallback"`
		Title       map[string]any `json:"title"`
	}
	type att struct {
		Blocks []block `json:"blocks"`
	}
	out := make([]att, 0, len(gs))
	for _, g := range gs {
		fb := gifFallback(g.sel)
		out = append(out, att{Blocks: []block{{Type: "image", BlockID: uuidv4(), ImageURL: g.sel.OriginalGifURL, AltText: fb,
			ImageWidth: g.sel.Width, ImageHeight: g.sel.Height, ImageBytes: g.sel.Bytes, IsAnimated: true, Fallback: fb,
			Title: map[string]any{"type": "plain_text", "text": title, "emoji": true}}}})
	}
	b, _ := json.Marshal(out)
	return b
}

// draftGifAttachments are the GIFs' attachments for a draft save (nil:
// none).
func (a *attachments) draftGifAttachments(pickerOn bool) json.RawMessage {
	a.mu.Lock()
	gs := append([]*draftGif(nil), a.gifs...)
	a.mu.Unlock()
	return gifAttachments(gs, pickerOn)
}

// gifFallback is handleSelectGif's fallback: the GIF's name, else "GIF".
func gifFallback(sel popups.GifSelection) string {
	if sel.Name != "" {
		return sel.Name
	}
	return "GIF"
}

// gifsFromDraft reads a server draft's GIF attachments back (what
// drafts.create saved): image blocks marked is_animated.
func gifsFromDraft(raw json.RawMessage) []*draftGif {
	var atts []struct {
		Blocks []struct {
			Type        string `json:"type"`
			ImageURL    string `json:"image_url"`
			ImageWidth  int    `json:"image_width"`
			ImageHeight int    `json:"image_height"`
			ImageBytes  int64  `json:"image_bytes"`
			IsAnimated  bool   `json:"is_animated"`
			Fallback    string `json:"fallback"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &atts) != nil {
		return nil
	}
	var out []*draftGif
	for _, at := range atts {
		for _, b := range at.Blocks {
			if b.Type != "image" || !b.IsAnimated || b.ImageURL == "" {
				continue
			}
			name := b.Fallback
			if name == "GIF" {
				name = ""
			}
			out = append(out, &draftGif{sel: popups.GifSelection{OriginalGifURL: b.ImageURL, Name: name,
				Width: b.ImageWidth, Height: b.ImageHeight, Bytes: b.ImageBytes}})
		}
	}
	return out
}

// uuidv4 is c5Jz's block id.
func uuidv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// openGif is DraftUnfurlsAttachment's openDetailedPreview: the GIF in
// the media viewer (hideOpenWindowButton, hideMediaViewerHeader).
func (a *attachments) openGif(g *draftGif) {
	a.mu.Lock()
	sel := g.sel
	a.mu.Unlock()
	a.h.OpenLightbox(richtext.Image{URL: sel.OriginalGifURL, W: sel.Width, H: sel.Height, Title: gifFallback(sel), Alt: gifFallback(sel)}, core.Message{})
}

// layoutGifTiles lays out the GIF tiles from x (gallery coordinates, wrapping
// like the files) and returns where the row ends.
func (a *attachments) layoutGifTiles(gtx layout.Context, gs []*draftGif, x, y, px, gap, pad, avail int, afterFiles bool) (int, int) {
	th := a.h.Theme()
	if afterFiles {
		// .p-composer_attachments__separator (border-left 1px
		// foreground-low-solid, 62px tall, margin-left 12px) after the
		// gallery's own 12px padding; then .p-draft_unfurls' 12px padding
		sep := x + gap
		if sep+1 > avail {
			x, y = 0, y+px+gap
		} else {
			offset(gtx, pad+sep, pad+y, func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, 0, max(1, gtx.Dp(1)), px)
				paint.FillShape(gtx.Ops, th.C.Border, clip.Rect(r).Op())
				return layout.Dimensions{Size: r.Size()}
			})
			x = sep + max(1, gtx.Dp(1)) + gap
		}
	}
	for _, g := range gs {
		if g.remove.Clicked(gtx) {
			a.removeGif(g)
			continue
		}
		if x > 0 && x+px > avail {
			x, y = 0, y+px+gap
		}
		offset(gtx, pad+x, pad+y, func(gtx layout.Context) layout.Dimensions {
			a.layoutGif(gtx, th, g, px)
			return layout.Dimensions{Size: image.Pt(px, px)}
		})
		x += px + gap
	}
	return x, y
}

// layoutGif is one DraftUnfurlsAttachment GIF tile, px square.
func (a *attachments) layoutGif(gtx layout.Context, th *theme.Theme, g *draftGif, px int) {
	a.mu.Lock()
	sel := g.sel
	a.mu.Unlock()
	hovered := g.hover.Update(gtx.Source)
	rad := gtx.Dp(4)
	r := image.Rect(0, 0, px, px)
	over := gtx.Dp(10)
	d := gtx.Dp(20)
	area := clip.Rect(image.Rect(0, -over, px+over, px)).Push(gtx.Ops)
	g.hover.Add(gtx.Ops)

	// object-fit: cover, asked at the size that makes the short side px
	need := px
	if w, h := sel.Width, sel.Height; w > 0 && h > 0 {
		need = int(math.Ceil(float64(px) * float64(max(w, h)) / float64(min(w, h))))
	}
	if img, ok := a.h.Images().Get(sel.OriginalGifURL, need); ok {
		sz := img.Size()
		if sz.X > 0 && sz.Y > 0 {
			sc := max(float32(px)/float32(sz.X), float32(px)/float32(sz.Y))
			off := f32.Pt((float32(px)-float32(sz.X)*sc)/2, (float32(px)-float32(sz.Y)*sc)/2)
			st := clip.UniformRRect(r, rad).Push(gtx.Ops)
			tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sc, sc)).Offset(off)).Push(gtx.Ops)
			img.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			st.Pop()
		}
	} else {
		fillRRect(gtx, r, rad, th.C.Skeleton)
	}
	// :after, box-shadow inset 0 0 0 1px foreground-low .13
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), withAlpha(th.C.Fg, 0x21))
	for {
		e, ok := g.open.Update(gtx.Source)
		if !ok {
			break
		}
		if e.Kind == gesture.KindClick {
			a.openGif(g)
		}
	}
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		g.open.Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
	}()
	onBtn := g.remove.Hovered()
	if hovered || onBtn {
		// RemoveUnfurlButton: base-inv-pry circle, close-filled in
		// content-inv-pry (12px font: the icon's em)
		offset(gtx, px-d+over, -over, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(d, d))
			return g.remove.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				fillCircle(gtx, image.Pt(d/2, d/2), d/2, th.C.Tooltip)
				ic := gtx.Dp(12)
				offset(gtx, (d-ic)/2, (d-ic)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("close-filled")(gtx, unit.Dp(12), th.C.TooltipText)
				})
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(d, d)}
			})
		})
	}
	area.Pop()
	if onBtn {
		popups.Tooltip(gtx, th, &g.tip, true, image.Pt(px, px), popups.Above, i18n.T("live_unfurling", "Remove GIF"), "")
	}
}

// pickGif is the composer's onGifSelected: the GIF joins the draft and
// the composer takes the focus back.
func (c *Composer) pickGif(g popups.Gif) {
	c.att.addGif(g.Selected(giphyFixedWidth(c.api.a)))
	c.Focus()
}

// gifHost is the ui root's emoji and GIF picker (ui.App).
type gifHost interface {
	OpenEmojiGifPicker(anchor image.Rectangle, onPick func(name string), onGif func(popups.Gif), gifs bool)
}

// openEmojiPicker is the composer's emoji button (EmojiButton, "Add emoji
// and GIFs" with the GIF picker): picked GIFs go into the draft.
func (c *Composer) openEmojiPicker(anchor image.Rectangle) {
	onPick := func(name string) { c.InsertEmoji(name) }
	if gh, ok := c.h.(gifHost); ok && gifPickerOn(c.api.a) {
		gh.OpenEmojiGifPicker(image.Rectangle{Min: anchor.Min, Max: anchor.Min}, onPick, c.pickGif, false)
		return
	}
	c.h.OpenEmojiPicker(anchor.Min, onPick)
}

// openGifPicker is the + menu's GIF row (onOpenGifPickerClick):
// EmojiAndGIFPickerPopover on its GIFs tab at the menu's target (the +
// button), openedBy plus_menu.
func (c *Composer) openGifPicker(anchor image.Rectangle) {
	gh, ok := c.h.(gifHost)
	if !ok {
		return
	}
	gh.OpenEmojiGifPicker(anchor, func(name string) { c.InsertEmoji(name) }, c.pickGif, true)
}
