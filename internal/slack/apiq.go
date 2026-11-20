package slack

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"time"
)

// Slack's API queue's retries (web client build 2c0d02ca130f): client-boot
// P8AB BaseRequest, QqUK SlackApiRequest and 3GCR's queue, for the calls a
// caller marks with SlackRetries (the profile photo's). Every other call
// keeps Clack's own rules (doPost's 429 loop, do's resend of a read on a
// dead pooled connection, the backoff guard).
//
//   - A request times out after 60 s (BaseRequest timeout) and is tried at
//     most 3 times (maxAttempts; n8Nf gives every method not in its
//     no-retry list the default).
//   - It is tried again (shouldRetry) after an error that isn't an answer:
//     the connection failing (XHR error, CLIENT_UNKNOWN), a timeout
//     (CLIENT_TIMEOUT) or an abort (CLIENT_ABORTED); after an HTTP 429,
//     503 or 504; and after {"ok":false,"error":"internal_error"}
//     (SlackApiRequest). Anything else is final: another HTTP status
//     (500 and 502 included), any other error code, an answer that isn't
//     JSON (_parse_error). {"ok":false} with a rate-limit code becomes the
//     queue's dummy request and fails at once (3GCR).
//   - The wait before the next try (_getRetryAfterMs, then 3GCR's
//     setTimeout(r+1)) is the answer's Retry-After in seconds (parseInt; a
//     0 or a date counts as none), else _getRetryAfterWithBackoff's: 60 s
//     after a 429 or 503, 15 s after a 504, 1 s otherwise, plus
//     getBackoffJittered (kLjU) of the attempts so far, a whole number of
//     seconds from 0 to min(180-base, 5·2^attempts)-2; never more than 180 s
//     (maxRetryAfter).
//   - Chromium also sends any request again at once, on a fresh
//     connection, when the reused one it went out on was already dead
//     (HttpNetworkTransaction::ShouldResendRequest), so do does that for
//     these calls too.
//
// Clack's calls also run inside a context: a wait the context's deadline
// can't fit isn't started, and the last error is returned instead.

// SlackMaxAttempts is BaseRequest's maxAttempts.
const SlackMaxAttempts = 3

// RequestTimeout is a request's limit, Slack's BaseRequest timeout and
// Client.HTTP's.
const RequestTimeout = 60 * time.Second

// MaxRetryAfter is BaseRequest's maxRetryAfter, the longest wait between
// two tries.
const MaxRetryAfter = 180 * time.Second

// SlackRetrySpan is the longest a SlackRetries call can take: every try
// timing out and both waits at their longest.
const SlackRetrySpan = SlackMaxAttempts*RequestTimeout + (SlackMaxAttempts-1)*MaxRetryAfter

// RetryUnit is a second of the retry schedule (tests shorten it).
var RetryUnit = time.Second

type slackRetryKey struct{}

// SlackRetries marks ctx's calls to be tried the way Slack's web client's
// API queue tries them (above).
func SlackRetries(ctx context.Context) context.Context {
	return context.WithValue(ctx, slackRetryKey{}, true)
}

func slackRetrying(ctx context.Context) bool {
	v, _ := ctx.Value(slackRetryKey{}).(bool)
	return v
}

// transportError is a request that got no answer: the connection failed,
// timed out or was cut before the whole response was read (XHR's error,
// timeout and abort). Its text is the underlying error's.
type transportError struct{ err error }

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

// refusal reports whether err is Clack refusing to send or follow a
// request: trying again would be refused the same way.
func refusal(err error) bool {
	for _, r := range []error{ErrNotAFile, ErrUntrustedHost, ErrRedirectRefused, ErrBlockedAddr,
		ErrNotAllowed, ErrForbiddenMethod, ErrSignedOut, ErrTooLarge} {
		if errors.Is(err, r) {
			return true
		}
	}
	return false
}

// slackRetryWait is the wait before trying again after err, attempts tries
// in, and whether Slack's queue tries again at all.
func slackRetryWait(err error, attempts int) (time.Duration, bool) {
	return slackRetryWaitUpTo(err, attempts, SlackMaxAttempts)
}

// slackRetryWaitUpTo is slackRetryWait for a request tried at most
// maxAttempts times.
func slackRetryWaitUpTo(err error, attempts, maxAttempts int) (time.Duration, bool) {
	if attempts >= maxAttempts {
		return 0, false
	}
	base, server := 1, 0
	var se *Error
	var he *HTTPError
	var te *transportError
	switch {
	case errors.As(err, &se):
		if se.Code != "internal_error" {
			return 0, false
		}
	case errors.As(err, &he):
		switch he.Status {
		case 429, 503:
			base = 60
		case 504:
			base = 15
		default:
			return 0, false
		}
		server = he.RetryAfter
	case refusal(err):
		return 0, false
	case errors.As(err, &te):
	default:
		return 0, false
	}
	maxSecs := int(MaxRetryAfter / time.Second)
	secs := server
	if secs <= 0 {
		secs = base + retryJitter(attempts, maxSecs-base)
	}
	return time.Duration(min(secs, maxSecs)) * RetryUnit, true
}

// retryJitter is getBackoffJittered({attempts, cap}): randomIntBetween(0,
// min(cap, 5·2^attempts)), which is Math.floor(Math.random()*(n-1)).
var retryJitter = func(attempts, cap int) int {
	n := min(cap, 5<<min(attempts, 20))
	if n < 2 {
		return 0
	}
	return rand.IntN(n - 1)
}

// retrySlack tries a SlackRetries call again as Slack's queue does, once
// first returned err; again sends it once more.
func (c *Client) retrySlack(ctx context.Context, method string, err error, again func() error) error {
	for attempts := 1; err != nil; attempts++ {
		if ctx.Err() != nil || c.SignedOut() {
			return err
		}
		most := slackMaxAttempts(ctx)
		wait, ok := slackRetryWaitUpTo(err, attempts, most)
		if !ok {
			return err
		}
		if dl, has := ctx.Deadline(); has && time.Until(dl) <= wait {
			return err
		}
		log.Printf("slack: %s: %s on try %d of %d, trying again in %s", method, FailureDetail(err), attempts, most, wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		err = again()
	}
	return nil
}

// retryAfterSecs is parseInt(Retry-After, 10): its leading digits, 0 when
// it has none.
func retryAfterSecs(v string) int {
	n := 0
	for i := 0; i < len(v) && v[i] >= '0' && v[i] <= '9'; i++ {
		n = n*10 + int(v[i]-'0')
		if n > 1<<20 {
			return 1 << 20
		}
	}
	return n
}
