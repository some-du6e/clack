package popups

import (
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// FileAction is getMainFileAction (boot-shared XAF4) for a file card's
// click, as handleFileContainerClick (MJzc) acts on it:
//
//   - "remote": an app's remote file (is_external, external_type app):
//     its url_private in the browser;
//   - "view": an image, a video (the viewers above), a hosted PDF of
//     50 MB or less (nonImageCanUseFSFV), what the media viewer plays;
//   - "detail": snippets, posts, emails, huddle transcripts, docs;
//   - "open": canvases, lists, surfaces (their viewers);
//   - else getFileOpenMode (vITP eB): a file that isn't hosted (a Google
//     Drive or Dropbox link) is "external", its url_private in the
//     browser; a hosted PDF over 50 MB is "window" (openFileInNewWindow:
//     its permalink in a browser tab, Slack's own fallback); any other
//     hosted file (an .exe, a zip, a document, a binary) is "download":
//     downloadFileAndClog, in the app.
func FileAction(f *richtext.File) string {
	switch {
	case core.IsCanvasFile(f.Type, f.Mode, f.PrettyType), core.IsListFile(f.Type, f.Mode, f.PrettyType):
		return "open"
	case f.IsExternal && f.ExternalType == "app":
		return "remote"
	case f.IsImage || f.IsVideo || f.IsAudio || (fileHosted(f) && fileIsPDF(f) && f.Size > 0 && f.Size <= 5e7):
		return "view"
	case f.IsSnippet:
		return "detail"
	}
	switch f.Mode {
	case "snippet", "post", "space", "docs", "email", "huddle_transcript":
		return "detail"
	}
	if strings.EqualFold(f.Type, "email") {
		return "detail"
	}
	if !fileHosted(f) {
		return "external"
	}
	if !strings.HasPrefix(f.Mime, "image/") && f.Mime != "" {
		return "download"
	}
	if fileIsPDF(f) {
		return "window"
	}
	return "download"
}

// fileHosted is mode "hosted" (an older answer without a mode: a file
// that isn't external).
func fileHosted(f *richtext.File) bool {
	return f.Mode == "hosted" || (f.Mode == "" && !f.IsExternal)
}

func fileIsPDF(f *richtext.File) bool { return strings.EqualFold(f.Type, "pdf") }

// OpenFileCard is handleFileContainerClick for a file card in a surface
// that has no viewers of its own: an image opens h's media viewer (a
// video the video viewer, when h has one), a file Slack downloads
// downloads in the app (DownloadFile), and the rest go as
// OpenFileCardWith says.
func OpenFileCard(h Host, f *richtext.File, open func(string)) {
	OpenFileCardWith(f, open, func(f *richtext.File) { DownloadFile(h, f) }, func(f *richtext.File) bool { return ViewFile(h, f) })
}

// ViewFile is ViewFileIn with no conversation of its own (a list cell, a
// Block Kit or Slack Kit surface, a search card, Activity).
func ViewFile(h Host, f *richtext.File) bool { return ViewFileIn(h, f, "") }

// ViewFileIn is getMainFileAction's "view" where h can show it, as
// handleFileContainerClick (MJzc) dispatches it: openFileViewer (cqzr)
// with the file's id, so the media viewer's item is the file itself
// (OpenFileViewer): its Download, More actions, "– title" and the Details
// tab all need that id. An image opens on its thumbnail, which the viewer
// swaps for the original once that's decoded (lbShown; the original alone
// when there's no thumbnail); a playable video plays in the viewer where
// h has players (Videos). channel is the conversation it's opened from;
// "" falls back as openMediaViewerModal (ry08) does, to the primary
// view's channel (viewerChannel). false: h can't show f (the caller opens
// its page).
func ViewFileIn(h Host, f *richtext.File, channel string) bool {
	if h == nil || f == nil {
		return false
	}
	switch {
	case f.IsImage && (f.Thumb != nil || f.URL != ""):
		img := richtext.Image{URL: f.URL, Proxy: true}
		if f.Thumb != nil {
			img = *f.Thumb
		}
		if img.Title == "" {
			img.Title = f.Title
		}
		if img.Title == "" {
			img.Title = f.Name
		}
		return OpenFileViewer(h, f, img, viewerChannel(h, channel))
	case f.IsVideo && f.URL != "" && lbVideos(h) != nil:
		// the poster is lbVideoItem's (the file's thumb_video)
		return OpenFileViewer(h, f, richtext.Image{}, viewerChannel(h, channel))
	}
	return false
}

// viewerChannel is the media viewer's channelId as openMediaViewerModal
// (ry08) gives it: `channelId: a ?? ei`, the conversation the file was
// opened from, else the primary view's channel (getChannel(primaryView
// .id)?.id ?? "": a canvas tab's or a channel Files tab's conversation,
// none for the Files browser or a canvas in full view).
func viewerChannel(h Host, channel string) string {
	if channel != "" {
		return channel
	}
	if p, ok := h.(interface{ PrimaryChannel() string }); ok {
		return p.PrimaryChannel()
	}
	return ""
}

// RichFile is a Files browser, search or Files & links row (core.File)
// as a message attachment (DownloadFile's, the file menus'): the file
// itself, its URL and Download the file's own url_private, which Download
// saves; a video's poster is its thumb_video. The media viewer plays a
// video's mp4 (PlayableFile).
func RichFile(c core.File) *richtext.File {
	title := c.Title
	if title == "" {
		title = c.Name
	}
	f := &richtext.File{ID: c.ID, Name: c.Name, Title: c.Title, Type: c.Filetype, PrettyType: c.PrettyType,
		Mime: c.Mime, User: c.User, URL: c.URL, Download: c.URL, Permalink: c.Permalink, Size: c.Size,
		Created: c.Created, IsImage: c.IsImage}
	if c.IsVideo {
		f.IsVideo = true
		if c.Thumb != "" {
			f.Thumb = &richtext.Image{URL: c.Thumb, Alt: title, Title: title, Proxy: true}
		}
	}
	return f
}

// PlayableFile is RichFile for the media viewer: a video's URL is its
// progressive source, mp4 || url_private as a message attachment's, and
// its Download still the file itself, which the viewer's More actions
// save (an mp4 in the URL had the Files tab's Download save Slack's
// transcode instead of the file). An image's thumb_360 is its Thumb, as a
// message's image file has one: the viewer's first paint while the
// original loads.
func PlayableFile(c core.File) *richtext.File {
	f := RichFile(c)
	if c.IsVideo && c.MP4 != "" {
		f.URL = c.MP4
	}
	if c.IsImage && c.Thumb != "" {
		title := c.Title
		if title == "" {
			title = c.Name
		}
		f.Thumb = &richtext.Image{URL: c.Thumb, Alt: title, W: c.ThumbW, H: c.ThumbH, Title: title, Proxy: true}
	}
	return f
}

// OpenFileCardWith is OpenFileCard with the download and the viewer given
// (view nil: no viewer here):
//   - "download": download (an .exe, a zip, a document, in the app);
//   - "view" and "window": view, else the file's page (a PDF, Slack's
//     openFileInNewWindow; an image, audio or video where nothing here
//     shows it, as before);
//   - "detail": the page, Clack's stand-in for the file details Slack
//     opens (snippets, posts, emails, huddle transcripts);
//   - "open", "remote", "external": the page, or the file's own link.
func OpenFileCardWith(f *richtext.File, open func(string), download func(*richtext.File), view func(*richtext.File) bool) {
	if f == nil {
		return
	}
	switch FileAction(f) {
	case "download":
		if download != nil {
			download(f)
		}
	case "remote", "external":
		if f.URL != "" {
			open(f.URL)
		} else if f.Permalink != "" {
			open(f.Permalink)
		}
	case "view", "window":
		if view != nil && view(f) {
			return
		}
		if f.Permalink != "" {
			open(f.Permalink)
		}
	default: // "open", "detail"
		if f.Permalink != "" {
			open(f.Permalink)
		}
	}
}
