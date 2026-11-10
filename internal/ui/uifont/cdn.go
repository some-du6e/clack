package uifont

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	gtot "github.com/go-text/typesetting/font/opentype"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
	"github.com/some-du6e/clack/internal/woff2"
)

// CDN is where slack-kit-gantry-v2-styles.f859766a17f82b2a.min.css
// lives; its @font-face urls are relative ("./NotoSans-Regular-….woff2").
const CDN = "https://a.slack-edge.com/bv1-13-br/"

// webFont is one @font-face rule.
type webFont struct {
	family     string
	file       string
	italic     bool
	wmin, wmax float32
	group      int // 0 the family; 1, 2 unicode-range companions (Cyrillic, box drawing)
}

// webFonts are slack-kit's @font-face rules for the font options' web
// families (lowercased family → rules). Every rule starts its src with
// local(☺), so an installed copy of the font never stands in.
var webFonts = map[string][]webFont{
	"slack-roboto-mono": {
		{family: "Slack-Roboto-Mono", file: "RobotoMono-Regular-6ddf6fb.woff2", wmin: 100, wmax: 700},
		{family: "Slack-Roboto-Mono", file: "RobotoMono-Italic-7937489.woff2", italic: true, wmin: 100, wmax: 700},
		// unicode-range:U+301,U+400-45F,U+490-491,U+4B0-4B1,U+2116
		{family: "Slack-Roboto-Mono", file: "RobotoMono-Cyrillic-Regular-7e038d4.woff2", wmin: 100, wmax: 700, group: 1},
		{family: "Slack-Roboto-Mono", file: "RobotoMono-Cyrillic-Italic-620d827.woff2", italic: true, wmin: 100, wmax: 700, group: 1},
		// unicode-range:U+2500-257F,U+2580-259F, one file for both styles
		{family: "Slack-Roboto-Mono", file: "NotoSansMono-BoxDrawing-6721f32.woff2", wmin: 100, wmax: 700, group: 2},
		{family: "Slack-Roboto-Mono", file: "NotoSansMono-BoxDrawing-6721f32.woff2", italic: true, wmin: 100, wmax: 700, group: 2},
	},
	"open dyslexic": {
		{family: "Open Dyslexic", file: "OpenDyslexic-Regular-61883a0.otf", wmin: 400, wmax: 400},
		{family: "Open Dyslexic", file: "OpenDyslexic-Bold-01eb992.otf", wmin: 700, wmax: 700},
		{family: "Open Dyslexic", file: "OpenDyslexic-Italic-7304cf6.otf", italic: true, wmin: 400, wmax: 400},
		{family: "Open Dyslexic", file: "OpenDyslexic-Bold-Italic-2332d83.otf", italic: true, wmin: 700, wmax: 700},
		// unicode-range:U+370-FFFF
		{family: "Open Dyslexic", file: "OpenDyslexic-Regular-Cyrillic-37f4be9.otf", wmin: 400, wmax: 400, group: 1},
		{family: "Open Dyslexic", file: "OpenDyslexic-Bold-Cyrillic-523b15e.otf", wmin: 700, wmax: 700, group: 1},
		{family: "Open Dyslexic", file: "OpenDyslexic-Italic-Cyrillic-ed57883.otf", italic: true, wmin: 400, wmax: 400, group: 1},
		{family: "Open Dyslexic", file: "OpenDyslexic-Bold-Italic-Cyrillic-25eb806.otf", italic: true, wmin: 700, wmax: 700, group: 1},
	},
	"noto sans": {
		{family: "Noto Sans", file: "NotoSans-Regular-b20b6ba.woff2", wmin: 100, wmax: 700},
		{family: "Noto Sans", file: "NotoSans-Italic-22a8c66.woff2", italic: true, wmin: 100, wmax: 700},
	},
	"atkinson hyperlegible next": {
		{family: "Atkinson Hyperlegible Next", file: "AtkinsonHyperlegibleNext-3795751.woff2", wmin: 200, wmax: 800},
	},
}

// webFamily loads a web family's files: from CacheDir, else the CDN.
func (l *Loader) webFamily(ctx context.Context, rules []webFont) ([]*face, error) {
	var out []*face
	for _, r := range rules {
		b, err := l.cached("cdn:"+r.file, func() ([]byte, error) { return l.cdnFile(ctx, r.file) })
		if err != nil {
			return nil, err
		}
		ld, err := gtot.NewLoader(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("uifont: %s: %w", r.file, err)
		}
		out = append(out, &face{data: b, source: r.file, axes: axesOf(ld), italic: r.italic, wmin: r.wmin, wmax: r.wmax, group: r.group})
	}
	return out, nil
}

// cdnFile returns a CDN font as sfnt bytes (woff2 decoded).
func (l *Loader) cdnFile(ctx context.Context, file string) ([]byte, error) {
	var path string
	if l.CacheDir != "" {
		path = filepath.Join(l.CacheDir, file)
		if b, err := os.ReadFile(path); err == nil {
			if out, err := sfnt(b); err == nil {
				return out, nil
			}
			_ = os.Remove(path) // damaged: fetch it again
		}
	}
	if l.Offline {
		return nil, ErrUnavailable
	}
	fetch := l.Fetch
	if fetch == nil {
		fetch = fetchCDN
	}
	b, err := fetch(ctx, CDN+file)
	if err != nil {
		return nil, err
	}
	out, err := sfnt(b)
	if err != nil {
		return nil, fmt.Errorf("uifont: %s: %w", file, err)
	}
	if path != "" && os.MkdirAll(l.CacheDir, 0o755) == nil {
		tmp := path + ".part"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return out, nil
}

// sfnt checks a downloaded font and decodes woff2.
func sfnt(b []byte) ([]byte, error) {
	if woff2.IsWOFF2(b) {
		var err error
		if b, err = woff2.Decode(b); err != nil {
			return nil, err
		}
	}
	if _, err := gtot.NewLoader(bytes.NewReader(b)); err != nil {
		return nil, err
	}
	return b, nil
}

// fetchCDN is a plain GET: a public asset, no cookie and no token (the
// way the browser loads an @font-face url), with Slack's ClientHello.
func fetchCDN(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
		return nil, errors.New("uifont: " + strings.TrimPrefix(url, CDN) + ": " + res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 8<<20))
}

// DefaultCacheDir is <user cache>/clack/fonts ("" when the OS has none).
func DefaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "clack", "fonts")
}
