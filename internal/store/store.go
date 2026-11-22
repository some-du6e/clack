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
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slackasset"
)

// Timeline is a contiguous window of a conversation's newest messages.
type Timeline struct {
	Msgs    []slack.Message // ascending by ts
	HasMore bool            // older messages exist beyond Msgs[0]
	Loaded  bool
	// EndStale: the window may stop short of the conversation's newest
	// message. Slack's resetReachedEnd at every slow connect
	// (reached_end.go): nothing the socket missed meanwhile is in it, so
	// it is read forward from End before it is the newest window again,
	// and what the socket brings past End waits in Ahead until then.
	EndStale bool
	End      string          // EndStale: the newest message from Slack the window held then
	Ahead    []slack.Message // EndStale: the socket's messages past End, ascending
	EndGen   uint64          // the reset (ResetReachedEnd) the window's end last went stale at
	// Epoch is which window this is: a newest page that doesn't meet the
	// window it replaces starts a new one (SetLatestReadAt), and a list
	// holding an older epoch's messages takes this one whole instead of
	// keeping its own in front (Slack keeps such a page a slice of its own)
	Epoch uint64
}

type Store struct {
	// released: Release ran; the package's maps keyed by *Store keep it
	// no more (a late reader gets state kept nowhere)
	released    atomic.Bool
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
	windows     map[string][]*Window       // a conversation's stretches apart from its timeline (windows.go)
	windowHolds map[string]map[string]int  // the windows lists show, by a ts each holds (HoldWindow)
	threads     map[string][]slack.Message // key channel/ts
	threadHist  map[string]*threadHist     // key channel/ts: the threads' history slices (thread_slices.go)
	pins        map[string]pinRec          // key channel/ts: pin events (pins.go)
	pinQ        []string                   // pins' keys, oldest first
	pinSeq      uint64                     // pin events seen
	endGen      uint64                     // ResetReachedEnd calls so far (reached_end.go)
	epochs      uint64                     // windows started so far (Timeline.Epoch)
	live        map[string]liveRec         // key channel/ts: realtime message changes (live.go)
	liveQ       []string                   // live's keys, oldest first
	liveSeq     uint64                     // realtime message changes seen
	fetched     map[string]*slack.Message  // key channel/ts: messages fetched by id (fetched.go)
	fetchedBy   map[string]map[string]bool // fetched's ts by channel (fetched.go)
	fetchedUse  map[string]fetchedUse      // fetched's last use and size (fetched.go)
	fetchedQ    []fetchedTick              // fetched's uses, oldest first
	fetchedN    uint64                     // fetched's uses so far
	fetchedSize int                        // fetched's messages' size (messageSize)
	reacts      map[string][]reactOp       // key channel/ts: realtime reactions (reacts.go)
	reactQ      []string                   // reacts' keys, oldest first
	readRevs    []ReadRevision             // revisions reads brought, for core (read_revisions.go)
	fetchedNew  []MsgRef                   // messages a fetch by id brought that the store held no copy of, for core (fetched.go)
	subs        map[string]subRec          // key channel/thread_ts: thread subscriptions (thread_subs.go)
	subQ        []string                   // subs' keys, oldest first
	subSeq      uint64                     // subscription events seen
	CustomEmoji emoji.Custom
	EmojiStyle  string
	cacheDir    string

	// Groups holds full usergroup records (Usergroups maps id -> handle).
	Groups []slack.Usergroup
	// MyGroups are the user groups the viewer is in (Slack's
	// userGroupMembership: usergroups.membership, subteam_self_added and
	// subteam_self_removed; memory only)
	MyGroups map[string]bool
	// groupUsers is each user group's member ids as last fetched (the
	// users field Slack's bulkAddMembersToGroup / appendMembersToGroup,
	// client-boot CjnZ, keep on a userGroups record; memory only).
	groupUsers map[string][]string
	// groupChannels is each user group's default channels (Slack's
	// getUserGroupDefaultChannels, client-boot PWhS UN: prefs.channels
	// then prefs.groups) as usergroups.search or subteam_updated last
	// gave them (memory only).
	groupChannels map[string][]string
	// ugGen counts changes to a user group's record, members or channels
	// (the User Group pane watches it, as Slack's pane re-renders on its
	// userGroups record).
	ugGen atomic.Uint64
	// groupsLoaded: SetUsergroups ran (the disk cache's list or
	// usergroups.list), so a group id Usergroups lacks doesn't exist
	groupsLoaded bool
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
	// countsLive: a client.counts answer was installed (SetCounts*), so the
	// boot data's thread state no longer applies (bootdata.go)
	countsLive bool
	// jr is the write journal (journal.go)
	jr journal
	// names of conversations we are not a member of (from search/browse)
	names       map[string]string
	prefsCached bool
	experiments map[string]string                 // the last experiments.getByUser groups (diskCache.Experiments)
	ext         map[string]*slack.Conversation    // non-sidebar conversations
	access      map[string]richtext.ChannelAccess // edge channels/info answers for mentioned channels
	bots        map[string]BotInfo
	// botsGone are bot ids nothing returned (PutNonexistentBots)
	botsGone map[string]bool

	// userSeen is when each user was last fetched (unix seconds; from the
	// disk cache for users it restored): what SaveCache keeps.
	userSeen map[string]int64
	// members is Slack's members cache (members.go): when each member
	// was last found, the ids users/info didn't return, and the pass that
	// trims them to MembersTarget.
	members       memberCache
	membersOnline func() bool

	// gen counts changes to what the disk cache holds; SaveCache writes
	// only when it (or the emoji map's version) moved since the last load
	// or save.
	gen    atomic.Uint64
	saveMu sync.Mutex
	fileMu sync.Mutex // WriteFile's (the warm boot's small files), apart from meta.json's long saves
	// warmGone: the warm boot's files were deleted for good (a session
	// that ended, sign-out): WriteFile writes neither again (fileMu)
	warmGone  bool
	dropped   atomic.Bool // DropDisk ran (signed out): never write again
	saved     bool
	savedGen  uint64
	savedEmo  uint64
	emojiAt   time.Time // when the cached emoji map was fetched (emoji.list)
	emojiAtMu sync.Mutex

	// cachePending: LoadCacheBase installed meta.json's first part and its
	// rest hasn't run: SaveCache writes nothing meanwhile
	cachePending atomic.Bool
	// emojiPartial: the emoji map is the boot data's few only
	emojiPartial atomic.Bool

	missMu     sync.Mutex
	missSeen   map[string]bool
	missing    []string // "c"+id | "b"+id
	missNotify chan struct{}
	// missUsers are the users lookups missed, asked for by core's member
	// fetcher (Slack's UnknownFetchManager, core/members.go) on its own
	// schedule
	missUsers  []string
	userNotify chan struct{}
	// missBotApps are app ids the lookups that missed a bot knew (a
	// message's app_id: getBotById's app_id param), for bots.info's app_ids
	missBotApps map[string]string
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

// TabSlug resolves a channel-tab slug for richtext (core sets it to its
// ChannelTabSlug); nil leaves every slug the plain channel mention.
var TabSlug func(s *Store, channelID, tabID string) (richtext.TabSlug, bool)

// ChannelTabSlug is richtext.ChannelTabResolver.
func (s *Store) ChannelTabSlug(channelID, tabID string) (richtext.TabSlug, bool) {
	if TabSlug == nil {
		return richtext.TabSlug{}, false
	}
	return TabSlug(s, channelID, tabID)
}

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
// unknown, which asks for the member).
func (s *Store) UserAvatar(id string) string { return userAvatar(s.User, id) }

func userAvatar(look userLookup, id string) string {
	if u, ok := look(id); ok && u != nil {
		if u.ID == SlackSystemUserID { // getMemberProfileImage's n8qp (AvatarURL)
			return slackasset.SlackLogoMark
		}
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
		s.storedLocked(u.ID)
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
		s.storedLocked(u.ID)
	}
	s.mu.Unlock()
	if len(us) > 0 {
		s.gen.Add(1)
	}
}

// storedLocked is an upsert's part of the members cache (s.mu held for
// writing): the member has a lastAccess cell and isn't non-existent.
func (s *Store) storedLocked(id string) {
	s.cellLocked(id)
	delete(s.members.nonexistent, id)
}

// User is Slack's getMemberById: the member, recording the hit for the
// members cache (members.go). A member the store doesn't hold is asked
// for (users/info in the background; "names" follows) and reads as
// missing until then; one users/info said doesn't exist reads as missing
// without being asked for again.
func (s *Store) User(id string) (*slack.User, bool) {
	now := time.Now().UnixMilli()
	s.mu.RLock()
	u, known := s.lookupLocked(id, now)
	s.mu.RUnlock()
	s.afterLookup(id, known)
	return u, u != nil
}

// RawUser is getRawMemberById: the member if the store holds it, without
// recording a hit or asking for a missing one (logic over the cache, not
// something shown).
func (s *Store) RawUser(id string) (*slack.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.Users[id]
	return u, ok
}

// MissingUsers filters ids to those not yet known (ensureMembersArePresent:
// a non-existent member counts as known).
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
		if _, ok := s.Users[id]; !ok && !s.members.nonexistent[id] {
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

// UsersNamed finds the members called each of names (folded with
// FoldName): a live, human member whose display name, real name or
// username is that name. One pass over the users for all of them.
func (s *Store) UsersNamed(names []string) map[string][]string {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		if f := FoldName(n); f != "" {
			want[f] = true
		}
	}
	out := map[string][]string{}
	if len(want) == 0 {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, u := range s.Users {
		if u == nil || u.Deleted || u.IsBot {
			continue
		}
		seen := ""
		for _, n := range [...]string{u.Profile.DisplayName, u.Profile.RealName, u.RealName, u.Name} {
			if f := FoldName(n); f != "" && f != seen && want[f] {
				out[f] = append(out[f], id)
				seen = f
			}
		}
	}
	for f, ids := range out { // one id per member
		sort.Strings(ids)
		out[f] = slices.Compact(ids)
	}
	return out
}

// UserCount is how many users the store holds.
func (s *Store) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Users)
}

