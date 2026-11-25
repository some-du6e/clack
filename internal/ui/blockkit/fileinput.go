package blockkit

// ---- file_input ----
//
// Slack's FileInputElement (n9p0; CDN build 9f450b8ad3db, with
// a11y_bk_file_input off), its upload hook (Yt0k) and checks (mgGk), as
// client-boot-styles draws them:
//
//   - The files, when there are any, in a FileGallery (nPCK):
//     .galleryContainer (base-sec, 1px otl-ter, radius 4, padding 16,
//     margin-bottom 16) around a GalleryScroller (LSo8, pageSize 250) of
//     .filesContainer, a flex row (column-gap 16, padding 1). The
//     scroller's wrapper reaches 8px above the cards (margin-top -8px,
//     padding-top 8px) for their remove buttons. Its arrows sit outside
//     it (left / right -12px, centred on it), each a plain medium
//     IconButton (36px circle, the 20px arrow-left / arrow-right in
//     content-sec; hovered, content-pry on base-ter), shown while it can
//     scroll that way; one scrolls 250px, to the end when that's under
//     50px away, over 160ms. A sideways wheel scrolls it.
//   - Each file as Slack Kit's file entity on its EntityContainer (aJif:
//     radius 12, 1px otl-ter, base-pry). An image (MIME image/*) is an
//     ImageFileEntity thumbnail (/9HR, 60x60): the picture stretched over
//     the card (thumbnailSrc: the pending upload's own pixels, then the
//     file's thumb_64, thumb_80, …), at .3 opacity under a 24px spinner
//     while it uploads, darkened to 80% under the pointer; #ddd while an
//     image with no preview uploads. Anything else is a GenericFileEntity
//     compact card (zLDk, 220x62): PASQ's row (padding 12, gap 12, items
//     centred) of the 36px radius-8 tile (FileEntityIcon's 18px white
//     glyph on its palette once uploaded; #f8f8f8 under a 24px
//     InfiniteSpinner while it uploads) and StackedLabels (mXJ3): the
//     name in body bold (15px/1.5), then pretty_type once uploaded, else
//     " ", in caption content-ter (13px/1.25), 4px apart, each one line
//     cut at any character with an ellipsis (c-truncate, break-all), the
//     name's full text as a tooltip when it's cut.
//   - The row is a flex row: its cards shrink to fit it (flex-shrink 1,
//     by their widths) down to their min-content (a compact card's
//     border, padding and tile and one character; an image's border),
//     and the scroller scrolls what doesn't fit. Every card's remove
//     button counts in its scroll width.
//   - Hovering a card shows its remove button (hdjv RemoveButton, Panda
//     units of 4px): a 20px base-inv-pry circle 8px out from its top
//     right corner with the 12px close-filled in content-inv-pry, the
//     "Remove" tooltip (sk_entities) above.
//   - The errors (FileInputErrors, SCvc), each a Kit Alert (MlDl, error,
//     boxed: surf-imp, radius 8, padding 12 16, 15px/1.46668 content-pry
//     after the 20px warning in content-imp, 12px apart), 16px apart.
//   - The Upload File button (UploadButton): Kit's outline medium
//     c-button (36px, radius 8, padding 0 12 1, min-width 80, 15px bold;
//     hovered base-sec with its shadow) of the 16px file-upload 4px
//     before "Upload File", in a Tooltip above: "The people who can edit
//     this automation will also have access to any files shared here".
//     The workspace's disable_file_uploads "disable_all" disables it
//     (c-button--disabled: base-ter, content-sec, no pointer: no tooltip)
//     and starts the errors with "File uploads to this workspace have
//     been disabled at the request of your administrator.".
//
// The button opens FileInputDialog (fileinput_dialog.go). Its files go
// through Yt0k's uploadFiles: the errors cleared, mgGk's checks again (an
// error joins the errors), then each file within the size limit uploads
// unless there were too many: a pending card until files.info returns the
// file. One that fails is dropped with "There was a problem uploading the
// file {filename}. Please try again in a few minutes." first in the
// errors. Removing a card cancels its upload, or deletes the uploaded
// file (deleteFile, files.delete); removing the last one clears the
// errors. Each upload and removal sets the input's value, files [{id}]
// (onAction), and a modal's submit waits while an upload is pending
// (isAllAsyncActionsResolved).
//
// Left out: the a11y list (VuR3) and the screen reader announcements,
// the arrows' hover scale and fades (they show or hide at once), and the
// thumbnail's 80% brightness as a filter (a 20% black veil here).

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// PickFiles opens the platform file chooser (set by the ui root); accept
// is an <input accept> list (".pdf,.png", or "image/*"), multiple allows
// several files.
var PickFiles func(accept string, multiple bool) ([]core.UploadFile, error)

// The element's numbers (dp unless noted).
const (
	fiMaxFiles  = 10          // FileInputElement's maxFiles default
	fiMaxBytes  = 100_000_000 // its maxFileSizeBytes (jI32)
	fiPageSize  = 250         // the gallery's GalleryScroller pageSize (px)
	fiSnap      = 50          // scrollByDirection's snap to an end (px)
	fiGap       = 16          // .filesContainer's column-gap; below the gallery and each alert
	fiPad       = 16          // .galleryContainer's padding
	fiCompactW  = 220
	fiCompactH  = 62
	fiThumb     = 60
	fiTile      = 36 // .thumbnailImageContainer
	fiRemove    = 20 // RemoveButton: width / height 5
	fiRemoveOut = 8  // its insets, -2
	fiArrow     = 32 // .p-gallery_scroller__arrow
	fiArrowBtn  = 36 // its IconButton, medium
	fiArrowOut  = 12 // --left / --right: -12px
	fiBtnH      = 36 // c-button--medium
)

// fiAnim is scrollWithAnimation's duration.
const fiAnim = 160 * time.Millisecond

