package popups

// Round 3's review, a MINOR: More actions' Download saved the mp4
// transcode the player streams under the original file's name. It saves
// the file itself (url_private_download), as Slack's getFileDownloadUrl
// does. videoDownloadOf is video_menu_hooks_test.go's.
//
// A NIT: a clip's More actions said "Delete file"; Slack's says "Delete
// clip" (frPc, isVideoClip).

import (
	"image"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

func TestVideoDownloadIsTheFile(t *testing.T) {
	f := &richtext.File{ID: "F1", Name: "IMG_1234.MOV", IsVideo: true,
		URL:      "https://files.slack.com/files-tmb/T1-F1-abc/img_1234.mp4",
		Download: "https://files.slack.com/files-pri/T1-F1/download/img_1234.mov"}
	d := videoDownloadOf(f)
	if d.URL != f.Download || d.Name != "IMG_1234.MOV" {
		t.Errorf("Download saves %q as %q, want the file %q", d.URL, d.Name, f.Download)
	}
	if f.URL == f.Download {
		t.Error("the player's source changed")
	}
}

func TestVideoClipMenuSaysDeleteClip(t *testing.T) {
	h := mkHost(t)
	m := mkMsg(true)
	for _, tc := range []struct{ subtype, want, not string }{
		{"slack_video", "Delete clip", "Delete file"},
		{"video_story", "Delete clip", "Delete file"},
		{"", "Delete file", "Delete clip"},
	} {
		f := &richtext.File{ID: "F0CLIP00001", Name: "clip.mp4", Title: "clip.mp4", IsVideo: true, User: fakeslack.SelfID, Subtype: tc.subtype,
			URL: "https://files.slack.com/files-tmb/T1-F0CLIP00001-abc/clip.mp4", Permalink: "https://x.slack.com/files/U1/F0CLIP00001/clip.mp4"}
		mn := NewVideoActionsMenu(h, image.Rect(0, 0, 24, 24), m, f, false)
		if mn == nil {
			t.Fatalf("subtype %q: no menu", tc.subtype)
		}
		if l := mn.Labels(); !slices.Contains(l, tc.want) || slices.Contains(l, tc.not) {
			t.Errorf("subtype %q: %q, want %q", tc.subtype, l, tc.want)
		}
	}
}
