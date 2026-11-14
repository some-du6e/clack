package imgcache

import (
	"bytes"
	"image"
	"runtime"
	"time"

	"gioui.org/op/paint"
)

// Animated GIFs play at the size they're drawn, as they do in Slack (a
// file's animatedSrc, an attachment's or image block's src), and only
// while something draws them:
//
//   - Every GIF's entry is its first frame at the requested size, a still
//     like any other image (the LRU, the idle sweep). An animated one also
//     keeps its encoded bytes and their index (counted in the budget,
//     dropped before stills when it's tight), so playing it again needs
//     no download: Slack's image proxy and Giphy aren't cached anywhere
//     else.
//   - Playing is a player: it starts once Get has drawn the GIF for
//     animSettle (a fast scroll's GIFs never start) and goes again as soon
//     as nothing has drawn it for animGrace past its frame's due time
//     (scrolled away, the view closed, the window hidden). A player that fits in animKeepBytes keeps all its frames
//     (custom emoji, avatars, small GIFs: no work per frame); a larger one
//     streams, holding its canvas and three frames, and composes each
//     frame on a worker just before it's due (gifstream.go).
//   - A player starts only if its bytes fit in the budget once what
//     nobody drew lately is evicted; otherwise the GIF shows its still and
//     tries again after animRetry. When the budget runs out the players
//     stop (largest first) before anything on screen is evicted, so a
//     burst of large GIFs can't push the decoded pixels past the budget,
//     nor make them load and evict each other in a loop.
//   - Accessibility > "Allow animations" off (SetAnimate(false)): Get
//     draws stills only. GetPlaying plays a GIF anyway: Slack's
//     PlayPauseAnimations, the still's play button.
//
// Frames play for their GIF delays (10 ms or less as 100 ms), the loop
// count is honoured (no NETSCAPE2.0 block: once; the last frame then
// stays), and a frame that's late shows when its decode is ready: a slow
// machine plays slower, never drops to the still.

const (
	// animKeepBytes: a player whose frames at the drawn size take at most
	// this keeps them all once decoded; a larger one streams.
	animKeepBytes = 2 << 20
	// animGrace: a player nothing drew this long after its frame was due
	// is off screen, and goes.
	animGrace = time.Second
	// animSettle: a GIF plays once it has been drawn this long: one a
	// fast scroll only flies past shows its still and starts nothing.
	animSettle = 150 * time.Millisecond
	// animRetry: how long a GIF that didn't fit the budget (or stopped
	// for room) shows its still before it asks again.
	animRetry = 3 * time.Second
	// animMaxPixels caps the logical screen of a GIF that plays (its
	// canvas takes 4 bytes a pixel): a bigger one shows its still.
	animMaxPixels = 4 << 20
)

// animSem bounds the decoders running at once (players starting, frames
// composing): every Cache together.
var animSem = make(chan struct{}, max(2, min(4, runtime.GOMAXPROCS(0))))

func isGIF(data []byte) bool { return bytes.HasPrefix(data, []byte("GIF8")) }

// gifSource is an animated GIF's encoded bytes and their index.
type gifSource struct {
	data []byte
	ix   *gifIndex
}

// player is an animated GIF playing at one size.
type player struct {
	ix      *gifIndex
	size    image.Point // a frame at the requested size
	n       int         // bytes held (in the entry's n and the cache's)
	forced  bool        // asked for by GetPlaying: plays with animations off
	dropped bool        // released: a decode in flight throws its work away

	// keep: every frame at size, decoded once (frames nil until then)
	keep   bool
	frames []paint.ImageOp

	// streaming: the decoder (only the worker touches it while busy) and
	// three frames: the one shown, the one decoded next, and the one
	// shown before (another window may still be drawing it)
	src    *gifSource
	dec    *gifDecoder
	sc     frameScaler
	bufs   [3]*image.RGBA
	front  int // the buffer cur shows (-1: the still)
	busy   bool
	next   int // the frame in nextOp, -1 none
	nextOp paint.ImageOp
	nextB  int

	cur      int
	curOp    paint.ImageOp
	shown    time.Time // when cur began showing
	passes   int       // passes played to the end
	ended    bool      // the loop count ran out: cur stays
	deadline time.Time // released if nothing draws it by then
}

