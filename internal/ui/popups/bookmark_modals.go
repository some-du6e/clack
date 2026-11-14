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
//   - RenameTabModal (O1Cc): "Rename tab" or, with isRenameFolder, "Rename
//     folder"; "Tab name" / "Folder name" over the label (over 256: "… name
//     can’t be longer than 256 characters."), the clear button, a canvas's
//     hint, Cancel and Save (updateTabLabel).
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
	// ShowFolder is showSetFolder (LinkEntity's "Add link to folder"):
	// under Name, "Folder" over a select of Folders (useFoldersForChannel's
	// order), the channel's default folder tab (SwCK: the folder tab with
	// no folder id, DefaultFolder its label) spliced in at DefaultAt among
	// them, and with NewFolder (useCanEditTabs) a divider and "New folder"
	// (plus). The default folder tab is picked first, else the first
	// folder. "New folder" shows "Folder Name" (folder icon, placeholder
	// "Bookmarks") for the folder to make. With no folders and no default
	// tab the select is the info banner "We’ll add this link to a Bookmarks
	// folder for safekeeping." with its "Edit folder name" link (which
	// shows "Folder Name" emptied), and the folder to make is "Bookmarks".
	// SaveIn gets the pick: the caller makes a new folder first
	// (addBookmarkFolderForChannel).
	ShowFolder    bool
	Folders       []FolderOption
	DefaultFolder string // the default folder tab's label ("" none)
	DefaultAt     int
	NewFolder     bool
	SaveIn        func(ctx context.Context, link, name string, pick FolderPick) error
}

// FolderPick is the folder a link goes to: an existing folder (ID), the
// channel's loose bookmarks (Default: parent_id ""), or a new folder
// named New.
type FolderPick struct {
	ID      string
	Default bool
	New     string
}

// Folder option values (qSP5 eR, eL).
const (
	folderOptNew     = "new_folder"
	folderOptDefault = "default_folder"
)

// folderOpt is one of the select's options.
type folderOpt struct {
	value, label string
	divider      bool
}

// FolderOption is one of showSetFolder's folders.
type FolderOption struct{ ID, Title string }

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
	opts     []folderOpt // the select's options (t$)
	sel      string      // the picked option's value (tM)
	editName bool        // the banner's "Edit folder name" was clicked (tL)
	banner   bool        // no folders and no default tab (tB)
	newName  widget.Editor
	selBtn   widget.Clickable
	editLink widget.Clickable
	selRect  image.Rectangle
}

// Folder is the picked folder's id ("" for none, the default folder or a
// new one).
func (m *BookmarkLinkModal) Folder() string {
	if m.sel == folderOptNew || m.sel == folderOptDefault {
		return ""
	}
	return m.sel
}

// FolderOptions are the select's labels, "─" a divider (tests).
func (m *BookmarkLinkModal) FolderOptions() []string {
	var out []string
	for _, o := range m.opts {
		if o.divider {
			out = append(out, "─")
			continue
		}
		out = append(out, o.label)
	}
	return out
}

// Picked is the selected option's label (tests).
func (m *BookmarkLinkModal) Picked() string {
	for _, o := range m.opts {
		if o.value == m.sel && !o.divider {
			return o.label
		}
	}
	return ""
}

// PickFolder picks the select's option i (tests; onSelectionChange).
func (m *BookmarkLinkModal) PickFolder(i int) {
	if i >= 0 && i < len(m.opts) && !m.opts[i].divider {
		m.sel = m.opts[i].value
	}
}

// EditFolderName is the banner's "Edit folder name" (tests).
func (m *BookmarkLinkModal) EditFolderName() {
	m.editName = true
	m.newName.SetText("")
}

// SetFolderName types into "Folder Name" (tests).
func (m *BookmarkLinkModal) SetFolderName(s string) { m.newName.SetText(s) }

// naming is tq: "Folder Name" shows (New folder picked, or the banner's
// Edit folder name).
func (m *BookmarkLinkModal) naming() bool { return m.sel == folderOptNew || m.editName }

// folderOK is tW: something to save into.
func (m *BookmarkLinkModal) folderOK() bool {
	n := m.newName.Len() > 0
	return m.naming() && n || !m.naming() && m.sel != "" || m.banner && n
}

