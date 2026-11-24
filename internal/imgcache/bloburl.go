package imgcache

import (
	"fmt"
	"image"
	"strings"
	"sync/atomic"
)

// A plugin's custom emoji can name an object URL as its image: HMojis
// upserts each hidden emoji with a blob: URL its runtime made from the
// image it fetched (URL.createObjectURL), and Chromium loads such a URL
// from the page's own blob store. raw loads one from the blob source the
// UI sets (the plugins' runtime): memory only, never the network or the
// disk tier. With no source, or a URL the runtime doesn't hold (revoked,
// or its runtime gone), the image fails and is tried again later.

var blobSource atomic.Pointer[func(u string) ([]byte, bool)]

// SetBlobSource sets where blob: URLs load from (nil: nowhere).
func SetBlobSource(f func(u string) ([]byte, bool)) {
	if f == nil {
		blobSource.Store(nil)
		return
	}
	blobSource.Store(&f)
}

var errBlobGone = fmt.Errorf("%w: imgcache: no such blob: URL", image.ErrFormat)

func isBlobURL(u string) bool { return strings.HasPrefix(u, "blob:") }

// blobData is a blob: URL's bytes from the blob source.
func blobData(u string) ([]byte, error) {
	if p := blobSource.Load(); p != nil {
		if b, ok := (*p)(u); ok && len(b) > 0 {
			return b, nil
		}
	}
	return nil, errBlobGone
}
