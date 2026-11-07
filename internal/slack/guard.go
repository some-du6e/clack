package slack

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Traffic guards: once Slack says the session is gone, stop talking to it;
// and never let a retry loop hammer the same failing request.

// authCodes are the answers that mean the session ended.
var authCodes = map[string]bool{
	"invalid_auth": true, "not_authed": true, "token_revoked": true,
	"account_inactive": true, "token_expired": true,
}

// IsAuthError reports whether err means the session ended.
func IsAuthError(err error) bool {
	if errors.Is(err, ErrSignedOut) {
		return true
	}
	var se *Error
	return errors.As(err, &se) && authCodes[se.Code]
}

// ErrSignedOut is returned without network traffic after the session ended.
var ErrSignedOut = errors.New("slack: signed out (session ended)")

type guard struct {
	signedOut atomic.Bool
	onceAuth  sync.Once
	onAuth    func(code string)

	mu    sync.Mutex
	fails map[[32]byte]*failState
}

type failState struct {
	n     int
	until time.Time
	err   error
	last  time.Time
}

// Backoff bounds for repeated identical failures.
var (
	BackoffBase = time.Second
	BackoffMax  = 60 * time.Second
)

type noBackoffKey struct{}

// NoBackoff marks ctx as an explicit user retry: the request skips the
// backoff window (the signed-out state still applies).
func NoBackoff(ctx context.Context) context.Context {
	return context.WithValue(ctx, noBackoffKey{}, true)
}

func skipBackoff(ctx context.Context) bool {
	v, _ := ctx.Value(noBackoffKey{}).(bool)
	return v
}

// SignedOut reports whether the session ended (all calls now fail fast).
func (c *Client) SignedOut() bool { return c.g.signedOut.Load() }

// OnAuthFailed registers f, called once (on its own goroutine) when the
// session ends. Set it before making calls.
func (c *Client) OnAuthFailed(f func(code string)) { c.g.onAuth = f }

// markAuth trips the signed-out state once.
func (c *Client) markAuth(code string) {
	c.g.signedOut.Store(true)
	c.g.onceAuth.Do(func() {
		if f := c.g.onAuth; f != nil {
			go f(code)
		}
	})
}

func reqKey(method, u string, body []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(u))
	h.Write([]byte{0})
	h.Write(body)
	var k [32]byte
	copy(k[:], h.Sum(nil))
	return k
}

// blocked returns the stored error while a request is backing off.
func (c *Client) blocked(k [32]byte, now time.Time) error {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if f, ok := c.g.fails[k]; ok && now.Before(f.until) {
		return f.err
	}
	return nil
}

// record updates backoff state after a request finished.
func (c *Client) record(k [32]byte, err error, now time.Time) {
	c.g.mu.Lock()
	defer c.g.mu.Unlock()
	if err == nil {
		delete(c.g.fails, k)
		return
	}
	if c.g.fails == nil {
		c.g.fails = map[[32]byte]*failState{}
	}
	f := c.g.fails[k]
	if f == nil {
		f = &failState{}
		c.g.fails[k] = f
	}
	f.n++
	d := BackoffBase << (f.n - 1)
	if f.n > 16 || d > BackoffMax || d <= 0 {
		d = BackoffMax
	}
	f.until, f.err, f.last = now.Add(d), err, now
	if len(c.g.fails) > 2048 { // drop stale entries
		for key, s := range c.g.fails {
			if now.Sub(s.last) > 10*BackoffMax {
				delete(c.g.fails, key)
			}
		}
	}
}
