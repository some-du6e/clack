package core

import (
	"encoding/json"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Posting permissions, ported from Slack's web client (build a84662d2d135,
// 2026-11-08; webpack ids in client-boot):
//
//   - setPostingPermissions: the channel's is_read_only / is_thread_only
//     flags when the server sends them, else computeChannelPostingPermissions
//     (XJ7S) over properties.posting_restricted_to / threads_restricted_to.
//   - XJ7S checkChannelPrefCascade(pref) says whether the viewer may post
//     under one pref: an empty pref ({}, lodash isEmpty) blocks; an unknown
//     viewer is blocked; a viewer listed in pref.user may post; otherwise
//     head(pref.role || pref.type) decides: "regular" = not a guest,
//     "admin" = a workspace or org admin, "org_admin" = an org admin (both
//     only when the channel isn't hosted by another org), and "ra", "all",
//     anything else (or none) lets everyone post.
//   - isReadOnly = can't post and can't reply; isThreadOnly = can't post at
//     the top level but can reply in threads.
//   - canUserPostInChannel (o/IU) is false for either (Xdwr isReadOnly,
//     1lcC isThreadOnly): MessagePaneInput then shows
//     MessageInputAnnounceOnlyRoadblock. canThread (Opi6) is false for
//     read-only (or is_non_threadable) channels: the thread footer shows the
//     same roadblock.

// Posting is what the viewer may do in a conversation.
type Posting struct {
	ReadOnly   bool // no top-level messages, no thread replies
	ThreadOnly bool // thread replies only
	NoThreads  bool // is_non_threadable: no thread replies (canThread)
}

// CanPost reports whether the viewer may post at the top level.
func (p Posting) CanPost() bool { return !p.ReadOnly && !p.ThreadOnly }

// CanReply reports whether the viewer may reply in threads.
func (p Posting) CanReply() bool { return !p.ReadOnly && !p.NoThreads }

// PostingIn returns the viewer's posting permissions in ch (zero for
// unknown conversations, DMs and group DMs).
func (a *App) PostingIn(ch string) Posting {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return Posting{}
		}
	}
	return a.PostingFor(c)
}

// PostingFor is PostingIn for a stored conversation.
func (a *App) PostingFor(c *slack.Conversation) Posting {
	if c == nil || c.IsIM || c.IsMPIM {
		return Posting{}
	}
	var p Posting
	if c.IsReadOnly != nil || c.IsThreadOnly != nil {
		if c.IsReadOnly != nil {
			p.ReadOnly = *c.IsReadOnly
		}
		if c.IsThreadOnly != nil {
			p.ThreadOnly = *c.IsThreadOnly
		}
	} else {
		post, thread := a.restrictedTo(c)
		canPost := post == nil || a.prefAllows(c, post)
		canThread := thread == nil || a.prefAllows(c, thread)
		p = Posting{ReadOnly: !canPost && !canThread, ThreadOnly: !canPost && canThread}
	}
	p.NoThreads = c.IsNonThreadable
	return p
}

// postingPref is one posting_restricted_to / threads_restricted_to value.
type postingPref struct {
	Type json.RawMessage `json:"type"` // ["admin"] (Slack also accepts a string)
	Role json.RawMessage `json:"role"`
	User []string        `json:"user"`
	// keys counts the object's keys: lodash isEmpty({}) blocks everyone.
	keys int
}

func (p *postingPref) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	p.keys = len(m)
	p.Type, p.Role = m["type"], m["role"]
	if u, ok := m["user"]; ok {
		_ = json.Unmarshal(u, &p.User)
	}
	return nil
}

// head is lodash head over a string-or-array JSON value.
func head(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var l []string
	if json.Unmarshal(raw, &l) == nil && len(l) > 0 {
		return l[0]
	}
	return ""
}

func (a *App) restrictedTo(c *slack.Conversation) (post, thread *postingPref) {
	if len(c.Properties) == 0 {
		return nil, nil
	}
	var p struct {
		Post   *postingPref `json:"posting_restricted_to"`
		Thread *postingPref `json:"threads_restricted_to"`
	}
	if json.Unmarshal(c.Properties, &p) != nil {
		return nil, nil
	}
	return p.Post, p.Thread
}

// prefAllows is XJ7S checkChannelPrefCascade.
func (a *App) prefAllows(c *slack.Conversation, p *postingPref) bool {
	if p.keys == 0 {
		return false
	}
	selfID := a.S.SelfID()
	self, ok := a.S.User(selfID)
	if !ok || selfID == "" {
		return false // isNonExistentMember
	}
	for _, u := range p.User {
		if u == selfID {
			return true
		}
	}
	role := p.Role
	if len(role) == 0 || string(role) == "null" || string(role) == `""` {
		role = p.Type
	}
	foreign := c.IsExtShared && a.hostedElsewhere(c)
	switch head(role) {
	case "regular":
		return !foreign && !self.IsRestricted && !self.IsUltraRestricted
	case "admin": // canUserActAsWorkspaceAdmin: org admin or workspace admin
		return !foreign && (self.IsAdmin || (self.EnterpriseUser != nil && self.EnterpriseUser.IsAdmin))
	case "org_admin":
		return !foreign && self.EnterpriseUser != nil && self.EnterpriseUser.IsAdmin
	}
	return true // "ra", "all" and anything else
}

// hostedElsewhere is 2ES1: the channel's host is neither the viewer's
// workspace nor their org.
func (a *App) hostedElsewhere(c *slack.Conversation) bool {
	h := c.ConversationHostID
	if h == "" {
		return false
	}
	a.S.RLock()
	team := a.S.TeamID
	a.S.RUnlock()
	return h != team && h != a.C.Creds.EnterpriseID
}
