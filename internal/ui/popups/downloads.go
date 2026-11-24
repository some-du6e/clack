package popups

// Downloads, as Slack desktop shows them (CDN build bv1-13, boot-shared;
// desktop 4.53.276 main.bundle.cjs):
//
//   - downloadFile (sY5W) on the desktop: startDesktopDownload with the
//     file's getFileDownloadUrl (I83v.S: url_private_download with
//     origin_team), addActiveDownload and showDownloadToast (core's
//     Downloads is the main process's half: the stream, the store).
//   - DownloadToast / DownloadToastItem (jz7l): a c-toast at the bottom
//     right holding one item per active download: DownloadFile (3CkT, the
//     inverted pillow with FileSize's "{downloaded} of {total} "),
//     DownloadProgress (AuuO) and DownloadControls (g6Tr: Pause / Resume /
//     Cancel, Canceled with Retry, View all downloads or Show in Finder).
//   - Downloads (8Hw4): the "View all downloads" modal (⌘⇧J).
//   - openDownloadEpic (main process): opening a finished download, with
//     its refusals and its "Open File" question.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsPageClient = i18n.NS("page_client") // DownloadToast, DownloadControls, Downloads
	nsMenuFile   = i18n.NS("menu_file")   // FileSize
	nsFilesize   = i18n.NS("filesize")    // convertFilesize
	nsMessagebox = i18n.NS("messagebox")  // the main process's message boxes
)

// ChooseDownloadDir, set by the ui root, is the folder a download goes
// to: asked is false when "Choose location for each download" is off (or
// there's no chooser), and the folder in the preference (DownloadDir) is
// used; on, the system's folder chooser (Slack's showOpenDialog with
// openDirectory, "Save") answers, ErrDownloadCanceled when it's closed.
// It may block: never call it on the UI goroutine.
var ChooseDownloadDir func() (dir string, asked bool, err error)

// downloadDir is a download's folder (core.DownloadRequest.Dir).
func downloadDir(context.Context) (string, error) {
	if ChooseDownloadDir != nil {
		if d, asked, err := ChooseDownloadDir(); asked {
			if errors.Is(err, ErrDownloadCanceled) {
				err = core.ErrDownloadCanceled
			}
			return d, err
		}
	}
	if DownloadDir != nil {
		if d := DownloadDir(); d != "" {
			return d, nil
		}
	}
	return core.DefaultDownloadDir()
}

// StartDownload is downloadFile on the desktop for f: core downloads it
// to the download folder, the file joins the toast's list and the toast
// shows. false when f has no download URL (Slack's "No download URL":
// nothing happens).
func StartDownload(h Host, f *richtext.File) bool {
	c := h.Core()
	if c == nil || f == nil || f.ID == "" {
		return false
	}
	src := c.FileDownloadURL(f.Download)
	if src == "" || strings.HasPrefix(src, "blob:") {
		return false
	}
	ds := c.Downloads()
	ds.Start(core.DownloadRequest{ID: f.ID, URL: src, File: *f, Dir: downloadDir})
	ds.AddActive(f.ID)
	if DownloadsChanged != nil {
		DownloadsChanged(h)
	}
	h.Invalidate()
	return true
}

// DownloadsChanged, set by the ui root, hears of a download started
// (showDownloadToast): the root draws the toast from then on.
var DownloadsChanged func(h Host)

// ---- the toast ----

// DownloadToast is Slack's download toast for one window. The ui root
// keeps one and draws it (Layout) while core's active list has ids.
type DownloadToast struct {
	items map[string]*dlToastItem
	// hovering is DownloadToast's isHovering (onMouseOver / onMouseOut on
	// the content); hideAt is onMouseOut's 3 s toastHideTimeout
	hovering bool
	hideAt   time.Time
	pending  []string // pendingActiveRemovals
	hover    gesture.Hover
	close    widget.Clickable
	closeTip TipState
	rect     image.Rectangle
	// openModal is "View all downloads" (the ui root's ⌘⇧J too)
	tag struct{}
}

// dlToastItem is DownloadToastItem's state for one file.
type dlToastItem struct {
	init              bool
	prev              core.Download
	hadPrev           bool
	file              richtext.File
	canceled          bool // the user cancelled it, or it never started
	retrying          bool
	removeAt          time.Time // activeDownloadRemoveTimeout (10 s)
	pillow            widget.Clickable
	pause, resume     widget.Clickable
	cancel, retry     widget.Clickable
	viewAll, showDir  widget.Clickable
	viewTip           TipState
	pillowHov, ctlHov bool
	url               string // the download's URL (Retry asks for it again)
}

