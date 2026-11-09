package store

import (
	"encoding/json"
	"strings"
)

// MorePrefs are users.prefs Clack applies beyond the first set, named so
// the zero value is Slack's default (the disk cache drops false fields).
// They are what Preferences' Notifications, Home, Messages & media and
// Language & region pages write (docs/PREFS-AUDIT.md).
type MorePrefs struct {
	// Keywords is all_notifications_prefs.global.global_keywords split on
	// commas (Slack's getHighlightWords, 8hkW): they act like mentions
	// (checkForMentionInMessage) and are highlighted in messages.
	Keywords []string `json:"keywords,omitempty"`
	// EmojiAsText is emoji_mode=as_text ("Display emoji as plain text"):
	// emoji draw as their :name: (shouldRenderEmojiAsText).
	EmojiAsText bool `json:"emoji_as_text,omitempty"`
	// LargeImgs is obey_inline_img_limit=false ("Even if they’re larger
	// than 20MB"): inline images over 20MB still expand.
	LargeImgs bool `json:"large_imgs,omitempty"`
	// SidebarAvatars is show_sidebar_avatars (DM rows show the person's
	// photo instead of a presence dot; Slack reads !!pref).
	SidebarAvatars bool `json:"sidebar_avatars,omitempty"`
	// NoHomeDot is home_unread_dot=false: the Home tab hides its dot and
	// badge (+oIJ hideMentions).
	NoHomeDot bool `json:"no_home_dot,omitempty"`
	// TZ is the tz pref (an IANA zone); SetTZAuto set_tz_automatically.
	TZ        string `json:"tz,omitempty"`
	NoTZAuto  bool   `json:"no_tz_auto,omitempty"`
	MacBounce string `json:"mac_ssb_bounce,omitempty"` // "", "short", "long"
	// NoMacBullet is mac_ssb_bullet=false (no "•" dock badge for unreads).
	NoMacBullet bool `json:"no_mac_bullet,omitempty"`
	// QuickReactions is user_customized_quick_reactions_display_feature
	// = 1 ("Show one-click reactions on messages"; shouldDisplayUsers-
	// QuickReactions: 2 waits on a server space Clack doesn't load, absent
	// is off).
	QuickReactions bool `json:"quick_reactions,omitempty"`
	// Dense is messages_theme=dense (Compact).
	Dense bool `json:"dense,omitempty"`
}

// parseMore fills MorePrefs from users.prefs.get (str reads a string pref).
func (p *Prefs) parseMore(raw map[string]json.RawMessage, str func(string) string) {
	boolean := func(k string, def bool) bool {
		v, ok := raw[k]
		if !ok {
			return def
		}
		switch strings.Trim(string(v), `"`) {
		case "true", "1":
			return true
		case "false", "0", "":
			return false
		}
		return def
	}
	if s := str("all_notifications_prefs"); s != "" {
		var n struct {
			Global struct {
				Keywords string `json:"global_keywords"`
			} `json:"global"`
		}
		if json.Unmarshal([]byte(s), &n) == nil {
			p.Keywords = SplitKeywords(n.Global.Keywords)
		}
	}
	p.EmojiAsText = str("emoji_mode") == "as_text"
	p.LargeImgs = !boolean("obey_inline_img_limit", true)
	p.SidebarAvatars = boolean("show_sidebar_avatars", false)
	p.NoHomeDot = !boolean("home_unread_dot", true)
	p.TZ = str("tz")
	p.NoTZAuto = !boolean("set_tz_automatically", true)
	p.MacBounce = str("mac_ssb_bounce")
	p.NoMacBullet = !boolean("mac_ssb_bullet", true)
	p.QuickReactions = strings.Trim(string(raw["user_customized_quick_reactions_display_feature"]), `"`) == "1"
	p.Dense = str("messages_theme") == "dense"
}

// SplitKeywords is Slack's highlight-words selector: the pref split on
// commas (empty and blank entries are dropped when matching).
func SplitKeywords(csv string) []string {
	var out []string
	for _, k := range strings.Split(csv, ",") {
		if strings.TrimSpace(k) != "" {
			out = append(out, k)
		}
	}
	return out
}

// HighlightWords is the viewer's keywords (richtext's highlighter).
func (s *Store) HighlightWords() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Prefs.Keywords
}

// EmojiAsText is emoji_mode=as_text (richtext's emoji texter).
func (s *Store) EmojiAsText() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Prefs.EmojiAsText
}
