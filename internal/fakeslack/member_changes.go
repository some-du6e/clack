package fakeslack

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Members changing their profile (a new picture, name or status) as
// Slack's servers carry it, and the realtime gateway's user_change
// subscriptions (core/member_fresh.go is the client's side):
//
//   - EditMember gives a member a new copy with a newer updated stamp;
//     users.info and edge users/info answer with it from then on.
//   - edge users/info is conditional: an id asked with a stamp the
//     member's isn't newer than comes back with no result (Hack Club,
//     read 2026-11-22; 0 asks for the member as such).
//   - subscription_update frames (user_change_subscribe_request,
//     user_change_unsubscribe_request) are answered {"ok":true,
//     "reply_to":id}, and refused with error code 12 once a subscription
//     would pass the cap SetUserSubCap set (0: none) or a request names
//     more than 100 ids ("Too many updated ids"). A new connection starts
//     with none.
//   - PushUserChange sends user_change for a member to the clients that
//     subscribed to them, as flannel does on Grid.

type memberChanges struct {
	mu      sync.Mutex
	edited  map[string]slack.User
	subs    map[string]bool
	cap     int
	frames  []string // subscription_update subtypes with their ids, oldest first
	clock   int64
	refused int
	hold    chan struct{} // HoldHello's: the next connections' hello waits for it
	early   int           // subscription frames read before their connection's hello
}

var memberChangeStores = newPerServer() // *Server → *memberChanges

func (s *Server) memberChanges() *memberChanges {
	if v, ok := memberChangeStores.Load(s); ok {
		return v.(*memberChanges)
	}
	v, _ := memberChangeStores.LoadOrStore(s, &memberChanges{edited: map[string]slack.User{}, subs: map[string]bool{}})
	return v.(*memberChanges)
}

// EditMember changes member id's copy with edit (its profile, names,
// status) and stamps it updated now (later than any stamp before); the
// answers from then on carry it. It reports the member as edited, false
// for one the fake doesn't have.
func (s *Server) EditMember(id string, edit func(u *slack.User)) (slack.User, bool) {
	u, ok := s.userByID(id)
	if !ok {
		return slack.User{}, false
	}
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	edit(&u)
	now := time.Now().Unix()
	if now <= mc.clock {
		now = mc.clock + 1
	}
	if now <= u.Updated {
		now = u.Updated + 1
	}
	mc.clock, u.Updated = now, now
	mc.edited[id] = u
	return u, true
}

// editedUser is id's edited copy, if EditMember changed it.
func (s *Server) editedUser(id string) (slack.User, bool) {
	v, ok := memberChangeStores.Load(s)
	if !ok {
		return slack.User{}, false
	}
	mc := v.(*memberChanges)
	mc.mu.Lock()
	defer mc.mu.Unlock()
	u, ok := mc.edited[id]
	return u, ok
}

// PushUserChange sends user_change with member id's copy to the clients
// subscribed to them (false: nobody is, nothing went).
func (s *Server) PushUserChange(id string) bool {
	if !s.UserSubscribed(id) {
		return false
	}
	u, ok := s.userByID(id)
	if !ok {
		return false
	}
	s.emit(map[string]any{"type": "user_change", "user": u, "event_ts": fmt.Sprintf("%d.000100", time.Now().Unix())})
	return true
}

// UserSubscribed reports whether a client subscribed to member id's
// user_change.
func (s *Server) UserSubscribed(id string) bool {
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.subs[id]
}

// UserSubscriptions is the subscription_update requests clients sent,
// oldest first: "subscribe:U1,U2" / "unsubscribe:U3".
func (s *Server) UserSubscriptions() []string {
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return append([]string(nil), mc.frames...)
}

// SetUserSubCap sets how many members the gateway lets a client
// subscribe to (0: no cap); past it a request is refused with code 12.
func (s *Server) SetUserSubCap(n int) {
	mc := s.memberChanges()
	mc.mu.Lock()
	mc.cap = n
	mc.mu.Unlock()
}

// newGatewayConnection forgets the subscriptions: they were the last
// connection's.
func (s *Server) newGatewayConnection() {
	mc := s.memberChanges()
	mc.mu.Lock()
	mc.subs = map[string]bool{}
	mc.mu.Unlock()
}

// HoldHello holds the hello of the connections opened from now on until
// release; a subscription_update a client sends on one before its hello
// isn't taken (EarlyFrames counts it, nothing answers it).
func (s *Server) HoldHello() (release func()) {
	mc := s.memberChanges()
	ch := make(chan struct{})
	mc.mu.Lock()
	mc.hold = ch
	mc.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			mc.mu.Lock()
			if mc.hold == ch {
				mc.hold = nil
			}
			mc.mu.Unlock()
			close(ch)
		})
	}
}

// EarlyFrames is how many subscription_update frames came before their
// connection's hello.
func (s *Server) EarlyFrames() int {
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.early
}

func (s *Server) helloHold() chan struct{} {
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.hold
}

// earlyFrame notes data if it's a subscription_update sent before hello.
func (s *Server) earlyFrame(data []byte) bool {
	var f struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &f) != nil || f.Type != "subscription_update" {
		return false
	}
	mc := s.memberChanges()
	mc.mu.Lock()
	mc.early++
	mc.mu.Unlock()
	return true
}

// subscriptionReply answers a subscription_update frame (nil: not one).
func (s *Server) subscriptionReply(data []byte) []byte {
	var f struct {
		Type    string   `json:"type"`
		Subtype string   `json:"subtype"`
		ID      int64    `json:"id"`
		IDs     []string `json:"ids"`
	}
	if json.Unmarshal(data, &f) != nil || f.Type != "subscription_update" {
		return nil
	}
	mc := s.memberChanges()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	refuse := func(msg string) []byte {
		mc.refused++
		b, _ := json.Marshal(map[string]any{"ok": false, "reply_to": f.ID, "error": map[string]any{"code": 12, "msg": msg}})
		return b
	}
	tag := ""
	for i, id := range f.IDs {
		if i > 0 {
			tag += ","
		}
		tag += id
	}
	switch f.Subtype {
	case "user_change_subscribe_request":
		mc.frames = append(mc.frames, "subscribe:"+tag)
		if len(f.IDs) > slack.MaxUserSubs {
			return refuse("Too many updated ids")
		}
		add := 0
		for _, id := range f.IDs {
			if !mc.subs[id] {
				add++
			}
		}
		if mc.cap > 0 && len(mc.subs)+add > mc.cap {
			return refuse("Too many subscribed ids")
		}
		for _, id := range f.IDs {
			mc.subs[id] = true
		}
	case "user_change_unsubscribe_request":
		mc.frames = append(mc.frames, "unsubscribe:"+tag)
		for _, id := range f.IDs {
			delete(mc.subs, id)
		}
	default:
		return nil
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "reply_to": f.ID})
	return b
}

// conditionalMember reports whether users/info should leave id out: the
// client asked with a stamp the member's copy isn't newer than.
func conditionalMember(u slack.User, stamp int64) bool {
	return stamp != 0 && u.Updated != 0 && u.Updated <= stamp
}
