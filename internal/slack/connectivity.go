package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The two requests Slack's client makes to learn whether it is back
// (build 889ac449a32d). Core runs the waits around them
// (core/connectivity.go).
//
//   - The network (client-boot 2Aob promiseToTestCDNConnectivity): a GET of
//     the CDN's test pixel on a. and b.slack-edge.com, each with a cache
//     buster (?q=), the first to load winning (Promise.any). They carry no
//     credentials, as the CDN's images don't, so they go through
//     PublicHTTP.
//   - The service (client-boot aZ2/ testApiConnectivity): a POST of
//     https://www.slack.com/api/api.test. Slack's carries
//     _x_reason=api_connectivity_test with its other _x_* metadata; Clack
//     sends none of those (docs/PLUGINS-api-compat.md), and api.test needs
//     no token, so it goes without credentials too.

// CDNPixelURLs are getCDNConnectivityTests' two pixels.
var CDNPixelURLs = []string{
	"https://a.slack-edge.com/f70ec/img/test_cdn_pixel.gif",
	"https://b.slack-edge.com/f70ec/img/test_cdn_pixel.gif",
}

// APITestURL is testSendRequestInner's url.
const APITestURL = "https://www.slack.com/api/api.test"

// defaultProbeRetry is a failed fetch's retryAfter when it brings none
// (2Aob: `e?.retryAfter || 1e3`).
const defaultProbeRetry = time.Second

// ProbeCDN is promiseToTestCDNConnectivity: nil once either pixel loaded.
// A failure's retryAfter is the longest either fetch asked for (its
// Retry-After), else a second each: Slack waits that out before the next
// fetch (the CDN's full backoff).
func ProbeCDN(ctx context.Context) (retryAfter time.Duration, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // the other fetch, once one loaded
	type result struct {
		wait time.Duration
		err  error
	}
	out := make(chan result, len(CDNPixelURLs))
	q := cacheBuster()
	for _, u := range CDNPixelURLs {
		go func(u string) {
			w, err := fetchPixel(ctx, u+"?q="+q)
			out <- result{w, err}
		}(u)
	}
	var errs []error
	for range CDNPixelURLs {
		r := <-out
		if r.err == nil {
			return 0, nil
		}
		errs = append(errs, r.err)
		retryAfter = max(retryAfter, r.wait)
	}
	return retryAfter, fmt.Errorf("slack: the CDN's test pixels didn't load: %w", errors.Join(errs...))
}

func fetchPixel(ctx context.Context, u string) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return defaultProbeRetry, err
	}
	HonestHeaders(req.Header)
	resp, err := PublicHTTP.Do(req)
	if err != nil {
		return defaultProbeRetry, scrubErr(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return probeRetry(resp), &HTTPError{Status: resp.StatusCode, text: "GET " + req.URL.Hostname() + ": " + resp.Status}
	}
	return 0, nil
}

// APITest is testApiConnectivity's request: nil when api.test answered
// {"ok":true}. A failure's retryAfter is the answer's Retry-After, else a
// second (the ConnectivityManager's wait before the next try).
func APITest(ctx context.Context) (retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APITestURL, strings.NewReader(""))
	if err != nil {
		return defaultProbeRetry, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	HonestHeaders(req.Header)
	resp, err := PublicHTTP.Do(req)
	if err != nil {
		return defaultProbeRetry, scrubErr(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return defaultProbeRetry, scrubErr(err)
	}
	if resp.StatusCode != http.StatusOK {
		return probeRetry(resp), &HTTPError{Method: "api.test", Status: resp.StatusCode, text: "slack api.test: " + resp.Status}
	}
	var ok struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(b, &ok); err != nil {
		return defaultProbeRetry, &ParseError{Method: "api.test", Err: err}
	}
	if !ok.OK {
		return defaultProbeRetry, &Error{Method: "api.test", Code: ok.Error, Status: resp.StatusCode}
	}
	return 0, nil
}

func probeRetry(resp *http.Response) time.Duration {
	if n := retryAfterSecs(resp.Header.Get("Retry-After")); n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultProbeRetry
}

// cacheBuster is generateCacheBuster's: a value no cache has seen.
func cacheBuster() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + strconv.FormatUint(rand.Uint64()>>32, 36)
}