// Rect is the toast as last drawn (window coordinates), empty when
// hidden.
func (t *DownloadToast) Rect() image.Rectangle { return t.rect }

// dlToastWidth is .p-download_toast__content's width; dlCloseSize the
// close button (c-icon_button medium).
const (
	dlToastWidth = 294
	dlCloseSize  = 36
)

// shown is one item as DownloadToastItem renders it.
type dlShown struct {
	state    core.DownloadState // "" for a file with no download (retrying)
	received int64
	total    int64
	progress float64
}

// Layout draws the toast at the bottom right of gtx (the window), 20px
// in, over everything, when the active list has ids.
func (t *DownloadToast) Layout(gtx layout.Context, h Host) layout.Dimensions {
	t.rect = image.Rectangle{}
	c := h.Core()
	if c == nil {
		return layout.Dimensions{}
	}
	ds := c.Downloads()
	ids := ds.Active()
	if t.items == nil {
		t.items = map[string]*dlToastItem{}
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	for id := range t.items {
		if !containsStr(ids, id) {
			delete(t.items, id)
		}
	}
	if len(ids) == 0 {
		t.pending, t.hovering = nil, false
		return layout.Dimensions{}
	}
	// the close button: hideToast → onClose (toastHideTimeout), then the
	// unmount removes every active download
	if t.close.Clicked(gtx) {
		for _, id := range ids {
			ds.RemoveActive(id)
		}
		t.pending = nil
		return layout.Dimensions{}
	}
	// componentDidUpdate's removal timers
	var wake time.Time
	soon := func(at time.Time) {
		if !at.IsZero() && (wake.IsZero() || at.Before(wake)) {
			wake = at
		}
	}
	anyComplete := false
	for _, id := range ids {
		it := t.items[id]
		if it == nil {
			it = &dlToastItem{}
			t.items[id] = it
		}
		cur, ok := ds.Get(id)
		if ok {
			it.file, it.url = cur.File, cur.URL
		}
		changed := !it.init || ok != it.hadPrev || cur.State != it.prev.State || cur.Received != it.prev.Received
		if changed {
			prevState := it.prev.State
			if !it.hadPrev {
				prevState = ""
			}
			if ok || prevState != core.DownloadNotStarted {
				if !ok || cur.State == core.DownloadCompleted {
					it.removeAt = now.Add(10 * time.Second)
				}
			} else if !it.canceled {
				// gone before it started: "Canceled", with Retry
				it.canceled = true
				it.removeAt = now.Add(10 * time.Second)
			}
			if ok {
				it.retrying = false
			}
			it.init, it.prev, it.hadPrev = true, cur, ok
		}
		if !it.removeAt.IsZero() && !now.Before(it.removeAt) {
			it.removeAt = time.Time{}
			if t.hovering {
				t.pending = appendUnique(t.pending, id)
			} else {
				ds.RemoveActive(id)
				it.canceled = false
			}
		}
		soon(it.removeAt)
		if ok && cur.State == core.DownloadCompleted {
			anyComplete = true
		}
	}
	if !t.hideAt.IsZero() && !now.Before(t.hideAt) {
		t.hideAt = time.Time{}
		if !t.hovering {
			for _, id := range t.pending {
				ds.RemoveActive(id)
			}
			t.pending = nil
		}
	}
	soon(t.hideAt)
	if !wake.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: wake})
	}

	th := h.Theme()
	cl := &th.C
	w := dp(gtx, dlToastWidth)
	// the items, stacked (record them to size the toast)
	var calls []op.CallOp
	var hs []int
	y := 0
	for i, id := range ids {
		it := t.items[id]
		cur, ok := ds.Get(id)
		if !ok && !it.canceled && !it.retrying {
			continue
		}
		s := dlShown{state: cur.State, received: cur.Received, total: cur.Total, progress: cur.Progress()}
		if !ok {
			s = dlShown{}
		}
		if it.canceled {
			s.state, s.progress = core.DownloadCancelled, 0
		} else if s.state == core.DownloadCancelled {
			s.state = core.DownloadProgressing
		}
		last := i == len(ids)-1
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return t.item(gtx, h, it, id, s, last, anyComplete, len(ids) == 1)
		})
		calls = append(calls, call)
		hs = append(hs, d.Size.Y)
		y += d.Size.Y
	}
	if len(calls) == 0 {
		return layout.Dimensions{}
	}
	ch := y
	closeS := dp(gtx, dlCloseSize)
	gap, padR := dp(gtx, 8), dp(gtx, 12)
	tw := w + gap + closeS + padR
	th2 := ch
	if m := dp(gtx, 8) + closeS; th2 < m {
		th2 = m
	}
	sz := gtx.Constraints.Max
	off := dp(gtx, 20)
	r := image.Rect(sz.X-off-tw, sz.Y-off-th2, sz.X-off, sz.Y-off)
	t.rect = r
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	box := image.Rectangle{Max: r.Size()}
	// .c-toast: the ring and shadow (deeper while hovered), base-inv-pry
	shadowY, blur := dp(gtx, 4), dp(gtx, 12)
	if t.hovering {
		shadowY, blur = dp(gtx, 18), dp(gtx, 48)
	}
	theme.Shadow(gtx, box.Add(image.Pt(0, shadowY)), dp(gtx, 8), blur, theme.WithAlpha(cl.PanelShadow, 0x14))
	theme.RRect(gtx, box.Inset(-border1(gtx)), dp(gtx, 8)+1, theme.WithAlpha(cl.Fg, 0x21))
	theme.RRect(gtx, box, dp(gtx, 8), cl.Tooltip)
	// the toast takes the pointer: nothing under it is hit
	area := clip.Rect(box).Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tag)
	area.Pop()
	// the content's hover (onMouseOver / onMouseOut): its area holds the
	// items, so the pointer on a pillow or a link is on the content too
	ca := clip.Rect(image.Rect(0, 0, w, ch)).Push(gtx.Ops)
	t.hover.Add(gtx.Ops)
	yy := 0
	for i, call := range calls {
		macroAt(gtx, call, image.Pt(0, yy))
		yy += hs[i]
	}
	ca.Pop()
	hov := t.hover.Update(gtx.Source)
	if hov != t.hovering {
		t.hovering = hov
		if !hov {
			t.hideAt = now.Add(3 * time.Second)
			gtx.Execute(op.InvalidateCmd{At: t.hideAt})
		}
	}
	// the close button: visible while the toast is hovered (.c-toast:hover
	// .c-toast__close-button), top-aligned 8px down
	bx := w + gap
	at(gtx, image.Pt(bx, dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		hover := t.hovering || t.close.Hovered()
		d := clickBox(gtx, &t.close, func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), toastSurfInv(th))
			}
		}, func(gtx layout.Context) layout.Dimensions {
			s := dp(gtx, dlCloseSize)
			if hover {
				col := cl.TooltipText
				at(gtx, image.Pt((s-dp(gtx, 20))/2, (s-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "close", 20, col)
				})
			}
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		if hover {
			Tooltip(gtx, th, &t.closeTip, t.close.Hovered(), d.Size, Above, nsToast.T("Close"), "")
		}
		return d
	})
	return layout.Dimensions{Size: sz}
}

