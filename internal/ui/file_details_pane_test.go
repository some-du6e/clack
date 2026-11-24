package ui

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// fdPaneOf is the pane FileDetailsPane built (the tab's: header hidden).
func fdPaneOf(t *testing.T, a *App, f *richtext.File) *fileDetailsPane {
	t.Helper()
	fp := a.FileDetailsPane(f, fakeslack.FilesChannelID)
	p, ok := fp.(*fileDetailsPane)
	if !ok || p == nil {
		t.Fatalf("FileDetailsPane(%s) = %T, want the file's FileDetails", f.ID, fp)
	}
	if fp.FileID() != f.ID {
		t.Errorf("FileID() = %q, want %q", fp.FileID(), f.ID)
	}
	return p
}

// fdShowAsTab draws p where the test can see it: as the secondary view's
// body, its header still hidden (the media viewer's Details tab draws it
// the same way, in its sidebar).
func fdShowAsTab(e *perfEnv, p *fileDetailsPane) {
	e.a.secondary = secondaryState{kind: secFileDetails, channel: fakeslack.FilesChannelID, width: e.a.secondary.width}
	e.a.secondary.view = p
	e.a.Invalidate()
	e.frame()
}

// fdDetailsForm is the files.info FileDetails sent for id after the n-th.
func fdDetailsForm(e *perfEnv, id string, n int) (map[string][]string, bool) {
	fs := e.fs.FilesForms("files.info")
	for _, f := range fs[min(n, len(fs)):] {
		if f.Get("file") == id && f.Get("skip_shares") != "" {
			return f, true
		}
	}
	return nil, false
}

// The Details tab's FileDetails (eg5B with hideHeader) for #files-gallery's
// landscape image and its video: files.info with fetchFileById's args on
// creation; the name, "Owned by" the uploader, "Created on" the upload's
// date (no year only within shouldExcludeYear), the image at thumb_480's
// size fitted to the content width, the Shared in section's card
// (files.getShares) and the footer's Share File…, Download and More
// actions; no "File" header.
func TestFileDetailsPane(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	c := e.a.data.core
	for _, tc := range []struct {
		name, fixture string
		shares        int
	}{
		{"image", "one image", 1},
		{"video", "video", 3}, // the video case, the image and video case, the thread's reply
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := docFiles(e.filesMsg(tc.fixture))[0]
			n := len(e.fs.FilesForms("files.info"))
			p := fdPaneOf(t, e.a, f)
			e.until("files.info for FileDetails", 10*time.Second, func() bool { e.frame(); return p.got })
			form, ok := fdDetailsForm(e, f.ID, n)
			if !ok {
				t.Fatalf("no files.info for %s with skip_shares; sent %v", f.ID, e.fs.FilesForms("files.info")[n:])
			}
			for k, want := range map[string]string{"file": f.ID, "page": "1", "count": "500", "truncate": "true", "skip_shares": "true"} {
				if got := form[k]; len(got) != 1 || got[0] != want {
					t.Errorf("files.info %s=%v, want %q (fetchFileById: file-subscription.fetchFileInfo)", k, got, want)
				}
			}
			if _, has := form["public_shared"]; has {
				t.Errorf("files.info sent public_shared %v, which FileDetails leaves undefined", form["public_shared"])
			}

			fdShowAsTab(e, p)
			e.until("the Shared in section's card and the owner's name", 10*time.Second, func() bool {
				s := frameStrings(e)
				return s[sharesHeading(tc.shares)] && s["Thread in #files-gallery"] && p.ownerName != ""
			})
			s := frameStrings(e)
			d := p.drawn
			if d.state != fdFile {
				t.Fatalf("state %v, want the file's details", d.state)
			}
			if d.header != "" {
				t.Errorf("the tab's FileDetails drew the header %q (hideHeader)", d.header)
			}
			if d.name != f.Name || !s[f.Name] {
				t.Errorf("name %q (drawn %v), want %q", d.name, s[f.Name], f.Name)
			}
			u, _ := c.User(f.User)
			owner := firstNonEmptyStr(u.Name, u.RealName, u.Handle)
			if owner == "" || d.owner != "Owned by "+owner || !s[owner] {
				t.Errorf("owner row %q (name drawn %v), want \"Owned by %s\"", d.owner, s[owner], owner)
			}
			up := time.Unix(f.Created, 0)
			now := time.Now()
			want := "Created on " + i18n.CalendarDate(i18n.Locale(), up, i18n.CalendarOpts{ExcludeYear: i18n.ShouldExcludeYear(up, now)})
			if f.Created == 0 || d.created != want || !s[want] {
				t.Errorf("date row %q, want %q (toCalendarDateOrNamedDay, shouldExcludeYear)", d.created, want)
			}
			if !d.share || !d.download || !d.more || !s["Share File…"] {
				t.Errorf("footer share %v download %v more %v (Share File… drawn %v), want all three", d.share, d.download, d.more, s["Share File…"])
			}
			if p.shareR.Dy() != 36 || p.downloadR.Dx() != 36 || p.moreR.Dx() != 36 || p.shareR.Max.X+8 != p.downloadR.Min.X || p.downloadR.Max.X+8 != p.moreR.Min.X {
				t.Errorf("footer Share %v, Download %v, More %v: want 36px tall, 8px apart, Share filling the row", p.shareR, p.downloadR, p.moreR)
			}
			if tc.name == "image" {
				// thumb_480 of the 1600x900 original is 480x270, fitted to
				// the pane's content (16px in each side)
				cw := e.a.secondary.drawnW - 32
				if d.previewW != min(480, cw) || d.previewH < 270*min(480, cw)/480 {
					t.Errorf("preview %dx%d in a %dpx content box, want thumb_480's 480x270 fitted to it", d.previewW, d.previewH, cw)
				}
			}
		})
	}
}

