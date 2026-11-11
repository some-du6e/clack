package popups

import (
	"bytes"
	"context"
	"errors"
	"image"
	"net/url"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/clipimg"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Right-click (context) menus, read from Slack's web client and desktop
// shell (build a84662d2d135, 2026-11-08):
//
//   - A message's list item opens MessageActionsContextMenu: the More
//     actions menu with menuUsageLocation "context", whose first group is
//     the hover toolbar's actions (Add reaction… R, Reply in thread T,
//     Forward message… F, Add to-do A).
//   - should_show_message_contextmenu leaves links, images, emoji, inputs
//     and video to the desktop shell's own menu: Copy Link (Copy Email
//     Address for mailto:) and Open Link, then Copy Image and Copy Image
//     URL when the link holds an image; an image alone gets the last two.
//   - User mentions sit in MemberContextMenu (the member menu), channel
//     links in ChannelContextMenu{id}, files in FileContextMenuTrigger
//     (Slack's file menu, its media-gallery groups in channels).
//   - With text selected in a message, TextSelectionContextMenu: Copy text
//     (⌘C / Ctrl+C) and Search in Slack.
//   - Menu item keys: menu_keys.go.

// MessageContext says where a message's context menu opened.
type MessageContext uint8

const (
	InChannel     MessageContext = iota // a channel's message list
	InThread                            // the thread pane
	InThreadsView                       // the Threads view
)

// NewMessageContextMenu is Slack's MessageActionsContextMenu for m, its
// top-left corner at the pointer (window coordinates). The menu is the
// More actions menu (MessageActionsMenu, so plugins that patch it still
// reach it) under the hover toolbar's group.
func NewMessageContextMenu(h Host, at image.Point, m core.Message, where MessageContext, o MessageMenuOpts) *Menu {
	o.Where = where
	items := append(ctxHoverItems(h, at, m, where, o), Sep())
	items = append(items, actMessageItems(h, m, o)...)
	a := pointRect(at)
	p := actMenuProps(m, a)
	p["menuUsageLocation"] = "context"
	p["messageContainerType"] = ctxContainer(where)
	wrap := slackc.Props{"ts": m.TS, "channelId": m.Channel, "messageContainerType": ctxContainer(where), "targetBounds": rectProps(a, 1)}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		wrap["threadTs"] = m.ThreadTS
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: a, Within: "MessageActionsMenu", WithinProps: p,
		Wraps: []MenuWrap{{Name: "MessageActionsContextMenu", Props: wrap}}})
}

func ctxContainer(where MessageContext) string {
	switch where {
	case InThread:
		return "thread"
	case InThreadsView:
		return "threads_view"
	}
	return "message_pane"
}

// ctxHoverItems is the context menu's first group (kctM for "context"):
// Add reaction…, the thread action (not in the thread pane), Forward
// message… and Add to-do / Complete to-do.
func ctxHoverItems(h Host, at image.Point, m core.Message, where MessageContext, o MessageMenuOpts) []MenuItem {
	var items []MenuItem
	ch, ts := m.Channel, m.TS
	if !m.System {
		items = append(items, MenuItem{Label: "Add reaction…", Icon: "add-reaction", Hint: nsMessage.T("R"), Keys: KeyReact, Do: func() {
			h.ShowOverlay(NewReactionPicker(h, at, ch, ts, m.ThreadTS, nil))
		}}.In(nsMessage))
	}
	if where != InThread {
		isReply := m.ThreadTS != "" && m.ThreadTS != m.TS
		label := "Reply in thread"
		switch {
		case where == InThreadsView:
			label = "View thread"
		case m.ReplyCount > 0 || isReply:
			label = "Reply to thread"
		}
		items = append(items, MenuItem{Label: label, Icon: "threads", Hint: nsMessage.T("T"), Keys: KeyComment, Do: func() {
			if o.OnReply != nil {
				o.OnReply()
				return
			}
			root := m.ThreadTS
			if root == "" {
				root = ts
			}
			h.OpenThread(ch, root)
		}}.In(nsMessage))
	}
	items = append(items, MenuItem{Label: "Forward message…", Icon: "forward-msg", Hint: nsMessage.T("F"), Keys: KeyShare, Do: func() {
		h.ShowOverlay(NewForwardModal(h, m))
	}}.In(nsMessage))
	todo := MenuItem{Label: "Add to-do", Icon: "todo-add", Hint: nsSaved.T("A"), Keys: KeyLaterTodos}
	if m.Saved {
		todo.Label, todo.Icon = "Complete to-do", "check"
	}
	todo = todo.In(nsSaved) // the to-do action (LaZW)
	todo.Do = func() {
		if o.OnTodo != nil {
			o.OnTodo()
			return
		}
		if m.Saved {
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.Unsave(ctx, ch, ts) })
			return
		}
		actDo(h, "", func(ctx context.Context, a *core.App) error { return a.AddTodo(ctx, ch, ts, 0) })
	}
	return append(items, todo)
}