var nsToast = i18n.NS("toast")

// toastSurfInv is .c-icon_button--dark:hover's surf-inv over the toast.
func toastSurfInv(th *theme.Theme) color.NRGBA {
	if th.Dark {
		return color.NRGBA{R: 0x2c, G: 0x2d, B: 0x30, A: 0xff}
	}
	return color.NRGBA{R: 0x34, G: 0x33, B: 0x34, A: 0xff}
}

// item lays out one DownloadToastItem: the pillow, the progress bar
// while it isn't complete, and the controls rows that show.
func (t *DownloadToast) item(gtx layout.Context, h Host, it *dlToastItem, id string, s dlShown, last, anyComplete, single bool) layout.Dimensions {
	th := h.Theme()
	cl := &th.C
	c := h.Core()
	ds := c.Downloads()
	w := gtx.Constraints.Max.X
	done := s.state == core.DownloadCompleted
	// clicks first
	for {
		ev, ok := it.pillow.Update(gtx)
		if !ok {
			break
		}
		if done {
			if ev.Modifiers.Contain(key.ModShift) {
				showDownload(h, id)
			} else {
				OpenDownload(h, id)
			}
		}
	}
	if it.pause.Clicked(gtx) {
		go ds.Pause(id)
	}
	if it.resume.Clicked(gtx) {
		go ds.Resume(id)
	}
	if it.cancel.Clicked(gtx) {
		it.canceled = true // onCancel: setState canceled
		go ds.Cancel(id)
	}
	if it.retry.Clicked(gtx) {
		// retryDesktopDownload: the same file from the same URL (a list's
		// export has no url_private_download of its own)
		it.removeAt = time.Time{}
		it.canceled, it.retrying = false, true
		if it.url != "" {
			ds.Start(core.DownloadRequest{ID: id, URL: it.url, File: it.file, Dir: downloadDir})
			ds.AddActive(id)
		} else {
			f := it.file
			StartDownload(h, &f)
		}
	}
	if it.viewAll.Clicked(gtx) {
		h.ShowOverlay(NewDownloadsModal(h))
	}
	if it.showDir.Clicked(gtx) {
		showDownload(h, id)
	}

	y := 0
	// DownloadFile: the inverted pillow (c-pillow_file--active_download)
	ph := dlPillowH(gtx)
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ph))
		return it.pillow.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if done {
				pointerCursor(gtx, image.Pt(w, ph))
			}
			dlPillow(gtx, th, &it.file, s, true, done && it.pillow.Hovered())
			return layout.Dimensions{Size: image.Pt(w, ph)}
		})
	})
	y += ph
	if !done {
		// DownloadProgress: 6px, #ddd under #1d9bd1, radius 3; 60px in,
		// calc(100% - 24px) wide (it runs on past the item, as Slack's),
		// 12px under it
		bw := w - dp(gtx, 24)
		r := image.Rect(dp(gtx, 60), y, dp(gtx, 60)+bw, y+dp(gtx, 6))
		dlProgressBar(gtx, r, s.progress)
		y += dp(gtx, 6) + dp(gtx, 12)
	}
	// DownloadControls: the actions row and the view-all row
	actions := t.actionButtons(it, s)
	var viewAll *dlLink
	if last && anyComplete {
		if single {
			lbl := nsPageClient.T("Open containing folder")
			if runtime.GOOS == "darwin" {
				lbl = nsPageClient.T("Show in Finder")
			}
			viewAll = &dlLink{label: lbl, c: &it.showDir}
		} else {
			viewAll = &dlLink{label: nsPageClient.T("View all downloads"), c: &it.viewAll, tip: &it.viewTip, shortcut: "Mod+Shift+J"}
		}
	}
	row := func(links []dlLink) {
		x := dp(gtx, 60)
		for i := range links {
			l := &links[i]
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				d := dlLinkLayout(gtx, th, l, cl.TooltipText, 13, 18)
				x += d.Size.X + dp(gtx, 12)
				return d
			})
		}
		y += dp(gtx, 18) + dp(gtx, 12)
	}
	switch {
	case !done && last && anyComplete:
		// both in the actions row (p-download_controls__actions)
		if viewAll != nil {
			actions = append(actions, *viewAll)
		}
		row(actions)
	case !done:
		row(actions)
	case done && last && viewAll != nil:
		// --completed.--last-item: the view-all row alone
		row([]dlLink{*viewAll})
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// actionButtons is DownloadControls' switch on the download's state.
func (t *DownloadToast) actionButtons(it *dlToastItem, s dlShown) []dlLink {
	cancel := dlLink{label: nsPageClient.T("Cancel"), c: &it.cancel}
	switch s.state {
	case core.DownloadPaused:
		return []dlLink{{label: nsPageClient.T("Resume"), c: &it.resume}, cancel}
	case core.DownloadCancelled:
		return []dlLink{{label: nsPageClient.T("Canceled")}, {label: nsPageClient.T("Retry"), c: &it.retry}}
	case core.DownloadCompleted:
		return nil
	}
	return []dlLink{{label: nsPageClient.T("Pause"), c: &it.pause}, cancel}
}

// dlLink is one p-download_controls__button: an unstyled button (c nil:
// the "Canceled" span), underlined while hovered.
type dlLink struct {
	label    string
	c        *widget.Clickable
	tip      *TipState
	shortcut string
}

func dlLinkLayout(gtx layout.Context, th *theme.Theme, l *dlLink, col color.NRGBA, size, lh float32) layout.Dimensions {
	draw := func(gtx layout.Context, hovered bool) layout.Dimensions {
		tw := textW(gtx, th, size, theme.Regular, l.label)
		h := dp(gtx, lh)
		vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, size, theme.Regular, col, l.label)
		})
		if hovered {
			fill(gtx, image.Rect(0, h-dp(gtx, 3), tw, h-dp(gtx, 2)), col)
		}
		return layout.Dimensions{Size: image.Pt(tw, h)}
	}
	if l.c == nil {
		return draw(gtx, false)
	}
	d := clickBox(gtx, l.c, nil, func(gtx layout.Context) layout.Dimensions { return draw(gtx, l.c.Hovered()) })
	if l.tip != nil {
		// ⌘⇧J under View all downloads (delay 300)
		l.tip.Delay = 300 * time.Millisecond
		Tooltip(gtx, th, l.tip, l.c.Hovered(), d.Size, Above, "", l.shortcut)
	}
	return d
}

