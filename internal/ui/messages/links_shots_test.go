package messages

// Message links in a channel, on compact_shots_test.go's synthetic
// #release (made-up people, fakeslack's pictures; nothing talks to
// Slack): two message_mention slugs back to back (the blocks Slack's
// composer makes from two pasted permalinks: no text between them, so
// the pills sit flush, as Slack's rich text renderer Y5SQ and
// MessageMention p9lB draw them), and a link above a message unfurl
// card whose author's real name is a lone zero-width space: the card
// shows the member's display name and picture (MemberLink,
// ConnectedBaseAvatar), not author_name / author_icon, and nothing in it
// is underlined, also with Accessibility > "Underline links to
// websites", which underlines the message's link only. With
// MESSAGES_AUDIT_SHOTS_DIR set (the messages-audit-shots workflow) each
// state is saved, light and dark.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

const linksDee = "UDEE00001" // display name "dee", real name a zero-width space

func linksPermalink(ts string) string {
	return "https://fakeclub.slack.com/archives/" + tShots + "/p" + strings.ReplaceAll(ts, ".", "")
}

// linksMessages adds dee and three messages after the fixture's: dee's
// note (lk1), ana's two slugs (lk2: lk1 and the thread root) and ben's
// link with the unfurl of lk1 (lk3). It returns their ts.
func linksMessages(t *testing.T, s *store.Store, root string) map[string]string {
	t.Helper()
	s.PutUsers([]slack.User{{ID: linksDee, Name: "dee", RealName: "​",
		Profile: slack.Profile{DisplayName: "dee", RealName: "​", Image48: shotAvatar(linksDee), Image72: shotAvatar(linksDee)}}})
	day := time.Now()
	at := func(h, m int) string {
		return fmt.Sprintf("%d.000100", time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local).Unix())
	}
	ts := map[string]string{"lk1": at(9, 54), "lk2": at(9, 55), "lk3": at(9, 56)}
	u1, u2 := linksPermalink(ts["lk1"]), linksPermalink(root)
	raw := []string{
		`{"type":"message","ts":"` + ts["lk1"] + `","user":"` + linksDee + `","text":"Shipping notes are up for 4.2",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Shipping notes are up for 4.2"}]}]}]}`,
		`{"type":"message","ts":"` + ts["lk2"] + `","user":"UANA00001","text":"Both threads: <` + u1 + `><` + u2 + `> for context",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"Both threads: "},
			{"type":"message_mention","channel_id":"` + tShots + `","message_ts":"` + ts["lk1"] + `","author_id":"` + linksDee + `","text":"` + u1 + `","url":"` + u1 + `"},
			{"type":"message_mention","channel_id":"` + tShots + `","message_ts":"` + root + `","author_id":"UANA00001","text":"` + u2 + `","url":"` + u2 + `"},
			{"type":"text","text":" for context"}]}]}]}`,
		`{"type":"message","ts":"` + ts["lk3"] + `","user":"UBEN00001","text":"<https://example.invalid/community-log|View community log>",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[
			{"type":"link","url":"https://example.invalid/community-log","text":"View community log"}]}]}],
			"attachments":[{"id":1,"is_msg_unfurl":true,"author_id":"` + linksDee + `","author_name":"​","author_subname":"​",
			"author_icon":"https://avatars.invalid/` + linksDee + `-48.png","author_link":"https://fakeclub.slack.com/team/` + linksDee + `",
			"channel_id":"` + tShots + `","from_url":"` + u1 + `","original_url":"` + u1 + `","ts":"` + ts["lk1"] + `",
			"fallback":"[9:54 AM] : Shipping notes are up for 4.2","footer":"Slack Conversation","mrkdwn_in":["text"],"text":"Shipping notes are up for 4.2",
			"blocks":[{"type":"rich_text","block_id":"b","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Shipping notes are up for 4.2"}]}]}]}]}`,
	}
	var msgs []slack.Message
	if tl := s.Timeline(tShots); tl != nil {
		msgs = append(msgs, tl.Msgs...)
	}
	for _, r := range raw {
		var m slack.Message
		if err := json.Unmarshal([]byte(r), &m); err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}
	s.SetLatest(tShots, msgs, false)
	return ts
}

func TestLinksShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	t.Cleanup(uipref.Reset)
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, ul := range []bool{false, true} {
			name := "links-chips-card-" + mode
			if ul {
				name += "-underline-pref"
			}
			uipref.Set(store.Prefs{UnderlineLinks: ul})
			app, fs, fts := shotsApp(t)
			ts := linksMessages(t, app.S, fts["root"])
			th := theme.New(dark)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
			const w, h = 1100, 1500
			v := NewChannelView(host, tShots)
			v.Preview = true
			ops := new(op.Ops)
			f := func() { shotFrame(v, th, w, h, ops) }
			loadList(v, f)
			until(t, 5*time.Second, "the link messages", f, func() bool {
				return v.s.rows[ts["lk3"]] != nil && itemHeight(v.s, ts["lk3"]) > 0 && v.s.rows[ts["lk1"]] != nil
			})
			for i := 0; i < 60; i++ { // pictures
				f()
				time.Sleep(10 * time.Millisecond)
			}
			f()
			// the card draws dee (display name, own picture); the slugs say dee and ana
			var card *richtext.Attachment
			for _, b := range v.s.rows[ts["lk3"]].m.Doc.Blocks {
				if b.Att != nil && b.Att.MsgCard {
					card = b.Att
				}
			}
			if card == nil || !card.CardMember || card.CardAuthor != "dee" || card.CardAvatar != shotAvatar(linksDee) {
				t.Fatalf("%s: the unfurl card's author %+v, want the member dee", name, card)
			}
			var labels []string
			for _, b := range v.s.rows[ts["lk2"]].m.Doc.Blocks {
				for _, sp := range b.Spans {
					if sp.Kind == richtext.MessageMention {
						labels = append(labels, sp.Text)
					}
				}
			}
			if strings.Join(labels, "|") != "dee in #release|ana in #release" {
				t.Errorf("%s: slugs %q", name, labels)
			}
			img := pixeltest.Render(t, w, h, ops)
			y0, y1 := -1, 0
			for i, it := range v.s.items {
				if it.kind != itMsg || i >= len(v.s.pos) || i >= len(v.s.heights) {
					continue
				}
				switch it.key {
				case ts["lk1"]:
					y0 = v.s.pos[i]
				case ts["lk3"]:
					y1 = v.s.pos[i] + v.s.heights[i]
				}
			}
			if y0 < 0 || y1 <= y0 {
				t.Fatalf("%s: rows at %d..%d", name, y0, y1)
			}
			o := v.s.origin.Y
			savePNG(t, filepath.Join(dir, name+".png"), crop(img, max(y0+o-8, 0), min(y1+o+8, h)))
		}
	}
}
