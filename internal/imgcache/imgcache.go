// Package imgcache fetches and decodes images off the UI goroutine and keeps
// them in a memory LRU. Avatars and emoji (metadata under the Hack Club
// scraping policy) are also cached on disk, in the account's cache
// directory (SetDir: <cache root>/v1/<team>-<user>/img, dropped on
// sign-out), at most DiskLimit bytes, least recently used first out;
// message files, thumbnails and attachments (files.slack.com and anything
// else) stay in memory only.
//
// Get never blocks: it returns (op, true) when the image is ready and
// otherwise starts a fetch, returns false, and calls invalidate when the
// image arrives so the window repaints. Callers draw Slack's placeholder
// (the base-ter tile) meanwhile. A failed fetch is tried again with
// backoff (retryAfter), and the window repaints when the retry is due, so
// a visible avatar that hit a timeout or a dropped connection loads by
// itself.
//
// Every decode, and the scale after it, charges what it allocates to the
// process-wide decode memory (decmem.go): decoding never takes more than
// DecodeMemory in all, and one that can't have its memory now tries again
// shortly.
package imgcache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	// The standard decoders stay registered with package image for its
	// DecodeConfig (core's photo checks, the composer's and the emoji
	// dialog's size checks read headers through it); imgcache itself
	// decodes only through the budgeted copies in internal (decmem.go).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/op/paint"
	_ "golang.org/x/image/bmp" // registered for DecodeConfig too (composer attachments)

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
	"github.com/some-du6e/clack/internal/imgcache/internal/webp"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/slack"
)

// WebP is decoded by the vendored, budget-charging decoder in
// internal/webp and internal/vp8l (webpanim.go), never x/image/webp's,
// whose vp8l allocates its Huffman groups before reading them. x/image's
// webp is not imported for its init, which would register that unbounded
// decoder with package image, so image.Decode would run it. Instead the
// format is registered here with the vendored decoder's header reader —
// what image.DecodeConfig needs to size-gate a WebP everywhere — and a
// full decode that routes through the budgeted Decode, so even a direct
// image.Decode of a WebP can't allocate out of proportion.
func init() {
	image.RegisterFormat("webp", "RIFF????WEBPVP8", decodeWebPRegistered, webp.DecodeConfig)
}

// decodeWebPRegistered is the package image decoder for WebP: it buffers
// the reader (capped, as the bytes are untrusted) and hands them to
// Decode, so a registry decode is gated and budgeted like every other.
func decodeWebPRegistered(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFetchBytes+1))
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// Fetcher downloads url. For files.slack.com it must authenticate (core.Fetch).
type Fetcher func(ctx context.Context, url string) ([]byte, error)

// Cache is safe for concurrent use.
type Cache struct {
	fetch      Fetcher
	dir        string // the disk tier ("" for none); under mu
	invalidate func()

	diskLimit int64 // DiskLimit unless SetDiskLimit
	written   int64 // bytes saved since the last trim (under mu)
	trimming  bool

	mu       sync.Mutex
	entries  map[key]*list.Element
	byURL    map[string]map[int]*list.Element // entries per URL, by size (derive)
	lru      *list.List                       // front = most recent
	bytes    int
	maxBytes int
	inflight map[key]bool
	failed   map[key]failure
	// held: busy loads' bytes for their next try (holdLocked), heldBytes of
	// them, counted in bytes and at most 1/heldShare of the budget
	held      map[key]held
	heldBytes int

	// Two fetch lanes: avatars and emoji (DiskOK URLs, a few KB each) never
	// wait behind message images, files and unfurls, which can take the
	// whole 45 s timeout each. Each lane is a queue that at most
	// laneWorkers goroutines work through, in the order the images were
	// asked for (under mu). A goroutine per request waiting its turn cost
	// 8 KB of stack each: scrolling the emoji picker through Hack Club's
	// 65,000 emoji parked 30,000 of them, 250 MB.
	//
	// An image asked for again while it waits (Get draws it every frame:
	// it's on screen) also goes in its lane's hot queue, which the workers
	// take first: the emoji of the message list don't wait behind the
	// thousands a picker scroll or the last channel left queued. A hot one
	// no frame drew in the last animGrace of frames by the time its turn
	// comes goes back to its place in the queue. Every queued image still
	// loads: after hotRunMax hot ones in a row a lane takes its queue's
	// head.
	queue   [2][]key
	hot     [2][]key
	hotRun  [2]int         // hot loads each lane took in a row
	waiting map[key]queued // queued, not started yet
	workers [2]int
	ctx     context.Context
	// drawnAt is when Get last drew anything (unix milliseconds): the
	// latest frame. An idle window draws none, and what it showed last is
	// on screen still.
	drawnAt int64
	// the frame being laid out (FrameBegin) and what its Gets drew: the
	// entries and the waiting loads a frame drawn again from its ops
	// (FrameRedrawn) draws again
	gen      uint32
	genEls   []*list.Element
	genLoads []key

	// the next repaints (anim.go wakeLocked): wake for a load's retry, a
	// layout's worth (invalidate); animWake for an animation's next frame
	// or a player's start (animInvalidate, which a window may answer by
	// showing its last frame again with its animated images moved on)
	wake, animWake waker
	animInvalidate func() // nil: invalidate
	// liveOps: Get hands out an animated image as a live ImageOp (anim.go
	// SetLiveOps); liveHeld: the live images show the frame they showed
	// when hold holdGen began (HoldLive)
	liveOps  bool
	liveHeld bool
	holdGen  uint32

	// animations (anim.go): whether Get plays them (SetAnimate), the
	// entries with a player playing, and the sweep that stops them once
	// nothing draws them
	animate  bool
	playing  map[*entry]struct{}
	animT    *time.Timer
	advances uint64 // frames the players moved on (Stats)
	skipped  uint64 // frames their timelines passed over, late (Stats)
	gifBytes int    // the entries' encoded GIF bytes (e.gif): at most gifShare of the budget
}

