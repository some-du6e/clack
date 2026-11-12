package fakeslack

import (
	"net/url"
	"sync"
)

// Content review flags: moderation.flags.create, which Slack's
// FlagMessageDialog calls (channel_id, item_id, type, reason, and for
// channel details channel_details_flagged). The fake answers ok and keeps
// each call (FlagCalls); SetHook can answer an error instead.

type modState struct {
	mu    sync.Mutex
	flags []url.Values
}

var modStates sync.Map // *Server -> *modState

func (s *Server) mod() *modState {
	v, _ := modStates.LoadOrStore(s, &modState{})
	return v.(*modState)
}

// FlagCalls returns the moderation.flags.create calls so far.
func (s *Server) FlagCalls() []url.Values {
	st := s.mod()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]url.Values(nil), st.flags...)
}

func (s *Server) handleModeration(method string, f url.Values) (any, bool) {
	if method != "moderation.flags.create" {
		return nil, false
	}
	st := s.mod()
	st.mu.Lock()
	st.flags = append(st.flags, f)
	st.mu.Unlock()
	return ok(), true
}
