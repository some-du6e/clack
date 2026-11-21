package fakeslack

import (
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Rosters give a channel an exact member list for tests of the member
// counts and the Members tab:
//
//   - SetRoster(ch, ids…) sets ch's members. edge users/counts for ch
//     then counts them (people: the non-bots, apps: the bots), and edge
//     users/list for ch lists them (its people filter the non-bots,
//     "apps" the bots).
//   - JoinChannel and LeaveChannel change the list and push
//     member_joined_channel / member_left_channel to every socket, as
//     Slack tells every member's client when anyone joins or leaves.
//   - CountsAsked is how many users/counts calls asked for ch.

type rosterState struct {
	mu     sync.Mutex
	ids    map[string][]string
	counts map[string]int
}

var rosterStates sync.Map // *Server -> *rosterState

func (s *Server) roster() *rosterState {
	v, _ := rosterStates.LoadOrStore(s, &rosterState{ids: map[string][]string{}, counts: map[string]int{}})
	return v.(*rosterState)
}

// SetRoster makes ids ch's members.
func (s *Server) SetRoster(ch string, ids ...string) {
	r := s.roster()
	r.mu.Lock()
	r.ids[ch] = append([]string(nil), ids...)
	r.mu.Unlock()
}

// Roster is ch's members, ok false for a channel without one.
func (s *Server) Roster(ch string) (ids []string, ok bool) {
	r := s.roster()
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, ok = r.ids[ch]
	return append([]string(nil), ids...), ok
}

// JoinChannel adds user to ch's roster and pushes member_joined_channel.
func (s *Server) JoinChannel(ch, user string) {
	r := s.roster()
	r.mu.Lock()
	if !slices.Contains(r.ids[ch], user) {
		r.ids[ch] = append(r.ids[ch], user)
	}
	r.mu.Unlock()
	s.pushMembership("member_joined_channel", ch, user)
}

// LeaveChannel takes user off ch's roster and pushes member_left_channel.
func (s *Server) LeaveChannel(ch, user string) {
	r := s.roster()
	r.mu.Lock()
	r.ids[ch] = slices.DeleteFunc(r.ids[ch], func(id string) bool { return id == user })
	r.mu.Unlock()
	s.pushMembership("member_left_channel", ch, user)
}

// CountsAsked is how many edge users/counts calls named ch.
func (s *Server) CountsAsked(ch string) int {
	r := s.roster()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[ch]
}

func (s *Server) pushMembership(typ, ch, user string) {
	ts := strconv.FormatFloat(float64(time.Now().UnixMicro())/1e6, 'f', 6, 64)
	s.Push(map[string]any{"type": typ, "user": user, "channel": ch, "channel_type": "C", "team": TeamID, "event_ts": ts})
}

// rosterUsers is ch's members as user objects, ok false without a roster.
func (s *Server) rosterUsers(ch string) ([]slack.User, bool) {
	ids, has := s.Roster(ch)
	if !has {
		return nil, false
	}
	var out []slack.User
	for _, id := range ids {
		if u, found := s.userByID(id); found {
			out = append(out, u)
		}
	}
	return out, true
}

// rosterCounts answers edge users/counts for a channel with a roster.
func (s *Server) rosterCounts(edge map[string]json.RawMessage) (any, bool) {
	var ch string
	_ = json.Unmarshal(edge["channel"], &ch)
	r := s.roster()
	r.mu.Lock()
	r.counts[ch]++
	r.mu.Unlock()
	us, has := s.rosterUsers(ch)
	if !has {
		return nil, false
	}
	people, apps := 0, 0
	for _, u := range us {
		if u.IsBot {
			apps++
		} else {
			people++
		}
	}
	return ok("counts", map[string]int{"everyone": people + apps, "people": people, "members": people,
		"guests": 0, "bots": apps, "apps": apps}), true
}

// rosterList answers edge users/list for a channel with a roster.
func (s *Server) rosterList(edge map[string]json.RawMessage, filter string) (any, bool) {
	var chs []string
	_ = json.Unmarshal(edge["channels"], &chs)
	if len(chs) != 1 {
		return nil, false
	}
	us, has := s.rosterUsers(chs[0])
	if !has {
		return nil, false
	}
	out := []slack.User{}
	for _, u := range us {
		switch filter {
		case "apps":
			if u.IsBot {
				out = append(out, u)
			}
		case "everyone AND NOT bots AND NOT apps":
			if !u.IsBot {
				out = append(out, u)
			}
		default:
			out = append(out, u)
		}
	}
	return ok("results", out, "next_marker", ""), true
}
