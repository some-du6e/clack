package popups

// Animated GIF tiles for the picker's GIF tab.
//
// imgcache decodes only the first frame, so the GIF tab keeps its own small
// cache of animations: Giphy's 200w rendition is fetched as .gif (Go's webp
// decoder can't do animated webp; Giphy serves the same path as .gif),
// every frame is composed (honouring disposal), cover-cropped to a square
// and scaled to the tile size on a worker goroutine. Tiles pick the frame
// from the clock and schedule the next redraw only while they are laid out,
// so nothing animates (or costs anything) once the tile scrolls away or the
// picker closes. Media is Giphy's public CDN; nothing is written to disk.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"net/url"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

const (
	gifAnimMax       = 40       // animations kept in memory
	gifAnimMaxFrames = 120      // frames kept per animation
	gifAnimMaxBytes  = 12 << 20 // largest .gif fetched
	gifAnimWorkers   = 4        // concurrent fetch+decode jobs
)

type gifAnim struct {
	frames  []paint.ImageOp
	ends    []time.Duration // cumulative end time of each frame
	total   time.Duration
	start   time.Time // first shown
	used    time.Time
	ready   bool
	failed  bool
	loading bool
}

type gifAnimKey struct {
	url string
	px  int
}

var gifAnims = struct {
	mu  sync.Mutex
	m   map[gifAnimKey]*gifAnim
	sem chan struct{}
}{
	m:   map[gifAnimKey]*gifAnim{},
	sem: make(chan struct{}, gifAnimWorkers),
}

// gifAnimURL is the animated .gif for a result (from the 200w webp preview).
func gifAnimURL(g Gif) string {
	u := g.Preview
	if u == "" {
		u = g.URL
	}
	if pu, err := url.Parse(u); u == "" || err != nil || pu.Scheme != "https" || !slack.HostUnder(pu.Hostname(), "giphy.com") {
		return ""
	}
	q := ""
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u, q = u[:i], u[i:]
	}
	if strings.HasSuffix(u, ".webp") {
		u = strings.TrimSuffix(u, ".webp") + ".gif"
	} else if !strings.HasSuffix(u, ".gif") {
		return ""
	}
	return u + q
}

// gifAnimGet returns a ready animation for url at px×px, starting a
// background load when it isn't cached. Call on the UI goroutine.
func gifAnimGet(h Host, url string, px int, now time.Time) (*gifAnim, bool) {
	if url == "" || px <= 0 {
		return nil, false
	}
	k := gifAnimKey{url, px}
	gifAnims.mu.Lock()
	a := gifAnims.m[k]
	if a == nil {
		a = &gifAnim{loading: true}
		gifAnims.m[k] = a
		gifAnimEvictLocked()
		go gifAnimLoad(h, k, a)
	}
	a.used = now
	ready := a.ready
	gifAnims.mu.Unlock()
	if !ready {
		return nil, false
	}
	if a.start.IsZero() {
		a.start = now
	}
	return a, true
}

func gifAnimEvictLocked() {
	for len(gifAnims.m) > gifAnimMax {
		var oldK gifAnimKey
		var oldT time.Time
		first := true
		for k, a := range gifAnims.m {
			if a.loading {
				continue
			}
			if first || a.used.Before(oldT) {
				oldK, oldT, first = k, a.used, false
			}
		}
		if first {
			return
		}
		delete(gifAnims.m, oldK)
	}
}

func gifAnimLoad(h Host, k gifAnimKey, a *gifAnim) {
	gifAnims.sem <- struct{}{}
	frames, ends, err := gifAnimDecode(k.url, k.px)
	<-gifAnims.sem
	gifAnims.mu.Lock()
	a.loading = false
	if err != nil || len(frames) == 0 {
		a.failed = true
	} else {
		a.frames, a.ends, a.total = frames, ends, ends[len(ends)-1]
		a.ready = true
	}
	gifAnims.mu.Unlock()
	if a.ready && h != nil {
		h.Invalidate()
	}
}

// gifAnimDecode fetches and decodes url into px×px cover-cropped frames.
func gifAnimDecode(raw string, px int) ([]paint.ImageOp, []time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	// gif.search results are Giphy media, which Slack's picker loads
	// directly; fetch them with no credentials, anything else via the proxy.
	var b []byte
	var err error
	if pu, perr := url.Parse(raw); perr == nil && pu.Scheme == "https" && slack.HostUnder(pu.Hostname(), "giphy.com") {
		b, _, err = slack.GetNoCreds(ctx, raw, gifAnimMaxBytes)
	} else {
		b, _, err = slack.FetchPublic(ctx, raw, gifAnimMaxBytes)
	}
	if err != nil {
		return nil, nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	return gifAnimFrames(g, px)
}

// gifAnimFrames composes g's frames (disposal aware) and scales each to a
// px×px center crop.
func gifAnimFrames(g *gif.GIF, px int) ([]paint.ImageOp, []time.Duration, error) {
	if len(g.Image) == 0 {
		return nil, nil, fmt.Errorf("gif: no frames")
	}
	w, hgt := g.Config.Width, g.Config.Height
	if w <= 0 || hgt <= 0 {
		b := g.Image[0].Bounds()
		w, hgt = b.Max.X, b.Max.Y
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, hgt))
	var prev *image.RGBA
	side := w
	if hgt < side {
		side = hgt
	}
	crop := image.Rect((w-side)/2, (hgt-side)/2, (w-side)/2+side, (hgt-side)/2+side)
	n := len(g.Image)
	if n > gifAnimMaxFrames {
		n = gifAnimMaxFrames
	}
	frames := make([]paint.ImageOp, 0, n)
	ends := make([]time.Duration, 0, n)
	var t time.Duration
	for i := 0; i < n; i++ {
		fr := g.Image[i]
		disp := byte(gif.DisposalNone)
		if i < len(g.Disposal) {
			disp = g.Disposal[i]
		}
		if disp == gif.DisposalPrevious {
			prev = image.NewRGBA(canvas.Rect)
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		out := image.NewRGBA(image.Rect(0, 0, px, px))
		xdraw.ApproxBiLinear.Scale(out, out.Rect, canvas, crop, xdraw.Src, nil)
		frames = append(frames, paint.NewImageOp(out))
		d := 100 * time.Millisecond // browsers treat delays < 20ms as 100ms
		if i < len(g.Delay) && g.Delay[i] >= 2 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		t += d
		ends = append(ends, t)
		switch disp {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if prev != nil {
				copy(canvas.Pix, prev.Pix)
			}
		}
	}
	return frames, ends, nil
}

// frameAt returns the frame to show at now and schedules the redraw for the
// next frame change (only called for tiles being laid out).
func (a *gifAnim) frameAt(gtx layout.Context) paint.ImageOp {
	if len(a.frames) == 1 || a.total <= 0 {
		return a.frames[0]
	}
	el := gtx.Now.Sub(a.start) % a.total
	i := 0
	for i < len(a.ends)-1 && a.ends[i] <= el {
		i++
	}
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(a.ends[i] - el)})
	return a.frames[i]
}
