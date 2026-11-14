package views

// Renaming and removing a channel tab, shared by the tab bar's menu (ui's
// getTabContextMenu) and Manage tabs (ManageChannelTabsListItem), as
// Slack's web client has them (CDN build fa938a086569):
//
//   - RenameTabModal (O1Cc, popups.RenameTabModal) saves with
//     updateTabLabel (Fx4h): the label at once, then conversations.updateTab
//     {id, channel_id, label}; a failure puts the old label back.
//   - useRemoveTab (plzS) by the tab's type: a folder with bookmarks
//     (+Ltq Iz, folders aside) asks first (TabDeleteConfirmationModal: "Are
//     you sure you want to delete this folder with {n} items?", Delete),
//     else it goes; a canvas or list is
//     maybeOpenRemoveFileTabConfirmationModal (OH3f, core.FileTabRemoval):
//     the channel's knowledge canvas asks "Remove Channel Knowledge
//     Canvas?" (Remove: ai.channelKnowledge.disable, then removeTab), one
//     shared nowhere else asks "Also remove access to this canvas?" / "…
//     list?" ("Since it hasn’t been shared anywhere else in {channelName},
//     you can remove access if you’d like."; Remove takes the share away,
//     Keep is keep_file_share, the close button does nothing); any other
//     tab goes at once (a built-in one is hidden). The open tab goes back
//     to Messages when it is the one removed (navigateToTab Channel).
//
// maybeShowCannotDeleteCkaFolder (the Channel Expert agent's folder while
// the agent is in the channel) isn't ported: it needs the channel's app
// members search.

import (
	"context"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"

	"gioui.org/layout"
)

// EditTab is the tab a rename or remove acts on.
type EditTab struct {
	ID, Type  string
	Label     string // the tab's own label ("" none)
	FileTitle string // a canvas or list tab's file title
	FileID    string
	FolderID  string // a folder tab's data.folder_bookmark_id
}

// TabLabelHost is optionally implemented by the Host: the tab bar shows
// a tab's new label at once (updateTabLabel's setChannel), or its old one
// back after a failure.
type TabLabelHost interface {
	SetTabLabel(ch, id, label string)
}

// OpenRenameTab opens RenameTabModal for t; folder is isRenameFolder (the
// tab bar's menu passes it for a folder, Manage tabs never does).
func OpenRenameTab(h Host, ch string, t EditTab, folder bool) {
	ph, ok := h.(popups.Host)
	if !ok {
		return
	}
	start := firstNonEmpty(t.Label, t.FileTitle)
	ph.ShowOverlay(popups.NewRenameTabModal(ph, popups.RenameTabOpts{Label: start, Folder: folder, Canvas: t.Type == "canvas",
		Save: func(ctx context.Context, label string) error { return renameTab(h, ctx, ch, t, label) }}))
}

func renameTab(h Host, ctx context.Context, ch string, t EditTab, label string) error {
	c := h.Core()
	if c == nil {
		return context.Canceled
	}
	lh, _ := h.(TabLabelHost)
	post := func(f func()) {
		if p, ok := h.(interface{ Post(func()) }); ok {
			p.Post(f)
			return
		}
		f()
	}
	if lh != nil {
		post(func() { lh.SetTabLabel(ch, t.ID, label) })
	}
	err := c.RenameTab(ctx, ch, t.ID, label)
	if err != nil && lh != nil {
		post(func() { lh.SetTabLabel(ch, t.ID, t.Label) })
	}
	return err
}

// RemoveTab is useRemoveTab for t (see the file comment).
func RemoveTab(h Host, ch string, t EditTab) {
	c := h.Core()
	ph, ok := h.(popups.Host)
	if c == nil || !ok {
		return
	}
	remove := func(keep bool) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var err error
			if t.Type == "folder" || t.Type == "canvas" || t.Type == "list" {
				err = c.RemoveChannelTab(ctx, ch, t.ID, keep)
			} else {
				err = c.HideTab(ctx, ch, t.ID) // a built-in tab is hidden (removeTab)
			}
			fh, ok := h.(FolderHost)
			switch {
			case !ok:
			case err != nil: // removeTab's catch: the old tabs back (setChannel), no message
				fh.TabsChanged(ch)
			default:
				fh.TabRemoved(ch, t.ID)
			}
		}()
	}
	post := func(f func()) {
		if p, ok := h.(interface{ Post(func()) }); ok {
			p.Post(f)
			return
		}
		f()
	}
	switch t.Type {
	case "folder":
		n := c.FolderItemCount(ch, t.FolderID)
		if n == 0 {
			remove(false)
			return
		}
		name := firstNonEmpty(t.Label, nsTabbed.T("Bookmarks"))
		ph.ShowOverlay(popups.NewDeleteFolderModal(ph, n, name, tabChannelName(c, ch), func() { remove(false) }))
	case "canvas", "list":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			what, list, _ := c.FileTabRemoval(ctx, ch, t.FileID)
			post(func() {
				switch what {
				case core.FileTabRemoveKnowledge:
					ph.ShowOverlay(NewRemoveKnowledgeCanvasModal(ph, func(ctx context.Context) error {
						if err := c.DisableChannelKnowledge(ctx, ch); err != nil {
							return err
						}
						remove(false)
						return nil
					}))
				case core.FileTabRemoveAsk:
					ph.ShowOverlay(NewRemoveFileTabModal(ph, list, tabConvName(c, ch), func(keep bool) { remove(keep) }))
				default:
					remove(false)
				}
			})
		}()
	default:
		// useRemoveTab g: Messages first when it is the open tab, then
		// removeTab
		if fh, ok := h.(FolderHost); ok {
			fh.TabRemoved(ch, t.ID)
		}
		remove(false)
	}
}

