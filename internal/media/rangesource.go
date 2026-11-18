package media

import (
	"container/list"
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"time"
)

// The RangeSource's sizes (RangeOptions' defaults).
const (
	// BlockSize is a cached block's.
	BlockSize = 256 << 10
	// CacheMax caps a player's blocks.
	CacheMax = 16 << 20
	// ReadAhead is how far past the furthest byte read the GET reads on.
	ReadAhead = 8 << 20
	// nearAhead is how far past the GET's position a read waits for it
	// rather than starting the GET again where the read is.
	nearAhead = 1 << 20
)

// ErrClosed is a read of a closed RangeSource.
var ErrClosed = errors.New("media: source closed")

// RangeOptions are a RangeSource's sizes; zero fields take the defaults.
type RangeOptions struct {
	Block, Cache, Ahead, Near int64
	// Log reports the file's one log line (log.Printf when nil).
	Log func(format string, args ...any)
}

// RangeSource reads a Source's file for a player: through at most one
// streaming GET at once (Range: bytes=N-), keeping the bytes in memory in
// blocks (an LRU of CacheMax), reading ahead at most ReadAhead past the
// furthest byte read from the GET (a paused player's GET stops there). A
// read outside what the GET is bringing starts it again where the read is
// (a file with its moov at the end jumps there once, and back), unless a
// read the GET was started for still waits. A server that ignores ranges
// (a 200) is read in order from byte 0: a read behind what's cached starts
// over from 0. Nothing goes to disk.
//
// The first answer is logged once per file, with no URL: the status, the
// size, the ftyp brand and the video sample entry.
type RangeSource struct {
	src  Source
	opt  RangeOptions
	logf func(string, ...any)

	mu      sync.Mutex
	changed chan struct{} // closed and made again at every change
	blocks  map[int64]*rblock
	lru     list.List // of *rblock, the last used first
	cached  int64
	st      *rstream // the GET, nil for none
	info    Info
	infoOK  bool
	// err is the last GET's failure to open, returned to reads until
	// errUntil (a source that fails isn't asked again at once)
	err      error
	errUntil time.Time
	closed   bool
	stats    RangeStats
	inFlight int
	probe    *probe
	logged   bool
	done     sync.WaitGroup
}

// RangeStats are a RangeSource's counts.
type RangeStats struct {
	// Requests are the GETs made, Restarts those that replaced one
	// bringing other bytes, BytesRead what they brought.
	Requests, Restarts, BytesRead int64
	// MaxInFlight is the most bodies open at once (the budget: 1).
	MaxInFlight int
	// Cached is the bytes held now.
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
	// reads on to furthest+Ahead.
	furthest int64
	// target is the read it was started for, until that read returns: no
	// other read starts the GET again meanwhile.
	target  *int
	opening bool
	ended   bool
	err     error
	cancel  context.CancelFunc
	closed  chan struct{} // the body is closed (or never opened)
	brought int64
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
	s := &RangeSource{src: src, opt: opt, logf: opt.Log, changed: make(chan struct{}), blocks: map[int64]*rblock{}, probe: newProbe()}
	if s.logf == nil {
		s.logf = log.Printf
	}
	return s
}

// broadcast wakes every waiter. Call with mu held.
func (s *RangeSource) broadcast() {
	close(s.changed)
	s.changed = make(chan struct{})
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
		if s.err != nil && time.Now().Before(s.errUntil) {
			return Info{}, s.err
		}
		if s.st == nil {
			s.start(0, nil)
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			s.mu.Lock()
			return Info{}, ctx.Err()
		}
		s.mu.Lock()
	}
}

// ReadSome reads the bytes at off into p as soon as there are some: it
// returns at least one byte, io.EOF at the end of the file, the GET's
// error, ErrClosed, or ctx's error.
func (s *RangeSource) ReadSome(ctx context.Context, p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, errors.New("media: negative offset")
	}
	me := new(int)
	s.mu.Lock()
	defer func() {
		// a read that returns lets its GET be started again elsewhere
		if s.st != nil && s.st.target == me {
			s.st.target = nil
			s.broadcast()
		}
		s.mu.Unlock()
	}()
	var mine *rstream
	for {
		if s.closed {
			return 0, ErrClosed
		}
		if s.infoOK && s.info.Total >= 0 && off >= s.info.Total {
			return 0, io.EOF
		}
		if n := s.copyAt(p, off); n > 0 {
			if st := s.st; st != nil && off >= st.start && off+int64(n) > st.furthest {
				st.furthest = off + int64(n)
				s.broadcast() // its read-ahead moves on
			}
			return n, nil
		}
		if mine != nil && mine.ended && mine.brought == 0 && mine.err != nil {
			return 0, mine.err // the GET started for this read failed
		}
		if s.err != nil && time.Now().Before(s.errUntil) {
			return 0, s.err
		}
		st := s.st
		switch {
		case st != nil && s.inWindow(st, off):
			if end := off + int64(len(p)); end > st.furthest {
				st.furthest = end // reading on toward it
				s.broadcast()
			}
		case st != nil && st.target != nil && st.target != me:
			// the GET is serving a read that hasn't got its bytes yet
		default:
			s.start(off, me)
			mine = s.st
		}
		ch := s.changed
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			s.mu.Lock()
			return 0, ctx.Err()
		}
		s.mu.Lock()
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

// inWindow is whether st brings off soon: at or after its position, within
// Near of it (any distance on a source that isn't seekable, which can only
// be read on).
func (s *RangeSource) inWindow(st *rstream, off int64) bool {
	if st.ended {
		return false
	}
	if off < st.pos {
		return false // gone from the cache: only a new GET brings it
	}
	if s.infoOK && !s.info.Seekable {
		return true
	}
	return off < st.pos+s.opt.Near
}

