// Package fakeslack is an in-process fake of the Slack web-client API for
// benchmarks: a deterministic, synthetic workspace the size of the Hack Club
// Slack (tens of thousands of custom emoji, hundreds of channels, thousands
// of users, long histories with images). Nothing in it comes from a real
// workspace (Hack Club scraping policy); every name and message is generated
// from a fixed seed.
//
// It serves the Web API (POST /api/<method>), the edge API (POST
// /api/edge/<path>, see slack.New's loopback routing), generated PNG images
// (GET /img/<w>x<h>/<seed>.png) and a realtime gateway that sends hello.
package fakeslack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// Size sets how big the workspace is.
type Size struct {
	Emoji      int // custom emoji (aliases come on top: Emoji/100)
	Users      int // users in the directory (all cached on a warm start)
	Channels   int // channels the viewer is a member of
	DMs        int
	Usergroups int
	History    int // messages per channel (conversations.history pages through them)
	Unread     int // channels with unreads (core prefetches these)
}

// HackClub approximates the Hack Club Slack seen from one account.
var HackClub = Size{Emoji: 65000, Users: 3000, Channels: 400, DMs: 40, Usergroups: 2000, History: 600, Unread: 30}

// Small is for unit tests.
var Small = Size{Emoji: 500, Users: 100, Channels: 30, DMs: 5, Usergroups: 20, History: 120, Unread: 3}

const (
	TeamID       = "T0FAKE0001"
	EnterpriseID = "E0FAKE0001"
	SelfID       = "U0SELF0001"

	// PreviewChannelID is a public channel the viewer isn't in; opening it
	// shows Slack's join pane. ArchivedChannelID is an archived one.
	PreviewChannelID  = "C0OTHER00001"
	ArchivedChannelID = "C0OTHER00002"
)

// JoinCall is one conversations.join the fake answered.
type JoinCall struct {
	Channel      string
	ExpirationTS string // "" for a permanent join
}

// Server is a running fake.
type Server struct {
	Size Size
	URL  string // http://127.0.0.1:port/

	srv   *http.Server
	ln    net.Listener
	users []slack.User
	convs []slack.Conversation

	// others are channels the viewer is not in (channel previews):
	// conversations.info answers them with is_member false, and
	// conversations.join moves them into convs (Joins records each call).
	convMu sync.Mutex
	others []slack.Conversation
	Joins  []JoinCall
	emoji  map[string]string
	words  []string

	imgMu sync.Mutex
	imgs  map[string][]byte

	histMu sync.Mutex
	hist   map[string][]slack.Message // channel -> all messages, newest first

	// Calls counts requests per method (atomic adds).
	callsMu sync.Mutex
	Calls   map[string]*atomic.Int64
	// Latency is added to every API call (not images), to model a network.
	Latency time.Duration
	// Hook, when set, sees every API call first; ok=true answers it with
	// out (tests: hold a call back, fail it). Set it before the calls.
	Hook func(method string, form url.Values) (out any, ok bool)

	prefsMu sync.Mutex
	prefs   map[string]any // users.prefs.get; users.prefs.set writes it

	// drafts and uploads (drafts.go)
	draftMu sync.Mutex
	drafts  map[string]*Draft        // drafts.create/update/delete/list
	files   map[string]*UploadedFile // files.getUploadURLExternal + upload
	shared  []SharedFiles            // files.completeUploadExternal calls
	nextID  int

	chans chanState // conversations.create and friends (channels.go)
}

