package imgcache

import (
	"bytes"
	"image"
	"runtime"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
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
//     tries again after animRetry. When the budget runs out the streaming
//     players stop (largest first) before anything on screen is evicted,
//     so a burst of large GIFs can't push the decoded pixels past the
//     budget, nor make them load and evict each other in a loop. A
//     keeping player is its image's frames, decoded once like Chromium's
//     (an emoji's), and plays on while stills fill the budget; keeping
//     players stop for room only past a quarter of it (keepShare).
//   - Off screen means not drawn while other images were: a window that
//     draws nothing at all (a long frame, or hidden) doesn't stop its
//     keeping players, and they go on from where the timeline is.
//   - Accessibility > "Allow animations" off (SetAnimate(false)): Get
//     draws stills only. GetPlaying plays a GIF anyway: Slack's
//     PlayPauseAnimations, the still's play button.
//
// An animated WebP (webpanim.go) plays the same way, through the same
// index, decoder and players.
//
// What a player decodes with is charged to a Budget from the decode pool
// (decmem.go): a keeping one's decoder, scaler and frames while it decodes
// them (the frames are then the cache's, in its byte budget); a streaming
// one's decoder, scaler and three frames for as long as it plays, given
// back when it stops. One the pool can't cover now shows its still, or
// stays on its frame, and tries again shortly.
//
// Frames play for their delays (10 ms or less as 100 ms: Blink's
// DeferredImageDecoder::FrameDurationAtIndex), the loop count is honoured
// (no NETSCAPE2.0 block: once; the last frame then stays), and a keeping
// player keeps to its timeline as Chromium's compositor does (advance):
// a frame drawn late doesn't delay the rest, a draw a frame or more late
// skips to the frame due, and every keeping player of one image (its
// sizes: a message's emoji, a jumbomoji, a reaction) shares that timeline
// and its repaints. A streaming player's frame that's late shows when its
// decode is ready: a slow machine plays it slower, never drops to the
// still.

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

// gifSource is an animated GIF's (or WebP's) encoded bytes and their index.
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
	// budget holds a streaming player's decoder, scaler and frames from the
	// decode pool while it plays: released when it stops (by whoever is
	// last: releaseLocked, or the worker busy then). A frame that couldn't
	// decode (ErrBusy: the pool is full) is tried again from retryAt.
	budget  *budget.Budget
	fails   int
	retryAt time.Time

	// s is where it is on its animation's timeline: its own while it
	// streams (or decodes its frames), then shared with every keeping
	// player of the image (schedule). curOp is the frame it draws: the
	// still until the schedule first moves, then the schedule's frame;
	// step is the schedule's step curOp shows (-1: none yet).
	s        *schedule
	step     int
	curOp    paint.ImageOp
	deadline time.Time // released if nothing draws it by then
}

// schedule is an animation's place on its timeline, as Chromium's
// AnimationState keeps it (cc/trees/image_animation_controller.cc): the
// frame showing and when it began (on the timeline, not when it was drawn),
// the passes played to the end, and reps, Chromium's
// repetitions_completed (passes since the schedule last started over),
// which decides when catching up starts over instead. Every keeping
// player of one image shares one, as every <img> of one image shares its
// animation in Chromium (one PaintImage): the emoji in a message, as a
// jumbomoji and in a reaction show the same frame, and one repaint moves
// them all.
type schedule struct {
	cur     int
	shown   time.Time
	passes  int
	reps    int
	ended   bool // the loop count ran out: cur stays
	steps   int  // frames it has moved on (skipped ones included)
	skipped int  // frames it passed over to catch up
}

// following is the frame after the schedule's and whether a pass ends
// there; ok is false when the loop count is done.
func (p *player) following() (n int, wrap, ok bool) {
	if p.s.cur+1 < len(p.ix.frames) {
		return p.s.cur + 1, false, true
	}
	if l := p.ix.loop; l < 0 || l > 0 && p.s.passes >= l {
		return 0, true, false
	}
	return 0, true, true
}

// animResync is Chromium's kAnimationResyncCutoff: an animation more than
// this behind starts its current frame again instead of fast-forwarding.
const animResync = 5 * time.Minute

