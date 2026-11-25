package messages

import (
	"bytes"
	"image"
	"image/draw"
	"image/gif"
	"sort"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// MessageContainerLoading is Slack's (the message-kit responsive-max-width
// chunk's q3Mq, namespace unread, build 889ac449a32d), a view's first
// load: .p-message_container_loading covers the view (inset 0, its
// primary background) with the uv-loader GIF at 200x200 in its centre
// (uv-loader-dark in the dark theme), over which __msg, a 220px box
// centred the same way, writes the loading message at its top: 15px bold
// in rgba(--sk_foreground_high_solid, 1), centred. The message is the
// view's loadingMessage ("Loading messages…" when ""), and 5 s after it
// mounted (since) "Still loading… sorry for the long wait!". The Threads
// view's first load passes "Loading threads…", Unreads' "Loading unread
// messages…".
func MessageContainerLoading(gtx layout.Context, th *theme.Theme, size image.Point, msg string, since time.Time) {
	now := gtx.Now
	if msg == "" {
		msg = unreadNS.T("Loading messages…")
	}
	if wait := since.Add(StillLoadingAfter); now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
	} else {
		msg = StillLoading()
	}
	fill(gtx.Ops, image.Rectangle{Max: size}, th.C.Bg)
	gs := dp(gtx, 200)
	r := image.Rect((size.X-gs)/2, (size.Y-gs)/2, (size.X+gs)/2, (size.Y+gs)/2)
	uvLoader(th.Dark).draw(gtx, r, since)
	ink := skColor(th.Dark, "--sk_foreground_high_solid", 1, rgb(0x868686))
	ts := textStyle{size: 15, weight: theme.Bold, col: ink}
	w := measure(gtx, th, ts, msg).X
	at(gtx, (size.X-w)/2, (size.Y-dp(gtx, 220))/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, msg) })
}

// StillLoadingAfter is MessageContainerLoading's 5 s (and the Threads
// view's paging row's): a var for tests.
var StillLoadingAfter = 5 * time.Second

// StillLoading is the 5 s message, "Still loading… sorry for the long
// wait!" (namespace unread; the Threads view's paging row has its own in
// threads).
func StillLoading() string { return unreadNS.T("Still loading… sorry for the long wait!") }

var unreadNS = i18n.NS("unread")

// gifPlayer draws an animated GIF frame by frame. It keeps the decoded
// (paletted) frames and composes the one asked for on one canvas, going on
// from the last it composed or starting over when the animation wrapped;
// each frame, once composed, is kept at the size it's drawn (scaled once,
// one image op each), so a loop re-uses its textures instead of making a
// new 400x400 image and upload every GIF frame. The kept frames go a few
// seconds after the loader stops drawing (gifKeep).
type gifPlayer struct {
	once   sync.Once
	g      *gif.GIF
	ends   []time.Duration
	total  time.Duration
	canvas *image.RGBA
	saved  *image.RGBA // the canvas before a DisposalPrevious frame
	at     int         // the frame composed on canvas (-1: none)

	mu    sync.Mutex
	size  image.Point     // the drawn size the kept frames are at
	ops   []paint.ImageOp // the kept frames, by index (zero: not composed yet)
	used  time.Time       // the last draw
	sweep *time.Timer     // drops the kept frames once unused for gifKeep
}

// gifKeep is how long a player keeps its frames after its last draw.
var gifKeep = 3 * time.Second

var uvPlayers [2]gifPlayer // light, dark

func uvLoader(dark bool) *gifPlayer {
	p, url := &uvPlayers[0], slackasset.UVLoader
	if dark {
		p, url = &uvPlayers[1], slackasset.UVLoaderDark
	}
	p.once.Do(func() {
		raw, _ := slackasset.Get(url)
		g, err := gif.DecodeAll(bytes.NewReader(raw))
		if err != nil || len(g.Image) == 0 {
			return
		}
		p.g = g
		for i := range g.Image {
			d := 30 * time.Millisecond
			if i < len(g.Delay) && g.Delay[i] > 0 {
				d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
			}
			p.total += d
			p.ends = append(p.ends, p.total)
		}
		p.canvas = image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
		p.at = -1
	})
	return p
}

// frame is frame i at size sz, composed and scaled the first time it's
// asked for. p.mu held.
func (p *gifPlayer) frame(i int, sz image.Point) paint.ImageOp {
	if sz != p.size || len(p.ops) != len(p.g.Image) {
		p.size, p.ops = sz, make([]paint.ImageOp, len(p.g.Image))
	}
	if op := p.ops[i]; op.Size() != (image.Point{}) {
		return op
	}
	p.compose(i)
	out := image.NewRGBA(image.Rectangle{Max: sz})
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), p.canvas, p.canvas.Bounds(), xdraw.Src, nil)
	p.ops[i] = paint.NewImageOp(out)
	return p.ops[i]
}

// compose draws frame i on the canvas.
func (p *gifPlayer) compose(i int) {
	if i == p.at {
		return
	}
	b := p.canvas.Bounds()
	if i < p.at {
		draw.Draw(p.canvas, b, image.Transparent, image.Point{}, draw.Src)
		p.at = -1
	}
	for p.at < i {
		if p.at >= 0 { // the last frame's disposal
			switch p.disposal(p.at) {
			case gif.DisposalBackground:
				draw.Draw(p.canvas, p.g.Image[p.at].Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if p.saved != nil {
					draw.Draw(p.canvas, b, p.saved, image.Point{}, draw.Src)
				}
			}
		}
		p.at++
		f := p.g.Image[p.at]
		if p.disposal(p.at) == gif.DisposalPrevious {
			if p.saved == nil {
				p.saved = image.NewRGBA(b)
			}
			draw.Draw(p.saved, b, p.canvas, image.Point{}, draw.Src)
		}
		draw.Draw(p.canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
	}
}

// keep notes a draw and has the kept frames dropped once unused for
// gifKeep. p.mu held.
func (p *gifPlayer) keep(now time.Time) {
	p.used = now
	if p.sweep != nil {
		return
	}
	var check func()
	check = func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if left := gifKeep - time.Since(p.used); left > 0 {
			p.sweep = time.AfterFunc(left, check)
			return
		}
		p.ops, p.size, p.sweep = nil, image.Point{}, nil
	}
	p.sweep = time.AfterFunc(gifKeep, check)
}

func (p *gifPlayer) disposal(i int) byte {
	if i < len(p.g.Disposal) {
		return p.g.Disposal[i]
	}
	return 0
}

// draw paints the frame for now in r, the animation running from start,
// and asks for the frame after it.
func (p *gifPlayer) draw(gtx layout.Context, r image.Rectangle, start time.Time) {
	if p.g == nil || p.total <= 0 {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	el := now.Sub(start) % p.total
	if el < 0 {
		el = 0
	}
	i := min(sort.Search(len(p.ends), func(i int) bool { return p.ends[i] > el }), len(p.g.Image)-1)
	p.mu.Lock()
	img := p.frame(i, r.Size())
	p.keep(time.Now())
	p.mu.Unlock()
	t := op.Offset(r.Min).Push(gtx.Ops)
	img.Add(gtx.Ops)
	cl := clip.Rect(image.Rectangle{Max: r.Size()}).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	t.Pop()
	gtx.Execute(op.InvalidateCmd{At: start.Add(now.Sub(start) - el + p.ends[i])})
}
