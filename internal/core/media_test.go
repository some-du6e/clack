package core

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A video's bytes come from the offset asked for, with the cookie; a URL
// that isn't Slack's is refused before any request.
func TestOpenRange(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Cookie") != "d=xoxd-test" {
			http.Error(w, "no cookie", 403)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "v.mp4", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	a := New(slack.New(slack.Creds{Cookie: "xoxd-test", UserID: "USELF", TeamURL: srv.URL + "/"}), store.New(""))
	body, info, err := a.OpenRange(context.Background(), srv.URL+"/files-pri/T1-F1/v.mp4", 1000)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	if !bytes.Equal(b, data[1000:]) || info != (slack.RangeInfo{Status: 206, Start: 1000, Total: int64(len(data)), Seekable: true}) {
		t.Errorf("%d bytes, %+v; want the file from 1000, a seekable 206", len(b), info)
	}
	n := hits.Load()
	for _, u := range []string{"https://evil.example/v.mp4", "http://files.slack.com/files-pri/T1-F1/v.mp4", "https://hackclub.slack.com/ssb/redirect"} {
		if _, _, err := a.OpenRange(context.Background(), u, 0); err == nil {
			t.Errorf("OpenRange(%q) opened", u)
		} else if !strings.Contains(err.Error(), "refuses") && !strings.Contains(err.Error(), "not a file") {
			t.Errorf("OpenRange(%q): %v", u, err)
		}
	}
	if hits.Load() != n {
		t.Errorf("refused URLs made %d requests", hits.Load()-n)
	}
}
