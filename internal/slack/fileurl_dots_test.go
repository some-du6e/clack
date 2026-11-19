package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// noNetwork fails the test on any request: a URL these tests expect to be
// refused must never reach a server. (Without it, the red run's old code
// sent real GETs to files.slack.com/ssb/redirect from the runner.)
type noNetwork struct{ t *testing.T }

func (n noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.t.Errorf("request sent: %s", r.URL.Redacted())
	return nil, errors.New("no network in this test")
}

// fileServer answers every request with a small PNG and records it, so a
// test sees exactly what Download sent, and nothing leaves the runner.
type fileServer struct{ got []*http.Request }

func (f *fileServer) RoundTrip(r *http.Request) (*http.Response, error) {
	f.got = append(f.got, r)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
		Body: io.NopCloser(strings.NewReader("PNG")), Request: r}, nil
}

// refuseAll checks that FileURL refuses each URL and that Download returns
// ErrNotAFile without sending a request.
func refuseAll(t *testing.T, urls []string) {
	t.Helper()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	c.HTTP.Transport = noNetwork{t}
	for _, u := range urls {
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
}

// downloadAll checks that FileURL accepts each URL and that Download sends
// it, with the session cookie, to the host and the path exactly as Slack
// wrote them.
func downloadAll(t *testing.T, urls []string) {
	t.Helper()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1"})
	for _, u := range urls {
		pu, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse %s: %v", u, err)
		}
		if !FileURL(pu) {
			t.Errorf("FileURL(%s) = false, want true", u)
		}
		srv := &fileServer{}
		c.HTTP.Transport = srv
		resp, err := c.Download(context.Background(), u)
		if err != nil {
			t.Errorf("Download(%s) = %v, want the file", u, err)
			continue
		}
		resp.Body.Close()
		want := strings.TrimPrefix(u, "https://"+pu.Host)
		if len(srv.got) != 1 {
			t.Errorf("Download(%s) sent %d requests, want 1", u, len(srv.got))
			continue
		}
		r := srv.got[0]
		if r.URL.Host != pu.Host || r.URL.EscapedPath() != want || r.Header.Get("Cookie") != "d=xoxd-1" {
			t.Errorf("Download(%s) sent %s %s with Cookie %q, want %s %s with d=xoxd-1", u, r.URL.Host, r.URL.EscapedPath(), r.Header.Get("Cookie"), pu.Host, want)
		}
	}
}