// advance moves p's schedule on to the frame due at now, and returns when
// the next one is due (zero: it ended, or it waits for a decode that
// repaints).
//
// A keeping player (every frame decoded) follows Chromium's
// AnimationState::AdvanceFrame and AdvanceAnimationState: a frame is due
// once the frame before it has shown for its duration, counted on the
// timeline, not from when that frame happened to be drawn, so a frame
// drawn late doesn't push the rest back, and a draw more than a frame
// late skips to the frame due now. Two exceptions, as there: behind at the
// end of the first pass (it started late), the timeline starts over from
// the first frame now, so every frame shows once more rather than being
// skipped, and the loop count starts over with it (that first pass
// doesn't count); and more than animResync behind, the frame showing
// starts again now.
//
// A streaming player decodes each frame once the one before is up, in
// order (a frame draws over the last), so it can't skip: a late frame
// shows when it's ready and the timeline goes on from then, as before (a
// slow machine plays slower, never drops to the still).
func (p *player) advance(now time.Time) time.Time {
	s := p.s
	if !p.keep {
		for !s.ended {
			due := s.shown.Add(p.ix.frames[s.cur].delay)
			if now.Before(due) {
				return due
			}
			n, wrap, ok := p.following()
			if !ok {
				s.ended = true
				break
			}
			if p.next != n {
				return time.Time{} // not decoded yet
			}
			p.curOp, p.front, p.next = p.nextOp, p.nextB, -1
			s.cur, s.steps = n, s.steps+1
			p.step = s.steps
			if wrap {
				s.passes++
			}
			// on schedule, unless it's a frame or more behind
			if d := p.ix.frames[n].delay; now.Sub(due) < d {
				s.shown = due
			} else {
				s.shown = now
			}
		}
		return time.Time{}
	}
	if s.ended || len(p.frames) != len(p.ix.frames) {
		return time.Time{} // ended, or decoding (the decode repaints)
	}
	if now.Sub(s.shown.Add(p.ix.frames[s.cur].delay)) > animResync {
		s.shown = now
	}
	first := s.steps
	for {
		due := s.shown.Add(p.ix.frames[s.cur].delay)
		if now.Before(due) {
			if n := s.steps - first; n > 1 {
				s.skipped += n - 1
			}
			return due
		}
		n, wrap, ok := p.following()
		if !ok {
			s.ended = true
			return time.Time{}
		}
		s.steps++
		if wrap && s.reps == 1 && !due.Add(p.ix.frames[0].delay).After(now) {
			// behind at the end of the first pass: the first frame, now,
			// and the passes counted from here (Chromium sets
			// repetitions_completed, its loop count too, back to 0)
			s.cur, s.shown, s.reps, s.passes = 0, now, 0, 0
			first = s.steps - 1
			continue
		}
		s.cur, s.shown = n, due
		if wrap {
			s.passes++
		}
		if n == len(p.ix.frames)-1 {
			s.reps++
		}
	}
}

