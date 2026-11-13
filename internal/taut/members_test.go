package taut

import (
	"testing"
	"time"
)

// A plugin's members.getMember (useMember too) for a member the store
// doesn't hold, never fetched or evicted from the members cache
// (store/members.go): ensureMembersArePresent fetches it (edge
// users/info), and the JS side reads it at once instead of the absent
// entry it had cached, as Slack's selectors see the upsert. An evicted
// member is gone from state.members, as from Slack's store.
func TestGetMemberFetchesMissing(t *testing.T) {
	h := newHarness(t)
	h.api.mu.Lock()
	h.api.reply["edge/"+entID+"/users/info"] = map[string]any{"ok": true, "results": []map[string]any{
		{"id": "UCAROL01", "name": "carol", "real_name": "Carol Example", "profile": map[string]any{"display_name": "carol", "real_name": "Carol Example"}},
	}}
	h.api.mu.Unlock()
	var got struct {
		Before bool    `json:"before"`
		Name   *string `json:"name"`
		Ms     float64 `json:"ms"`
	}
	h.jsInto(`
		const before = T.redux.getRawState().members.UCAROL01 === undefined
		const t0 = Date.now()
		const m = await api.members.getMember('UCAROL01')
		return {before, name: m?.real_name ?? null, ms: Date.now() - t0}`, &got)
	if !got.Before || got.Name == nil || *got.Name != "Carol Example" {
		t.Fatalf("getMember: before=%v name=%v", got.Before, got.Name)
	}
	if got.Ms > 2500 { // getMember's retry waits 3 s: the first read saw it
		t.Errorf("getMember took %.0f ms (a retry: the first read saw the stale entry)", got.Ms)
	}
	if len(h.api.called("edge/"+entID+"/users/info")) == 0 {
		t.Error("users/info wasn't asked")
	}

	// evicted: gone from state.members once the mirror hears of it
	h.app.S.SetMembersSizeForDebug(1, 1)
	gone := h.app.EvictMembers()
	h.app.S.SetMembersSizeForDebug(-1, -1)
	if len(gone) == 0 {
		t.Fatal("nothing evicted")
	}
	eventually(t, 3*time.Second, "the evicted member gone from the mirror", func() bool {
		var absent bool
		h.jsInto(`return T.redux.getRawState().members.UCAROL01 === undefined && T.redux.getRawState().members.UALICE === undefined`, &absent)
		return absent
	})
}
