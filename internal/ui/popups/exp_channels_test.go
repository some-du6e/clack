package popups

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
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

// The switcher names a group DM with a custom name by it, and finds it by
// that name first, then by its members (y+lP filterMpims); with
// mpdm_naming off it is its member list again.
func TestSwitcherGroupDMNames(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Size{Emoji: 50, Users: 30, Channels: 8, DMs: 2, Usergroups: 2, History: 5, GroupDMs: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	on := "on"
	a.SetExperimentGroups(map[string]*string{"mpdm_naming": &on, "sc_mpdm_naming": &on})
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	srResetRecents()
	t.Cleanup(srResetRecents)
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	h := &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}
	cand := func(q, id string) (srCand, bool) {
		p := NewSearchPopover(h, q, image.Rectangle{})
		d := &srDrv{size: image.Pt(1280, 800), w: p}
		d.frame()
		for _, c := range srRank(p.cands, q, 0, false) {
			if c.ID == id {
				return c, true
			}
		}
		return srCand{}, false
	}
	c, ok := cand("design", fakeslack.GroupDMNamedID)
	if !ok || c.Name != "Design crew" {
		t.Fatalf("\"design\" finds %+v (%v)", c, ok)
	}
	if c, ok := cand("partn", fakeslack.GroupDMConnectID); !ok || c.Name != "Partners" {
		t.Errorf("the Slack Connect group DM: %+v (%v)", c, ok)
	}
	m, _ := a.User(fs.GroupDMUsers()[0].ID)
	if _, ok := cand(userName(m), fakeslack.GroupDMNamedID); !ok {
		t.Errorf("a member's name %q doesn't find the named group DM", userName(m))
	}
	off := "off"
	a.SetExperimentGroup("mpdm_naming", off)
	if c, ok := cand(userName(m), fakeslack.GroupDMNamedID); !ok || c.Name == "Design crew" {
		t.Errorf("mpdm_naming off: %+v (%v)", c, ok)
	}
}
