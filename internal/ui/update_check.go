package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net"
	"runtime"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Update checks: when they run (a wall-clock schedule), "Check now"
// (Preferences > Clack > Updates), Clack > Check for Updates… on macOS
// and the workspace menu's item elsewhere. Every check runs on
// updateLoop's goroutine; results come back to the UI goroutine through
// Post. desktop.go has the download/install phases, update_ui.go what
// they look like (Slack desktop's Help dot, dialog and menu titles).

const (
	// updateFirst: the check after launch, once the window is up (it's
	// one small request off the UI goroutine)
	updateFirst = 3 * time.Second
	updateTick  = 5 * time.Minute // how often the loop looks at the clock
	updateEvery = time.Hour       // automatic checks, by the wall clock
	updateRetry = 5 * time.Minute // after a failed check, doubling to updateEvery
	// updateSkew: a last check further than this in the future means the
	// clock was set back; check rather than wait for it to catch up.
	updateSkew = 5 * time.Minute
)

// updateSched decides when an automatic check is due. Times are wall
// clock (Round(0) strips the monotonic reading): Go's monotonic clock
// stops while a Mac sleeps, so "an hour" of sleeping timers can be days.
// Last and Limited persist in ui.json, so a restart doesn't ask GitHub
// again; the error backoff doesn't.
type updateSched struct {
	Last    time.Time // the last check GitHub answered
	Limited time.Time // GitHub's rate limit: no request before this
	retry   time.Time // a failed check: no automatic retry before this
	fails   int
	// found: the last check found a newer release this build isn't
	// (Options' UpdateFound), so the launch check runs within the hour
	found bool
}

// dueAtLaunch reports whether the check right after launch should run:
// when due, or when the last session found an update it didn't restart
// into (GitHub's rate limit still holds it).
func (s *updateSched) dueAtLaunch(now time.Time) bool {
	if s.due(now) {
		return true
	}
	_, lim := s.limited(now)
	return s.found && !lim && !now.Round(0).Before(s.retry)
}

// due reports whether an automatic check should run at now.
func (s *updateSched) due(now time.Time) bool {
	now = now.Round(0)
	if now.Before(s.Limited) && s.Limited.Sub(now) <= 24*time.Hour {
		return false
	}
	if now.Before(s.retry) {
		return false
	}
	if s.Last.IsZero() {
		return true
	}
	since := now.Sub(s.Last)
	return since >= updateEvery || since < -updateSkew
}

// limited reports whether GitHub's rate limit is still on at now, and
// until when.
func (s *updateSched) limited(now time.Time) (time.Time, bool) {
	now = now.Round(0)
	return s.Limited, now.Before(s.Limited) && s.Limited.Sub(now) <= 24*time.Hour
}

// retryAter is desktop.RateLimitError (ui doesn't import desktop).
type retryAter interface{ RetryAt() time.Time }

// record takes a check's outcome at now.
func (s *updateSched) record(now time.Time, err error) {
	now = now.Round(0)
	var rl retryAter
	switch {
	case err == nil:
		s.Last, s.Limited, s.retry, s.fails = now, time.Time{}, time.Time{}, 0
	case errors.As(err, &rl):
		s.Limited = rl.RetryAt().Round(0)
		if s.Limited.Before(now.Add(time.Minute)) {
			s.Limited = now.Add(time.Minute)
		}
	default:
		s.fails = min(s.fails+1, 8)
		s.retry = now.Add(min(updateEvery, updateRetry<<(s.fails-1)))
	}
}

// updateClock is updateLoop's time: the real one, or a test's.
type updateClock struct {
	now   func() time.Time
	first <-chan time.Time // the first automatic check after launch
	tick  <-chan time.Time
	stop  <-chan struct{} // nil: runs for the life of the process
}

func realUpdateClock() updateClock {
	return updateClock{now: time.Now, first: time.After(updateFirst), tick: time.NewTicker(updateTick).C}
}

// updateResult is one check's outcome, for the UI goroutine.
type updateResult struct {
	info   UpdateInfo
	ok     bool
	err    error
	manual bool
	at     time.Time   // when it finished (wall clock)
	sched  updateSched // the schedule after it, to persist
}

