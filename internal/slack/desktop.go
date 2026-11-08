package slack

import (
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

// A desktop session (Creds.Session == SessionDesktop, from LoginMagic) is
// Clack's own: Slack vended its cookies to Clack, as it does to Slack
// desktop. So Clack treats them the way Slack desktop's cookie store does:
//
//   - every request to slack.com sends all of the session's cookies (d, and
//     lc, d-s, … whatever Slack set), not d alone;
//   - every Set-Cookie on a response from slack.com is taken in, so the jar
//     follows Slack's rotation (d-s is a last-seen stamp reset on every
//     answer, and d itself can rotate);
//   - rotated values are written back to creds.json (Client.PersistSession),
//     debounced like Chromium's cookie store, which commits about every 30
//     seconds, and soon (credsFlushSoon) when d or the set of cookies
//     changes;
//   - every request and the realtime socket carry Clack's own User-Agent,
//     the same one the session signed in with (ua.go): never Slack desktop's.
//
// The jar is flat: one value per cookie name, sent to every https slack.com
// host, which is how Slack scopes its session cookies (.slack.com). A
// host-only or path-scoped cookie Slack might set would also go to its other
// slack.com hosts; it never leaves slack.com.
//
// Pasted (browser) sessions don't use any of this: they send d only, never
// d-s, with the same Clack User-Agent, and ignore Set-Cookie (see
// cookieHeader and docs/SIGNOUT-AUDIT.md).

// Debounce of writing a desktop session's rotated cookies to disk.
var (
	credsFlushSoon = 1 * time.Second  // d changed, or a cookie came or went
	credsFlushLazy = 30 * time.Second // only a value like d-s moved
)

// Limits on what Slack may make Clack store.
const (
	maxSessionCookies = 48
	maxCookieValue    = 4096
)

// desktopSession is a desktop session's cookie jar.
type desktopSession struct {
	mu      sync.Mutex
	d       string
	others  map[string]string // every other cookie, by name
	gen     uint64            // bumped on every change
	written uint64            // gen last handed to persist
	timer   *time.Timer
	due     time.Time
	persist func(Creds) error
	base    Creds // the rest of the credentials (Cookie and Cookies are filled in from the jar)

	flushMu sync.Mutex // one write at a time, newest last
}

func newDesktopSession(c Creds) *desktopSession {
	s := &desktopSession{d: c.Cookie, others: map[string]string{}, base: c}
	for _, k := range decodeCookies(c.Cookies) {
		if k.Name != "d" && len(s.others) < maxSessionCookies {
			s.others[k.Name] = k.Value
		}
	}
	return s
}

// header is the Cookie header: d first, then the rest by name.
func (s *desktopSession) header() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := "d=" + s.d
	for _, k := range s.sortedLocked() {
		h += "; " + k.Name + "=" + k.Value
	}
	return h
}

