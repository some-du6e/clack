package listview

import (
	"context"
	"image"
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// BaseListErrorState: a list files.info refuses draws Slack's state, never
// Clack's "Try again"; it asks files.canUndelete on mount, and a deleted
// list it may restore reads "Was that a mistake? Restore this list." whose
// link opens UndeleteFileDialog (Actions.Restore) with the answer's file;
// a list Slack can't find reads "This list was not found.". Shots:
// list-gone-<state>-<light|dark>.
func TestRenderListGoneStates(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs := newEnv(t, dark)
		fs.SetFileGone("F0LVGONE01", "file_deleted", nil)
		fs.SetCanUndelete("F0LVGONE01", true, "Groceries", 1763900000)
		fs.SetFileGone("F0LVGONE02", "file_not_found", nil)
		fs.SetFileGone("F0LVGONE03", "not_visible", map[string]any{"filetype": "list", "title": "Secret list"})
		for _, c := range []struct {
			id, name, title, sub string
			restore              bool
		}{
			{"F0LVGONE01", "deleted", "This list was deleted.", "Was that a mistake? Restore this list.", true},
			{"F0LVGONE02", "not-found", "This list was not found.", "", false},
			{"F0LVGONE03", "not-visible", "You don’t have access to this list.", "", false},
		} {
			var restored []core.DeletedFile
			v := New(h, c.id, Tab, lvActions(func(f core.DeletedFile, done func()) { restored = append(restored, f) }, nil, nil, nil))
			s := newShooter(t, image.Pt(900, 640))
			draw := func(gtx layout.Context) { v.Layout(gtx) }
			settle(t, s, draw, c.name+"'s state", func() bool { return lvGone(v) })
			settle(t, s, draw, "files.canUndelete", func() bool {
				for _, f := range fs.CanUndeleteCalls() {
					if f.Get("file_id") == c.id {
						return true
					}
				}
				return false
			})
			for i := 0; i < 10; i++ { // its answer lands
				s.frame(draw)
				time.Sleep(10 * time.Millisecond)
			}
			title, sub, link := lvText(v)
			if title != c.title || sub != c.sub || link != c.restore {
				t.Errorf("%s: %q / %q link %v, want %q / %q %v", c.name, title, sub, link, c.title, c.sub, c.restore)
			}
			s.shot("list-gone-"+c.name+"-"+mode(dark), draw)
			if !c.restore {
				continue
			}
			lvRestore(v)
			s.frame(draw)
			if len(restored) != 1 || restored[0].ID != c.id || restored[0].Title != "Groceries" || restored[0].FileType != "list" {
				t.Errorf("%s: restore opened %+v, want UndeleteFileDialog for %s", c.name, restored, c.id)
			}
		}
	}
}

// DeleteFileDialog's onGo: the channel's tab holding the list goes first
// (Actions.LeaveTab), then files.delete; from the tab its toast
// (Actions.Deleted), elsewhere the view closes. The list's next read
// answers the tombstone.
func TestDeleteListLeavesTab(t *testing.T) {
	h, _ := newEnv(t, false)
	for _, fromTab := range []bool{true, false} {
		var mu sync.Mutex
		var steps []string
		note := func(s string) { mu.Lock(); steps = append(steps, s); mu.Unlock() }
		v := New(h, fakeslack.ListTodos, Tab, lvActions(nil, func() bool { note("leave"); return fromTab },
			func() { note("toast") }, func() { note("close") }))
		s := newShooter(t, image.Pt(900, 640))
		draw := func(gtx layout.Context) { v.Layout(gtx) }
		settle(t, s, draw, "the list", func() bool { return v.Doc() != nil })
		lvDelete(v, fakeslack.ListTodos)
		want := []string{"leave", "toast"}
		if !fromTab {
			want = []string{"leave", "close"}
		}
		settle(t, s, draw, "the delete", func() bool { mu.Lock(); defer mu.Unlock(); return len(steps) == 2 })
		mu.Lock()
		if !slices.Equal(steps, want) {
			t.Errorf("from the tab %v: %v, want %v", fromTab, steps, want)
		}
		mu.Unlock()
		if _, err := h.a.List(context.Background(), fakeslack.ListTodos); !lvIsGone(err) {
			t.Errorf("the deleted list's read: %v", err)
		}
		if err := h.a.UndeleteFile(context.Background(), fakeslack.ListTodos); err != nil {
			t.Fatalf("undelete: %v", err)
		}
	}
}
