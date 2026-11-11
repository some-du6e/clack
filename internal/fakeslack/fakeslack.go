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

	"github.com/some-du6e/clack/internal/slack"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
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
	// Identity adds #identity-fixtures: one message per bot/app sender
	// case (identity.go). Off by default so other fixtures don't shift.
	Identity bool
	// Gallery adds the gallery's extra conversations (an app DM…,
	// registerGalleryConv in gallery.go) after the generated ones;
	// #blockkit-gallery itself is always there.
	Gallery bool
	// ChannelLinks adds #channel-links and the channels its message
	// mentions (channel_links.go).
	ChannelLinks bool
	// CodeChannels adds the code channel fixtures (codechannels.go): one
	// per agent status, an archived one and one the viewer isn't in.
	CodeChannels bool
	// Views adds the rail list views' fixtures (views.go): followed
	// threads, scheduled drafts, recorded activity writes.
	Views bool
	// Embeds adds #embeds-gallery: one message per way Slack draws a
	// link preview (embeds.go).
	Embeds bool
	// Files adds #files-gallery: one message per way Slack draws shared
	// files (files.go).
	Files bool
	// ThreadHeader adds one thread in a conversation of each kind the
	// thread pane's header names differently (threadheader.go).
	ThreadHeader bool
	// ThreadsAudit adds #th-audit: one thread per way the thread pane
	// marks unread replies, opens and counts (threads_audit.go).
	ThreadsAudit bool
	// PluginBench adds the plugin bench's channels (plugin_bench.go):
	// #plugin-bench (500 messages), #admins-only and a Slackbot DM; the
	// viewer becomes a workspace admin.
	PluginBench bool
	// Unreads adds the Unreads view's and the channel join banner's
	// fixtures (unreads.go): unread channels, a muted one, a channel
	// whose message_input megaphone space holds the join banner.
	Unreads bool
	// Workflows adds #workflows-gallery and the Workflow Builder methods
	// (workflows.go): workflow links, a workflow button, a workflow
	// step's message, a bookmarked workflow, tripping, triggers.
	Workflows bool
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

// Fixed profile variants for profile-card tests. They're outside the
// generated directory (so they never shift the random fixtures) and
// users/info serves them by id.
const (
	ProfileID     = "U0FAKEPROF" // every card field: pronouns, title, status, Berlin time
	AdminID       = "U0FAKEADMN" // a workspace admin ("Workspace Admin" banner)
	DeactivatedID = "U0FAKEGONE" // a deactivated account
	SlackbotID    = "USLACKBOT"

	// AppUserID is an app's bot user (profile.bot_id AppBotID): Slack
	// opens AppHoverCard for it. ExternalID is a Slack Connect member of
	// ExternalTeamID ("Partner Co"), ExternalAppUserID that team's app.
	AppUserID         = "U0FAKEAPPU"
	AppBotID          = "B0FAKEAPPB"
	AppAppID          = "A0FAKEAPPA"
	ExternalID        = "U0FAKEXTRN"
	ExternalTeamID    = "T0PARTNER1"
	ExternalAppUserID = "U0FAKEXAPP"
	ExternalAppBotID  = "B0FAKEXAPP"
	ExternalAppAppID  = "A0FAKEXAPP"
)

