package popups

// "Create a folder" (boot-shared qSP5 AddEditBookmarkModal in folderMode,
// opened by tFkL useCreateFolder: Move to folder's "New folder"), as
// Slack's web client has it (build 859cadc0e0ab, read from the public CDN
// without a cookie):
//
//   - The title "Create a folder"; no Link field (tY = !folderMode); "Name"
//     over its medium input with the folder's icon in front of it
//     (.emojiButton__gtXfi: 44 wide, padding 8, position absolute; the
//     folder icon 20 in content-ter, .imageIconFolder__BHE45:after: a 1px
//     otl-sec line 20 tall, 8 to its left and right; the input padded 44
//     on the left), placeholder "Ex. Project tracker", focused at once.
//     Over 257 characters: "Folder name can’t be longer than 257
//     characters." and the field in error.
//   - Cancel and "Create" (primary), disabled while the name is empty
//     (nameIsRequiredForBookmarkFolder), too long, or saving.
//   - Create is addBookmarkFolderForChannel (core.AddBookmarkFolder); then
//     the caller's onSave (Move to folder moves the bookmark into it). A
//     failure keeps the modal with the error alert: "Channel and DM tabs
//     can have up to 100 total folders, folder items, pins, or workflows.
//     …" for too_many_bookmarks, "Couldn’t add new folder. The channel has
//     the maximum number of tabs." for too_many_tabs, else "Sorry, that
//     didn’t save! If the problem persists, please drop us a line."

import (
	"context"
	"errors"
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// CreateFolderModal is "Create a folder".
type CreateFolderModal struct {
	h       Host
	modal   *Modal
	name    widget.Editor
	focused bool
	pending bool
	err     string
	create  func(ctx context.Context, name string) error
}

// NewCreateFolderModal opens it; create makes the folder (and runs the
// caller's onSave). An error keeps the modal open with Slack's alert.
func NewCreateFolderModal(h Host, create func(ctx context.Context, name string) error) *CreateFolderModal {
	m := &CreateFolderModal{h: h, create: create}
	m.name.SingleLine, m.name.Submit = true, true
	ok := &ModalButton{Label: nsTabbedCh.T("Create"), Kind: BtnPrimary, Enabled: m.CanCreate, Do: func() bool { m.Create(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsTabbedCh.T("Create a folder"), Body: m.layout,
		Buttons:    []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, ok},
		FocusStops: func() []event.Tag { return []event.Tag{&m.name} }})
	return m
}

func (m *CreateFolderModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *CreateFolderModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *CreateFolderModal) Dismiss()                                    { m.modal.Dismiss() }

// SetName types into the field (tests).
func (m *CreateFolderModal) SetName(s string) { m.name.SetText(s); m.err = "" }

// Error is the alert's text ("" none).
func (m *CreateFolderModal) Error() string { return m.err }

func (m *CreateFolderModal) tooLong() bool { return utf8.RuneCountInString(m.name.Text()) > 257 }

// CanCreate: a name, not too long, nothing in flight.
func (m *CreateFolderModal) CanCreate() bool {
	return m.name.Len() > 0 && !m.tooLong() && !m.pending
}

// Create runs the write.
func (m *CreateFolderModal) Create() {
	if !m.CanCreate() || m.create == nil {
		return
	}
	name := m.name.Text()
	m.pending = true
	m.err = ""
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, m.create(ctx, name) }, func(_ struct{}, err error) {
		m.pending = false
		if err != nil {
			m.err = folderCreateError(err)
			return
		}
		m.modal.Dismiss()
	})
}

// folderCreateError is the modal's catch (ai).
func folderCreateError(err error) string {
	var se *slack.Error
	code := ""
	if errors.As(err, &se) {
		code = se.Code
	} else {
		code = err.Error()
	}
	switch code {
	case "too_many_bookmarks":
		return nsTabbedCh.T("Channel and DM tabs can have up to 100 total folders, folder items, pins, or workflows. To add another, remove one of those items from the conversation’s tabs or folders.")
	case "too_many_tabs":
		return nsTabbedCh.T("Couldn’t add new folder. The channel has the maximum number of tabs.")
	}
	return i18n.Plain(nsTabbedCh.RT("Sorry, that didn’t save! If the problem persists, please <a>drop us a line</a>."))
}

func (m *CreateFolderModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.name})
		m.focused = true
	}
	for {
		e, ok := m.name.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			m.Create()
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	if m.err != "" {
		d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, ccErrColor(th, cl), strings.TrimSpace(m.err), 0)
		})
		y += d.Size.Y + dp(gtx, 12)
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsTabbedCh.T("Name"), 1)
	})
	y += d.Size.Y + dp(gtx, 8)
	ih := dp(gtx, 44)
	long := m.tooLong()
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := m.folderField(gtx, th, cl, ih)
		if long {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), border1(gtx), ccErrColor(th, cl))
		}
		return d
	})
	y += ih
	if long {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, ccErrColor(th, cl), nsTabbedCh.T("Folder name can’t be longer than 257 characters."), 0)
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 20))}
}