// FoldName is a name as UsersNamed compares it: trimmed, without a
// leading @, lower case.
func FoldName(n string) string {
	n = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n), "@"))
	if n == "\u200b" {
		return ""
	}
	return strings.ToLower(n)
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

// SlackSystemUserID is the Slack system user (Slack's isSlackSystemUser,
// client-boot iXFk: id "USLACK"), the partner of the DM Slack's own
// notices and apps acting for Slack post in.
const SlackSystemUserID = "USLACK"

// AvatarURL is u's picture for a slot of size px: the profile image that
// covers it, else the ca.slack-edge.com picture of avatar_hash under
// avatarTeam, else Slack's default. viewerTeam is the viewer's team.
//
// The Slack system user (USLACK, the DM apps post Slack's own notices in)
// is Slack's logo at every size: getMemberProfileImage (client-boot 6Knt)
// returns its bundled slack_logo_mark (n8qp) for memberId "USLACK" before
// it reads anything else. Its profile has an avatar_hash, whose
// ca.slack-edge.com picture is the generic blue person, and image_* SVGs
// of the same logo.
func AvatarURL(u *slack.User, size int, viewerTeam string) string {
	if u.ID == SlackSystemUserID {
		return slackasset.SlackLogoMark
	}
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

// userLookup is how a resolver method reads a member: Store.User (a
// view's read: the hit counts) or peekUser (Peek's: it doesn't).
type userLookup func(id string) (*slack.User, bool)

func (s *Store) UserName(id string) string { return userName(s.User, id) }

func userName(look userLookup, id string) string {
	u, _ := look(id) // a miss is asked for
	return DisplayName(u)
}

// MemberName is richtext.MemberNamer: Slack's getPrefCompliantMemberName
// (client-boot vUCu sE) for id under the viewer's display_real_names
// pref, "Name hidden" for someone in their hidden_users (0OuZ
// isUserHidden). ok is false for a member not loaded yet, which is noted
// to be fetched.
func (s *Store) MemberName(id string) (string, bool) { return s.memberName(s.User, id) }

// MemberNameHeld is MemberName for a member the store holds now, a miss
// asked for by nobody (getRawMemberById's read): for text built once
// what it names was waited for (a notification's body).
func (s *Store) MemberNameHeld(id string) (string, bool) { return s.memberName(s.RawUser, id) }

// PrefNameHeld is the held member id's name under the viewer's
// display_real_names (PrefName), nothing asked and no hidden check (the
// caller's: core.UserHidden reads the locked hide_person_opt_out too).
func (s *Store) PrefNameHeld(id string) (string, bool) {
	u, ok := s.RawUser(id)
	if !ok || u == nil {
		return "", false
	}
	s.mu.RLock()
	real := s.Prefs.DisplayRealNames
	s.mu.RUnlock()
	return PrefName(u, real), true
}

func (s *Store) memberName(look userLookup, id string) (string, bool) {
	u, ok := look(id) // a miss is asked for
	if !ok || u == nil {
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
func (s *Store) MemberRealName(id string) string { return memberRealName(s.User, id) }

func memberRealName(look userLookup, id string) string {
	u, ok := look(id) // a miss is asked for
	if !ok || u == nil {
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

// MemberDeleted is richtext.MemberStater: whether the member is
// deleted; known is false for a member not loaded yet, which is noted to
// be fetched.
func (s *Store) MemberDeleted(id string) (deleted, known bool) {
	now := time.Now().UnixMilli()
	s.mu.RLock()
	u, known := s.lookupLocked(id, now)
	s.mu.RUnlock()
	s.afterLookup(id, known) // a miss is asked for
	// a member users.info didn't find is known (Slack's non-existent
	// member), and not deleted
	return u != nil && u.Deleted, known
}

// MemberAvatar is richtext.MemberNamer: the member's own picture for a
// 36px avatar (ConnectedBaseAvatar asks for twice that), the image
// message rows draw; "" for a member not loaded yet.
func (s *Store) MemberAvatar(id string) string { return s.memberAvatar(s.User, id) }

func (s *Store) memberAvatar(look userLookup, id string) string {
	u, ok := look(id)
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

func (s *Store) ChannelName(id string) string { return s.channelName(id, true) }

// channelName is ChannelName; a DM's name is its partner's, read as a hit
// when touch (a view's read) and not otherwise (Peek).
func (s *Store) channelName(id string, touch bool) string {
	s.mu.RLock()
	c, ok := s.Convs[id]
	if !ok {
		c, ok = s.ext[id]
	}
	if ok {
		if c.IsIM {
			partner, name := c.User, c.Name
			var u *slack.User
			var known bool
			if touch {
				u, known = s.lookupLocked(partner, time.Now().UnixMilli())
			} else {
				u, known = s.peekLocked(partner)
			}
			s.mu.RUnlock()
			s.afterLookup(partner, known)
			if u != nil {
				return DisplayName(u)
			}
			return name
		}
		defer s.mu.RUnlock()
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

// noteMissing records an id that rendering could not name. Channels and
// bots are recorded once per session (see TakeMissing); a user is
// recorded once while it is asked for, and again once it was evicted
// from the members cache (members.go) or its lookup failed
// (ForgetMissing).
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
	s.missInitLocked()
	if !s.missSeen[key] {
		s.missSeen[key] = true
		ch := s.missNotify
		if kind == 'u' {
			s.missUsers = append(s.missUsers, id)
			ch = s.userNotify
		} else {
			s.missing = append(s.missing, key)
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.missMu.Unlock()
}

func (s *Store) missInitLocked() {
	if s.missSeen == nil {
		s.missSeen = map[string]bool{}
	}
	if s.missNotify == nil {
		s.missNotify = make(chan struct{}, 1)
	}
	if s.userNotify == nil {
		s.userNotify = make(chan struct{}, 1)
	}
}

// NoteMissingBot records a bot id whose name/icon is unknown.
func (s *Store) NoteMissingBot(id string) { s.NoteMissingBotApp(id, "") }

// NoteMissingBotApp is NoteMissingBot with the app the bot belongs to,
// when the message that drew it says (getBotByMsg passes its app_id):
// bots.info gets it as app_ids.
func (s *Store) NoteMissingBotApp(id, appID string) {
	if !strings.HasPrefix(id, "B") {
		return
	}
	if appID != "" {
		s.missMu.Lock()
		if s.missBotApps == nil {
			s.missBotApps = map[string]string{}
		}
		s.missBotApps[id] = appID
		s.missMu.Unlock()
	}
	s.noteMissing('b', id)
}

// MissingBotApp is the app id a lookup that missed bot id knew ("" when
// none did).
func (s *Store) MissingBotApp(id string) string {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	return s.missBotApps[id]
}

// MissingNotify fires (coalesced) when new unresolved channel, bot or
// canvas ids were recorded.
func (s *Store) MissingNotify() <-chan struct{} {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	s.missInitLocked()
	return s.missNotify
}

// MissingUsersNotify fires (coalesced) when lookups missed new users.
func (s *Store) MissingUsersNotify() <-chan struct{} {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	s.missInitLocked()
	return s.userNotify
}

// TakeMissing returns and clears the unresolved channel, user and bot ids.
func (s *Store) TakeMissing() (chans, users, bots []string) {
	chans, bots = s.TakeMissingNames()
	return chans, s.TakeMissingUsers(), bots
}

// TakeMissingNames returns and clears the unresolved channel and bot ids.
func (s *Store) TakeMissingNames() (chans, bots []string) {
	s.missMu.Lock()
	keys := s.missing
	s.missing = nil
	s.missMu.Unlock()
	for _, k := range keys {
		switch k[0] {
		case 'c':
			chans = append(chans, k[1:])
		case 'b':
			bots = append(bots, k[1:])
		}
	}
	return
}

// TakeMissingUsers returns and clears the users lookups missed.
func (s *Store) TakeMissingUsers() []string {
	s.missMu.Lock()
	ids := s.missUsers
	s.missUsers = nil
	s.missMu.Unlock()
	return ids
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
	s.putExtConvLocked(c)
	s.mu.Unlock()
}

// putExtConvLocked is PutExtConv's body (s.mu held).
func (s *Store) putExtConvLocked(c slack.Conversation) {
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
	delete(s.botsGone, id)
	s.mu.Unlock()
	s.gen.Add(1)
}

// PutNonexistentBots records bots neither the edge cache nor bots.info
// returned: the bots UnknownFetchManager's upsertNonExistent (client-boot
// 0mwJ marks every id a fetch didn't return), whose stub getBotById
// (Emf2 AF) answers from then on. Nothing asks for them again this
// session (noteMissing records a bot once), and a message only such a bot
// would name draws BaseMissingMember's non-existent bar.
func (s *Store) PutNonexistentBots(ids []string) {
	if len(ids) == 0 {
		return
	}
	s.mu.Lock()
	for _, id := range ids {
		if _, ok := s.bots[id]; ok || id == "" {
			continue
		}
		if s.botsGone == nil {
			s.botsGone = map[string]bool{}
		}
		s.botsGone[id] = true
	}
	s.mu.Unlock()
}

// BotNonexistent reports whether nothing returned bot id
// (PutNonexistentBots): Slack's isNonExistent bot stub.
func (s *Store) BotNonexistent(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.botsGone[id]
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

// UsergroupsLoaded reports whether the workspace's user group list was
// read (the disk cache's or usergroups.list): a group id UsergroupName
// can't name after that doesn't exist.
func (s *Store) UsergroupsLoaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.groupsLoaded
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

// TeamIDs is the viewer's workspace and enterprise ids (richtext's
// TeamResolver: Slack's selectTeamId and selectEnterpriseId, which a
// condition block's "team" conditions compare against).
func (s *Store) TeamIDs() (team, enterprise string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if eu := s.Self.EnterpriseUser; eu != nil {
		enterprise = eu.EnterpriseID
	}
	return s.TeamID, enterprise
}

// ---- conversations ----

func (s *Store) PutConv(c slack.Conversation) {
	s.mu.Lock()
	s.putConvLocked(c)
	s.mu.Unlock()
}

// putConvLocked is PutConv's body (s.mu held).
func (s *Store) putConvLocked(c slack.Conversation) {
	if old, ok := s.Convs[c.ID]; ok {
		if c.NumMembers == 0 {
			c.NumMembers = old.NumMembers
		}
		if c.LastRead == "" {
			c.LastRead = old.LastRead
		}
		// a payload without is_member (users.conversations, a write's
		// channel echo) doesn't make a member a previewer: leaving takes
		// the conversation out of Convs first (core dropMembership)
		if old.IsMember && !c.IsMember {
			c.IsMember = true
		}
	}
	s.Convs[c.ID] = &c
}

func (s *Store) Conv(id string) (*slack.Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.Convs[id]
	return c, ok
}

// DMUser is an IM's other member (richtext's DMResolver: a DM call's
// card names them, getOtherMemberNameInDmCall).
func (s *Store) DMUser(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.Convs[id]; ok && c.IsIM && c.User != "" {
		return c.User, true
	}
	return "", false
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
	cp.Ahead = append([]slack.Message(nil), t.Ahead...)
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

// SetLatest replaces the newest window (from a fresh history fetch); a
// window it now meets joins it (windows.go).
func (s *Store) SetLatest(ch string, msgs []slack.Message, hasMore bool) {
	SortAsc(msgs)
	s.mu.Lock()
	t := &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true, EndGen: s.endGen}
	if old := s.timelines[ch]; old != nil {
		t.Epoch = old.Epoch // the same window, written whole (a test's)
	} else {
		t.Epoch = s.newEpochLocked()
	}
	s.timelines[ch] = t
	s.absorbWindowsLocked(ch)
	s.mu.Unlock()
}

// PrependOlder adds an older page to the front (one no request read:
// a test's window).
func (s *Store) PrependOlder(ch string, msgs []slack.Message, hasMore bool) {
	s.PrependOlderSince(ch, msgs, hasMore, ^uint64(0))
}

// PrependOlderSince is PrependOlder for an older page whose request began
// at LiveSeq since (scrolling up), a read like the newest page's: a
// message the socket changed meanwhile keeps the socket's copy, one it
// deleted stays out, the reactions it brought stay, and a revision the
// store held another of reaches every copy and core (readWindowLocked).
// It returns what it kept of the page.
func (s *Store) PrependOlderSince(ch string, msgs []slack.Message, hasMore bool, since uint64) []slack.Message {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs = s.liveKeepLocked(ch, msgs, since)
	s.readWindowLocked(ch, msgs, since)
	t := s.timelines[ch]
	if t == nil {
		s.timelines[ch] = &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true, EndGen: s.endGen, Epoch: s.newEpochLocked()}
		return msgs
	}
	merged := append(append([]slack.Message(nil), msgs...), t.Msgs...)
	t.Msgs = dedupe(merged)
	t.HasMore = hasMore
	// bound memory per channel
	if len(t.Msgs) > 5000 {
		t.Msgs = t.Msgs[len(t.Msgs)-5000:]
	}
	s.absorbWindowsLocked(ch) // the timeline grew back to a window: they're one (Slack's slices merge as they meet)
	return msgs
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

// UpsertInRange is Upsert for a message the socket changed that no window
// or cached thread held (a message_changed of one Activity, Threads or
// Saved fetched by id: a poll 80 messages back, a root after one of its
// replies is deleted): it goes into a window holding older history
// (HasMore) only at or after the window's oldest message from Slack, as
// the message lists take an update (ui/messages hub.go inRange). Taken in
// above it, the window's next reader drew it right above the newest page
// and load-older read from it, skipping the history between; Slack's invH
// keeps such a ts as a slice of its own, which a page covering it brings.
// An ephemeral message, a local copy, is no boundary.
func (s *Store) UpsertInRange(ch string, m slack.Message) {
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
	if s.aheadLocked(ch, t, m) { // past a window whose end went stale (reached_end.go)
		return
	}
	if t.HasMore {
		oldest := ""
		for i := range t.Msgs {
			if !t.Msgs[i].IsEphemeral {
				oldest = t.Msgs[i].TS
				break
			}
		}
		if oldest == "" || TSLess(m.TS, oldest) {
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
		for i := range t.Ahead { // past a stale end (reached_end.go)
			if t.Ahead[i].TS == ts {
				f(&t.Ahead[i])
				if !found {
					out, found = t.Ahead[i], true
				}
			}
		}
	}
	if m := s.windowMsgLocked(ch, func(m *slack.Message) bool { return m.TS == ts }); m != nil {
		f(m)
		if !found {
			out, found = *m, true
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
	// a message fetched by id (fetched.go): the socket's changes reach
	// it as Slack's reach every message of its store
	if m := s.fetched[ch+"/"+ts]; m != nil {
		f(m)
		s.measureFetchedLocked(ch + "/" + ts)
		if !found {
			out, found = *m, true
		}
	}
	return out, found
}

func (s *Store) Remove(ch, ts string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(ch, ts)
}

func (s *Store) removeLocked(ch, ts string) {
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == ts {
				t.Msgs = append(t.Msgs[:i], t.Msgs[i+1:]...)
				break
			}
		}
		for i := range t.Ahead { // past a stale end (reached_end.go)
			if t.Ahead[i].TS == ts {
				t.Ahead = append(t.Ahead[:i], t.Ahead[i+1:]...)
				break
			}
		}
	}
	s.removeWindowMsgLocked(ch, ts)
	for k, th := range s.threads {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for i := range th {
			if th[i].TS == ts {
				s.threads[k] = append(th[:i], th[i+1:]...)
				s.removeTimestampLocked(k, ts) // its history's removeTimestamp (thread_slices.go)
				break
			}
		}
	}
	s.dropFetchedLocked(ch + "/" + ts) // fetched.go
}

func (s *Store) Find(ch, ts string) (slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m := s.findRefLocked(ch, ts, ""); m != nil {
		return *m, true
	}
	return slack.Message{}, false
}

// Holds reports whether ch's cached messages (timeline and threads) have
// ts, or with cid != "" a message with that client_msg_id: Find without
// copying the message out.
func (s *Store) Holds(ch, ts, cid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findRefLocked(ch, ts, cid) != nil
}

// findRefLocked is the cached message of ch with ts (or client_msg_id cid),
// indexed in place: ranging by value copied every message it passed
// (slack.Message is large), and InjectedMessages asked once per message of
// a channel.
func (s *Store) findRefLocked(ch, ts, cid string) *slack.Message {
	match := func(m *slack.Message) bool { return m.TS == ts || cid != "" && m.ClientMsgID == cid }
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if match(&t.Msgs[i]) {
				return &t.Msgs[i]
			}
		}
		for i := range t.Ahead { // past a stale end (reached_end.go)
			if match(&t.Ahead[i]) {
				return &t.Ahead[i]
			}
		}
	}
	if m := s.windowMsgLocked(ch, match); m != nil {
		return m
	}
	pre := ch + "/"
	for k, th := range s.threads {
		if strings.HasPrefix(k, pre) {
			for i := range th {
				if match(&th[i]) {
					return &th[i]
				}
			}
		}
	}
	return nil
}

// MessageKeys is every ts and client_msg_id among ch's cached messages
// (timeline, a jump's windows, threads and the messages fetched by id), in
// one pass: what the plugins' messages slice holds of core's (taut's
// injected check). The slice holds the windows' messages and the fetched
// copies, as Slack's one messages store does, and none of them is a
// message a plugin injected: a conversation's list draws its history
// (Slack's channelHistory, the windows' slices with it), which a message
// fetched by id never joins.
func (s *Store) MessageKeys(ch string) (ts, cids map[string]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ts, cids = map[string]bool{}, map[string]bool{}
	add := func(m *slack.Message) {
		ts[m.TS] = true
		if m.ClientMsgID != "" {
			cids[m.ClientMsgID] = true
		}
	}
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			add(&t.Msgs[i])
		}
	}
	for _, w := range s.windows[ch] {
		for i := range w.Msgs {
			add(&w.Msgs[i])
		}
	}
	pre := ch + "/"
	for k, th := range s.threads {
		if strings.HasPrefix(k, pre) {
			for i := range th {
				add(&th[i])
			}
		}
	}
	for _, m := range s.fetchedInLocked(ch) {
		add(m)
	}
	return ts, cids
}

// MsgKey is a message by ts or client_msg_id (Held).
type MsgKey struct{ TS, CID string }

// Held reports, for each key, whether the store caches a message of ch
// with its ts (or client_msg_id) in the timeline, a jump's window, a
// thread or the messages fetched by id (MessageKeys' set: taut's injected
// check): Holds for many
// at once, one pass over the store under one read lock (Holds per message
// scanned the timeline and every cached thread once each).
func (s *Store) Held(ch string, keys []MsgKey) []bool {
	out := make([]bool, len(keys))
	if len(keys) == 0 {
		return out
	}
	byTS := make(map[string][]int, len(keys))
	byCID := map[string][]int{}
	for i, k := range keys {
		byTS[k.TS] = append(byTS[k.TS], i)
		if k.CID != "" {
			byCID[k.CID] = append(byCID[k.CID], i)
		}
	}
	mark := func(m *slack.Message) {
		for _, i := range byTS[m.TS] {
			out[i] = true
		}
		if m.ClientMsgID != "" {
			for _, i := range byCID[m.ClientMsgID] {
				out[i] = true
			}
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			mark(&t.Msgs[i])
		}
	}
	for _, w := range s.windows[ch] { // a jump's windows, which Holds reads too
		for i := range w.Msgs {
			mark(&w.Msgs[i])
		}
	}
	pre := ch + "/"
	for k, th := range s.threads {
		if strings.HasPrefix(k, pre) {
			for i := range th {
				mark(&th[i])
			}
		}
	}
	for _, m := range s.fetchedInLocked(ch) {
		mark(m)
	}
	return out
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
	for k, m := range s.fetched { // messages fetched by id (fetched.go)
		ch, _, _ := strings.Cut(k, "/")
		add(ch, m)
	}
	return out
}

// HeldFile is a complete copy (not a FileStub) of file id that a cached
// message carries: Slack keeps files in one store by id, which merges
// every copy it gets (4OY3 processFile: {...held, ...copy}), so a
// message's stub reads the whole file there; Clack keeps the copies in
// the messages (core keepFiles). It reads each held message (timelines,
// threads, messages fetched by id) until it finds one: for a stub only.
func (s *Store) HeldFile(id string) (slack.File, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	find := func(m *slack.Message) (slack.File, bool) {
		for i := range m.Files {
			if f := &m.Files[i]; f.ID == id && !FileStub(f) {
				return *f, true
			}
		}
		return slack.File{}, false
	}
	for _, t := range s.timelines {
		for i := range t.Msgs {
			if f, ok := find(&t.Msgs[i]); ok {
				return f, true
			}
		}
	}
	for _, th := range s.threads {
		for i := range th {
			if f, ok := find(&th[i]); ok {
				return f, true
			}
		}
	}
	for _, m := range s.fetched {
		if f, ok := find(m); ok {
			return f, true
		}
	}
	return slack.File{}, false
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
	s.setThreadWholeLocked(ch+"/"+ts, msgs) // the whole thread: one slice, both ends (thread_slices.go)
	s.mu.Unlock()
}

// AddReply appends a reply to a cached thread (if open) and bumps the parent.
func (s *Store) AddReply(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addReplyLocked(ch, m)
}

// AddReplyNoted is AddReply and the reply's live note (NoteLive) under
// one lock: a replies read landing between the two put an answer from
// before the reply back over the cached thread.
func (s *Store) AddReplyNoted(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addReplyLocked(ch, m)
	if ch != "" && m.TS != "" {
		s.noteLiveLocked(ch, m.TS, &m)
	}
}

func (s *Store) addReplyLocked(ch string, m slack.Message) {
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
		if !replaced {
			s.liveReplyLocked(key, m.TS) // its history's addTimestamps (thread_slices.go)
		}
	}
}

// ---- counts ----

// SetCounts installs a client.counts answer whole (SetCountsSince keeps
// what changed while the call was out).
func (s *Store) SetCounts(c *slack.Counts) { s.SetCountsSince(c, math.MaxUint64) }

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
	s.noteMarkLocked(ch) // a counts answer fetched before keeps this cursor (journal.go)
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
	s.noteMessageLocked(ch) // newer than a counts answer still out (journal.go)
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
	if mentionsSelf || s.countsAsDMLocked(ch) {
		e.MentionCount++
	}
	return *e
}

// countsAsDMLocked reports whether NoteMessage counts every message from
// others in ch as a mention, the way Slack badges a DM: an IM, or a group
// DM that isn't muted (a muted one badges on mentions only: Slack's
// unreadHighlightCnt). s.mu held.
func (s *Store) countsAsDMLocked(ch string) bool {
	c := s.Convs[ch]
	isDM := c != nil && (c.IsIM || c.IsMPIM) || strings.HasPrefix(ch, "D")
	if isDM && c != nil && c.IsMPIM && s.Muted[ch] {
		isDM = false
	}
	return isDM
}

// CountsAsDM is countsAsDMLocked for the conversation as the store holds it
// now.
func (s *Store) CountsAsDM(ch string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.countsAsDMLocked(ch)
}

// NoteLateDM is NoteMessage's DM rule for message m of ch, counted before
// the store knew ch (core held the message for its conversations.info):
// one mention more once ch turns out to be an IM or a group DM, unless the
// viewer read past it meanwhile or it is the viewer's own.
func (s *Store) NoteLateDM(ch string, m *slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Counts[ch]
	if !ok || !e.HasUnreads || !s.countsAsDMLocked(ch) {
		return
	}
	if self := s.Self.ID; m.User == self && self != "" {
		return
	}
	if e.LastRead != "" && !TSLess(e.LastRead, m.TS) {
		return
	}
	s.noteMessageLocked(ch) // newer than a counts answer still out, as NoteMessage's
	e.MentionCount++
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
	// Experiments is the last experiments.getByUser answer, name -> group:
	// Slack's experiments store is persisted too (client-boot 5fie,
	// persistenceEnabled), so checks read the account's groups before the
	// call answers again.
	Experiments map[string]string `json:"experiments,omitempty"`
}

// MaxSavedUsers bounds the users meta.json keeps: the most recently fetched
// (and the viewer). Slack persists its members store between starts
// (ukWt: persistenceEnabled), which its members cache keeps to 700,
// trimmed to 500 (members.go), and fetches anyone else when a message,
// mention or profile needs them, as EnsureUsers and the member fetcher do
// here; a start restoring every user a long-used Clack ever saw held tens
// of thousands in memory, 48 MB at the Hack Club account's scale.
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

// SetExperimentGroups keeps experiments.getByUser's groups for the disk
// cache (name -> group).
func (s *Store) SetExperimentGroups(g map[string]string) {
	cp := make(map[string]string, len(g))
	for k, v := range g {
		cp[k] = v
	}
	s.mu.Lock()
	s.experiments = cp
	s.mu.Unlock()
	s.gen.Add(1)
}

// CachedExperimentGroups returns the experiment groups the disk cache (or
// the last SetExperimentGroups) holds, nil when there are none.
func (s *Store) CachedExperimentGroups() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.experiments == nil {
		return nil
	}
	out := make(map[string]string, len(s.experiments))
	for k, v := range s.experiments {
		out[k] = v
	}
	return out
}

// HasCachedPrefs reports whether prefs came from the disk cache.
func (s *Store) HasCachedPrefs() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefsCached
}

// SetMyUsergroups replaces the viewer's user groups
// (bulkSetUserGroupMembership).
func (s *Store) SetMyUsergroups(ids []string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]bool, len(ids))
	for _, id := range ids {
		next[id] = true
	}
	if s.MyGroups != nil && maps.Equal(next, s.MyGroups) {
		return false // the usual answer: nothing a message reads moved
	}
	s.MyGroups = next
	s.gen.Add(1)
	return true
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

// GroupUsers is group id's member ids as last fetched (nil, false
// before the first fetch).
func (s *Store) GroupUsers(id string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, ok := s.groupUsers[id]
	return ids, ok
}

// SetGroupUsers is bulkAddMembersToGroup: ids replace group id's members.
// With appendPage it is appendMembersToGroup (a later page).
func (s *Store) SetGroupUsers(id string, ids []string, appendPage bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupUsers == nil {
		s.groupUsers = map[string][]string{}
	}
	if appendPage {
		ids = append(append([]string(nil), s.groupUsers[id]...), ids...)
	}
	old, had := s.groupUsers[id]
	s.groupUsers[id] = append([]string{}, ids...)
	if !had || !slices.Equal(old, ids) {
		s.ugGen.Add(1)
	}
}

// UsergroupGen counts changes to user group records, members and
// default channels.
func (s *Store) UsergroupGen() uint64 { return s.ugGen.Load() }

// GroupChannels is group id's default channels as last known (nil, false
// before usergroups.search or subteam_updated gave them).
func (s *Store) GroupChannels(id string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids, ok := s.groupChannels[id]
	return ids, ok
}

// SetGroupChannels records group id's default channels.
func (s *Store) SetGroupChannels(id string, ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groupChannels == nil {
		s.groupChannels = map[string][]string{}
	}
	old, had := s.groupChannels[id]
	s.groupChannels[id] = append([]string{}, ids...)
	if !had || !slices.Equal(old, ids) {
		s.ugGen.Add(1)
	}
}

// PutUsergroup is bulkAddGroups for one group (subteam_updated,
// subteam_created): g replaces the stored record with its id (members
// and default channels are kept apart, so they stay), or joins the list.
func (s *Store) PutUsergroup(g slack.Usergroup) {
	if g.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.Groups {
		if s.Groups[i].ID == g.ID {
			if s.Groups[i] == g {
				return
			}
			s.Groups[i], found = g, true
		}
	}
	if !found {
		s.Groups = append(s.Groups, g)
	}
	if g.Handle != "" {
		s.Usergroups[g.ID] = g.Handle
	}
	s.gen.Add(1)
	s.ugGen.Add(1)
}

// SetUsergroupCount is bulkAddMembersToGroup's user_count: the number of
// members it stored.
func (s *Store) SetUsergroupCount(id string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Groups {
		if s.Groups[i].ID == id && s.Groups[i].UserCount != n {
			s.Groups[i].UserCount = n
			s.gen.Add(1)
			s.ugGen.Add(1)
		}
	}
}

// SetUsergroupDescription is updateDescription (client-boot CjnZ ub):
// the description usergroups.update sent, once it answered ok.
func (s *Store) SetUsergroupDescription(id, desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Groups {
		if s.Groups[i].ID == id && s.Groups[i].Description != desc {
			s.Groups[i].Description = desc
			s.gen.Add(1)
			s.ugGen.Add(1)
		}
	}
}

// InUsergroup is richtext.UsergroupMember: whether the viewer is in group
// id (isUserInGroup, client-boot 3HzN nc).
func (s *Store) InUsergroup(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MyGroups[id]
}

// SetUsergroups installs usergroups (for <!subteam^> names and
// autocomplete) and reports whether they changed.
func (s *Store) SetUsergroups(gs []slack.Usergroup) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groupsLoaded = true
	if s.sameGroups(gs) {
		return false // the usual refresh: nothing to install or save
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
	s.ugGen.Add(1)
	return true
}

func (s *Store) LoadCache() {
	if rest := s.LoadCacheBase(); rest != nil {
		rest()
	}
}

// LoadCacheBase reads and installs the part of meta.json a launch's first
// frame reads: the users, the prefs, the channel names, the bots and the
// experiments (metaLayout). The returned rest reads and installs the
// custom emoji and the user groups (6.6 MB and 0.4 MB on Hack Club), once
// (nil: no cache, or none of it loaded). Until rest has run SaveCache
// writes nothing, as a save then would lose what rest installs. A file
// whose first part is cut short loads nothing; one cut inside the rest
// keeps its first part (Boot fetches the emoji then: the map is empty).
func (s *Store) LoadCacheBase() (rest func()) {
	if s.cacheDir == "" || s.dropped.Load() {
		return nil
	}
	t0 := time.Now()
	f, err := os.Open(filepath.Join(s.cacheDir, "meta.json"))
	if err != nil {
		return nil
	}
	var d diskCache
	var users []*slack.User
	m := newMetaDecoder(bufio.NewReaderSize(f, 64<<10), &d, func(u *slack.User) { users = append(users, u) })
	if err := m.base(); err != nil {
		f.Close()
		return nil
	}
	s.cachePending.Store(true)
	s.installCacheBase(&d, users)
	gen := s.gen.Load() // what the store holds now is what is on disk, so far
	tBase := time.Since(t0)
	return func() {
		defer s.cachePending.Store(false)
		t1 := time.Now()
		err := m.rest()
		f.Close()
		if err != nil || s.dropped.Load() {
			return // the first part stays; the next save writes the store whole
		}
		packed := len(d.EmojiPacked)
		s.installCacheRest(&d)
		s.saveMu.Lock()
		if s.gen.Load() == gen || !s.saved { // nothing else changed between the parts
			s.saved, s.savedGen, s.savedEmo = true, s.gen.Load(), s.CustomEmoji.Version()
		}
		s.saveMu.Unlock()
		if f := cacheLoadLog; f != nil {
			f(fmt.Sprintf("cache: meta.json (layout %d) %d users, prefs and names in %s; %d emoji (%.1f MB packed) and %d user groups in %s",
				m.layout, len(users), tBase.Round(time.Millisecond), s.CustomEmoji.Len(), float64(packed)/(1<<20), len(d.Usergroups),
				time.Since(t1).Round(time.Millisecond)))
		}
	}
}

// cacheLoadLog hears LoadCacheBase's timing line (core logs it as a debug
// line: the startup timeline's cache-read detail).
var cacheLoadLog func(string)

// SetCacheLoadLog sets cacheLoadLog (core, before Boot).
func SetCacheLoadLog(f func(string)) { cacheLoadLog = f }

// installCacheBase installs LoadCacheBase's part.
func (s *Store) installCacheBase(d *diskCache, users []*slack.User) {
	s.putUserPtrs(users, d.UserSeen)
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
		// under what this session stored or found gone meanwhile: that
		// answer is newer than the disk's copy, and nothing would ask for
		// the bot again (noteMissing records a bot once a session)
		if len(s.bots)+len(s.botsGone) == 0 {
			s.bots = d.Bots
		} else {
			if s.bots == nil {
				s.bots = make(map[string]BotInfo, len(d.Bots))
			}
			for id, b := range d.Bots {
				if _, ok := s.bots[id]; !ok && !s.botsGone[id] {
					s.bots[id] = b
				}
			}
		}
	}
	if d.Experiments != nil {
		s.experiments = d.Experiments
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
}

// installCacheRest installs the rest's part: the custom emoji (over the
// boot data's few, InstallBootEmoji) and the user groups.
func (s *Store) installCacheRest(d *diskCache) {
	if d.EmojiPacked != "" {
		s.CustomEmoji.Set(unpackEmoji(d.EmojiPacked))
		s.emojiPartial.Store(false)
	} else if len(d.Emoji) > 0 {
		s.CustomEmoji.Set(d.Emoji)
		s.emojiPartial.Store(false)
	}
	if d.EmojiAt > 0 {
		s.emojiAtMu.Lock()
		s.emojiAt = time.Unix(d.EmojiAt, 0)
		s.emojiAtMu.Unlock()
	}
	if len(d.Usergroups) > 0 {
		s.SetUsergroups(d.Usergroups)
	}
}

// SetEmojiList installs a fresh emoji.list result, compacted the way the
// disk cache stores it (see diskCache.EmojiPacked), and stamps its age.
func (s *Store) SetEmojiList(m map[string]string) {
	s.CustomEmoji.Set(unpackEmoji(packEmoji(m)))
	s.emojiPartial.Store(false)
	s.emojiAtMu.Lock()
	s.emojiAt = time.Now()
	s.emojiAtMu.Unlock()
}

// SetEmojiPacked installs a fresh emoji.list result already packed as
// "name\nvalue\n" pairs (slack.PackEmojiJSON): the map's keys and values
// share the one string.
func (s *Store) SetEmojiPacked(p string) {
	s.CustomEmoji.Set(unpackEmoji(p))
	s.emojiPartial.Store(false)
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
	if s.cacheDir == "" || s.cachePending.Load() {
		return nil // pending: the emoji and the user groups aren't in yet (LoadCacheBase)
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
	if s.experiments != nil {
		d.Experiments = s.experiments // replaced whole, never changed in place
	}
	// the stored users are never changed in place (PutUsers replaces
	// them), so they're written after the lock is let go: the viewer, the
	// people the sidebar names (DM partners, group DM members: a warm
	// boot's rows are labelled from these, bootdata.go) and the
	// MaxSavedUsers most recently fetched
	users := make([]*slack.User, 0, len(s.Users))
	for _, u := range s.Users {
		users = append(users, u)
	}
	if len(users) > MaxSavedUsers {
		self := s.Self.ID
		side := map[string]bool{}
		for _, c := range s.Convs {
			if c.IsIM && c.User != "" {
				side[c.User] = true
			}
			if c.IsMPIM {
				for _, m := range c.Members {
					side[m] = true
				}
			}
		}
		sort.Slice(users, func(i, j int) bool {
			if (users[i].ID == self) != (users[j].ID == self) {
				return users[i].ID == self
			}
			if a, b := side[users[i].ID], side[users[j].ID]; a != b {
				return a
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
