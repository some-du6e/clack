package popups

// The media viewer's entry points outside a message carry the file
// (Slack's openFileViewer, cqzr, gets a fileId at every one): ViewFile
// and ViewFileIn (file cards, list cells, Slack Kit, a view's richview),
// srOpenFile (search's file results), and RichFile / PlayableFile, the
// core.File mapping the Files browser and search share.

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// primaryHost is a host whose primary view shows a channel (the App's
// PrimaryChannel).
type primaryHost struct {
	*tzHost
	primary string
}

func (h *primaryHost) PrimaryChannel() string { return h.primary }

// lastViewer is the last media viewer h was shown, nil for none.
func lastViewer(h *tzHost) *Lightbox {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.shown) - 1; i >= 0; i-- {
		if lb, ok := h.shown[i].(*Lightbox); ok {
			return lb
		}
	}
	return nil
}

// lbDesc is a viewer as a test's message names it.
func lbDesc(lb *Lightbox) string {
	if lb == nil {
		return "no viewer"
	}
	return fmt.Sprintf("a viewer on file %q in channel %q (video %v)", lb.FileID(), lb.channel, lb.cur().video)
}

// fakePlayers is the window's players on the fake backend (nothing is
// ever opened: the viewers here aren't drawn).
func fakePlayers(t *testing.T, th *theme.Theme) *videoplayer.Controller {
	t.Helper()
	ctl := videoplayer.New(videoplayer.Env{
		Manager: media.NewManager(media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})),
		Repaint: func() {},
		Theme:   func() *theme.Theme { return th },
	})
	t.Cleanup(ctl.CloseAll)
	if !ctl.Available() {
		t.Fatal("the fake backend doesn't play videos")
	}
	return ctl
}

const (
	evImgID   = "F0VIEWIMG01"
	evVidID   = "F0VIEWVID01"
	evThumb   = "https://files.slack.com/files-tmb/T1-F0VIEWIMG01-x/x_360.png"
	evOrig    = "https://files.slack.com/files-pri/T1-F0VIEWIMG01/x.png"
	evPage    = "https://x.slack.com/files/U1/F0VIEWIMG01/x.png"
	evMP4     = "https://files.slack.com/files-tmb/T1-F0VIEWVID01-x/demo.mp4"
	evPoster  = "https://files.slack.com/files-tmb/T1-F0VIEWVID01-x/demo_thumb_video.jpg"
	evVidOrig = "https://files.slack.com/files-pri/T1-F0VIEWVID01/demo.mov"
)

func evImage() *richtext.File {
	return &richtext.File{ID: evImgID, Name: "x.png", Type: "png", Mime: "image/png", Mode: "hosted", IsImage: true,
		Thumb: &richtext.Image{URL: evThumb, W: 360, H: 240}, URL: evOrig, Download: evOrig, Permalink: evPage}
}

// An image file opens the media viewer on the file itself: the item has
// the file's id (bDav's iR, which looksLikeFileId accepts, so the Details
// tab and the file's actions have it), its thumbnail as the first paint
// and the original once decoded, its name as the title; the viewer's
// channel is the one it was opened from, else openMediaViewerModal's
// fallback (ry08 `channelId: a ?? ei`), the primary view's channel.
func TestViewFileCarriesTheFile(t *testing.T) {
	th := theme.New(false)
	h := &tzHost{th: th}
	f := evImage()
	if !ViewFileIn(h, f, "C0FROM00001") {
		t.Fatal("an image file didn't open the viewer")
	}
	lb := lastViewer(h)
	if lb == nil {
		t.Fatal("no viewer shown")
	}
	if !LooksLikeFileID(lb.FileID()) || lb.FileID() != evImgID {
		t.Errorf("the item's file id is %q, want %s", lb.FileID(), evImgID)
	}
	if idx, n := lb.Position(); idx != 0 || n != 1 {
		t.Errorf("item %d of %d, want the file alone", idx+1, n)
	}
	it := lb.cur()
	if it.file != f || it.video || !it.hosted() {
		t.Errorf("item file %p video %v hosted %v, want the file %p as a hosted image", it.file, it.video, it.hosted(), f)
	}
	if it.img.URL != evThumb || it.displayURL() != evOrig {
		t.Errorf("shows %q first and %q loaded, want the thumb %s then the original %s", it.img.URL, it.displayURL(), evThumb, evOrig)
	}
	if it.title() != "x.png" {
		t.Errorf("title %q, want the file's name", it.title())
	}
	if lb.channel != "C0FROM00001" {
		t.Errorf("channel %q, want the one it was opened from", lb.channel)
	}

	// no channel of its own: the primary view's, and none without one
	ph := &primaryHost{tzHost: &tzHost{th: th}, primary: "C0PRIMARY01"}
	if !ViewFile(ph, evImage()) {
		t.Fatal("ViewFile didn't open the viewer")
	}
	if lb := lastViewer(ph.tzHost); lb == nil || lb.channel != "C0PRIMARY01" || lb.FileID() != evImgID {
		t.Errorf("ViewFile opened %s, want file %s in the primary view's channel", lbDesc(lb), evImgID)
	}
	if !ViewFileIn(ph, evImage(), "C0FROM00001") || lastViewer(ph.tzHost).channel != "C0FROM00001" {
		t.Errorf("the opener's channel didn't win over the primary view's: %s", lbDesc(lastViewer(ph.tzHost)))
	}
	if !ViewFile(h, evImage()) || lastViewer(h).channel != "" {
		t.Errorf("a host with no primary channel opened %s", lbDesc(lastViewer(h)))
	}

	// an image with no thumbnail opens on its original
	bare := evImage()
	bare.Thumb = nil
	if !ViewFile(h, bare) {
		t.Fatal("an image without a thumbnail didn't open")
	}
	if it := lastViewer(h).cur(); it.img.URL != evOrig || it.file != bare {
		t.Errorf("a thumbless image shows %q (file %p), want the original on the file", it.img.URL, it.file)
	}
}

