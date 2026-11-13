// Package store keeps the client's state in memory.
//
// Scraping policy: message contents are only ever held in memory for the
// lifetime of the process (like any Slack client). Only metadata — the user
// directory, custom emoji, channel names and prefs — is cached on disk, in
// the account's cache directory (cachedir.go: 0600, per workspace and per
// user, deleted on sign-out).
package store

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Timeline is a contiguous window of a conversation's newest messages.
type Timeline struct {
	Msgs    []slack.Message // ascending by ts
	HasMore bool            // older messages exist beyond Msgs[0]
	Loaded  bool
}

type Store struct {
	mu          sync.RWMutex
	Self        slack.User
	TeamID      string
	TeamName    string
	TeamIcon    string
	TeamDomain  string
	Users       map[string]*slack.User
	Convs       map[string]*slack.Conversation
	Sections    []slack.Section
	Counts      map[string]*slack.CountEntry
	Usergroups  map[string]string
	Presence    map[string]string
	timelines   map[string]*Timeline
	threads     map[string][]slack.Message // key channel/ts
	pins        map[string]pinRec          // key channel/ts: pin events (pins.go)
	pinQ        []string                   // pins' keys, oldest first
	pinSeq      uint64                     // pin events seen
	CustomEmoji emoji.Custom
	EmojiStyle  string
	cacheDir    string

	// Groups holds full usergroup records (Usergroups maps id -> handle).
	Groups []slack.Usergroup
	// MyGroups are the user groups the viewer is in (Slack's
	// userGroupMembership: usergroups.membership, subteam_self_added and
	// subteam_self_removed; memory only)
	MyGroups map[string]bool
	// Muted is the set of muted conversation ids (from prefs).
	Muted map[string]bool
	// Prefs is the curated set of user prefs that change rendering.
	Prefs Prefs
	// RawPrefs is users.prefs.get's whole map (memory only: Preferences
	// reads the keys it shows from it; Prefs is what the disk cache keeps).
	RawPrefs map[string]json.RawMessage
	// ThreadsUnread / ThreadMentions mirror client.counts "threads", and
	// ThreadsUnreadByChannel its unread_count_by_channel.
	ThreadsUnread          bool
	ThreadMentions         int
	ThreadsUnreadByChannel map[string]int
	// names of conversations we are not a member of (from search/browse)
	names       map[string]string
	prefsCached bool
	ext         map[string]*slack.Conversation    // non-sidebar conversations
	access      map[string]richtext.ChannelAccess // edge channels/info answers for mentioned channels
	bots        map[string]BotInfo

	// userSeen is when each user was last fetched (unix seconds; from the
	// disk cache for users it restored): what SaveCache keeps.
	userSeen map[string]int64

	// gen counts changes to what the disk cache holds; SaveCache writes
	// only when it (or the emoji map's version) moved since the last load
	// or save.
	gen       atomic.Uint64
	saveMu    sync.Mutex
	dropped   atomic.Bool // DropDisk ran (signed out): never write again
	saved     bool
	savedGen  uint64
	savedEmo  uint64
	emojiAt   time.Time // when the cached emoji map was fetched (emoji.list)
	emojiAtMu sync.Mutex

	missMu     sync.Mutex
	missSeen   map[string]bool
	missing    []string // "c"+id | "u"+id | "b"+id
	missNotify chan struct{}
}

// PutName remembers the name of a conversation outside the sidebar.
// SetChannelAccess records what edge channels/info said about a channel
// mentioned in a message: visible, private (the viewer is in it) or
// hidden (in failed_ids). Memory only.
func (s *Store) SetChannelAccess(id string, acc richtext.ChannelAccess) {
	if id == "" {
		return
	}
	s.mu.Lock()
	if s.access == nil {
		s.access = map[string]richtext.ChannelAccess{}
	}
	s.access[id] = acc
	s.mu.Unlock()
}

// ChannelAccess is richtext.ChannelAccessResolver: a stored conversation
// is visible (private ones with the lock), else what edge channels/info
// said, else unresolved.
func (s *Store) ChannelAccess(id string) richtext.ChannelAccess {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.Convs[id]
	if !ok {
		c, ok = s.ext[id]
	}
	if ok {
		if c.IsPrivate && !c.IsIM && !c.IsMPIM {
			return richtext.ChannelPrivate
		}
		return richtext.ChannelVisible
	}
	return s.access[id]
}

// ExperimentGroup reads a Slack experiment's group for richtext's
// experiment-dependent text (core sets it to its Experiment).
var ExperimentGroup func(s *Store, name string) string

// HuddleTextOpts is richtext.HuddleResolver: a huddle_thread message's
// conversation (a DM and its other member, a group DM) and the
// experiments its sentence follows.
func (s *Store) HuddleTextOpts(channel string) richtext.HuddleTextOpts {
	s.mu.RLock()
	c, ok := s.Convs[channel]
	if !ok {
		c, ok = s.ext[channel]
	}
	var o richtext.HuddleTextOpts
	if ok {
		o.IM, o.MPIM, o.Partner = c.IsIM, c.IsMPIM, c.User
	}
	s.mu.RUnlock()
	if f := ExperimentGroup; f != nil {
		o.Redesign = f(s, "system_messages_redesign_m1") == "on"
		o.Missed = f(s, "huddles_missed_notification") == "on"
	}
	return o
}

// UserAvatar is richtext.AvatarResolver: a user's 48px picture ("" when
// unknown).
func (s *Store) UserAvatar(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.Users[id]; ok {
		return u.Profile.Image48
	}
	return ""
}

func (s *Store) PutName(id, name string) {
	if id == "" || name == "" {
		return
	}
	s.mu.Lock()
	if s.names == nil {
		s.names = map[string]string{}
	}
	s.names[id] = name
	s.mu.Unlock()
	s.gen.Add(1)
}

