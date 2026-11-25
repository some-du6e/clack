package richview

import (
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Each whole percent of an upload re-renders the pending message
// (core's filePlaceholder.onProgress, PendingFilesSpinner's
// "Uploading… N%"): the row's selection in its text and the hover on its
// file stay, as Slack's React re-render keeps the DOM; only the tip moves.
// A real change of the message still starts the row over.
func TestUploadPercentKeepsRowState(t *testing.T) {
	mk := func(text string, pct int) rt.Doc {
		f := &rt.File{ID: "F0UPPCT", Name: "game_scarlett_master.exe", Title: "game_scarlett_master.exe", Type: "exe", Mode: "hosted", Pending: true, Percent: pct}
		return doc(para(txt(text)), fileBlock(f))
	}
	hs := &harness{st: newTestStyle(), v: New(mk("hello world", 10), Options{Own: true}), h: &recHandler{}, w: 600}
	hs.frame()
	a, b := findFrag(t, &hs.v.d, "hello"), findFrag(t, &hs.v.d, "world")
	pa := f32.Pt(a.x+1, float32(a.top+a.bot)/2)
	pb := f32.Pt(b.x+b.w-1, float32(b.top+b.bot)/2)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pa}, press(pa, 0),
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pb, Time: 30 * time.Millisecond},
		release(pb, 60*time.Millisecond))
	sel := hs.v.SelectedText()
	if !hs.v.HasSelection() || !strings.Contains(sel, "hello") {
		t.Fatalf("the drag selected %q", sel)
	}
	card := fileCardRect(hs.v)
	pc := f32.Pt(float32(card.Min.X+card.Max.X)/2, float32(card.Min.Y+card.Max.Y)/2)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pc, Time: time.Second})
	hov := hs.v.hover // a pending file may have no hover target: then -1 stays -1

	for _, pct := range []int{11, 40, 98} {
		hs.v.Set(mk("hello world", pct), Options{Own: true})
		hs.frame()
		if got := hs.v.SelectedText(); got != sel {
			t.Errorf("at %d%%: the selection is %q, was %q", pct, got, sel)
		}
		if hs.v.hover != hov {
			t.Errorf("at %d%%: hover %d, was %d", pct, hs.v.hover, hov)
		}
		if len(hs.v.d.pendSpins) != 1 || !strings.Contains(hs.v.d.pendSpins[0].tip, "%") || !strings.Contains(hs.v.d.pendSpins[0].tip, itoa(int64(pct))) {
			t.Errorf("at %d%%: the spinner's tips %+v", pct, hs.v.d.pendSpins)
		}
	}
	hs.v.Set(mk("hello there", 98), Options{Own: true})
	hs.frame()
	if hs.v.HasSelection() {
		t.Errorf("the message's text changed and the selection %q stayed", hs.v.SelectedText())
	}
}
