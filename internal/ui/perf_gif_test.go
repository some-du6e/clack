package ui

// The GIF memory regression test (perf.yml's perf job, "GIF memory"
// step): #gif-wall at Hack Club scale, every message a large animated GIF
// (fakeslackd -gifs; synthetic), paged back and scrolled through with a
// few on screen at a time. Its own file: build.sh overlays only
// perf_test.go on the base tree.
//
//   - The image cache's decoded bytes (stills, players, kept encoded
//     bytes) never pass its budget, at any frame.
//   - The GIFs play at their drawn size (past the old 160px cap): frames
//     advance while the scroll rests.
//   - Only what's on screen plays: once the scroll rests, at most the
//     GIFs a screen holds have players; once the channel is left, none.
//
// Skipped unless CLACK_PERF_GIF is set. CLACK_PERF_GIF_REPORT names a
// markdown file for the numbers (the step summary).

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
)

func TestPerfGIFMemory(t *testing.T) {
	if os.Getenv("CLACK_PERF_GIF") == "" {
		t.Skip("CLACK_PERF_GIF not set (perf.yml's GIF memory step)")
	}
	n := 300
	if v, err := strconv.Atoi(os.Getenv("CLACK_PERF_GIF")); err == nil && v > 1 {
		n = v
	}
	sz := fakeslack.HackClub
	sz.GIFs = n
	var fs *fakeslack.Server
	if u := os.Getenv("CLACK_FAKESLACK_URL"); u != "" { // fakeslackd -gifs n: the heap is the client's alone
		fs = fakeslack.Remote(u, sz)
	} else {
		var err error
		if fs, err = fakeslack.Start(sz); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
	}
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	images := e.a.images
	ch := fakeslack.GIFWallID
	e.openChannel(e.fs.ChannelIDs()[1])
	e.settle(2 * time.Second)
	heapBefore := liveHeapMB()

	var peak imgcache.Stats
	frames := 0
	sample := func(what string) imgcache.Stats {
		s := images.Stats()
		frames++
		if s.Bytes > s.Limit {
			t.Fatalf("%s: %.1f MB decoded, over the %.0f MB budget (%d entries, %d players holding %.1f MB, %.1f MB encoded)",
				what, gifMB(s.Bytes), gifMB(s.Limit), s.Entries, s.Players, gifMB(s.PlayerBytes), gifMB(s.EncodedBytes))
		}
		if s.Bytes > peak.Bytes {
			peak.Bytes, peak.Entries, peak.EncodedBytes = s.Bytes, s.Entries, s.EncodedBytes
		}
		peak.Players, peak.PlayerBytes = max(peak.Players, s.Players), max(peak.PlayerBytes, s.PlayerBytes)
		return s
	}
	step := func(what string, dy float32) {
		if dy != 0 {
			e.scroll(e.listPoint(), dy)
		}
		e.frame()
		sample(what)
		time.Sleep(15 * time.Millisecond)
	}

	e.openChannel(ch)
	e.until("#gif-wall's history", 30*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	t0 := time.Now()
	for dl := time.Now().Add(4 * time.Minute); e.loadedMessages(ch) < n && time.Now().Before(dl); {
		step("paging back", -3000)
	}
	if got := e.loadedMessages(ch); got < n {
		t.Fatalf("paged back to %d of %d messages", got, n)
	}
	paged := time.Since(t0)
	for i := 0; i < 40; i++ { // the top
		step("at the top", -3000)
	}
	// down through every row, a few GIFs on screen at a time (a GIF
	// message is under 400px: past the bottom, the list stays put)
	t1 := time.Now()
	steps := n*400/240 + 40
	for i := 0; i < steps; i++ {
		step("scrolling down", 240)
	}
	scrolled := time.Since(t1)
	heapScrolled := liveHeapMB()

	// the scroll rests: the GIFs on screen play, nothing else does
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
		step("resting", 0)
	}
	rest := sample("resting")
	adv0 := rest.Advances
	for dl := time.Now().Add(2 * time.Second); time.Now().Before(dl); {
		step("resting", 0)
	}
	rest = sample("resting")
	onScreen := e.size.Y/200 + 2 // a GIF row is over 200px tall: at most this many show
	switch {
	case rest.Players == 0:
		t.Errorf("resting on #gif-wall: no GIF plays")
	case rest.Players > onScreen:
		t.Errorf("resting: %d players, but at most %d GIFs fit on screen (the ones scrolled past kept theirs)", rest.Players, onScreen)
	case rest.Advances-adv0 < 10:
		t.Errorf("resting 2 s: %d frames advanced (GIFs at 40 ms a frame don't play)", rest.Advances-adv0)
	case max(rest.LargestFrame.X, rest.LargestFrame.Y) <= 160:
		t.Errorf("the largest frame playing is %v: GIFs past 160px don't play at their drawn size", rest.LargestFrame)
	}

	// another channel: #gif-wall's players all go
	e.openChannel(e.fs.ChannelIDs()[1])
	for dl := time.Now().Add(4 * time.Second); time.Now().Before(dl); {
		step("left", 0)
	}
	left := sample("left")
	if left.Players != 0 || left.PlayerBytes != 0 {
		t.Errorf("4 s after leaving #gif-wall: %d players still hold %.1f MB", left.Players, gifMB(left.PlayerBytes))
	}
	heapLeft := liveHeapMB()

	t.Logf("#gif-wall: %d GIF messages, paged back in %v, %d steps down in %v, %d frames sampled", n, paged.Round(time.Second), steps, scrolled.Round(time.Second), frames)
	t.Logf("decoded peak %.1f MB of %.0f MB (%d entries, %.1f MB encoded); players at most %d, %.1f MB", gifMB(peak.Bytes), gifMB(rest.Limit), peak.Entries, gifMB(peak.EncodedBytes), peak.Players, gifMB(peak.PlayerBytes))
	t.Logf("resting: %d players (%.1f MB), %d frames advanced in 2 s, largest frame %v; left: %d players", rest.Players, gifMB(rest.PlayerBytes), rest.Advances-adv0, rest.LargestFrame, left.Players)
	t.Logf("live Go heap: %.0f MB before, %.0f MB scrolled, %.0f MB after leaving", heapBefore, heapScrolled, heapLeft)
	if p := os.Getenv("CLACK_PERF_GIF_REPORT"); p != "" {
		md := "### GIF memory (#gif-wall, Hack Club scale)\n\n" +
			fmt.Sprintf("%d large animated GIFs (fakeslackd -gifs: files with thumb_480_gif, /giphy attachments), paged back and scrolled through a few at a time, layout only.\n\n", n) +
			"| | value |\n|---|---|\n" +
			fmt.Sprintf("| decoded bytes, peak (budget) | %.1f MB (%.0f MB) |\n", gifMB(peak.Bytes), gifMB(rest.Limit)) +
			fmt.Sprintf("| encoded GIF bytes kept at that peak | %.1f MB |\n", gifMB(peak.EncodedBytes)) +
			fmt.Sprintf("| players at most, while scrolling | %d (%.1f MB) |\n", peak.Players, gifMB(peak.PlayerBytes)) +
			fmt.Sprintf("| players resting on a screen | %d (%.1f MB), %d frames in 2 s, largest %d×%d |\n", rest.Players, gifMB(rest.PlayerBytes), rest.Advances-adv0, rest.LargestFrame.X, rest.LargestFrame.Y) +
			fmt.Sprintf("| players 4 s after leaving | %d (%.1f MB) |\n", left.Players, gifMB(left.PlayerBytes)) +
			fmt.Sprintf("| live Go heap: before / scrolled / left | %.0f / %.0f / %.0f MB |\n", heapBefore, heapScrolled, heapLeft) +
			fmt.Sprintf("| RSS after leaving | %.0f MB |\n", rssMB())
		if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
			t.Error(err)
		}
	}
}

func gifMB(n int) float64 { return float64(n) / (1 << 20) }