// Prefs are the user preferences the UI needs for rendering.
type Prefs struct {
	MorePrefs               // prefs_more.go
	EmojiStyle       string `json:"emoji_style"` // emoji.Style(): Slack's set follows the platform, no pref picks it
	EmojiMode        string `json:"emoji_mode,omitempty"`
	SkinTone         string `json:"skin_tone,omitempty"`
	Time24           bool   `json:"time24"`
	DisplayRealNames bool   `json:"display_real_names"`
	Jumbomoji        bool   `json:"jumbomoji"`
	ConvertEmoticons bool   `json:"convert_emoticons"`
	SidebarBehavior  string `json:"sidebar_behavior,omitempty"`
	ChannelSort      string `json:"channel_sort,omitempty"`
	Locale           string `json:"locale,omitempty"`
	Theme            string `json:"theme,omitempty"`
	DarkMode         string `json:"dark_mode,omitempty"`
	SidebarTheme     string `json:"sidebar_theme,omitempty"`
	MessagesTheme    string `json:"messages_theme,omitempty"`
	// IATheme is Slack's 2026 theme, users.prefs ia_theme: a JSON string
	// {primary,highlight1,highlight2,important:{palette,custom},
	// brightness, sidebarInverted, useCustomHex} (theme.ParseIATheme).
	IATheme string `json:"ia_theme,omitempty"`
	// NoWindowGradients is window_background_gradients=false (Appearance >
	// "Show gradients": a flat chrome instead of Slack's conic gradient).
	NoWindowGradients bool `json:"no_window_gradients,omitempty"`
	EnterIsSpecial    bool `json:"enter_is_special_in_tbt"`
	// PlainTextMode is Preferences > Advanced > "Format messages with
	// markup": the composer keeps markup as typed (no live formatting, no
	// toolbar) and formats it when the message is sent.
	PlainTextMode     bool     `json:"plain_text_mode"`
	ShowTyping        bool     `json:"show_typing"`
	ColorNames        bool     `json:"color_names_in_list"`
	FrequentEmoji     []string `json:"frequent_emoji,omitempty"`
	Muted             []string `json:"muted"`
	HideInactiveConvs bool     `json:"hide_inactive"`
	// Sidebar rules (see internal/core/sidebar_rules.go): the
	// channel_sections pref (section id -> {"sidebar": filter, "sort": …,
	// "c": collapsed}) and the prefs that group and hide rows.
	SectionPrefs     map[string]map[string]string `json:"section_prefs,omitempty"`
	ShowMutedOutside bool                         `json:"show_muted_outside,omitempty"` // show_muted_items_outside_sidebar_menus
	BoostMentions    bool                         `json:"boost_mentions,omitempty"`
	SeparatePrivate  bool                         `json:"separate_private,omitempty"`
	UndoIntermix     bool                         `json:"undo_intermix,omitempty"` // undo_channel_intermix
	// Tab rail (internal/core/rail.go): Preferences > Navigation.
	TabRailCustomization map[string]bool `json:"tab_rail_customization,omitempty"` // tab -> shown in the rail
	TabRailOrder         []string        `json:"tab_rail_order,omitempty"`         // tab_rail_customization_order
	TabRailOrderSet      bool            `json:"tab_rail_order_set,omitempty"`     // the order pref parsed as an array
	HasUploaded          bool            `json:"has_uploaded,omitempty"`
	HasStarredItem       bool            `json:"has_starred_item,omitempty"`
	ActivityDMsOptOut    bool            `json:"activity_dms_opt_out,omitempty"`
	CompactRail          bool            `json:"compact_rail,omitempty"` // ia4_compact_mode
	// Notifications: global defaults and per-conversation overrides
	// (users.prefs.get all_notifications_prefs).
	GlobalDesktop string                   `json:"global_desktop,omitempty"` // everything | mentions_dms | nothing
	GlobalMobile  string                   `json:"global_mobile,omitempty"`
	ChannelNotify map[string]ChannelNotify `json:"channel_notify,omitempty"`
	// NoTextInNotifications is the global no_text_in_notifications
	// (Notifications > "Include a preview of the message"): desktop
	// notifications show no message text.
	NoTextInNotifications bool `json:"no_text_in_notifications,omitempty"`
	// NoDesktopPush is global_desktop_push_enabled=false (Notifications >
	// "Desktop notifications" off): no desktop notifications at all.
	NoDesktopPush bool `json:"no_desktop_push,omitempty"`
	// SendOnModEnter is msg_input_send_btn (Advanced > "When writing a
	// message, press Enter to… Start a new line"): Enter adds a line and
	// Mod+Enter sends.
	SendOnModEnter bool `json:"msg_input_send_btn,omitempty"`
	// HiddenViews are the Home > "Always show in the sidebar" links turned
	// off: enable_threads_view, enable_huddles_home_view, enable_sent_view,
	// enable_saved_view, enable_unified_directory_view (=false).
	HiddenViews []string `json:"hidden_views,omitempty"`

	// Render prefs (Preferences dialog). Named so the zero value is
	// Slack's default: the disk cache drops false fields.
	UnderlineLinks bool `json:"underline_links,omitempty"` // underline_links
	NoAnimations   bool `json:"no_animations,omitempty"`   // a11y_animations=false
	HideHexSwatch  bool `json:"hide_hex_swatch,omitempty"` // hide_hex_swatch
	// NoInternalImgs is expand_internal_inline_imgs=false (images uploaded
	// to Slack show as file cards); NoInlineImgs expand_inline_imgs=false
	// (no images from linked websites); NoTextPreviews
	// expand_non_media_attachments=false (no text previews of links).
	NoInternalImgs bool `json:"no_internal_imgs,omitempty"`
	NoInlineImgs   bool `json:"no_inline_imgs,omitempty"`
	NoTextPreviews bool `json:"no_text_previews,omitempty"`
	// MarkReadImmediately and StartAtOldest are Mark as read's
	// mark_msgs_read_immediately and start_scroll_at_oldest (both false:
	// Slack's "default", leave unseen messages unread).
	MarkReadImmediately bool `json:"mark_msgs_read_immediately,omitempty"`
	StartAtOldest       bool `json:"start_scroll_at_oldest,omitempty"`
}

// ChannelNotify is a conversation's notification override.
type ChannelNotify struct {
	Desktop           string `json:"desktop,omitempty"` // everything | mentions_dms | nothing | default
	Mobile            string `json:"mobile,omitempty"`
	Muted             bool   `json:"muted,omitempty"`
	FollowAllThreads  bool   `json:"follow_all_threads,omitempty"`
	SuppressAtChannel bool   `json:"suppress_at_channel,omitempty"`
	// TempMuteExp is temp_mute_expiration_ts: muted until then (Slack's
	// "Temporarily mute…" Until tomorrow / 1 hour), 0 when not.
	TempMuteExp int64 `json:"temp_mute_expiration_ts,omitempty"`
	// BadgeAllUnreads is badge_all_unreads ("Badge every new message in
	// your sidebar"); DesktopPush is desktop_push_enabled, "" when the
	// channel follows global_desktop_push_enabled, else "true" / "false".
	BadgeAllUnreads bool   `json:"badge_all_unreads,omitempty"`
	DesktopPush     string `json:"desktop_push_enabled,omitempty"`
}

// parseSectionPrefs reads the channel_sections pref, a JSON object of
// section id -> {pref: value}; values become strings ("c":"0", "sidebar":"hid").
func parseSectionPrefs(s string) map[string]map[string]string {
	if s == "" {
		return nil
	}
	var raw map[string]map[string]any
	if json.Unmarshal([]byte(s), &raw) != nil {
		return nil
	}
	out := make(map[string]map[string]string, len(raw))
	for id, kv := range raw {
		m := make(map[string]string, len(kv))
		for k, v := range kv {
			switch x := v.(type) {
			case string:
				m[k] = x
			case nil:
			default:
				b, _ := json.Marshal(x)
				m[k] = string(b)
			}
		}
		out[id] = m
	}
	return out
}

