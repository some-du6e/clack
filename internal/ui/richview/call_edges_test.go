package richview

import (
	"strconv"
	"testing"
	"time"
)

// TestCallCardEdges: an external participant without an avatar_url shows
// Slack's ava_0002 (Facepile's renderExternalAvatarFallback, 3px
// corners); a missed DM call with a join_url and its facepile hidden has
// no footer band (renderCallFooter returns null), so the card closes
// under its header.
func TestCallCardEdges(t *testing.T) {
	defer setDefaultAvatar("local://ava_0002")()
	st := newTestStyle()
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	d := kitDoc(t, `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Design review","date_start":`+start+`,
		"join_url":"https://call.test/j","active_participants":[{"slack_id":"U1"},{"external_id":"g1","display_name":"Guest"}],"all_participants":[]}}}]`)
	v := layoutAt(st, d, Options{}, 800)
	if ims := imgsWithURL(&v.d, "local://ava_0002"); len(ims) != 1 || ims[0].r.Dx() != 36 || ims[0].radii[0] != 3 {
		t.Errorf("guest face %+v, want ava_0002 at 36px with 3px corners", ims)
	}
	// the viewer (U0) missed U9's DM call
	d = kitDoc(t, `[{"type":"call","call_id":"R2","call":{"v1":{"id":"R2","is_dm_call":true,"channels":["D1"],"created_by":"U9","was_missed":true,
		"date_start":1700000000,"date_end":1700000060,"join_url":"https://call.test/j2","all_participants":[{"slack_id":"U9"}]}}}]`)
	v = layoutAt(st, d, Options{}, 800)
	p := st.Palette
	body := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.04), p.Panel)
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.fill == body {
			t.Errorf("a footer band %v on a missed DM call with its facepile hidden", b.r)
		}
	}
	if !hasFrag(&v.d, "missed") {
		t.Errorf("no status: %q", fragStrings(&v.d))
	}
}