// fiState is a file_input's files, errors and controls, on its widget
// (it outlives relayouts and a views.update that keeps the element).
type fiState struct {
	init bool
	// done are the uploaded files in the order files.info returned them,
	// pending the uploads in the order they began: Afte lists the files
	// store's first, then the pending uploads
	done, pending []*fiFile
	errs          []fiError
	next          int // the last pendingFileId

	up    tap             // the Upload File button
	upBox image.Rectangle // its box, element-local, as last drawn (tests)
	upTip popups.TipState
	gal   fiGallery
	dlg   *fiDialog // the open Add file dialog
}

// fiError is one of the element's errors (Yt0k's {id, message}).
type fiError struct{ id, msg string }

// fiFile is one of the element's files, as Afte gives it to the gallery:
// a pending upload, then the file object files.info returned.
type fiFile struct {
	pid      int    // pendingFileId while it uploads
	id       string // the file's id once uploaded
	uploaded bool
	name     string
	mime     string // the File's type while pending, the file's mimetype after
	pretty   string // pretty_type
	ftype    string // filetype
	thumb    string // thumbnailSrc once uploaded
	preview  paint.ImageOp
	hasPrev  bool // a pending image's own pixels are decoded
	cancel   context.CancelFunc

	// charW is fiWidestChar's last answer, for charKey (the labels and
	// the scale)
	charW   int
	charKey string

	box     image.Rectangle // the card, element-local, as last drawn (tests)
	hover   gesture.Hover   // the card (EntityContainer's group)
	nameHov gesture.Hover   // the name, while it's cut
	rm      tap
	rmTip   popups.TipState
	nameTip popups.TipState
}

// files is the gallery's order: the uploaded files, then the uploads.
func (fs *fiState) files() []*fiFile {
	return append(append(make([]*fiFile, 0, len(fs.done)+len(fs.pending)), fs.done...), fs.pending...)
}

func (fs *fiState) count() int { return len(fs.done) + len(fs.pending) }

func (f *fiFile) image() bool { return strings.HasPrefix(f.mime, "image/") }

// state is deriveEntityStateFromFile (bcj8): a pending image with no
// preview is loading, another pending file processing.
func (f *fiFile) state() string {
	switch {
	case f.uploaded:
		return "enabled"
	case f.image() && !f.hasPrev:
		return "loading"
	}
	return "processing"
}

// fileState is the element's file_input state, made on first use (with
// the disable_all error, as Yt0k's useState starts).
func (w *ew) fileState() *fiState {
	if w.fi == nil {
		w.fi = &fiState{}
	}
	fs := w.fi
	if !fs.init {
		fs.init = true
		if w.uploadPref() == "disable_all" {
			fs.errs = []fiError{{"disable_all_error", blockKitNS.T("File uploads to this workspace have been disabled at the request of your administrator.")}}
		}
	}
	return fs
}

// uploadPref is the workspace's disable_file_uploads team pref.
func (w *ew) uploadPref() string {
	if c := w.o.host().Core(); c != nil {
		return c.TeamPrefString("disable_file_uploads")
	}
	return ""
}

func (w *ew) maxFiles() int {
	if w.e.MaxFiles > 0 {
		return w.e.MaxFiles
	}
	return fiMaxFiles
}

// fiTok is Slack's --dt_color-<name> in the theme's mode, a custom
// theme's value when it sets one.
func fiTok(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(th.Dark)["--dt_color-"+name])
	if th.Tokens != nil {
		theme.SetColor(th.Tokens, &c, "var(--dt_color-"+name+")")
	}
	return c
}

// ---- size and layout ----

// fileHeight is the element's height at width wd: the gallery, the
// errors, the button.
func (w *ew) fileHeight(gtx C, wd int) int {
	fs := w.fileState()
	th := w.th()
	h := 0
	if fs.count() > 0 {
		h += fs.galleryHeight(gtx) + dp(gtx, fiGap)
	}
	for _, e := range fs.errs {
		h += fiAlertHeight(gtx, th, wd, e.msg) + dp(gtx, fiGap)
	}
	return h + dp(gtx, fiBtnH)
}

func (w *ew) layoutFile(gtx C, sz image.Point) {
	fs := w.fileState()
	th := w.th()
	y := 0
	if fs.count() > 0 {
		gh := fs.galleryHeight(gtx)
		at(gtx, image.Pt(0, y), func(gtx C) D { w.layoutGallery(gtx, image.Pt(sz.X, gh)); return D{} })
		y += gh + dp(gtx, fiGap)
	}
	for _, e := range fs.errs {
		msg := e.msg
		at(gtx, image.Pt(0, y), func(gtx C) D { fiAlert(gtx, th, sz.X, msg); return D{} })
		y += fiAlertHeight(gtx, th, sz.X, msg) + dp(gtx, fiGap)
	}
	var bs image.Point
	at(gtx, image.Pt(0, y), func(gtx C) D { bs = w.layoutUploadButton(gtx); return D{} })
	fs.upBox = image.Rectangle{Min: image.Pt(0, y), Max: image.Pt(bs.X, y+bs.Y)}
}

// layoutUploadButton draws UploadButton in its tooltip; it returns its
// size.
func (w *ew) layoutUploadButton(gtx C) image.Point {
	fs := w.fi
	h := w.o.host()
	th := w.th()
	off := w.uploadPref() == "disable_all" // isUploadDisabled; isDisabled is never set (ignoreClientDisabled)
	if fs.up.clicked(gtx, h) && !off && fs.dlg == nil {
		w.openFileDialog()
	}
	txt := blockKitNS.T("Upload File")
	sz := fiButtonSize(gtx, th, 16, txt)
	hov := fs.up.g.Hovered() && !off
	fg := fiKitButton(gtx, th, sz, "outline", hov, off)
	fiButtonContent(gtx, th, sz, "file-upload", 16, txt, fg)
	if off {
		return sz
	}
	fs.up.add(gtx, sz)
	popups.Tooltip(gtx, th, &fs.upTip, fs.up.g.Hovered(), sz, popups.Above,
		blockKitNS.T("The people who can edit this automation will also have access to any files shared here"), "")
	return sz
}

