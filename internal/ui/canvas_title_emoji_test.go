package ui

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
)

// The whole client: the canvas editor's ":" typeahead is the App's emoji
// search (canvasview EmojiSearch), images included.
func TestCanvasEmojiSource(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	var _ canvasview.EmojiSearch = e.a
	var hits []canvasview.EmojiHit
	e.until("emoji for \"tad\"", 10*time.Second, func() bool {
		hits = e.a.CanvasEmoji("tad", 6)
		return len(hits) > 0
	})
	if hits[0].Name != "tada" || hits[0].URL == "" {
		t.Errorf("first hit %+v, want tada with its image", hits[0])
	}
}
