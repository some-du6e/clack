package popups

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

func TestMenuKeys(t *testing.T) {
	items := []MenuItem{
		{Label: "Copy member ID"},
		Sep(),
		{Key: "sign-out", Label: "Sign out of Hack Club"},
		{Label: "Copy member ID"},
		Sep(),
	}
	got := menuKeys(items)
	want := []string{"copy_member_id", "separator_1", "sign-out", "copy_member_id_3", "separator_4"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("key %d = %q, want %q (all %q)", i, got[i], want[i], got)
		}
	}
}

// A plugin inserts a submenu before "sign-out" (AccountSwitcher), appends
// a separator and an item (AdminBackend) and drops a row; native rows keep
// their actions.
func TestMenuFromTemplateRoundTrip(t *testing.T) {
	ran := ""
	items := []MenuItem{
		{Label: "Copy display name", Do: func() { ran = "copy" }},
		{Label: "Copy member ID", Do: func() { ran = "id" }},
		Sep(),
		{Key: "sign-out", Label: "Sign out", Do: func() { ran = "out" }},
	}
	m := &Menu{}
	tpl := m.menuTemplate(items)
	if len(tpl) != 4 {
		t.Fatalf("template has %d items", len(tpl))
	}
	if e := asProps(tpl[2]); e.StrOr("type", "") != "separator" {
		t.Fatalf("separator item = %v", e)
	}
	var out []any
	for _, v := range tpl {
		e := asProps(v)
		switch e.StrOr("key", "") {
		case "sign-out":
			out = append(out, slackc.Props{
				"key": "taut-account-switcher", "label": "Switch account", "type": "submenu",
				"template": []any{slackc.Props{"key": "acct", "label": "Other", "click": slackc.JSFunc{ID: 7}}},
			})
		case "copy_member_id":
			continue // removed
		}
		out = append(out, v)
	}
	out = append(out,
		slackc.Props{"key": "taut-admin-backend__separator", "type": "separator"},
		slackc.Props{"key": "taut-admin-backend__tool-0", "label": "Open in admin", "click": slackc.JSFunc{ID: 9}},
	)
	got := menuFromTemplate(nil, out, items)
	labels := make([]string, len(got))
	for i, it := range got {
		labels[i] = it.Label
		if it.Separator {
			labels[i] = "-"
		}
	}
	want := []string{"Copy display name", "-", "Switch account", "Sign out", "-", "Open in admin"}
	if len(labels) != len(want) {
		t.Fatalf("rows = %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("rows = %q, want %q", labels, want)
		}
	}
	if len(got[2].Sub) != 1 || got[2].Sub[0].Label != "Other" || got[2].Sub[0].Do == nil {
		t.Fatalf("submenu = %+v", got[2].Sub)
	}
	if got[3].Key != "sign-out" || got[3].Do == nil {
		t.Fatalf("sign-out row lost its action: %+v", got[3])
	}
	got[3].Do()
	if ran != "out" {
		t.Fatalf("sign-out ran %q", ran)
	}
	got[0].Do()
	if ran != "copy" {
		t.Fatalf("copy ran %q", ran)
	}
}