// dlPillowH is a download pillow's height: 12px padding round the 36px
// icon, and the 2px the name's line adds.
func dlPillowH(gtx layout.Context) int { return dp(gtx, 36) + 2*dp(gtx, 12) + dp(gtx, 2) }

// dlPillow draws DownloadFile's pillow at the origin, gtx.Constraints.Max
// wide: the icon, the bold name and FileSize's bold meta ("{downloaded}
// of {total} " and the type while it downloads; the type, or on hover the
// action "Open file", once complete). inverted is the toast's
// c-pillow_file--active_download (white text on base-inv-pry, which the
// toast already is); else the modal's plain pillow.
func dlPillow(gtx layout.Context, th *theme.Theme, f *richtext.File, s dlShown, inverted, hoverDone bool) {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	pad, ic := dp(gtx, 12), dp(gtx, 36)
	fileIconTile(gtx, th, f, image.Pt(pad, pad), ic)
	name, meta := cl.Fg, cl.Muted
	if inverted {
		name, meta = th.C.TooltipText, th.C.TooltipText
	}
	tx := pad + ic + pad
	tw := maxInt(1, w-tx-pad)
	title := f.Name
	if title == "" {
		title = f.Title
	}
	at(gtx, image.Pt(tx, pad-dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = tw
		return para(gtx, th, 16, 20, theme.Bold, name, title, 1)
	})
	line := dlMeta(f, s, hoverDone)
	at(gtx, image.Pt(tx, pad+dp(gtx, 20)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = tw
		return para(gtx, th, 13, 16, theme.Bold, meta, line, 1) // isDetailTextBold
	})
	if s.state == core.DownloadCompleted && inverted {
		// c-pillow_file_meta_icon: the white check at top 34, left 32 on
		// a 10px #1d1c1d square
		x, y := dp(gtx, 32), dp(gtx, 34)
		fill(gtx, image.Rect(x+dp(gtx, 5), y+dp(gtx, 6), x+dp(gtx, 15), y+dp(gtx, 16)), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff})
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "circle-checkbox-checked", 20, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		})
	}
}