// following is the frame after cur and whether a pass ends there; ok is
// false when the loop count is done.
func (p *player) following() (n int, wrap, ok bool) {
	if p.cur+1 < len(p.ix.frames) {
		return p.cur + 1, false, true
	}
	if l := p.ix.loop; l < 0 || l > 0 && p.passes >= l {
		return 0, true, false
	}
	return 0, true, true
}

// advance moves p to the frame due at now, and returns when the next one
// is due (zero: it ended, or it waits for a decode that repaints).
func (p *player) advance(now time.Time) time.Time {
	for !p.ended {
		due := p.shown.Add(p.ix.frames[p.cur].delay)
		if now.Before(due) {
			return due
		}
		n, wrap, ok := p.following()
		if !ok {
			p.ended = true
			break
		}
		switch {
		case p.keep && len(p.frames) == len(p.ix.frames):
			p.curOp = p.frames[n]
		case !p.keep && p.next == n:
			p.curOp, p.front, p.next = p.nextOp, p.nextB, -1
		default:
			return time.Time{} // not decoded yet
		}
		p.cur = n
		if wrap {
			p.passes++
		}
		// on schedule, unless it's a frame or more behind
		if d := p.ix.frames[n].delay; now.Sub(due) < d {
			p.shown = due
		} else {
			p.shown = now
		}
	}
	return time.Time{}
}

// frameLocked is what Get draws for e at now: the frame of its player
// (started on the first draw), or its still.
func (c *Cache) frameLocked(e *entry, now time.Time, forced bool) paint.ImageOp {
	if e.frames < 2 || !c.animate && !forced {
		return e.op
	}
	if now.Sub(e.seenAt) > animGrace { // back on screen
		e.seenFrom = now
	}
	e.seenAt = now
	p := e.play
	if p == nil {
		if settled := e.seenFrom.Add(animSettle); !forced && now.Before(settled) {
			c.wakeLocked(settled)
			return e.op
		}
		c.startPlayLocked(e, now, forced)
		return e.op
	}
	if p.ended {
		return p.curOp
	}
	if forced {
		p.forced = true
	}
	was := p.cur
	wake := p.advance(now)
	if p.cur != was {
		c.advances++
	}
	if p.ended {
		c.endLocked(e, p)
		return p.curOp
	}
	if !p.keep && p.next < 0 && !p.busy {
		if _, _, ok := p.following(); ok { // none past the last pass's last frame
			c.decodeLocked(p)
		}
	}
	if wake.IsZero() {
		p.deadline = now.Add(animGrace)
	} else {
		p.deadline = wake.Add(animGrace)
		c.wakeLocked(wake)
	}
	return p.curOp
}

// startPlayLocked starts e's player in the background, unless one is
// starting or e waits out animRetry.
func (c *Cache) startPlayLocked(e *entry, now time.Time, forced bool) {
	if e.starting {
		return
	}
	if now.Before(e.retry) {
		c.wakeLocked(e.retry)
		return
	}
	e.starting = true
	go c.startPlay(e.k, forced)
}

