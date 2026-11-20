package composer

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The drafts button's and the hint bar's parts the tests reach
// (draft_button_test.go). A red-evidence commit on today's composer swaps
// only this file, for stubs.

// dmStash saves text as a draft ch's composer saved ("Save as draft": a
// composer draft, stashed now).
func dmStash(ctx context.Context, a *core.App, ch, text string) (core.Draft, error) {
	return a.SaveDraft(ctx, core.DraftOpts{Channel: ch, Text: text, DateStashed: time.Now().UnixMilli()})
}

// dmButton is the drafts button's and the send button's rects
// (composer-local) and the count the drafts button has.
func dmButton(c *Composer) (drafts, send image.Rectangle, n int) {
	return c.ui.rect[crDrafts], c.ui.rect[crSend], c.ui.cur.Drafts
}

// dmTip is the drafts button's tooltip.
func dmTip(c *Composer) string {
	st := &c.ui.cur
	return draftsTip(st.Drafts, st.IsDM, st.DraftsBusy)
}

// dmMenu is the drafts button's menu; dmOpenMenu opens it at anchor
// (window coordinates).
func dmMenu(c *Composer) []popups.MenuItem { return c.draftsMenuItems() }

func dmOpenMenu(c *Composer, anchor image.Rectangle) { c.draftsMenu(anchor) }

// dmStashed reports a fake's draft saved as a composer draft, stashed.
func dmStashed(d fakeslack.Draft) bool { return d.IsFromComposer && d.DateStashed > 0 }

// hintOpacity is the formatting tip's opacity at now.
func hintOpacity(c *Composer, now time.Time) float32 {
	a, _ := c.ui.hint.at(now)
	return a
}

// hintRect is the formatting tip's ButtonLink (composer-local; empty
// while it takes no pointer).
func hintRect(c *Composer) image.Rectangle { return c.ui.hint.rect }

// setOnline makes the hint bar read wasRecentlyOnline from on.
func setOnline(t *testing.T, on func() (bool, time.Time)) {
	prev := recentlyOnline
	recentlyOnline = func(Host) (bool, time.Time) { return on() }
	t.Cleanup(func() { recentlyOnline = prev })
}

// hookIcons sends the name of each icon drawn to f until restore.
func hookIcons(f func(name string)) (restore func()) {
	prev := icons.DrawHook
	icons.DrawHook = f
	return func() { icons.DrawHook = prev }
}
