package slack

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

// A file path that a normalising server would read as a Slack page never
// carries the cookie: Go sends /files-pri/../ssb/redirect as written, and
// Slack's edge resolving it would answer the sign-out redirect to a
// cookie-bearing request. Download refuses before any request is made.
func TestDownloadRefusesDotSegments(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	for _, u := range []string{
		"https://hackclub.slack.com/files-pri/../ssb/redirect",
		"https://hackclub.slack.com/files-pri/%2e%2e/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%2E%2E/ssb/redirect",
		"https://hackclub.slack.com/files-pri/T1-F1/..%2f..%2fssb/redirect",
		"https://hackclub.slack.com/files-pri/./../ssb/redirect",
		"https://hackclub.slack.com/files-pri/..;/ssb/redirect",
		"https://hackclub.slack.com/files-pri/x%5c..%5cssb",
		"https://hackclub.slack.com/files-tmb/T1-F1/../../signin",
		"https://files.slack.com/files-pri/T1-F1/../../ssb/redirect",
		"https://files-origin.slack.com/files-pri/%2e%2e/%2e%2e/ssb/redirect",
		// a server that decodes twice (or three times) reads these as ".."
		"https://hackclub.slack.com/files-pri/%252e%252e/%252e%252e/ssb/redirect",
		"https://hackclub.slack.com/files-pri/T1-F1/%252f..%252fssb",
		"https://hackclub.slack.com/files-pri/T1-F1/%25252e%25252e/ssb/redirect",
		// overlong UTF-8 for "." (invalid UTF-8, which some decoders accept)
		"https://hackclub.slack.com/files-pri/%c0%ae%c0%ae/ssb/redirect",
		// look-alikes NFKC folds to dots: fullwidth full stops, the two-dot
		// leader, a segment of three dots
		"https://hackclub.slack.com/files-pri/%EF%BC%8E%EF%BC%8E/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%E2%80%A5/ssb/redirect",
		"https://hackclub.slack.com/files-pri/.../ssb/redirect",
		// the prefix is matched on the wire: this goes out as /%66iles-pri/
		"https://hackclub.slack.com/%66iles-pri/T1-F1/x.png",
	} {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse %s: %v", u, err)
		}
		if FileURL(pu) {
			t.Errorf("FileURL(%s) = true, want false", u)
		}
		if _, err := c.Download(context.Background(), u); !errors.Is(err, ErrNotAFile) {
			t.Errorf("Download(%s) = %v, want ErrNotAFile", u, err)
		}
	}
	// Slack's real file URLs keep working, dots in names included.
	for _, u := range []string{
		"https://files.slack.com/files-pri/T0266FRGM-F0CJ6JF1KC7/image.png",
		"https://files.slack.com/files-pri/T1-F1/download/report.v2.final.pdf",
		"https://hackclub.slack.com/files-pri/T1-F1/..hidden.txt",
		"https://files.slack.com/files-pri/T1-F1/wait...what.png",
		"https://files.slack.com/files-tmb/T1-F1-abc/x_720.jpg",
		"https://files.slack.com/files-pri/T1-F1/100%25.png",
		"https://files.slack.com/files-pri/T1-F1/caf%C3%A9%20%E2%80%A5%20menu.png",
	} {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse %s: %v", u, err)
		}
		if !FileURL(pu) {
			t.Errorf("FileURL(%s) = false, want true", u)
		}
	}
}

// Slack's file hosts serve pages too, so on them as everywhere the path
// decides: files.slack.com/ssb/redirect is a page, not a file, and a
// message's image URL pointing there must never get the session cookie
// (Download, and every redirect hop through checkRedirect).
func TestFileHostsNeedFilePaths(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	for _, u := range []string{
		"https://files.slack.com/ssb/redirect",
		"https://files.slack.com/signin",
		"https://files.slack.com/",
		"https://files-origin.slack.com/customize",
		"https://files-x.slack.com/ssb/redirect",
		"https://files-edu.slack.com/files/U1/F1/x.png", // a file's web page
	} {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse %s: %v", u, err)
		}
		if FileURL(pu) {
			t.Errorf("FileURL(%s) = true, want false", u)
		}
		if _, err := c.Download(context.Background(), u); !errors.Is(err, ErrNotAFile) {
			t.Errorf("Download(%s) = %v, want ErrNotAFile", u, err)
		}
	}
	for _, u := range []string{
		"https://files.slack.com/files-pri/T1-F1/x.png",
		"https://files-origin.slack.com/files-pri/T1-F1/x.png",
		"https://files-edu.slack.com/files-pri/T1-F1/x.png",
		"https://files-edge.slack.com/files-tmb/T1-F1-abc/x_360.png",
		"https://files.slack.com/files-pri-safe/T1-F1/x.png",
		"https://files.slack.com/files-preview/video-stream/T1-F1/x.mp4",
	} {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse %s: %v", u, err)
		}
		if !FileURL(pu) {
			t.Errorf("FileURL(%s) = false, want true", u)
		}
	}
}
