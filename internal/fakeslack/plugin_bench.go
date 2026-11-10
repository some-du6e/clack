package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Plugin bench fixtures (Size.PluginBench): one long channel whose 500
// messages exercise what the plugins that ship in both Taut and Slick do
// (internal/ui's TestPluginBench, docs/PLUGIN-BENCH.md): senders with and
// without pronouns in runs of three, reactions with more reactors than an
// avatar row shows, the words Censorship masks by default, threads with
// replies, a relay bot posting for a person (ShowRealUser) and an app
// posting with a person's token (ShowSendingBot). Next to it: an
// admins-only channel (the viewer is an admin) and a Slackbot DM. All
// synthetic; nothing comes from a real workspace.
const (
	BenchChannelID    = "C0PLUGBNCH"
	BenchAdminsID     = "C0PLUGADMN" // posting restricted to admins
	BenchSlackbotDMID = "D0PLUGSLKB"
	BenchMessages     = 500

	// BenchRelayBotID is at-channel's bot id (a relay both ShowRealUser
	// variants know); its messages name the person in metadata.
	BenchRelayBotID = "B08G06U6SJG"
	// BenchSenderBotID posts with a person's token (ShowSendingBot).
	BenchSenderBotID = "B0PLUGSEND"
	BenchSenderBot   = "Bench Sender"
)

// BenchCase describes the fixture for the bench's checks.
type BenchCase struct {
	Authors     []string // senders, in run order
	Pronouns    map[string]string
	Reacted     []string          // ts of messages with reactions
	Censored    []string          // ts of messages containing "job"
	Threads     []string          // ts of thread roots
	ThreadBy    map[string]string // thread root ts → its sender
	Relayed     map[string]string // ts → the person a relay bot posted for
	SentByBot   []string          // ts of person messages an app sent
	OldestTS    string
	NewestTS    string
	ReplyAuthor string
}

func benchTS(i int) string { return fmt.Sprintf("%d.%06d", 1794000000+i*97, 100+i) }

// benchAuthors picks eight people with pronouns and four without (fixed by
// the seed, so every run has the same rows).
func (s *Server) benchAuthors() (with, without []string) {
	for _, u := range s.users[1:min(len(s.users), 400)] {
		switch {
		case u.Profile.Pronouns != "" && len(with) < 8:
			with = append(with, u.ID)
		case u.Profile.Pronouns == "" && len(without) < 4:
			without = append(without, u.ID)
		}
	}
	return with, without
}

// Bench returns the fixture's cases.
func (s *Server) Bench() BenchCase {
	with, without := s.benchAuthors()
	var authors []string
	for i := range max(len(with), len(without)) {
		if i < len(with) {
			authors = append(authors, with[i])
		}
		if i < len(without) && i%2 == 1 {
			authors = append(authors, without[i])
		}
	}
	c := BenchCase{Authors: authors, Pronouns: map[string]string{}, Relayed: map[string]string{}, ThreadBy: map[string]string{}}
	for _, id := range with {
		if u, ok := s.userByID(id); ok {
			c.Pronouns[id] = u.Profile.Pronouns
		}
	}
	for i := 0; i < BenchMessages; i++ {
		ts := benchTS(i)
		switch {
		case i%50 == 10:
			c.Relayed[ts] = authors[(i/3+1)%len(authors)]
		case benchSentByBot(i):
			c.SentByBot = append(c.SentByBot, ts)
		}
		if i%5 == 0 {
			c.Reacted = append(c.Reacted, ts)
		}
		if i%7 == 0 {
			c.Censored = append(c.Censored, ts)
		}
		if i%20 == 0 {
			c.Threads = append(c.Threads, ts)
			c.ThreadBy[ts] = authors[(i/3)%len(authors)]
		}
	}
	c.OldestTS, c.NewestTS = benchTS(0), benchTS(BenchMessages-1)
	c.ReplyAuthor = authors[0]
	return c
}

func benchRich(text string) json.RawMessage {
	b, _ := json.Marshal([]map[string]any{{"type": "rich_text", "block_id": "pb",
		"elements": []map[string]any{{"type": "rich_text_section", "elements": []map[string]any{{"type": "text", "text": text}}}}}})
	return b
}

