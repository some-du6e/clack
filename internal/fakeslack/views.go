package fakeslack

import (
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The rail's list views (Size.Views), made-up content only:
//
//   - subscriptions.thread.getView {limit, fetch_threads_state, max_ts}:
//     two followed threads in ViewsChannel. The first has one unread
//     reply, the second is read (the Threads view's "You're up to date"
//     divider goes between them once the first is read).
//   - subscriptions.thread.mark {channel, thread_ts, ts} is recorded
//     (ThreadMarks) and clears the unread reply.
//   - drafts.list carries a draft, a scheduled message and a scheduled
//     thread reply (Drafts & sent: the Drafts and Scheduled tabs).
//   - the Activity inbox: activity.feed and its writes (activity.go).

// ViewsChannel is the channel the views fixtures live in.
const ViewsChannel = "C0FAKE00000"

// The views fixtures' thread roots and their replies.
const (
	ViewsThreadUnread = "1762600000.000100" // has an unread reply
	ViewsThreadRead   = "1762500000.000100"
)

// ThreadMark is one subscriptions.thread.mark call.
type ThreadMark struct{ Channel, ThreadTS, TS string }

// ActivityWrite is one activity.* write: its method and form.
type ActivityWrite struct {
	Method string
	Form   url.Values
}

type viewsState struct {
	mu       sync.Mutex
	marks    []ThreadMark
	writes   []ActivityWrite
	read     bool // the unread thread was marked read
	seeded   bool
	activity *activityState // activity.go
}

var viewsStates sync.Map // *Server -> *viewsState

func (s *Server) views() *viewsState {
	v, _ := viewsStates.LoadOrStore(s, &viewsState{})
	return v.(*viewsState)
}

// ThreadMarks returns the subscriptions.thread.mark calls, in order.
func (s *Server) ThreadMarks() []ThreadMark {
	v := s.views()
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]ThreadMark(nil), v.marks...)
}

// ActivityWrites returns the activity.* writes, in order.
func (s *Server) ActivityWrites() []ActivityWrite {
	v := s.views()
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]ActivityWrite(nil), v.writes...)
}

// SeedViewDrafts puts the views fixtures' drafts in drafts.list: a
// draft to ViewsChannel, a message scheduled for tomorrow at 9:00 and a
// thread reply scheduled the day after. Called on the first drafts.list
// with Size.Views; tests may call it directly.
func (s *Server) SeedViewDrafts() {
	v := s.views()
	v.mu.Lock()
	if v.seeded {
		v.mu.Unlock()
		return
	}
	v.seeded = true
	v.mu.Unlock()
	now := time.Now()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
	text := func(t string) json.RawMessage {
		b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": t}}}}}})
		return b
	}
	dest := func(ch, thread string) json.RawMessage {
		d := map[string]any{"channel_id": ch}
		if thread != "" {
			d["thread_ts"] = thread
		}
		b, _ := json.Marshal([]any{d})
		return b
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	for _, d := range []*Draft{
		{Blocks: text("a synthetic draft about the plan"), Destinations: dest(ViewsChannel, ""), DateCreated: now.Add(-50 * time.Hour).Unix()},
		{Blocks: text("a synthetic scheduled update"), Destinations: dest(ViewsChannel, ""), DateCreated: now.Add(-2 * time.Hour).Unix(), DateScheduled: tomorrow.Unix()},
		{Blocks: text("a synthetic scheduled reply"), Destinations: dest(ViewsChannel, ViewsThreadRead), DateCreated: now.Add(-time.Hour).Unix(), DateScheduled: tomorrow.AddDate(0, 0, 1).Unix()},
	} {
		d.ID = s.newID("Dr")
		d.ClientMsgID = d.ID + "-client"
		d.LastUpdatedTS = strconv.FormatInt(d.DateCreated*1000, 10)
		s.drafts[d.ID] = d
	}
}

func viewsMsg(ch, ts, thread, user, text string) slack.Message {
	return slack.Message{Type: "message", Channel: ch, TS: ts, ThreadTS: thread, User: user, Text: text}
}

// handleViews answers the views fixtures' methods (handled=false for
// anything else, or without Size.Views).
func (s *Server) handleViews(method string, f url.Values) (any, bool) {
	v := s.views()
	switch method {
	case "subscriptions.thread.getView":
		v.mu.Lock()
		read := v.read
		v.mu.Unlock()
		ch := ViewsChannel
		r1 := viewsMsg(ch, ViewsThreadUnread, ViewsThreadUnread, "U0FAKE00001", "a synthetic question for the team")
		r1.ReplyCount, r1.ReplyUsers = 2, []string{SelfID, "U0FAKE00002"}
		r1.LatestReply = "1762600200.000100"
		a1 := viewsMsg(ch, "1762600100.000100", ViewsThreadUnread, SelfID, "a synthetic answer")
		a2 := viewsMsg(ch, "1762600200.000100", ViewsThreadUnread, "U0FAKE00002", "a synthetic follow-up")
		r2 := viewsMsg(ch, ViewsThreadRead, ViewsThreadRead, SelfID, "a synthetic note that was settled")
		r2.ReplyCount, r2.ReplyUsers = 1, []string{"U0FAKE00001"}
		r2.LatestReply = "1762500100.000100"
		b1 := viewsMsg(ch, "1762500100.000100", ViewsThreadRead, "U0FAKE00001", "a synthetic thumbs up")
		unread := []slack.Message{a2}
		total := 1
		if read {
			unread, total = nil, 0
		}
		threads := []any{
			map[string]any{"root_msg": r1, "latest_replies": []slack.Message{a1, a2}, "unread_replies": unread},
			map[string]any{"root_msg": r2, "latest_replies": []slack.Message{b1}, "unread_replies": []slack.Message{}},
		}
		return ok("threads", threads, "has_more", false, "total_unread_replies", total,
			"threads_state", map[string]any{"has_unreads": total > 0, "mention_count": 0}), true
	case "subscriptions.thread.mark":
		v.mu.Lock()
		v.marks = append(v.marks, ThreadMark{Channel: f.Get("channel"), ThreadTS: f.Get("thread_ts"), TS: f.Get("ts")})
		if f.Get("thread_ts") == ViewsThreadUnread {
			v.read = true
		}
		v.mu.Unlock()
		return ok(), true
	case "drafts.list":
		s.SeedViewDrafts()
		return nil, false // the drafts handler answers
	}
	return s.handleActivity(method, f)
}
