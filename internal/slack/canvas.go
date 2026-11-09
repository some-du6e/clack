package slack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotACanvas is returned by DownloadCanvasHTML for an HTML answer that
// isn't Quip's canvas export (a sign-in page, an error page).
var ErrNotACanvas = errors.New("slack: not a canvas export")

// DownloadCanvasHTML fetches a canvas file's url_private, which is Quip's
// HTML export of the document (docs/CANVASES.md). Download refuses every
// text/html answer, since a file URL that answers with a page is usually a
// redirect to sign-in; this is the one place HTML is expected, so it is
// accepted only when the URL (and every redirect hop, see checkRedirect)
// is a file URL and the body is the export (<div class="quip-canvas-content">).
// The body is read up to max bytes; it is never written to disk.
func (c *Client) DownloadCanvasHTML(ctx context.Context, rawURL string, max int64) (string, error) {
	if c.SignedOut() {
		return "", ErrSignedOut
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", scrubErr(err)
	}
	if !c.credsOK(req.URL) {
		return "", ErrUntrustedHost
	}
	if !FileURL(req.URL) {
		return "", ErrNotAFile
	}
	req.Header.Set("Cookie", c.cookieHeader())
	HonestHeaders(req.Header)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", scrubErr(err)
	}
	defer resp.Body.Close()
	if resp.Request != nil && !FileURL(resp.Request.URL) {
		return "", ErrNotAFile
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack: canvas export: http %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "text/html") && !strings.HasPrefix(ct, "text/plain") {
		return "", ErrNotACanvas
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return "", scrubErr(err)
	}
	if int64(len(b)) > max {
		return "", errors.New("slack: canvas export too large")
	}
	head := b
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !strings.Contains(string(head), "quip-canvas-content") {
		return "", ErrNotACanvas
	}
	return string(b), nil
}
