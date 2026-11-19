package media

import (
	"container/list"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// The RangeSource's sizes (RangeOptions' defaults).
const (
	// BlockSize is a cached block's.
	BlockSize = 256 << 10
	// CacheMax caps a player's blocks, counted whole (a block partly
	// filled holds its 256 KB too).
	CacheMax = 16 << 20
	// ReadAhead is how far past the furthest byte read the GETs read on,
	// shared between them.
	ReadAhead = 8 << 20
	// nearAhead is how far past a GET's position a read waits for it
	// rather than starting another GET where the read is.
	nearAhead = 1 << 20
	// maxGets is how many GETs a seekable source has at once: two readers
	// far apart (a demuxer's video and audio, AVFoundation's overlapping
	// requests) each keep one instead of taking one GET from each other.
	maxGets = 2
	// tenure is how long a GET keeps its place before a read elsewhere may
	// take it, unless it brought nearAhead by then.
	tenure = 300 * time.Millisecond
)

// ErrClosed is a read of a closed RangeSource.
var ErrClosed = errors.New("media: source closed")

// ErrChanged is a read of a file that changed under the source: a later
// answer with another size or validator, or the whole file (a 200) after
// ranges (206s).
var ErrChanged = errors.New("media: the file changed while it played")

// ErrShort is a GET that ended short of what a read waits for, bringing
// nothing new.
var ErrShort = errors.New("media: the file's answer ended short")

// RangeOptions are a RangeSource's sizes; zero fields take the defaults.
type RangeOptions struct {
	Block, Cache, Ahead, Near int64
	// Log reports the file's one log line (log.Printf when nil).
	Log func(format string, args ...any)
	// Detail says what went wrong in an error from Source.Open for the
	// log, never its URL (the caller's slack.FailureDetail); nil says only
	// what kind of failure it was.
	Detail func(error) string
	// Logged is the set of files whose line was logged (one per file; the
	// package's when nil).
	Logged *sync.Map
}

// RangeSource reads a Source's file for a player: through at most two
// streaming GETs at once (Range: bytes=N-), keeping the bytes in memory in
// blocks (an LRU of CacheMax), reading ahead at most ReadAhead past the
// furthest byte read (a paused player's GETs stop there). A read the GETs
// aren't bringing starts one where its block lacks bytes (a file with its
// moov at the end jumps there once, and back); with two GETs already, the
// one no read waits on and least recently read takes its place, once it
// had its tenure. A server that ignores ranges (a 200) is read in order
// from byte 0 by one GET: a read behind what's cached starts it over. A
// GET that ends short of the read waiting for it, bringing nothing new,
// fails the read and holds off the next GET (1 s, doubling, or the
// answer's Retry-After). Nothing goes to disk.
//
// The first answer is logged once per file, with no URL: the status, the
// size, the ftyp brand and the video sample entry (or why it failed).
type RangeSource struct {
	src    Source
	opt    RangeOptions
	logf   func(string, ...any)
	logged *sync.Map

	mu       sync.Mutex
	changed  chan struct{} // closed and made again at every change
	blocks   map[int64]*rblock
	lru      list.List // of *rblock, the last used first
	cached   int64     // Block for each block held
	gets     []*rstream
	info     Info
	infoOK   bool
	err      error     // the last failure without progress
	errUntil time.Time // no new GET before then
	fails    int       // failures in a row without progress
	sticky   error     // the file changed: every read fails
	closed   bool
	stats    RangeStats
	inFlight int
	probe    *probe
	isLogged bool
	done     sync.WaitGroup
}

// RangeStats are a RangeSource's counts.
type RangeStats struct {
	// Requests are the GETs made, Restarts those that took another GET's
	// place, Failures those that ended without bringing anything new,
	// BytesRead what they brought, Merged those that ended where another
	// had already brought the bytes on.
	Requests, Restarts, Failures, BytesRead, Merged int64
	// MaxInFlight is the most bodies open at once (at most maxGets).
	MaxInFlight int
	// Cached is the memory the blocks hold now.
	Cached int64
}

type rblock struct {
	idx  int64
	data []byte // the block's bytes from its start: len is what's there
	el   *list.Element
}

// rstream is a GET.
type rstream struct {
	start int64 // the body's first byte
	pos   int64 // the next byte it brings
	// furthest is the end of the furthest read served from its bytes: it
	// reads on to furthest+Ahead/GETs.
	furthest int64
	opening  bool
	ended    bool
	cancel   context.CancelFunc
	closed   chan struct{} // the body is closed (or was never opened)
	added    int64         // bytes it added to the cache
	began    time.Time
	served   time.Time // when a read last took its bytes
	waiters  int       // reads waiting for its bytes now
}

// NewRangeSource reads src.
func NewRangeSource(src Source, opt RangeOptions) *RangeSource {
	if opt.Block <= 0 {
		opt.Block = BlockSize
	}
	if opt.Cache <= 0 {
		opt.Cache = CacheMax
	}
	if opt.Ahead <= 0 {
		opt.Ahead = ReadAhead
	}
	if opt.Near <= 0 {
		opt.Near = nearAhead
	}
	if opt.Detail == nil {
		opt.Detail = failureKind
	}
	s := &RangeSource{src: src, opt: opt, logf: opt.Log, logged: opt.Logged, changed: make(chan struct{}), blocks: map[int64]*rblock{}, probe: newProbe()}
	if s.logf == nil {
		s.logf = log.Printf
	}
	if s.logged == nil {
		s.logged = &loggedFiles
	}
	s.info.Total = -1
	return s
}

// failureKind says what kind of failure err is, and nothing it carries (a
// URL, a file name).
func failureKind(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled), errors.Is(err, ErrClosed):
		return "cancelled"
	case errors.As(err, &ne) && ne.Timeout():
		return "timed out"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, ErrShort):
		return "the answer ended short"
	case errors.Is(err, ErrChanged):
		return "the file changed"
	}
	return "the request failed"
}

