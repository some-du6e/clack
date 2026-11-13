package fakeslack

// An agent app's DM thread (Size.AgentThread), synthetic, in the shapes
// an agent built on the Chat SDK's Slack adapter posts with native
// streaming off (Karim's report, 2026-11-12):
//
//   - the reply goes out as fallbackStream's placeholder (chat.postMessage
//     {text: "working..."}), then one chat.update {markdown_text} with
//     the answer, which Slack stores as a markdown block (an edited
//     message, edited by the app's bot user);
//   - each turn ends with a separate footer message: a context line
//     (mrkdwn "_done in 1 second_") and a context_actions block with
//     feedback_buttons, its text the plain "done in 1 second".
//
// The thread (in AgentDMID, rooted at AgentDMRootTS): the viewer's "yo",
// the agent's "yo, what's up?" and footer, the viewer's "ping" and the
// agent's placeholder. PlayAgentEdit plays the rest live: the placeholder's
// message_changed to "pong", then the second footer. conversations.replies
// answers the thread as it stands; history has the root.

import (
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	AgentDMID     = "D0AGENTDM01"
	AgentDMUserID = "U0AGENTDM01" // the app's bot user, "pip"
	AgentDMBotID  = "B0AGENTDM01"
	AgentDMAppID  = "A0AGENTDM01"
	AgentDMRootTS = "1762800000.000100"
)

// The thread's replies, oldest first.
const (
	AgentDMReply1TS  = "1762800005.000100" // "yo, what's up?"
	AgentDMFooter1TS = "1762800006.000100" // "done in 1 second"
	AgentDMPingTS    = "1762800030.000100" // the viewer's "ping"
	AgentDMAnswerTS  = "1762800031.000100" // "working..." → "pong"
	AgentDMFooter2TS = "1762800032.000100" // "done in under a second" (live)
)

// agentDMState is the thread as it stands.
type agentDMState struct {
	mu    sync.Mutex
	msgs  []slack.Message // root first
	reads int             // conversations.replies answers read
}

var agentDMStates sync.Map // *Server → *agentDMState

func (s *Server) agentDM() *agentDMState {
	v, _ := agentDMStates.LoadOrStore(s, &agentDMState{})
	return v.(*agentDMState)
}

func (s *Server) agentDMUser() slack.User {
	img := func(px int) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/agentdm.png"
	}
	p := slack.Profile{RealName: "Pip", Team: TeamID, Fields: json.RawMessage(`{}`), BotID: AgentDMBotID, APIAppID: AgentDMAppID,
		Image24: img(24), Image32: img(32), Image48: img(48), Image72: img(72), Image192: img(192), Image512: img(512)}
	return slack.User{ID: AgentDMUserID, TeamID: TeamID, Name: "pip", RealName: "Pip", IsBot: true, Profile: p, Presence: "active"}
}

// agentDMBotMsg is a message the app posted (chat.postMessage with its
// bot token): user is the bot user, with bot_id, app_id and bot_profile.
func (s *Server) agentDMBotMsg(ts, text string, blocks any) slack.Message {
	m := slack.Message{Type: "message", TS: ts, User: AgentDMUserID, BotID: AgentDMBotID, Text: text, ThreadTS: AgentDMRootTS,
		ParentUser: SelfID, Channel: AgentDMID,
		BotProfile: &slack.BotProfile{ID: AgentDMBotID, AppID: AgentDMAppID, UserID: AgentDMUserID, Name: "pip", TeamID: TeamID}}
	if blocks != nil {
		m.Blocks = rawJSON(blocks)
	}
	return m
}

// AgentDMFooter is a turn's footer blocks: the italic context line and
// the 👍/👎 feedback buttons (buildFeedbackButtonsBlock's shape).
func AgentDMFooter(text string) []any {
	return []any{
		map[string]any{"type": "context", "block_id": "ft" + strconv.Itoa(len(text)), "elements": []any{
			map[string]any{"type": "mrkdwn", "text": "_" + text + "_"}}},
		map[string]any{"type": "context_actions", "block_id": "fb" + strconv.Itoa(len(text)), "elements": []any{
			map[string]any{"type": "feedback_buttons", "action_id": "message_feedback",
				"positive_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Good response"}, "value": "up:trace"},
				"negative_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Bad response"}, "value": "down:trace"}}}},
	}
}

func (s *Server) addAgentThread() {
	s.convs = append(s.convs, slack.Conversation{ID: AgentDMID, IsIM: true, User: AgentDMUserID, IsOpen: true, Created: 1762000000})
	self := func(ts, text string) slack.Message {
		m := slack.Message{Type: "message", TS: ts, User: SelfID, Text: text, ThreadTS: AgentDMRootTS, Channel: AgentDMID}
		m.Blocks = rawJSON(richText(map[string]any{"type": "text", "text": text}))
		return m
	}
	root := self(AgentDMRootTS, "yo")
	st := s.agentDM()
	st.mu.Lock()
	st.msgs = []slack.Message{root,
		s.agentDMBotMsg(AgentDMReply1TS, "yo, what's up?", nil),
		s.agentDMBotMsg(AgentDMFooter1TS, "done in 1 second", AgentDMFooter("done in 1 second")),
		self(AgentDMPingTS, "ping"),
		s.agentDMBotMsg(AgentDMAnswerTS, "working...", nil),
	}
	st.rootMetaLocked()
	root = st.msgs[0]
	st.mu.Unlock()
	s.hist[AgentDMID] = []slack.Message{root}
}

