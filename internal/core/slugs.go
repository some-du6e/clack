package core

// Link slugs: Slack's composer turns pasted Slack links into objects
// (gantry-v2 2026-11, TextySlugs.processSlugs): links whose text is the
// URL become unprocessed slugs, chat.slugifyUrl {urls (≤10), channel,
// reason: "link-paste-slugify"} resolves them, and the answer by
// slug_type becomes a message slug (sent as message_mention), a
// #channel or an @user mention. Anything else stays a link. A message
// slug still unresolved at send time is resolved from the message cache
// (withUnprocessedMessageSlugsResolved).
//
// chat.slugifyUrl answers (live shapes 2026-11-08, keys only):
//
//	message: {slug_type, author_id, channel_id, ts, message{…}}
//	channel: {slug_type, id, name, channel{…}}
//	user:    {slug_type, id, name, user{…}}

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Slug is one resolved link.
type Slug struct {
	Type      string // message | channel | user
	ID        string // message ts, channel id or user id
	Name      string // channel / user name (channel, user)
	ChannelID string // message
	AuthorID  string // message
	ThreadTS  string // message: the thread it is in, if a reply
	IsIM      bool   // channel: a DM or group DM (Slack drops the "#")
	IsMPIM    bool
}

// Permalink is a parsed Slack link.
type Permalink struct {
	Kind      string // message | channel | user
	ChannelID string
	TS        string // message ts ("p1700000000000100" → "1700000000.000100")
	ThreadTS  string // ?thread_ts= of a reply link
	UserID    string
}

var (
	permalinkMsg  = regexp.MustCompile(`^/archives/([CDG][A-Z0-9]+)/p(\d{10})(\d{6})$`)
	permalinkChan = regexp.MustCompile(`^/archives/([CDG][A-Z0-9]+)/?$`)
	permalinkUser = regexp.MustCompile(`^/team/([UW][A-Z0-9]+)/?$`)
)

// IsSlackURL reports whether u is on a Slack host (*.slack.com), the
// links the composer offers chat.slugifyUrl.
func IsSlackURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") {
		return false
	}
	h := strings.ToLower(p.Hostname())
	return h == "slack.com" || strings.HasSuffix(h, ".slack.com")
}

// ParsePermalink parses a message, channel or user link on a Slack host.
func ParsePermalink(u string) (Permalink, bool) {
	if !IsSlackURL(u) {
		return Permalink{}, false
	}
	p, _ := url.Parse(u)
	if m := permalinkMsg.FindStringSubmatch(p.Path); m != nil {
		pl := Permalink{Kind: "message", ChannelID: m[1], TS: m[2] + "." + m[3]}
		if t := p.Query().Get("thread_ts"); t != "" && t != pl.TS {
			pl.ThreadTS = t
		}
		return pl, true
	}
	if m := permalinkChan.FindStringSubmatch(p.Path); m != nil {
		return Permalink{Kind: "channel", ChannelID: m[1]}, true
	}
	if m := permalinkUser.FindStringSubmatch(p.Path); m != nil {
		return Permalink{Kind: "user", UserID: m[1]}, true
	}
	return Permalink{}, false
}

// CachedSlug resolves a message link from the messages core already has
// (Slack's send-time fallback): ok is false when the message isn't cached.
func (a *App) CachedSlug(u string) (Slug, bool) {
	pl, ok := ParsePermalink(u)
	if !ok || pl.Kind != "message" {
		return Slug{}, false
	}
	m, ok := a.S.Find(pl.ChannelID, pl.TS)
	if !ok || m.User == "" {
		return Slug{}, false
	}
	thread := pl.ThreadTS
	if thread == "" && m.ThreadTS != "" && m.ThreadTS != m.TS {
		thread = m.ThreadTS
	}
	return Slug{Type: "message", ID: pl.TS, ChannelID: pl.ChannelID, AuthorID: m.User, ThreadTS: thread}, true
}

// SlugifyURLs is chat.slugifyUrl for up to 10 pasted links in channel:
// the links Slack resolved, by URL. The users and conversations it
// returns are cached, so labels name them.
func (a *App) SlugifyURLs(ctx context.Context, urls []string, channel string) (map[string]Slug, error) {
	if len(urls) > 10 {
		urls = urls[:10]
	}
	b, _ := json.Marshal(urls)
	p := url.Values{"urls": {string(b)}, "reason": {"link-paste-slugify"}}
	if channel != "" {
		p.Set("channel", channel)
	}
	var res struct {
		Slugs map[string]struct {
			Type      string              `json:"slug_type"`
			ID        string              `json:"id"`
			Name      string              `json:"name"`
			AuthorID  string              `json:"author_id"`
			ChannelID string              `json:"channel_id"`
			TS        string              `json:"ts"`
			Message   *slack.Message      `json:"message"`
			Channel   *slack.Conversation `json:"channel"`
			User      *slack.User         `json:"user"`
		} `json:"slugs"`
	}
	if err := a.C.Call(ctx, "chat.slugifyUrl", p, &res); err != nil {
		return nil, err
	}
	out := map[string]Slug{}
	for u, s := range res.Slugs {
		switch s.Type {
		case "message":
			sl := Slug{Type: "message", ID: s.TS, ChannelID: s.ChannelID, AuthorID: s.AuthorID}
			if m := s.Message; m != nil {
				sl.ID = firstNonEmpty(sl.ID, m.TS)
				sl.AuthorID = firstNonEmpty(sl.AuthorID, m.User)
				if m.ThreadTS != "" && m.ThreadTS != m.TS {
					sl.ThreadTS = m.ThreadTS
				}
			}
			if pl, ok := ParsePermalink(u); ok && sl.ThreadTS == "" {
				sl.ThreadTS = pl.ThreadTS
			}
			if sl.ID == "" || sl.ChannelID == "" {
				continue
			}
			out[u] = sl
		case "channel":
			id := firstNonEmpty(s.ID, func() string {
				if s.Channel != nil {
					return s.Channel.ID
				}
				return ""
			}())
			if id == "" {
				continue
			}
			sl := Slug{Type: "channel", ID: id, Name: s.Name}
			if c := s.Channel; c != nil {
				sl.IsIM, sl.IsMPIM = c.IsIM, c.IsMPIM
				if _, known := a.S.Conv(c.ID); !known {
					a.S.PutExtConv(*c)
				}
				sl.Name = firstNonEmpty(sl.Name, c.Name)
			}
			out[u] = sl
		case "user":
			id := s.ID
			if s.User != nil {
				id = firstNonEmpty(id, s.User.ID)
				a.S.PutUsers([]slack.User{*s.User})
			}
			if id == "" {
				continue
			}
			out[u] = Slug{Type: "user", ID: id, Name: s.Name}
		}
	}
	return out, nil
}
