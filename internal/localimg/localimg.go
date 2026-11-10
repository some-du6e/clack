// Package localimg holds the bytes of files the viewer is uploading, so an
// optimistic message can draw them before Slack has a thumbnail (Slack's
// PendingFilesGalleryGridImage draws the File's object URL). The bytes
// live in memory only, under URLs with the Scheme prefix; nothing here
// touches the network or the disk.
package localimg

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Scheme prefixes every local image URL ("clack-pending:7").
const Scheme = "clack-pending:"

var (
	mu    sync.Mutex
	files = map[string][]byte{}
	next  atomic.Int64
)

// Put stores data and returns its URL. Drop it when the upload is done.
func Put(data []byte) string {
	u := Scheme + strconv.FormatInt(next.Add(1), 10)
	mu.Lock()
	files[u] = data
	mu.Unlock()
	return u
}

// Get returns the bytes stored under u.
func Get(u string) ([]byte, bool) {
	mu.Lock()
	defer mu.Unlock()
	b, ok := files[u]
	return b, ok
}

// Drop forgets u (a no-op for unknown URLs).
func Drop(u string) {
	if u == "" {
		return
	}
	mu.Lock()
	delete(files, u)
	mu.Unlock()
}

// Is reports whether u is a local image URL.
func Is(u string) bool { return strings.HasPrefix(u, Scheme) }

// Len is the number of stored images (tests).
func Len() int {
	mu.Lock()
	defer mu.Unlock()
	return len(files)
}
