package richview

import (
	"image"
	"strconv"
	"testing"
	"time"
)

// TestCollapseSurvivesUpdates: what the viewer toggled outlives the
// message changing. A reaction, a reply or chat.update hands the row's
// View a rebuilt Doc (View.Set); Slack keeps ContainerBlock's state
// (useState, which its useEffect resets only when is_collapsible or
// default_collapsed change) and the call's expandable (redux, by call
// id).
func TestCollapseSurvivesUpdates(t *testing.T) {
	st := newTestStyle()
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	blocks := func(collapsible bool) string {
		return `[{"type":"container","block_id":"c1","title":{"type":"plain_text","text":"Alpha"},"is_collapsible":` +
			strconv.FormatBool(collapsible) + `,"child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Bravo"}}]},
			{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Standup","date_start":` + start + `,
			"join_url":"https://call.test/j","active_participants":[{"slack_id":"U1"}],"all_participants":[]}}}]`
	}
	v := layoutAt(st, kitDoc(t, blocks(true)), Options{}, 800)
	if !hasFrag(&v.d, "Bravo") || !hasFrag(&v.d, "Join") {
		t.Fatalf("starts open: %q", fragStrings(&v.d))
	}
	// collapse the container and the call card
	h := &recHandler{}
	n := 0
	for i, tg := range v.d.targets {
		if tg.kind == tCollapse {
			v.activate(int16(i), image.Point{}, h)
			n++
		}
	}
	v.relayout(st, 800, 1, 1)
	if n != 2 || hasFrag(&v.d, "Bravo") || hasFrag(&v.d, "Join") {
		t.Fatalf("%d toggles, then %q; want both collapsed", n, fragStrings(&v.d))
	}
	// the message changes: the same blocks in a rebuilt Doc
	v.Set(kitDoc(t, blocks(true)), Options{})
	v.relayout(st, 800, 1, 1)
	if hasFrag(&v.d, "Bravo") || hasFrag(&v.d, "Join") {
		t.Errorf("after a rebuild: %q, want both still collapsed", fragStrings(&v.d))
	}
	// is_collapsible goes off and on again: the container's state resets
	v.Set(kitDoc(t, blocks(false)), Options{})
	v.relayout(st, 800, 1, 1)
	v.Set(kitDoc(t, blocks(true)), Options{})
	v.relayout(st, 800, 1, 1)
	if !hasFrag(&v.d, "Bravo") {
		t.Errorf("after is_collapsible changed: %q, want the container back at its default (open)", fragStrings(&v.d))
	}
}