// LinkMenu is what NewLinkMenu needs.
type LinkMenu struct {
	URL string
	// Image is the image inside the link ("" for a text link); Proxy says
	// it needs Slack's cookie (files.slack.com).
	Image string
	Proxy bool
	// Open opens the link the way a click would; nil opens it in the
	// browser.
	Open func()
}

// NewLinkMenu is the desktop shell's link menu (buildMenuForLink) at the
// pointer: Copy Link, or Copy Email Address for a mailto: link, and Open
// Link; then Copy Image and Copy Image URL when the link holds an image.
func NewLinkMenu(h Host, at image.Point, l LinkMenu) *Menu {
	copyLabel, copied := "Copy Link", l.URL
	if strings.HasPrefix(strings.ToLower(l.URL), "mailto:") {
		addr := l.URL[len("mailto:"):]
		if i := strings.IndexByte(addr, '?'); i >= 0 {
			addr = addr[:i]
		}
		if addr != "" {
			copyLabel = "Copy Email Address"
			if a, err := url.PathUnescape(addr); err == nil {
				addr = a
			}
			copied = addr
		}
	}
	items := []MenuItem{
		{Label: copyLabel, DoCtx: func(gtx layout.Context) { copyText(gtx, copied) }},
		{Label: "Open Link", Do: func() {
			if l.Open != nil {
				l.Open()
				return
			}
			lbOpen(h, l.URL)
		}},
	}
	if l.Image != "" {
		items = append(items, Sep())
		items = append(items, ctxImageItems(h, l.Image, l.Proxy)...)
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(at), Width: 220})
}

// NewImageMenu is the desktop shell's image menu (buildMenuForImage):
// Copy Image and Copy Image URL. Emoji get it too.
func NewImageMenu(h Host, at image.Point, src string, proxy bool) *Menu {
	return NewMenuOpts(h, ctxImageItems(h, src, proxy), MenuOpts{Anchor: pointRect(at), Width: 220})
}

func ctxImageItems(h Host, src string, proxy bool) []MenuItem {
	return []MenuItem{
		{Label: "Copy Image", Do: func() { CopyImage(h, src, proxy, "") }},
		{Label: "Copy Image URL", DoCtx: func(gtx layout.Context) { copyText(gtx, src) }},
	}
}