// DefaultPrefs are the fake account's users.prefs (Slack's defaults for the
// keys Clack reads; JSON-typed prefs are strings holding JSON, as Slack
// sends them).
func DefaultPrefs() map[string]any {
	return map[string]any{"emoji_mode": "default", "time24": false, "display_real_names": false,
		"emoji_use": `{"smile":10,"tada":7,"+1":30,"fire":5,"eyes":9}`, "muted_channels": "",
		"ia_theme":                    `{"primary":{"palette":"aubergine"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}`,
		"window_background_gradients": true,
		// Preferences dialog keys Clack wires, at Slack's defaults
		"display_real_names_override": 0, "mentions_real_names_override": 0, "messages_theme": "default",
		"show_typing": true, "hide_hex_swatch": false, "jumbomoji": true, "convert_emoticons": true,
		"enter_is_special_in_tbt": false, "expand_internal_inline_imgs": true, "expand_inline_imgs": true,
		"obey_inline_img_limit": true, "expand_non_media_attachments": true,
		"mark_msgs_read_immediately": false, "start_scroll_at_oldest": false, "confirm_clear_all_unreads": true,
		"a11y_animations": true, "underline_links": false, "locale": "en-US",
		"who_can_see_account_by_searching_email": "EVERYONE", "who_can_share_contact_card": "EVERYONE",
		"vip_users": "", "priority_dnd_override": false, "sidebar_behavior": "",
		"separate_private_channels": false, "undo_channel_intermix": false,
		"boost_mentions": false, "show_muted_items_outside_sidebar_menus": false, "plain_text_mode": false,
		"huddles_mute_by_default": false, "huddles_set_status": true, "huddles_disconnect_on_lock": true,
		"all_notifications_prefs": `{"channels":{},"global":{"global_desktop":"mentions_dms","global_mobile":"mentions_dms",` +
			`"global_keywords":"","no_text_in_notifications":false,"push_idle_wait":0,"threads_everything":true,` +
			`"desktop_sound":"knock_brush.mp3"}}`,
	}
}

// setGlobalNotify is users.prefs.setNotifications global=true: the value
// lands in all_notifications_prefs.global, keeping the pref's type.
func (s *Server) setGlobalNotify(name, value string) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	var n map[string]any
	if str, _ := s.prefs["all_notifications_prefs"].(string); str != "" {
		_ = json.Unmarshal([]byte(str), &n)
	}
	if n == nil {
		n = map[string]any{}
	}
	g, _ := n["global"].(map[string]any)
	if g == nil {
		g = map[string]any{}
	}
	var v any = value
	switch g[name].(type) {
	case bool:
		v = value == "true" || value == "1"
	case float64:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			v = f
		}
	}
	g[name] = v
	n["global"] = g
	b, _ := json.Marshal(n)
	s.prefs["all_notifications_prefs"] = string(b)
}

// setVIP adds or removes a user from vip_users (users.priority.add/remove).
func (s *Server) setVIP(user string, add bool) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	cur, _ := s.prefs["vip_users"].(string)
	var ids []string
	for _, id := range strings.Split(cur, ",") {
		if id != "" && id != user {
			ids = append(ids, id)
		}
	}
	if add {
		ids = append(ids, user)
	}
	s.prefs["vip_users"] = strings.Join(ids, ",")
}

// Pref returns the fake account's pref (what users.prefs.get serves).
func (s *Server) Pref(name string) (any, bool) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	v, ok := s.prefs[name]
	return v, ok
}

// SetPrefs merges prefs into the account (test setup; no pref_change).
func (s *Server) SetPrefs(kv map[string]any) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	for k, v := range kv {
		s.prefs[k] = v
	}
}

// setPref is users.prefs.set: Slack keeps a pref's type, so a value
// written as "true" to a boolean pref reads back as true. New keys keep
// the form string, except "true"/"false" and integers.
func (s *Server) setPref(name, value string) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	var v any = value
	switch s.prefs[name].(type) {
	case bool:
		v = value == "true" || value == "1"
	case int, float64:
		if n, err := strconv.Atoi(value); err == nil {
			v = n
		}
	case string:
	default:
		if value == "true" || value == "false" {
			v = value == "true"
		} else if n, err := strconv.Atoi(value); err == nil {
			v = n
		}
	}
	s.prefs[name] = v
}

// Start serves a workspace of the given size on a loopback port.
func Start(sz Size) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return Serve(ln, sz), nil
}

// Serve serves on ln.
func Serve(ln net.Listener, sz Size) *Server {
	s := &Server{Size: sz, ln: ln, URL: "http://" + ln.Addr().String() + "/",
		imgs: map[string][]byte{}, hist: map[string][]slack.Message{}, Calls: map[string]*atomic.Int64{},
		prefs: DefaultPrefs(), drafts: map[string]*Draft{}, files: map[string]*UploadedFile{}}
	s.generate()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.api)
	mux.HandleFunc("/img/", s.img)
	mux.HandleFunc("/ws", s.ws)
	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(ln)
	return s
}

// Close stops the server.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// Creds returns credentials pointing every host (team, enterprise, edge) at
// the fake.
func (s *Server) Creds() slack.Creds {
	return slack.Creds{Token: "xoxc-fake", Cookie: "xoxd-fake", Team: TeamID, EnterpriseToken: "xoxc-fake-org",
		EnterpriseID: EnterpriseID, UserID: SelfID, TeamURL: s.URL, EnterpriseURL: s.URL}
}

