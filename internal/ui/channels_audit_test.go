package ui

// The channels audit (docs/CHANNELS-AUDIT.md) against internal/fakeslack
// (synthetic workspace; nothing talks to Slack): the header star's
// "Move to…" section picker, the header and channel menu of a channel the
// viewer isn't in, channel details on Members / Agents & apps (the
// people-only and apps edge filters), and tab labels with emoji. With
// PREVIEW_SHOTS_DIR set (the preview-shots workflow) each state is saved.

import (
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/views"
)

func TestTabLabelSpans(t *testing.T) {
	emoji := func(name string) (string, string, bool) {
		switch name {
		case "cat-slackbot":
			return "https://emoji.example/cat.png", "", true
		case "tada":
			return "", "🎉", true
		}
		return "", "", false
	}
	if s := tabLabelSpans("clack testing", emoji); s != nil {
		t.Errorf("no emoji: %+v", s)
	}
	if s := tabLabelSpans("at 10:30 today: notes", emoji); s != nil {
		t.Errorf("colons that aren't emoji: %+v", s)
	}
	s := tabLabelSpans("clack testing :cat-slackbot:", emoji)
	if len(s) != 2 || s[0].Text != "clack testing " || s[0].Style&richtext.Bold == 0 || s[1].Kind != richtext.Emoji || s[1].URL == "" {
		t.Fatalf("custom emoji: %+v", s)
	}
	s = tabLabelSpans(":tada: launch :nope: :cat-slackbot:", emoji)
	var got []string
	for _, sp := range s {
		got = append(got, sp.Text)
	}
	if strings.Join(got, "|") != "🎉 launch :nope: |:cat-slackbot:" {
		t.Errorf("mixed: %q", got)
	}
}

// customSectionChannel is a channel the fake files in its "Hack Club"
// custom section (L0FAKE01), as the sidebar model has it.
func customSectionChannel(t *testing.T, e *perfEnv) string {
	t.Helper()
	var ch string
	e.until("a custom section with a channel", 20*time.Second, func() bool {
		if sec := e.a.sectionByID("L0FAKE01"); sec != nil {
			for _, id := range append(append([]string(nil), sec.Rows...), sec.All...) {
				if c := e.a.model().conv(id); c != nil && c.Kind == "channel" && !c.Starred {
					ch = id
					return true
				}
			}
		}
		return false
	})
	return ch
}

func TestChannelHeaderMoveTo(t *testing.T) {
	e, fs := previewEnv(t)
	ch := customSectionChannel(t, e)
	e.openChannel(ch)
	e.settle(300 * time.Millisecond)

	st := e.a.channelSections(ch)
	if st.custom == nil || st.custom.ID != "L0FAKE01" || st.starred {
		t.Fatalf("sections of %s: custom %+v starred %v", ch, st.custom, st.starred)
	}
	if icon, _ := headerStarIcon(st); icon != sectionIcon("standard") {
		t.Errorf("a channel in a custom section shows %q, not the section glyph", icon)
	}
	items := e.a.moveToItems(ch, st)
	want := "Move to… | Starred | Hack Club | Projects | Events | Random stuff | Bots |  | Create new section | Remove from section"
	if got := menuLabels(items); got != want {
		t.Errorf("Move to… menu:\n got %s\nwant %s", got, want)
	}
	for _, it := range items {
		if it.Checked != (it.Label == "Hack Club") {
			t.Errorf("%q checked=%v", it.Label, it.Checked)
		}
	}

	// the header star opens it
	n := len(e.a.overlays)
	o := e.a.primaryOrigin
	e.click(o.Add(image.Pt(18+14, int(theme.HeaderH)/2)))
	e.until("Move to… menu", 5*time.Second, func() bool { return len(e.a.overlays) > n })
	e.settle(150 * time.Millisecond)
	e.shot("channel-move-menu")
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	// Starred: stars.add, and out of Hack Club (performBulkChannelMove)
	for _, it := range items {
		if it.Label == "Starred" {
			it.Do()
		}
	}
	e.until("stars.add", 10*time.Second, func() bool { return fs.Starred(ch) })
	e.until("removed from Hack Club", 10*time.Second, func() bool {
		for _, s := range fs.Sections() {
			if s.ID == "L0FAKE01" {
				return !slices.Contains(s.Channels, ch)
			}
		}
		return false
	})
}