// fiKitButton paints a Kit c-button's box and returns its ink: outline
// (base-pry, 1px otl-sec, content-pry; hovered base-sec with
// 0 1px 3px #00000019), primary (Block Kit's green), disabled
// (c-button--disabled: base-ter, content-sec, no border).
func fiKitButton(gtx C, th *theme.Theme, sz image.Point, kind string, hovered, disabled bool) color.NRGBA {
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 8)
	switch {
	case disabled:
		rrect(gtx, r, rad, fiTok(th, "base-ter"))
		return fiTok(th, "content-sec")
	case kind == "primary":
		bg, fg, _ := buttonColors(colorsOf(th), "primary", hovered)
		rrect(gtx, r, rad, bg)
		return fg
	}
	bg := fiTok(th, "base-pry")
	if hovered {
		bg = fiTok(th, "base-sec")
		theme.Shadow(gtx, r.Add(image.Pt(0, dp(gtx, 1))), rad, dp(gtx, 2), color.NRGBA{A: 0x19})
	}
	outline(gtx, r, rad, max(1, dp(gtx, 1)), fiTok(th, "otl-sec"), bg)
	return fiTok(th, "content-pry")
}

// fiButtonSize is a medium c-button's size for a label after an icon of
// isDp (0: none): padding 12 each side, 4px between, min-width 80.
func fiButtonSize(gtx C, th *theme.Theme, isDp float32, txt string) image.Point {
	cw := measure(gtx, th, 15, theme.Bold, txt)
	if isDp > 0 {
		cw += dp(gtx, isDp) + dp(gtx, 4)
	}
	return image.Pt(max(dp(gtx, 80), cw+2*dp(gtx, 12)), dp(gtx, fiBtnH))
}

// fiButtonContent draws a button's centred content: an icon of isDp
// (none when ic is "") 4px before the label, in the box above its 1px
// bottom padding.
func fiButtonContent(gtx C, th *theme.Theme, sz image.Point, ic string, isDp float32, txt string, fg color.NRGBA) {
	tw := measure(gtx, th, 15, theme.Bold, txt)
	is := dp(gtx, isDp)
	cw := tw
	if ic != "" {
		cw += is + dp(gtx, 4)
	}
	x := (sz.X - cw) / 2
	ch := sz.Y - dp(gtx, 1)
	if ic != "" {
		at(gtx, image.Pt(x, (ch-is)/2), func(gtx C) D { return icon(gtx, ic, isDp, fg) })
		x += is + dp(gtx, 4)
	}
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		return vcenter(gtx, ch, func(gtx C) D { return label(gtx, th, 15, theme.Bold, fg, txt) })
	})
}

// ---- the alerts ----

// fiAlertText lays out an alert's message (15px/1.46668: 22px lines) at
// width w.
func fiAlertText(gtx C, th *theme.Theme, w int, col color.NRGBA, msg string) D {
	// one 22px line box per line, as Chromium lays out 15px/1.46668 (Gio's
	// own line height for a paragraph is 1.2 times what it's given, and a
	// lone line its font's height)
	lh := dp(gtx, 22)
	lines := wrapWeight(gtx, th, 15, theme.Regular, msg, max(w, 1))
	for i, ln := range lines {
		at(gtx, image.Pt(0, i*lh), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(w, 1), lh)}
			return vcenter(gtx, lh, func(gtx C) D { return th.Label(gtx, unit.Sp(15), theme.Regular, col, ln) })
		})
	}
	return D{Size: image.Pt(w, len(lines)*lh)}
}

// fiAlertWidth is the message's room in an alert w wide: the border,
// padding 16 each side, the icon and its 12px.
func fiAlertWidth(gtx C, w int) int {
	return w - 2*max(1, dp(gtx, 1)) - 2*dp(gtx, 16) - dp(gtx, 20) - dp(gtx, 12)
}

func fiAlertHeight(gtx C, th *theme.Theme, w int, msg string) int {
	m := op.Record(gtx.Ops)
	d := fiAlertText(gtx, th, fiAlertWidth(gtx, w), color.NRGBA{}, msg)
	m.Stop()
	return 2*max(1, dp(gtx, 1)) + 2*dp(gtx, 12) + max(dp(gtx, 20), d.Size.Y)
}

// fiAlert draws a Kit Alert, level error, type boxed.
func fiAlert(gtx C, th *theme.Theme, w int, msg string) {
	h := fiAlertHeight(gtx, th, w, msg)
	b := max(1, dp(gtx, 1))
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 8), fiTok(th, "surf-imp")) // its 1px border is transparent
	x, y := b+dp(gtx, 16), b+dp(gtx, 12)
	at(gtx, image.Pt(x, y+dp(gtx, 1)), func(gtx C) D { return icon(gtx, "warning", 20, fiTok(th, "content-imp")) }) // translateY(1px)
	x += dp(gtx, 20) + dp(gtx, 12)
	at(gtx, image.Pt(x, y), func(gtx C) D { return fiAlertText(gtx, th, fiAlertWidth(gtx, w), fiTok(th, "content-pry"), msg) })
}

// ---- the gallery ----

// fiGallery is the gallery's GalleryScroller: its scroll (scrollLeft,
// px), the arrows' animation and the wheel.
type fiGallery struct {
	scroll, from, to float32
	start            time.Time
	on               bool
	left, right      tap
	// obs sees the wheel over the scroller and takes none of it; seen is
	// the first event it saw of a gesture, seenAt when it saw the last
	obs    int
	seen   time.Duration
	seenAt time.Time
	// the content's scroll width and the scroller's width (px)
	contentW, viewW int
}

func (g *fiGallery) maxScroll() float32 { return float32(max(g.contentW-g.viewW, 0)) }

func (g *fiGallery) clamp() {
	g.scroll = min(max(g.scroll, 0), g.maxScroll())
	g.to = min(max(g.to, 0), g.maxScroll())
}

// scrollBy is scrollByDirection: pageSize left (dir < 0) or right,
// clamped, to an end when it would stop under 50px from it.
func (g *fiGallery) scrollBy(dir int, now time.Time) {
	mx := g.maxScroll()
	t := min(max(g.scroll+float32(dir*fiPageSize), 0), mx)
	switch {
	case mx-t < fiSnap:
		t = mx
	case t < fiSnap:
		t = 0
	}
	g.from, g.to, g.start, g.on = g.scroll, t, now, true
}