// ChannelIDs returns the member channel ids in sidebar order.
func (s *Server) ChannelIDs() []string {
	out := make([]string, s.Size.Channels)
	for i := range out {
		out[i] = fmt.Sprintf("C0FAKE%05d", i)
	}
	return out
}

// Remote describes a fakeslackd already serving at url (another process,
// so benchmarks measure the client's memory without the fake's). Only the
// client-side helpers (Creds, ChannelIDs, Fetch) work on it.
func Remote(url string, sz Size) *Server {
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return &Server{Size: sz, URL: url, Calls: map[string]*atomic.Int64{}, prefs: DefaultPrefs()}
}

// CallCount returns how often method was called.
func (s *Server) CallCount(method string) int64 {
	s.callsMu.Lock()
	defer s.callsMu.Unlock()
	if c := s.Calls[method]; c != nil {
		return c.Load()
	}
	return 0
}

func (s *Server) count(method string) {
	s.callsMu.Lock()
	c := s.Calls[method]
	if c == nil {
		c = new(atomic.Int64)
		s.Calls[method] = c
	}
	s.callsMu.Unlock()
	c.Add(1)
}

// ---- data ----

var syllables = []string{"ka", "ri", "mo", "zu", "pe", "la", "ton", "vi", "shi", "do", "ne", "qua", "ber", "lin", "ga", "fo",
	"xi", "ru", "sa", "mi", "hack", "club", "dev", "bot", "pix", "nova", "byte", "loop", "zap", "orb"}

func (s *Server) generate() {
	r := rand.New(rand.NewSource(42))
	word := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(syllables[r.Intn(len(syllables))])
		}
		return b.String()
	}
	for i := 0; i < 2000; i++ {
		s.words = append(s.words, word(1+r.Intn(3)))
	}
	// users
	for i := 0; i < s.Size.Users; i++ {
		id := fmt.Sprintf("U0FAKE%05d", i)
		if i == 0 {
			id = SelfID
		}
		first, last := title(word(2)), title(word(2))
		handle := strings.ToLower(first + "." + last)
		hash := fmt.Sprintf("%012x", r.Int63())
		img := func(px int) string {
			return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/u" + strconv.Itoa(i) + ".png"
		}
		u := slack.User{ID: id, TeamID: TeamID, Name: handle, RealName: first + " " + last, TZ: "America/New_York",
			TZLabel: "Eastern Standard Time", TZOffset: -18000, Color: fmt.Sprintf("%06x", r.Intn(1<<24)),
			Profile: slack.Profile{RealName: first + " " + last, DisplayName: handle, FirstName: first, LastName: last,
				Title: title(word(2) + " " + word(3)), AvatarHash: hash,
				Image24: img(24), Image32: img(32), Image48: img(48), Image72: img(72), Image192: img(192), Image512: img(512),
				Team: TeamID, Fields: json.RawMessage(`{}`)}}
		if r.Intn(4) == 0 {
			u.Profile.StatusText = word(2) + " " + word(2)
			u.Profile.StatusEmoji = ":" + []string{"palm_tree", "house_with_garden", "computer", "coffee", "zzz"}[r.Intn(5)] + ":"
		}
		if r.Intn(3) == 0 {
			u.Profile.Pronouns = []string{"they/them", "she/her", "he/him"}[r.Intn(3)]
		}
		s.users = append(s.users, u)
	}
	// emoji
	s.emoji = make(map[string]string, s.Size.Emoji+s.Size.Emoji/100)
	var names []string
	for len(names) < s.Size.Emoji {
		n := word(1+r.Intn(3)) + []string{"", "-" + word(1), "_" + strconv.Itoa(r.Intn(100)), "-parrot", "-yay"}[r.Intn(5)]
		if _, dup := s.emoji[n]; dup {
			continue
		}
		ext := []string{"png", "png", "gif", "jpg"}[r.Intn(4)]
		// same shape and length as emoji.slack-edge.com URLs, served here
		s.emoji[n] = s.URL + "img/64x64/" + TeamID + "/" + url.PathEscape(n) + "/" + fmt.Sprintf("%016x", r.Uint64()) + "." + ext
		names = append(names, n)
	}
	for i := 0; i < s.Size.Emoji/100; i++ {
		s.emoji["alias-"+word(2)+strconv.Itoa(i)] = "alias:" + names[r.Intn(len(names))]
	}
	// conversations
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).Unix()
	for i := 0; i < s.Size.Channels; i++ {
		c := slack.Conversation{ID: fmt.Sprintf("C0FAKE%05d", i), Name: word(2) + "-" + word(1), IsChannel: true,
			IsMember: true, Created: now - int64(r.Intn(1e8)), Creator: s.users[r.Intn(len(s.users))].ID,
			NumMembers: 10 + r.Intn(30000), ContextTeamID: TeamID,
			Topic:   &slack.Purpose{Value: s.sentence(r, 8)},
			Purpose: &slack.Purpose{Value: s.sentence(r, 14)}}
		if i == 0 {
			c.IsGeneral, c.Name = true, "announcements"
		}
		c.IsPrivate = r.Intn(10) == 0
		s.convs = append(s.convs, c)
	}
	for i := 0; i < s.Size.DMs && i+1 < len(s.users); i++ {
		s.convs = append(s.convs, slack.Conversation{ID: fmt.Sprintf("D0FAKE%05d", i), IsIM: true, User: s.users[i+1].ID,
			IsOpen: true, Created: now - int64(r.Intn(1e7)), Priority: r.Float64()})
	}
	// channels the viewer isn't in (fixed names: screenshot tests read them)
	s.others = []slack.Conversation{
		{ID: PreviewChannelID, Name: "lounge-preview", IsChannel: true, Created: now - 9e7, Creator: s.users[1].ID,
			NumMembers: 4210, ContextTeamID: TeamID, Topic: &slack.Purpose{Value: "Say hi"},
			Purpose: &slack.Purpose{Value: "A channel you can read before joining. Ask questions, share what you made."}},
		{ID: ArchivedChannelID, Name: "old-hackathon", IsChannel: true, IsArchived: true, Created: now - 2e8,
			Creator: s.users[2].ID, NumMembers: 37, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Archived."}},
	}
}

