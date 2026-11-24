package canvasview

// TestRenderGoneStates' reach into the fix's API (a red run on the old
// code swaps this file for stubs).

func cvGone(v *View) bool { _, ok := v.Gone(); return ok }

func cvTitle(v *View) (title, button string) { return v.GoneTitle() }

func cvRequest(v *View) { v.GoneRequestAccess() }

// cvPick makes getDeletedState answer i from now on.
func cvPick(i int) { pickDeleted = func() int { return i } }