// The sidebar row menu's getTemplate round trip (ChatTabs): the template
// is plain JSON-able data with Slack's keys; what a plugin returns comes
// back with the native actions by key and its own item clickable.
func TestChannelMenuTemplateRoundTrip(t *testing.T) {
	ran := ""
	do := func(key string) func() { return func() { ran = key } }
	items := ChannelMenuItems(ChannelMenuInfo{ID: "C1", Kind: "channel", Starred: true}, do)
	tpl := MenuTemplate(items)
	keys := make([]string, len(tpl))
	for i, e := range tpl {
		keys[i], _ = e["key"].(string)
		if _, ok := e["click"]; ok {
			t.Errorf("template item %s carries a click", keys[i])
		}
	}
	want := []string{"view_details", "copy_link", "copy_name", "separator_1", "toggle_star", "move_to_new_section", "toggle_mute", "mark_read", "separator_2", "leave"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	if tpl[4]["label"] != "Unstar channel" || tpl[9]["danger"] != true || tpl[3]["type"] != "separator" {
		t.Fatalf("template %v", tpl)
	}
	if _, err := json.Marshal(tpl); err != nil {
		t.Fatal(err)
	}
	// a plugin inserts its row before Leave and drops Copy name
	var out []any
	for _, e := range tpl {
		switch e["key"] {
		case "copy_name":
			continue
		case "leave":
			out = append(out, map[string]any{"key": "slick-chat-tabs-open", "label": "Open in new Slick tab", "click": slackc.JSFunc{ID: 3}})
		}
		out = append(out, map[string]any(e))
	}
	got := ItemsFromTemplate(nil, out, items)
	var gk []string
	for _, it := range got {
		gk = append(gk, it.Key)
	}
	want = []string{"view_details", "copy_link", "separator_1", "toggle_star", "move_to_new_section", "toggle_mute", "mark_read", "separator_2", "slick-chat-tabs-open", "leave"}
	if strings.Join(gk, ",") != strings.Join(want, ",") {
		t.Fatalf("items %v, want %v", gk, want)
	}
	if got[8].Label != "Open in new Slick tab" || got[8].Do == nil {
		t.Fatalf("plugin row %+v", got[8])
	}
	if !got[9].Danger || got[9].Do == nil {
		t.Fatalf("leave row %+v", got[9])
	}
	got[9].Do()
	if ran != "leave" {
		t.Fatalf("leave ran %q", ran)
	}
	dm := ChannelMenuItems(ChannelMenuInfo{ID: "D1", Kind: "im", Muted: true}, nil)
	if l := dm[len(dm)-1]; l.Key != "close" || l.Label != "Close conversation" || dm[6].Label != "Unmute conversation" || l.Do != nil {
		t.Fatalf("DM items %+v", dm)
	}
}

// The sidebar menu's section rows are getChannelSectionsTemplate's
// (boot-render, sidebar): Star and "Move to new section" without a custom
// section; "Move channel" ▸ (no Star) with one; "Move to section" ▸ under
// channel_ia_header_refinements; nothing for an archived channel or one
// you aren't in. The ▸ rows are the section picker's, with its actions.
func TestChannelMenuSectionRows(t *testing.T) {
	picked := ""
	move := []MenuItem{{Header: "Move to…"}, {Label: "Starred", Do: func() { picked = "Starred" }}, {Label: "Projects", Do: func() { picked = "Projects" }}}
	do := func(key string) func() { return func() {} }
	rows := func(c ChannelMenuInfo) (labels []string, items []MenuItem) {
		items = ChannelMenuItems(c, do)
		for _, it := range items[4:] { // after details, copy link, copy name, separator
			if it.Key == "toggle_mute" {
				break
			}
			labels = append(labels, it.Label)
		}
		return labels, items
	}
	cases := []struct {
		name string
		c    ChannelMenuInfo
		want string
	}{
		{"no custom section", ChannelMenuInfo{ID: "C1", Kind: "channel"}, "Star channel,Move to new section"},
		{"DM, no custom section", ChannelMenuInfo{ID: "D1", Kind: "im", Starred: true}, "Unstar conversation,Move to new section"},
		{"custom section", ChannelMenuInfo{ID: "C1", Kind: "channel", Custom: true, Move: move}, "Move channel"},
		{"DM, custom section", ChannelMenuInfo{ID: "D1", Kind: "im", Custom: true, Move: move}, "Move conversation"},
		{"refinements", ChannelMenuInfo{ID: "C1", Kind: "channel", MoveToSection: true, Move: move}, "Move to section"},
		{"archived", ChannelMenuInfo{ID: "C1", Kind: "channel", NoSections: true, Custom: true, Move: move}, ""},
	}
	for _, tc := range cases {
		labels, items := rows(tc.c)
		if got := strings.Join(labels, ","); got != tc.want {
			t.Errorf("%s: section rows %q, want %q", tc.name, got, tc.want)
			continue
		}
		for _, it := range items {
			if it.Key != "move" && it.Key != "move_to_section" {
				continue
			}
			if len(it.Sub) != len(move) || it.Do != nil {
				t.Errorf("%s: %s has %d sub rows (want %d) and Do %v", tc.name, it.Label, len(it.Sub), len(move), it.Do != nil)
				continue
			}
			picked = ""
			it.Sub[2].Do()
			if picked != "Projects" {
				t.Errorf("%s: the submenu's Projects ran %q", tc.name, picked)
			}
		}
	}
}

func TestTrimSeps(t *testing.T) {
	got := trimSeps([]MenuItem{Sep(), {Label: "a"}, Sep(), Sep(), {Label: "b"}, Sep()})
	if len(got) != 3 || got[0].Label != "a" || !got[1].Separator || got[2].Label != "b" {
		t.Fatalf("trimSeps = %+v", got)
	}
}