// updateSchedule is the persisted schedule (UI goroutine: Run).
func (a *App) updateSchedule() updateSched {
	var s updateSched
	if a.prefs.UpdateCheckedAt > 0 {
		s.Last = time.Unix(a.prefs.UpdateCheckedAt, 0)
	}
	if a.prefs.UpdateLimitedUntil > 0 {
		s.Limited = time.Unix(a.prefs.UpdateLimitedUntil, 0)
	}
	s.found = a.prefs.UpdateFound != "" && a.prefs.UpdateFound != buildVersion
	return s
}

// updateLoop runs the checks: automatically once due (and "Check for
// updates" is on), and right away for "Check now" whatever that setting
// says. It is the only goroutine that calls Options.CheckUpdate, so a
// check never overlaps another.
func (a *App) updateLoop(s updateSched, clk updateClock) {
	if a.opts.CheckUpdate == nil {
		return
	}
	first := clk.first
	for {
		manual, launch := false, false
		select {
		case <-clk.stop:
			return
		case <-first:
			first, launch = nil, true
		case <-clk.tick:
		case <-a.updWake:
		case <-a.updNow:
			manual = true
		}
		now := clk.now()
		due := s.due(now)
		if launch {
			due = s.dueAtLaunch(now)
		}
		if !manual && (a.updatesOff.Load() || !due) {
			continue
		}
		var r updateResult
		if until, on := s.limited(now); on {
			// rate limited: asking now only pushes the limit out
			r = updateResult{err: &updateLimited{until}, manual: manual, at: now.Round(0)}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, ok, err := a.opts.CheckUpdate(ctx)
			cancel()
			now = clk.now()
			s.record(now, err)
			if err != nil {
				log.Printf("ui: update check: %v", err)
			}
			r = updateResult{info: info, ok: ok, err: err, manual: manual, at: now.Round(0)}
		}
		r.sched = s
		a.Post(func() { a.updateChecked(r) })
	}
}

// updateLimited is a check Clack didn't send: GitHub's rate limit is on.
type updateLimited struct{ until time.Time }

func (e *updateLimited) Error() string      { return "update check: rate limited" }
func (e *updateLimited) RetryAt() time.Time { return e.until }

// nudgeUpdates asks the loop to look at the clock now (the window got
// focus: maybe after the machine slept). It checks only if one is due.
func (a *App) nudgeUpdates() {
	select {
	case a.updWake <- struct{}{}:
	default:
	}
}

// checkForUpdates is "Check now" and the menus' Check for Updates…: a
// check right away, even with "Check for updates" off. notify (the
// menus) answers like Slack's manual check: a message box when there's
// nothing new or the check failed, else the download starts (the menus
// say Downloading Update, then Help gets its dot). The Preferences row
// shows it in place. UI goroutine.
func (a *App) checkForUpdates(notify bool) {
	if a.opts.CheckUpdate == nil {
		if notify {
			a.Toast("This build of " + AppName + " doesn’t check for updates.")
		}
		return
	}
	c := &a.update.check
	c.notify = c.notify || notify
	if c.checking {
		return // one is on its way; it answers both
	}
	c.checking = true
	select {
	case a.updNow <- struct{}{}:
	default:
	}
	a.Invalidate()
}

// updateCheckState is the result of the last check this session, for
// Preferences.
type updateCheckState struct {
	checking bool   // a manual check is running
	notify   bool   // a menu asked: answer in a message box
	upToDate bool   // the last answer: nothing newer
	err      string // the last manual check failed: why, in plain words
	at       time.Time
}

