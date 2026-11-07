package core

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// MaxFetch bounds a single Fetch (bytes are held in memory).
const MaxFetch = 64 << 20

// Fetch downloads a Slack URL. Only https slack.com and its subdomains
// (files.slack.com thumbnails and downloads) get the session cookie, via
// slack.Client.Download; Slack's CDNs (slack-edge.com avatars and emoji,
// slack-imgs.com) are fetched with no credentials. Anything else, such as a
// remote file's url_private on the sender's own host, is refused.
// Results are kept in a memory-only LRU (256 MB) and never written to
// disk; concurrent requests for the same URL share one download.
func (a *App) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	authed := a.C.CredentialsOK(rawURL)
	if !authed && !slack.PublicFetchURL(rawURL) {
		return nil, "", fmt.Errorf("core: Fetch refuses %q", rawURL)
	}
	if e, ok := a.files.get(rawURL); ok {
		return e.data, e.mime, nil
	}
	var data []byte
	var mime string
	err := a.once("f:"+rawURL, func() error {
		if e, ok := a.files.get(rawURL); ok {
			data, mime = e.data, e.mime
			return nil
		}
		var b []byte
		if authed {
			resp, err := a.C.Download(ctx, rawURL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("core: Fetch: http %d", resp.StatusCode)
			}
			if b, err = io.ReadAll(io.LimitReader(resp.Body, MaxFetch+1)); err != nil {
				return err
			}
			if len(b) > MaxFetch {
				return errors.New("core: Fetch: file too large")
			}
			mime = resp.Header.Get("Content-Type")
		} else {
			var err error
			if b, mime, err = slack.GetNoCreds(ctx, rawURL, MaxFetch); err != nil {
				return err
			}
		}
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