// tick advances the arrows' animation; it reports whether it still runs.
func (g *fiGallery) tick(now time.Time) bool {
	if !g.on {
		return false
	}
	t := now.Sub(g.start)
	if t >= fiAnim || now.IsZero() {
		g.scroll, g.on = g.to, false
		return false
	}
	f := float32(t) / float32(fiAnim)
	f = 1 - (1-f)*(1-f)*(1-f)
	g.scroll = g.from + (g.to-g.from)*f
	return true
}

// rowHeight is .filesContainer's: its tallest card and its padding.
func (fs *fiState) rowHeight(gtx C) int {
	h := dp(gtx, fiThumb)
	for _, f := range fs.files() {
		if !f.image() {
			h = dp(gtx, fiCompactH)
			break
		}
	}
	return h + 2*dp(gtx, 1)
}

// galleryHeight is .galleryContainer's: border, padding and the row (the
// wrapper's -8px margin and 8px padding cancel).
func (fs *fiState) galleryHeight(gtx C) int {
	b := max(1, dp(gtx, 1))
	return 2*b + 2*dp(gtx, fiPad) + fs.rowHeight(gtx)
}

// fiRow is the gallery's flex row resolved: each card's x (from the
// content's left) and width.
type fiRow struct{ x, w []int }

// layRow resolves the row's flexible lengths (CSS flexbox §9.7, a single
// line, every item flex-shrink 1 on its width) in a scroller viewW wide.
func (fs *fiState) layRow(gtx C, th *theme.Theme, viewW int) fiRow {
	files := fs.files()
	n := len(files)
	base, minW := make([]float64, n), make([]float64, n)
	b2 := float64(2 * max(1, dp(gtx, 1)))
	for i, f := range files {
		if f.image() {
			base[i], minW[i] = float64(dp(gtx, fiThumb)), b2 // the img's percentage width: no min-content
			continue
		}
		base[i] = float64(dp(gtx, fiCompactW))
		minW[i] = b2 + float64(3*dp(gtx, 12)+dp(gtx, fiTile)) + float64(fiWidestChar(gtx, th, f))
	}
	gap := dp(gtx, fiGap)
	avail := float64(viewW - 2*dp(gtx, 1) - gap*max(n-1, 0))
	size := slices.Clone(base)
	sum := 0.0
	for _, b := range base {
		sum += b
	}
	if sum > avail {
		frozen := make([]bool, n)
		for {
			used, scaled := 0.0, 0.0
			for i := range size {
				if frozen[i] {
					used += size[i]
				} else {
					used += base[i]
					scaled += base[i]
				}
			}
			free := avail - used
			if scaled == 0 || free >= 0 {
				break
			}
			viol := 0.0
			for i := range size {
				if frozen[i] {
					continue
				}
				t := base[i] + free*base[i]/scaled
				if t < minW[i] {
					viol += minW[i] - t
					t = minW[i]
				}
				size[i] = t
			}
			if viol == 0 {
				break
			}
			// a total min violation freezes the clamped items and the line
			// is resolved again
			for i := range size {
				if !frozen[i] && size[i] == minW[i] {
					frozen[i] = true
				}
			}
		}
	}
	r := fiRow{x: make([]int, n), w: make([]int, n)}
	x := float64(dp(gtx, 1))
	for i := range size {
		r.x[i] = int(math.Round(x))
		r.w[i] = int(math.Round(x+size[i])) - r.x[i]
		x += size[i] + float64(gap)
	}
	return r
}

// fiWidestChar is a compact card's labels' min-content: c-truncate breaks
// anywhere (break-all), so its widest character.
func fiWidestChar(gtx C, th *theme.Theme, f *fiFile) int {
	key := f.name + "\x00" + f.pretty + "\x00" + strconv.FormatFloat(float64(gtx.Metric.PxPerSp), 'g', -1, 32)
	if key == f.charKey {
		return f.charW
	}
	w := 0
	defer func() { f.charW, f.charKey = w, key }()
	for _, s := range []struct {
		txt  string
		size float32
		wt   font.Weight
	}{{f.name, 15, theme.Bold}, {f.pretty, 13, theme.Regular}} {
		seen := map[rune]bool{}
		for _, c := range s.txt {
			if seen[c] {
				continue
			}
			seen[c] = true
			w = max(w, measure(gtx, th, s.size, s.wt, string(c)))
		}
	}
	return w
}

