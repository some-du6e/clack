package imgcache

import (
	"bytes"
	"compress/lzw"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/op/paint"
)

// Synthetic GIFs only (nothing here comes from Slack).

func fillPaletted(r image.Rectangle, pal color.Palette, f func(x, y int) uint8) *image.Paletted {
	p := image.NewPaletted(r, pal)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p.SetColorIndex(x, y, f(x, y))
		}
	}
	return p
}

func encodeGIF(t testing.TB, g *gif.GIF) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// synthGIF: a 32×32 canvas; frame 0 red over the left half only (so the
// first frame doesn't cover the canvas), frame 1 blue over the right half
// (disposal none: red stays), frame 2 green everywhere with a 0 delay.
func synthGIF(t testing.TB) []byte {
	t.Helper()
	fr := func(r image.Rectangle, c color.Color) *image.Paletted {
		p := image.NewPaletted(r, palette.Plan9)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				p.Set(x, y, c)
			}
		}
		return p
	}
	return encodeGIF(t, &gif.GIF{
		Image: []*image.Paletted{
			fr(image.Rect(0, 0, 16, 32), color.RGBA{255, 0, 0, 255}),
			fr(image.Rect(16, 0, 32, 32), color.RGBA{0, 0, 255, 255}),
			fr(image.Rect(0, 0, 32, 32), color.RGBA{0, 255, 0, 255}),
		},
		Delay:    []int{5, 20, 0}, // 50 ms, 200 ms, 0 (= 100 ms)
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{Width: 32, Height: 32, ColorModel: color.Palette(palette.Plan9)},
	})
}

// bigGIF is a w×h GIF of n frames, each a full frame of its own colour
// with a moving bar (so every frame differs), delay cs centiseconds.
func bigGIF(t testing.TB, w, h, n, cs, loop int) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0, 0, 0, 255}}
	for i := 1; i < 256; i++ {
		pal = append(pal, color.RGBA{uint8(i * 7), uint8(255 - i), uint8(i * 3), 255})
	}
	g := &gif.GIF{Config: image.Config{Width: w, Height: h, ColorModel: pal}, LoopCount: loop}
	for i := 0; i < n; i++ {
		bar := i * w / n
		g.Image = append(g.Image, fillPaletted(image.Rect(0, 0, w, h), pal, func(x, y int) uint8 {
			if x >= bar && x < bar+w/n+1 {
				return 0
			}
			return uint8(1 + (i*13)%255)
		}))
		g.Delay = append(g.Delay, cs)
	}
	return encodeGIF(t, g)
}

// refFrames is image/gif's composition of data, at its own size: each
// frame drawn over the canvas, then disposed of (the old decodeGIF).
func refFrames(t *testing.T, data []byte) []*image.RGBA {
	t.Helper()
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	var out []*image.RGBA
	for i, fr := range g.Image {
		var prev *image.RGBA
		if g.Disposal[i] == gif.DisposalPrevious {
			prev = image.NewRGBA(canvas.Rect)
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		c := image.NewRGBA(canvas.Rect)
		copy(c.Pix, canvas.Pix)
		out = append(out, c)
		switch g.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, prev.Pix)
		}
	}
	return out
}