// tabChannelName is the channel's "#name" (ChannelName with its prefix).
func tabChannelName(c *core.App, ch string) string {
	if cv, ok := c.Conv(ch); ok && cv.Name != "" {
		return "#" + strings.TrimPrefix(cv.Name, "#")
	}
	return ch
}

// tabConvName is the remove-access sentence's place: "" for a DM or
// group DM (isIm || isMpim: "this conversation"), else "#name".
func tabConvName(c *core.App, ch string) string {
	if cv, ok := c.Conv(ch); ok && (cv.Kind == "im" || cv.Kind == "mpim") {
		return ""
	}
	return tabChannelName(c, ch)
}

var nsChTabs = i18n.NS("channel_tabs")

// NewRemoveFileTabModal is RemoveFileTabConfirmationModal: "Also remove
// access to this canvas?" (a list: "… this list?"), the sentence naming
// channel ("" a DM: "this conversation"), Keep (the cancel button: the tab
// goes, the share stays) and Remove (primary: both go). The close button
// and Escape only close it.
func NewRemoveFileTabModal(h popups.Host, list bool, channel string, done func(keep bool)) *popups.Modal {
	title := nsChTabs.T("Also remove access to this canvas?")
	if list {
		title = nsChTabs.T("Also remove access to this list?")
	}
	body := i18n.Plain(nsChTabs.RT("Since it hasn’t been shared anywhere else in this conversation, you can remove access if you’d like."))
	if channel != "" {
		body = i18n.Plain(nsChTabs.RT("Since it hasn’t been shared anywhere else in {channelName}, you can remove access if you’d like.",
			"channelName", channel))
	}
	ok := &popups.ModalButton{Label: nsChTabs.T("Remove"), Kind: popups.BtnPrimary, Do: func() bool { done(false); return true }}
	keep := &popups.ModalButton{Label: nsChTabs.T("Keep"), Kind: popups.BtnOutline, Do: func() bool { done(true); return true }}
	th := h.Theme()
	return popups.NewModal(h, popups.ModalOpts{Title: title, AutoFocus: ok, Body: func(gtx layout.Context) layout.Dimensions {
		return modalPara(gtx, th, body)
	}, Buttons: []*popups.ModalButton{keep, ok}})
}

// NewRemoveKnowledgeCanvasModal is RemoveKnowledgeCanvasTabConfirmationModal:
// "Remove Channel Knowledge Canvas?", the refresh sentence, Cancel and
// Remove (primary; it stays open if disabling the refresh fails).
func NewRemoveKnowledgeCanvasModal(h popups.Host, remove func(ctx context.Context) error) *popups.Modal {
	ns := i18n.NS("tabbed-channel")
	body := ns.T("Removing this tab will disable the automatic nightly refresh of the Channel Knowledge Canvas for this channel. You can re-enable it later.")
	var m *popups.Modal
	pending := false
	ok := &popups.ModalButton{Label: ns.T("Remove"), Kind: popups.BtnPrimary, Enabled: func() bool { return !pending },
		Do: func() bool {
			pending = true
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				err := remove(ctx)
				post := func(f func()) {
					if p, ok := h.(interface{ Post(func()) }); ok {
						p.Post(f)
						return
					}
					f()
				}
				post(func() {
					pending = false
					if err == nil {
						m.Dismiss()
					}
					h.Invalidate()
				})
			}()
			return false
		}}
	th := h.Theme()
	m = popups.NewModal(h, popups.ModalOpts{Title: ns.T("Remove Channel Knowledge Canvas?"), AutoFocus: ok,
		Body:    func(gtx layout.Context) layout.Dimensions { return modalPara(gtx, th, body) },
		Buttons: []*popups.ModalButton{{Label: ns.T("Cancel"), Kind: popups.BtnOutline}, ok}})
	return m
}

// modalPara is a confirmation's paragraph (15/22 content-pry).
func modalPara(gtx layout.Context, th *theme.Theme, s string) layout.Dimensions {
	return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Color: th.C.Fg}, s)
}
