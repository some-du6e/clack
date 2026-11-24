package ui

// The deleted-file-tab tests' reach into the fix's API, in one file (a red
// run on the old code swaps it for stubs).

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
)

func dtCanvasGone(v *canvasview.View) bool { _, ok := v.Gone(); return ok }

func dtCanvasTitle(v *canvasview.View) (title, button string) { return v.GoneTitle() }

func dtListGone(v *listview.View) bool { _, ok := v.Gone(); return ok }

func dtListText(v *listview.View) (title, sub string, restore bool) { return v.GoneText() }

func dtTabPeekKey(ch, id string) string { return barTabPeekKey(ch, id) }

// dtTabPeekCanvas is the open bar peek's canvas view (nil: not a canvas
// peek, or not mounted yet).
func dtTabPeekCanvas(e *perfEnv, key string) *canvasview.View {
	p, ok := e.a.tpeeks.m[key]
	if !ok {
		return nil
	}
	b, ok := p.content.(*barTabPeek)
	if !ok {
		return nil
	}
	cp, ok := b.sp.body.(*canvasTabPeek)
	if !ok {
		return nil
	}
	return cp.v
}

func dtLookHook(f func(key string)) { tabLookHook = f }

func dtLeaveFileTab(e *perfEnv, fileID string) bool { return e.a.leaveFileTab(fileID) }

// dtTabGone is a tab read's file state.
func dtTabGone(t core.Tab) (status, name string, gated, ok bool) {
	if t.Gone == nil {
		return "", "", false, false
	}
	return t.Gone.Status, t.Gone.Name, t.Gone.Gated, true
}
