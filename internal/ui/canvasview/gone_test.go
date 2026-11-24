package canvasview

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
)

// QuipDeletedState: a canvas files.info refuses draws Slack's state for
// it in every mode, never Clack's "Try again": deleted reads one of the
// six titles (picked once) over "It’s been deleted by the owner.", not
// found "This canvas was not found.", not visible "You don’t have access
// to this canvas." with Request Access, whose click asks for access
// (reason DirectRequestAccessModal, request_type access) and turns into
// "Request Sent"; a files.info file in tombstone mode is deleted. Shots:
// canvas-gone-<state>-<light|dark>.
func TestRenderGoneStates(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs := newEnv(t, dark)
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		fs.SetFileGone("F0CVGONE01", "file_deleted", nil)
		fs.SetFileGone("F0CVGONE02", "file_not_found", nil)
		fs.SetFileGone("F0CVGONE03", "not_visible", map[string]any{"filetype": "quip", "title": "Secret plan"})
		fs.SetFileInfoFile("F0CVGONE04", map[string]any{"id": "F0CVGONE04", "mode": "tombstone"})
		cvPick(2)
		t.Cleanup(func() { cvPick(0) })
		for _, c := range []struct {
			id, name, title, button string
			mode                    Mode
		}{
			{"F0CVGONE01", "deleted", "This canvas was swept away", "", Tab},
			{"F0CVGONE01", "deleted-side", "This canvas was swept away", "", Side},
			{"F0CVGONE02", "not-found", "This canvas was not found.", "", Tab},
			{"F0CVGONE03", "not-visible", "You don’t have access to this canvas.", "Request Access", Peek},
			{"F0CVGONE04", "tombstone", "This canvas was swept away", "", Full},
		} {
			v := New(h, c.id, c.mode, Actions{FullView: func() {}, SideView: func() {}, Close: func() {}})
			s := newShooter(t, image.Pt(900, 640))
			settle(t, s, v, c.name+"'s state", func() bool { return cvGone(v) })
			for i := 0; i < 20; i++ { // the emoji decodes
				s.frame(func(gtx layout.Context) { v.Layout(gtx) })
				time.Sleep(10 * time.Millisecond)
			}
			title, btn := cvTitle(v)
			if title != c.title || btn != c.button {
				t.Errorf("%s: title %q button %q, want %q %q", c.name, title, btn, c.title, c.button)
			}
			s.shot("canvas-gone-"+c.name+"-"+suffix, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
			if c.button == "" {
				continue
			}
			cvRequest(v)
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			settle(t, s, v, "the access request", func() bool { return len(fs.GoneAccessRequests()) > 0 })
			got := fs.GoneAccessRequests()[0]
			if got.Get("file_id") != c.id || got.Get("reason") != "DirectRequestAccessModal" || got.Get("request_type") != "access" {
				t.Errorf("%s: files.requestPermission %v", c.name, got)
			}
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			if _, btn := cvTitle(v); btn != "Request Sent" {
				t.Errorf("%s: button after the click %q, want Request Sent", c.name, btn)
			}
		}
		// the deleted title is picked once for a load's answer, not per frame
		cvPick(4)
		v := New(h, "F0CVGONE01", Tab, Actions{})
		s := newShooter(t, image.Pt(900, 640))
		settle(t, s, v, "the deleted state", func() bool { return cvGone(v) })
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		cvPick(1)
		for i := 0; i < 5; i++ {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		}
		if title, _ := cvTitle(v); title != "This canvas was retired" {
			t.Errorf("the deleted title changed between frames: %q", title)
		}
	}
}
