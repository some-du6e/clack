package core

import (
	"context"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// OlderReplies is the Threads view's "Show N more replies"
// (client-threads-view loadOlderReplies → requestReplies (0VEE) →
// fetchReplies (client-boot QJKZ $b), build 4d0c78085232):
// conversations.replies {channel, ts, inclusive: true, limit, latest},
// the latest being the oldest reply the view has. It returns those
// replies oldest first without the root, and whether the server has
// older ones (has_more). The thread cache is left alone: the view shows
// them in its card.
func (a *App) OlderReplies(ctx context.Context, ch, ts, latest string, limit int) ([]Message, bool, error) {
	return a.replies(ctx, url.Values{"channel": {ch}, "ts": {ts}, "inclusive": {"true"}, "limit": {strconv.Itoa(limit)},
		"include_all_metadata": {"true"}}, latest)
}

// RepliesBetween is requestReplies over a span of thread ts: the replies
// after oldest and before latest (conversations.replies {oldest, latest,
// inclusive: false}), oldest first, and whether there are more than limit
// of them (has_more). The Threads view fills a card's gap with it.
func (a *App) RepliesBetween(ctx context.Context, ch, ts, oldest, latest string, limit int) ([]Message, bool, error) {
	msgs, more, err := a.replies(ctx, url.Values{"channel": {ch}, "ts": {ts}, "oldest": {oldest}, "inclusive": {"false"},
		"limit": {strconv.Itoa(limit)}, "include_all_metadata": {"true"}}, latest)
	out := msgs[:0]
	for _, m := range msgs {
		if store.TSLess(oldest, m.TS) && store.TSLess(m.TS, latest) {
			out = append(out, m)
		}
	}
	return out, more, err
}

// replies is conversations.replies with p (latest added when set): the
// replies without the root, oldest first, and has_more.
func (a *App) replies(ctx context.Context, p url.Values, latest string) ([]Message, bool, error) {
	ch, ts := p.Get("channel"), p.Get("ts")
	if latest != "" {
		p.Set("latest", latest)
	}
	var r struct {
		Messages []slack.Message `json:"messages"`
		HasMore  bool            `json:"has_more"`
	}
	since := a.S.LiveSeq()
	if err := a.C.Call(ctx, "conversations.replies", p, &r); err != nil {
		return nil, false, err
	}
	out := r.Messages[:0]
	for _, m := range r.Messages {
		if m.TS != ts { // requestReplies drops the root when latest is set
			out = append(out, m)
		}
	}
	// Slack's requestReplies puts them in its messages store, where the
	// socket's reactions reach them: the store keeps them, merged with
	// what the socket changed while the request was out (store/fetched.go)
	out = a.S.PutFetched(ch, out, since)
	a.announceReads() // read_changes.go
	// the replies' people are asked for as rows draw them (lazy_users.go)
	return a.messages(ch, out), r.HasMore, nil
}
