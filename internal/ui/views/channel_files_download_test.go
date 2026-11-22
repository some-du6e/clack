package views

// The channel Files tab's Download of a video saves the file itself. Not
// in channel_files_video_test.go, which files-shots copies onto ui
// (before-ui), where core.File has no MP4.

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Files tab's Download (its row's button, DownloadFile: f.URL, else
// f.Download) and the viewer's More actions' Download (videoDownload:
// f.Download, else f.URL) save the video itself, url_private as for
// every file; only playback takes Slack's mp4 transcode. Download saved
// the mp4 after 312afcac. The fixture's video is its own mp4, so this one
// isn't.
func TestChannelFilesVideoDownloadIsTheFile(t *testing.T) {
	app, fs := cfFake(t)
	base := &cfHost{filesHost: &filesHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}, imgs: cfImages(fs.URL)}
	h := &cfVideoHost{cfHost: base}
	v := NewChannelFiles(h, fakeslack.FilesChannelID)
	orig := fs.URL + "files-pri/" + fakeslack.TeamID + "-F0CFDLVID1/walkthrough.mov"
	mp4 := fs.URL + "files-tmb/" + fakeslack.TeamID + "-F0CFDLVID1-1a2b3c/walkthrough.mp4"
	it := fileItem{File: core.File{ID: "F0CFDLVID1", Name: "walkthrough.mov", Filetype: "mov", PrettyType: "QuickTime Movie",
		Mime: "video/quicktime", URL: orig, MP4: mp4, IsVideo: true, Thumb: fs.URL + "files-tmb/F0CFDLVID1/thumb_video.png",
		Permalink: fs.URL + "files/F0CFDLVID1/walkthrough.mov"}}
	if f := cfRich(&it); f.URL != orig || f.Download != orig && f.Download != "" || f.Permalink != it.Permalink {
		t.Errorf("the Files tab's Download gets %q (Download %q), Copy link %q; want the file itself %q and its permalink",
			f.URL, f.Download, f.Permalink, orig)
	}
	v.media.items = []fileItem{it}
	v.openMedia(it)
	if len(h.items) != 1 || h.items[0].File == nil {
		t.Fatalf("the viewer opened %+v, want the video", h.items)
	}
	f := h.items[0].File
	if f.URL != mp4 {
		t.Errorf("the viewer plays %q, want the mp4 %q", f.URL, mp4)
	}
	if f.Download != orig {
		t.Errorf("the viewer's Download gets %q (its URL %q), want the file itself %q", f.Download, f.URL, orig)
	}
}
