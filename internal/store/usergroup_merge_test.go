package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// MergeUsergroup is upsertEntity: the answer's fields over the stored
// group, the rest kept.
func TestMergeUsergroupKeepsMissing(t *testing.T) {
	s := New("")
	s.PutUsergroup(slack.Usergroup{ID: "S1", Handle: "admins", Name: "Admins", TeamID: "T1", AutoType: "admin", IsSection: true, DateUpdate: 7})
	g := s.MergeUsergroup("S1", func(g *slack.Usergroup) { g.Name, g.UserCount = "Team admins", 3 })
	if g.TeamID != "T1" || g.AutoType != "admin" || !g.IsSection || g.DateUpdate != 7 || g.Name != "Team admins" || g.Handle != "admins" {
		t.Errorf("merged %+v", g)
	}
	if s.Groups[0] != g {
		t.Errorf("stored %+v, want %+v", s.Groups[0], g)
	}
	n := s.MergeUsergroup("S2", func(g *slack.Usergroup) { g.Handle = "new" })
	if n.ID != "S2" || len(s.Groups) != 2 || s.UsergroupName("S2") != "new" {
		t.Errorf("a new group: %+v, %d stored", n, len(s.Groups))
	}
}

// The feature answer survives a restart (meta.json "features").
func TestFeatureAccessRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if _, ok := s.CachedFeatureAccess("user_groups__manage"); ok {
		t.Fatal("a new store has a feature answer")
	}
	s.SetFeatureAccess(map[string]bool{"user_groups__manage": true, "huddle__start": false})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	rest := s2.LoadCacheBase() // read before the first frame
	if on, ok := s2.CachedFeatureAccess("user_groups__manage"); !ok || !on {
		t.Errorf("after a restart: %v %v", on, ok)
	}
	rest()
	if on, ok := s2.CachedFeatureAccess("huddle__start"); !ok || on {
		t.Errorf("huddle__start: %v %v", on, ok)
	}
}
