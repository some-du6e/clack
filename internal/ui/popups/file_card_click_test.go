package popups

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// viewerHost records the media viewer's opens: an image that isn't a
// file (OpenLightbox) by its URL, a file's viewer (OpenFileViewer's
// overlay) by what it shows once loaded.
type viewerHost struct {
	*tzHost
	viewed []string
}

func (h *viewerHost) OpenLightbox(img richtext.Image, _ core.Message) {
	h.viewed = append(h.viewed, img.URL)
}

func (h *viewerHost) ShowOverlay(o Overlay) {
	if lb, ok := o.(*Lightbox); ok {
		h.viewed = append(h.viewed, lb.cur().displayURL())
	}
	h.tzHost.ShowOverlay(o)
}

// OpenFileCard is what the surfaces with no viewer of their own (search
// cards, Block Kit, the email and work object panes) do with a file
// card's click (the review of 93ab723be: images and audio lost theirs):
// an image opens the media viewer on its original; an .exe or zip
// downloads in the app; a PDF, audio, a post, an email or a huddle
// transcript opens its page (Clack has no viewer or details for them);
// an external file opens its own link. OpenFileCardWith with no viewer
// sends the image to its page too.
func TestOpenFileCardActions(t *testing.T) {
	const page = "https://x.slack.com/files/U1/F1/x"
	thumb := &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-F1/x_360.png", W: 360, H: 240}
	for _, c := range []struct {
		name string
		f    richtext.File
		want string // "view:<url>", "download", "open:<url>", ""
	}{
		{"image", richtext.File{Type: "png", Mime: "image/png", Mode: "hosted", IsImage: true, Thumb: thumb, URL: "https://files.slack.com/files-pri/T1-F1/x.png"}, "view:https://files.slack.com/files-pri/T1-F1/x.png"},
		{"exe", richtext.File{Type: "exe", Mime: "application/x-msdownload", Mode: "hosted", Download: "https://files.slack.com/files-pri/T1-F1/download/x.exe"}, "download"},
		{"zip", richtext.File{Type: "zip", Mime: "application/zip", Mode: "hosted"}, "download"},
		{"pdf", richtext.File{Type: "pdf", Mime: "application/pdf", Mode: "hosted", Size: 1 << 20}, "open:" + page},
		{"audio", richtext.File{Type: "mp3", Mime: "audio/mpeg", Mode: "hosted", IsAudio: true}, "open:" + page},
		{"post", richtext.File{Type: "space", Mode: "space"}, "open:" + page},
		{"email", richtext.File{Type: "email", Mode: "email"}, "open:" + page},
		{"huddle transcript", richtext.File{Type: "huddle_transcript", Mode: "huddle_transcript"}, "open:" + page},
		{"gdrive", richtext.File{Type: "gdoc", Mode: "external", IsExternal: true, ExternalType: "gdrive", URL: "https://docs.google.com/d/1"}, "open:https://docs.google.com/d/1"},
	} {
		f := c.f
		f.ID, f.Name, f.Permalink = "F1", "x", page
		h := &viewerHost{tzHost: &tzHost{th: theme.New(false)}}
		var got []string
		open := func(u string) { got = append(got, "open:"+u) }
		OpenFileCardWith(&f, open, func(*richtext.File) { got = append(got, "download") }, func(f *richtext.File) bool { return ViewFile(h, f) })
		for _, u := range h.viewed {
			got = append(got, "view:"+u)
		}
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: %v, want %s", c.name, got, c.want)
		}
	}
	// no viewer here (Slack Kit before the shell's): the image's page
	img := richtext.File{ID: "F1", Type: "png", Mime: "image/png", Mode: "hosted", IsImage: true, Thumb: thumb, Permalink: page}
	var got []string
	OpenFileCardWith(&img, func(u string) { got = append(got, u) }, nil, nil)
	if len(got) != 1 || got[0] != page {
		t.Errorf("an image with no viewer opened %v, want its page", got)
	}
}

// The review of c38b91b8, again: an audio file's Download is the upload,
// url_private_download (getFileDownloadUrl), never the aac rendition its
// player plays nor url_private; a file without url_private_download
// downloads nothing ("No download URL").
func TestAudioDownloadIsTheUpload(t *testing.T) {
	h, a, base := downloadHost(t)
	audio := &richtext.File{ID: "F0AUD01", Name: "standup.mp3", IsAudio: true, Mode: "hosted",
		URL:      base + "files-pri/T1-F0AUD01/standup.mp3",
		Download: base + "files-pri/T1-F0AUD01/download/standup.mp3",
		MediaURL: base + "files-tmb/T1-F0AUD01-x/standup_audio.mp4"}
	if !StartDownload(h, audio) {
		t.Fatal("no download for an audio file with url_private_download")
	}
	d, _ := a.Downloads().Get(audio.ID)
	if !strings.HasPrefix(d.URL, audio.Download) || strings.Contains(d.URL, "files-tmb") {
		t.Errorf("an audio file downloads %q, want url_private_download %q", d.URL, audio.Download)
	}
	if StartDownload(h, &richtext.File{ID: "F0AUD02", IsAudio: true, URL: audio.URL, MediaURL: audio.MediaURL}) {
		t.Error("a file with no url_private_download started a download")
	}
}

// downloadHost is a popups host over fakeslack whose downloads wait at
// the folder chooser until the test ends (nothing is fetched), and the
// fake's base URL.
func downloadHost(t *testing.T) (*tzHost, *core.App, string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	gate := make(chan struct{})
	dir := t.TempDir()
	ChooseDownloadDir = func() (string, bool, error) { <-gate; return dir, true, nil }
	t.Cleanup(func() {
		close(gate)
		a.Downloads().CancelAll()
		ChooseDownloadDir = nil
	})
	return &tzHost{a: a, th: theme.New(false), im: imgcache.New(noFetch, "", nil)}, a, fs.URL
}
