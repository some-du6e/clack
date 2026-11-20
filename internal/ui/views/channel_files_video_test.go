package views

// The channel Files tab's videos in the media viewer, where videos play
// in place. files-shots copies this file onto ui (before-ui), where the
// video still opens its page: the check fails there.

import (
	"image"
	"strings"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cfVideoHost is the Files tab's host where videos play (the App's
// VideosPlay), keeping the gallery it was asked to open.
type cfVideoHost struct {
	*cfHost
	items []popups.GalleryItem
	idx   int
}

func (h *cfVideoHost) VideosPlay() bool { return true }

func (h *cfVideoHost) OpenMediaGallery(items []popups.GalleryItem, idx int) {
	h.items, h.idx = items, idx
	h.cfHost.OpenMediaGallery(items, idx)
}

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

// A media cell's video opens the media viewer at it, with the channel's
// loaded images and videos to page through (handleFileContainerClick
// with fileChannelMediaInfo: fileIsViewableInMediaViewer keeps playable
// video); its item is the video's own file with its poster.
func TestChannelFilesVideoViewer(t *testing.T) {
	app, fs := cfFake(t)
	base := &cfHost{filesHost: &filesHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}, imgs: cfImages(fs.URL)}
	h := &cfVideoHost{cfHost: base}
	v := NewChannelFiles(h, fakeslack.FilesChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the media browser", func() bool { return len(v.media.items) > 0 })
	v.setType(cfMedia)
	e.frame()
	var vid *fileItem
	n := 0
	for i := range v.media.items {
		m := &v.media.items[i]
		if strings.HasPrefix(m.Mime, "video/") && m.ID == "F0FILESVID01" {
			vid = m
		}
		if m.isImage() || m.ID == "F0FILESVID01" {
			n++
		}
	}
	if vid == nil {
		t.Fatal("no video in the media browser")
	}
	v.openMedia(*vid)
	if got := base.last(); got != "gallery:"+vid.ID+"/"+itoaTest(n) {
		t.Fatalf("the video opened %q, want the viewer at it with the %d images and videos", got, n)
	}
	it := h.items[h.idx]
	if it.File == nil || !it.File.IsVideo || it.File.URL == "" {
		t.Fatalf("the viewer's item %+v, want the playable video with its source", it.File)
	}
	if it.Img.URL == "" || !strings.Contains(it.Img.URL, "F0FILESVID01") {
		t.Errorf("its poster %q, want the video's thumb_video", it.Img.URL)
	}
}