// ParsePrefs extracts Prefs (and the muted set) from users.prefs.get.
func ParsePrefs(raw map[string]json.RawMessage) (Prefs, map[string]bool) {
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(raw[k], &v)
		return v
	}
	// jsonStr is a JSON-typed pref: Slack sends it as a string holding
	// JSON; an object is taken as is.
	jsonStr := func(k string) string {
		if v := str(k); v != "" {
			return v
		}
		if r := raw[k]; len(r) > 0 && r[0] == '{' {
			return string(r)
		}
		return ""
	}
	boolean := func(k string, def bool) bool {
		v, ok := raw[k]
		if !ok {
			return def
		}
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return b
		}
		var n int
		if json.Unmarshal(v, &n) == nil {
			return n != 0
		}
		return def
	}
	// display_real_names_override is 1 (full names), -1 (display names)
	// or 0 (the team's display_real_names default), like Slack's
	// MessageNamesPref.
	override := 0
	if v, ok := raw["display_real_names_override"]; ok {
		var n float64
		if json.Unmarshal(v, &n) == nil {
			override = int(n)
		} else if boolean("display_real_names_override", false) {
			override = 1
		}
	}
	p := Prefs{
		EmojiMode: str("emoji_mode"), SkinTone: str("preferred_skin_tone"), Time24: boolean("time24", false),
		DisplayRealNames: override > 0 || (override == 0 && boolean("display_real_names", false)),
		Jumbomoji:        boolean("jumbomoji", true), ConvertEmoticons: boolean("convert_emoticons", true),
		SidebarBehavior: str("sidebar_behavior"), ChannelSort: str("channel_sort"), Locale: str("locale"),
		Theme: str("theme"), DarkMode: str("dark_mode"), SidebarTheme: str("sidebar_theme"), MessagesTheme: str("messages_theme"),
		EnterIsSpecial: boolean("enter_is_special_in_tbt", false), ShowTyping: boolean("show_typing", true),
		ColorNames:    boolean("color_names_in_list", true),
		PlainTextMode: boolean("plain_text_mode", false),
		IATheme:       jsonStr("ia_theme"), NoWindowGradients: !boolean("window_background_gradients", true),
		UnderlineLinks: boolean("underline_links", false), NoAnimations: !boolean("a11y_animations", true),
		HideHexSwatch:  boolean("hide_hex_swatch", false),
		NoInternalImgs: !boolean("expand_internal_inline_imgs", true), NoInlineImgs: !boolean("expand_inline_imgs", true),
		NoTextPreviews:      !boolean("expand_non_media_attachments", true),
		MarkReadImmediately: boolean("mark_msgs_read_immediately", false), StartAtOldest: boolean("start_scroll_at_oldest", false),
	}
	p.HideInactiveConvs = p.SidebarBehavior == "hide_inactive_channels"
	p.ShowMutedOutside = boolean("show_muted_items_outside_sidebar_menus", false)
	p.BoostMentions = boolean("boost_mentions", false)
	p.SeparatePrivate = boolean("separate_private_channels", false)
	p.UndoIntermix = boolean("undo_channel_intermix", false)
	p.SectionPrefs = parseSectionPrefs(str("channel_sections"))
	if s := str("tab_rail_customization"); s != "" {
		var m map[string]bool
		if json.Unmarshal([]byte(s), &m) == nil && len(m) > 0 {
			p.TabRailCustomization = m
		}
	}
	if s := str("tab_rail_customization_order"); s != "" {
		var order []any
		if json.Unmarshal([]byte(s), &order) == nil {
			p.TabRailOrderSet = true
			for _, v := range order {
				if t, ok := v.(string); ok {
					p.TabRailOrder = append(p.TabRailOrder, t)
				}
			}
		}
	}
	p.HasUploaded = boolean("has_uploaded", false)
	p.HasStarredItem = boolean("has_starred_item", false)
	p.ActivityDMsOptOut = boolean("activity_dms_opt_out", false)
	p.CompactRail = boolean("ia4_compact_mode", false)
	p.SendOnModEnter = boolean("msg_input_send_btn", false)
	// shouldShowAllUnreadsLink: the Unreads link needs enable_unread_view true
	if !boolean("enable_unread_view", false) {
		p.HiddenViews = append(p.HiddenViews, "enable_unread_view")
	}
	for _, k := range []string{"enable_threads_view", "enable_huddles_home_view", "enable_sent_view", "enable_saved_view", "enable_unified_directory_view"} {
		if !boolean(k, true) {
			p.HiddenViews = append(p.HiddenViews, k)
		}
	}
	// Slack's getEmojiMode: emoji_mode only says "as_text" or "default";
	// the image set is Apple's on macOS and Google's everywhere else.
	p.EmojiStyle = emoji.Style()
	var use struct {
		Recent []string `json:"recent"`
	}
	if s := str("emoji_use"); s != "" {
		var counts map[string]int
		if json.Unmarshal([]byte(s), &counts) == nil {
			for n := range counts {
				use.Recent = append(use.Recent, n)
			}
			sort.Slice(use.Recent, func(i, j int) bool {
				a, b := counts[use.Recent[i]], counts[use.Recent[j]]
				if a != b {
					return a > b
				}
				return use.Recent[i] < use.Recent[j]
			})
			if len(use.Recent) > 40 {
				use.Recent = use.Recent[:40]
			}
		}
	}
	p.FrequentEmoji = use.Recent
	muted := map[string]bool{}
	for _, id := range strings.Split(str("muted_channels"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			muted[id] = true
		}
	}
	if s := str("all_notifications_prefs"); s != "" {
		var n struct {
			Channels map[string]struct {
				Desktop           string          `json:"desktop"`
				Mobile            string          `json:"mobile"`
				Muted             json.RawMessage `json:"muted"`
				FollowAllThreads  json.RawMessage `json:"follow_all_threads"`
				SuppressAtChannel json.RawMessage `json:"suppress_at_channel"`
				TempMuteExp       json.RawMessage `json:"temp_mute_expiration_ts"`
				BadgeAllUnreads   json.RawMessage `json:"badge_all_unreads"`
				DesktopPush       json.RawMessage `json:"desktop_push_enabled"`
			} `json:"channels"`
			Global map[string]json.RawMessage `json:"global"`
		}
		if json.Unmarshal([]byte(s), &n) == nil {
			truthy := func(r json.RawMessage) bool {
				v := strings.Trim(string(r), `"`)
				return v == "true" || v == "1"
			}
			p.ChannelNotify = map[string]ChannelNotify{}
			for id, c := range n.Channels {
				cn := ChannelNotify{Desktop: c.Desktop, Mobile: c.Mobile, Muted: truthy(c.Muted),
					FollowAllThreads: truthy(c.FollowAllThreads), SuppressAtChannel: truthy(c.SuppressAtChannel),
					BadgeAllUnreads: truthy(c.BadgeAllUnreads)}
				if len(c.DesktopPush) > 0 && string(c.DesktopPush) != "null" {
					cn.DesktopPush = strconv.FormatBool(truthy(c.DesktopPush))
				}
				if v, err := strconv.ParseInt(strings.Trim(string(c.TempMuteExp), `"`), 10, 64); err == nil {
					cn.TempMuteExp = v
				}
				p.ChannelNotify[id] = cn
				if cn.Muted {
					muted[id] = true
				} else {
					delete(muted, id)
				}
			}
			var gd, gm string
			_ = json.Unmarshal(n.Global["global_desktop"], &gd)
			_ = json.Unmarshal(n.Global["global_mobile"], &gm)
			p.GlobalDesktop, p.GlobalMobile = gd, gm
			p.NoTextInNotifications = truthy(n.Global["no_text_in_notifications"])
			if r, ok := n.Global["global_desktop_push_enabled"]; ok { // absent: on
				v := strings.Trim(string(r), `"`)
				p.NoDesktopPush = v == "false" || v == "0"
			}
		}
	}
	p.parseMore(raw, str)
	p.Muted = make([]string, 0, len(muted))
	for id := range muted {
		p.Muted = append(p.Muted, id)
	}
	sort.Strings(p.Muted)
	return p, muted
}