// dlMeta is FileSize for a download: "{downloadedSizeMb} of {totalSizeMb} "
// before the file's type while it's progressing or paused; once complete,
// the type, or "Open file" (getActionText "show") while the pillow is
// hovered (c-pillow_file__swap).
func dlMeta(f *richtext.File, s dlShown, hoverDone bool) string {
	kind := f.PrettyType
	if kind == "" {
		kind = strings.ToUpper(f.Type)
	}
	if kind == "Arugula" {
		kind = nsMenuFile.T("Post")
	}
	switch s.state {
	case core.DownloadProgressing, core.DownloadPaused:
		total := s.total
		if total <= 0 {
			total = f.Size
		}
		return nsMenuFile.T("{downloadedSizeMb} of {totalSizeMb} ", "downloadedSizeMb", ConvertFilesize(s.received), "totalSizeMb", ConvertFilesize(total)) + kind
	case core.DownloadCompleted:
		if hoverDone {
			return i18n.T("files", "Open file")
		}
	}
	return kind
}

// dlProgressBar is DownloadProgress in r: the #ddd track and the
// #1d9bd1 bar p of its width.
func dlProgressBar(gtx layout.Context, r image.Rectangle, p float64) {
	rad := dp(gtx, 3)
	rrect(gtx, r, rad, color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff})
	if p <= 0 {
		return
	}
	if p > 1 {
		p = 1
	}
	in := r
	in.Max.X = r.Min.X + int(math.Round(float64(r.Dx())*p))
	rrect(gtx, in, rad, color.NRGBA{R: 0x1d, G: 0x9b, B: 0xd1, A: 0xff})
}