// folderField is the Name input with the folder icon and its line before
// the text (the input padded 44 on the left).
func (m *CreateFolderModal) folderField(gtx layout.Context, th *theme.Theme, cl *Colors, hh int) layout.Dimensions {
	w := gtx.Constraints.Max.X
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	if gtx.Focused(&m.name) {
		ring, one := dp(gtx, 5), maxInt(1, dp(gtx, 1))
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r.Inset(-one), rad+one, cl.Focus)
		rrect(gtx, r, rad, cl.Bg)
	} else {
		bw := border1(gtx)
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(bw), rad-bw, cl.Bg)
	}
	ix := dp(gtx, 8)
	at(gtx, image.Pt(ix, (hh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "folder", 20, cl.Fg3) // content-ter
	})
	lx := ix + dp(gtx, 20) + dp(gtx, 8)
	lh := dp(gtx, 20)
	rrect(gtx, image.Rect(lx, (hh-lh)/2, lx+border1(gtx), (hh+lh)/2), 0, cl.BtnBorder) // otl-sec
	x := dp(gtx, 44)
	tw := maxInt(1, w-x-dp(gtx, 12))
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, hh)}
			if m.name.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 18, theme.Regular, cl.Placeholder, nsTabbedCh.T("Ex. Project tracker"))
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &m.name, unit.Sp(18), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	return layout.Dimensions{Size: r.Max}
}

// FolderShareKind is who a folder's files are shared with (aK5v K5:
// the share prompt's sentence).
type FolderShareKind int

const (
	FolderShareChannel FolderShareKind = iota
	FolderShareDM
	FolderShareMPIM
)

// NewFolderShareModal is uploadFilesToChannelFolder's prompt for n files:
// "Share this file?" / "Share these files?", whom they reach (name: the
// DM partner, or the channel's "#name"), Cancel and Share; share runs the
// upload.
func NewFolderShareModal(h Host, n int, kind FolderShareKind, name string, share func()) *Modal {
	title := nsTabbedCh.T("Share {fileCount, plural, one {this file} other {these files}}?", "fileCount", n)
	var body []i18n.Part
	switch kind {
	case FolderShareDM:
		body = nsTabbedCh.RT("When you share {fileCount, plural, one {a file} other {files}} here, <b>{channel}</b> can access {fileCount, plural, one {it} other {them}}.",
			"fileCount", n, "channel", name)
	case FolderShareMPIM:
		body = nsTabbedCh.RT("When you share {fileCount, plural, one {a file} other {files}} here, anyone in this conversation can access {fileCount, plural, one {it} other {them}}.",
			"fileCount", n)
	default:
		body = nsTabbedCh.RT("When you share {fileCount, plural, one {a file} other {files}} here, anyone in {channelName} can access {fileCount, plural, one {it} other {them}}.",
			"fileCount", n, "channelName", name)
	}
	ok := &ModalButton{Label: nsTabbedCh.T("Share"), Kind: BtnPrimary, Do: func() bool { share(); return true }}
	th := h.Theme()
	text := i18n.Plain(body)
	return NewModal(h, ModalOpts{Title: title, AutoFocus: ok, Body: func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, text, 0)
	}, Buttons: []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, ok}})
}

// NewFolderErrorModal is ShareFileAsFolderItemErrorDialog (ak4d) for an
// error with no file: Slack's title is then the link's ("We couldn’t add
// the link to the folder", isNonExistentObject of no file), the error's
// sentence, and OK.
func NewFolderErrorModal(h Host, code string) *Modal {
	var body string
	switch code {
	case "too_many_bookmarks":
		body = i18n.Plain(nsTabbedCh.RT("Channel and DM tabs can have up to 100 total folders, folder items, pins, or workflows. To add another, remove one of those items from the conversation’s tabs or folders."))
	case "cannot_share_file_uploads_disabled":
		body = nsTabbedCh.T("File uploads to this workspace have been disabled at the request of your administrator.")
	}
	th := h.Theme()
	return NewModal(h, ModalOpts{Title: nsTabbedCh.T("We couldn’t add the link to the folder"), Body: func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, body, 0)
	}, Buttons: []*ModalButton{{Label: nsTabbedCh.T("OK"), Kind: BtnPrimary}}})
}

// NewFolderUploadFailedModal is uploadFilesToChannelFolder's report of
// the files that failed: "We couldn’t add the file to the folder" / "…
// # files …", "This file couldn’t be uploaded:" / "These files …" over
// their names as a list, and OK.
func NewFolderUploadFailedModal(h Host, names []string) *Modal {
	n := len(names)
	title := i18n.Plain(nsTabbedCh.RT("We couldn’t add {errorCount, plural, one {the file} other {# files}} to the folder", "errorCount", n))
	lead := i18n.Plain(nsTabbedCh.RT("{errorCount, plural, one {This file} other {These files}} couldn’t be uploaded:", "errorCount", n))
	th := h.Theme()
	return NewModal(h, ModalOpts{Title: title, Body: func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		d := para(gtx, th, 15, 22, theme.Regular, cl.Fg, lead, 0)
		y := d.Size.Y
		for _, nm := range names { // <ul>: a bullet, 40px in
			at(gtx, image.Pt(dp(gtx, 22), y), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Fg, "•")
			})
			dd := at(gtx, image.Pt(dp(gtx, 40), y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 40)), 1<<20)}
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nm, 0)
			})
			y += dd.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(w, y)}
	}, Buttons: []*ModalButton{{Label: nsTabbedCh.T("OK"), Kind: BtnPrimary}}})
}