// CopyImage puts the image at src on the clipboard (Slack-hosted images
// come through core.Fetch; proxy forces that). It toasts ok ("" for none)
// or the failure.
func CopyImage(h Host, src string, proxy bool, ok string) {
	run(h, func(ctx context.Context) (struct{}, error) {
		data, err := fetchBytes(ctx, h, src, proxy)
		if err != nil {
			return struct{}{}, err
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, clipimg.Write(ctx, img)
	}, func(_ struct{}, err error) {
		switch {
		case err != nil:
			h.Toast("Failed to copy image: " + errText(err))
		case ok != "":
			h.Toast(ok)
		}
	})
}

// fetchBytes downloads src: through core for Slack's hosts (or when
// viaCore), else publicly without credentials (lbGet).
func fetchBytes(ctx context.Context, h Host, src string, viaCore bool) ([]byte, error) {
	if c := h.Core(); c != nil && (viaCore || lbSlackHost(src)) {
		data, _, err := c.Fetch(ctx, src)
		return data, err
	}
	return lbGet(ctx, src)
}

// NewFileMenu is Slack's file context menu (FileContextMenuTrigger,
// "File actions", buildFileContextMenuItems) for f in m, with the
// media-gallery templates Slack uses in channels and threads, minus what
// Clack can't do yet (file details, external links, folders, sections,
// transcripts, editing):
//
//	image: Download | Share file…, Copy link, Copy image, Save for later |
//	       Open in ▸ | Delete file
//	video: Download, Share file…, Copy link, Save for later | Open in ▸ |
//	       Delete file
//	other: Share file…, Copy link, Download, Save for later
//
// Delete file shows for the uploader's own files (fileActions.delete_file)
// and asks first (DeleteFileDialog).
func NewFileMenu(h Host, at image.Point, m core.Message, f *richtext.File) *Menu {
	share := MenuItem{Label: "Share file…", Do: func() { h.ShowOverlay(NewForwardModal(h, m)) }}.In(nsFile)
	link := f.Permalink
	copyLink := MenuItem{Label: "Copy link", DoCtx: func(gtx layout.Context) {
		copyText(gtx, link)
		h.Toast(nsFile.T("Link copied to clipboard")) // enqueueCopyLinkSuccessToast (Bwpd)
	}}.In(nsFile)
	download := MenuItem{Label: "Download", Do: func() { DownloadFile(h, f) }}.In(nsFile)
	var openIn []MenuItem
	if f.Permalink != "" {
		openIn = append(openIn, MenuItem{Label: "Open in browser", Do: func() { lbOpen(h, f.Permalink) }}.In(nsFile))
	}
	if m.Channel != "" && m.TS != "" && f.IsVideo {
		root := m.ThreadTS
		if root == "" {
			root = m.TS
		}
		openIn = append(openIn, MenuItem{Label: "Open in thread", Do: func() { h.OpenThread(m.Channel, root) }}.In(nsFile))
	}
	if f.URL != "" {
		label := "Open in new tab"
		if f.IsVideo {
			label = "Open in new window"
		}
		openIn = append(openIn, MenuItem{Label: label, Do: func() { lbOpen(h, f.URL) }}.In(nsFile))
	}
	var items []MenuItem
	group := func(g ...MenuItem) {
		var keep []MenuItem
		for _, it := range g {
			// by key: the labels are in the viewer's locale
			if it.Label == "" || it.Key == LabelKey("Copy link") && link == "" {
				continue
			}
			if it.Key == LabelKey("Open in") && len(it.Sub) == 0 {
				continue
			}
			keep = append(keep, it)
		}
		if len(keep) == 0 {
			return
		}
		if len(items) > 0 {
			items = append(items, Sep())
		}
		items = append(items, keep...)
	}
	open := MenuItem{Label: "Open in", Sub: openIn}.In(nsFile)
	later := fileLaterItem(h, f)
	del := fileDeleteItem(h, m, f)
	switch {
	case f.IsImage:
		src := f.URL
		if src == "" && f.Thumb != nil {
			src = f.Thumb.URL
		}
		cp := MenuItem{Label: "Copy image", Do: func() { CopyImage(h, src, true, nsFile.T("Image copied to clipboard")) }}.In(nsFile)
		group(download)
		if src != "" {
			group(share, copyLink, cp, later)
		} else {
			group(share, copyLink, later)
		}
		group(open)
		group(del)
	case f.IsVideo:
		group(download, share, copyLink, later)
		group(open)
		group(del)
	default:
		group(share, copyLink, download, later)
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(at), Width: 240})
}