// failure is a key's failed load: how many in a row, and when Get may try
// again. Each retry repaints the window when it's due (wakeLocked), so an
// image on screen comes back on its own after a dropped connection, a
// timeout or a 5xx, instead of staying a placeholder until something else
// redraws the window.
//
// A final failure (bytes that arrived whole and don't decode:
// ErrUndecodable) isn't tried again for finalFailureTTL while the account
// is signed in: the same bytes would fail the same way. Slack's <img>
// doesn't retry an onError either. A busy one (ErrBusy: other decodes held
// the decode memory) is tried again within RetryBusyMax, with the bytes it
// already fetched (held), and for busyTries tries isn't a failure to the
// surfaces asking (Failed): the image is still loading. Past them it has
// failed (the lightbox shows the file's largest thumbnail instead) and is
// tried again as a failure that may pass, RetryMin doubling to RetryMax,
// still with its bytes: so under memory pressure that doesn't end, nothing
// spins for good, and the image loads once the pressure is over.
type failure struct {
	at    time.Time
	n     int
	retry time.Time
	final bool
	busy  bool // busy and still loading (Failed is false)
	busyN int  // busy failures in a row
}

// busyTries is how many busy failures in a row an image is still loading
// for; after them it has failed (and is tried again later).
const busyTries = 3

// errBusyLong is a load busy past busyTries: tried again as an error that
// may pass (RetryMin doubling to RetryMax), not every RetryBusyMax.
var errBusyLong = errors.New("imgcache: decode memory busy for a while")

// held is a busy load's bytes (and its disk-tier save), kept for its next
// try so it isn't downloaded again.
type held struct {
	data []byte
	save func()
	at   time.Time
}

// heldShare: busy loads' kept bytes are counted in the cache's byte budget
// (c.bytes, as its images are) and take at most 1/heldShare of it; past
// that the oldest go, and a file bigger than that isn't kept (its next try
// downloads it again).
const heldShare = 4

// ErrUndecodable marks bytes in an image format this package reads
// (PNG, JPEG, GIF, WebP) that don't decode: a final failure, never
// retried. Before it, an animated WebP behind a .png URL (x/image/webp
// reads only stills) failed with "webp: invalid format", which wasn't
// permanent, so the emoji was fetched again every minute for good and
// stayed a blank tile.
var ErrUndecodable = errors.New("imgcache: undecodable image")

// undecodable wraps a format decoder's error as ErrUndecodable; ErrTooBig,
// image.ErrFormat (not an image at all, e.g. an error page) and ErrBusy
// (the decode memory was taken: nothing about the bytes) stay as they are.
func undecodable(err error) error {
	if err == nil || errors.Is(err, ErrTooBig) || errors.Is(err, image.ErrFormat) || errors.Is(err, ErrUndecodable) || errors.Is(err, ErrBusy) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUndecodable, err)
}

// Retry backoff: RetryMin doubling up to RetryMax for errors that may pass
// (network, timeouts, HTTP 429 and 5xx); RetryPermanent for bytes that
// aren't an image or are too big, and URLs that are refused outright;
// RetryBusy doubling up to RetryBusyMax for a decode the decode memory was
// too full for (ErrBusy), which loads as soon as the decodes holding it
// are done.
const (
	RetryMin       = time.Second
	RetryMax       = time.Minute
	RetryPermanent = 10 * time.Minute
	RetryBusy      = 250 * time.Millisecond
	RetryBusyMax   = 4 * time.Second
)

// retryAfter is the wait before the n-th retry (n ≥ 1) after err.
func retryAfter(n int, err error) time.Duration {
	if errors.Is(err, ErrBusy) {
		d := RetryBusy
		for i := 1; i < n && d < RetryBusyMax; i++ {
			d *= 2
		}
		return min(d, RetryBusyMax)
	}
	if permanent(err) {
		return RetryPermanent
	}
	d := RetryMin
	for i := 1; i < n && d < RetryMax; i++ {
		d *= 2
	}
	return min(d, RetryMax)
}

// permanent reports errors a retry can't fix: undecodable or oversized
// bytes and URLs the fetchers refuse. ErrBusy never is.
func permanent(err error) bool {
	if errors.Is(err, ErrBusy) {
		return false
	}
	if errors.Is(err, ErrTooBig) || errors.Is(err, image.ErrFormat) || errors.Is(err, ErrBlockedAddr) || errors.Is(err, ErrUndecodable) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "refusing") || strings.Contains(s, "not an http(s) URL") || strings.Contains(s, "local image gone")
}

type key struct {
	url  string
	size int
}

type entry struct {
	k    key
	op   paint.ImageOp // the image; an animated GIF's first frame (its still)
	size image.Point
	n    int   // bytes held: the pixels, and an animation's (anim.go)
	used int64 // unix milliseconds of the last Get (idle eviction, on screen)
	// px are the decoded pixels, the same memory the ImageOp uploads
	// from: another size of the URL is scaled from them at once (derive)
	px []*image.RGBA
	// interim: scaled from another size of the URL; the exact decode is
	// loading and replaces it
	interim bool

	// an animated GIF (anim.go): its frame count (0 or 1: a still image),
	// its encoded bytes for playing it again (nil once let go), the
	// player while it plays, and whether one is starting or when it may
	frames   int
	gif      *gifSource
	play     *player
	starting bool
	retry    time.Time
	// playFails: player starts in a row that failed (a busy decode
	// memory, a fetch): each waits longer (retryAfter)
	playFails int
	// seenFrom..seenAt: the stretch Get has drawn it without a gap of
	// animGrace (a player starts once it's animSettle long)
	seenFrom, seenAt time.Time
	// live: its live ImageOps (SetLiveOps), for Get and GetPlaying, and
	// their LiveImages
	live  [2]paint.ImageOp
	lives [2]*live
	// gen: the frame (FrameBegin) whose Gets drew it last
	gen uint32
}

// IdleAfter: images not asked for in this long are dropped by a sweep every
// IdleAfter/2, so an idle client holds only what it showed recently.
const IdleAfter = 60 * time.Second

// DefaultLimit is the decoded-pixel budget. Images are decoded at display
// size, so this holds a few screens of photos (a 720 px thumbnail is
// ~1.5 MB) plus hundreds of avatars and emoji; anything evicted is decoded
// again off the UI goroutine from the disk cache or core's raw-bytes cache.
// It was 384 MB, which a long image channel actually filled.
const DefaultLimit = 64 << 20

