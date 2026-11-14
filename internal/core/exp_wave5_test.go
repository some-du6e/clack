package core

import (
	"context"
	"testing"
)

// fNVg's read: "on" only.
func TestPrivateToPublicVisualExperiment(t *testing.T) {
	a, _ := testApp(t)
	for _, c := range []struct {
		group string
		want  bool
	}{{"on", true}, {"off", false}, {"control", false}, {"", false}} {
		a.setExperiments(map[string]string{ExpPrivateToPublicVisual: c.group})
		if got := a.PrivateToPublicVisualUpdates(); got != c.want {
			t.Errorf("group %q: %v, want %v", c.group, got, c.want)
		}
	}
}

// CX15 bF: the experiment on and the underline_links pref set.
func TestUnderlineButtonLinks(t *testing.T) {
	a, _ := testApp(t)
	for _, c := range []struct {
		group string
		pref  bool
		want  bool
	}{{"on", true, true}, {"on", false, false}, {"off", true, false}, {"", true, false}} {
		a.setExperiments(map[string]string{ExpUnderlineButtonLink: c.group})
		a.S.Lock()
		a.S.Prefs.UnderlineLinks = c.pref
		a.S.Unlock()
		if got := a.UnderlineButtonLinks(); got != c.want {
			t.Errorf("group %q, underline_links %v: %v, want %v", c.group, c.pref, got, c.want)
		}
	}
}

// Change to Public: conversations.convertToPublic {channel}, no reason.
func TestConvertToPublicCall(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.convertToPublic"] = `{"ok":true}`
	if err := a.ConvertToPublic(context.Background(), "G1"); err != nil {
		t.Fatal(err)
	}
	got := f.last("conversations.convertToPublic")
	if got == nil || got.Get("channel") != "G1" || got.Has("reason") {
		t.Errorf("form %v", got)
	}
}
