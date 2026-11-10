package popups

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// n9iP Xq: the checked radio of the bell menu is "Customized" when the
// channel's advanced settings differ from that choice's defaults.
func TestNotifyCustomized(t *testing.T) {
	cases := []struct {
		name    string
		n       core.ChannelNotify
		desktop string
		muted   bool
		setting string
		want    bool
	}{
		{"plain just mentions", core.ChannelNotify{Desktop: "mentions_dms"}, "mentions_dms", false, "mentions_dms", false},
		{"follow every thread", core.ChannelNotify{Desktop: "mentions_dms", FollowAllThreads: true}, "mentions_dms", false, "mentions_dms", true},
		{"the other radio", core.ChannelNotify{Desktop: "mentions_dms", FollowAllThreads: true}, "mentions_dms", false, "everything", false},
		{"muted", core.ChannelNotify{Desktop: "mentions_dms", FollowAllThreads: true}, "mentions_dms", true, "mentions_dms", false},
		{"no broadcasts", core.ChannelNotify{Desktop: "mentions_dms", SuppressAtChannel: true}, "mentions_dms", false, "mentions_dms", true},
		{"no broadcasts, all posts", core.ChannelNotify{Desktop: "everything", SuppressAtChannel: true}, "everything", false, "everything", false},
		{"badge every message", core.ChannelNotify{Desktop: "everything", BadgeAllUnreads: true}, "everything", false, "everything", true},
		{"badge, just mentions", core.ChannelNotify{Desktop: "mentions_dms", BadgeAllUnreads: true}, "mentions_dms", false, "mentions_dms", false},
		{"mobile differs", core.ChannelNotify{Desktop: "everything", Mobile: "mentions_dms"}, "everything", false, "everything", true},
		{"mobile nothing reads as mentions", core.ChannelNotify{Desktop: "mentions_dms", Mobile: "nothing"}, "mentions_dms", false, "mentions_dms", false},
		{"mobile same", core.ChannelNotify{Desktop: "everything", Mobile: "everything"}, "everything", false, "everything", false},
		{"global default, mobile override", core.ChannelNotify{Mobile: "everything"}, "mentions_dms", false, "mentions_dms", true},
	}
	for _, c := range cases {
		if got := notifyCustomized(c.n, c.desktop, c.muted, c.setting); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if got := customizedTag(c.n, c.desktop, c.muted, c.setting); (got == "Customized") != c.want {
			t.Errorf("%s: tag %q", c.name, got)
		}
	}
}

// A menu item's tag crosses MenuFromTemplate as {label, style}; a plugin
// may drop it.
func TestMenuTagTemplate(t *testing.T) {
	items := []MenuItem{{Key: "everything", Label: "All new posts", Radio: true, Checked: true, Tag: "Customized", Do: func() {}}}
	m := &Menu{}
	tpl := m.menuTemplate(items)
	tag := asProps(asProps(tpl[0])["tag"])
	if tag == nil || tag.StrOr("label", "") != "Customized" || tag.StrOr("style", "") != "informative" {
		t.Fatalf("template tag %v", asProps(tpl[0])["tag"])
	}
	if got := menuFromTemplate(nil, tpl, items); len(got) != 1 || got[0].Tag != "Customized" {
		t.Errorf("round trip lost the tag: %+v", got)
	}
	e := asProps(tpl[0])
	e["tag"] = nil
	if got := menuFromTemplate(nil, []any{e}, items); len(got) != 1 || got[0].Tag != "" {
		t.Errorf("a plugin's removed tag stayed: %q", got[0].Tag)
	}
	e["tag"] = slackc.Props{"label": "New", "style": "informative"}
	if got := menuFromTemplate(nil, []any{e}, items); got[0].Tag != "New" {
		t.Errorf("a plugin's tag: %q", got[0].Tag)
	}
}

// dZPS getStringLength counts graphemes (Intl.Segmenter).
func TestGraphemeLen(t *testing.T) {
	cases := map[string]int{
		"":       0,
		"Design": 6,
		"café":   4,
		"café":  4, // e + combining acute
		"👍🏽":     1, // modifier
		"👨‍👩‍👧":  1, // ZWJ family
		"🇫🇷🇩🇪":   2, // two flags
		"❤️":     1, // variation selector
	}
	for s, want := range cases {
		if got := GraphemeLen(s); got != want {
			t.Errorf("GraphemeLen(%q) = %d, want %d", s, got, want)
		}
	}
}

// RenameMpdmDialog's error line for conversations.rename's error ids.
func TestRenameMpdmErrorText(t *testing.T) {
	punct := "Conversation names can’t contain the punctuation you included."
	perm := "You don’t have permission to rename."
	other := "For some weird reason, that didn’t work. Please try again to continue."
	for id, want := range map[string]string{
		"": "", "invalid_name": punct, "invalid_name_specials": punct, "restricted_action": perm,
		"not_authorized": perm, "name_taken": other, "unknown_error": other,
	} {
		if got := renameMpdmErrorText(id); got != want {
			t.Errorf("%q: %q", id, got)
		}
	}
}
