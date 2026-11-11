package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// Closed DMs and group DMs for the sidebar text filter's closed-DM
// matches (sidebar_text_filter_m2): users.conversations lists them,
// sidebar.dms says is_open false and client.counts has them read.

// AddClosedDM adds a closed 1:1 DM with user (call before the client
// boots).
func (s *Server) AddClosedDM(id, user string) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	s.convs = append(s.convs, slack.Conversation{ID: id, IsIM: true, User: user, Created: 1762000000})
}

// AddClosedMPDM adds a closed group DM with members (the viewer among
// them).
func (s *Server) AddClosedMPDM(id, name string, members []string) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	s.convs = append(s.convs, slack.Conversation{ID: id, Name: name, IsMPIM: true, IsGroup: true, IsPrivate: true,
		Members: append([]string(nil), members...), Created: 1762000000})
}

// closedDM reports whether c is a DM or group DM that isn't open.
func closedDM(c slack.Conversation) bool { return (c.IsIM || c.IsMPIM) && !c.IsOpen }
