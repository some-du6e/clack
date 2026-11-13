package core

import (
	"context"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
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
	p := url.Values{"channel": {ch}, "ts": {ts}, "inclusive": {"true"}, "limit": {strconv.Itoa(limit)},
		"include_all_metadata": {"true"}}
	if latest != "" {
		p.Set("latest", latest)
	}
	var r struct {
		Messages []slack.Message `json:"messages"`
		HasMore  bool            `json:"has_more"`
	}
	if err := a.C.Call(ctx, "conversations.replies", p, &r); err != nil {
		return nil, false, err
	}
	out := r.Messages[:0]
	for _, m := range r.Messages {
		if m.TS != ts { // requestReplies drops the root when latest is set
			out = append(out, m)
		}
	}
	// the replies' people are asked for as rows draw them (lazy_users.go)
	return a.messages(ch, out), r.HasMore, nil
}
