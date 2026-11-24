package listview

// The list error state tests' reach into the fix's API (a red run on the
// old code swaps this file for stubs).

import "github.com/some-du6e/clack/internal/core"

func lvGone(v *View) bool { _, ok := v.Gone(); return ok }

func lvText(v *View) (title, sub string, restore bool) { return v.GoneText() }

func lvRestore(v *View) { v.GoneRestore() }

func lvDelete(v *View, id string) { v.deleteConfirmed(id) }

// lvActions are Actions with the error state's and the delete's hooks.
func lvActions(restore func(f core.DeletedFile, done func()), leave func() bool, deleted, closed func()) Actions {
	return Actions{Restore: restore, LeaveTab: leave, Deleted: deleted, Close: closed}
}

func lvIsGone(err error) bool { _, ok := goneOf(err); return ok }
