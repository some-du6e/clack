package ui

// Files dragged from the OS onto a bookmark folder tab: Slack's
// FolderTabView wraps itself in FileDragDrop (boot-shared oCSS) with
// location folder_tab, the folder's name and onDrop
// uploadFilesToChannelFolder (disabled only for a Channel Expert folder
// the viewer can't edit), from the web client (build 859cadc0e0ab, read
// from the public CDN without a cookie):
//
//   - onDrop: nothing under disable_file_uploads "disable_all", a Slack
//     Connect conversation that bans uploads, or where the viewer can't
//     post; else the folder's upload (views.FolderTab.UploadFiles).
//   - FolderTabDragOverlay: .container__VcU8d (z-index 200, inset 0,
//     base-pry at .95, folderTabDragOverlayOverlayTransition .12s: opacity
//     and the background from nothing), .info__Xh5JK (a centred column 20vh
//     down, padding 0 16) with .anim__sDAyr (171 square: .animCircle__MI841
//     in theme-surf-pry growing from .5 and fading in, and the
//     dragdrop_sprites cards the message pane's overlay has, the night
//     card from 20:00 to 08:00) and .title__boi2V "Upload to {folderName}"
//     (28px Black, 16 above, 8 under). Blocked (no posting, uploads
//     disabled, a Slack Connect ban): the circle and the blocked-files
//     picture, "Can’t upload file" or "Uploads disabled" and the
//     subtitle (15px, content-sec).

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// folderDrop is a folder tab's drop zone.
type folderDrop struct {
	ch, tabID, name string
	ft              *views.FolderTab
}

// folderDropFor is the tab's zone, one per tab so drop.hover compares.
func (a *App) folderDropFor(ch string, t chTab, ft *views.FolderTab) *folderDrop {
	if a.folderDrops == nil {
		a.folderDrops = map[string]*folderDrop{}
	}
	k := ch + "/" + t.key
	fd := a.folderDrops[k]
	if fd == nil {
		fd = &folderDrop{ch: ch, tabID: strings.TrimPrefix(t.key, "tab:")}
		a.folderDrops[k] = fd
	}
	fd.ft, fd.name = ft, ft.Name()
	return fd
}

// layoutFolderTab draws a folder tab's body with its FileDragDrop.
func (a *App) layoutFolderTab(gtx layout.Context, ch string, t chTab, origin image.Point) layout.Dimensions {
	ft := a.folderTab(ch, t)
	d := ft.Layout(gtx)
	fd := a.folderDropFor(ch, t, ft)
	sz := gtx.Constraints.Max
	if ft.DropDisabled() { // disabled: no overlay, no drop
		return d
	}
	a.drop.zones = append(a.drop.zones, dropZone{fd: fd, r: image.Rectangle{Min: origin, Max: origin.Add(sz)}})
	a.layoutFolderDropOverlay(gtx, fd, sz)
	return d
}

// folderDropBlocked is the overlay's blocked state: "" for none, else its
// title and subtitle.
func (a *App) folderDropBlocked(ch string) (title, sub string) {
	tr := i18n.NS("tabbed-channel")
	c := a.Core()
	canPost := c != nil && c.CanEditBookmarks(ch)
	switch {
	case !canPost:
		return tr.T("Can’t upload file"), tr.T("You don’t have permission to upload files to this channel.")
	case a.dropUploads() == dropUploadsNone:
		return tr.T("Uploads disabled"), tr.T("At the request of your administrator, file uploads have been disabled on this workspace.")
	}
	// the Slack Connect ban ("Your admins have disabled file upload in this
	// Slack Connect conversation.") reads the conversation's
	// can_upload_files from Slack's list prefs store, which Clack doesn't
	// keep; the drop's upload checks it (core.CheckFolderUpload)
	return "", ""
}

// dropToFolder is the folder's onDrop.
func (a *App) dropToFolder(fd *folderDrop, paths []string) {
	if len(paths) == 0 || fd.ft == nil {
		return
	}
	if t, _ := a.folderDropBlocked(fd.ch); t != "" {
		return
	}
	go func() {
		var files []core.UploadFile
		for _, p := range paths {
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			n := filepath.Base(p)
			files = append(files, core.UploadFile{Name: n, Title: n, Data: b})
		}
		fd.ft.UploadFiles(files)
	}()
}

// layoutFolderDropOverlay is FolderTabDragOverlay over the tab (size sz).
func (a *App) layoutFolderDropOverlay(gtx layout.Context, fd *folderDrop, sz image.Point) {
	if !a.drop.on || a.drop.hover != any(fd) {
		return
	}
	if a.drop.since.IsZero() {
		a.drop.since = gtx.Now
	}
	p := float64(1)
	if !a.reduceMotion() {
		p = math.Min(1, float64(gtx.Now.Sub(a.drop.since))/float64(dropOverlayAnim))
		if p < 1 {
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	th := a.th
	e := cssEase(p)
	ostack := paint.PushOpacity(gtx.Ops, float32(0.95*e)) // opacity .95, the transition from 0
	theme.Fill(gtx, image.Rectangle{Max: sz}, dropTok(th, "dt_color-base-pry"))
	title, sub := a.folderDropBlocked(fd.ch)
	kind := dropOvBlocked
	if title == "" {
		kind = dropOvUpload
		title = i18n.NS("tabbed-channel").T("Upload to folder")
		if fd.name != "" {
			title = i18n.Plain(i18n.NS("tabbed-channel").RT("Upload to {folderName}", "folderName", fd.name))
		}
	}
	top := gtx.Dp(100)
	if wh := a.WindowHeight(); wh > 0 {
		top = wh * 20 / 100 // margin 20vh 0
	}
	anim := gtx.Dp(171)
	func() {
		defer op.Offset(image.Pt((sz.X-anim)/2, top)).Push(gtx.Ops).Pop()
		a.dropAnim(gtx, kind, false, p, image.Pt(anim, anim), dropTok(th, "dt_color-theme-surf-pry"))
	}()
	y := top + anim + gtx.Dp(16)
	padX := gtx.Dp(16)
	availW := max(1, sz.X-2*padX)
	titleLH := gtx.Dp(34)
	tw := min(th.Measure(gtx, 28, theme.Black, title), availW)
	func() {
		defer op.Offset(image.Pt((sz.X-tw)/2, y)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(availW, titleLH)}
		atLine(g, titleLH, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 28, theme.Black, th.C.Fg, title)
		})
	}()
	y += titleLH + gtx.Dp(8)
	for _, ln := range a.dropSubtitleLines(gtx, sub, availW) {
		a.dropSubtitleLine(gtx, ln, image.Pt((sz.X-ln.w)/2, y), gtx.Dp(22), dropTok(th, "dt_color-content-sec"))
		y += gtx.Dp(22)
	}
	ostack.Pop()
}