// variants are the fixed users above.
func (s *Server) variants() []slack.User {
	img := func(px int, n string) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
	}
	prof := func(display, real, title, n string) slack.Profile {
		return slack.Profile{DisplayName: display, RealName: real, Title: title, Team: TeamID, Fields: json.RawMessage(`{}`),
			Image24: img(24, n), Image32: img(32, n), Image48: img(48, n), Image72: img(72, n), Image192: img(192, n), Image512: img(512, n)}
	}
	p := prof("robin", "Robin Example", "Synthetic Fixture Wrangler", "prof")
	p.Pronouns = "they/them"
	p.StatusText = "Out sailing for the afternoon"
	p.StatusEmoji = ":sailboat:"
	a := prof("avery.admin", "Avery Admin", "Keeps the lights on", "admn")
	g := prof("gone.user", "Gone User", "Former fixture", "gone")
	b := prof("Slackbot", "Slackbot", "", "slackbot")
	ap := prof("", "Deploy Bot", "", "app")
	ap.BotID, ap.APIAppID = AppBotID, AppAppID
	x := prof("sam", "Sam Partner", "Partner Engineer", "xtrn")
	x.Team = ExternalTeamID
	xap := prof("", "Partner Bot", "", "xapp")
	xap.Team, xap.BotID, xap.APIAppID = ExternalTeamID, ExternalAppBotID, ExternalAppAppID
	return []slack.User{
		{ID: ProfileID, TeamID: TeamID, Name: "robin", RealName: "Robin Example", TZ: "Europe/Berlin", TZLabel: "Central European Time", TZOffset: 3600, Profile: p, Presence: "active"},
		{ID: AdminID, TeamID: TeamID, Name: "avery.admin", RealName: "Avery Admin", IsAdmin: true, TZ: "America/Los_Angeles", TZOffset: -28800, Profile: a},
		{ID: DeactivatedID, TeamID: TeamID, Name: "gone.user", RealName: "Gone User", Deleted: true, TZ: "America/New_York", TZOffset: -18000, Profile: g},
		// Slack serves Slackbot with is_bot false
		{ID: SlackbotID, TeamID: TeamID, Name: "slackbot", RealName: "Slackbot", Profile: b, Presence: "active"},
		{ID: AppUserID, TeamID: TeamID, Name: "deploybot", RealName: "Deploy Bot", IsBot: true, Profile: ap, Presence: "active"},
		{ID: ExternalID, TeamID: ExternalTeamID, Name: "sam.partner", RealName: "Sam Partner", TZ: "Asia/Tokyo", TZOffset: 32400, Profile: x, Presence: "active"},
		{ID: ExternalAppUserID, TeamID: ExternalTeamID, Name: "partnerbot", RealName: "Partner Bot", IsBot: true, Profile: xap},
	}
}

// appProfile is apps.profile.get's app_profile for the fixed apps (the
// keys Slack's AppHoverCard reads, as the real method sends them).
func (s *Server) appProfile(bot, app string) (map[string]any, bool) {
	icons := func(n string) map[string]any {
		m := map[string]any{}
		for _, px := range []int{32, 48, 64, 72, 96, 128, 192, 512} {
			m["image_"+strconv.Itoa(px)] = s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
		}
		return m
	}
	if p, ok := s.identityAppProfile(bot, app); ok && s.Size.Identity {
		return p, true
	}
	switch {
	case bot == AppBotID || app == AppAppID:
		return map[string]any{"id": AppAppID, "name": "Deploy Bot", "desc": "Ships builds and posts release notes",
			"app_card_color": "1264a3", "app_user_id": AppUserID, "team_id": TeamID, "enterprise_id": EnterpriseID,
			"is_xoxa_app": false, "is_slack_integration": false, "is_workflow_app": false, "is_directory_published": false,
			"is_installed": true, "icons": icons("app"), "bot_user": map[string]any{"id": AppUserID, "username": "deploybot"},
			"auth":     map[string]any{"created_by": AdminID, "date_created": "1700000000"},
			"commands": map[string]any{"deploy": map[string]any{"desc": "Deploy a branch", "usage": "[branch]"}}}, true
	case bot == ExternalAppBotID || app == ExternalAppAppID:
		return map[string]any{"id": ExternalAppAppID, "name": "Partner Bot", "desc": "Partner Co's helpdesk",
			"app_card_color": "2eb67d", "app_user_id": ExternalAppUserID, "team_id": ExternalTeamID, "enterprise_id": "",
			"is_directory_published": false, "is_installed": true, "icons": icons("xapp"),
			"bot_user": map[string]any{"id": ExternalAppUserID, "username": "partnerbot"},
			"auth":     map[string]any{"created_by": ExternalID}}, true
	}
	return nil, false
}

