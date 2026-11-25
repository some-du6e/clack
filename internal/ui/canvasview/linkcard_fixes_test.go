package canvasview

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// cardRig opens the card canvas in a pointer rig (a real input router) and
// frames until cond holds or d passes.
func cardRig(t *testing.T, h *testHost, act Actions, d time.Duration, cond func(v *View) bool) (*pointerRig, *View) {
	t.Helper()
	act.FullView, act.SideView, act.Close = func() {}, func() {}, func() {}
	v := New(h, cardCanvasID, Full, act)
	r := &pointerRig{t: t, v: v, sz: image.Pt(1100, 900)}
	for end := time.Now().Add(d); time.Now().Before(end); {
		r.frame()
		if embedBlk(v) != nil && cond(v) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return r, v
}

// wakes reports whether the view asked for another frame after a settled
// one (an invalidation, or input events waiting).
func wakes(r *pointerRig) bool {
	r.router.WakeupTime() // the latch, from before
	r.frame()
	_, w := r.router.WakeupTime()
	return w
}

// TestRenderLinkCardPublicThumbRests: a public link card with a thumbnail
// under a resting pointer asks for no more frames (its text view used to
// be made anew every frame: a new pointer tag, an Enter, a frame), and a
// link in its text opens (the text is the attachment's Mrkdwn).
func TestRenderLinkCardPublicThumbRests(t *testing.T) {
	m := webUnfurl(cardTS)
	a := &m.Attachments[0]
	a.Text = "Read <https://example.com/docs|the docs> first"
	a.ThumbURL, a.ThumbWidth, a.ThumbHeight = "https://example.com/thumb.png", 400, 300
	h, _, _ := cardEnv(t, false, m, cardTS, nil)
	var opened []string
	r, v := cardRig(t, h, Actions{OpenURL: func(u string) { opened = append(opened, u) }}, 10*time.Second, func(v *View) bool {
		k, _, _ := cardState(v)
		return k == "public" && publicText(v) != nil
	})
	if k, _, _ := cardState(v); k != "public" || publicText(v) == nil {
		t.Fatalf("the card is %q, want public with text", k)
	}
	tv := publicText(v)
	var at image.Point
	full, _, ok := r.router.AreaBounds(tv)
	if ok {
		for _, lr := range tv.LinkRects() {
			if lr.Kind == richview.HitLink {
				at = full.Min.Add(lr.R.Min.Add(lr.R.Max).Div(2))
			}
		}
	}
	if at == (image.Point{}) {
		t.Error("the public card's text has no link")
		if ok {
			at = full.Min.Add(image.Pt(5, 5))
		}
	}
	r.queue(mouse(pointer.Move, float32(at.X), float32(at.Y)))
	for i := 0; i < 5; i++ {
		r.frame()
	}
	if wakes(r) {
		t.Error("with the pointer resting on the card's text the view keeps asking for frames")
	}
	r.click(float32(at.X), float32(at.Y))
	if len(opened) != 1 || opened[0] != "https://example.com/docs" {
		t.Errorf("the text's link opened %q, want [https://example.com/docs]", opened)
	}
}

// TestRenderLinkCardSkeletonStops: a card left loading (an attachment
// that never unfurls) shimmers for Card's wait, then holds still instead
// of asking for a frame every frame.
func TestRenderLinkCardSkeletonStops(t *testing.T) {
	defer setCardTimeout(300 * time.Millisecond)()
	h, _, _ := cardEnv(t, false, appUnfurl(cardTS, false), cardTS, nil)
	r, v := cardRig(t, h, Actions{}, 10*time.Second, func(v *View) bool {
		k, _, _ := cardState(v)
		return k == "loading"
	})
	if k, _, _ := cardState(v); k != "loading" {
		t.Fatalf("the card is %q, want loading", k)
	}
	time.Sleep(500 * time.Millisecond)
	r.frame()
	if wakes(r) {
		t.Error("the skeleton keeps shimmering past Card's wait (a frame every frame)")
	}
}

// TestRenderLinkCardWorkObjectFill: a work object's card in a canvas has
// no width cap and no border of its own (ExternalWorkObjectEntity with
// source CANVAS): it is the column's width, not 480.
func TestRenderLinkCardWorkObjectFill(t *testing.T) {
	const raw = `{"type":"message","ts":"1762500000.000100","document_unfurl":{"is_unfurled":true},"attachments":[
		{"id":1,"from_url":"https://tracker.example.com/T-1","original_url":"https://tracker.example.com/T-1","is_app_unfurl":true,
		 "work_object_entity":{"work_object_type":"external","entity_id":"W:A1:T-1","app_id":"A1","display_type":"Task",
		  "layouts":{"expanded":{"title":{"text":"Fix it"}}}}}]}`
	var m slack.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	_, _, open := cardEnvURL(t, false, "https://tracker.example.com/T-1", &m, cardTS, nil)
	v, s := open(Full, image.Pt(1100, 900))
	softWait(s, v, 8*time.Second, func() bool { return bodyWidth(v) > 0 })
	_, size, _ := cardState(v)
	if w := bodyWidth(v); w != size.X || w <= 480 {
		t.Errorf("the work object card is %d wide in a %d card, want the card's width", w, size.X)
	}
	if !strings.Contains(embedText(v), "Fix it") {
		t.Errorf("the card %q has no title", embedText(v))
	}
}
