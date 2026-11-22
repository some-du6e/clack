package ui

// A message's audio files play in place (internal/ui/videoplayer's audio
// player, Slack's FileAudio), driven through Gio's router over
// fakeslack's #files-gallery on Go audio alone (no OS backend and no
// sound card, so every runner plays the same way): the voice clip's flat
// row and the mp3's card, each played by a click on its play button (the
// aac rendition first, which Go audio can't decode, then url_private) and
// paused by the next; no click opens the file's page. Before this an
// audio file's click opened its page in the browser and nothing played.
// TestAudioPlayerShots saves both players (FILES_SHOTS_DIR: files-shots).

import (
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// goAudioOnly plays audio files with Go audio alone, on its clock.
func goAudioOnly(t *testing.T) {
	t.Helper()
	prev := videoBackend
	videoBackend = media.NewChain(nil, &goaudio.Backend{Output: func(func([]int16)) (io.Closer, error) {
		return nil, errors.New("no sound card (the test's)")
	}})
	t.Cleanup(func() { videoBackend = prev })
}

// audioTile waits for message ts's audio card on screen (window
// coordinates).
func (e *perfEnv) audioTile(t *testing.T, ts string) messages.FileRect {
	t.Helper()
	var out messages.FileRect
	e.until("the audio player", 15*time.Second, func() bool {
		for _, r := range rectsOf(e.channelFiles(fakeslack.FilesChannelID), ts) {
			if r.File.IsAudio {
				out = r
				return true
			}
		}
		return false
	})
	return out
}

func (e *perfEnv) audioState(ts, id string) (videoplayer.State, bool) {
	if e.a.vids == nil {
		return videoplayer.State{}, false
	}
	return e.a.vids.TileState("channel", fakeslack.FilesChannelID, ts, id)
}

// audioPlay is message ts's play button, laid out (window coordinates).
func (e *perfEnv) audioPlay(t *testing.T, name, ts, id string) (videoplayer.State, image.Point) {
	t.Helper()
	var st videoplayer.State
	e.until(name+"'s play button", 10*time.Second, func() bool {
		var ok bool
		st, ok = e.audioState(ts, id)
		return ok && !st.AudioPlay.Empty()
	})
	tile := e.audioTile(t, ts)
	return st, mid(st.AudioPlay.Add(tile.R.Min))
}

var audioCases = []struct{ name, id string }{
	{"voice clip", fakeslack.FilesClipID},
	{"audio", fakeslack.FilesAudioID},
}

func TestAudioInlinePlay(t *testing.T) {
	goAudioOnly(t)
	var mu sync.Mutex
	var opened []string
	t.Cleanup(messages.SwapOpenURL(func(u string) {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, u)
	}))
	e := filesEnv(t, image.Pt(1440, 6000), false)
	if e.a.vids == nil || !e.a.vids.AudioAvailable() {
		t.Fatal("no audio players: the list didn't ask, or Go audio isn't in use")
	}
	clk := time.Second
	for _, tc := range audioCases {
		m := e.filesMsg(tc.name)
		st, play := e.audioPlay(t, tc.name, m.TS, tc.id)
		if st.Mounted {
			t.Fatalf("%s: a player before any click: %+v", tc.name, st)
		}
		e.clickAt(play, clk)
		clk += 500 * time.Millisecond
		e.until(tc.name+" playing", 15*time.Second, func() bool { st, _ := e.audioState(m.TS, tc.id); return st.Playing && !st.Loading })
		st, _ = e.audioState(m.TS, tc.id)
		if st.Alt == goaudio.DecodesAAC() {
			t.Errorf("%s: playing url_private %v; Go audio decodes AAC: %v (the aac rendition first, url_private where it can't)", tc.name, st.Alt, goaudio.DecodesAAC())
		}
		if st.Duration < 2950*time.Millisecond || st.Duration > 3100*time.Millisecond {
			t.Errorf("%s: duration %v, want the file's 3 s once its metadata came", tc.name, st.Duration)
		}
		e.settle(400 * time.Millisecond)
		if p := st.Player.State().Pos; p < 100*time.Millisecond {
			t.Errorf("%s: at %v after 400 ms of playing", tc.name, p)
		}
		e.clickAt(play, clk)
		clk += 500 * time.Millisecond
		e.until(tc.name+" paused", 5*time.Second, func() bool { st, _ := e.audioState(m.TS, tc.id); return !st.Playing })
	}
	if n := e.a.vids.Mounted(); n != 2 {
		t.Errorf("%d players for the two audio files", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 0 {
		t.Errorf("the clicks opened %v in the browser", opened)
	}
}

// cropShot saves the last frame's r (and a margin) as name.png
// (FILES_SHOTS_DIR only).
func (e *perfEnv) cropShot(name string, r image.Rectangle) {
	dir := os.Getenv("FILES_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(600 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img.SubImage(r.Inset(-24).Intersect(img.Bounds()))); err != nil {
		e.tb.Fatal(err)
	}
}

// Both players, light and dark: before any click, the clip playing (its
// waveform filling), the card playing, and the card hovered (its
// FileActions over the player).
func TestAudioPlayerShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	goAudioOnly(t)
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := filesEnv(t, image.Pt(1440, 900), dark)
			clk := time.Second
			for _, tc := range audioCases {
				m := e.filesMsg(tc.name)
				fr := e.fileInView(t, tc.name)
				slug := map[string]string{"voice clip": "clip", "audio": "card"}[tc.name]
				e.cropShot("audio-"+slug+"-idle-"+mode, fr.R)
				_, play := e.audioPlay(t, tc.name, m.TS, tc.id)
				e.clickAt(play, clk)
				clk += 500 * time.Millisecond
				e.until(tc.name+" a second in", 15*time.Second, func() bool {
					st, _ := e.audioState(m.TS, tc.id)
					return st.Player != nil && st.Player.State().Pos >= time.Second
				})
				e.cropShot("audio-"+slug+"-playing-"+mode, fr.R)
				e.clickAt(play, clk)
				clk += 500 * time.Millisecond
				e.until(tc.name+" paused", 5*time.Second, func() bool { st, _ := e.audioState(m.TS, tc.id); return !st.Playing })
				// hovered away from the controls: the card's FileActions
				in := fr.R.Min.Add(image.Pt(fr.R.Dx()/2, 20))
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(in.X), float32(in.Y)), Time: clk})
				clk += 100 * time.Millisecond
				e.cropShot("audio-"+slug+"-paused-hover-"+mode, fr.R)
			}
		})
	}
}