// Server is a running fake.
type Server struct {
	Size Size
	URL  string // http://127.0.0.1:port/

	// OnPost, when set, runs inside chat.postMessage before it answers,
	// with the posted form and the new message (tests push the realtime
	// echo from it, so it reaches the client before the reply). Set it
	// before the first post.
	OnPost func(f url.Values, msg map[string]any)

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
	Leaves []string // conversations.leave calls (LeaveCalls)
	// listFilters: every edge users/list filter (UserListFilters).
	listFilters []string
	emoji       map[string]string
	words       []string

	imgMu sync.Mutex
	imgs  map[string][]byte

	histMu sync.Mutex
	// delAtts: chat.deleteAttachment calls (embeds.go)
	delAtts []DeletedAttachment
	hist    map[string][]slack.Message // channel -> all messages, newest first
	code    codeCalls                  // code channel API calls (codechannels.go)
	codeFx  *CodeFixtures

	bkMu      sync.Mutex
	bkActions []BlockActionCall // blocks.actions, in order (bkgallery_elements.go)
	bkSuggest []url.Values      // blocks.suggestions forms

	// Calls counts requests per method (atomic adds).
	callsMu sync.Mutex
	Calls   map[string]*atomic.Int64
	// Latency is added to every API call (not images), to model a network.
	Latency time.Duration
	// ImgLatency is added to every image (avatars, emoji, thumbnails).
	ImgLatency time.Duration
	// Emit, when set, receives the realtime events the fake apps send
	// (view_opened…) instead of the socket: tests running the client
	// without realtime inject them (core.App.Inject). Set it before the calls.
	Emit func(ev map[string]any)
	// hook (SetHook) sees every API call first; ok=true answers it with
	// out (tests: hold a call back, fail it).
	hookMu sync.Mutex
	hook   func(method string, form url.Values) (out any, ok bool)

	// Permissions are permissions/info's answers by channel_id ("" for a
	// request without one). nil: every broadcast permission is allowed.
	// Set it before the calls.
	Permissions map[string][]string
	// TeamPrefs is team.prefs.get's prefs (nil: none; Slack's client then
	// never warns before @channel and has no large-channel limit), and
	// ChannelPrefs channels.prefs.get's pref_value by channel and pref
	// name (missing: {}, enabled). Set them before the calls.
	TeamPrefs    map[string]any
	ChannelPrefs map[string]map[string]any

	prefsMu sync.Mutex
	prefs   map[string]any // users.prefs.get; users.prefs.set writes it

	// drafts and uploads (drafts.go)
	draftMu sync.Mutex
	drafts  map[string]*Draft        // drafts.create/update/delete/list
	files   map[string]*UploadedFile // files.getUploadURL + upload + files.completeUpload
	shared  []SharedFiles            // files.share calls
	faultMu sync.Mutex
	faults  map[string]*Fault // SetFault (faults.go)
	// DuplicateEvents sends every upload event twice (the files.share
	// echo, file_created), as a reconnecting socket can.
	DuplicateEvents bool
	// ProcessDelay is how long an upload takes to process after
	// files.completeUpload: files.info answers a stub until then, and
	// file_created goes out at the end (Slack's eager upload resolves on
	// it). Negative: never processed. Set it before the uploads.
	ProcessDelay time.Duration
	nextID       int

	emailTokens []map[string]any // users.emailtoken.* (under prefsMu)

	chans chanState   // conversations.create and friends (channels.go)
	secs  secState    // sidebar sections, marks, channel notification prefs (sections.go)
	hud   huddleState // rooms.getLink, huddles.scheduled.new, calendar.* (huddles.go)
}