// conv finds a conversation by id: the viewer's, then the others.
func (s *Server) conv(id string) (slack.Conversation, bool) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for _, c := range s.convs {
		if c.ID == id {
			return c, true
		}
	}
	for _, c := range s.others {
		if c.ID == id {
			return c, true
		}
	}
	return slack.Conversation{}, false
}

// join is conversations.join: an unarchived channel the viewer isn't in
// moves into its conversations.
func (s *Server) join(f url.Values) any {
	id := f.Get("channel")
	s.convMu.Lock()
	defer s.convMu.Unlock()
	s.Joins = append(s.Joins, JoinCall{Channel: id, ExpirationTS: f.Get("expiration_ts")})
	for _, c := range s.convs {
		if c.ID == id {
			return ok("channel", c, "already_in_channel", true)
		}
	}
	for i, c := range s.others {
		if c.ID != id {
			continue
		}
		if c.IsArchived {
			return map[string]any{"ok": false, "error": "is_archived"}
		}
		c.IsMember = true
		s.others = append(s.others[:i], s.others[i+1:]...)
		s.convs = append(s.convs, c)
		return ok("channel", c)
	}
	return map[string]any{"ok": false, "error": "channel_not_found"}
}

// SetProperties replaces a conversation's properties (posting_restricted_to
// …) in what conversations.info and users.conversations answer.
func (s *Server) SetProperties(id string, props json.RawMessage) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for i := range s.convs {
		if s.convs[i].ID == id {
			s.convs[i].Properties = props
		}
	}
}

// JoinCalls returns the conversations.join calls so far.
func (s *Server) JoinCalls() []JoinCall {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]JoinCall(nil), s.Joins...)
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// sentence is plain text of n words.
func (s *Server) sentence(r *rand.Rand, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s.words[r.Intn(len(s.words))])
	}
	return b.String()
}

func (s *Server) userAt(r *rand.Rand) slack.User { return s.users[r.Intn(min(len(s.users), 400))] }

