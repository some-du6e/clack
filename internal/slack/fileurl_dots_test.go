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
		"https://files.slack.com/files-tmb/T1-F1-abc/x_720.jpg",
		"https://files.slack.com/files-pri/T1-F1/100%25.png",
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
