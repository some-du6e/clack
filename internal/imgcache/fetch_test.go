package imgcache

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net/netip"
	"testing"
	"time"
)

func TestDialControlRefusesPrivate(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:443":               false,
		"127.8.9.10:80":               false,
		"[::1]:443":                   false,
		"169.254.169.254:80":          false, // cloud metadata
		"10.1.2.3:443":                false,
		"172.16.0.1:443":              false,
		"172.31.255.255:443":          false,
		"192.168.1.1:443":             false,
		"100.64.0.1:443":              false,
		"0.0.0.0:443":                 false,
		"[fe80::1]:443":               false,
		"[fc00::1]:443":               false,
		"[fd12:3456::1]:443":          false,
		"[::ffff:127.0.0.1]:443":      false,
		"[::ffff:169.254.169.254]:80": false,
		"[::]:443":                    false,
		"1.1.1.1:443":                 true,
		"151.101.1.140:443":           true,
		"[2606:4700::1111]:443":       true,
		"172.32.0.1:443":              true,
	} {
		err := dialControl("tcp", addr, nil)
		if (err == nil) != ok {
			t.Errorf("dialControl(%s) = %v, want ok=%v", addr, err, ok)
		}
	}
	if PublicIP(netip.Addr{}) {
		t.Error("zero addr is public")
	}
}

func TestPublicFetchRefusesURLs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, u := range []string{
		"http://example.com/a.png",
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