// history builds (once) a channel's messages, newest first.
func (s *Server) history(ch string) []slack.Message {
	seed := int64(0)
	for _, c := range ch {
		seed = seed*31 + int64(c)
	}
	r := rand.New(rand.NewSource(seed))
	base := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).Unix()
	n := s.Size.History
	out := make([]slack.Message, 0, n)
	var emo []string
	for k := range s.emoji {
		emo = append(emo, k)
		if len(emo) == 64 {
			break
		}
	}
	sort.Strings(emo)
	std := []string{"smile", "tada", "+1", "fire", "eyes", "heart", "joy", "rocket", "sparkles", "pray"}
	t := base
	for i := 0; i < n; i++ {
		t -= int64(30 + r.Intn(3000))
		u := s.userAt(r)
		m := slack.Message{Type: "message", TS: fmt.Sprintf("%d.%06d", t, r.Intn(1e6)), User: u.ID, ClientMsgID: fmt.Sprintf("%08x-fake", r.Uint32())}
		// text with mrkdwn, mentions, channel links, emoji and links
		var parts []string
		var els []map[string]any
		nw := 4 + r.Intn(40)
		for w := 0; w < nw; w++ {
			switch x := r.Intn(40); {
			case x == 0:
				o := s.userAt(r)
				parts = append(parts, "<@"+o.ID+">")
				els = append(els, map[string]any{"type": "user", "user_id": o.ID})
			case x == 1:
				c := s.convs[r.Intn(min(len(s.convs), s.Size.Channels))]
				parts = append(parts, "<#"+c.ID+"|"+c.Name+">")
				els = append(els, map[string]any{"type": "channel", "channel_id": c.ID})
			case x == 2 || x == 3:
				e := std[r.Intn(len(std))]
				if x == 3 && len(emo) > 0 {
					e = emo[r.Intn(len(emo))]
				}
				parts = append(parts, ":"+e+":")
				els = append(els, map[string]any{"type": "emoji", "name": e})
			case x == 4:
				l := "https://example.com/" + s.words[r.Intn(len(s.words))]
				parts = append(parts, "<"+l+">")
				els = append(els, map[string]any{"type": "link", "url": l})
			case x == 5:
				wd := s.words[r.Intn(len(s.words))]
				parts = append(parts, "*"+wd+"*")
				els = append(els, map[string]any{"type": "text", "text": wd + " ", "style": map[string]bool{"bold": true}})
			case x == 6:
				wd := s.words[r.Intn(len(s.words))]
				parts = append(parts, "`"+wd+"`")
				els = append(els, map[string]any{"type": "text", "text": wd + " ", "style": map[string]bool{"code": true}})
			default:
				wd := s.words[r.Intn(len(s.words))]
				parts = append(parts, wd)
				els = append(els, map[string]any{"type": "text", "text": wd + " "})
			}
		}
		m.Text = strings.Join(parts, " ")
		if r.Intn(10) < 7 { // most modern messages carry rich_text blocks
			b, _ := json.Marshal([]map[string]any{{"type": "rich_text", "block_id": fmt.Sprintf("b%d", i),
				"elements": []map[string]any{{"type": "rich_text_section", "elements": els}}}})
			m.Blocks = b
		}
		if r.Intn(5) == 0 { // an image file
			w, h := 400+r.Intn(1600), 300+r.Intn(1200)
			tw, th := 360, 360*h/w
			t7w, t7h := 720, 720*h/w
			img := func(px, py int) string {
				return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(py) + "/f" + strconv.Itoa(i) + ".png"
			}
			m.Files = []slack.File{{ID: fmt.Sprintf("F0FAKE%05d", i), Name: "image.png", Title: "image.png", Mimetype: "image/png",
				Filetype: "png", PrettyType: "PNG", Size: int64(w * h), Mode: "hosted",
				URLPrivate: img(w, h), Thumb360: img(tw, th), Thumb480: img(480, 480*h/w), Thumb720: img(t7w, t7h),
				Thumb360W: tw, Thumb360H: th, Thumb720W: t7w, Thumb720H: t7h, OriginalW: w, OriginalH: h}}
		}
		if r.Intn(6) == 0 {
			k := 1 + r.Intn(5)
			for j := 0; j < k; j++ {
				name := std[r.Intn(len(std))]
				if j%2 == 1 && len(emo) > 0 {
					name = emo[r.Intn(len(emo))]
				}
				cnt := 1 + r.Intn(12)
				var us []string
				for q := 0; q < cnt; q++ {
					us = append(us, s.userAt(r).ID)
				}
				m.Reactions = append(m.Reactions, slack.Reaction{Name: name, Count: cnt, Users: us})
			}
		}
		if r.Intn(10) == 0 {
			m.ThreadTS = m.TS
			m.ReplyCount = 1 + r.Intn(30)
			m.ReplyUsers = []string{s.userAt(r).ID, s.userAt(r).ID}
			m.LatestReply = fmt.Sprintf("%d.000100", t+600)
		}
		if r.Intn(30) == 0 {
			m.Edited = &slack.Edited{User: m.User, TS: fmt.Sprintf("%d.000000", t+60)}
		}
		out = append(out, m)
	}
	return out
}

