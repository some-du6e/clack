package slick

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCustomSoundsLikeSlack: Slick's CustomSounds, from Slick's build with
// its main half. Clack plays a notification sound on the page through
// Slack's sound playback (Taut.PlaySound, client-boot Ofe8: an <audio>
// element per sound), so the element's player opens Slack's own sound
// (its CDN URL, which the host opens itself). With CustomSounds on, its
// patched HTMLMediaElement.prototype.play swaps that element's source for
// the file the user chose: slick-custom-sounds://current/sound.mp3?p=<path>,
// which the page fetches through the main half's protocol handler, so the
// player opens it with the file's bytes. Stopped, the plugin puts Slack's
// sound back.
func TestCustomSoundsLikeSlack(t *testing.T) {
	dir := pluginsDir(t)
	h := newHarness(t)
	const slackSound = "https://a.slack-edge.com/bv1-13-br/knock_brush-ac2b6e8.mp3"
	opened := func(what string, n int, ok func(mediaOpened) bool) mediaOpened {
		t.Helper()
		var last mediaOpened
		eventually(t, 10*time.Second, what, func() bool {
			o := h.media.opens()
			if len(o) < n {
				return false
			}
			last = o[n-1]
			return ok(last)
		})
		return last
	}

	h.rt() // the plugins' page is up while plugins run; nothing has started it here yet
	if !playSound(h, slackSound) {
		t.Fatal("PlaySound didn't play: no runtime or no media host")
	}
	opened("Slack's own sound", 1, func(o mediaOpened) bool { return o.src == slackSound && o.data == nil })

	sound := filepath.Join(t.TempDir(), "my-ding.mp3")
	body := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), bytes.Repeat([]byte("custom sound "), 40)...)
	if err := os.WriteFile(sound, body, 0o644); err != nil {
		t.Fatal(err)
	}
	h.importFile(filepath.Join(dir, "CustomSounds.js"))
	h.importFile(filepath.Join(dir, "CustomSounds.main.js"))
	if err := h.s.SetOption(h.ctx(), "CustomSounds", "soundPath", sound); err != nil {
		t.Fatal(err)
	}
	if err := h.s.SetEnabled(h.ctx(), "CustomSounds", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("CustomSounds"); !info.Running || info.StartError != "" || info.MainError != "" {
		t.Fatalf("CustomSounds: %+v", info)
	}
	eventually(t, 5*time.Second, "its protocol", func() bool { return has(h.state().Protocols, "slick-custom-sounds") })

	playSound(h, slackSound)
	got := opened("the user's sound", 2, func(o mediaOpened) bool { return o.src != slackSound })
	want := "slick-custom-sounds://current/sound.mp3?p=" + url.QueryEscape(sound)
	if got.src != strings.ReplaceAll(want, "+", "%20") && got.src != want {
		t.Errorf("the element opened %q, want %q", got.src, want)
	}
	if !bytes.Equal(got.data, body) {
		t.Errorf("the element opened %d bytes, want the chosen file's %d (fetched through the plugin's protocol handler)", len(got.data), len(body))
	}

	// stopped, the element plays Slack's sound again
	if err := h.s.SetEnabled(h.ctx(), "CustomSounds", false); err != nil {
		t.Fatal(err)
	}
	playSound(h, slackSound)
	opened("Slack's sound once the plugin stopped", 3, func(o mediaOpened) bool { return o.src == slackSound && o.data == nil })
}
