package emoji

import (
	"runtime"
	"testing"
)

// Slack's getEmojiMode: Apple's set on macOS only.
func TestStyleFollowsPlatform(t *testing.T) {
	want := "google"
	if runtime.GOOS == "darwin" {
		want = "apple"
	}
	if got := Style(); got != want {
		t.Errorf("Style() on %s = %q, want %q", runtime.GOOS, got, want)
	}
	if got := URL("1f601", ""); got != CDN+want+"-medium/1f601.png" {
		t.Errorf("URL default = %q", got)
	}
	if u, ok := StandardURL("grin", "", ""); !ok || u != CDN+want+"-medium/1f601.png" {
		t.Errorf("StandardURL(grin) = %q %v", u, ok)
	}
}

// The CDN size a standard emoji loads for the device pixels it covers:
// Slack's <size>@2x for devicePixelRatio > 1, then bigger sizes, never a
// smaller image than the pixels drawn.
func TestForPixels(t *testing.T) {
	const b = "https://a.slack-edge.com/production-standard-emoji-assets/16.0/"
	med := b + "google-medium/1f601.png"
	for _, c := range []struct {
		u    string
		px   int
		want string
	}{
		{med, 0, med},
		{med, 22, med}, // 22px at 100%: medium, 1:1
		{med, 16, med}, // a smaller box keeps the URL's size, scaled down (a 16px reaction)
		{med, 28, b + "google-medium/1f601@2x.png"},  // 22px at 125% (27.5): medium@2x, 44px, like Slack
		{med, 33, b + "google-medium/1f601@2x.png"},  // 150%
		{med, 44, b + "google-medium/1f601@2x.png"},  // 200%
		{med, 45, b + "google-large/1f601@2x.png"},   // past @2x: the next size's @2x (64)
		{med, 32, b + "google-large/1f601.png"},      // 32px jumbomoji at 100%: large, 1:1, like Slack
		{med, 40, b + "google-medium/1f601@2x.png"},  // jumbomoji at 125%
		{med, 48, b + "google-large/1f601@2x.png"},   // jumbomoji at 150%: large@2x, like Slack
		{med, 64, b + "google-xlarge/1f601.png"},     // 64px exactly: xlarge 1:1
		{med, 66, b + "google-xlarge/1f601@2x.png"},  // 22px at 300%: 128
		{med, 500, b + "google-xlarge/1f601@2x.png"}, // the biggest there is
		{b + "apple-small/1f44d-1f3fd.png", 20, b + "apple-small/1f44d-1f3fd@2x.png"},
		{b + "google-large/1f601@2x.png", 32, b + "google-large/1f601.png"}, // 1x when it covers, whatever the URL said
		{b + "google-xlarge/1f601.png", 64, b + "google-xlarge/1f601.png"},
		{b + "google-medium/1f44d/base.png", 30, b + "google-medium/1f44d/base@2x.png"},
		{"https://a.slack-edge.com/production-standard-emoji-assets/17.0/apple-medium/1fae9.png", 40,
			"https://a.slack-edge.com/production-standard-emoji-assets/17.0/apple-medium/1fae9@2x.png"},
		{"https://emoji.slack-edge.com/T1/party/abc.gif", 40, "https://emoji.slack-edge.com/T1/party/abc.gif"},
		{b + "google-huge/1f601.png", 40, b + "google-huge/1f601.png"},
		{b + "sheet_google_64.png", 40, b + "sheet_google_64.png"},
	} {
		if got := ForPixels(c.u, c.px); got != c.want {
			t.Errorf("ForPixels(%q, %d) = %q, want %q", c.u, c.px, got, c.want)
		}
		if n := AssetPx(c.want); n > 0 && n < c.px && n < 128 {
			t.Errorf("ForPixels(%q, %d) loads %d px: the GPU would stretch it", c.u, c.px, n)
		}
	}
	for u, want := range map[string]int{
		b + "google-small/1f601.png": 16, b + "google-medium/1f601@2x.png": 44, b + "apple-large/1f601.png": 32,
		b + "apple-xlarge/1f601@2x.png": 128, "https://example.com/a.png": 0,
	} {
		if got := AssetPx(u); got != want {
			t.Errorf("AssetPx(%q) = %d, want %d", u, got, want)
		}
	}
}