// fileIconTile is a file's 36px type tile at p (the pillows' icon).
func fileIconTile(gtx layout.Context, th *theme.Theme, f *richtext.File, p image.Point, ic int) {
	cl := colorsOf(th)
	label := strings.ToUpper(f.Type)
	if len(label) > 4 {
		label = label[:4]
	}
	if label == "" {
		label = "FILE"
	}
	rrect(gtx, image.Rectangle{Min: p, Max: p.Add(image.Pt(ic, ic))}, dp(gtx, 8), cl.Ring)
	lc, ld := record(loose(gtx, image.Pt(ic, ic)), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 10, theme.Black, cl.Muted, label)
	})
	macroAt(gtx, lc, p.Add(image.Pt((ic-ld.Size.X)/2, (ic-ld.Size.Y)/2)))
}

// ConvertFilesize is Slack's convertFilesize (R9ib): 1000s, rounded,
// "0 bytes", "{bytes} B", "{size} kB", "{size} MB" and on.
func ConvertFilesize(n int64) string {
	if n == 0 {
		return nsFilesize.T("0 bytes")
	}
	if n < 1000 && n > -1000 {
		return nsFilesize.T("{bytes} B", "bytes", strconv.FormatInt(n, 10))
	}
	v := float64(n)
	r := -1
	for {
		v /= 1000
		r++
		if math.Abs(v) < 1000 || r >= 7 {
			break
		}
	}
	units := []string{"{size} kB", "{size} MB", "{size} GB", "{size} TB", "{size} PB", "{size} EB", "{size} ZB", "{size} YB"}
	return nsFilesize.T(units[r], "size", strconv.FormatInt(int64(math.Floor(v+0.5)), 10))
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func appendUnique(ss []string, s string) []string {
	if containsStr(ss, s) {
		return ss
	}
	return append(ss, s)
}

// ---- opening a download ----

// dlRestricted is the main process's fG: extensions it never opens
// ("Slack can't open that kind of file."); dlSafe its fB, opened without
// asking. Anything else asks first ("Open File").
var (
	dlRestricted = []string{".bat", ".bin", ".cmd", ".command", ".com", ".exe", ".js", ".msi", ".pl", ".py", ".rb", ".sh", ".vb"}
	dlSafe       = []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".heic", ".psd", ".ai", ".jfif", ".wav", ".mp3", ".ogg", ".m4a", ".mp4", ".avi", ".flv", ".webp", ".mov", ".csv", ".pdf", ".txt", ".rtf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".xlsb", ".pages", ".numbers", ".key", ".rar", ".zip", ".7z"}
)

// dlAllowed is allowedFileExts: the extensions "Always open" was ticked
// for (this session).
var (
	dlAllowedMu sync.Mutex
	dlAllowed   = map[string]bool{}
)

// OpenDownload is openDesktopDownload → openDownloadEpic for file id's
// finished download: refused for a restricted or missing extension (a
// warning box), opened by the system for the safe ones and those
// "Always open" covers, else asked first. A file that's gone says so and
// leaves the list.
func OpenDownload(h Host, id string) {
	c := h.Core()
	if c == nil {
		return
	}
	d, ok := c.Downloads().Get(id)
	if !ok || d.State != core.DownloadCompleted || d.Path == "" {
		return // "not verified as completed by main process"
	}
	ext := filepath.Ext(d.Path)
	lext := strings.ToLower(ext)
	if strings.TrimSpace(ext) == "" || ext == "." || containsStr(dlRestricted, lext) {
		dlMessageBox(h, nsMessagebox.T("Slack can't open that kind of file."))
		return
	}
	dlAllowedMu.Lock()
	allowed := dlAllowed[lext]
	dlAllowedMu.Unlock()
	if containsStr(dlSafe, lext) || allowed {
		dlOpenPath(h, id, d.Path)
		return
	}
	detail := nsMessagebox.T("This is a {fileExt} file and will open in a different application. Make sure you trust this file before opening it directly.", "fileExt", ext)
	Confirm(h, nsMessagebox.T("Open File"), detail, nsMessagebox.T("Open File"), false, nil, func() { dlOpenPath(h, id, d.Path) })
}

// dlOpenPath is shell.openPath, and the main process's answer when it
// fails because the file is gone.
func dlOpenPath(h Host, id, p string) {
	go func() {
		err := openurl.OpenPath(p)
		if err == nil {
			return
		}
		if _, serr := os.Stat(p); errors.Is(serr, os.ErrNotExist) {
			post(h, func() {
				dlMessageBox(h, nsMessagebox.T("That file no longer exists."))
				if c := h.Core(); c != nil {
					go c.Downloads().Remove(id)
				}
			})
		}
	}()
}

// showDownload is showDesktopDownload: the file in its folder
// (shell.showItemInFolder).
func showDownload(h Host, id string) {
	c := h.Core()
	if c == nil {
		return
	}
	if d, ok := c.Downloads().Get(id); ok && d.Path != "" {
		p := d.Path
		go openurl.Reveal(p)
	}
}

// dlMessageBox is the main process's warning box: the message, OK.
func dlMessageBox(h Host, msg string) {
	m := NewModal(h, ModalOpts{
		Title: AppName, EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, strings.ReplaceAll(msg, "Slack", AppName), 0)
		},
		Buttons: []*ModalButton{{Label: nsDialog.T("OK"), Kind: BtnPrimary}},
	})
	h.ShowOverlay(m)
}

