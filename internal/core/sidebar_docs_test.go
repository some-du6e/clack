package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Synthetic fixtures only (fakeslack Size.FileChannels): canvases' and
// lists' hidden conversations, shaped like the real account's.

func bootFileChannels(t *testing.T) (*fakeslack.Server, *App) {
	t.Helper()
	sz := fakeslack.Small
	sz.FileChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{ExpListsPinToSidebar, ExpProgrammableSidebar, ExpSidebarIA6} {
		a.SetExperimentGroup(g, "") // whatever the fake assigns: start from Slack's default (none)
	}
	return fs, a
}

func fileSecOf(s Snapshot, id string) string {
	for _, sec := range s.Sections {
		for _, l := range [][]string{sec.Channels, sec.All} {
			for _, c := range l {
				if c == id {
					return sec.ID
				}
			}
		}
	}
	return ""
}

func fileConvIn(s Snapshot, id string) (Conv, bool) {
	for _, c := range s.Convs {
		if c.ID == id {
			return c, true
		}
	}
	return Conv{}, false
}

// waitSnap polls the sidebar until cond holds (files.info is async).
func waitFileSnap(t *testing.T, a *App, what string, cond func(Snapshot) bool) Snapshot {
	t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for {
		s := a.Snapshot()
		if cond(s) {
			return s
		}
		if time.Now().After(dl) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// lJBI: with lists_pin_to_sidebar, programmable_sidebar and sidebar_ia6
// all off (O2By GH), no file channel is a sidebar candidate, even in a
// section; none is in the snapshot's conversations, so nothing counts,
// switches to or mentions it.
func TestFileChannelsHiddenByDefault(t *testing.T) {
	_, a := bootFileChannels(t)
	c, ok := a.S.Conv(fakeslack.FileChanSnacks)
	if !ok || !IsFileChannel(c) || !c.IsPrivate {
		t.Fatalf("the fixture's file channel %+v (is_file and is_private, like Slack's)", c)
	}
	s := a.Snapshot()
	for _, id := range []string{fakeslack.FileChanSnacks, fakeslack.FileChanStatus, fakeslack.FileChanKitchen} {
		if sec := fileSecOf(s, id); sec != "" {
			t.Errorf("%s is in section %s with GH off", id, sec)
		}
		if _, ok := fileConvIn(s, id); ok {
			t.Errorf("%s is in the snapshot's conversations", id)
		}
		if k, _ := a.SidebarFileTarget(id); k != "" {
			t.Errorf("%s: click target %q with GH off, want the channel", id, k)
		}
	}
	// the conversation itself still resolves (activity, links), named
	// after its title
	if cv, ok := a.Conv(fakeslack.FileChanSnacks); !ok || !cv.File || cv.FileID != fakeslack.CanvasSnacks || cv.Name != fakeslack.CanvasSnacksTitle {
		t.Errorf("Conv = %+v", cv)
	}
}

// lists_pin_to_sidebar alone: the section's file channels stay (the one
// in no section doesn't); the list is a document row, the canvas a
// private channel row (getSidebarDocumentType wants programmable_sidebar
// for canvases) that opens as a channel.
func TestFileChannelsListsPinToSidebar(t *testing.T) {
	_, a := bootFileChannels(t)
	a.SetExperimentGroup(ExpListsPinToSidebar, "on")
	s := waitFileSnap(t, a, "the list's file loaded", func(s Snapshot) bool {
		c, ok := fileConvIn(s, fakeslack.FileChanKitchen)
		return ok && c.Doc == "list"
	})
	if got := fileSecOf(s, fakeslack.FileChanSnacks); got != fakeslack.FileChanSection {
		t.Errorf("canvas file channel in %q, want %s", got, fakeslack.FileChanSection)
	}
	if got := fileSecOf(s, fakeslack.FileChanKitchen); got != fakeslack.FileChanSection {
		t.Errorf("list file channel in %q, want %s", got, fakeslack.FileChanSection)
	}
	if got := fileSecOf(s, fakeslack.FileChanStatus); got != "" {
		t.Errorf("the file channel no section lists is in %s", got)
	}
	if _, ok := fileConvIn(s, fakeslack.FileChanStatus); ok {
		t.Error("the unlisted file channel is in the snapshot's conversations")
	}
	s = waitFileSnap(t, a, "the canvas's file loaded", func(Snapshot) bool { return a.sidebarFileOf(fakeslack.CanvasSnacks).done })
	cv, _ := fileConvIn(s, fakeslack.FileChanSnacks)
	if cv.Doc != "" || cv.Kind != "private" || cv.NameSpans != nil {
		t.Errorf("canvas row without programmable_sidebar: %+v, want a private channel row", cv)
	}
	if cv.Name != fakeslack.CanvasSnacksTitle {
		t.Errorf("canvas row name %q, want the file's title", cv.Name)
	}
	if k, f := a.SidebarFileTarget(fakeslack.FileChanKitchen); k != "list" || f != fakeslack.ListKitchen {
		t.Errorf("list click target %q %q", k, f)
	}
	if k, _ := a.SidebarFileTarget(fakeslack.FileChanSnacks); k != "" {
		t.Errorf("canvas click target %q, want the channel (neither programmable_sidebar nor sidebar_ia6)", k)
	}
	if ic := a.SidebarDocIcon("list"); ic != "bullet-list" {
		t.Errorf("list icon %q", ic)
	}
	// sidebar_ia6 opens the canvas in the canvas view, the row unchanged
	a.SetExperimentGroup(ExpSidebarIA6, "on")
	if k, f := a.SidebarFileTarget(fakeslack.FileChanSnacks); k != "canvas" || f != fakeslack.CanvasSnacks {
		t.Errorf("canvas click target under sidebar_ia6 %q %q", k, f)
	}
}

// programmable_sidebar: both are document rows; the canvas's title draws
// its :shortcode: as an emoji, the list's icon is "lists".
func TestFileChannelsProgrammableSidebar(t *testing.T) {
	_, a := bootFileChannels(t)
	a.SetExperimentGroup(ExpProgrammableSidebar, "on")
	s := waitFileSnap(t, a, "both files loaded", func(s Snapshot) bool {
		c1, ok1 := fileConvIn(s, fakeslack.FileChanSnacks)
		c2, ok2 := fileConvIn(s, fakeslack.FileChanKitchen)
		return ok1 && ok2 && c1.Doc == "canvas" && c2.Doc == "list"
	})
	cv, _ := fileConvIn(s, fakeslack.FileChanSnacks)
	if len(cv.NameSpans) != 2 || cv.NameSpans[0].Kind != richtext.Emoji || cv.NameSpans[0].ID != "shopping_trolley" ||
		cv.NameSpans[0].URL == "" || cv.NameSpans[1].Text != " Snack run" {
		t.Errorf("canvas name spans %+v, want the emoji then \" Snack run\"", cv.NameSpans)
	}
	if ic := a.SidebarDocIcon("list"); ic != "lists" {
		t.Errorf("list icon under programmable_sidebar %q", ic)
	}
	if ic := a.SidebarDocIcon("canvas"); ic != "canvas" {
		t.Errorf("canvas icon %q", ic)
	}
	if k, f := a.SidebarFileTarget(fakeslack.FileChanSnacks); k != "canvas" || f != fakeslack.CanvasSnacks {
		t.Errorf("canvas click target %q %q", k, f)
	}
}

// the open conversation stays a candidate (lJBI currentlySelectedId),
// GH off included.
func TestFileChannelSelectedStays(t *testing.T) {
	_, a := bootFileChannels(t)
	a.SetSelected(fakeslack.FileChanStatus)
	s := a.Snapshot()
	if fileSecOf(s, fakeslack.FileChanStatus) == "" {
		t.Error("the open file channel left the sidebar")
	}
}

func TestEmojiText(t *testing.T) {
	r := emojiOnly{"tada": true}
	if got := richtext.EmojiText(r, "plain *title*"); got != nil {
		t.Errorf("no emoji: %+v, want nil", got)
	}
	got := richtext.EmojiText(r, "a :tada: b :nope: c:tada:")
	if len(got) != 4 || got[0].Text != "a " || got[1].Kind != richtext.Emoji || got[2].Text != " b :nope: c" || got[3].ID != "tada" {
		t.Errorf("spans %+v", got)
	}
}

type emojiOnly map[string]bool

func (e emojiOnly) Emoji(name string) (string, string, bool) {
	if e[name] {
		return "https://emoji.test/" + name + ".png", "", true
	}
	return "", "", false
}