// interlacedGIF writes a GIF by hand (image/gif can't write interlaced
// frames): a 7×11 canvas, frame 0 interlaced over all of it, frame 1 an
// interlaced 5×6 at (1,3) with index 3 transparent, a local colour table
// and disposal to previous, frame 2 a plain 2×2 at (5,9).
func interlacedGIF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	le := func(v int) { b.WriteByte(byte(v)); b.WriteByte(byte(v >> 8)) }
	b.WriteString("GIF89a")
	le(7)
	le(11)
	b.WriteByte(0xf1) // global table, 4 entries
	b.WriteByte(0)
	b.WriteByte(0)
	b.Write([]byte{10, 20, 30, 200, 0, 0, 0, 200, 0, 0, 0, 200})
	b.Write([]byte{0x21, 0xff, 11})
	b.WriteString("NETSCAPE2.0")
	b.Write([]byte{3, 1, 0, 0, 0})
	frame := func(x, y, w, h int, inter bool, local []byte, trans int, disp byte, idx func(x, y int) byte) {
		pk := disp << 2
		if trans >= 0 {
			pk |= 1
		}
		b.Write([]byte{0x21, 0xf9, 4, pk, 4, 0, byte(max(trans, 0)), 0})
		b.WriteByte(0x2c)
		le(x)
		le(y)
		le(w)
		le(h)
		var f byte
		if inter {
			f |= 0x40
		}
		if local != nil {
			f |= 0x80 | 1 // 4 entries
		}
		b.WriteByte(f)
		b.Write(local)
		b.WriteByte(2) // LZW minimum code size
		var rows [][]byte
		for j := 0; j < h; j++ {
			r := j
			if inter {
				r = interlacedRow(j, h)
			}
			row := make([]byte, w)
			for i := range row {
				row[i] = idx(i, r)
			}
			rows = append(rows, row)
		}
		var z bytes.Buffer
		lw := lzw.NewWriter(&z, lzw.LSB, 2)
		for _, r := range rows {
			lw.Write(r)
		}
		lw.Close()
		d := z.Bytes()
		for len(d) > 0 {
			n := min(len(d), 255)
			b.WriteByte(byte(n))
			b.Write(d[:n])
			d = d[n:]
		}
		b.WriteByte(0)
	}
	frame(0, 0, 7, 11, true, nil, -1, gif.DisposalNone, func(x, y int) byte { return byte((x + 2*y) % 4) })
	frame(1, 3, 5, 6, true, []byte{255, 255, 0, 0, 255, 255, 128, 128, 128, 9, 9, 9}, 3, gif.DisposalPrevious, func(x, y int) byte { return byte((x * y) % 4) })
	frame(5, 9, 2, 2, false, nil, -1, gif.DisposalBackground, func(x, y int) byte { return 1 })
	b.WriteByte(0x3b)
	return b.Bytes()
}

