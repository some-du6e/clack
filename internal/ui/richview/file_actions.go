package richview

// FileActions on a message's files (boot-shared gaA3, CDN build
// 2c0d02ca130f): the group of buttons shown while a file is hovered.
//
//   - A file pillow (PillowFile, 1JzM: generic files, snippets, audio,
//     canvases) puts FileActions in .c-pillow_file__actions:
//     c-file__actions c-message_actions__group, absolute at top 12px,
//     right 12px, opacity 0 until .c-pillow_file_container:hover (z-index
//     2). The group: primary background, 0 0 0 1px foreground_low .13 and
//     0 1px 3px #00000014, radius 12, padding 4. PillowFile passes no
//     viewContext, so shouldUseMediaGallery is off there.
//   - A gallery image (FileGalleryImageFile, T4f+) passes its
//     viewContext: shouldUseMediaGallery is on in channels and threads
//     (isMediaGalleryViewContext), and the group is
//     c-file__actions--image c-file__actions--overflow--gallery_v3
//     c-message_actions--gallery_v3: top 8px, right 8px, no background,
//     no shadow, no padding.
//
// render() lays the buttons out in this order, each when its rule holds
// (MessageActionButton, 1MqE: smedium 32px unless sized; a
// c-message_actions__button is 32px tall with an 18px glyph in
// foreground_max_solid, :hover foreground_min .04 and
// primary_foreground; tooltips on top after 60ms):
//
//	renderCollapse   arrow-collapse "Collapse": the file's expandable is
//	                 expanded (an expanded snippet)
//	renderDownload   download "Download": not external, remote or a post,
//	                 fileActions.download (a hosted or snippet file); in a
//	                 gallery size small with c-message_actions__button--
//	                 gallery_v3 (28px, a 16px glyph in content-inv-sec on
//	                 base-modal with a 1px otl-inv-ter border, radius 4;
//	                 hovered content-inv-pry on base-inv-pry) and 2px after
//	renderExpand     zoom-in "Expand": a GIF or PNG with deanimate while
//	                 animations are off (animated.go)
//	renderEdit       fileActions.edit (the viewer's own snippet or post):
//	                 a snippet's edit "Edit", a post's new-window "Edit in
//	                 new window"
//	renderExternal   new-window: "Open in new window" for a snippet the
//	                 viewer can't edit (and an email, an uneditable
//	                 space), "Open file on Google Drive" / Dropbox / Box /
//	                 OneDrive / "a web page" for an external file (a
//	                 Google Drive one only when it's an image); never a
//	                 docs or CSV file
//	renderShare      share-message: "Share file…", "Share canvas", "Share
//	                 list"; never in a gallery
//	renderFavoriteCanvas  a canvas: star "Add to starred", star-filled
//	                 "Unstar canvas" (c-file__unstar_button), or with
//	                 custom_file_sections the section button
//	                 (filesection.go)
//	renderFileReferences  info "View file details" ("View canvas
//	                 details", "View list details"); never in a gallery
//	the menu         ellipsis-vertical-filled "More actions", size small
//	                 (28px wide), tooltip top-right; in a gallery with the
//	                 gallery_v3 look
//
// Left out: Ask Slackbot and the file summary (Slack AI), Preview and the
// remote-file buttons (third-party app files), Star / Open in Finder /
// the download list's buttons (not in messages), the huddle and call
// attachments' remove buttons, and the email-only Expand. The group stays
// shown while its menu is open in Slack (c-file__actions__hover); here it
// shows while the file is hovered.

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// FileAction is what a file's FileActions button (or a snippet's pill)
// does.
type FileAction uint8

const (
	FileCollapse   FileAction = iota + 1 // renderCollapse: collapse the expanded snippet
	FileDownload                         // renderDownload
	FileExpand                           // renderExpand: the media viewer (Handler.OpenFile)
	FileEdit                             // renderEdit: a snippet's Edit
	FileEditWindow                       // renderEdit: a post's Edit in new window
	FileExternal                         // renderExternal: Open in new window / Open file on …
	FileShare                            // renderShare
	FileStar                             // renderFavoriteCanvas: Add to starred / Unstar canvas
	FileSection                          // renderFavoriteButton (custom_file_sections): FileSectionOpener
	FileDetails                          // renderFileReferences: View file details
	FileMore                             // More actions: the file's menu
	FileShowMore                         // a snippet's Show more: fetchAndExpand
	FileShowLess                         // an expanded snippet's Show less
)

