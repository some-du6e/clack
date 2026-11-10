package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// ReleasesURL is the GitHub API for Clack's newest published release
// (drafts and pre-releases are skipped by GitHub). CLACK_UPDATE_URL
// overrides it, for testing the banner and the auto-updater: it may serve
// one release object or a list of them.
var ReleasesURL = "https://api.github.com/repos/some-du6e/clack/releases/latest"

// ReleaseListURL lists recent releases, pre-releases included. A
// pre-release build (v0.1.0-alpha.8) follows the pre-release channel:
// releases/latest never returns a pre-release, so it would never see one.
var ReleaseListURL = "https://api.github.com/repos/some-du6e/clack/releases?per_page=20"

func releasesURL(version string) string {
	if u := os.Getenv("CLACK_UPDATE_URL"); u != "" {
		return u
	}
	if v, ok := parseSemver(version); ok && len(v.pre) > 0 {
		return ReleaseListURL
	}
	return ReleasesURL
}

// Release is a published GitHub release.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	URL        string  `json:"html_url"` // the release page (the banner's button)
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// LatestRelease asks GitHub for the newest release: releases/latest for a
// release build, the newest of the recent releases for a pre-release
// build. It reads release metadata only; downloads are Updater's.
func LatestRelease(ctx context.Context, version string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", releasesURL(version), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clack/"+version)
	resp, err := tlsfp.Client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if rl := rateLimit(resp, time.Now()); rl != nil {
		return Release{}, rl
	}
	if resp.StatusCode != 200 {
		return Release{}, &StatusError{Status: resp.Status, Code: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Release{}, err
	}
	r, err := parseReleases(b)
	if err != nil {
		return Release{}, err
	}
	if r.URL == "" {
		r.URL = "https://github.com/some-du6e/clack/releases/tag/" + r.Tag
	}
	return r, nil
}

// StatusError is a releases API answer other than 200 (and not a rate
// limit).
type StatusError struct {
	Status string // "502 Bad Gateway"
	Code   int
}

func (e *StatusError) Error() string { return "update check: " + e.Status }

// HTTPStatus is the status line (ui reads it through an interface).
func (e *StatusError) HTTPStatus() string { return e.Status }

// RateLimitError is GitHub refusing the check for now: 429, or 403 with
// the rate limit used up. The unauthenticated API allows 60 requests an
// hour per IP, shared by everything on the network.
type RateLimitError struct {
	Status string
	Until  time.Time // wall clock; no request before then
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("update check: %s (rate limited until %s)", e.Status, e.Until.Format(time.RFC3339))
}

// RetryAt is when GitHub takes requests again (ui reads it through an
// interface, without importing this package).
func (e *RateLimitError) RetryAt() time.Time { return e.Until }

// rateLimitWait is the wait when GitHub says "rate limited" without
// saying until when (its docs: at least a minute; secondary limits
// clear in a few minutes).
const rateLimitWait = 10 * time.Minute

// rateLimit reads GitHub's rate-limit answer: 429, or 403 with
// x-ratelimit-remaining: 0 or a retry-after (a plain 403 is not a rate
// limit). retry-after (seconds or an HTTP date) wins over
// x-ratelimit-reset (unix seconds).
func rateLimit(resp *http.Response, now time.Time) *RateLimitError {
	h := resp.Header
	retry := h.Get("Retry-After")
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
	case resp.StatusCode == http.StatusForbidden && (h.Get("X-RateLimit-Remaining") == "0" || retry != ""):
	default:
		return nil
	}
	now = now.Round(0)
	until := time.Time{}
	if retry != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(retry)); err == nil && n >= 0 {
			until = now.Add(time.Duration(n) * time.Second)
		} else if t, err := http.ParseTime(retry); err == nil {
			until = t
		}
	}
	if until.IsZero() {
		if n, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Reset")), 10, 64); err == nil && n > 0 {
			until = time.Unix(n, 0)
		}
	}
	// nonsense (the past, or days away from a wrong clock): the default
	if !until.After(now) || until.Sub(now) > 24*time.Hour {
		until = now.Add(rateLimitWait)
	}
	return &RateLimitError{Status: resp.Status, Until: until}
}

// parseReleases reads one release object, or a list of them (the newest
// non-draft release with a version tag wins).
func parseReleases(b []byte) (Release, error) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || t[0] != '[' {
		var r Release
		err := json.Unmarshal(t, &r)
		return r, err
	}
	var list []Release
	if err := json.Unmarshal(t, &list); err != nil {
		return Release{}, err
	}
	var best Release
	var bv semver
	for _, r := range list {
		v, ok := parseSemver(r.Tag)
		if r.Draft || !ok {
			continue
		}
		if best.Tag == "" || compareSemver(v, bv) > 0 {
			best, bv = r, v
		}
	}
	if best.Tag == "" {
		return Release{}, errors.New("update check: no published release")
	}
	return best, nil
}

// Newer reports whether release tag latest is a newer semantic version than
// current. Either one not being a version (e.g. "dev" builds) gives false.
func Newer(latest, current string) bool {
	l, ok1 := parseSemver(latest)
	c, ok2 := parseSemver(current)
	return ok1 && ok2 && compareSemver(l, c) > 0
}

// Released reports whether version is a release build (vMAJOR.MINOR.PATCH…);
// dev and CI builds don't check for updates.
func Released(version string) bool {
	_, ok := parseSemver(version)
	return ok
}

type semver struct {
	n   [3]int
	pre []string
}

func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		if s[i+1:] == "" {
			return v, false
		}
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.n[i] = n
	}
	return v, true
}

// compareSemver orders versions by semver 2.0 precedence.
func compareSemver(a, b semver) int {
	for i := range a.n {
		if a.n[i] != b.n[i] {
			if a.n[i] < b.n[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1 // a release outranks its pre-releases
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
		case xerr == nil:
			return -1 // numeric identifiers sort before alphanumeric ones
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}
