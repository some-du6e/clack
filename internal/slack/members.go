package slack

import (
	"context"
	"sync"
	"time"
)

// MembersInfo is Slack's fetchMembersById (client-boot iFK2, build
// 0e695dd18302fa08): edge users/info (flannel MEMBER_INFO) in chunks of
// MembersPerQuery ids, MembersInFlight chunks at a time
// (fetchObjectsById's mapping, concurrency 2), each asking with
// check_interaction, include_profile_only_users and updated_ids (every id
// at 0: the members asked for are ones the store doesn't hold). Ids an
// answer lists in pending_ids are asked again, up to three more rounds,
// 500 ms × the round apart; ids still pending after that count as
// missing.
func (c *Client) MembersInfo(ctx context.Context, ids []string) MembersResult {
	var out MembersResult
	ask := ids
	for round := 1; len(ask) > 0; round++ {
		r := c.membersRound(ctx, ask)
		out.Users = append(out.Users, r.Users...)
		out.Missing = append(out.Missing, r.Missing...)
		out.Failed = append(out.Failed, r.Failed...)
		if out.Err == nil {
			out.Err = r.Err
		}
		ask = r.pending
		if len(ask) == 0 {
			break
		}
		if round > 3 {
			out.Missing = append(out.Missing, ask...)
			break
		}
		select {
		case <-ctx.Done():
			out.Failed = append(out.Failed, ask...)
			if out.Err == nil {
				out.Err = ctx.Err()
			}
			return out
		case <-time.After(time.Duration(round) * 500 * time.Millisecond):
		}
	}
	return out
}

// Slack's flannel limits for users/info (iFK2 MAX_IDS_PER_QUERY, and
// fetchObjectsById's concurrency).
const (
	MembersPerQuery = 100
	MembersInFlight = 2
)

// MembersResult is what MembersInfo learned: the members users/info
// returned; the ids it answered without (failed_ids, invalid_ids, still
// pending, or simply not among the results); and the ids whose request
// failed (no answer: the network, a timeout, an error reply), with the
// first such error.
type MembersResult struct {
	Users   []User
	Missing []string
	Failed  []string
	Err     error

	pending []string // pending_ids of this round
}

// membersRound asks for ids once, chunk by chunk.
func (c *Client) membersRound(ctx context.Context, ids []string) MembersResult {
	var (
		mu  sync.Mutex
		out MembersResult
		wg  sync.WaitGroup
	)
	sem := make(chan struct{}, MembersInFlight)
	for i := 0; i < len(ids); i += MembersPerQuery {
		part := ids[i:min(len(ids), i+MembersPerQuery)]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := c.membersChunk(ctx, part)
			mu.Lock()
			out.Users = append(out.Users, r.Users...)
			out.Missing = append(out.Missing, r.Missing...)
			out.Failed = append(out.Failed, r.Failed...)
			out.pending = append(out.pending, r.pending...)
			if out.Err == nil {
				out.Err = r.Err
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// membersChunk is one users/info request (fetchObjectsWithQuery, which
// follows next_marker).
func (c *Client) membersChunk(ctx context.Context, ids []string) MembersResult {
	up := make(map[string]int, len(ids))
	for _, id := range ids {
		up[id] = 0
	}
	body := map[string]any{"check_interaction": true, "include_profile_only_users": true, "updated_ids": up}
	var out MembersResult
	got := map[string]bool{}
	held := map[string]bool{} // listed in failed/invalid/pending: not "simply absent"
	for {
		var r struct {
			Results  []User   `json:"results"`
			Failed   []string `json:"failed_ids"`
			Pending  []string `json:"pending_ids"`
			Invalid  []string `json:"invalid_ids"`
			NextMark string   `json:"next_marker"`
		}
		if err := c.Edge(ctx, "users/info", body, &r); err != nil {
			// a chunk that fails is all failed (fetchObjectsById's catch)
			for _, id := range ids {
				if !got[id] {
					out.Failed = append(out.Failed, id)
				}
			}
			out.Err = err
			return out
		}
		for _, u := range r.Results {
			if u.ID != "" && !got[u.ID] {
				got[u.ID] = true
				out.Users = append(out.Users, u)
			}
		}
		for _, id := range r.Pending {
			if !got[id] && !held[id] {
				held[id] = true
				out.pending = append(out.pending, id)
			}
		}
		for _, list := range [][]string{r.Failed, r.Invalid} {
			for _, id := range list {
				if !got[id] && !held[id] {
					held[id] = true
					out.Missing = append(out.Missing, id)
				}
			}
		}
		if r.NextMark == "" {
			break
		}
		body["marker"] = r.NextMark
	}
	for _, id := range ids {
		if !got[id] && !held[id] {
			out.Missing = append(out.Missing, id)
		}
	}
	return out
}