// layoutGallery draws FileGallery in sz: the container, the scroller of
// cards and its arrows.
func (w *ew) layoutGallery(gtx C, sz image.Point) {
	fs := w.fi
	h := w.o.host()
	th := w.th()
	g := &fs.gal
	b := max(1, dp(gtx, 1))
	outline(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), b, fiTok(th, "otl-ter"), fiTok(th, "base-sec"))
	in := b + dp(gtx, fiPad)
	g.viewW = max(sz.X-2*in, 0)
	files := fs.files()
	row := fs.layRow(gtx, th, g.viewW)
	rowH := fs.rowHeight(gtx)
	// the scroll width: the cards, and the last remove button 8px past
	// its card (laid out at opacity 0 while it's hidden)
	g.contentW = g.viewW
	if n := len(files); n > 0 {
		g.contentW = max(g.contentW, row.x[n-1]+row.w[n-1]+dp(gtx, fiRemoveOut), row.x[n-1]+row.w[n-1]+dp(gtx, 1))
	}
	g.clamp()
	// the wrapper: 8px above the row, the row's height below it
	wrap := image.Rect(in, in-dp(gtx, fiRemoveOut), in+g.viewW, in+rowH)
	w.galleryWheel(gtx)
	if g.left.clicked(gtx, h) {
		g.scrollBy(-1, gtx.Now)
	}
	if g.right.clicked(gtx, h) {
		g.scrollBy(1, gtx.Now)
	}
	if g.tick(gtx.Now) {
		gtx.Execute(op.InvalidateCmd{})
	}
	sx := int(g.scroll + 0.5)
	// the wrapper's area holds the cards' (nested: they don't hide its
	// wheel); the arrows, its siblings in Slack's DOM, sit outside it
	cl := clip.Rect(wrap).Push(gtx.Ops)
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &g.obs)
	}()
	event.Op(gtx.Ops, g)
	for i, f := range files {
		cx := in + row.x[i] - sx
		if cx+row.w[i]+dp(gtx, fiRemoveOut) <= wrap.Min.X || cx >= wrap.Max.X {
			continue
		}
		cy := in + dp(gtx, 1)
		ch := dp(gtx, fiCompactH)
		if f.image() {
			ch = dp(gtx, fiThumb)
		}
		f.box = image.Rect(cx, cy, cx+row.w[i], cy+ch)
		at(gtx, image.Pt(cx, cy), func(gtx C) D { w.layoutFileCard(gtx, f, row.w[i]); return D{} })
	}
	cl.Pop()
	// the arrows over it, centred on the scroller (its box starts at the
	// wrapper's top)
	ay := wrap.Min.Y + (wrap.Dy()-dp(gtx, fiArrow))/2
	if g.scroll > 0.5 {
		fiArrowButton(gtx, th, &g.left, image.Pt(in-dp(gtx, fiArrowOut), ay), "arrow-left")
	}
	if g.contentW > g.viewW && g.scroll < g.maxScroll()-0.5 {
		fiArrowButton(gtx, th, &g.right, image.Pt(in+g.viewW+dp(gtx, fiArrowOut)-dp(gtx, fiArrow), ay), "arrow-right")
	}
}

// galleryWheel scrolls the gallery by a sideways wheel over its wrapper,
// a gesture that began on it (Chromium's latch: richview.WheelElsewhere);
// layoutGallery registers its targets.
func (w *ew) galleryWheel(gtx C) {
	g := &w.fi.gal
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &g.obs, Kinds: pointer.Scroll}) // no range: it takes none
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll {
			if g.seenAt.IsZero() || gtx.Now.Sub(g.seenAt) >= richview.WheelLatch {
				g.seen = e.Time
			}
			g.seenAt = gtx.Now
		}
	}
	rng := pointer.ScrollRange{Min: -int(g.scroll), Max: int(g.maxScroll() - g.scroll)}
	if elsewhere, until := richview.WheelElsewhere(gtx.Now, g.seen); elsewhere {
		rng = pointer.ScrollRange{}
		gtx.Execute(op.InvalidateCmd{At: until})
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: g, Kinds: pointer.Scroll, ScrollX: rng})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll && e.Scroll.X != 0 {
			g.on = false
			g.scroll += e.Scroll.X
			g.clamp()
		}
	}
}

// fiArrowButton draws a scroller arrow: the 32px span at p with its
// medium IconButton (36px, so 4px past the span's right and bottom).
func fiArrowButton(gtx C, th *theme.Theme, t *tap, p image.Point, ic string) {
	s := dp(gtx, fiArrowBtn)
	at(gtx, p, func(gtx C) D {
		col := fiTok(th, "content-sec")
		if t.g.Hovered() {
			col = fiTok(th, "content-pry")
			rrect(gtx, image.Rect(0, 0, s, s), s/2, fiTok(th, "base-ter"))
		}
		is := dp(gtx, 20) // font-size: .5 × 36px + 2px
		at(gtx, image.Pt((s-is)/2, (s-is)/2), func(gtx C) D { return icon(gtx, ic, 20, col) })
		defer clip.UniformRRect(image.Rect(0, 0, s, s), s/2).Push(gtx.Ops).Pop()
		t.add(gtx, image.Pt(s, s))
		return D{}
	})
}

// layoutFileCard draws one file's entity, cw wide, at the op origin, and
// its remove button while it's hovered.
func (w *ew) layoutFileCard(gtx C, f *fiFile, cw int) {
	h := w.o.host()
	th := w.th()
	if f.rm.clicked(gtx, h) {
		w.removeFile(f)
		return
	}
	hov := f.hover.Update(gtx.Source)
	ch := dp(gtx, fiCompactH)
	if f.image() {
		ch = dp(gtx, fiThumb)
	}
	// the group's hover: the card and its remove button (a descendant),
	// passing the pointer on to the name's tooltip and the scroller
	rmR := image.Rect(cw-dp(gtx, fiRemove)+dp(gtx, fiRemoveOut), -dp(gtx, fiRemoveOut), cw+dp(gtx, fiRemoveOut), dp(gtx, fiRemove)-dp(gtx, fiRemoveOut))
	for _, r := range []image.Rectangle{{Max: image.Pt(cw, ch)}, rmR} {
		c := clip.Rect(r).Push(gtx.Ops)
		p := pointer.PassOp{}.Push(gtx.Ops)
		f.hover.Add(gtx.Ops)
		p.Pop()
		c.Pop()
	}
	b := max(1, dp(gtx, 1))
	rad := dp(gtx, 12)
	outline(gtx, image.Rect(0, 0, cw, ch), rad, b, fiTok(th, "otl-ter"), fiTok(th, "base-pry"))
	inner := image.Rect(b, b, cw-b, ch-b)
	if f.image() {
		w.imageEntity(gtx, f, inner, rad-b, hov)
	} else {
		w.compactEntity(gtx, f, inner, rad-b)
	}
	if !hov && !f.rm.g.Hovered() {
		return
	}
	at(gtx, rmR.Min, func(gtx C) D {
		s := rmR.Dx()
		rrect(gtx, image.Rect(0, 0, s, s), s/2, fiTok(th, "base-inv-pry"))
		is := dp(gtx, 12) // fontSize micro
		at(gtx, image.Pt((s-is)/2, (s-is)/2), func(gtx C) D { return icon(gtx, "close-filled", 12, fiTok(th, "content-inv-pry")) })
		defer clip.UniformRRect(image.Rect(0, 0, s, s), s/2).Push(gtx.Ops).Pop()
		f.rm.add(gtx, image.Pt(s, s))
		popups.Tooltip(gtx, th, &f.rmTip, f.rm.g.Hovered(), image.Pt(s, s), popups.Above, skEntitiesNS.T("Remove"), "")
		return D{}
	})
}