func (s *desktopSession) sortedLocked() []StoredCookie {
	out := make([]StoredCookie, 0, len(s.others))
	for n, v := range s.others {
		out = append(out, StoredCookie{Name: n, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *desktopSession) creds() Creds {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credsLocked()
}

func (s *desktopSession) credsLocked() Creds {
	c := s.base
	c.Cookie = s.d
	c.Cookies = encodeCookies(s.sortedLocked())
	return c
}

func (s *desktopSession) cookie() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d
}

func (s *desktopSession) setCookie(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v != "" && v != s.d {
		s.d = v
		s.changedLocked(true)
	}
}

// unbind forgets the DPoP binding (Slack said dpop-unbind) and saves that.
func (s *desktopSession) unbind() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base.DPoP != "" {
		s.base.DPoP = ""
		s.changedLocked(true)
	}
}

// absorb takes in the cookies a slack.com response set. A cookie Slack
// deletes (Max-Age<=0, an expiry in the past, or an empty value) is
// dropped, except d: Clack keeps sending its last d, and if the session
// really ended Slack answers invalid_auth and the guard stops all traffic.
func (s *desktopSession) absorb(cs []*http.Cookie, now time.Time) {
	if len(cs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	urgent, changed := false, false
	for _, k := range cs {
		if !validCookieName(k.Name) || !validCookieValue(k.Value) || len(k.Value) > maxCookieValue {
			continue
		}
		del := k.MaxAge < 0 || (!k.Expires.IsZero() && k.Expires.Before(now)) || k.Value == ""
		if k.Name == "d" {
			if del {
				log.Printf("slack: Slack cleared this session's d cookie; keeping it (a real sign-out shows up as invalid_auth)")
				continue
			}
			if k.Value != s.d {
				s.d = k.Value
				changed, urgent = true, true
			}
			continue
		}
		old, had := s.others[k.Name]
		switch {
		case del && had:
			delete(s.others, k.Name)
			changed, urgent = true, true
		case del:
		case !had && len(s.others) >= maxSessionCookies:
		case !had:
			s.others[k.Name] = k.Value
			changed, urgent = true, true
		case old != k.Value:
			s.others[k.Name] = k.Value
			changed = true
		}
	}
	if changed {
		s.changedLocked(urgent)
	}
}

func (s *desktopSession) changedLocked(urgent bool) {
	s.gen++
	if s.persist == nil {
		return
	}
	delay := credsFlushLazy
	if urgent {
		delay = credsFlushSoon
	}
	due := time.Now().Add(delay)
	if s.timer != nil {
		if !s.due.After(due) {
			return // already due sooner
		}
		s.timer.Stop()
	}
	s.due = due
	s.timer = time.AfterFunc(delay, s.flush)
}

// flush writes the jar if it changed since the last write.
func (s *desktopSession) flush() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.persist == nil || s.written == s.gen {
		s.mu.Unlock()
		return
	}
	c, gen, persist := s.credsLocked(), s.gen, s.persist
	s.mu.Unlock()
	if err := persist(c); err != nil {
		log.Printf("slack: saving the session's rotated cookies: %v", scrubErr(err))
		return
	}
	s.mu.Lock()
	if gen > s.written {
		s.written = gen
	}
	s.mu.Unlock()
}

// desktopTransport is a desktop session's transport: it takes in every
// Set-Cookie from slack.com (also on redirect hops and the websocket
// handshake, which all go through it) and, for DPoP-bound sessions, adds a
// fresh proof to each slack.com request (dpop.go).
type desktopTransport struct {
	base http.RoundTripper
	c    *Client
}

func (t *desktopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ok := t.c.credsOK(req.URL)
	if ok && t.c.dpop != nil {
		r, err := t.c.dpop.sign(req)
		if err != nil {
			if req.Body != nil {
				req.Body.Close()
			}
			return nil, err
		}
		req = r
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || !ok {
		return resp, err
	}
	t.c.sess.absorb(resp.Cookies(), time.Now())
	if t.c.dpop != nil {
		t.c.dpop.observe(resp)
	}
	return resp, nil
}

// PersistSession sets where a desktop session's rotated cookies are saved
// (cmd/clack: WriteCreds to creds.json). save is called debounced, from its
// own goroutine, with the full credentials. No-op for pasted sessions.
func (c *Client) PersistSession(save func(Creds) error) {
	if c.sess == nil {
		return
	}
	c.sess.mu.Lock()
	c.sess.persist = save
	pending := c.sess.gen != c.sess.written
	if pending {
		c.sess.changedLocked(true)
	}
	c.sess.mu.Unlock()
}

// FlushSession saves a desktop session's pending cookie changes now
// (before exit). No-op for pasted sessions.
func (c *Client) FlushSession() {
	if c.sess != nil {
		c.sess.flush()
	}
}

// SessionCreds returns the credentials with the session's current cookies
// (a desktop session's rotate; a pasted one's are c.Creds as given).
func (c *Client) SessionCreds() Creds {
	if c.sess == nil {
		return c.Creds
	}
	return c.sess.creds()
}

// SessionCookie is the d cookie this client sends (URL-encoded).
func (c *Client) SessionCookie() string {
	if c.sess == nil {
		return c.Creds.Cookie
	}
	return c.sess.cookie()
}

// SetSessionCookie replaces the d cookie for this run (Taut's
// api.cookies.set). A desktop session saves it like a rotation.
func (c *Client) SetSessionCookie(v string) {
	if c.sess == nil {
		c.Creds.Cookie = v
		return
	}
	c.sess.setCookie(v)
}
