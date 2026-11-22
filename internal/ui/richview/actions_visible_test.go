package richview

import "testing"

// TestActionsHiddenElements: an Actions block whose every element is
// hidden from the viewer (visible_to_user_ids; codeRes's viewer is U0)
// draws nothing but stays, as Slack's empty .p-actions_block does: its
// 8px bottom margin (the 8px above it came from its elements' own
// margins), so the next block sits 8px lower than without it. A hidden
// button among visible ones isn't drawn.
func TestActionsHiddenElements(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[
		{"type":"section","text":{"type":"mrkdwn","text":"Alfa"}},
		{"type":"actions","block_id":"a","elements":[{"type":"button","action_id":"x","text":{"type":"plain_text","text":"Xray"},"visible_to_user_ids":["U2"]}]},
		{"type":"section","text":{"type":"mrkdwn","text":"Bravo"}},
		{"type":"actions","block_id":"b","elements":[
			{"type":"button","action_id":"y","text":{"type":"plain_text","text":"Yankee"},"visible_to_user_ids":["U2"]},
			{"type":"button","action_id":"z","text":{"type":"plain_text","text":"Zulu"}}]}
	]`)
	v := layoutAt(st, d, Options{}, 800)
	alfa, bravo := findFrag(t, &v.d, "Alfa"), findFrag(t, &v.d, "Bravo")
	// Alfa's 22px line and its section's 4px bottom margin, the empty
	// actions block's 8px bottom margin, Bravo's section's 8px top margin
	if got, want := bravo.top-alfa.top, 22+4+8+8; got != want {
		t.Errorf("Bravo %dpx under Alfa, want %d", got, want)
	}
	for _, s := range []string{"Xray", "Yankee"} {
		if hasFrag(&v.d, s) {
			t.Errorf("hidden button %q drawn", s)
		}
	}
	findFrag(t, &v.d, "Zulu")
}