// updateChecked takes a check's result (UI goroutine).
func (a *App) updateChecked(r updateResult) {
	c := &a.update.check
	notify := false
	if r.manual {
		notify = c.notify
		c.checking, c.notify = false, false
	}
	// persist the schedule, and what it found
	last, lim := unixOrZero(r.sched.Last), unixOrZero(r.sched.Limited)
	found := a.prefs.UpdateFound
	switch {
	case r.err != nil:
	case r.ok:
		found = r.info.Tag
	default:
		found = ""
	}
	if last != a.prefs.UpdateCheckedAt || lim != a.prefs.UpdateLimitedUntil || found != a.prefs.UpdateFound {
		a.prefs.UpdateCheckedAt, a.prefs.UpdateLimitedUntil, a.prefs.UpdateFound = last, lim, found
		a.savePrefs()
	}
	switch {
	case r.err != nil:
		if !r.manual {
			break // automatic: logged; Preferences keeps the last answer
		}
		c.err = updateErrText(r.err, r.at)
		if notify { // Slack's yR
			a.updateMessageBox("Couldn’t update "+AppName, c.err)
		}
	case r.ok:
		c.err, c.upToDate, c.at = "", false, r.at
		a.updateFound(r.info, r.manual)
		if notify && a.update.phase == updAvailable {
			// Slack's manual check downloads what it finds; Clack's
			// does too when automatic downloads are off. Without a
			// downloader (a package-managed install), Preferences > Clack
			// has the release notes.
			if a.opts.DownloadUpdate != nil {
				a.startUpdateDownload(true)
			} else {
				a.openPrefsSection(clackTabID)
			}
		}
	default:
		c.err, c.upToDate, c.at = "", true, r.at
		if notify { // Slack's yI
			a.updateMessageBox("You’re all good", "You’ve got the latest version of "+AppName+"; thanks for staying on the ball.")
		}
	}
	a.Invalidate()
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// updateErrText says why a check failed, in plain words.
func updateErrText(err error, now time.Time) string {
	var rl retryAter
	if errors.As(err, &rl) {
		return "GitHub is limiting update checks from your network. Try again in " + waitText(rl.RetryAt().Round(0).Sub(now.Round(0))) + "."
	}
	var st interface{ HTTPStatus() string }
	if errors.As(err, &st) {
		return "GitHub couldn’t answer right now (" + st.HTTPStatus() + "). Try again later."
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return "GitHub didn’t answer in time. Check your internet connection and try again."
	}
	var oe *net.OpError
	var de *net.DNSError
	if errors.As(err, &oe) || errors.As(err, &de) {
		return "Couldn’t reach GitHub. Check your internet connection and try again."
	}
	return "Couldn’t check for updates: " + err.Error()
}

// waitText is a wait in minutes ("1 minute", "12 minutes", "2 hours").
func waitText(d time.Duration) string {
	m := int((d + time.Minute - 1) / time.Minute)
	switch {
	case m <= 1:
		return "a minute"
	case m < 90:
		return strconv.Itoa(m) + " minutes"
	}
	return strconv.Itoa((m+30)/60) + " hours"
}

// agoText is when something happened, relative to now.
func agoText(t, now time.Time) string {
	d := now.Round(0).Sub(t.Round(0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d/time.Minute))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d/time.Hour))
	}
	return "on " + t.Local().Format("Jan 2")
}

// updateStatus is the step the Preferences > Clack update row shows
// under its "Download updates automatically" box, and the row's one
// button. title is Slack desktop's word for the step (the menu item dY,
// the message boxes yI/yR, AppUpdateDialog's button); desc says which
// version and how far.
type updateStatus struct {
	title, desc string
	notes       bool   // desc ends with the release notes link (AppUpdateDialog's)
	button      string // "" for none
	primary     bool   // the button is the step to take (Download, Restart)
	enabled     bool
	// bar: a progress bar under the description, filled frac (0..1), or
	// indeterminate when frac < 0
	bar  bool
	frac float32
}

// Slack's labels for an update's steps (dY, yI, yR) and the row's buttons.
const (
	updRowCheck   = "Check for Updates"
	updRowGood    = "You’re all good"
	updRowFailed  = "Couldn’t update " + AppName
	updRowRestart = "Restart " + AppName // AppUpdateDialog's button
	updRowTry     = "Try Again"
	updRowCancel  = "Cancel"
	updRowGet     = "Download"
)

