package slack

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// rangeFile is a file's bytes, distinct at every offset.
func rangeFile(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

// A file server answering ranges (http.ServeContent, as files.slack.com
// is assumed to) gives the bytes from the offset with a 206, the total
// from Content-Range, and the request carried the cookie, the range and no
// content coding.
func TestDownloadRange206(t *testing.T) {
	data := rangeFile(100_000)
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "v.mp4", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	for _, tc := range []struct {
		off, end  int64
		want      []byte
		rangeHdr  string
		wantTotal int64
	}{
		{0, -1, data, "bytes=0-", 100_000},
		{70_000, -1, data[70_000:], "bytes=70000-", 100_000},
		{10, 19, data[10:20], "bytes=10-19", 100_000},
	} {
		resp, info, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/T1-F1/v.mp4", tc.off, tc.end)
		if err != nil {
			t.Fatalf("%d-%d: %v", tc.off, tc.end, err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if info != (RangeInfo{Status: 206, Start: tc.off, Total: tc.wantTotal, Seekable: true}) {
			t.Errorf("%d-%d: %+v, want a seekable 206 from %d of %d", tc.off, tc.end, info, tc.off, tc.wantTotal)
		}
		if !bytes.Equal(b, tc.want) {
			t.Errorf("%d-%d: %d bytes, not the file's from the offset", tc.off, tc.end, len(b))
		}
		if got.Get("Range") != tc.rangeHdr || got.Get("Cookie") == "" || !strings.Contains(got.Get("Cookie"), "xoxd-1") ||
			got.Get("Accept-Encoding") != "identity;q=1, *;q=0" || got.Get("User-Agent") != UserAgent() {
			t.Errorf("%d-%d: sent Range %q, Cookie %q, Accept-Encoding %q, UA %q", tc.off, tc.end, got.Get("Range"), got.Get("Cookie"), got.Get("Accept-Encoding"), got.Get("User-Agent"))
		}
	}
	// past the end: a 416, with the total
	if _, info, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/T1-F1/v.mp4", 100_000, -1); err == nil || info.Status != 416 || info.Total != 100_000 {
		t.Errorf("past the end: %+v, %v; want a 416 error knowing the total", info, err)
	} else if he := (*HTTPError)(nil); !errors.As(err, &he) || he.Status != 416 {
		t.Errorf("past the end: %v, want an *HTTPError 416", err)
	}
	if _, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/T1-F1/v.mp4", 5, 2); err == nil {
		t.Error("an end before the offset was sent")
	}
}

// A server that ignores Range answers the whole file with a 200: the
// download is that, from byte 0, and not seekable; its size is the
// Content-Length, unknown (-1) for a chunked answer.
func TestDownloadRange200(t *testing.T) {
	data := rangeFile(5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		if r.URL.Path != "/files-pri/T1-F1/chunked.mp4" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		}
		w.Write(data) // over 2 KB: chunked without a Content-Length
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	for name, total := range map[string]int64{"v.mp4": 5000, "chunked.mp4": -1} {
		resp, info, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/T1-F1/"+name, 1234, -1)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if info != (RangeInfo{Status: 200, Start: 0, Total: total, Seekable: false}) || !bytes.Equal(b, data) {
			t.Errorf("%s: %+v and %d bytes, want the whole file (total %d), not seekable", name, info, len(b), total)
		}
	}
}

// A file URL answering with a page (sign-in) is refused, as Download
// refuses it.
func TestDownloadRangeRefusesHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("<html>sign in</html>"))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	if _, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/x", 0, -1); !errors.Is(err, ErrNotAFile) {
		t.Errorf("HTML answer: %v, want ErrNotAFile", err)
	}
}

// Signed out, nothing is sent.
func TestDownloadRangeSignedOut(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	c.markAuth("invalid_auth")
	if _, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/x", 0, -1); !errors.Is(err, ErrSignedOut) {
		t.Errorf("signed out: %v, want ErrSignedOut", err)
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests after sign-out", hits.Load())
	}
}

// The cookie goes only where Download sends it: never to another host,
// never to a Slack page.
func TestDownloadRangeGuards(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	ctx := context.Background()
	for _, u := range []string{"https://evil.example/x.mp4", "https://files.slack.com.evil.io/x.mp4", "http://files.slack.com/x.mp4", "https://u:p@files.slack.com/x.mp4"} {
		if _, _, err := c.DownloadRange(ctx, u, 0, -1); !errors.Is(err, ErrUntrustedHost) {
			t.Errorf("DownloadRange(%s) = %v, want ErrUntrustedHost", u, err)
		}
	}
	for _, u := range []string{"https://hackclub.slack.com/ssb/redirect", "https://app.slack.com/client/T1/C1", "https://hackclub.slack.com/files/U1/F1/x.mp4"} {
		if _, _, err := c.DownloadRange(ctx, u, 0, -1); !errors.Is(err, ErrNotAFile) {
			t.Errorf("DownloadRange(%s) = %v, want ErrNotAFile", u, err)
		}
	}
}

