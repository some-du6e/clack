package core

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

func snapSection(a *App, id string) (Section, bool) {
	for _, s := range a.Snapshot().Sections {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

func channelSectionsPref(t *testing.T, fs *fakeslack.Server) map[string]map[string]any {
	t.Helper()
	v, _ := fs.Pref("channel_sections")
	s, _ := v.(string)
	out := map[string]map[string]any{}
	if s != "" {
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatalf("channel_sections %q: %v", s, err)
		}
	}
	return out
}

// Filter: a section's "sidebar" value goes into channel_sections; the
// first one that differs from the global behavior pins every section and
// switches sidebar_behavior to Custom by section (setChannelSectionPrefByApi).
// Stale section ids are dropped.
func TestSetSectionPrefFilter(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	fs.SetPrefs(map[string]any{"channel_sections": `{"LGONE":{"sidebar":"hid"}}`})
	if _, err := a.RefreshPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSectionPref(ctx, "L0FAKE01", "sidebar", "hid"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fs.Pref("sidebar_behavior"); v != "hide_read_channels_unless_starred" {
		t.Errorf("sidebar_behavior = %v, want Custom by section", v)
	}
	p := channelSectionsPref(t, fs)
	if p["L0FAKE01"]["sidebar"] != "hid" {
		t.Errorf("L0FAKE01 = %v", p["L0FAKE01"])
	}
	for _, id := range []string{"L0FAKE00", "L0FAKE02", "L0FAKECH", "L0FAKEDM"} {
		if p[id]["sidebar"] != "all" {
			t.Errorf("%s pinned to %v, want its old filter all", id, p[id]["sidebar"])
		}
	}
	if _, ok := p["LGONE"]; ok {
		t.Error("a deleted section's prefs survived")
	}
	sec, _ := snapSection(a, "L0FAKE01")
	if sec.Filter != "hid" {
		t.Errorf("filter %q, want hid", sec.Filter)
	}
	if len(sec.All) == 0 || len(sec.Channels)+len(sec.HiddenRead)+len(sec.HiddenMuted)+len(sec.HiddenUnread)+len(sec.HiddenDormant) != len(sec.All) {
		t.Errorf("rows %d + hidden read %d muted %d unread %d dormant %d != all %d", len(sec.Channels), len(sec.HiddenRead),
			len(sec.HiddenMuted), len(sec.HiddenUnread), len(sec.HiddenDormant), len(sec.All))
	}
	for _, id := range sec.Channels {
		if c := a.Count(id); !c.Unread && c.Mentions == 0 {
			t.Errorf("read %s still shows under Unreads", id)
		}
	}
	if len(sec.HiddenRead) == 0 {
		t.Error("nothing listed as hidden read")
	}
	// another section keeps showing everything
	if s2, _ := snapSection(a, "L0FAKE02"); s2.Filter != "all" {
		t.Errorf("L0FAKE02 filter %q after the switch", s2.Filter)
	}
}

// Sort on its own (equal filters) leaves sidebar_behavior alone.
func TestSetSectionPrefSort(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	if err := a.SetSectionPref(ctx, "L0FAKE02", "sort", "recent"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fs.Pref("sidebar_behavior"); v != "" {
		t.Errorf("sidebar_behavior = %v", v)
	}
	if p := channelSectionsPref(t, fs); p["L0FAKE02"]["sort"] != "recent" || len(p) != 1 {
		t.Errorf("channel_sections %v", p)
	}
	if sec, _ := snapSection(a, "L0FAKE02"); sec.Sort != "recent" {
		t.Errorf("sort %q", sec.Sort)
	}
	if sec, _ := snapSection(a, "L0FAKE03"); sec.Sort != "default" {
		t.Errorf("other section sort %q", sec.Sort)
	}
}

func TestSectionNameError(t *testing.T) {
	_, a := bootFake(t)
	for name, want := range map[string]string{
		"":                      SectionNameEmpty,
		"   ":                   SectionNameEmpty,
		strings.Repeat("x", 81): SectionNameTooLong,
		"ekm_access_denied":     SectionNameNotValid,
		"projects":              SectionNameTaken, // a custom section, any case
		" Channels ":            SectionNameTaken, // Slack's own names count
		"starred":               SectionNameTaken,
		"Launch":                "",
		strings.Repeat("y", 80): "",
	} {
		if got := a.SectionNameError(name, ""); got != want {
			t.Errorf("SectionNameError(%q) = %q, want %q", name, got, want)
		}
	}
	if got := a.SectionNameError("Projects", "L0FAKE02"); got != "" {
		t.Errorf("renaming Projects to itself: %q", got)
	}
	if SectionNameErrorText(SectionNameTaken) != "You already have a section with this name." || SectionNameErrorText(SectionNameEmpty) != "" {
		t.Error("error text")
	}
}

// Create (placed before the section whose menu opened it), rename, move
// conversations in (out of their old custom section), delete, undo.
func TestSectionLifecycle(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	var forms = map[string][]url.Values{}
	fs.Hook = func(m string, f url.Values) (any, bool) {
		if strings.HasPrefix(m, "users.channelSections.") {
			forms[m] = append(forms[m], f)
		}
		return nil, false
	}
	id, err := a.CreateSection(ctx, "Launch", ":rocket:", "L0FAKE02")
	if err != nil {
		t.Fatal(err)
	}
	f := forms["users.channelSections.create"][0]
	if f.Get("name") != "Launch" || f.Get("emoji") != ":rocket:" || f.Get("next_channel_section_id") != "L0FAKE02" {
		t.Errorf("create form %v", f)
	}
	var order []string
	for _, s := range a.Snapshot().Sections {
		order = append(order, s.ID)
	}
	i, j := slices.Index(order, id), slices.Index(order, "L0FAKE02")
	if i < 0 || j != i+1 {
		t.Errorf("new section at %d, Projects at %d: %v", i, j, order)
	}

	if err := a.RenameSection(ctx, id, "Liftoff", ":tada:"); err != nil {
		t.Fatal(err)
	}
	if f := forms["users.channelSections.set"][0]; f.Get("channel_section_id") != id || f.Get("name") != "Liftoff" || f.Get("emoji") != ":tada:" {
		t.Errorf("set form %v", f)
	}
	if s, _ := snapSection(a, id); s.Name != "Liftoff" || s.Emoji != ":tada:" {
		t.Errorf("renamed section %+v", s)
	}

	from, _ := snapSection(a, "L0FAKE03")
	if len(from.All) == 0 {
		t.Fatal("L0FAKE03 is empty")
	}
	ch := from.All[0]
	if a.OtherSectionOf(ch, id) != "L0FAKE03" {
		t.Errorf("OtherSectionOf(%s) = %q", ch, a.OtherSectionOf(ch, id))
	}
	if err := a.MoveToSection(ctx, id, []string{ch}, nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := snapSection(a, id); !slices.Contains(s.All, ch) {
		t.Errorf("%s not in the new section: %v", ch, s.All)
	}
	if s, _ := snapSection(a, "L0FAKE03"); slices.Contains(s.All, ch) {
		t.Errorf("%s still in its old section", ch)
	}

	d, err := a.DeleteSection(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapSection(a, id); ok {
		t.Error("deleted section still listed")
	}
	if sectionOf(a.Snapshot(), ch) != "channels" {
		t.Errorf("%s went to %q after the delete, want channels", ch, sectionOf(a.Snapshot(), ch))
	}
	nid, err := a.UndoDeleteSection(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := snapSection(a, nid)
	if !ok || s.Name != "Liftoff" || !slices.Contains(s.All, ch) {
		t.Errorf("undo: %+v", s)
	}
	if _, err := a.DeleteSection(ctx, "LNOPE"); err != nil {
		t.Errorf("section_not_found should count as deleted: %v", err)
	}
}

func TestMuteMany(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	var form url.Values
	fs.Hook = func(m string, f url.Values) (any, bool) {
		if m == "users.prefs.setNotifications" {
			form = f
		}
		return nil, false
	}
	ids := fs.ChannelIDs()[:3]
	if err := a.MuteMany(ctx, ids, true); err != nil {
		t.Fatal(err)
	}
	if form.Get("name") != "muted" || form.Get("value") != "true" || form.Get("global") != "false" ||
		form.Get("channel_ids") != strings.Join(ids, ",") {
		t.Errorf("form %v", form)
	}
	for _, id := range ids {
		if c, _ := a.Conv(id); !c.Muted {
			t.Errorf("%s not muted", id)
		}
	}
	if err := a.MuteMany(ctx, ids, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if c, _ := a.Conv(id); c.Muted {
			t.Errorf("%s still muted", id)
		}
	}
}

func TestMarkManyRead(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	var unread []string
	for _, c := range a.Snapshot().Convs {
		if cnt := a.Count(c.ID); cnt.Unread && c.Kind == "channel" {
			unread = append(unread, c.ID)
		}
	}
	if len(unread) < 2 {
		t.Fatalf("fake has %d unread channels", len(unread))
	}
	before := map[string]string{}
	for _, id := range unread {
		before[id] = a.Count(id).LastRead
	}
	prev, err := a.MarkManyRead(ctx, unread)
	if err != nil {
		t.Fatal(err)
	}
	if len(prev) != len(unread) {
		t.Errorf("marked %d of %d", len(prev), len(unread))
	}
	for _, id := range unread {
		if c := a.Count(id); c.Unread || c.Mentions != 0 {
			t.Errorf("%s still unread", id)
		}
		if fs.Marked(id) == "" {
			t.Errorf("no conversations.mark for %s", id)
		}
	}
	if err := a.UndoMarkRead(ctx, prev); err != nil {
		t.Fatal(err)
	}
	for _, id := range unread {
		if fs.Marked(id) != before[id] {
			t.Errorf("undo marked %s at %s, want %s", id, fs.Marked(id), before[id])
		}
	}
}

func TestSectionLink(t *testing.T) {
	fs, a := bootFake(t)
	u, err := a.SectionLink(context.Background(), "Projects", ":rocket:", []string{"C1", "C2"})
	if err != nil || !strings.Contains(u, "/section/") {
		t.Fatalf("link %q, %v", u, err)
	}
	f := fs.SectionLinks()[0]
	if f.Get("section_name") != "Projects" || f.Get("emoji") != ":rocket:" || f.Get("channels") != "C1,C2" {
		t.Errorf("createLink form %v", f)
	}
}
