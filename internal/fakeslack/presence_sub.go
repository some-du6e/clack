package fakeslack

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Presence, as Slack's gateway keeps it per socket. A presence_sub frame
// replaces the socket's subscription with its ids, and the gateway
// answers each id it adds with that member's presence at once, in
// presence_change events that list the members of one presence
// ({"type":"presence_change","presence":"active","users":[…]}, as a
// batch_presence_aware client, which Clack is, gets them); a change later
// reaches every socket subscribed to the member. The fake knows the
// presence of the members a test gave one (SetPresence) and answers for
// those only, so a test that sets none sees the gateway it always has.

type presenceState struct {
	mu   sync.Mutex
	of   map[string]string                   // member → "active" | "away"
	subs map[*websocket.Conn]map[string]bool // socket → the members it subscribes to
}

var presenceStates = newPerServer() // *Server -> *presenceState

func (s *Server) presenceStore() *presenceState {
	v, _ := presenceStates.LoadOrStore(s, &presenceState{of: map[string]string{}, subs: map[*websocket.Conn]map[string]bool{}})
	return v.(*presenceState)
}

// SetPresence gives member a presence ("active" or "away"); when it
// changed, every socket subscribed to member gets presence_change.
func (s *Server) SetPresence(member, presence string) {
	p := s.presenceStore()
	p.mu.Lock()
	was := p.of[member]
	p.of[member] = presence
	var to []*websocket.Conn
	if was != presence {
		for c, ids := range p.subs {
			if ids[member] {
				to = append(to, c)
			}
		}
	}
	p.mu.Unlock()
	for _, c := range to {
		if s.liveSocket(c) {
			writeEvent(c, presenceChange(presence, []string{member}))
		}
	}
}

// presenceSub answers the presence_sub frame c sent with ids: the
// presence of every member it adds to c's subscription, one
// presence_change per presence, members in id order.
func (s *Server) presenceSub(c *websocket.Conn, ids []string) {
	p := s.presenceStore()
	p.mu.Lock()
	old := p.subs[c]
	now := make(map[string]bool, len(ids))
	byPresence := map[string][]string{}
	for _, id := range ids {
		if now[id] {
			continue
		}
		now[id] = true
		if pr := p.of[id]; pr != "" && !old[id] {
			byPresence[pr] = append(byPresence[pr], id)
		}
	}
	p.subs[c] = now
	p.mu.Unlock()
	if !s.liveSocket(c) {
		return
	}
	for _, pr := range []string{"active", "away"} {
		if us := byPresence[pr]; len(us) > 0 {
			sort.Strings(us)
			writeEvent(c, presenceChange(pr, us))
		}
	}
}

// dropPresenceSub forgets a closed socket's subscription.
func (s *Server) dropPresenceSub(c *websocket.Conn) {
	p := s.presenceStore()
	p.mu.Lock()
	delete(p.subs, c)
	p.mu.Unlock()
}

func presenceChange(presence string, users []string) map[string]any {
	return map[string]any{"type": "presence_change", "presence": presence, "users": users}
}

func writeEvent(c *websocket.Conn, ev any) {
	b, _ := json.Marshal(ev)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, b)
}