// DiskLimit is the disk tier's default size budget. A trim runs at
// SetDir and after every DiskLimit/16 written, dropping the least recently
// used files down to 3/4 of the limit.
const DiskLimit = 384 << 20

// New creates a cache. diskDir may be "" to disable the disk cache.
// invalidate is called (from any goroutine) when an image becomes ready.
func New(fetch Fetcher, diskDir string, invalidate func()) *Cache {
	if diskDir != "" {
		_ = os.MkdirAll(diskDir, 0o700)
	}
	if invalidate == nil {
		invalidate = func() {}
	}
	c := &Cache{
		fetch:      fetch,
		dir:        diskDir,
		invalidate: invalidate,
		entries:    map[key]*list.Element{},
		byURL:      map[string]map[int]*list.Element{},
		lru:        list.New(),
		maxBytes:   DefaultLimit,
		inflight:   map[key]bool{},
		failed:     map[key]failure{},
		ctx:        context.Background(),
		diskLimit:  DiskLimit,
		animate:    true,
	}
	go c.sweepLoop()
	return c
}

// SetDir moves the disk tier to dir ("" turns it off: sign-out), for the
// account that's signed in now. The memory tier is dropped with it, so
// nothing fetched for one account draws for another. It doesn't touch
// the disk itself; a trim of the new directory starts in the background.
func (c *Cache) SetDir(dir string) {
	c.mu.Lock()
	if dir == c.dir {
		c.mu.Unlock()
		return
	}
	c.dir = dir
	for el := c.lru.Front(); el != nil; el = el.Next() {
		c.releaseLocked(el.Value.(*entry))
	}
	c.gifBytes = 0
	c.entries = map[key]*list.Element{}
	c.byURL = map[string]map[int]*list.Element{}
	c.lru.Init()
	c.bytes = 0
	c.failed = map[key]failure{}
	c.held, c.heldBytes = nil, 0 // the last account's
	c.written = 0
	c.mu.Unlock()
	if dir != "" {
		go c.trim(dir)
	}
}

// Dir is the disk tier's directory ("" for none).
func (c *Cache) Dir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dir
}

// SetDiskLimit sets the disk tier's budget in bytes.
func (c *Cache) SetDiskLimit(n int64) {
	c.mu.Lock()
	c.diskLimit = n
	c.mu.Unlock()
}

// Pending is how many images are being fetched or decoded now.
func (c *Cache) Pending() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.inflight)
}

// trim drops dir's least recently used files (by modification time; a
// disk hit refreshes it once a day) until the tier is at 3/4 of its limit.
func (c *Cache) trim(dir string) {
	c.mu.Lock()
	if c.trimming {
		c.mu.Unlock()
		return
	}
	c.trimming = true
	limit := c.diskLimit
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.trimming = false
		c.mu.Unlock()
	}()
	type f struct {
		path string
		size int64
		mod  time.Time
	}
	var fs []f
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if strings.HasSuffix(p, ".tmp") && time.Since(info.ModTime()) > time.Hour {
			_ = os.Remove(p) // a write that never finished
			return nil
		}
		fs = append(fs, f{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if total <= limit {
		return
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mod.Before(fs[j].mod) })
	for _, x := range fs {
		if total <= limit*3/4 {
			break
		}
		if os.Remove(x.path) == nil {
			total -= x.size
		}
	}
}

func (c *Cache) sweepLoop() {
	t := time.NewTicker(IdleAfter / 2)
	defer t.Stop()
	for range t.C {
		c.sweep(time.Now().Add(-IdleAfter).UnixMilli())
	}
}

// sweep drops entries last used before cutoff (unix milliseconds).
func (c *Cache) sweep(cutoff int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); e.used < cutoff {
			c.dropLocked(el)
		}
		el = prev
	}
	finals := 0
	for k, f := range c.failed {
		switch {
		case f.final && f.at.UnixMilli() >= cutoff-finalFailureTTL.Milliseconds():
			finals++
		case f.final, f.retry.UnixMilli() < cutoff:
			// a final failure past its time goes, and so does one nobody
			// asked about since its retry was due; one still backing off
			// stays, or the backoff would start over
			delete(c.failed, k)
			c.unholdLocked(k)
		}
	}
	for k := range c.held { // held for a failure that went
		if _, ok := c.failed[k]; !ok {
			c.unholdLocked(k)
		}
	}
	if finals > maxFinalFailures { // the oldest go
		ks := make([]key, 0, finals)
		for k, f := range c.failed {
			if f.final {
				ks = append(ks, k)
			}
		}
		sort.Slice(ks, func(i, j int) bool { return c.failed[ks[i]].at.Before(c.failed[ks[j]].at) })
		for _, k := range ks[:finals-maxFinalFailures] {
			delete(c.failed, k)
		}
	}
}

// Final failures are kept for finalFailureTTL, at most maxFinalFailures
// of them (one per URL and size, the oldest going first), so a session
// that meets many broken images doesn't hold them all for good. One that
// goes is fetched once more the next time it's drawn.
const (
	finalFailureTTL  = 6 * time.Hour
	maxFinalFailures = 4096
)

// SetInvalidate replaces the repaint callback (the window may be created
// after the cache).
func (c *Cache) SetInvalidate(f func()) {
	c.mu.Lock()
	c.invalidate = f
	c.mu.Unlock()
}

// SetAnimInvalidate sets the callback for repaints only animations need
// (any goroutine): an animated image's next frame is due, a player started
// or decoded its next frame. Nothing about the layout changed: with live
// ImageOps (SetLiveOps) a window may show its last frame's ops again, and
// the GPU draws each animation's frame due then. nil: those go to the
// SetInvalidate callback.
func (c *Cache) SetAnimInvalidate(f func()) {
	c.mu.Lock()
	c.animInvalidate = f
	c.mu.Unlock()
}

// SetFetcher replaces the downloader (e.g. once an authenticated session
// exists for files.slack.com).
func (c *Cache) SetFetcher(f Fetcher) {
	c.mu.Lock()
	c.fetch = f
	c.mu.Unlock()
}

// SetLimit sets the memory budget for decoded pixels in bytes.
func (c *Cache) SetLimit(n int) {
	c.mu.Lock()
	c.maxBytes = n
	c.evict()
	c.mu.Unlock()
}

