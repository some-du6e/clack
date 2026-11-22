package popups

import (
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// The review of c38b91b8: an audio file's Download fetched the aac
// rendition its player plays. It fetches the upload: url_private_download,
// else url_private; other files keep url_private first.
func TestAudioDownloadIsTheUpload(t *testing.T) {
	const (
		orig = "https://files.slack.com/files-pri/T1-F1/standup.mp3"
		dl   = "https://files.slack.com/files-pri/T1-F1/download/standup.mp3"
		aac  = "https://files.slack.com/files-tmb/T1-F1-x/standup_audio.mp4"
	)
	audio := &richtext.File{ID: "F1", IsAudio: true, URL: orig, Download: dl, MediaURL: aac}
	if got := downloadSource(audio); got != dl {
		t.Errorf("an audio file downloads %q, want url_private_download %q", got, dl)
	}
	if got := downloadSource(&richtext.File{ID: "F1", IsAudio: true, URL: orig, MediaURL: aac}); got != orig {
		t.Errorf("an audio file with no url_private_download downloads %q, want url_private %q", got, orig)
	}
	if got := downloadSource(&richtext.File{ID: "F2", URL: orig, Download: dl}); got != orig {
		t.Errorf("a file downloads %q, want url_private %q as before", got, orig)
	}
	if got := videoDownload(audio).URL; got != dl {
		t.Errorf("the clip menu's Download takes %q, want %q", got, dl)
	}
}