// Slack's header and channel menu for a channel you aren't in: no star,
// no bell, no "+" tab; the menu keeps Channel details, Copy, Add a
// workflow, Edit settings and Open in split view (C08E083LZ9A).
func TestChannelPreviewHeaderAndMenu(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslack.PreviewChannelID
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	e.until("join pane", 20*time.Second, func() bool { return e.a.footerFor(c) == footPreview && c.jp != nil })
	e.settle(300 * time.Millisecond)
	e.shot("channel-preview-header")

	// no star: the title starts where the star would be (a click there
	// opens details, not the Move to… menu)
	n := len(e.a.overlays)
	o := e.a.primaryOrigin
	e.click(o.Add(image.Pt(18+14, int(theme.HeaderH)/2)))
	e.settle(200 * time.Millisecond)
	if _, ok := topOverlay[*popups.Menu](e.a); ok {
		t.Error("the preview header still has the star's menu")
	}
	if len(e.a.overlays) == n {
		t.Error("the title at +18 didn't open channel details")
	}
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	m := popups.NewChannelMenu(e.a, image.Pt(900, 60), ch)
	got := strings.Join(m.Labels(), " | ")
	want := "Channel details | Copy | --- | Add a workflow | Edit settings | --- | Open in split view"
	if got != want {
		t.Errorf("preview channel menu:\n got %s\nwant %s", got, want)
	}
	member := popups.NewChannelMenu(e.a, image.Pt(900, 60), fakeslackMemberChannel(e))
	if l := strings.Join(member.Labels(), " | "); !strings.Contains(l, "Move channel") || !strings.Contains(l, "Leave channel") {
		t.Errorf("member channel menu lost rows: %s", l)
	}
}

// topDetails is the open channel details dialog.
func topDetails(t *testing.T, e *perfEnv) *views.ChannelDetails {
	t.Helper()
	o, ok := topOverlay[detailsOverlay](e.a)
	if !ok {
		t.Fatal("no channel details open")
	}
	return o.d
}

func fakeslackMemberChannel(e *perfEnv) string {
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); c != nil && c.Kind == "channel" && !c.Preview {
				return id
			}
		}
	}
	return ""
}

// Channel details: Members asks for people only (Slack's MEMBER_LIST
// filter), Agents & apps for the channel's apps; "Edit settings" opens
// the Settings tab.
func TestChannelDetailsMembersAndApps(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(300 * time.Millisecond)
	e.shot("channel-details-about")

	d := topDetails(t, e)
	d.ShowTab("Members")
	e.until("people-only users/list", 10*time.Second, func() bool {
		return slices.Contains(fs.UserListFilters(), "everyone AND NOT bots AND NOT apps")
	})
	e.settle(300 * time.Millisecond)
	e.shot("channel-details-members")
	d.ShowTab("Agents & apps")
	e.until("apps users/list", 10*time.Second, func() bool { return slices.Contains(fs.UserListFilters(), "apps") })
	e.settle(500 * time.Millisecond)
	e.shot("channel-details-apps")
	d.ShowTab("Settings")
	e.settle(200 * time.Millisecond)
	e.shot("channel-details-settings")
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	e.a.OpenDetailsTab(ch, "Settings")
	e.frame()
	if d := topDetails(t, e); d.Tab() != "Settings" {
		t.Errorf("Edit settings opened on %q", d.Tab())
	}
}

