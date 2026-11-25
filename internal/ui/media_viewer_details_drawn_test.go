package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// TestMediaViewerDetailsDrawn: the #files-gallery image in the media
// viewer, its More actions' "View file details" (Y4Ir, under
// media_viewer_file_details_tab in treatment, as the account is) opens
// the sidebar's Details tab, which draws Slack's FileDetails for the file:
// the tab's label, "Owned by" its uploader, the Shared in card, and the
// footer's Share File…. Clack's viewer had no such item and no sidebar.
func TestMediaViewerDetailsDrawn(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	msg := e.filesMsg("one image")
	f := docFiles(msg)[0]
	lb := mvdOpen(t, e, func() { e.a.ShowOverlay(popups.NewLightbox(e.a, *f.Thumb, msg)) })
	mvdViewDetails(t, e, lb)
	e.until("the Details tab's FileDetails", 15*time.Second, func() bool {
		e.frame()
		s := frameStrings(e)
		return s["Details"] && mvdHas(s, "Owned by") && s["Thread in #files-gallery"] && s["Share File\u2026"]
	})
}

// mvdOpen runs open and returns the viewer it put up.
func mvdOpen(t *testing.T, e *perfEnv, open func()) *popups.Lightbox {
	t.Helper()
	open()
	mvdFrames(e)
	lb := mvdTop(e)
	if lb == nil {
		t.Fatalf("no media viewer open (overlays %v)", e.overlayTypes())
	}
	e.settle(500 * time.Millisecond) // the original decodes
	return lb
}

// mvdTop is the open media viewer, if any.
func mvdTop(e *perfEnv) *popups.Lightbox {
	for i := len(e.a.overlays) - 1; i >= 0; i-- {
		if lb, ok := e.a.overlays[i].(*popups.Lightbox); ok && !lb.Dismissed() {
			return lb
		}
	}
	return nil
}

// mvdViewDetails clicks More actions and chooses its View file details
// (an image's) or View details (a video's).
func mvdViewDetails(t *testing.T, e *perfEnv, lb *popups.Lightbox) {
	t.Helper()
	r := lb.MoreRect()
	if r.Empty() {
		t.Fatal("the viewer drew no More actions")
	}
	e.click(r.Min.Add(r.Size().Div(2)))
	mvdFrames(e)
	m := e.topMenu()
	if m == nil {
		t.Fatalf("More actions opened no menu (overlays %v)", e.overlayTypes())
	}
	for _, it := range m.Items() {
		if (it.Label == "View file details" || it.Label == "View details") && it.Do != nil {
			m.Dismiss()
			it.Do()
			mvdFrames(e)
			return
		}
	}
	t.Fatalf("More actions has no View file details: %v", m.Labels())
}

// mvdHas reports whether a drawn string starts with p.
func mvdHas(s map[string]bool, p string) bool {
	for k := range s {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

func mvdFrames(e *perfEnv) {
	for range 4 {
		e.frame()
	}
}
