package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// NewVideoActionsMenu is a message video's More actions: Slack's
// VideoActionsButton (frPc, build 2c0d02ca130f) as its player's corner
// opens it in a channel or a thread (isMediaGalleryViewContext). In
// Slack's order, with the items Clack has:
//
//	Download, Share file…, Copy link
//	Open in ▸ Open in thread (not in a thread pane); View details
//	Delete file (Delete clip for a clip)
//
// Left out until Clack has them: Open in new window (Slack's pop-out
// player window), Edit thumbnail…, Add to folder and Add to section, and
// Report to Slack. View details opens the file's page, as FileActions'
// View file details does. (Dock to corner is only with
// showPlaybackSettings, the media viewer's menu: the viewer's own Dock
// button docks it there.)
// inThread: the message shows in a thread pane.
func NewVideoActionsMenu(h Host, btn image.Rectangle, m core.Message, f *richtext.File, inThread bool) *Menu {
	return NewVideoActionsMenuClosing(h, btn, m, f, inThread, nil)
}

// NewVideoActionsMenuClosing is NewVideoActionsMenu with onClose run when
// the menu closes (frPc's onPopoverClose: the player's overlay stays
// active while it's open).
func NewVideoActionsMenuClosing(h Host, btn image.Rectangle, m core.Message, f *richtext.File, inThread bool, onClose func()) *Menu {
	var items []MenuItem
	group := func(g ...MenuItem) {
		var keep []MenuItem
		for _, it := range g {
			if it.Label != "" {
				keep = append(keep, it)
			}
		}
		if len(keep) == 0 {
			return
		}
		if len(items) > 0 {
			items = append(items, Sep())
		}
		items = append(items, keep...)
	}
	var download, share, copyLink MenuItem
	if f.URL != "" || f.Download != "" {
		download = MenuItem{Label: "Download", Do: func() { DownloadFile(h, videoDownload(f)) }}.In(nsFile)
	}
	if m.Channel != "" && m.TS != "" {
		share = MenuItem{Label: "Share file…", Do: func() { h.ShowOverlay(NewForwardModal(h, m)) }}.In(nsFile)
	}
	if link := f.Permalink; link != "" {
		copyLink = MenuItem{Label: "Copy link", DoCtx: func(gtx layout.Context) {
			copyText(gtx, link)
			h.Toast(nsFile.T("Link copied to clipboard")) // enqueueCopyLinkSuccessToast (Bwpd)
		}}.In(nsFile)
	}
	group(download, share, copyLink)
	var openIn, details MenuItem
	if !inThread && m.Channel != "" && m.TS != "" {
		root := m.ThreadTS
		if root == "" {
			root = m.TS
		}
		thread := MenuItem{Label: "Open in thread", Do: func() { h.OpenThread(m.Channel, root) }}.In(nsFile)
		openIn = MenuItem{Label: "Open in", Sub: []MenuItem{thread}}.In(nsFile)
	}
	if f.Permalink != "" {
		details = MenuItem{Label: "View details", Do: func() { lbOpen(h, f.Permalink) }}.In(nsFile)
	}
	group(openIn, details)
	del := fileDeleteItem(h, m, f)
	if del.Label != "" && videoClip(f) { // frPc: eR (isVideoClip) ? "Delete clip" : "Delete file"
		del = MenuItem{Label: "Delete clip", Danger: true, Do: del.Do}.In(nsFile)
	}
	group(del)
	if len(items) == 0 {
		return nil
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Width: 220, Popover: "bottom-left", TryOpposite: true, OnClose: onClose})
}

// videoDownload is what Download saves: the file itself
// (url_private_download, under its own name: I83v's getFileDownloadUrl),
// not the mp4 transcode the player streams from f.URL.
func videoDownload(f *richtext.File) *richtext.File {
	d := *f
	if d.Download != "" {
		d.URL = d.Download
	}
	return &d
}

// videoClip is zRjl's isVideoClip: a clip recorded in Slack (subtype
// slack_video) or a video story.
func videoClip(f *richtext.File) bool {
	return f != nil && (f.Subtype == "slack_video" || f.Subtype == "video_story")
}
