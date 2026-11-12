package core

import "testing"

// huddles.history's form as Slack's page sends it: the mount fetch is
// just the limit (fetchHuddleHistory({})); a filtered one carries
// transformFiltersForApi's args, its two booleans always (false
// included), arrays comma-joined (getFormData), the Has flags only when
// set; a page adds the cursor.
func TestHuddleHistoryParamsLikeSlack(t *testing.T) {
	p := huddleHistoryParams(HuddleHistoryOpts{})
	if len(p) != 1 || p.Get("limit") != "30" {
		t.Errorf("mount fetch %v", p)
	}
	f := &HuddleFilters{Users: []string{"U1", "U2"}, Channels: []string{"C1"}, StartTS: "100", EndTS: "200",
		MissedOnly: true, UsersOnly: true, HasNotes: true, HasArtifacts: true}
	p = huddleHistoryParams(HuddleHistoryOpts{Cursor: "next", Filters: f})
	for k, want := range map[string]string{"limit": "30", "cursor": "next", "users": "U1,U2", "channels": "C1",
		"start_date_ts": "100", "end_date_ts": "200", "missed_huddles_only": "true", "users_only": "true",
		"has_huddle_notes": "true", "has_artifacts": "true"} {
		if got := p.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if p.Has("has_thread_replies") {
		t.Error("has_thread_replies sent unset")
	}
	p = huddleHistoryParams(HuddleHistoryOpts{Filters: &HuddleFilters{UsersOnly: true}})
	if p.Get("missed_huddles_only") != "false" || p.Get("users_only") != "false" || p.Has("users") {
		t.Errorf("empty filters %v (users_only needs someone)", p)
	}
	if (&HuddleFilters{}).Set() || !(&HuddleFilters{MissedOnly: true}).Set() {
		t.Error("HuddleFilters.Set")
	}
}

// A huddle object's page fields (huddles.history items).
func TestParseHuddlePageFields(t *testing.T) {
	h, ok := ParseHuddle([]byte(`{"id":"R1","has_ended":true,"date_start":10,"date_end":70,"channels":["C1"],
		"thread_root_ts":"1.000100","transcript_file_id":"F1","recording":{"recording_type":"transcription_only"},
		"artifacts":{"file_ids":["F2"],"links":[{"url":"https://example.invalid","title":"Doc"}]},
		"attached_file_ids":["F3"],"missed_participants":{"U2":{},"U1":{}},"channel_type":"public",
		"access_type_by_user":{"U1":0},"background_id":"STAR_GAZING","participant_history":["U1","U2"]}`))
	if !ok || !h.HasEnded || h.ThreadTS != "1.000100" || h.TranscriptFileID != "F1" || h.RecordingType != "transcription_only" ||
		len(h.FileIDs) != 1 || len(h.Links) != 1 || h.Links[0].Title != "Doc" || len(h.AttachedFileIDs) != 1 ||
		len(h.Missed) != 2 || h.Missed[0] != "U1" || h.ChannelType != "public" || h.AccessByUser["U1"] != 0 ||
		h.BackgroundID != "STAR_GAZING" || h.Active {
		t.Errorf("parsed %+v", h)
	}
}
