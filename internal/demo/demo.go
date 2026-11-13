// Package demo is `clack demo`: the normal Clack signed in to a built-in,
// entirely fake workspace, Quillfeather Paper Co. It runs
// internal/fakeslack in-process on 127.0.0.1 with its realtime socket, so
// it works offline, and puts a curated cast, a few days of history and a
// set of scripted, local "agentic" bots on top of it (no LLM calls, no
// network). Nothing in it comes from a real workspace and nothing in it
// ever reaches Slack: see guard.go.
package demo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Options configure a demo session.
type Options struct {
	// Speed scales the bots' cadence: 2 is twice as chatty, 0.5 half.
	// 0 means 1.
	Speed float64
	// Quiet starts the world without the ambient timers (tests drive the
	// bots directly); replies to the viewer still happen.
	Quiet bool
	// Now is the demo's clock at start (zero: time.Now()).
	Now time.Time
	// Seed makes the bots' random choices repeatable (0: from the clock).
	Seed int64
	// Logf, when set, gets the bots' log lines.
	Logf func(format string, args ...any)
}

// Session is a running demo: the fake, its workspace and the bots.
type Session struct {
	FS    *fakeslack.Server
	WS    *Workspace
	World *World

	cancel context.CancelFunc

	refusedMu sync.Mutex
	refused   []string // Fetch's refusals, redacted (Refused)
}

// Start serves the demo workspace on a loopback port and starts the bots.
func Start(opts Options) (*Session, error) {
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Seed == 0 {
		opts.Seed = opts.Now.UnixNano()
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	// a nearly empty generated workspace: the world answers the rest
	// no fixture canvases or lists of the fake's own; permalinks on the
	// demo's host
	fs, err := fakeslack.Start(fakeslack.Size{Users: 3, NoFixtureDocs: true, ActivityViews: true, PermalinkHost: "https://" + TeamDomain + ".slack.com"}) // generate() reads users[2]
	if err != nil {
		return nil, err
	}
	if err := CheckLoopback(fs.URL); err != nil {
		fs.Close()
		return nil, err
	}
	ws, err := Build(fs.URL, opts.Now)
	if err != nil {
		fs.Close()
		return nil, fmt.Errorf("demo: %w", err)
	}
	fs.SetImages(Image)
	ctx, cancel := context.WithCancel(context.Background())
	w := newWorld(fs, ws, opts)
	w.Start(ctx)
	return &Session{FS: fs, WS: ws, World: w, cancel: cancel}, nil
}

// Close stops the bots and the fake.
func (s *Session) Close() {
	s.cancel()
	s.World.Stop()
	s.FS.Close()
}

// Creds are the session's credentials: every host is the loopback fake.
func (s *Session) Creds() slack.Creds { return s.FS.Creds() }

// NewClient is the session's Slack client. It refuses credentials with
// any host that isn't the loopback fake.
func (s *Session) NewClient() (*slack.Client, error) {
	return NewClient(s.Creds())
}

// NewClient builds a client for demo credentials: the team and enterprise
// URLs must be http(s) on a loopback address, or it refuses.
func NewClient(c slack.Creds) (*slack.Client, error) {
	for _, u := range []string{c.TeamURL, c.EnterpriseURL} {
		if err := CheckLoopback(u); err != nil {
			return nil, err
		}
	}
	return slack.New(c), nil
}

// ErrNotLoopback is returned for a demo host that isn't loopback.
var ErrNotLoopback = errors.New("demo: refusing a non-loopback host")

// CheckLoopback is nil only for an http(s) URL on localhost or a loopback
// address.
func CheckLoopback(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !loopbackHost(u.Hostname()) {
		return fmt.Errorf("%w: %q", ErrNotLoopback, raw)
	}
	return nil
}

// Fetch is the image cache's fetcher for a demo session: the fake's own
// URLs over loopback, Slack's standard emoji and static images drawn
// locally (art), and nothing else, ever (no CDN, no image proxy, no
// unfurl).
func (s *Session) Fetch(ctx context.Context, raw string) ([]byte, error) {
	if strings.HasPrefix(raw, s.FS.URL) {
		return s.FS.Fetch(ctx, raw)
	}
	if strings.HasPrefix(raw, emoji.CDN) {
		if b, ok := StandardEmoji(raw); ok {
			return b, nil
		}
	}
	if b, ok := SlackAsset(raw); ok { // Slack's static images, drawn here
		return b, nil
	}
	s.refusedMu.Lock()
	s.refused = append(s.refused, slack.Redact(raw))
	s.refusedMu.Unlock()
	return nil, fmt.Errorf("demo: not fetching %q (offline)", slack.Redact(raw))
}

// Refused is every URL Fetch refused so far (redacted): an image the
// demo shows from outside its fake (a CDN, an unfurl) draws as a gap.
func (s *Session) Refused() []string {
	s.refusedMu.Lock()
	defer s.refusedMu.Unlock()
	return append([]string(nil), s.refused...)
}
