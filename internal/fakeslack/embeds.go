package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// Link preview fixtures: Size.Embeds adds #embeds-gallery, one message
// per way Slack draws a link unfurl (docs/EMBEDS.md): an X post (the
// shape Slack's unfurler sends: service X (formerly Twitter), a 1200x675
// image, multi-line text with emoji), a YouTube video (video_html,
// thumb_url), a GitHub app unfurl (author, fields, footer, color, a
// bot's), a site with a small thumb, a site with a large image, a
// text-only site, an image-only link, a Block Kit unfurl and a message
// linking two PNGs (two image-only attachments, Slack's shape for bare
// image links: from_url = original_url = image_url). The viewer
// sends the X, small-thumb and text-only ones (their previews get the
// remove button). Every image is this server's /img gradient; all text is
// made up. chat.deleteAttachment removes the attachment and is recorded.

const (
	EmbedsChannelID = "C0EMBEDS001"
	embedsBaseTS    = 1762700000
)

// EmbedsCase is one #embeds-gallery message.
type EmbedsCase struct {
	Name string
	TS   string
}

// EmbedsFixture is one #embeds-gallery message: User is an index into
// the fake's users, -1 the viewer.
type EmbedsFixture struct {
	Name, Text  string
	User        int
	Attachments []slack.Attachment
}

func embedsTS(i int) string { return strconv.Itoa(embedsBaseTS+i*60) + ".000200" }

// EmbedsCases are the #embeds-gallery messages in channel order.
func (s *Server) EmbedsCases() []EmbedsCase {
	var out []EmbedsCase
	for i, f := range EmbedsFixtures(s.URL) {
		out = append(out, EmbedsCase{Name: f.Name, TS: embedsTS(i)})
	}
	return out
}

// DeletedAttachment is one chat.deleteAttachment call.
type DeletedAttachment struct{ Channel, TS, Attachment string }

var embedsMu sync.Mutex

// DeletedAttachments returns the chat.deleteAttachment calls so far.
func (s *Server) DeletedAttachments() []DeletedAttachment {
	embedsMu.Lock()
	defer embedsMu.Unlock()
	return append([]DeletedAttachment(nil), s.delAtts...)
}

func rawTS(n int) json.RawMessage { return json.RawMessage(strconv.Itoa(n)) }