// SetRawPrefs keeps users.prefs.get's whole map (memory only).
func (s *Store) SetRawPrefs(raw map[string]json.RawMessage) {
	s.mu.Lock()
	s.RawPrefs = raw
	s.mu.Unlock()
}

// SetPrefs installs parsed prefs (and the emoji style used for rendering).
func (s *Store) SetPrefs(p Prefs, muted map[string]bool) {
	s.mu.Lock()
	if reflect.DeepEqual(s.Prefs, p) && reflect.DeepEqual(s.Muted, muted) {
		s.mu.Unlock()
		return // refresh with no change: keep the cache clean
	}
	s.Prefs = p
	s.Muted = muted
	s.EmojiStyle = emoji.Style() // not p's: a cached older build's "apple" stays out
	s.mu.Unlock()
	s.gen.Add(1)
}

func New(cacheDir string) *Store {
	return &Store{
		Users: map[string]*slack.User{}, Convs: map[string]*slack.Conversation{},
		Counts: map[string]*slack.CountEntry{}, Usergroups: map[string]string{},
		Presence:  map[string]string{},
		timelines: map[string]*Timeline{}, threads: map[string][]slack.Message{},
		cacheDir: cacheDir, EmojiStyle: emoji.Style(), Muted: map[string]bool{},
		Prefs: Prefs{EmojiStyle: emoji.Style(), Jumbomoji: true, ShowTyping: true, Muted: []string{}},
	}
}

func (s *Store) Lock()    { s.mu.Lock() }
func (s *Store) Unlock()  { s.mu.Unlock() }
func (s *Store) RLock()   { s.mu.RLock() }
func (s *Store) RUnlock() { s.mu.RUnlock() }

// ---- users ----

func (s *Store) PutUsers(us []slack.User) {
	now := time.Now().Unix()
	s.mu.Lock()
	if s.userSeen == nil {
		s.userSeen = map[string]int64{}
	}
	for i := range us {
		u := us[i]
		if old, ok := s.Users[u.ID]; ok && old.Presence != "" {
			u.Presence = old.Presence
		}
		s.Users[u.ID] = &u
		s.userSeen[u.ID] = now
	}
	s.mu.Unlock()
	if len(us) > 0 {
		s.gen.Add(1)
	}
}

// putUserPtrs is PutUsers for users the store may keep as they are (the
// metadata cache's, decoded one by one: no second copy), fetched at the
// times seen says.
func (s *Store) putUserPtrs(us []*slack.User, seen map[string]int64) {
	s.mu.Lock()
	if s.userSeen == nil {
		s.userSeen = map[string]int64{}
	}
	for _, u := range us {
		if old, ok := s.Users[u.ID]; ok && old.Presence != "" {
			u.Presence = old.Presence
		}
		s.Users[u.ID] = u
		if _, newer := s.userSeen[u.ID]; !newer {
			s.userSeen[u.ID] = seen[u.ID]
		}
	}
	s.mu.Unlock()
	if len(us) > 0 {
		s.gen.Add(1)
	}
}

func (s *Store) User(id string) (*slack.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.Users[id]
	return u, ok
}