// ---- the Downloads modal ----

// downloadsModal is Downloads (8Hw4): "Downloads", Clear downloads, the
// list newest first or the empty message.
type downloadsModal struct {
	h     Host
	m     *Modal
	clear widget.Clickable
	tip   TipState
	rows  map[string]*dlModalRow
	list  layout.List
}

type dlModalRow struct {
	pillow        widget.Clickable
	pause, resume widget.Clickable
	cancel, retry widget.Clickable
	canceled      bool
}

// NewDownloadsModal is openDownloadsModal.
func NewDownloadsModal(h Host) *Modal {
	dm := &downloadsModal{h: h, rows: map[string]*dlModalRow{}}
	dm.list.Axis = layout.Vertical
	downloadsModals.Add(1)
	dm.m = NewModal(h, ModalOpts{
		Title:   nsPageClient.T("Downloads"),
		Body:    dm.body,
		Over:    dm.over,
		OnClose: func() { downloadsModals.Add(-1) },
	})
	return dm.m
}

// downloadsModals counts the open Downloads modals (DownloadsShown).
var downloadsModals atomic.Int32

// DownloadsShown reports whether a Downloads modal is open: the ui root
// redraws for the downloads' progress only while it or the toast shows.
func DownloadsShown() bool { return downloadsModals.Load() > 0 }

// over draws the title's Clear downloads button (IconButton "clear", a
// tooltip on top, a 1px line 12px to its right) left of the close button.
func (dm *downloadsModal) over(gtx layout.Context) {
	c := dm.h.Core()
	if c == nil || len(c.Downloads().List()) == 0 {
		return
	}
	th := dm.h.Theme()
	cl := colorsOf(th)
	if dm.clear.Clicked(gtx) {
		// clearTeamDesktopDownloads, dismissCurrentToast
		ds := c.Downloads()
		go ds.Clear()
		for _, id := range ds.Active() {
			ds.RemoveActive(id)
		}
	}
	sz := gtx.Constraints.Max
	s := dp(gtx, 36)
	x := sz.X - dp(gtx, 28) - dp(gtx, 36) - dp(gtx, 24) - s // the close button's 36px, 24px apart
	y := (dp(gtx, 70) - s) / 2
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &dm.clear, "clear", 36, 20, cl.Muted, cl.Selected, 8)
		Tooltip(gtx, th, &dm.tip, dm.clear.Hovered(), d.Size, Above, nsPageClient.T("Clear downloads"), "")
		return d
	})
	lx := x + s + dp(gtx, 12)
	fill(gtx, image.Rect(lx, y+(s-dp(gtx, 25))/2, lx+border1(gtx), y+(s-dp(gtx, 25))/2+dp(gtx, 25)), cl.Divider)
}