// Get returns the image at url scaled so its larger side is at most sizePx
// (0 = original size, capped at 2048). Non-blocking. An animated GIF
// returns the frame due now and repaints the window for the next one, as
// long as something keeps drawing it (anim.go).
func (c *Cache) Get(url string, sizePx int) (paint.ImageOp, bool) {
	return c.get(url, sizePx, false)
}

func (c *Cache) get(url string, sizePx int, forced bool) (paint.ImageOp, bool) {
	if c == nil || url == "" {
		return paint.ImageOp{}, false
	}
	k := key{url, sizePx}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drawnAt = now.UnixMilli()
	el, ok := c.entries[k]
	if ok {
		c.lru.MoveToFront(el)
	} else {
		e := c.deriveLocked(k)
		if e == nil {
			c.startLocked(k, true)
			if w, ok := c.waiting[k]; ok && w.gen != c.gen { // this frame's (FrameRedrawn)
				w.gen = c.gen
				c.waiting[k] = w
				c.genLoads = append(c.genLoads, k)
			}
			return paint.ImageOp{}, false
		}
		// the exact decode replaces it: always for a standard emoji's own
		// CDN size, else when the disk tier has the bytes (a read, no
		// network); otherwise the scaled copy stays
		e.interim = e.interim || c.dir != "" && DiskOK(k.url)
		c.putLocked(e)
		c.evict()
		if e.interim {
			c.startLocked(k, true)
		}
		if el, ok = c.entries[k]; !ok {
			return e.op, true
		}
	}
	e := el.Value.(*entry)
	e.used = now.UnixMilli()
	if e.gen != c.gen { // this frame's (FrameRedrawn)
		e.gen = c.gen
		c.genEls = append(c.genEls, el)
	}
	if c.liveHeld && c.liveOps && e.frames > 1 && (c.animate || forced) {
		// HoldLive (anim.go): its live op shows the held frame; nothing
		// moves on and nothing asks for a repaint, whichever window Gets
		return c.liveOpLocked(e, forced), true
	}
	op := c.frameLocked(e, now, forced)
	if c.liveOps && e.frames > 1 && (c.animate || forced) {
		return c.liveOpLocked(e, forced), true
	}
	return op, true
}

// FrameBegin says a window's frame is being laid out: what Get draws from
// now until the next FrameBegin is that frame's images, which
// FrameRedrawn draws again (any goroutine).
func (c *Cache) FrameBegin() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gen++
	clear(c.genEls)
	c.genEls = c.genEls[:0]
	clear(c.genLoads)
	c.genLoads = c.genLoads[:0]
	c.mu.Unlock()
}

// FrameRedrawn says the frame laid out since FrameBegin was drawn again
// from its ops, with no Get (ui's quiet frames): its images count as drawn
// now, as that frame's Gets drew them. They're used now (the idle sweep
// drops what wasn't drawn in IdleAfter, and room is made from what wasn't
// drawn in animGrace first), they come first in the LRU, and the loads
// they wait for stay hot. Without it a minute of quiet frames dropped
// every still on screen while the GPU kept drawing them from the old ops,
// and the next layout showed placeholders while they loaded again. Its
// animated images are asked for their frames by the GPU (SetLiveOps), which
// counts as their draw (any goroutine).
func (c *Cache) FrameRedrawn() {
	if c == nil {
		return
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.genEls) - 1; i >= 0; i-- { // the first drawn ends up first
		el := c.genEls[i]
		e := el.Value.(*entry)
		if c.entries[e.k] != el { // gone since
			continue
		}
		e.used = now
		c.lru.MoveToFront(el)
	}
	for _, k := range c.genLoads {
		if w, ok := c.waiting[k]; ok {
			w.asked = now
			c.waiting[k] = w
		}
	}
	c.drawnAt = now
}

// Loaded reports whether some size of url is decoded.
func (c *Cache) Loaded(url string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.byURL[url]) > 0
}

// entryLocked is k's entry, or nil.
func (c *Cache) entryLocked(k key) *entry {
	if el, ok := c.entries[k]; ok {
		return el.Value.(*entry)
	}
	return nil
}

// Stats is the memory tier as it is: decoded entries, their bytes (an
// animation's included), the budget, and the animations playing and what
// they hold.
type Stats struct {
	Entries, Bytes, Limit int
	Players, PlayerBytes  int
	Animated              int         // entries of animated GIFs, playing or not
	EncodedBytes          int         // animated GIFs' encoded bytes kept for playing again
	Advances              uint64      // frames the players have moved on, ever
	Skipped               uint64      // frames their timelines passed over to catch up, ever
	LargestFrame          image.Point // the largest frame a player plays now
	LiveHeld              bool        // the live images are held (HoldLive)
}

// Stats reports the memory tier.
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Stats{Entries: len(c.entries), Bytes: c.bytes, Limit: c.maxBytes, Advances: c.advances, Skipped: c.skipped, LiveHeld: c.liveHeld}
	for el := c.lru.Front(); el != nil; el = el.Next() {
		e := el.Value.(*entry)
		if e.frames > 1 {
			s.Animated++
		}
		if e.gif != nil {
			s.EncodedBytes += len(e.gif.data)
		}
		if p := e.play; p != nil {
			s.Players++
			s.PlayerBytes += p.n
			if p.size.X*p.size.Y > s.LargestFrame.X*s.LargestFrame.Y {
				s.LargestFrame = p.size
			}
		}
	}
	return s
}

// Size returns the decoded size of a ready image.
func (c *Cache) Size(url string, sizePx int) (image.Point, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key{url, sizePx}]; ok {
		return el.Value.(*entry).size, true
	}
	return image.Point{}, false
}

// Prefetch starts loading without returning anything.
func (c *Cache) Prefetch(url string, sizePx int) {
	if url == "" {
		return
	}
	c.mu.Lock()
	k := key{url, sizePx}
	if _, ok := c.entries[k]; !ok {
		c.startLocked(k, false)
	}
	c.mu.Unlock()
}

// Failed reports whether the last fetch of url failed (so callers can show a
// broken-image state instead of a spinner). One the decode memory was too
// full for (ErrBusy) hasn't: it's still loading, and tries again shortly.
func (c *Cache) Failed(url string, sizePx int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.failed[key{url, sizePx}]
	return ok && !f.busy
}

