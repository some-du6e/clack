package ui

import (
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Karim (karim-1113c, macOS): "does the app icon on mac really have to
// bounce every time you get a notif". Slack's rule: the web client's
// maybeBounceDockIcon bounces only with mac_ssb_bounce set ("long":
// dock.bounceIndefinitely, Electron's "critical"; anything else
// dock.bounceOnce, "informational"; "" never), and the macOS shell's
// SLACK_BOUNCE_DOCK skips the bounce while Do Not Disturb or a Focus is on.
func TestDockBounceLikeSlack(t *testing.T) {
	for pref, want := range map[string][2]bool{"": {false, false}, "short": {true, false}, "long": {true, true}, "once": {true, false}} {
		if b, c := dockBounceKind(pref); b != want[0] || c != want[1] {
			t.Errorf("mac_ssb_bounce %q: bounce %v critical %v, want %v", pref, b, c, want)
		}
	}
	t.Cleanup(uipref.Reset)
	dockAnyOS = true
	var bounces []bool
	dnd := false
	oldB, oldD := dockBounce, doNotDisturb
	dockBounce = func(c bool) { bounces = append(bounces, c) }
	doNotDisturb = func() bool { return dnd }
	t.Cleanup(func() { dockAnyOS, dockBounce, doNotDisturb = false, oldB, oldD })
	a := &App{}
	for _, c := range []struct {
		pref string
		dnd  bool
	}{{"", false}, {"short", false}, {"long", false}, {"short", true}, {"long", true}} {
		uipref.Set(store.Prefs{MacBounce: c.pref})
		dnd = c.dnd
		a.bounceDock()
	}
	if !reflect.DeepEqual(bounces, []bool{false, true}) {
		t.Errorf("bounces %v, want once (short) then critical (long), none unset or under Do Not Disturb", bounces)
	}
}
