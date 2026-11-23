package taut

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// flannelMethods is Slack's flannel table (client-boot EVxR x, build
// a09b625d90aa): the edge cache methods query sends over HTTP, every one a
// read. 05_slack.js checks a query against it first; the host function
// holds to it as well, since any script in the runtime can reach it.
var flannelMethods = map[string]bool{
	"users/info": true, "users/search": true, "users/list": true, "users/counts": true,
	"channels/info": true, "channels/search": true, "mpims/search": true, "channels/membership": true,
	"usergroups/info": true, "usergroups/search": true, "emojis/info": true, "emojis/search": true,
	"emojis/list": true, "huddles/info": true, "huddles/list": true, "objects/search": true,
	"permissions/info": true, "users/searchBulk": true, "usergroups/searchBulk": true,
	"externalTeams/search": true, "externalTeams/list": true, "externalTeams/counts": true,
	"apps/info": true,
}

// flannel sends one query of Slack's flannel client (05_slack.js query)
// as its FlannelRequest goes (client-boot Ln2M): the method's path on the
// edge cache, args as the JSON body with Clack's token (core's edge
// client: what Clack's own member lists send), tried as BaseRequest tries
// it (slack.SlackRetriesUpTo, BaseRequest's 3 attempts; a not-ok answer
// is final, slack.NoAnswerRetries, as FlannelRequest's _onNotOk rejects
// an internal_error at once). The answer is
// the last try whole (lastTry) and flannelUrl, where it went, for
// flannelSettle to settle the query from as FlannelRequest does. team is
// the query's teamId: the signed-in team or its enterprise, as Slack
// finds no flannel URL for a team it isn't signed in to.
func (t *Taut) flannel(ctx context.Context, key int64, team, method string, args json.RawMessage) (any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	k := flannelKey{t, key}
	if v, loaded := flannelCancels.LoadOrStore(k, cancel); loaded {
		flannelCancels.Delete(k)
		if _, early := v.(flannelCanceled); early {
			return nil, context.Canceled // aborted before it started
		}
	}
	defer flannelCancels.Delete(k)
	if !flannelMethods[method] {
		return nil, fmt.Errorf("taut: flannel: %q isn't one of Slack's flannel methods", method)
	}
	c := t.app.C
	if team == "" || team != c.Creds.Team && team != c.Creds.EnterpriseID {
		return nil, fmt.Errorf("taut: flannel: not signed in to team %q", team)
	}
	body := map[string]any{}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &body); err != nil {
			return nil, fmt.Errorf("taut: flannel: %s's args: %w", method, err)
		}
	}
	var last *slack.Attempt
	tries := 0
	ctx = slack.ObserveAttempts(slack.NoAnswerRetries(slack.SlackRetriesUpTo(ctx, slack.SlackMaxAttempts)), func(at slack.Attempt) {
		tries++
		last = &at
	})
	var out json.RawMessage
	err := c.Edge(ctx, method, body, &out)
	o, err := lastTry("flannel", last, tries, err)
	if err != nil {
		return nil, err
	}
	o["flannelUrl"] = c.EdgeBase()
	return o, nil
}

// flannelKey is a query's request, by its runtime's Taut and its key in
// the page's queue (05_slack.js apiQEnqueue).
type flannelKey struct {
	t   *Taut
	key int64
}

// flannelCancels are the running queries' cancels: the queue aborting one
// (its signal) stops the request and its retries, as ApiQ cancels its
// request.
var flannelCancels sync.Map // flannelKey → context.CancelFunc

// flannelCanceled marks a query aborted before its request started (the
// queue sent it, the abort came first): it starts and stops at once.
type flannelCanceled struct{}

// cancelFlannel stops the query key: the queue only cancels what it sent,
// so a key not running yet is about to, and the mark stops it.
func (t *Taut) cancelFlannel(key int64) {
	if v, loaded := flannelCancels.LoadOrStore(flannelKey{t, key}, flannelCanceled{}); loaded {
		if c, ok := v.(context.CancelFunc); ok {
			c()
		}
	}
}
