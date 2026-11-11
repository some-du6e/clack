package ui

// The channels experiments against internal/fakeslack, each in the groups
// Slack's code tells apart (forced with core.SetExperimentGroup(s), the
// way the Experiments plugins override them): group DM names in the
// header, details and rename dialog, the wide topic's hover card and the
// topic permission, the bell menu's "Customized" tag, details' agents
// tabs and the list tab label. Synthetic only; with PREVIEW_SHOTS_DIR set
// (the exp-channels workflow) the tests also write screenshots.

import (
	"context"
	"image"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// gdmEnv is the client on a fake with the group DM fixtures; prep runs
// on the fake before the client boots.
func gdmEnv(t *testing.T, prep func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.GroupDMs = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if prep != nil {
		prep(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("PREVIEW_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	var ids []string // the group DMs' members, as the resolve loop would fetch them
	for _, u := range fs.GroupDMUsers() {
		ids = append(ids, u.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.a.Core().EnsureUsers(ctx, ids)
	e.a.data.resnapshot(e.a)
	e.settle(50 * time.Millisecond)
	return e, fs
}

func ptrs(kv ...string) map[string]*string {
	m := map[string]*string{}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		m[kv[i]] = &v
	}
	return m
}

// MPDMTitle in the header: a named group DM by its name (mpdm_naming, and
// sc_mpdm_naming for Slack Connect), else its members sorted, the first
// two and "N others" past three; the member names fill the topic slot of
// a named one.
func TestGroupDMHeaderTitles(t *testing.T) {
	e, fs := gdmEnv(t, nil)
	c := e.a.Core()
	c.SetExperimentGroups(ptrs("mpdm_naming", "on", "sc_mpdm_naming", "on"))
	e.settle(100 * time.Millisecond)
	us := fs.GroupDMUsers()
	names := func(n int) []string {
		var out []string
		for _, u := range us[:n] {
			cu, _ := c.User(u.ID)
			out = append(out, cu.Name)
		}
		slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
		return out
	}
	title := func(ch string) (string, bool) {
		e.openChannel(ch)
		e.settle(150 * time.Millisecond)
		return e.a.mpdmHeaderTitle(e.a.model().conv(ch))
	}
	if got, named := title(fakeslack.GroupDMNamedID); got != "Design crew" || !named {
		t.Errorf("named: %q %v", got, named)
	}
	e.shot("gdm-header-named")
	three := strings.Join(names(3), ", ")
	if got := mpdmMemberList(names(3)); got != three {
		t.Errorf("the member list %q", got)
	}
	if got, named := title(fakeslack.GroupDMPlainID); got != three || named {
		t.Errorf("plain: %q %v, want %q", got, named, three)
	}
	e.shot("gdm-header-plain")
	five := names(5)
	if got, _ := title(fakeslack.GroupDMBigID); got != five[0]+", "+five[1]+", 3 others" {
		t.Errorf("five others: %q", got)
	}
	if got, named := title(fakeslack.GroupDMConnectID); got != "Partners" || !named {
		t.Errorf("Slack Connect, sc_mpdm_naming on: %q %v", got, named)
	}

	c.SetExperimentGroups(ptrs("sc_mpdm_naming", ""))
	if got, named := title(fakeslack.GroupDMConnectID); got != three || named {
		t.Errorf("Slack Connect, sc_mpdm_naming unassigned: %q %v", got, named)
	}
	if _, named := title(fakeslack.GroupDMNamedID); !named {
		t.Error("sc_mpdm_naming turned off a plain group DM's name")
	}
	c.SetExperimentGroups(ptrs("mpdm_naming", "off"))
	if got, named := title(fakeslack.GroupDMNamedID); got != three || named {
		t.Errorf("mpdm_naming off: %q %v", got, named)
	}
	e.shot("gdm-header-naming-off")
}

// EditMpdmNameButton shows while the title is hovered under mpdm_naming;
// RenameMpdmDialog saves with conversations.rename, words Slack's errors,
// warns about a duplicate name, and the header shows the new name.
func TestGroupDMRename(t *testing.T) {
	e, fs := gdmEnv(t, nil)
	c := e.a.Core()
	c.SetExperimentGroups(ptrs("mpdm_naming", "on", "sc_mpdm_naming", "on"))
	ch := fakeslack.GroupDMPlainID
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	if !c.CanNameMpdm(ch) {
		t.Fatal("mpdm_naming on: the group DM can't be named")
	}
	r := hdrMpdmTitleRect.Add(e.a.primaryOrigin)
	e.move(f32.Pt(float32(r.Min.X+10), float32(r.Min.Y+r.Dy()/2)))
	e.settle(100 * time.Millisecond)
	if !hdrMpdmEditShown {
		t.Error("hovering the title shows no edit-name button")
	}
	e.shot("gdm-header-edit-name")

	var mu sync.Mutex
	fail := ""
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		if method == "conversations.rename" && fail != "" {
			return map[string]any{"ok": false, "error": fail}, true
		}
		return nil, false
	})
	e.a.openRenameMpdm(ch, e.a.model().conv(ch))
	e.settle(150 * time.Millisecond)
	d, ok := topOverlay[*popups.RenameMpdmDialog](e.a)
	if !ok {
		t.Fatal("no Edit conversation name dialog")
	}
	if d.Text() != "" || !strings.HasPrefix(d.Hints()[0], "Name your conversation with ") {
		t.Errorf("start %q, hints %q", d.Text(), d.Hints())
	}
	d.SetText("Design crew") // the named fixture's name
	e.settle(50 * time.Millisecond)
	if h := d.Hints(); len(h) != 2 || !strings.HasPrefix(h[1], "You already have a conversation with this name.") {
		t.Errorf("no duplicate warning: %q", h)
	}
	e.shot("gdm-rename-duplicate")
	mu.Lock()
	fail = "invalid_name_specials"
	mu.Unlock()
	d.SetText("Launch/crew")
	d.Save()
	e.until("the refusal", 10*time.Second, func() bool { return d.Error() != "" })
	if d.Error() != "Conversation names can’t contain the punctuation you included." || d.Dismissed() {
		t.Errorf("after invalid_name_specials: %q, dismissed %v", d.Error(), d.Dismissed())
	}
	e.shot("gdm-rename-error")
	mu.Lock()
	fail = ""
	mu.Unlock()
	d.SetText("Launch crew")
	e.frame()
	d.Save()
	e.until("saved", 10*time.Second, func() bool { return d.Dismissed() })
	calls := fs.Renames()
	if last := calls[len(calls)-1]; last.Get("channel") != ch || last.Get("name") != "Launch crew" || last.Get("validate") != "true" {
		t.Errorf("conversations.rename %v", last)
	}
	e.until("the header's new name", 10*time.Second, func() bool {
		got, named := e.a.mpdmHeaderTitle(e.a.model().conv(ch))
		return got == "Launch crew" && named
	})
	e.shot("gdm-header-renamed")

	c.SetExperimentGroups(ptrs("mpdm_naming", ""))
	e.settle(100 * time.Millisecond)
	e.move(f32.Pt(float32(r.Min.X+10), float32(r.Min.Y+r.Dy()/2)))
	e.settle(50 * time.Millisecond)
	if c.CanNameMpdm(ch) || hdrMpdmEditShown {
		t.Error("mpdm_naming unassigned: naming is still offered")
	}
}

// A topic too wide for the header is --truncated; hovering it opens the
// card with the whole topic. A header under 600px has no topic.
func TestHeaderTopicHoverCard(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	c := e.a.Core()
	sc, ok := c.S.Conv(ch)
	if !ok {
		t.Fatal("no member channel")
	}
	long := *sc
	long.Topic = &slack.Purpose{Value: strings.Repeat("A long topic that keeps going past the header's edge, ", 6) + "the end."}
	c.S.PutConv(long)
	e.a.data.resnapshot(e.a)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	if !hdrTopicTruncated {
		t.Fatal("the long topic isn't --truncated")
	}
	r := hdrTopicRect.Add(e.a.primaryOrigin)
	e.move(f32.Pt(float32(r.Min.X+6), float32(r.Min.Y+r.Dy()/2)))
	e.settle(100 * time.Millisecond)
	if !hdrTopicCardShown {
		t.Error("hovering the truncated topic shows no card")
	}
	e.shot("channel-header-topic-card")
	// the pointer may move down onto the card (inside the button's overflow)
	e.move(f32.Pt(float32(r.Min.X+20), float32(r.Max.Y+12)))
	e.settle(50 * time.Millisecond)
	if !hdrTopicCardShown {
		t.Error("the card closed when the pointer reached it")
	}
	e.move(f32.Pt(float32(r.Min.X+6), float32(r.Max.Y+300)))
	e.settle(50 * time.Millisecond)
	if hdrTopicCardShown {
		t.Error("the card stayed after the pointer left")
	}

	// a short topic isn't truncated and has no card
	short := *sc
	short.Topic = &slack.Purpose{Value: "Short"}
	c.S.PutConv(short)
	e.a.data.resnapshot(e.a)
	e.settle(100 * time.Millisecond)
	e.move(f32.Pt(float32(r.Min.X+6), float32(r.Min.Y+r.Dy()/2)))
	e.settle(50 * time.Millisecond)
	if hdrTopicTruncated || hdrTopicCardShown {
		t.Errorf("a short topic: truncated %v, card %v", hdrTopicTruncated, hdrTopicCardShown)
	}

	// @container (width < 600px): no topic
	e.size = image.Pt(600, 900)
	hdrTopicRect = image.Rectangle{}
	e.settle(100 * time.Millisecond)
	if !hdrTopicRect.Empty() {
		t.Errorf("a %dpx window still lays out the topic", e.size.X)
	}
	e.shot("channel-header-narrow")
}

// With channel_topic_purpose_setting_restrict_to_channel_managers on the
// header's topic is read-only without SET_CHANNEL_TOPIC
// (permissions/info), and details' Topic and Description are disabled;
// the grants bring Edit back.
func TestTopicRestrictedToManagers(t *testing.T) {
	var ch string
	e, _ := gdmEnv(t, func(fs *fakeslack.Server) {
		ch = fs.ChannelIDs()[1]
		fs.Permissions = map[string][]string{ch: {"CAN_AT_CHANNEL"}}
	})
	c := e.a.Core()
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	conv := e.a.model().conv(ch)
	if !e.a.canSetTopic(ch, conv) {
		t.Fatal("unassigned: a member can't set the topic")
	}
	c.SetExperimentGroup("channel_topic_purpose_setting_restrict_to_channel_managers", "on")
	e.settle(100 * time.Millisecond)
	if e.a.canSetTopic(ch, conv) {
		t.Error("on, no grant: the header offers Edit")
	}
	e.a.OpenDetails(ch)
	e.settle(400 * time.Millisecond)
	d := topDetails(t, e)
	if f := d.AboutFields(); f["Topic"] || f["Description"] {
		t.Errorf("on, no grant: details fields %v", f)
	}
	e.shot("details-topic-restricted")
	c.SetExperimentGroup("channel_topic_purpose_setting_restrict_to_channel_managers", "off")
	e.settle(50 * time.Millisecond)
	if f := d.AboutFields(); !f["Topic"] || !f["Description"] {
		t.Errorf("off: details fields %v", f)
	}
}

// ChannelNotificationPrefMenu tags the checked radio "Customized" when the
// channel's advanced settings differ from that choice's defaults.
func TestBellMenuCustomizedTag(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	// checked is the checked radio's key and every radio's tag
	checked := func() (string, map[string]string) {
		on, tags := "", map[string]string{}
		for _, it := range popups.ChannelNotifyItems(e.a, ch) {
			if it.Radio {
				tags[it.Key] = it.Tag
				if it.Checked {
					on = it.Key
				}
			}
		}
		return on, tags
	}
	on, tags := checked()
	if on == "" || tags["everything"] != "" || tags["mentions_dms"] != "" {
		t.Fatalf("defaults: checked %q, tags %v", on, tags)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.a.Core().SetChannelNotifyPref(ctx, ch, "follow_all_threads", "true"); err != nil {
		t.Fatal(err)
	}
	e.until("follow every thread", 10*time.Second, func() bool { return e.a.Core().NotifyPrefs(ch).FollowAllThreads })
	on, tags = checked()
	for k, tag := range tags {
		if want := map[bool]string{true: "Customized"}[k == on]; tag != want {
			t.Errorf("following every thread: %s tagged %q (checked %s)", k, tag, on)
		}
	}
	e.a.openNotifPrefs(ch)
	e.settle(150 * time.Millisecond)
	e.shot("channel-bell-customized")
	e.a.dismissTop()
	if err := e.a.Core().Mute(ctx, ch, true); err != nil { // the menu's Mute (actToggleMute → core.Mute)
		t.Fatal(err)
	}
	e.until("muted", 10*time.Second, func() bool { c, ok := e.a.Core().Conv(ch); return ok && c.Muted })
	e.settle(100 * time.Millisecond)
	if _, tags := checked(); tags["everything"] != "" || tags["mentions_dms"] != "" {
		t.Errorf("muted: %v", tags)
	}
}

// A group DM's details: MPDMInfo's title and, under mpdm_naming,
// MpdmConversationNameField, whose Edit opens RenameMpdmDialog stacked
// over the details.
func TestDetailsGroupDMName(t *testing.T) {
	e, _ := gdmEnv(t, nil)
	c := e.a.Core()
	c.SetExperimentGroups(ptrs("mpdm_naming", "on", "sc_mpdm_naming", "on"))
	ch := fakeslack.GroupDMNamedID
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(400 * time.Millisecond)
	d := topDetails(t, e)
	if d.Title() != "Design crew" {
		t.Errorf("title %q", d.Title())
	}
	if _, ok := d.AboutFields()["Conversation name"]; !ok {
		t.Error("no Conversation name field")
	}
	e.shot("gdm-details-named")
	d.Press("conversation-name")
	e.settle(150 * time.Millisecond)
	dlg, ok := topOverlay[*popups.RenameMpdmDialog](e.a)
	if !ok {
		t.Fatal("Conversation name's Edit opened no dialog")
	}
	if dlg.Text() != "Design crew" {
		t.Errorf("the dialog starts with %q", dlg.Text())
	}
	e.shot("gdm-details-rename")
	dlg.Dismiss()
	c.SetExperimentGroups(ptrs("mpdm_naming", "off"))
	e.settle(100 * time.Millisecond)
	if _, ok := d.AboutFields()["Conversation name"]; ok {
		t.Error("mpdm_naming off: the Conversation name field stays")
	}
	if d.Title() == "Design crew" {
		t.Error("mpdm_naming off: the title is still the custom name")
	}
}

// channel_details_agents_apps_tab: "Agents & apps" and "Automations" when
// on; otherwise no agents tab (it needs Agentforce or Slackbot data) and
// the integrations tab is "Integrations", after Tabs.
func TestDetailsAgentsAppsTabExperiment(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(300 * time.Millisecond)
	d := topDetails(t, e)
	if got := strings.Join(d.TabLabels(), ","); !strings.HasPrefix(got, "About,Members,Agents & apps,Automations,Tabs") {
		t.Errorf("on: %s", got)
	}
	for _, g := range []string{"off", ""} {
		e.a.Core().SetExperimentGroup("channel_details_agents_apps_tab", g)
		e.settle(50 * time.Millisecond)
		if got := strings.Join(d.TabLabels(), ","); !strings.HasPrefix(got, "About,Members,Tabs,Integrations") || strings.Contains(got, "Agents") {
			t.Errorf("%q: %s", g, got)
		}
	}
	e.shot("details-tabs-integrations")
}

// useTabLabel under list_channel_tab_show_view_name_fe: a list tab without
// its own label whose default view is one of the list's reads
// "<title>: <view>".
func TestListTabViewName(t *testing.T) {
	base := chTab{label: "Roadmap", icon: "list-tab", viewName: "Board"}
	if got := withListViewName(base).label; got != "Roadmap: Board" {
		t.Errorf("label %q", got)
	}
	for _, tb := range []chTab{
		{label: "Roadmap", icon: "list-tab", viewName: "Board", labeled: true},
		{label: "Roadmap", icon: "list-tab"},
		{label: "Notes", icon: "canvas", viewName: "Board"},
	} {
		if got := withListViewName(tb).label; got != tb.label {
			t.Errorf("%+v: %q", tb, got)
		}
	}
}
