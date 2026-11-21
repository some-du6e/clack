package slack

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
)

// Attempt is one request a Web API call sent and what came of it, for a
// caller that hands Slack's own request objects on (Taut's apiCall settles
// its thunk the way Slack's SlackApiRequest does: it needs every try's
// HTTP status, the answer's X-Slack-Req-Id and Retry-After headers and the
// body). Body is the answer as it came (memory only, never logged); it is
// nil, with Err set, when no answer came.
type Attempt struct {
	// URL is where the request went (the team's or the enterprise's API
	// host: Call sends a method on to the other host after a
	// team_is_restricted or enterprise_is_restricted answer).
	URL        string
	Status     int
	ReqID      string // X-Slack-Req-Id
	RetryAfter string // Retry-After, as sent
	Body       []byte
	// Err is why no answer came: the connection failing or being cut
	// before the whole answer was read. Timeout reports that it ran past
	// RequestTimeout (XHR's timeout, not its error).
	Err     error
	Timeout bool
}

type attemptsKey struct{}

// ObserveAttempts makes ctx's calls hand f each request they send, once it
// is answered or has failed (on the calling goroutine). A call the backoff
// guard holds back sends nothing and reports nothing.
func ObserveAttempts(ctx context.Context, f func(Attempt)) context.Context {
	return context.WithValue(ctx, attemptsKey{}, f)
}

func observing(ctx context.Context) func(Attempt) {
	f, _ := ctx.Value(attemptsKey{}).(func(Attempt))
	return f
}

// answered reports an answered request to ctx's observer.
func answered(ctx context.Context, u string, resp *http.Response, body []byte) {
	if f := observing(ctx); f != nil {
		f(Attempt{URL: u, Status: resp.StatusCode, ReqID: resp.Header.Get("X-Slack-Req-Id"), RetryAfter: resp.Header.Get("Retry-After"), Body: body})
	}
}

// observedBody closes an answer the call doesn't read (a 429), reading it
// first when ctx's observer wants it.
func observedBody(ctx context.Context, resp *http.Response) []byte {
	defer resp.Body.Close()
	if observing(ctx) == nil {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return b
}

// kept is an answer an error keeps: only for a call whose observer wants
// answers (the backoff guard holds a failed call's error for minutes).
func kept(ctx context.Context, body []byte) []byte {
	if observing(ctx) == nil {
		return nil
	}
	return body
}

// unanswered reports a request that got no answer to ctx's observer.
func unanswered(ctx context.Context, u string, err error) {
	if f := observing(ctx); f != nil {
		var ne net.Error
		f(Attempt{URL: u, Err: err, Timeout: errors.As(err, &ne) && ne.Timeout()})
	}
}

// AttemptOf is the try err came from, for a call that sent nothing because
// the backoff guard answered it with the failure it holds (an observer
// heard no attempt): an {"ok":false} answer, an HTTP error status, an
// answer that isn't JSON, or a request no answer came to. ok is false for
// anything else (a request Clack refused to send, a cancelled context).
func AttemptOf(err error) (at Attempt, ok bool) {
	var se *Error
	var he *HTTPError
	var pe *ParseError
	var te *transportError
	switch {
	case errors.As(err, &se):
		at.Status, at.Body = se.Status, se.Body
		if at.Status == 0 {
			at.Status = http.StatusOK
		}
	case errors.As(err, &he):
		at.Status, at.Body = he.Status, he.Body
		if he.RetryAfter > 0 {
			at.RetryAfter = strconv.Itoa(he.RetryAfter)
		}
	case errors.As(err, &pe):
		at.Status, at.Body = http.StatusOK, pe.Body
	case errors.As(err, &te):
		var ne net.Error
		at.Err, at.Timeout = err, errors.As(err, &ne) && ne.Timeout()
	default:
		return Attempt{}, false
	}
	return at, true
}

type maxAttemptsKey struct{}

// SlackRetriesUpTo is SlackRetries with BaseRequest's maxAttempts set to
// n: Slack's apiCall passes 1 for the methods it never retries
// (shouldRetryMethod) and its caller's maxAttempts otherwise (3 when it
// gives none). n is held to 1…SlackMaxAttemptsCap.
func SlackRetriesUpTo(ctx context.Context, n int) context.Context {
	return context.WithValue(SlackRetries(ctx), maxAttemptsKey{}, min(max(n, 1), SlackMaxAttemptsCap))
}

// SlackMaxAttemptsCap bounds a caller's maxAttempts: Slack's queue tries
// as often as it is told, and a plugin's call shouldn't hammer Slack.
const SlackMaxAttemptsCap = 10

func slackMaxAttempts(ctx context.Context) int {
	if n, ok := ctx.Value(maxAttemptsKey{}).(int); ok {
		return n
	}
	return SlackMaxAttempts
}

// ParseError is a 200 answer that isn't JSON (a Slack page, a body cut
// short). Body is the answer (memory only) when the call's observer asked
// for answers. Its text is the one the client has always given it.
type ParseError struct {
	Method string
	Body   []byte
	Err    error // the decoder's
}

func (e *ParseError) Error() string { return "slack " + e.Method + ": bad json: " + e.Err.Error() }
func (e *ParseError) Unwrap() error { return e.Err }