// fileLaterItem is save_for_later: "Save for later", or "Remove from
// Later" for a saved file (saveFile / removeFile).
func fileLaterItem(h Host, f *richtext.File) MenuItem {
	a := h.Core()
	if a == nil || f.ID == "" {
		return MenuItem{}
	}
	id := f.ID
	if a.FileSaved(id) {
		return MenuItem{Label: "Remove from Later", Do: func() {
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.UnsaveFile(ctx, id) })
		}}.In(nsFile)
	}
	return MenuItem{Label: "Save for later", Do: func() {
		actDo(h, "", func(ctx context.Context, a *core.App) error { return a.SaveFile(ctx, id) })
	}}.In(nsFile)
}

// fileDeleteItem is delete_file for the uploader's own files: Slack's
// DeleteFileDialog ("Delete file", "Are you sure you want to delete this
// file permanently?", the file, "Yes, Delete This File") then files.delete.
func fileDeleteItem(h Host, m core.Message, f *richtext.File) MenuItem {
	self := actSelfID(h)
	owner := f.User
	if owner == "" {
		owner = m.User
	}
	if self == "" || owner != self || f.ID == "" {
		return MenuItem{}
	}
	return MenuItem{Label: "Delete file", Danger: true, Do: func() { ConfirmDeleteFile(h, f) }}.In(nsFile)
}

// ConfirmDeleteFile is DeleteFileDialog for f.
func ConfirmDeleteFile(h Host, f *richtext.File) {
	id := f.ID
	Confirm(h, nsFiles.T("Delete file"), nsFiles.T("Are you sure you want to delete this file permanently?"), nsFiles.T("Yes, Delete This File"), true,
		func(gtx layout.Context) layout.Dimensions { return filePillowPreview(gtx, h, f) },
		func() {
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.DeleteFile(ctx, id) })
		}).OldDialog() // DeleteFileDialog: DeprecatedBaseDialog, focusOnPrimaryButton
}

// DownloadFile saves f to the Downloads folder, like the lightbox's
// Download button.
func DownloadFile(h Host, f *richtext.File) {
	src := f.URL
	if src == "" {
		src = f.Download
	}
	if src == "" {
		h.Toast("This file can't be downloaded")
		return
	}
	name := f.Name
	if name == "" {
		name = f.Title
	}
	saveDownload(h, src, name, true, nil)
}

// saveDownload fetches src (through core when viaCore or on Slack's
// hosts) into the Downloads folder and toasts where it went.
func saveDownload(h Host, src, name string, viaCore bool, done func()) {
	run(h, func(ctx context.Context) (string, error) {
		data, err := fetchBytes(ctx, h, src, viaCore)
		if err != nil {
			return "", err
		}
		return lbSave(name, src, data)
	}, func(p string, err error) {
		if done != nil {
			done()
		}
		if errors.Is(err, ErrDownloadCanceled) {
			return // the save dialog was closed
		}
		if err != nil {
			h.Toast("Couldn't download: " + errText(err))
			return
		}
		h.Toast("Saved to " + p)
	})
}

// NewCodeMenu is what Slack desktop opens on a right-click on inline code
// or a code block with nothing selected: BaseCopyCodeBlockContextMenuTrigger
// (boot-shared StVK) hands the shell one item, "Copy this code" ("Copy
// this code block" for a block), copying getCodeBlockText of the code run;
// the shell (app.asar buildMenuForNonText) appends a separator and nothing
// else outside fullscreen and devtools.
func NewCodeMenu(h Host, at image.Point, code string, block bool) *Menu {
	label := "Copy this code"
	if block {
		label = "Copy this code block"
	}
	items := []MenuItem{MenuItem{Label: label, DoCtx: func(gtx layout.Context) { copyText(gtx, code) }}.In(nsMessage)}
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(at), Width: 220})
}

