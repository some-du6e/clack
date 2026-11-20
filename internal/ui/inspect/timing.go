package inspect

import (
	"fmt"
	"image"
	"log"
	"strings"
	"sync"
	"time"
)

// A capture's stage times. Karim, 2026-11-18, on Windows at 1918x1080:
// "this shit takes to long between a selection and the blur page". Each
// capture logs one line when it ends (copied, dropped or failed): how long
// every stage from the pick to the review on screen took, and after Enter
// the delivery's. Every log line goes to Clack's log file (applog).

// capTimes are one capture's stages.
type capTimes struct {
	kind   string      // "click", "shift-click", "region"
	frozen bool        // picked on a frozen frame (freeze.go)
	win    image.Point // the window (px)
	scale  float32     // its px per dp
	shot   image.Point // the shot (px)
	k      float32     // the shot's px per window px

	report time.Duration // building the report (the press's; a region's at the release)
	pick   time.Time     // the release was read
	pickN  int           // the tool's frame count then
	submit time.Duration // from the release to its frame submitted (AfterFrame)
	shoot  shotTimes
	shown  time.Duration // the review's shot, its margin blurred
	drawn  time.Duration // from the release to the first frame drawing the review's shot
	frames int           // frames from the release's to that one

	atOnce                                bool          // copied at once, no review (a Shift+click; Enter in an element pick's frame)
	noShot                                bool          // no shot (it failed, or the pick was outside the window): copied as text at once
	enter                                 time.Duration // from the review drawn (or the shot, with no review) to Enter
	colors, compose, encode, save, clipbd time.Duration
	outcome                               string
	logged                                bool
}

// shotTimes are Shoot's stages: the headless GPU context and its device
// (gpu, none when one was ready: reused), the frame rendered, the pixels
// read back.
type shotTimes struct {
	gpu, render, read, total time.Duration
	reused                   bool
}

// lastShot is what the default Shoot measured (UI goroutine; zero after a
// test's Shoot).
var lastShot shotTimes

// timesLog is the last line logged (tests).
var timesLog struct {
	mu   sync.Mutex
	line string
}

// logTimes writes c's line (once; any goroutine that owns c).
func logTimes(c *capture) {
	if c == nil || c.tm.logged {
		return
	}
	c.tm.logged = true
	line := c.tm.line()
	log.Print(line)
	timesLog.mu.Lock()
	timesLog.line = line
	timesLog.mu.Unlock()
}

// LastTimes is the last capture's line (tests).
func LastTimes() string {
	timesLog.mu.Lock()
	defer timesLog.mu.Unlock()
	return timesLog.line
}

func (m *capTimes) line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "inspect: capture %s", m.kind)
	if m.frozen {
		b.WriteString(" (frozen)")
	}
	sc := m.scale
	if sc <= 0 {
		sc = 1
	}
	fmt.Fprintf(&b, ", window %dx%d px at %gx", m.win.X, m.win.Y, sc)
	if m.shot != (image.Point{}) {
		fmt.Fprintf(&b, ", shot %dx%d px at %.2fx", m.shot.X, m.shot.Y, m.k)
	}
	fmt.Fprintf(&b, ": report %s, pick to frame submitted %s", dur(m.report), dur(m.submit))
	if m.shoot.total > 0 {
		fmt.Fprintf(&b, ", shoot %s (", dur(m.shoot.total))
		switch {
		case m.shoot.reused && m.shoot.gpu >= time.Millisecond:
			fmt.Fprintf(&b, "gpu ready after %s", dur(m.shoot.gpu)) // the session's, still being made at the pick
		case m.shoot.reused:
			b.WriteString("gpu ready")
		default:
			fmt.Fprintf(&b, "gpu context %s", dur(m.shoot.gpu))
		}
		fmt.Fprintf(&b, ", render %s, readback %s)", dur(m.shoot.render), dur(m.shoot.read))
	}
	if m.shown > 0 {
		fmt.Fprintf(&b, ", review shot ready %s", dur(m.shown))
	}
	if m.drawn > 0 {
		fmt.Fprintf(&b, ", review drawn %s after the pick (%d frames)", dur(m.drawn), m.frames)
	}
	if m.noShot || m.compose > 0 || m.encode > 0 || m.clipbd > 0 || m.save > 0 {
		switch {
		case m.noShot: // whatever its stages took (no folder to save in: 0 on Windows' clock)
			b.WriteString("; no shot: copied as text at once:")
		case m.atOnce:
			b.WriteString("; copied at once:")
		case m.drawn == 0:
			// an element's Enter in the frame after its pick: the review
			// was never drawn (a region's waits for it: reviewKey)
			fmt.Fprintf(&b, "; Enter %s after the pick, before the review showed:", dur(m.enter))
		default:
			fmt.Fprintf(&b, "; Enter %s after the review showed:", dur(m.enter))
		}
		fmt.Fprintf(&b, " colours %s, compose %s, png %s, save %s, clipboard %s",
			dur(m.colors), dur(m.compose), dur(m.encode), dur(m.save), dur(m.clipbd))
	}
	if m.outcome != "" {
		b.WriteString("; " + m.outcome)
	}
	return b.String()
}

// dur is d for the log: ms to a tenth under 10 s.
func dur(d time.Duration) string {
	if d >= 10*time.Second {
		return d.Round(10 * time.Millisecond).String()
	}
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
}