// pick is the save's folder (the submit's parentId).
func (m *BookmarkLinkModal) pick() FolderPick {
	if name := m.newName.Text(); m.naming() && name != "" || m.banner {
		return FolderPick{New: name}
	}
	if m.sel == folderOptDefault {
		return FolderPick{Default: true}
	}
	return FolderPick{ID: m.sel}
}

// NewBookmarkLinkModal builds it.
func NewBookmarkLinkModal(h Host, o BookmarkLinkOpts) *BookmarkLinkModal {
	m := &BookmarkLinkModal{h: h, o: o}
	m.link.SingleLine, m.link.Submit = true, true
	m.name.SingleLine, m.name.Submit = true, true
	m.newName.SingleLine, m.newName.Submit = true, true
	if o.ShowFolder {
		for _, f := range o.Folders {
			m.opts = append(m.opts, folderOpt{value: f.ID, label: f.Title})
		}
		if o.DefaultFolder != "" {
			at := min(max(o.DefaultAt, 0), len(m.opts))
			m.opts = append(m.opts[:at], append([]folderOpt{{value: folderOptDefault, label: o.DefaultFolder}}, m.opts[at:]...)...)
			m.sel = folderOptDefault
		} else if len(o.Folders) > 0 {
			m.sel = o.Folders[0].ID
		}
		if o.NewFolder {
			if len(m.opts) > 0 {
				m.opts = append(m.opts, folderOpt{divider: true})
			}
			m.opts = append(m.opts, folderOpt{value: folderOptNew, label: nsTabbedCh.T("New folder")})
		}
		if m.banner = len(o.Folders) == 0 && o.DefaultFolder == ""; m.banner {
			m.newName.SetText(nsTabbedCh.T("Bookmarks"))
		}
	}
	m.link.SetText(o.Link)
	m.name.SetText(o.Name)
	label := nsTabbedCh.T("Add")
	if o.Edit {
		label = nsTabbedCh.T("Save")
	}
	m.cancel = &ModalButton{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}
	m.primary = &ModalButton{Label: label, Kind: BtnPrimary, Enabled: m.CanSave, Do: func() bool { m.Save(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: o.Title, Body: m.layout, Buttons: []*ModalButton{m.cancel, m.primary},
		FocusStops: func() []event.Tag {
			stops := []event.Tag{&m.link, &m.name}
			if m.o.ShowFolder && m.naming() {
				stops = append(stops, &m.newName)
			}
			return stops
		}})
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

// CanSave: a link, neither field too long, nothing in flight, and with
// the folder select somewhere to save into.
func (m *BookmarkLinkModal) CanSave() bool {
	l, n := m.tooLong()
	return strings.TrimSpace(m.link.Text()) != "" && !l && !n && !m.pending && (!m.o.ShowFolder || m.folderOK())
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
	if !m.CanSave() || m.o.Save == nil && m.o.SaveIn == nil {
		return
	}
	link, ok := NormalizeBookmarkLink(m.link.Text())
	if !ok {
		m.err = nsTabbedCh.T("Sorry could not save link. Please check if the link is a valid url.")
		return
	}
	name := strings.TrimSpace(m.name.Text())
	m.pending = true
	pick := m.pick()
	run(m.h, func(ctx context.Context) (struct{}, error) {
		if m.o.ShowFolder && m.o.SaveIn != nil {
			return struct{}{}, m.o.SaveIn(ctx, link, name, pick)
		}
		return struct{}{}, m.o.Save(ctx, link, name)
	},
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
	for _, ed := range []*widget.Editor{&m.link, &m.name, &m.newName} {
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
	if m.o.ShowFolder {
		y = m.folderField(gtx, th, cl, w, y)
	}
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

// RenameTabModal is RenameTabModal (O1Cc): "Rename tab" (or "Rename
// folder" with isRenameFolder), "Tab name" / "Folder name" over the
// field, which starts with the tab's label (or its file's title) all
// selected; a clear button (close-circle-filled, "Clear text") inside it
// while it has text; for a canvas the hint "Clear text to default to file
// name" under it; over 256 characters "Tab name can’t be longer than 256
// characters." (the folder's wording for a folder). Enter or Save is
// updateTabLabel and closes the modal; Save is disabled for an empty name
// (but on a canvas, which goes back to its file's name) or a long one.
type RenameTabModal struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	clear   widget.Clickable
	o       RenameTabOpts
	focused bool
	pending bool
}

// RenameTabOpts open the rename modal.
type RenameTabOpts struct {
	Label  string // the field's start: the tab's label, else its file's title
	Folder bool   // isRenameFolder: the folder's wording
	Canvas bool   // a canvas tab: an empty name resets it, with the hint
	Save   func(ctx context.Context, label string) error
}

// NewRenameFolderModal opens "Rename folder" with the folder's label.
func NewRenameFolderModal(h Host, label string, save func(ctx context.Context, label string) error) *RenameTabModal {
	return NewRenameTabModal(h, RenameTabOpts{Label: label, Folder: true, Save: save})
}

// NewRenameTabModal opens the rename modal.
func NewRenameTabModal(h Host, o RenameTabOpts) *RenameTabModal {
	m := &RenameTabModal{h: h, o: o}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.ed.SetText(o.Label)
	m.ed.SetCaret(len([]rune(o.Label)), 0) // forceSelectAll
	ok := &ModalButton{Label: nsTabbedCh.T("Save"), Kind: BtnPrimary, Enabled: m.canSave, Do: func() bool { m.Save(); return false }}
	title := nsTabbedCh.T("Rename tab")
	if o.Folder {
		title = nsTabbedCh.T("Rename folder")
	}
	m.modal = NewModal(h, ModalOpts{Title: title, Body: m.layout,
		Buttons:    []*ModalButton{{Label: nsTabbedCh.T("Cancel"), Kind: BtnOutline}, ok},
		FocusStops: func() []event.Tag { return []event.Tag{&m.ed, &m.clear} }})
	return m
}

func (m *RenameTabModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *RenameTabModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *RenameTabModal) Dismiss()                                    { m.modal.Dismiss() }

// SetLabel types into the field (tests).
func (m *RenameTabModal) SetLabel(s string) { m.ed.SetText(s) }

// Label is the field's text (tests).
func (m *RenameTabModal) Label() string { return m.ed.Text() }

// Title is the modal's title (tests).
func (m *RenameTabModal) Title() string { return m.modal.o.Title }

// ClearButton is the field's clear button (tests).
func (m *RenameTabModal) ClearButton() *widget.Clickable { return &m.clear }

// CanSave is the Save button's state.
func (m *RenameTabModal) CanSave() bool { return m.canSave() }

func (m *RenameTabModal) canSave() bool {
	n := utf8.RuneCountInString(m.ed.Text())
	return !m.pending && n <= 256 && (n > 0 || m.o.Canvas)
}

// Save is updateTabLabel with the field's label.
func (m *RenameTabModal) Save() {
	if !m.canSave() {
		return
	}
	label := m.ed.Text()
	m.pending = true
	m.modal.Dismiss() // the modal closes as the write goes out (closeModal after the dispatch)
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, m.o.Save(ctx, label) }, func(_ struct{}, err error) {
		m.pending = false
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
	if m.clear.Clicked(gtx) { // Clear text: empty, and the field keeps the focus
		m.ed.SetText("")
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	name, long := nsTabbedCh.T("Tab name"), nsTabbedCh.T("Tab name can’t be longer than 256 characters.")
	if m.o.Folder {
		name, long = nsTabbedCh.T("Folder name"), nsTabbedCh.T("Folder name can’t be longer than 256 characters.")
	}
	d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, name, 1)
	})
	y := d.Size.Y + dp(gtx, 8)
	ih := dp(gtx, 44)
	tooLong := utf8.RuneCountInString(m.ed.Text()) > 256
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &m.ed, "", "", 44, 18)
		if tooLong {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), border1(gtx), ccErrColor(th, cl))
		}
		if m.ed.Len() > 0 {
			// .clearButton__m92BY: the smedium icon button at the field's right
			bs := dp(gtx, 28)
			at(gtx, image.Pt(w-dp(gtx, 8)-bs, (ih-bs)/2), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, &m.clear, "close-circle-filled", 28, 18, cl.Fg3, cl.BgHover, 6)
			})
		}
		return d
	})
	y += ih
	switch {
	case tooLong:
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, ccErrColor(th, cl), long, 0)
		})
		y += d.Size.Y
	case m.o.Canvas: // hintText
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, nsTabbedCh.T("Clear text to default to file name"), 0)
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

