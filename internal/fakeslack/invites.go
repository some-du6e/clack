package fakeslack

// The invite modal's methods (core/invite.go, popups/invite.go): each call
// is recorded (InviteCalls) and answered here. Nothing is sent anywhere:
// this is the only place Clack's tests and shots invite anyone.
//
//   - users.admin.inviteBulk: every address in `invites` is invited, except
//     one with "fail" before the @ (invite_failed) and one with "already"
//     before the @ (already_in_team). InviteError, when set, fails the
//     whole call with that code.
//   - users.inviteRequests.create: the requests succeed unless
//     InviteError is set; then the error carries `requests` with each
//     address failed with it.
//   - users.sharedInvites.list (mode last_active_for_user): the links made
//     so far, newest first; users.admin.createSharedInvite makes one (30
//     days, 400 sign-ups) unless InviteLinkError is set.
//   - conversations.recommend (the modal's "Suggested:" channels, recorded
//     in RecommendCalls, not InviteCalls): the ids
//     SetRecommendedChannels gave, else the public channels that aren't
//     archived or shared, the viewer's first, up to limit, with their
//     channel objects (channels_info).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

type inviteState struct {
	mu           sync.Mutex
	calls        []InviteCall
	links        []map[string]any
	err, linkErr string
	recommend    []string     // nil: the default list
	recCalls     []url.Values // conversations.recommend's forms
}

// InviteCall is one invite method call: its method and form.
type InviteCall struct {
	Method string
	Form   url.Values
}

type inviteRecordJSON struct {
	Email string `json:"email"`
	Type  string `json:"type"`
	Mode  string `json:"mode"`
}

// InviteCalls returns the invite methods' calls so far, in order.
func (s *Server) InviteCalls() []InviteCall {
	s.inv.mu.Lock()
	defer s.inv.mu.Unlock()
	return append([]InviteCall(nil), s.inv.calls...)
}

// SetInviteError makes users.admin.inviteBulk and
// users.inviteRequests.create fail with code ("" clears it).
func (s *Server) SetInviteError(code string) {
	s.inv.mu.Lock()
	s.inv.err = code
	s.inv.mu.Unlock()
}

// SetInviteLinkError makes users.sharedInvites.list and
// users.admin.createSharedInvite fail with code ("" clears it).
func (s *Server) SetInviteLinkError(code string) {
	s.inv.mu.Lock()
	s.inv.linkErr = code
	s.inv.mu.Unlock()
}

// RecommendCalls returns conversations.recommend's forms so far (not in
// InviteCalls).
func (s *Server) RecommendCalls() []url.Values {
	s.inv.mu.Lock()
	defer s.inv.mu.Unlock()
	return append([]url.Values(nil), s.inv.recCalls...)
}

// SetRecommendedChannels sets conversations.recommend's answer (nil: the
// default list, an empty slice: none).
func (s *Server) SetRecommendedChannels(ids []string) {
	s.inv.mu.Lock()
	s.inv.recommend = ids
	s.inv.mu.Unlock()
}

// recommendChannels is conversations.recommend's channel list.
func (s *Server) recommendChannels(ids []string, limit int) []slack.Conversation {
	out := []slack.Conversation{}
	if ids != nil {
		for _, id := range ids {
			if c, ok := s.conv(id); ok && len(out) < limit {
				out = append(out, c)
			}
		}
		return out
	}
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for _, list := range [][]slack.Conversation{s.convs, s.others} {
		for _, c := range list {
			if len(out) == limit {
				return out
			}
			if c.IsIM || c.IsMPIM || c.IsPrivate || c.IsArchived || c.IsExtShared || c.ID == "" {
				continue
			}
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) handleInvites(method string, f url.Values) (any, bool) {
	switch method {
	case "users.admin.inviteBulk", "users.inviteRequests.create", "users.sharedInvites.list", "users.admin.createSharedInvite", "conversations.recommend":
	default:
		return nil, false
	}
	s.inv.mu.Lock()
	defer s.inv.mu.Unlock()
	form := url.Values{}
	for k, v := range f {
		if k != "token" {
			form[k] = append([]string(nil), v...)
		}
	}
	if method != "conversations.recommend" {
		s.inv.calls = append(s.inv.calls, InviteCall{Method: method, Form: form})
	}
	switch method {
	case "conversations.recommend":
		s.inv.recCalls = append(s.inv.recCalls, form)
		limit, _ := strconv.Atoi(f.Get("limit"))
		if limit <= 0 {
			limit = 10
		}
		ids := s.inv.recommend
		s.inv.mu.Unlock() // conv takes convMu
		chans := s.recommendChannels(ids, limit)
		s.inv.mu.Lock()
		return ok("channels", chans), true
	case "users.admin.inviteBulk":
		var recs []inviteRecordJSON
		if json.Unmarshal([]byte(f.Get("invites")), &recs) != nil || len(recs) == 0 {
			return fail("invalid_json"), true
		}
		if s.inv.err != "" {
			return fail(s.inv.err), true
		}
		out := make([]map[string]any, 0, len(recs))
		for _, r := range recs {
			local, _, _ := strings.Cut(r.Email, "@")
			switch {
			case strings.Contains(local, "already"):
				out = append(out, map[string]any{"email": r.Email, "ok": false, "error": "already_in_team"})
			case strings.Contains(local, "fail"):
				out = append(out, map[string]any{"email": r.Email, "ok": false, "error": "invite_failed"})
			default:
				out = append(out, map[string]any{"email": r.Email, "ok": true, "invite_id": fmt.Sprintf("I0FAKE%04d", len(s.inv.calls))})
			}
		}
		return ok("invites", out), true
	case "users.inviteRequests.create":
		var recs []inviteRecordJSON
		if json.Unmarshal([]byte(f.Get("requests")), &recs) != nil || len(recs) == 0 {
			return fail("invalid_requests"), true
		}
		if s.inv.err != "" {
			reqs := make([]map[string]any, 0, len(recs))
			for _, r := range recs {
				reqs = append(reqs, map[string]any{"email": r.Email, "ok": false, "error": s.inv.err})
			}
			return map[string]any{"ok": false, "error": s.inv.err, "requests": reqs}, true
		}
		return ok(), true
	case "users.sharedInvites.list":
		if s.inv.linkErr != "" {
			return fail(s.inv.linkErr), true
		}
		list := make([]map[string]any, 0, len(s.inv.links))
		for i := len(s.inv.links) - 1; i >= 0; i-- {
			list = append(list, s.inv.links[i])
		}
		return ok("invites", list), true
	default: // users.admin.createSharedInvite
		if s.inv.linkErr != "" {
			return fail(s.inv.linkErr), true
		}
		now := time.Now().Unix()
		code := fmt.Sprintf("zt-fake%04d", len(s.inv.links)+1)
		l := map[string]any{"code": code, "url": "https://join.slack.com/t/fakeclub/shared_invite/" + code,
			"date_create": now, "date_expire": now + 30*24*3600, "max_signups": 400, "num_signups": 0,
			"inviter_id": SelfID, "notifications_enabled": true}
		s.inv.links = append(s.inv.links, l)
		return ok(kvPairs(l)...), true
	}
}

// kvPairs is m as ok's key, value pairs.
func kvPairs(m map[string]any) []any {
	out := make([]any, 0, 2*len(m))
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}