func (s *Server) addPluginBench() {
	c := s.Bench()
	s.users[0].IsAdmin = true // RestrictedChannelWarning shows admins its box
	authors := c.Authors
	msgs := make([]slack.Message, 0, BenchMessages)
	for i := 0; i < BenchMessages; i++ {
		ts := benchTS(i)
		u := authors[(i/3)%len(authors)]
		text := fmt.Sprintf("bench message %d: %s", i, s.words[(i*7)%len(s.words)]+" "+s.words[(i*13)%len(s.words)])
		if i%7 == 0 {
			text += " — the job fair is on Friday"
		}
		if i%4 == 0 {
			text += ", with a longer body that wraps over more than one line of the message pane so rows differ in height"
		}
		m := slack.Message{Type: "message", TS: ts, User: u, Text: text, Blocks: benchRich(text), ClientMsgID: fmt.Sprintf("%08x-bench", i)}
		if i%5 == 0 {
			var r1, r2 []string
			for k := 0; k < 3; k++ {
				r1 = append(r1, authors[(i+k)%len(authors)])
			}
			for k := 0; k < 7; k++ {
				r2 = append(r2, s.users[1+(i+k*11)%min(len(s.users)-1, 300)].ID)
			}
			m.Reactions = []slack.Reaction{{Name: "tada", Count: len(r1), Users: r1}, {Name: "eyes", Count: len(r2), Users: r2}}
		}
		if i%20 == 0 {
			m.ThreadTS, m.ReplyCount = ts, 3
			m.ReplyUsers = []string{authors[(i+1)%len(authors)], authors[(i+2)%len(authors)]}
			m.LatestReply = benchReplyTS(ts, 2)
		}
		if who, ok := c.Relayed[ts]; ok {
			meta, _ := json.Marshal(map[string]any{"event_type": "at_channel_relay", "event_payload": map[string]any{"source_user_id": who}})
			m.User, m.Subtype, m.BotID, m.Username = "", "bot_message", BenchRelayBotID, "at-channel"
			m.BotProfile = &slack.BotProfile{ID: BenchRelayBotID, Name: "at-channel", AppID: "A08G06U6SJG", TeamID: TeamID}
			m.Metadata = meta
		}
		if benchSentByBot(i) {
			meta, _ := json.Marshal(map[string]any{"event_type": "bench_post", "event_payload": map[string]any{}})
			m.BotID = BenchSenderBotID
			m.BotProfile = &slack.BotProfile{ID: BenchSenderBotID, Name: BenchSenderBot, AppID: "A0PLUGSEND", TeamID: TeamID,
				Icons: s.identityIcons("bench-sender")}
			m.Metadata = meta
		}
		msgs = append(msgs, m)
	}
	newest := make([]slack.Message, len(msgs))
	for i, m := range msgs {
		newest[len(msgs)-1-i] = m
	}
	s.hist[BenchChannelID] = newest
	props, _ := json.Marshal(map[string]any{"posting_restricted_to": map[string]any{"type": []string{"admin"}}})
	s.convs = append(s.convs,
		slack.Conversation{ID: BenchChannelID, Name: "plugin-bench", IsChannel: true, IsMember: true, NumMembers: 1240,
			Created: 1700000000, Creator: authors[0], ContextTeamID: TeamID,
			Topic: &slack.Purpose{Value: "Plugin bench fixtures"}, Purpose: &slack.Purpose{Value: "Synthetic messages for the plugin bench"}},
		slack.Conversation{ID: BenchAdminsID, Name: "admins-only", IsChannel: true, IsMember: true, NumMembers: 300,
			Created: 1700000000, Creator: authors[0], ContextTeamID: TeamID, Properties: props,
			Purpose: &slack.Purpose{Value: "Only admins can post here"}},
		slack.Conversation{ID: BenchSlackbotDMID, IsIM: true, User: SlackbotID, IsOpen: true, Created: 1700000000, Priority: 0.1},
	)
	s.hist[BenchAdminsID] = []slack.Message{{Type: "message", TS: benchTS(3), User: authors[0], Text: "admins only", Blocks: benchRich("admins only")}}
	s.hist[BenchSlackbotDMID] = []slack.Message{{Type: "message", TS: benchTS(1), User: SlackbotID, Text: "welcome to the bench"}}
}

