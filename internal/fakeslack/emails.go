package fakeslack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Email file fixtures: Size.Emails adds #email-inbox, three messages the
// Email app posted (Slack's shape, read from Hack Club's on 2026-11-18:
// the message has no text, upload true, user USLACKBOT with
// display_as_bot and the app's bot_id; the file is mode "email",
// filetype "email", text/html, pretty_type "Email", with from, to (the
// channel's address: an empty address and the channel's id as the name),
// cc, subject, plain_text, preview (HTML), preview_plain_text, headers,
// email_security, attachments and the app's ids):
//
//   - EmailPlain: a short plain email (a 150px body inline);
//   - EmailHTML: an HTML newsletter (heading, bold, a link, a list, a
//     layout table) from a member (from's slack_user_id), cc'ing an
//     address, whose sender failed DMARC (the "Heads up" warning);
//   - EmailAttachments: an email with two attachments kept as Slack
//     files (slack_file_id).
//
// files.info answers for the emails (simplified_html: the whole HTML) and
// their attachments; bots.info names the Email app's bot. All text is
// made up.

const (
	EmailsChannelID = "C0EMAIL0001"
	emailsBaseTS    = 1762800000
	EmailsBotID     = "B0FAKEMAIL"
	EmailsAppID     = "A0FAKEMAIL"

	EmailPlain       = "plain"
	EmailHTML        = "html"
	EmailAttachments = "attachments"
)

// EmailsCase is one #email-inbox message.
type EmailsCase struct {
	Name, TS, FileID string
}

// EmailsCases are #email-inbox's messages, oldest first.
func (s *Server) EmailsCases() []EmailsCase {
	var out []EmailsCase
	for i, f := range s.emailFixtures() {
		out = append(out, EmailsCase{Name: f.name, TS: emailsTS(i), FileID: f.file.ID})
	}
	return out
}

func emailsTS(i int) string { return strconv.Itoa(emailsBaseTS+i*60) + ".000200" }

type emailFixture struct {
	name string
	file slack.File
	html string // simplified_html: the whole email
}

func valid(b bool) *slack.EmailSecurity { return &slack.EmailSecurity{HasValidDMARC: &b} }

func (s *Server) emailFixtures() []emailFixture {
	file := func(id, subject, plain, html string, i int) slack.File {
		ts := int64(emailsBaseTS + i*60)
		pv := html
		if len(pv) > 2800 {
			pv = pv[:2800]
		}
		ppt := plain
		if len([]rune(ppt)) > 500 {
			ppt = string([]rune(ppt)[:500])
		}
		return slack.File{ID: id, Name: subject, Title: subject, Mimetype: "text/html", Filetype: "email", PrettyType: "Email",
			Mode: "email", Size: int64(len(html)), User: SlackbotID, Created: ts, Timestamp: ts - 4,
			URLPrivate:         s.URL + "files-pri/" + TeamID + "-" + id + "/email",
			URLPrivateDownload: s.URL + "files-pri/" + TeamID + "-" + id + "/download/email",
			Permalink:          "https://fakeclub.slack.com/files/" + SlackbotID + "/" + id + "/email",
			Subject:            subject, PlainText: plain, PreviewPlainText: ppt, Preview: pv,
			To:           slack.EmailAddresses{{Address: "", Name: EmailsChannelID, Original: "email-inbox"}},
			EmailHeaders: &slack.EmailHeaders{Date: "Tue, 11 Nov 2025 10:00:00 +0000", MessageID: "<" + id + "@mail.example>"},
			AppID:        EmailsAppID, BotID: EmailsBotID, EmailSecurity: valid(true)}
	}
	plain := file("F0EMAIL0001", "Lunch on Friday?", "Hi team,\nAnyone up for lunch on Friday at noon?\nAlex",
		"<div>Hi team,</div><div>Anyone up for lunch on Friday at noon?</div><div>Alex</div>", 0)
	plain.From = slack.EmailAddresses{{Address: "alex@mail.example", Name: "Alex Rivera", Original: "Alex Rivera <alex@mail.example>"}}

	news := strings.Join([]string{
		"<html><head><style>p{color:red}</style><title>ignored</title></head><body>",
		"<h1>The Weekly Build</h1>",
		"<p>Hello <b>makers</b>, here is what shipped this week. Read the <a href=\"https://example.com/notes\">full notes</a>.</p>",
		"<ul><li>A faster sidebar</li><li>Email previews</li><li>Fewer bugs</li></ul>",
		"<table><tr><td>Builds</td><td>42</td></tr><tr><td>Reviews</td><td>17</td></tr></table>",
		"<p style=\"display:none\">tracking pixel text</p>",
		"<p>See you next week,<br>The Build Team</p>",
		"</body></html>"}, "")
	newsPlain := "The Weekly Build\n\nHello makers, here is what shipped this week. Read the full notes (https://example.com/notes).\n\n" +
		"* A faster sidebar\n* Email previews\n* Fewer bugs\n\nBuilds 42\nReviews 17\n\nSee you next week,\nThe Build Team"
	html := file("F0EMAIL0002", "The Weekly Build: what shipped", newsPlain, news, 1)
	html.From = slack.EmailAddresses{{Address: "news@build.example", Name: s.users[1%len(s.users)].RealName,
		Original: "news@build.example", SlackUserID: s.users[1%len(s.users)].ID}}
	html.Cc = slack.EmailAddresses{{Address: "team@build.example", Name: "", Original: "team@build.example"}}
	html.EmailSecurity = valid(false)

	att := file("F0EMAIL0003", "Invoice and receipt", "Hello,\nThe invoice and the receipt are attached.\nThanks",
		"<p>Hello,</p><p>The invoice and the receipt are attached.</p><p>Thanks</p>", 2)
	att.From = slack.EmailAddresses{{Address: "billing@shop.example", Name: "Shop Billing", Original: "Shop Billing <billing@shop.example>"}}
	att.EmailAttachments = slack.EmailAttachments{
		{Filename: "invoice-2025-11.pdf", Size: 48213, Mimetype: "application/pdf", URL: s.URL + "files-pri/" + TeamID + "-F0EMATT0001/invoice-2025-11.pdf", SlackFileID: "F0EMATT0001"},
		{Filename: "receipt.txt", Size: 912, Mimetype: "text/plain", URL: s.URL + "files-pri/" + TeamID + "-F0EMATT0002/receipt.txt", SlackFileID: "F0EMATT0002"},
	}
	att.OriginalAttachmentCount = 2
	return []emailFixture{{EmailPlain, plain, plain.Preview}, {EmailHTML, html, news}, {EmailAttachments, att, att.Preview}}
}

