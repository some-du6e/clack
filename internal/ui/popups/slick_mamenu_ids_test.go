package popups

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// MAMenu's action ids are Slack's: a row with a Slack id (an app
// shortcut's action id included) is found by it wherever a patch moves
// it, and a row without one only under its own parent, so a Connect to
// apps shortcut named "Pin" can't turn into Organize's Pin, nor a
// reminder preset into another submenu's row.
func TestMAMenuRowIDs(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	ran := ""
	do := func(what string) func() { return func() { ran = what } }
	items := []MenuItem{
		{Action: "remind", Label: "Remind me about this", Sub: []MenuItem{
			{Key: LabelKey("In 30 mins"), Label: "In 30 mins", Do: do("remind in 30")},
		}},
		Sep(),
		{Action: "organize_actions_menu", Label: "Organize", Sub: []MenuItem{
			{Action: "pin", Label: "Pin to channel", Do: do("Organize's Pin")},
		}},
		{Action: "connect_to_apps_actions_menu", Label: "Connect to apps", Sub: []MenuItem{
			{Label: "Pin", Do: do("the app's Pin")},
			{Action: "Ft0APPSHORT1", QA: "app_action_Ft0APPSHORT1", Label: "Create a task", Do: do("the app shortcut")},
			{Action: "browse_apps", QA: "app_message_actions_dialog", Label: "More message shortcuts…", Do: do("browse apps")},
		}},
	}
	m := NewMenuOpts(h, items, MenuOpts{Anchor: image.Rect(900, 200, 900, 200), MAMenu: slackc.Props{}})

	// the descriptors MAMenu gets: ids and data-qa as Slack's
	groups := m.maProps()["actions"].([]any)
	var connect slackc.Props
	for _, g := range groups {
		for _, d := range g.([]any) {
			if p := d.(slackc.Props); p["id"] == "connect_to_apps_actions_menu" {
				connect = p
			}
		}
	}
	if connect == nil {
		t.Fatalf("no Connect to apps descriptor in %v", groups)
	}
	var got [][2]string
	for _, d := range connect["submenuActions"].([]any) {
		p := d.(slackc.Props)
		got = append(got, [2]string{p["id"].(string), p["data-qa"].(string)})
	}
	want := [][2]string{
		{"connect_to_apps_actions_menu/pin", "connect_to_apps_actions_menu/pin"},
		{"Ft0APPSHORT1", "app_action_Ft0APPSHORT1"},
		{"browse_apps", "app_message_actions_dialog"},
	}
	if len(got) != len(want) {
		t.Fatalf("Connect to apps' submenuActions %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("submenu action %d: %v, want %v", i, got[i], want[i])
		}
	}
	if id := maID(&m.ma.native[0].Sub[0]); id != "remind/"+LabelKey("In 30 mins") {
		t.Errorf("the reminder preset's id %q", id)
	}

	// a patch's layout: Organize's Pin moved to the top, the app's Pin in
	// Connect to apps, the app shortcut moved to the top too
	m.maApply(slackc.Props{"actions": []any{
		[]any{
			slackc.Props{"id": "pin", "name": "Pin to channel"},
			slackc.Props{"id": "Ft0APPSHORT1", "name": "Create a task"},
			slackc.Props{"id": "connect_to_apps_actions_menu", "name": "Connect to apps", "submenuActions": []any{
				slackc.Props{"id": "connect_to_apps_actions_menu/pin", "name": "Pin"},
			}},
		},
	}})
	if len(m.items) != 3 {
		t.Fatalf("rows %s", maIDs(m.items))
	}
	for _, c := range []struct {
		it   MenuItem
		want string
	}{
		{m.items[0], "Organize's Pin"},
		{m.items[1], "the app shortcut"},
		{m.items[2].Sub[0], "the app's Pin"},
	} {
		ran = ""
		if c.it.Do == nil {
			t.Errorf("%q does nothing", c.it.Label)
			continue
		}
		c.it.Do()
		if ran != c.want {
			t.Errorf("%q ran %q, want %q", c.it.Label, ran, c.want)
		}
	}
}
