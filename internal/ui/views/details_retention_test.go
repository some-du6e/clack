package views

// Settings' Message history row and export section
// (details_retention.go): who sees them (the rules are core's,
// channel_retention.go; here the sections Slack's i+kN builds from them),
// the row's text, MessageRetentionModal's getDataRetention /
// setDataRetention requests, and ExportChannelHistoryModal's
// conversations.export. The expected strings and values are Slack's
// (admin-channel-modal oWlM, CDN build 35b42de8b870).

import (
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// retViewerAs makes the viewer a workspace owner and/or admin.
func retViewerAs(e *cdTestEnv, owner, admin bool) {
	s := e.h.app.S
	u, _ := s.User("USELF")
	cp := *u
	cp.IsOwner, cp.IsAdmin = owner, admin
	s.PutUsers([]slack.User{cp})
	s.Lock()
	s.Self = cp
	s.Unlock()
}

func retOpen(t *testing.T, srv *cdTestServer, ch string, prep func(e *cdTestEnv)) *cdTestEnv {
	t.Helper()
	e := newCDTestEnv(t, srv, ch)
	if prep != nil {
		prep(e)
	}
	e.until("info", e.loaded)
	if ch[0] != 'D' {
		e.d.ShowTab("Settings")
		e.until("permissions", func() bool { _, ok, _ := e.d.x.perms.get(); return ok })
	}
	e.frame()
	return e
}

func TestDetailsRetentionSections(t *testing.T) {
	t.Run("member of a public channel", func(t *testing.T) {
		e := retOpen(t, &cdTestServer{people: 3}, "C1", func(e *cdTestEnv) {
			retPaid(e)
			e.h.app.SetExperimentGroup("channel_exports", "on")
		})
		inf, _, _ := e.d.info.get()
		if retHistoryShown(e) {
			t.Error("Message history shows a plain member of a public channel (dataRetention needs an owner or org admin)")
		}
		if slices.Contains(e.d.settingsSections(inf), retSecExport) {
			t.Error("Export shows a plain member of a public channel (exportHistory needs a workspace admin)")
		}
	})
	t.Run("owner, public channel", func(t *testing.T) {
		e := retOpen(t, &cdTestServer{people: 3, self: `"is_owner":true`}, "C1", func(e *cdTestEnv) { retPaid(e); retViewerAs(e, true, false) })
		if !retHistoryShown(e) {
			t.Fatal("no Message history for a workspace owner")
		}
		e.scrollTo(cdSecPermissions)
		e.d.Press("message-history")
		e.frame()
		if e.d.Dialog() != "retention" {
			t.Fatalf("Message history's Edit opened %q", e.d.Dialog())
		}
	})
	t.Run("member of a private channel, channel_exports", func(t *testing.T) {
		e := retOpen(t, &cdTestServer{people: 1}, "C9", func(e *cdTestEnv) {
			retPaid(e)
			e.h.app.SetExperimentGroup("channel_exports", "on")
		})
		inf, _, _ := e.d.info.get()
		secs := e.d.settingsSections(inf)
		i := slices.Index(secs, retSecExport)
		// i+kN: … add-member-suggestions, export, copy-members, …
		if i < 1 || secs[i-1] != cdSecMemberSuggest || i+1 >= len(secs) || secs[i+1] != cdSecCopyMembers {
			t.Errorf("sections %q: export goes between add-member-suggestions and copy-members", secs)
		}
		if !retHistoryShown(e) {
			t.Error("no Message history in a private channel whose team allows overrides")
		}
	})
	t.Run("DM", func(t *testing.T) {
		e := retOpen(t, &cdTestServer{people: 2}, "D1", func(e *cdTestEnv) {
			retPaid(e)
			e.h.app.SetExperimentGroup("channel_exports", "on")
		})
		e.until("the Settings tab", func() bool { return retSettingsTab(e) })
		inf, _, _ := e.d.info.get()
		if got := e.d.settingsSections(inf); !slices.Equal(got, []string{cdSecPermissions, retSecExport}) {
			t.Errorf("a DM's sections %q, want [permissions export]", got)
		}
		if got := e.d.permissionsRows(inf); len(got) != 1 {
			t.Errorf("a DM's permissions section has %d rows, want Message history alone", len(got))
		}
	})
	t.Run("DM, free team, no exports", func(t *testing.T) {
		e := retOpen(t, &cdTestServer{people: 2}, "D1", nil)
		for range 5 {
			e.frame()
		}
		if retSettingsTab(e) {
			t.Error("a DM shows Settings with neither Message history nor Export")
		}
	})
}

func TestDetailsRetentionModal(t *testing.T) {
	srv := &cdTestServer{people: 3, self: `"is_owner":true`, replies: map[string]string{
		"conversations.getRetention": `{"ok":true,"retention":{"retention_type":1,"retention_duration":30}}`,
	}}
	open := func(t *testing.T) *cdTestEnv {
		e := retOpen(t, srv, "C1", func(e *cdTestEnv) { retPaid(e); retViewerAs(e, true, false) })
		e.scrollTo(cdSecPermissions)
		e.d.Press("message-history")
		e.until("getDataRetention", func() bool { return retLoaded(e) })
		return e
	}
	e := open(t)
	if got := retRowText(e); got != "Delete messages and revisions after 30 days" {
		t.Errorf("row text %q", got)
	}
	if retType(e) != 1 || retDuration(e) != "30" {
		t.Errorf("the modal starts at type %d, %q days; want the channel's 1, 30", retType(e), retDuration(e))
	}
	// Save Changes stays disabled until a field changes
	e.d.Press("save")
	for range 3 {
		e.frame()
	}
	if n := len(srv.seen("conversations.setRetention")); n != 0 {
		t.Fatalf("Save Changes sent %d calls with nothing changed", n)
	}
	// 30 years: the days go out
	retPickUnit(e, 1)
	e.frame()
	e.d.Press("save")
	e.until("conversations.setRetention", func() bool { return len(srv.seen("conversations.setRetention")) == 1 })
	if f := srv.seen("conversations.setRetention")[0].form; f.Get("channel") != "C1" || f.Get("retention_type") != "1" || f.Get("retention_duration") != "10950" {
		t.Errorf("setRetention form %v, want channel C1, type 1, 10950 days", f)
	}
	e.until("the modal closes", func() bool { return e.d.Dialog() == "" })
	if n := len(srv.seen("conversations.getRetention")); n != 2 {
		t.Errorf("getRetention asked %d times, want the row's and the modal's (tU doesn't fetch again after a save)", n)
	}

	// the team's default: no duration
	e.d.Press("message-history")
	e.until("getDataRetention", func() bool { return retLoaded(e) })
	e.d.Press("retention-default")
	e.frame()
	e.d.Press("save")
	e.until("the second save", func() bool { return len(srv.seen("conversations.setRetention")) == 2 })
	if f := srv.seen("conversations.setRetention")[1].form; f.Get("retention_type") != "0" || f.Has("retention_duration") {
		t.Errorf("default setRetention form %v", f)
	}
	e.until("the modal closes", func() bool { return e.d.Dialog() == "" })

	// an invalid duration can't be saved; a failed save keeps the modal
	srv.mu.Lock()
	srv.replies["conversations.setRetention"] = `{"ok":false,"error":"internal_error"}`
	srv.mu.Unlock()
	e.d.Press("message-history")
	e.until("getDataRetention", func() bool { return retLoaded(e) })
	retSetDuration(e, "0")
	e.frame()
	e.d.Press("save")
	for range 3 {
		e.frame()
	}
	if n := len(srv.seen("conversations.setRetention")); n != 2 {
		t.Fatalf("0 days was saved (%d calls)", n)
	}
	retSetDuration(e, "45")
	e.frame()
	e.d.Press("save")
	want := "Oops, something went wrong with updating message retention settings. Please try again."
	e.until("the error", func() bool { return retErr(e) == want })
	if e.d.Dialog() != "retention" {
		t.Errorf("a failed save closed the modal (%q)", e.d.Dialog())
	}
}

func TestDetailsExportModal(t *testing.T) {
	srv := &cdTestServer{people: 1}
	e := retOpen(t, srv, "C9", func(e *cdTestEnv) {
		retPaid(e)
		e.h.app.SetExperimentGroup("channel_exports", "on")
	})
	e.scrollTo(retSecExport)
	e.d.Press("export")
	e.frame()
	if e.d.Dialog() != "export" {
		t.Fatalf("Export message history opened %q", e.d.Dialog())
	}
	e.d.Press("cancel")
	e.frame()
	if e.d.Dialog() != "" || len(srv.seen("conversations.export")) != 0 {
		t.Fatal("Cancel exported or kept the modal")
	}
	e.d.Press("export")
	e.frame()
	e.d.Press("ok") // Export Messages
	e.frame()
	if e.d.Dialog() != "" {
		t.Errorf("Export Messages left %q open (it closes at once)", e.d.Dialog())
	}
	e.until("conversations.export", func() bool { return len(srv.seen("conversations.export")) == 1 })
	if f := srv.seen("conversations.export")[0].form; f.Get("channel") != "C9" {
		t.Errorf("export form %v", f)
	}
}

// tU, getTeamRetentionDescription, getRetentionDurationWarning and
// getCustomRetentionDurationError, string for string.
func TestRetentionTextsLikeSlack(t *testing.T) {
	for _, c := range []struct {
		r    retR
		want string
	}{
		{retR{0, 0}, "Keep all messages, except for revisions"},
		{retR{1, 0}, "Keep all messages, including revisions"},
		{retR{2, 1}, "Delete messages and revisions after 1 day"},
		{retR{1, 400}, "Delete messages and revisions after 400 days"},
		{retR{1, 730}, "Delete messages and revisions after 2 years"},
		{retR{1, 365}, "Delete messages and revisions after 1 year"},
		{retR{2, 0}, ""},
	} {
		if got := retText(c.r); got != c.want {
			t.Errorf("text %+v: %q, want %q", c.r, got, c.want)
		}
	}
	for _, c := range []struct {
		dur, typ int
		want     string
	}{
		{0, 1, "By default, messages are kept forever."},
		{0, 7, "By default, messages are kept forever."},
		{0, 0, "By default, messages are kept forever, but revisions aren’t tracked."},
		{90, 2, "By default, messages older than 90 days will be deleted."},
		{1, 2, "By default, messages older than 1 day will be deleted."},
		{400, 2, "By default, messages older than 400 days (1.1 years) will be deleted."},
		{365, 3, "By default, messages older than 365 days (1 year) will be deleted (full history still accessible via compliance export)."},
		{30, 3, "By default, messages older than 30 days will be deleted (full history still accessible via compliance export)."},
	} {
		if got := retTeamDesc(c.dur, c.typ); got != c.want {
			t.Errorf("team description %d/%d: %q, want %q", c.dur, c.typ, got, c.want)
		}
	}
	for _, c := range []struct {
		name         string
		typ, n, unit int
		team         retR
		admin        bool
		want         string
	}{
		{"default period", 0, 10, 0, retR{2, 90}, false, ""},
		{"invalid", 1, 0, 0, retR{2, 90}, false, ""},
		{"under 30 days", 1, 10, 0, retR{1, 0}, false, "Historical messages help people find the information they need to get work done. Please check with your team before choosing to delete messages in this channel after 10 days."},
		{"longer than the team, member", 1, 1, 1, retR{2, 90}, false, "It looks like you’re choosing to keep messages in this channel longer than the rest of this workspace. You may want to check with an Admin to make sure that’s OK."},
		{"longer than the team, admin", 1, 1, 1, retR{2, 90}, true, "Heads up, It looks like you’re choosing to keep messages in this channel longer than the rest of this workspace."},
		{"over 15 years, team keeps all", 1, 16, 1, retR{1, 0}, false, "If you want to keep your messages forever, you can choose the default period for your organization."},
		{"15 years exactly", 1, 5475, 0, retR{1, 0}, false, ""},
	} {
		if got := retWarning(c.typ, c.n, c.unit, c.team, c.admin); got != c.want {
			t.Errorf("warning %s: %q, want %q", c.name, got, c.want)
		}
	}
	if got := retDurErr(0, 0); got != "Messages must be kept for at least 1 day." {
		t.Errorf("0 days: %q", got)
	}
	if got := retDurErr(100, 1); got != "Slack can only keep messages for up to 99 years, but thanks for your vote of confidence." {
		t.Errorf("100 years: %q", got)
	}
}

// isRetentionValid in Slack's float math: Years × 365 never wraps, so the
// values a 64-bit int would wrap into range (365 × 5508753709683126373 ≡ 1,
// 365 × -758085372892173354 ≡ 30) stay invalid, and parseInt reads
// numbers past 2^63 instead of failing.
func TestRetentionDurationBoundsLikeSlack(t *testing.T) {
	const tooLong = "Slack can only keep messages for up to 99 years, but thanks for your vote of confidence."
	const tooShort = "Messages must be kept for at least 1 day."
	for _, c := range []struct {
		text  string
		unit  int // 0 days, 1 years
		valid bool
		err   string
	}{
		{"5508753709683126373", 1, false, tooLong},
		{"-758085372892173354", 1, false, tooShort},
		{"99999999999999999999", 0, false, tooLong},
		{"99999999999999999999", 1, false, tooLong},
		{"99", 1, true, ""},
		{"100", 1, false, tooLong},
		{"36499", 0, true, ""},
		{"36500", 0, false, tooLong},
		{"0", 1, false, tooShort},
	} {
		if got := retValidText(1, c.text, c.unit); got != c.valid {
			t.Errorf("%s (unit %d) valid = %v, want %v", c.text, c.unit, got, c.valid)
		}
		if !c.valid {
			if got := retDurErrText(c.text, c.unit); got != c.err {
				t.Errorf("%s (unit %d) error %q, want %q", c.text, c.unit, got, c.err)
			}
		}
	}
	if got := retTitle(""); got != "Edit message history" {
		t.Errorf("a DM's title %q: an empty ChannelName leaves one space in HTML", got)
	}
	if got := retTitle("#plans"); got != "Edit #plans message history" {
		t.Errorf("a channel's title %q", got)
	}
}

func TestDetailsRetentionModalEdges(t *testing.T) {
	owner := func(e *cdTestEnv) { retPaid(e); retViewerAs(e, true, false) }
	edit := func(t *testing.T, srv *cdTestServer) *cdTestEnv {
		e := retOpen(t, srv, "C1", owner)
		e.scrollTo(cdSecPermissions)
		e.d.Press("message-history")
		e.until("getDataRetention", func() bool { return retLoadSettled(e) })
		e.frame()
		return e
	}
	custom := `{"ok":true,"retention":{"retention_type":1,"retention_duration":30}}`
	t.Run("a wrapping Years value isn't saved", func(t *testing.T) {
		srv := &cdTestServer{people: 3, self: `"is_owner":true`, replies: map[string]string{"conversations.getRetention": custom}}
		e := edit(t, srv)
		retPickUnit(e, 1)
		for _, v := range []string{"5508753709683126373", "-758085372892173354"} {
			retSetDuration(e, v)
			e.frame()
			e.d.Press("save")
			for range 3 {
				e.frame()
			}
		}
		if n := len(srv.seen("conversations.setRetention")); n != 0 {
			t.Errorf("Save Changes sent %d setRetention calls for a wrapped duration: %v", n, srv.seen("conversations.setRetention")[0].form)
		}
	})
	t.Run("a failed getRetention keeps the spinner", func(t *testing.T) {
		srv := &cdTestServer{people: 3, self: `"is_owner":true`, replies: map[string]string{
			"conversations.getRetention": `{"ok":false,"error":"ratelimited"}`}}
		e := edit(t, srv)
		for range 3 {
			e.frame()
		}
		if retLoaded(e) || retType(e) != 0 {
			t.Errorf("after a failed getRetention the form shows (type %d); Slack's BaseMessageRetention keeps the spinner", retType(e))
		}
	})
	t.Run("Escape closes the open Days / Years menu first", func(t *testing.T) {
		srv := &cdTestServer{people: 3, self: `"is_owner":true`, replies: map[string]string{"conversations.getRetention": custom}}
		e := edit(t, srv)
		retSetDuration(e, "45")
		e.frame()
		retOpenUnitMenu(e)
		e.frame()
		if !e.d.Escape() {
			t.Fatal("Escape closed the details")
		}
		e.frame()
		if e.d.Dialog() != "retention" || retUnitMenuOpen(e) {
			t.Errorf("after Escape: dialog %q, menu open %v; want the modal with the menu closed", e.d.Dialog(), retUnitMenuOpen(e))
		}
		if retDuration(e) != "45" {
			t.Errorf("the typed duration became %q", retDuration(e))
		}
	})
}

// A DM's Settings tab shows only when settingsSections has a section, so
// it never opens blank or appears and then goes away.
func TestDetailsRetentionDMTabStable(t *testing.T) {
	t.Run("archived group DM with exports on", func(t *testing.T) {
		e := newCDTestEnv(t, &cdTestServer{people: 3}, "G8")
		retPaid(e)
		e.h.app.SetExperimentGroup("channel_exports", "on")
		e.until("info", e.loaded)
		for range 5 {
			e.frame()
		}
		inf, _, _ := e.d.info.get()
		if retSettingsTab(e) && len(e.d.settingsSections(inf)) == 0 {
			t.Errorf("an archived group DM shows a Settings tab with no sections")
		}
	})
	t.Run("Grid org that turns overrides off, a DM the store doesn't hold", func(t *testing.T) {
		srv := &cdTestServer{people: 2, replies: map[string]string{
			"enterprise.prefs.get": `{"ok":true,"org_prefs":{},"locked_prefs":{},"unlocked_prefs":{"allow_retention_override":false}}`}}
		e := newCDTestEnv(t, srv, "D2")
		retPaid(e)
		if err := e.h.app.LoadEnterprisePrefs(t.Context()); err != nil {
			t.Fatal(err)
		}
		seen := false
		for i := 0; i < 40 && !(e.loaded() && i > 10); i++ {
			e.frame()
			seen = seen || retSettingsTab(e)
		}
		if seen {
			t.Error("the DM showed a Settings tab its org's unlocked allow_retention_override hides")
		}
	})
}
