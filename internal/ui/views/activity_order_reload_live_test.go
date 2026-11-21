package views

// Reloads on internal/fakeslack's Activity fixture (activity_order.go
// actReloadKeeps): an invitation you ignore leaves on the reload that
// follows, the newest row too; a reload after paging keeps the later
// pages on a type tab and under the VIP sort, as ui did (the peek opening,
// Undo and ⌘Z after a Clear and the invitation answers all reload).
// Synthetic data.

import (
	"context"
	"image"
	"net/url"
	"slices"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Ignore on the newest row, the Slack Connect DM invitation: the fake
// stops listing it, and the reload after the write takes the row out
// (with its buttons, which would send the writes again).
func TestActivityAnsweredInviteLeaves(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	const key = "fa-ext-dm-invite"
	if k := actShownKeys(v); len(k) == 0 || k[0] != key {
		t.Fatalf("fixture: the DM invitation is the newest row, the list starts %v", k[:min(3, len(k))])
	}
	last := func() *popups.Modal {
		open := h.open()
		if len(open) == 0 {
			return nil
		}
		m, _ := open[len(open)-1].(*popups.Modal)
		return m
	}
	feeds := fs.CallCount("activity.feed")
	v.inviteClicked(actItem(t, v, key), core.InviteActIgnore)
	e.frame()
	if v.forked[0] == nil {
		t.Fatal("Ignore opened no forked modal")
	}
	v.forked[0]() // Ignore this invitation
	m := last()
	if m == nil {
		t.Fatal("no Ignore this invitation modal")
	}
	m.PressDefault()
	e.until(t, "sharedInvites.ignoreInvite", func() bool {
		return actFakeWrite(fs, "sharedInvites.ignoreInvite", func(f url.Values) bool { return f.Get("invite") == fakeslack.ActivityInviteDM })
	})
	e.until(t, "the reload", func() bool { return fs.CallCount("activity.feed") > feeds && !v.data.busy() && !v.wasBusy })
	for range 5 {
		e.frame()
	}
	if k := actShownKeys(v); slices.Contains(k, key) {
		t.Fatalf("the invitation you ignored is still listed after the reload (rows %v)", k[:min(3, len(k))])
	}
}

// A reload after paging to the end keeps every row loaded and the cursor,
// on the Mentions tab and under the VIP sort.
func TestActivityReloadKeepsLaterPages(t *testing.T) {
	for _, view := range []string{"Mentions", "the VIP sort"} {
		t.Run(view, func(t *testing.T) {
			app, fs := vaFake(t)
			fs.SetActivityBulk(30)
			h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
			v := NewActivity(h)
			e := &vaEnv{size: image.Pt(1200, 900)}
			e.draw = func(gtx layout.Context) { v.Layout(gtx) }
			e.until(t, "the feed and the saved views", func() bool {
				_, ok, _ := v.data.get()
				return ok && !v.data.busy() && v.viewsLoaded
			})
			switch view {
			case "Mentions":
				v.pickTab(2)
				e.until(t, "the Mentions tab", func() bool {
					types, _ := v.types()
					return types == actMentionTypes && !v.data.busy() && !v.wasBusy
				})
			default:
				if err := app.AddVIP(context.Background(), "U0FAKE00001"); err != nil {
					t.Fatal(err)
				}
				v.setSort(true)
				e.until(t, "the VIP order", func() bool { return v.sortVIP() && len(v.vis) > 0 && v.vis[0] == actVisVIP && !v.data.busy() })
			}
			e.until(t, "every page", func() bool {
				v.list.Position.First = len(v.vis) // the list asks for the next page at its end
				return actPage(v).Next == "" && !v.data.busy() && !v.wasBusy
			})
			loaded := actPage(v)
			if len(loaded.Items) <= actLimit {
				t.Fatalf("precondition: %d rows loaded, want more than a page (%d)", len(loaded.Items), actLimit)
			}
			// back at the top, so the list asks for no page while the
			// reload is out (an end of list in view pages cut rows back)
			v.list.Position = layout.Position{}
			e.frame()
			// a reload: the peek opening, Undo or ⌘Z after a Clear, an
			// invitation's answer, activity_invalidated
			feeds := fs.CallCount("activity.feed")
			v.load(actRefresh)
			e.until(t, "the reload", func() bool { return fs.CallCount("activity.feed") > feeds && !v.data.busy() && !v.wasBusy })
			got := actPage(v)
			if actOrderSet(got.Items) != actOrderSet(loaded.Items) {
				t.Errorf("the reload lists %d rows, want the %d loaded (the later pages too)", len(got.Items), len(loaded.Items))
			}
			if got.Next != loaded.Next {
				t.Errorf("cursor %q after the reload, want %q", got.Next, loaded.Next)
			}
		})
	}
}
