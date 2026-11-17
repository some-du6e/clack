package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// timeoutErr is a net.Error that timed out (an XHR timeout's like).
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

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
// status, or what failed before an answer (each refusal its own label: a
// Slack page where a file was asked for, a redirect off Slack, a host
// Clack doesn't load from; an answer that isn't the JSON asked for), and
// nothing a call carried, not even the URL a *url.Error holds.
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
	var v struct{ ID string }
	syntaxErr := json.Unmarshal([]byte("<html>be right back</html>"), &v)
	typeErr := json.Unmarshal([]byte(`{"ID": 12}`), &v)
	tokURL := "https://hackclub.slack.com/api/users.setPhoto?token=xoxc-secret-1&id=1000000000001"
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
		{ErrNotAFile, "answered with a Slack page, not a file"},
		{&url.Error{Op: "Get", URL: tokURL, Err: ErrNotAFile}, "answered with a Slack page, not a file"},
		{&url.Error{Op: "Get", URL: tokURL, Err: &redirectRefused{`slack: refusing redirect off Slack to "bucket.example.net"`}}, "redirect off Slack refused"},
		{ErrUntrustedHost, "refused (not a host Clack loads from)"},
		{ErrBlockedAddr, "refused (not a host Clack loads from)"},
		{fmt.Errorf("slack m: bad json: %w", syntaxErr), "unexpected answer"},
		{typeErr, "unexpected answer"},
		{&transportError{&url.Error{Op: "Post", URL: tokURL, Err: errors.New("read: connection reset by peer")}}, "network error"},
		{&transportError{io.ErrUnexpectedEOF}, "network error"},
		{&url.Error{Op: "Post", URL: tokURL, Err: timeoutErr{}}, "timeout"},
		{NewHTTPError(404, "core: Fetch: http 404"), "http=404"},
		{errors.New("https://files.slack.com/secret?t=xoxc-1 failed"), "failed"},
	} {
		got := FailureDetail(c.err)
		if got != c.want {
			t.Errorf("FailureDetail(%v) = %q, want %q", c.err, got, c.want)
		}
		if strings.Contains(got, "xoxc") || strings.Contains(got, "hackclub") || strings.Contains(got, "bucket") {
			t.Errorf("FailureDetail(%v) carries part of the call: %q", c.err, got)
		}
	}
	if got := FailureDetail(&Error{Code: strings.Repeat("a", 100)}); got != "error="+strings.Repeat("a", 64) {
		t.Errorf("a long code: %q", got)
	}
}

// Synthetic: a SlackRetries call is tried as Slack's API queue tries it
// (apiq.go): up to 3 times after a 429, 503 or 504, a dropped connection
// or internal_error, waiting Retry-After or the schedule's base (60, 15 or
// 1 s) plus its jitter; once after anything else, and once without
// SlackRetries (where a 429 keeps doPost's own loop).
func TestSlackRetriesLikeSlack(t *testing.T) {
	old := RetryUnit
	RetryUnit = time.Millisecond
	t.Cleanup(func() { RetryUnit = old })
	type answer struct {
		status     int
		body       string
		retryAfter string
		hangUp     bool
	}
	ok := answer{200, `{"ok":true}`, "", false}
	for _, c := range []struct {
		what    string
		answers []answer
		retry   bool // SlackRetries
		calls   int
		wantErr string        // FailureDetail of the error, "" for none
		minWait time.Duration // the schedule's wait, the jitter at 0
	}{
		{"503 then ok", []answer{{503, "", "", false}, ok}, true, 2, "", 60 * time.Millisecond},
		{"504 then ok", []answer{{504, "", "", false}, ok}, true, 2, "", 15 * time.Millisecond},
		{"429 with Retry-After", []answer{{429, "", "2", false}, ok}, true, 2, "", 2 * time.Millisecond},
		{"Retry-After capped", []answer{{503, "", "900", false}, ok}, true, 2, "", 180 * time.Millisecond},
		{"internal_error then ok", []answer{{200, `{"ok":false,"error":"internal_error"}`, "", false}, ok}, true, 2, "", time.Millisecond},
		{"hang-up then ok", []answer{{hangUp: true}, ok}, true, 2, "", 0},
		{"503 three times", []answer{{503, "", "", false}, {503, "", "", false}, {503, "", "", false}, ok}, true, 3, "http=503", 120 * time.Millisecond},
		{"500", []answer{{500, "", "", false}, ok}, true, 1, "http=500", 0},
		{"502", []answer{{502, "", "", false}, ok}, true, 1, "http=502", 0},
		{"bad_image", []answer{{200, `{"ok":false,"error":"bad_image"}`, "", false}, ok}, true, 1, "error=bad_image http=200", 0},
		{"ok:false ratelimited", []answer{{200, `{"ok":false,"error":"ratelimited"}`, "", false}, ok}, true, 1, "error=ratelimited http=200", 0},
		{"not JSON", []answer{{200, "<html>", "", false}, ok}, true, 1, "unexpected answer", 0},
		{"503 without SlackRetries", []answer{{503, "", "", false}, ok}, false, 1, "http=503", 0},
	} {
		t.Run(c.what, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				mu.Lock()
				a := c.answers[min(n, len(c.answers)-1)]
				n++
				mu.Unlock()
				if a.hangUp {
					if hj, ok := w.(http.Hijacker); ok {
						if conn, _, err := hj.Hijack(); err == nil {
							conn.Close()
							return
						}
					}
				}
				if a.retryAfter != "" {
					w.Header().Set("Retry-After", a.retryAfter)
				}
				w.WriteHeader(a.status)
				w.Write([]byte(a.body))
			}))
			defer srv.Close()
			cl := New(Creds{Token: "team-tok", TeamURL: srv.URL + "/"})
			jitter := retryJitter
			retryJitter = func(attempts, cap int) int { return 0 } // the shortest wait; its range is checked below
			t.Cleanup(func() { retryJitter = jitter })
			ctx := NoBackoff(context.Background())
			if c.retry {
				ctx = SlackRetries(ctx)
			}
			t0 := time.Now()
			err := cl.Call(ctx, "users.setPhoto", nil, nil)
			took := time.Since(t0)
			if got := FailureDetail(err); c.wantErr == "" && err != nil || c.wantErr != "" && got != c.wantErr {
				t.Fatalf("error %v (%q), want %q", err, got, c.wantErr)
			}
			mu.Lock()
			got := n
			mu.Unlock()
			if got != c.calls {
				t.Errorf("%d calls, want %d", got, c.calls)
			}
			if took < c.minWait {
				t.Errorf("took %v, want at least the schedule's %v", took, c.minWait)
			}
		})
	}
}

