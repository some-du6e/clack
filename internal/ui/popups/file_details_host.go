package popups

import (
	"regexp"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// FileDetailsHost is the optional Host side of Slack's FileDetails
// (boot-shared eg5B, FileDetailsWithSubscription): the media viewer's
// Details tab draws it with its header hidden (bDav's
// flexpaneHeaderProps {hideHeader: true}), and with the tab's gate off
// the viewer's "View file details" / "View details" navigate to it in the
// secondary view (toFileDetails). The app implements it (ui's
// file_details_pane.go); popups can't import the views it is built from.
type FileDetailsHost interface {
	// FileDetailsPane is FileDetails for file f with its header hidden,
	// to draw in the viewer's Details tab. channel is the conversation
	// the viewer was opened in ("" for none): FileDetails' channelId.
	FileDetailsPane(f *richtext.File, channel string) FileDetailsPane
	// OpenFileDetails navigates the secondary view to FileDetails for f
	// (jhdg toFileDetails(f.ID, {channelId: channel})).
	OpenFileDetails(f *richtext.File, channel string)
}

// FileDetailsPane is one FileDetails in the viewer's Details tab.
type FileDetailsPane interface {
	// Layout fills gtx.Constraints.Max: the scrolling body, then the
	// flexpane's footer.
	Layout(gtx layout.Context) layout.Dimensions
	// FileID is the file it shows (it refetches when the viewer's item
	// changes: the viewer asks for a new pane).
	FileID() string
}

// fileDetailsHost is h's FileDetailsHost, if it has one.
func fileDetailsHost(h Host) FileDetailsHost {
	fh, _ := h.(FileDetailsHost)
	return fh
}

var fileIDRe = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)

// LooksLikeFileID is oRye looksLikeFileId: /^F[A-Z0-9]{8,14}$/.
func LooksLikeFileID(id string) bool { return fileIDRe.MatchString(id) }

// mediaViewerDetailsOn is 2mfZ zq: `"treatment" ===
// getGroupForUser(state, "media_viewer_file_details_tab")` (no exposure
// logged on read).
func mediaViewerDetailsOn(h Host) bool {
	if h == nil {
		return false
	}
	a := h.Core()
	return a != nil && a.Experiment(core.ExpMediaViewerFileDetailsTab) == "treatment"
}

// OpenFileViewer is Slack's openFileViewer (cqzr) for one file outside a
// message (the Files browser, search results, a canvas, a list cell, a
// permalink): the media viewer over f alone, carrying the file (its id
// for the Details tab, Download, More actions), with its image at img (or
// f's original); a playable video plays in the viewer. channel is the
// conversation it's opened from ("" for none). It returns false when f
// isn't viewable there (the caller falls back as it did).
func OpenFileViewer(h Host, f *richtext.File, img richtext.Image, channel string) bool {
	if f == nil {
		return false
	}
	lb := NewFileGallery(h, []GalleryItem{{Img: img, File: f}}, 0)
	if lb == nil {
		return false
	}
	lb.channel = channel
	h.ShowOverlay(lb)
	return true
}
