package fakeslack

// The blockkit-gallery channel: one fixture channel holding a message for
// every Block Kit block and variant, for render tests and CI screenshots.
// Everything in it is synthetic.
//
// Each part of the gallery lives in its own file (gallery_layout.go for
// layout blocks; elements, surfaces, agent blocks… add theirs) and
// registers itself from init:
//
//	func init() { RegisterGallery("20-elements", galleryElements) }
//
// Parts show in name order, oldest first; a part's messages keep their
// order. A message with Replies is a thread parent (conversations.replies
// serves them).

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

const (
	GalleryChannelID   = "C0BKGALLERY"
	GalleryChannelName = "blockkit-gallery"
)

// GalleryMessage is one gallery message. Blocks and Attachments are JSON
// (empty: none). From is the sender (empty: the Deploy Bot app, AppUserID).
type GalleryMessage struct {
	Name        string // what it shows ("section/fields"); its fallback text when Text is empty
	Text        string
	Blocks      string
	Attachments string
	From        string
	Replies     []GalleryMessage
}

// GalleryEnv is what a part can build URLs and mentions from.
type GalleryEnv struct {
	URL         string   // the fake's base URL (images: URL + "img/<w>x<h>/<seed>.png")
	Users       []string // a few user ids
	Channel     string   // a member channel id
	Usergroup   string
	CustomEmoji []string // custom emoji names
}

// Img is a generated image URL of w×h.
func (e GalleryEnv) Img(w, h int, seed string) string {
	return fmt.Sprintf("%simg/%dx%d/%s.png", e.URL, w, h, seed)
}

var (
	galleryMu    sync.Mutex
	galleryParts = map[string]func(GalleryEnv) []GalleryMessage{}
)

// RegisterGallery adds a part of the gallery (call it from init).
func RegisterGallery(part string, f func(GalleryEnv) []GalleryMessage) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	if _, dup := galleryParts[part]; dup {
		panic("fakeslack: gallery part registered twice: " + part)
	}
	galleryParts[part] = f
}

// Gallery returns every gallery message in order (oldest first).
func (s *Server) Gallery() []GalleryMessage {
	galleryMu.Lock()
	names := make([]string, 0, len(galleryParts))
	for n := range galleryParts {
		names = append(names, n)
	}
	sort.Strings(names)
	fs := make([]func(GalleryEnv) []GalleryMessage, len(names))
	for i, n := range names {
		fs[i] = galleryParts[n]
	}
	galleryMu.Unlock()
	env := s.galleryEnv()
	var out []GalleryMessage
	for _, f := range fs {
		out = append(out, f(env)...)
	}
	return out
}

func (s *Server) galleryEnv() GalleryEnv {
	env := GalleryEnv{URL: s.URL, Usergroup: "S0FAKE00001"}
	for i := 1; i < min(len(s.users), 4); i++ {
		env.Users = append(env.Users, s.users[i].ID)
	}
	if len(s.convs) > 1 {
		env.Channel = s.convs[1].ID
	}
	var names []string
	for n, u := range s.emoji {
		if !strings.HasPrefix(u, "alias:") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	env.CustomEmoji = names[:min(3, len(names))]
	return env
}

// galleryConv is the gallery's conversation (the viewer is a member).
func galleryConv() slack.Conversation {
	return slack.Conversation{ID: GalleryChannelID, Name: GalleryChannelName, IsChannel: true, IsMember: true,
		Created: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).Unix(), NumMembers: 3, ContextTeamID: TeamID,
		Topic: &slack.Purpose{Value: "Every Block Kit block, synthetic"}, Purpose: &slack.Purpose{Value: "Render tests and screenshots"}}
}

// galleryBase is the first gallery message's time; messages are a minute apart.
var galleryBase = time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Unix()

func (s *Server) galleryMessage(g GalleryMessage, ts string) slack.Message {
	m := slack.Message{Type: "message", TS: ts, Text: g.Text}
	if m.Text == "" {
		m.Text = g.Name
	}
	if g.From != "" {
		m.User = g.From
	} else {
		m.User, m.BotID = AppUserID, AppBotID
		m.BotProfile = &slack.BotProfile{ID: AppBotID, Name: "Deploy Bot", AppID: AppAppID, UserID: AppUserID, TeamID: TeamID,
			Icons: slack.Icons{Image48: s.URL + "img/48x48/app.png", Image72: s.URL + "img/72x72/app.png"}}
	}
	if g.Blocks != "" {
		m.Blocks = mustRaw(g.Name, g.Blocks)
	}
	if g.Attachments != "" {
		raw := mustRaw(g.Name, g.Attachments)
		_ = json.Unmarshal(raw, &m.Attachments)
		m.RawAttachments = raw
	}
	return m
}

func mustRaw(name, s string) json.RawMessage {
	if !json.Valid([]byte(s)) {
		panic("fakeslack: gallery message " + name + ": invalid JSON")
	}
	return json.RawMessage(s)
}

// galleryHistory is the channel's messages, newest first.
func (s *Server) galleryHistory() []slack.Message {
	gs := s.Gallery()
	out := make([]slack.Message, len(gs))
	for i, g := range gs {
		ts := fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60, i+1)
		m := s.galleryMessage(g, ts)
		if len(g.Replies) > 0 {
			m.ThreadTS, m.ReplyCount = ts, len(g.Replies)
			m.LatestReply = fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60+int64(len(g.Replies)), len(g.Replies))
		}
		out[len(gs)-1-i] = m
	}
	return out
}

// galleryReplies answers conversations.replies for a gallery thread.
func (s *Server) galleryReplies(ts string) ([]slack.Message, bool) {
	gs := s.Gallery()
	for i, g := range gs {
		pts := fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60, i+1)
		if pts != ts || len(g.Replies) == 0 {
			continue
		}
		parent := s.galleryMessage(g, pts)
		parent.ThreadTS, parent.ReplyCount = pts, len(g.Replies)
		msgs := []slack.Message{parent}
		for j, r := range g.Replies {
			m := s.galleryMessage(r, fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60+int64(j)+1, j+1))
			m.ThreadTS = pts
			msgs = append(msgs, m)
		}
		return msgs, true
	}
	return nil, false
}

// GalleryTS returns the ts of the gallery message named name ("" if none).
func (s *Server) GalleryTS(name string) string {
	for i, g := range s.Gallery() {
		if g.Name == name {
			return fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60, i+1)
		}
	}
	return ""
}
