package ui

// Reports to Clack's developers, through its Sentry project
// (internal/sentry; Karim, 2026-11-24: "sentry for the app, hyper+r =
// report + comment"). Two kinds, both Clack's own, not Slack features:
//
//   - The report box. Hyper+R (⌃⌥⇧⌘R on macOS, Ctrl+Alt+Shift+R or with
//     Super/Win too elsewhere) takes what the inspect tool's pick would
//     report about the element under the pointer and a shot of the whole
//     window (inspect.Tool.StartReport, with a tool of the box's own, so
//     the inspect tool's gate doesn't apply), then opens the box
//     (popups.ReportBox): a comment, the report's rows as they'll be sent,
//     and the screenshot, off until ticked, which the inspect review can
//     blur parts of first. Send sends it as Sentry user feedback; nothing
//     goes out before.
//   - Crash reports, with Preferences > Clack > Send crash reports on (off
//     by default): cmd/clack hands ReportCrash the previous run's crash
//     record 10 s into a start (applog's crash.log record and run record,
//     Windows' own), and one scrubbed event goes out (sentry.CrashEvent).
//     Off, nothing is sent, and a crash isn't kept for later either.
//
// Only release builds can send (release.yml embeds the DSN). In any other
// build Options.Sentry is nil: the box opens, says reports can't be sent
// from this build and shows what one would hold; the crash row is greyed.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"log"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/sentry"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// reportFilter is the report box's shortcut, Hyper+R: all four modifiers
// (off macOS Mod is Ctrl, so three), Super allowed too, as inspectFilter.
func reportFilter() key.Filter {
	n, m := shortcut.Parse("Mod+Alt+Ctrl+Shift+R")
	return key.Filter{Name: n, Required: m, Optional: key.ModSuper}
}

// reportHint is the shortcut as this platform writes it.
func reportHint() string { return shortcut.Format("Mod+Alt+Ctrl+Shift+R") }

// reportBegin reads Hyper+R before anything else reads keys (inspectBegin:
// it works over a modal too), and while the box's capture or review runs
// returns its tool's context: the window laid out hover-only, as in an
// inspect session.
func (a *App) reportBegin(gtx layout.Context) layout.Context {
	if a.rptKey.Name == "" {
		// an App not made by New (tests): the empty name would match
		// every key
		a.rptKey = reportFilter()
	}
	for {
		e, ok := gtx.Event(a.rptKey)
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			a.openReport()
		}
	}
	if a.rpt == nil || !a.rpt.Active() {
		return gtx
	}
	return a.rpt.Begin(gtx)
}

// reportActive reports the box's capture or review under way: the window
// belongs to its tool (inspecting).
func (a *App) reportActive() bool { return a.rpt != nil && a.rpt.Active() }

// openReport is Hyper+R: the capture, then the box (showReportBox). One
// box at a time, and none while an inspect session runs (its shortcut and
// keys are the session's).
func (a *App) openReport() {
	if a.rbox != nil && !a.rbox.Dismissed() {
		return
	}
	if a.insp != nil && a.insp.Active() || a.login != nil {
		return
	}
	if a.rpt == nil {
		a.rpt = inspect.New(inspectHost{a}, inspectFilter())
	}
	if a.rpt.Active() {
		return
	}
	if a.ptrOK {
		a.rpt.SeedPointer(a.ptrWin)
	}
	a.rpt.StartReport(a.showReportBox)
}

// showReportBox opens the box with the capture's report and shot (UI
// goroutine, from the capture).
func (a *App) showReportBox(rep *inspect.Report, shot *image.RGBA) {
	if a.login != nil {
		return // signed out meanwhile
	}
	var rows []popups.ReportRow
	for _, l := range rep.Lines() {
		rows = append(rows, popups.ReportRow{Key: l.Key, Value: l.Value})
	}
	a.rbox = popups.NewReportBox(a, popups.ReportBoxOpts{
		Rows:        rows,
		Shot:        shot,
		Unavailable: a.opts.Sentry == nil,
		Blur: func(img *image.RGBA, done func(*image.RGBA, int)) bool {
			if a.rpt == nil {
				return false
			}
			s := a.pxPerDp
			if s <= 0 {
				s = 1
			}
			return a.rpt.ReviewShot(img, s, done)
		},
		Send: func(comment string, shot *image.RGBA, blurred int, done func(error)) {
			a.sendReport(rows, comment, shot, blurred, done)
		},
	})
}

