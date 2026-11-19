package ui

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// emailsEnv is #email-inbox open (fakeslack's Emails: the Email app's
// plain, HTML and attachments emails).
func emailsEnv(t *testing.T, size image.Point, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Emails = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = size
	if os.Getenv("FILES_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(size.X, size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	e.openChannel(fakeslack.EmailsChannelID)
	n := len(fs.EmailsCases())
	e.until("the #email-inbox history", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.EmailsChannelID) >= n })
	e.settle(800 * time.Millisecond)
	return e
}

// emailCase is #email-inbox's case name.
func (e *perfEnv) emailCase(name string) fakeslack.EmailsCase {
	e.tb.Helper()
	for _, c := range e.fs.EmailsCases() {
		if c.Name == name {
			return c
		}
	}
	e.tb.Fatalf("no email case %q", name)
	return fakeslack.EmailsCase{}
}

// emailMsg is #email-inbox's message for case name.
func (e *perfEnv) emailMsg(name string) core.Message {
	e.tb.Helper()
	c := e.a.data.core
	ec := e.emailCase(name)
	m, ok := c.S.Find(fakeslack.EmailsChannelID, ec.TS)
	if !ok {
		e.tb.Fatalf("%s (%s) isn't loaded", name, ec.TS)
	}
	return c.MessageFromSlack(fakeslack.EmailsChannelID, &m)
}

// emailCard is the email's pillow on screen (its first file box: the
// card), scrolled into view.
func (e *perfEnv) emailCard(t *testing.T, name string) messages.FileRect {
	t.Helper()
	ts := e.emailCase(name).TS
	var fr messages.FileRect
	for i := 0; i < 30; i++ {
		e.frame()
		rs := rectsOf(e.channelFiles(fakeslack.EmailsChannelID), ts)
		if len(rs) == 0 {
			e.scroll(e.listPoint(), -400)
			e.settle(150 * time.Millisecond)
			continue
		}
		fr = rs[0]
		for _, r := range rs { // the pillow is the largest box
			if r.R.Dy()*r.R.Dx() > fr.R.Dy()*fr.R.Dx() {
				fr = r
			}
		}
		if fr.R.Min.Y >= 150 && fr.R.Max.Y <= e.size.Y-180 {
			return fr
		}
		e.scroll(e.listPoint(), float32(fr.R.Min.Y-260))
		e.settle(200 * time.Millisecond)
	}
	t.Fatalf("%s's card stays at %v", name, fr.R)
	return fr
}

// emailActions is the FileActions shown now on message ts's file.
func (e *perfEnv) emailActions(ts string) ([]richview.FileAction, []image.Rectangle) {
	var acts []richview.FileAction
	var rs []image.Rectangle
	if c := e.a.convos[fakeslack.EmailsChannelID+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			for _, r := range l.FileActionRects() {
				if r.TS == ts {
					acts = append(acts, r.Action)
					rs = append(rs, r.R)
				}
			}
		}
	}
	return acts, rs
}

