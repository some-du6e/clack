package fakeslack

// The blockkit-gallery channel: one fixture channel holding a message for
// every Block Kit block and variant, for render tests and CI screenshots.
// Everything in it is synthetic.
//
// Each part of the gallery lives in its own file (gallery_layout.go for
// layout blocks, bkgallery_elements.go, bkgallery_surfaces.go…) and
// registers itself from init, in one of two forms:
//
//	func init() { RegisterGallery("10-layout", galleryLayout) }          // GalleryMessage specs
//	func init() { RegisterGalleryMessages("50-elements", galleryElements) } // whole slack.Messages
//
// Parts show in name order, oldest first; a part's messages keep their
// order and get a minute apart each, unless a slack.Message part sets its
// own TS (the channel is then ordered by ts). A GalleryMessage with Replies
// is a thread parent (conversations.replies serves them).
//
// The channel is always there; Size.Gallery also adds the parts' extra
// conversations (registerGalleryConv: an app DM) after the generated ones,
// and registerGalleryAPI adds a part's fake app methods.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	GalleryChannelID   = "C0BKGALLERY"
	GalleryChannelName = "blockkit-gallery"
	// BlockKitGalleryID is GalleryChannelID (bkgallery_surfaces.go's name).
	BlockKitGalleryID = GalleryChannelID
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

// galleryPart is one registered part: GalleryMessage specs or whole
// messages.
type galleryPart struct {
	specs func(GalleryEnv) []GalleryMessage
	msgs  func(s *Server) []slack.Message
}

var (
	galleryMu    sync.Mutex
	galleryParts = map[string]galleryPart{}
	galleryAPIs  []func(s *Server, method string, f url.Values) (any, bool)
	galleryConvs []func(s *Server) (slack.Conversation, func(*Server) []slack.Message)
)

func addGalleryPart(part string, p galleryPart) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	if _, dup := galleryParts[part]; dup {
		panic("fakeslack: gallery part registered twice: " + part)
	}
	galleryParts[part] = p
}

// RegisterGallery adds a part of GalleryMessage specs (call it from init).
func RegisterGallery(part string, f func(GalleryEnv) []GalleryMessage) {
	addGalleryPart(part, galleryPart{specs: f})
}

// RegisterGalleryMessages adds a part of whole messages, oldest first
// (call it from init). Messages without a TS get their place's; set TS to
// pin one (permalink fixtures).
func RegisterGalleryMessages(part string, f func(s *Server) []slack.Message) {
	addGalleryPart(part, galleryPart{msgs: f})
}

// registerGallery is RegisterGalleryMessages (bkgallery_surfaces.go).
func registerGallery(part string, f func(s *Server) []slack.Message) {
	RegisterGalleryMessages(part, f)
}

// registerGalleryConv adds a conversation (an app DM…) and its history,
// newest first, listed with Size.Gallery (init time).
func registerGalleryConv(c func(s *Server) (slack.Conversation, func(*Server) []slack.Message)) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	galleryConvs = append(galleryConvs, c)
}

// registerGalleryAPI adds a part's fake methods (init time); the first
// to answer a method wins.
func registerGalleryAPI(h func(s *Server, method string, f url.Values) (any, bool)) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	galleryAPIs = append(galleryAPIs, h)
}

// Gallery is Small with the gallery's extra conversations.
var Gallery = func() Size { sz := Small; sz.Gallery = true; return sz }()

// addGalleryConvs lists the parts' extra conversations (Size.Gallery),
// after the generated ones so no fixture shifts.
func (s *Server) addGalleryConvs() {
	galleryMu.Lock()
	cs := append([]func(*Server) (slack.Conversation, func(*Server) []slack.Message)(nil), galleryConvs...)
	galleryMu.Unlock()
	for _, c := range cs {
		cv, _ := c(s)
		s.convs = append(s.convs, cv)
	}
}

// galleryHistoryOf is the history of a gallery conversation, newest
// first (ok false for the generated ones).
func (s *Server) galleryHistoryOf(ch string) ([]slack.Message, bool) {
	if ch == GalleryChannelID {
		return s.galleryHistory(), true
	}
	if !s.Size.Gallery {
		return nil, false
	}
	galleryMu.Lock()
	cs := append([]func(*Server) (slack.Conversation, func(*Server) []slack.Message)(nil), galleryConvs...)
	galleryMu.Unlock()
	for _, c := range cs {
		if cv, h := c(s); cv.ID == ch {
			return h(s), true
		}
	}
	return nil, false
}

