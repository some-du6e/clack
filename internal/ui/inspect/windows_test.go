package inspect

import (
	"image"
	"slices"
	"sync"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// TestExperimentNotesFromTheRecorder: each of Clack's windows records its
// frames on its own goroutine (huddleui's windows run tools of their own,
// 2026-11-20). An experiment read on the main window's goroutine while
// another window's frame is being recorded goes to no node of that frame
// (it would race the recording, and isn't that frame's), and one read on
// the recording goroutine goes to the node drawing.
func TestExperimentNotesFromTheRecorder(t *testing.T) {
	uiguard.MarkUI() // this goroutine: the main window's
	t.Cleanup(uiguard.Unmark)
	e := newDragEnv(t)
	drawing, goOn := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.content = func(gtx layout.Context) {
		defer op.Offset(image.Pt(380, 290)).Push(gtx.Ops).Pop()
		m := slackc.InspectAs("Pill")
		if slackc.Inspecting() {
			observeExperiment("read_where_it_draws", "on")
			once.Do(func() {
				close(drawing)
				<-goOn
			})
		}
		m.End(gtx, image.Pt(40, 20))
	}
	e.tool.Toggle()
	done := make(chan struct{})
	go func() { // the other window's frame, on its goroutine
		defer close(done)
		e.frame()
	}()
	<-drawing
	observeExperiment("read_on_the_main_goroutine", "on")
	close(goOn)
	<-done
	var exps []string
	found := false
	for _, n := range e.tool.frames[e.tool.cur].Nodes {
		if n.Name == "Pill" {
			found, exps = true, n.Exps
		}
	}
	if !found {
		t.Fatal("the frame recorded no Pill")
	}
	if !slices.Contains(exps, "read_where_it_draws=on") || slices.Contains(exps, "read_on_the_main_goroutine=on") {
		t.Errorf("the Pill's experiments %q: want the one read where it drew, not the one read on another goroutine meanwhile", exps)
	}
}
