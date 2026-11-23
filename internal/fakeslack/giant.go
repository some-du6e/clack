package fakeslack

// #giant-wall (Size.Giant): a channel whose latest messages are each about
// 908,000 characters of plain text in about 92,000 lines, the shape of the
// messages in the channel Heliodex reported (2026-11-20: opening it took
// Clack to 13 GB). Only the shape was probed; the text here is generated
// (the scraping policy: no message content is fetched or kept).
//
//   - Of the latest Size.Giant messages, every twelfth is a short line of
//     words and the rest are giant (100: 92 giant ones, as the real
//     channel's latest 100), then 60 older short ones, so a client paging
//     back still finds more (has_more).
//   - A giant message is plain ASCII: 32 distinct characters (the
//     lowercase letters, a hyphen, four punctuation marks and the
//     newline), 907,928 to 908,048 characters in about 92,000 lines of
//     one word each. Its blocks are one rich_text block holding one
//     rich_text_section with one text element; its text field is the
//     same text. It is from a user, with no files, attachments,
//     reactions or thread.
//
// The memory test opens it (internal/ui TestGiantMessagesBounded).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// GiantWallID is #giant-wall's id (sorted after every C0FAKE channel, so
// the drivers' "first channels" don't move).
const GiantWallID = "C0GIANTWALL1"

// giantOlder is how many short messages #giant-wall has before its latest
// Size.Giant.
const giantOlder = 60

// GiantShort reports whether the k-th newest of #giant-wall's latest
// messages (k from 0) is a short one rather than giant.
func GiantShort(k int) bool { return k%12 == 11 }

// GiantChars is the length of the k-th newest message's text when it is
// giant: 907,928 to 908,048.
func GiantChars(k int) int { return 907928 + (k*53)%121 }

// GiantText is the text of the k-th newest of #giant-wall's latest
// messages when it is giant: GiantChars(k) characters of one-word lines
// (4 to 16 characters), deterministic in k.
func GiantText(k int) string {
	n := GiantChars(k)
	var b strings.Builder
	b.Grow(n + 32)
	x := uint64(k)*0x9e3779b97f4a7c15 + 0x2545f4914f6cdd1d
	next := func() uint64 { // xorshift64
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return x
	}
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const punct = "',.!"
	for b.Len() < n {
		r := next()
		l := 4 + int(r%10)
		if (r>>8)%8 == 0 {
			l += 3
		}
		hy := -1
		if (r>>16)%16 == 0 {
			hy = 1 + int((r>>20)%uint64(l-2))
		}
		pu := (r>>28)%20 == 0
		for i := 0; i < l; i++ {
			switch {
			case i == hy:
				b.WriteByte('-')
			case pu && i == l-1:
				b.WriteByte(punct[(r>>32)%4])
			default:
				b.WriteByte(letters[next()%26])
			}
		}
		b.WriteByte('\n')
	}
	s := b.String()[:n]
	if s[n-1] == '\n' { // a message never ends with its newline
		s = s[:n-1] + "z"
	}
	return s
}

// giantBlocks is a rich_text message's blocks holding text: one block,
// one section, one text element (the shape Slack's composer sends).
func giantBlocks(id, text string) json.RawMessage {
	t, _ := json.Marshal(text)
	var b strings.Builder
	b.Grow(len(t) + 128)
	b.WriteString(`[{"type":"rich_text","block_id":"`)
	b.WriteString(id)
	b.WriteString(`","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":`)
	b.Write(t)
	b.WriteString(`}]}]}]`)
	return json.RawMessage(b.String())
}

// GiantTS is the ts of the k-th newest of #giant-wall's latest messages.
func GiantTS(k int) string {
	base := time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC).Unix()
	return fmt.Sprintf("%d.%06d", base-int64(k)*60, 300)
}

// addGiantWall adds #giant-wall with Size.Giant latest messages.
func (s *Server) addGiantWall() {
	s.convs = append(s.convs, slack.Conversation{ID: GiantWallID, Name: "giant-wall", IsChannel: true, IsMember: true,
		Created: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix(), NumMembers: 400, ContextTeamID: TeamID,
		Purpose: &slack.Purpose{Value: "Very long messages. Synthetic."}})
	n := s.Size.Giant
	msgs := make([]slack.Message, 0, n+giantOlder)
	short := func(k int) string {
		var w []string
		for j := 0; j < 6; j++ {
			w = append(w, s.words[(k*7+j*13)%len(s.words)])
		}
		return strings.Join(w, " ")
	}
	for k := 0; k < n+giantOlder; k++ { // newest first
		u := s.users[(k*7+3)%len(s.users)].ID
		m := slack.Message{Type: "message", TS: GiantTS(k), User: u, ClientMsgID: fmt.Sprintf("00000000-0000-4000-8000-%012d", k)}
		if k < n && !GiantShort(k) {
			m.Text = GiantText(k)
		} else {
			m.Text = short(k)
		}
		m.Blocks = giantBlocks(fmt.Sprintf("g%04d", k), m.Text)
		msgs = append(msgs, m)
	}
	s.hist[GiantWallID] = msgs
}