// A file path that a normalising server would read as a Slack page never
// carries the cookie: Go sends /files-pri/../ssb/redirect as written, and
// Slack's edge resolving it would answer the sign-out redirect to a
// cookie-bearing request. Download refuses before any request is made.
func TestDownloadRefusesDotSegments(t *testing.T) {
	refuseAll(t, []string{
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
		// a server that decodes twice, three or four times reads these as ".."
		"https://hackclub.slack.com/files-pri/%252e%252e/%252e%252e/ssb/redirect",
		"https://hackclub.slack.com/files-pri/T1-F1/%252f..%252fssb",
		"https://hackclub.slack.com/files-pri/T1-F1/%25252e%25252e/ssb/redirect",
		"https://hackclub.slack.com/files-pri/T1-F1/%2525252e%2525252e/ssb/redirect",
		// still decoding after four passes, and ".." on the fifth
		"https://hackclub.slack.com/files-pri/T1-F1/%252525252e%252525252e/ssb/redirect",
		// overlong UTF-8 for "." (invalid UTF-8, which some decoders accept),
		// as sent and encoded once more
		"https://hackclub.slack.com/files-pri/%c0%ae%c0%ae/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25c0%25ae%25c0%25ae/ssb/redirect",
		// look-alikes NFKC folds to dots: fullwidth full stops (also encoded
		// twice), the two-dot leader, the ellipsis, the vertical ellipsis and
		// two-dot leader, a segment of three dots, and dots padded with spaces
		"https://hackclub.slack.com/files-pri/%EF%BC%8E%EF%BC%8E/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25EF%25BC%258E%25EF%25BC%258E/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%E2%80%A5/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%E2%80%A6/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%EF%B8%99/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%EF%B8%B0/ssb/redirect",
		"https://hackclub.slack.com/files-pri/.../ssb/redirect",
		"https://hackclub.slack.com/files-pri/.%20./ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%20/ssb/redirect",
		// the prefix is matched on the wire: this goes out as /%66iles-pri/
		"https://hackclub.slack.com/%66iles-pri/T1-F1/x.png",
		// bytes a lenient UTF-8 decoder could read as '.', '/' or '\', once a
		// server has decoded twice: 0xAF, 0xDC and 0xAE, which are '/', '\'
		// and '.' with the high bit set, and overlong forms of those. Any
		// other invalid byte reads as U+FFFD, which counts as a dot (0xB7,
		// and 0xFE before a slash).
		"https://hackclub.slack.com/files-pri/x%25c0%25af..%25c0%25afssb/redirect",
		"https://hackclub.slack.com/files-pri/x%25AF..%25AFssb/redirect",
		"https://hackclub.slack.com/files-pri/x%25DC..%25DCssb/redirect",
		"https://hackclub.slack.com/files-pri/%25AE%25AE/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25e0%2580%25ae%25e0%2580%25ae/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25f0%2580%2580%25ae%25f0%2580%2580%25ae/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25f8%2580%2580%2580%25ae%25f8%2580%2580%2580%25ae/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%25B7%25B7/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%25FE/ssb/redirect",
		// the overlong leads are the only guard against an overlong
		// backslash, which carries no 0xAE/0xAF/0xDC byte: C1, E0, F0 and the
		// five-byte F8, each before the continuation bytes of an overlong
		// '\'. C0 too: its overlong '\' (C0 9C) carries no 0xAE/0xAF/0xDC
		// byte either, unlike its overlong '.' and '/'. (smuggles)
		"https://hackclub.slack.com/files-pri/..%25c0%259cssb/redirect",
		"https://hackclub.slack.com/files-pri/..%25c1%259cssb/redirect",
		"https://hackclub.slack.com/files-pri/..%25e0%2581%259cssb/redirect",
		"https://hackclub.slack.com/files-pri/..%25f0%2580%2581%259cssb/redirect",
		"https://hackclub.slack.com/files-pri/..%25f8%2580%2580%2581%259cssb/redirect",
		// NFKC can fold a fullwidth or small percent (U+FF05, U+FE6A) to a
		// literal '%', which a decode→NFKC→decode server then reads as an
		// escape: these build "../../ssb/redirect" and "..%00/ssb/redirect"
		// out of fullwidth percents and are read the same way (round 5).
		"https://files.slack.com/files-pri/T1-F1/%EF%BC%852e%EF%BC%852e%EF%BC%8F%EF%BC%852e%EF%BC%852e%EF%BC%8Fssb%EF%BC%8Fredirect",
		"https://hackclub.slack.com/files-pri/%EF%B9%AA2e%EF%B9%AA2e/ssb/redirect",
		// a fullwidth or small percent before the bytes of an overlong or
		// high-bit '/', '\' or a NUL: the NFKC stage decodes to bytes the raw
		// chain never held (plainDecoded, round 6), refused like their ASCII
		// "%25" twin, the first case
		"https://files.slack.com/files-pri/T1-F1/..%25c0%25af..%25c0%25afssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%BC%85c0%EF%BC%85af..%EF%BC%85c0%EF%BC%85afssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%BC%85c0%EF%BC%85af..%EF%BC%85c0%EF%BC%85af/ssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%B9%AAc0%EF%B9%AAaf..%EF%B9%AAc0%EF%B9%AAafssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%BC%85c1%EF%BC%859c..%EF%BC%85c1%EF%BC%859cssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%BC%85af..%EF%BC%85afssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/..%EF%BC%8500/ssb/redirect",
		// NFKC separator look-alikes: the fullwidth solidus folds to '/', the
		// fullwidth and small reverse solidi to '\' (also encoded twice, and
		// between fullwidth dots)
		"https://files.slack.com/files-pri/..%EF%BC%8Fssb/redirect",
		"https://files.slack.com/files-pri/..%EF%BC%BCssb/redirect",
		"https://files.slack.com/files-pri/..%EF%B9%A8ssb/redirect",
		"https://files.slack.com/files-pri/..%25EF%25BC%258Fssb/redirect",
		"https://files.slack.com/files-pri/T1-F1/%EF%BC%8E%EF%BC%8E%EF%BC%8F%EF%BC%8E%EF%BC%8E%EF%BC%8Fssb/redirect",
		// control bytes (NUL, also encoded twice, and a tab), which a server may
		// strip or trim, and the spaces NFKC folds to ' ' (NBSP, the
		// ideographic and the narrow no-break space)
		"https://hackclub.slack.com/files-pri/..%00/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%2500/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%09/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%C2%A0/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%E3%80%80/ssb/redirect",
		"https://hackclub.slack.com/files-pri/..%E2%80%AF/ssb/redirect",
		// refused only because it still decodes after four passes (it ends as
		// a harmless A.png), and the small full stop and the one-dot leader
		"https://hackclub.slack.com/files-pri/T1-F1/%252525252541.png",
		"https://hackclub.slack.com/files-pri/%EF%B9%92%EF%B9%92/ssb/redirect",
		"https://hackclub.slack.com/files-pri/%E2%80%A4%E2%80%A4/ssb/redirect",
	})
	// Slack's real file URLs keep working: dots in names, a literal % or
	// encoded slash in a name (which decodes to more segments, not to a
	// dot segment), and non-ASCII names with a look-alike inside them.
	downloadAll(t, []string{
		"https://files.slack.com/files-pri/T0266FRGM-F0CJ6JF1KC7/image.png",
		"https://files.slack.com/files-pri/T1-F1/download/report.v2.final.pdf",
		"https://hackclub.slack.com/files-pri/T1-F1/..hidden.txt",
		"https://files.slack.com/files-pri/T1-F1/wait...what.png",
		"https://files.slack.com/files-tmb/T1-F1-abc/x_720.jpg",
		"https://files.slack.com/files-pri/T1-F1/100%25.png",
		"https://files.slack.com/files-pri/T1-F1/50%2525%20off.png",
		"https://files.slack.com/files-pri/T1-F1/uploads%252F2024%252Fphoto.jpg",
		"https://files.slack.com/files-pri/T1-F1/caf%C3%A9%20%E2%80%A5%20menu.png",
		// a literal % before two hex digits that aren't UTF-8 (the name
		// 100%Beef.jpg, or a Latin-1 caf%E9.pdf kept as it was saved), and
		// their thumbnails
		"https://files.slack.com/files-pri/T1-F1/100%25Beef.jpg",
		"https://files.slack.com/files-tmb/T1-F1-abc/100%25Beef_360.jpg",
		"https://files.slack.com/files-pri/T1-F1/caf%25E9.pdf",
		"https://files.slack.com/files-tmb/T1-F1-abc/caf%25E9_thumb_pdf.png",
		"https://files.slack.com/files-pri/T1-F1/10%25Fee.pdf",
		// a fullwidth percent in a name before hex digits that decode to a
		// byte no decoder reads as '.', '/' or '\' (0xAB)
		"https://files.slack.com/files-pri/T1-F1/100%EF%BC%85ab.png",
		"https://files.slack.com/files-tmb/T1-F1-abc/10%25Fee_thumb_pdf.png",
		"https://files.slack.com/files-pri/T1-F1/R%25E9sum%25E9.pdf",
		"https://files.slack.com/files-tmb/T1-F1-abc/R%25E9sum%25E9_thumb_pdf.png",
		"https://files.slack.com/files-pri/T1-F1/download/100%25Beef.jpg",
		// non-ASCII names as NFKC leaves them: NFC and NFD cafe, Japanese with
		// fullwidth brackets and digits, a fullwidth slash and backslash that
		// leave no dot segment, and spaces NFKC folds (a macOS screenshot's
		// narrow no-break space, an NBSP)
		"https://files.slack.com/files-pri/T1-F1/caf%C3%A9.png",
		"https://files.slack.com/files-pri/T1-F1/cafe%CC%81.png",
		"https://files.slack.com/files-pri/T1-F1/%E5%86%99%E7%9C%9F%EF%BC%88%EF%BC%91%EF%BC%89.png",
		"https://files.slack.com/files-pri/T1-F1/%EF%BC%BC%28%5Eo%5E%29%EF%BC%8F.png",
		"https://files.slack.com/files-pri/T1-F1/Screenshot%202026-11-18%20at%2010.00.00%E2%80%AFAM.png",
		"https://files.slack.com/files-pri/T1-F1/a%C2%A0b.png",
		// a fullwidth percent in a name is just a character (NFKC reads it as
		// '%', but what follows isn't an escape, so nothing decodes to a dot)
		"https://files.slack.com/files-pri/T1-F1/%EF%BC%85.png",
		"https://files.slack.com/files-pri/T1-F1/100%EF%BC%85.png",
		// names Go keeps a RawPath for (a '+' and ';' escaped, lower-case
		// hex): Download must send the path exactly as the wire has it, not
		// re-escape it from the decoded form
		"https://files.slack.com/files-pri/T1-F1/a%2Bb%3Bc.png",
		"https://files.slack.com/files-pri/T1-F1/caf%c3%a9.png",
	})
}

// Slack's file hosts serve pages too, so on them as everywhere the path
// decides: files.slack.com/ssb/redirect is a page, not a file, and a
// message's image URL pointing there must never get the session cookie
// (Download, and every redirect hop through checkRedirect).
func TestFileHostsNeedFilePaths(t *testing.T) {
	refuseAll(t, []string{
		"https://files.slack.com/ssb/redirect",
		"https://files.slack.com/signin",
		"https://files.slack.com/",
		"https://files-origin.slack.com/customize",
		"https://files-x.slack.com/ssb/redirect",
		"https://files-edu.slack.com/files/U1/F1/x.png", // a file's web page
	})
	downloadAll(t, []string{
		"https://files.slack.com/files-pri/T1-F1/x.png",
		"https://files-origin.slack.com/files-pri/T1-F1/x.png",
		"https://files-edu.slack.com/files-pri/T1-F1/x.png",
		"https://files-edge.slack.com/files-tmb/T1-F1-abc/x_360.png",
		"https://files.slack.com/files-pri-safe/T1-F1/x.png",
		"https://files.slack.com/files-preview/video-stream/T1-F1/x.mp4",
	})
}
