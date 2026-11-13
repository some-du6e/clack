package popups

// The bookmark folder modals, as Slack's web client has them (build
// 859cadc0e0ab, read from the public CDN without a cookie):
//
//   - AddBookmarkModal (boot-shared qSP5) for a link: the title (the
//     caller's modalHeaderTitle: "Add link" from a folder, "Edit link" from
//     a row's menu), "Link" over its input (aria "Enter url for link.";
//     over 2049 characters: "Bookmark link can’t be longer than 2049
//     characters."), "Name" over its input ("Ex. Project tracker"; over 257:
//     "Link name can’t be longer than 257 characters."), then Cancel and
//     the primary button: "Add", or "Save" when editing. A link that
//     doesn't linkify fails with "Sorry could not save link. Please check if
//     the link is a valid url."
//   - DeleteBookmarkModal (4cxD): "Delete link" / "Remove file", "Are you
//     sure you want to delete <b>{link}</b> from <b>{folderName}</b>? This
//     cannot be undone." (a file: "…remove <b>{fileName}</b> from …"),
//     Cancel and Delete (Remove), a boxed error on failure ("Sorry could
//     not delete link, Please retry." / "…file…").
//   - RenameTabModal (O1Cc isRenameFolder): "Rename folder", "Folder name"
//     over the label (over 256: "Folder name can’t be longer than 256
//     characters."), Cancel and Save (updateTabLabel).
//   - TabDeleteConfirmationModal (plzS) for a folder with bookmarks: "Are
//     you sure you want to delete this folder with {n} item(s)?", "Deleting
//     <b>{folderName}</b> from <b>{channelName}</b> will unshare any files
//     and remove saved links for all members of the channel.", Cancel and
//     Delete (danger).

import (
	"context"
	"image"
	"net/url"
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsTabbedCh = i18n.NS("tabbed-channel")

// BookmarkLinkOpts open the link modal.
type BookmarkLinkOpts struct {
	Title      string // "Add link" or "Edit link"
	Link, Name string // prefilled (editing)
	Edit       bool   // the primary button says Save
	// Save writes the bookmark; an error keeps the modal open with
	// Slack's message.
	Save func(ctx context.Context, link, name string) error
}

// BookmarkLinkModal is AddBookmarkModal for a link.
type BookmarkLinkModal struct {
	h        Host
	o        BookmarkLinkOpts
	modal    *Modal
	link     widget.Editor
	name     widget.Editor
	focused  bool
	pending  bool
	err      string
	primary  *ModalButton
	cancel   *ModalButton
	OnSaved  func()
	linkSeen string
}

// NewBookmarkLinkModal builds it.
func NewBookmarkLinkModal(h Host, o BookmarkLinkOpts) *BookmarkLinkModal {
	m := &BookmarkLinkModal{h: h, o: o}
	m.link.SingleLine, m.link.Submit = true, true
	m.name.SingleLine, m.name.Submit = true, true
	m.link.SetText(o.Link)
	m.name.SetText(o.Name)
	label := nsTabbedCh.T("Add")
	if o.Edit {
		label = nsTabbedCh.T("Save")
	}
	m.cancel = &ModalButton{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}
	m.primary = &ModalButton{Label: label, Kind: BtnPrimary, Enabled: m.CanSave, Do: func() bool { m.Save(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: o.Title, Body: m.layout, Buttons: []*ModalButton{m.cancel, m.primary},
		FocusStops: func() []event.Tag { return []event.Tag{&m.link, &m.name} }})
	return m
}

func (m *BookmarkLinkModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *BookmarkLinkModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *BookmarkLinkModal) Dismiss()                                    { m.modal.Dismiss() }

// SetLink and SetName type into the fields (tests).
func (m *BookmarkLinkModal) SetLink(s string) { m.link.SetText(s); m.err = "" }
func (m *BookmarkLinkModal) SetName(s string) { m.name.SetText(s) }

// Error is the modal's error line ("" for none).
func (m *BookmarkLinkModal) Error() string { return m.err }

func (m *BookmarkLinkModal) tooLong() (link, name bool) {
	return utf8.RuneCountInString(m.link.Text()) > 2049, utf8.RuneCountInString(m.name.Text()) > 257
}

// CanSave: a link, neither field too long, nothing in flight.
func (m *BookmarkLinkModal) CanSave() bool {
	l, n := m.tooLong()
	return strings.TrimSpace(m.link.Text()) != "" && !l && !n && !m.pending
}

// NormalizeBookmarkLink is the modal's linkify step: the link as Slack
// would save it (a bare host gets http://), false when it isn't a link.
func NormalizeBookmarkLink(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return "", false
	}
	if !strings.Contains(s, "://") {
		if !strings.Contains(s, ".") {
			return "", false
		}
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return "", false
	}
	return s, true
}

// Save runs the write.
func (m *BookmarkLinkModal) Save() {
	if !m.CanSave() || m.o.Save == nil {
		return
	}
	link, ok := NormalizeBookmarkLink(m.link.Text())
	if !ok {
		m.err = nsTabbedCh.T("Sorry could not save link. Please check if the link is a valid url.")
		return
	}
	name := strings.TrimSpace(m.name.Text())
	m.pending = true
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, m.o.Save(ctx, link, name) },
		func(_ struct{}, err error) {
			m.pending = false
			if err != nil {
				m.err = i18n.Plain(nsTabbedCh.RT("Sorry, that didn’t save! If the problem persists, please <a>drop us a line</a>."))
				return
			}
			m.modal.Dismiss()
			if m.OnSaved != nil {
				m.OnSaved()
			}
		})
}

