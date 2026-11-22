//go:build windows && cgo

package mf

import (
	"testing"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// Audio files are Go audio's (internal/media/goaudio, which decodes AAC
// with Media Foundation's source reader and keeps time without an audio
// output): the Media Engine takes videos only. On the runners (no audio
// output) an audio-only engine, muted, failed every file with
// MEDIA_ERR_SRC_NOT_SUPPORTED 2.2 s in (ci run 44990778509).
func TestMFTakesNoAudio(t *testing.T) {
	for _, fx := range mediatest.AudioFixtures() {
		if (backend{}).Takes(media.Source{Mime: fx.Mime, Ext: fx.Ext}) {
			t.Errorf("Media Foundation takes %s (%s, %s)", fx.Name, fx.Mime, fx.Ext)
		}
	}
	if !(backend{}).Takes(media.Source{Mime: "video/mp4", Ext: "mp4"}) {
		t.Error("Media Foundation doesn't take a video")
	}
}
