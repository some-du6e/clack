package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The canvas export is the only HTML Clack accepts with the cookie: from a
// file URL, never redirected to a page, and only if it is Quip's export.
func TestDownloadCanvasHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files-pri/T1-F1/canvas":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<div class="quip-canvas-content"><p class="line">hi</p></div>`))
		case "/files-pri/T1-F2/canvas":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<html>sign in</html>"))
		case "/files-pri/T1-F3/canvas":
			http.Redirect(w, r, "/signin", http.StatusFound)
		case "/files-pri/T1-F4/canvas":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte(`<div class="quip-canvas-content"></div>`))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<div class="quip-canvas-content">page</div>`))
		}
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", TeamURL: srv.URL + "/"})
	ctx := context.Background()
	if s, err := c.DownloadCanvasHTML(ctx, srv.URL+"/files-pri/T1-F1/canvas", 1<<20); err != nil || s == "" {
		t.Fatalf("export: %q %v", s, err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, srv.URL+"/files-pri/T1-F2/canvas", 1<<20); !errors.Is(err, ErrNotACanvas) {
		t.Errorf("sign-in page: %v", err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, srv.URL+"/files-pri/T1-F3/canvas", 1<<20); err == nil {
		t.Error("redirect to a page followed")
	}
	if _, err := c.DownloadCanvasHTML(ctx, srv.URL+"/files-pri/T1-F4/canvas", 1<<20); !errors.Is(err, ErrNotACanvas) {
		t.Errorf("non-HTML type: %v", err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, srv.URL+"/docs/T1/F1", 1<<20); !errors.Is(err, ErrNotAFile) {
		t.Errorf("a docs page: %v", err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, "https://hackclub.slack.com/docs/T1/F1", 1<<20); !errors.Is(err, ErrNotAFile) {
		t.Errorf("a slack.com docs page: %v", err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, "https://evil.example/files-pri/x", 1<<20); !errors.Is(err, ErrUntrustedHost) {
		t.Errorf("foreign host: %v", err)
	}
	if _, err := c.DownloadCanvasHTML(ctx, srv.URL+"/files-pri/T1-F1/canvas", 8); err == nil {
		t.Error("over max accepted")
	}
}
