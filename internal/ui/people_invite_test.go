package ui

// Directories > People's "Invite People" and its education banner against
// internal/fakeslack (Slack's client-browse-people-view, build
// fa938a086569: InvitePeopleButton and BrowsePeopleView's
// ExplorerEducationBanner, shown by client-boot ot1p
// showPeopleEducationBanner). Synthetic only: the invite modal opens on
// the fake, and the banner's pref goes to the fake account.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// lastInviteModal is the newest invite modal on the overlay stack.
func lastInviteModal(a *App) *popups.InviteModal {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		if m, ok := a.overlays[i].(*popups.InviteModal); ok && !m.Dismissed() {
			return m
		}
	}
	return nil
}

// peopleBrowserHook answers the People browser's search.modules.people
// (desktop_people_browser) like a team of total people: eight synthetic
// cards on the page.
func peopleBrowserHook(total int) func(string, url.Values) (any, bool) {
	return func(method string, f url.Values) (any, bool) {
		if method != "search.modules.people" || f.Get("search_context") != "desktop_people_browser" {
			return nil, false
		}
		items := []any{}
		for i := 1; i <= 8 && i <= total; i++ {
			name := fmt.Sprintf("Fixture Person %d", i)
			items = append(items, map[string]any{"id": fmt.Sprintf("U0PEOPLE%02d", i), "name": fmt.Sprintf("person%d", i), "real_name": name,
				"profile": map[string]any{"real_name": name, "display_name": name, "title": "Synthetic fixture"}})
		}
		return map[string]any{"ok": true, "module": "people", "items": items,
			"pagination": map[string]any{"total_count": total, "page": 1, "page_count": (total + 49) / 50, "per_page": 50}}, true
	}
}

func TestPeopleBrowserInvite(t *testing.T) {
	e, fs := previewEnv(t)
	fs.SetHook(peopleBrowserHook(100))
	t.Cleanup(func() { fs.SetHook(nil) })
	if v, ok := fs.Pref("browsers_seen_initial_people_education"); ok && v == true {
		t.Fatal("the fake account starts with the banner seen")
	}
	e.a.Navigate("directories")
	e.settle(300 * time.Millisecond)
	v, ok := e.a.views[Route("directories")].(*views.Directories)
	if !ok {
		t.Fatalf("no Directories: %T", e.a.views[Route("directories")])
	}
	// a member of an open team: canUserInviteMembersToWorkspace and
	// canUserSeeInviteModal, and the banner not seen yet
	e.until("the People banner", 20*time.Second, func() bool { shown, _ := v.PeopleHero(); return shown })
	if _, btn := v.PeopleHero(); !btn || !v.InviteButtonShown() {
		t.Errorf("banner button %v, Invite People %v", btn, v.InviteButtonShown())
	}
	e.settle(200 * time.Millisecond)
	e.shot("people-invite-hero-light")
	e.a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.shot("people-invite-hero-dark")
	e.a.setDark(false)
	e.settle(100 * time.Millisecond)

	// view_once: the banner sets browsers_seen_initial_people_education
	e.until("the seen pref", 10*time.Second, func() bool {
		p, ok := fs.Pref("browsers_seen_initial_people_education")
		return ok && p == true
	})

	// Invite People: openInviteModal({source: "people_browser"})
	v.InvitePeople(false)
	e.frame()
	m := lastInviteModal(e.a)
	if m == nil || m.Source() != popups.InviteFromPeopleBrowser {
		t.Fatalf("Invite People opened %v", m)
	}
	m.Dismiss()
	e.settle(100 * time.Millisecond)
	// the banner's Invite people: people_browser_hero
	v.InvitePeople(true)
	e.frame()
	if m := lastInviteModal(e.a); m == nil || m.Source() != popups.InviteFromPeopleBrowserHero {
		t.Fatalf("the banner's Invite people opened %v", m)
	}
	lastInviteModal(e.a).Dismiss()
	e.settle(100 * time.Millisecond)
	if shown, _ := v.PeopleHero(); !shown {
		t.Error("the banner left mid-visit (Slack writes the pref when it unmounts)")
	}

	// the next visit: seen, and 100 people (not the low-results case), so
	// no banner
	e.a.Navigate("activity")
	e.settle(200 * time.Millisecond)
	e.a.Navigate("directories")
	e.settle(500 * time.Millisecond)
	if shown, _ := v.PeopleHero(); shown {
		p, _ := fs.Pref("browsers_seen_initial_people_education")
		t.Errorf("the banner came back on the next visit (seen pref %v)", p)
	}

	// invites_only_admins without requests: no Invite People, no banner
	var prefs map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"invites_only_admins":true}`), &prefs)
	e.a.data.core.SetTeamPrefs(prefs)
	e.settle(100 * time.Millisecond)
	if v.InviteButtonShown() {
		t.Error("Invite People shows for a member who can't invite or request")
	}
	v.InvitePeople(false)
	e.frame()
	if m := lastInviteModal(e.a); m != nil {
		t.Errorf("a member who can't invite opened %s", m.Source())
	}
	e.shot("people-invite-none-light")
}

// showPeopleEducationBanner's second branch: seen already, but the
// browser in its initial state with fewer than 7 people shows the banner
// again, unless browsers_dismissed_people_low_results_education is set.
func TestPeopleBrowserLowResultsBanner(t *testing.T) {
	for _, dismissed := range []bool{false, true} {
		e, _ := previewEnvWith(t, func(fs *fakeslack.Server) {
			fs.SetHook(peopleBrowserHook(3))
			fs.SetPrefs(map[string]any{"browsers_seen_initial_people_education": true,
				"browsers_dismissed_people_low_results_education": dismissed})
		})
		e.a.Navigate("directories")
		e.settle(300 * time.Millisecond)
		v := e.a.views[Route("directories")].(*views.Directories)
		e.until("the people", 20*time.Second, func() bool { return v.PeopleLoaded() })
		e.settle(200 * time.Millisecond)
		if shown, _ := v.PeopleHero(); shown == dismissed {
			t.Errorf("3 people, low-results dismissed %v: banner %v", dismissed, shown)
		}
	}
}
