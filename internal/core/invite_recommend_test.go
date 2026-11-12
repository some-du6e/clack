package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// fetchRecommendedChannels (GXKR) as Slack's fetcher sends it:
// conversations.recommend with query [""] (getFormData JSON-encodes
// objects), source joiner-preadd-channels, channels_info and limit 10.
func TestInviteRecommendedChannels(t *testing.T) {
	a, fs := inviteApp(t, slack.User{}, `{}`)
	ctx := context.Background()
	cs, err := a.RecommendedChannels(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) == 0 || len(cs) > 10 {
		t.Fatalf("%d channels", len(cs))
	}
	for _, c := range cs {
		if c.ID == "" || c.Name == "" || c.Kind != "channel" || c.IsArchived {
			t.Errorf("channel %+v", c)
		}
	}
	calls := fs.InviteCalls()
	if len(calls) != 1 || calls[0].Method != "conversations.recommend" {
		t.Fatalf("calls %+v", calls)
	}
	f := calls[0].Form
	if f.Get("query") != `[""]` || f.Get("source") != InviteRecommendSource || f.Get("channels_info") != "true" || f.Get("limit") != "10" {
		t.Errorf("form %v", f)
	}
	// the fake's answer, in its order; a channel the fake doesn't have is
	// left out by it
	fs.SetRecommendedChannels([]string{cs[len(cs)-1].ID, cs[0].ID, "C0NOTHERE1"})
	got, err := a.RecommendedChannels(ctx, 10)
	if err != nil || len(got) != 2 || got[0].ID != cs[len(cs)-1].ID || got[1].ID != cs[0].ID {
		t.Errorf("set list: %+v %v", got, err)
	}
	fs.SetRecommendedChannels([]string{})
	if got, err := a.RecommendedChannels(ctx, 10); err != nil || len(got) != 0 {
		t.Errorf("none: %+v %v", got, err)
	}
}
