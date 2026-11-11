package richtext

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only (scraping policy). Shapes from the live
// message_mention and is_msg_unfurl payloads (2026-11-08, keys only).

type kindRes struct{ fakeRes }

var kinds = map[string]string{"C1": "public", "G1": "private", "D1": "im", "M1": "mpim"}

func (kindRes) ChannelName(id string) string {
	return map[string]string{"C1": "general", "G1": "secret", "D1": "alice", "M1": "mpdm-alice--bob--me-1"}[id]
}
func (kindRes) ConvKind(id string) (string, bool) { k, ok := kinds[id]; return k, ok }

func TestMessageMentionLabel(t *testing.T) {
	cases := []struct {
		ch, author string
		label      string
		lock, off  bool
	}{
		{"C1", "U1", "alice in #general", false, false},
		{"G1", "U1", "alice in secret", true, false},
		{"D1", "U1", "alice", false, false},
		{"D1", "USELF", "me with alice", false, false},
		{"M1", "U1", "alice with alice, bob, me", false, false},
		{"C1", "U404", "Unknown author in #general", false, false},
		{"CNOPE", "U1", "Private message", false, true},
	}
	for _, c := range cases {
		label, lock, pre, off := MessageMentionLabel(kindRes{}, c.ch, c.author)
		if label != c.label || lock != c.lock || off != c.off {
			t.Errorf("%s/%s: %q lock=%v off=%v, want %q %v %v", c.ch, c.author, label, lock, off, c.label, c.lock, c.off)
		}
		if lock && label[pre:] != "secret" {
			t.Errorf("lock split %q at %d", label, pre)
		}
	}
}

func TestMessageMentionElement(t *testing.T) {
	var m slack.Message
	_ = json.Unmarshal([]byte(`{"ts":"2.0","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"text","text":"see "},
		{"type":"message_mention","message_ts":"1700000000.000100","channel_id":"C1","text":"https://ws.slack.com/archives/C1/p1700000000000100","url":"https://ws.slack.com/archives/C1/p1700000000000100","author_id":"U1"}]}]}]}`), &m)
	d := FromMessage(kindRes{}, &m)
	want := []Block{para(tx("see "), Span{Kind: MessageMention, Text: "alice in #general", URL: "https://ws.slack.com/archives/C1/p1700000000000100", ID: "1700000000.000100"})}
	eq(t, "mention", d.Blocks, want)
	eq(t, "mention plain", PlainText(d), "see alice in #general")
}

func TestMessageUnfurlCard(t *testing.T) {
	att := func(extra string) Block {
		var m slack.Message
		raw := `{"ts":"2.0","text":"x","attachments":[{"id":1,"is_msg_unfurl":true,"author_id":"U1","author_name":"alice","author_subname":"Alice A",
			"author_icon":"https://i.test/a.png","author_link":"https://ws.slack.com/team/U1","channel_team":"T1","fallback":"[x] alice: hi",
			"footer":"Slack Conversation","from_url":"https://ws.slack.com/archives/C1/p1700000000000100","original_url":"https://ws.slack.com/archives/C1/p1700000000000100",
			"mrkdwn_in":["text"],"text":"hi","ts":"1700000000.000100",
			"blocks":[{"type":"rich_text","block_id":"b","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"hi from blocks"}]}]}]` + extra + `}]}`
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return FromMessage(kindRes{}, &m).Blocks[1]
	}
	a := att(`,"channel_id":"C1"`).Att
	if !a.MsgCard || a.AuthorName != "alice" || a.AuthorID != "U1" || a.MsgTS != "1700000000.000100" || a.Footer != nil || a.Color != "" {
		t.Fatalf("card: %+v", a)
	}
	eq(t, "text", a.Text, []Block{para(tx("hi from blocks"))})
	eq(t, "posted", a.Context, []Span{tx("Posted in "), {Kind: Channel, Text: "general", ID: "C1"}})
	if a.ContextLink != a.FromURL || a.ContextLock {
		t.Errorf("link %q lock %v", a.ContextLink, a.ContextLock)
	}
	if a := att(`,"channel_id":"G1"`).Att; !a.ContextLock {
		t.Error("private channel: lock")
	}
	eq(t, "reply", att(`,"channel_id":"C1","is_reply_unfurl":true`).Att.Context, []Span{tx("From a thread in "), {Kind: Channel, Text: "general", ID: "C1"}})
	eq(t, "root", att(`,"channel_id":"C1","is_thread_root_unfurl":true`).Att.Context, []Span{tx("Thread in "), {Kind: Channel, Text: "general", ID: "C1"}})
	eq(t, "dm", att(`,"channel_id":"D1"`).Att.Context, []Span{tx("Direct Message")})
	eq(t, "mpim", att(`,"channel_id":"M1"`).Att.Context, []Span{tx("Direct Message")})
	if a := att(`,"channel_id":"CNOPE"`).Att; a.ContextLink != "" || len(a.Context) != 1 || a.Context[0].Text != "From a private conversation" {
		t.Errorf("unknown channel: %+v", a.Context)
	}
	if a := att(``).Att; a.Context[0].Text != "From another organization" {
		t.Errorf("no channel: %+v", a.Context)
	}
}

