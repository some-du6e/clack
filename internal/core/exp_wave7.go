package core

// The seventh wave of the account's experiments (branch exp-wave-7, CDN
// build c07ab895506f, 2026-11-15; docs/EXPERIMENTS.md "wave 7"): the
// search results' density card.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// ExpPersistentTSDogfood is persistent_ts_dogfood: client-boot WkcD
// isTsDogfoodUser, which lets `+zqw` Ui read the employees' density
// switcher pref (search-view UK4f SearchResultsCardDensitySwitcher).
const ExpPersistentTSDogfood = "persistent_ts_dogfood"

// wave7Reads join experimentReads (TestExperimentReadsListed);
// persistent_ts_dogfood isn't assigned, so it has no default.
var wave7Reads = map[string][]string{
	ExpPersistentTSDogfood: on,
}

func init() {
	for name, groups := range wave7Reads {
		experimentReads[name] = groups
	}
}

// SearchCardExplore is FvhF Ui (was `+zqw`): unless the viewer is a
// dogfood user who picked the "original" density
// (search_results_card_density), the results draw the density card
// (popups/search_card.go). It used to need srp_card_explore on too; Slack
// dropped that experiment and kept its on path (build 8f19be21ed7e, the
// 2026-11-21 drift).
func (a *App) SearchCardExplore() bool {
	return a.Experiment(ExpPersistentTSDogfood) != "on" || a.PrefString("search_results_card_density") != "original"
}

// searchHitBlocks finds, in a search.modules answer still holding its
// highlight markers, the first block of each message with one
// (blockHasHighlight: a string with U+E000 anywhere in the block),
// keyed by item and message index. Only messages of more than three
// blocks are looked at: compactCardBlocks keeps shorter ones whole.
func searchHitBlocks(raw []byte) map[[2]int]int {
	if !hasHighlightStart(raw) {
		return nil
	}
	var res struct {
		Items []struct {
			Messages []struct {
				Blocks []json.RawMessage `json:"blocks"`
			} `json:"messages"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	out := map[[2]int]int{}
	for i, it := range res.Items {
		for j, m := range it.Messages {
			if len(m.Blocks) <= 3 {
				continue
			}
			for k, b := range m.Blocks {
				if hasHighlightStart(b) {
					out[[2]int{i, j}] = k
					break
				}
			}
		}
	}
	return out
}

// hasHighlightStart reports a U+E000, raw or JSON-escaped (search.go).
func hasHighlightStart(b []byte) bool { return hasHighlightMarker(b, true) }

// SearchViewerReply is SearchRepliesAction's useFetchLatestReplyTimestamp
// (boot-shared Wzwx) for a result whose requestor_replied is set: the
// viewer's latest reply in the thread. conversations.replies {channel,
// ts: the thread, limit, latest, reason
// "search-engagement-badge-reply-preview"} with limit 100, then 400 with
// latest the oldest reply of the page before, until a page has one of
// the viewer's replies; no more pages (has_more false) or no replies
// ends it with nothing. The last of the page's replies by the viewer is
// the one.
func (a *App) SearchViewerReply(ctx context.Context, ch, threadTS string) (Message, bool, error) {
	self := a.C.Creds.UserID
	latest := ""
	for _, limit := range []int{100, 400} {
		p := url.Values{"channel": {ch}, "ts": {threadTS}, "limit": {strconv.Itoa(limit)},
			"reason": {"search-engagement-badge-reply-preview"}}
		if latest != "" {
			p.Set("latest", latest)
		}
		var r struct {
			Messages []slack.Message `json:"messages"`
			HasMore  bool            `json:"has_more"`
		}
		if err := a.C.Call(ctx, "conversations.replies", p, &r); err != nil {
			return Message{}, false, err
		}
		if len(r.Messages) == 0 {
			return Message{}, false, nil
		}
		var replies []slack.Message
		mine := -1
		for _, m := range r.Messages {
			if m.TS == threadTS {
				continue
			}
			replies = append(replies, m)
			if m.User == self {
				mine = len(replies) - 1
			}
		}
		if mine >= 0 {
			return a.message(ch, &replies[mine]), true, nil
		}
		if !r.HasMore || len(replies) == 0 {
			return Message{}, false, nil
		}
		latest = replies[0].TS
	}
	return Message{}, false, nil
}