// A playable video plays in the viewer where the window has players, its
// item the file with its poster; where nothing plays it, ViewFile says
// so (the caller opens its page) and shows nothing.
func TestViewFileVideo(t *testing.T) {
	th := theme.New(false)
	v := &richtext.File{ID: evVidID, Name: "demo.mov", Mime: "video/quicktime", Mode: "hosted", IsVideo: true,
		URL: evMP4, Download: evVidOrig, Thumb: &richtext.Image{URL: evPoster, W: 1280, H: 720}}
	h := &tzHost{th: th}
	if ViewFile(h, v) || h.shownCount() != 0 {
		t.Errorf("a video opened %d overlays with no players", h.shownCount())
	}
	vh := &nrHost{tzHost: &tzHost{th: th}, vids: fakePlayers(t, th)}
	if !ViewFileIn(vh, v, "C0FROM00001") {
		t.Fatal("a playable video didn't open the viewer")
	}
	lb := lastViewer(vh.tzHost)
	if lb == nil || lb.FileID() != evVidID || lb.channel != "C0FROM00001" {
		t.Fatalf("opened %s, want video %s from C0FROM00001", lbDesc(lb), evVidID)
	}
	if it := lb.cur(); !it.video || it.file != v || it.img.URL != evPoster {
		t.Errorf("item video %v file %p poster %q, want the video file with its poster %s", it.video, it.file, it.img.URL, evPoster)
	}
	noURL := *v
	noURL.URL = ""
	if ViewFile(vh, &noURL) {
		t.Error("a video with nothing to play opened the viewer")
	}
}

// RichFile is the Files & links tab's cfRich (a row as DownloadFile's
// attachment: URL and Download are url_private); PlayableFile is
// cfPlayable plus an image's thumb_360 as its Thumb (the viewer's first
// paint): a video's URL is its mp4, its Download still the file.
func TestRichFileFromCore(t *testing.T) {
	img := core.File{ID: evImgID, Name: "x.png", Title: "Shot", Filetype: "png", PrettyType: "PNG", Mime: "image/png", User: "U1",
		Thumb: evThumb, ThumbW: 360, ThumbH: 240, URL: evOrig, Permalink: evPage, Size: 9000, Created: 1762600000, IsImage: true}
	r := RichFile(img)
	if r.ID != evImgID || r.Title != "Shot" || r.Type != "png" || r.URL != evOrig || r.Download != evOrig || r.Permalink != evPage ||
		!r.IsImage || r.IsVideo || r.Thumb != nil || r.User != "U1" || r.Size != 9000 || r.Created != 1762600000 {
		t.Errorf("RichFile(image) = %+v", r)
	}
	p := PlayableFile(img)
	if p.URL != evOrig || p.Thumb == nil || p.Thumb.URL != evThumb || p.Thumb.W != 360 || p.Thumb.H != 240 || p.Thumb.Title != "Shot" {
		t.Errorf("PlayableFile(image) = %+v thumb %+v", p, p.Thumb)
	}
	vid := core.File{ID: evVidID, Name: "demo.mov", Mime: "video/quicktime", Thumb: evPoster, URL: evVidOrig, MP4: evMP4, IsVideo: true}
	r = RichFile(vid)
	if !r.IsVideo || r.URL != evVidOrig || r.Download != evVidOrig || r.Thumb == nil || r.Thumb.URL != evPoster || r.Thumb.Title != "demo.mov" {
		t.Errorf("RichFile(video) = %+v", r)
	}
	if p := PlayableFile(vid); p.URL != evMP4 || p.Download != evVidOrig {
		t.Errorf("PlayableFile(video) URL %q Download %q, want the mp4 and the file", p.URL, p.Download)
	}
}

// A search file result opens the viewer on the file (openFileViewer with
// the result's fileId): an image's item is the result's file, a playable
// video plays in the viewer where the window has players.
func TestSearchOpenFileViewer(t *testing.T) {
	th := theme.New(false)
	h := &tzHost{th: th}
	srOpenFile(h, core.File{ID: evImgID, Name: "x.png", Mime: "image/png", IsImage: true, URL: evOrig, Thumb: evThumb, ThumbW: 360, ThumbH: 240})
	lb := lastViewer(h)
	if lb == nil || lb.FileID() != evImgID {
		t.Fatalf("an image result opened %s, want the viewer on %s", lbDesc(lb), evImgID)
	}
	if it := lb.cur(); it.img.URL != evThumb || it.displayURL() != evOrig {
		t.Errorf("the result shows %q then %q, want its thumb then its original", it.img.URL, it.displayURL())
	}
	vh := &nrHost{tzHost: &tzHost{th: th}, vids: fakePlayers(t, th)}
	srOpenFile(vh, core.File{ID: evVidID, Name: "demo.mov", Mime: "video/quicktime", IsVideo: true, URL: evVidOrig, MP4: evMP4, Thumb: evPoster})
	lb = lastViewer(vh.tzHost)
	if lb == nil || lb.FileID() != evVidID || !lb.cur().video || lb.cur().file.URL != evMP4 {
		t.Fatalf("a video result opened %s, want the viewer playing %s's mp4", lbDesc(lb), evVidID)
	}
}