// FileActionHandler is optional on a Handler: the clicks on FileActions'
// buttons and a snippet's pill. btn is the button, in window coordinates
// (View coordinates plus Origin). Without it, Expand opens the file
// (Handler.OpenFile), the section button its FileSectionOpener and the
// rest do nothing.
type FileActionHandler interface {
	FileAction(a FileAction, f *richtext.File, btn image.Rectangle)
}

// SelfID, set by the ui root, is the viewer's member id: FileActions'
// fileActions.edit is the viewer's own snippet (fileOwnedByCurrentUser).
// nil: the viewer owns no file.
var SelfID func() string

// FileStarred, set by the ui root, reports whether the viewer starred
// canvas fileID (renderFavoriteCanvas' isCollection). nil: none is.
var FileStarred func(fileID string) bool

// barBtn is one button of a FileActions group.
type barBtn struct {
	act     FileAction
	w, h    int  // Dp
	gal     bool // c-message_actions__button--gallery_v3
	mr      int  // Dp after it (c-message_actions__button--gallery_v3_margin)
	icon    string
	tip     string
	right   bool // the tooltip at top-right (the menu's)
	starred bool // FileStar on a starred canvas: c-file__unstar_button
}

// fileBar is one FileActions group over a file card.
type fileBar struct {
	card  image.Rectangle // the card: the group shows while it's hovered
	f     *richtext.File
	inset int  // Dp from the card's top and right
	group bool // a c-message_actions__group (pillows); else gallery_v3's bare group
	btns  []barBtn
}

// viewerOwns is fileOwnedByCurrentUser for a message file.
func viewerOwns(f *richtext.File) bool {
	if SelfID == nil || f.User == "" {
		return false
	}
	return f.User == SelfID()
}

// isPostFile is fileIsPost (2HRR): a post, space or doc that isn't a canvas.
func isPostFile(f *richtext.File) bool {
	return f.Type != "quip" && (f.Mode == "post" || f.Mode == "space" || f.Mode == "docs")
}

// externalLabel is renderExternal's label for an external file.
func externalLabel(f *richtext.File) string {
	switch f.ExternalType {
	case "gdrive":
		return i18n.T("file", "Open file on Google Drive")
	case "dropbox":
		return i18n.T("file", "Open file on Dropbox")
	case "box":
		return i18n.T("file", "Open file on Box")
	case "onedrive":
		return i18n.T("file", "Open file on OneDrive")
	}
	return i18n.T("file", "Open file on a web page")
}

// shareLabel is getShareLabel (not the file details' title case).
func shareLabel(f *richtext.File) string {
	switch {
	case richtext.IsCanvasFile(f):
		return i18n.T("file", "Share canvas")
	case isListFile(f):
		return i18n.T("file", "Share list")
	}
	return i18n.T("file", "Share file…")
}

// detailsLabel is getViewFileDetailsText (not a template).
func detailsLabel(f *richtext.File) string {
	switch {
	case richtext.IsCanvasFile(f):
		return i18n.T("file", "View canvas details")
	case isListFile(f):
		return i18n.T("file", "View list details")
	}
	return i18n.T("file", "View file details")
}

