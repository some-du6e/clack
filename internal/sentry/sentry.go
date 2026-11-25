// Package sentry sends Clack's two kinds of reports to the project's
// Sentry (Karim, 2026-11-24: "sentry for the app, hyper+r = report +
// comment"): the report box's (Hyper+R, internal/ui/report_box.go), a
// comment with the inspect report's fields and, if the reporter ticks
// it, a screenshot, sent as Sentry user feedback; and, with Preferences >
// Clack > Send crash reports on, the previous run's crash, sent once at
// the next start (crash.go).
//
// It is a small envelope sender, not sentry-go: it sends only the events
// it is handed, built from fields Clack chose, through Sentry's envelope
// endpoint. Nothing is captured on its own: no breadcrumbs, no hostname,
// no environment, no user, no request. Every string an event carries goes
// through Scrub first (scrub.go).
//
// Only release builds can send: release.yml sets the DSN with
//
//	-ldflags "-X github.com/some-du6e/clack/internal/sentry.dsn=$SENTRY_DSN"
//
// from the repo's Actions secret. A build without it (a local build,
// CI's tests) has no Default client, so it sends nothing, and the report
// box says reports aren't available. Tests send to a fake endpoint on
// loopback (sentrytest), never to Sentry.
package sentry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// dsn is the project's DSN, set by release builds only (the package
// comment). Never logged or shown.
var dsn string

// Configured reports whether this build was made with a DSN.
func Configured() bool { return dsn != "" }

// Client sends events to one Sentry project. Safe for concurrent use.
type Client struct {
	endpoint string // the project's envelope URL
	key      string // the DSN's public key
	release  string // "clack@<version>"
	version  string
	http     *http.Client
}

// Default is the client for this build's DSN, nil without one. version is
// Clack's ("dev" for a dev build), the events' release.
func Default(version string) *Client {
	if dsn == "" {
		return nil
	}
	c, err := New(dsn, version)
	if err != nil {
		// the error never quotes the DSN (ParseDSN)
		return nil
	}
	return c
}

// sendTimeout bounds one send: a report waits for it with its dialog
// open, a crash report runs in the background.
const sendTimeout = 30 * time.Second

// New is a client for dsn (ParseDSN's form).
func New(dsn, version string) (*Client, error) {
	d, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if version == "" {
		version = "dev"
	}
	return &Client{
		endpoint: d.EnvelopeURL(),
		key:      d.PublicKey,
		release:  "clack@" + version,
		version:  version,
		// Go's own transport: sentry.io isn't a Slack host, and nothing
		// of Slack's (no token, no cookie) goes with a report
		// (internal/slack/tlsfp's scan lists this file)
		http: &http.Client{Transport: http.DefaultTransport, Timeout: sendTimeout},
	}, nil
}

// DSN is a parsed Sentry DSN: scheme://<public key>@<host>/<path>/<project>.
type DSN struct {
	Scheme    string
	PublicKey string
	Host      string // with its port, if any
	Path      string // before the project id, "" or "/prefix"
	ProjectID string
}

// ParseDSN reads a DSN. It takes https, and plain http only to a
// loopback address (sentrytest's fake), so a report never goes out
// unencrypted. Its errors never quote the DSN: it is a key.
func ParseDSN(s string) (DSN, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return DSN{}, errors.New("sentry: the DSN isn't a URL")
	}
	d := DSN{Scheme: u.Scheme, Host: u.Host}
	if u.User != nil {
		d.PublicKey = u.User.Username()
	}
	switch {
	case d.Scheme == "https":
	case d.Scheme == "http" && loopback(u.Hostname()):
	default:
		return DSN{}, errors.New("sentry: the DSN must be https")
	}
	if d.Host == "" || d.PublicKey == "" {
		return DSN{}, errors.New("sentry: the DSN has no host or no key")
	}
	p := strings.TrimSuffix(u.Path, "/")
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return DSN{}, errors.New("sentry: the DSN has no project")
	}
	d.Path, d.ProjectID = p[:i], p[i+1:]
	if _, err := strconv.ParseUint(d.ProjectID, 10, 64); err != nil {
		return DSN{}, errors.New("sentry: the DSN's project isn't a number")
	}
	return d, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// EnvelopeURL is where envelopes go: <scheme>://<host><path>/api/<project>/envelope/.
func (d DSN) EnvelopeURL() string {
	return d.Scheme + "://" + d.Host + d.Path + "/api/" + d.ProjectID + "/envelope/"
}

// Attachment is a file sent with an event (the report box's screenshot).
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ErrRateLimited is Sentry's 429: too many events for now.
var ErrRateLimited = errors.New("sentry: rate limited, try again later")

// Send sends ev, with atts, as one envelope and returns the event's id.
// ev's strings are scrubbed first (Event.scrub). ctx bounds the send
// along with the client's own timeout.
func (c *Client) Send(ctx context.Context, ev *Event, atts ...Attachment) (string, error) {
	if c == nil {
		return "", errors.New("sentry: this build can't send reports")
	}
	ev.fill(c)
	ev.scrub()
	body, err := envelope(ev, atts, time.Now())
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("sentry: bad endpoint")
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("User-Agent", "clack/"+c.version)
	req.Header.Set("X-Sentry-Auth", fmt.Sprintf("Sentry sentry_version=7, sentry_client=clack/%s, sentry_key=%s", c.version, c.key))
	resp, err := c.http.Do(req)
	if err != nil {
		// a *url.Error names the URL, whose path has the project id; the
		// key isn't in it, but keep the log to what failed
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("sentry: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", ErrRateLimited
	case resp.StatusCode/100 != 2:
		return "", fmt.Errorf("sentry: the server answered %d", resp.StatusCode)
	}
	return ev.EventID, nil
}

// envelope is ev and atts in Sentry's envelope format: a header line,
// then each item's header line and payload.
func envelope(ev *Event, atts []Attachment, now time.Time) ([]byte, error) {
	payload, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	head, _ := json.Marshal(map[string]any{
		"event_id": ev.EventID,
		"sent_at":  now.UTC().Format(time.RFC3339Nano),
		"sdk":      ev.SDK,
	})
	b.Write(head)
	b.WriteByte('\n')
	typ := "event"
	if ev.Type != "" {
		typ = ev.Type // "feedback"
	}
	item := func(h map[string]any, data []byte) {
		h["length"] = len(data)
		hb, _ := json.Marshal(h)
		b.Write(hb)
		b.WriteByte('\n')
		b.Write(data)
		b.WriteByte('\n')
	}
	item(map[string]any{"type": typ, "content_type": "application/json"}, payload)
	for _, a := range atts {
		item(map[string]any{"type": "attachment", "filename": a.Filename, "content_type": a.ContentType, "attachment_type": "event.attachment"}, a.Data)
	}
	return b.Bytes(), nil
}

// newEventID is a random 32-hex-digit id.
func newEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// osName is Sentry's name for GOOS.
func osName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return goos
}

// runtimeContext is the events' runtime, os and device contexts: the Go
// version, GOOS and GOARCH, nothing about the machine itself.
func runtimeContexts() map[string]any {
	return map[string]any{
		"runtime": map[string]any{"type": "runtime", "name": "go", "version": runtime.Version()},
		"os":      map[string]any{"type": "os", "name": osName(runtime.GOOS)},
		"device":  map[string]any{"type": "device", "arch": runtime.GOARCH},
	}
}