// DefaultPrefs are the fake account's users.prefs (Slack's defaults for the
// keys Clack reads; JSON-typed prefs are strings holding JSON, as Slack
// sends them).
func DefaultPrefs() map[string]any {
	return map[string]any{"emoji_mode": "default", "time24": false, "display_real_names": false,
		"emoji_use": `{"smile":10,"tada":7,"+1":30,"fire":5,"eyes":9}`, "muted_channels": "",
		// Slack's frecency store (internal/frecency): emoji the account
		// reacted with, a query it typed, and a channel record
		"frecency": `{"E1f44d":{"id":"E1f44d","count":6,"visits":[1762500000000,1762510000000]},` +
			`"E1f389":{"id":"E1f389","count":4,"visits":[1762520000000]},"ta":{"id":"E1f389","count":2,"visits":[1762520000000]},` +
			`"E1f525":{"id":"E1f525","count":2,"visits":[1762530000000],"_reduced":true},` +
			`"C0GENERAL":{"id":"C0GENERAL","count":12,"visits":[1762540000000]}}`,
		"ia_theme":                    `{"primary":{"palette":"aubergine"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}`,
		"window_background_gradients": true,
		// Preferences part A (prefs_a.go): Slack's server defaults
		"show_sidebar_avatars": true, "home_unread_dot": true, "should_show_connect_section": true,
		"mute_sounds": false, "mute_huddle_sounds": false,
		"mac_ssb_bullet": true, "mac_ssb_bounce": "", "reminder_notification_time": "9:00",
		"tz": "America/Los_Angeles", "set_tz_automatically": true, "webapp_spellcheck": true,
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
		// Preferences audit (2026-11-08): the rest of the dialog's keys
		"msg_input_send_btn": false, "ia4_compact_mode": false, "preferred_skin_tone": "",
		"enable_unread_view": false, "enable_threads_view": true, "enable_huddles_home_view": true,
		"enable_sent_view": true, "enable_saved_view": true, "enable_unified_directory_view": true,
		"huddles_allow_smart_notif": true, "inactive_user_summary_notification": false, "activity_dms_opt_out": false,
		"overloaded_message_enabled": true, "in_prod_surveys_enabled": true,
		"all_notifications_prefs": `{"channels":{},"global":{"global_desktop":"mentions_dms","global_mobile":"mentions_dms",` +
			`"global_keywords":"","no_text_in_notifications":false,"push_idle_wait":0,"threads_everything":true,` +
			`"desktop_sound":"knock_brush.mp3","global_desktop_push_enabled":true,"global_channels_in_activity":false,` +
			`"global_reminders_in_activity":true}}`,
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
// SetHook makes f see every API call first; ok=true answers it with out
// (tests: hold a call back, fail it). nil removes it. Safe while calls are
// in flight (a booted client keeps loading in the background).
func (s *Server) SetHook(f func(method string, form url.Values) (out any, ok bool)) {
	s.hookMu.Lock()
	s.hook = f
	s.hookMu.Unlock()
}

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
	if sz.Identity {
		s.addIdentity()
	}
	if sz.Gallery {
		s.addGalleryConvs() // gallery.go
	}
	if sz.ChannelLinks {
		s.addChannelLinks() // channel_links.go
	}
	if sz.CodeChannels {
		s.addCodeChannels() // codechannels.go
	}
	if sz.Files {
		s.addFilesChannel() // files.go
	}
	if sz.Embeds {
		s.addEmbedsChannel() // embeds.go
	}
	if sz.ThreadHeader {
		s.addThreadHeader() // threadheader.go
	}
	if sz.ThreadsAudit {
		s.addThreadsAudit() // threads_audit.go
	}
	if sz.PluginBench {
		s.addPluginBench() // plugin_bench.go
	}
	if sz.Unreads {
		s.addUnreads() // unreads.go
	}
	if sz.Workflows {
		s.addWorkflowsChannel() // workflows.go
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.api)
	mux.HandleFunc("/img/", s.img)
	mux.HandleFunc("/ws", s.ws)
	mux.HandleFunc("/files-pri/", s.filesPri) // canvas exports (canvas.go)
	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(ln)
	return s
}

// Close stops the server.
func (s *Server) Close() error {
	searchLogs.Delete(s)
	viewsStates.Delete(s)
	threadsAuditStates.Delete(s)
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
	s.setupSelfProfile() // profile.go: no random draws
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
	s.convs = append(s.convs, galleryConv()) // gallery.go
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

// leave is conversations.leave: the channel goes back to the ones the
// viewer isn't in (a preview again), recorded in Leaves.
func (s *Server) leave(f url.Values) any {
	id := f.Get("channel")
	s.convMu.Lock()
	defer s.convMu.Unlock()
	s.Leaves = append(s.Leaves, id)
	for i, c := range s.convs {
		if c.ID != id {
			continue
		}
		if c.IsGeneral {
			return map[string]any{"ok": false, "error": "cant_leave_general"}
		}
		c.IsMember = false
		s.convs = append(s.convs[:i], s.convs[i+1:]...)
		s.others = append(s.others, c)
		return ok()
	}
	return ok("not_in_channel", true)
}

// LeaveCalls returns the channels conversations.leave was called for.
func (s *Server) LeaveCalls() []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]string(nil), s.Leaves...)
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

