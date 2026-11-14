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
package imgcache

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/slack"
)

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

	// Two fetch lanes: avatars and emoji (DiskOK URLs, a few KB each) never
	// wait behind message images, files and unfurls, which can take the
	// whole 45 s timeout each. Each lane is a queue that at most
	// laneWorkers goroutines work through, in the order the images were
	// asked for (under mu). A goroutine per request waiting its turn cost
	// 8 KB of stack each: scrolling the emoji picker through Hack Club's
	// 65,000 emoji parked 30,000 of them, 250 MB.
	queue   [2][]key
	workers [2]int
	ctx     context.Context

	wakeAt time.Time // the next repaint: an animation frame (anim.go) or a retry
	wakeT  *time.Timer

	// animations (anim.go): whether Get plays them (SetAnimate), the
	// entries with a player playing, and the sweep that stops them once
	// nothing draws them
	animate  bool
	playing  map[*entry]struct{}
	animT    *time.Timer
	advances uint64 // frames the players moved on (Stats)
}

// failure is a key's failed load: how many in a row, and when Get may try
// again. Each retry repaints the window when it's due (wakeLocked), so an
// image on screen comes back on its own after a dropped connection, a
// timeout or a 5xx, instead of staying a placeholder until something else
// redraws the window.
type failure struct {
	at    time.Time
	n     int
	retry time.Time
}

// Retry backoff: RetryMin doubling up to RetryMax for errors that may pass
// (network, timeouts, HTTP 429 and 5xx); RetryPermanent for bytes that
// aren't an image or are too big, and URLs that are refused outright.
const (
	RetryMin       = time.Second
	RetryMax       = time.Minute
	RetryPermanent = 10 * time.Minute
)

// retryAfter is the wait before the n-th retry (n ≥ 1) after err.
func retryAfter(n int, err error) time.Duration {
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
// bytes and URLs the fetchers refuse.
func permanent(err error) bool {
	if errors.Is(err, ErrTooBig) || errors.Is(err, image.ErrFormat) || errors.Is(err, ErrBlockedAddr) {
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
	c.entries = map[key]*list.Element{}
	c.byURL = map[string]map[int]*list.Element{}
	c.lru.Init()
	c.bytes = 0
	c.failed = map[key]failure{}
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
	for k, f := range c.failed {
		// a failure nobody asked about since (its retry is long due) goes;
		// one still backing off stays, or the backoff would start over
		if f.retry.UnixMilli() < cutoff {
			delete(c.failed, k)
		}
	}
}

// SetInvalidate replaces the repaint callback (the window may be created
// after the cache).
func (c *Cache) SetInvalidate(f func()) {
	c.mu.Lock()
	c.invalidate = f
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
	el, ok := c.entries[k]
	if ok {
		c.lru.MoveToFront(el)
	} else {
		e := c.deriveLocked(k)
		if e == nil {
			c.startLocked(k)
			return paint.ImageOp{}, false
		}
		// the exact decode replaces it: always for a standard emoji's own
		// CDN size, else when the disk tier has the bytes (a read, no
		// network); otherwise the scaled copy stays
		e.interim = e.interim || c.dir != "" && DiskOK(k.url)
		c.putLocked(e)
		c.evict()
		if e.interim {
			c.startLocked(k)
		}
		if el, ok = c.entries[k]; !ok {
			return e.op, true
		}
	}
	e := el.Value.(*entry)
	e.used = now.UnixMilli()
	return c.frameLocked(e, now, forced), true
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
	LargestFrame          image.Point // the largest frame a player plays now
}

// Stats reports the memory tier.
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Stats{Entries: len(c.entries), Bytes: c.bytes, Limit: c.maxBytes, Advances: c.advances}
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
		c.startLocked(k)
	}
	c.mu.Unlock()
}

// Failed reports whether the last fetch of url failed (so callers can show a
// broken-image state instead of a spinner).
func (c *Cache) Failed(url string, sizePx int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.failed[key{url, sizePx}]
	return ok
}

func (c *Cache) startLocked(k key) {
	if c.inflight[k] {
		return
	}
	if f, ok := c.failed[k]; ok && time.Now().Before(f.retry) {
		return
	}
	c.inflight[k] = true
	lane := laneOther
	if DiskOK(k.url) {
		lane = laneSmall
	}
	c.queue[lane] = append(c.queue[lane], k)
	if c.workers[lane] < laneWorkers {
		c.workers[lane]++
		go c.work(lane)
	}
}

// The fetch lanes (Cache.queue) and how many loads each runs at once.
const (
	laneSmall   = 0 // avatars and emoji (DiskOK)
	laneOther   = 1 // message images, files, unfurls
	laneWorkers = 8
)

// work loads the lane's queued images in order until it is empty.
func (c *Cache) work(lane int) {
	for {
		c.mu.Lock()
		q := c.queue[lane]
		if len(q) == 0 {
			c.queue[lane] = nil // a drained burst's array goes too
			c.workers[lane]--
			c.mu.Unlock()
			return
		}
		k := q[0]
		q[0] = key{}
		c.queue[lane] = q[1:]
		c.mu.Unlock()
		c.load(k)
	}
}

