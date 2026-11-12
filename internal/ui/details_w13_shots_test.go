package ui

// W13 shots (ch-details-shots): channel details' Start a Call ▾ (the
// button, CallMenu, "Start a call?") and the channel email modal's panes,
// light and dark, against internal/fakeslack. Nothing is dialled or
// mailed: no call is started here, and the addresses are the fake's.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func TestChannelDetailsCallEmailShots(t *testing.T) {
	for _, mode := range []string{"light", "dark"} {
		t.Run(mode, func(t *testing.T) { chDetailsCallEmailShots(t, mode) })
	}
}

func chDetailsCallEmailShots(t *testing.T, mode string) {
	e, fs := previewEnv(t)
	cur := e.a.router.Current().Channel()
	ch := ""
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); ch == "" && c != nil && c.Kind == "channel" && !c.Preview && id != cur {
				ch = id
			}
		}
	}
	if ch == "" {
		t.Skip("no second member channel")
	}
	// before anything asks for the channel's grants (an hour's cache)
	fs.Permissions = map[string][]string{ch: {"CAN_AT_CHANNEL", "CAN_AT_EVERYONE", core.PermCreateChannelEmailAddresses}}
	fs.SetEnterpriseOrgPrefs(map[string]any{"channel_email_addresses_enabled": true})
	app := e.a.Core()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.LoadEnterprisePrefs(ctx); err != nil {
		t.Fatal(err)
	}
	var prefs map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"allow_calls":true,"calls_apps":{"video":[{"id":"A5GE9BMQC","name":"Zoom"},{"id":"AMEET0001","name":"Google Meet"}]}}`), &prefs)
	app.SetTeamPrefs(prefs)
	e.setDark(mode == "dark")
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e)
	e.until("the emails field", 10*time.Second, func() bool { return d.EmailsMode() == "button" })
	e.settle(300 * time.Millisecond)
	if vis, label, caret, _ := d.CallButton(); !vis || label != "Start a Call" || !caret {
		t.Fatalf("call button: %v %q caret %v", vis, label, caret)
	}
	e.shot("ch-details-call-about-" + mode)

	d.PressCall()
	e.until("the call menu", 5*time.Second, func() bool { return d.CallMenu() != nil })
	e.settle(800 * time.Millisecond)
	e.shot("ch-details-call-menu-" + mode)
	m := d.CallMenu()
	var meet func()
	for _, it := range m.Items() {
		if it.Label == "Google Meet" {
			meet = it.Do
		}
	}
	m.Dismiss()
	e.settle(100 * time.Millisecond)
	if meet == nil {
		t.Fatal("no Google Meet row")
	}
	meet()
	e.until("Start a call?", 5*time.Second, func() bool { return d.CallConfirm() != nil })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-call-confirm-" + mode)
	d.CallConfirm().Dismiss() // never start one
	e.settle(200 * time.Millisecond)

	d.PressEmails()
	e.until("the email modal", 5*time.Second, func() bool { return d.EmailModal() != nil })
	em := d.EmailModal()
	e.until("info", 10*time.Second, func() bool { return em.Status() == "done" })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-emails-get-" + mode)
	em.GetAddress()
	e.until("the address", 10*time.Second, func() bool { return em.Email().Address != "" })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-emails-address-" + mode)

	em.SwitchPane(popups.CEUpdate)
	em.Update().SetName("Build alerts")
	em.Update().SetEmoji("bell")
	e.settle(800 * time.Millisecond)
	e.shot("ch-details-emails-customize-" + mode)
	em.SwitchPane(popups.CEHelp)
	e.settle(2 * time.Second) // the illustration comes from Slack's CDN
	e.shot("ch-details-emails-help-" + mode)
	em.SwitchPane(popups.CEDelete)
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-emails-delete-" + mode)
	em.Dismiss()
	e.settle(100 * time.Millisecond)
}
