package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// releasesAPI is a fake GitHub releases list: the handler answers with
// whatever the test set last.
type releasesAPI struct {
	srv   *httptest.Server
	hits  atomic.Int32
	reply atomic.Pointer[func(w http.ResponseWriter)]
}

func newReleasesAPI(t *testing.T) *releasesAPI {
	api := &releasesAPI{}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.hits.Add(1)
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Errorf("no User-Agent (GitHub refuses those)")
		}
		(*api.reply.Load())(w)
	}))
	t.Cleanup(api.srv.Close)
	t.Setenv("CLACK_UPDATE_URL", api.srv.URL)
	return api
}

func (api *releasesAPI) set(f func(w http.ResponseWriter)) { api.reply.Store(&f) }

func (api *releasesAPI) list(rs ...Release) {
	api.set(func(w http.ResponseWriter) { json.NewEncoder(w).Encode(rs) })
}

// The check Clack runs against GitHub: up to date, a newer pre-release, a
// server error, and the rate limit (429 and 403 with the limit used up),
// with the time GitHub asks for.
func TestReleaseCheckFakeServer(t *testing.T) {
	api := newReleasesAPI(t)
	ctx := context.Background()
	const cur = "v0.1.0-alpha.12"

	// up to date: the newest release is this one
	api.list(Release{Tag: "v0.1.0-alpha.11", Prerelease: true}, Release{Tag: cur, Prerelease: true}, Release{Tag: "v0.1.0-alpha.20", Draft: true})
	r, err := LatestRelease(ctx, cur)
	if err != nil || r.Tag != cur || Newer(r.Tag, cur) {
		t.Fatalf("up to date: %+v %v", r, err)
	}

	// alpha.13 and alpha.14 are out: alpha.14 is found
	api.list(Release{Tag: "v0.1.0-alpha.13", Prerelease: true}, Release{Tag: "v0.1.0-alpha.14", Prerelease: true}, Release{Tag: cur, Prerelease: true})
	r, err = LatestRelease(ctx, cur)
	if err != nil || r.Tag != "v0.1.0-alpha.14" || !Newer(r.Tag, cur) {
		t.Fatalf("newer pre-release: %+v %v", r, err)
	}
	if r.URL != "https://github.com/some-du6e/clack/releases/tag/v0.1.0-alpha.14" {
		t.Errorf("release page %q", r.URL)
	}

	// a server error: a StatusError, not a rate limit
	api.set(func(w http.ResponseWriter) { http.Error(w, "upstream", http.StatusBadGateway) })
	_, err = LatestRelease(ctx, cur)
	var se *StatusError
	var rl *RateLimitError
	if !errors.As(err, &se) || se.Code != 502 || errors.As(err, &rl) {
		t.Fatalf("502: %v", err)
	}

	// a plain 403 (no rate-limit headers) isn't a rate limit either
	api.set(func(w http.ResponseWriter) { http.Error(w, "forbidden", http.StatusForbidden) })
	if _, err = LatestRelease(ctx, cur); errors.As(err, &rl) || !errors.As(err, &se) {
		t.Fatalf("plain 403: %v", err)
	}

	// 429 with Retry-After: seconds from now
	api.set(func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	before := time.Now()
	_, err = LatestRelease(ctx, cur)
	if !errors.As(err, &rl) {
		t.Fatalf("429: %v", err)
	}
	if d := rl.RetryAt().Sub(before); d < 119*time.Second || d > 125*time.Second {
		t.Errorf("429 Retry-After 120: retry in %v", d)
	}

	// 403 with the primary limit used up: x-ratelimit-reset (unix seconds)
	reset := time.Now().Add(40 * time.Minute).Truncate(time.Second)
	api.set(func(w http.ResponseWriter) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		w.WriteHeader(http.StatusForbidden)
	})
	_, err = LatestRelease(ctx, cur)
	if !errors.As(err, &rl) || !rl.RetryAt().Equal(reset) {
		t.Fatalf("403 rate limit: %v", err)
	}
}

func TestReleaseRateLimitHeaders(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	resp := func(code int, kv ...string) *http.Response {
		h := http.Header{}
		for i := 0; i+1 < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return &http.Response{StatusCode: code, Status: strconv.Itoa(code), Header: h}
	}
	for _, c := range []struct {
		name string
		r    *http.Response
		want time.Duration // -1: not a rate limit
	}{
		{"200", resp(200), -1},
		{"plain 403", resp(403), -1},
		{"403 remaining 1", resp(403, "X-RateLimit-Remaining", "1"), -1},
		{"429 bare", resp(429), rateLimitWait},
		{"429 retry-after", resp(429, "Retry-After", "30"), 30 * time.Second},
		{"429 retry-after date", resp(429, "Retry-After", now.Add(5*time.Minute).Format(http.TimeFormat)), 5 * time.Minute},
		{"403 secondary limit", resp(403, "Retry-After", "60"), time.Minute},
		{"403 reset", resp(403, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", strconv.FormatInt(now.Add(17*time.Minute).Unix(), 10)), 17 * time.Minute},
		{"retry-after beats reset", resp(429, "Retry-After", "90", "X-RateLimit-Reset", strconv.FormatInt(now.Add(time.Hour).Unix(), 10)), 90 * time.Second},
		{"reset in the past", resp(403, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)), rateLimitWait},
		{"reset days away", resp(403, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", strconv.FormatInt(now.Add(72*time.Hour).Unix(), 10)), rateLimitWait},
	} {
		rl := rateLimit(c.r, now)
		switch {
		case c.want < 0 && rl != nil:
			t.Errorf("%s: rate limited until %v", c.name, rl.Until)
		case c.want >= 0 && rl == nil:
			t.Errorf("%s: not a rate limit", c.name)
		case c.want >= 0 && rl.Until.Sub(now) != c.want:
			t.Errorf("%s: wait %v, want %v", c.name, rl.Until.Sub(now), c.want)
		}
	}
}