// MissingUsers filters ids to those not yet known.
func (s *Store) MissingUsers(ids []string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" || seen[id] || !(strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W")) {
			continue
		}
		seen[id] = true
		if _, ok := s.Users[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// DisplayName mirrors Slack: display_name, else real_name, else name.
func DisplayName(u *slack.User) string {
	if u == nil {
		return ""
	}
	if n := strings.TrimSpace(u.Profile.DisplayName); n != "" {
		return n
	}
	if n := strings.TrimSpace(u.Profile.RealName); n != "" && n != "​" {
		return n
	}
	if n := strings.TrimSpace(u.RealName); n != "" && n != "​" {
		return n
	}
	return u.Name
}

// PrefName is Slack's getPrefCompliantMemberNamePure (client-boot vUCu):
// the display name, unless displayRealNames (the display_real_names pref)
// asks for real names, then the real name (getMemberRealName, Yn47:
// profile.real_name, else real_name). Unlike DisplayName there is no
// username fallback and nothing is trimmed: a real name of a lone
// zero-width space is that space.
func PrefName(u *slack.User, displayRealNames bool) string {
	if u == nil {
		return ""
	}
	if !displayRealNames && u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.RealName
}

// AvatarURL is u's picture for a slot of size px: the profile image that
// covers it, else the ca.slack-edge.com picture of avatar_hash under
// avatarTeam, else Slack's default. viewerTeam is the viewer's team.
func AvatarURL(u *slack.User, size int, viewerTeam string) string {
	p := u.Profile
	switch {
	case size <= 72 && p.Image72 != "":
		return p.Image72
	case size > 72 && size <= 192 && p.Image192 != "":
		return p.Image192
	case size > 192 && p.Image512 != "":
		return p.Image512
	}
	if p.AvatarHash != "" {
		return "https://ca.slack-edge.com/" + avatarTeam(u, viewerTeam) + "-" + u.ID + "-" + p.AvatarHash + "-" + strconv.Itoa(size)
	}
	if p.Image48 != "" {
		return p.Image48
	}
	return "https://a.slack-edge.com/80588/img/avatars/ava_0001-" + strconv.Itoa(size) + ".png"
}

// avatarTeam is the team in Slack's avatar URLs (client-boot ppn0
// computeImages, which builds every member's image_24…image_1024 as
// <avatar base><team>-<id>-<avatar_hash>-<size>): an app or bot's
// profile.team, else computeTeamId: team_id, else the org's id when the
// user is the enterprise user itself, else the viewer's team.
func avatarTeam(u *slack.User, viewerTeam string) string {
	if (u.IsAppUser || u.IsBot) && u.Profile.Team != "" {
		return u.Profile.Team
	}
	if u.TeamID != "" {
		return u.TeamID
	}
	if eu := u.EnterpriseUser; eu != nil && eu.ID == u.ID && eu.EnterpriseID != "" {
		return eu.EnterpriseID
	}
	if viewerTeam != "" {
		return viewerTeam
	}
	return u.Profile.Team
}

// ---- richtext.Resolver ----

func (s *Store) UserName(id string) string {
	u, ok := s.User(id)
	if !ok {
		s.noteMissing('u', id)
	}
	return DisplayName(u)
}

// MemberName is richtext.MemberNamer: Slack's getPrefCompliantMemberName
// (client-boot vUCu sE) for id under the viewer's display_real_names
// pref, "Name hidden" for someone in their hidden_users (0OuZ
// isUserHidden). ok is false for a member not loaded yet, which is noted
// to be fetched.
func (s *Store) MemberName(id string) (string, bool) {
	u, ok := s.User(id)
	if !ok || u == nil {
		s.noteMissing('u', id)
		return "", false
	}
	s.mu.RLock()
	real := s.Prefs.DisplayRealNames
	hidden := hiddenUser(s.RawPrefs["hidden_users"], id)
	s.mu.RUnlock()
	if hidden {
		return "Name hidden", true
	}
	return PrefName(u, real), true
}

// MemberRealName is richtext.RealNamer: the member's real name, as the
// member tokens Slack draws with forceRealName print it (a work object's
// user fields), else their display name; "" for a member not loaded yet.
func (s *Store) MemberRealName(id string) string {
	u, ok := s.User(id)
	if !ok || u == nil {
		s.noteMissing('u', id)
		return ""
	}
	if n := firstNonEmptyName(u.Profile.RealName, u.RealName); n != "" {
		return n
	}
	return DisplayName(u)
}

func firstNonEmptyName(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// MemberAvatar is richtext.MemberNamer: the member's own picture for a
// 36px avatar (ConnectedBaseAvatar asks for twice that), the image
// message rows draw; "" for a member not loaded yet.
func (s *Store) MemberAvatar(id string) string {
	u, ok := s.User(id)
	if !ok || u == nil {
		return ""
	}
	s.mu.RLock()
	team := s.TeamID
	s.mu.RUnlock()
	return AvatarURL(u, 72, team)
}

// hiddenUser reports whether the hidden_users pref (a comma-separated
// string of ids, People you've hidden) holds id.
func hiddenUser(raw json.RawMessage, id string) bool {
	if len(raw) == 0 || id == "" {
		return false
	}
	var list string
	if json.Unmarshal(raw, &list) != nil {
		return false
	}
	for _, h := range strings.Split(list, ",") {
		if strings.TrimSpace(h) == id {
			return true
		}
	}
	return false
}

func (s *Store) ChannelName(id string) string {
	s.mu.RLock()
	c, ok := s.Convs[id]
	if !ok {
		c, ok = s.ext[id]
	}
	if ok {
		defer s.mu.RUnlock()
		if c.IsIM {
			if u, ok := s.Users[c.User]; ok {
				return DisplayName(u)
			}
			s.noteMissing('u', c.User)
		}
		return c.Name
	}
	n, named := s.names[id]
	s.mu.RUnlock()
	if named {
		return n
	}
	s.noteMissing('c', id)
	return id
}

// ---- unresolved ids (resolved in the background by core) ----

// noteMissing records an id that rendering could not name. Ids are only
// recorded once per session (see TakeMissing).
func (s *Store) noteMissing(kind byte, id string) {
	if id == "" {
		return
	}
	switch kind {
	case 'u':
		if !(strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W")) {
			return
		}
	case 'c':
		if !(strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G") || strings.HasPrefix(id, "D")) {
			return
		}
	}
	key := string(kind) + id
	s.missMu.Lock()
	if s.missSeen == nil {
		s.missSeen = map[string]bool{}
		s.missNotify = make(chan struct{}, 1)
	}
	if !s.missSeen[key] {
		s.missSeen[key] = true
		s.missing = append(s.missing, key)
		select {
		case s.missNotify <- struct{}{}:
		default:
		}
	}
	s.missMu.Unlock()
}

// NoteMissingBot records a bot id whose name/icon is unknown.
func (s *Store) NoteMissingBot(id string) {
	if strings.HasPrefix(id, "B") {
		s.noteMissing('b', id)
	}
}

// MissingNotify fires (coalesced) when new unresolved ids were recorded.
func (s *Store) MissingNotify() <-chan struct{} {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	if s.missNotify == nil {
		s.missSeen = map[string]bool{}
		s.missNotify = make(chan struct{}, 1)
	}
	return s.missNotify
}

// TakeMissing returns and clears the unresolved channel, user and bot ids.
func (s *Store) TakeMissing() (chans, users, bots []string) {
	s.missMu.Lock()
	keys := s.missing
	s.missing = nil
	s.missMu.Unlock()
	for _, k := range keys {
		switch k[0] {
		case 'c':
			chans = append(chans, k[1:])
		case 'u':
			users = append(users, k[1:])
		case 'b':
			bots = append(bots, k[1:])
		}
	}
	return
}

// ForgetMissing lets ids be recorded (and resolved) again, e.g. after a
// failed lookup that may succeed later.
func (s *Store) ForgetMissing(kind byte, ids ...string) {
	s.missMu.Lock()
	for _, id := range ids {
		delete(s.missSeen, string(kind)+id)
	}
	s.missMu.Unlock()
}

// ---- conversations outside the sidebar (closed DMs, previews) ----

// PutExtConv remembers a conversation the viewer is not showing in the
// sidebar (metadata, memory only).
func (s *Store) PutExtConv(c slack.Conversation) {
	if c.ID == "" {
		return
	}
	s.mu.Lock()
	if s.ext == nil {
		s.ext = map[string]*slack.Conversation{}
	}
	s.ext[c.ID] = &c
	if c.Name != "" && !c.IsIM {
		if s.names == nil {
			s.names = map[string]string{}
		}
		s.names[c.ID] = c.Name
		s.gen.Add(1)
	}
	s.mu.Unlock()
}

// ExtConv returns a non-sidebar conversation.
func (s *Store) ExtConv(id string) (*slack.Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.ext[id]
	return c, ok
}

// ---- bots ----

// BotInfo is a bot's display metadata (bots.info), cached on disk.
type BotInfo struct {
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"`
	AppID    string `json:"app_id,omitempty"`
	Workflow bool   `json:"is_workflow_bot,omitempty"` // a Workflow Builder bot (badged WORKFLOW)
}

func (s *Store) PutBot(id string, b BotInfo) {
	s.mu.Lock()
	if s.bots == nil {
		s.bots = map[string]BotInfo{}
	}
	s.bots[id] = b
	s.mu.Unlock()
	s.gen.Add(1)
}

// BotName is richtext.BotNamer: the cached bot's name, or "" after
// asking for it (bots.info in the background; a names event follows).
func (s *Store) BotName(id string) string {
	if b, ok := s.Bot(id); ok && b.Name != "" {
		return b.Name
	}
	s.NoteMissingBot(id)
	return ""
}

func (s *Store) Bot(id string) (BotInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bots[id]
	return b, ok
}

func (s *Store) UsergroupName(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n, ok := s.Usergroups[id]; ok {
		return n
	}
	return id
}

func (s *Store) Emoji(name string) (string, string, bool) {
	s.mu.RLock()
	style := s.EmojiStyle
	s.mu.RUnlock()
	return s.CustomEmoji.Resolve(name, style)
}

func (s *Store) SelfID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Self.ID
}

// ---- conversations ----

func (s *Store) PutConv(c slack.Conversation) {
	s.mu.Lock()
	if old, ok := s.Convs[c.ID]; ok {
		if c.NumMembers == 0 {
			c.NumMembers = old.NumMembers
		}
		if c.LastRead == "" {
			c.LastRead = old.LastRead
		}
	}
	s.Convs[c.ID] = &c
	s.mu.Unlock()
}

func (s *Store) Conv(id string) (*slack.Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.Convs[id]
	return c, ok
}

// ---- timelines ----

func (s *Store) Timeline(ch string) *Timeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.timelines[ch]
	if t == nil {
		return nil
	}
	cp := *t
	cp.Msgs = append([]slack.Message(nil), t.Msgs...)
	return &cp
}