// rootMetaLocked gives the root its reply metadata (caller holds mu).
func (st *agentDMState) rootMetaLocked() {
	r := &st.msgs[0]
	r.ReplyCount = len(st.msgs) - 1
	r.LatestReply = st.msgs[len(st.msgs)-1].TS
	r.ReplyUsers = []string{AgentDMUserID, SelfID}
	r.ReplyUsersN = 2
}

// AgentDMThread is the thread as it stands, root first.
func (s *Server) AgentDMThread() []slack.Message {
	st := s.agentDM()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]slack.Message(nil), st.msgs...)
}

// AgentDMRepliesRead is how many conversations.replies answers read the
// thread (a test holding the reply with Fault.ReplyDelay knows the answer
// has the thread as it was).
func (s *Server) AgentDMRepliesRead() int {
	st := s.agentDM()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.reads
}

// AgentDMAnswer is the edited answer: chat.update {markdown_text: "pong"}.
func (s *Server) AgentDMAnswer(at time.Time) slack.Message {
	m := s.agentDMBotMsg(AgentDMAnswerTS, "pong", []any{map[string]any{"type": "markdown", "block_id": "md1", "text": "pong"}})
	m.Edited = &slack.Edited{User: AgentDMUserID, TS: strconv.FormatInt(at.Unix(), 10) + ".000000"}
	return m
}

// AgentDMFooter2 is the second turn's footer.
func (s *Server) AgentDMFooter2() slack.Message {
	return s.agentDMBotMsg(AgentDMFooter2TS, "done in under a second", AgentDMFooter("done in under a second"))
}

// PlayAgentEdit plays the turn's end live: the answer's chat.update
// (message_changed, state first), then, gap later, the footer (message).
func (s *Server) PlayAgentEdit(gap time.Duration) {
	st := s.agentDM()
	now := time.Now()
	ans := s.AgentDMAnswer(now)
	st.mu.Lock()
	prev := st.msgs[4]
	st.msgs[4] = ans
	st.mu.Unlock()
	s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": AgentDMID, "hidden": true,
		"message": ans, "previous_message": prev, "ts": strconv.FormatInt(now.Unix(), 10) + ".000100",
		"event_ts": strconv.FormatInt(now.Unix(), 10) + ".000100"})
	if gap > 0 {
		time.Sleep(gap)
	}
	ft := s.AgentDMFooter2()
	st.mu.Lock()
	st.msgs = append(st.msgs, ft)
	st.rootMetaLocked()
	root := st.msgs[0]
	st.mu.Unlock()
	ev := map[string]any{}
	b, _ := json.Marshal(ft)
	_ = json.Unmarshal(b, &ev)
	ev["channel"], ev["event_ts"] = AgentDMID, ft.TS
	s.Push(ev)
	s.Push(map[string]any{"type": "message", "subtype": "message_replied", "channel": AgentDMID, "hidden": true,
		"message": root, "event_ts": ft.TS})
}

// AgentDMActivity are the realtime activity entries the server pushes for
// an agent reply in the viewer's thread: thread_reply (an entry Slack's
// client drops) and the thread's thread_v2 bundle.
func AgentDMActivity(ts string) []map[string]any {
	return []map[string]any{
		{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
			"key": "thread_reply-" + AgentDMID + "-" + ts, "feed_ts": ts, "is_unread": true,
			"item": map[string]any{"type": "thread_reply", "message": map[string]any{
				"ts": ts, "channel": AgentDMID, "thread_ts": AgentDMRootTS, "author_user_id": AgentDMUserID, "is_broadcast": false}}}},
		{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
			"key": "thread_v2-" + AgentDMID + "-" + AgentDMRootTS, "feed_ts": ts, "is_unread": true,
			"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{
				"thread_entry": map[string]any{"channel_id": AgentDMID, "thread_ts": AgentDMRootTS, "latest_ts": ts, "unread_msg_count": 1}}}}}},
	}
}

// handleAgentThread answers the thread's replies and messages.list for
// its messages.
func (s *Server) handleAgentThread(method string, f url.Values) (any, bool) {
	if !s.Size.AgentThread {
		return nil, false
	}
	switch method {
	case "conversations.replies":
		if f.Get("channel") != AgentDMID || f.Get("ts") != AgentDMRootTS {
			return nil, false
		}
		msgs := s.AgentDMThread()
		st := s.agentDM()
		st.mu.Lock()
		st.reads++
		st.mu.Unlock()
		return ok("messages", msgs, "has_more", false), true
	case "messages.list":
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
			return nil, false
		}
		if len(want) == 0 {
			return nil, false
		}
		out := map[string][]slack.Message{}
		for _, w := range want {
			if w.Channel != AgentDMID {
				return nil, false // another fixture's messages
			}
			for _, m := range s.AgentDMThread() {
				for _, ts := range w.Timestamps {
					if m.TS == ts {
						out[AgentDMID] = append(out[AgentDMID], m)
					}
				}
			}
		}
		return ok("messages", out), true
	}
	return nil, false
}
