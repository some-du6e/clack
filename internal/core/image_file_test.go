package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// AttachmentImage's Download (UvHM): an image whose url is a Slack file's
// (getFileIdFromUrlWithState: a Slack host's files/<member>/<id>/…,
// files-pri/<team>-<id>/… or files-tmb/<team>-<id>-<hash>/…) and that the
// client holds with a url_private_download is that file; any other url,
// host or member, an unheld file or one without a download URL, isn't.
func TestImageFile(t *testing.T) {
	a, _ := testApp(t)
	held := slack.File{ID: "F0IMAGE0001", Name: "shot.png", Title: "A shot", Mimetype: "image/png", Filetype: "png",
		URLPrivate: "https://files.slack.com/files-pri/T1-F0IMAGE0001/shot.png", URLPrivateDownload: "https://files.slack.com/files-pri/T1-F0IMAGE0001/download/shot.png"}
	noDL := slack.File{ID: "F0IMAGE0002", Name: "nodl.png", Mimetype: "image/png", Filetype: "png", URLPrivate: "https://files.slack.com/files-pri/T1-F0IMAGE0002/nodl.png"}
	a.S.PutUsers([]slack.User{{ID: "U0IMAGEOWN1", Name: "shotowner"}})
	a.S.SetLatest("C1", []slack.Message{{Type: "message", TS: "1700000000.000100", User: "U0IMAGEOWN1", Files: []slack.File{held, noDL}}}, false)
	for u, want := range map[string]string{
		"https://files.slack.com/files-pri/T1-F0IMAGE0001/shot.png":                "F0IMAGE0001",
		"https://files.slack.com/files-tmb/T1-F0IMAGE0001-0a1b2c3d4e/shot_720.png": "F0IMAGE0001",
		"https://example.slack.com/files/U0IMAGEOWN1/F0IMAGE0001/shot.png":         "F0IMAGE0001", // permalink, a known member
		"https://example.slack.com/files/shotowner/F0IMAGE0001/shot.png":           "F0IMAGE0001", // by the member's name
		"https://example.slack.com/files/U1/F0IMAGE0001/shot.png":                  "",            // "U1" isn't a member id, nor anyone's name
		"https://example.slack.com/files/U0NOBODY01/F0IMAGE0001/shot.png":          "",            // a member the store doesn't hold
		"https://files.slack.com/files-pri/T1-F0IMAGE0009/gone.png":                "",            // a file the client doesn't hold
		"https://files.slack.com/files-pri/T1-F0IMAGE0002/nodl.png":                "",            // no url_private_download
		"https://files.slack.evil.com/files-pri/T1-F0IMAGE0001/shot.png":           "",            // not a Slack host
		"https://cdn.example.com/files-pri/T1-F0IMAGE0001/shot.png":                "",
		"https://files.slack.com/files-pri/F0IMAGE0001/shot.png":                   "", // no team part
		"https://files.slack.com/files-pri/T1-notafile/shot.png":                   "",
		"not a url %": "",
	} {
		f, ok := a.ImageFile(u)
		got := ""
		if ok {
			got = f.ID
		}
		if got != want {
			t.Errorf("ImageFile(%q) = %q, want %q", u, got, want)
		}
		if ok && (f.Download != held.URLPrivateDownload || f.Name != "shot.png") {
			t.Errorf("ImageFile(%q): %+v", u, f)
		}
	}
}
