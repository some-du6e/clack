package views

// maybeOpenShareFileAsFolderItemConfirmationModal (boot-shared ien3 V)
// and ShareFileAsChannelFolderItemConfirmationModal, for a file the
// unified file picker (or Create New) puts into a folder; see
// file_picker.go.

import (
	"context"
	"image"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ShareFileIntoFolder adds f to folder in ch: at once when Slack would
// (core.FolderShareDirect), else after "Share this file?". done runs on
// the UI goroutine once the file is in (added) or the prompt is
// cancelled.
func ShareFileIntoFolder(h Host, ch, folder string, f core.File, done func(added bool)) {
	c := h.Core()
	ph, ok := h.(popups.Host)
	if c == nil || !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ff, err := c.FolderFileInfo(ctx, f)
		if err != nil {
			postTo(h, func() { done(false) })
			return
		}
		if c.FolderShareDirect(ch, ff) {
			_, err := c.AddFolderFile(ctx, ch, folder, ff, "")
			postTo(h, func() {
				if err != nil {
					ph.ShowOverlay(newFolderFileErrorModal(ph, ff))
				}
				done(err == nil)
			})
			return
		}
		postTo(h, func() { ph.ShowOverlay(newShareIntoFolderModal(h, ph, ch, folder, ff, done)) })
	}()
}

// folderFileTitle is ak4d's title for a file that failed to go in.
func folderFileTitle(f core.File) string {
	switch {
	case f.IsList:
		return nsTabbed.T("We couldn’t add the list to the folder")
	case f.IsCanvas:
		return nsTabbed.T("We couldn’t add the canvas to the folder")
	}
	return nsTabbed.T("We couldn’t add the file to the folder")
}

func newFolderFileErrorModal(ph popups.Host, f core.File) *popups.Modal {
	return popups.NewModal(ph, popups.ModalOpts{Title: folderFileTitle(f), Body: func(gtx C) D { return D{} },
		Buttons: []*popups.ModalButton{{Label: nsTabbed.T("OK"), Kind: popups.BtnPrimary}}})
}

// shareIntoFolder is ShareFileAsChannelFolderItemConfirmationModal.
type shareIntoFolder struct {
	h      Host
	modal  *popups.Modal
	access string // "write" (Can edit) | "read" (Can view)
	btn    widget.Clickable
	btnSz  image.Point
	busy   bool
}

func newShareIntoFolderModal(h Host, ph popups.Host, ch, folder string, f core.File, done func(bool)) *popups.Modal {
	c := h.Core()
	s := &shareIntoFolder{h: h, access: "write"}
	name := (&fileItem{File: f}).title()
	var body []i18n.Part
	cv, _ := c.Conv(ch)
	switch cv.Kind {
	case "im":
		body = nsTabbed.RT("When you share <b>{fileName}</b> here, <b>{channel}</b> can access it.", "fileName", name, "channel", cv.Name)
	case "mpim":
		body = nsTabbed.RT("When you share <b>{fileName}</b> here, anyone in this conversation can access it.", "fileName", name)
	default:
		body = nsTabbed.RT("When you share <b>{fileName}</b> here, anyone in <b>{channel}</b> can access it.", "fileName", name, "channel", "#"+cv.Name)
	}
	th := h.Theme()
	share := &popups.ModalButton{Label: nsTabbed.T("Share"), Kind: popups.BtnPrimary, Enabled: func() bool { return !s.busy }}
	share.Do = func() bool {
		s.busy = true
		access := ""
		if f.IsCanvas || f.IsList {
			access = s.access
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := c.AddFolderFile(ctx, ch, folder, f, access)
			postTo(h, func() {
				s.busy = false
				s.modal.Dismiss()
				if err != nil {
					ph.ShowOverlay(newFolderFileErrorModal(ph, f))
				}
				done(err == nil)
			})
		}()
		return false
	}
	cancel := &popups.ModalButton{Label: nsTabbed.T("Cancel"), Kind: popups.BtnOutline, Do: func() bool { done(false); return true }}
	text := i18n.Plain(body)
	s.modal = popups.NewModal(ph, popups.ModalOpts{Title: nsTabbed.T("Share this file?"), AutoFocus: share,
		Body: func(gtx C) D {
			d := th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: th.C.Fg}, text)
			if !(f.IsCanvas || f.IsList) {
				return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y+gtx.Dp(16))}
			}
			// the access select (c7mJ): Can edit / Can view
			y := d.Size.Y + gtx.Dp(16)
			if s.btn.Clicked(gtx) {
				items := []popups.MenuItem{
					{Label: i18n.T("file-permissions", "Can edit"), Checked: s.access == "write", Do: func() { s.access = "write" }},
					{Label: i18n.T("file-permissions", "Can view"), Checked: s.access == "read", Do: func() { s.access = "read" }},
				}
				ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Width: 200, Popover: "bottom-left"}))
			}
			label := i18n.T("file-permissions", "Can edit")
			if s.access == "read" {
				label = i18n.T("file-permissions", "Can view")
			}
			dirAt(gtx, image.Pt(0, y), func(gtx C) D {
				return s.btn.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					lw := th.Measure(gtx, 15, wBold, label)
					w, hh := gtx.Dp(12)+lw+gtx.Dp(28), gtx.Dp(36)
					r := image.Rect(0, 0, w, hh)
					theme.RRect(gtx, r, gtx.Dp(8), theme.WithAlpha(th.C.Fg, 0x4d))
					theme.RRect(gtx, r.Inset(max(1, gtx.Dp(1))), gtx.Dp(8)-1, th.C.Bg)
					dirAt(gtx, image.Pt(gtx.Dp(12), 0), func(gtx C) D {
						return vmid(gtx, hh, func(gtx C) D {
							return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: th.C.Fg, MaxLines: 1}, label)
						})
					})
					s.btnSz = r.Max
					return D{Size: r.Max}
				})
			})
			return D{Size: image.Pt(gtx.Constraints.Max.X, y+gtx.Dp(36)+gtx.Dp(8))}
		},
		Buttons: []*popups.ModalButton{cancel, share}})
	return s.modal
}