// fiStateBg is a thumbnail's background by its entity state (PASQ's
// classes; the same in both themes).
func fiStateBg(state string, compact bool) color.NRGBA {
	switch state {
	case "loading":
		return color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}
	case "processing":
		if compact {
			return color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
		}
	case "error":
		return color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}
	}
	return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

// imageEntity is ImageFileEntity's thumbnail in r (radius rad): the
// state's background, the picture stretched over it while enabled or
// processing (.3 opacity then), the spinner over a pending one.
func (w *ew) imageEntity(gtx C, f *fiFile, r image.Rectangle, rad int, hov bool) {
	th := w.th()
	st := f.state()
	defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, fiStateBg(st, false), clip.Rect(r).Op())
	if st == "enabled" || st == "processing" {
		var img paint.ImageOp
		ok := false
		switch {
		case f.hasPrev:
			img, ok = f.preview, true
		case f.thumb != "" && w.o.host().Images() != nil:
			img, ok = w.o.host().Images().Get(f.thumb, r.Dx())
		}
		if ok {
			alpha := float32(1)
			if st == "processing" {
				alpha = .3
			}
			fiStretch(gtx, img, r, alpha)
			if hov { // :hover filter: brightness(80%)
				paint.FillShape(gtx.Ops, color.NRGBA{A: 0x33}, clip.Rect(r).Op())
			}
		}
	}
	if st == "processing" {
		fiSpinner(gtx, th, r)
	}
}

// fiStretch draws img stretched over r (an <img> at width and height
// 100%, no object-fit) at opacity alpha.
func fiStretch(gtx C, img paint.ImageOp, r image.Rectangle, alpha float32) {
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 || r.Empty() {
		return
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	if alpha < 1 {
		defer paint.PushOpacity(gtx.Ops, alpha).Pop()
	}
	aff := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(r.Dx())/float32(is.X), float32(r.Dy())/float32(is.Y))).Offset(layout.FPt(r.Min))
	defer op.Affine(aff).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// fiSpinner is PASQ's OverlaySpinner: a large (24px) blue
// InfiniteSpinner centred on r.
func fiSpinner(gtx C, th *theme.Theme, r image.Rectangle) {
	s := dp(gtx, 24)
	at(gtx, image.Pt(r.Min.X+(r.Dx()-s)/2, r.Min.Y+(r.Dy()-s)/2), func(gtx C) D {
		popups.InfiniteSpinner(gtx, s, false, fiTok(th, "content-ter"), fiTok(th, "content-hgl-1"))
		return D{}
	})
}

// compactEntity is GenericFileEntity's compact row in r: the tile and the
// labels, centred on the labels' two lines (taller than the tile).
func (w *ew) compactEntity(gtx C, f *fiFile, r image.Rectangle, rad int) {
	th := w.th()
	defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop() // the container's overflow: hidden
	pad := dp(gtx, 12)
	l1, gapY, l2 := 22.5*gtx.Metric.PxPerDp, 4*gtx.Metric.PxPerDp, 16.25*gtx.Metric.PxPerDp
	lh := l1 + gapY + l2
	top := float32(r.Min.Y + pad)
	rowH := max(lh, float32(dp(gtx, fiTile)))
	// the tile, centred on the row
	tile := dp(gtx, fiTile)
	ty := int(top + (rowH-float32(tile))/2 + .5)
	tr := image.Rect(r.Min.X+pad, ty, r.Min.X+pad+tile, ty+tile)
	st := f.state()
	func() {
		defer clip.UniformRRect(tr, dp(gtx, 8)).Push(gtx.Ops).Pop()
		paint.FillShape(gtx.Ops, fiStateBg(st, true), clip.Rect(tr).Op())
		if st == "enabled" { // showThumbnailForStates: [enabled]
			ic, pal := icons.FileEntityIcon(f.entityType())
			paint.FillShape(gtx.Ops, theme.PltColor(pal, icons.FileEntityShade(pal), th.Dark), clip.Rect(tr).Op())
			is := dp(gtx, 18) // .miniThumbnail
			at(gtx, image.Pt(tr.Min.X+(tile-is)/2, tr.Min.Y+(tile-is)/2), func(gtx C) D { return icon(gtx, ic, 18, fiTok(th, "constants-white")) })
		}
		if st == "processing" {
			fiSpinner(gtx, th, tr)
		}
	}()
	// the labels
	lx := tr.Max.X + pad
	lw := max(r.Max.X-pad-lx, 0)
	ly := int(top + (rowH-lh)/2 + .5)
	sec := " "
	if f.uploaded { // 7OvI: an uploaded file's pretty_type
		sec = f.pretty
	}
	cut := measure(gtx, th, 15, theme.Bold, f.name) > lw
	at(gtx, image.Pt(lx, ly), func(gtx C) D {
		nsz := image.Pt(lw, int(l1+.5))
		gtx.Constraints = layout.Constraints{Max: nsz}
		d := th.Layout(gtx, theme.TextStyle{Size: unit.Sp(15), Weight: theme.Bold, Color: fiTok(th, "content-pry"),
			LineHeight: unit.Sp(22.5), MaxLines: 1, Wrap: text.WrapGraphemes}, f.name)
		if cut { // TruncationTooltip: the whole name while it's cut
			hovN := f.nameHov.Update(gtx.Source)
			func() {
				defer clip.Rect{Max: nsz}.Push(gtx.Ops).Pop()
				defer pointer.PassOp{}.Push(gtx.Ops).Pop() // the card keeps its hover
				f.nameHov.Add(gtx.Ops)
			}()
			popups.Tooltip(gtx, th, &f.nameTip, hovN, nsz, popups.Above, f.name, "")
		}
		return d
	})
	at(gtx, image.Pt(lx, ly+int(l1+gapY+.5)), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(lw, int(l2+.5))}
		return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(13), Color: fiTok(th, "content-ter"),
			LineHeight: unit.Sp(16.25), MaxLines: 1, Wrap: text.WrapGraphemes}, sec)
	})
}