// TimelineIDs lists the conversations with a cached newest window.
func (s *Store) TimelineIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.timelines))
	for id := range s.timelines {
		ids = append(ids, id)
	}
	return ids
}

// SetLatest replaces the newest window (from a fresh history fetch).
func (s *Store) SetLatest(ch string, msgs []slack.Message, hasMore bool) {
	SortAsc(msgs)
	s.mu.Lock()
	s.timelines[ch] = &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true}
	s.mu.Unlock()
}

// PrependOlder adds an older page to the front.
func (s *Store) PrependOlder(ch string, msgs []slack.Message, hasMore bool) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil {
		s.timelines[ch] = &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true}
		return
	}
	merged := append(append([]slack.Message(nil), msgs...), t.Msgs...)
	t.Msgs = dedupe(merged)
	t.HasMore = hasMore
	// bound memory per channel
	if len(t.Msgs) > 5000 {
		t.Msgs = t.Msgs[len(t.Msgs)-5000:]
	}
}

// Upsert inserts or replaces a top-level message in a loaded timeline.
func (s *Store) Upsert(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil {
		return
	}
	for i := range t.Msgs {
		if t.Msgs[i].TS == m.TS {
			s.keepPinLocked(ch, &m, &t.Msgs[i])
			t.Msgs[i] = m
			return
		}
	}
	s.keepPinLocked(ch, &m, nil)
	t.Msgs = append(t.Msgs, m)
	SortAsc(t.Msgs)
}

// Mutate applies f to a message in the timeline and its thread cache.
func (s *Store) Mutate(ch, ts string, f func(*slack.Message)) (slack.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutateLocked(ch, ts, f)
}

func (s *Store) mutateLocked(ch, ts string, f func(*slack.Message)) (slack.Message, bool) {
	var out slack.Message
	found := false
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == ts {
				f(&t.Msgs[i])
				out, found = t.Msgs[i], true
			}
		}
	}
	for k, th := range s.threads {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for i := range th {
			if th[i].TS == ts {
				f(&th[i])
				if !found {
					out, found = th[i], true
				}
			}
		}
	}
	return out, found
}

func (s *Store) Remove(ch, ts string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == ts {
				t.Msgs = append(t.Msgs[:i], t.Msgs[i+1:]...)
				break
			}
		}
	}
	for k, th := range s.threads {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for i := range th {
			if th[i].TS == ts {
				s.threads[k] = append(th[:i], th[i+1:]...)
				break
			}
		}
	}
}

func (s *Store) Find(ch, ts string) (slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil {
		for _, m := range t.Msgs {
			if m.TS == ts {
				return m, true
			}
		}
	}
	for k, th := range s.threads {
		if strings.HasPrefix(k, ch+"/") {
			for _, m := range th {
				if m.TS == ts {
					return m, true
				}
			}
		}
	}
	return slack.Message{}, false
}

// MsgRef names a cached message.
type MsgRef struct{ Channel, TS string }

// WithFile lists the cached messages (timelines and threads, each once)
// that carry file id.
func (s *Store) WithFile(id string) []MsgRef {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []MsgRef
	seen := map[MsgRef]bool{}
	add := func(ch string, m *slack.Message) {
		for i := range m.Files {
			if m.Files[i].ID == id {
				if r := (MsgRef{ch, m.TS}); !seen[r] {
					seen[r] = true
					out = append(out, r)
				}
				return
			}
		}
	}
	for ch, t := range s.timelines {
		for i := range t.Msgs {
			add(ch, &t.Msgs[i])
		}
	}
	for k, th := range s.threads {
		ch, _, _ := strings.Cut(k, "/")
		for i := range th {
			add(ch, &th[i])
		}
	}
	return out
}

// ClientMsgIDs lists the client_msg_ids of ch's cached messages (timeline
// and threads).
func (s *Store) ClientMsgIDs(ch string) map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if id := t.Msgs[i].ClientMsgID; id != "" {
				out[id] = true
			}
		}
	}
	for k, th := range s.threads {
		if strings.HasPrefix(k, ch+"/") {
			for i := range th {
				if id := th[i].ClientMsgID; id != "" {
					out[id] = true
				}
			}
		}
	}
	return out
}

// ---- threads ----

func (s *Store) Thread(ch, ts string) ([]slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, ok := s.threads[ch+"/"+ts]
	return append([]slack.Message(nil), th...), ok
}