// A file URL redirecting to a Slack page (/ssb/redirect: the sign-out
// trap) or off Slack is not followed.
func TestDownloadRangeRedirects(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { evilHits.Add(1) }))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files-pri/ssb":
			http.Redirect(w, r, "https://hackclub.slack.com/ssb/redirect?r=1", http.StatusFound)
		case "/files-pri/off":
			http.Redirect(w, r, evil.URL+"/x.mp4", http.StatusTemporaryRedirect)
		}
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	if _, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/ssb", 0, -1); !errors.Is(err, ErrNotAFile) {
		t.Errorf("redirect to /ssb/: %v, want ErrNotAFile", err)
	}
	if resp, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/off", 0, -1); err == nil {
		resp.Body.Close()
		t.Error("followed a redirect off Slack")
	}
	if evilHits.Load() != 0 {
		t.Errorf("the other host got %d requests", evilHits.Load())
	}
}

// Cancelling the context ends a download mid-body: the read fails and the
// server sees its request end.
func TestDownloadRangeCancel(t *testing.T) {
	ended := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-999999/1000000")
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 1000))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(ended)
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	ctx, cancel := context.WithCancel(context.Background())
	resp, info, err := c.DownloadRange(ctx, srv.URL+"/files-pri/x", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !info.Seekable || info.Total != 1_000_000 {
		t.Errorf("%+v", info)
	}
	if _, err := io.ReadFull(resp.Body, make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Error("the body read on after the context was cancelled")
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Error("the server's request didn't end")
	}
	if _, _, err := c.DownloadRange(ctx, srv.URL+"/files-pri/x", 0, -1); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: %v, want context.Canceled", err)
	}
}

func TestParseContentRange(t *testing.T) {
	for _, tc := range []struct {
		in                 string
		first, last, total int64
		ok                 bool
	}{
		{"bytes 0-99/100", 0, 99, 100, true},
		{"bytes 20985-24979/24980", 20985, 24979, 24980, true},
		{"bytes 5-9/*", 5, 9, -1, true},
		{"bytes */24980", -1, -1, 24980, true},
		{"BYTES 1-1/2", 1, 1, 2, true},
		{"bytes 5-4/10", 0, 0, 0, false},
		{"bytes 5-10/10", 0, 0, 0, false},
		{"bytes 5-9", 0, 0, 0, false},
		{"items 0-1/2", 0, 0, 0, false},
		{"bytes */*", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	} {
		f, l, n, ok := parseContentRange(tc.in)
		if ok != tc.ok || (ok && (f != tc.first || l != tc.last || n != tc.total)) {
			t.Errorf("%q: %d %d %d %v", tc.in, f, l, n, ok)
		}
	}
}

// The answer's validators come back with it; an error status keeps its
// Retry-After.
func TestDownloadRangeValidators(t *testing.T) {
	data := rangeFile(1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files-pri/busy" {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "v.mp4", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), bytes.NewReader(data))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	resp, info, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/T1-F1/v.mp4", 10, -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if info.ETag != `"v1"` || info.LastModified != "Sun, 01 Nov 2026 00:00:00 GMT" {
		t.Errorf("validators %q %q", info.ETag, info.LastModified)
	}
	_, _, err = c.DownloadRange(context.Background(), srv.URL+"/files-pri/busy", 0, -1)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 || he.RetryAfterDuration() != 7*time.Second {
		t.Errorf("a 503: %v, want an HTTPError with Retry-After 7 s", err)
	}
}

// A streaming GET isn't cut by the client's request timeout (a video
// plays for more than a minute); it is cut once its body brings nothing
// for StreamIdleTimeout.
func TestDownloadRangeStreams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-99999/100000")
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusPartialContent)
		n := 10
		if r.URL.Path == "/files-pri/stall" {
			n = 1
		}
		for i := 0; i < n; i++ {
			w.Write(make([]byte, 1000))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	c.HTTP.Timeout = 200 * time.Millisecond // shorter than the body takes
	defer func(d time.Duration) { StreamIdleTimeout = d }(StreamIdleTimeout)
	StreamIdleTimeout = 300 * time.Millisecond
	resp, _, err := c.DownloadRange(context.Background(), srv.URL+"/files-pri/slow", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 10000))
	resp.Body.Close()
	if len(got) != 10000 {
		t.Errorf("read %d of the 10,000 bytes sent over 600 ms: the request timeout cut the stream", len(got))
	}
	resp, _, err = c.DownloadRange(context.Background(), srv.URL+"/files-pri/stall", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil || time.Since(t0) > 3*time.Second {
		t.Errorf("a stalled body: %v after %v, want cut after about 300 ms", err, time.Since(t0))
	}
}
