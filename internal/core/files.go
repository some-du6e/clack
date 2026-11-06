package core

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// MaxFetch bounds a single Fetch (bytes are held in memory).
const MaxFetch = 64 << 20

// fetchHostAllowed limits Fetch to Slack's file and CDN hosts.
var fetchHostAllowed = func(host string) bool {
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com") || strings.HasSuffix(host, ".slack-edge.com")
}

// Fetch downloads an authenticated Slack URL (files.slack.com thumbnails,
// downloads, avatars, emoji) with the session cookie. Results are kept in a
// memory-only LRU (256 MB) and never written to disk; concurrent requests
// for the same URL share one download.
func (a *App) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && !testAllowHTTP) || !fetchHostAllowed(u.Hostname()) {
		return nil, "", fmt.Errorf("core: Fetch refuses %q", rawURL)
	}
	if e, ok := a.files.get(rawURL); ok {
		return e.data, e.mime, nil
	}
	var data []byte
	var mime string
	err = a.once("f:"+rawURL, func() error {
		if e, ok := a.files.get(rawURL); ok {
			data, mime = e.data, e.mime
			return nil
		}
		resp, err := a.C.Download(ctx, rawURL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("core: Fetch: http %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetch+1))
		if err != nil {
			return err
		}
		if len(b) > MaxFetch {
			return errors.New("core: Fetch: file too large")
		}
		mime = resp.Header.Get("Content-Type")
		if mime == "" {
			mime = http.DetectContentType(b)
		}
		data = b
		a.files.put(rawURL, fileEntry{data: b, mime: mime})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	if data == nil { // shared a concurrent download
		if e, ok := a.files.get(rawURL); ok {
			return e.data, e.mime, nil
		}
		return nil, "", errors.New("core: Fetch: download evicted")
	}
	return data, mime, nil
}

// testAllowHTTP lets tests fetch from an httptest server.
var testAllowHTTP = false

type fileEntry struct {
	data []byte
	mime string
}

// fileCache is a byte-bounded LRU (memory only).
type fileCache struct {
	mu    sync.Mutex
	max   int
	size  int
	order *list.List
	items map[string]*list.Element
}

type fileItem struct {
	key string
	fileEntry
}

func newFileCache(max int) *fileCache {
	return &fileCache{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (c *fileCache) get(k string) (fileEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*fileItem).fileEntry, true
	}
	return fileEntry{}, false
}

func (c *fileCache) put(k string, e fileEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(e.data) > c.max {
		return
	}
	if el, ok := c.items[k]; ok {
		c.size -= len(el.Value.(*fileItem).data)
		c.order.Remove(el)
	}
	c.items[k] = c.order.PushFront(&fileItem{key: k, fileEntry: e})
	c.size += len(e.data)
	for c.size > c.max {
		el := c.order.Back()
		it := el.Value.(*fileItem)
		c.order.Remove(el)
		delete(c.items, it.key)
		c.size -= len(it.data)
	}
}