// entityType is V7l1's type for the tile: the file's filetype when D0WB
// knows it (FileEntityIcon falls back to generic), else generic.
func (f *fiFile) entityType() string {
	if f.ftype != "" {
		return f.ftype
	}
	return "generic"
}

// ---- uploading ----

// fiAccept is the chooser's accept (iSZO): image/* when the workspace
// takes only images, else the filetypes as .ext,.ext.
func fiAccept(filetypes []string, pref string) string {
	switch {
	case pref == "disable_all":
		return ""
	case pref == "disable_all_except_images":
		return "image/*"
	case len(filetypes) > 0:
		return "." + strings.Join(filetypes, ",.")
	}
	return ""
}

// fiType is a picked file's type, as Chromium gives a File.
func fiType(f core.UploadFile) string {
	if f.Type != "" {
		return f.Type
	}
	return slack.FileType(f.Name)
}

// fileChecks is FileInputDialog's check of the chosen files (O): the
// empty ones, non-images when the workspace takes only images, and those
// whose extension (lower-cased) isn't one of the filetypes are dropped,
// an error when none is left; then mgGk's limits. It returns the files to
// upload or the dialog's error ("" with no files: uploads are off).
func (w *ew) fileChecks(picked []core.UploadFile) ([]core.UploadFile, string) {
	pref := w.uploadPref()
	if pref == "disable_all" {
		return nil, ""
	}
	var keep []core.UploadFile
	for _, f := range picked {
		if len(f.Data) == 0 || pref == "disable_all_except_images" && !strings.HasPrefix(fiType(f), "image/") {
			continue
		}
		if len(w.e.FileTypes) > 0 {
			ext := f.Name[strings.LastIndexByte(f.Name, '.')+1:] // name.split(".") last
			if !slices.Contains(w.e.FileTypes, strings.ToLower(ext)) {
				continue
			}
		}
		keep = append(keep, f)
	}
	if len(keep) == 0 {
		return nil, blockKitNS.T("The file type you attempted to upload is not supported")
	}
	if e := fiLimits(keep, w.fileState().count(), w.maxFiles(), fiMaxBytes); e != nil {
		return nil, e.msg
	}
	return keep, ""
}

// fiLimits is mgGk's check: more files than maxFiles with the existing
// ones, then any over maxBytes.
func fiLimits(fs []core.UploadFile, existing, maxFiles int, maxBytes int64) *fiError {
	if len(fs)+existing > maxFiles {
		return &fiError{"too-many-files", blockKitNS.T("You have selected too many files. The limit is {number}.", "number", maxFiles)}
	}
	var big []string
	for _, f := range fs {
		if int64(len(f.Data)) > maxBytes {
			big = append(big, f.Name)
		}
	}
	if len(big) == 0 {
		return nil
	}
	return &fiError{"files-too-large", blockKitNS.T("The {numberOfTooLargeFiles, plural, =1 {file} other {files}} {numberOfTooLargeFiles, plural, =1 {{singleFileName}} other {{allFileNamesExceptLast} and {lastFileName}}} {numberOfTooLargeFiles, plural, =1 {is} other {are}} too large. Our max file size is {maxFileSize}.",
		"allFileNamesExceptLast", strings.Join(big[:len(big)-1], ", "), "singleFileName", big[0], "lastFileName", big[len(big)-1],
		"maxFileSize", fiFileSize(maxBytes), "numberOfTooLargeFiles", len(big))}
}

// fiFileSize is R9ib's convertFilesize (decimal units, rounded).
func fiFileSize(b int64) string {
	if b == 0 {
		return filesizeNS.T("0 bytes")
	}
	if b < 1000 && b > -1000 {
		return filesizeNS.T("{bytes} B", "bytes", b)
	}
	s, r := float64(b), -1
	for {
		s /= 1000
		r++
		if math.Abs(s) < 1000 || r >= 7 {
			break
		}
	}
	l := int64(math.Floor(s + .5)) // Math.round
	switch r {
	case 0:
		return filesizeNS.T("{size} kB", "size", l)
	case 1:
		return filesizeNS.T("{size} MB", "size", l)
	case 2:
		return filesizeNS.T("{size} GB", "size", l)
	case 3:
		return filesizeNS.T("{size} TB", "size", l)
	case 4:
		return filesizeNS.T("{size} PB", "size", l)
	case 5:
		return filesizeNS.T("{size} EB", "size", l)
	case 6:
		return filesizeNS.T("{size} ZB", "size", l)
	}
	return filesizeNS.T("{size} YB", "size", l)
}

// uploadFiles is Yt0k's uploadFiles.
func (w *ew) uploadFiles(picked []core.UploadFile) {
	fs := w.fileState()
	fs.errs = nil
	e := fiLimits(picked, fs.count(), w.maxFiles(), fiMaxBytes)
	var ok []core.UploadFile
	for _, f := range picked {
		if int64(len(f.Data)) <= fiMaxBytes {
			ok = append(ok, f)
		}
	}
	if e != nil {
		fs.errs = append(fs.errs, *e)
	}
	if e == nil || e.id != "too-many-files" {
		for _, f := range ok {
			w.startUpload(f)
		}
	}
	w.o.changed()
}

// startUpload uploads f (never shared: core's UploadForInput, then
// files.info) as a pending card.
func (w *ew) startUpload(f core.UploadFile) {
	fs := w.fi
	h := w.o.host()
	fs.next++
	ff := &fiFile{pid: fs.next, name: f.Name, mime: fiType(f)}
	fs.pending = append(fs.pending, ff)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	ff.cancel = cancel
	if ff.image() {
		go func() {
			img, err := fiPreview(ctx, f.Data)
			if err != nil {
				return
			}
			post(h, func() {
				ff.preview, ff.hasPrev = paint.NewImageOp(img), true
				w.o.changed()
			})
		}()
	}
	c := h.Core()
	go func() {
		var raw json.RawMessage
		err := context.Canceled
		if c != nil {
			var e error
			raw, e = c.Interactive().UploadForInput(ctx, f, nil)
			err = e
		}
		cancelled := ctx.Err() == context.Canceled
		cancel()
		post(h, func() { w.uploadDone(ff, raw, err, cancelled) })
	}()
}

