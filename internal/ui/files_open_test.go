package ui

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// FileOpenSubscriber in the shell: canvases and lists opened in the main
// view join the Files sidebar's Recently viewed (newest first), channel
// routes don't, and the list is kept in ui.json under the team, from
// where the next start restores it.
func TestFilesRecentlyViewedRoutes(t *testing.T) {
	ca := core.New(slack.New(slack.Creds{Token: "xoxc-test", Team: "T0TEST1"}), store.New(""))
	a := &App{}
	a.data = &backend{core: ca, app: a}
	a.attachOpenFiles(ca)
	a.fileOpenedRoute("docs/F0CANVAS0001")
	a.fileOpenedRoute("c/C1")
	a.fileOpenedRoute("lists/F0LIST000001")
	a.fileOpenedSide("F0CANVAS0002")
	if got := strings.Join(ca.RecentlyViewedFileIDs(), " "); got != "F0CANVAS0002 F0LIST000001 F0CANVAS0001" {
		t.Fatalf("Recently viewed %q", got)
	}
	a.drain() // the saver's posts
	p, ok := a.prefs.OpenFiles["T0TEST1"]
	if !ok || strings.Join(p.Display, " ") != "F0CANVAS0002 F0LIST000001 F0CANVAS0001" || len(p.Cache) != 3 {
		t.Fatalf("ui.json open_files %+v", a.prefs.OpenFiles)
	}
	// a new start: the saved list comes back
	cb := core.New(slack.New(slack.Creds{Token: "xoxc-test", Team: "T0TEST1"}), store.New(""))
	b := &App{prefs: a.prefs}
	b.data = &backend{core: cb, app: b}
	b.attachOpenFiles(cb)
	if got := strings.Join(cb.RecentlyViewedFileIDs(), " "); got != "F0CANVAS0002 F0LIST000001 F0CANVAS0001" {
		t.Errorf("restored %q", got)
	}
	// removing the last one clears the team's entry
	for _, id := range []string{"F0CANVAS0001", "F0CANVAS0002", "F0LIST000001"} {
		cb.RemoveOpenFile(id)
	}
	b.drain()
	if _, ok := b.prefs.OpenFiles["T0TEST1"]; ok {
		t.Errorf("an empty list stayed in ui.json: %+v", b.prefs.OpenFiles)
	}
}