// Karim, 2026-11-17 (#clack 1794883661.462709) and 2026-11-18 ("u cant
// see emails i alr told u like 10 times"): an email the Email app posted
// drew as a plain file card, its type tile cut to "EMAI". With Slack's
// new_email_rendering (on for Hack Club), each #email-inbox email is
// an email file (from, to, the subject) drawn as MessageFile's email
// header, 594px wide and 62 tall; hovered, its FileActions start with
// Expand; Expand reads the email (files.info) and FileEmailV2 shows under
// the header; a click on the header opens the email's FileDetails in the
// secondary view, which reads the whole email and draws its address and
// body links.
func TestEmailInbox(t *testing.T) {
	e := emailsEnv(t, image.Pt(1440, 900), false)
	for _, c := range e.fs.EmailsCases() {
		fs := docFiles(e.emailMsg(c.Name))
		if len(fs) != 1 || !emailFileOf(fs[0]) {
			t.Errorf("%s: files %+v, want one email file", c.Name, fs)
		}
	}
	ec := e.emailCase(fakeslack.EmailPlain)
	fr := e.emailCard(t, fakeslack.EmailPlain)
	if fr.R.Dx() != 594 || fr.R.Dy() != 62 {
		t.Errorf("the email's card %v (%dx%d), want 594x62 (.p-message_file--email's header)", fr.R, fr.R.Dx(), fr.R.Dy())
	}
	mid := image.Pt(fr.R.Min.X+300, fr.R.Min.Y+31)
	e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
	e.settle(200 * time.Millisecond)
	acts, rs := e.emailActions(ec.TS)
	want := []richview.FileAction{richview.FileShowMore, richview.FileShare, richview.FileDetails, richview.FileMore}
	if !sameActs(acts, want) {
		t.Errorf("the email's FileActions %v, want %v (renderEmailExpand first, no Open in new window)", acts, want)
	}
	if len(acts) == 0 || acts[0] != richview.FileShowMore {
		t.Fatalf("the email's FileActions %v start with no Expand", acts)
	}
	n := e.fs.CallCount("files.info")
	e.clickAt(rs[0].Min.Add(rs[0].Max).Div(2), time.Second)
	e.until("the email expanded", 10*time.Second, func() bool { return docFiles(e.emailMsg(fakeslack.EmailPlain))[0].Expanded })
	if e.fs.CallCount("files.info") <= n {
		t.Error("Expand read no files.info (fetchAndExpand)")
	}
	e.settle(300 * time.Millisecond)
	if fr = e.emailCard(t, fakeslack.EmailPlain); fr.R.Dy() < 62+150 {
		t.Errorf("the expanded email is %dpx tall, want the header, its rows and the 150px body", fr.R.Dy())
	}
	e.a.data.core.CollapseFile(fakeslack.EmailsChannelID, ec.TS, ec.FileID)
	e.until("the email collapsed", 5*time.Second, func() bool { return !docFiles(e.emailMsg(fakeslack.EmailPlain))[0].Expanded })
	e.settle(300 * time.Millisecond)

	// the header opens FileDetails in the secondary view
	hc := e.emailCase(fakeslack.EmailHTML)
	fr = e.emailCard(t, fakeslack.EmailHTML)
	n = e.fs.CallCount("files.info")
	e.clickAt(image.Pt(fr.R.Min.X+120, fr.R.Min.Y+24), 3*time.Second)
	e.until("the email's FileDetails", 10*time.Second, func() bool { return emailPaneFileID(e.a) == hc.FileID })
	e.until("the whole email (files.info)", 10*time.Second, func() bool { return e.fs.CallCount("files.info") > n && emailPaneFull(e.a) })
	e.settle(500 * time.Millisecond)
	v := emailPaneView(e.a)
	if v == nil {
		t.Fatal("the pane has no view")
	}
	var urls []string
	for _, l := range v.LinkRects() {
		urls = append(urls, l.URL)
	}
	all := strings.Join(urls, " ")
	for _, u := range []string{"https://example.com/notes", "mailto:team@build.example"} {
		if !strings.Contains(all, u) {
			t.Errorf("FileDetails lacks the link %s; links %q", u, urls)
		}
	}
	if v.PlainLen() < 200 {
		t.Errorf("FileDetails drew %d characters, want the whole email", v.PlainLen())
	}
	// under the email the Shared in section (FileShareContext:
	// files.getShares, the email's #email-inbox message as a card); the
	// flexpane's footer has Share File… and More actions
	e.until("files.getShares for the email", 10*time.Second, func() bool { return e.fs.CallCount("files.getShares") > 0 })
	emailPaneShowShares(e.a)
	e.until("the Shared in section's card", 10*time.Second, func() bool {
		s := frameStrings(e)
		return s["Conversations 1"] && s["Thread in #email-inbox"]
	})
	if s := frameStrings(e); !s["Share File\u2026"] {
		t.Error("the FileDetails footer has no Share File\u2026")
	}
	e.move(f32.Pt(1300, 450)) // over the pane: its origin
	e.settle(200 * time.Millisecond)
	sr, mr := emailPaneFooter(e.a)
	if sr.Dy() != 36 || mr.Dx() != 36 || mr.Dy() != 36 || sr.Max.X+8 != mr.Min.X {
		t.Errorf("the footer's Share File\u2026 %v and More actions %v, want 36px tall, 8px apart, Share filling the row", sr, mr)
	}
	ov := len(e.a.overlays)
	e.clickAt(mr.Min.Add(mr.Max).Div(2), time.Second)
	e.until("More actions' menu", 5*time.Second, func() bool {
		if len(e.a.overlays) <= ov {
			return false
		}
		_, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Menu)
		return ok
	})
	e.a.closeOverlays()
	e.settle(300 * time.Millisecond)
	ov = len(e.a.overlays)
	e.clickAt(sr.Min.Add(sr.Max).Div(2), time.Second)
	e.until("Share File\u2026's dialog", 5*time.Second, func() bool {
		if len(e.a.overlays) <= ov {
			return false
		}
		_, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Modal)
		return ok
	})
	e.a.closeOverlays()
}

// TestEmailShots saves #email-inbox (FILES_SHOTS_DIR, the files-shots
// workflow), light and dark: the three emails collapsed, the plain one
// and the HTML one expanded, and the HTML one's FileDetails.
func TestEmailShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := emailsEnv(t, image.Pt(1440, 1100), dark)
			e.filesShot("emails-" + mode)
			c := e.a.data.core
			for _, name := range []string{fakeslack.EmailPlain, fakeslack.EmailHTML, fakeslack.EmailAttachments} {
				ec := e.emailCase(name)
				c.ExpandFile(fakeslack.EmailsChannelID, ec.TS, ec.FileID)
				e.until(name+" expanded", 10*time.Second, func() bool { return docFiles(e.emailMsg(name))[0].Expanded })
			}
			e.settle(800 * time.Millisecond)
			e.filesShot("emails-expanded-" + mode)
			hc := e.emailCase(fakeslack.EmailHTML)
			openEmailPane(e.a, docFiles(e.emailMsg(fakeslack.EmailHTML))[0], fakeslack.EmailsChannelID, hc.TS)
			e.until("the whole email", 10*time.Second, func() bool { return emailPaneFull(e.a) })
			e.settle(800 * time.Millisecond)
			e.filesShot("emails-details-" + mode)
			e.shutdown()
		})
	}
}