// reportSendTimeout bounds a report's send (the box waits for it).
const reportSendTimeout = 40 * time.Second

// sendReport sends the box's report off the UI goroutine: the comment,
// its rows (a "screenshot" row saying whether one goes with it and how
// much of it was blurred) and the screenshot as a PNG. done gets the
// outcome on the UI goroutine.
func (a *App) sendReport(rows []popups.ReportRow, comment string, shot *image.RGBA, blurred int, done func(error)) {
	c := a.opts.Sentry
	if c == nil {
		done(errors.New("this build can't send reports"))
		return
	}
	rr := make([]sentry.ReportRow, 0, len(rows)+1)
	for _, r := range rows {
		rr = append(rr, sentry.ReportRow{Key: r.Key, Value: r.Value})
	}
	if shot != nil {
		v := "the window, nothing blurred"
		switch {
		case blurred == 1:
			v = "the window, 1 part blurred by the reporter"
		case blurred > 1:
			v = "the window, " + itoa(blurred) + " parts blurred by the reporter"
		}
		rr = append(rr, sentry.ReportRow{Key: "screenshot", Value: v})
	}
	go func() {
		var atts []sentry.Attachment
		if shot != nil {
			var b bytes.Buffer
			if err := png.Encode(&b, shot); err != nil {
				log.Printf("report: the screenshot didn't encode: %v", err)
			} else {
				atts = append(atts, sentry.Attachment{Filename: "window.png", ContentType: "image/png", Data: b.Bytes()})
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), reportSendTimeout)
		id, err := c.Send(ctx, sentry.Feedback(comment, rr, len(atts) > 0), atts...)
		cancel()
		if err != nil {
			log.Printf("report: not sent: %v", err)
		} else {
			log.Printf("report: sent to Clack's Sentry (event %s, %d rows, screenshot %v)", id, len(rr), len(atts) > 0)
		}
		if errors.Is(err, sentry.ErrRateLimited) {
			err = popups.ErrReportRateLimited
		}
		a.Post(func() { done(err) })
	}()
}

// setCrashReports is Preferences > Clack > Send crash reports.
func (a *App) setCrashReports(on bool) {
	a.prefs.CrashReports = on
	a.crashOn.Store(on)
	a.savePrefs()
}

// crashReportSendTimeout bounds a crash report's send.
const crashReportSendTimeout = 40 * time.Second

// ReportCrash sends c, the previous run's crash, as one crash report when
// Send crash reports is on and this build can send; otherwise it sends
// nothing (and keeps nothing: crash.log already started afresh). It
// reports whether it sent. Any goroutine; it waits for the send.
func (a *App) ReportCrash(c sentry.Crash) bool {
	cl := a.opts.Sentry
	if cl == nil || c.Empty() || !a.crashOn.Load() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), crashReportSendTimeout)
	defer cancel()
	id, err := cl.Send(ctx, sentry.CrashEvent(c))
	if err != nil {
		log.Printf("crash report: not sent: %v", err)
		return false
	}
	log.Printf("crash report: the previous run's crash went to Clack's Sentry (event %s)", id)
	return true
}

// crashReportsRow is Preferences > Clack > Send crash reports, the tab's
// third row (clack_prefs.go): greyed in a build that can't send.
func (p *prefsModal) crashReportsRow(f *prefsForm) {
	a := p.a
	can := a.opts.Sentry != nil
	hint := "Sends the stack trace, version and OS after a crash: never messages, names or tokens."
	if !can {
		hint = "This build can’t send them: only Clack’s releases can."
	}
	f.optionAt("crash_reports", "Send crash reports", hint, a.prefs.CrashReports, false, can, func() {
		a.setCrashReports(!a.prefs.CrashReports)
	})
}
