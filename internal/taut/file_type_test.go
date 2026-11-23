package taut

import "testing"

// A plugin's File goes up with its own type, as Slack's FormData sends a
// File: application/octet-stream when it has none (new File([..], name)
// without one), never a type guessed from its name.
func TestFileUploadType(t *testing.T) {
	for _, c := range []struct {
		f    File
		want string
	}{
		{File{Name: "a.js", Type: "text/javascript"}, "text/javascript"},
		{File{Name: "a.js"}, "application/octet-stream"},
		{File{Name: "audio_message.m4a", Type: "audio/mp4"}, "audio/mp4"},
	} {
		if got := c.f.UploadType(); got != c.want {
			t.Errorf("%+v: UploadType() = %q, want %q", c.f, got, c.want)
		}
	}
}