// syncOp points a keeping player's curOp at its schedule's frame once the
// schedule moved (another size of the image may have moved it), and
// reports whether it changed.
func (p *player) syncOp() bool {
	if !p.keep || len(p.frames) != len(p.ix.frames) || p.step == p.s.steps {
		return false
	}
	p.curOp, p.step = p.frames[p.s.cur], p.s.steps
	return true
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
			c.wakeAnimLocked(settled)
			return e.op
		}
		c.startPlayLocked(e, now, forced)
		return e.op
	}
	if forced {
		p.forced = true
	}
	was, sk := p.curOp, p.s.skipped
	wake := p.advance(now)
	p.syncOp()
	if p.curOp != was {
		c.advances++
	}
	c.skipped += uint64(p.s.skipped - sk)
	if p.s.ended {
		if _, on := c.playing[e]; on {
			c.endLocked(e, p)
		}
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
		c.wakeAnimLocked(wake)
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
		c.wakeAnimLocked(e.retry)
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
	// what the player decodes with: a streaming one keeps it while it
	// plays, a keeping one gives it back once its frames are decoded. A
	// player's never takes the big slot, nor waits (it may hold its memory
	// for as long as it plays: newPlayerBudget)
	b := newPlayerBudget()
	var err error
	if src == nil { // let go for room, or a derived size: the bytes again (disk tier, core's cache or the network)
		var data []byte
		if data, _, err = c.rawFor(k); err == nil {
			var ix *gifIndex
			if ix, err = indexAnim(data, b); err == nil {
				src = &gifSource{data: data, ix: ix}
			}
		}
	}
	now := time.Now()
	c.mu.Lock()
	e = c.entryLocked(k)
	if e == nil {
		c.mu.Unlock()
		b.Release()
		return
	}
	e.starting = false
	if err != nil {
		e.playFails++
		e.retry = now.Add(retryAfter(e.playFails, budgetErr(err)))
		c.wakeAnimLocked(e.retry)
		c.mu.Unlock()
		b.Release()
		return
	}
	if len(src.ix.frames) < 2 || src.ix.w*src.ix.h > animMaxPixels {
		e.retry = now.Add(RetryPermanent) // it stays a still
		c.mu.Unlock()
		b.Release()
		return
	}
	if now.UnixMilli()-e.used > animGrace.Milliseconds() { // nobody drew it since it was asked for
		c.mu.Unlock()
		b.Release()
		return
	}
	ix := src.ix
	// the frames at the still's size: a size derived from another size's
	// still (deriveLocked) can be a pixel off the original's scaled size,
	// and its live op draws its frames in the still's place (SetLiveOps)
	sz := e.size
	if sz.X <= 0 || sz.Y <= 0 {
		sz = scaledSize(ix.w, ix.h, k.size)
	}
	fb := 4 * sz.X * sz.Y
	p := &player{ix: ix, size: sz, forced: forced, front: -1, next: -1, curOp: e.op, s: &schedule{shown: now}}
	// the decoder and its scaler: a streaming player's for as long as it
	// plays, a keeping one's while it decodes its frames
	work := decoderBytes(ix) + scalerBytes(ix.w, ix.h)
	if fb*len(ix.frames) <= animKeepBytes {
		p.keep, p.n = true, fb*len(ix.frames)+work
	} else {
		p.n = work + 3*fb
	}
	held := 0 // the encoded bytes, when the entry doesn't hold them already
	if e.gif == nil {
		held = len(src.data)
	}
	if !c.fitLocked(p.n+held, now) {
		e.retry = now.Add(animRetry)
		c.wakeAnimLocked(e.retry)
		c.mu.Unlock()
		b.Release()
		return
	}
	switch {
	case held > 0 && held <= c.maxBytes/(2*gifShare): // the entry keeps them for next time
		e.gif = src
		e.n += held
		c.bytes += held
		c.keptGIFLocked(e)
	case held > 0: // only the player holds them: while it plays, or decodes
		p.n += held
		work += held
	}
	p.deadline = now.Add(ix.frames[0].delay + animGrace)
	if !p.keep {
		// the budget is the player's, and only startPlay touches it until
		// the decoder is ready (busy: a release meanwhile leaves it here)
		p.budget, p.busy = b, true
	}
	c.installLocked(e, p)
	c.mu.Unlock()

	animSem <- struct{}{}
	defer func() { <-animSem }()
	if p.keep {
		frames, err := decodeFrames(src, sz, b)
		b.Release() // the frames are the cache's now (p.n), the rest garbage
		c.mu.Lock()
		switch {
		case p.dropped:
		case err != nil: // the still again, and another try later
			c.releaseLocked(e)
			e.playFails++
			e.retry = time.Now().Add(retryAfter(e.playFails, budgetErr(err)))
			c.wakeAnimLocked(e.retry)
		default: // its frames are all it holds now
			p.frames = frames
			e.playFails = 0
			now := time.Now()
			c.joinLocked(e, p, now)
			p.deadline = now.Add(animGrace)
			p.n -= work
			e.n -= work
			c.bytes -= work
		}
		inv := c.animInvalidateLocked()
		c.mu.Unlock()
		inv()
		return
	}
	d, err := newGIFDecoder(ix, src.data, b)
	if err == nil {
		_, err = d.compose() // frame 0: the still's canvas
	}
	if err == nil {
		err = p.prepare(ix, b)
	}
	c.mu.Lock()
	p.busy = false
	switch {
	case p.dropped:
		b.Release()
	case err != nil:
		c.releaseLocked(e)
		e.playFails++
		e.retry = time.Now().Add(retryAfter(e.playFails, budgetErr(err)))
		c.wakeAnimLocked(e.retry)
	default:
		p.src, p.dec = src, d
		e.playFails = 0
		p.deadline = time.Now().Add(ix.frames[0].delay + animGrace)
		c.decodeLocked(p)
	}
	c.mu.Unlock()
}

// joinLocked puts keeping player p, its frames just decoded, on the
// schedule of another keeping player of the same image (another size of it
// on screen), so they show the same frame and repaint together; with none,
// p's own schedule starts now, its first frame (the still) showing from
// now.
func (c *Cache) joinLocked(e *entry, p *player, now time.Time) {
	for _, el := range c.byURL[e.k.url] {
		o := el.Value.(*entry)
		if q := o.play; o != e && q != nil && q.keep && !q.dropped && len(q.frames) == len(p.frames) && !q.s.ended {
			p.s, p.step = q.s, -1 // its frame now, at the next draw (syncOp)
			return
		}
	}
	p.s.shown = now
}