// memberRes adds Slack's member names (MemberNamer) to kindRes: U1's
// display name is "ally" and its real name a lone zero-width space, as
// some people set theirs; U404 isn't loaded.
type memberRes struct{ kindRes }

func (memberRes) MemberName(id string) (string, bool) {
	if id == "U1" {
		return "ally", true
	}
	return "", false
}

func (memberRes) MemberAvatar(id string) string {
	if id == "U1" {
		return "https://avatars.test/U1-72.png"
	}
	return ""
}

// The slug's author is getMessageSenderNameFromState's member name
// (MemberNamer), not the resolver's plain UserName.
func TestMessageMentionLabelMemberName(t *testing.T) {
	if label, _, _, _ := MessageMentionLabel(memberRes{}, "C1", "U1"); label != "ally in #general" {
		t.Errorf("label %q, want the member's name", label)
	}
	if label, _, _, _ := MessageMentionLabel(memberRes{}, "C1", "U404"); label != "Unknown author in #general" {
		t.Errorf("unknown member: %q", label)
	}
	if label, _, _, _ := MessageMentionLabel(kindRes{}, "C1", "U1"); label != "alice in #general" {
		t.Errorf("without MemberNamer: %q", label)
	}
}

// A message unfurl with an author_id draws the member: their name and
// picture (CardMember), while author_name / author_icon (the server's
// real name, a zero-width space here, and a 48px picture) stay as the
// attachment had them for plugins.
func TestMessageUnfurlCardMember(t *testing.T) {
	card := func(r Resolver, author string) *Attachment {
		var m slack.Message
		raw := `{"ts":"2.0","text":"x","attachments":[{"id":1,"is_msg_unfurl":true,"author_id":"` + author + `","author_name":"​",
			"author_subname":"​","author_icon":"https://avatars.test/U1-48.png","channel_id":"C1",
			"from_url":"https://ws.slack.com/archives/C1/p1700000000000100","ts":"1700000000.000100","text":"hi"}]}`
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		for _, b := range FromMessage(r, &m).Blocks {
			if b.Att != nil {
				return b.Att
			}
		}
		t.Fatal("no attachment block")
		return nil
	}
	a := card(memberRes{}, "U1")
	if !a.MsgCard || !a.CardMember || a.CardAuthor != "ally" || a.CardAvatar != "https://avatars.test/U1-72.png" {
		t.Errorf("member card: member %v author %q avatar %q", a.CardMember, a.CardAuthor, a.CardAvatar)
	}
	if a.AuthorName != "​" || a.AuthorIcon != "https://avatars.test/U1-48.png" {
		t.Errorf("the attachment's own author fields changed: %q %q", a.AuthorName, a.AuthorIcon)
	}
	if a := card(memberRes{}, "U404"); a.CardMember || a.CardAuthor != "" {
		t.Errorf("unknown member: %+v", a)
	}
	if a := card(kindRes{}, "U1"); a.CardMember {
		t.Error("a resolver without MemberNamer marked the card's member")
	}
}