// start makes the GET bring off: from off's block, or from byte 0 on a
// source that isn't seekable. Call with mu held.
func (s *RangeSource) start(off int64, target *int) {
	at := off / s.opt.Block * s.opt.Block
	if s.infoOK && !s.info.Seekable {
		at = 0
	}
	old := s.st
	if old != nil {
		old.ended = true
		old.cancel()
		s.stats.Restarts++
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &rstream{start: at, pos: at, furthest: at, target: target, opening: true, cancel: cancel, closed: make(chan struct{})}
	s.st = st
	s.err = nil
	s.stats.Requests++
	s.done.Add(1)
	var prev chan struct{}
	if old != nil {
		prev = old.closed
	}
	go s.run(ctx, st, prev)
}

// run is a GET's goroutine: it opens the file at st.start once the GET
// before it closed its body (one in flight), then puts what it brings in
// the cache until it ends, reading at most Ahead past its furthest read.
func (s *RangeSource) run(ctx context.Context, st *rstream, prev chan struct{}) {
	defer s.done.Done()
	defer close(st.closed)
	if prev != nil {
		// even a GET replaced before it opened waits: the one after it
		// waits for it, so none opens before every one before it closed
		<-prev
	}
	if ctx.Err() != nil {
		return
	}
	body, info, err := s.src.Open(ctx, st.start)
	s.mu.Lock()
	if err != nil || s.closed || st.ended {
		if body != nil {
			body.Close()
		}
		if err != nil && !st.ended && !s.closed {
			if ctx.Err() == nil {
				s.err, s.errUntil = err, time.Now().Add(time.Second)
			}
			st.ended, st.err = true, err
			if s.st == st {
				s.st = nil
			}
			s.broadcast()
		}
		s.mu.Unlock()
		return
	}
	s.inFlight++
	s.stats.MaxInFlight = max(s.stats.MaxInFlight, s.inFlight)
	if !s.infoOK {
		s.info, s.infoOK = info, true
	}
	if !info.Seekable {
		// the whole file from byte 0
		st.start, st.pos = 0, 0
		if s.info.Seekable {
			s.info.Seekable = false
		}
	}
	st.opening = false
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
		for !s.closed && !st.ended && st.pos >= st.furthest+s.opt.Ahead {
			ch := s.changed
			s.mu.Unlock()
			select {
			case <-ch:
			case <-ctx.Done():
			}
			s.mu.Lock()
		}
		if s.closed || st.ended {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		n, err := body.Read(buf)
		s.mu.Lock()
		if s.closed || st.ended {
			s.mu.Unlock()
			return
		}
		if n > 0 {
			s.put(st, buf[:n])
			s.broadcast()
		}
		end := s.infoOK && s.info.Total >= 0 && st.pos >= s.info.Total
		if err != nil || end {
			if errors.Is(err, io.EOF) || end {
				err = nil
				if s.info.Total < 0 {
					s.info.Total = st.pos // the end of a file the server didn't size
				}
				s.probe.finish()
				s.logProbe(false)
			} else if st.brought == 0 {
				s.err, s.errUntil = err, time.Now().Add(time.Second)
			}
			st.ended, st.err = true, err
			if s.st == st {
				s.st = nil
			}
			s.broadcast()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// put adds b, the GET's next bytes, to the cache. Call with mu held.
func (s *RangeSource) put(st *rstream, b []byte) {
	s.probe.feed(st.pos, b)
	s.logProbe(false)
	s.stats.BytesRead += int64(len(b))
	st.brought += int64(len(b))
	for len(b) > 0 {
		idx := st.pos / s.opt.Block
		in := st.pos - idx*s.opt.Block
		bl := s.blocks[idx]
		if bl == nil {
			bl = &rblock{idx: idx, data: make([]byte, 0, s.opt.Block)}
			bl.el = s.lru.PushFront(bl)
			s.blocks[idx] = bl
		} else {
			s.lru.MoveToFront(bl.el)
		}
		n := min(int64(len(b)), s.opt.Block-in)
		switch have := int64(len(bl.data)); {
		case in > have:
			// a gap (the block went while the GET was in it): it'll come
			// again from the block's start
		case in+n > have:
			s.cached += in + n - have
			bl.data = append(bl.data, b[have-in:n]...)
		}
		st.pos += n
		b = b[n:]
	}
	cur := (st.pos - 1) / s.opt.Block
	for s.cached > s.opt.Cache {
		el := s.lru.Back()
		for el != nil && el.Value.(*rblock).idx == cur {
			el = el.Prev()
		}
		if el == nil {
			break
		}
		bl := el.Value.(*rblock)
		s.lru.Remove(el)
		delete(s.blocks, bl.idx)
		s.cached -= int64(len(bl.data))
	}
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

// logged is the files whose line was logged.
var logged sync.Map

// logProbe logs the file's line once its probe is done (or now, at
// Close). Call with mu held.
func (s *RangeSource) logProbe(now bool) {
	if s.logged || !s.infoOK || (!now && !s.probe.done()) {
		return
	}
	s.logged = true
	if s.src.FileID != "" {
		if _, dup := logged.LoadOrStore(s.src.FileID, true); dup {
			return
		}
	}
	p := s.probe.info
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

// Close ends the GET and every read; it returns at once (Wait waits for
// the GET's goroutine).
func (s *RangeSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.logProbe(true)
	s.closed = true
	if s.st != nil {
		s.st.ended = true
		s.st.cancel()
		s.st = nil
	}
	s.blocks, s.cached = map[int64]*rblock{}, 0
	s.lru.Init()
	s.broadcast()
	return nil
}

// Wait waits until the GET's goroutine is gone (after Close).
func (s *RangeSource) Wait() { s.done.Wait() }
