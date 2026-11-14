package views

// Settings' managing-this-channel rows (details_managing.go): which rows
// show for which permissions, each modal's call (conversations.
// convertToPrivate, conversations.archive, conversations.delete once its
// checkbox is ticked), the archived channel's Unarchive
// (conversations.unarchive, no modal), the shared channel's disabled
// delete row, and the archive modal's error texts.

import (
	"slices"
	"testing"
)

func TestDetailsManagingRows(t *testing.T) {
	all := `["ARCHIVE_CHANNEL","DELETE_CHANNEL","CONVERT_TO_PRIVATE"]`
	for _, tc := range []struct {
		name, ch, allowed string
		want              cdManageOpts
	}{
		{"public, all", "C1", all, cdManageOpts{toPrivate: true, archive: true, del: true}},
		{"public, none", "C1", `[]`, cdManageOpts{}},
		{"public, under prefs", "C1", `["CONVERT_TO_PRIVATE_UNDER_PREFS","CREATE_PRIVATE_CHANNEL"]`, cdManageOpts{toPrivate: true}},
		{"private, all", "C9", all, cdManageOpts{archive: true, del: true}},
		{"archived", "C8", `["UNARCHIVE_CHANNEL","ARCHIVE_CHANNEL","DELETE_CHANNEL","CONVERT_TO_PRIVATE"]`, cdManageOpts{unarchive: true, del: true}},
		{"shared", "C7", all, cdManageOpts{toPrivate: true, del: true, delDisabled: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &cdTestServer{people: 4, allowed: tc.allowed}
			e := newCDTestEnv(t, srv, tc.ch)
			e.until("info", e.loaded)
			e.d.ShowTab("Settings")
			e.until("permissions", func() bool {
				_, ok, _ := e.d.x.perms.get()
				_, ok2, _ := e.d.x.teamPerms.get()
				return ok && ok2
			})
			e.frame()
			inf, _, _ := e.d.info.get()
			if got := e.d.manageOpts(inf); got != tc.want {
				t.Fatalf("rows %+v, want %+v", got, tc.want)
			}
			secs := e.d.settingsSections(inf)
			if got := slices.Contains(secs, cdSecManaging); got != tc.want.any() {
				t.Errorf("sections %q", secs)
			}
			if tc.ch == "C8" && !slices.Equal(secs, []string{cdSecManaging}) {
				t.Errorf("an archived channel's sections %q, want managing-this-channel alone", secs)
			}
		})
	}
}

func TestDetailsManagingModals(t *testing.T) {
	srv := &cdTestServer{people: 4, allowed: `["ARCHIVE_CHANNEL","DELETE_CHANNEL","CONVERT_TO_PRIVATE"]`}
	open := func(t *testing.T, ch string) *cdTestEnv {
		e := newCDTestEnv(t, srv, ch)
		e.until("info", e.loaded)
		e.d.ShowTab("Settings")
		e.until("permissions", func() bool { _, ok, _ := e.d.x.perms.get(); return ok })
		e.frame()
		e.scrollTo(cdSecManaging)
		return e
	}
	t.Run("to private", func(t *testing.T) {
		e := open(t, "C1")
		e.d.Press("to-private")
		e.frame()
		if e.d.Dialog() != "to-private" {
			t.Fatalf("the row opened %q", e.d.Dialog())
		}
		e.d.Press("ok") // Change to Private
		e.until("conversations.convertToPrivate", func() bool { return len(srv.seen("conversations.convertToPrivate")) > 0 })
		if f := srv.seen("conversations.convertToPrivate")[0].form; f.Get("channel") != "C1" || f.Has("name") || f.Has("user_ids") {
			t.Errorf("convertToPrivate form %v", f)
		}
		e.until("the modal closes", func() bool { return e.d.Dialog() == "" })
	})
	t.Run("archive", func(t *testing.T) {
		e := open(t, "C1")
		n := len(srv.seen("conversations.archive"))
		e.d.Press("archive")
		e.frame()
		if e.d.Dialog() != "archive" {
			t.Fatalf("the row opened %q", e.d.Dialog())
		}
		e.d.Press("ok") // Archive Channel
		e.until("conversations.archive", func() bool { return len(srv.seen("conversations.archive")) > n })
		e.until("the details close", e.d.Closed)
	})
	t.Run("delete", func(t *testing.T) {
		e := open(t, "C1")
		e.d.Press("delete")
		e.frame()
		if e.d.Dialog() != "delete" {
			t.Fatalf("the row opened %q", e.d.Dialog())
		}
		e.d.Press("ok") // disabled until the checkbox is ticked
		e.frame()
		e.frame()
		if len(srv.seen("conversations.delete")) != 0 {
			t.Fatal("Delete Channel sent before the checkbox was ticked")
		}
		// "archive a channel instead" swaps the modal for the archive one
		e.d.Press("archive-instead")
		e.frame()
		if e.d.Dialog() != "archive" {
			t.Fatalf("archive instead opened %q", e.d.Dialog())
		}
		e.d.Press("cancel")
		e.frame()
		e.d.Press("delete")
		e.frame()
		e.d.Press("delete-confirm")
		e.frame()
		e.d.Press("ok")
		e.until("conversations.delete", func() bool { return len(srv.seen("conversations.delete")) > 0 })
		if f := srv.seen("conversations.delete")[0].form; f.Get("channel") != "C1" {
			t.Errorf("delete form %v", f)
		}
		e.until("the details close", e.d.Closed)
	})
	t.Run("unarchive", func(t *testing.T) {
		usrv := &cdTestServer{people: 2, allowed: `["UNARCHIVE_CHANNEL"]`}
		e := newCDTestEnv(t, usrv, "C8")
		e.until("info", e.loaded)
		e.d.ShowTab("Settings")
		e.until("permissions", func() bool { _, ok, _ := e.d.x.perms.get(); return ok })
		e.frame()
		e.scrollTo(cdSecManaging)
		e.d.Press("unarchive")
		e.until("conversations.unarchive", func() bool { return len(usrv.seen("conversations.unarchive")) > 0 })
		if e.d.Dialog() != "" {
			t.Errorf("unarchive opened %q", e.d.Dialog())
		}
		e.until("the details close", e.d.Closed)
	})
}

func TestArchiveModalErrors(t *testing.T) {
	for _, c := range []struct {
		err            string
		admin, private bool
		want           string
	}{
		{"slack: already_archived", false, false, "This channel was already archived."},
		{"slack: cant_archive_general", false, false, "This channel cannot be archived."},
		{"slack: restricted_action", false, true, "You don’t have permission to archive private channels. Talk to your Workspace Owner."},
		{"slack: user_is_restricted", false, false, "You don’t have permission to archive channels. Talk to your Workspace Owner."},
		{"slack: last_ra_channel", true, false, "Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to. If you first disable the guest account, you will then be able to archive the channel."},
		{"slack: internal_error", false, false, "Archiving failed with error: internal_error"},
	} {
		if got := archiveModalError(errString(c.err), c.admin, c.private); got != c.want {
			t.Errorf("%s: %q", c.err, got)
		}
	}
}