// prepare makes a streaming player's three frames and its scaler's halved
// canvases, charged to b, so that decoding a frame (decodeFrame) allocates
// nothing more but a WebP frame's picture.
func (p *player) prepare(ix *gifIndex, b *budget.Budget) error {
	for i := range p.bufs {
		if err := b.Charge(rgbaBytes(p.size)); err != nil {
			return err
		}
		p.bufs[i] = image.NewRGBA(image.Rectangle{Max: p.size})
	}
	return p.sc.prepare(image.Pt(ix.w, ix.h), p.size, b)
}

// decodeFrames composes every frame of src at size sz, the decoder, the
// scaler and the frames charged to b.
func decodeFrames(src *gifSource, sz image.Point, b *budget.Budget) ([]paint.ImageOp, error) {
	d, err := newGIFDecoder(src.ix, src.data, b)
	if err != nil {
		return nil, err
	}
	var sc frameScaler
	if err := b.Charge(int64(len(src.ix.frames)) * opBytes); err != nil {
		return nil, err
	}
	out := make([]paint.ImageOp, len(src.ix.frames))
	for i := range out {
		if _, err := d.compose(); err != nil {
			return nil, err
		}
		if err := b.Charge(rgbaBytes(sz) + opBytes); err != nil {
			return nil, err
		}
		m := image.NewRGBA(image.Rectangle{Max: sz})
		if err := sc.scale(m, d.canvas, b); err != nil {
			return nil, err
		}
		out[i] = paint.NewImageOp(m)
	}
	return out, nil
}

// opBytes is a paint.ImageOp, and what NewImageOp allocates for one.
const opBytes = 64

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
	if p.dec == nil || p.busy || p.s.ended || p.dropped || time.Now().Before(p.retryAt) {
		return
	}
	p.busy = true
	t := (p.front + 1) % 3 // -1 (the still) → 0
	go c.decodeFrame(p, t)
}