// fileActions is FileActions' render for f: a pillow's buttons, or a
// gallery image's (gallery: shouldUseMediaGallery). noAnim: animations
// are off (renderExpand).
func fileActions(f *richtext.File, gallery, noAnim bool) []barBtn {
	var bs []barBtn
	add := func(b barBtn) {
		if b.w == 0 {
			b.w, b.h = 32, 32
		}
		bs = append(bs, b)
	}
	snippet := f.IsSnippet
	canvas := richtext.IsCanvasFile(f)
	post := isPostFile(f)
	ext := f.IsExternal && f.ExternalType != "app"
	remote := f.IsExternal && f.ExternalType == "app"
	email := f.Mode == "email"
	// fileActions.edit (KfB/): the owner's snippet or space (a space
	// anyone may edit while it isn't locked)
	edit := (f.Mode == "snippet" || f.Mode == "space") && viewerOwns(f)
	if f.Expanded && !gallery {
		add(barBtn{act: FileCollapse, icon: "arrow-collapse", tip: i18n.T("file", "Collapse")})
	}
	// fileActions.download (QjaE): a hosted or snippet file
	if !ext && !remote && !post && (f.Mode == "hosted" || f.Mode == "snippet") {
		if gallery {
			add(barBtn{act: FileDownload, w: 28, h: 28, gal: true, mr: 2, icon: "download", tip: i18n.T("file", "Download")})
		} else {
			add(barBtn{act: FileDownload, icon: "download", tip: i18n.T("file", "Download")})
		}
	}
	if t := strings.ToLower(f.Type); noAnim && f.IsImage && f.Deanimate && (t == "gif" || t == "png") {
		add(barBtn{act: FileExpand, icon: "zoom-in", tip: i18n.T("file", "Expand")})
	}
	switch {
	case edit && snippet:
		add(barBtn{act: FileEdit, icon: "edit", tip: i18n.T("file", "Edit")})
	case edit:
		add(barBtn{act: FileEditWindow, icon: "new-window", tip: i18n.T("file", "Edit in new window")})
	}
	extOK := ext && (f.ExternalType != "gdrive" || f.IsImage)
	if (extOK || email || snippet && !edit || post && !edit && f.Mode != "post") && f.Mode != "docs" && !strings.EqualFold(f.Type, "csv") {
		tip := i18n.T("file", "Open in new window")
		if ext {
			tip = externalLabel(f)
		}
		add(barBtn{act: FileExternal, icon: "new-window", tip: tip})
	}
	if !gallery {
		add(barBtn{act: FileShare, icon: "share-message", tip: shareLabel(f)})
	}
	if canvas && !gallery {
		if FileSectionFace != nil {
			if _, _, ok := FileSectionFace(f.ID); ok {
				add(barBtn{act: FileSection, w: 28, h: 32, tip: "Move file"})
			}
		}
		if n := len(bs); n == 0 || bs[n-1].act != FileSection {
			b := barBtn{act: FileStar}
			b.icon, b.tip, b.starred = starLook(f)
			add(b)
		}
	}
	if !gallery {
		add(barBtn{act: FileDetails, icon: "info", tip: detailsLabel(f)})
	}
	more := barBtn{act: FileMore, w: 28, h: 32, icon: "kebab", tip: i18n.T("file", "More actions"), right: true}
	if gallery {
		more.h, more.gal = 28, true
	}
	add(more)
	return bs
}

// starLook is renderFavoriteCanvas' button for canvas f: star "Add to
// starred", or star-filled "Unstar canvas" on c-file__unstar_button once
// starred (read as it draws: a click changes it).
func starLook(f *richtext.File) (icon, tip string, starred bool) {
	if FileStarred != nil && FileStarred(f.ID) {
		return "star-filled", i18n.T("file", "Unstar canvas"), true
	}
	return "star", i18n.T("file", "Add to starred"), false
}

// pillowBar adds file pillow r's FileActions (not in a hover card, a
// canvas's own doc, or for an upload still going).
func (l *lay) pillowBar(f *richtext.File, r image.Rectangle) {
	if f == nil || l.opts.NoHoverCards || l.opts.Canvas || f.Pending {
		return
	}
	l.d.bars = append(l.d.bars, fileBar{card: r, f: f, inset: 12, group: true, btns: fileActions(f, false, l.noAnim)})
}

// galleryBar adds gallery image tile r's FileActions.
func (l *lay) galleryBar(f *richtext.File, r image.Rectangle) {
	if f == nil || l.opts.NoHoverCards || l.opts.Canvas || f.Pending || f.IsVideo {
		return
	}
	l.d.bars = append(l.d.bars, fileBar{card: r, f: f, inset: 8, btns: fileActions(f, true, l.noAnim)})
}

