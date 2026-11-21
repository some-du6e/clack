package media_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/slack"
)

// slackSource is a Source reading url through c.DownloadRange, as the app
// opens a message's video (core.OpenRange).
func slackSource(c *slack.Client, id, url string) media.Source {
	return media.Source{FileID: id, Mime: "video/mp4", Ext: "mp4", Open: func(ctx context.Context, off int64) (io.ReadCloser, media.Info, error) {
		resp, info, err := c.DownloadRange(ctx, url, off, -1)
		mi := media.Info{Status: info.Status, Total: info.Total, Seekable: info.Seekable}
		if err != nil {
			return nil, mi, err
		}
		return resp.Body, mi, nil
	}}
}

// SKEPTIC note 1's first test: files.slack.com is assumed to answer
// ranges (206), and nothing may depend on it. The fixture plays to its end
// through the Slack client (the recorded TLS hello's transport, the
// cookie, Download's guards) both from a server answering ranges and from
// one that answers every request with the whole file (200), with the moov
// first and at the end; the one log line says which it was.
func TestPlaysOverDownloadRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		whole bool
	}{
		{"206 faststart", mediatest.GreyGreyRed, false},
		{"206 moov at the end", mediatest.MoovAtEnd, false},
		{"200 faststart", mediatest.GreyGreyRed, true},
		{"200 moov at the end", mediatest.MoovAtEnd, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ranges atomic.Int32
			h := mediatest.Handler(tc.data, tc.whole)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "d=xoxd-test" {
					http.Error(w, "no cookie", 403)
					return
				}
				if r.Header.Get("Range") != "" {
					ranges.Add(1)
				}
				h.ServeHTTP(w, r)
			}))
			defer srv.Close()
			c := slack.New(slack.Creds{Cookie: "xoxd-test", UserID: "USELF", TeamURL: srv.URL + "/"})
			logf, lines := logLines()
			m := media.NewManager(media.NewFake(media.FakeOptions{Log: logf}))
			id := fmt.Sprintf("FDR%d", time.Now().UnixNano())
			r := newRecorder()
			p, err := m.Open(slackSource(c, id, srv.URL+"/files-pri/T1-"+id+"/v.mp4"), r.frameReady, r.onEvent)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			p.Play()
			g := draw(p)
			md := r.wait(t, media.LoadedMetadata, 10*time.Second)
			r.wait(t, media.Ended, 10*time.Second)
			g.end()
			if md.State.Duration != 3*time.Second || md.State.Size.X != 320 {
				t.Errorf("metadata %+v", md.State)
			}
			if ranges.Load() == 0 {
				t.Error("no request asked for a range")
			}
			p.Close()
			<-p.Done()
			status := "206"
			if tc.whole {
				status = "200"
			}
			want := fmt.Sprintf(`media: %s: http %s, %d bytes, seekable %v, ftyp "isom", video "avc1"`, id, status, len(tc.data), !tc.whole)
			deadline := time.Now().Add(2 * time.Second)
			for len(lines()) == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if l := lines(); len(l) != 1 || !strings.Contains(l[0], want) {
				t.Errorf("logged %q, want %q", l, want)
			}
		})
	}
}
