package ui

import (
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The media viewer's Details tab end to end, in the app with
// #files-gallery (media_viewer_file_details_tab in treatment, as the
// account is): More actions' "View file details" (Y4Ir) opens the
// sidebar on Details (bDav aD), whose panel is the app's FileDetails for
// the item's file (files.info, the Shared in card); a file opened outside
// a message (the Files browser's openFileViewer) shows "Close details",
// which closes only the sidebar; a message's file, which has a channel and
// a thread, "Close" (no Thread tab yet: Details alone); Escape through the
// shell closes the viewer with the sidebar open. With the gate off there is
// no tab, and the item opens FileDetails in the secondary view with the
// viewer still up.
func TestMediaViewerDetailsInApp(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	msg := e.filesMsg("one image")
	f := docFiles(msg)[0]

	t.Run("outside a message", func(t *testing.T) {
		lb := mvdOpen(t, e, func() { popups.OpenFileViewer(e.a, f, *f.Thumb, "") })
		mvdViewDetails(t, e, lb)
		mvdCheckDetails(t, e, lb, f.ID)
		if got := lb.CloseLabel(); got != "Close details" {
			t.Errorf("close label %q, want \"Close details\" (Details selected, no thread)", got)
		}
		_, cl := lb.SidebarParts()
		e.click(cl.Min.Add(cl.Size().Div(2)))
		mvdFrames(e)
		if lb.Dismissed() {
			t.Fatal("Close details closed the viewer")
		}
		if ids, _ := lb.SidebarTabs(); len(ids) != 0 {
			t.Errorf("after Close details the sidebar shows %v, want it closed", ids)
		}
		lb.Dismiss()
		mvdFrames(e)
	})

	t.Run("a message's file", func(t *testing.T) {
		lb := mvdOpen(t, e, func() { e.a.ShowOverlay(popups.NewLightbox(e.a, *f.Thumb, msg)) })
		mvdViewDetails(t, e, lb)
		mvdCheckDetails(t, e, lb, f.ID)
		if got := lb.CloseLabel(); got != "Close" {
			t.Errorf("close label %q, want \"Close\" (the viewer has the message's channel and thread)", got)
		}
		e.press(key.NameEscape, 0)
		if !lb.Dismissed() {
			t.Error("Escape left the viewer open with the sidebar showing")
		}
	})

	t.Run("gate off", func(t *testing.T) {
		c := e.a.data.core
		c.SetExperimentGroup(core.ExpMediaViewerFileDetailsTab, "control")
		t.Cleanup(func() { c.ClearExperimentGroup(core.ExpMediaViewerFileDetailsTab) })
		lb := mvdOpen(t, e, func() { popups.OpenFileViewer(e.a, f, *f.Thumb, "") })
		mvdViewDetails(t, e, lb)
		if ids, _ := lb.SidebarTabs(); len(ids) != 0 {
			t.Errorf("gate off: the sidebar shows %v, want no tab", ids)
		}
		if lb.Dismissed() {
			t.Error("gate off: View file details closed the viewer (an image's stays: Y4Ir navigates only)")
		}
		e.until("FileDetails in the secondary view", 10*time.Second, func() bool {
			p, ok := e.a.secondary.view.(*fileDetailsPane)
			return ok && e.a.secondary.kind == secFileDetails && p.FileID() == f.ID
		})
		lb.Dismiss()
		mvdFrames(e)
	})
}

// TestMediaViewerDetailsShots: the viewer with the Details tab open on the
// #files-gallery image, light and dark, at Slack's three sidebar caps
// (1500, 1440 and 1280 wide windows: 400, 360 and 300), and the video
// where the runner plays video (FILES_SHOTS_DIR only).
func TestMediaViewerDetailsShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, w := range []int{1500, 1440, 1280} {
			e := filesEnv(t, image.Pt(w, 900), dark)
			f := docFiles(e.filesMsg("one image"))[0]
			lb := mvdOpen(t, e, func() { popups.OpenFileViewer(e.a, f, *f.Thumb, "") })
			mvdViewDetails(t, e, lb)
			mvdCheckDetails(t, e, lb, f.ID)
			e.filesShot("viewer-details-image-" + itoa(w) + "-" + mode)
			lb.Dismiss()
			mvdFrames(e)
			if w == 1440 {
				v := docFiles(e.filesMsg("video"))[0]
				var vl *popups.Lightbox
				if popups.OpenFileViewer(e.a, v, richtext.Image{}, "") {
					vl = mvdTop(e)
				}
				if vl != nil {
					mvdViewDetails(t, e, vl)
					mvdCheckDetails(t, e, vl, v.ID)
					e.filesShot("viewer-details-video-" + mode)
					vl.Dismiss()
					mvdFrames(e)
				} else {
					t.Logf("%s: no video players here: no video shot", mode)
				}
			}
			e.shutdown()
		}
	}
}

// mvdCheckDetails: the sidebar shows Details alone, its pane is the app's
// FileDetails for id, and that FileDetails has drawn the file's owner and
// its Shared in card.
func mvdCheckDetails(t *testing.T, e *perfEnv, lb *popups.Lightbox, id string) {
	t.Helper()
	ids, active := lb.SidebarTabs()
	if len(ids) != 1 || ids[0] != "details" || active != "details" {
		t.Fatalf("sidebar tabs %v (showing %q), want Details alone", ids, active)
	}
	if got := lb.DetailsFileID(); got != id {
		t.Fatalf("the Details tab shows %q, want %q", got, id)
	}
	side, media := lb.SidebarRect()
	want := 400
	switch w := e.size.X; {
	case w <= 1280:
		want = 300
	case w <= 1440:
		want = 360
	}
	if side.Dx() != want || media.Max.X > side.Min.X {
		t.Errorf("sidebar %v beside the media %v in a %d px window, want %d px wide at the right", side, media, e.size.X, want)
	}
	e.until("the FileDetails' owner row and Shared in card", 15*time.Second, func() bool {
		e.frame()
		s := frameStrings(e)
		return mvdHas(s, "Owned by") && s["Thread in #files-gallery"]
	})
}