// UserListFilters returns the filter of every edge users/list call.
func (s *Server) UserListFilters() []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]string(nil), s.listFilters...)
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
	fault, faulted := s.takeFault(method)
	if faulted {
		if fault.Delay > 0 {
			time.Sleep(fault.Delay)
		}
		if !fault.AfterHandle && s.applyFault(w, fault) {
			return
		}
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
		if strings.HasPrefix(method, "upload/") && r.Header.Get("Cookie") != "" {
			form.Set(uploadCookieKey, "1")
		}
		if strings.HasPrefix(method, "upload/") {
			s.keepUploadBytes(strings.TrimPrefix(method, "upload/"), r.Header.Get("Content-Type"), body)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	var out any
	hooked := false
	s.hookMu.Lock()
	hook := s.hook
	s.hookMu.Unlock()
	if hook != nil {
		out, hooked = hook(method, form)
	}
	if eh := s.edgeHook(); !hooked && eh != nil && strings.HasPrefix(method, "edge/") { // extend.go
		out, hooked = eh(method, edge)
	}
	if !hooked {
		out = s.handle(method, form, edge)
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	if faulted && fault.AfterHandle && s.applyFault(w, fault) {
		return // handled, but the client never hears back
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
	if s.Size.Workflows { // first: search.go answers search.autocomplete.triggers with none
		if out, handled := s.handleWorkflows(method, f, edge); handled { // workflows.go
			return out
		}
	}
	if out, handled := s.handleFilesView(method, f); handled { // files_view.go
		return out
	}
	if out, handled := s.handleGallery(method, f); handled { // bkgallery*.go
		return out
	}
	if out, handled := s.handleLists(method, f); handled { // lists.go
		return out
	}
	if out, handled := s.handleCanvases(method, f); handled { // canvas.go
		return out
	}
	if out, handled := s.handleSearch(method, f); handled { // search.go
		return out
	}
	if out, handled := s.handleSections(method, f); handled { // sections.go
		return out
	}
	if out, handled := s.handleBlockActions(method, f); handled { // bkgallery_elements.go
		return out
	}
	if out, handled := s.handleAgents(method, f); handled { // agents.go
		return out
	}
	if out, handled := s.handleFiles(method, f); handled { // files.go
		return out
	}
	if out, handled := s.handleThreadHeader(method, f); handled { // threadheader.go
		return out
	}
	if out, handled := s.handleThreadsAudit(method, f); handled { // threads_audit.go
		return out
	}
	if out, handled := s.handleUnreads(method, f); handled { // unreads.go
		return out
	}
	// the bench threads' replies: ahead of the switch, whose
	// conversations.replies answers every other thread with none
	if method == "conversations.replies" && s.Size.PluginBench {
		if out, handled := s.handlePluginBench(method, f); handled { // plugin_bench.go
			return out
		}
	}
	switch method {
	case "auth.test":
		return ok("url", s.URL, "team", "Fake Club", "user", "self", "team_id", TeamID, "user_id", SelfID, "enterprise_id", EnterpriseID)
	case "users.profile.getSections":
		return s.profileSections(f) // profile.go
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
			if fx, ok := s.unreadsCount(c.ID); ok { // unreads.go
				e = fx
			}
			e.LastRead, e.HasUnreads, e.MentionCount = s.applyMark(c.ID, e.LastRead, e.Latest, e.HasUnreads, e.MentionCount)
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
			"dm_recency_sort_rollout": g("without_edu"), "reskin_custom_responses": g("on"), "temp_join_channel": g("on"),
			"sidebar_section_search": g("on"), "add_channels_channel_sections_slackathon": g("on"), "mute_channel_edu": g("on"),
			"session_channels": g("on"), "session_channel_views": g("on"), "code_channels_naming": g("on"),
			"composer_smart_symbols": g("on"), "agents_in_dm_sidebar": g("on"), "agents_in_dm_sidebar_pref": g("on"),
			"suggested_sections_auto_sort_v2": g("on"),
			"lists_tabbing_navigation":        g("on"),
			// focus mode and the Availability section (see core's defaultExperimentGroups)
			"silent_sidebar": g("on"), "availability_prefs_section": g("on"),
			// the thread pane (core's defaultExperimentGroups, live 2026-11-10)
			"nested_secondary_view": g("on"), "secondary_view_default_width": g("on"), "smaller_secondary_headers": g("on"),
			"unread_thread_indication_in_replybar": g("on")})
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
	case "users.emailtoken.get", "users.emailtoken.create", "users.emailtoken.delete": // Bring emails into Slack
		s.prefsMu.Lock()
		defer s.prefsMu.Unlock()
		switch method {
		case "users.emailtoken.create":
			s.emailTokens = append(s.emailTokens, map[string]any{"token_id": 1000 + len(s.emailTokens),
				"token": "fake", "email_address": "fake-" + strconv.Itoa(len(s.emailTokens)) + "@fake.slack.com"})
			return ok("token_id", s.emailTokens[len(s.emailTokens)-1]["token_id"], "token", "fake",
				"email_address", s.emailTokens[len(s.emailTokens)-1]["email_address"])
		case "users.emailtoken.delete":
			keep := s.emailTokens[:0]
			for _, t := range s.emailTokens {
				if strconv.Itoa(t["token_id"].(int)) != f.Get("email_token_id") {
					keep = append(keep, t)
				}
			}
			s.emailTokens = keep
			return ok()
		}
		if len(s.emailTokens) == 0 {
			return map[string]any{"ok": false, "error": "no_emailtoken"}
		}
		return ok("email_forwarding_addresses", s.emailTokens)
	case "users.priority.getRecommendations": // the VIP page's Recommended list
		var recs []map[string]any
		reasons := []string{"manager", "frequent_collaborator", "frequently_used", "skip_level"}
		for _, u := range s.users {
			if u.ID == SelfID || u.IsBot || u.Deleted || len(recs) == len(reasons) {
				continue
			}
			recs = append(recs, map[string]any{"user_id": u.ID, "source": "graph", "reason": reasons[len(recs)]})
		}
		return ok("users", recs, "request_id", "fake-rec")
	case "users.priority.add", "users.priority.remove":
		u := f.Get("user")
		if u == "" {
			return map[string]any{"ok": false, "error": "user_not_found"}
		}
		s.setVIP(u, method == "users.priority.add")
		return ok()
	case "team.prefs.get":
		prefs := s.TeamPrefs
		if prefs == nil {
			prefs = map[string]any{}
		}
		return ok("prefs", prefs)
	case "channels.prefs.get":
		ch, name := f.Get("channel_id"), f.Get("pref_name")
		var v any = map[string]any{}
		if p, found := s.ChannelPrefs[ch][name]; found {
			v = p
		}
		return ok("channel_id", ch, "pref_name", name, "pref_value", v)
	case "team.info":
		if f.Get("team") == ExternalTeamID {
			return ok("team", map[string]any{"id": ExternalTeamID, "name": "Partner Co", "domain": "partnerco",
				"icon": map[string]any{"image_34": s.URL + "img/34x34/partner.png", "image_44": s.URL + "img/44x44/partner.png",
					"image_88": s.URL + "img/88x88/partner.png"}})
		}
		return ok("team", map[string]any{"id": TeamID, "name": "Fake Club", "domain": "fakeclub",
			"icon": map[string]any{"image_88": s.URL + "img/88x88/team.png"}})
	case "apps.profile.get":
		if p, found := s.appProfile(f.Get("bot"), f.Get("app")); found {
			return ok("app_profile", p)
		}
		return map[string]any{"ok": false, "error": "app_not_found"}
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
		if f.Get("channel") == GalleryChannelID {
			if msgs, found := s.galleryReplies(f.Get("ts")); found {
				return ok("messages", msgs, "has_more", false)
			}
		}
		return ok("messages", []any{}, "has_more", false)
	case "conversations.info":
		if c, found := s.conv(f.Get("channel")); found {
			if f.Get("timezone_count") == "true" || f.Get("display_counts") == "true" {
				return ok("channel", withCounts(c, f))
			}
			return ok("channel", c)
		}
		if c, found := s.createdConv(f.Get("channel")); found {
			return ok("channel", c)
		}
		return map[string]any{"ok": false, "error": "channel_not_found"}
	case "conversations.join":
		return s.join(f)
	case "conversations.leave":
		return s.leave(f)
	case "chat.deleteAttachment":
		return s.deleteAttachment(f)
	case "chat.postMessage":
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
		msg := map[string]any{"type": "message", "ts": ts, "user": SelfID, "text": f.Get("text")}
		if th := f.Get("thread_ts"); th != "" {
			msg["thread_ts"] = th
		}
		if cid := f.Get("client_msg_id"); cid != "" {
			msg["client_msg_id"] = cid // Slack's reply carries it, as does the echo
		}
		if s.OnPost != nil {
			s.OnPost(f, msg)
		}
		return ok("ts", ts, "channel", f.Get("channel"), "message", msg)
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
		// filter as Slack's client sends it: "everyone AND NOT bots AND NOT
		// apps" (channel members, people), "apps" (Agents & apps: the fixed
		// bot users), anything else everyone.
		var filter string
		_ = json.Unmarshal(edge["filter"], &filter)
		s.convMu.Lock()
		s.listFilters = append(s.listFilters, filter)
		s.convMu.Unlock()
		if filter == "apps" {
			var bots []slack.User
			for _, u := range s.variants() {
				if u.IsBot && u.TeamID == TeamID {
					bots = append(bots, u)
				}
			}
			return ok("results", bots, "next_marker", "")
		}
		return ok("results", s.users[:min(len(s.users), 50)], "next_marker", "")
	case "edge/channels/info":
		var up map[string]int
		_ = json.Unmarshal(edge["updated_ids"], &up)
		return s.edgeChannelsInfo(up) // channel_links.go
	case "edge/permissions/info":
		var ch string
		_ = json.Unmarshal(edge["channel_id"], &ch)
		allowed := []string{"CAN_AT_CHANNEL", "CAN_AT_EVERYONE"}
		if s.Permissions != nil {
			allowed = append([]string{}, s.Permissions[ch]...)
		}
		return ok("allowed", allowed, "allowed_per_team", map[string]any{})
	case "edge/huddles/list":
		return ok("huddles", []any{})
	}
	if out, handled := s.handlePluginBench(method, f); handled && s.Size.PluginBench { // plugin_bench.go
		return out
	}
	if out, handled := s.handleIdentity(method, f); handled && s.Size.Identity { // identity.go
		return out
	}
	if out, handled := s.handleEmbeds(method, f); handled && s.Size.Embeds { // embeds.go
		return out
	}
	if out, handled := s.handleHuddles(method, f); handled { // huddles.go
		return out
	}
	if out, handled := s.handleCodeChannels(method, f); handled && s.Size.CodeChannels { // codechannels.go
		return out
	}
	if s.Size.Views {
		if out, handled := s.handleViews(method, f); handled { // views.go
			return out
		}
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
	for _, u := range s.variants() {
		if u.ID == id {
			return u, true
		}
	}
	if s.Size.Identity {
		for _, u := range s.identityUsers() {
			if u.ID == id {
				return u, true
			}
		}
	}
	if s.codeFx != nil && id == s.codeFx.Agent.ID {
		return s.codeFx.Agent, true
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
		var gallery bool
		if msgs, gallery = s.galleryHistoryOf(ch); !gallery { // gallery.go
			msgs = s.history(ch)
		}
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
	if oldest := f.Get("oldest"); oldest != "" {
		// Slack answers oldest (without latest) with the messages just
		// after it, newest first; has_more when newer ones remain.
		incl := f.Get("inclusive") == "true"
		stop := start
		for stop < len(msgs) && (slackTSLess(oldest, msgs[stop].TS) || incl && msgs[stop].TS == oldest) {
			stop++
		}
		from := start
		if latest == "" {
			from = max(start, stop-limit)
		}
		end = min(stop, from+limit)
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(map[string]any{"ok": true, "messages": msgs[from:end], "has_more": from > start, "pin_count": 0})
		return buf.Bytes()
	}
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
	if s.ImgLatency > 0 {
		time.Sleep(s.ImgLatency)
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/img/"), "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	if f := s.imageHook(); f != nil { // extend.go
		if b, ct, ok := f(strings.TrimPrefix(r.URL.Path, "/img/")); ok {
			w.Header().Set("Content-Type", ct)
			w.Write(b)
			return
		}
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
	// registered before hello: once the client is connected, Push reaches it
	s.addSocket(c) // Push (canvas.go)
	defer s.dropSocket(c)
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

// withCounts is conversations.info's channel with what the broadcast
// warning asks for (addCounts).
func withCounts(c slack.Conversation, f url.Values) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	addCounts(m, c, f)
	return m
}

// addCounts adds timezone_count (a stable 1–12 per channel) and
// display_counts {display_counts, guest_counts} when the request asks.
func addCounts(m map[string]any, c slack.Conversation, f url.Values) {
	if f.Get("timezone_count") == "true" {
		tz := 1
		for _, r := range c.ID {
			tz = (tz*31 + int(r)) % 12
		}
		m["timezone_count"] = tz + 1
	}
	if f.Get("display_counts") == "true" {
		m["display_counts"] = map[string]int{"display_counts": c.NumMembers, "guest_counts": 0}
	}
}
