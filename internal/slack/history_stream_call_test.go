package slack

// The streamed decode through Call (history_stream.go): what Call returns
// for an answer it streams is what it returned reading the whole body
// first (an *oldPage is decoded that way: Call streams only a
// *HistoryPage), for pages, error answers and the bodies the split hands
// back; a connection cut inside an element, or a context cancelled
// mid-body, is a request no answer came to (transportError), the page
// untouched; and no read leaves a worker behind.

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// oldPage is a HistoryPage Call decodes the old way: readBody, its head
// check, json.Unmarshal.
type oldPage HistoryPage

// streamMsg is a message of about 2 KB.
func streamMsg(i int) string {
	return fmt.Sprintf(`{"type":"message","ts":"1700000000.%06d","user":"U1","text":"%s","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"x"}]}]}]}`,
		100+i, strings.Repeat("synthetic ", 200))
}

// streamPage is a page of n of them.
func streamPage(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"ok":true,"messages":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(streamMsg(i))
	}
	b.WriteString(`],"has_more":true,"pin_count":1}`)
	return []byte(b.String())
}

// answerServer answers every request with body: "length" with its
// Content-Length, "chunked" flushed in pieces with none, "gzip" gzipped
// (the transport asked for it and decompresses it: no length either).
func answerServer(t *testing.T, body []byte, mode string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "length":
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		case "chunked":
			for b := body; len(b) > 0; {
				n := min(len(b), 3000)
				w.Write(b[:n])
				w.(http.Flusher).Flush()
				b = b[n:]
			}
		case "gzip":
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			zw.Write(body)
			zw.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// splitterGoroutines is the stacks of goroutines still in a streamed
// read's splitter after waiting a moment for them to end ("" for none).
func splitterGoroutines() string {
	for k := 0; ; k++ {
		buf := make([]byte, 4<<20)
		buf = buf[:runtime.Stack(buf, true)]
		if !bytes.Contains(buf, []byte("slack.(*msgSplitter)")) {
			return ""
		}
		if k == 100 {
			return string(buf)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// What Call makes of an answer it streams is what it made reading it
// whole: the page, or the same error (an error answer's Error with its
// body, a ParseError, a decode error). An error answer's body is its own
// bytes, not a mostly empty read buffer kept in the client's backoff
// guard (a gzipped answer has no length, and the first chunk was 1 MB).
func TestHistoryStreamThroughCall(t *testing.T) {
	bodies := map[string][]byte{
		"a page":                {},
		"an error answer":       []byte(`{"ok":false,"error":"channel_not_found"}`),
		"a null element":        []byte(`{"ok":true,"messages":[null],"has_more":false}`),
		"a number error":        []byte(`{"messages":[],"ok":true,"error":5}`),
		"an element it refuses": []byte(`{"ok":true,"messages":[{"type":"message","ts":1700000000}]}`),
		"not json":              []byte(`<html>busy</html>`),
		"empty":                 []byte(``),
	}
	bodies["a page"] = streamPage(40)
	ctx := context.Background()
	for name, body := range bodies {
		for _, mode := range []string{"length", "chunked", "gzip"} {
			srv := answerServer(t, body, mode)
			creds := Creds{Token: "tok", TeamURL: srv.URL + "/team/"}
			var got HistoryPage
			gerr := New(creds).Call(ctx, "conversations.history", nil, &got)
			var want oldPage
			werr := New(creds).Call(ctx, "conversations.history", nil, &want) // its own client: no backoff guard shared
			// the same error, but for the name encoding/json gives the type
			if fmt.Sprintf("%T %v", gerr, gerr) != strings.ReplaceAll(fmt.Sprintf("%T %v", werr, werr), "oldPage.", "HistoryPage.") {
				t.Errorf("%s (%s): streamed, Call says %T %v; reading it whole, %T %v", name, mode, gerr, gerr, werr, werr)
			}
			if !reflect.DeepEqual(got, HistoryPage(want)) {
				t.Errorf("%s (%s): streamed, the page is %+v; reading it whole, %+v", name, mode, got, want)
			}
			var ge, we *Error
			if errors.As(gerr, &ge) && errors.As(werr, &we) {
				if !bytes.Equal(ge.Body, we.Body) {
					t.Errorf("%s (%s): the error's body is %q, reading it whole %q", name, mode, ge.Body, we.Body)
				}
				if cap(ge.Body) > 2*len(ge.Body)+64 {
					t.Errorf("%s (%s): the error keeps a %d-byte buffer for its %d-byte body", name, mode, cap(ge.Body), len(ge.Body))
				}
			}
			if name == "a page" && (gerr != nil || len(got.Messages) != 40) {
				t.Errorf("control: %s (%s): %d messages, %v", name, mode, len(got.Messages), gerr)
			}
		}
	}
	if left := splitterGoroutines(); left != "" {
		t.Errorf("the streamed reads left goroutines behind:\n%s", left)
	}
}

// historyCutServer answers with a page cut off inside an element:
// "length" short of its Content-Length, "exact" right after the first
// chunk of an answer without a length (8 KB), "chunked" inside a chunk,
// "gzip" a gzip stream cut half way.
func historyCutServer(t *testing.T, mode string, payload []byte) *httptest.Server {
	t.Helper()
	cut := len(payload) / 2
	for payload[cut] != 's' { // inside a message's text
		cut++
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "gzip" {
			var z bytes.Buffer
			zw := gzip.NewWriter(&z)
			zw.Write(payload)
			zw.Close()
			b := z.Bytes()[:z.Len()/2]
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Write(b)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		switch mode {
		case "length":
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", len(payload))
			buf.Write(payload[:cut])
		case "exact":
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
			fmt.Fprintf(buf, "%x\r\n", streamChunks[0])
			buf.Write(payload[:streamChunks[0]])
			fmt.Fprintf(buf, "\r\n")
		case "chunked":
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
			fmt.Fprintf(buf, "%x\r\n", cut+5000)
			buf.Write(payload[:cut])
		}
		buf.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A page cut off inside an element is an error, never a page of the
// messages before the cut: read directly (with and without its length),
// and through Call a transportError, the page untouched.
func TestHistoryStreamCutOff(t *testing.T) {
	payload := streamPage(60) // 120 KB: past the first chunks
	for _, mode := range []string{"length", "exact", "chunked", "gzip"} {
		srv := historyCutServer(t, mode, payload)
		for _, hint := range []bool{true, false} {
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			cl := resp.ContentLength
			if !hint {
				cl = -1
			}
			var p HistoryPage
			done, back, err := p.readStreamed(resp.Body, cl)
			resp.Body.Close()
			if err == nil || done || back != nil || !reflect.DeepEqual(p, HistoryPage{}) {
				t.Errorf("%s (hint %v): a cut-off page read as done %v, %d bytes back, error %v, %d messages", mode, hint, done, len(back), err, len(p.Messages))
			}
		}
		if mode == "gzip" {
			continue // a gzip stream the client didn't ask for isn't decompressed by its transport: the read above is its part
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var p HistoryPage
		err := New(Creds{Token: "tok", TeamURL: srv.URL + "/team/"}).Call(ctx, "conversations.history", nil, &p)
		cancel()
		var te *transportError
		if !errors.As(err, &te) || !reflect.DeepEqual(p, HistoryPage{}) {
			t.Errorf("%s: Call over a cut-off page: %v, %d messages; want a transport error and no page", mode, err, len(p.Messages))
		}
	}
	if left := splitterGoroutines(); left != "" {
		t.Errorf("the cut-off reads left goroutines behind:\n%s", left)
	}
}

// A context cancelled while the page is coming in ends the read at once,
// an error and no page, and the workers decoding its first messages go.
func TestHistoryStreamCancelMidBody(t *testing.T) {
	payload := streamPage(400) // 800 KB, a few long messages decoded on workers
	half := len(payload) / 2
	sent := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(payload[:half])
		w.(http.Flusher).Flush()
		close(sent)
		select { // the rest never comes: the client hangs up
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-sent
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	var p HistoryPage
	start := time.Now()
	err := New(Creds{Token: "tok", TeamURL: srv.URL + "/team/"}).Call(ctx, "conversations.history", nil, &p)
	if err == nil || !reflect.DeepEqual(p, HistoryPage{}) {
		t.Errorf("a read cancelled mid-page: %v, %d messages; want an error and no page", err, len(p.Messages))
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the cancelled read took %v to end", d)
	}
	if left := splitterGoroutines(); left != "" {
		t.Errorf("the cancelled read left goroutines behind:\n%s", left)
	}
}

// Long elements go to the shared workers, at most decodeSlots' size of
// them across calls; with every slot taken by other calls, a call decodes
// its elements itself and still gets the page.
func TestHistoryStreamSharedWorkers(t *testing.T) {
	var long strings.Builder
	long.WriteString(`{"ok":true,"messages":[`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			long.WriteByte(',')
		}
		fmt.Fprintf(&long, `{"type":"message","ts":"1700000000.%06d","text":"%s"}`, 100+i, strings.Repeat("w", inlineElement+i))
	}
	long.WriteString(`]}`)
	body := []byte(long.String())
	read := func() (HistoryPage, bool) {
		var p HistoryPage
		done, _, err := p.readStreamed(bytes.NewReader(body), -1)
		return p, done && err == nil
	}
	if p, ok := read(); !ok || len(p.Messages) != 12 {
		t.Fatalf("control: %d messages, streamed %v", len(p.Messages), ok)
	}
	// every slot held elsewhere
	for i := 0; i < cap(decodeSlots); i++ {
		decodeSlots <- struct{}{}
	}
	p, ok := read()
	for i := 0; i < cap(decodeSlots); i++ {
		<-decodeSlots
	}
	if !ok || len(p.Messages) != 12 || len(p.Messages[11].Text) != inlineElement+11 {
		t.Errorf("with no slot free: %d messages, streamed %v", len(p.Messages), ok)
	}
	if left := splitterGoroutines(); left != "" {
		t.Errorf("the reads left goroutines behind:\n%s", left)
	}
}
