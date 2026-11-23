package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// NewAudioClipMenuClosing is an audio clip's More actions: Slack's
// AudioClipOverflowMenu (boot-shared o/6F, build 35b42de8b870) at the
// player's ⋮, "top-left". In Slack's order, with the items Clack has:
//
//	Download
//	Share clip…, Copy link to audio clip, View details
//	Delete clip
//
// Left out until Clack has them: Open in new window (the file's own
// window, open_original), Dock to corner (the audio mini player) and
// Report to Slack. View details opens the file's page, as the video's
// does. onClose runs once it closes.
func NewAudioClipMenuClosing(h Host, btn image.Rectangle, m core.Message, f *richtext.File, onClose func()) *Menu {
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
	var download, share, copyLink, details MenuItem
	if f.URL != "" || f.Download != "" {
		download = MenuItem{Label: "Download", Do: func() { DownloadFile(h, videoDownload(f)) }}.In(nsFile)
	}
	group(download)
	if m.Channel != "" && m.TS != "" {
		share = MenuItem{Label: "Share clip…", Do: func() { h.ShowOverlay(NewForwardModal(h, m)) }}.In(nsFile)
	}
	if link := f.Permalink; link != "" {
		copyLink = MenuItem{Label: "Copy link to audio clip", DoCtx: func(gtx layout.Context) {
			copyText(gtx, link)
			h.Toast(nsFile.T("Link copied to clipboard"))
		}}.In(nsFile)
		details = MenuItem{Label: "View details", Do: func() { lbOpen(h, f.Permalink) }}.In(nsFile)
	}
	group(share, copyLink, details)
	del := fileDeleteItem(h, m, f)
	if del.Label != "" {
		del = MenuItem{Label: "Delete clip", Danger: true, Do: del.Do}.In(nsFile)
	}
	group(del)
	if len(items) == 0 {
		return nil
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Width: 220, Popover: "top-left", TryOpposite: true, OnClose: onClose})
}