func (s *Store) SetThread(ch, ts string, msgs []slack.Message) {
	SortAsc(msgs)
	s.mu.Lock()
	s.threads[ch+"/"+ts] = msgs
	s.mu.Unlock()
}

// AddReply appends a reply to a cached thread (if open) and bumps the parent.
func (s *Store) AddReply(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ch + "/" + m.ThreadTS
	if th, ok := s.threads[key]; ok {
		replaced := false
		for i := range th {
			if th[i].TS == m.TS {
				mm := m
				s.keepPinLocked(ch, &mm, &th[i])
				th[i] = mm
				replaced = true
			}
		}
		if !replaced {
			s.keepPinLocked(ch, &m, nil)
			th = append(th, m)
			SortAsc(th)
		}
		s.threads[key] = th
	}
}

// ---- counts ----

func (s *Store) SetCounts(c *slack.Counts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Counts = map[string]*slack.CountEntry{}
	for _, l := range [][]slack.CountEntry{c.Channels, c.IMs, c.MPIMs} {
		for i := range l {
			e := l[i]
			s.Counts[e.ID] = &e
		}
	}
	s.ThreadsUnread = c.Threads.HasUnreads
	s.ThreadMentions = c.Threads.MentionCount
	s.ThreadsUnreadByChannel = c.Threads.UnreadCountByChannel
}

// Count returns a copy of a conversation's unread state.
func (s *Store) Count(ch string) (slack.CountEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.Counts[ch]; ok {
		return *e, true
	}
	return slack.CountEntry{ID: ch}, false
}

// MarkRead moves a conversation's read cursor to ts. Marking at or after
// the latest message clears it; marking earlier (mark unread) leaves it
// unread. Mentions before the new cursor are unknown locally, so they are
// cleared on read and kept on mark-unread until counts are refreshed.
func (s *Store) MarkRead(ch, ts string) slack.CountEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Counts[ch]
	if !ok {
		e = &slack.CountEntry{ID: ch}
		s.Counts[ch] = e
	}
	e.LastRead = ts
	if e.Latest == "" || !TSLess(ts, e.Latest) {
		e.HasUnreads = false
		e.MentionCount = 0
	} else {
		e.HasUnreads = true
	}
	return *e
}

// NoteMessage updates unread state for a new top-level message (or
// broadcast) in ch. DMs count every message from others as a mention, the
// way Slack badges them.
func (s *Store) NoteMessage(ch string, m *slack.Message, mentionsSelf bool) slack.CountEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Counts[ch]
	if !ok {
		e = &slack.CountEntry{ID: ch}
		s.Counts[ch] = e
	}
	if e.Latest == "" || TSLess(e.Latest, m.TS) {
		e.Latest = m.TS
	}
	self := s.Self.ID
	if m.User == self && self != "" {
		// Our own message (from any client) reads the conversation.
		e.LastRead, e.HasUnreads, e.MentionCount = m.TS, false, 0
		return *e
	}
	if e.LastRead != "" && !TSLess(e.LastRead, m.TS) {
		return *e
	}
	e.HasUnreads = true
	c := s.Convs[ch]
	isDM := c != nil && (c.IsIM || c.IsMPIM) || strings.HasPrefix(ch, "D")
	if isDM && c != nil && c.IsMPIM && s.Muted[ch] {
		isDM = false // a muted group DM badges on mentions only (Slack's unreadHighlightCnt)
	}
	if mentionsSelf || isDM {
		e.MentionCount++
	}
	return *e
}

// ---- disk cache (metadata only) ----

type diskCache struct {
	Users []slack.User      `json:"users"`
	Emoji map[string]string `json:"emoji,omitempty"` // caches written before 2026-11-07
	// EmojiPacked is the custom emoji map as "name\nvalue\n" pairs. Loaded
	// as one string whose substrings become the map's keys and values: one
	// allocation instead of 130k, and ~10x faster to parse than an object.
	EmojiPacked string             `json:"emoji_packed,omitempty"`
	EmojiAt     int64              `json:"emoji_at,omitempty"` // unix time of the emoji.list it came from
	Usergroups  []slack.Usergroup  `json:"usergroups,omitempty"`
	Prefs       *Prefs             `json:"prefs,omitempty"`
	Names       map[string]string  `json:"channel_names,omitempty"` // channels outside the sidebar
	Bots        map[string]BotInfo `json:"bots,omitempty"`
	// UserSeen is when each saved user was last fetched (unix seconds):
	// the next save keeps the MaxSavedUsers most recent.
	UserSeen map[string]int64 `json:"user_seen,omitempty"`
}

// MaxSavedUsers bounds the users meta.json keeps: the most recently fetched
// (and the viewer). Slack's client keeps no directory between starts (its
// members cache holds 700, trimmed to 500, and fetches anyone else when a
// message, mention or profile needs them, as EnsureUsers and the name
// resolver do here); a start restoring every user a long-used Clack ever
// saw held tens of thousands in memory, 48 MB at the Hack Club account's
// scale.
const MaxSavedUsers = 1000

// sameGroups reports whether gs (minus deleted groups) equals s.Groups.
func (s *Store) sameGroups(gs []slack.Usergroup) bool {
	i := 0
	for _, g := range gs {
		if g.DateDelete != 0 {
			continue
		}
		if i >= len(s.Groups) || s.Groups[i] != g {
			return false
		}
		i++
	}
	return i == len(s.Groups)
}

// HasCachedPrefs reports whether prefs came from the disk cache.
func (s *Store) HasCachedPrefs() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefsCached
}

// SetUsergroups installs usergroups (for <!subteam^> names and autocomplete).
// SetMyUsergroups replaces the viewer's user groups
// (bulkSetUserGroupMembership).
func (s *Store) SetMyUsergroups(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MyGroups = make(map[string]bool, len(ids))
	for _, id := range ids {
		s.MyGroups[id] = true
	}
	s.gen.Add(1)
}

// SetMyUsergroup records the viewer joining or leaving group id
// (subteam_self_added / subteam_self_removed).
func (s *Store) SetMyUsergroup(id string, in bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.MyGroups == nil {
		s.MyGroups = map[string]bool{}
	}
	if s.MyGroups[id] == in {
		return
	}
	if in {
		s.MyGroups[id] = true
	} else {
		delete(s.MyGroups, id)
	}
	s.gen.Add(1)
}

// InUsergroup is richtext.UsergroupMember: whether the viewer is in group
// id (isUserInGroup, client-boot 3HzN nc).
func (s *Store) InUsergroup(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MyGroups[id]
}

func (s *Store) SetUsergroups(gs []slack.Usergroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sameGroups(gs) {
		return // the usual refresh: nothing to install or save
	}
	s.Groups = s.Groups[:0]
	for _, g := range gs {
		if g.DateDelete != 0 {
			continue
		}
		s.Groups = append(s.Groups, g)
		s.Usergroups[g.ID] = g.Handle
	}
	s.gen.Add(1)
}