func (c *Cache) decodeFrame(p *player, t int) {
	animSem <- struct{}{}
	i, err := p.dec.compose() // a charge that fails changes nothing: the same frame next time
	m := p.bufs[t]
	if err == nil {
		err = p.sc.scale(m, p.dec.canvas, p.budget)
	}
	<-animSem
	op := paint.NewImageOp(m)
	c.mu.Lock()
	p.busy = false
	if p.dropped {
		p.budget.Release()
		c.mu.Unlock()
		return
	}
	if err != nil { // the frame it shows stays; this one again shortly
		p.fails++
		p.retryAt = time.Now().Add(retryAfter(p.fails, budgetErr(err)))
		c.wakeAnimLocked(p.retryAt)
		c.mu.Unlock()
		return
	}
	p.fails = 0
	p.next, p.nextOp, p.nextB = i, op, t
	now := time.Now()
	late := !now.Before(p.s.shown.Add(p.ix.frames[p.s.cur].delay))
	if late {
		p.deadline = now.Add(animGrace)
	}
	inv := c.animInvalidateLocked()
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
	p.budget.Release() // the frame it stays on is the cache's (keep)
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

// releaseLocked stops e's player: e draws its still again, its live
// images too, so the frame one gave last (a streaming player's buffer, the
// size of a frame) is no longer held where neither budget counts it.
func (c *Cache) releaseLocked(e *entry) {
	for _, l := range e.lives {
		if l != nil {
			l.last = e.op
		}
	}
	p := e.play
	if p == nil {
		return
	}
	p.dropped = true
	if !p.busy { // else the worker gives it back once it's done
		p.budget.Release()
	}
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
	drawn := c.drawnAt
	for e := range c.playing {
		p := e.play
		if p.busy || p.keep && p.frames == nil || !now.After(p.deadline) {
			continue
		}
		// a keeping player nothing drew by its deadline is off screen once
		// the window drew something else since: one that drew nothing at
		// all is stalled on a long frame (or hidden), and its players stay
		// (they're small; the idle sweep lets a hidden window's images go)
		if p.keep && drawn < p.deadline.UnixMilli() {
			continue
		}
		c.releaseLocked(e)
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

// SetLiveOps makes Get (and GetPlaying) hand out an animated image as a
// live ImageOp (gioui.org/op/paint.NewLiveImageOp, Gio patch 35): the GPU
// draws the frame due when it draws the window's frame, and a frame drawn
// again from the ops it was drawn with (ui's quiet frames) plays it on. The
// GPU's ask counts as a draw, as a Get does, and only the paints a frame
// shows ask. Off (the default), Get returns the frame due at the Get. A
// build on stock Gio, which has no live images, stays off
// (gio_patch35_stock.go).
func (c *Cache) SetLiveOps(on bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.liveOps = on && liveOpsSupported
	c.mu.Unlock()
}

// live is an animated entry's paint.LiveImage (SetLiveOps): the frame due
// when the GPU asks, as Get draws it. A released player leaves it on the
// still (releaseLocked). An entry that's gone while a frame still draws it
// (the last frame's ops, drawn again) stays on the still, and asks once
// for a frame that lays out, whose Get loads it again.
type live struct {
	c      *Cache
	k      key
	forced bool
	making bool          // NewLiveImageOp asks while liveOpLocked holds mu
	last   paint.ImageOp // under mu
	gone   bool          // under mu: the entry went, a repaint was asked for
}

func (l *live) Image() paint.ImageOp {
	if l.making {
		return l.last
	}
	c := l.c
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[l.k]
	if !ok {
		if !l.gone {
			l.gone = true
			c.wakeLocked(now) // not inline: the GPU asks on the macOS main thread
		}
		return l.last
	}
	e := el.Value.(*entry)
	e.used = now.UnixMilli()
	c.drawnAt = e.used
	if op := c.frameLocked(e, now, l.forced); op.Size() == l.last.Size() {
		l.last = op
	}
	return l.last
}

// liveOpLocked is e's live ImageOp, made once from its still: the GPU
// draws its frame due in the still's place, of the still's size (the
// player's frames are, startPlay).
func (c *Cache) liveOpLocked(e *entry, forced bool) paint.ImageOp {
	i := 0
	if forced {
		i = 1
	}
	if e.lives[i] == nil {
		l := &live{c: c, k: e.k, forced: forced, making: true, last: e.op}
		e.live[i] = newLiveImageOp(l) // gio_patch35.go
		e.lives[i] = l
		l.making = false
	}
	return e.live[i]
}

// ReplayLive does for the live images (SetLiveOps) what a GPU drawing a
// frame again from its ops does: each one drawn in the last animGrace is
// asked for its frame now. For windows drawn without a GPU (headless tests
// and benchmarks), whose quiet frames nothing draws.
func (c *Cache) ReplayLive() {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.lru.Front(); el != nil; el = el.Next() {
		e := el.Value.(*entry)
		for i := range e.lives {
			if e.lives[i] != nil && now.Sub(e.seenAt) <= animGrace {
				e.used = now.UnixMilli()
				c.drawnAt = e.used
				c.frameLocked(e, now, i == 1)
			}
		}
	}
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

// waker repaints the window at the earliest time it was asked for (later
// ones are asked for again by the Get that the repaint brings).
type waker struct {
	at time.Time
	t  *time.Timer
}

// wakeLocked repaints the window at t (the earliest of the pending wakes)
// through invalidate: a load's retry, which may change the layout.
func (c *Cache) wakeLocked(t time.Time) { c.armLocked(&c.wake, t, c.fireWake) }

// wakeAnimLocked repaints the window at t through animInvalidate: an
// animation's next frame, a player's start or its retry.
func (c *Cache) wakeAnimLocked(t time.Time) { c.armLocked(&c.animWake, t, c.fireAnimWake) }

func (c *Cache) armLocked(w *waker, t time.Time, fire func()) {
	now := time.Now()
	if !w.at.IsZero() && w.at.After(now) && !t.Before(w.at) {
		return
	}
	w.at = t
	d := t.Sub(now)
	if w.t == nil {
		w.t = time.AfterFunc(d, fire)
	} else {
		w.t.Reset(d)
	}
}

func (c *Cache) fireWake() {
	c.mu.Lock()
	c.wake.at = time.Time{}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}

func (c *Cache) fireAnimWake() {
	c.mu.Lock()
	c.animWake.at = time.Time{}
	inv := c.animInvalidateLocked()
	c.mu.Unlock()
	inv()
}

// animInvalidateLocked is the repaint for animations (SetAnimInvalidate).
func (c *Cache) animInvalidateLocked() func() {
	if c.animInvalidate != nil {
		return c.animInvalidate
	}
	return c.invalidate
}