// Synthetic: getBackoffJittered's range, Math.floor(Math.random()*(n-1))
// with n = min(cap, 5·2^attempts): 0 to 8 s after the first try, 0 to 18
// after the second, and Retry-After read as parseInt reads it.
func TestRetryScheduleLikeSlack(t *testing.T) {
	for attempts, hi := range map[int]int{1: 8, 2: 18} {
		lo, top := 1<<30, -1
		for i := 0; i < 2000; i++ {
			v := retryJitter(attempts, 179)
			lo, top = min(lo, v), max(top, v)
		}
		if lo != 0 || top != hi {
			t.Errorf("attempts %d: jitter %d..%d, want 0..%d", attempts, lo, top, hi)
		}
	}
	if v := retryJitter(5, 10); v < 0 || v > 8 {
		t.Errorf("capped jitter %d", v)
	}
	for in, want := range map[string]int{"": 0, "0": 0, "2": 2, "15": 15, "1.5": 1, "10abc": 10, "abc": 0, "-5": 0,
		"Wed, 21 Oct 2015 07:28:00 GMT": 0} {
		if got := retryAfterSecs(in); got != want {
			t.Errorf("retryAfterSecs(%q) = %d, want %d", in, got, want)
		}
	}
	for _, c := range []struct {
		err      error
		attempts int
		wait     time.Duration
		ok       bool
	}{
		{&HTTPError{Status: 503}, 1, 60 * RetryUnit, true},
		{&HTTPError{Status: 429}, 2, 60 * RetryUnit, true},
		{&HTTPError{Status: 504}, 1, 15 * RetryUnit, true},
		{&HTTPError{Status: 503, RetryAfter: 7}, 1, 7 * RetryUnit, true},
		{&HTTPError{Status: 503, RetryAfter: 500}, 1, 180 * RetryUnit, true},
		{&HTTPError{Status: 503}, 3, 0, false},
		{&Error{Code: "internal_error"}, 1, RetryUnit, true},
		{&transportError{io.EOF}, 1, RetryUnit, true},
		{&transportError{&url.Error{Op: "Get", URL: "https://x.slack.com/", Err: ErrNotAFile}}, 1, 0, false},
		{&transportError{&url.Error{Op: "Get", URL: "https://x.slack.com/", Err: &redirectRefused{"r"}}}, 1, 0, false},
		{&HTTPError{Status: 500}, 1, 0, false},
		{&Error{Code: "ratelimited"}, 1, 0, false},
		{errors.New("other"), 1, 0, false},
	} {
		jitter := retryJitter
		retryJitter = func(int, int) int { return 0 }
		wait, ok := slackRetryWait(c.err, c.attempts)
		retryJitter = jitter
		if ok != c.ok || ok && wait != c.wait {
			t.Errorf("slackRetryWait(%v, %d) = %v %v, want %v %v", c.err, c.attempts, wait, ok, c.wait, c.ok)
		}
	}
}

// Synthetic: a SlackRetries call whose next wait doesn't fit its
// context's deadline isn't tried again: the last error comes back.
func TestSlackRetriesStopAtTheDeadline(t *testing.T) {
	n := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		w.WriteHeader(503)
	}))
	defer srv.Close()
	cl := New(Creds{Token: "team-tok", TeamURL: srv.URL + "/"})
	ctx, cancel := context.WithTimeout(SlackRetries(NoBackoff(context.Background())), 5*time.Second) // 60 s to wait: no
	defer cancel()
	t0 := time.Now()
	err := cl.Call(ctx, "users.setPhoto", nil, nil)
	if FailureDetail(err) != "http=503" || time.Since(t0) > 4*time.Second {
		t.Fatalf("error %v after %v", err, time.Since(t0))
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 1 {
		t.Errorf("%d calls, want 1", n)
	}
}
