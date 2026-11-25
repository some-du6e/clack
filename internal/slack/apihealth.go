package slack

import (
	"context"
	"errors"
	"net"
	"sync"
)

// Slack's API queue counts its requests' outcomes (client-boot 7quX ApiQ
// _countError and _countSuccess, build 889ac449a32d) and pauses once ten in
// a row went wrong the same way (_passedErrorThreshold, errorThreshold 10):
//
//   - an answer of 500 or more adds a service error and resets the client
//     errors (`_consecutiveServiceErrors = e >= 500 ? +1 : 0`);
//   - no HTTP answer at all (CLIENT_UNKNOWN, status 0) adds a client error
//     and resets the service errors;
//   - a timeout (CLIENT_TIMEOUT), a cancelled or aborted request, a 429 or
//     any other status reset both, and so does a success;
//   - an error with no status (`void 0 !== i && this._countError(i)`), a
//     request that never went out, counts nothing.
//
// Every try counts, the queue's own retries included (a retry is enqueued
// again and ends in the same catch). Chromium's resend of a request whose
// pooled connection was dead (Client.do) is below the queue, part of the
// same try, and a request the backoff guard holds back never reaches it.
//
// Ten service errors make the client service-offline (setServiceOffline,
// then awaitServiceReconnection: api.test); ten client errors, or the
// client already offline, make it client-offline (setClientOffline, then
// awaitNetworkReconnection: the CDN's test pixels). Core owns both waits
// (core/connectivity.go) and resets the counts when one ends (_unpause).

// APIErrorThreshold is ApiQ's errorThreshold.
const APIErrorThreshold = 10

// apiHealth is the queue's two counts and the hooks core sets.
type apiHealth struct {
	mu        sync.Mutex
	client    int
	service   int
	onClient  func() // ten client errors in a row (and every one after)
	onService func() // ten service errors in a row (and every one after)
}

// SetAPIHealthHooks sets what runs when APIErrorThreshold client errors
// (no answer) or service errors (an answer of 500 or more) came in a row,
// and again at every one after until ResetAPIErrors. The hooks run on the
// failed call's goroutine and must not block.
func (c *Client) SetAPIHealthHooks(clientErrors, serviceErrors func()) {
	c.health.mu.Lock()
	c.health.onClient, c.health.onService = clientErrors, serviceErrors
	c.health.mu.Unlock()
}

// ResetAPIErrors is _unpause's _resetErrorCounts: a wait for the network
// or the service ended.
func (c *Client) ResetAPIErrors() {
	c.health.mu.Lock()
	c.health.client, c.health.service = 0, 0
	c.health.mu.Unlock()
}

// APIErrors reports the counts (client errors, service errors).
func (c *Client) APIErrors() (client, service int) {
	c.health.mu.Lock()
	defer c.health.mu.Unlock()
	return c.health.client, c.health.service
}

// countAnswer is _countSuccess or _countError of an HTTP answer's status.
func (c *Client) countAnswer(status int) {
	h := &c.health
	h.mu.Lock()
	h.client = 0
	if status >= 500 {
		h.service++
	} else {
		h.service = 0
	}
	f := h.onService
	due := h.service >= APIErrorThreshold
	h.mu.Unlock()
	if due && f != nil {
		f()
	}
}

// countNoAnswer is _countError of a try that got no HTTP answer: a
// timeout or a cancel is a status of its own (TIMEOUT, CANCELLED) and
// resets both counts; anything else is CLIENT_UNKNOWN.
func (c *Client) countNoAnswer(ctx context.Context, err error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		c.ResetAPIErrors()
		return
	}
	h := &c.health
	h.mu.Lock()
	h.service = 0
	h.client++
	f := h.onClient
	due := h.client >= APIErrorThreshold
	h.mu.Unlock()
	if due && f != nil {
		f()
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
