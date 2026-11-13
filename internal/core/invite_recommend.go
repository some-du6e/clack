package core

import (
	"context"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// The invite modal's "Suggested:" channels (gantry-v2-async-client-boot-
// shared GXKR, build fa938a086569): ChannelSelect's recommended channels
// row runs fetchRecommendedChannels on mount, which is
// conversations.recommend (boot-vendors E8xC conversationsRecommendFetcher)
// with {query: [""], source: "joiner-preadd-channels", channels_info: true,
// limit: 10}. Slack's API helper JSON-encodes object arguments (client-boot
// getFormData: `"object" == typeof i ? JSON.stringify(i)`), so query goes
// as [""]. The answer's channels' ids are stored (czvC YC); the row shows
// the first three not already picked.

// InviteRecommendSource is fetchRecommendedChannels' default source.
const InviteRecommendSource = "joiner-preadd-channels"

// RecommendedChannels is fetchRecommendedChannels: up to limit channels in
// Slack's order. A channel Clack already has comes from its store (Slack's
// getChannelById); the rest from the answer's channels_info.
func (a *App) RecommendedChannels(ctx context.Context, limit int) ([]Conv, error) {
	if limit <= 0 {
		limit = 10
	}
	p := url.Values{
		"query":         {`[""]`},
		"source":        {InviteRecommendSource},
		"channels_info": {"true"},
		"limit":         {strconv.Itoa(limit)},
	}
	var r struct {
		Channels []slack.Conversation `json:"channels"`
	}
	if err := a.C.Call(ctx, "conversations.recommend", p, &r); err != nil {
		return nil, err
	}
	out := make([]Conv, 0, len(r.Channels))
	for i := range r.Channels {
		c := &r.Channels[i]
		if c.ID == "" {
			continue
		}
		if known, ok := a.S.Conv(c.ID); ok {
			out = append(out, a.conv(known))
			continue
		}
		if c.Name == "" {
			c.Name = a.S.ChannelName(c.ID)
		}
		out = append(out, a.conv(c))
	}
	return out, nil
}
