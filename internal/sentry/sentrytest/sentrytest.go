// Package sentrytest is a fake Sentry for tests: an envelope endpoint on
// loopback that keeps what it is sent. Clack's tests send their reports
// here, never to Sentry.
package sentrytest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Key and Project are the fake DSN's public key and project.
const (
	Key     = "fakepublickey"
	Project = "4242"
)

// Server is the fake. Its methods are safe from any goroutine.
type Server struct {
	srv *httptest.Server

	mu        sync.Mutex
	envelopes []Envelope
	bad       []string // requests that weren't a well-formed envelope post
	status    int      // the answer's status (0: 200)
}

// Envelope is one envelope the fake was sent.
type Envelope struct {
	Auth   string         // its X-Sentry-Auth header
	Path   string         // the URL path it was posted to
	Header map[string]any // the envelope's first line
	Items  []Item
	Raw    []byte // the whole body
}

// Item is one item of an envelope: its header and payload.
type Item struct {
	Header  map[string]any
	Payload []byte
}

// Type is the item's type ("event", "feedback", "attachment").
func (it Item) Type() string { s, _ := it.Header["type"].(string); return s }

// JSON decodes the item's payload (an event's).
func (it Item) JSON() (map[string]any, error) {
	var m map[string]any
	err := json.Unmarshal(it.Payload, &m)
	return m, err
}

// Start starts the fake for the test's life.
func Start(t testing.TB) *Server {
	t.Helper()
	s := &Server{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// DSN is the fake's DSN (plain http to loopback, which sentry.ParseDSN
// takes for loopback only).
func (s *Server) DSN() string {
	return strings.Replace(s.srv.URL, "http://", "http://"+Key+"@", 1) + "/" + Project
}

// Answer makes the fake answer every later post with status (429: rate
// limited, 500: failing).
func (s *Server) Answer(status int) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// Envelopes are the envelopes posted so far, oldest first.
func (s *Server) Envelopes() []Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Envelope(nil), s.envelopes...)
}

// Bad are the requests that weren't a well-formed envelope post, what
// was wrong with each.
func (s *Server) Bad() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bad...)
}

// Wait waits (at most d) until n envelopes have come, and returns them.
func (s *Server) Wait(t testing.TB, n int, d time.Duration) []Envelope {
	t.Helper()
	dl := time.Now().Add(d)
	for {
		if es := s.Envelopes(); len(es) >= n {
			return es
		}
		if time.Now().After(dl) {
			t.Fatalf("sentrytest: %d envelopes after %v, want %d (bad requests: %q)", len(s.Envelopes()), d, n, s.Bad())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	fail := func(why string) {
		s.mu.Lock()
		s.bad = append(s.bad, why)
		s.mu.Unlock()
		http.Error(w, why, http.StatusBadRequest)
	}
	if r.Method != http.MethodPost {
		fail("method " + r.Method)
		return
	}
	if want := "/api/" + Project + "/envelope/"; r.URL.Path != want {
		fail("path " + r.URL.Path)
		return
	}
	auth := r.Header.Get("X-Sentry-Auth")
	if !strings.Contains(auth, "sentry_key="+Key) || !strings.Contains(auth, "sentry_version=7") {
		fail("auth " + auth)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/x-sentry-envelope" {
		fail("content type " + ct)
		return
	}
	env, err := Parse(body)
	if err != nil {
		fail(err.Error())
		return
	}
	env.Auth, env.Path = auth, r.URL.Path
	s.mu.Lock()
	s.envelopes = append(s.envelopes, env)
	status := s.status
	s.mu.Unlock()
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	id, _ := env.Header["event_id"].(string)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":%q}`, id)
}

// Parse reads an envelope: its header line, then each item's header
// line and its payload, length bytes when the header gives a length
// (then a newline), else to the end of the line.
func Parse(body []byte) (Envelope, error) {
	env := Envelope{Raw: body}
	rd := bufio.NewReader(bytes.NewReader(body))
	line, err := rd.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return env, fmt.Errorf("no envelope header")
	}
	if err := json.Unmarshal(bytes.TrimSpace(line), &env.Header); err != nil {
		return env, fmt.Errorf("envelope header: %v", err)
	}
	for {
		h, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(h)) == 0 {
			if err != nil {
				return env, nil
			}
			continue
		}
		var it Item
		if err := json.Unmarshal(bytes.TrimSpace(h), &it.Header); err != nil {
			return env, fmt.Errorf("item header: %v", err)
		}
		if n, ok := it.Header["length"].(float64); ok {
			it.Payload = make([]byte, int(n))
			if _, err := io.ReadFull(rd, it.Payload); err != nil {
				return env, fmt.Errorf("item of %d bytes: %v", int(n), err)
			}
			if b, err := rd.ReadByte(); err == nil && b != '\n' {
				return env, fmt.Errorf("item %q: no newline after its %s bytes", it.Type(), strconv.Itoa(int(n)))
			}
		} else {
			p, _ := rd.ReadBytes('\n')
			it.Payload = bytes.TrimRight(p, "\n")
		}
		env.Items = append(env.Items, it)
	}
}
