package imgcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// A plugin's custom emoji can name an object URL as its image: HMojis
// upserts each hidden emoji with a blob: URL its runtime made from the
// image it fetched (URL.createObjectURL), and Chromium loads such a URL
// from the page's own blob store. raw loads one from the blob source the
// UI sets (the plugins' runtime): memory only, never the network or the
// disk tier. With no source, or a URL the runtime doesn't hold (revoked,
// or its runtime gone), the image fails and is tried again later.

var blobSource atomic.Pointer[func(ctx context.Context, u string, max int) ([]byte, int, bool)]

// SetBlobSource sets where blob: URLs load from (nil: nowhere): f gives a
// URL's bytes and the blob's size, the bytes nil past max, and false for a
// URL it doesn't hold or when ctx ends first.
func SetBlobSource(f func(ctx context.Context, u string, max int) (data []byte, size int, ok bool)) {
	if f == nil {
		blobSource.Store(nil)
		return
	}
	blobSource.Store(&f)
}

// errBlobGone: the source doesn't hold the URL, or not yet (plugins start
// after the first frames, and a busy runtime answers late): tried again
// from RetryMin, as a failed download is, not after RetryPermanent.
var errBlobGone = errors.New("imgcache: no such blob: URL")

// blobWait bounds a blob load's wait for the plugins' runtime. Blob loads
// share laneOther's workers with message images, files and unfurls, which
// busy plugins mustn't hold up.
const blobWait = time.Second

func isBlobURL(u string) bool { return strings.HasPrefix(u, "blob:") }

// blobData is a blob: URL's bytes from the blob source, capped as a
// download is (MaxFetchBytes): a bigger blob is refused unread.
func blobData(u string) ([]byte, error) {
	p := blobSource.Load()
	if p == nil {
		return nil, errBlobGone
	}
	ctx, cancel := context.WithTimeout(context.Background(), blobWait)
	defer cancel()
	b, size, ok := (*p)(ctx, u, MaxFetchBytes)
	if ok && size > MaxFetchBytes {
		return nil, fmt.Errorf("%w: a blob: URL of %d bytes, past %d", ErrTooBig, size, MaxFetchBytes)
	}
	if !ok || len(b) == 0 {
		return nil, errBlobGone
	}
	return b, nil
}