// body is p-downloads_modal__content: 424px tall (shorter to fit a short
// list), the list 5px down and 13px in; empty, the centred message.
func (dm *downloadsModal) body(gtx layout.Context) layout.Dimensions {
	h := dm.h
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	c := h.Core()
	var list []core.Download
	if c != nil {
		list = c.Downloads().List()
	}
	if len(list) == 0 {
		hh := dp(gtx, 424)
		mw := minInt(w, dp(gtx, 300))
		call, d := record(loose(gtx, image.Pt(mw, hh)), func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return para(gtx, th, 18, 24, theme.Bold, cl.Fg, nsPageClient.T("Keep track of your downloads"), 0)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return paraCentered(gtx, th, 15, 22, cl.Fg, nsPageClient.T("Files you download from Slack to your computer will appear here."), mw)
				}),
			)
		})
		macroAt(gtx, call, image.Pt((w-d.Size.X)/2, (hh-d.Size.Y)/2))
		return layout.Dimensions{Size: image.Pt(w, hh)}
	}
	for id := range dm.rows {
		found := false
		for _, d := range list {
			found = found || d.ID == id
		}
		if !found {
			delete(dm.rows, id)
		}
	}
	inner := w - 2*dp(gtx, 13)
	var calls []op.CallOp
	var hs []int
	total := dp(gtx, 5)
	for _, d := range list {
		r := dm.rows[d.ID]
		if r == nil {
			r = &dlModalRow{}
			dm.rows[d.ID] = r
		}
		d := d
		call, sz := record(loose(gtx, image.Pt(inner, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return dm.row(gtx, r, d)
		})
		calls = append(calls, call)
		hs = append(hs, sz.Size.Y)
		total += sz.Size.Y
	}
	hh := dp(gtx, 424)
	if total+dp(gtx, 20) < hh {
		hh = total + dp(gtx, 20) // onListHeightChange: the list's height + 20
	}
	// DownloadsList is a virtual list: past 424px it scrolls
	lg := exact(gtx, image.Pt(w, hh))
	inset := dp(gtx, 5)
	dm.list.Layout(lg, len(calls)+1, func(gtx layout.Context, i int) layout.Dimensions {
		if i == 0 {
			return layout.Dimensions{Size: image.Pt(w, inset)} // margin-top 5
		}
		macroAt(gtx, calls[i-1], image.Pt(dp(gtx, 13), 0))
		return layout.Dimensions{Size: image.Pt(w, hs[i-1])}
	})
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

// row is DownloadsList's renderItem: DownloadFile (action "show": a
// click opens it) and, while it isn't complete, DownloadProgress and
// DownloadControls without the view-all row.
func (dm *downloadsModal) row(gtx layout.Context, r *dlModalRow, d core.Download) layout.Dimensions {
	h := dm.h
	th := h.Theme()
	cl := colorsOf(th)
	ds := h.Core().Downloads()
	w := gtx.Constraints.Max.X
	done := d.State == core.DownloadCompleted
	if r.pillow.Clicked(gtx) {
		OpenDownload(h, d.ID)
	}
	if r.pause.Clicked(gtx) {
		go ds.Pause(d.ID)
	}
	if r.resume.Clicked(gtx) {
		go ds.Resume(d.ID)
	}
	if r.cancel.Clicked(gtx) {
		ds.RemoveActive(d.ID) // onCancel: removeActiveDownload
		go ds.Cancel(d.ID)
	}
	s := dlShown{state: d.State, received: d.Received, total: d.Total, progress: d.Progress()}
	if s.state == core.DownloadCancelled {
		s.state = core.DownloadProgressing
	}
	ph := dlPillowH(gtx)
	r.pillow.Layout(exact(gtx, image.Pt(w, ph)), func(gtx layout.Context) layout.Dimensions {
		pointerCursor(gtx, image.Pt(w, ph))
		if r.pillow.Hovered() {
			rrect(gtx, image.Rect(0, 0, w, ph), dp(gtx, 8), cl.Selected)
		}
		dlPillow(gtx, th, &d.File, s, false, r.pillow.Hovered() && done)
		return layout.Dimensions{Size: image.Pt(w, ph)}
	})
	y := ph
	if done {
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
	// .p-downloads_modal__content .p-download_progress__outer: 48px in,
	// calc(100% - 48px), margin 8 0 4
	y += dp(gtx, 8)
	dlProgressBar(gtx, image.Rect(dp(gtx, 48), y, w, y+dp(gtx, 6)), s.progress)
	y += dp(gtx, 6) + dp(gtx, 4)
	var links []dlLink
	cancel := dlLink{label: nsPageClient.T("Cancel"), c: &r.cancel}
	if s.state == core.DownloadPaused {
		links = []dlLink{{label: nsPageClient.T("Resume"), c: &r.resume}, cancel}
	} else {
		links = []dlLink{{label: nsPageClient.T("Pause"), c: &r.pause}, cancel}
	}
	x := dp(gtx, 48)
	for i := range links {
		l := &links[i]
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			dd := dlLinkLayout(gtx, th, l, cl.Fg, 13, 18)
			x += dd.Size.X + dp(gtx, 12)
			return dd
		})
	}
	y += dp(gtx, 18)
	return layout.Dimensions{Size: image.Pt(w, y)}
}
