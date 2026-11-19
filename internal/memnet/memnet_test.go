package memnet

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// An HTTP server on the listener answers a client whose transport dials it,
// several requests at once and one after another on kept-alive connections.
func TestHTTPOverPipes(t *testing.T) {
	ln := Listen(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4321})
	if got := ln.Addr().String(); got != "127.0.0.1:4321" {
		t.Fatalf("Addr %q", got)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello "+strings.TrimPrefix(r.URL.Path, "/"))
	})}
	go srv.Serve(ln)
	defer srv.Close()
	c := &http.Client{Transport: &http.Transport{DialContext: ln.DialContext}, Timeout: 5 * time.Second}
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 5; j++ {
				resp, err := c.Get("http://127.0.0.1:4321/demo")
				if err != nil {
					errs <- err
					return
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || string(b) != "hello demo" {
					errs <- errors.New("body " + string(b))
					return
				}
			}
			errs <- nil
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// After Close, Accept and Dial fail, and a Dial waiting on Accept gives
// up with its context.
func TestClose(t *testing.T) {
	ln := Listen(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := ln.DialContext(ctx, "tcp", "x:1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unaccepted dial: %v", err)
	}
	ln.Close()
	ln.Close()
	if _, err := ln.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close: %v", err)
	}
	if _, err := ln.DialContext(context.Background(), "tcp", "x:1"); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Dial after Close: %v", err)
	}
}