// startLocked queues k's load. drawn: Get asked (it's on screen), not
// Prefetch; asked again while it waits, it moves to the hot queue.
func (c *Cache) startLocked(k key, drawn bool) {
	if c.inflight[k] {
		if w, ok := c.waiting[k]; ok && drawn {
			w.asked = time.Now().UnixMilli()
			if !w.hot {
				w.hot = true
				lane := laneOf(k)
				c.hot[lane] = append(c.hot[lane], k)
			}
			c.waiting[k] = w
		}
		return
	}
	if f, ok := c.failed[k]; ok && (f.final || time.Now().Before(f.retry)) {
		return
	}
	c.inflight[k] = true
	lane := laneOf(k)
	if c.waiting == nil {
		c.waiting = map[key]queued{}
	}
	c.waiting[k] = queued{asked: time.Now().UnixMilli()}
	c.queue[lane] = append(c.queue[lane], k)
	if c.workers[lane] < laneWorkers {
		c.workers[lane]++
		go c.work(lane)
	}
}

// queued is a waiting load: when Get last asked for it and whether it's
// in the hot queue.
type queued struct {
	asked int64 // unix milliseconds
	hot   bool
	gen   uint32 // the frame that last asked (FrameRedrawn)
}

// The fetch lanes (Cache.queue) and how many loads each runs at once.
const (
	laneSmall   = 0 // avatars and emoji (DiskOK)
	laneOther   = 1 // message images, files, unfurls
	laneWorkers = 8
)

func laneOf(k key) int {
	if DiskOK(k.url) {
		return laneSmall
	}
	return laneOther
}

// work loads the lane's queued images until it is empty: the hot queue's
// first, then the rest in the order they were asked for.
func (c *Cache) work(lane int) {
	for {
		c.mu.Lock()
		k, ok := c.nextLocked(lane)
		if !ok {
			c.queue[lane], c.hot[lane], c.hotRun[lane] = nil, nil, 0 // a drained burst's arrays go too
			c.workers[lane]--
			c.mu.Unlock()
			return
		}
		delete(c.waiting, k)
		c.mu.Unlock()
		c.load(k)
	}
}

// hotRunMax is how many hot loads a lane takes in a row while its queue
// has others waiting. On-screen images that need more than the budget
// keep coming back hot (each load evicts another one still drawn), and
// would hold up the rest of the queue for good.
const hotRunMax = laneWorkers

// nextLocked takes the lane's next load: the first hot one still drawn,
// else (and after hotRunMax hot ones in a row) the queue's first not
// started yet (a hot one started already is still in the queue too, and
// skipped there).
func (c *Cache) nextLocked(lane int) (key, bool) {
	if c.hotRun[lane] < hotRunMax {
		if k, ok := c.nextHotLocked(lane); ok {
			c.hotRun[lane]++
			return k, true
		}
	}
	c.hotRun[lane] = 0
	for q := c.queue[lane]; len(q) > 0; q = c.queue[lane] {
		k := q[0]
		q[0] = key{}
		c.queue[lane] = q[1:]
		if _, ok := c.waiting[k]; ok {
			return k, true
		}
	}
	if k, ok := c.nextHotLocked(lane); ok { // the queue held only hot ones, started already
		c.hotRun[lane] = 1
		return k, true
	}
	return key{}, false
}

// nextHotLocked takes the lane's first hot load that's still drawn. One
// Get hasn't asked for within animGrace of the latest frame (drawnAt) is
// off screen, and goes back to its turn in the queue: the latest frame,
// not the clock, as a load landing on an idle window comes before the
// window draws again, and the frame it showed last is still on screen
// however long ago that was.
func (c *Cache) nextHotLocked(lane int) (key, bool) {
	for h := c.hot[lane]; len(h) > 0; h = c.hot[lane] {
		k := h[0]
		h[0] = key{}
		c.hot[lane] = h[1:]
		w, ok := c.waiting[k]
		if !ok || !w.hot {
			continue
		}
		if c.drawnAt-w.asked > animGrace.Milliseconds() {
			w.hot = false
			c.waiting[k] = w
			continue
		}
		return k, true
	}
	return key{}, false
}

func (c *Cache) load(k key) {
	c.mu.Lock()
	h := c.held[k] // the bytes a busy try kept
	c.unholdLocked(k)
	c.mu.Unlock()
	op, sz, n, an, px, h, err := c.loadFrom(k, h)
	c.mu.Lock()
	delete(c.inflight, k)
	if err != nil {
		now := time.Now()
		f := c.failed[k]
		f.n++
		f.at = now
		busy := errors.Is(err, ErrBusy)
		if busy {
			f.busyN++
		} else {
			f.busyN = 0
		}
		f.final = errors.Is(err, ErrUndecodable)
		f.busy = busy && f.busyN <= busyTries
		switch {
		case f.busy: // still loading: again shortly
			f.retry = now.Add(retryAfter(f.busyN, err))
		case busy: // busy for a while: failed, and again later
			f.retry = now.Add(retryAfter(f.busyN-busyTries, errBusyLong))
		default:
			f.retry = now.Add(retryAfter(f.n, err))
		}
		if busy && h.data != nil {
			c.holdLocked(k, h)
		}
		c.failed[k] = f
		if !f.final {
			c.wakeLocked(f.retry) // the surface asks again when the retry is due
		}
	} else {
		delete(c.failed, k)
		e := &entry{k: k, op: op, size: sz, n: n, used: time.Now().UnixMilli(), px: px}
		if an != nil {
			e.frames = len(an.ix.frames)
			if len(an.data) <= c.maxBytes/(2*gifShare) { // the bytes for playing it (anim.go)
				e.gif = an
				e.n += len(an.data)
			}
		}
		c.putLocked(e) // over an interim one (derive)
		if e.gif != nil {
			c.keptGIFLocked(e)
		}
		c.evict()
	}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}

