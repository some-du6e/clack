package imgcache

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// A data: URL's body, as the Fetch standard's data: URL processor reads
// it: after the first comma, the fragment dropped, percent-decoded, then
// forgiving-base64-decoded when the media type part ends in ";base64".
func TestDataBody(t *testing.T) {
	for _, c := range []struct {
		url, body string
		err       error
	}{
		{"data:image/gif;base64,R0lGODlh", "GIF89a", nil},
		{"DATA:image/gif;BASE64,R0lGODlh", "GIF89a", nil},      // the scheme and "base64" in any case
		{"  data:image/gif;base64,R0lGODlh \n", "GIF89a", nil}, // the spaces a URL parser strips
		{"data:image/gif;base64,R0lG\nOD\tlh", "GIF89a", nil},  // tabs and newlines: dropped by the URL parser
		{"data:image/gif;base64,R0lG ODlh", "GIF89a", nil},     // other whitespace: by forgiving base64
		{"data:image/gif; base64 ,R0lGODlh", "GIF89a", nil},    // spaces before "base64", whitespace after
		{"data:;base64,R0lGODlhAQ", "GIF89a\x01", nil},         // the padding optional
		{"data:;base64,R0lGODlhAQ==", "GIF89a\x01", nil},
		{"data:;base64,R0lGODlh%41Q%3D%3D", "GIF89a\x01", nil}, // percent-decoded before base64
		{"data:image/svg+xml,%3Csvg%3E", "<svg>", nil},         // not base64: percent-decoded only
		{"data:,a%zzb%4", "a%zzb%4", nil},                      // a "%" without two hex digits stays
		{"data:,abc#frag", "abc", nil},                         // the fragment isn't the body
		{"data:,", "", nil},
		{"data:image/gif;base64", "", errDataURL},     // no comma: no body
		{"data:;base64,R0lGO", "", errDataURL},        // a length of 4n+1 isn't base64
		{"data:;base64,R0lGODlh-_", "", errDataURL},   // the URL-safe alphabet isn't base64's
		{"data:;base64,R0lG=ODl", "", errDataURL},     // "=" only as padding
		{"https://example.com/a.png", "", errDataURL}, // not a data: URL
		{"data:," + strings.Repeat("a", maxDataURL), "", ErrTooBig},
	} {
		got, err := dataBody(c.url)
		name := c.url[:min(len(c.url), 48)]
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%q: err %v, want %v", name, err, c.err)
			}
			continue
		}
		if err != nil || string(got) != c.body {
			t.Errorf("%q: %q, %v; want %q", name, got, err, c.body)
		}
	}
}

// PageURL: a page's data: URL loads under a key no text has; any other
// URL is itself; one longer than maxDataURL fails without being decoded.
func TestPageURL(t *testing.T) {
	u := "data:image/gif;base64,R0lGODlh"
	if k := PageURL(u); k != pageData+u || PageURL(u) != k {
		t.Errorf("PageURL(%q) = %q", u, k)
	}
	if k := PageURL(" DATA:,x"); k != pageData+" DATA:,x" {
		t.Errorf("a data: URL with a space before it and its scheme upper case: %q", k)
	}
	for _, u := range []string{"https://a.slack-edge.com/x.png", "clack-pending:3", ""} {
		if k := PageURL(u); k != u {
			t.Errorf("PageURL(%q) = %q, want it as it is", u, k)
		}
	}
	long := "data:," + strings.Repeat("a", maxDataURL)
	if k := PageURL(long); k != pageData {
		t.Errorf("a data: URL over maxDataURL: key of %d bytes, want the one that fails", len(k))
	}
	if _, err := dataBody(PageURL(long)[len(pageData):]); !errors.Is(err, image.ErrFormat) {
		t.Errorf("its body: %v, want a malformed data: URL's", err)
	}
	// the memo holds at most pageKeysMax URLs and only short ones
	for i := 0; i < 3*pageKeysMax; i++ {
		PageURL("data:," + strings.Repeat("b", i))
	}
	PageURL("data:," + strings.Repeat("c", pageKeyMemoMax))
	pageKeys.Lock()
	n := len(pageKeys.m)
	pageKeys.Unlock()
	if n > pageKeysMax {
		t.Errorf("the key memo holds %d URLs, more than %d", n, pageKeysMax)
	}
}