// barRects is bar i's group and its buttons' boxes.
func (v *View) barRects(i int) (group image.Rectangle, btns []image.Rectangle) {
	b := &v.d.bars[i]
	pad := 0
	if b.group {
		pad = v.px(4)
	}
	w, h := 0, 0
	for _, bt := range b.btns {
		w += v.px(bt.w) + v.px(bt.mr)
		h = max(h, v.px(bt.h))
	}
	in := v.px(b.inset)
	group = image.Rect(b.card.Max.X-in-w-2*pad, b.card.Min.Y+in, b.card.Max.X-in, b.card.Min.Y+in+h+2*pad)
	x := group.Min.X + pad
	btns = make([]image.Rectangle, len(b.btns))
	for k, bt := range b.btns {
		btns[k] = image.Rect(x, group.Min.Y+pad, x+v.px(bt.w), group.Min.Y+pad+v.px(bt.h))
		x += v.px(bt.w) + v.px(bt.mr)
	}
	return group, btns
}

// barShown reports whether bar i shows: its card is hovered.
func (v *View) barShown(i int) bool {
	return v.hovering && image.Pt(int(v.ptr.X), int(v.ptr.Y)).In(v.d.bars[i].card)
}

// barAt is the shown bar button under p: its bar and index, ok false
// for none. Later bars are on top.
func (v *View) barAt(p f32.Point) (bar, btn int, ok bool) {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := len(v.d.bars) - 1; i >= 0; i-- {
		if !pt.In(v.d.bars[i].card) || !v.hovering {
			continue
		}
		g, bs := v.barRects(i)
		if !pt.In(g) {
			continue
		}
		for k, r := range bs {
			if pt.In(r) {
				return i, k, true
			}
		}
		return i, -1, true // on the group, between buttons
	}
	return -1, -1, false
}

// barKey is a bar button as one comparable int (0: none).
func barKey(bar, btn int) int {
	if bar < 0 || btn < 0 {
		return 0
	}
	return bar<<8 | (btn + 1)
}

// barAction runs bar button (bar, btn)'s action.
func (v *View) barAction(bar, btn int, h Handler) {
	if h == nil || bar < 0 || bar >= len(v.d.bars) || btn < 0 || btn >= len(v.d.bars[bar].btns) {
		return
	}
	b := &v.d.bars[bar]
	_, rs := v.barRects(bar)
	r := rs[btn].Add(v.Origin)
	switch act := b.btns[btn].act; act {
	case FileExpand:
		h.OpenFile(b.f) // FileActions' onExpand: openViewer
	case FileSection:
		if o, ok := h.(FileSectionOpener); ok {
			o.OpenFileSection(b.f, r)
		}
	default:
		if fh, ok := h.(FileActionHandler); ok {
			fh.FileAction(act, b.f, r)
		}
	}
}

// The gallery_v3 button's colours (kit.css: base-modal, otl-inv-ter,
// content-inv-sec; :hover base-inv-pry, content-inv-pry).
func galleryBtnColors(dark bool) (bg, border, icon, hoverBg, hoverIcon color.NRGBA) {
	if dark {
		return rgba(16, 18, 20, 0.6), rgba(121, 124, 129, 0.3), rgb(185, 186, 189), rgb(33, 36, 40), rgb(248, 248, 248)
	}
	return rgba(29, 28, 29, 0.7), rgba(248, 248, 248, 0.2), rgb(203, 204, 207), rgb(29, 28, 29), rgb(255, 255, 255)
}

// actionBtnColors are c-message_actions__button's: the glyph in
// sk_foreground_max_solid, and hovered sk_primary_foreground over
// sk_foreground_min .04 (the message toolbar's, messages/style.go).
func actionBtnColors(dark bool) (icon, hoverIcon, hoverBg color.NRGBA) {
	if dark {
		return rgb(171, 171, 173), rgb(209, 210, 211), rgba(232, 232, 232, 0.04)
	}
	return rgb(97, 96, 97), rgb(29, 28, 29), rgba(29, 28, 29, 0.04)
}

