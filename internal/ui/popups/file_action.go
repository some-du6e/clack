package popups

import (
	"strings"
	"time"

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

// ViewFile is getMainFileAction's "view" where h can show it: an image
// with a thumbnail in the media viewer (OpenLightbox, the original's
// URL), a playable video in the video viewer when h has one. false: h
// can't.
func ViewFile(h Host, f *richtext.File) bool {
	switch {
	case f.IsImage && f.Thumb != nil:
		img := *f.Thumb
		if f.URL != "" {
			img.URL, img.Proxy = f.URL, true
		}
		if img.Title == "" {
			img.Title = f.Title
		}
		h.OpenLightbox(img, core.Message{})
		return true
	case f.IsVideo:
		if vo, ok := h.(interface {
			OpenVideoViewer(f *richtext.File, m core.Message, at time.Duration, autoplay bool) bool
		}); ok {
			return vo.OpenVideoViewer(f, core.Message{}, 0, true)
		}
	}
	return false
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