func benchReplyTS(root string, n int) string {
	sec, _, _ := strings.Cut(root, ".")
	return fmt.Sprintf("%s.%06d", sec, 900000+n)
}

// handlePluginBench answers conversations.replies for the bench threads,
// bots.info and messages.list for the bench's bots (the app behind a
// person's message, ShowSendingBot), and search.messages "from:<@U>"
// (Last Seen's latest message).
func (s *Server) handlePluginBench(method string, f url.Values) (any, bool) {
	switch method {
	case "bots.info":
		var out []slack.BotProfile
		for _, id := range strings.Split(f.Get("bots")+","+f.Get("bot"), ",") {
			switch id {
			case BenchSenderBotID:
				out = append(out, slack.BotProfile{ID: BenchSenderBotID, Name: BenchSenderBot, AppID: "A0PLUGSEND", TeamID: TeamID, Icons: s.identityIcons("bench-sender")})
			case BenchRelayBotID:
				out = append(out, slack.BotProfile{ID: BenchRelayBotID, Name: "at-channel", AppID: "A08G06U6SJG", TeamID: TeamID})
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return map[string]any{"ok": true, "bots": out}, true
	case "messages.list":
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
			return nil, false
		}
		out := map[string][]map[string]any{}
		s.histMu.Lock()
		for _, w := range want {
			for _, m := range s.hist[w.Channel] {
				for _, ts := range w.Timestamps {
					if m.TS == ts {
						e := map[string]any{"ts": m.TS, "user": m.User, "text": m.Text, "type": "message"}
						if m.BotID == BenchSenderBotID {
							e["bot_id"], e["app_id"] = m.BotID, "A0PLUGSEND"
						}
						out[w.Channel] = append(out[w.Channel], e)
					}
				}
			}
		}
		s.histMu.Unlock()
		return map[string]any{"ok": true, "messages": out}, true
	case "search.messages":
		q := f.Get("query")
		u, ok := strings.CutPrefix(q, "from:<@")
		if !ok {
			return nil, false
		}
		u, _, _ = strings.Cut(u, ">")
		var matches []map[string]any
		s.histMu.Lock()
		for _, m := range s.hist[BenchChannelID] { // newest first
			if m.User == u {
				matches = append(matches, map[string]any{"ts": m.TS, "user": m.User, "text": m.Text, "channel": map[string]any{"id": BenchChannelID, "name": "plugin-bench"}})
				break
			}
		}
		s.histMu.Unlock()
		return map[string]any{"ok": true, "query": q, "messages": map[string]any{"total": len(matches), "matches": matches}}, true
	}
	if method != "conversations.replies" || f.Get("channel") != BenchChannelID {
		return nil, false
	}
	root := f.Get("ts")
	s.histMu.Lock()
	var rm *slack.Message
	for i := range s.hist[BenchChannelID] {
		if m := &s.hist[BenchChannelID][i]; m.TS == root {
			c := *m
			rm = &c
			break
		}
	}
	s.histMu.Unlock()
	if rm == nil {
		return map[string]any{"ok": false, "error": "thread_not_found"}, true
	}
	out := []slack.Message{*rm}
	for n := 0; n < rm.ReplyCount; n++ {
		u := rm.User
		if len(rm.ReplyUsers) > 0 {
			u = rm.ReplyUsers[n%len(rm.ReplyUsers)]
		}
		text := fmt.Sprintf("bench reply %d", n)
		out = append(out, slack.Message{Type: "message", TS: benchReplyTS(root, n), ThreadTS: root, User: u, Text: text, Blocks: benchRich(text)})
	}
	return ok("messages", out, "has_more", false), true
}

// benchSentByBot: an app posted message i with a person's token (one in
// every fifty, and one near the bottom so it's on screen when the channel
// opens).
func benchSentByBot(i int) bool { return i%50 == 30 || i == BenchMessages-4 }
