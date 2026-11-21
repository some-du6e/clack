package fakeslack

import (
	"fmt"
	"time"
)

// ReadSystemDMElsewhere reads the system DM (Size.SystemDM) up to ts in
// another client: the server's read cursor moves as that client's
// conversations.mark moves it (client.counts and conversations.info
// follow), and it returns the im_marked event Slack pushes for the mark,
// unpushed (inject it with core.Inject, or Push it with realtime on). The
// event's counts are the posts still unread after ts; with
// SetSystemDMMentions(false) its mention counts are 0.
func (s *Server) ReadSystemDMElsewhere(ts string) map[string]any {
	s.secs.mu.Lock()
	if s.secs.marks == nil {
		s.secs.marks = map[string]string{}
	}
	s.secs.marks[SystemDMID] = ts
	s.secs.mu.Unlock()
	_, unread := s.systemDMUnread(ts)
	mentions := unread
	if s.sysDM.noMentions.Load() {
		mentions = 0
	}
	now := time.Now()
	return map[string]any{"type": "im_marked", "channel": SystemDMID, "ts": ts, "dm_count": unread,
		"unread_count_display": unread, "num_mentions_display": mentions, "mention_count_display": mentions,
		"event_ts": fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)}
}