// TestGIFDecodeMatchesImageGIF: composing a frame at a time (gifstream.go)
// gives image/gif's composition exactly: partial frames, transparency,
// each disposal, local colour tables, interlaced frames; then a second
// pass starts from an empty canvas.
func TestGIFDecodeMatchesImageGIF(t *testing.T) {
	pal := color.Palette{color.RGBA{}, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}}
	local := color.Palette{color.RGBA{9, 9, 9, 255}, color.RGBA{1, 2, 3, 255}, color.RGBA{}, color.RGBA{200, 100, 50, 255}}
	disposals := encodeGIF(t, &gif.GIF{
		Image: []*image.Paletted{
			fillPaletted(image.Rect(0, 0, 20, 10), pal, func(x, y int) uint8 { return uint8(1 + (x+y)%3) }),
			fillPaletted(image.Rect(2, 2, 12, 8), pal, func(x, y int) uint8 { return uint8((x * y) % 4) }), // 0: transparent
			fillPaletted(image.Rect(5, 1, 18, 9), local, func(x, y int) uint8 { return uint8((x + 3*y) % 4) }),
			fillPaletted(image.Rect(0, 0, 6, 6), pal, func(x, y int) uint8 { return 3 }),
			fillPaletted(image.Rect(10, 0, 20, 10), pal, func(x, y int) uint8 { return uint8(x % 4) }),
		},
		Delay:    []int{1, 2, 3, 4, 5},
		Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious, gif.DisposalPrevious, gif.DisposalNone},
		Config:   image.Config{Width: 20, Height: 10, ColorModel: pal},
	})
	for name, data := range map[string][]byte{"disposals": disposals, "interlaced": interlacedGIF(t), "synth": synthGIF(t)} {
		t.Run(name, func(t *testing.T) {
			want := refFrames(t, data)
			ix, err := indexGIF(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(ix.frames) != len(want) {
				t.Fatalf("%d frames indexed, image/gif has %d", len(ix.frames), len(want))
			}
			d := newGIFDecoder(ix, data)
			for pass := 0; pass < 2; pass++ {
				for i := range want {
					if got := d.compose(); got != i {
						t.Fatalf("pass %d: composed frame %d, want %d", pass, got, i)
					}
					if !bytes.Equal(d.canvas.Pix, want[i].Pix) {
						for y := 0; y < ix.h; y++ {
							for x := 0; x < ix.w; x++ {
								if g, w := d.canvas.RGBAAt(x, y), want[i].RGBAAt(x, y); g != w {
									t.Fatalf("pass %d frame %d: pixel (%d,%d) %v, image/gif %v", pass, i, x, y, g, w)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestGIFIndex(t *testing.T) {
	ix, err := indexGIF(synthGIF(t))
	if err != nil {
		t.Fatal(err)
	}
	if ix.w != 32 || ix.h != 32 || len(ix.frames) != 3 || ix.loop != 0 {
		t.Fatalf("index %dx%d, %d frames, loop %d", ix.w, ix.h, len(ix.frames), ix.loop)
	}
	for i, want := range []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 100 * time.Millisecond} {
		if d := ix.frames[i].delay; d != want {
			t.Errorf("frame %d delay %v, want %v", i, d, want)
		}
	}
	if r := ix.frames[1].r; r != image.Rect(16, 0, 32, 32) {
		t.Errorf("frame 1 at %v", r)
	}
	for _, loop := range []int{-1, 0, 2} {
		ix, err := indexGIF(bigGIF(t, 8, 8, 3, 5, loop))
		if err != nil {
			t.Fatal(err)
		}
		if ix.loop != loop {
			t.Errorf("LoopCount %d indexed as %d", loop, ix.loop)
		}
	}
	data := synthGIF(t)
	for _, cut := range []int{len(data) - 1, len(data) - 30} { // cut off: what's there
		if ix, err := indexGIF(data[:cut]); err != nil || len(ix.frames) == 0 {
			t.Errorf("cut at %d: %v", cut, err)
		}
	}
	if _, err := indexGIF([]byte("GIF89a")); err == nil {
		t.Error("a 6-byte GIF indexed")
	}
}

// gifCache serves GIFs by URL and counts repaints.
func gifCache(t *testing.T, gifs map[string][]byte) (*Cache, *atomic.Int32) {
	t.Helper()
	var inv atomic.Int32
	var mu sync.Mutex
	c := New(func(_ context.Context, u string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if b, ok := gifs[u]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("no %s", u)
	}, "", func() { inv.Add(1) })
	return c, &inv
}

func readyEntry(t *testing.T, c *Cache, url string, px int) *entry {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		if _, ok := c.Get(url, px); ok {
			c.mu.Lock()
			e := c.entries[key{url, px}].Value.(*entry)
			c.mu.Unlock()
			return e
		}
	}
	t.Fatal("image never loaded")
	return nil
}

// drawFor calls Get every few ms for d, as frames of a window showing it
// would, and returns the distinct frames it was given.
func drawFor(c *Cache, url string, px int, d time.Duration) []paint.ImageOp {
	var seen []paint.ImageOp
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		op, ok := c.Get(url, px)
		if !ok {
			continue
		}
		if len(seen) == 0 || seen[len(seen)-1] != op {
			seen = append(seen, op)
		}
	}
	return seen
}

func playerOf(c *Cache, url string, px int) *player {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key{url, px}]; ok {
		return el.Value.(*entry).play
	}
	return nil
}

func TestAnimatedGIF(t *testing.T) {
	const u = "https://emoji.slack-edge.com/T1/party/abc.gif"
	c, inv := gifCache(t, map[string][]byte{u: synthGIF(t)})
	e := readyEntry(t, c, u, 32)
	if !c.Animated(u, 32) || !c.IsAnimation(u, 32) || e.frames != 3 {
		t.Fatalf("not animated: %d frames", e.frames)
	}
	if e.size != image.Pt(32, 32) {
		t.Fatalf("size %v: the canvas, not the first frame's bounds", e.size)
	}
	// the still is frame 0 on its canvas: red left, transparent right
	if l, r := e.px[0].RGBAAt(4, 16), e.px[0].RGBAAt(28, 16); l.R < 200 || r.A != 0 {
		t.Errorf("still: left %v right %v", l, r)
	}
	before := inv.Load()
	seen := drawFor(c, u, 32, 600*time.Millisecond)
	if len(seen) < 3 {
		t.Fatalf("%d frames drawn in 600 ms of a 350 ms loop", len(seen))
	}
	if inv.Load() == before {
		t.Error("no repaint for the next frame")
	}
	p := playerOf(c, u, 32)
	if p == nil || !p.keep || len(p.frames) != 3 {
		t.Fatalf("player %+v: a 32px GIF keeps its frames", p)
	}
	m := c.Stats()
	if m.Players != 1 || m.PlayerBytes != 3*32*32*4 {
		t.Errorf("stats %+v", m)
	}
}

// TestLargeGIFAnimatesAtDrawnSize: a GIF far past the old 160px cap plays,
// its frames decoded at the size drawn (360px), streaming (its frames
// don't fit animKeepBytes), in order, each one different.
func TestLargeGIFAnimatesAtDrawnSize(t *testing.T) {
	const u = "https://files.slack.com/files-tmb/T1-F1-x/giphy_480.gif"
	c, _ := gifCache(t, map[string][]byte{u: bigGIF(t, 480, 270, 12, 4, 0)})
	e := readyEntry(t, c, u, 360)
	if e.size != image.Pt(360, 202) || e.frames != 12 {
		t.Fatalf("still %v, %d frames: want 360×202, 12", e.size, e.frames)
	}
	seen := drawFor(c, u, 360, 900*time.Millisecond)
	if len(seen) < 8 {
		t.Fatalf("%d frames drawn in 900 ms at 40 ms a frame", len(seen))
	}
	for _, op := range seen {
		if op.Size() != image.Pt(360, 202) {
			t.Fatalf("a frame of %v, want the drawn 360×202", op.Size())
		}
	}
	p := playerOf(c, u, 360)
	if p == nil || p.keep || p.dec == nil {
		t.Fatalf("player %+v: 12 frames of 360×202 stream", p)
	}
	if fb := 360 * 202 * 4; p.n > decoderBytes(p.ix)+scalerBytes(480, 270)+3*fb {
		t.Errorf("a streaming player holds %d bytes", p.n)
	}
}

// TestOffscreenGIFReleased: once nothing draws a GIF its player goes
// (its frames, canvas and buffers), and its still stays like any image's.
func TestOffscreenGIFReleased(t *testing.T) {
	const big, small = "https://example.com/big.gif", "https://emoji.slack-edge.com/T1/s/x.gif"
	c, _ := gifCache(t, map[string][]byte{big: bigGIF(t, 400, 300, 10, 5, 0), small: synthGIF(t)})
	readyEntry(t, c, big, 300)
	readyEntry(t, c, small, 32)
	drawFor(c, big, 300, 300*time.Millisecond)
	drawFor(c, small, 32, 300*time.Millisecond)
	if s := c.Stats(); s.Players != 2 {
		t.Fatalf("%d players while drawn", s.Players)
	}
	dl := time.Now().Add(4 * time.Second)
	for c.Stats().Players > 0 && time.Now().Before(dl) {
		time.Sleep(50 * time.Millisecond)
	}
	s := c.Stats()
	if s.Players != 0 || s.PlayerBytes != 0 {
		t.Fatalf("off screen 4 s: %d players holding %d bytes", s.Players, s.PlayerBytes)
	}
	if s.Entries != 2 || s.Bytes != 300*225*4+32*32*4+s.EncodedBytes {
		t.Errorf("stills: %+v", s)
	}
	// back on screen: it plays again (from the bytes it kept)
	if seen := drawFor(c, big, 300, 400*time.Millisecond); len(seen) < 3 {
		t.Errorf("back on screen: %d frames", len(seen))
	}
}

// TestGIFBurstStaysInBudget is a long channel of large GIFs scrolled
// through with two or three on screen at a time, then all of them asked
// for at once: the decoded bytes never pass the budget, players run only
// for what's drawn, and the ones scrolled past let their frames go.
func TestGIFBurstStaysInBudget(t *testing.T) {
	gifs := map[string][]byte{}
	var urls []string
	for i := 0; i < 30; i++ {
		u := fmt.Sprintf("https://example.com/gif%02d.gif", i)
		gifs[u] = bigGIF(t, 480, 270, 16, 3, 0)
		urls = append(urls, u)
	}
	c, _ := gifCache(t, gifs)
	const limit = 12 << 20
	c.SetLimit(limit)
	maxBytes, maxPlayers := 0, 0
	check := func() {
		s := c.Stats()
		maxBytes, maxPlayers = max(maxBytes, s.Bytes), max(maxPlayers, s.Players)
		if s.Bytes > limit {
			t.Fatalf("%d bytes over the %d budget: %+v", s.Bytes, limit, s)
		}
		c.mu.Lock()
		sum := 0
		for el := c.lru.Front(); el != nil; el = el.Next() {
			if e := el.Value.(*entry); e.gif != nil {
				sum += len(e.gif.data)
			}
		}
		kept := c.gifBytes
		c.mu.Unlock()
		if sum != kept || kept > limit/gifShare {
			t.Fatalf("encoded GIF bytes: entries hold %d, counted %d (at most %d)", sum, kept, limit/gifShare)
		}
	}
	draw := func(us []string, d time.Duration) {
		for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(8 * time.Millisecond) {
			for _, u := range us {
				c.Get(u, 360)
			}
			check()
		}
	}
	for top := 0; top+3 <= len(urls); top++ { // scroll: three on screen
		draw(urls[top:top+3], 250*time.Millisecond)
	}
	last := urls[len(urls)-3:]
	draw(last, 2500*time.Millisecond) // the scroll stops: only those three play
	for _, u := range urls {
		on := u == last[0] || u == last[1] || u == last[2]
		if p := playerOf(c, u, 360); (p != nil) != on {
			t.Errorf("%s (on screen %v): player %v", u, on, p != nil)
		}
	}
	for dl := time.Now().Add(2 * time.Second); time.Now().Before(dl); time.Sleep(8 * time.Millisecond) {
		for _, u := range urls { // all thirty at once
			c.Get(u, 360)
		}
		check()
	}
	time.Sleep(3 * time.Second) // nothing drawn
	check()
	if s := c.Stats(); s.Players != 0 {
		t.Errorf("nothing drawn for 3 s: %d players still hold %d bytes", s.Players, s.PlayerBytes)
	}
	t.Logf("peak %.1f MB of %.0f MB, %d players at most", float64(maxBytes)/(1<<20), float64(limit)/(1<<20), maxPlayers)
}

// TestGIFLoopCount: NETSCAPE2.0's count is honoured (1: two passes), a
// GIF without one plays once, and the last frame then stays.
func TestGIFLoopCount(t *testing.T) {
	once, twice := "https://example.com/once.gif", "https://example.com/twice.gif"
	c, _ := gifCache(t, map[string][]byte{once: bigGIF(t, 40, 40, 4, 2, -1), twice: bigGIF(t, 40, 40, 4, 2, 1)})
	for u, passes := range map[string]int{once: 1, twice: 2} {
		readyEntry(t, c, u, 40)
		seen := drawFor(c, u, 40, 800*time.Millisecond) // 80 ms a pass
		if len(seen) != 4*passes {
			t.Errorf("%s: %d frames shown, want %d", u, len(seen), 4*passes)
		}
		if p := playerOf(c, u, 40); p == nil || !p.ended || p.cur != 3 {
			t.Errorf("%s: player %+v, want ended on the last frame", u, p)
		}
	}
}

// TestAnimationsOff: Accessibility > "Allow animations" off draws the
// still (Animated false, IsAnimation still true) and stops what played;
// GetPlaying plays one anyway (the still's play button).
func TestAnimationsOff(t *testing.T) {
	const u = "https://example.com/a.gif"
	c, _ := gifCache(t, map[string][]byte{u: bigGIF(t, 200, 100, 6, 3, 0)})
	e := readyEntry(t, c, u, 200)
	drawFor(c, u, 200, 200*time.Millisecond)
	c.SetAnimate(false)
	if c.Animated(u, 200) || !c.IsAnimation(u, 200) {
		t.Errorf("animations off: Animated %v, IsAnimation %v", c.Animated(u, 200), c.IsAnimation(u, 200))
	}
	if s := c.Stats(); s.Players != 0 {
		t.Errorf("animations off: %d players", s.Players)
	}
	if seen := drawFor(c, u, 200, 300*time.Millisecond); len(seen) != 1 || seen[0] != e.op {
		t.Errorf("animations off: %d frames drawn, want the still", len(seen))
	}
	var played []paint.ImageOp
	for dl := time.Now().Add(500 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		if op, ok := c.GetPlaying(u, 200); ok && (len(played) == 0 || played[len(played)-1] != op) {
			played = append(played, op)
		}
		if op, _ := c.Get(u, 200); op != e.op {
			t.Fatal("Get drew a frame of the one GetPlaying plays")
		}
	}
	if len(played) < 4 {
		t.Errorf("GetPlaying: %d frames in 500 ms at 30 ms a frame", len(played))
	}
}

// TestStillDecodesFirstFrameOnly: a GIF's still costs its first frame, not
// image/gif's every frame (DecodeAll on a long GIF allocated all of them
// to draw one).
func TestStillDecodesFirstFrameOnly(t *testing.T) {
	data := bigGIF(t, 400, 300, 60, 3, 0)
	ix, err := indexGIF(data)
	if err != nil || len(ix.frames) != 60 {
		t.Fatalf("index: %v", err)
	}
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	d := newGIFDecoder(ix, data)
	d.compose()
	runtime.ReadMemStats(&b)
	if got := b.TotalAlloc - a.TotalAlloc; got > 3*400*300*4 {
		t.Errorf("the first frame allocated %d bytes; its canvas is %d", got, 400*300*4)
	}
}
