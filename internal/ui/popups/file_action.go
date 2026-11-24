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

// OpenFileCard is handleFileContainerClick for a file card whose viewers
// (an image's, a video's, a canvas's or list's) the caller already
// tried: a file Slack downloads downloads in the app (DownloadFile); a
// remote or external file opens its own link with open (Slack's
// window.open of url_private); a PDF, which Clack's viewer can't show,
// takes openFileInNewWindow's permalink. Nothing else opens: a Slack
// file never goes to the browser in Slack's place.
func OpenFileCard(h Host, f *richtext.File, open func(string)) {
	OpenFileCardWith(f, open, func(f *richtext.File) { DownloadFile(h, f) })
}

// OpenFileCardWith is OpenFileCard with the download given (a host
// without popups' Host: Slack Kit components; tests).
func OpenFileCardWith(f *richtext.File, open func(string), download func(*richtext.File)) {
	if f == nil {
		return
	}
	switch FileAction(f) {
	case "download":
		if download != nil {
			download(f)
		}
	case "open":
		// a canvas or a list the caller has no viewer for: its page,
		// as before (the hosts with viewers open them first)
		if f.Permalink != "" {
			open(f.Permalink)
		}
	case "remote", "external":
		if f.URL != "" {
			open(f.URL)
		} else if f.Permalink != "" {
			open(f.Permalink)
		}
	case "view", "window":
		// a PDF (FSFV's, which Clack lacks: openFileInNewWindow), or a
		// video where the caller has no player (Clack's fallback, as
		// before: its page)
		if (fileIsPDF(f) || f.IsVideo) && !f.IsImage && f.Permalink != "" {
			open(f.Permalink)
		}
	case "detail":
		if f.Permalink != "" && (f.Mode == "post" || f.Mode == "docs" || f.Mode == "space") {
			open(f.Permalink)
		}
	}
}
