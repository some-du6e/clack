// Package imgcache fetches and decodes images off the UI goroutine and keeps
// them in a memory LRU. Avatars and emoji (metadata under the Hack Club
// scraping policy) are also cached on disk; message files and attachments
// (files.slack.com and anything else) stay in memory only.
//
// Get never blocks: it returns (op, true) when the image is ready and
// otherwise starts a fetch, returns false, and calls invalidate when the
// image arrives so the window repaints.
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
	"strings"
	"sync"
	"time"

	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Fetcher downloads url. For files.slack.com it must authenticate (core.Fetch).
type Fetcher func(ctx context.Context, url string) ([]byte, error)

// Cache is safe for concurrent use.
type Cache struct {
	fetch      Fetcher
	dir        string
	invalidate func()

	mu       sync.Mutex
	entries  map[key]*list.Element
	lru      *list.List // front = most recent
	bytes    int
	maxBytes int
	inflight map[key]bool
	failed   map[key]time.Time

	sem chan struct{}
	ctx context.Context
}

type key struct {
	url  string
	size int
}

type entry struct {
	k    key
	op   paint.ImageOp
	size image.Point
	n    int // decoded bytes
}

// New creates a cache. diskDir may be "" to disable the disk cache.
// invalidate is called (from any goroutine) when an image becomes ready.
func New(fetch Fetcher, diskDir string, invalidate func()) *Cache {
	if diskDir != "" {
		_ = os.MkdirAll(diskDir, 0o700)
	}
	if invalidate == nil {
		invalidate = func() {}
	}
	return &Cache{
		fetch:      fetch,
		dir:        diskDir,
		invalidate: invalidate,
		entries:    map[key]*list.Element{},
		lru:        list.New(),
		maxBytes:   384 << 20,
		inflight:   map[key]bool{},
		failed:     map[key]time.Time{},
		sem:        make(chan struct{}, 8),
		ctx:        context.Background(),
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
// (0 = original size, capped at 2048). Non-blocking.
func (c *Cache) Get(url string, sizePx int) (paint.ImageOp, bool) {
	if c == nil || url == "" {
		return paint.ImageOp{}, false
	}
	k := key{url, sizePx}
	c.mu.Lock()
	if el, ok := c.entries[k]; ok {
		c.lru.MoveToFront(el)
		op := el.Value.(*entry).op
		c.mu.Unlock()
		return op, true
	}
	c.startLocked(k)
	c.mu.Unlock()
	return paint.ImageOp{}, false
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
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.failed[key{url, sizePx}]
	return ok
}

func (c *Cache) startLocked(k key) {
	if c.inflight[k] {
		return
	}
	if t, ok := c.failed[k]; ok && time.Since(t) < 30*time.Second {
		return
	}
	c.inflight[k] = true
	go c.load(k)
}

func (c *Cache) load(k key) {
	c.sem <- struct{}{}
	op, sz, n, err := c.loadImage(k)
	<-c.sem
	c.mu.Lock()
	delete(c.inflight, k)
	if err != nil {
		c.failed[k] = time.Now()
	} else {
		delete(c.failed, k)
		e := &entry{k: k, op: op, size: sz, n: n}
		c.entries[k] = c.lru.PushFront(e)
		c.bytes += n
		c.evict()
	}
	inv := c.invalidate
	c.mu.Unlock()
	inv()
}

func (c *Cache) evict() {
	for c.bytes > c.maxBytes && c.lru.Len() > 1 {
		el := c.lru.Back()
		e := el.Value.(*entry)
		c.lru.Remove(el)
		delete(c.entries, e.k)
		c.bytes -= e.n
	}
}

func (c *Cache) loadImage(k key) (paint.ImageOp, image.Point, int, error) {
	data, save, err := c.raw(k.url)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, err
	}
	img, err := Decode(data)
	if err != nil {
		return paint.ImageOp{}, image.Point{}, 0, err
	}
	save() // only bytes that decoded as an image reach the disk tier
	rgba := Scale(img, k.size)
	return paint.NewImageOp(rgba), rgba.Bounds().Size(), len(rgba.Pix), nil
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
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width*cfg.Height > MaxPixels {
		return nil, ErrTooBig
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// raw returns the encoded bytes from disk (DiskOK URLs only: avatars and
// emoji) or the network, plus a func that saves fetched bytes to the disk
// tier (a no-op for everything else, e.g. message images and files).
func (c *Cache) raw(u string) ([]byte, func(), error) {
	nop := func() {}
	disk := c.dir != "" && DiskOK(u)
	var path string
	if disk {
		h := sha256.Sum256([]byte(u))
		name := hex.EncodeToString(h[:16])
		path = filepath.Join(c.dir, name[:2], name)
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
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
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, path)
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
func DiskOK(raw string) bool {
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