func (m *BookmarkLinkModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.link})
		m.focused = true
	}
	for _, ed := range []*widget.Editor{&m.link, &m.name} {
		for {
			e, ok := ed.Update(gtx)
			if !ok {
				break
			}
			switch e.(type) {
			case widget.ChangeEvent:
				if ed == &m.link && m.link.Text() != m.linkSeen {
					m.linkSeen, m.err = m.link.Text(), ""
				}
			case widget.SubmitEvent:
				m.Save()
			}
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	longLink, longName := m.tooLong()
	block := func(label string, ed *widget.Editor, placeholder, alert string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, label, 1)
		})
		y += d.Size.Y + dp(gtx, 8)
		ih := dp(gtx, 44)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, ih))
			d := field(gtx, th, ed, placeholder, "", 44, 18)
			if alert != "" {
				stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), border1(gtx), ccErrColor(th, cl))
			}
			return d
		})
		y += ih
		if alert != "" {
			y += dp(gtx, 4)
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, ccErrColor(th, cl), alert, 0)
			})
			y += d.Size.Y
		}
		y += dp(gtx, 20)
	}
	la := ""
	if longLink {
		la = nsTabbedCh.T("Bookmark link can’t be longer than 2049 characters.")
	}
	block(nsTabbedCh.T("Link"), &m.link, "", la)
	na := ""
	if longName {
		na = nsTabbedCh.T("Link name can’t be longer than 257 characters.")
	}
	block(nsTabbedCh.T("Name"), &m.name, nsTabbedCh.T("Ex. Project tracker"), na)
	if m.err != "" {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, ccErrColor(th, cl), m.err, 0)
		})
		y += d.Size.Y + dp(gtx, 8)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// NewDeleteBookmarkModal is DeleteBookmarkModal for a folder's link (file