// Detail is what the log may say of err (the opener's errors carry its
// URL): RangeOptions.Detail.
func (s *RangeSource) Detail(err error) string { return s.opt.Detail(err) }

// broadcast wakes every waiter. Call with mu held.
func (s *RangeSource) broadcast() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// wait waits for a change or ctx, with mu held around it.
func (s *RangeSource) wait(ctx context.Context) error {
	ch := s.changed
	s.mu.Unlock()
	defer s.mu.Lock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// failing is the error reads get now: the file changed, or a failure
// without progress whose hold is on. Call with mu held.
func (s *RangeSource) failing() error {
	if s.sticky != nil {
		return s.sticky
	}
	if s.err != nil && time.Now().Before(s.errUntil) {
		return s.err
	}
	return nil
}

// Info is the server's first answer, asking for the file from byte 0 if
// nothing has yet.
func (s *RangeSource) Info(ctx context.Context) (Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.closed {
			return Info{}, ErrClosed
		}
		if s.infoOK {
			return s.info, nil
		}
		if err := s.failing(); err != nil {
			return Info{}, err
		}
		if len(s.gets) == 0 {
			s.start(0, nil)
		}
		if err := s.wait(ctx); err != nil {
			return Info{}, err
		}
	}
}

// ReadSome reads the bytes at off into p as soon as there are some: it
// returns at least one byte, io.EOF at the end of the file, the GETs'
// error, ErrClosed, or ctx's error.
func (s *RangeSource) ReadSome(ctx context.Context, p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, errors.New("media: negative offset")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.closed {
			return 0, ErrClosed
		}
		if s.sticky != nil {
			return 0, s.sticky // what's cached may be the old version's too
		}
		if s.infoOK && s.info.Total >= 0 && off >= s.info.Total {
			return 0, io.EOF
		}
		if n := s.copyAt(p, off); n > 0 {
			s.served(off, off+int64(n))
			return n, nil
		}
		if err := s.failing(); err != nil {
			return 0, err
		}
		if g := s.covering(off); g != nil {
			if end := off + int64(len(p)); end > g.furthest {
				g.furthest = end // reading on toward it
				s.broadcast()
			}
			g.waiters++
			err := s.wait(ctx)
			g.waiters--
			if err != nil {
				return 0, err
			}
			continue
		}
		if old, ok := s.slot(); ok {
			s.start(off, old)
		}
		if err := s.wait(ctx); err != nil {
			return 0, err
		}
	}
}