// fiPreview decodes a pending image's pixels for its card (previewData's
// object URL is the file itself): 128px square, stretched, as the card
// stretches it.
func fiPreview(ctx context.Context, data []byte) (*image.RGBA, error) {
	const px = 128
	var dst *image.RGBA
	err := imgcache.WithBudget(ctx, func(bg *imgcache.Budget) error {
		img, err := imgcache.DecodeBudget(data, 80<<20, 80<<20, bg)
		if err != nil {
			return err
		}
		if err := imgcache.ChargeFunc(bg)(4 * px * px); err != nil {
			return err
		}
		dst = image.NewRGBA(image.Rect(0, 0, px, px))
		return imgcache.ScaleInto(dst, img, img.Bounds(), bg)
	})
	return dst, err
}

// uploadDone settles an upload: an uploaded file joins the done ones and
// the value; a failed one is dropped with its error first (onUploadError),
// a cancelled one quietly (it was removed).
func (w *ew) uploadDone(ff *fiFile, raw json.RawMessage, err error, cancelled bool) {
	fs := w.fi
	if w.closed() || !w.attached() {
		// the view went, or a views.update dropped the element: no value
		// for a block that's gone, no block_actions for a closed view; a
		// file that got up anyway is deleted, unless the view was submitted
		if s, ok := w.o.(*Surface); ok && err == nil && !s.submitted {
			var fo struct{ ID string }
			if json.Unmarshal(raw, &fo) == nil && fo.ID != "" {
				if c := w.o.host().Core(); c != nil {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						_ = c.DeleteFile(ctx, fo.ID)
					}()
				}
			}
		}
		return
	}
	i := slices.Index(fs.pending, ff)
	if i < 0 {
		return
	}
	fs.pending = slices.Delete(fs.pending, i, i+1)
	if err != nil {
		if !cancelled {
			fs.errs = append([]fiError{{"upload-" + ff.name, blockKitNS.T("There was a problem uploading the file {filename}. Please try again in a few minutes.", "filename", ff.name)}}, fs.errs...)
		}
		w.o.changed()
		return
	}
	var fo struct {
		ID, Name, Mimetype, Filetype string
		PrettyType                   string `json:"pretty_type"`
		Thumb64                      string `json:"thumb_64"`
		Thumb80                      string `json:"thumb_80"`
		ThumbVideo                   string `json:"thumb_video"`
		Thumb160                     string `json:"thumb_160"`
		Thumb360                     string `json:"thumb_360"`
		ThumbPDF                     string `json:"thumb_pdf"`
	}
	_ = json.Unmarshal(raw, &fo)
	if fo.ID == "" {
		fs.errs = append([]fiError{{"upload-" + ff.name, blockKitNS.T("There was a problem uploading the file {filename}. Please try again in a few minutes.", "filename", ff.name)}}, fs.errs...)
		w.o.changed()
		return
	}
	ff.uploaded, ff.id, ff.pid, ff.cancel = true, fo.ID, 0, nil
	ff.name = firstNonEmpty(fo.Name, ff.name)
	ff.mime = firstNonEmpty(fo.Mimetype, " ")
	ff.pretty = firstNonEmpty(fo.PrettyType, " ")
	ff.ftype = fo.Filetype
	ff.thumb = firstNonEmpty(fo.Thumb64, fo.Thumb80, fo.ThumbVideo, fo.Thumb160, fo.Thumb360, fo.ThumbPDF)
	ff.preview, ff.hasPrev = paint.ImageOp{}, false
	fs.done = append(fs.done, ff)
	w.syncFiles()
}

// closed: w's surface is a modal view that went (closed, popped or
// replaced).
func (w *ew) closed() bool {
	s, ok := w.o.(*Surface)
	return ok && s.closed
}

// attached: w is still one of its surface's elements (a views.update can
// drop it).
func (w *ew) attached() bool {
	s, ok := w.o.(*Surface)
	if !ok {
		return true
	}
	for _, x := range s.ws {
		if x == w {
			return true
		}
	}
	return false
}

// removeFile is Yt0k's removeFile: the last file's removal clears the
// errors; an uploaded file is deleted (removeFile's deleteFile, reason
// bk_file_input_remove), a pending one's upload cancelled.
func (w *ew) removeFile(ff *fiFile) {
	fs := w.fi
	if fs.count() == 1 {
		fs.errs = nil
	}
	if i := slices.Index(fs.pending, ff); i >= 0 {
		fs.pending = slices.Delete(fs.pending, i, i+1)
		if ff.cancel != nil {
			ff.cancel()
		}
		w.o.changed()
		return
	}
	i := slices.Index(fs.done, ff)
	if i < 0 {
		return
	}
	fs.done = slices.Delete(fs.done, i, i+1)
	if c := w.o.host().Core(); c != nil {
		id := ff.id
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = c.DeleteFile(ctx, id)
		}()
	}
	w.syncFiles()
}

// syncFiles puts the uploaded files into the input's value, files
// [{id}], and sends it as Slack's onAction does.
func (w *ew) syncFiles() {
	prev := w.snapshot()
	v := w.value()
	v.Files = v.Files[:0:0]
	for _, f := range w.fi.done {
		b, _ := json.Marshal(map[string]string{"id": f.id})
		v.Files = append(v.Files, b)
	}
	if s, ok := w.o.(*Surface); ok {
		s.clearErr(w.bid)
	}
	w.commit(prev)
}

// uploading reports whether any of the surface's file inputs has an
// upload pending: Slack's modal holds its submit until every
// blockKitAsyncAction is ready.
func (s *Surface) uploading() bool {
	for _, w := range s.ws {
		if w.fi != nil && len(w.fi.pending) > 0 {
			return true
		}
	}
	return false
}