// pngDataURL is img as a base64 PNG data: URL.
func pngDataURL(t *testing.T, img image.Image) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

// Both sides of a data: URL. Under PageURL's URL (a plugin page's <img
// src> or background-image) it's decoded from its own text, as Chromium
// decodes one: drawn at its size, nothing asked of the fetcher, nothing
// written to the disk tier. The same URL as it is (message content's) is
// refused as before, by the fetcher (Slack's image proxy refuses one too).
func TestPageDataImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 2 {
				c = color.NRGBA{B: 255, A: 255}
			}
			src.SetNRGBA(x, y, c)
		}
	}
	u := pngDataURL(t, src)
	var mu sync.Mutex
	var asked []string
	fetch := func(ctx context.Context, raw string) ([]byte, error) {
		mu.Lock()
		asked = append(asked, raw)
		mu.Unlock()
		return PublicFetch(ctx, raw)
	}
	dir := t.TempDir()
	c := New(fetch, dir, nil)
	defer c.Close()

	page := PageURL(u)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := c.Get(page, 0); ok {
			break
		}
		if c.Failed(page, 0) {
			t.Fatal("the page's data: URL failed to load")
		}
		if time.Now().After(deadline) {
			t.Fatal("the page's data: URL didn't load in 10s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sz, _ := c.Size(page, 0); sz != image.Pt(4, 2) {
		t.Errorf("drawn at %v, want its 4×2", sz)
	}
	c.mu.Lock()
	px := c.entries[key{page, 0}].Value.(*entry).px[0]
	c.mu.Unlock()
	if l, r := px.RGBAAt(0, 1), px.RGBAAt(3, 0); l != (color.RGBA{R: 255, A: 255}) || r != (color.RGBA{B: 255, A: 255}) {
		t.Errorf("its pixels: left %v, right %v; want red, blue", l, r)
	}

	// message content's: the URL as it is, refused by the fetcher
	for !c.Failed(u, 0) {
		if _, ok := c.Get(u, 0); ok {
			t.Fatal("message content's data: URL was drawn")
		}
		if time.Now().After(deadline) {
			t.Fatal("message content's data: URL neither drew nor failed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), asked...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{u}) {
		t.Errorf("the fetcher was asked for %d URLs, want only message content's (the page's never)", len(got))
	}
	files := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Errorf("%d files in the disk tier, want none", files)
	}
}

// A page's data: URL decodes within the same limits as a fetched image:
// one whose image is wider than MaxSide is refused by its header as
// ErrTooBig (decodeImage reads the dimensions first); one longer than
// maxDataURL is refused before anything decodes.
func TestPageDataLimits(t *testing.T) {
	c := New(func(context.Context, string) ([]byte, error) {
		t.Error("the fetcher was asked for a page's data: URL")
		return nil, errors.New("no network here")
	}, "", nil)
	defer c.Close()
	wide := pngDataURL(t, image.NewGray(image.Rect(0, 0, MaxSide+1, 1)))
	if _, _, _, _, _, err := c.loadImage(key{PageURL(wide), 0}); !errors.Is(err, ErrTooBig) {
		t.Errorf("an image %d px wide in a data: URL: %v, want ErrTooBig", MaxSide+1, err)
	}
	long := "data:image/png;base64," + strings.Repeat("A", maxDataURL)
	if _, _, _, _, _, err := c.loadImage(key{PageURL(long), 0}); !errors.Is(err, image.ErrFormat) {
		t.Errorf("a data: URL over maxDataURL: %v, want a malformed one's", err)
	}
	if _, _, err := c.raw(pageData + long); !errors.Is(err, ErrTooBig) {
		t.Errorf("its text, read as a body: %v, want ErrTooBig", err)
	}
}