// emit sends a realtime event: to Emit when a test set it (clients
// without the socket), else over the realtime socket.
func (s *Server) emit(ev map[string]any) {
	if f := s.Emit; f != nil {
		f(ev)
		return
	}
	s.Push(ev)
}

// handleGallery answers the parts' fake app methods.
func (s *Server) handleGallery(method string, f url.Values) (any, bool) {
	galleryMu.Lock()
	apis := append([]func(*Server, string, url.Values) (any, bool)(nil), galleryAPIs...)
	galleryMu.Unlock()
	for _, h := range apis {
		if out, ok := h(s, method, f); ok {
			return out, true
		}
	}
	return nil, false
}

// galleryEntry is one gallery message in channel order.
type galleryEntry struct {
	spec GalleryMessage // the spec (whole messages: Name part/ts, Text, Blocks)
	msg  slack.Message  // as served
}

// galleryEntries builds the channel, oldest first: parts by name, each
// message at its place's ts (a minute apart) unless it pinned its own,
// then ordered by ts.
func (s *Server) galleryEntries() []galleryEntry {
	galleryMu.Lock()
	names := make([]string, 0, len(galleryParts))
	for n := range galleryParts {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]galleryPart, len(names))
	for i, n := range names {
		parts[i] = galleryParts[n]
	}
	galleryMu.Unlock()
	env := s.galleryEnv()
	var out []galleryEntry
	place := func() string { i := len(out); return fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60, i+1) }
	for pi, p := range parts {
		if p.specs != nil {
			for _, g := range p.specs(env) {
				ts := place()
				m := s.galleryMessage(g, ts)
				if len(g.Replies) > 0 {
					m.ThreadTS, m.ReplyCount = ts, len(g.Replies)
					m.LatestReply = replyTS(ts, len(g.Replies))
				}
				out = append(out, galleryEntry{spec: g, msg: m})
			}
			continue
		}
		for _, m := range p.msgs(s) {
			if m.TS == "" {
				m.TS = place()
			}
			if m.Type == "" {
				m.Type = "message"
			}
			name := names[pi] + "/" + m.TS
			if m.ClientMsgID != "" {
				name = names[pi] + "/" + m.ClientMsgID
			}
			out = append(out, galleryEntry{spec: GalleryMessage{Name: name, Text: m.Text, Blocks: string(m.Blocks)}, msg: m})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return slackTSLess(out[i].msg.TS, out[j].msg.TS) })
	return out
}

// replyTS is reply n's ts in the thread of parent (n from 1).
func replyTS(parent string, n int) string {
	sec, _, _ := strings.Cut(parent, ".")
	var base int64
	fmt.Sscan(sec, &base)
	return fmt.Sprintf("%d.%06d", base+int64(n), n)
}

// Gallery returns every gallery message in channel order (oldest first).
func (s *Server) Gallery() []GalleryMessage {
	es := s.galleryEntries()
	out := make([]GalleryMessage, len(es))
	for i := range es {
		out[i] = es[i].spec
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
	es := s.galleryEntries()
	out := make([]slack.Message, len(es))
	for i := range es {
		out[len(es)-1-i] = es[i].msg
	}
	return out
}

// galleryReplies answers conversations.replies for a gallery thread.
func (s *Server) galleryReplies(ts string) ([]slack.Message, bool) {
	for _, e := range s.galleryEntries() {
		g := e.spec
		if e.msg.TS != ts || len(g.Replies) == 0 {
			continue
		}
		msgs := []slack.Message{e.msg}
		for j, r := range g.Replies {
			m := s.galleryMessage(r, replyTS(ts, j+1))
			m.ThreadTS = ts
			msgs = append(msgs, m)
		}
		return msgs, true
	}
	return nil, false
}

// GalleryTS returns the ts of the gallery message named name ("" if none).
func (s *Server) GalleryTS(name string) string {
	for _, e := range s.galleryEntries() {
		if e.spec.Name == name {
			return e.msg.TS
		}
	}
	return ""
}