// holdLocked keeps h for k's next try, in the cache's byte budget: the
// oldest held go first past the budget's 1/heldShare, and what nobody drew
// lately makes room (shedLocked); bytes that don't fit then aren't kept.
func (c *Cache) holdLocked(k key, h held) {
	c.unholdLocked(k)
	limit := c.maxBytes / heldShare
	if len(h.data) > limit {
		return
	}
	for c.heldBytes+len(h.data) > limit {
		var old key
		var at time.Time
		first := true
		for hk, hv := range c.held {
			if first || hv.at.Before(at) {
				old, at, first = hk, hv.at, false
			}
		}
		c.unholdLocked(old)
	}
	if !c.shedLocked(c.maxBytes-len(h.data), time.Now()) {
		return // what's drawn now has the budget
	}
	if c.held == nil {
		c.held = map[key]held{}
	}
	h.at = time.Now()
	c.held[k] = h
	c.heldBytes += len(h.data)
	c.bytes += len(h.data)
}

// unholdLocked lets k's held bytes go.
func (c *Cache) unholdLocked(k key) {
	if h, ok := c.held[k]; ok {
		c.heldBytes -= len(h.data)
		c.bytes -= len(h.data)
		delete(c.held, k)
	}
}

// evict brings the decoded bytes back under the budget: first what
// nobody drew in the last animGrace (an animated GIF's encoded bytes
// before its still, least recently used first), then busy loads' kept
// bytes (oldest first: their next try downloads them again), then the
// animations playing on screen (the largest first; each draws its still
// until animRetry has passed), and only then, as before, images on
// screen.
func (c *Cache) evict() {
	now := time.Now()
	if c.shedLocked(c.maxBytes, now) {
		return
	}
	for c.bytes > c.maxBytes && len(c.held) > 0 { // busy loads' bytes: downloaded again if need be
		var old key
		var at time.Time
		first := true
		for hk, hv := range c.held {
			if first || hv.at.Before(at) {
				old, at, first = hk, hv.at, false
			}
		}
		c.unholdLocked(old)
	}
	// players stop, largest first: the streaming ones (each holds a canvas
	// and three frames of a large animation), then keeping ones past
	// their share of the budget. A keeping player is its image's frames,
	// decoded once (an emoji's 13 frames are 53 KB), and plays on while
	// stills fill the budget, as Chromium's animations do; a burst of
	// them (the picker's animated emoji) still can't take the budget.
	for c.bytes > c.maxBytes && len(c.playing) > 0 {
		var big *entry
		kept := 0
		for e := range c.playing {
			if p := e.play; p.keep {
				kept += p.n
			} else if big == nil || p.n > big.play.n {
				big = e
			}
		}
		if big == nil && kept > c.maxBytes/keepShare {
			for e := range c.playing {
				if big == nil || e.play.n > big.play.n {
					big = e
				}
			}
		}
		if big == nil {
			break
		}
		c.releaseLocked(big)
		big.retry = now.Add(animRetry)
		c.wakeAnimLocked(big.retry)
	}
	for c.bytes > c.maxBytes && c.lru.Len() > 1 {
		c.dropLocked(c.lru.Back())
	}
}

// shedLocked brings the decoded bytes down to target by letting go of
// what nobody drew in the last animGrace, least recently used first:
// animated GIFs' encoded bytes before stills. It reports whether they got
// there.
func (c *Cache) shedLocked(target int, now time.Time) bool {
	if c.bytes <= target {
		return true
	}
	ms := now.UnixMilli()
	cold := func(e *entry) bool { return ms-e.used >= animGrace.Milliseconds() }
	for el := c.lru.Back(); el != nil && c.bytes > target && c.gifBytes > 0; el = el.Prev() {
		if e := el.Value.(*entry); cold(e) && e.play == nil {
			c.dropGIFLocked(e)
		}
	}
	for c.bytes > target && c.lru.Len() > 1 && cold(c.lru.Back().Value.(*entry)) {
		c.dropLocked(c.lru.Back())
	}
	return c.bytes <= target
}

// putLocked adds e as the most recent entry, replacing one at its key.
func (c *Cache) putLocked(e *entry) {
	if old, ok := c.entries[e.k]; ok {
		c.dropLocked(old)
	}
	el := c.lru.PushFront(e)
	c.entries[e.k] = el
	if c.byURL == nil {
		c.byURL = map[string]map[int]*list.Element{}
	}
	m := c.byURL[e.k.url]
	if m == nil {
		m = map[int]*list.Element{}
		c.byURL[e.k.url] = m
	}
	m[e.k.size] = el
	c.bytes += e.n
}

// keepShare: keeping players' frames (anim.go) stop for room only past
// this share of the budget (a quarter).
const keepShare = 4

// gifShare is the most of the budget animated GIFs' encoded bytes (kept
// for playing them again) take; past it the least recently used go
// first, the ones playing last. The rest of the budget is pixels.
const gifShare = 4 // a quarter

// keptGIFLocked counts e's encoded bytes (just kept) and lets go of older
// ones past the budget's gifShare.
func (c *Cache) keptGIFLocked(e *entry) {
	c.gifBytes += len(e.gif.data)
	for el := c.lru.Back(); el != nil && c.gifBytes > c.maxBytes/gifShare; el = el.Prev() {
		if o := el.Value.(*entry); o != e && o.play == nil {
			c.dropGIFLocked(o)
		}
	}
}

// dropGIFLocked lets go of e's encoded bytes (a player draws its still
// again after fetching them anew).
func (c *Cache) dropGIFLocked(e *entry) {
	if e.gif == nil {
		return
	}
	n := len(e.gif.data)
	e.n -= n
	c.bytes -= n
	c.gifBytes -= n
	e.gif = nil
}

// dropLocked removes entry el. Its live images (a last frame's ops may
// still draw them) are left on its still, which those ops hold already.
func (c *Cache) dropLocked(el *list.Element) {
	e := el.Value.(*entry)
	c.releaseLocked(e)
	if e.gif != nil {
		c.gifBytes -= len(e.gif.data)
	}
	c.lru.Remove(el)
	if c.entries[e.k] == el {
		delete(c.entries, e.k)
	}
	if m := c.byURL[e.k.url]; m != nil && m[e.k.size] == el {
		delete(m, e.k.size)
		if len(m) == 0 {
			delete(c.byURL, e.k.url)
		}
	}
	c.bytes -= e.n
}