func (s *Store) LoadCache() {
	if s.cacheDir == "" || s.dropped.Load() {
		return
	}
	f, err := os.Open(filepath.Join(s.cacheDir, "meta.json"))
	if err != nil {
		return
	}
	var d diskCache
	var users []*slack.User
	err = readDiskCache(bufio.NewReaderSize(f, 64<<10), &d, func(u *slack.User) { users = append(users, u) })
	f.Close()
	if err != nil {
		return
	}
	s.putUserPtrs(users, d.UserSeen)
	if d.EmojiPacked != "" {
		s.CustomEmoji.Set(unpackEmoji(d.EmojiPacked))
	} else if len(d.Emoji) > 0 {
		s.CustomEmoji.Set(d.Emoji)
	}
	if d.EmojiAt > 0 {
		s.emojiAtMu.Lock()
		s.emojiAt = time.Unix(d.EmojiAt, 0)
		s.emojiAtMu.Unlock()
	}
	if len(d.Usergroups) > 0 {
		s.SetUsergroups(d.Usergroups)
	}
	s.mu.Lock()
	if len(d.Names) > 0 {
		if s.names == nil {
			s.names = map[string]string{}
		}
		for k, v := range d.Names {
			s.names[k] = v
		}
	}
	if len(d.Bots) > 0 {
		s.bots = d.Bots
	}
	s.mu.Unlock()
	if d.Prefs != nil {
		muted := map[string]bool{}
		for _, id := range d.Prefs.Muted {
			muted[id] = true
		}
		s.SetPrefs(*d.Prefs, muted)
		s.mu.Lock()
		s.prefsCached = true
		s.mu.Unlock()
	}
	// what was just read is what is on disk
	s.saveMu.Lock()
	s.saved, s.savedGen, s.savedEmo = true, s.gen.Load(), s.CustomEmoji.Version()
	s.saveMu.Unlock()
}

// SetEmojiList installs a fresh emoji.list result, compacted the way the
// disk cache stores it (see diskCache.EmojiPacked), and stamps its age.
func (s *Store) SetEmojiList(m map[string]string) {
	s.CustomEmoji.Set(unpackEmoji(packEmoji(m)))
	s.emojiAtMu.Lock()
	s.emojiAt = time.Now()
	s.emojiAtMu.Unlock()
}

// SetEmojiPacked installs a fresh emoji.list result already packed as
// "name\nvalue\n" pairs (slack.PackEmojiJSON): the map's keys and values
// share the one string.
func (s *Store) SetEmojiPacked(p string) {
	s.CustomEmoji.Set(unpackEmoji(p))
	s.emojiAtMu.Lock()
	s.emojiAt = time.Now()
	s.emojiAtMu.Unlock()
}

// EmojiAge is how old the custom emoji map is (a large value when unknown).
func (s *Store) EmojiAge() time.Duration {
	s.emojiAtMu.Lock()
	defer s.emojiAtMu.Unlock()
	if s.emojiAt.IsZero() {
		return 1 << 62
	}
	return time.Since(s.emojiAt)
}

func packEmoji(m map[string]string) string {
	n := 0
	for k, v := range m {
		n += len(k) + len(v) + 2
	}
	var b strings.Builder
	b.Grow(n)
	for k, v := range m {
		if strings.ContainsRune(k, '\n') || strings.ContainsRune(v, '\n') {
			continue
		}
		b.WriteString(k)
		b.WriteByte('\n')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}

// unpackEmoji builds the map from packed pairs; keys and values share p's
// memory.
func unpackEmoji(p string) map[string]string {
	m := make(map[string]string, strings.Count(p, "\n")/2)
	for len(p) > 0 {
		i := strings.IndexByte(p, '\n')
		if i < 0 {
			break
		}
		k := p[:i]
		p = p[i+1:]
		j := strings.IndexByte(p, '\n')
		if j < 0 {
			break
		}
		m[k] = p[:j]
		p = p[j+1:]
	}
	return m
}

func (s *Store) SaveCache() error {
	if s.cacheDir == "" {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.dropped.Load() {
		return nil
	}
	gen, emo := s.gen.Load(), s.CustomEmoji.Version()
	if s.saved && gen == s.savedGen && emo == s.savedEmo {
		return nil // unchanged since the last load or save
	}
	s.emojiAtMu.Lock()
	emojiAt := s.emojiAt
	s.emojiAtMu.Unlock()
	s.mu.RLock()
	prefs := s.Prefs
	d := diskCache{EmojiPacked: packEmoji(s.CustomEmoji.Snapshot()), Usergroups: append([]slack.Usergroup(nil), s.Groups...), Prefs: &prefs,
		Names: map[string]string{}, Bots: map[string]BotInfo{}}
	if !emojiAt.IsZero() {
		d.EmojiAt = emojiAt.Unix()
	}
	for k, v := range s.names {
		d.Names[k] = v
	}
	for k, v := range s.bots {
		d.Bots[k] = v
	}
	// the stored users are never changed in place (PutUsers replaces
	// them), so they're written after the lock is let go: the viewer and
	// the MaxSavedUsers most recently fetched
	users := make([]*slack.User, 0, len(s.Users))
	for _, u := range s.Users {
		users = append(users, u)
	}
	if len(users) > MaxSavedUsers {
		self := s.Self.ID
		sort.Slice(users, func(i, j int) bool {
			if (users[i].ID == self) != (users[j].ID == self) {
				return users[i].ID == self
			}
			if a, b := s.userSeen[users[i].ID], s.userSeen[users[j].ID]; a != b {
				return a > b
			}
			return users[i].ID < users[j].ID
		})
		users = users[:MaxSavedUsers]
	}
	d.UserSeen = make(map[string]int64, len(users))
	for _, u := range users {
		if t := s.userSeen[u.ID]; t != 0 {
			d.UserSeen[u.ID] = t
		}
	}
	s.mu.RUnlock()
	if err := writeAtomicStream(s.cacheDir, "meta.json", func(w io.Writer) error { return writeDiskCache(w, &d, users) }); err != nil {
		return err
	}
	s.saved, s.savedGen, s.savedEmo = true, gen, emo
	return nil
}

func SortAsc(m []slack.Message) {
	sort.SliceStable(m, func(i, j int) bool { return TSLess(m[i].TS, m[j].TS) })
}

// TSLess compares Slack timestamps ("1234567890.123456").
func TSLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func dedupe(m []slack.Message) []slack.Message {
	SortAsc(m)
	out := m[:0]
	for i := range m {
		if i > 0 && m[i].TS == out[len(out)-1].TS {
			out[len(out)-1] = m[i]
			continue
		}
		out = append(out, m[i])
	}
	return out
}
