package canvasview

// The link-url-box-3 review's blockers: a card whose lookup fails settles
// in Slack's end state instead of asking again every frame, and stops
// asking for frames once it has.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// badExportID is a canvas whose url_private answers a page that isn't
// Quip's export.
const badExportID = "F0BADEXPRT01"

// frameFor frames the rig for d.
func frameFor(r *pointerRig, d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); {
		r.frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRenderLinkCardCanvasPreviewFails: a canvas card whose export can't
// be read (an HTTP 500, a page that isn't the export) draws the canvas
// without a preview, as Slack's thumbnail with an error does. files.info
// and the export are asked for once: they used to be asked again on every
// frame (ForgetCanvas after the failure, and the card reads the preview
// each frame), a files.info and an export GET a few times a second, with
// the skeleton drawing at frame rate.
func TestRenderLinkCardCanvasPreviewFails(t *testing.T) {
	for _, c := range []struct {
		name, id string
		prep     func(fs *fakeslack.Server)
	}{
		{"export 500", fakeslack.CanvasChannel, func(fs *fakeslack.Server) {
			fs.SetFault("files-pri/"+fakeslack.CanvasChannel, fakeslack.Fault{Status: 500})
		}},
		{"not a canvas", badExportID, func(fs *fakeslack.Server) {
			fs.AddCanvas(fakeslack.FakeCanvas{ID: badExportID, Title: "Not an export", HTML: "<html><body><p>Not an export</p></body></html>"})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := newEnv(t, false)
			link := "https://fake.slack.com/docs/" + fakeslack.TeamID + "/" + c.id
			cardEnvOn(t, h, fs, link, bareContainer(), cardTS, c.prep)
			r, v := cardRig(t, h, Actions{}, 15*time.Second, func(v *View) bool {
				k, _, _ := cardState(v)
				return k == "canvas" && previewSettled(v)
			})
			if k, _, _ := cardState(v); k != "canvas" || !previewSettled(v) {
				t.Fatalf("the card is %q (preview settled %v), want the canvas without a preview; export GETs %d, files.info %d",
					k, previewSettled(v), fs.CallCount("files-pri/"+c.id), fs.CallCount("files.info"))
			}
			infos := fs.CallCount("files.info")
			frameFor(r, 3*time.Second)
			if n := fs.CallCount("files-pri/" + c.id); n != 1 {
				t.Errorf("the canvas's export was fetched %d times, want once", n)
			}
			if n := fs.CallCount("files.info"); n != infos {
				t.Errorf("files.info was asked %d more times after the card settled", n-infos)
			}
			if wakes(r) {
				t.Error("the settled card keeps asking for frames")
			}
		})
	}
}

// TestRenderLinkCardMemberSettles: a member card ends. With
// users.profile.getSections failing it draws the member without the
// location (Clack's profile pane falls back the same way); for a member
// users.info doesn't know it draws nothing, as UserProfileUnfurl does
// without a member object. Either way it stops asking for frames: the
// skeleton used to stay, laying out and painting the canvas every frame.
func TestRenderLinkCardMemberSettles(t *testing.T) {
	t.Run("sections fail", func(t *testing.T) {
		h, fs := newEnv(t, false)
		user := fs.UserIDs()[1]
		cardEnvOn(t, h, fs, "https://fake.slack.com/team/"+user, bareContainer(), cardTS, func(fs *fakeslack.Server) {
			fs.SetFault("users.profile.getSections", fakeslack.Fault{Error: "internal_error"})
		})
		r, v := cardRig(t, h, Actions{}, 10*time.Second, func(v *View) bool {
			k, _, _ := cardState(v)
			return k == "member"
		})
		if k, _, _ := cardState(v); k != "member" {
			t.Fatalf("the card is %q with getSections failing (asked %d times), want the member", k, fs.CallCount("users.profile.getSections"))
		}
		if got := memberShown(v); got != user {
			t.Errorf("the card shows member %q, want %q", got, user)
		}
		frameFor(r, time.Second)
		if wakes(r) {
			t.Error("the member card keeps asking for frames")
		}
	})
	t.Run("unknown member", func(t *testing.T) {
		h, fs := newEnv(t, false)
		cardEnvOn(t, h, fs, "https://fake.slack.com/team/U0NOSUCHUSR1", bareContainer(), cardTS, nil)
		r, v := cardRig(t, h, Actions{}, 10*time.Second, func(v *View) bool { return fs.CallCount("users.info") > 0 })
		frameFor(r, 2*time.Second)
		if fs.CallCount("users.info") == 0 {
			t.Error("users.info wasn't asked for the member")
		}
		if k, _, _ := cardState(v); k != "member-none" {
			t.Errorf("the card for a member users.info doesn't know is %q, want nothing in it (member-none)", k)
		}
		if wakes(r) {
			t.Error("the unknown member's card keeps asking for frames")
		}
	})
}