// deriveLocked makes k from an exact decode of the same URL at a size at
// least as large (the smallest such; size 0 is the original), scaled down
// at once, so a surface asking for a new size of an image another one
// already shows draws it on its first frame (the picker's emoji in a
// reaction pill, a 16px and a 24px avatar). Get loads the exact decode for
// k over it when the disk tier holds the bytes. nil when there's none.
func (c *Cache) deriveLocked(k key) *entry {
	if k.size <= 0 {
		return nil
	}
	var src *entry
	for sz, el := range c.byURL[k.url] {
		e := el.Value.(*entry)
		if e.interim || len(e.px) == 0 || (sz != 0 && sz < k.size) {
			continue
		}
		if src == nil || sizeBetter(sz, src.k.size) {
			src = e
		}
	}
	if src == nil || src.size.X*src.size.Y > deriveMaxPx {
		return nil
	}
	// a standard emoji whose two sizes are different CDN assets (emoji-
	// sharp's ForPixels): the scaled copy only shows until k's own asset
	// arrives. An animated GIF's copy is its still; its player decodes
	// the frames at k's size (anim.go).
	e := &entry{k: k, used: time.Now().UnixMilli(), frames: src.frames,
		interim: emoji.ForPixels(k.url, k.size) != emoji.ForPixels(k.url, src.k.size)}
	e.px = []*image.RGBA{Scale(src.px[0], k.size)}
	e.size = e.px[0].Bounds().Size()
	e.n = len(e.px[0].Pix)
	e.op = paint.NewImageOp(e.px[0])
	return e
}

// deriveMaxPx bounds the source pixels derive scales on the UI goroutine:
// emoji and avatars, not photos.
const deriveMaxPx = 512 * 512

// sizeBetter reports whether a decode at size a is a better source than
// one at b for scaling down: the smaller of two sized decodes, a sized one
// over the original (0).
func sizeBetter(a, b int) bool {
	switch {
	case a == 0:
		return false
	case b == 0:
		return true
	}
	return a < b
}

// rawFor is k's encoded bytes. A standard emoji loads the CDN size that
// covers k.size device pixels (Slack's @2x choice; emoji.ForPixels), so
// it is only ever scaled down. The URL as given is the fallback.
func (c *Cache) rawFor(k key) ([]byte, func(), error) {
	src := emoji.ForPixels(k.url, k.size)
	data, save, err := c.raw(src)
	if err != nil && src != k.url {
		data, save, err = c.raw(k.url)
	}
	return data, save, err
}

// loadImage decodes k. A GIF or an animated WebP decodes its first frame
// only (gifstream.go, webpanim.go); an animated one also returns its
// bytes and their index (anim.go). Bytes that don't decode are
// ErrUndecodable; a decode the decode memory is too full for now is
// ErrBusy (decmem.go).
//
// Everything the decode and its scale allocate is charged to one Budget,
// given back once the display-size copy (the cache's now) is all that's
// kept.
func (c *Cache) loadImage(k key) (paint.ImageOp, image.Point, int, *gifSource, []*image.RGBA, error) {
	op, sz, n, an, px, _, err := c.loadFrom(k, held{})
	return op, sz, n, an, px, err
}

// loadFrom is loadImage from h's bytes when a busy try kept them (else
// fetched as loadImage fetches them), and returns the bytes it decoded
// for a busy failure to keep.
func (c *Cache) loadFrom(k key, h held) (paint.ImageOp, image.Point, int, *gifSource, []*image.RGBA, held, error) {
	data, save := h.data, h.save
	if data == nil {
		var err error
		if data, save, err = c.rawFor(k); err != nil {
			return paint.ImageOp{}, image.Point{}, 0, nil, nil, held{}, err
		}
	}
	h = held{data: data, save: save}
	op, sz, n, an, px, err := c.decodeLoad(k, data, save)
	return op, sz, n, an, px, h, err
}

// decodeLoad decodes data for k (loadImage), save writing it to the disk
// tier once it has decoded.
func (c *Cache) decodeLoad(k key, data []byte, save func()) (paint.ImageOp, image.Point, int, *gifSource, []*image.RGBA, error) {
	b := NewBudget()
	defer b.Release()
	if isAnim(data) {
		fail := func(err error) (paint.ImageOp, image.Point, int, *gifSource, []*image.RGBA, error) {
			return paint.ImageOp{}, image.Point{}, 0, nil, nil, undecodable(budgetErr(err))
		}
		if err := checkSize(data); err != nil {
			return fail(err)
		}
		ix, err := indexAnim(data, b)
		if err != nil {
			return fail(err)
		}
		if ix.w*ix.h > bigPixels { // its canvas, as a big image's decode
			bigSem <- struct{}{}
			defer func() { <-bigSem }()
		}
		d, err := newGIFDecoder(ix, data, b)
		if err != nil {
			return fail(err)
		}
		if _, err := d.compose(); err != nil {
			return fail(err)
		}
		if d.werr != nil { // an animated WebP whose first frame doesn't decode
			return fail(d.werr)
		}
		sz := scaledSize(ix.w, ix.h, k.size)
		if err := b.Charge(rgbaBytes(sz)); err != nil {
			return fail(err)
		}
		still := image.NewRGBA(image.Rectangle{Max: sz})
		var sc frameScaler
		if err := sc.scale(still, d.canvas, b); err != nil {
			return fail(err)
		}
		save()
		var an *gifSource
		if len(ix.frames) > 1 {
			an = &gifSource{data: data, ix: ix}
		}
		return paint.NewImageOp(still), still.Rect.Size(), len(still.Pix), an, []*image.RGBA{still}, nil
	}
	// A big image decodes to its full size and scales (a band at a time:
	// internal/catmullrom) before the display-size copy is all that's kept.
	// The decode memory bounds what they hold together; at most bigDecodes
	// at a time bounds the CPU they take.
	if big := isBig(data); big {
		bigSem <- struct{}{}
		defer func() { <-bigSem }()
	}
	img, err := decodeImage(data, MaxPixels, MaxSide, b)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, nil, nil, undecodable(err)
	}
	save() // only bytes that decoded as an image reach the disk tier
	rgba, err := ScaleBudget(img, k.size, b)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, nil, nil, undecodable(err)
	}
	return paint.NewImageOp(rgba), rgba.Bounds().Size(), len(rgba.Pix), nil, []*image.RGBA{rgba}, nil
}

