package imgcache

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func TestPublicFetchRefusesURLs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, u := range []string{
		"http://127.0.0.1/a.png",
		"http://localhost/a.png",
		"http://user:pass@example.com/a.png",
		"file:///etc/passwd",
		"data:image/png;base64,AAAA",
		"javascript:alert(1)",
		"ftp://example.com/a.png",
		"https://127.0.0.1/a.png",
		"https://[::1]/a.png",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/x",
		"https://localhost/x",
		"https://foo.localhost./x",
		"https://user:pass@example.com/x",
		"https:///nohost",
	} {
		if _, err := PublicFetch(ctx, u); err == nil {
			t.Errorf("PublicFetch(%q) allowed", u)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// An image on a plain-http host (Karim's weather icon in an image block)
// loads as Slack's client loads it (getImgProxyUrl, client-boot d0up):
// never as it is, but over https through Slack's image proxy as
// ?c=1&o1=ro&url=<the URL>, with no credentials. An https one off Slack's
// hosts goes the same way; one on Slack's CDN loads directly.
func TestPublicFetchProxiesHTTP(t *testing.T) {
	var got []string
	old := slack.PublicHTTP.Transport
	t.Cleanup(func() { slack.PublicHTTP.Transport = old })
	slack.PublicHTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("credentials sent to %s", r.URL)
		}
		got = append(got, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
			Body: io.NopCloser(bytes.NewReader(pngBytes(4, 4))), Request: r}, nil
	})
	ctx := context.Background()
	const proxy = "https://slack-imgs.com/?c=1&o1=ro&url="
	for _, c := range []struct{ raw, want string }{
		{"http://cdn.weather.example/weather/64x64/day/113.png", proxy + url.QueryEscape("http://cdn.weather.example/weather/64x64/day/113.png")},
		{"http://a.slack-edge.com/x.png", proxy + url.QueryEscape("http://a.slack-edge.com/x.png")},
		{"https://cdn.weather.example/icon.png", proxy + url.QueryEscape("https://cdn.weather.example/icon.png")},
		{"https://a.slack-edge.com/x.png", "https://a.slack-edge.com/x.png"},
	} {
		got = nil
		if b, err := PublicFetch(ctx, c.raw); err != nil || len(b) == 0 {
			t.Errorf("PublicFetch(%q) = %d bytes, %v", c.raw, len(b), err)
		}
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("PublicFetch(%q) requested %q, want %q", c.raw, got, c.want)
		}
	}
}

// SessionFetch: a URL the session's credentials go to loads through the
// authenticated fetcher, anything else through PublicFetch.
func TestSessionFetch(t *testing.T) {
	old := slack.PublicHTTP.Transport
	t.Cleanup(func() { slack.PublicHTTP.Transport = old })
	var public []string
	slack.PublicHTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		public = append(public, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte("img"))), Request: r}, nil
	})
	var authed []string
	f := SessionFetch(func(raw string) bool { return raw == "https://files.slack.com/x.png" }, func(_ context.Context, raw string) ([]byte, error) {
		authed = append(authed, raw)
		return []byte("file"), nil
	})
	ctx := context.Background()
	if b, err := f(ctx, "https://files.slack.com/x.png"); err != nil || string(b) != "file" {
		t.Errorf("credentialed URL: %q, %v", b, err)
	}
	if b, err := f(ctx, "http://cdn.weather.example/a.png"); err != nil || string(b) != "img" {
		t.Errorf("public URL: %q, %v", b, err)
	}
	if len(authed) != 1 || len(public) != 1 || public[0] != "https://slack-imgs.com/?c=1&o1=ro&url="+url.QueryEscape("http://cdn.weather.example/a.png") {
		t.Errorf("authed %q, public %q", authed, public)
	}
}

// pngHeader is a valid PNG signature + IHDR claiming w×h (no image data).
func pngHeader(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestDecodeRefusesBombs(t *testing.T) {
	for _, d := range [][2]uint32{{100000, 100000}, {20000, 10}, {9000, 9000}} {
		if _, err := Decode(pngHeader(d[0], d[1])); !errors.Is(err, ErrTooBig) {
			t.Errorf("Decode(%dx%d) = %v, want ErrTooBig", d[0], d[1], err)
		}
	}
	if _, err := Decode(pngBytes(20, 10)); err != nil {
		t.Errorf("small png: %v", err)
	}
}

func TestBombNotCached(t *testing.T) {
	dir := t.TempDir()
	done := make(chan struct{}, 1)
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		return pngHeader(100000, 100000), nil
	}, dir, func() { done <- struct{}{} })
	u := "https://avatars.slack-edge.com/bomb.png"
	c.Get(u, 0)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if !c.Failed(u, 0) {
		t.Error("bomb decoded")
	}
	if n := countFiles(dir); n != 0 {
		t.Errorf("undecodable bytes written to disk: %d files", n)
	}
}
