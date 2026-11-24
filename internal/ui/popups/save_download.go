package popups

import (
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// SaveDownload is downloadFile with a URL of its own (a list's export:
// exportListsTemplate, listsFormat): on the desktop startDesktopDownload
// with f's id and src, so the export streams to the download folder like
// any download, with the download toast, fresh every time (core's
// Downloads never caches). false when it can't start (no URL, not a
// Slack file URL, no session): the caller says what Slack says ("CSV
// download failed").
func SaveDownload(h Host, f *richtext.File, src string) bool {
	c := h.Core()
	if c == nil || f == nil || f.ID == "" || src == "" || strings.HasPrefix(src, "blob:") || !c.C.CredentialsOK(src) {
		return false
	}
	ds := c.Downloads()
	ds.Start(core.DownloadRequest{ID: f.ID, URL: src, File: *f, Dir: downloadDir})
	ds.AddActive(f.ID)
	if DownloadsChanged != nil {
		DownloadsChanged(h)
	}
	h.Invalidate()
	return true
}
