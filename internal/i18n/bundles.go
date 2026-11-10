package i18n

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// CDN is where the client page's translationMap points (window.wd.cdn,
// the page's data-cdn).
const CDN = "https://a.slack-edge.com/bv1-13/"

// Build is the web client build the bundle names below come from.
const Build = "a84662d2d135"

// bundleFiles is the client page's translationMap (the inline script
// right after wd.cdn is set) for build a84662d2d135: each locale's bundle,
// named by a content hash, so a cached copy never goes stale. Slack's CDN
// keeps old builds' files. Fetched with credentials "omit" outside dev and
// gov builds (7LPv ensureTranslationData). en-US has a bundle too, but
// every entry in it is 0 (the source stands) and Slack never loads it.
var bundleFiles = map[string]string{
	"de-DE": "gantry-v2-translations_de-DE.gantry-v2.a6c642245c92c99d8b72.min.json",
	"en-GB": "gantry-v2-translations_en-GB.gantry-v2.c34cee50dc491fc4c180.min.json",
	"en-US": "gantry-v2-translations_en-US.gantry-v2.1a3768856db2587dd7a9.min.json",
	"es-ES": "gantry-v2-translations_es-ES.gantry-v2.ff67f40af3a0e64c1b5f.min.json",
	"es-LA": "gantry-v2-translations_es-LA.gantry-v2.6f38726c8d7218543986.min.json",
	"fr-FR": "gantry-v2-translations_fr-FR.gantry-v2.53cb068b59065caf5fe9.min.json",
	"it-IT": "gantry-v2-translations_it-IT.gantry-v2.571866803e9468b186ee.min.json",
	"ja-JP": "gantry-v2-translations_ja-JP.gantry-v2.3e8c3ed21d06720c0ff7.min.json",
	"ko-KR": "gantry-v2-translations_ko-KR.gantry-v2.026e8b0ff053c7518654.min.json",
	"pt-BR": "gantry-v2-translations_pt-BR.gantry-v2.45c966c2813696d3559a.min.json",
	"zh-CN": "gantry-v2-translations_zh-CN.gantry-v2.efb269ac8ecbda7c3474.min.json",
	"zh-TW": "gantry-v2-translations_zh-TW.gantry-v2.09438414aab6ed7224a4.min.json",
}

// BundleURL is a locale's bundle on the CDN ("" for none).
func BundleURL(loc string) string {
	f := bundleFiles[loc]
	if f == "" {
		return ""
	}
	return CDN + f
}

// ErrOffline: the bundle isn't cached and fetching is off.
var ErrOffline = errors.New("i18n: bundle not cached and offline")

// Loader gets bundles: the cache directory first, else the CDN.
type Loader struct {
	CacheDir string                                                // "" caches nothing
	Fetch    func(ctx context.Context, url string) ([]byte, error) // nil: FetchCDN
	Offline  bool
}

// DefaultCacheDir is <user cache>/clack/i18n ("" when the OS has none).
func DefaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "clack", "i18n")
}

// Load returns loc's catalog.
func (l *Loader) Load(ctx context.Context, loc string) (*Catalog, error) {
	file := bundleFiles[loc]
	if file == "" {
		return nil, errors.New("i18n: no bundle for " + loc)
	}
	var path string
	if l.CacheDir != "" {
		path = filepath.Join(l.CacheDir, file)
		if b, err := os.ReadFile(path); err == nil {
			if c, err := ParseBundle(loc, b); err == nil {
				return c, nil
			}
			_ = os.Remove(path) // damaged: fetch it again
		}
	}
	if l.Offline {
		return nil, ErrOffline
	}
	fetch := l.Fetch
	if fetch == nil {
		fetch = FetchCDN
	}
	b, err := fetch(ctx, CDN+file)
	if err != nil {
		return nil, err
	}
	c, err := ParseBundle(loc, b)
	if err != nil {
		return nil, err
	}
	if path != "" && os.MkdirAll(l.CacheDir, 0o755) == nil {
		tmp := path + ".part"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return c, nil
}

// FetchCDN is a plain GET of a public CDN file: no cookie, no token
// (credentials "omit"), with the ClientHello Clack uses toward Slack.
func FetchCDN(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := tlsfp.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("i18n: " + strings.TrimPrefix(url, CDN) + ": " + res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 32<<20))
}