// startPlay makes k's player: its frames decoded (keep) or its decoder
// ready (streaming), once it fits in the budget.
func (c *Cache) startPlay(k key, forced bool) {
	c.mu.Lock()
	e := c.entryLocked(k)
	if e == nil {
		c.mu.Unlock()
		return
	}
	src := e.gif
	c.mu.Unlock()
	var err error
	if src == nil { // let go for room, or a derived size: the bytes again (disk tier, core's cache or the network)
		var data []byte
		if data, _, err = c.rawFor(k); err == nil {
			var ix *gifIndex
			if ix, err = indexGIF(data); err == nil {
				src = &gifSource{data: data, ix: ix}
			}
		}
	}
	now := time.Now()
	c.mu.Lock()
	e = c.entryLocked(k)
	if e == nil {
		c.mu.Unlock()
		return
	}
	e.starting = false
	if err != nil {
		e.retry = now.Add(retryAfter(1, err))
		c.mu.Unlock()
		return
	}
	if len(src.ix.frames) < 2 || src.ix.w*src.ix.h > animMaxPixels {
		e.retry = now.Add(RetryPermanent) // it stays a still
		c.mu.Unlock()
		return
	}
	if now.UnixMilli()-e.used > animGrace.Milliseconds() { // nobody drew it since it was asked for
		c.mu.Unlock()
		return
	}
	ix := src.ix
	sz := scaledSize(ix.w, ix.h, k.size)
	fb := 4 * sz.X * sz.Y
	p := &player{ix: ix, size: sz, forced: forced, front: -1, next: -1, curOp: e.op, shown: now}
	if fb*len(ix.frames) <= animKeepBytes {
		p.keep, p.n = true, fb*len(ix.frames)
	} else {
		p.n = decoderBytes(ix) + scalerBytes(ix.w, ix.h) + 3*fb
	}
	held := 0 // the encoded bytes, when the entry doesn't hold them already
	if e.gif == nil {
		held = len(src.data)
	}
	if !c.fitLocked(p.n+held, now) {
		e.retry = now.Add(animRetry)
		c.wakeLocked(e.retry)
		c.mu.Unlock()
		return
	}
	switch {
	case held > 0 && held <= c.maxBytes/(2*gifShare): // the entry keeps them for next time
		e.gif = src
		e.n += held
		c.bytes += held
		c.keptGIFLocked(e)
	case held > 0 && !p.keep: // only the player holds them, while it plays
		p.n += held
	}
	p.deadline = now.Add(ix.frames[0].delay + animGrace)
	c.installLocked(e, p)
	c.mu.Unlock()

	animSem <- struct{}{}
	defer func() { <-animSem }()
	if p.keep {
		frames := decodeFrames(src, sz)
		c.mu.Lock()
		if !p.dropped {
			p.frames = frames
			p.deadline = time.Now().Add(animGrace)
		}
		inv := c.invalidate
		c.mu.Unlock()
		inv()
		return
	}
	d := newGIFDecoder(ix, src.data)
	d.compose() // frame 0: the still's canvas
	c.mu.Lock()
	if !p.dropped {
		p.src, p.dec = src, d
		p.deadline = time.Now().Add(ix.frames[0].delay + animGrace)
		c.decodeLocked(p)
	}
	c.mu.Unlock()
}

// decodeFrames composes every frame of src at size sz.
func decodeFrames(src *gifSource, sz image.Point) []paint.ImageOp {
	d := newGIFDecoder(src.ix, src.data)
	var sc frameScaler
	out := make([]paint.ImageOp, len(src.ix.frames))
	for i := range out {
		d.compose()
		m := image.NewRGBA(image.Rectangle{Max: sz})
		sc.scale(m, d.canvas)
		out[i] = paint.NewImageOp(m)
	}
	return out
}

// installLocked makes p e's player, its bytes counted.
func (c *Cache) installLocked(e *entry, p *player) {
	e.play = p
	e.n += p.n
	c.bytes += p.n
	if c.playing == nil {
		c.playing = map[*entry]struct{}{}
	}
	c.playing[e] = struct{}{}
	if c.animT == nil {
		c.animT = time.AfterFunc(animGrace/2, c.animSweep)
	}
}

// decodeLocked composes p's next frame on a worker.
func (c *Cache) decodeLocked(p *player) {
	if p.dec == nil || p.busy || p.ended || p.dropped {
		return
	}
	p.busy = true
	t := (p.front + 1) % 3 // -1 (the still) → 0
	go c.decodeFrame(p, t)
}

func (c *Cache) decodeFrame(p *player, t int) {
	animSem <- struct{}{}
	i := p.dec.compose()
	m := p.bufs[t]
	if m == nil {
		m = image.NewRGBA(image.Rectangle{Max: p.size})
		p.bufs[t] = m
	}
	p.sc.scale(m, p.dec.canvas)
	<-animSem
	op := paint.NewImageOp(m)
	c.mu.Lock()
	p.busy = false
	if p.dropped {
		c.mu.Unlock()
		return
	}
	p.next, p.nextOp, p.nextB = i, op, t
	now := time.Now()
	late := !now.Before(p.shown.Add(p.ix.frames[p.cur].delay))
	if late {
		p.deadline = now.Add(animGrace)
	}
	inv := c.invalidate
	c.mu.Unlock()
	if late {
		inv()
	}
}

