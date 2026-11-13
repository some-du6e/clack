package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestTabSlugLabel is useTabLabel (83sY) for every tab kind a slug can
// name, and its fallbacks.
func TestTabSlugLabel(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutCanvasRef("FCANVAS", richtext.CanvasRef{Title: "Roadmap", Status: "normal"})
	a.S.PutCanvasRef("FLIST", richtext.CanvasRef{Title: "Bugs", Status: "normal", Views: map[string]string{"V1": "Open"}})
	tab := func(id, typ, label, file string) slugTab {
		var s slugTab
		s.ID, s.Type, s.Label, s.Data.FileID = id, typ, label, file
		return s
	}
	list := tab("Lt1", "list", "", "FLIST")
	list.Data.DefaultView = "V1"
	folder := tab("Ft1", "folder", "", "")
	named := tab("Ft2", "folder", "", "")
	named.Data.FolderBookmarkID = "Bk1"
	cases := []struct {
		name string
		t    slugTab
		want string
	}{
		{"canvas, labeled", tab("Ct1", "canvas", "Our plan", "FCANVAS"), "Our plan"},
		{"canvas, the file's title", tab("Ct1", "canvas", "", "FCANVAS"), "Roadmap"},
		{"canvas, file not loaded", tab("Ct1", "canvas", "", "FNOTYET"), "Untitled"},
		{"list, its default view", list, "Bugs: Open"},
		{"folder of bookmarks", folder, "Bookmarks"},
		{"a bookmark's folder", named, "Untitled"},
		{"add canvas", tab("channel_canvas", "channel_canvas", "", ""), "Add canvas"},
		{"bookmarks", tab("bookmarks", "bookmarks", "Old name", ""), "Pins"},
		{"pins", tab("pins", "pins", "", ""), "Pins"},
		{"messages", tab("channel", "channel", "", ""), "Messages"},
		{"files", tab("files", "files", "", ""), "Files & links"},
		{"automations", tab("automations", "automations", "", ""), "Automations"},
		{"record summary", tab("record_summary", "record_summary", "", ""), "Summary"},
		{"record details", tab("work_object_overview", "work_object_overview", "", ""), "Record details"},
		{"workflows", tab("workflows", "workflows", "", ""), "Workflows"},
		{"threads, without channel_tabs_threads", tab("threads", "threads", "", ""), ""},
		{"skills", tab("slackbot_ai_skills", "slackbot_ai_skills", "", ""), "Skills"},
		{"tasks", tab("channel_tasks", "channel_tasks", "", ""), "Tasks"},
		{"agent tasks", tab("app_3p_agent_tasks", "app_3p_agent_tasks", "", ""), "Tasks"},
		{"import emails", tab("import_emails", "import_emails", "", ""), "Import Emails"},
		{"emails", tab("emails", "emails", "", ""), "Emails"},
		{"code", tab("code", "code", "", ""), "Code"},
		{"agent sessions tab", tab("agent_sessions", "agent_sessions", "", ""), "Code"},
		{"channel agent sessions", tab("channel_agent_sessions", "channel_agent_sessions", "", ""), "Agent Sessions"},
		{"app sessions", tab("app_sessions", "app_sessions", "", ""), "Sessions"},
		{"a labeled app tab", tab("Xt1", "agent_view", "Preview", ""), "Preview"},
		{"no label", tab("Xt2", "agent_view", "", ""), "Untitled"},
	}
	for _, c := range cases {
		if got := a.tabSlugLabel(c.t); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	a.SetExperimentGroups(map[string]*string{"resources_channel_tab": ptr("on"), "channel_tabs_threads": ptr("on"),
		"list_channel_tab_show_view_name_fe": ptr("off")})
	for _, c := range []struct {
		t    slugTab
		want string
	}{{tab("files", "files", "", ""), "Resources"}, {tab("threads", "threads", "", ""), "Threads"}, {list, "Bugs"}} {
		if got := a.tabSlugLabel(c.t); got != c.want {
			t.Errorf("%s with its experiment flipped: %q, want %q", c.t.ID, got, c.want)
		}
	}
	if got := tabSlugIcon("files", true); got != "files" {
		t.Errorf("Resources icon %q", got)
	}
	if got := tabSlugIcon("files", false); got != "canvas-browser" {
		t.Errorf("Files & links icon %q", got)
	}
}

func ptr(s string) *string { return &s }

// TestChannelTabSlugResolves: MrkdwnChannelTab resolves a slug's tab from
// properties.tabz (by id or type), else the Code tab; an unknown tab, an
// unknown channel or a private channel the viewer isn't in stays the
// plain mention. The channel's name reads "#name", or after its icon.
func TestChannelTabSlugResolves(t *testing.T) {
	a, _ := testApp(t)
	tabz := func(tabs ...map[string]any) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"tabz": tabs})
		return b
	}
	a.S.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true,
		Properties: tabz(map[string]any{"id": "Ct01", "type": "canvas", "label": "Plan"}, map[string]any{"id": "files", "type": "files"})})
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "secret", IsChannel: true, IsPrivate: true, IsMember: true,
		Properties: tabz(map[string]any{"id": "pins", "type": "pins"})})
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "old", IsChannel: true, IsArchived: true})
	a.S.PutExtConv(slack.Conversation{ID: "G2", Name: "closed", IsChannel: true, IsPrivate: true,
		Properties: tabz(map[string]any{"id": "pins", "type": "pins"})})

	got, ok := a.ChannelTabSlug("C1", "Ct01")
	if want := (richtext.TabSlug{Label: "Plan", Icon: "canvas-content", Name: "general"}); !ok || got != want {
		t.Errorf("canvas tab: %+v %v, want %+v", got, ok, want)
	}
	if got, ok := a.ChannelTabSlug("C1", "files"); !ok || got.Label != "Files & links" || got.Icon != "canvas-browser" {
		t.Errorf("files by type: %+v %v", got, ok)
	}
	if got, ok := a.ChannelTabSlug("G1", "pins"); !ok || got.ChIcon != "lock" || got.Label != "Pins" || got.Icon != "pin" {
		t.Errorf("a private channel's pins: %+v %v", got, ok)
	}
	if got, ok := a.ChannelTabSlug("C2", "code"); !ok || got.Label != "Code" || got.ChIcon != "archive" {
		t.Errorf("the Code tab of an archived channel: %+v %v", got, ok)
	}
	for _, c := range [][2]string{{"C1", "Ct99"}, {"C9", "files"}, {"G2", "pins"}} {
		if _, ok := a.ChannelTabSlug(c[0], c[1]); ok {
			t.Errorf("%s %s resolved; Slack draws the plain channel mention", c[0], c[1])
		}
	}

	// the message's spans: "{tab} in #channel" outside the channel, the
	// tab alone in it, the plain mention for a tab that doesn't resolve
	el := func(ch, tab string) map[string]any {
		return map[string]any{"type": "channel", "channel_id": ch, "tab_id": tab}
	}
	blocks, _ := json.Marshal([]map[string]any{{"type": "rich_text", "elements": []map[string]any{{"type": "rich_text_section",
		"elements": []map[string]any{el("C1", "Ct01"), el("G1", "pins"), el("C1", "Ct99")}}}}})
	m := &slack.Message{TS: "1.0", User: "U1", Blocks: blocks}
	spans := func(ch string) []richtext.Span { return richtext.FromMessageIn(a.S, m, ch).Blocks[0].Spans }
	out := spans("D1")
	if len(out) != 3 {
		t.Fatalf("spans %+v", out)
	}
	if s := out[0]; s.Kind != richtext.ChannelTab || s.Text != "Plan in #general" || s.URL != "/archives/C1/tabs/Ct01" || s.Icon != "canvas-content" {
		t.Errorf("canvas slug elsewhere: %+v", s)
	}
	if s := out[1]; s.Kind != richtext.ChannelTab || s.Text != "Pins in secret" || s.ChIcon != "lock" || s.Text[s.Pre:] != "secret" {
		t.Errorf("private channel slug: %+v", s)
	}
	if s := out[2]; s.Kind != richtext.Channel || s.Text != "#general" || s.URL != "/archives/C1/tabs/Ct99" {
		t.Errorf("unresolved tab: %+v", s)
	}
	if s := spans("C1")[0]; s.Text != "Plan" {
		t.Errorf("canvas slug in its own channel: %q", s.Text)
	}
}