// ReadAt implements io.ReaderAt (until Close).
func (s *RangeSource) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) {
		m, err := s.ReadSome(context.Background(), p[n:], off+int64(n))
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// served notes a read of [off, end): the GET whose bytes it was reads on
// past it. Call with mu held.
func (s *RangeSource) served(off, end int64) {
	for _, g := range s.gets {
		if !g.ended && off >= g.start && off < g.pos+s.opt.Near {
			g.served = time.Now()
			if end > g.furthest {
				g.furthest = end
				s.broadcast()
			}
		}
	}
}

// seekable is whether reads may start GETs elsewhere than byte 0. Call
// with mu held.
func (s *RangeSource) seekable() bool { return !s.infoOK || s.info.Seekable }

// covering is the GET that brings off soon: at or after its position,
// within Near of it (any distance on a source that isn't seekable, which
// can only be read on). Call with mu held.
func (s *RangeSource) covering(off int64) *rstream {
	for _, g := range s.gets {
		if g.ended || off < g.pos {
			continue
		}
		if !s.seekable() || off < g.pos+s.opt.Near {
			return g
		}
	}
	return nil
}

// slot is where a new GET may go: a free place (nil), or the GET it
// takes the place of (no read waits on it, it had its tenure, the least
// recently read). false: none yet. Call with mu held.
func (s *RangeSource) slot() (*rstream, bool) {
	most := maxGets
	if !s.seekable() {
		most = 1
	}
	if len(s.gets) < most {
		return nil, true
	}
	var pick *rstream
	for _, g := range s.gets {
		if g.ended {
			continue // closing: its place frees itself
		}
		if g.waiters > 0 && s.seekable() {
			continue
		}
		if g.opening || (g.added < s.opt.Near && time.Since(g.began) < tenure) {
			continue
		}
		if pick == nil || g.served.Before(pick.served) {
			pick = g
		}
	}
	return pick, pick != nil
}

// firstMissing is where a GET bringing off starts: its block's first byte
// not cached (from 0 on a source that isn't seekable). Call with mu held.
func (s *RangeSource) firstMissing(off int64) int64 {
	if !s.seekable() {
		return 0
	}
	idx := off / s.opt.Block
	at := idx * s.opt.Block
	if bl := s.blocks[idx]; bl != nil {
		at += int64(len(bl.data))
	}
	return min(at, off)
}

// start starts a GET bringing off, in old's place (nil: a place of its
// own). Call with mu held.
func (s *RangeSource) start(off int64, old *rstream) {
	at := s.firstMissing(off)
	var prev chan struct{}
	if old != nil {
		old.ended = true
		old.cancel()
		prev = old.closed
		s.stats.Restarts++
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	g := &rstream{start: at, pos: at, furthest: max(at, off), opening: true, cancel: cancel, closed: make(chan struct{}), began: now, served: now}
	s.gets = append(s.gets, g)
	s.stats.Requests++
	s.done.Add(1)
	go s.run(ctx, g, prev)
}

// remove takes g out of the GETs, its body closed. Call with mu held.
func (s *RangeSource) remove(g *rstream) {
	for i, x := range s.gets {
		if x == g {
			s.gets = append(s.gets[:i], s.gets[i+1:]...)
			break
		}
	}
	s.broadcast()
}

// backoff is the hold after the n-th failure in a row: 1 s, doubling, at
// most 30 s, or longer when the server's Retry-After says so.
func backoff(n int, err error) time.Duration {
	d := time.Second << min(n-1, 5)
	d = min(d, 30*time.Second)
	var ra interface{ RetryAfterDuration() time.Duration }
	if errors.As(err, &ra) && ra.RetryAfterDuration() > d {
		d = ra.RetryAfterDuration()
	}
	return d
}

// failed records a failure without progress: reads waiting get err, and
// no GET starts for the backoff. Call with mu held.
func (s *RangeSource) failed(err error) {
	s.fails++
	s.stats.Failures++
	s.err, s.errUntil = err, time.Now().Add(backoff(s.fails, err))
	s.broadcast()
}

// same is whether a later answer is the file the first one was. Call with
// mu held.
func (s *RangeSource) same(info Info) bool {
	if s.info.Seekable && !info.Seekable {
		return false // the whole file after ranges
	}
	if s.info.Total >= 0 && info.Total >= 0 && s.info.Total != info.Total {
		return false
	}
	if s.info.ETag != "" && info.ETag != "" && s.info.ETag != info.ETag {
		return false
	}
	return s.info.LastModified == "" || info.LastModified == "" || s.info.LastModified == info.LastModified
}

// run is a GET's goroutine: it opens the file at g.start once the GET it
// replaced closed its body, then puts what it brings in the cache until
// it ends, reading at most its share of Ahead past its furthest read.
func (s *RangeSource) run(ctx context.Context, g *rstream, prev chan struct{}) {
	defer s.done.Done()
	defer func() {
		s.mu.Lock()
		s.remove(g)
		s.mu.Unlock()
		close(g.closed)
	}()
	if prev != nil {
		<-prev // the one it replaced closed its body first: one in its place
	}
	if ctx.Err() != nil {
		return
	}
	body, info, err := s.src.Open(ctx, g.start)
	s.mu.Lock()
	if err != nil || s.closed || g.ended {
		if body != nil {
			body.Close()
		}
		switch {
		case err == nil || g.ended || s.closed || ctx.Err() != nil:
		case info.Status == 416 && info.Total >= 0:
			// a read past the end of a file the answers didn't size: the
			// end is there, which is no failure
			if !s.infoOK {
				s.info, s.infoOK = Info{Status: info.Status, Total: info.Total, Seekable: true}, true
			} else if s.info.Total < 0 {
				s.info.Total = info.Total
			}
			s.broadcast()
		default:
			if !s.infoOK {
				s.logFailure(info, err)
			}
			s.failed(err)
		}
		g.ended = true
		s.mu.Unlock()
		return
	}
	s.inFlight++
	s.stats.MaxInFlight = max(s.stats.MaxInFlight, s.inFlight)
	switch {
	case !s.infoOK:
		s.info, s.infoOK = info, true
	case !s.same(info):
		s.sticky = ErrChanged
		g.ended = true
		s.broadcast()
	}
	if !info.Seekable {
		g.start, g.pos = 0, 0 // the whole file from byte 0
	}
	g.opening = false
	s.broadcast()
	s.mu.Unlock()
	defer func() {
		body.Close()
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()

	buf := make([]byte, 32<<10)
	for {
		s.mu.Lock()
		for !s.closed && !g.ended && g.pos >= g.furthest+s.share() {
			s.wait(ctx)
		}
		if s.closed || g.ended {
			s.mu.Unlock()
			return
		}
		if s.overtaken(g) {
			// the bytes on from here are another GET's, in the cache: this
			// one would bring them again (AVFoundation's two requests to
			// the end, a GET each, read the whole file twice). Its readers
			// read them from the cache, and on from the first byte
			// missing through another GET.
			g.ended = true
			s.stats.Merged++
			s.broadcast()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		n, err := body.Read(buf)
		s.mu.Lock()
		if s.closed || g.ended {
			s.mu.Unlock()
			return
		}
		if n > 0 {
			s.put(g, buf[:n])
			s.broadcast()
		}
		end := s.info.Total >= 0 && g.pos >= s.info.Total
		if err != nil || end {
			if errors.Is(err, io.EOF) || end {
				if s.info.Total < 0 {
					s.info.Total = g.pos // the end of a file the server didn't size
				} else if g.pos < s.info.Total {
					err = ErrShort // a clean end before the file's
				}
				if err == io.EOF || end {
					err = nil
				}
				s.probe.finish()
				s.logProbe(false)
			}
			g.ended = true
			if err != nil && g.added == 0 {
				s.failed(err) // nothing new: the read waiting gets the error
			}
			s.broadcast()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// overtaken is whether g's next byte is cached already: another GET
// brought it (g brings only bytes past its own). Its readers take them from
// the cache, and a GET starts again at the first byte missing, if any.
// Call with mu held.
func (s *RangeSource) overtaken(g *rstream) bool {
	idx := g.pos / s.opt.Block
	bl := s.blocks[idx]
	return bl != nil && int64(len(bl.data)) > g.pos-idx*s.opt.Block
}

// share is each GET's read-ahead. Call with mu held.
func (s *RangeSource) share() int64 {
	n := 0
	for _, g := range s.gets {
		if !g.ended {
			n++
		}
	}
	return s.opt.Ahead / int64(max(n, 1))
}

// put adds b, g's next bytes, to the cache. Call with mu held.
func (s *RangeSource) put(g *rstream, b []byte) {
	s.probe.feed(g.pos, b)
	s.logProbe(false)
	s.stats.BytesRead += int64(len(b))
	for len(b) > 0 {
		idx := g.pos / s.opt.Block
		in := g.pos - idx*s.opt.Block
		bl := s.blocks[idx]
		if bl == nil && in == 0 {
			bl = &rblock{idx: idx, data: make([]byte, 0, s.opt.Block)}
			bl.el = s.lru.PushFront(bl)
			s.blocks[idx] = bl
			s.cached += s.opt.Block
		} else if bl != nil {
			s.lru.MoveToFront(bl.el)
		}
		n := min(int64(len(b)), s.opt.Block-in)
		if bl != nil {
			switch have := int64(len(bl.data)); {
			case in > have:
				// a gap (the block went while the GET was in it): it comes
				// again from its first missing byte
			case in+n > have:
				bl.data = append(bl.data, b[have-in:n]...)
				g.added += in + n - have
				s.fails = 0 // progress
			}
		}
		g.pos += n
		b = b[n:]
	}
	s.evict()
}

// evict lets blocks go while the cache is over its cap, the least
// recently used first: first those no GET brought past its furthest read
// (a GET's read-ahead, which no read took yet), then any; never one a GET
// is filling. Read-ahead goes last because LRU alone misjudges it: blocks
// a GET brought ahead and nothing has read since are older than blocks a
// reader behind is reading again (AVFoundation's second request to the
// end begins behind where the first was dropped), so they went first and
// a GET brought them again. Call with mu held.
func (s *RangeSource) evict() {
	for pass := 0; pass < 2 && s.cached > s.opt.Cache; pass++ {
		for el := s.lru.Back(); el != nil && s.cached > s.opt.Cache; {
			prev := el.Prev()
			if bl := el.Value.(*rblock); !s.held(bl, pass == 0) {
				s.lru.Remove(el)
				delete(s.blocks, bl.idx)
				s.cached -= s.opt.Block
			}
			el = prev
		}
	}
}

// held is whether bl stays: a GET is filling it, or (ahead) it holds bytes
// a GET brought past its furthest read. Call with mu held.
func (s *RangeSource) held(bl *rblock, ahead bool) bool {
	lo, hi := bl.idx*s.opt.Block, (bl.idx+1)*s.opt.Block
	for _, g := range s.gets {
		if g.ended {
			continue
		}
		if g.pos/s.opt.Block == bl.idx || ahead && hi > g.furthest && lo < g.pos {
			return true
		}
	}
	return false
}

// copyAt copies the cached bytes at off into p, as far as they're
// contiguous. Call with mu held.
func (s *RangeSource) copyAt(p []byte, off int64) int {
	n := 0
	for n < len(p) {
		at := off + int64(n)
		idx := at / s.opt.Block
		bl := s.blocks[idx]
		if bl == nil {
			break
		}
		in := at - idx*s.opt.Block
		if in >= int64(len(bl.data)) {
			break
		}
		s.lru.MoveToFront(bl.el)
		c := copy(p[n:], bl.data[in:])
		n += c
		if in+int64(c) < s.opt.Block && int64(len(bl.data)) < s.opt.Block {
			break // the block's end isn't here yet
		}
	}
	if s.infoOK && s.info.Total >= 0 && off+int64(n) > s.info.Total {
		n = int(s.info.Total - off)
	}
	return max(n, 0)
}

// loggedFiles is the files whose line was logged (RangeOptions.Logged's
// default).
var loggedFiles sync.Map

// claimLog is whether this source logs its file's line (the first of the
// file's). Call with mu held.
func (s *RangeSource) claimLog() bool {
	if s.isLogged {
		return false
	}
	s.isLogged = true
	if s.src.FileID == "" {
		return true
	}
	_, dup := s.logged.LoadOrStore(s.src.FileID, true)
	return !dup
}

// logFailure logs the file's line for a first GET that failed: the
// status, if there was an answer, and what kind of failure. Call with mu
// held.
func (s *RangeSource) logFailure(info Info, err error) {
	if !s.claimLog() {
		return
	}
	if info.Status != 0 {
		s.logf("media: %s: http %d, the file wasn't served: %s", s.src.FileID, info.Status, s.opt.Detail(err))
		return
	}
	s.logf("media: %s: the file wasn't served: %s", s.src.FileID, s.opt.Detail(err))
}

// logProbe logs the file's line once its probe is done (or now, at
// Close). Call with mu held.
func (s *RangeSource) logProbe(now bool) {
	if s.isLogged || !s.infoOK || (!now && !s.probe.done()) || !s.claimLog() {
		return
	}
	p := s.probe.result()
	entry := p.Entry
	if entry == "" {
		entry = "?"
	}
	brand := p.Brand
	if brand == "" {
		brand = "?"
	}
	where := ""
	if p.MoovAt >= 0 && s.info.Total > 0 && p.MoovAt > s.info.Total/2 {
		where = ", moov at the end"
	}
	s.logf("media: %s: http %d, %d bytes, seekable %v, ftyp %q, video %q%s", s.src.FileID, s.info.Status, s.info.Total, s.info.Seekable, brand, entry, where)
}

// Probe is what the file's boxes said so far.
func (s *RangeSource) Probe() MP4Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probe.result()
}

// Stats are the source's counts.
func (s *RangeSource) Stats() RangeStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Cached = s.cached
	return st
}

// Close ends the GETs and every read; it returns at once (Wait waits for
// the GETs' goroutines).
func (s *RangeSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.logProbe(true)
	s.closed = true
	for _, g := range s.gets {
		g.ended = true
		g.cancel()
	}
	s.blocks, s.cached = map[int64]*rblock{}, 0
	s.lru.Init()
	s.broadcast()
	return nil
}

// Wait waits until the GETs' goroutines are gone (after Close).
func (s *RangeSource) Wait() { s.done.Wait() }