// updateStatusAt is the row's state at now (UI goroutine).
func (a *App) updateStatusAt(now time.Time) updateStatus {
	u := &a.update
	c := &u.check
	check := updateStatus{button: updRowCheck, enabled: !c.checking}
	if u.info.Tag != "" {
		name := AppName + " " + u.info.Tag
		switch u.phase {
		case updAvailable:
			switch {
			case u.dlErr != "":
				return updateStatus{title: updRowFailed, desc: "The download of " + name + " didn’t work: " + u.dlErr, button: updRowTry, primary: true, enabled: true}
			case a.opts.DownloadUpdate == nil: // a package-managed install
				return updateStatus{title: updMenuAvailable, desc: name + " is out: update it the way you installed " + AppName + ".", notes: true, button: updRowCheck, enabled: !c.checking}
			}
			return updateStatus{title: updMenuAvailable, desc: name + " is ready to download.", notes: true, button: updRowGet, primary: true, enabled: true}
		case updDownloading:
			return updateStatus{title: updMenuDownload, desc: updateProgressText(u.info.Tag, u.prog), button: updRowCancel, enabled: true, bar: true, frac: u.prog.frac()}
		case updReady:
			return updateStatus{title: updMenuRestart, desc: name + " is ready. Curious what’s new? ", notes: true, button: updRowRestart, primary: true, enabled: true}
		case updInstalling:
			return updateStatus{title: updMenuRestart, desc: "Installing " + name + "…", button: updRowRestart, primary: true, bar: true, frac: -1}
		case updManual:
			return updateStatus{title: updMenuRestart, desc: "Finish installing " + name + " in the window that opened, then restart " + AppName + "."}
		case updFailed:
			return updateStatus{title: updRowFailed, desc: "Couldn’t install " + name + ": " + u.err, button: updRowTry, primary: true, enabled: true}
		}
	}
	st := check
	switch {
	case c.checking:
		st.title = updMenuChecking
	case c.err != "":
		st.title, st.desc = updRowFailed, c.err
	case c.upToDate:
		st.title = updRowGood
		st.desc = AppName + " " + versionOr() + " is the latest. Checked " + agoText(c.at, now) + "."
	default:
		st.title = AppName + " " + versionOr()
		st.desc = "Not checked yet."
		if a.prefs.UpdateCheckedAt > 0 {
			st.desc = "Last checked " + agoText(time.Unix(a.prefs.UpdateCheckedAt, 0), now) + "."
		}
	}
	return st
}

// versionOr is this build's version, or "dev build" when unset.
func versionOr() string {
	if buildVersion == "" {
		return "dev build"
	}
	return buildVersion
}

// updateRowClick runs the row's button (UI goroutine).
func (a *App) updateRowClick(label string) {
	u := &a.update
	switch label {
	case updRowCheck:
		a.checkForUpdates(false)
	case updRowGet:
		u.manual = true
		a.startUpdateDownload(true)
	case updRowTry:
		if u.phase == updFailed {
			a.installUpdate()
		} else {
			u.manual = true
			a.startUpdateDownload(true)
		}
	case updRowCancel:
		a.cancelUpdateDownload()
	case updRowRestart:
		a.installUpdate()
	}
	a.Invalidate()
}

// updatesSection is Preferences > Clack's update row: Slack's checkbox
// row, "Download updates automatically" (checks and downloads; off, only
// the button checks), its subtext the step in Slack desktop's words, and
// the step's one button on the right.
func (p *prefsModal) updatesSection(f *prefsForm) {
	a := p.a
	if a.opts.CheckUpdate == nil { // dev builds: no checks, nothing to show
		return
	}
	p.updateRow(f, a.updateStatusAt(time.Now()))
}

// autoUpdatesOn is the row's box: automatic checks and downloads.
func (a *App) autoUpdatesOn() bool { return a.updateChecksOn() && a.updateDownloadsOn() }

// toggleAutoUpdates flips both settings together.
func (a *App) toggleAutoUpdates() {
	on := !a.autoUpdatesOn()
	if a.updateChecksOn() != on {
		a.toggleUpdateChecks()
	}
	if a.updateDownloadsOn() != on {
		a.toggleUpdateDownloads()
	}
	if on {
		a.nudgeUpdates() // a check now if one is due
	}
}

// hint is the row's subtext: the step, then which version and how
// far ("Downloading Update · Clack v0.1.0-alpha.14 · 12.1 of 35.6 MB
// (34%)").
func (st updateStatus) hint() string {
	if st.desc == "" {
		return st.title
	}
	return st.title + " · " + st.desc
}