// sharesHeading is FileShareContext's divider for n conversations.
func sharesHeading(n int) string {
	return i18n.NS("file-details").T("Conversations {count}", "count", n)
}

// The gate's fallback (toFileDetails): the secondary view shows the file's
// FileDetails with its header, titled "File", and opening it again is a
// no-op.
func TestOpenFileDetails(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	f := docFiles(e.filesMsg("one image"))[0]
	e.a.OpenFileDetails(f, fakeslack.FilesChannelID)
	p, ok := e.a.secondary.view.(*fileDetailsPane)
	if !ok || e.a.secondary.kind != secFileDetails || p.FileID() != f.ID {
		t.Fatalf("secondary %v (%T), want FileDetails for %s", secKindName(e.a.secondary.kind), e.a.secondary.view, f.ID)
	}
	e.until("FileDetails drawn", 10*time.Second, func() bool { return frameStrings(e)[f.Name] })
	if p.drawn.header != "File" || !frameStrings(e)["File"] {
		t.Errorf("header %q, want \"File\" (eg5B titleContent)", p.drawn.header)
	}
	e.a.OpenFileDetails(f, fakeslack.FilesChannelID)
	if e.a.secondary.view != p {
		t.Error("opening the same file's FileDetails again built another pane")
	}
}

// renderFileDetails' states: an unknown file loading draws nothing, then
// FileDetailsError if the read failed; files.info's stubs draw the
// tombstone, the not-found line, or the preview alone.
func TestFileDetailsStates(t *testing.T) {
	bare := &richtext.File{ID: "F0BARE00001"}
	p := &fileDetailsPane{id: bare.ID, msg: bare, fetching: true}
	if s := p.state(); s != fdNothing {
		t.Errorf("an unknown file loading: state %v, want nothing", s)
	}
	p.fetching = false
	if s := p.state(); s != fdError {
		t.Errorf("an unknown file not read: state %v, want the error", s)
	}
	known := &richtext.File{ID: "F0KNOWN0001", Name: "a.png", IsImage: true}
	p = &fileDetailsPane{id: known.ID, msg: known, fetching: true}
	if s := p.state(); s != fdFile {
		t.Errorf("a known file loading: state %v, want its details (isFileFetching is false for it)", s)
	}
	for mode, want := range map[string]fdState{"tombstone": fdTombstone, "not_found": fdNotFound, "access_denied": fdPreviewOnly,
		"canvas_slack_connect_blocked": fdPreviewOnly} {
		info := core.FileDetailsInfo{Mode: mode}
		p := &fileDetailsPane{id: known.ID, msg: known, info: info, got: true, full: info.RichFile(known.ID)}
		if s := p.state(); s != want {
			t.Errorf("%s: state %v, want %v", mode, s, want)
		}
		if fdShowFile(p.file()) {
			t.Errorf("%s: shouldShowFile, want false (no Share or Download)", mode)
		}
	}
	if !fdShowFile(known) {
		t.Error("a file: shouldShowFile false")
	}
}

// maaT Name: a clip's "Audio clip" / "Video clip", the title, else the
// name, else "Untitled".
func TestFileDetailsName(t *testing.T) {
	for _, c := range []struct {
		f    richtext.File
		want string
	}{
		{richtext.File{Name: "a.mp4", Subtype: "slack_video"}, "Video clip"},
		{richtext.File{Name: "a.webm", Subtype: "slack_audio"}, "Audio clip"},
		{richtext.File{Name: "a.webm", Subtype: "slack_name_pronunciation"}, "Audio clip"},
		{richtext.File{Name: "IMG_1.png", Title: "Our cat"}, "Our cat"},
		{richtext.File{Name: "IMG_1.png"}, "IMG_1.png"},
		{richtext.File{}, "Untitled"},
	} {
		if got := fileDetailsName(&c.f); got != c.want {
			t.Errorf("fileDetailsName(%+v) = %q, want %q", c.f, got, c.want)
		}
	}
}

// renderReferencesMetadata's date row: "Created today" / "Created
// yesterday" (lower case), else "Created on {date}" with the year only
// outside shouldExcludeYear.
func TestFileCreatedText(t *testing.T) {
	now := time.Date(2026, 11, 24, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-2 * time.Hour), "Created today"},
		{now.AddDate(0, 0, -1), "Created yesterday"},
		{now.AddDate(0, 0, 1), "Created on tomorrow"},
		{time.Date(2026, 11, 3, 9, 0, 0, 0, time.Local), "Created on November 3rd"},
		{time.Date(2025, 12, 30, 9, 0, 0, 0, time.Local), "Created on December 30th, 2025"},
		{time.Date(2025, 11, 8, 9, 0, 0, 0, time.Local), "Created on November 8th, 2025"},
	} {
		if got := fileCreatedText(i18n.EnUS, c.t, now); got != c.want {
			t.Errorf("fileCreatedText(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	jan := time.Date(2027, 1, 10, 12, 0, 0, 0, time.Local)
	if got := fileCreatedText(i18n.EnUS, time.Date(2026, 12, 30, 9, 0, 0, 0, time.Local), jan); got != "Created on December 30th" {
		t.Errorf("last year within 31 days: %q, want no year", got)
	}
}
