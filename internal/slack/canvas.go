package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotACanvas is returned by DownloadCanvasHTML for an HTML answer that
// isn't Quip's canvas export (a sign-in page, an error page).
var ErrNotACanvas = errors.New("slack: not a canvas export")

// ErrCanvasTooLarge is DownloadCanvasHTML's for an export past its max.
var ErrCanvasTooLarge = errors.New("slack: canvas export too large")

// ExportStatusError is a canvas export GET answered with a status other
// than 200.
type ExportStatusError struct{ Status int }

func (e *ExportStatusError) Error() string {
	return fmt.Sprintf("slack: canvas export: http %d", e.Status)
}

// ExportAnswered reports whether err is an answer about the file itself
// (not a canvas, too large, refused with a 4xx other than 429, a redirect
// off the file host), not a failure to get one (the network, a timeout, a
// 5xx, a 429, the identical-failure backoff's held error).
func ExportAnswered(err error) bool {
	var se *ExportStatusError
	switch {
	case errors.Is(err, ErrNotACanvas), errors.Is(err, ErrCanvasTooLarge), errors.Is(err, ErrNotAFile), errors.Is(err, ErrUntrustedHost):
		return true
	case errors.As(err, &se):
		return se.Status >= 400 && se.Status < 500 && se.Status != 429
	}
	return false
}

// DownloadCanvasHTML fetches a canvas file's url_private, which is Quip's
// HTML export of the document (docs/CANVASES.md). Download refuses every
// text/html answer, since a file URL that answers with a page is usually a
// redirect to sign-in; this is the one place HTML is expected, so it is
// accepted only when the URL (and every redirect hop, see checkRedirect)
// is a file URL and the body is the export (<div class="quip-canvas-content">).
// The body is read up to max bytes; it is never written to disk. A GET
// that keeps failing the same way backs off as a method call does (the
// guard: the failure again, nothing sent), so a preview that fails isn't
// fetched again until its backoff ends.
func (c *Client) DownloadCanvasHTML(ctx context.Context, rawURL string, max int64) (string, error) {
	k := reqKey(http.MethodGet, rawURL, nil)
	if !skipBackoff(ctx) {
		if err := c.blocked(k, time.Now()); err != nil {
			return "", err
		}
	}
	s, err := c.downloadCanvasHTML(ctx, rawURL, max)
	if ctx.Err() != nil && err != nil { // cancelled by the caller: not a server failure
		return "", err
	}
	if !errors.Is(err, ErrSignedOut) {
		c.record(k, err, time.Now())
	}
	return s, err
}

func (c *Client) downloadCanvasHTML(ctx context.Context, rawURL string, max int64) (string, error) {
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
		return "", &ExportStatusError{Status: resp.StatusCode}
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
		return "", ErrCanvasTooLarge
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

// Quip's canvas calls (docs/CANVASES.md "Slack's edit protocol"): the
// syncer's load-data, fetch-latest and edit-document, binary protobuf in
// a form, answered with binary protobuf. Slack's web client (zRL+) posts
// them to https://<top-level team's host>/canvas/-/<path>?_x_version_ts=…
// with the token as a form field and the d cookie; CanvasCall does the
// same through the Web API transport, so the session guard, the
// identical-failure backoff and Clack's honest headers apply.

// canvasPaths are the calls CanvasCall makes; any other path is refused.
var canvasPaths = map[string]bool{"load-data": true, "fetch-latest": true, "edit-document": true}

// CanvasField is one form field of a canvas call. The form keeps its
// order, as Slack's encodeBody writes the object it's given.
type CanvasField struct{ Name, Value string }

// canvasAnswer receives a canvas call's binary answer in doPost.
type canvasAnswer struct{ b []byte }

// MaxCanvasAnswer is the most of a canvas answer doPost reads: quip's
// decoder refuses anything longer (quip.MaxMessage).
const MaxCanvasAnswer = 16 << 20

// CanvasMethod is a canvas path's name in Clack's logs, the backoff and
// Client.Allow: "canvas/load-data".
func CanvasMethod(path string) string { return "canvas/" + path }

// CanvasCall posts form to /canvas/-/<path> on the top-level team's host
// (the enterprise host and token when the creds have them, else the
// team's: XgZX.Kc) and returns the binary answer. An {"ok":false} answer
// is an *Error (an auth code signs the client out, as Call's do), any
// other status an *HTTPError.
func (c *Client) CanvasCall(ctx context.Context, path string, form []CanvasField) ([]byte, error) {
	if !canvasPaths[path] {
		return nil, fmt.Errorf("%w: canvas %s", ErrNotAllowed, path)
	}
	base, tok := c.base, c.Creds.Token
	if c.Creds.EnterpriseToken != "" {
		base, tok = c.ent, c.Creds.EnterpriseToken
	}
	u := strings.TrimSuffix(base, "api/") + "canvas/-/" + path + "?_x_version_ts=noversion"
	// applyCanvasAuth: cross-origin, so the token goes in the form, last
	body := encodeCanvasForm(append(append([]CanvasField(nil), form...), CanvasField{"token", tok}))
	var out canvasAnswer
	if err := c.post(ctx, CanvasMethod(path), u, "application/x-www-form-urlencoded", []byte(body), &out); err != nil {
		return nil, err
	}
	return out.b, nil
}

// encodeCanvasForm is Slack's encodeBody: name=encodeURIComponent(value)
// pairs joined with &, in order.
func encodeCanvasForm(form []CanvasField) string {
	var b strings.Builder
	for i, f := range form {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(encodeURIComponent(f.Name))
		b.WriteByte('=')
		b.WriteString(encodeURIComponent(f.Value))
	}
	return b.String()
}

// encodeURIComponent is JavaScript's: UTF-8 bytes percent-encoded (upper
// case hex) except A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '!', c == '~', c == '*', c == '\'', c == '(', c == ')':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// canvasJSONError reads a JSON answer to a canvas call: Slack's API
// answers a refused call with {"ok":false,"error":…} (an expired token,
// a missing scope). Protobuf bytes can start with "{" too (0x7b, a group
// for field 15), so the first byte alone doesn't decide: only a JSON
// content type, or a body that parses as an object with "ok", does.
func canvasJSONError(method string, resp *http.Response, data []byte) error {
	ct := resp.Header.Get("Content-Type")
	isJSON := strings.HasPrefix(ct, "application/json")
	var head struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if !isJSON && (len(data) == 0 || data[0] != '{') {
		return nil
	}
	if err := json.Unmarshal(data, &head); err != nil || head.OK == nil {
		if isJSON {
			return &ParseError{Method: method, Err: errors.New("a JSON answer to a canvas call")}
		}
		return nil // protobuf that happens to start with "{"
	}
	if !*head.OK {
		return &Error{Method: method, Code: head.Error, Body: data, Status: resp.StatusCode}
	}
	return &ParseError{Method: method, Err: errors.New("a JSON answer to a canvas call")}
}