// groupCard paints a c-message_actions__group card in r.
func (v *View) groupCard(gtx layout.Context, p *Palette, r image.Rectangle) {
	bg, ring := rgb(255, 255, 255), rgba(29, 28, 29, 0.13)
	if p.Dark {
		bg, ring = rgb(26, 29, 33), rgba(232, 232, 232, 0.13)
	}
	rad := v.px(12)
	paint.FillShape(gtx.Ops, rgba(0, 0, 0, 0x14/255.0), clip.UniformRRect(r.Add(image.Pt(0, v.px(1))).Inset(-v.px(1)), rad).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, ring, clip.UniformRRect(r.Inset(-max(1, v.px(1))), rad+v.px(1)).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, bg, clip.UniformRRect(r, rad).Op(gtx.Ops))
}

// drawFileBars draws the hovered files' FileActions and, while the
// pointer rests on a button, its tooltip.
func (v *View) drawFileBars(gtx layout.Context, st *Style) {
	if !v.hovering || len(v.d.bars) == 0 {
		return
	}
	p := &st.Palette
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	for i := range v.d.bars {
		if !v.barShown(i) {
			continue
		}
		b := &v.d.bars[i]
		group, rs := v.barRects(i)
		icon, hoverIcon, hoverBg := actionBtnColors(p.Dark)
		if b.group {
			v.groupCard(gtx, p, group)
			// the group's card is opaque: the hover fill as the browser
			// blends it
			bg := rgb(255, 255, 255)
			if p.Dark {
				bg = rgb(26, 29, 33)
			}
			hoverBg = Over(hoverBg, bg)
		}
		tipAt := -1
		for k := range b.btns {
			bt := &b.btns[k]
			if bt.act == FileStar {
				bt.icon, bt.tip, bt.starred = starLook(b.f)
			}
			r := rs[k]
			hot := pt.In(r)
			if hot {
				tipAt = k
			}
			switch {
			case bt.act == FileSection:
				v.drawSectionBtn(gtx, st, b.f, r, hot)
			case bt.gal:
				gbg, gb, gi, ghb, ghi := galleryBtnColors(p.Dark)
				col := gi
				if hot {
					gbg, col = ghb, ghi
				}
				rad := v.px(4)
				paint.FillShape(gtx.Ops, gbg, clip.UniformRRect(r, rad).Op(gtx.Ops))
				strokeRRect(gtx.Ops, r, rad, max(1, v.px(1)), gb)
				v.barIcon(gtx, bt.icon, r, 16, col)
			case bt.starred:
				// c-file__unstar_button: plt-sunflower-0 (-5 hovered) under
				// the plt-sunflower-70 star
				c := secSun0
				if hot {
					c = secSun5
				}
				paint.FillShape(gtx.Ops, c, clip.UniformRRect(r, v.px(8)).Op(gtx.Ops))
				v.barIcon(gtx, bt.icon, r, 18, secSun70)
			default:
				col := icon
				if hot {
					paint.FillShape(gtx.Ops, hoverBg, clip.UniformRRect(r, v.px(8)).Op(gtx.Ops))
					col = hoverIcon
				}
				v.barIcon(gtx, bt.icon, r, 18, col)
			}
		}
		if tipAt >= 0 {
			bt := b.btns[tipAt]
			v.btnTip(gtx, st, rs[tipAt], bt.tip, bt.right, msgActionTipDelay)
		}
	}
}

// barIcon draws glyph name, s Dp, centred in r.
func (v *View) barIcon(gtx layout.Context, name string, r image.Rectangle, s int, col color.NRGBA) {
	ip := v.px(s)
	ir := image.Rectangle{Min: r.Min.Add(image.Pt((r.Dx()-ip)/2, (r.Dy()-ip)/2))}
	ir.Max = ir.Min.Add(image.Pt(ip, ip))
	v.secIcon(gtx, name, ir, col)
}

// FileActionRect is a shown FileActions button, for tests and owners.
type FileActionRect struct {
	File   *richtext.File
	Action FileAction
	R      image.Rectangle
}

// FileActionRects lists the buttons of the FileActions groups that show
// now (the pointer's file), in View coordinates.
func (v *View) FileActionRects() []FileActionRect {
	var out []FileActionRect
	for i := range v.d.bars {
		if !v.barShown(i) {
			continue
		}
		_, rs := v.barRects(i)
		for k, bt := range v.d.bars[i].btns {
			out = append(out, FileActionRect{File: v.d.bars[i].f, Action: bt.act, R: rs[k]})
		}
	}
	return out
}