// updateRow lays out st as a Slack checkbox row (the box, the 15px label,
// the c-label__subtext under it, wrapped, the release notes link inline,
// the progress bar under that) with a medium button 16px to the right,
// centred on the text.
func (p *prefsModal) updateRow(f *prefsForm, st updateStatus) {
	gtx, a := f.gtx, p.a
	pc := p.colors()
	label := pt("Download updates automatically")
	on := a.autoUpdatesOn()
	f.record("check", "update_auto", label, on, true)
	f.record("text", "", st.hint(), false, true)
	b := p.btn("update_row")
	hit := b.Clicked(gtx) // popups.Button lays the clickable out: read it first
	var bcall op.CallOp
	var bd layout.Dimensions
	if st.button != "" {
		f.record("button", "update_row", st.button, false, st.enabled)
		kind := popups.BtnOutline
		switch {
		case !st.enabled:
			kind = popups.BtnDisabled
		case st.primary:
			kind = popups.BtnPrimary
		}
		bcall, bd = measured(gtx, func(gtx layout.Context) layout.Dimensions {
			// as wide as its label: the page may come with exact
			// constraints, which would stretch it over the text
			gtx.Constraints = layout.Constraints{Max: image.Pt(f.w/2, gtx.Dp(36))}
			return popups.Button(gtx, a.th, b, kind, popups.BtnMedium, pt(st.button), "", 0)
		})
	}
	lx := gtx.Dp(theme.NativeCheckSize + 12)
	tw := f.w
	if bd.Size.X > 0 {
		tw = max(lx+gtx.Dp(120), f.w-bd.Size.X-gtx.Dp(16))
	}
	textW := tw - lx
	lh := gtx.Dp(22)
	// the subtext first, recorded: the box's click area covers the label
	// and it, and the row is as tall as they are
	hint := pt(st.hint())
	notes := &a.update.notesRow
	hcall, hd := measured(gtx, func(gtx layout.Context) layout.Dimensions {
		if st.notes {
			h := linkPara(gtx, a.th, textW, 15, 22, pc.subtext, pc.link, hint, pt(updDialogLink), notes)
			return layout.Dimensions{Size: image.Pt(textW, h)}
		}
		gtx.Constraints = layout.Constraints{Max: image.Pt(textW, 1<<20)}
		d := a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: pc.subtext}, hint)
		d.Size.Y = max(d.Size.Y, lh)
		return d
	})
	textH := gtx.Dp(6) + lh + gtx.Dp(2) + hd.Size.Y
	box := p.btn("update_auto")
	at(gtx, image.Pt(0, f.y), image.Pt(tw, textH), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, box, nil, func(gtx layout.Context) layout.Dimensions {
			cs := theme.NativeCheckState{On: on, Hover: box.Hovered()}
			at(gtx, image.Pt(0, gtx.Dp(4)), image.Pt(gtx.Dp(theme.NativeCheckSize), gtx.Dp(theme.NativeCheckSize)), func(gtx layout.Context) layout.Dimensions {
				return theme.NativeCheck(gtx, pc.dark, cs)
			})
			at(gtx, image.Pt(lx, 0), image.Pt(textW, lh+gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 15, theme.Regular, pc.text, label)
			})
			return layout.Dimensions{Size: image.Pt(tw, textH)}
		})
	})
	// the subtext above the box's click area, so its link takes clicks
	place(gtx, hcall, image.Pt(lx, f.y+lh+gtx.Dp(2)))
	if clicked(box) {
		a.toggleAutoUpdates()
		a.Invalidate()
	}
	if clicked(notes) {
		url := a.update.info.URL
		if url == "" {
			url = releasesPage
		}
		prefsOpenURL(url)
	}
	y := f.y + textH
	if st.bar {
		// the composer's upload bar: 4dp, the track the text colour at
		// 15%, the bar the link colour
		y += gtx.Dp(6)
		h := max(gtx.Dp(4), 2)
		o := op.Offset(image.Pt(lx, y)).Push(gtx.Ops)
		theme.ProgressBar(gtx, image.Rect(0, 0, textW, h), pc.text, pc.link, st.frac)
		o.Pop()
		y += h + gtx.Dp(2)
	}
	rowH := max(y-f.y, bd.Size.Y)
	if bd.Size.X > 0 {
		place(gtx, bcall, image.Pt(f.w-bd.Size.X, f.y+(rowH-bd.Size.Y)/2))
		if hit && st.enabled {
			a.updateRowClick(st.button)
		}
	}
	f.y += rowH + gtx.Dp(4)
}

// menuCheckForUpdates is the workspace menu's update item on Linux and
// Windows (macOS has it in the menu bar), nil without update checks. It
// does what its title says (updateMenuItem).
func (a *App) menuCheckForUpdates() func() {
	return a.menuCheckForUpdatesOn(runtime.GOOS)
}

func (a *App) menuCheckForUpdatesOn(goos string) func() {
	if goos == "darwin" || a.opts.CheckUpdate == nil {
		return nil
	}
	return a.updateMenuClick
}
