package ui

// W14's channel details parts against internal/fakeslack, light and dark
// (synthetic; the ch-details-shots workflow saves each state with
// PREVIEW_SHOTS_DIR): Settings › Posting permissions and its modal, the
// apps browser "Add an App"
// opens, and a DM's add people modal on Custom history with its preview.

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// w14Hooks answers the Settings reads with MANAGE_CHANNEL_PERMISSIONS, the
// posting feature on and the channel's posting prefs (admins plus
// specific people, threads open, mentions on), and the apps index with
// two installed apps and one to install.
func w14Hooks(fs *fakeslack.Server) {
	prefs := map[string]string{
		core.PrefWhoCanPost:   `{"type":["admin"],"user":["` + fakeslack.SelfID + `"]}`,
		core.PrefCanThread:    `{"type":["ra"]}`,
		core.PrefEnableHere:   `{"enabled":true}`,
		core.PrefEnableAtChan: `{}`,
	}
	img := func(seed string) string { return fs.URL + "img/72x72/" + seed + ".png" }
	index := map[string]any{"ok": true, "total_apps_installed": 2, "categories": []any{
		map[string]any{"id": "Ai01", "display_name": "Installed apps", "apps": []any{
			map[string]any{"app_id": "A0POLLY", "name": "Polly", "short_description": "Surveys and polls in Slack",
				"bot_user_id": "U0POLLYBOT", "bot_id": "B0POLLY", "is_installed": true, "icons": map[string]string{"image_72": img("polly")}},
			map[string]any{"app_id": "A0GIPHY", "name": "Giphy", "short_description": "An online library of animated GIFs",
				"bot_user_id": "U0GIPHYBOT", "bot_id": "B0GIPHY", "is_installed": true, "icons": map[string]string{"image_72": img("giphy")}},
		}},
		map[string]any{"id": "Ai02", "display_name": "Recommended apps", "apps": []any{
			map[string]any{"app_id": "A0JIRA", "name": "Jira Cloud", "short_description": "Create, track and act on issues",
				"is_installed": false, "icons": map[string]string{"image_72": img("jira")}},
		}},
	}}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "permissions.info":
			return map[string]any{"ok": true, "allowed": []string{"RENAME_CHANNEL", "MANAGE_CHANNEL_PERMISSIONS"}}, true
		case "conversations.permissions.accountTypes.list":
			return json.RawMessage(`{"ok":true,"account_types":[]}`), true
		case "conversations.canSetProperties":
			return json.RawMessage(`{"ok":true,"props":[]}`), true
		case "features.access.policies.list":
			return json.RawMessage(`{"ok":true,"feature_policies":{"manage_channel_posting__access":{"is_enabled":true}}}`), true
		case "channels.prefs.get":
			v, ok := prefs[form.Get("pref_name")]
			if !ok {
				v = "{}"
			}
			return json.RawMessage(`{"ok":true,"pref_value":` + v + `}`), true
		case "apps.index.list", "apps.index.search":
			return index, true
		}
		return nil, false
	})
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/channels/membership" {
			return map[string]any{"ok": true, "members": []string{"U0GIPHYBOT"}, "non_members": []string{"U0POLLYBOT"}}, true
		}
		return nil, false
	})
}

// shotBoth saves the frame as name-light and, after switching, name-dark,
// then switches back.
func (e *perfEnv) shotBoth(name string) {
	e.setDark(false)
	e.settle(300 * time.Millisecond)
	e.shot(name + "-light")
	e.setDark(true)
	e.settle(500 * time.Millisecond)
	e.shot(name + "-dark")
	e.setDark(false)
	e.settle(200 * time.Millisecond)
}

// Settings with Posting permissions, then PostingPermissionsModal as
// loaded (Admins only) and on Everyone (no Allow threads).
func TestChannelDetailsPostingShot(t *testing.T) {
	e, fs := previewEnv(t)
	w14Hooks(fs)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.Core().FeatureAccess(core.FeatureManageChannelPosting) // the boot read
	e.a.OpenDetailsTab(ch, "Settings")
	e.until("posting feature", 10*time.Second, func() bool { return e.a.Core().FeatureAccess(core.FeatureManageChannelPosting) })
	e.settle(800 * time.Millisecond)
	d := topDetails(t, e)
	if d.Tab() != "Settings" {
		t.Fatalf("opened on %q", d.Tab())
	}
	e.shotBoth("ch-details-posting")
	d.Press("posting")
	e.until("posting modal", 10*time.Second, func() bool { return d.Dialog() == "posting" && d.PostingLoaded() })
	e.settle(300 * time.Millisecond)
	e.shotBoth("ch-details-posting-modal")
	d.PostingSelect("ra")
	e.settle(200 * time.Millisecond)
	e.shotBoth("ch-details-posting-modal-everyone")
}

// "Add an App" (Press add-app): the apps browser over the window.
func TestChannelDetailsAppsBrowserShot(t *testing.T) {
	e, fs := previewEnv(t)
	w14Hooks(fs)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(400 * time.Millisecond)
	d := topDetails(t, e)
	d.Press("add-app")
	e.until("apps browser", 10*time.Second, func() bool {
		b, ok := topOverlay[*popups.AppsBrowser](e.a)
		return ok && !b.Loading() && len(b.Rows()) > 0
	})
	e.settle(800 * time.Millisecond)
	b, _ := topOverlay[*popups.AppsBrowser](e.a)
	for _, r := range b.Rows() {
		if r.Kind == "app" && r.App.AppID == "A0POLLY" && b.ActionLabel(r.App) != "Add" {
			t.Errorf("Polly's button %q, want Add", b.ActionLabel(r.App))
		}
	}
	e.shotBoth("ch-details-apps-browser")
}

// A DM's add people modal on Custom, from the first message's day, with
// its preview.
func TestChannelDetailsDMCustomHistoryShot(t *testing.T) {
	e, _ := previewEnv(t)
	dm := chDetailsFind(e, "im")
	if dm == "" {
		t.Skip("the fake workspace has no DM")
	}
	e.openChannel(dm)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(dm)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e)
	d.Press("add-people")
	e.until("add people", 10*time.Second, func() bool { return d.Dialog() == "add-people" })
	var other core.User
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); c != nil && c.Kind == "im" && id != dm {
				if u, ok := e.a.Core().User(c.User); ok {
					other = u
				}
			}
		}
	}
	if other.ID == "" {
		t.Skip("no second person")
	}
	d.AddPerson(other)
	e.settle(100 * time.Millisecond)
	d.Press("ok")
	e.until("history step", 10*time.Second, func() bool { return d.Dialog() == "add-people-history" })
	e.until("the preview from today", 10*time.Second, d.HistoryPreviewLoaded)
	e.settle(300 * time.Millisecond)
	e.shotBoth("ch-details-dm-history-today")
	e.until("the first message", 10*time.Second, d.HistoryCustomFromOldest)
	e.until("the custom preview", 10*time.Second, d.HistoryPreviewLoaded)
	e.settle(500 * time.Millisecond)
	e.shotBoth("ch-details-dm-history-custom")
}
