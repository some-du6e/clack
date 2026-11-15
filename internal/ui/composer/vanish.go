package composer

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The message box never vanishes. Where Slack shows no composer (an
// archived channel, a channel you're previewing, a thread with replies
// off) the shell draws Slack's footer instead and never asks for the
// composer, and a roadblock notice (an announcement-only channel) takes
// the box's place inside it. Every other frame draws the box.
//
// Under a plugin the box is drawn by the plugin's tree, and that tree can
// come back without it: every patched component is a React root of its
// own, and an error nothing caught unmounted the root, so its tree was "[]"
// until its props changed. Karim's composer was simply gone from #clack
// (Better Typing Indicator patches MessagePaneInput, and its typing line
// renders again whenever someone types) until Clack restarted. Taut's
// PatchGuard catches those errors now (internal/taut/js/04_react.js); this
// guard is for whatever else leaves the box out. A frame in which the box
// wasn't laid out, no roadblock was drawn and no CSS hid it is drawn again
// without plugins (slackc.Unpatched), the composer's plugin roots are
// mounted again (5 s apart at first, doubling while they keep failing),
// and a log line says what each plugin tree held.

// OnVanish, when set, gets the guard's log lines on the UI goroutine (the
// ui root adds the plugins that are running); unset, they go to log.Print.
var OnVanish func(line string)

func vanishLog(line string) {
	if f := OnVanish; f != nil {
		f(line)
		return
	}
	log.Print(line)
}

const (
	vanishLogEvery   = time.Minute      // the most often a box still missing is logged
	vanishSettle     = 30 * time.Second // drawn through its plugins this long: the run is over
	vanishRemountMin = 5 * time.Second  // the wait after the first remount; it doubles
	vanishRemountMax = 5 * time.Minute
)

// vanishGuard is a composer's run of frames whose boundary drew no box.
type vanishGuard struct {
	first, last time.Time // the run's first and latest such frame (zero: no run)
	frames      int       // such frames in the run
	remounts    int
	loggedAt    time.Time
	remountAt   time.Time     // no remount before
	backoff     time.Duration // the wait after the next remount
}

// boundaryName is the composer's outermost Slack component: MessageInput
// for an edit or a Block Kit field, MessagePaneInput for a channel,
// InputContainer for a thread.
func (c *Composer) boundaryName() string {
	switch {
	case c.edit != nil || c.field != nil:
		return "MessageInput"
	case c.ts != "":
		return "InputContainer"
	}
	return "MessagePaneInput"
}

// drawBoundary draws the composer through its outermost boundary (slack.go).
func (c *Composer) drawBoundary(gtx layout.Context) layout.Dimensions {
	switch {
	case c.edit != nil || c.field != nil:
		return slackc.Component(gtx, "MessageInput", c, (*Composer).inputProps, (*Composer).layoutInput)
	case c.ts != "":
		return slackc.Component(gtx, "InputContainer", c, (*Composer).paneProps, (*Composer).layoutPane)
	}
	return slackc.Component(gtx, "MessagePaneInput", c, (*Composer).paneProps, (*Composer).layoutPane)
}

// resetDraw clears what a draw of the boundary records about itself.
func (c *Composer) resetDraw() {
	c.top, c.drewInput, c.drewRoadblock, c.cssHidden = 0, false, false, false
	c.ui.f.boxOK, c.ui.f.boxHidden = false, false
	c.paneP, c.inputP = nil, nil
}

// boxShown reports whether the last draw of the boundary showed the box,
// or what stands in for it on purpose: a roadblock notice, or CSS that hid
// the pane, the input or the box.
func (c *Composer) boxShown() bool {
	return c.ui.f.boxOK || c.drewRoadblock || c.cssHidden || c.ui.f.boxHidden
}

// layoutBoundary draws the boundary, and the composer without plugins when
// the boundary left the box out.
func (c *Composer) layoutBoundary(gtx layout.Context) layout.Dimensions {
	c.resetDraw()
	m := op.Record(gtx.Ops)
	dims := c.drawBoundary(gtx)
	call := m.Stop()
	live := gtx.Source.Enabled() // not the measure pass of a patched parent
	if c.boxShown() {
		call.Add(gtx.Ops)
		if live {
			c.vanishEnded(gtx.Now)
		}
		return dims
	}
	// What the plugins drew is dropped: it has no box in it, and the box
	// drawn below would sit on top of it.
	if live {
		c.vanished(gtx.Now)
	}
	c.resetDraw()
	return slackc.Unpatched(func() layout.Dimensions { return c.drawBoundary(gtx) })
}

// vanished notes a live frame whose boundary drew no box: it logs the
// first one and one a minute after, and remounts the plugin roots.
func (c *Composer) vanished(now time.Time) {
	g := &c.vanish
	first := g.first.IsZero()
	if first {
		g.first = now
	}
	g.last = now
	g.frames++
	trees := slackc.KeyedTrees(c.key()) // before the remount drops them
	remounted := 0
	if !now.Before(g.remountAt) {
		remounted = slackc.RemountKeyed(c.key())
		g.remounts++
		if g.backoff == 0 {
			g.backoff = vanishRemountMin
		} else {
			g.backoff = min(2*g.backoff, vanishRemountMax)
		}
		g.remountAt = now.Add(g.backoff)
	}
	if !first && now.Sub(g.loggedAt) < vanishLogEvery {
		return
	}
	g.loggedAt = now
	var b strings.Builder
	if first {
		fmt.Fprintf(&b, "composer: %s for %s drew no message box; drawing it without plugins", c.boundaryName(), c.where())
	} else {
		fmt.Fprintf(&b, "composer: %s for %s still draws no message box through its plugins: %d frames over %v, plugin roots remounted %d times",
			c.boundaryName(), c.where(), g.frames, now.Sub(g.first).Round(time.Second), g.remounts)
	}
	if remounted > 0 {
		fmt.Fprintf(&b, "; remounted %d plugin root(s)", remounted)
	}
	if trees == "" {
		trees = "none"
	}
	fmt.Fprintf(&b, "; its plugin trees: %s; patched: %s", trees, strings.Join(slackc.PatchedNames(), ", "))
	vanishLog(b.String())
}

// vanishEnded notes a live frame that drew the box: a run of missing
// boxes is over once the box has been drawn for vanishSettle.
func (c *Composer) vanishEnded(now time.Time) {
	g := &c.vanish
	if g.first.IsZero() || now.Sub(g.last) < vanishSettle {
		return
	}
	vanishLog(fmt.Sprintf("composer: %s for %s draws its message box through its plugins again (%d frames without, over %v; plugin roots remounted %d times)",
		c.boundaryName(), c.where(), g.frames, g.last.Sub(g.first).Round(time.Second), g.remounts))
	*g = vanishGuard{}
}

// where names the conversation for a log line: ids, no names or text.
func (c *Composer) where() string {
	s := c.ch
	if s == "" {
		s = "(no channel)"
	}
	if c.ts != "" {
		s += " thread " + c.ts
	}
	switch {
	case c.edit != nil:
		s += " (editing " + c.edit.TS + ")"
	case c.field != nil:
		s += " (a Block Kit field)"
	}
	return s
}

// BoxVanished reports whether the composer's last live frame drew its box
// without plugins because its boundary left the box out (tests; the shell).
func (c *Composer) BoxVanished() bool {
	return !c.vanish.first.IsZero() && c.vanish.last.Equal(c.drawnAt)
}

// HiddenByCSS reports whether CSS (a theme's, a plugin's, the user's) hid
// the composer's pane, input or box in its last frame: no height then is
// what the stylesheet asked for (the shell's check, composer_guard.go).
func (c *Composer) HiddenByCSS() bool { return c.cssHidden || c.ui.f.boxHidden }
