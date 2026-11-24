package fakeslack

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

// A canvas's link cards (docs/CANVASES.md "Link cards"): Quip's export
// holds only "Link URL: <url>", and Quip's model gives each card a
// ContentLinkUnfurlCard of type SLACK_UNFURL naming its unfurl container
// message ("suo:<team>/<channel>/<ts>"), which messages.list answers.
// Every Embed gets a container in CardChannel with a synthetic app unfurl
// unless SetLinkUnfurl says otherwise.

// CardChannel is the conversation the fake's unfurl containers are in
// (not in any conversation list).
const CardChannel = "C0FAKECARD0"

// linkUnfurl is a link card's unfurl container: msg nil while
// messages.list answers without it.
type linkUnfurl struct {
	ch, ts string
	msg    *slack.Message
}

// SetLinkUnfurl makes the link cards of url name the unfurl container
// ch/ts, which messages.list answers with m (nil: answered without it, as
// Slack answers without a deleted message). m's TS is set to ts. The
// cards' sections change, as Quip's do when a card unfurls.
func (s *Server) SetLinkUnfurl(link, ch, ts string, m *slack.Message) {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.unfurls == nil {
		cs.unfurls = map[string]linkUnfurl{}
	}
	if m != nil {
		c := *m
		c.TS = ts
		m = &c
	}
	cs.unfurls[link] = linkUnfurl{ch: ch, ts: ts, msg: m}
}

// SetLinkUnfurlAt is SetLinkUnfurl for the n-th (from 0, in document
// order) of the cards of url only: two cards of one URL, each with its
// own container, as Slack gives each card its own.
func (s *Server) SetLinkUnfurlAt(link string, n int, ch, ts string, m *slack.Message) {
	s.SetLinkUnfurl(fmt.Sprintf("%s#%d", link, n), ch, ts, m)
}

// unfurlForLocked is url's container, a default one made the first time
// (cs.mu held).
func (cs *canvasStore) unfurlForLocked(link string, n int) linkUnfurl {
	if u, ok := cs.unfurls[fmt.Sprintf("%s#%d", link, n)]; ok {
		return u
	}
	if u, ok := cs.unfurls[link]; ok {
		return u
	}
	if cs.unfurls == nil {
		cs.unfurls = map[string]linkUnfurl{}
	}
	ts := fmt.Sprintf("%d.%06d", FakeUpdated, 100+len(cs.unfurls))
	u := linkUnfurl{ch: CardChannel, ts: ts, msg: defaultUnfurlContainer(link, ts)}
	cs.unfurls[link] = u
	return u
}

// defaultUnfurlContainer is an unfurled container holding a synthetic app
// unfurl of link: what Slack's fallback card draws (EmbeddedFallbackUnfurl).
func defaultUnfurlContainer(link, ts string) *slack.Message {
	host := link
	if u, err := url.Parse(link); err == nil && u.Host != "" {
		host = strings.TrimPrefix(u.Host, "www.") + u.Path
	}
	return &slack.Message{Type: "message", TS: ts, BotID: "B0FAKECARD1",
		DocumentUnfurl: &slack.DocumentUnfurl{IsUnfurled: true},
		Attachments: []slack.Attachment{{ID: 1, Color: "24292f", Title: host, TitleLink: link, FromURL: link, OriginalURL: link,
			Text: "A synthetic preview of the link.", Fallback: host, BotID: "B0FAKECARD1", IsAppUnfurl: true}}}
}

// linkCardSection is an Embed's card section, its content the card of its
// URL's container (cs.mu held), and what it holds as a sig suffix.
func (cs *canvasStore) linkCardSection(id, link string, n int) (quip.Section, string) {
	u := cs.unfurlForLocked(link, n)
	unfurled := u.msg != nil && u.msg.DocumentUnfurl != nil && u.msg.DocumentUnfurl.IsUnfurled
	card := &quip.LinkUnfurlCard{URL: quip.Ptr(link), Type: quip.Ptr(quip.CardSlackUnfurl),
		CardContent: &quip.CardContent{SlackUnfurl: &quip.CardUnfurl{SlackUnfurlID: quip.Ptr(quip.UnfurlID(TeamID, u.ch, u.ts))}},
		IsUnfurled:  quip.Ptr(unfurled)}
	sec := quip.Section{ID: quip.Ptr(id), Type: quip.Ptr(quip.TypeLinkUnfurlCard), Style: quip.Ptr(int32(0)),
		Content: &quip.Content{LinkUnfurl: card}}
	return sec, fmt.Sprintf("|card:%s/%s/%v", u.ch, u.ts, unfurled)
}

// cardContainer is the unfurl container ch/ts, when the fake holds one.
func (s *Server) cardContainer(ch, ts string) (slack.Message, bool) {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, u := range cs.unfurls {
		if u.ch == ch && u.ts == ts && u.msg != nil {
			return *u.msg, true
		}
	}
	return slack.Message{}, false
}