// EmailsHistory is #email-inbox's history, newest first.
func (s *Server) EmailsHistory() []slack.Message {
	fx := s.emailFixtures()
	out := make([]slack.Message, 0, len(fx))
	for i := len(fx) - 1; i >= 0; i-- {
		f := fx[i].file
		out = append(out, slack.Message{Type: "message", TS: emailsTS(i), User: SlackbotID, BotID: EmailsBotID, DisplayAsBot: true,
			Upload: true, Files: []slack.File{f}})
	}
	return out
}

func (s *Server) addEmailsChannel() {
	s.convs = append(s.convs, slack.Conversation{ID: EmailsChannelID, Name: "email-inbox", IsChannel: true, IsMember: true,
		NumMembers: 4, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Email fixtures."}})
	s.hist[EmailsChannelID] = s.EmailsHistory()
}

// handleEmails answers files.info for the emails and their attachments,
// files.getShares for the emails, and bots.info for the Email app's bot.
func (s *Server) handleEmails(method string, f url.Values) (any, bool) {
	if !s.Size.Emails {
		return nil, false
	}
	switch method {
	case "files.info":
		id := f.Get("file")
		for _, x := range s.emailFixtures() {
			if x.file.ID == id {
				file := x.file
				file.SimplifiedHTML = x.html
				return ok("file", file, "comments", []any{}), true
			}
			for _, a := range x.file.EmailAttachments {
				if a.SlackFileID == id {
					ft := strings.TrimPrefix(a.Filename[strings.LastIndexByte(a.Filename, '.')+1:], ".")
					return ok("file", slack.File{ID: id, Name: a.Filename, Title: a.Filename, Mimetype: a.Mimetype, Filetype: ft,
						PrettyType: strings.ToUpper(ft), Size: a.Size, Mode: "hosted", User: SlackbotID, URLPrivate: a.URL}, "comments", []any{}), true
				}
			}
		}
	case "files.getShares":
		// the email's one share, its #email-inbox message (the shape Hack
		// Club answers: core/file_shares.go)
		for i, x := range s.emailFixtures() {
			if x.file.ID == f.Get("file_id") {
				share := map[string]any{"ts": emailsTS(i), "channel_name": "email-inbox", "team_id": TeamID, "share_user_id": SlackbotID,
					"source": "UNKNOWN", "is_silent_share": false, "reply_users": []string{}, "reply_users_count": 0, "reply_count": 0}
				none := map[string]any{"count": 0, "shares": map[string]any{}}
				return ok("conversation_shares", map[string]any{"count": 1, "shares": map[string]any{"public": map[string]any{EmailsChannelID: []any{share}}}},
					"tab_shares", none, "file_channel_shares", none), true
			}
		}
	case "bots.info":
		for _, id := range strings.Split(f.Get("bots")+","+f.Get("bot"), ",") {
			if id == EmailsBotID {
				return map[string]any{"ok": true, "bots": []slack.BotProfile{{ID: EmailsBotID, Name: "Email", AppID: EmailsAppID, TeamID: TeamID}}}, true
			}
		}
	}
	return nil, false
}
