package ui

import (
	"log"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/composer"
)

// A conversation's composer is always under its list when layoutConvo
// draws one: the footers (an archived channel, a preview) and a thread
// without an input row are decided before it. composer/vanish.go keeps a
// plugin's tree from leaving the box out; this is the shell's last check.
// A composer that laid out to no height at all, with room for it and no
// CSS hiding it, is replaced by a new one (Close keeps the draft and the
// new one restores it), at most once every composerRebuildEvery, and each
// such frame run is logged: field evidence for a composer that went away
// (Karim, 2026-11-15: "my composer went byebye", back only after a
// restart). Ids only in the lines: no names or message text.

// composerRebuildEvery is the most often a conversation's composer is
// replaced.
const composerRebuildEvery = 30 * time.Second

// guardLogf writes the shell's composer lines (tests swap it).
var guardLogf = log.Printf

// After mount_features.go's installHooks (file order): the composer's
// guard lines name the plugins running, read off the UI goroutine.
func init() {
	prev := installHooks
	installHooks = func(a *App) {
		if prev != nil {
			prev(a)
		}
		composer.OnVanish = func(line string) {
			t, s := a.Taut(), a.Slick()
			go func() { log.Printf("%s; plugins running: %s", line, runningPlugins(t, s)) }()
		}
	}
}

// composerHeight checks the height h the conversation's composer comp just
// laid out to under constraints g, and reports whether it put a new
// composer in c (lay that one out instead).
func (a *App) composerHeight(gtx, g layout.Context, c *convo, comp composerView, h int) bool {
	if comp != c.comp || !gtx.Source.Enabled() { // an edit composer, or a parent's measure pass
		return false
	}
	if h > 0 {
		if !c.zeroSince.IsZero() {
			guardLogf("ui: the composer of %s has its height again (%dpx) after %v", c.where(), h, gtx.Now.Sub(c.zeroSince).Round(time.Second))
			c.zeroSince = time.Time{}
		}
		return false
	}
	if g.Constraints.Max.Y < gtx.Dp(40) { // no room for one: nothing to fix
		return false
	}
	hidden := false
	if hc, ok := comp.(interface{ HiddenByCSS() bool }); ok {
		hidden = hc.HiddenByCSS()
	}
	first := c.zeroSince.IsZero()
	if first {
		c.zeroSince = gtx.Now
	}
	rebuild := !hidden && (c.rebuiltAt.IsZero() || gtx.Now.Sub(c.rebuiltAt) >= composerRebuildEvery)
	switch {
	case hidden && first:
		guardLogf("ui: the composer of %s laid out to no height: CSS hides it", c.where())
	case rebuild:
		guardLogf("ui: the composer of %s laid out to no height for %v; putting a new one in its place (the draft carries over)", c.where(), gtx.Now.Sub(c.zeroSince).Round(time.Second))
	}
	if !rebuild {
		return false
	}
	c.rebuiltAt = gtx.Now
	if c.comp != nil {
		c.comp.Close() // saves the draft
	}
	c.comp = nil
	a.attachComposer(c)
	return c.comp != nil
}

// where names the conversation for a log line.
func (c *convo) where() string {
	if c.ts != "" {
		return c.ch + " thread " + c.ts
	}
	return c.ch
}
