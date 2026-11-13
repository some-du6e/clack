package store

import "testing"

// TestExperimentGroupsPersist: experiments.getByUser's groups go to
// meta.json and come back on the next start, as Slack persists its
// experiments store.
func TestExperimentGroupsPersist(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.SetExperimentGroups(map[string]string{"temp_mute": "on", "inviter_joiner_first_dm_v2": "control"})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	if s2.CachedExperimentGroups() != nil {
		t.Fatal("experiment groups before LoadCache")
	}
	s2.LoadCache()
	got := s2.CachedExperimentGroups()
	if len(got) != 2 || got["temp_mute"] != "on" || got["inviter_joiner_first_dm_v2"] != "control" {
		t.Errorf("restored groups %v", got)
	}
	got["temp_mute"] = "off"
	if s2.CachedExperimentGroups()["temp_mute"] != "on" {
		t.Error("CachedExperimentGroups handed out the store's map")
	}
}
