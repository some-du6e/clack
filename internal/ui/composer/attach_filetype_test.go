package composer

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A pending file's type (its mimetype, and the File a plugin renaming it
// gets) is its File's as Slack's client holds it: a clip's the recorder's
// audio/mp4, any other the type Chromium gives its name, here on an OS that
// types .py and nothing else. Go's mime table, which it was, gave a .js
// "text/javascript; charset=utf-8".
func TestAttachmentFileTypes(t *testing.T) {
	defer slack.SetPlatformMimeTypes(func(ext string) (string, bool) { return "text/x-python-script", ext == "py" })()
	for _, c := range []struct {
		chip *attChip
		want string
	}{
		{&attChip{name: "BlockKitSend (2).js"}, "text/javascript"},
		{&attChip{name: "notes.txt"}, "text/plain"},
		{&attChip{name: "script.py"}, "text/x-python-script"},
		{&attChip{name: "main.go"}, ""},
		{&attChip{name: "audio_message.m4a", clip: &clipMeta{subtype: "slack_audio", mime: "audio/mp4"}}, "audio/mp4"},
	} {
		if got := attFileType(c.chip.name, c.chip.mime()); got != c.want {
			t.Errorf("%s: attFileType %q, want %q", c.chip.name, got, c.want)
		}
		if got := c.chip.pendingLocked().Mimetype; got != c.want {
			t.Errorf("%s: pending file mimetype %q, want %q", c.chip.name, got, c.want)
		}
		// a plugin's rename (AnonymiseFileNames) leaves the File's type
		name := c.chip.name
		c.chip.name = "renamed.bin"
		if got := c.chip.pendingLocked().Mimetype; got != c.want {
			t.Errorf("%s renamed renamed.bin: pending file mimetype %q, want the File's %q", name, got, c.want)
		}
	}
}