// NewTextSelectionMenu is Slack's TextSelectionContextMenu: Copy text
// (⌘C on macOS, Ctrl+C elsewhere) and Search in Slack. text is what Copy
// puts on the clipboard. ("Explain this" needs Slack AI; the "Switch to
// default menu" hint points at a preference Clack doesn't have.)
func NewTextSelectionMenu(h Host, at image.Point, text string) *Menu {
	items := inAll(nsMessage, []MenuItem{
		{Label: "Copy text", Icon: "text-format", Hint: shortcut.Format("Mod+C"), Keys: KeyCopyText, DoCtx: func(gtx layout.Context) { copyText(gtx, text) }},
		{Label: "Search in Slack", Icon: "search", Do: func() {
			if q := ctxSearchQuery(text); q != "" {
				h.Navigate("search?q=" + url.QueryEscape(q))
			}
		}},
	})
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(at), Width: 240})
}

// ctxSearchQuery is Slack's normalizeSearchQuery: trimmed, lines joined
// with spaces, at most 1023 characters.
func ctxSearchQuery(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }), " ")
	if r := []rune(s); len(r) > 1023 {
		s = string(r[:1023])
	}
	return s
}

// NewChannelContextMenu is the channel menu (Slack's ChannelContextMenu
// template) opened by a right-click: NewChannelMenu with its top-left
// corner at the pointer instead of right-aligned to the header's ⋮.
func NewChannelContextMenu(h Host, at image.Point, convID string) *Menu {
	return newChannelMenu(h, at, convID, false)
}

// UsergroupLink is where a user group mention links in Slack (the
// workspace's admin/user_groups page): what its link menu copies.
func UsergroupLink(h Host) string { return "https://" + teamDomain(h) + ".slack.com/admin/user_groups" }

// Items are the menu's current rows (after plugins' templates).
func (m *Menu) Items() []MenuItem { return m.items }

// filePillowPreview is the file pillow DeleteFileDialog shows under its
// question (margin_top_100): a 36px type tile, the name in 16px Bold and
// "uploader • time" in 13px muted (customSeparator "•", showYou), in a
// 12px-radius 1px-ring card with 12px padding.
func filePillowPreview(gtx layout.Context, h Host, f *richtext.File) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	pad, ic := dp(gtx, 12), dp(gtx, 36)
	tx := pad + ic + pad
	tw := maxInt(1, w-tx-pad)
	name := f.Title
	if name == "" {
		name = f.Name
	}
	detail := ""
	if u := f.User; u != "" {
		if u == actSelfID(h) {
			detail = "You"
		} else if usr, ok := lookupUser(h, u); ok {
			detail = userName(usr)
		}
	}
	if f.Created > 0 {
		when := dayTime(time.Unix(f.Created, 0), time.Now())
		if detail != "" {
			detail += "  •  "
		}
		detail += when
	}
	nc, nd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 16, 20, theme.Bold, cl.Fg, name, 1)
	})
	var dc op.CallOp
	var dd layout.Dimensions
	if detail != "" {
		dc, dd = record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 16, theme.Regular, cl.Muted, detail, 1)
		})
	}
	ht := maxInt(ic, nd.Size.Y+dp(gtx, 4)+dd.Size.Y) + 2*pad
	label := strings.ToUpper(f.Type)
	if len(label) > 4 {
		label = label[:4]
	}
	if label == "" {
		label = "FILE"
	}
	rrect(gtx, image.Rect(pad, pad, pad+ic, pad+ic), dp(gtx, 8), cl.Ring)
	lc, ld := record(loose(gtx, image.Pt(ic, ic)), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 10, theme.Black, cl.Muted, label)
	})
	macroAt(gtx, lc, image.Pt(pad+(ic-ld.Size.X)/2, pad+(ic-ld.Size.Y)/2))
	macroAt(gtx, nc, image.Pt(tx, pad-dp(gtx, 1)))
	if detail != "" {
		macroAt(gtx, dc, image.Pt(tx, pad+nd.Size.Y+dp(gtx, 4)))
	}
	stroke(gtx, image.Rect(0, 0, w, ht), dp(gtx, 12), 1, cl.Ring)
	return layout.Dimensions{Size: image.Pt(w, ht)}
}
