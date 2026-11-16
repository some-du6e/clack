package popups

// Animated GIF tiles for the picker's GIF tab.
//
// imgcache decodes only the first frame, so the GIF tab keeps its own small
// cache of animations: Giphy's 200w rendition is fetched as .gif (Go's webp
// decoder can't do animated webp; Giphy serves the same path as .gif), its
// first gifAnimMaxFrames frames are decoded (imgcache.DecodeGIF: the
// vendored image/gif, every allocation charged to the decode memory, no
// frame past them read) and composed (honouring disposal), cover-cropped to
// a square and scaled to the tile size on a worker goroutine, the
// composition charged to the same budget: one in the small lane
// (imgcache.WithSmallBudget), so previews never queue for the big-decode
// slot. The composed frames an animation keeps are charged to the decode
// memory for as long as they're kept (imgcache.NewKeepBudget), at most
// gifAnimKeepBytes of them in all past what's on screen. Tiles pick the frame
// from the clock and schedule the next redraw only while they are laid out,
// so nothing animates (or costs anything) once the tile scrolls away or the
// picker closes. Media is Giphy's public CDN; nothing is written to disk.

import (
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

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	gifAnimMax       = 40       // animations kept in memory
	gifAnimMaxFrames = 120      // frames kept per animation
	gifAnimMaxBytes  = 12 << 20 // largest .gif fetched
	gifAnimWorkers   = 4        // concurrent fetch+decode jobs
	// gifAnimKeepBytes: the composed frames kept, in all, past those of
	// animations drawn in the last gifAnimOnScreen (which stay: evicting
	// a tile on screen would decode it again at once)
	gifAnimKeepBytes = 64 << 20
	gifAnimOnScreen  = time.Second
)

type gifAnim struct {
	frames  []paint.ImageOp
	bytes   int              // what frames hold
	keep    *imgcache.Budget // frames' memory, charged while they're kept
	ends    []time.Duration  // cumulative end time of each frame
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
	mu    sync.Mutex
	m     map[gifAnimKey]*gifAnim
	kept  int // the animations' frame bytes
	sem   chan struct{}
	fetch func(ctx context.Context, raw string) ([]byte, error) // tests swap it (no network)
}{
	m:     map[gifAnimKey]*gifAnim{},
	sem:   make(chan struct{}, gifAnimWorkers),
	fetch: gifAnimFetch,
}

// gifAnimFetch loads a GIF with no credentials. gif.search results are
// Giphy media, which Slack's picker loads directly; anything else goes
// through Slack's image proxy.
func gifAnimFetch(ctx context.Context, raw string) ([]byte, error) {
	var b []byte
	var err error
	if pu, perr := url.Parse(raw); perr == nil && pu.Scheme == "https" && slack.HostUnder(pu.Hostname(), "giphy.com") {
		b, _, err = slack.GetNoCreds(ctx, raw, gifAnimMaxBytes)
	} else {
		b, _, err = slack.FetchPublic(ctx, raw, gifAnimMaxBytes)
	}
	return b, err
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
		gifAnimEvictLocked(now)
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

// gifAnimEvictLocked lets the least recently drawn animations go while
// there are more than gifAnimMax, or their frames hold more than
// gifAnimKeepBytes (then only ones off screen), giving back their memory.
func gifAnimEvictLocked(now time.Time) {
	for {
		over := len(gifAnims.m) > gifAnimMax
		if !over && gifAnims.kept <= gifAnimKeepBytes {
			return
		}
		var oldK gifAnimKey
		var oldT time.Time
		first := true
		for k, a := range gifAnims.m {
			if a.loading || !over && now.Sub(a.used) < gifAnimOnScreen {
				continue
			}
			if first || a.used.Before(oldT) {
				oldK, oldT, first = k, a.used, false
			}
		}
		if first {
			return
		}
		a := gifAnims.m[oldK]
		delete(gifAnims.m, oldK)
		gifAnims.kept -= a.bytes
		a.keep.Release()
	}
}

func gifAnimLoad(h Host, k gifAnimKey, a *gifAnim) {
	gifAnims.sem <- struct{}{}
	g, err := gifAnimDecode(k.url, k.px)
	<-gifAnims.sem
	gifAnims.mu.Lock()
	a.loading = false
	switch {
	case err != nil || len(g.frames) == 0:
		a.failed = true
	case gifAnims.m[k] != a: // let go meanwhile
		g.keep.Release()
	default:
		a.frames, a.ends, a.total = g.frames, g.ends, g.ends[len(g.ends)-1]
		a.bytes, a.keep = g.bytes, g.keep
		a.ready = true
		gifAnims.kept += a.bytes
		gifAnimEvictLocked(time.Now())
	}
	gifAnims.mu.Unlock()
	if a.ready && h != nil {
		h.Invalidate()
	}
}

// gifAnimMaxSide bounds the logical screen of a GIF we decode (the picker
// shows Giphy's 200 px renditions), so a small file claiming a huge canvas
// can't allocate gigabytes per frame.
const gifAnimMaxSide = 1024

// gifDecoded is a decoded animation: its frames, their end times, what
// they hold and the budget that holds it (the caller's to Release when it
// lets them go).
type gifDecoded struct {
	frames []paint.ImageOp
	ends   []time.Duration
	bytes  int
	keep   *imgcache.Budget
}

// gifAnimDecode fetches and decodes url into px×px cover-cropped frames,
// charged, as kept, to the returned budget.
func gifAnimDecode(raw string, px int) (gifDecoded, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	gifAnims.mu.Lock()
	fetch := gifAnims.fetch
	gifAnims.mu.Unlock()
	b, err := fetch(ctx, raw) // no credentials; Giphy direct, anything else via Slack's image proxy
	if err != nil {
		return gifDecoded{}, err
	}
	var d gifDecoded
	err = imgcache.WithSmallBudget(ctx, func(bg *imgcache.Budget) error {
		g, err := imgcache.DecodeGIF(b, gifAnimMaxFrames, gifAnimMaxSide, bg)
		if err != nil {
			return err
		}
		frames, ends, err := gifAnimFrames(g, px, imgcache.ChargeFunc(bg))
		if err != nil {
			return err
		}
		// the frames outlive the decode: charged again, as kept, before
		// its budget gives them back
		n := 4 * px * px * len(frames)
		keep := imgcache.NewKeepBudget()
		if err := imgcache.ChargeFunc(keep)(int64(n)); err != nil {
			keep.Release()
			return err
		}
		d = gifDecoded{frames: frames, ends: ends, bytes: n, keep: keep}
		return nil
	})
	if err != nil {
		return gifDecoded{}, err
	}
	return d, nil
}

// gifAnimFrames composes g's frames (disposal aware) and scales each to a
// px×px center crop, its canvases and frames charged to charge first.
func gifAnimFrames(g *imgcache.GIF, px int, charge func(int64) error) ([]paint.ImageOp, []time.Duration, error) {
	if len(g.Image) == 0 {
		return nil, nil, fmt.Errorf("gif: no frames")
	}
	w, hgt := g.Config.Width, g.Config.Height
	if w <= 0 || hgt <= 0 {
		b := g.Image[0].Bounds()
		w, hgt = b.Max.X, b.Max.Y
	}
	if err := charge(4 * int64(w) * int64(hgt)); err != nil {
		return nil, nil, err
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
			if prev == nil { // one copy of the canvas, made once
				if err := charge(int64(len(canvas.Pix))); err != nil {
					return nil, nil, err
				}
				prev = image.NewRGBA(canvas.Rect)
			}
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		if err := charge(4 * int64(px) * int64(px)); err != nil {
			return nil, nil, err
		}
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
