package views

// Settings' managing-this-channel row (details_convert_public.go) with
// fe_private_to_public_slack_connect_and_visual_updates on and off, for a
// member who may and may not convert the private channel, and the modal's
// conversations.convertToPublic.

import (
	"slices"
	"testing"
)

func TestDetailsConvertToPublic(t *testing.T) {
	for _, tc := range []struct {
		name               string
		visual, allowed    bool
		show, disabled     bool
		wantDisabledReason bool
	}{
		{"on, can't", true, false, true, true, true},
		{"on, can", true, true, true, false, false},
		{"off, can't", false, false, false, false, false},
		{"off, can", false, true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &cdTestServer{people: 4, allowed: `[]`}
			if tc.allowed {
				srv.allowed = `["CONVERT_PRIVATE_TO_PUBLIC"]`
			}
			e := newCDTestEnv(t, srv, "C9")
			g := "off"
			if tc.visual {
				g = "on"
			}
			e.h.app.SetExperimentGroup("fe_private_to_public_slack_connect_and_visual_updates", g)
			e.until("info", e.loaded)
			e.d.ShowTab("Settings")
			e.until("permissions", func() bool { _, ok, _ := e.d.x.perms.get(); return ok })
			e.frame()
			inf, _, _ := e.d.info.get()
			o := e.d.toPublicOpt(inf)
			secs := e.d.settingsSections(inf)
			if o.show != tc.show || o.disabled != tc.disabled || (o.text != "") != tc.wantDisabledReason {
				t.Fatalf("row %+v", o)
			}
			if got := slices.Contains(secs, cdSecManaging); got != tc.show || tc.show && secs[len(secs)-1] != cdSecManaging {
				t.Errorf("sections %q", secs)
			}
			if tc.wantDisabledReason && o.text != "You don’t have permission to change this channel to public" {
				t.Errorf("disabled text %q", o.text)
			}
			if !tc.show {
				return
			}
			e.scrollTo(cdSecManaging)
			e.d.Press("to-public")
			e.frame()
			if tc.disabled {
				if e.d.Dialog() != "" {
					t.Errorf("the disabled row opened %q", e.d.Dialog())
				}
				return
			}
			if e.d.Dialog() != "to-public" {
				t.Fatalf("the row opened %q", e.d.Dialog())
			}
			e.d.Press("ok") // Change to Public
			e.until("conversations.convertToPublic", func() bool { return len(srv.seen("conversations.convertToPublic")) > 0 })
			if f := srv.seen("conversations.convertToPublic")[0].form; f.Get("channel") != "C9" || f.Has("reason") {
				t.Errorf("convertToPublic form %v", f)
			}
			e.until("the modal closes", func() bool { return e.d.Dialog() == "" })
		})
	}
}

// A failed conversion keeps the modal open with Slack's error text.
func TestDetailsConvertToPublicError(t *testing.T) {
	if got := toPublicError(errString("slack: is_idp_managed")); got != "This channel cannot be changed to public because it’s managed by an IDP group." {
		t.Errorf("idp: %q", got)
	}
	if got := toPublicError(errString("slack: internal_error")); got != "Something went wrong. Sorry about that — please try again later, and contact Slack if this problem persists." {
		t.Errorf("other: %q", got)
	}
}
