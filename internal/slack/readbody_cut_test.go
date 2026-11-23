package slack

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// cutServer answers every request with an answer cut off part way:
// "length" a body shorter than its Content-Length, "chunked" a chunked
// body that stops inside a chunk, "gzip" a gzipped body whose stream
// stops half way (the transport decompresses it), "exact" a cut that
// falls right after a whole 8 KB, readBody's first chunk.
func cutServer(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	payload := []byte(`{"ok":true,"value":"` + strings.Repeat("synthetic ", 20000) + `"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "gzip" {
			var z bytes.Buffer
			zw := gzip.NewWriter(&z)
			zw.Write(payload)
			zw.Close()
			cut := z.Bytes()[:z.Len()/2]
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(cut)))
			w.Write(cut)
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
			buf.Write(payload[:len(payload)/2])
		case "exact":
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", len(payload))
			buf.Write(payload[:8<<10])
		case "chunked":
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
			fmt.Fprintf(buf, "%x\r\n", 10000)
			buf.Write(payload[:10000])
			fmt.Fprintf(buf, "\r\n%x\r\n", 10000)
			buf.Write(payload[10000:15000])
		}
		buf.Flush()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReadBodyCutOff: an answer the connection cuts off is an error, as
// io.ReadAll's is, never a whole answer of fewer bytes; through Call it
// is a request no answer came to (transportError), not a parse of a cut
// body. (Reading chunks with io.ReadFull took the body's own
// io.ErrUnexpectedEOF for ReadFull's short read: the end.)
func TestReadBodyCutOff(t *testing.T) {
	for _, mode := range []string{"length", "exact", "chunked", "gzip"} {
		srv := cutServer(t, mode)
		for _, hint := range []bool{true, false} {
			resp, err := http.Get(srv.URL)
			if err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			cl := resp.ContentLength
			if !hint {
				cl = -1
			}
			got, err := readBody(resp.Body, cl)
			resp.Body.Close()
			if err == nil {
				t.Errorf("%s (hint %v): a cut-off answer read as a whole one of %d bytes", mode, hint, len(got))
			}
		}
		// what io.ReadAll says of the same answer: an error too
		if resp, err := http.Get(srv.URL); err == nil {
			_, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr == nil {
				t.Errorf("%s: io.ReadAll read the cut-off answer whole (the server doesn't cut it)", mode)
			}
		}
		if mode == "gzip" {
			continue // the client's own transport and headers decide gzip: readBody's part is above
		}
		c := New(Creds{Token: "tok", TeamURL: srv.URL + "/team/"})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var out struct {
			Value string `json:"value"`
		}
		err := c.Call(ctx, "plain.method", nil, &out)
		cancel()
		var te *transportError
		if !errors.As(err, &te) {
			t.Errorf("%s: Call over a cut-off answer: %v (value %d bytes), want a transport error", mode, err, len(out.Value))
		}
	}
}