// EmbedsFixtures are the #embeds-gallery messages, images on base's /img.
func EmbedsFixtures(base string) []EmbedsFixture {
	img := func(w, h int, seed string) string {
		return base + "img/" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + "/" + seed + ".png"
	}
	const xURL = "https://x.com/clackdevs/status/1987650000000000001"
	x := slack.Attachment{ID: 1, FromURL: xURL, OriginalURL: xURL, TitleLink: xURL,
		Title:    "Clack Devs (@clackdevs) on X",
		Text:     "November was a big month for Clack :maple_leaf:\n\nNative unfurls, huddles and canvases :rocket:\nThanks to everyone who filed bugs :heart:",
		Fallback: "X (formerly Twitter): Clack Devs (@clackdevs) on X", ServiceName: "X (formerly Twitter)",
		ServiceIcon: img(64, 64, "x-icon"), ImageURL: img(1200, 675, "x-post"), ImageWidth: 1200, ImageHeight: 675,
		ImageBytes: 412000, TS: rawTS(embedsBaseTS - 3600)}
	const ytURL = "https://www.youtube.com/watch?v=c1ack0000000"
	yt := slack.Attachment{ID: 1, FromURL: ytURL, OriginalURL: ytURL, TitleLink: ytURL,
		Title: "Building a native Slack client in Go", ServiceName: "YouTube", ServiceIcon: img(64, 64, "yt-icon"),
		AuthorName: "Clack Channel", AuthorLink: "https://www.youtube.com/@clack",
		ThumbURL: img(480, 360, "yt-thumb"), ThumbWidth: 480, ThumbHeight: 360,
		VideoHTML: `<iframe width="400" height="225" src="https://www.youtube.com/embed/c1ack0000000?feature=oembed&autoplay=1&iv_load_policy=3" frameborder="0" allowfullscreen title="Building a native Slack client in Go"></iframe>`,
		Fallback:  "YouTube Video: Building a native Slack client in Go"}
	const ghURL = "https://github.com/octo-org/clack/pull/42"
	gh := slack.Attachment{ID: 1, Color: "#36a64f", FromURL: ghURL, AuthorName: "octo-org/clack", AuthorIcon: img(64, 64, "gh-avatar"),
		AuthorLink: "https://github.com/octo-org/clack", Title: "#42 Draw link previews like Slack", TitleLink: ghURL,
		Text:   "Unfurls now follow the web client's attachment components.\nFixes the oversized X previews.",
		Fields: []slack.AttachmentField{{Title: "Comments", Value: "3", Short: true}, {Title: "Status", Value: "Open", Short: true}},
		Footer: "GitHub", FooterIcon: img(32, 32, "gh-footer"), TS: rawTS(embedsBaseTS - 7200), BotID: "B0FAKEGH01",
		IsAppUnfurl: true, Fallback: "[octo-org/clack] Pull request #42"}
	const smallURL = "https://news.example.com/2026/11/native-clients"
	small := slack.Attachment{ID: 1, FromURL: smallURL, OriginalURL: smallURL, TitleLink: smallURL,
		Title: "Why native clients are back", ServiceName: "Example News", ServiceIcon: img(32, 32, "news-icon"),
		Text:     "A look at the apps trading web views for GPU-drawn widgets, and what it costs to match the original pixel for pixel.",
		ThumbURL: img(150, 150, "news-thumb"), ThumbWidth: 150, ThumbHeight: 150, Fallback: "Example News: Why native clients are back"}
	const bigURL = "https://blog.example.org/posts/gio-tips"
	big := slack.Attachment{ID: 1, FromURL: bigURL, OriginalURL: bigURL, TitleLink: bigURL,
		Title: "Ten Gio tips for smooth scrolling", ServiceName: "Example Blog", ServiceIcon: img(32, 32, "blog-icon"),
		Text:     "Stable row heights, reserved image boxes and no layout loops.",
		ImageURL: img(1200, 630, "blog-hero"), ImageWidth: 1200, ImageHeight: 630, ImageBytes: 98000, Fallback: "Example Blog: Ten Gio tips"}
	const txtURL = "https://docs.example.net/guide/start"
	txt := slack.Attachment{ID: 1, FromURL: txtURL, OriginalURL: txtURL, TitleLink: txtURL,
		Title: "Getting started", ServiceName: "Example Docs", Text: "Install the app, sign in with your workspace URL and pick a theme.",
		Fallback: "Example Docs: Getting started"}
	const picURL = "https://images.example.com/sunset.png"
	pic := slack.Attachment{ID: 1, FromURL: picURL, OriginalURL: picURL, ImageURL: img(800, 600, "pic-only"), ImageWidth: 800, ImageHeight: 600,
		ImageBytes: 120000, Fallback: "800x600px image"}
	const kitURL = "https://tracker.example.com/issues/77"
	kit := slack.Attachment{ID: 1, FromURL: kitURL, BotID: "B0FAKETRK1", IsAppUnfurl: true, Color: "#1d9bd1",
		Fallback: "Issue 77: Previews are too large",
		Blocks: json.RawMessage(`[{"type":"section","block_id":"k1","text":{"type":"mrkdwn","text":"*<` + kitURL + `|Issue 77: Previews are too large>*\nOpened by tracker-bot · Priority high"},` +
			`"accessory":{"type":"image","image_url":"` + img(96, 96, "kit-acc") + `","alt_text":"tracker"}},` +
			`{"type":"context","block_id":"k2","elements":[{"type":"mrkdwn","text":"Example Tracker · updated today"}]}]`)}
	// Two links to PNGs, each unfurled the way Slack's unfurler sends a
	// bare image link: an image-only attachment whose from_url,
	// original_url and image_url are the same URL, with media_type
	// "image" (no title, text or service). Both are this server's /img.
	shot := func(id int, seed string) slack.Attachment {
		u := img(1440, 900, seed)
		return slack.Attachment{ID: id, FromURL: u, OriginalURL: u, ImageURL: u, ImageWidth: 1440, ImageHeight: 900,
			ImageBytes: 214000 + int64(id)*1000, MediaType: "image", Fallback: "1440x900px image"}
	}
	shotA, shotB := shot(1, "shot-a"), shot(2, "shot-b")
	return []EmbedsFixture{
		{"x post", "<" + xURL + ">", -1, []slack.Attachment{x}},
		{"youtube", "watch this <" + ytURL + ">", 1, []slack.Attachment{yt}},
		{"github", "<" + ghURL + ">", 2, []slack.Attachment{gh}},
		{"small thumb", "<" + smallURL + ">", -1, []slack.Attachment{small}},
		{"large image", "<" + bigURL + ">", 3, []slack.Attachment{big}},
		{"text only", "see <" + txtURL + ">", -1, []slack.Attachment{txt}},
		{"image link", "<" + picURL + ">", 4, []slack.Attachment{pic}},
		{"block kit", "<" + kitURL + ">", 5, []slack.Attachment{kit}},
		{"image links", "before <" + shotA.FromURL + "> and after <" + shotB.FromURL + ">", 1, []slack.Attachment{shotA, shotB}},
	}
}

func (s *Server) addEmbedsChannel() {
	s.convs = append(s.convs, slack.Conversation{ID: EmbedsChannelID, Name: "embeds-gallery", IsChannel: true, IsMember: true,
		NumMembers: 6, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Link preview fixtures."}})
	var msgs []slack.Message
	for i, f := range EmbedsFixtures(s.URL) {
		user := SelfID
		if f.User >= 0 {
			user = s.users[f.User%len(s.users)].ID
		}
		msgs = append(msgs, slack.Message{Type: "message", TS: embedsTS(i), User: user, Text: f.Text,
			Attachments: f.Attachments, ClientMsgID: fmt.Sprintf("%08x-embeds", i)})
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 { // history is newest first
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	s.hist[EmbedsChannelID] = msgs
}

// deleteAttachment is chat.deleteAttachment: {attachment, channel, ts}.
// Only the sender may remove a preview (cant_update_message otherwise).
func (s *Server) deleteAttachment(f url.Values) map[string]any {
	ch, ts, id := f.Get("channel"), f.Get("ts"), f.Get("attachment")
	s.histMu.Lock()
	defer s.histMu.Unlock()
	msgs := s.hist[ch]
	for i := range msgs {
		if msgs[i].TS != ts {
			continue
		}
		if msgs[i].User != SelfID {
			return map[string]any{"ok": false, "error": "cant_update_message"}
		}
		kept := msgs[i].Attachments[:0:0]
		found := false
		for _, a := range msgs[i].Attachments {
			if strconv.Itoa(a.ID) == id {
				found = true
				continue
			}
			kept = append(kept, a)
		}
		if !found {
			return map[string]any{"ok": false, "error": "attachment_not_found"}
		}
		msgs[i].Attachments = kept
		embedsMu.Lock()
		s.delAtts = append(s.delAtts, DeletedAttachment{Channel: ch, TS: ts, Attachment: id})
		embedsMu.Unlock()
		return ok()
	}
	return map[string]any{"ok": false, "error": "message_not_found"}
}