func (c *Cache) load(k key) {
	op, sz, n, an, px, err := c.loadImage(k)
	c.mu.Lock()
	delete(c.inflight, k)
	if err != nil {
		now := time.Now()
		f := c.failed[k]
		f.n++
		f.at = now
		f.retry = now.Add(retryAfter(f.n, err))
		c.failed[k] = f
		c.wakeLocked(f.retry) // the surface asks again when the retry is due
	} else {
		delete(c.failed, k)
		e := &entry{k: k, op: op, size: sz, n: n, used: time.Now().UnixMilli(), px: px}
		if an != nil {
			e.frames = len(an.ix.frames)
			if len(an.data) <= c.maxBytes/8 { // the bytes for playing it (anim.go)
				e.gif = an
				e.n += len(an.data)
			}
		}
		c.putLocked(e) // over an interim one (derive)
		c.evict()
	}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}

// evict brings the decoded bytes back under the budget: first what
// nobody drew in the last animGrace (an animated GIF's encoded bytes
// before its still, least recently used first), then the animations
// playing on screen (the largest first; each draws its still until
// animRetry has passed), and only then, as before, images on screen.
func (c *Cache) evict() {
	if c.bytes <= c.maxBytes {
		return
	}
	now := time.Now()
	ms := now.UnixMilli()
	cold := func(e *entry) bool { return ms-e.used >= animGrace.Milliseconds() }
	for el := c.lru.Back(); el != nil && c.bytes > c.maxBytes; el = el.Prev() {
		if e := el.Value.(*entry); cold(e) && e.gif != nil && e.play == nil {
			e.n -= len(e.gif.data)
			c.bytes -= len(e.gif.data)
			e.gif = nil
		}
	}
	for c.bytes > c.maxBytes && c.lru.Len() > 1 && cold(c.lru.Back().Value.(*entry)) {
		c.dropLocked(c.lru.Back())
	}
	for c.bytes > c.maxBytes && len(c.playing) > 0 {
		var big *entry
		for e := range c.playing {
			if big == nil || e.play.n > big.play.n {
				big = e
			}
		}
		c.releaseLocked(big)
		big.retry = now.Add(animRetry)
		c.wakeLocked(big.retry)
	}
	for c.bytes > c.maxBytes && c.lru.Len() > 1 {
		c.dropLocked(c.lru.Back())
	}
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

// dropLocked removes entry el.
func (c *Cache) dropLocked(el *list.Element) {
	e := el.Value.(*entry)
	c.releaseLocked(e)
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

// loadImage decodes k. A GIF decodes its first frame only (gifstream.go);
// an animated one also returns its bytes and their index (anim.go).
func (c *Cache) loadImage(k key) (paint.ImageOp, image.Point, int, *gifSource, []*image.RGBA, error) {
	data, save, err := c.rawFor(k)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, nil, nil, err
	}
	if isGIF(data) {
		if err := checkSize(data); err != nil {
			return paint.ImageOp{}, image.Point{}, 0, nil, nil, err
		}
		ix, err := indexGIF(data)
		if err != nil {
			return paint.ImageOp{}, image.Point{}, 0, nil, nil, err
		}
		if ix.w*ix.h > bigPixels { // its canvas, as a big image's decode
			bigSem <- struct{}{}
			defer func() { <-bigSem }()
		}
		d := newGIFDecoder(ix, data)
		d.compose()
		still := image.NewRGBA(image.Rectangle{Max: scaledSize(ix.w, ix.h, k.size)})
		var sc frameScaler
		sc.scale(still, d.canvas)
		save()
		var an *gifSource
		if len(ix.frames) > 1 {
			an = &gifSource{data: data, ix: ix}
		}
		return paint.NewImageOp(still), still.Rect.Size(), len(still.Pix), an, []*image.RGBA{still}, nil
	}
	// A big image decodes to its full size and scales through a temporary
	// buffer as big again (x/image/draw's kernel scaler: 32 bytes per
	// destination column per source row) before the display-size copy is
	// all that's kept. Eight of them at once while a channel of screenshots
	// scrolled by held 150 MB for a moment: at most bigDecodes at a time.
	if big := isBig(data); big {
		bigSem <- struct{}{}
		defer func() { <-bigSem }()
	}
	img, err := Decode(data)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, nil, nil, err
	}
	save() // only bytes that decoded as an image reach the disk tier
	rgba := Scale(img, k.size)
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
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	return err == nil && cfg.Width*cfg.Height > bigPixels
}

// MaxPixels bounds the decoded size of one image (width × height), so a
// tiny file that claims to be 60000×60000 (a decompression bomb) can't
// allocate gigabytes. MaxSide bounds either dimension.
const (
	MaxPixels = 40 << 20
	MaxSide   = 16384
)

// ErrTooBig is returned for images over MaxPixels or MaxSide.
var ErrTooBig = errors.New("imgcache: image dimensions too large")

// Decode decodes data after checking its header's dimensions.
func Decode(data []byte) (image.Image, error) {
	if err := checkSize(data); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// checkSize reads the header's dimensions only.
func checkSize(data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
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
// fastest) with its larger side at most max px (0 = 2048 cap).
func Scale(img image.Image, max int) *image.RGBA {
	if max <= 0 {
		max = 2048
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > max || h > max {
		if w >= h {
			h = max * h / w
			w = max
		} else {
			w = max * w / h
			h = max
		}
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		return dst
	}
	if r, ok := img.(*image.RGBA); ok && b.Min == (image.Point{}) {
		return r
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
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
