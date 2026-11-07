package popups

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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

func TestTrimSeps(t *testing.T) {
	got := trimSeps([]MenuItem{Sep(), {Label: "a"}, Sep(), Sep(), {Label: "b"}, Sep()})
	if len(got) != 3 || got[0].Label != "a" || !got[1].Separator || got[2].Label != "b" {
		t.Fatalf("trimSeps = %+v", got)
	}
}
