package taut

import (
	"encoding/json"
	"testing"
)

func TestHuddleState(t *testing.T) {
	tt := &Taut{}
	if r, _ := tt.huddleReaction([]json.RawMessage{json.RawMessage(`{"emoji":"tada"}`)}); r != false {
		t.Fatal("reaction without a huddle")
	}
	var got string
	tt.SetHuddleReaction(func(e, k, u string) bool { got = e + "/" + k; return true })
	tt.SetActiveHuddle("R1", "C1", "a1", "U1")
	tt.SetHuddlePeers(map[string]string{"a1": "U1", "a2": "U2"})
	if r, _ := tt.huddleReaction([]json.RawMessage{json.RawMessage(`{"emoji":"tada","reactionType":"standard"}`)}); r != true || got != "tada/standard" {
		t.Fatalf("reaction: %v %q", r, got)
	}
	p := tt.peersSlice()
	if by := p["byId"].(map[string]any); len(by) != 1 || by["a2"].(map[string]any)["userId"] != "U2" {
		t.Fatalf("peers: %v", p)
	}
	tt.SetHuddlePeers(map[string]string{"a1": "U1"})
	if left := tt.peersSlice()["leftById"].(map[string]any); left["a2"] == nil {
		t.Fatalf("left: %v", left)
	}
	_, _ = tt.huddleEventLog([]json.RawMessage{json.RawMessage(`true`)})
	if !tt.huddleEventsSlice()["enabled"].(bool) {
		t.Fatal("event log")
	}
	tt.SetActiveHuddle("", "", "", "")
	if tt.huddleEventsSlice()["enabled"].(bool) || len(tt.peersSlice()["byId"].(map[string]any)) != 0 {
		t.Fatal("reset")
	}
}
