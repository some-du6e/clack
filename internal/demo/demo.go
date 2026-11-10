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
	fs, err := fakeslack.Start(fakeslack.Size{Users: 3}) // generate() reads users[2]
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
// URLs over loopback, Slack's standard emoji drawn locally (art), and
// nothing else, ever (no CDN, no image proxy, no unfurl).
func (s *Session) Fetch(ctx context.Context, raw string) ([]byte, error) {
	if strings.HasPrefix(raw, s.FS.URL) {
		return s.FS.Fetch(ctx, raw)
	}
	if strings.HasPrefix(raw, emoji.CDN) {
		if b, ok := StandardEmoji(raw); ok {
			return b, nil
		}
	}
	return nil, fmt.Errorf("demo: not fetching %q (offline)", slack.Redact(raw))
}
