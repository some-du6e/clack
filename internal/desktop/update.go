package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// ReleasesURL is the GitHub API for Clack's newest published release
// (drafts and pre-releases are skipped by GitHub). CLACK_UPDATE_URL
// overrides it, for testing the banner.
var ReleasesURL = "https://api.github.com/repos/some-du6e/yoloslackgoclient/releases/latest"

func releasesURL() string {
	if u := os.Getenv("CLACK_UPDATE_URL"); u != "" {
		return u
	}
	return ReleasesURL
}

// Release is a published GitHub release.
type Release struct {
	Tag  string `json:"tag_name"`
	Name string `json:"name"`
	URL  string `json:"html_url"` // the release page (the banner's button)
}

// LatestRelease asks GitHub for the newest release. It downloads nothing
// else: the user opens the release page themselves.
func LatestRelease(ctx context.Context, version string) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", releasesURL(), nil)
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
	if resp.StatusCode != 200 {
		return Release{}, fmt.Errorf("update check: %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return Release{}, err
	}
	if r.URL == "" {
		r.URL = "https://github.com/some-du6e/yoloslackgoclient/releases/tag/" + r.Tag
	}
	return r, nil
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