// bigDecodes is how many images of more than bigPixels decode at once
// (every Cache together: they share the memory).
const (
	bigDecodes = 2
	bigPixels  = 1 << 20
)

var bigSem = make(chan struct{}, bigDecodes)

// isBig reports whether data's header says it has more than bigPixels.
func isBig(data []byte) bool {
	cfg, _, err := decodeConfig(data)
	return err == nil && cfg.Width*cfg.Height > bigPixels
}

// MaxPixels bounds the decoded size of one image (width × height), and
// MaxSide either dimension: an image past them isn't decoded at all. What
// a decode may allocate is bounded by its budget (MaxDecode), which a
// photo of 64 MP may or may not fit in by its format (a 48 MP phone photo,
// 8064×6048, does), so this bounds the work: a tiny file claiming to be
// 60000×60000 is refused by its header before anything decodes.
const (
	MaxPixels = 64 << 20
	MaxSide   = 16384
)

// ErrTooBig is returned for images over MaxPixels or MaxSide, and for one
// whose decode would need more than MaxDecode (or a WebP picture's budget).
var ErrTooBig = errors.New("imgcache: image dimensions too large")

// Decode is the one safe entry point for decoding untrusted image bytes:
// the cache's own loads, and every UI path that decodes bytes some
// workspace member or a picked file could control (Copy Image, custom
// emoji, profile photos, email icons). It decodes PNG, JPEG, GIF (its
// first frame), WebP and BMP only, each through a vendored decoder that
// charges every allocation to a Budget from the decode memory before
// making it (decmem.go): it rejects a header claiming more than MaxPixels
// or MaxSide before decoding, a decode that would need more than MaxDecode
// fails with ErrTooBig before the allocation that doesn't fit, and one the
// decode memory is too full for now is tried again for a while
// (WithBudget) before failing with ErrBusy. A bomb comes back as an error,
// so a caller on an overlay or popup with no recover shows a failure
// instead of crashing the app. The image is the caller's once it returns
// (no longer charged); one that works with it after (scales, converts)
// uses DecodeBudget under its own WithBudget instead.
func Decode(data []byte) (image.Image, error) {
	return DecodeWithin(data, MaxPixels, MaxSide)
}

// DecodeWithin is Decode with the caller's own decoded-size limits, for a
// surface that legitimately decodes larger images than the cache's
// MaxPixels (the composer's attachment thumbnails, at attMaxPixels). The
// decode is still bounded by MaxDecode, and a WebP's by its picture's
// budget too.
func DecodeWithin(data []byte, maxPixels, maxSide int) (image.Image, error) {
	var img image.Image
	err := WithBudget(context.Background(), func(b *Budget) error {
		var err error
		img, err = decodeImage(data, maxPixels, maxSide, b)
		return err
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}

// checkSize reads the header's dimensions only, against the cache budget.
func checkSize(data []byte) error { return checkSizeWithin(data, MaxPixels, MaxSide) }

func checkSizeWithin(data []byte, maxPixels, maxSide int) error {
	cfg, _, err := decodeConfig(data)
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return ErrTooBig
	}
	return nil
}

// raw returns the encoded bytes from disk (DiskOK URLs only: avatars and
// emoji) or the network, plus a func that saves fetched bytes to the disk
// tier (a no-op for everything else, e.g. message images and files).
func (c *Cache) raw(u string) ([]byte, func(), error) {
	nop := func() {}
	if localimg.Is(u) { // a file the viewer is uploading: memory only
		if b, ok := localimg.Get(u); ok {
			return b, nop, nil
		}
		return nil, nop, errors.New("imgcache: local image gone")
	}
	c.mu.Lock()
	dir := c.dir
	c.mu.Unlock()
	disk := dir != "" && DiskOK(u)
	var path string
	if disk {
		h := sha256.Sum256([]byte(u))
		name := hex.EncodeToString(h[:16])
		path = filepath.Join(dir, name[:2], name)
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				now := time.Now()
				_ = os.Chtimes(path, now, now) // recently used: last out of trim
			}
			return b, nop, nil
		}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 45*time.Second)
	defer cancel()
	c.mu.Lock()
	fetch := c.fetch
	c.mu.Unlock()
	b, err := fetch(ctx, u)
	if err != nil {
		return nil, nop, err
	}
	if !disk {
		return b, nop, nil
	}
	return b, func() {
		c.mu.Lock()
		still, limit := c.dir == dir, c.diskLimit
		c.mu.Unlock()
		if !still { // signed out or switched accounts meanwhile
			return
		}
		if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
			return
		}
		f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp") // 0600, never through a symlink
		if err != nil {
			return
		}
		_, err = f.Write(b)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err != nil {
			_ = os.Remove(f.Name())
			return
		}
		c.mu.Lock()
		c.written += int64(len(b))
		due := c.written > limit/16 && c.dir == dir
		if due {
			c.written = 0
		}
		c.mu.Unlock()
		if due {
			go c.trim(dir)
		}
	}, nil
}

// Scale converts img to *image.RGBA (premultiplied, what Gio uploads
// fastest) with its larger side at most max px (0 = 2048 cap): the pixels
// x/image/draw's CatmullRom.Scale draws, a band at a time (ScaleBudget,
// uncharged: for images already decoded and small, as derive's).
func Scale(img image.Image, max int) *image.RGBA {
	r, _ := ScaleBudget(img, max, budget.Unlimited()) // an unlimited budget refuses nothing
	return r
}

// DiskOK reports whether an image URL may be persisted: avatars and emoji
// only. Never files.slack.com, slack-files, or third-party unfurl images.
// (A perfharness build also keeps the loopback fake's images.)
func DiskOK(raw string) bool {
	if slack.IsHarnessURL(raw) {
		return strings.Contains(raw, "/img/")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "files.slack.com" || strings.HasSuffix(host, ".files.slack.com") || strings.Contains(host, "slack-files"):
		return false
	case host == "avatars.slack-edge.com", host == "ca.slack-edge.com", host == "secure.gravatar.com",
		host == "emoji.slack-edge.com":
		return true
	case host == "a.slack-edge.com":
		// standard emoji sheets/images live under /production-standard-emoji-assets/
		return strings.Contains(u.Path, "emoji")
	}
	return false
}