// false) or file: title, the sentence naming it and the folder, Cancel and
// Delete / Remove; del runs the remove, an error shows Slack's line and
// keeps it open.
func NewDeleteBookmarkModal(h Host, file bool, what, folder string, del func(ctx context.Context) error) *Modal {
	title := nsTabbedCh.T("Delete link")
	body := i18n.Plain(nsTabbedCh.RT("Are you sure you want to delete <b>{link}</b> from <b>{folderName}</b>? This cannot be undone.",
		"link", what, "folderName", folder))
	action := nsTabbedCh.T("Delete")
	failed := nsTabbedCh.T("Sorry could not delete link, Please retry.")
	if file {
		title = nsTabbedCh.T("Remove file")
		body = i18n.Plain(nsTabbedCh.RT("Are you sure you want to remove <b>{fileName}</b> from <b>{folderName}</b>? This cannot be undone.",
			"fileName", what, "folderName", folder))
		action = nsTabbedCh.T("Remove")
		failed = nsTabbedCh.T("Sorry could not delete file, Please retry.")
	}
	var m *Modal
	errText, pending := "", false
	btn := &ModalButton{Label: action, Kind: BtnDanger, Enabled: func() bool { return !pending }}
	btn.Do = func() bool {
		pending = true
		run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, del(ctx) }, func(_ struct{}, err error) {
			pending = false
			if err != nil {
				errText = failed
				return
			}
			m.Dismiss()
		})
		return false
	}
	m = NewModal(h, ModalOpts{Title: title, AutoFocus: btn, Body: func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, body, 0)
		})
		y := d.Size.Y + dp(gtx, 20)
		if errText != "" {
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return para(gtx, th, 15, 22, theme.Regular, ccErrColor(th, cl), errText, 0)
			})
			y += d.Size.Y + dp(gtx, 8)
		}
		return layout.Dimensions{Size: image.Pt(w, y)}
	}, Buttons: []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, btn}})
	return m
}

// RenameTabModal is the folder's "Rename folder".
type RenameTabModal struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	label0  string
	focused bool
	pending bool
	save    func(ctx context.Context, label string) error
}

// NewRenameFolderModal opens "Rename folder" with the folder's label.
func NewRenameFolderModal(h Host, label string, save func(ctx context.Context, label string) error) *RenameTabModal {
	m := &RenameTabModal{h: h, label0: label, save: save}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.ed.SetText(label)
	m.ed.SetCaret(len([]rune(label)), len([]rune(label)))
	ok := &ModalButton{Label: nsTabbedCh.T("Save"), Kind: BtnPrimary, Enabled: m.canSave, Do: func() bool { m.Save(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsTabbedCh.T("Rename folder"), Body: m.layout,
		Buttons:    []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, ok},
		FocusStops: func() []event.Tag { return []event.Tag{&m.ed} }})
	return m
}

func (m *RenameTabModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *RenameTabModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *RenameTabModal) Dismiss()                                    { m.modal.Dismiss() }

// SetLabel types into the field (tests).
func (m *RenameTabModal) SetLabel(s string) { m.ed.SetText(s) }

func (m *RenameTabModal) canSave() bool {
	return !m.pending && utf8.RuneCountInString(m.ed.Text()) <= 256
}

// Save is updateTabLabel with the field's label.
func (m *RenameTabModal) Save() {
	if !m.canSave() {
		return
	}
	label := m.ed.Text()
	if label == m.label0 {
		m.modal.Dismiss()
		return
	}
	m.pending = true
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, m.save(ctx, label) }, func(_ struct{}, err error) {
		m.pending = false
		m.modal.Dismiss()
	})
}

func (m *RenameTabModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			m.Save()
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsTabbedCh.T("Folder name"), 1)
	})
	y := d.Size.Y + dp(gtx, 8)
	ih := dp(gtx, 44)
	long := utf8.RuneCountInString(m.ed.Text()) > 256
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &m.ed, "", "", 44, 18)
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
			return para(gtx, th, 13, 18, theme.Regular, ccErrColor(th, cl), nsTabbedCh.T("Folder name can’t be longer than 256 characters."), 0)
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 20))}
}

// NewDeleteFolderModal is TabDeleteConfirmationModal for a folder holding
// n bookmarks in channel (its "#name"); del removes the tab.
func NewDeleteFolderModal(h Host, n int, folder, channel string, del func()) *Modal {
	title := nsTabbedCh.T("Are you sure you want to delete this folder with {bookmarksCount, plural, =1 {1 item} other {# items}}?",
		"bookmarksCount", n)
	body := i18n.Plain(nsTabbedCh.RT("Deleting <b>{folderName}</b> from <b>{channelName}</b> will unshare any files and remove saved links for all members of the channel.",
		"folderName", folder, "channelName", channel))
	ok := &ModalButton{Label: nsTabbedCh.T("Delete"), Kind: BtnDanger, Do: func() bool { del(); return true }}
	th := h.Theme()
	return NewModal(h, ModalOpts{Title: title, AutoFocus: ok, Body: func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, body, 0)
	}, Buttons: []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, ok}})
}