// ---- HTTP ----

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	if rest, ok := strings.CutPrefix(method, "edge/"); ok { // edge/<enterprise>/<path>
		if _, p, ok := strings.Cut(rest, "/"); ok {
			method = "edge/" + p
		}
	}
	s.count(method)
	if s.Latency > 0 {
		time.Sleep(s.Latency)
	}
	body, _ := io.ReadAll(r.Body)
	var form url.Values
	var edge map[string]json.RawMessage
	if strings.HasPrefix(method, "edge/") {
		_ = json.Unmarshal(body, &edge)
	} else {
		form, _ = url.ParseQuery(string(body))
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			form = url.Values{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	var out any
	hooked := false
	if s.Hook != nil {
		out, hooked = s.Hook(method, form)
	}
	if !hooked {
		out = s.handle(method, form, edge)
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	if b, ok := out.([]byte); ok {
		w.Write(b)
		return
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(out)
}

func ok(kv ...any) map[string]any {
	m := map[string]any{"ok": true}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func (s *Server) handle(method string, f url.Values, edge map[string]json.RawMessage) any {
	switch method {
	case "auth.test":
		return ok("url", s.URL, "team", "Fake Club", "user", "self", "team_id", TeamID, "user_id", SelfID, "enterprise_id", EnterpriseID)
	case "users.conversations":
		s.convMu.Lock()
		cs := append([]slack.Conversation(nil), s.convs...)
		s.convMu.Unlock()
		return ok("channels", append(cs, s.Created()...), "response_metadata", map[string]string{"next_cursor": ""})
	case "client.counts":
		var chans, ims []slack.CountEntry
		for i, c := range s.convs {
			e := slack.CountEntry{ID: c.ID, LastRead: "1762500000.000000", Latest: "1762516800.000000"}
			if i%(max(1, s.Size.Channels/max(1, s.Size.Unread))) == 1 {
				e.HasUnreads = true
				e.LastRead = "1762400000.000000"
				if i%3 == 0 {
					e.MentionCount = 1 + i%4
				}
			}
			if c.IsIM {
				ims = append(ims, e)
			} else {
				chans = append(chans, e)
			}
		}
		return ok("channels", chans, "ims", ims, "mpims", []any{}, "threads", map[string]any{"has_unreads": true, "mention_count": 2})
	case "sidebar.dms": // every generated DM is open
		var ims, mpdms []map[string]any
		for _, c := range s.convs {
			switch {
			case c.IsIM:
				ims = append(ims, map[string]any{"id": c.ID, "user": c.User, "is_open": true, "is_im": true})
			case c.IsMPIM:
				mpdms = append(mpdms, map[string]any{"id": c.ID, "is_open": true, "is_mpim": true})
			}
		}
		return ok("ims", ims, "mpdms", mpdms)
	case "experiments.getByUser": // the sidebar experiments as a real account has them (2026-11-08)
		g := func(group string) map[string]any { return map[string]any{"group": group} }
		return ok("assignments", map[string]any{"filter_dormant_channels": g("on"), "improve_mute_pref": g("on"),
			"dm_recency_sort_rollout": g("without_edu"), "reskin_custom_responses": g("on"), "temp_join_channel": g("on")})
	case "users.channelSections.list":
		var secs []map[string]any
		ids := s.ChannelIDs()
		per := max(1, len(ids)/6)
		names := []string{"Starred", "Hack Club", "Projects", "Events", "Random stuff", "Bots"}
		for i, n := range names {
			lo, hi := i*per, min(len(ids), (i+1)*per)
			if i == len(names)-1 {
				hi = len(ids)
			}
			typ := "standard"
			if i == 0 {
				typ = "stars"
			}
			id := fmt.Sprintf("L0FAKE%02d", i)
			chs := append(s.SectionInserts(id), ids[lo:hi]...)
			secs = append(secs, map[string]any{"channel_section_id": id, "name": n, "type": typ,
				"channel_ids_page": map[string]any{"channel_ids": chs, "count": len(chs)}})
		}
		secs = append(secs, map[string]any{"channel_section_id": "L0FAKECH", "name": "", "type": "channels"},
			map[string]any{"channel_section_id": "L0FAKEDM", "name": "", "type": "direct_messages"},
			map[string]any{"channel_section_id": "L0FAKETMP", "name": "", "type": "temporary_channels"})
		return ok("channel_sections", secs)
	case "users.prefs.get":
		s.prefsMu.Lock()
		m := make(map[string]any, len(s.prefs))
		for k, v := range s.prefs {
			m[k] = v
		}
		s.prefsMu.Unlock()
		return ok("prefs", m)
	case "users.prefs.set":
		if n := f.Get("name"); n != "" {
			s.setPref(n, f.Get("value"))
		} else if ps := f.Get("prefs"); ps != "" { // setMultipleUserPrefsByApi
			var kv map[string]any
			if json.Unmarshal([]byte(ps), &kv) != nil {
				return map[string]any{"ok": false, "error": "invalid_prefs"}
			}
			for k, v := range kv {
				switch v := v.(type) {
				case string:
					s.setPref(k, v)
				default:
					b, _ := json.Marshal(v)
					s.setPref(k, string(b))
				}
			}
		} else {
			return map[string]any{"ok": false, "error": "invalid_name"}
		}
		return ok()
	case "users.prefs.setNotifications":
		if f.Get("global") != "true" {
			return map[string]any{"ok": false, "error": "not_supported_by_fake"}
		}
		if n := f.Get("name"); n != "" {
			s.setGlobalNotify(n, f.Get("value"))
		} else {
			var ps []struct{ Name, Value string }
			if json.Unmarshal([]byte(f.Get("prefs")), &ps) != nil {
				return map[string]any{"ok": false, "error": "invalid_prefs"}
			}
			for _, p := range ps {
				s.setGlobalNotify(p.Name, p.Value)
			}
		}
		return ok()
	case "users.priority.add", "users.priority.remove":
		u := f.Get("user")
		if u == "" {
			return map[string]any{"ok": false, "error": "user_not_found"}
		}
		s.setVIP(u, method == "users.priority.add")
		return ok()
	case "team.info":
		return ok("team", map[string]any{"id": TeamID, "name": "Fake Club", "domain": "fakeclub",
			"icon": map[string]any{"image_88": s.URL + "img/88x88/team.png"}})
	case "emoji.list":
		return ok("emoji", s.emoji)
	case "usergroups.list":
		var gs []slack.Usergroup
		for i := 0; i < s.Size.Usergroups; i++ {
			gs = append(gs, slack.Usergroup{ID: fmt.Sprintf("S0FAKE%05d", i), Handle: s.usergroupHandle(i),
				Name: title(s.words[(i*7)%len(s.words)]), Description: s.words[(i*3)%len(s.words)], UserCount: i % 50})
		}
		return ok("usergroups", gs)
	case "client.getWebSocketURL":
		ws := "ws" + strings.TrimPrefix(s.URL, "http") + "ws"
		return ok("primary_websocket_url", ws, "fallback_websocket_url", ws, "routing_context", "fake")
	case "conversations.history":
		return s.historyPage(f)
	case "conversations.replies":
		return ok("messages", []any{}, "has_more", false)
	case "conversations.info":
		if c, found := s.conv(f.Get("channel")); found {
			return ok("channel", c)
		}
		if c, found := s.createdConv(f.Get("channel")); found {
			return ok("channel", c)
		}
		return map[string]any{"ok": false, "error": "channel_not_found"}
	case "conversations.join":
		return s.join(f)
	case "chat.postMessage":
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
		return ok("ts", ts, "channel", f.Get("channel"), "message", map[string]any{"type": "message", "ts": ts, "user": SelfID, "text": f.Get("text")})
	case "edge/users/info":
		var up map[string]int
		_ = json.Unmarshal(edge["updated_ids"], &up)
		var res []slack.User
		for id := range up {
			if u, ok := s.userByID(id); ok {
				res = append(res, u)
			}
		}
		return ok("results", res, "failed_ids", []string{})
	case "edge/users/search":
		var q string
		_ = json.Unmarshal(edge["query"], &q)
		var res []slack.User
		for _, u := range s.users {
			if strings.Contains(u.Name, strings.ToLower(q)) {
				res = append(res, u)
				if len(res) == 30 {
					break
				}
			}
		}
		return ok("results", res)
	case "edge/channels/search":
		var q string
		_ = json.Unmarshal(edge["query"], &q)
		var res []map[string]any
		for _, c := range s.convs {
			if c.IsChannel && strings.Contains(c.Name, strings.ToLower(q)) {
				res = append(res, map[string]any{"id": c.ID, "name": c.Name, "is_channel": true, "is_member": true, "num_members": c.NumMembers})
				if len(res) == 30 {
					break
				}
			}
		}
		return ok("results", res)
	case "edge/users/counts":
		return ok("counts", map[string]int{"everyone": 1200, "people": 1100, "members": 1150, "guests": 3, "bots": 47, "apps": 12})
	case "edge/users/list":
		return ok("results", s.users[:min(len(s.users), 50)], "next_marker", "")
	case "edge/channels/info":
		return ok("results", []any{}, "failed_ids", []string{})
	case "edge/huddles/list":
		return ok("huddles", []any{})
	}
	if out, handled := s.handleDrafts(method, f); handled {
		return out
	}
	if out, handled := s.handleChannels(method, f); handled {
		return out
	}
	return nil
}

func (s *Server) userByID(id string) (slack.User, bool) {
	if id == SelfID {
		return s.users[0], true
	}
	if strings.HasPrefix(id, "U0FAKE") {
		if n, err := strconv.Atoi(id[6:]); err == nil && n < len(s.users) {
			return s.users[n], true
		}
	}
	return slack.User{}, false
}

func (s *Server) historyPage(f url.Values) any {
	ch := f.Get("channel")
	s.histMu.Lock()
	msgs, okc := s.hist[ch]
	if !okc {
		msgs = s.history(ch)
		s.hist[ch] = msgs
	}
	s.histMu.Unlock()
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	latest := f.Get("latest")
	start := 0
	if latest != "" {
		for start < len(msgs) && !slackTSLess(msgs[start].TS, latest) {
			start++
		}
	}
	end := min(len(msgs), start+limit)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	_ = enc.Encode(map[string]any{"ok": true, "messages": msgs[start:end], "has_more": end < len(msgs), "pin_count": 0})
	return buf.Bytes()
}

func slackTSLess(a, b string) bool {
	af, _ := strconv.ParseFloat(a, 64)
	bf, _ := strconv.ParseFloat(b, 64)
	return af < bf
}

// img serves /img/<w>x<h>/<seed>.png: a deterministic gradient PNG.
func (s *Server) img(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/img/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	s.imgMu.Lock()
	b, okc := s.imgs[r.URL.Path]
	s.imgMu.Unlock()
	if !okc {
		var iw, ih int
		fmt.Sscanf(parts[0], "%dx%d", &iw, &ih)
		iw, ih = min(max(iw, 1), 4096), min(max(ih, 1), 4096)
		seed := 0
		for _, c := range parts[1] {
			seed = seed*31 + int(c)
		}
		im := image.NewNRGBA(image.Rect(0, 0, iw, ih))
		for y := 0; y < ih; y++ {
			for x := 0; x < iw; x++ {
				im.SetNRGBA(x, y, color.NRGBA{uint8(x*255/iw + seed), uint8(y*255/ih + seed>>3), uint8(seed >> 6), 255})
			}
		}
		var buf bytes.Buffer
		_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, im)
		b = buf.Bytes()
		s.imgMu.Lock()
		s.imgs[r.URL.Path] = b
		s.imgMu.Unlock()
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(b)
}

// ws accepts the realtime gateway and says hello; it then idles.
func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	s.count("ws")
	// the client sends Origin: https://app.slack.com, like the web client
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello"}`))
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
	}
}

// WriteCreds writes the fake's credentials to path (for running the real
// binary against it).
func (s *Server) WriteCreds(path string) error {
	b, _ := json.MarshalIndent(s.Creds(), "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// Fetch downloads url for an image cache: the fake's own URLs over HTTP,
// anything else (Slack's standard-emoji CDN) as a generated 64 px image, so
// benchmarks never touch the network.
func (s *Server) Fetch(ctx context.Context, raw string) ([]byte, error) {
	if !strings.HasPrefix(raw, s.URL) {
		raw = s.URL + "img/64x64/ext/" + url.PathEscape(raw)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tlsfp.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fakeslack: GET %s: %s", raw, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