// The channel browser (Directories → Channels) with its filter bar and
// Reset, for the screenshot set.
func TestChannelBrowserShot(t *testing.T) {
	e, _ := previewEnv(t)
	e.a.Navigate("directories?tab=channels")
	e.settle(1500 * time.Millisecond)
	v, ok := e.a.views[Route("directories?tab=channels")].(*views.Directories)
	if !ok || v.Tab() != "Channels" {
		t.Fatalf("Browse channels opened %T on %q", e.a.views[Route("directories?tab=channels")], dirTabName(v))
	}
	e.shot("channel-browser")
}

// The header bell is Slack's ChannelNotificationPrefMenu: the level
// radios write users.prefs.setNotifications, "1 hour" a temporary mute.
func TestChannelBellMenu(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.openNotifPrefs(ch)
	e.settle(150 * time.Millisecond)
	m, ok := topOverlay[*popups.Menu](e.a)
	if !ok {
		t.Fatal("the bell opened no menu")
	}
	want := "[Notify you about…] | All new posts | Just mentions | Mute and hide | --- | More options"
	if got := strings.Join(m.Labels(), " | "); got != want {
		t.Errorf("bell menu:\n got %s\nwant %s", got, want)
	}
	e.shot("channel-bell-menu")
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	pick := func(items []popups.MenuItem, label string) popups.MenuItem {
		for _, it := range items {
			if it.Label == label {
				return it
			}
			for _, s := range it.Sub {
				if s.Label == label {
					return s
				}
			}
		}
		t.Fatalf("no %q row", label)
		return popups.MenuItem{}
	}
	c := e.a.Core()
	pick(popups.ChannelNotifyItems(e.a, ch), "All new posts").Do()
	e.until("desktop everything", 10*time.Second, func() bool { return c.NotifyPrefs(ch).Desktop == "everything" })
	if it := pick(popups.ChannelNotifyItems(e.a, ch), "All new posts"); !it.Checked {
		t.Error("All new posts isn't checked after choosing it")
	}
	before := time.Now().Unix()
	pick(popups.ChannelNotifyItems(e.a, ch), "1 hour").Do()
	e.until("temp_mute_expiration_ts", 10*time.Second, func() bool {
		exp := c.NotifyPrefs(ch).TempMuteExp
		return exp >= before+3500 && exp <= before+3700
	})
}

func dirTabName(v *views.Directories) string {
	if v == nil {
		return ""
	}
	return v.Tab()
}

// "Edit topic" is Slack's stacked modal: a refused save keeps it open
// with getChannelTopicError's text under the input; the retry closes it.
func TestChannelTopicEditModal(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	var mu sync.Mutex
	var topics []string
	fs.Hook = func(method string, form url.Values) (any, bool) {
		if method != "conversations.setTopic" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		topics = append(topics, form.Get("topic"))
		if len(topics) == 1 {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return map[string]any{"ok": true}, true
	}
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(300 * time.Millisecond)
	d := topDetails(t, e)
	d.EditTopic()
	e.settle(200 * time.Millisecond)
	if !d.Editing() {
		t.Fatal("Edit didn't open the topic modal")
	}
	if strings.Contains(d.EditText(), "<http") {
		t.Errorf("the editor shows raw link markup: %q", d.EditText())
	}
	e.shot("channel-topic-modal")
	d.SetEditText("Release notes go here")
	d.SaveEdit()
	e.until("the refusal", 10*time.Second, func() bool { return d.EditError() != "" })
	if !d.Editing() || !strings.Contains(d.EditError(), "don’t have permission to change the channel topic") {
		t.Errorf("after restricted_action: editing %v, error %q", d.Editing(), d.EditError())
	}
	e.settle(150 * time.Millisecond)
	e.shot("channel-topic-error")
	// (an identical retry would hit the client's backoff for a request
	// that just failed: slack.Client.post)
	d.SetEditText("Release notes go here, again")
	d.SaveEdit()
	e.until("the modal closes", 10*time.Second, func() bool { return !d.Editing() })
	mu.Lock()
	defer mu.Unlock()
	if len(topics) != 2 || topics[1] != "Release notes go here, again" {
		t.Errorf("setTopic calls %q", topics)
	}
}