// endLocked lets go of all p holds but the frame it stays on (no decode
// runs past the last pass's last frame).
func (c *Cache) endLocked(e *entry, p *player) {
	delete(c.playing, e)
	if p.busy {
		return
	}
	keep := 4 * p.size.X * p.size.Y
	if p.n > keep {
		e.n -= p.n - keep
		c.bytes -= p.n - keep
		p.n = keep
	}
	p.frames, p.src, p.dec, p.sc = nil, nil, nil, frameScaler{}
	for i := range p.bufs {
		if i != p.front {
			p.bufs[i] = nil
		}
	}
}

// releaseLocked stops e's player: e draws its still again.
func (c *Cache) releaseLocked(e *entry) {
	p := e.play
	if p == nil {
		return
	}
	p.dropped = true
	e.play = nil
	e.n -= p.n
	c.bytes -= p.n
	delete(c.playing, e)
}

// animSweep releases the players nothing drew in time (every animGrace/2
// while any plays).
func (c *Cache) animSweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for e := range c.playing {
		if p := e.play; !p.busy && (!p.keep || p.frames != nil) && now.After(p.deadline) {
			c.releaseLocked(e)
		}
	}
	if len(c.playing) > 0 {
		c.animT.Reset(animGrace / 2)
	} else {
		c.animT = nil
	}
}

// fitLocked makes room for need more bytes by letting go of what nobody
// drew in the last animGrace (shedLocked), and reports whether they fit.
// A player wanting over half the budget doesn't start (nothing is evicted
// for it): it shows its still.
func (c *Cache) fitLocked(need int, now time.Time) bool {
	if need > c.maxBytes/2 {
		return false
	}
	return c.shedLocked(c.maxBytes-need, now)
}

// SetAnimate turns playing on or off for Get: Slack's Accessibility >
// "Allow animations" (a11y_animations). Off, animated GIFs draw their
// first frame (Slack's deanimated stills) and only GetPlaying plays one.
func (c *Cache) SetAnimate(on bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.animate == on {
		c.mu.Unlock()
		return
	}
	c.animate = on
	if !on {
		for el := c.lru.Front(); el != nil; el = el.Next() {
			if e := el.Value.(*entry); e.play != nil && !e.play.forced {
				c.releaseLocked(e)
			}
		}
	}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}

// Animating reports whether Get plays animations (SetAnimate).
func (c *Cache) Animating() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.animate
}

// GetPlaying is Get for a GIF the viewer chose to play (Slack's
// PlayPauseAnimations with animations off): it plays whether or not
// SetAnimate is on. Other images are as Get returns them.
func (c *Cache) GetPlaying(url string, sizePx int) (paint.ImageOp, bool) {
	return c.get(url, sizePx, true)
}

// Animated reports whether the ready image at url/sizePx changes as Get
// draws it (an animated GIF, animations on): callers that record their
// drawing draw it every frame instead.
func (c *Cache) Animated(url string, sizePx int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key{url, sizePx}]; ok {
		return c.animate && el.Value.(*entry).frames > 1
	}
	return false
}

// IsAnimation reports whether the ready image at url/sizePx is an
// animated GIF, animations on or off (the still's play button: Slack
// asks its image proxy for x-slack-animated).
func (c *Cache) IsAnimation(url string, sizePx int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key{url, sizePx}]; ok {
		return el.Value.(*entry).frames > 1
	}
	return false
}

// wakeLocked repaints the window at t (the earliest of the pending wakes).
func (c *Cache) wakeLocked(t time.Time) {
	now := time.Now()
	if !c.wakeAt.IsZero() && c.wakeAt.After(now) && !t.Before(c.wakeAt) {
		return
	}
	c.wakeAt = t
	d := t.Sub(now)
	if c.wakeT == nil {
		c.wakeT = time.AfterFunc(d, c.fireWake)
	} else {
		c.wakeT.Reset(d)
	}
}

func (c *Cache) fireWake() {
	c.mu.Lock()
	c.wakeAt = time.Time{}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}
