package core

import (
	"bytes"
	"encoding/json"
	"maps"
	"math"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
	"github.com/some-du6e/clack/internal/slack"
)

// The unread-counts parity dimension (docs/PARITY.md): for each case of
// testdata/parity/unread-counts.json, what each conversation of a
// client.counts answer shows in the sidebar, against what Slack's own
// counts code makes of the same answer (tools/oracles/unread_counts.cjs
// runs applyCountsToChannels and refreshFilteredUnreadChannelCounts over
// Slack's CDN build; `tools/parity.py --refresh unread-counts`).
//
// Clack's side is the counts core installs (installCounts: the store's
// rules, a DM's unread as one mention among them) read the way the sidebar
// draws a row (ui sidebar.go: bold when Unread and not Muted, the badge its
// Mentions). Slack's is the row's badge (getBadgeCount) and whether its
// filtered counts hold it unread. The base is the composer and core tests'
// world (Hack Club's team in its grid, the viewer USELF).

type ucIn struct {
	Self        json.RawMessage            `json:"self,omitempty"`
	Team        json.RawMessage            `json:"team,omitempty"`
	Users       []json.RawMessage          `json:"users,omitempty"`
	Experiments map[string]string          `json:"experiments,omitempty"`
	Prefs       map[string]json.RawMessage `json:"prefs,omitempty"`
	Convs       []json.RawMessage          `json:"convs,omitempty"`
	Counts      json.RawMessage            `json:"counts,omitempty"`
	Muted       []string                   `json:"muted,omitempty"`
}

type ucRow struct {
	Badge  int  `json:"badge"`
	Unread bool `json:"unread"`
}

type ucOut struct {
	Rows map[string]ucRow `json:"rows"`
}

func (b ucIn) over(c ucIn) ucIn {
	r := b
	r.Users = append(append([]json.RawMessage(nil), b.Users...), c.Users...)
	r.Experiments = maps.Clone(b.Experiments)
	if r.Experiments == nil {
		r.Experiments = map[string]string{}
	}
	maps.Copy(r.Experiments, c.Experiments)
	r.Convs, r.Counts, r.Muted = c.Convs, c.Counts, c.Muted
	return r
}

func TestUnreadCountsLikeSlack(t *testing.T) {
	s := paritytest.Load[ucIn, ucOut](t, "testdata/parity/unread-counts.json")
	var base ucIn
	dec := json.NewDecoder(bytes.NewReader(s.Base))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&base); err != nil {
		t.Fatalf("the snapshot's base: %v", err)
	}
	s.Run(t, paritytest.Check[ucIn, ucOut]{Clack: func(t *testing.T, in ucIn) ucOut {
		return clackUnreadRows(t, base.over(in))
	}})
}

func clackUnreadRows(t *testing.T, c ucIn) ucOut {
	t.Helper()
	a, _ := testApp(t)
	var users []slack.User
	for _, raw := range c.Users {
		var u slack.User
		if err := json.Unmarshal(raw, &u); err != nil {
			t.Fatalf("users: %v", err)
		}
		users = append(users, u)
	}
	a.S.PutUsers(users)
	for _, raw := range c.Convs {
		var cv slack.Conversation
		if err := json.Unmarshal(raw, &cv); err != nil {
			t.Fatalf("convs: %v", err)
		}
		a.S.PutConv(cv)
	}
	a.S.Lock()
	if a.S.Muted == nil {
		a.S.Muted = map[string]bool{}
	}
	for _, id := range c.Muted {
		a.S.Muted[id] = true
	}
	a.S.Unlock()
	for k, v := range c.Experiments {
		a.SetExperimentGroup(k, v)
	}
	var counts slack.Counts
	if err := json.Unmarshal(c.Counts, &counts); err != nil {
		t.Fatalf("counts: %v", err)
	}
	a.installCounts(&counts, math.MaxUint64)
	out := ucOut{Rows: map[string]ucRow{}}
	for _, list := range [][]slack.CountEntry{counts.Channels, counts.MPIMs, counts.IMs} {
		for _, e := range list {
			n := a.Count(e.ID)
			out.Rows[e.ID] = ucRow{Badge: n.Mentions, Unread: n.Unread && !n.Muted}
		}
	}
	return out
}
