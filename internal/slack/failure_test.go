package slack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic: CallMultipartFile types the file part as a browser's FormData
// types a File (Content-Type, the filename's ", CR and LF percent-encoded),
// with the token first; CallMultipart's part stays application/octet-stream.
func TestCallMultipartFileLikeFormData(t *testing.T) {
	type part struct{ name, disp, ctype, body string }
	var parts []part
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts = nil
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("Content-Type %q: %v", r.Header.Get("Content-Type"), err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			parts = append(parts, part{p.FormName(), p.Header.Get("Content-Disposition"), p.Header.Get("Content-Type"), string(b)})
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "team-tok", TeamURL: srv.URL + "/"})
	f := MultipartFile{Field: "image", Name: "me \"x\"\r\n.png", Type: "image/png", Data: []byte("PNGDATA")}
	if err := c.CallMultipartFile(context.Background(), "users.preparePhoto", nil, f, nil); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].name != "token" || parts[0].body != "team-tok" {
		t.Fatalf("parts %+v", parts)
	}
	img := parts[1]
	if img.disp != `form-data; name="image"; filename="me %22x%22%0D%0A.png"` || img.ctype != "image/png" || img.body != "PNGDATA" {
		t.Errorf("image part %+v", img)
	}
	if err := c.CallMultipart(context.Background(), "emoji.add", nil, "image", "a.png", []byte("x"), nil); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[1].ctype != "application/octet-stream" {
		t.Errorf("CallMultipart's part %+v", parts)
	}
}

// Synthetic: FailureDetail gives the log Slack's error code and the HTTP
// status, or what failed before an answer, and nothing a call carried.
func TestFailureDetail(t *testing.T) {
	status := 200
	body := `{"ok":false,"error":"too_large"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-secret-token", TeamURL: srv.URL + "/"})
	call := func() error { return c.Call(NoBackoff(context.Background()), "users.setPhoto", nil, nil) }
	if got := FailureDetail(call()); got != "error=too_large http=200" {
		t.Errorf("ok:false: %q", got)
	}
	status, body = 503, "upstream secret page"
	err := call()
	if got := FailureDetail(err); got != "http=503" {
		t.Errorf("503: %q", got)
	}
	if err.Error() != "slack users.setPhoto: http 503" {
		t.Errorf("an HTTP error's text changed: %q", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []struct {
		err  error
		want string
	}{
		{nil, "ok"},
		{&Error{Method: "m", Code: "bad\x1b[31m code\n<script>"}, "error=bad31mcodescript"},
		{&Error{Method: "m", Code: ""}, "error=?"},
		{fmt.Errorf("wrapped: %w", &Error{Method: "m", Code: "not_found", Status: 200}), "error=not_found http=200"},
		{RateLimited, "http=429 (rate limited)"},
		{ErrSignedOut, "signed out"},
		{context.DeadlineExceeded, "timeout"},
		{ctx.Err(), "canceled"},
		{ErrTooLarge, "response too large"},
		{ErrNotAFile, "refused (not a host Clack loads from)"},
		{NewHTTPError(404, "core: Fetch: http 404"), "http=404"},
		{errors.New("https://files.slack.com/secret?t=xoxc-1 failed"), "failed"},
	} {
		if got := FailureDetail(c.err); got != c.want {
			t.Errorf("FailureDetail(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if got := FailureDetail(&Error{Code: strings.Repeat("a", 100)}); got != "error="+strings.Repeat("a", 64) {
		t.Errorf("a long code: %q", got)
	}
}
