package emoji

import (
	"runtime"
	"strings"
)

// Slack's standard emoji images, as its web client loads them (build
// fa938a086569, 2026-11-12):
//
//   - The set follows the platform, not a pref. getEmojiMode (client-boot
//     module v3vO) is "as_text" when the emoji_mode pref says so, else
//     isMac() ? "apple" : "google" (+oeN's isMac reads the user agent).
//     Slack desktop on Windows and Linux draws Google's emoji; only macOS
//     gets Apple's. emoji_mode's other value is "default".
//   - URLs (Ak7A constructStandardEmojiUrl): <CDN>/<style>-<size>/<unicode>
//     <"@2x" when window.devicePixelRatio > 1>.png, sizes small, medium,
//     large and xlarge (s2rp). Version "16.0", or "17.0" with the emoji_v17
//     feature on.
//   - The URL's size is the Emoji component's CSS class (vbPY): c-emoji__
//     small 16px, medium 22px (the default: message text), large 32px
//     (jumbomoji), larger 56px (loads xlarge), xsmall 12px and relative
//     1em (load small). One <img src>, no srcset; Chromium downscales it to
//     the box.
//
// The CDN's images are 16, 22, 32 and 64 px, and twice that at @2x.

// Style is the emoji set Slack's client shows on this platform: "apple"
// on macOS, "google" everywhere else.
func Style() string {
	if runtime.GOOS == "darwin" {
		return "apple"
	}
	return "google"
}

// URL is the CDN image for an image stem ("1f601") in style ("" for
// Style()), at Slack's default size (medium, 22px). Callers draw it at any
// size: the image cache loads the variant that covers the pixels it is
// asked for (ForPixels).
func URL(stem, style string) string {
	if style == "" {
		style = Style()
	}
	return CDN + style + "-medium/" + stem + ".png"
}

// assetBase is where Slack serves the standard emoji, per version.
const assetBase = "https://a.slack-edge.com/production-standard-emoji-assets/"

// assetSizes are the CDN's size directories and their 1x pixel sizes.
var assetSizes = [...]struct {
	name string
	px   int
}{{"small", 16}, {"medium", 22}, {"large", 32}, {"xlarge", 64}}

// ForPixels is the standard-emoji URL u loads when it is drawn px device
// pixels wide, the way Slack's client picks it: u's own size at 1x when
// that covers px, a bigger size whose 1x is exactly px, u's @2x when that
// covers px (Slack's devicePixelRatio > 1 rule: medium@2x, 44px, for 22px
// emoji at 125%, 150% and 200%), else the next size up whose @2x does. The cache then scales the image down
// to px (CatmullRom), never up. Any other URL, or px ≤ 0, comes back as
// is. Masked skin-tone URLs (<style>-<size>/<stem>/base.png) work too.
func ForPixels(u string, px int) string {
	if px <= 0 {
		return u
	}
	rest, ok := strings.CutPrefix(u, assetBase)
	if !ok {
		return u
	}
	ver, rest, ok := strings.Cut(rest, "/")
	if !ok {
		return u
	}
	dir, file, ok := strings.Cut(rest, "/")
	if !ok || !strings.HasSuffix(file, ".png") {
		return u
	}
	i := strings.LastIndexByte(dir, '-')
	if i <= 0 {
		return u
	}
	style, size := dir[:i], dir[i+1:]
	cur := -1
	for j, s := range assetSizes {
		if s.name == size {
			cur = j
		}
	}
	if cur < 0 {
		return u
	}
	file = strings.TrimSuffix(strings.TrimSuffix(file, ".png"), "@2x")
	j, x2 := cur, false
	switch {
	case px <= assetSizes[cur].px:
	case exact1x(cur, px) >= 0: // a bigger size drawn 1:1 (32px jumbomoji at 100%: large, like Slack)
		j = exact1x(cur, px)
	case px <= 2*assetSizes[cur].px:
		x2 = true
	default:
		x2 = true
		for j < len(assetSizes)-1 && 2*assetSizes[j].px < px {
			j++
		}
	}
	suffix := ".png"
	if x2 {
		suffix = "@2x.png"
	}
	return assetBase + ver + "/" + style + "-" + assetSizes[j].name + "/" + file + suffix
}

// exact1x is the size from cur up whose 1x image is px pixels, or -1.
func exact1x(cur, px int) int {
	for j := cur; j < len(assetSizes); j++ {
		if assetSizes[j].px == px {
			return j
		}
	}
	return -1
}

// AssetPx is the pixel size of the standard-emoji image at u (its size
// directory, doubled for @2x), 0 for any other URL.
func AssetPx(u string) int {
	rest, ok := strings.CutPrefix(u, assetBase)
	if !ok {
		return 0
	}
	_, rest, _ = strings.Cut(rest, "/")
	dir, file, ok := strings.Cut(rest, "/")
	if !ok {
		return 0
	}
	i := strings.LastIndexByte(dir, '-')
	if i <= 0 {
		return 0
	}
	for _, s := range assetSizes {
		if s.name == dir[i+1:] {
			if strings.HasSuffix(file, "@2x.png") {
				return 2 * s.px
			}
			return s.px
		}
	}
	return 0
}