// folderField is showSetFolder's part: "Folder" over the BasicSelect
// (44 tall, the picked option's title, caret-down; a click opens the
// options list), "Folder Name" when a new folder is being named, or with
// no folders the boxed info banner with "Edit folder name".
func (m *BookmarkLinkModal) folderField(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	if m.editLink.Clicked(gtx) {
		m.EditFolderName()
		gtx.Execute(key.FocusCmd{Tag: &m.newName})
	}
	if m.banner && !m.editName {
		pad := dp(gtx, 12)
		call, d := record(loose(gtx, image.Pt(w-2*pad, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			sentence := i18n.Plain(nsTabbedCh.RT("We’ll add this link to a <strong>Bookmarks</strong> folder for safekeeping. "))
			d := para(gtx, th, 15, 22, theme.Regular, cl.Fg, sentence, 0)
			// the Link-styled "Edit folder name" right after the sentence
			// (inline when it fits on the line)
			lp := image.Pt(0, d.Size.Y)
			edit := nsTabbedCh.T("Edit folder name")
			if sw := th.Measure(gtx, 15, theme.Regular, sentence); d.Size.Y <= dp(gtx, 22) && sw+th.Measure(gtx, 15, theme.Regular, edit) <= gtx.Constraints.Max.X {
				lp = image.Pt(sw, 0)
			}
			lk := at(gtx, lp, func(gtx layout.Context) layout.Dimensions {
				return m.editLink.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					d := text(gtx, th, 15, theme.Regular, cl.Link, edit)
					pointerCursor(gtx, d.Size)
					return d
				})
			})
			return layout.Dimensions{Size: image.Pt(max(d.Size.X, lp.X+lk.Size.X), max(d.Size.Y, lp.Y+lk.Size.Y))}
		})
		r := image.Rect(0, y, w, y+d.Size.Y+2*pad)
		rrect(gtx, r, dp(gtx, 8), cl.Ring)
		rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), cl.BgAlt)
		macroAt(gtx, call, image.Pt(pad, y+pad))
		return r.Max.Y + dp(gtx, 20)
	}
	if !m.banner && len(m.opts) > 0 {
		if m.selBtn.Clicked(gtx) {
			labels := make([]string, len(m.opts))
			cur := -1
			o := SelectListOpts{Dividers: map[int]bool{}, Icons: map[int]string{}}
			for i, f := range m.opts {
				labels[i] = f.label
				if f.divider {
					o.Dividers[i] = true
				}
				if f.value == folderOptNew {
					o.Icons[i] = "plus"
				}
				if f.value == m.sel && !f.divider {
					cur = i
				}
			}
			m.h.ShowOverlay(NewSelectList(m.h, m.selRect, labels, cur, o, func(i int) { m.PickFolder(i) }))
		}
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsTabbedCh.T("Folder"), 1)
		})
		y += d.Size.Y + dp(gtx, 8)
		ih := dp(gtx, 44)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return m.selBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(w, ih)
				r := image.Rectangle{Max: sz}
				stroke(gtx, r, dp(gtx, 8), border1(gtx), cl.BtnBorder)
				x := dp(gtx, 12)
				if m.sel == folderOptNew { // renderSelectedOption: the plus too
					at(gtx, image.Pt(x, (ih-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "plus", 20, cl.Fg) })
					x += dp(gtx, 24)
				}
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-x-dp(gtx, 36), ih)}
					return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 18, theme.Regular, cl.Fg, m.Picked()) })
				})
				at(gtx, image.Pt(w-dp(gtx, 12)-dp(gtx, 20), (ih-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "caret-down", 20, cl.Fg)
				})
				pointerCursor(gtx, sz)
				return layout.Dimensions{Size: sz}
			})
		})
		m.selRect = image.Rect(0, y, w, y+ih).Add(dtModalOrigin(gtx, m.modal)) // window coordinates
		y += ih + dp(gtx, 20)
	}
	if m.naming() {
		// "Folder Name": the folder icon box (imageIconFolder) before the
		// input, placeholder "Bookmarks"
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsTabbedCh.T("Folder Name"), 1)
		})
		y += d.Size.Y + dp(gtx, 8)
		ih := dp(gtx, 44)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, ih))
			return field(gtx, th, &m.newName, nsTabbedCh.T("Bookmarks"), "folder", 44, 18)
		})
		y += ih + dp(gtx, 20)
	}
	return y
}
